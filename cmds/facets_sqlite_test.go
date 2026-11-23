package cmds

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/map-services/gps-routes-api/models/common"
	"github.com/map-services/gps-routes-api/models/domain"
	"github.com/map-services/gps-routes-api/models/request"
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

	repo, cleanup := newSQLiteTestRepo(t)
	defer cleanup()

	// Title goes into the routes_fts index (via the AFTER INSERT trigger), so a
	// full-text query for "mountain" must match this route.
	assert.NoError(t, repo.Store(ctx, &domain.RouteMetadata{
		ObjectID:      "facet-r-1",
		Ref:           "REF-F1",
		Title:         "Mountain Breeze Trail",
		Description:   "a scenic mountain route",
		StartPosition: common.GeoLoc{Latitude: 48.85, Longitude: 2.35},
		Country:       strPtr("France"),
		DistanceKm:    12.3,
	}))

	criteria := &request.SearchRequest{Query: "mountain", Limit: 100}

	// FacetCounts — the query that previously errored with "no such column".
	facets, err := repo.FacetCounts(ctx, criteria, "country", 10, false)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), (*facets)["France"])

	// CountAll with the same full-text criteria — same latent JOIN gap.
	count, err := repo.CountAll(ctx, criteria)
	assert.NoError(t, err)
	assert.Equal(t, int64(1), count)

	// SearchHits must still match the single indexed route.
	hits, err := repo.SearchHits(ctx, criteria)
	assert.NoError(t, err)
	assert.Len(t, *hits, 1)
}

// TestSQLiteFacetExcludesEmptyValues reproduces the empty-string facet bucket
// the SQLite implementation produced: the JSON import path stores
// RouteMetadata scalar pointers (e.g. District) as "" instead of NULL, and the
// SQLite facet query's "IS NOT NULL" does not drop "". This asserts the SQLite
// facet WHERE filters the empty bucket out.
func TestSQLiteFacetExcludesEmptyValues(t *testing.T) {
	ctx := context.Background()

	repo, cleanup := newSQLiteTestRepo(t)
	defer cleanup()

	assert.NoError(t, repo.Store(ctx, &domain.RouteMetadata{
		ObjectID:      "ev-1",
		Ref:           "E1",
		Title:         "Halton Route",
		Description:   "a route",
		StartPosition: common.GeoLoc{Latitude: 50, Longitude: -1},
		District:      strPtr("Halton"),
		Country:       strPtr("France"),
	}))
	assert.NoError(t, repo.Store(ctx, &domain.RouteMetadata{
		ObjectID:      "ev-2",
		Ref:           "E2",
		Title:         "Blank District Route",
		Description:   "another route",
		StartPosition: common.GeoLoc{Latitude: 51, Longitude: -2},
		District:      strPtr(""), // empty string -> must be excluded from facets (== PG NULL)
		Country:       strPtr("France"),
	}))

	// No full-text query -> no routes_fts JOIN; the "" exclusion is the point.
	facets, err := repo.FacetCounts(ctx, &request.SearchRequest{}, "district", 10, false)
	assert.NoError(t, err)
	_, hasEmpty := (*facets)[""]
	assert.False(t, hasEmpty, "facet bucket \"\" should be excluded")
	assert.Equal(t, int64(1), (*facets)["Halton"])
}
