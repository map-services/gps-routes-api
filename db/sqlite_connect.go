package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
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
	Path     string
	ReadOnly bool
}

func NewSQLiteDB(ctx context.Context, config SQLiteConfig) (*sql.DB, error) {
	// Construct connection string for SQLite
	var dsn string
	if config.ReadOnly {
		// Read-only mode: open the database without write access. Multiple
		// concurrent readers are allowed, so the pool is larger (see below).
		dsn = fmt.Sprintf("file:%s?mode=ro&cache=shared&_busy_timeout=5000", config.Path)
		slog.Info("Connecting to SQLite database", "path", config.Path, "mode", "read-only")
	} else {
		// Read-write mode: used by the import command and tests.
		dsn = fmt.Sprintf("file:%s?cache=shared&mode=rwc&_journal_mode=WAL&_synchronous=NORMAL&_cache_size=10000&_temp_store=MEMORY", config.Path)
		slog.Info("Connecting to SQLite database", "path", config.Path, "mode", "read-write")
	}

	// Open through the haversine-enabled driver. ConnectHook fires on the first
	// connection (and every new one) to register haversine_distance().
	db, err := sql.Open("sqlite3_haversine", dsn)
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

// loadSQLiteExtensions checks that FTS5 and R*Tree are compiled into the
// SQLite library. It uses pragma_compile_options (a read-only query) so it
// works in both read-only and read-write modes without creating temporary
// tables.
func loadSQLiteExtensions(db *sql.DB) error {
	var fts5Count, rtreeCount int
	if err := db.QueryRow("SELECT count(*) FROM pragma_compile_options WHERE compile_options LIKE '%FTS5%'").Scan(&fts5Count); err != nil {
		return fmt.Errorf("checking FTS5 availability: %w", err)
	}
	if fts5Count == 0 {
		return fmt.Errorf("FTS5 not available — rebuild with -tags \"sqlite,sqlite_fts5\"")
	}

	if err := db.QueryRow("SELECT count(*) FROM pragma_compile_options WHERE compile_options LIKE '%RTREE%'").Scan(&rtreeCount); err != nil {
		return fmt.Errorf("checking R*Tree availability: %w", err)
	}
	if rtreeCount == 0 {
		return fmt.Errorf("R*Tree not available — rebuild with -tags \"sqlite,sqlite_fts5\"")
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
	slog.Info("Running ANALYZE to update query planner statistics")
	if _, err := db.Exec("ANALYZE"); err != nil {
		return fmt.Errorf("failed to run ANALYZE: %w", err)
	}

	// VACUUM must run outside of a transaction and reclaims space from deleted
	// rows while compacting the database file.
	slog.Info("Running VACUUM to compact database file")
	if _, err := db.Exec("VACUUM"); err != nil {
		return fmt.Errorf("failed to run VACUUM: %w", err)
	}

	// PRAGMA optimize runs automatic optimizations, such as index statistics
	// updates. Log but don't fail on error — it can fail in edge cases.
	slog.Info("Running PRAGMA optimize")
	if _, err := db.Exec("PRAGMA optimize"); err != nil {
		slog.Warn("PRAGMA optimize failed", "error", err)
	}

	slog.Info("Database optimization completed successfully")
	return nil
}
