package db

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Dialect interface {
	FormatParam(i int) string
	BuildArrayOverlapQuery(field string, placeholder string) string
	BuildAnyQuery(field string, placeholder string) string
	BuildFullTextQuery(field string, placeholder string) string
	BuildSTWithinQuery(field string, xMin, yMin, xMax, yMax string) string
	BuildSTDWithinQuery(field string, lon, lat, distance string) string
	BuildDistanceSort(field string, lon, lat float64) string
	BuildFTSSort(field string, placeholder string) string
	BuildFacetQuery(facetField string, unnest bool) string
	BuildPrefixQuery(query string) string
	BuildTruncateQuery(fieldName string, maxLength int) string
	PrepareParam(param any) any
}

// SQLiteDialect implements the Dialect interface for SQLite.
// It uses FTS5 for full-text search, Spatialite for geometry, and JSON1 for
// array operations.
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

func (d *SQLiteDialect) BuildSTWithinQuery(field string, xMin, yMin, xMax, yMax string) string {
	// With the mod_spatialite extension loaded, SQLite provides ST_Within and
	// BuildMbr(xmin, ymin, xmax, ymax, srid) for constructing axis-aligned
	// bounding boxes. Without Spatialite, spatial queries are handled at the
	// repository layer; see db/sqlite_connect.go.
	return fmt.Sprintf("ST_Within(%s, BuildMbr(%s, %s, %s, %s, 4326))", field, xMin, yMin, xMax, yMax)
}

func (d *SQLiteDialect) BuildSTDWithinQuery(field string, lon, lat, distance string) string {
	// Simplified distance calculation without spatial functions.
	return fmt.Sprintf("(latitude - %s) * (latitude - %s) + (longitude - %s) * (longitude - %s) < (%s/111.0) * (%s/111.0)", lat, lat, lon, lon, distance, distance)
}

func (d *SQLiteDialect) BuildDistanceSort(field string, lon, lat float64) string {
	// Simplified distance sort without spatial functions
	return fmt.Sprintf("(latitude - %f) * (latitude - %f) + (longitude - %f) * (longitude - %f)", lat, lat, lon, lon)
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
	// FTS5 prefix search uses '*' at the end of words.
	words := strings.Split(query, " ")
	for i, word := range words {
		words[i] = word + "*"
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
