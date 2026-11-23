package db

import (
	"fmt"
	"strings"

	"github.com/map-services/gps-routes-api/models/request"
)

type QueryBuilder struct {
	dialect         Dialect
	criteria        *request.SearchRequest
	selectPart      string
	whereClauses    []string
	excludedFacets  map[string]struct{}
	arrayFields     map[string]struct{}
	params          []any
	orderBy         string
	groupBy         string
	limit           string
	limitParam      int32
	offset          string
	offsetParam     int32
	truncatedFields map[string]int
}

func NewQueryBuilder(dialect Dialect, selectPart string, criteria *request.SearchRequest) *QueryBuilder {
	qb := &QueryBuilder{
		dialect:         dialect,
		selectPart:      selectPart,
		criteria:        criteria,
		params:          make([]any, 0),
		truncatedFields: make(map[string]int, 0),
	}

	return qb.applyWhereConditions()
}

func (qb *QueryBuilder) WithWhereClause(whereClause string) *QueryBuilder {
	qb.whereClauses = append(qb.whereClauses, whereClause)
	return qb
}

func (qb *QueryBuilder) WithOrderBy(orderByClause string) *QueryBuilder {
	if qb.orderBy != "" {
		panic("unexpected: orderBy value already set")
	}

	qb.orderBy = fmt.Sprintf("ORDER BY %s", orderByClause)
	return qb
}

func (qb *QueryBuilder) WithGroupBy(fieldName string) *QueryBuilder {
	if qb.groupBy != "" {
		panic("unexpected: groupBy value already set")
	}

	qb.groupBy = fmt.Sprintf("GROUP BY %s", fieldName)
	return qb
}

func (qb *QueryBuilder) WithOffset(offset int32) *QueryBuilder {
	if qb.offset != "" {
		panic("unexpected: offset value already set")
	}

	// Placeholder + param are emitted in Build() (not here) so qb.params order
	// matches the textual order of '?' in the final SQL (WHERE ... LIMIT ...
	// OFFSET ...). See Build()'s comment for why SQLite needs this.
	qb.offset = "OFFSET"
	qb.offsetParam = offset
	return qb
}

func (qb *QueryBuilder) WithLimit(limit int32) *QueryBuilder {
	// interpret a negative number as "no limit"
	// NOTE: we don't allow external callers to set no limit though, only internal usage
	if limit < 0 {
		return qb
	}

	if qb.limit != "" {
		panic("unexpected: limit value already set")
	}

	qb.limit = "LIMIT"
	qb.limitParam = limit
	return qb
}

func (qb *QueryBuilder) WithExcludeFacets(facetNames ...string) *QueryBuilder {
	qb.excludedFacets = toSet(facetNames...)
	return qb
}

func (qb *QueryBuilder) WithArrayFields(arrayFields ...string) *QueryBuilder {
	qb.arrayFields = toSet(arrayFields...)
	return qb
}

func (qb *QueryBuilder) WithTruncatedField(fieldName string, maxLength int) *QueryBuilder {
	qb.truncatedFields[fieldName] = maxLength
	return qb
}

func (qb *QueryBuilder) WithParam(param any) *QueryBuilder {
	qb.params = append(qb.params, param)
	return qb
}

func (qb *QueryBuilder) Build() (string, []any) {

	if qb.criteria.Facets != nil {
		for facet, values := range qb.criteria.Facets {
			_, isExcluded := qb.excludedFacets[facet]
			if isExcluded {
				continue
			}

			if len(values) == 0 {
				continue
			}

			_, isArrayField := qb.arrayFields[facet]

			placeholder := qb.dialect.FormatParam(len(qb.params) + 1)
			var condition string
			if isArrayField {
				condition = qb.dialect.BuildArrayOverlapQuery(facet, placeholder)
			} else {
				condition = qb.dialect.BuildAnyQuery(facet, placeholder)
			}
			param := qb.dialect.PrepareParam(values)

			qb.WithWhereClause(condition).WithParam(param)
		}
	}

	// Append LIMIT/OFFSET placeholders + params LAST (after the WHERE/facet
	// params above) so qb.params order matches the textual order of '?'
	// placeholders in the final SQL (WHERE ... LIMIT ... OFFSET ...). SQLite
	// binds '?' positionally by appearance order; the previous implementation
	// appended these params within WithLimit/WithOffset — i.e. BEFORE the facet
	// params that Build() injects here — so a facet predicate like
	// `district IN (SELECT value FROM json_each(?))` was bound to the LIMIT int
	// while LIMIT was bound to the facet JSON array, silently yielding 0 facet
	// hits.
	if qb.limit != "" && qb.limit == "LIMIT" {
		placeholder := qb.dialect.FormatParam(len(qb.params) + 1)
		qb.limit = fmt.Sprintf("LIMIT %s", placeholder)
		qb.params = append(qb.params, qb.limitParam)
	}
	if qb.offset != "" && qb.offset == "OFFSET" {
		placeholder := qb.dialect.FormatParam(len(qb.params) + 1)
		qb.offset = fmt.Sprintf("OFFSET %s", placeholder)
		qb.params = append(qb.params, qb.offsetParam)
	}

	var whereClause string
	if len(qb.whereClauses) > 0 {
		whereClause = "WHERE " + strings.Join(qb.whereClauses, " AND ")
	}

	selectPart := qb.replaceTruncatedFields(qb.selectPart)
	// NOTE: standard SQL clause order is LIMIT ... OFFSET .... SQLite requires
	// LIMIT before OFFSET, so emit limit before offset. SearchHits/
	// WithLimit+WithOffset always append params as [limit, offset].
	filteredParts := removeEmptyStrings(selectPart, whereClause, qb.groupBy, qb.orderBy, qb.limit, qb.offset)
	return strings.Join(filteredParts, " "), qb.params
}

func (qb *QueryBuilder) replaceTruncatedFields(query string) string {
	for fieldName, maxLength := range qb.truncatedFields {
		query = strings.ReplaceAll(query, fieldName, qb.dialect.BuildTruncateQuery(fieldName, maxLength))
	}
	return query
}

func asFieldAlias(fieldName string) string {
	// if field starts with a table name, e.g. "routes.title", we want to use the field name without the table name as the alias, e.g. "title"
	parts := strings.Split(fieldName, ".")
	return parts[len(parts)-1]
}

func removeEmptyStrings(slice ...string) []string {
	var result []string
	for _, str := range slice {
		if str != "" {
			result = append(result, str)
		}
	}
	return result
}

func (qb *QueryBuilder) applyWhereConditions() *QueryBuilder {

	if qb.criteria.Query != "" {
		placeholder := qb.dialect.FormatParam(len(qb.params) + 1)
		// FTS is handled against the routes_fts virtual table; the JOIN is added
		// in the repository layer (CountAll / SearchHits / FacetCounts).
		qb.WithWhereClause(qb.dialect.BuildFullTextQuery("", placeholder))
		qb.params = append(qb.params, qb.dialect.BuildPrefixQuery(qb.criteria.Query))
	}

	if qb.criteria.BoundingBox != nil {
		// R*Tree bbox prefilter via routes_rtree (SQLite built-in, no extension).
		// Values are embedded directly as SQL literals by BuildSTWithinQuery —
		// no '?' params — consistent with BuildDistanceSort which also embeds
		// float64 literals.
		b := qb.criteria.BoundingBox
		qb.WithWhereClause(qb.dialect.BuildSTWithinQuery("_geoloc",
			b.MinLng, b.MinLat, b.MaxLng, b.MaxLat))
	} else if qb.criteria.Nearby != nil && qb.criteria.Nearby.Center != nil {
		// R*Tree bbox prefilter + haversine_distance() exact filter.
		// BuildSTDWithinQuery computes the bbox from the centre + distance and
		// embeds all values directly — no '?' params.
		lat := qb.criteria.Nearby.Center.Latitude
		lon := qb.criteria.Nearby.Center.Longitude
		dist := float64(qb.criteria.Nearby.DistanceKm) * 1000
		qb.WithWhereClause(qb.dialect.BuildSTDWithinQuery("_geoloc", lon, lat, dist))
	}

	return qb
}

func toSet(values ...string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}
