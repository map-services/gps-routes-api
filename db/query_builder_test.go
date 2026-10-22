package db

import (
	"fmt"
	"math"
	"testing"

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
			qb:   NewQueryBuilder(&SQLiteDialect{}, "SELECT * FROM routes", &request.SearchRequest{}),
			want: Result{
				sql:    "SELECT * FROM routes",
				params: []any{},
			},
		},
		{
			name: "single word query",
			qb:   NewQueryBuilder(&SQLiteDialect{}, "SELECT * FROM routes", &request.SearchRequest{Query: "test"}),
			want: Result{
				sql:    "SELECT * FROM routes WHERE routes_fts MATCH ?",
				params: []any{`"test"*`},
			},
		},
		{
			name: "multiple word query",
			qb:   NewQueryBuilder(&SQLiteDialect{}, "SELECT * FROM routes", &request.SearchRequest{Query: "hello world"}),
			want: Result{
				sql:    "SELECT * FROM routes WHERE routes_fts MATCH ?",
				params: []any{`"hello"* "world"*`},
			},
		},
		{
			name: "query with apostrophe",
			qb:   NewQueryBuilder(&SQLiteDialect{}, "SELECT * FROM routes", &request.SearchRequest{Query: "Randolph's"}),
			want: Result{
				sql:    "SELECT * FROM routes WHERE routes_fts MATCH ?",
				params: []any{`"Randolph's"*`},
			},
		},
		{
			name: "query with double quote",
			qb:   NewQueryBuilder(&SQLiteDialect{}, "SELECT * FROM routes", &request.SearchRequest{Query: `say "hello"`}),
			want: Result{
				sql:    "SELECT * FROM routes WHERE routes_fts MATCH ?",
				params: []any{`"say"* """hello"""*`},
			},
		},
		{
			name: "full text sort uses weighted bm25 (best-first)",
			qb: NewQueryBuilder(&SQLiteDialect{}, "SELECT * FROM routes", &request.SearchRequest{Query: "test"}).
				WithOrderBy((&SQLiteDialect{}).BuildFTSSort("routes_fts", "?") + " DESC"),
			want: Result{
				sql:    "SELECT * FROM routes WHERE routes_fts MATCH ? ORDER BY -bm25(routes_fts, 1.0, 0.4, 0.1) DESC",
				params: []any{`"test"*`},
			},
		},
		{
			name: "bounding box",
			qb:   NewQueryBuilder(&SQLiteDialect{}, "SELECT * FROM routes", &request.SearchRequest{BoundingBox: []float64{1.9, 2.8, 3.7, 4.6}}),
			// R*Tree bbox predicate: min_lat <= latMax AND max_lat >= latMin
			// AND min_lon <= lonMax AND max_lon >= lonMin.
			// boundingBox is [lngMin, latMin, lngMax, latMax] = {1.9, 2.8, 3.7, 4.6}.
			// Values are embedded directly (no '?' params) by BuildSTWithinQuery.
			want: Result{
				sql:    fmt.Sprintf("SELECT * FROM routes WHERE routes.rowid IN (SELECT id FROM routes_rtree WHERE min_lat <= %f AND max_lat >= %f AND min_lon <= %f AND max_lon >= %f)", 4.6, 2.8, 3.7, 1.9),
				params: []any{},
			},
		},
		{
			name: "nearest",
			qb: NewQueryBuilder(&SQLiteDialect{}, "SELECT * FROM routes", &request.SearchRequest{
				Nearby: &request.Nearby{
					Center: &common.GeoLoc{
						Latitude:  1.234,
						Longitude: 5.678,
					},
					DistanceKm: 20,
				},
			}),
			// BuildSTDWithinQuery embeds an R*Tree bbox (computed from centre +
			// distance) + haversine_distance() exact filter. All values are
			// embedded directly as SQL literals — no '?' params.
			// latDelta = dist/111320; lonDelta = dist/(111320*cos(radians(lat)))
			want: Result{
				sql: func() string {
					lat, lon, dist := 1.234, 5.678, float64(20000)
					latDelta := dist / 111320.0
					lonDelta := dist / (111320.0 * math.Cos(lat*math.Pi/180))
					return fmt.Sprintf("SELECT * FROM routes WHERE routes.rowid IN (SELECT id FROM routes_rtree WHERE min_lat <= %f AND max_lat >= %f AND min_lon <= %f AND max_lon >= %f) AND haversine_distance(%f, %f, longitude, latitude) <= %f",
						lat+latDelta, lat-latDelta, lon+lonDelta, lon-lonDelta, lon, lat, dist)
				}(),
				params: []any{},
			},
		},
		{
			name: "with where clause",
			qb:   NewQueryBuilder(&SQLiteDialect{}, "SELECT * FROM routes", &request.SearchRequest{}).WithWhereClause("name = 'test'"),
			want: Result{
				sql:    "SELECT * FROM routes WHERE name = 'test'",
				params: []any{},
			},
		},
		{
			name: "with multiple where clauses",
			qb:   NewQueryBuilder(&SQLiteDialect{}, "SELECT * FROM routes", &request.SearchRequest{}).WithWhereClause("name = 'test'").WithWhereClause("type = 'hike'"),
			want: Result{
				sql:    "SELECT * FROM routes WHERE name = 'test' AND type = 'hike'",
				params: []any{},
			},
		},
		{
			name: "with group by",
			qb:   NewQueryBuilder(&SQLiteDialect{}, "SELECT * FROM routes", &request.SearchRequest{}).WithGroupBy("name"),
			want: Result{
				sql:    "SELECT * FROM routes GROUP BY name",
				params: []any{},
			},
		},
		{
			name: "with order by",
			qb:   NewQueryBuilder(&SQLiteDialect{}, "SELECT * FROM routes", &request.SearchRequest{}).WithOrderBy("name DESC"),
			want: Result{
				sql:    "SELECT * FROM routes ORDER BY name DESC",
				params: []any{},
			},
		},
		{
			name: "with limit",
			qb:   NewQueryBuilder(&SQLiteDialect{}, "SELECT * FROM routes", &request.SearchRequest{}).WithLimit(10),
			want: Result{
				sql:    "SELECT * FROM routes LIMIT ?",
				params: []any{int32(10)},
			},
		},
		{
			name: "with no limit",
			qb:   NewQueryBuilder(&SQLiteDialect{}, "SELECT * FROM routes", &request.SearchRequest{}).WithLimit(-1),
			want: Result{
				sql:    "SELECT * FROM routes",
				params: []any{},
			},
		},
		{
			name: "with offset",
			qb:   NewQueryBuilder(&SQLiteDialect{}, "SELECT * FROM routes", &request.SearchRequest{}).WithOffset(20),
			want: Result{
				sql:    "SELECT * FROM routes OFFSET ?",
				params: []any{int32(20)},
			},
		},
		{
			name: "with truncated fields",
			qb:   NewQueryBuilder(&SQLiteDialect{}, "SELECT description FROM routes", &request.SearchRequest{}).WithTruncatedField("description", 100),
			want: Result{
				sql:    "SELECT SUBSTR(description, 1, 100) || CASE WHEN LENGTH(description) > 100 THEN '…' ELSE '' END AS description FROM routes",
				params: []any{},
			},
		},
		{
			name: "with all",
			qb: NewQueryBuilder(&SQLiteDialect{}, "SELECT * FROM routes", &request.SearchRequest{}).
				WithWhereClause("name = 'test'").
				WithGroupBy("name").
				WithOrderBy("name DESC").
				WithLimit(10).
				WithOffset(20),
			want: Result{
				sql:    "SELECT * FROM routes WHERE name = 'test' GROUP BY name ORDER BY name DESC LIMIT ? OFFSET ?",
				params: []any{int32(10), int32(20)},
			},
		},
		{
			// Regression: a facet predicate (json_each(?)) appears textually
			// BEFORE LIMIT/OFFSET in the SQL, so SQLite's positional '?'
			// binding requires params ordered [facetJSON, limit, offset]. The
			// previous implementation appended [limit, offset, facetJSON] (limit
			// appended in WithLimit before Build() injected the facet param),
			// which bound the LIMIT int to json_each -> 0 facet hits.
			name: "facet filter with pagination binds params in textual order",
			qb: NewQueryBuilder(&SQLiteDialect{}, "SELECT * FROM routes", &request.SearchRequest{
				Facets: map[string][]string{"district": {"London"}},
			}).WithLimit(24).WithOffset(0),
			want: Result{
				sql:    "SELECT * FROM routes WHERE district IN (SELECT value FROM json_each(?)) LIMIT ? OFFSET ?",
				params: []any{`["London"]`, int32(24), int32(0)},
			},
		},
		{
			name: "with array fields",
			qb: NewQueryBuilder(&SQLiteDialect{}, "SELECT * FROM routes", &request.SearchRequest{
				Facets: map[string][]string{
					"type": {"hike", "bike"},
				},
			}).WithArrayFields("type"),
			want: Result{
				sql:    "SELECT * FROM routes WHERE EXISTS (SELECT 1 FROM json_each(type) WHERE value IN (SELECT value FROM json_each(?)))",
				params: []any{`["hike","bike"]`},
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
