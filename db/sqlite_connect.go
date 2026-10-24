package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"log"
	"math"
	"os"

	mosqlite "modernc.org/sqlite"
)

// init registers haversine_distance(lon1, lat1, lon2, lat2) → meters as a
// deterministic scalar SQL function on the "sqlite" driver (modernc.org/sqlite).
// This replaces Spatialite's ST_DWithin for radius filtering and (via
// SQLiteDialect.BuildDistanceSort) for distance sorting.
func init() {
	mosqlite.MustRegisterDeterministicScalarFunction("haversine_distance", 4, func(ctx *mosqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		lon1 := args[0].(float64)
		lat1 := args[1].(float64)
		lon2 := args[2].(float64)
		lat2 := args[3].(float64)
		return haversineDistance(lon1, lat1, lon2, lat2), nil
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
	Path     string
	ReadOnly bool
}

func NewSQLiteDB(ctx context.Context, config SQLiteConfig) (*sql.DB, error) {
	// Construct connection string for SQLite (modernc.org/sqlite format).
	var dsn string
	if config.ReadOnly {
		// Read-only mode: open the database without write access. Multiple
		// concurrent readers are allowed, so the pool is larger (see below).
		dsn = fmt.Sprintf("file:%s?mode=ro&cache=shared&_busy_timeout=5000", config.Path)
		log.Printf("Connecting to SQLite database at %s (read-only)", config.Path)
	} else {
		// Read-write mode: used by the import command and tests.
		dsn = fmt.Sprintf("file:%s?cache=shared&mode=rwc&_journal_mode=WAL&_synchronous=NORMAL&_cache_size=10000&_temp_store=MEMORY", config.Path)
		log.Printf("Connecting to SQLite database at %s", config.Path)
	}

	// Open through the modernc "sqlite" driver. The haversine_distance() function
	// is registered globally in init() via MustRegisterDeterministicScalarFunction.
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening SQLite database: %w", err)
	}

	// Configure connection pool based on mode.
	if config.ReadOnly {
		// Read-only: allow multiple concurrent readers.
		db.SetMaxOpenConns(25)
		db.SetMaxIdleConns(5)
	} else {
		// Read-write: SQLite allows only one writer at a time.
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	}
	db.SetConnMaxLifetime(0) // No limit for SQLite

	// Verify FTS5 and R*Tree availability (hard requirement) and connection liveness.
	if err := loadSQLiteExtensions(db); err != nil {
		return nil, fmt.Errorf("loading SQLite extensions: %w", err)
	}

	// Set pragmas for performance and integrity (write mode only — read-only
	// connections cannot set pragmas that modify the database).
	if !config.ReadOnly {
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
	}

	// Verify connection
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("pinging SQLite database: %w", err)
	}

	return db, nil
}

// loadSQLiteExtensions verifies that FTS5 and R*Tree virtual tables are
// available. With modernc.org/sqlite these are always built-in, so we just
// check the compile options (read-only safe).
func loadSQLiteExtensions(db *sql.DB) error {
	var fts5Count, rtreeCount int
	if err := db.QueryRow("SELECT count(*) FROM pragma_compile_options WHERE compile_options LIKE '%FTS5%'").Scan(&fts5Count); err != nil {
		return fmt.Errorf("checking FTS5 availability: %w", err)
	}
	if fts5Count == 0 {
		return fmt.Errorf("FTS5 not available in SQLite build")
	}

	if err := db.QueryRow("SELECT count(*) FROM pragma_compile_options WHERE compile_options LIKE '%RTREE%'").Scan(&rtreeCount); err != nil {
		return fmt.Errorf("checking R*Tree availability: %w", err)
	}
	if rtreeCount == 0 {
		return fmt.Errorf("R*Tree not available in SQLite build")
	}

	return nil
}

func SQLiteConfigFromEnv() SQLiteConfig {
	dbPath := os.Getenv("SQLITE_DB_PATH")
	if dbPath == "" {
		dbPath = "data/routes.db"
	}
	// Default to read-only: the API server and most commands only read data.
	// The import command explicitly sets ReadOnly = false to opt in to writes.
	return SQLiteConfig{
		Path:     dbPath,
		ReadOnly: true,
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
