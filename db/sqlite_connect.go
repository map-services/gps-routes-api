package db

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"

	// The blank import registers the plain "sqlite3" driver; the named import
	// exposes sqlite3.SQLiteDriver for the Spatialite-enabled driver below.
	"github.com/mattn/go-sqlite3"
	_ "github.com/mattn/go-sqlite3"
)

// init registers a "sqlite3_spatialite" driver that enables SQLite's
// load_extension capability (go-sqlite3 only calls sqlite3_enable_load_extension
// when extensions are configured via the driver config) and auto-loads
// mod_spatialite. NewSQLiteDB opens through this driver and falls back to the
// plain "sqlite3" driver when the Spatialite library isn't installed/loadable,
// so the app still starts (FTS5-only; spatial predicates then error at runtime).
func init() {
	sql.Register("sqlite3_spatialite", &sqlite3.SQLiteDriver{
		Extensions: []string{spatialiteExtensionPath()},
	})
}

// spatialiteExtensionPath resolves the loadable mod_spatialite module so it can
// be loaded WITHOUT relying on the dynamic-loader search path. This matters on
// macOS arm64 (Homebrew lives in /opt/homebrew/lib, which is not a default loader
// path, and DYLD_LIBRARY_PATH is not reliably inherited by `go run`'s temp
// binary / the VS Code Go debugger / delve). Resolution order:
//  1. $SQLITE_SPATIALITE_PATH — explicit override (e.g. a custom build).
//  2. first existing well-known absolute path (macOS arm64 / Intel Homebrew,
//     Linux apt multilib / generic) — dlopen'd directly, no loader path needed.
//  3. bare "mod_spatialite" — relies on the loader search path (e.g. CI's apt
//     install of libsqlite3-mod-spatialite, or an explicit DYLD/LD override).
//
// sqlite3_load_extension accepts absolute paths, and each candidate is os.Stat'd
// before use, so this is non-speculative: a missing library simply falls through
// to the next option.
func spatialiteExtensionPath() string {
	if p := os.Getenv("SQLITE_SPATIALITE_PATH"); p != "" {
		return p
	}
	candidates := []string{
		"/opt/homebrew/lib/mod_spatialite.dylib",      // macOS arm64 Homebrew
		"/usr/local/lib/mod_spatialite.dylib",         // macOS Intel Homebrew
		"/usr/lib/x86_64-linux-gnu/mod_spatialite.so", // Debian/Ubuntu multilib
		"/usr/lib/sqlite3/mod_spatialite.so",          // Debian/Ubuntu (sqlite3-mod-spatialite)
		"/usr/lib/mod_spatialite.so",                  // generic Linux
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	return "mod_spatialite"
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
		_ = db.Close()
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
	dbPath := os.Getenv("SQLITE_DB_PATH")
	if dbPath == "" {
		dbPath = "data/routes.db"
	}
	return SQLiteConfig{
		Path: dbPath,
	}
}
