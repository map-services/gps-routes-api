package db

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

type Dialect interface {
	FormatParam(i int) string
	BuildArrayOverlapQuery(field string, placeholder string) string
	BuildAnyQuery(field string, placeholder string) string
	BuildFullTextQuery(field string, placeholder string) string
	// BuildSTWithinQuery generates an R*Tree bbox predicate. The four float64
	// args are corner coordinates (xMin=corners[0]=lngMin, yMin=latMin, xMax=lngMax, yMax=latMax)
	// and are embedded directly as SQL literals — no '?' params (consistent with BuildDistanceSort).
	BuildSTWithinQuery(field string, xMin, yMin, xMax, yMax float64) string
	// BuildSTDWithinQuery generates an R*Tree bbox prefilter + haversine_distance()
	// exact filter for radius queries. lon/lat/distance are the actual centre point
	// and radius in metres; the R*Tree bbox is computed internally from the
	// haversine radius. Values are embedded as SQL literals — no '?' params.
	BuildSTDWithinQuery(field string, lon, lat, distance float64) string
	BuildDistanceSort(field string, lon, lat float64) string
	BuildFTSSort(field string, placeholder string) string
	BuildFacetQuery(facetField string, unnest bool) string
	BuildPrefixQuery(query string) string
	BuildTruncateQuery(fieldName string, maxLength int) string
	PrepareParam(param any) any
}

// SQLiteDialect implements the Dialect interface for SQLite.
// It uses FTS5 for full-text search, the built-in R*Tree module for bbox
// filtering, and a Go-registered haversine_distance() SQL function for
// radius/distance queries — no Spatialite extension required.
// JSON1 is used for array operations.
type SQLiteDialect struct{}

func (d *SQLiteDialect) FormatParam(i int) string {
	return "?"
}

func (d *SQLiteDialect) BuildArrayOverlapQuery(field string, placeholder string) string {
	// For SQLite, arrays are stored as JSON. Check if any element in the JSON
	// array matches any element in the provided JSON array parameter.
	return fmt.Sprintf("EXISTS (SELECT 1 FROM json_each(%s) WHERE value IN (SELECT value FROM json_each(%s)))", field, placeholder)
}

func (d *SQLiteDialect) BuildAnyQuery(field string, placeholder string) string {
	return fmt.Sprintf("%s IN (SELECT value FROM json_each(%s))", field, placeholder)
}

func (d *SQLiteDialect) BuildFullTextQuery(field string, placeholder string) string {
	// FTS5 full-text search against the routes_fts virtual table. The field
	// parameter is ignored — FTS5 always matches against the whole rowid-indexed
	// table (joined to routes on rowid in the repository layer).
	return fmt.Sprintf("routes_fts MATCH %s", placeholder)
}

// BuildSTWithinQuery generates an R*Tree bbox predicate using SQLite's built-in
// R*Tree module (no extension needed). The field param is kept for interface
// compatibility but is unused; the routes_rtree virtual table replaces
// Spatialite's ST_Within(BuildMbr(...)).
//
// Parameter order matches the query-builder convention: xMin, yMin, xMax, yMax
// where x=longitude, y=latitude. boundingBox is [lngMin, latMin, lngMax, latMax].
// The R*Tree overlap test checks rectangle intersection — for a stored point
// (min==max) this is equivalent to latMin ≤ lat ≤ latMax AND lonMin ≤ lon ≤ lonMax.
func (d *SQLiteDialect) BuildSTWithinQuery(field string, xMin, yMin, xMax, yMax float64) string {
	_ = field // routes_rtree replaces the _geoloc geometry column
	return fmt.Sprintf(
		"routes.rowid IN (SELECT id FROM routes_rtree WHERE min_lat <= %f AND max_lat >= %f AND min_lon <= %f AND max_lon >= %f)",
		yMax, yMin, xMax, xMin,
	)
}

// metersPerDegreeLat is the ~metres-per-degree of latitude (111.32 km),
// used to convert a distance radius into a bounding box for the R*Tree prefilter.
const metersPerDegreeLat = 111320.0

func radians(d float64) float64 { return d * math.Pi / 180 }

// BuildSTDWithinQuery generates an R*Tree bbox prefilter combined with a
// haversine_distance() exact filter for radius (nearby) queries. The bbox is
// derived from the centre point and distance; the haversine function
// (registered via ConnectHook on the sqlite3_haversine driver) computes the
// great-circle distance in metres.
func (d *SQLiteDialect) BuildSTDWithinQuery(field string, lon, lat, distance float64) string {
	_ = field
	latDelta := distance / metersPerDegreeLat
	lonDelta := distance / (metersPerDegreeLat * math.Cos(radians(lat)))
	return fmt.Sprintf(
		"routes.rowid IN (SELECT id FROM routes_rtree WHERE min_lat <= %f AND max_lat >= %f AND min_lon <= %f AND max_lon >= %f) AND haversine_distance(%f, %f, longitude, latitude) <= %f",
		lat+latDelta, lat-latDelta, lon+lonDelta, lon-lonDelta, lon, lat, distance,
	)
}

// BuildDistanceSort generates the ORDER BY expression for distance sorting
// using the Go-registered haversine_distance() SQL function. ASC ordering
// places nearest routes first. Values are embedded directly as SQL literals.
func (d *SQLiteDialect) BuildDistanceSort(field string, lon, lat float64) string {
	_ = field
	return fmt.Sprintf("haversine_distance(%f, %f, longitude, latitude)", lon, lat)
}

func (d *SQLiteDialect) BuildFTSSort(field string, placeholder string) string {
	return "-bm25(routes_fts, 1.0, 0.4, 0.1)"
}

func (d *SQLiteDialect) BuildFacetQuery(facetField string, unnest bool) string {
	if unnest {
		return fmt.Sprintf("SELECT json_each.value AS key, COUNT(*) AS value FROM routes, json_each(routes.%s)", facetField)
	}
	return fmt.Sprintf("SELECT %s AS key, COUNT(*) AS value FROM routes", facetField)
}

func (d *SQLiteDialect) BuildPrefixQuery(query string) string {
	// FTS5 prefix search uses '*' at the end of words. Each word is wrapped in
	// double quotes so that special characters (e.g. apostrophes in "Randolph's")
	// are treated as literal text rather than FTS5 syntax. Any embedded double
	// quotes are escaped as "" per the FTS5 specification.
	words := strings.Split(query, " ")
	for i, word := range words {
		escaped := strings.ReplaceAll(word, `"`, `""`)
		words[i] = `"` + escaped + `"*`
	}
	return strings.Join(words, " ")
}

func (d *SQLiteDialect) BuildTruncateQuery(fieldName string, maxLength int) string {
	return fmt.Sprintf("SUBSTR(%s, 1, %d) || CASE WHEN LENGTH(%s) > %d THEN '…' ELSE '' END AS %s",
		fieldName, maxLength, fieldName, maxLength, asFieldAlias(fieldName))
}

func (d *SQLiteDialect) PrepareParam(param any) any {
	// For SQLite, arrays are passed as JSON strings
	if slice, ok := param.([]string); ok {
		b, _ := json.Marshal(slice)
		return string(b)
	}
	return param
}
