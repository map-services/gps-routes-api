//go:build sqlite

package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/rm-hull/gps-routes-api/db"
	"github.com/rm-hull/gps-routes-api/models/common"
	"github.com/rm-hull/gps-routes-api/models/domain"
	"github.com/rm-hull/gps-routes-api/models/request"
)

func setupSQLiteTestDB(t *testing.T) (*sql.DB, func()) {
	// Create temporary database file
	tmpFile, err := os.CreateTemp("", "test_routes_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Close()

	config := db.SQLiteConfig{Path: tmpFile.Name()}
	ctx := context.Background()
	testDB, err := db.NewSQLiteDB(ctx, config)
	if err != nil {
		t.Fatalf("Failed to create SQLite DB: %v", err)
	}

	// Create tables (simplified schema for testing)
	_, err = testDB.Exec(`
		CREATE TABLE routes (
			object_id TEXT PRIMARY KEY,
			created_at DATETIME,
			ref TEXT,
			title TEXT,
			headline_image_url TEXT,
			gpx_url TEXT,
			_geoloc_lat REAL,
			_geoloc_lon REAL,
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
		testDB.Close()
		os.Remove(tmpFile.Name())
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
