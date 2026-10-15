package db

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math"
	"os"

	"github.com/mattn/go-sqlite3"
)

// init registers a "sqlite3_haversine" driver that automatically registers a
// haversine_distance(lon1, lat1, lon2, lat2) → meters scalar function on every
// new connection via ConnectHook. This replaces Spatialite's ST_DWithin for
// radius filtering and (via SQLiteDialect.BuildDistanceSort) for distance
// sorting — no extension or sqlite_math_functions build tag required.
func init() {
	sql.Register("sqlite3_haversine", &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			return conn.RegisterFunc("haversine_distance", haversineDistance, true)
		},
	})
}

// haversineDistance returns the great-circle distance between two
// (longitude, latitude) points in metres, using the mean Earth radius of
// 6371 km. The argument order (lon, lat) matches the SQL column order
// (longitude, latitude) in the routes table, so callers can write
// haversine_distance(centerLon, centerLat, longitude, latitude).
func haversineDistance(lon1, lat1, lon2, lat2 float64) float64 {
	const R = 6371000.0 // Earth mean radius in metres

	phi1 := lat1 * math.Pi / 180
	phi2 := lat2 * math.Pi / 180
	dPhi := (lat2 - lat1) * math.Pi / 180
	dLambda := (lon2 - lon1) * math.Pi / 180

	a := math.Sin(dPhi/2)*math.Sin(dPhi/2) +
		math.Cos(phi1)*math.Cos(phi2)*math.Sin(dLambda/2)*math.Sin(dLambda/2)
	return R * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

type SQLiteConfig struct {
	Path string
}

func NewSQLiteDB(ctx context.Context, config SQLiteConfig) (*sql.DB, error) {
	// Construct connection string for SQLite
	dsn := fmt.Sprintf("file:%s?cache=shared&mode=rwc&_journal_mode=WAL&_synchronous=NORMAL&_cache_size=10000&_temp_store=MEMORY", config.Path)

	log.Printf("Connecting to SQLite database at %s", config.Path)

	// Open through the haversine-enabled driver. ConnectHook fires on the first
	// connection (and every new one) to register haversine_distance(); since the
	// pool is capped at 1 conn below, this happens once.
	db, err := sql.Open("sqlite3_haversine", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening SQLite database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(1)    // SQLite works best with single writer
	db.SetMaxIdleConns(1)    // Single connection for reads too
	db.SetConnMaxLifetime(0) // No limit for SQLite

	// Verify FTS5 availability (hard requirement) and connection liveness
	if err := loadSQLiteExtensions(db); err != nil {
		return nil, fmt.Errorf("loading SQLite extensions: %w", err)
	}

	// Set pragmas for performance and integrity
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA cache_size = 10000",
		"PRAGMA temp_store = MEMORY",
	}

	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			return nil, fmt.Errorf("setting pragma %s: %w", pragma, err)
		}
	}

	// Verify connection
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("pinging SQLite database: %w", err)
	}

	return db, nil
}

func loadSQLiteExtensions(db *sql.DB) error {
	// FTS5 is an SQLite module (not a scalar function), so probe it by creating
	// (then dropping) a throwaway virtual table. FTS5 is compiled into the engine
	// when building with the sqlite_fts5 build tag; if it's missing at runtime the
	// build/tag configuration is wrong and the app should fail fast rather than
	// silently running without full-text search.
	if _, err := db.Exec("DROP TABLE IF EXISTS _fts5_probe; CREATE VIRTUAL TABLE _fts5_probe USING fts5(content)"); err != nil {
		return fmt.Errorf("FTS5 not available: %w — rebuild with -tags \"sqlite,sqlite_fts5\"", err)
	}
	if _, err := db.Exec("DROP TABLE IF EXISTS _fts5_probe"); err != nil {
		return fmt.Errorf("could not drop FTS5 probe table: %w", err)
	}

	// R*Tree is built into go-sqlite3; verify it's usable (the schema's
	// routes_rtree virtual table depends on it, and BuildSTWithinQuery emits
	// queries against it).
	if _, err := db.Exec("DROP TABLE IF EXISTS _rtree_probe; CREATE VIRTUAL TABLE _rtree_probe USING rtree(id, min_lat, max_lat, min_lon, max_lon)"); err != nil {
		return fmt.Errorf("R*Tree not available: %w — rebuild with -tags \"sqlite,sqlite_fts5\"", err)
	}
	if _, err := db.Exec("DROP TABLE IF EXISTS _rtree_probe"); err != nil {
		return fmt.Errorf("could not drop R*Tree probe table: %w", err)
	}

	return nil
}

func SQLiteConfigFromEnv() SQLiteConfig {
	dbPath := os.Getenv("SQLITE_DB_PATH")
	if dbPath == "" {
		dbPath = "data/routes.db"
	}
	return SQLiteConfig{
		Path: dbPath,
	}
}

// OptimizeDatabase runs post-import maintenance on the SQLite database:
// ANALYZE (query planner statistics), VACUUM (reclaim space), and PRAGMA
// optimize (automatic optimizations). It is intended to be called after bulk
// data import so that the query planner has up-to-date statistics and the
// database file is compacted.
func OptimizeDatabase(db *sql.DB) error {
	// Run ANALYZE to update statistics for the query planner.
	log.Println("Running ANALYZE to update query planner statistics...")
	if _, err := db.Exec("ANALYZE"); err != nil {
		return fmt.Errorf("failed to run ANALYZE: %w", err)
	}

	// VACUUM must run outside of a transaction and reclaims space from deleted
	// rows while compacting the database file.
	log.Println("Running VACUUM to compact database file...")
	if _, err := db.Exec("VACUUM"); err != nil {
		return fmt.Errorf("failed to run VACUUM: %w", err)
	}

	// PRAGMA optimize runs automatic optimizations, such as index statistics
	// updates. Log but don't fail on error — it can fail in edge cases.
	log.Println("Running PRAGMA optimize...")
	if _, err := db.Exec("PRAGMA optimize"); err != nil {
		log.Printf("Warning: PRAGMA optimize failed: %v", err)
	}

	log.Println("Database optimization completed successfully")
	return nil
}
