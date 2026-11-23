package cmds

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/map-services/gps-routes-api/db"
	"github.com/map-services/gps-routes-api/models/common"
	"github.com/map-services/gps-routes-api/models/domain"
	"github.com/map-services/gps-routes-api/models/request"
	"github.com/map-services/gps-routes-api/repositories"
)

// repoRoot walks up from the test CWD until it finds
// db/migrations/00002_sqlite_schema.sql (i.e. the repository root). ImportData
// -> setupSQLiteDB reads that schema via a relative path, so the test chdir's
// to the repo root. t.Chdir is scoped and restored by the testing framework.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err, "getwd")
	for {
		if _, err := os.Stat(filepath.Join(dir, "db", "migrations", "00002_sqlite_schema.sql")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			require.Fail(t, "could not locate repository root (db/migrations/00002_sqlite_schema.sql)")
		}
		dir = parent
	}
}

func writeRouteBackup(t *testing.T, path string, route *domain.RouteMetadata) {
	t.Helper()
	b, err := json.Marshal(route)
	require.NoError(t, err, "marshal route")
	require.NoError(t, os.WriteFile(path, b, 0o644), "write %s", path)
}

func strPtr(s string) *string { return &s }

// TestSQLiteImportData_RoundTrip verifies the SQLite data-import flow end-to-end:
// JSON backup files -> cmds.ImportData -> schema applied + rows stored -> readable
// back via SQLiteDbRepository.
func TestSQLiteImportData_RoundTrip(t *testing.T) {
	// setupSQLiteDB (inside ImportData) reads the schema by relative path.
	t.Chdir(repoRoot(t))

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "routes.db")
	// ImportData reads SQLITE_DB_PATH via db.SQLiteConfigFromEnv().
	t.Setenv("SQLITE_DB_PATH", dbPath)

	backupDir := filepath.Join(tmpDir, "backup")
	require.NoError(t, os.MkdirAll(backupDir, 0o755), "mkdir backup")

	writeRouteBackup(t, filepath.Join(backupDir, "route-1.json"), &domain.RouteMetadata{
		ObjectID:         "route-1",
		Ref:              "REF001",
		Title:            "Test Route 1",
		Description:      "A test route",
		StartPosition:    common.GeoLoc{Latitude: 51.5074, Longitude: -0.1278},
		DistanceKm:       10.5,
		CreatedAt:        time.Now(),
		HeadlineImageUrl: strPtr("http://example.com/img1.jpg"),
	})

	writeRouteBackup(t, filepath.Join(backupDir, "route-2.json"), &domain.RouteMetadata{
		ObjectID:      "route-2",
		Ref:           "REF002",
		Title:         "Test Route 2",
		Description:   "Another route",
		StartPosition: common.GeoLoc{Latitude: 52.0, Longitude: -1.0},
		DistanceKm:    5.0,
		CreatedAt:     time.Now(),
		Images:        []domain.Image{{Src: "http://example.com/img.jpg", Title: "Img", Caption: "Cap"}},
		Details:       []domain.Detail{{Subtitle: "S1", Content: "C1"}},
	})

	// ImportData creates the schema at dbPath and stores every backup file.
	ImportData(backupDir, 2, false)

	// Verify via an independent connection / repository.
	ctx := context.Background()
	sqlDB, err := db.NewSQLiteDB(ctx, db.SQLiteConfig{Path: dbPath})
	assert.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()

	repo := repositories.NewSQLiteRouteRepository(sqlDB)

	count, err := repo.CountAll(ctx, &request.SearchRequest{})
	assert.NoError(t, err)
	assert.Equal(t, int64(2), count)

	r1, err := repo.FindByObjectID(ctx, "route-1")
	assert.NoError(t, err)
	assert.NotNil(t, r1)
	assert.Equal(t, "Test Route 1", r1.Title)
	assert.Equal(t, "http://example.com/img1.jpg", *r1.HeadlineImageUrl)
	assert.Empty(t, r1.Images)
	assert.Empty(t, r1.Details)

	r2, err := repo.FindByObjectID(ctx, "route-2")
	assert.NoError(t, err)
	assert.NotNil(t, r2)
	assert.Equal(t, "Test Route 2", r2.Title)
	assert.Len(t, r2.Images, 1)
	assert.Len(t, r2.Details, 1)
}
