package cmds

import (
	"context"
	"testing"

	"github.com/map-services/gps-routes-api/db"
	"github.com/map-services/gps-routes-api/models/common"
	"github.com/map-services/gps-routes-api/models/domain"
	"github.com/map-services/gps-routes-api/models/request"
	"github.com/map-services/gps-routes-api/repositories"
)

// TestSQLiteFTSOrdering guards relevancy ordering for full‑text search on
// SQLite. bm25() (SQLite's implicit `rank`) returns a NEGATIVE score where more
// negative = more relevant; BuildFTSSort previously returned "rank" and
// SearchHits appended " DESC", i.e. ORDER BY rank DESC => least‑relevant first.
// A route whose TITLE contains the query must outrank one where the query only
// appears in the (longer) description.
func TestSQLiteFTSOrdering(t *testing.T) {
	ctx := context.Background()

	t.Chdir(repoRoot(t))
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/routes.db"
	t.Setenv("SQLITE_DB_PATH", dbPath)

	if err := setupSQLiteDB(dbPath, false); err != nil {
		t.Fatalf("setup schema: %v", err)
	}
	sqlDB, err := db.NewSQLiteDB(ctx, db.SQLiteConfig{Path: dbPath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	repo := repositories.NewSQLiteRouteRepository(sqlDB)

	// A: "york" in TITLE + description -> most relevant
	if err := repo.Store(ctx, &domain.RouteMetadata{
		ObjectID:      "ord-a",
		Ref:           "A",
		Title:         "York Minster",
		Description:   "The York cathedral walk",
		StartPosition: common.GeoLoc{Latitude: 54, Longitude: -0.9},
	}); err != nil {
		t.Fatalf("store A: %v", err)
	}
	// B: "york" only in description -> less relevant
	if err := repo.Store(ctx, &domain.RouteMetadata{
		ObjectID:      "ord-b",
		Ref:           "B",
		Title:         "Coastal Path",
		Description:   "passing near york station",
		StartPosition: common.GeoLoc{Latitude: 53, Longitude: -1.0},
	}); err != nil {
		t.Fatalf("store B: %v", err)
	}

	hits, err := repo.SearchHits(ctx, &request.SearchRequest{Query: "york", Limit: 100})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(*hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(*hits))
	}

	var order []string
	for _, h := range *hits {
		order = append(order, h.Ref)
	}
	// Most relevant (title match) must come FIRST.
	if order[0] != "A" {
		t.Errorf("expected title-match 'A' first, got order=%v (relevance inverted)", order)
	}
}
