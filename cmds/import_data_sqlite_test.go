package cmds

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "db", "migrations", "00002_sqlite_schema.sql")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repository root (db/migrations/00002_sqlite_schema.sql)")
		}
		dir = parent
	}
}

func writeRouteBackup(t *testing.T, path string, route *domain.RouteMetadata) {
	t.Helper()
	b, err := json.Marshal(route)
	if err != nil {
		t.Fatalf("marshal route: %v", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
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
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("mkdir backup: %v", err)
	}

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
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	repo := repositories.NewSQLiteRouteRepository(sqlDB)

	count, err := repo.CountAll(ctx, &request.SearchRequest{})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 imported routes, got %d", count)
	}

	r1, err := repo.FindByObjectID(ctx, "route-1")
	if err != nil {
		t.Fatalf("lookup route-1: %v", err)
	}
	if r1 == nil || r1.Title != "Test Route 1" {
		t.Fatalf("route-1 = %+v", r1)
	}
	if r1.HeadlineImageUrl == nil || *r1.HeadlineImageUrl != "http://example.com/img1.jpg" {
		t.Errorf("route-1 headline mismatch: %+v", r1.HeadlineImageUrl)
	}
	if len(r1.Images) != 0 || len(r1.Details) != 0 {
		t.Errorf("route-1 expected 0 images/details, got %d/%d", len(r1.Images), len(r1.Details))
	}

	r2, err := repo.FindByObjectID(ctx, "route-2")
	if err != nil {
		t.Fatalf("lookup route-2: %v", err)
	}
	if r2 == nil || r2.Title != "Test Route 2" {
		t.Fatalf("route-2 = %+v", r2)
	}
	if len(r2.Images) != 1 || len(r2.Details) != 1 {
		t.Errorf("route-2 expected 1 image + 1 detail, got images=%d details=%d", len(r2.Images), len(r2.Details))
	}
}
