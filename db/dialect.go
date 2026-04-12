package db

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lib/pq"
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

type PostgreSQLDialect struct{}

func (d *PostgreSQLDialect) FormatParam(i int) string {
	return fmt.Sprintf("$%d", i)
}

func (d *PostgreSQLDialect) BuildArrayOverlapQuery(field string, placeholder string) string {
	return fmt.Sprintf("%s && %s::TEXT[]", field, placeholder)
}

func (d *PostgreSQLDialect) BuildAnyQuery(field string, placeholder string) string {
	return fmt.Sprintf("%s = ANY(%s)", field, placeholder)
}

func (d *PostgreSQLDialect) BuildFullTextQuery(field string, placeholder string) string {
	return fmt.Sprintf("%s @@ to_tsquery(%s)", field, placeholder)
}

func (d *PostgreSQLDialect) BuildSTWithinQuery(field string, xMin, yMin, xMax, yMax string) string {
	return fmt.Sprintf("ST_Within(%s, ST_MakeEnvelope(%s, %s, %s, %s, 4326))", field, xMin, yMin, xMax, yMax)
}

func (d *PostgreSQLDialect) BuildSTDWithinQuery(field string, lon, lat, distance string) string {
	return fmt.Sprintf("ST_DWithin(ST_Transform(%s, 3857), ST_Transform(ST_SetSRID(ST_Point(%s, %s), 4326), 3857), %s)", field, lon, lat, distance)
}

func (d *PostgreSQLDialect) BuildDistanceSort(field string, lon, lat float64) string {
	return fmt.Sprintf("%s <-> ST_SetSRID(ST_Point(%f, %f), 4326)", field, lon, lat)
}

func (d *PostgreSQLDialect) BuildFTSSort(field string, placeholder string) string {
	return fmt.Sprintf("ts_rank_cd(%s, to_tsquery(%s), 32)", field, placeholder)
}

func (d *PostgreSQLDialect) BuildFacetQuery(facetField string, unnest bool) string {
	if unnest {
		return fmt.Sprintf("SELECT UNNEST(%s) AS key, COUNT(*) AS value FROM routes", facetField)
	}
	return fmt.Sprintf("SELECT %s AS key, COUNT(*) AS value FROM routes", facetField)
}

func (d *PostgreSQLDialect) BuildPrefixQuery(query string) string {
	words := strings.Split(query, " ")
	for i, word := range words {
		words[i] = word + ":*"
	}
	return strings.Join(words, " & ")
}

func (d *PostgreSQLDialect) BuildTruncateQuery(fieldName string, maxLength int) string {
	return fmt.Sprintf("LEFT(%s, %d) || CASE WHEN LENGTH(%s) > %d THEN '…' ELSE '' END AS %s",
		fieldName, maxLength, fieldName, maxLength, asFieldAlias(fieldName))
}

func (d *PostgreSQLDialect) PrepareParam(param any) any {
	// For Postgres, arrays must be wrapped with pq.Array
	if slice, ok := param.([]string); ok {
		return pq.Array(slice)
	}
	return param
}

type SQLiteDialect struct{}

func (d *SQLiteDialect) FormatParam(i int) string {
	return "?"
}

func (d *SQLiteDialect) BuildArrayOverlapQuery(field string, placeholder string) string {
	// For SQLite, we assume arrays are stored as JSON.
	// We use a subquery to check if any element in the JSON array matches any element in the provided array.
	// Since we can't easily pass a JSON array as a parameter and use it in IN clause without json_each,
	// we assume the placeholder will contain a JSON array string.
	return fmt.Sprintf("EXISTS (SELECT 1 FROM json_each(%s) WHERE value IN (SELECT value FROM json_each(%s)))", field, placeholder)
}

func (d *SQLiteDialect) BuildAnyQuery(field string, placeholder string) string {
	return fmt.Sprintf("%s IN (SELECT value FROM json_each(%s))", field, placeholder)
}

func (d *SQLiteDialect) BuildFullTextQuery(field string, placeholder string) string {
	// In SQLite FTS5, we usually have a separate virtual table.
	// However, to keep it simple and compatible with the existing QueryBuilder,
	// we might need to adjust how we join with the FTS table.
	// For now, let's assume 'routes_fts' is the FTS table and it's joined or handled.
	// The plan says: "Both use fts_table MATCH query"
	return fmt.Sprintf("routes_fts MATCH %s", placeholder)
}

func (d *SQLiteDialect) BuildSTWithinQuery(field string, xMin, yMin, xMax, yMax string) string {
	// For simplified testing without spatial functions
	return fmt.Sprintf("%s_lat BETWEEN %s AND %s AND %s_lon BETWEEN %s AND %s", field, xMin, xMax, field, yMin, yMax)
}

func (d *SQLiteDialect) BuildSTDWithinQuery(field string, lon, lat, distance string) string {
	// Simplified distance calculation without spatial functions
	return fmt.Sprintf("(%s_lat - %s) * (%s_lat - %s) + (%s_lon - %s) * (%s_lon - %s) < (%s/111.0) * (%s/111.0)", field, lat, field, lat, field, lon, field, lon, distance, distance)
}

func (d *SQLiteDialect) BuildDistanceSort(field string, lon, lat float64) string {
	// Simplified distance sort without spatial functions
	return fmt.Sprintf("(%s_lat - %f) * (%s_lat - %f) + (%s_lon - %f) * (%s_lon - %f)", field, lat, field, lat, field, lon, field, lon)
}

func (d *SQLiteDialect) BuildFTSSort(field string, placeholder string) string {
	return "rank"
}

func (d *SQLiteDialect) BuildFacetQuery(facetField string, unnest bool) string {
	if unnest {
		return fmt.Sprintf("SELECT json_each.value AS key, COUNT(*) AS value FROM routes, json_each(routes.%s)", facetField)
	}
	return fmt.Sprintf("SELECT %s AS key, COUNT(*) AS value FROM routes", facetField)
}

func (d *SQLiteDialect) BuildPrefixQuery(query string) string {
	// FTS5 prefix search uses '*' at the end of words.
	// We should probably just pass the query through and let FTS5 handle it or format it.
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
