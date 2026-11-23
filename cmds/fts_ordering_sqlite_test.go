package cmds

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/map-services/gps-routes-api/models/common"
	"github.com/map-services/gps-routes-api/models/domain"
	"github.com/map-services/gps-routes-api/models/request"
)

// TestSQLiteFTSOrdering guards relevancy ordering for full‑text search on
// SQLite. bm25() (SQLite's implicit `rank`) returns a NEGATIVE score where more
// negative = more relevant; BuildFTSSort previously returned "rank" and
// SearchHits appended " DESC", i.e. ORDER BY rank DESC => least‑relevant first.
// A route whose TITLE contains the query must outrank one where the query only
// appears in the (longer) description.
func TestSQLiteFTSOrdering(t *testing.T) {
	ctx := context.Background()

	repo, cleanup := newSQLiteTestRepo(t)
	defer cleanup()

	// A: "york" in TITLE + description -> most relevant
	assert.NoError(t, repo.Store(ctx, &domain.RouteMetadata{
		ObjectID:      "ord-a",
		Ref:           "A",
		Title:         "York Minster",
		Description:   "The York cathedral walk",
		StartPosition: common.GeoLoc{Latitude: 54, Longitude: -0.9},
	}))
	// B: "york" only in description -> less relevant
	assert.NoError(t, repo.Store(ctx, &domain.RouteMetadata{
		ObjectID:      "ord-b",
		Ref:           "B",
		Title:         "Coastal Path",
		Description:   "passing near york station",
		StartPosition: common.GeoLoc{Latitude: 53, Longitude: -1.0},
	}))

	hits, err := repo.SearchHits(ctx, &request.SearchRequest{Query: "york", Limit: 100})
	assert.NoError(t, err)
	assert.Len(t, *hits, 2)

	var order []string
	for _, h := range *hits {
		order = append(order, h.Ref)
	}
	// Most relevant (title match) must come FIRST.
	assert.Equal(t, "A", order[0], "expected title-match 'A' first (relevance inverted)")
}
