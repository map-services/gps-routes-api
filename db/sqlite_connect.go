//go:build sqlite

package db

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	// The blank import registers the plain "sqlite3" driver; the named import
	// exposes sqlite3.SQLiteDriver for the Spatialite-enabled driver below.
	_ "github.com/mattn/go-sqlite3"
	"github.com/mattn/go-sqlite3"
)

// init registers a "sqlite3_spatialite" driver that enables SQLite's
// load_extension capability (go-sqlite3 only calls sqlite3_enable_load_extension
// when extensions are configured via the driver config) and auto-loads
// mod_spatialite. NewSQLiteDB opens through this driver and falls back to the
// plain "sqlite3" driver when the Spatialite library isn't installed/loadable,
// so the app still starts (FTS5-only; spatial predicates then error at runtime).
func init() {
	sql.Register("sqlite3_spatialite", &sqlite3.SQLiteDriver{
		Extensions: []string{"mod_spatialite"},
	})
}

type SQLiteConfig struct {
	Path string
}

func NewSQLiteDB(ctx context.Context, config SQLiteConfig) (*sql.DB, error) {
	// Construct connection string for SQLite
	dsn := fmt.Sprintf("file:%s?cache=shared&mode=rwc&_journal_mode=WAL&_synchronous=NORMAL&_cache_size=10000&_temp_store=MEMORY", config.Path)

	log.Printf("Connecting to SQLite database at %s", config.Path)

	// Open through the Spatialite-enabled driver (see init()). go-sqlite3 loads
	// extensions lazily on first use, so Ping() triggers the Spatialite load.
	db, err := sql.Open("sqlite3_spatialite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening SQLite database: %w", err)
	}

	// If mod_spatialite isn't installed / isn't on the loader path, fall back to
	// the plain driver so the app still starts (Spatialite-dependent queries will
	// error at runtime, but FTS5 works).
	if err := db.Ping(); err != nil {
		log.Printf("Could not load Spatialite extension (%v); continuing without it", err)
		db.Close()
		if db, err = sql.Open("sqlite3", dsn); err != nil {
			return nil, fmt.Errorf("opening SQLite database: %w", err)
		}
	}

	// Configure connection pool
	db.SetMaxOpenConns(1)    // SQLite works best with single writer
	db.SetMaxIdleConns(1)    // Single connection for reads too
	db.SetConnMaxLifetime(0) // No limit for SQLite

	// Load extensions
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
	// Spatialite is loaded by the "sqlite3_spatialite" driver registered in
	// init() (with graceful fallback to plain "sqlite3" handled in NewSQLiteDB),
	// so it need not be loaded here. This also explains why a bare
	// `SELECT load_extension('mod_spatialite')` was previously rejected with
	// "not authorized" — go-sqlite3 only enables sqlite3_enable_load_extension
	// via the driver Extensions config above.

	// FTS5 is an SQLite module (not a scalar function), so probe it by creating
	// (then dropping) a throwaway virtual table. FTS5 is compiled into the
	// engine when building with the `sqlite_fts5` build tag.
	if _, err := db.Exec("DROP TABLE IF EXISTS _fts5_probe; CREATE VIRTUAL TABLE _fts5_probe USING fts5(content)"); err != nil {
		fmt.Printf("Warning: FTS5 not available, creating schema without full-text search: %v\n", err)
	} else {
		if _, err := db.Exec("DROP TABLE IF EXISTS _fts5_probe"); err != nil {
			fmt.Printf("Warning: could not drop FTS5 probe table: %v\n", err)
		}
	}

	return nil
}

func SQLiteConfigFromEnv() SQLiteConfig {
	return SQLiteConfig{
		Path: getEnvOrDefault("SQLITE_DB_PATH", "data/routes.db"),
	}
}
