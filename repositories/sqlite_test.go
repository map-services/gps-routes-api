package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/map-services/gps-routes-api/db"
	"github.com/map-services/gps-routes-api/models/common"
	"github.com/map-services/gps-routes-api/models/domain"
	"github.com/map-services/gps-routes-api/models/request"
	_ "github.com/mattn/go-sqlite3"
)

func setupSQLiteTestDB(t *testing.T) (*sql.DB, func()) {
	// Create temporary database file
	tmpFile, err := os.CreateTemp("", "test_routes_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	_ = tmpFile.Close()

	config := db.SQLiteConfig{Path: tmpFile.Name()}
	ctx := context.Background()
	testDB, err := db.NewSQLiteDB(ctx, config)
	if err != nil {
		t.Fatalf("Failed to create SQLite DB: %v", err)
	}

	// Create tables (simplified schema for testing). Includes routes_rtree
	// (R*Tree virtual table + sync triggers) so spatial bbox queries work
	// via SQLiteDialect.BuildSTWithinQuery - no Spatialite extension needed.
	_, err = testDB.Exec(`
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

		CREATE TABLE nearby (
			route_object_id TEXT,
			description TEXT,
			object_id TEXT,
			ref TEXT
		);

		CREATE TABLE images (
			route_object_id TEXT,
			src TEXT,
			title TEXT,
			caption TEXT
		);

		CREATE TABLE details (
			route_object_id TEXT,
			subtitle TEXT,
			content TEXT
		);
	`)
	if err != nil {
		t.Fatalf("Failed to create tables: %v", err)
	}

	cleanup := func() {
		_ = testDB.Close()
		_ = os.Remove(tmpFile.Name())
	}

	return testDB, cleanup
}

func TestSQLiteDbRepository_Store(t *testing.T) {
	testDB, cleanup := setupSQLiteTestDB(t)
	defer cleanup()

	repo := NewSQLiteRouteRepository(testDB)
	ctx := context.Background()

	headlineImageUrl := "http://example.com/image.jpg"
	videoUrl := "http://example.com/video.mp4"
	displayAddress := "London, UK"
	postcode := "SW1A 1AA"
	district := "Westminster"
	county := "Greater London"
	region := "London"
	state := ""
	country := "United Kingdom"
	estimatedDuration := "2 hours"
	difficulty := "Easy"
	routeType := "Walking"

	route := &domain.RouteMetadata{
		ObjectID:          "test-route-1",
		Ref:               "REF001",
		Title:             "Test Route",
		Description:       "A test route",
		HeadlineImageUrl:  &headlineImageUrl,
		GpxUrl:            "http://example.com/route.gpx",
		StartPosition:     common.GeoLoc{Latitude: 51.5074, Longitude: -0.1278},
		DistanceKm:        10.5,
		VideoUrl:          &videoUrl,
		DisplayAddress:    &displayAddress,
		Postcode:          &postcode,
		District:          &district,
		County:            &county,
		Region:            &region,
		State:             &state,
		Country:           &country,
		EstimatedDuration: &estimatedDuration,
		Difficulty:        &difficulty,
		Terrain:           []string{"Grass", "Path"},
		PointsOfInterest:  []string{"Park", "Lake"},
		Facilities:        []string{"Parking", "Toilets"},
		RouteType:         &routeType,
		Activities:        []string{"Hiking", "Photography"},
		CreatedAt:         time.Now(),
		Images: []domain.Image{
			{Src: "http://example.com/img1.jpg", Title: "Image 1", Caption: "Caption 1"},
		},
		Nearby: []domain.RouteSummary{
			{ObjectID: "nearby-1", Ref: "NR001", Title: "Nearby Route", Description: "A nearby route"},
		},
		Details: []domain.Detail{
			{Subtitle: "Detail 1", Content: "Content 1"},
		},
	}

	err := repo.Store(ctx, route)
	if err != nil {
		t.Fatalf("Failed to store route: %v", err)
	}

	// Verify the route was stored
	retrieved, err := repo.FindByObjectID(ctx, "test-route-1")
	if err != nil {
		t.Fatalf("Failed to retrieve route: %v", err)
	}
	if retrieved == nil {
		t.Fatal("Route not found")
	}
	if retrieved.Title != "Test Route" {
		t.Errorf("Expected title 'Test Route', got '%s'", retrieved.Title)
	}
}

func TestSQLiteDbRepository_ConcurrentWrites(t *testing.T) {
	testDB, cleanup := setupSQLiteTestDB(t)
	defer cleanup()

	repo := NewSQLiteRouteRepository(testDB)
	ctx := context.Background()

	var wg sync.WaitGroup
	numGoroutines := 5
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			route := &domain.RouteMetadata{
				ObjectID:      fmt.Sprintf("concurrent-route-%d", id),
				Ref:           fmt.Sprintf("REF%03d", id),
				Title:         fmt.Sprintf("Concurrent Route %d", id),
				Description:   "Test route for concurrency",
				GpxUrl:        "http://example.com/route.gpx",
				StartPosition: common.GeoLoc{Latitude: 51.5074 + float64(id)*0.01, Longitude: -0.1278},
				DistanceKm:    float64(id) * 1.5,
				CreatedAt:     time.Now(),
			}

			err := repo.Store(ctx, route)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("Concurrent write failed: %v", err)
	}

	// Verify all routes were stored
	for i := 0; i < numGoroutines; i++ {
		retrieved, err := repo.FindByObjectID(ctx, fmt.Sprintf("concurrent-route-%d", i))
		if err != nil {
			t.Errorf("Failed to retrieve route %d: %v", i, err)
		}
		if retrieved == nil {
			t.Errorf("Route %d not found", i)
		}
	}
}

func TestSQLiteDbRepository_CountAll(t *testing.T) {
	testDB, cleanup := setupSQLiteTestDB(t)
	defer cleanup()

	repo := NewSQLiteRouteRepository(testDB)
	ctx := context.Background()

	// Insert test data
	for i := 0; i < 3; i++ {
		route := &domain.RouteMetadata{
			ObjectID:      fmt.Sprintf("count-test-%d", i),
			Ref:           fmt.Sprintf("CT%03d", i),
			Title:         fmt.Sprintf("Count Test Route %d", i),
			Description:   "Test route for counting",
			GpxUrl:        "http://example.com/route.gpx",
			StartPosition: common.GeoLoc{Latitude: 51.5074, Longitude: -0.1278},
			DistanceKm:    5.0,
			CreatedAt:     time.Now(),
		}
		err := repo.Store(ctx, route)
		if err != nil {
			t.Fatalf("Failed to store test route: %v", err)
		}
	}

	count, err := repo.CountAll(ctx, &request.SearchRequest{})
	if err != nil {
		t.Fatalf("Failed to count routes: %v", err)
	}
	if count != 3 {
		t.Errorf("Expected count 3, got %d", count)
	}
}

func strPtr(s string) *string { return &s }

// TestSQLiteStoreNormalizesEmptyStringsToNull verifies Store coerces empty
// string pointers to NULL, so blank scalar facet values are excluded downstream
// by IS NOT NULL predicates. This is the data-layer half of the empty-facet-
// bucket fix; FacetCounts_query-side "" guard (in repositories/sqlite.go
// FacetCounts) is the defense for pre-existing data.
func TestSQLiteStoreNormalizesEmptyStringsToNull(t *testing.T) {
	testDB, cleanup := setupSQLiteTestDB(t)
	defer cleanup()
	repo := NewSQLiteRouteRepository(testDB)
	ctx := context.Background()

	empty := ""
	route := &domain.RouteMetadata{
		ObjectID:      "norm-empty-1",
		Ref:           "NE1",
		Title:         "Blank District",
		Description:   "x",
		GpxUrl:        "http://x/g",
		StartPosition: common.GeoLoc{Latitude: 1, Longitude: 2},
		District:      &empty, // empty string -> must become NULL
		County:        nil,    // nil -> NULL
		Country:       strPtr("France"),
		CreatedAt:     time.Now(),
	}
	if err := repo.Store(ctx, route); err != nil {
		t.Fatalf("Store: %v", err)
	}

	var district sql.NullString
	if err := testDB.QueryRowContext(ctx,
		"SELECT district FROM routes WHERE object_id = ?", "norm-empty-1").Scan(&district); err != nil {
		t.Fatalf("scan district: %v", err)
	}
	if district.Valid {
		t.Errorf("district should be NULL after Store normalization, got %q", district.String)
	}

	var county sql.NullString
	if err := testDB.QueryRowContext(ctx,
		"SELECT county FROM routes WHERE object_id = ?", "norm-empty-1").Scan(&county); err != nil {
		t.Fatalf("scan county: %v", err)
	}
	if county.Valid {
		t.Errorf("county should be NULL, got %q", county.String)
	}

	// The empty district must not surface as a "" facet bucket. (Test schema has
	// no routes_fts, so this exercises the pure-scalar FacetCounts path.)
	facets, err := repo.FacetCounts(ctx, &request.SearchRequest{}, "district", 10, false)
	if err != nil {
		t.Fatalf("FacetCounts: %v", err)
	}
	if _, hasEmpty := (*facets)[""]; hasEmpty {
		t.Errorf("facet bucket \"\" should be absent after normalization, got %#v", *facets)
	}
}

// TestSQLiteFacetFilterWithPagination locks the parameter-ordering fix in
// db/query_builder.go Build(): a facet predicate (`district IN (SELECT value
// FROM json_each(?))`) appears textually BEFORE LIMIT/OFFSET, so SQLite's
// positional '?' binding requires params ordered [facetJSON, limit, offset].
// The previous implementation appended [limit, offset, facetJSON], binding the
// LIMIT int to json_each(?) -> 0 facet hits and an empty facet sidebar when a
// facet was combined with pagination. See the user's report:
// {"query":"","facets":{"district":["London"]},"limit":24} -> 0 hits.
func TestSQLiteFacetFilterWithPagination(t *testing.T) {
	testDB, cleanup := setupSQLiteTestDB(t)
	defer cleanup()
	repo := NewSQLiteRouteRepository(testDB)
	ctx := context.Background()

	for i, id := range []string{"r1", "r2"} {
		dist := "London"
		if i == 1 {
			dist = "Manchester"
		}
		if err := repo.Store(ctx, &domain.RouteMetadata{
			ObjectID:      id,
			Ref:           "R" + id,
			Title:         id,
			Description:   "x",
			GpxUrl:        "http://x/g",
			District:      strPtr(dist),
			StartPosition: common.GeoLoc{Latitude: 1, Longitude: 2},
			CreatedAt:     time.Now(),
		}); err != nil {
			t.Fatalf("store %s: %v", id, err)
		}
	}

	criteria := &request.SearchRequest{
		Facets: map[string][]string{"district": {"London"}},
		Limit:  24,
		Offset: 0,
	}

	hits, err := repo.SearchHits(ctx, criteria)
	if err != nil {
		t.Fatalf("SearchHits: %v", err)
	}
	if len(*hits) != 1 || (*hits)[0].ObjectID != "r1" {
		t.Fatalf("expected 1 hit (r1) for district=London, got %+v", *hits)
	}

	// Sidebar facet counts must also be populated (previously empty {} because
	// the same swapped binding zeroed out the json_each predicate).
	facets, err := repo.FacetCounts(ctx, criteria, "district", 10, false)
	if err != nil {
		t.Fatalf("FacetCounts: %v", err)
	}
	if got := (*facets)["London"]; got != 1 {
		t.Errorf("FacetCounts[London] = %d, want 1 (got %#v)", got, *facets)
	}
}

// TestSQLiteStoreLockingBbox is the regression test for "bbox matches return
// empty but do not error". It asserts the FULL stack: Store writes lat/lng, the
// routes_rtree trigger auto-populates the R*Tree index, then SearchHits'
// routes_rtree overlap predicate returns ONLY routes whose start position is
// inside the box — no Spatialite extension required, just SQLite's built-in
// R*Tree + the Go-registered haversine_distance() function for radius queries.
func TestSQLiteStoreLockingBbox(t *testing.T) {
	testDB, cleanup := setupSQLiteTestDB(t)
	defer cleanup()
	repo := NewSQLiteRouteRepository(testDB)
	ctx := context.Background()

	mustStore := func(id string, lat, lon float64) {
		t.Helper()
		if err := repo.Store(ctx, &domain.RouteMetadata{
			ObjectID:      id,
			Ref:           "R-" + id,
			Title:         id,
			Description:   "route " + id,
			GpxUrl:        "http://x/g",
			StartPosition: common.GeoLoc{Latitude: lat, Longitude: lon},
			DistanceKm:    1,
			CreatedAt:     time.Now(),
		}); err != nil {
			t.Fatalf("store %s: %v", id, err)
		}
	}
	mustStore("london", 51.5074, -0.1278) // London
	mustStore("nyc", 40.7128, -74.0060)   // New York
	mustStore("edin", 55.9533, -3.1883)   // Edinburgh (north of London box)

	// Box around London only
	londonBox := &common.BoundingBox{MinLng: -0.2, MinLat: 51.4, MaxLng: -0.1, MaxLat: 51.6}
	hits, err := repo.SearchHits(ctx, &request.SearchRequest{BoundingBox: londonBox, Limit: 10})
	if err != nil {
		t.Fatalf("SearchHits London box: %v", err)
	}
	ids := make([]string, len(*hits))
	for i, h := range *hits {
		ids[i] = h.ObjectID
	}
	if len(ids) != 1 || ids[0] != "london" {
		t.Errorf("London box hits = %v, want [london]", ids)
	}

	cnt, err := repo.CountAll(ctx, &request.SearchRequest{BoundingBox: londonBox})
	if err != nil {
		t.Fatalf("CountAll London box: %v", err)
	}
	if cnt != 1 {
		t.Errorf("CountAll London box = %d, want 1", cnt)
	}

	// Larger box spanning London + Edinburgh (but NYC at lon -74 is west of -3, excluded).
	ukBox := &common.BoundingBox{MinLng: -3.5, MinLat: 51.0, MaxLng: 2.0, MaxLat: 57.0}
	hits2, err := repo.SearchHits(ctx, &request.SearchRequest{BoundingBox: ukBox, Limit: 10})
	if err != nil {
		t.Fatalf("SearchHits UK box: %v", err)
	}
	if len(*hits2) != 2 {
		t.Errorf("UK box hits = %d, want 2 (london+edin)", len(*hits2))
	}

	// Box covering none of the points.
	farBox := &common.BoundingBox{MinLng: 10, MinLat: 60, MaxLng: 12, MaxLat: 62}
	hits3, err := repo.SearchHits(ctx, &request.SearchRequest{BoundingBox: farBox, Limit: 10})
	if err != nil {
		t.Fatalf("SearchHits far box: %v", err)
	}
	if len(*hits3) != 0 {
		t.Errorf("far box hits = %d, want 0", len(*hits3))
	}

	// Radius search: 100 km around London should match only London itself.
	// (York ~300 km, Bristol ~190 km — both excluded by haversine_distance filter.)
	nearbyHits, err := repo.SearchHits(ctx, &request.SearchRequest{
		Nearby: &request.Nearby{
			Center:     &common.GeoLoc{Latitude: 51.5074, Longitude: -0.1278},
			DistanceKm: 100,
		},
	})
	if err != nil {
		t.Fatalf("SearchHits nearby 100km: %v", err)
	}
	if len(*nearbyHits) != 1 || (*nearbyHits)[0].ObjectID != "london" {
		t.Errorf("100km radius from London = %d results, want 1", len(*nearbyHits))
	}

	// Radius search: 250 km around London should match only London.
	// (Bristol ~190 km is within range; Edinburgh ~615 km is excluded.)
	// NOTE: Bristol was not inserted in this test (only london, nyc, edin),
	// so 250 km returns just London.
	nearbyHits2, err := repo.SearchHits(ctx, &request.SearchRequest{
		Nearby: &request.Nearby{
			Center:     &common.GeoLoc{Latitude: 51.5074, Longitude: -0.1278},
			DistanceKm: 250,
		},
	})
	if err != nil {
		t.Fatalf("SearchHits nearby 250km: %v", err)
	}
	if len(*nearbyHits2) != 1 || (*nearbyHits2)[0].ObjectID != "london" {
		t.Errorf("250km radius from London = %d results, want 1", len(*nearbyHits2))
	}

	// Verify _geoloc column no longer exists — R*Tree + lat/lng replaced it entirely.
	var hasColumn int
	if err := testDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info('routes') WHERE name = '_geoloc'`).Scan(&hasColumn); err != nil {
		t.Fatalf("check _geoloc column: %v", err)
	}
	if hasColumn > 0 {
		t.Errorf("routes table should not have _geoloc column (R*Tree replaces it), but it exists")
	}
}
