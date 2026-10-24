package request

import "github.com/map-services/gps-routes-api/models/common"

type SearchRequest struct {

	// Search phrase
	Query string `json:"query"`

	Offset int32 `json:"offset,omitempty"`

	Limit int32 `json:"limit,omitempty"`

	// A bounding box filter for proximity search. Mutually exclusive with Nearby.
	BoundingBox *common.BoundingBox `json:"boundingBox,omitempty"`

	// Filtering by facet values
	Facets map[string][]string `json:"facets,omitempty"`

	Nearby *Nearby `json:"nearby,omitempty"`

	TruncateText bool `json:"truncateText,omitempty"`

	SkipFacets bool `json:"skipFacets,omitempty"`
}

type Nearby struct {
	Place      string         `json:"place"`
	Center     *common.GeoLoc `json:"center,omitempty"`
	DistanceKm int32          `json:"distanceKm"`
}
