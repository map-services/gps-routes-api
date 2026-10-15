package db

import (
	"testing"

	"github.com/lib/pq"
	"github.com/map-services/gps-routes-api/models/common"
	"github.com/map-services/gps-routes-api/models/request"
	"github.com/stretchr/testify/assert"
)

type Result struct {
	sql    string
	params []any
}

func TestQueryBuilder_Build(t *testing.T) {

	tests := []struct {
		name string
		qb   *QueryBuilder
		want Result
	}{
		{
			name: "empty query",
			qb:   NewQueryBuilder(&PostgreSQLDialect{}, "SELECT * FROM routes", &request.SearchRequest{}),
			want: Result{
				sql:    "SELECT * FROM routes",
				params: []any{},
			},
		},
		{
			name: "single word query",
			qb:   NewQueryBuilder(&PostgreSQLDialect{}, "SELECT * FROM routes", &request.SearchRequest{Query: "test"}),
			want: Result{
				sql:    "SELECT * FROM routes WHERE search_vector @@ to_tsquery($1)",
				params: []any{"test:*"},
			},
		},
		{
			name: "multiple word query",
			qb:   NewQueryBuilder(&PostgreSQLDialect{}, "SELECT * FROM routes", &request.SearchRequest{Query: "hello world"}),
			want: Result{
				sql:    "SELECT * FROM routes WHERE search_vector @@ to_tsquery($1)",
				params: []any{"hello:* & world:*"},
			},
		},
		{
			name: "bounding box",
			qb:   NewQueryBuilder(&PostgreSQLDialect{}, "SELECT * FROM routes", &request.SearchRequest{BoundingBox: []float64{1.9, 2.8, 3.7, 4.6}}),
			want: Result{
				sql:    "SELECT * FROM routes WHERE ST_Within(_geoloc, ST_MakeEnvelope($1, $2, $3, $4, 4326))",
				params: []any{1.9, 2.8, 3.7, 4.6},
			},
		},
		{
			name: "nearest",
			qb: NewQueryBuilder(&PostgreSQLDialect{}, "SELECT * FROM routes", &request.SearchRequest{
				Nearby: &request.Nearby{
					Center: &common.GeoLoc{
						Latitude:  1.234,
						Longitude: 5.678,
					},
					DistanceKm: 20,
				},
			}),
			want: Result{
				sql:    "SELECT * FROM routes WHERE ST_DWithin(ST_Transform(_geoloc, 3857), ST_Transform(ST_SetSRID(ST_Point($1, $2), 4326), 3857), $3)",
				params: []any{5.678, 1.234, int32(20000)},
			},
		},
		{
			name: "with where clause",
			qb:   NewQueryBuilder(&PostgreSQLDialect{}, "SELECT * FROM routes", &request.SearchRequest{}).WithWhereClause("name = 'test'"),
			want: Result{
				sql:    "SELECT * FROM routes WHERE name = 'test'",
				params: []any{},
			},
		},
		{
			name: "with multiple where clauses",
			qb:   NewQueryBuilder(&PostgreSQLDialect{}, "SELECT * FROM routes", &request.SearchRequest{}).WithWhereClause("name = 'test'").WithWhereClause("type = 'hike'"),
			want: Result{
				sql:    "SELECT * FROM routes WHERE name = 'test' AND type = 'hike'",
				params: []any{},
			},
		},
		{
			name: "with group by",
			qb:   NewQueryBuilder(&PostgreSQLDialect{}, "SELECT * FROM routes", &request.SearchRequest{}).WithGroupBy("name"),
			want: Result{
				sql:    "SELECT * FROM routes GROUP BY name",
				params: []any{},
			},
		},
		{
			name: "with order by",
			qb:   NewQueryBuilder(&PostgreSQLDialect{}, "SELECT * FROM routes", &request.SearchRequest{}).WithOrderBy("name DESC"),
			want: Result{
				sql:    "SELECT * FROM routes ORDER BY name DESC",
				params: []any{},
			},
		},
		{
			name: "with limit",
			qb:   NewQueryBuilder(&PostgreSQLDialect{}, "SELECT * FROM routes", &request.SearchRequest{}).WithLimit(10),
			want: Result{
				sql:    "SELECT * FROM routes LIMIT $1",
				params: []any{int32(10)},
			},
		},
		{
			name: "with no limit",
			qb:   NewQueryBuilder(&PostgreSQLDialect{}, "SELECT * FROM routes", &request.SearchRequest{}).WithLimit(-1),
			want: Result{
				sql:    "SELECT * FROM routes",
				params: []any{},
			},
		},
		{
			name: "with offset",
			qb:   NewQueryBuilder(&PostgreSQLDialect{}, "SELECT * FROM routes", &request.SearchRequest{}).WithOffset(20),
			want: Result{
				sql:    "SELECT * FROM routes OFFSET $1",
				params: []any{int32(20)},
			},
		},
		{
			name: "with truncated fields",
			qb:   NewQueryBuilder(&PostgreSQLDialect{}, "SELECT description FROM routes", &request.SearchRequest{}).WithTruncatedField("description", 100),
			want: Result{
				sql:    "SELECT LEFT(description, 100) || CASE WHEN LENGTH(description) > 100 THEN '…' ELSE '' END AS description FROM routes",
				params: []any{},
			},
		},
		{
			name: "with all",
			qb: NewQueryBuilder(&PostgreSQLDialect{}, "SELECT * FROM routes", &request.SearchRequest{}).
				WithWhereClause("name = 'test'").
				WithGroupBy("name").
				WithOrderBy("name DESC").
				WithLimit(10).
				WithOffset(20),
			want: Result{
				sql:    "SELECT * FROM routes WHERE name = 'test' GROUP BY name ORDER BY name DESC LIMIT $1 OFFSET $2",
				params: []any{int32(10), int32(20)},
			},
		},
		{
			name: "with facets and pagination",
			qb: NewQueryBuilder(&PostgreSQLDialect{}, "SELECT * FROM routes", &request.SearchRequest{
				Facets: map[string][]string{
					"type": {"hike"},
				},
			}).WithLimit(10).WithOffset(5),
			want: Result{
				// Facet '?' ($1) appears textually before LIMIT ($2)/OFFSET ($3);
				// params order MUST match textual order for SQLite, and $N indices
				// are recomputed to match, so both dialects bind correctly.
				sql:    "SELECT * FROM routes WHERE type = ANY($1) LIMIT $2 OFFSET $3",
				params: []any{pq.Array([]string{"hike"}), int32(10), int32(5)},
			},
		},
		{
			name: "with facets",
			qb: NewQueryBuilder(&PostgreSQLDialect{}, "SELECT * FROM routes", &request.SearchRequest{
				Facets: map[string][]string{
					"type": {"hike", "bike"},
				},
			}),
			want: Result{
				sql:    "SELECT * FROM routes WHERE type = ANY($1)",
				params: []any{pq.Array([]string{"hike", "bike"})},
			},
		},
		{
			name: "with empty facets",
			qb: NewQueryBuilder(&PostgreSQLDialect{}, "SELECT * FROM routes", &request.SearchRequest{
				Facets: map[string][]string{
					"type": {},
				},
			}),
			want: Result{
				sql:    "SELECT * FROM routes",
				params: []any{},
			},
		},
		{
			name: "with excluded facets",
			qb: NewQueryBuilder(&PostgreSQLDialect{}, "SELECT * FROM routes", &request.SearchRequest{
				Facets: map[string][]string{
					"type": {"hike", "bike"},
					"name": {"test"},
				},
			}).WithExcludeFacets("type"),
			want: Result{
				sql:    "SELECT * FROM routes WHERE name = ANY($1)",
				params: []any{pq.Array([]string{"test"})},
			},
		},
		{
			name: "with array fields",
			qb: NewQueryBuilder(&PostgreSQLDialect{}, "SELECT * FROM routes", &request.SearchRequest{
				Facets: map[string][]string{
					"type": {"hike", "bike"},
				},
			}).WithArrayFields("type"),
			want: Result{
				sql:    "SELECT * FROM routes WHERE type && $1::TEXT[]",
				params: []any{pq.Array([]string{"hike", "bike"})},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, params := tt.qb.Build()

			assert.Equal(t, tt.want.sql, sql)
			assert.Equal(t, tt.want.params, params)
		})
	}
}

func TestQueryBuilder_Build_SQLite(t *testing.T) {
	tests := []struct {
		name string
		qb   *QueryBuilder
		want Result
	}{
		{
			name: "empty query",
			qb:   NewQueryBuilder(&SQLiteDialect{}, "SELECT * FROM routes", &request.SearchRequest{}),
			want: Result{
				sql:    "SELECT * FROM routes",
				params: []any{},
			},
		},
		{
			name: "full text search",
			qb:   NewQueryBuilder(&SQLiteDialect{}, "SELECT * FROM routes", &request.SearchRequest{Query: "test"}),
			want: Result{
				sql:    "SELECT * FROM routes WHERE routes_fts MATCH ?",
				params: []any{"test*"},
			},
		},
		{
			// FTS5 bm25() is negative (more negative = more relevant); the repo
			// appends " DESC", so BuildFTSSort negates to keep DESC == best-first.
			// Column weights (title 1.0 / address 0.4 / description 0.1) mirror
			// PostgreSQL's weighted search_vector so title matches dominate.
			name: "full text sort uses weighted bm25 (best-first)",
			qb: NewQueryBuilder(&SQLiteDialect{}, "SELECT * FROM routes", &request.SearchRequest{Query: "test"}).
				WithOrderBy((&SQLiteDialect{}).BuildFTSSort("routes_fts", "?") + " DESC"),
			want: Result{
				sql:    "SELECT * FROM routes WHERE routes_fts MATCH ? ORDER BY -bm25(routes_fts, 1.0, 0.4, 0.1) DESC",
				params: []any{"test*"},
			},
		},
		{
			name: "bounding box",
			qb:   NewQueryBuilder(&SQLiteDialect{}, "SELECT * FROM routes", &request.SearchRequest{BoundingBox: []float64{1.9, 2.8, 3.7, 4.6}}),
			// SQLite emits a lat/lng range pre-filter (seekable via the
			// idx_routes_latitude / idx_routes_longitude B-tree indexes) ahead of
			// the precise ST_Within(BuildMbr(...)) spatial test. boundingBox is
			// [lngMin, latMin, lngMax, latMax] = {1.9, 2.8, 3.7, 4.6}, so the
			// positional '?' values follow textual order:
			//   latitude BETWEEN ?(2.8) AND ?(4.6)
			//   AND longitude BETWEEN ?(1.9) AND ?(3.7)
			//   AND ST_Within(_geoloc, BuildMbr(?(1.9),?(2.8),?(3.7),?(4.6), 4326))
			want: Result{
				sql:    "SELECT * FROM routes WHERE latitude BETWEEN ? AND ? AND longitude BETWEEN ? AND ? AND ST_Within(_geoloc, BuildMbr(?, ?, ?, ?, 4326))",
				params: []any{2.8, 4.6, 1.9, 3.7, 1.9, 2.8, 3.7, 4.6},
			},
		},
		{
			// Regression: a facet predicate (json_each(?)) appears textually
			// BEFORE LIMIT/OFFSET in the SQL, so for SQLite's positional '?'
			// binding the params MUST be [facetJSON, limit, offset]. The previous
			// implementation appended [limit, offset, facetJSON] (limit appended in
			// WithLimit before Build() injected the facet param), which bound the
			// LIMIT int to json_each -> 0 facet hits.
			name: "facet filter with pagination binds params in textual order",
			qb: NewQueryBuilder(&SQLiteDialect{}, "SELECT * FROM routes", &request.SearchRequest{
				Facets: map[string][]string{"district": {"London"}},
			}).WithLimit(24).WithOffset(0),
			want: Result{
				sql:    "SELECT * FROM routes WHERE district IN (SELECT value FROM json_each(?)) LIMIT ? OFFSET ?",
				params: []any{`["London"]`, int32(24), int32(0)},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, params := tt.qb.Build()

			assert.Equal(t, tt.want.sql, sql)
			assert.Equal(t, tt.want.params, params)
		})
	}
}
