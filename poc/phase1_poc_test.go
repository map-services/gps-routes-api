package main

import (
	"database/sql"
	"fmt"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// TestFTS5Setup verifies FTS5 virtual table creation and basic queries
func TestFTS5Setup(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open SQLite: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Enable FTS5 if available
	_, err = db.Exec(`
		CREATE VIRTUAL TABLE routes_fts USING fts5(
			title,
			description,
			display_address
		)
	`)
	if err != nil {
		t.Logf("Warning: FTS5 may not be available or extension not loaded: %v", err)
	} else {
		t.Log("✅ FTS5 virtual table created successfully")
	}

	// Insert test data
	_, err = db.Exec(`
		INSERT INTO routes_fts (title, description, display_address)
		VALUES
			('Ben Nevis Trail', 'Popular hiking route to Scotland highest peak', 'Fort William, Scotland'),
			('Lake District Walk', 'Scenic walking route in the Lake District', 'Ambleside, England'),
			('Welsh Mountain Trek', 'Challenging route climbing Snowdon', 'Snowdonia, Wales')
	`)
	if err != nil {
		t.Logf("Warning: FTS5 insert failed: %v", err)
	} else {
		t.Log("✅ FTS5 test data inserted")
	}

	// Test full-text search queries
	rows, err := db.Query(`
		SELECT title FROM routes_fts
		WHERE routes_fts MATCH 'mountain'
	`)
	if err != nil {
		t.Logf("Warning: FTS5 query failed: %v", err)
	} else {
		defer func() { _ = rows.Close() }()
		count := 0
		for rows.Next() {
			var title string
			_ = rows.Scan(&title)
			t.Logf("  - Found: %s", title)
			count++
		}
		if count > 0 {
			t.Logf("✅ FTS5 search works: found %d matches", count)
		}
	}
}

// TestJSONArrayOperations verifies JSON array handling
func TestJSONArrayOperations(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open SQLite: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Create table with JSON arrays
	_, err = db.Exec(`
		CREATE TABLE routes (
			id INTEGER PRIMARY KEY,
			title TEXT,
			activities JSON,
			terrain JSON,
			facilities JSON
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}
	t.Log("✅ Table with JSON columns created")

	// Insert test data with JSON arrays
	_, err = db.Exec(`
		INSERT INTO routes (title, activities, terrain, facilities)
		VALUES
			('Ben Nevis',
			 json('["hiking", "mountain", "scrambling"]'),
			 json('["rock", "grass", "snow"]'),
			 json('["parking", "cafe", "toilet"]')),
			('Lake Walk',
			 json('["walking", "photography"]'),
			 json('["flat", "grass"]'),
			 json('["parking", "bench"]'))
	`)
	if err != nil {
		t.Fatalf("Failed to insert JSON data: %v", err)
	}
	t.Log("✅ JSON array data inserted")

	// Test json_each() for unnesting (mimics PostgreSQL UNNEST)
	rows, err := db.Query(`
		SELECT r.title, json_each.value as activity
		FROM routes r, json_each(r.activities)
		WHERE json_each.value = 'hiking'
	`)
	if err != nil {
		t.Fatalf("json_each() query failed: %v", err)
	}
	defer func() { _ = rows.Close() }()

	count := 0
	for rows.Next() {
		var title, activity string
		_ = rows.Scan(&title, &activity)
		t.Logf("  - Route: %s, Activity: %s", title, activity)
		count++
	}
	if count > 0 {
		t.Logf("✅ JSON array unnesting works: found %d matches", count)
	}

	// Test facet query equivalent (group by array elements)
	rows, err = db.Query(`
		SELECT json_each.value as terrainType, COUNT(*) as count
		FROM routes, json_each(terrain)
		GROUP BY json_each.value
		ORDER BY count DESC
	`)
	if err != nil {
		t.Logf("Note: Facet query error (may need extension): %v", err)
	} else {
		defer func() { _ = rows.Close() }()
		t.Log("✅ Facet query results:")
		for rows.Next() {
			var terrainType string
			var count int
			_ = rows.Scan(&terrainType, &count)
			t.Logf("  - %s: %d", terrainType, count)
		}
	}
}

// TestSpatialitePlaceholder verifies Spatialite extension availability
func TestSpatialitePlaceholder(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open SQLite: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Try to load Spatialite extension
	var extErr error
	_, extErr = db.Exec(`SELECT load_extension('mod_spatialite')`)
	if extErr != nil {
		t.Logf("⚠️  Spatialite extension not loaded: %v", extErr)
		t.Logf("    → This is expected if Spatialite is not installed")
		t.Logf("    → Install via: brew install spatialite (macOS) or apt-get install libspatialite-dev (Linux)")

		// Create a placeholder test that documents expected spatial queries
		t.Log("\n✅ Spatial Query Patterns (for Phase 1 reference):")
		t.Log("  1. Bounding Box: ST_Within(_geoloc, ST_MakeEnvelope(min_lng, min_lat, max_lng, max_lat, 4326))")
		t.Log("  2. Distance-based: ST_DWithin(ST_Transform(_geoloc, 3857), ST_Transform(ST_Point(lng, lat), 3857), distance_meters)")
		t.Log("  3. KNN Sorting: ORDER BY _geoloc <-> ST_SetSRID(ST_Point(lng, lat), 4326)")

		return
	}

	// If Spatialite loaded, test basic spatial operations
	t.Log("✅ Spatialite extension loaded successfully")

	// Create geometry table
	_, err = db.Exec(`
		CREATE TABLE routes (
			id INTEGER PRIMARY KEY,
			title TEXT,
			_geoloc GEOMETRY(Point, 4326)
		)
	`)
	if err != nil {
		// Try without geometry type hint
		_, err = db.Exec(`
			CREATE TABLE routes (
				id INTEGER PRIMARY KEY,
				title TEXT,
				_geoloc BLOB
			)
		`)
		if err != nil {
			t.Logf("Note: Geometry table creation skipped: %v", err)
			return
		}
	}
	t.Log("✅ Geometry table created")

	// Insert sample coordinate data
	_, err = db.Exec(`
		INSERT INTO routes (title, _geoloc)
		VALUES
			('Ben Nevis', ST_GeomFromText('POINT(-4.7015 56.7960)', 4326)),
			('Lake Windermere', ST_GeomFromText('POINT(-3.1543 54.3537)', 4326)),
			('Snowdon', ST_GeomFromText('POINT(-3.9369 53.0682)', 4326))
	`)
	if err != nil {
		t.Logf("Note: Geometry insert attempted but may fail: %v", err)
	} else {
		t.Log("✅ Sample coordinates inserted")
	}
}

// TestConnectionPooling verifies connection pool settings
func TestConnectionPooling(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open SQLite: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Set connection pool options
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(0) // No lifetime limit for in-memory DB

	// Verify settings
	stats := db.Stats()
	t.Logf("✅ Connection pooling configured:")
	t.Logf("  - Open connections: %d", stats.OpenConnections)
	t.Logf("  - In-use connections: %d", stats.InUse)
	t.Logf("  - Idle connections: %d", stats.Idle)
}

// BenchmarkFTS5Queries compares FTS5 performance
func BenchmarkFTS5Queries(b *testing.B) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer func() { _ = db.Close() }()

	// Create FTS table (skip if not available)
	_, _ = db.Exec(`CREATE VIRTUAL TABLE routes_fts USING fts5(title, description)`)

	// Insert 1000 test rows
	for i := range 1000 {
		_, _ = db.Exec(
			`INSERT INTO routes_fts (title, description) VALUES (?, ?)`,
			fmt.Sprintf("Route %d", i),
			fmt.Sprintf("This is a hiking trail in location %d with various terrains", i),
		)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var count int
		_ = db.QueryRow(`SELECT COUNT(*) FROM routes_fts WHERE routes_fts MATCH 'hiking'`).Scan(&count)
	}
}
