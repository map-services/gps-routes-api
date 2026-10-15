package cmds

import (
	"context"
	"database/sql"
	"testing"

	"github.com/map-services/gps-routes-api/db"
	"github.com/map-services/gps-routes-api/repositories"
)

// sqliteTestSetup holds the results of newSQLiteTestRepoFull so that callers
// can access the repo, the DB handle, the DB path, and the cleanup function.
type sqliteTestSetup struct {
	repo   *repositories.SQLiteDbRepository
	db     *sql.DB
	dbPath string
}

// newSQLiteTestRepo sets up a temporary SQLite database with the full schema
// (including FTS5, R*Tree, and sync triggers) and returns a ready-to-use
// repository along with a cleanup function. It handles chdir to the repo root
// (required by setupSQLiteDB for reading migration files by relative path),
// creates a temp database file, applies the schema, and opens a connection.
//
// Usage:
//
//	repo, cleanup := newSQLiteTestRepo(t)
//	defer cleanup()
func newSQLiteTestRepo(t *testing.T) (*repositories.SQLiteDbRepository, func()) {
	t.Helper()
	setup := newSQLiteTestRepoFull(t)
	return setup.repo, func() { _ = setup.db.Close() }
}

// newSQLiteTestRepoFull is like newSQLiteTestRepo but returns all components
// (repo, DB handle, DB path) for tests that need direct access.
func newSQLiteTestRepoFull(t *testing.T) *sqliteTestSetup {
	t.Helper()

	// setupSQLiteDB reads the schema by relative path, so chdir to repo root.
	t.Chdir(repoRoot(t))

	tmpDir := t.TempDir()
	dbPath := tmpDir + "/routes.db"

	if err := setupSQLiteDB(dbPath, false); err != nil {
		t.Fatalf("setup SQLite schema: %v", err)
	}

	sqlDB, err := db.NewSQLiteDB(context.Background(), db.SQLiteConfig{Path: dbPath})
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}

	repo := repositories.NewSQLiteRouteRepository(sqlDB)

	return &sqliteTestSetup{repo: repo, db: sqlDB, dbPath: dbPath}
}
