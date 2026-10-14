//go:build sqlite

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

// TestSQLiteFacetCountsWithQuery is a regression test for the runtime error:
//
//	Error: executing facet query: no such column: routes_fts
//
// Reproduction: a request that includes BOTH a full-text `query=` and `facets=`
// makes SQLiteDbRepository.FacetCounts (and CountAll) build a
//
//	SELECT … FROM routes [WHERE <bbox> AND routes_fts MATCH ?]
//
// query. routes_fts is a *separate* FTS5 virtual table, so without a matching
// "JOIN routes_fts ON routes.rowid = routes_fts.rowid" it is out of scope and
// SQLite reports "no such column: routes_fts" (treating the identifier as a
// column). SearchHits already joined routes_fts; the count/facet paths did not.
//
// This test exercises the previously-broken path end-to-end and asserts the
// count is correct (not merely that it doesn't error).
func TestSQLiteFacetCountsWithQuery(t *testing.T) {
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

	// Title goes into the routes_fts index (via the AFTER INSERT trigger), so a
	// full-text query for "mountain" must match this route.
	if err := repo.Store(ctx, &domain.RouteMetadata{
		ObjectID:      "facet-r-1",
		Ref:           "REF-F1",
		Title:         "Mountain Breeze Trail",
		Description:   "a scenic mountain route",
		StartPosition: common.GeoLoc{Latitude: 48.85, Longitude: 2.35},
		Country:       strPtr("France"),
		DistanceKm:    12.3,
	}); err != nil {
		t.Fatalf("store: %v", err)
	}

	criteria := &request.SearchRequest{Query: "mountain", Limit: 100}

	// FacetCounts — the query that previously errored with "no such column".
	facets, err := repo.FacetCounts(ctx, criteria, "country", 10, false)
	if err != nil {
		t.Fatalf("facet counts: %v", err)
	}
	if got := (*facets)["France"]; got != 1 {
		t.Errorf(`facet "France" = %d, want 1 (got %#v)`, got, *facets)
	}

	// CountAll with the same full-text criteria — same latent JOIN gap.
	count, err := repo.CountAll(ctx, criteria)
	if err != nil {
		t.Fatalf("count with query: %v", err)
	}
	if count != 1 {
		t.Errorf("CountAll(query=mountain) = %d, want 1", count)
	}

	// SearchHits must still match the single indexed route.
	hits, err := repo.SearchHits(ctx, criteria)
	if err != nil {
		t.Fatalf("search hits: %v", err)
	}
	if len(*hits) != 1 {
		t.Errorf("SearchHits(query=mountain) returned %d hits, want 1", len(*hits))
	}
}
