package cmds

import (
	"database/sql"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

// setupSQLiteDB opens a SQLite database at dbPath (or :memory: when dryRun is
// true), verifies FTS5 availability, then applies the schema from
// db/migrations/00002_sqlite_schema.sql. FTS5 is a hard requirement — the app's
// full-text search depends on it, so absence is a configuration error, not a
// graceful degradation. The schema file is a relative path — callers must run
// from the repository root.
func setupSQLiteDB(dbPath string, dryRun bool) error {
	if dryRun {
		dbPath = "file:memdb1?mode=memory&cache=shared"
	}

	db, err := sql.Open("sqlite", dbPath+"?_foreign_keys=1&_journal_mode=WAL")
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// Probe FTS5 by creating (then dropping) a throwaway virtual table. FTS5 is
	// compiled into the engine when building with the sqlite_fts5 build tag; if
	// it's missing at runtime the build/tag configuration is wrong and the app
	// should fail fast rather than silently running without full-text search.
	if _, err := db.Exec("CREATE VIRTUAL TABLE __fts5_check__ USING fts5(content)"); err != nil {
		return fmt.Errorf("FTS5 not available: %w — rebuild with -tags \"sqlite,sqlite_fts5\"", err)
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
