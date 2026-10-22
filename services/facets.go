package services

import (
	"slices"
	"strings"
)

type FacetConfig struct {
	Unnest   bool
	Limit    int32
	Excluded []string
}

var FACET_FIELDS = map[string]*FacetConfig{
	"country": {
		Unnest:   false,
		Limit:    10,
		Excluded: []string{"country", "state", "region", "county", "district"},
	},
	"state": {
		Unnest:   false,
		Limit:    20,
		Excluded: []string{"state", "region", "county", "district"},
	},
	"region": {
		Unnest:   false,
		Limit:    20,
		Excluded: []string{"region", "county", "district"},
	},
	"county": {
		Unnest:   false,
		Limit:    100,
		Excluded: []string{"county", "district"},
	},
	"district": {
		Unnest:   false,
		Limit:    20,
		Excluded: []string{"district"},
	},
	"activities": {
		Unnest:   true,
		Limit:    50,
		Excluded: []string{"activities"},
	},
	"facilities": {
		Unnest:   true,
		Limit:    50,
		Excluded: []string{"facilities"},
	},
	"points_of_interest": {
		Unnest:   true,
		Limit:    50,
		Excluded: []string{"points_of_interest"},
	},
	"route_type": {
		Unnest:   false,
		Limit:    10,
		Excluded: []string{"route_type"},
	},
	"difficulty": {
		Unnest:   false,
		Limit:    10,
		Excluded: []string{"difficulty"},
	},
	"terrain": {
		Unnest:   true,
		Limit:    50,
		Excluded: []string{"terrain"},
	},
	"estimated_duration": {
		Unnest:   false,
		Limit:    10,
		Excluded: []string{"estimated_duration"},
	},
}

func IsValidFacetField(facetField string) bool {
	_, ok := FACET_FIELDS[facetField]
	return ok
}

// ValidFacetFields returns a sorted, comma-separated list of valid facet field names.
func ValidFacetFields() string {
	fields := make([]string, 0, len(FACET_FIELDS))
	for field := range FACET_FIELDS {
		fields = append(fields, field)
	}
	slices.Sort(fields)
	return strings.Join(fields, ", ")
}
