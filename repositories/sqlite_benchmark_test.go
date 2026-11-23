package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/map-services/gps-routes-api/db"
	"github.com/map-services/gps-routes-api/models/common"
	"github.com/map-services/gps-routes-api/models/domain"
	"github.com/map-services/gps-routes-api/models/request"
)

// benchmarkDB sets up a temp-file SQLite database with the full schema
// (routes, routes_rtree, routes_fts, plus triggers) and seeds it with
// N synthetic routes spread across the UK for spatial queries.
// It returns the db handle, a cleanup function, and the repo instance.
func benchmarkDB(b *testing.B, n int) (*sql.DB, func(), *SQLiteDbRepository) {
	b.Helper()

	dbh, cleanup := setupBenchmarkDB(b)
	repo := NewSQLiteRouteRepository(dbh)

	ctx := context.Background()
	routes := generateRoutes(b, n)

	// Use StoreBatch for efficient bulk insert
	if err := repo.StoreBatch(ctx, routes); err != nil {
		b.Fatalf("seed data: %v", err)
	}

	// Run ANALYZE so the query planner has statistics (matches production after import)
	if _, err := dbh.ExecContext(ctx, "ANALYZE"); err != nil {
		b.Fatalf("ANALYZE: %v", err)
	}

	return dbh, cleanup, repo
}

func setupBenchmarkDB(b *testing.B) (*sql.DB, func()) {
	b.Helper()

	tmpFile, err := os.CreateTemp("", "bench_routes_*.db")
	if err != nil {
		b.Fatalf("create temp file: %v", err)
	}
	_ = tmpFile.Close()

	config := db.SQLiteConfig{Path: tmpFile.Name()}
	ctx := context.Background()
	testDB, err := db.NewSQLiteDB(ctx, config)
	if err != nil {
		b.Fatalf("create SQLite DB: %v", err)
	}

	// Full schema matching migrations/00002_sqlite_schema.sql (minus indexes
	// that are not needed for benchmark correctness).
	schema := `
		CREATE TABLE routes (
			object_id TEXT PRIMARY KEY,
			created_at DATETIME,
			ref TEXT,
			title TEXT,
			headline_image_url TEXT,
			gpx_url TEXT,
			latitude REAL,
			longitude REAL,
			distance_km REAL,
			description TEXT,
			video_url TEXT,
			display_address TEXT,
			postcode TEXT,
			district TEXT,
			county TEXT,
			region TEXT,
			state TEXT,
			country TEXT,
			estimated_duration TEXT,
			difficulty TEXT,
			terrain TEXT,
			points_of_interest TEXT,
			facilities TEXT,
			route_type TEXT,
			activities TEXT
		);

		CREATE VIRTUAL TABLE routes_rtree USING rtree(
			id, min_lat, max_lat, min_lon, max_lon
		);

		CREATE TRIGGER routes_rtree_insert AFTER INSERT ON routes BEGIN
			INSERT OR REPLACE INTO routes_rtree (id, min_lat, max_lat, min_lon, max_lon)
			VALUES (NEW.rowid, NEW.latitude, NEW.latitude, NEW.longitude, NEW.longitude);
		END;

		CREATE TRIGGER routes_rtree_update AFTER UPDATE ON routes BEGIN
			INSERT OR REPLACE INTO routes_rtree (id, min_lat, max_lat, min_lon, max_lon)
			VALUES (NEW.rowid, NEW.latitude, NEW.latitude, NEW.longitude, NEW.longitude);
		END;

		CREATE TRIGGER routes_rtree_delete AFTER DELETE ON routes BEGIN
			DELETE FROM routes_rtree WHERE id = OLD.rowid;
		END;

		CREATE VIRTUAL TABLE routes_fts USING fts5(
			title,
			display_address,
			description,
			content=routes,
			content_rowid=rowid
		);

		CREATE TRIGGER routes_fts_insert AFTER INSERT ON routes BEGIN
			INSERT INTO routes_fts (rowid, title, display_address, description)
			VALUES (NEW.rowid, NEW.title, NEW.display_address, NEW.description);
		END;

		CREATE TRIGGER routes_fts_update AFTER UPDATE ON routes BEGIN
			UPDATE routes_fts
			SET title = NEW.title, display_address = NEW.display_address, description = NEW.description
			WHERE rowid = NEW.rowid;
		END;

		CREATE TRIGGER routes_fts_delete AFTER DELETE ON routes BEGIN
			DELETE FROM routes_fts WHERE rowid = OLD.rowid;
		END;

		CREATE TABLE nearby (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			route_object_id TEXT NOT NULL,
			description TEXT,
			object_id TEXT,
			ref TEXT
		);

		CREATE TABLE images (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			route_object_id TEXT NOT NULL,
			src TEXT NOT NULL,
			title TEXT,
			caption TEXT
		);

		CREATE TABLE details (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			route_object_id TEXT NOT NULL,
			subtitle TEXT NOT NULL,
			content TEXT NOT NULL
		);
	`
	if _, err := testDB.ExecContext(ctx, schema); err != nil {
		_ = testDB.Close()
		b.Fatalf("create schema: %v", err)
	}

	cleanup := func() {
		_ = testDB.Close()
		_ = os.Remove(tmpFile.Name())
	}
	return testDB, cleanup
}

// UK location data for generating realistic test routes
var ukDistricts = []string{"London", "Manchester", "Birmingham", "Leeds", "Liverpool",
	"Bristol", "Edinburgh", "Glasgow", "Cardiff", "Newcastle", "Sheffield", "Nottingham",
	"Leicester", "Southampton", "Plymouth", "Oxford", "Cambridge", "York", "Bath", "Exeter"}
var ukCounties = []string{"Greater London", "Greater Manchester", "West Midlands", "West Yorkshire",
	"Merseyside", "Avon", "Midlothian", "Lanarkshire", "South Glamorgan", "Tyne and Wear",
	"South Yorkshire", "Nottinghamshire", "Leicestershire", "Hampshire", "Devon",
	"Oxfordshire", "Cambridgeshire", "North Yorkshire", "Somerset", "Devon"}
var ukDifficulties = []string{"easy", "moderate", "challenging"}
var ukRouteTypes = []string{"circular", "one-way", "out-and-back"}
var ukTerrain = [][]string{
	{"grass", "path"},
	{"rock", "woodland"},
	{"waterside", "woodland"},
	{"field", "path"},
	{"coastal", "cliff"},
	{"mountain", "rock"},
	{"forest", "trail"},
	{"urban", "paved"},
}
var ukActivities = [][]string{
	{"walking"},
	{"hiking"},
	{"cycling", "walking"},
	{"running"},
	{"mountain biking"},
	{"nature watching", "walking"},
}
var ukFacilities = [][]string{
	{"parking"},
	{"parking", "toilets"},
	{"pub/cafe", "parking"},
	{"dog friendly"},
	{"toilets", "pub/cafe", "parking"},
}
var ukPOIs = [][]string{
	{"church"},
	{"viewpoint", "church"},
	{"waterfall"},
	{"castle", "viewpoint"},
	{"manor house"},
	{"lake", "wildlife"},
}

// UK bounding box for random coordinate generation
const ukLatMin, ukLatMax = 50.0, 58.7
const ukLonMin, ukLonMax = -8.0, 1.8

var benchRng *rand.Rand

func init() {
	benchRng = rand.New(rand.NewSource(42)) // deterministic for reproducibility
}

func generateRoutes(b *testing.B, n int) []*domain.RouteMetadata {
	b.Helper()

	routes := make([]*domain.RouteMetadata, n)
	for i := 0; i < n; i++ {
		district := ukDistricts[i%len(ukDistricts)]
		county := ukCounties[i%len(ukCounties)]
		lat := ukLatMin + benchRng.Float64()*(ukLatMax-ukLatMin)
		lon := ukLonMin + benchRng.Float64()*(ukLonMax-ukLonMin)

		terrain := ukTerrain[i%len(ukTerrain)]
		activities := ukActivities[i%len(ukActivities)]
		facilities := ukFacilities[i%len(ukFacilities)]
		pois := ukPOIs[i%len(ukPOIs)]

		title := fmt.Sprintf("Route %d through %s", i, district)
		description := fmt.Sprintf(
			"This is a scenic route through %s, %s. The route passes through beautiful countryside "+
				"with views of local landmarks including historic churches and ancient woodlands. "+
				"Enjoy the peaceful trails and stunning vistas along this %s difficulty trail.",
			district, county, ukDifficulties[i%len(ukDifficulties)])
		displayAddr := fmt.Sprintf("%s, %s, England, United Kingdom", district, county)

		routes[i] = &domain.RouteMetadata{
			ObjectID:          fmt.Sprintf("bench-route-%06d", i),
			Ref:               fmt.Sprintf("bench-%06d", i),
			Title:             title,
			Description:       description,
			StartPosition:     common.GeoLoc{Latitude: lat, Longitude: lon},
			DistanceKm:        1 + benchRng.Float64()*50,
			CreatedAt:         time.Now().AddDate(0, 0, -i),
			DisplayAddress:    &displayAddr,
			District:          &district,
			County:            &county,
			Country:           strPtr("United Kingdom"),
			State:             strPtr("England"),
			Difficulty:        strPtr(ukDifficulties[i%len(ukDifficulties)]),
			RouteType:         strPtr(ukRouteTypes[i%len(ukRouteTypes)]),
			EstimatedDuration: strPtr("1 day"),
			Terrain:           terrain,
			Activities:        activities,
			Facilities:        facilities,
			PointsOfInterest:  pois,
		}
	}
	return routes
}

// BenchmarkSearchSimple — basic search with no filters, just LIMIT/OFFSET
func BenchmarkSearchSimple(b *testing.B) {
	_, cleanup, repo := benchmarkDB(b, 500)
	defer cleanup()

	ctx := context.Background()
	criteria := &request.SearchRequest{Limit: 10}

	b.ResetTimer()
	for b.Loop() {
		_, _ = repo.SearchHits(ctx, criteria)
	}
}

// BenchmarkSearchFTS — full-text search with bm25 sorting
func BenchmarkSearchFTS(b *testing.B) {
	_, cleanup, repo := benchmarkDB(b, 500)
	defer cleanup()

	ctx := context.Background()
	criteria := &request.SearchRequest{
		Query: "scenic countryside historic",
		Limit: 10,
	}

	b.ResetTimer()
	for b.Loop() {
		_, _ = repo.SearchHits(ctx, criteria)
	}
}

// BenchmarkSearchBbox — R*Tree bounding box query
func BenchmarkSearchBbox(b *testing.B) {
	_, cleanup, repo := benchmarkDB(b, 500)
	defer cleanup()

	ctx := context.Background()
	// Box around London area
	criteria := &request.SearchRequest{
		BoundingBox: &common.BoundingBox{MinLng: -0.5, MinLat: 51.3, MaxLng: 0.1, MaxLat: 51.7},
		Limit:       10,
	}

	b.ResetTimer()
	for b.Loop() {
		_, _ = repo.SearchHits(ctx, criteria)
	}
}

// BenchmarkSearchRadius — R*Tree prefilter + haversine distance exact filter
func BenchmarkSearchRadius(b *testing.B) {
	_, cleanup, repo := benchmarkDB(b, 500)
	defer cleanup()

	ctx := context.Background()
	criteria := &request.SearchRequest{
		Nearby: &request.Nearby{
			Center:     &common.GeoLoc{Latitude: 51.5074, Longitude: -0.1278},
			DistanceKm: 50,
		},
		Limit: 10,
	}

	b.ResetTimer()
	for b.Loop() {
		_, _ = repo.SearchHits(ctx, criteria)
	}
}

// BenchmarkSearchFTSWithBbox — combined FTS + R*Tree bbox query
func BenchmarkSearchFTSWithBbox(b *testing.B) {
	_, cleanup, repo := benchmarkDB(b, 500)
	defer cleanup()

	ctx := context.Background()
	criteria := &request.SearchRequest{
		Query:       "countryside",
		BoundingBox: &common.BoundingBox{MinLng: -3.0, MinLat: 51.0, MaxLng: 2.0, MaxLat: 58.0},
		Limit:       10,
	}

	b.ResetTimer()
	for b.Loop() {
		_, _ = repo.SearchHits(ctx, criteria)
	}
}

// BenchmarkSearchWithFacets — search with facet filtering (JSON array overlap)
func BenchmarkSearchWithFacets(b *testing.B) {
	_, cleanup, repo := benchmarkDB(b, 500)
	defer cleanup()

	ctx := context.Background()
	criteria := &request.SearchRequest{
		Facets: map[string][]string{
			"activities": {"walking"},
			"district":   {"London"},
		},
		Limit: 10,
	}

	b.ResetTimer()
	for b.Loop() {
		_, _ = repo.SearchHits(ctx, criteria)
	}
}

// BenchmarkFacetCounts — facet count query with json_each (unnested arrays)
func BenchmarkFacetCounts(b *testing.B) {
	_, cleanup, repo := benchmarkDB(b, 500)
	defer cleanup()

	ctx := context.Background()
	criteria := &request.SearchRequest{}

	b.ResetTimer()
	for b.Loop() {
		_, _ = repo.FacetCounts(ctx, criteria, "activities", 10, true)
	}
}

// BenchmarkFacetCountsScalar — facet count query on scalar field (no json_each)
func BenchmarkFacetCountsScalar(b *testing.B) {
	_, cleanup, repo := benchmarkDB(b, 500)
	defer cleanup()

	ctx := context.Background()
	criteria := &request.SearchRequest{}

	b.ResetTimer()
	for b.Loop() {
		_, _ = repo.FacetCounts(ctx, criteria, "district", 10, false)
	}
}

// BenchmarkCountAll — COUNT(*) with no filters
func BenchmarkCountAll(b *testing.B) {
	_, cleanup, repo := benchmarkDB(b, 500)
	defer cleanup()

	ctx := context.Background()

	b.ResetTimer()
	for b.Loop() {
		_, _ = repo.CountAll(ctx, &request.SearchRequest{})
	}
}

// BenchmarkCountAllFTS — COUNT(*) with FTS filter
func BenchmarkCountAllFTS(b *testing.B) {
	_, cleanup, repo := benchmarkDB(b, 500)
	defer cleanup()

	ctx := context.Background()
	criteria := &request.SearchRequest{Query: "countryside"}

	b.ResetTimer()
	for b.Loop() {
		_, _ = repo.CountAll(ctx, criteria)
	}
}

// BenchmarkStoreBatch — bulk insert performance
func BenchmarkStoreBatch(b *testing.B) {
	for n := 10; n <= 100; n *= 10 {
		b.Run(fmt.Sprintf("%droutes", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_, cleanup, repo := benchmarkDBEmpty(b)
				ctx := context.Background()
				routes := generateRoutes(b, n)
				_ = repo.StoreBatch(ctx, routes)
				cleanup()
			}
		})
	}
}

// benchmarkDBEmpty creates a DB with schema but no seeded data
func benchmarkDBEmpty(b *testing.B) (*sql.DB, func(), *SQLiteDbRepository) {
	dbh, cleanup := setupBenchmarkDB(b)
	repo := NewSQLiteRouteRepository(dbh)
	return dbh, cleanup, repo
}

// BenchmarkHaversineFunction — raw haversine_distance() SQL function call
func BenchmarkHaversineFunction(b *testing.B) {
	dbh, cleanup := setupBenchmarkDB(b)
	defer cleanup()

	b.ResetTimer()
	for b.Loop() {
		var dist float64
		_ = dbh.QueryRowContext(context.Background(),
			"SELECT haversine_distance(-0.1278, 51.5074, -74.0060, 40.7128)").Scan(&dist)
	}
}

// BenchmarkHaversineOverTable — haversine_distance() computed for every row
// (simulates distance sort without R*Tree prefilter)
func BenchmarkHaversineOverTable(b *testing.B) {
	dbh, cleanup, _ := benchmarkDB(b, 500)
	defer cleanup()

	b.ResetTimer()
	for b.Loop() {
		rows, err := dbh.QueryContext(context.Background(),
			"SELECT object_id FROM routes ORDER BY haversine_distance(-0.1278, 51.5074, longitude, latitude) ASC LIMIT 10")
		if err != nil {
			b.Fatal(err)
		}
		_ = rows.Close()
	}
}

// BenchmarkSearchLargeDataset — same benchmarks but with 5000 routes to
// test scaling behavior
func BenchmarkSearchLargeDataset(b *testing.B) {
	_, cleanup, repo := benchmarkDB(b, 5000)
	defer cleanup()
	ctx := context.Background()

	b.Run("Simple", func(b *testing.B) {
		criteria := &request.SearchRequest{Limit: 10}
		b.ResetTimer()
		for b.Loop() {
			_, _ = repo.SearchHits(ctx, criteria)
		}
	})

	b.Run("FTS", func(b *testing.B) {
		criteria := &request.SearchRequest{Query: "scenic countryside", Limit: 10}
		b.ResetTimer()
		for b.Loop() {
			_, _ = repo.SearchHits(ctx, criteria)
		}
	})

	b.Run("Bbox", func(b *testing.B) {
		criteria := &request.SearchRequest{
			BoundingBox: &common.BoundingBox{MinLng: -3.0, MinLat: 51.0, MaxLng: 2.0, MaxLat: 58.0},
			Limit:       10,
		}
		b.ResetTimer()
		for b.Loop() {
			_, _ = repo.SearchHits(ctx, criteria)
		}
	})

	b.Run("Radius", func(b *testing.B) {
		criteria := &request.SearchRequest{
			Nearby: &request.Nearby{
				Center:     &common.GeoLoc{Latitude: 51.5074, Longitude: -0.1278},
				DistanceKm: 50,
			},
			Limit: 10,
		}
		b.ResetTimer()
		for b.Loop() {
			_, _ = repo.SearchHits(ctx, criteria)
		}
	})

	b.Run("FTS+Bbox", func(b *testing.B) {
		criteria := &request.SearchRequest{
			Query:       "countryside",
			BoundingBox: &common.BoundingBox{MinLng: -3.0, MinLat: 51.0, MaxLng: 2.0, MaxLat: 58.0},
			Limit:       10,
		}
		b.ResetTimer()
		for b.Loop() {
			_, _ = repo.SearchHits(ctx, criteria)
		}
	})
}
