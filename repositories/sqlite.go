package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"

	"github.com/map-services/gps-routes-api/db"
	"github.com/map-services/gps-routes-api/models/common"
	"github.com/map-services/gps-routes-api/models/domain"
	"github.com/map-services/gps-routes-api/models/request"
)

type SQLiteDbRepository struct {
	db         *sql.DB
	spatialite bool // probed once at construction; gates _geoloc population in Store
}

var DEFAULT_ARRAY_FIELDS_SQLITE = []string{"activities", "terrain", "facilities", "points_of_interest"}

func NewSQLiteRouteRepository(db *sql.DB) *SQLiteDbRepository {
	return &SQLiteDbRepository{
		db:         db,
		spatialite: sqliteSpatialiteAvailable(db),
	}
}

// sqliteSpatialiteAvailable reports whether mod_spatialite is genuinely loaded
// on db, by exercising the same geometry round-trip that
// db.TestSQLiteSpatialiteSpatialFunctions uses to confirm the extension is
// active (this is non-speculative: a missing library yields a NULL/error here).
//
// NewSQLiteRouteRepository probes once at construction, which is single
// threaded, so subsequent Store calls only read the cached result — no data
// race with the concurrent Stores exercised by TestSQLiteDbRepository_ConcurrentWrites.
func sqliteSpatialiteAvailable(db *sql.DB) bool {
	var wkt string
	err := db.QueryRowContext(context.Background(),
		`SELECT ST_AsText(ST_GeomFromText('POINT(0 0)', 4326))`).Scan(&wkt)
	return err == nil && wkt == "POINT(0 0)"
}

// nz returns nil for an empty-string pointer so nullable scalar columns store
// NULL when populated via the SQLite import path. The query side additionally
// guards against empty-string values in FacetCounts for databases that already
// contain them.
func nz(s *string) *string {
	if s == nil || *s == "" {
		return nil
	}
	return s
}

func (repo *SQLiteDbRepository) Store(ctx context.Context, route *domain.RouteMetadata) error {
	// Begin transaction
	tx, err := repo.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() {
		if err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				log.Printf("Error rolling back transaction: %v", rollbackErr)
			}
		}
	}()

	// Convert arrays to JSON
	terrainJSON, _ := json.Marshal(route.Terrain)
	activitiesJSON, _ := json.Marshal(route.Activities)
	facilitiesJSON, _ := json.Marshal(route.Facilities)
	pointsOfInterestJSON, _ := json.Marshal(route.PointsOfInterest)

	// Insert or replace route
	_, err = tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO routes (
		    object_id, created_at, ref, title, headline_image_url,
		    gpx_url, longitude, latitude, distance_km, description, video_url,
		    display_address, postcode, district, county, region,
			state, country, estimated_duration, difficulty, terrain,
			points_of_interest, facilities, route_type, activities
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		route.ObjectID, route.CreatedAt, route.Ref, route.Title, nz(route.HeadlineImageUrl),
		route.GpxUrl, route.StartPosition.Longitude, route.StartPosition.Latitude, route.DistanceKm,
		route.Description, nz(route.VideoUrl), nz(route.DisplayAddress), nz(route.Postcode), nz(route.District),
		nz(route.County), nz(route.Region), nz(route.State), nz(route.Country), nz(route.EstimatedDuration),
		nz(route.Difficulty), string(terrainJSON), string(pointsOfInterestJSON), string(facilitiesJSON),
		nz(route.RouteType), string(activitiesJSON))
	if err != nil {
		return fmt.Errorf("inserting route: %w", err)
	}

	// Populate the Spatialite geometry BLOB so bbox predicates
	// (ST_Within(_geoloc, BuildMbr(?, ?, ?, ?, 4326)) — emitted by
	// SQLiteDialect.BuildSTWithinQuery) filter on real coordinates instead of
	// matching NULL -> 0 rows. Empirical finding: Spatialite only decodes
	// geometry blobs it produced itself (its internal serialized format); storing
	// raw Go EWKB into a plain BLOB column leaves ST_Within/ST_AsText NULL, so
	// _geoloc MUST be built with ST_GeomFromText. This function (plus the '?'
	// WKT-param form) is the exact call path proven by
	// db.TestSQLiteSpatialiteSpatialFunctions, which runs green on CI's apt
	// libspatialite too — so this is non-speculative.
	//
	// When Spatialite isn't loaded (the documented FTS5-only fallback) Store
	// still succeeds: lat/lng were written by the INSERT above and _geoloc is
	// left NULL; spatial bbox predicates then no-op at query time, which is the
	// pre-existing — not regressed — behavior.
	if repo.spatialite {
		lon := route.StartPosition.Longitude
		lat := route.StartPosition.Latitude
		// WKT POINT is "POINT(longitude latitude)" == POINT(x y): longitude is
		// the x-axis (east/west), latitude the y-axis (north/south). Format with
		// strconv 'f'/'-1' for lossless round-trip without scientific notation.
		wkt := "POINT(" + strconv.FormatFloat(lon, 'f', -1, 64) + " " +
			strconv.FormatFloat(lat, 'f', -1, 64) + ")"
		if _, err = tx.ExecContext(ctx,
			`UPDATE routes SET _geoloc = ST_GeomFromText(?, 4326) WHERE object_id = ?`,
			wkt, route.ObjectID); err != nil {
			return fmt.Errorf("setting route _geoloc: %w", err)
		}
	}

	// Delete existing related records
	_, err = tx.ExecContext(ctx, "DELETE FROM nearby WHERE route_object_id = ?", route.ObjectID)
	if err != nil {
		return fmt.Errorf("deleting nearby: %w", err)
	}

	_, err = tx.ExecContext(ctx, "DELETE FROM images WHERE route_object_id = ?", route.ObjectID)
	if err != nil {
		return fmt.Errorf("deleting images: %w", err)
	}

	_, err = tx.ExecContext(ctx, "DELETE FROM details WHERE route_object_id = ?", route.ObjectID)
	if err != nil {
		return fmt.Errorf("deleting details: %w", err)
	}

	// Insert nearby routes
	for _, nearby := range route.Nearby {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO nearby (route_object_id, description, object_id, ref)
			VALUES (?, ?, ?, ?)`,
			route.ObjectID, nearby.Description, nearby.ObjectID, nearby.Ref)
		if err != nil {
			return fmt.Errorf("inserting nearby: %w", err)
		}
	}

	// Insert images
	for _, image := range route.Images {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO images (route_object_id, src, title, caption)
			VALUES (?, ?, ?, ?)`,
			route.ObjectID, image.Src, image.Title, image.Caption)
		if err != nil {
			return fmt.Errorf("inserting image: %w", err)
		}
	}

	// Insert details
	for _, detail := range route.Details {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO details (route_object_id, subtitle, content)
			VALUES (?, ?, ?)`,
			route.ObjectID, detail.Subtitle, detail.Content)
		if err != nil {
			return fmt.Errorf("inserting detail: %w", err)
		}
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}

func (repo *SQLiteDbRepository) FindByObjectID(ctx context.Context, objectID string) (*domain.RouteMetadata, error) {
	mainQuery := `
			SELECT
				object_id, ref, title, description, headline_image_url,
				longitude, latitude,
				created_at, gpx_url, distance_km, video_url,
				display_address, postcode, district, county,
				region, state, country, estimated_duration,
				difficulty, terrain, points_of_interest,
				facilities, route_type, activities
			FROM routes
			WHERE object_id = ?`

	var route domain.RouteMetadata
	var latitude, longitude float64
	var terrainJSON, activitiesJSON, facilitiesJSON, pointsOfInterestJSON string

	// Direct column scan
	err := repo.db.QueryRowContext(ctx, mainQuery, objectID).Scan(
		&route.ObjectID, &route.Ref, &route.Title, &route.Description,
		&route.HeadlineImageUrl, &longitude, &latitude, &route.CreatedAt,
		&route.GpxUrl, &route.DistanceKm, &route.VideoUrl, &route.DisplayAddress,
		&route.Postcode, &route.District, &route.County, &route.Region,
		&route.State, &route.Country, &route.EstimatedDuration, &route.Difficulty,
		&terrainJSON, &pointsOfInterestJSON, &facilitiesJSON, &route.RouteType, &activitiesJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scanning route: %w", err)
	}

	// Parse JSON arrays
	json.Unmarshal([]byte(terrainJSON), &route.Terrain)
	json.Unmarshal([]byte(activitiesJSON), &route.Activities)
	json.Unmarshal([]byte(facilitiesJSON), &route.Facilities)
	json.Unmarshal([]byte(pointsOfInterestJSON), &route.PointsOfInterest)

	route.StartPosition = common.GeoLoc{Latitude: latitude, Longitude: longitude}

	// Query images
	imageQuery := `SELECT src, title, caption FROM images WHERE route_object_id = ?`
	rows, err := repo.db.QueryContext(ctx, imageQuery, objectID)
	if err != nil {
		return nil, fmt.Errorf("querying images: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var image domain.Image
		if err := rows.Scan(&image.Src, &image.Title, &image.Caption); err != nil {
			return nil, fmt.Errorf("scanning image: %w", err)
		}
		route.Images = append(route.Images, image)
	}

	// Query nearby
	nearbySelectPart := `
			SELECT
				r.object_id, r.ref, r.title, r.description, r.headline_image_url,
				r.longitude, r.latitude, r.distance_km
			FROM nearby n
			INNER JOIN routes r ON n.object_id = r.object_id`

	qb := db.NewQueryBuilder(&db.SQLiteDialect{}, nearbySelectPart, &request.SearchRequest{TruncateText: true}).
		WithTruncatedField("r.title", 50).
		WithTruncatedField("r.description", 150).
		WithWhereClause("n.route_object_id = ?").
		WithParam(objectID)

	nearbyQuery, params := qb.Build()

	rows, err = repo.db.QueryContext(ctx, nearbyQuery, params...)
	if err != nil {
		return nil, fmt.Errorf("querying nearby: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var nearby domain.RouteSummary
		var latitude, longitude float64

		err := rows.Scan(&nearby.ObjectID, &nearby.Ref, &nearby.Title, &nearby.Description,
			&nearby.HeadlineImageUrl, &longitude, &latitude, &nearby.DistanceKm)
		if err != nil {
			return nil, fmt.Errorf("scanning nearby: %w", err)
		}
		nearby.StartPosition = common.GeoLoc{Latitude: latitude, Longitude: longitude}
		route.Nearby = append(route.Nearby, nearby)
	}

	// Query details
	detailsQuery := `SELECT subtitle, content FROM details WHERE route_object_id = ?`
	rows, err = repo.db.QueryContext(ctx, detailsQuery, objectID)
	if err != nil {
		return nil, fmt.Errorf("querying details: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var detail domain.Detail
		if err := rows.Scan(&detail.Subtitle, &detail.Content); err != nil {
			return nil, fmt.Errorf("scanning detail: %w", err)
		}
		route.Details = append(route.Details, detail)
	}

	return &route, nil
}

func (repo *SQLiteDbRepository) CountAll(ctx context.Context, criteria *request.SearchRequest) (int64, error) {
	selectPart := "SELECT COUNT(*) FROM routes"
	// When a full-text query is present, applyWhereConditions appends a
	// "routes_fts MATCH ?" WHERE clause; routes_fts is a separate FTS5 virtual
	// table, so it must be joined into scope (otherwise SQLite reports
	// "no such column: routes_fts"). Mirrors SearchHits' join below.
	if criteria.Query != "" {
		selectPart += " JOIN routes_fts ON routes.rowid = routes_fts.rowid"
	}
	qb := db.NewQueryBuilder(&db.SQLiteDialect{}, selectPart, criteria).
		WithArrayFields(DEFAULT_ARRAY_FIELDS_SQLITE...)

	query, params := qb.Build()

	var count int64
	err := repo.db.QueryRowContext(ctx, query, params...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting routes: %w", err)
	}
	return count, nil
}

func (repo *SQLiteDbRepository) SearchHits(ctx context.Context, criteria *request.SearchRequest) (*[]domain.RouteSummary, error) {

	selectPart := `
			SELECT
				routes.object_id, routes.ref, routes.title, routes.description,
				routes.headline_image_url, routes.longitude, routes.latitude, routes.distance_km
			FROM routes`

	if criteria.Query != "" {
		selectPart += " JOIN routes_fts ON routes.rowid = routes_fts.rowid"
	}

	dialect := &db.SQLiteDialect{}
	sortField := "routes.created_at DESC"
	if criteria.Nearby != nil && criteria.Nearby.Center != nil {
		sortField = dialect.BuildDistanceSort("_geoloc", criteria.Nearby.Center.Longitude, criteria.Nearby.Center.Latitude) + " ASC"
	} else if criteria.Query != "" {
		sortField = dialect.BuildFTSSort("routes_fts", "?") + " DESC"
	}

	qb := db.NewQueryBuilder(dialect, selectPart, criteria).
		WithArrayFields(DEFAULT_ARRAY_FIELDS_SQLITE...).
		WithOrderBy(sortField).
		WithLimit(criteria.Limit).
		WithOffset(criteria.Offset)

	if criteria.TruncateText {
		qb.WithTruncatedField("routes.title", 50)
		qb.WithTruncatedField("routes.description", 150)
	}

	query, params := qb.Build()
	rows, err := repo.db.QueryContext(ctx, query, params...)
	if err != nil {
		return nil, fmt.Errorf("executing search query: %w", err)
	}
	defer rows.Close()

	results := make([]domain.RouteSummary, 0, criteria.Limit)
	var summary domain.RouteSummary
	var latitude, longitude float64

	for rows.Next() {
		err := rows.Scan(&summary.ObjectID, &summary.Ref, &summary.Title,
			&summary.Description, &summary.HeadlineImageUrl, &longitude, &latitude, &summary.DistanceKm)
		if err != nil {
			return nil, fmt.Errorf("scanning summary: %w", err)
		}

		summary.StartPosition = common.GeoLoc{Latitude: latitude, Longitude: longitude}
		results = append(results, summary)
	}

	return &results, nil
}

func (repo *SQLiteDbRepository) FacetCounts(ctx context.Context, criteria *request.SearchRequest, facetField string, limit int32, unnest bool, excludeFacets ...string) (*map[string]int64, error) {
	results := make(map[string]int64)

	dialect := &db.SQLiteDialect{}
	selectPart := dialect.BuildFacetQuery(facetField, unnest)
	// routes_fts is a separate FTS5 virtual table; when a full-text query is
	// active, applyWhereConditions adds "routes_fts MATCH ?" to the WHERE, so the
	// table must be joined into scope here (mirrors SearchHits). Without this,
	// SQLite errors "no such column: routes_fts" on facet search-by-text.
	if criteria.Query != "" {
		selectPart += " JOIN routes_fts ON routes.rowid = routes_fts.rowid"
	}

	query, params := db.NewQueryBuilder(dialect, selectPart, criteria).
		WithArrayFields(DEFAULT_ARRAY_FIELDS_SQLITE...).
		// Exclude both NULL and empty string. The SQLite JSON import path stores
		// scalar facet values as '' instead of NULL, so we must also drop the
		// empty-string bucket. Array facets go through json_each; a non-empty
		// JSON column is never == '', so this filter is a no-op there.
		WithWhereClause(fmt.Sprintf("%s IS NOT NULL AND %s <> ''", facetField, facetField)).
		WithExcludeFacets(excludeFacets...).
		WithGroupBy("key").
		WithOrderBy("value DESC").
		WithLimit(limit).
		Build()

	rows, err := repo.db.QueryContext(ctx, query, params...)
	if err != nil {
		return nil, fmt.Errorf("executing facet query: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		var value int64

		err = rows.Scan(&name, &value)
		if err != nil {
			return nil, fmt.Errorf("scanning facet row: %w", err)
		}

		results[name] = value
	}

	return &results, nil
}
