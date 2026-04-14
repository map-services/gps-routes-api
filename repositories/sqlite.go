//go:build sqlite

package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"

	"github.com/rm-hull/gps-routes-api/db"
	"github.com/rm-hull/gps-routes-api/models/common"
	"github.com/rm-hull/gps-routes-api/models/domain"
	"github.com/rm-hull/gps-routes-api/models/request"
)

type SQLiteDbRepository struct {
	db *sql.DB
}

var DEFAULT_ARRAY_FIELDS_SQLITE = []string{"activities", "terrain", "facilities", "points_of_interest"}

func NewSQLiteRouteRepository(db *sql.DB) *SQLiteDbRepository {
	return &SQLiteDbRepository{db: db}
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
		route.ObjectID, route.CreatedAt, route.Ref, route.Title, route.HeadlineImageUrl,
		route.GpxUrl, route.StartPosition.Longitude, route.StartPosition.Latitude, route.DistanceKm,
		route.Description, route.VideoUrl, route.DisplayAddress, route.Postcode, route.District,
		route.County, route.Region, route.State, route.Country, route.EstimatedDuration,
		route.Difficulty, string(terrainJSON), string(pointsOfInterestJSON), string(facilitiesJSON),
		route.RouteType, string(activitiesJSON))
	if err != nil {
		return fmt.Errorf("inserting route: %w", err)
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

	query, params := db.NewQueryBuilder(dialect, selectPart, criteria).
		WithArrayFields(DEFAULT_ARRAY_FIELDS_SQLITE...).
		WithWhereClause(fmt.Sprintf("%s IS NOT NULL", facetField)).
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
