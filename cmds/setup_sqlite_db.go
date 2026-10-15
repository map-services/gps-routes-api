package cmds

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/mattn/go-sqlite3"
)

// setupSQLiteDB opens a SQLite database at dbPath (or :memory: when dryRun is
// true), probes for FTS5 availability, then applies the schema. When FTS5 is
// unavailable it falls back to createSchemaWithoutFTS5 (basic schema, no
// full-text search). The schema is read from db/migrations/00002_sqlite_schema.sql
// which is a relative path — callers must run from the repository root.
func setupSQLiteDB(dbPath string, dryRun bool) error {
	if dryRun {
		dbPath = ":memory:"
	}

	db, err := sql.Open("sqlite3", dbPath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// Probe FTS5 by creating (then dropping) a throwaway virtual table.
	_, err = db.Exec("CREATE VIRTUAL TABLE IF NOT EXISTS __fts5_check__ USING fts5(content)")
	if err != nil {
		log.Printf("FTS5 not available, creating schema without full-text search: %v", err)
		return createSchemaWithoutFTS5(db)
	}

	if _, err := db.Exec("DROP TABLE IF EXISTS __fts5_check__"); err != nil {
		return fmt.Errorf("failed to clean up temporary FTS5 table: %w", err)
	}

	schema, err := os.ReadFile("db/migrations/00002_sqlite_schema.sql")
	if err != nil {
		return fmt.Errorf("failed to read schema file: %w", err)
	}

	if _, err := db.Exec(string(schema)); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	return nil
}

// createSchemaWithoutFTS5 creates a basic schema (no FTS5 virtual table or
// triggers) for environments where the FTS5 module isn't compiled in.
func createSchemaWithoutFTS5(db *sql.DB) error {
	schema := `
-- SQLite3 Schema for gps-routes-api (without FTS5)
-- Basic functionality without full-text search

-- Enable required features
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;

-- Main routes table
CREATE TABLE routes (
    object_id TEXT PRIMARY KEY,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ref TEXT NOT NULL,
    title TEXT NOT NULL,
    headline_image_url TEXT,
    gpx_url TEXT,
    latitude REAL,
    longitude REAL,
    _geoloc BLOB,
    distance_km REAL,
    description TEXT NOT NULL,
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
    terrain JSON,
    points_of_interest JSON,
    facilities JSON,
    route_type TEXT,
    activities JSON,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Scalar field indexes (B-tree)
CREATE INDEX idx_routes_district ON routes (district);
CREATE INDEX idx_routes_county ON routes (county);
CREATE INDEX idx_routes_region ON routes (region);
CREATE INDEX idx_routes_country ON routes (country);
CREATE INDEX idx_routes_route_type ON routes (route_type);
CREATE INDEX idx_routes_difficulty ON routes (difficulty);

-- JSON expression indexes for array fields
CREATE INDEX idx_routes_terrain ON routes (json_extract(terrain, '$[0]'));
CREATE INDEX idx_routes_activities ON routes (json_extract(activities, '$[0]'));
CREATE INDEX idx_routes_facilities ON routes (json_extract(facilities, '$[0]'));

-- Spatial indexes (lat/lng B-tree for bounding box queries)
CREATE INDEX idx_routes_lat_lng ON routes (latitude, longitude);

-- Related tables
CREATE TABLE nearby (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    route_object_id TEXT NOT NULL REFERENCES routes(object_id) ON DELETE CASCADE,
    description TEXT,
    object_id TEXT,
    ref TEXT
);

CREATE TABLE images (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    route_object_id TEXT NOT NULL REFERENCES routes(object_id) ON DELETE CASCADE,
    src TEXT NOT NULL,
    title TEXT,
    caption TEXT
);

CREATE TABLE details (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    route_object_id TEXT NOT NULL REFERENCES routes(object_id) ON DELETE CASCADE,
    subtitle TEXT NOT NULL,
    content TEXT NOT NULL
);

-- Performance pragmas
PRAGMA cache_size = 10000;
PRAGMA synchronous = NORMAL;
PRAGMA temp_store = MEMORY;
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create basic schema: %w", err)
	}

	return nil
}
