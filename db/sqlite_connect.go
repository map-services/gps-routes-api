package db

import (
	"context"
	"database/sql"
	"fmt"
	"log"
)

type SQLiteConfig struct {
	Path string
}

func NewSQLiteDB(ctx context.Context, config SQLiteConfig) (*sql.DB, error) {
	// Construct connection string for SQLite
	dsn := fmt.Sprintf("file:%s?cache=shared&mode=rwc&_journal_mode=WAL&_synchronous=NORMAL&_cache_size=10000&_temp_store=MEMORY", config.Path)

	log.Printf("Connecting to SQLite database at %s", config.Path)

	// sql.Register("sqlite3_spatialite", &sqlite3.SQLiteDriver{
	// 	Extensions: []string{"mod_spatialite"},
	// })

	// Open the database
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening SQLite database: %w", err)
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
	// Load Spatialite extension
	if _, err := db.Exec("SELECT load_extension('mod_spatialite')"); err != nil {
		// Log warning but don't fail - Spatialite might not be available
		fmt.Printf("Warning: Could not load Spatialite extension: %v\n", err)
	}

	// FTS5 is usually built-in with go-sqlite3, but try to load if needed
	if _, err := db.Exec("SELECT fts5(?)", "test"); err != nil {
		fmt.Printf("Warning: FTS5 not available, creating schema without full-text search: %v\n", err)
	}

	return nil
}

func SQLiteConfigFromEnv() SQLiteConfig {
	return SQLiteConfig{
		Path: getEnvOrDefault("SQLITE_DB_PATH", "data/routes.db"),
	}
}
