//go:build sqlite

package cmds

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/map-services/gps-routes-api/db"
	"github.com/map-services/gps-routes-api/models/common"
	"github.com/map-services/gps-routes-api/models/domain"
	"github.com/map-services/gps-routes-api/repositories"
)

// TestSQLiteGenerateSitemap verifies the sqlite-tagged GenerateSitemap produces a
// sitemap.xml (mirroring the Postgres implementation in sitemap.go). It also
// guards the single-file debug build of main.go: if cmds.GenerateSitemap is
// ever undefined under the sqlite tag again, this test's package fails to
// compile.
func TestSQLiteGenerateSitemap(t *testing.T) {
	ctx := context.Background()

	// GenerateSitemap -> setupSQLiteDB reads the schema by relative path, so run
	// from the repo root. t.Chdir is scoped/restored by the testing framework.
	t.Chdir(repoRoot(t))

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "routes.db")
	t.Setenv("SQLITE_DB_PATH", dbPath)

	if err := setupSQLiteDB(dbPath, false); err != nil {
		t.Fatalf("setup schema: %v", err)
	}

	sqlDB, err := db.NewSQLiteDB(ctx, db.SQLiteConfig{Path: dbPath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	repo := repositories.NewSQLiteRouteRepository(sqlDB)

	if err := repo.Store(ctx, &domain.RouteMetadata{
		ObjectID:         "r-1",
		Ref:              "REF-1",
		Title:            "Sitemap Route",
		StartPosition:    common.GeoLoc{Latitude: 1, Longitude: 2},
		HeadlineImageUrl: strPtr("https://example.com/img.jpg"),
	}); err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// GenerateSitemap writes "sitemap.xml" to the CWD — chdir into the temp dir
	// first so the file doesn't land in the repo root.
	t.Chdir(tmpDir)

	GenerateSitemap("https://example.com")

	xmlPath := filepath.Join(tmpDir, "sitemap.xml")
	b, err := os.ReadFile(xmlPath)
	if err != nil {
		t.Fatalf("read sitemap: %v", err)
	}
	if len(bytes.TrimSpace(b)) == 0 {
		t.Fatal("sitemap.xml is empty")
	}
	for _, want := range []string{"example.com", "REF-1", "https://example.com/img.jpg"} {
		if !bytes.Contains(b, []byte(want)) {
			t.Errorf("sitemap.xml missing %q; got:\n%s", want, b)
		}
	}
}
