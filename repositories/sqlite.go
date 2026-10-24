package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"

	"github.com/map-services/gps-routes-api/db"
	"github.com/map-services/gps-routes-api/models/common"
	"github.com/map-services/gps-routes-api/models/domain"
	"github.com/map-services/gps-routes-api/models/request"
)

type SQLiteDbRepository struct {
	db *sql.DB
}

// defaultLimit returns the limit to use when the caller doesn't specify one.
// A zero-value Limit (0) means "use the default" — without this, SQLite would
// receive LIMIT 0 and return no rows.
func defaultLimit(limit int32) int32 {
	if limit <= 0 {
		return 10
	}
	return limit
}

var DEFAULT_ARRAY_FIELDS_SQLITE = []string{"activities", "terrain", "facilities", "points_of_interest"}

func NewSQLiteRouteRepository(db *sql.DB) *SQLiteDbRepository {
	return &SQLiteDbRepository{db: db}
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
	return repo.StoreBatch(ctx, []*domain.RouteMetadata{route})
}

// StoreBatch inserts multiple routes in a single transaction using prepared
// statements. This is significantly faster than calling Store() for each route
// individually, especially during bulk imports, because it avoids per-route
// transaction overhead and batches FTS/R*Tree trigger execution.
func (repo *SQLiteDbRepository) StoreBatch(ctx context.Context, routes []*domain.RouteMetadata) error {
	if len(routes) == 0 {
		return nil
	}

	// Begin a single transaction for all routes
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

	// Prepare statements once and reuse for all routes
	insertRouteSQL := `
		INSERT OR REPLACE INTO routes (
		    object_id, created_at, ref, title, headline_image_url,
		    gpx_url, longitude, latitude, distance_km, description, video_url,
		    display_address, postcode, district, county, region,
			state, country, estimated_duration, difficulty, terrain,
			points_of_interest, facilities, route_type, activities
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	insertRouteStmt, err := tx.PrepareContext(ctx, insertRouteSQL)
	if err != nil {
		return fmt.Errorf("preparing route insert: %w", err)
	}
	defer func() { _ = insertRouteStmt.Close() }()

	deleteNearbySQL := "DELETE FROM nearby WHERE route_object_id = ?"
	deleteImagesSQL := "DELETE FROM images WHERE route_object_id = ?"
	deleteDetailsSQL := "DELETE FROM details WHERE route_object_id = ?"

	insertNearbySQL := `INSERT INTO nearby (route_object_id, description, object_id, ref) VALUES (?, ?, ?, ?)`
	insertNearbyStmt, err := tx.PrepareContext(ctx, insertNearbySQL)
	if err != nil {
		return fmt.Errorf("preparing nearby insert: %w", err)
	}
	defer func() { _ = insertNearbyStmt.Close() }()

	insertImageSQL := `INSERT INTO images (route_object_id, src, title, caption) VALUES (?, ?, ?, ?)`
	insertImageStmt, err := tx.PrepareContext(ctx, insertImageSQL)
	if err != nil {
		return fmt.Errorf("preparing image insert: %w", err)
	}
	defer func() { _ = insertImageStmt.Close() }()

	insertDetailSQL := `INSERT INTO details (route_object_id, subtitle, content) VALUES (?, ?, ?)`
	insertDetailStmt, err := tx.PrepareContext(ctx, insertDetailSQL)
	if err != nil {
		return fmt.Errorf("preparing detail insert: %w", err)
	}
	defer func() { _ = insertDetailStmt.Close() }()

	// Insert all routes
	for _, route := range routes {
		// Convert arrays to JSON
		terrainJSON, _ := json.Marshal(route.Terrain)
		activitiesJSON, _ := json.Marshal(route.Activities)
		facilitiesJSON, _ := json.Marshal(route.Facilities)
		pointsOfInterestJSON, _ := json.Marshal(route.PointsOfInterest)

		// Insert or replace route
		_, err = insertRouteStmt.ExecContext(ctx,
			route.ObjectID, route.CreatedAt, route.Ref, route.Title, nz(route.HeadlineImageUrl),
			route.GpxUrl, route.StartPosition.Longitude, route.StartPosition.Latitude, route.DistanceKm,
			route.Description, nz(route.VideoUrl), nz(route.DisplayAddress), nz(route.Postcode), nz(route.District),
			nz(route.County), nz(route.Region), nz(route.State), nz(route.Country), nz(route.EstimatedDuration),
			nz(route.Difficulty), string(terrainJSON), string(pointsOfInterestJSON), string(facilitiesJSON),
			nz(route.RouteType), string(activitiesJSON))
		if err != nil {
			return fmt.Errorf("inserting route %s: %w", route.ObjectID, err)
		}

		// Delete existing related records
		_, err = tx.ExecContext(ctx, deleteNearbySQL, route.ObjectID)
		if err != nil {
			return fmt.Errorf("deleting nearby for %s: %w", route.ObjectID, err)
		}
		_, err = tx.ExecContext(ctx, deleteImagesSQL, route.ObjectID)
		if err != nil {
			return fmt.Errorf("deleting images for %s: %w", route.ObjectID, err)
		}
		_, err = tx.ExecContext(ctx, deleteDetailsSQL, route.ObjectID)
		if err != nil {
			return fmt.Errorf("deleting details for %s: %w", route.ObjectID, err)
		}

		// Insert nearby routes
		for _, nearby := range route.Nearby {
			_, err = insertNearbyStmt.ExecContext(ctx,
				route.ObjectID, nearby.Description, nearby.ObjectID, nearby.Ref)
			if err != nil {
				return fmt.Errorf("inserting nearby for %s: %w", route.ObjectID, err)
			}
		}

		// Insert images
		for _, image := range route.Images {
			_, err = insertImageStmt.ExecContext(ctx,
				route.ObjectID, image.Src, image.Title, image.Caption)
			if err != nil {
				return fmt.Errorf("inserting image for %s: %w", route.ObjectID, err)
			}
		}

		// Insert details
		for _, detail := range route.Details {
			_, err = insertDetailStmt.ExecContext(ctx,
				route.ObjectID, detail.Subtitle, detail.Content)
			if err != nil {
				return fmt.Errorf("inserting detail for %s: %w", route.ObjectID, err)
			}
		}
	}

	// Commit the single transaction for all routes
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
	if err := json.Unmarshal([]byte(terrainJSON), &route.Terrain); err != nil {
		return nil, fmt.Errorf("unmarshalling terrain: %w", err)
	}
	if err := json.Unmarshal([]byte(activitiesJSON), &route.Activities); err != nil {
		return nil, fmt.Errorf("unmarshalling activities: %w", err)
	}
	if err := json.Unmarshal([]byte(facilitiesJSON), &route.Facilities); err != nil {
		return nil, fmt.Errorf("unmarshalling facilities: %w", err)
	}
	if err := json.Unmarshal([]byte(pointsOfInterestJSON), &route.PointsOfInterest); err != nil {
		return nil, fmt.Errorf("unmarshalling points_of_interest: %w", err)
	}

	route.StartPosition = common.GeoLoc{Latitude: latitude, Longitude: longitude}

	// Query images
	imageQuery := `SELECT src, title, caption FROM images WHERE route_object_id = ?`
	rows, err := repo.db.QueryContext(ctx, imageQuery, objectID)
	if err != nil {
		return nil, fmt.Errorf("querying images: %w", err)
	}
	defer func() { _ = rows.Close() }()

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
	defer func() { _ = rows.Close() }()

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
	defer func() { _ = rows.Close() }()

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

// buildSortExpression builds the SQL ORDER BY expression based on the Sort criteria.
// If no explicit sort is provided, it falls back to defaults:
// - nearby → distance from center point
// - query → bm25 relevance
// - otherwise → created_at DESC
func buildSortExpression(criteria *request.SearchRequest, dialect *db.SQLiteDialect) string {
	if criteria.Sort != nil && common.IsValidSortField(criteria.Sort.Field) {
		return buildExplicitSortExpression(criteria.Sort)
	}

	// Default sort logic
	if criteria.Nearby != nil && criteria.Nearby.Center != nil {
		return dialect.BuildDistanceSort("_geoloc", criteria.Nearby.Center.Longitude, criteria.Nearby.Center.Latitude) + " ASC"
	}
	if criteria.Query != "" {
		return dialect.BuildFTSSort("routes_fts", "?") + " DESC"
	}
	return "routes.created_at DESC"
}

// buildExplicitSortExpression builds the SQL ORDER BY expression for an explicit sort.
func buildExplicitSortExpression(sort *common.Sort) string {
	direction := sort.Direction
	if direction == "" {
		// Default direction: ASC for most fields, DESC for relevance
		if sort.Field == "relevance" {
			direction = "DESC"
		} else {
			direction = "ASC"
		}
	}

	var expr string
	switch sort.Field {
	case "distance_km":
		expr = "routes.distance_km"
	case "difficulty":
		expr = "CASE WHEN routes.difficulty = 'easy' THEN 1 WHEN routes.difficulty = 'moderate' THEN 2 WHEN routes.difficulty = 'hard' THEN 3 ELSE 4 END"
	case "estimated_duration":
		expr = "routes.distance_km * CASE WHEN routes.difficulty = 'easy' THEN 1.0 WHEN routes.difficulty = 'moderate' THEN 1.4 WHEN routes.difficulty = 'hard' THEN 2.5 ELSE 1.5 END"
	case "title":
		expr = "routes.title"
	case "relevance":
		expr = "-bm25(routes_fts, 1.0, 0.4, 0.1)"
	default:
		expr = "routes.created_at"
	}

	return expr + " " + direction
}

func (repo *SQLiteDbRepository) SearchHits(ctx context.Context, criteria *request.SearchRequest) (*[]domain.RouteSummary, error) {

	selectPart := `
			SELECT
				routes.object_id, routes.ref, routes.title, routes.description,
				routes.headline_image_url, routes.longitude, routes.latitude, routes.distance_km
			FROM routes`

	// Add routes_fts join if needed for query or relevance sort
	needsFTS := criteria.Query != ""
	if criteria.Sort != nil && criteria.Sort.Field == "relevance" {
		needsFTS = true
	}
	if needsFTS {
		selectPart += " JOIN routes_fts ON routes.rowid = routes_fts.rowid"
	}

	dialect := &db.SQLiteDialect{}
	sortField := buildSortExpression(criteria, dialect)

	qb := db.NewQueryBuilder(dialect, selectPart, criteria).
		WithArrayFields(DEFAULT_ARRAY_FIELDS_SQLITE...).
		WithOrderBy(sortField).
		WithLimit(defaultLimit(criteria.Limit)).
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
	defer func() { _ = rows.Close() }()

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

	// Determine the GROUP BY expression and the NULL-check expression.
	// SQLite does not group correctly on SELECT-list aliases from table-valued
	// functions (e.g. "json_each.value AS key" — GROUP BY key produces duplicate
	// buckets). Always reference the actual expression instead.
	groupByExpr := facetField
	nullCheckExpr := facetField
	if unnest {
		groupByExpr = "json_each.value"
		nullCheckExpr = "json_each.value"
	}

	query, params := db.NewQueryBuilder(dialect, selectPart, criteria).
		WithArrayFields(DEFAULT_ARRAY_FIELDS_SQLITE...).
		// Exclude both NULL and empty string. The SQLite JSON import path stores
		// scalar facet values as '' instead of NULL, so we must also drop the
		// empty-string bucket. Array facets go through json_each; a non-empty
		// JSON column is never == '', so this filter is a no-op there.
		// Also exclude NULL keys from json_each results when array elements are null.
		WithWhereClause(fmt.Sprintf("%s IS NOT NULL AND %s <> '' AND %s IS NOT NULL", facetField, facetField, nullCheckExpr)).
		WithExcludeFacets(excludeFacets...).
		WithGroupBy(groupByExpr).
		WithOrderBy("value DESC").
		WithLimit(limit).
		Build()

	rows, err := repo.db.QueryContext(ctx, query, params...)
	if err != nil {
		return nil, fmt.Errorf("executing facet query: %w", err)
	}
	defer func() { _ = rows.Close() }()

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
