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

// TestSQLiteSortDistanceKm verifies sorting by distance_km in both ASC and DESC directions.
func TestSQLiteSortDistanceKm(t *testing.T) {
	testDB, cleanup := setupSQLiteTestDB(t)
	defer cleanup()
	repo := NewSQLiteRouteRepository(testDB)
	ctx := context.Background()

	short := "easy"
	medium := "moderate"
	hard := "hard"

	mustStore := func(id string, dist float64, diff string) {
		t.Helper()
		if err := repo.Store(ctx, &domain.RouteMetadata{
			ObjectID:      id,
			Ref:           "R-" + id,
			Title:         id,
			Description:   "route " + id,
			GpxUrl:        "http://x/g",
			StartPosition: common.GeoLoc{Latitude: 51.5, Longitude: -0.1},
			DistanceKm:    dist,
			Difficulty:    &diff,
			CreatedAt:     time.Now(),
		}); err != nil {
			t.Fatalf("store %s: %v", id, err)
		}
	}
	mustStore("short", 2.0, short)
	mustStore("medium", 10.0, medium)
	mustStore("long", 25.0, hard)

	// Sort by distance_km ASC — shortest first
	hits, err := repo.SearchHits(ctx, &request.SearchRequest{
		Sort:  &common.Sort{Field: "distance_km", Direction: "ASC"},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("SearchHits distance_km ASC: %v", err)
	}
	if len(*hits) != 3 {
		t.Fatalf("expected 3 hits, got %d", len(*hits))
	}
	if (*hits)[0].ObjectID != "short" || (*hits)[1].ObjectID != "medium" || (*hits)[2].ObjectID != "long" {
		t.Errorf("distance_km ASC order = %v, want [short medium long]", extractIDs(*hits))
	}

	// Sort by distance_km DESC — longest first
	hits, err = repo.SearchHits(ctx, &request.SearchRequest{
		Sort:  &common.Sort{Field: "distance_km", Direction: "DESC"},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("SearchHits distance_km DESC: %v", err)
	}
	if (*hits)[0].ObjectID != "long" || (*hits)[1].ObjectID != "medium" || (*hits)[2].ObjectID != "short" {
		t.Errorf("distance_km DESC order = %v, want [long medium short]", extractIDs(*hits))
	}
}

// TestSQLiteSortDifficulty verifies sorting by difficulty level (ordinal: easy < moderate < hard).
func TestSQLiteSortDifficulty(t *testing.T) {
	testDB, cleanup := setupSQLiteTestDB(t)
	defer cleanup()
	repo := NewSQLiteRouteRepository(testDB)
	ctx := context.Background()

	easy := "easy"
	moderate := "moderate"
	hard := "hard"

	mustStore := func(id string, diff string) {
		t.Helper()
		if err := repo.Store(ctx, &domain.RouteMetadata{
			ObjectID:      id,
			Ref:           "R-" + id,
			Title:         id,
			Description:   "route " + id,
			GpxUrl:        "http://x/g",
			StartPosition: common.GeoLoc{Latitude: 51.5, Longitude: -0.1},
			DistanceKm:    5.0,
			Difficulty:    &diff,
			CreatedAt:     time.Now(),
		}); err != nil {
			t.Fatalf("store %s: %v", id, err)
		}
	}
	mustStore("hard-route", hard)
	mustStore("easy-route", easy)
	mustStore("moderate-route", moderate)

	// Sort by difficulty ASC — easy first
	hits, err := repo.SearchHits(ctx, &request.SearchRequest{
		Sort:  &common.Sort{Field: "difficulty", Direction: "ASC"},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("SearchHits difficulty ASC: %v", err)
	}
	if len(*hits) != 3 {
		t.Fatalf("expected 3 hits, got %d", len(*hits))
	}
	if (*hits)[0].ObjectID != "easy-route" || (*hits)[1].ObjectID != "moderate-route" || (*hits)[2].ObjectID != "hard-route" {
		t.Errorf("difficulty ASC order = %v, want [easy-route moderate-route hard-route]", extractIDs(*hits))
	}

	// Sort by difficulty DESC — hard first
	hits, err = repo.SearchHits(ctx, &request.SearchRequest{
		Sort:  &common.Sort{Field: "difficulty", Direction: "DESC"},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("SearchHits difficulty DESC: %v", err)
	}
	if (*hits)[0].ObjectID != "hard-route" || (*hits)[1].ObjectID != "moderate-route" || (*hits)[2].ObjectID != "easy-route" {
		t.Errorf("difficulty DESC order = %v, want [hard-route moderate-route easy-route]", extractIDs(*hits))
	}
}

// TestSQLiteSortEstimatedDuration verifies sorting by composite effort score (distance × difficulty multiplier).
func TestSQLiteSortEstimatedDuration(t *testing.T) {
	testDB, cleanup := setupSQLiteTestDB(t)
	defer cleanup()
	repo := NewSQLiteRouteRepository(testDB)
	ctx := context.Background()

	easy := "easy"
	hard := "hard"

	mustStore := func(id string, dist float64, diff string) {
		t.Helper()
		if err := repo.Store(ctx, &domain.RouteMetadata{
			ObjectID:      id,
			Ref:           "R-" + id,
			Title:         id,
			Description:   "route " + id,
			GpxUrl:        "http://x/g",
			StartPosition: common.GeoLoc{Latitude: 51.5, Longitude: -0.1},
			DistanceKm:    dist,
			Difficulty:    &diff,
			CreatedAt:     time.Now(),
		}); err != nil {
			t.Fatalf("store %s: %v", id, err)
		}
	}
	// short-hard: 2.0 * 2.5 = 5.0
	mustStore("short-hard", 2.0, hard)
	// long-easy: 20.0 * 1.0 = 20.0
	mustStore("long-easy", 20.0, easy)
	// medium-hard: 10.0 * 2.5 = 25.0
	mustStore("medium-hard", 10.0, hard)

	// Sort by estimated_duration ASC — lowest effort first
	hits, err := repo.SearchHits(ctx, &request.SearchRequest{
		Sort:  &common.Sort{Field: "estimated_duration", Direction: "ASC"},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("SearchHits estimated_duration ASC: %v", err)
	}
	if len(*hits) != 3 {
		t.Fatalf("expected 3 hits, got %d", len(*hits))
	}
	if (*hits)[0].ObjectID != "short-hard" || (*hits)[1].ObjectID != "long-easy" || (*hits)[2].ObjectID != "medium-hard" {
		t.Errorf("estimated_duration ASC order = %v, want [short-hard long-easy medium-hard]", extractIDs(*hits))
	}

	// Sort by estimated_duration DESC — highest effort first
	hits, err = repo.SearchHits(ctx, &request.SearchRequest{
		Sort:  &common.Sort{Field: "estimated_duration", Direction: "DESC"},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("SearchHits estimated_duration DESC: %v", err)
	}
	if (*hits)[0].ObjectID != "medium-hard" || (*hits)[1].ObjectID != "long-easy" || (*hits)[2].ObjectID != "short-hard" {
		t.Errorf("estimated_duration DESC order = %v, want [medium-hard long-easy short-hard]", extractIDs(*hits))
	}
}

// TestSQLiteSortTitle verifies alphabetical sorting by title.
func TestSQLiteSortTitle(t *testing.T) {
	testDB, cleanup := setupSQLiteTestDB(t)
	defer cleanup()
	repo := NewSQLiteRouteRepository(testDB)
	ctx := context.Background()

	mustStore := func(id, title string) {
		t.Helper()
		if err := repo.Store(ctx, &domain.RouteMetadata{
			ObjectID:      id,
			Ref:           "R-" + id,
			Title:         title,
			Description:   "route " + id,
			GpxUrl:        "http://x/g",
			StartPosition: common.GeoLoc{Latitude: 51.5, Longitude: -0.1},
			DistanceKm:    5.0,
			CreatedAt:     time.Now(),
		}); err != nil {
			t.Fatalf("store %s: %v", id, err)
		}
	}
	mustStore("z-id", "Zebra Trail")
	mustStore("a-id", "Alpha Path")
	mustStore("m-id", "Middle Route")

	// Sort by title ASC — alphabetical
	hits, err := repo.SearchHits(ctx, &request.SearchRequest{
		Sort:  &common.Sort{Field: "title", Direction: "ASC"},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("SearchHits title ASC: %v", err)
	}
	if len(*hits) != 3 {
		t.Fatalf("expected 3 hits, got %d", len(*hits))
	}
	if (*hits)[0].ObjectID != "a-id" || (*hits)[1].ObjectID != "m-id" || (*hits)[2].ObjectID != "z-id" {
		t.Errorf("title ASC order = %v, want [a-id m-id z-id]", extractIDs(*hits))
	}

	// Sort by title DESC — reverse alphabetical
	hits, err = repo.SearchHits(ctx, &request.SearchRequest{
		Sort:  &common.Sort{Field: "title", Direction: "DESC"},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("SearchHits title DESC: %v", err)
	}
	if (*hits)[0].ObjectID != "z-id" || (*hits)[1].ObjectID != "m-id" || (*hits)[2].ObjectID != "a-id" {
		t.Errorf("title DESC order = %v, want [z-id m-id a-id]", extractIDs(*hits))
	}
}

// TestSQLiteSortDefault verifies default sort behavior when no explicit sort is provided.
func TestSQLiteSortDefault(t *testing.T) {
	testDB, cleanup := setupSQLiteTestDB(t)
	defer cleanup()
	repo := NewSQLiteRouteRepository(testDB)
	ctx := context.Background()

	easy := "easy"

	// Insert routes with different created_at times to verify created_at DESC default
	oldTime := time.Now().Add(-24 * time.Hour)
	newTime := time.Now()

	mustStore := func(id string, createdAt time.Time) {
		t.Helper()
		if err := repo.Store(ctx, &domain.RouteMetadata{
			ObjectID:      id,
			Ref:           "R-" + id,
			Title:         id,
			Description:   "route " + id,
			GpxUrl:        "http://x/g",
			StartPosition: common.GeoLoc{Latitude: 51.5, Longitude: -0.1},
			DistanceKm:    5.0,
			Difficulty:    &easy,
			CreatedAt:     createdAt,
		}); err != nil {
			t.Fatalf("store %s: %v", id, err)
		}
	}
	mustStore("old-route", oldTime)
	mustStore("new-route", newTime)

	// No sort, no query, no nearby → created_at DESC (newest first)
	hits, err := repo.SearchHits(ctx, &request.SearchRequest{
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("SearchHits default: %v", err)
	}
	if len(*hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(*hits))
	}
	if (*hits)[0].ObjectID != "new-route" || (*hits)[1].ObjectID != "old-route" {
		t.Errorf("default sort order = %v, want [new-route old-route]", extractIDs(*hits))
	}
}

// TestSQLiteSortDirectionDefault verifies that omitting direction defaults to ASC.
func TestSQLiteSortDirectionDefault(t *testing.T) {
	testDB, cleanup := setupSQLiteTestDB(t)
	defer cleanup()
	repo := NewSQLiteRouteRepository(testDB)
	ctx := context.Background()

	short := "easy"
	hard := "hard"

	mustStore := func(id string, dist float64, diff string) {
		t.Helper()
		if err := repo.Store(ctx, &domain.RouteMetadata{
			ObjectID:      id,
			Ref:           "R-" + id,
			Title:         id,
			Description:   "route " + id,
			GpxUrl:        "http://x/g",
			StartPosition: common.GeoLoc{Latitude: 51.5, Longitude: -0.1},
			DistanceKm:    dist,
			Difficulty:    &diff,
			CreatedAt:     time.Now(),
		}); err != nil {
			t.Fatalf("store %s: %v", id, err)
		}
	}
	mustStore("long", 25.0, hard)
	mustStore("short", 2.0, short)

	// Sort with field only, no direction — should default to ASC
	hits, err := repo.SearchHits(ctx, &request.SearchRequest{
		Sort:  &common.Sort{Field: "distance_km"},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("SearchHits distance_km default direction: %v", err)
	}
	if (*hits)[0].ObjectID != "short" || (*hits)[1].ObjectID != "long" {
		t.Errorf("distance_km with no direction (default ASC) = %v, want [short long]", extractIDs(*hits))
	}
}

// TestSQLiteSortOverridesNearbyDefault verifies explicit sort takes precedence over nearby distance default.
func TestSQLiteSortOverridesNearbyDefault(t *testing.T) {
	testDB, cleanup := setupSQLiteTestDB(t)
	defer cleanup()
	repo := NewSQLiteRouteRepository(testDB)
	ctx := context.Background()

	hard := "hard"
	easy := "easy"

	mustStore := func(id string, lat, lon float64, dist float64, diff string) {
		t.Helper()
		if err := repo.Store(ctx, &domain.RouteMetadata{
			ObjectID:      id,
			Ref:           "R-" + id,
			Title:         id,
			Description:   "route " + id,
			GpxUrl:        "http://x/g",
			StartPosition: common.GeoLoc{Latitude: lat, Longitude: lon},
			DistanceKm:    dist,
			Difficulty:    &diff,
			CreatedAt:     time.Now(),
		}); err != nil {
			t.Fatalf("store %s: %v", id, err)
		}
	}
	// Close to center but long distance
	mustStore("close-long", 51.51, -0.13, 25.0, hard)
	// Far from center but short distance
	mustStore("far-short", 52.0, -1.0, 2.0, easy)

	// Nearby without explicit sort → distance from center (close-long first)
	hits, err := repo.SearchHits(ctx, &request.SearchRequest{
		Nearby: &request.Nearby{
			Center:     &common.GeoLoc{Latitude: 51.5074, Longitude: -0.1278},
			DistanceKm: 100,
		},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("SearchHits nearby default: %v", err)
	}
	if len(*hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(*hits))
	}
	if (*hits)[0].ObjectID != "close-long" {
		t.Errorf("nearby default sort first = %s, want close-long", (*hits)[0].ObjectID)
	}

	// Nearby with explicit sort by distance_km DESC → longest route first
	hits, err = repo.SearchHits(ctx, &request.SearchRequest{
		Nearby: &request.Nearby{
			Center:     &common.GeoLoc{Latitude: 51.5074, Longitude: -0.1278},
			DistanceKm: 100,
		},
		Sort:  &common.Sort{Field: "distance_km", Direction: "DESC"},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("SearchHits nearby with explicit sort: %v", err)
	}
	if (*hits)[0].ObjectID != "close-long" || (*hits)[1].ObjectID != "far-short" {
		t.Errorf("nearby + distance_km DESC = %v, want [close-long far-short]", extractIDs(*hits))
	}
}

// extractIDs returns the ObjectID values from a slice of RouteSummary.
func extractIDs(hits []domain.RouteSummary) []string {
	ids := make([]string, len(hits))
	for i, h := range hits {
		ids[i] = h.ObjectID
	}
	return ids
}
