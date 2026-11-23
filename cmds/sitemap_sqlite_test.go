package cmds

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/map-services/gps-routes-api/models/common"
	"github.com/map-services/gps-routes-api/models/domain"
)

// TestSQLiteGenerateSitemap verifies the GenerateSitemap function produces a
// sitemap.xml. It also guards compilation: if cmds.GenerateSitemap is ever
// undefined, this test's package fails to compile.
func TestSQLiteGenerateSitemap(t *testing.T) {
	ctx := context.Background()

	setup := newSQLiteTestRepoFull(t)
	tmpDir := filepath.Dir(setup.dbPath)
	// GenerateSitemap reads SQLITE_DB_PATH from the environment.
	t.Setenv("SQLITE_DB_PATH", setup.dbPath)

	assert.NoError(t, setup.repo.Store(ctx, &domain.RouteMetadata{
		ObjectID:         "r-1",
		Ref:              "REF-1",
		Title:            "Sitemap Route",
		StartPosition:    common.GeoLoc{Latitude: 1, Longitude: 2},
		HeadlineImageUrl: strPtr("https://example.com/img.jpg"),
	}))
	assert.NoError(t, setup.db.Close())

	// GenerateSitemap writes "sitemap.xml" to the CWD — chdir into the temp dir
	// first so the file doesn't land in the repo root.
	t.Chdir(tmpDir)

	GenerateSitemap("https://example.com")

	xmlPath := filepath.Join(tmpDir, "sitemap.xml")
	b, err := os.ReadFile(xmlPath)
	assert.NoError(t, err)
	assert.NotEmpty(t, bytes.TrimSpace(b), "sitemap.xml is empty")
	for _, want := range []string{"example.com", "REF-1", "https://example.com/img.jpg"} {
		assert.Contains(t, string(b), want, "sitemap.xml should contain %s", want)
	}
}
