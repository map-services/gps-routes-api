package domain

import "github.com/map-services/gps-routes-api/models/common"

type SearchResults struct {
	Total int64 `json:"total"`

	Hits []RouteSummary `json:"hits"`

	BoundingBox *common.BoundingBox `json:"boundingBox,omitempty"`

	Facets Facets `json:"facets,omitempty"`

	Attribution []string `json:"attribution"`

	// Pagination metadata
	Offset int32 `json:"offset"`

	Limit int32 `json:"limit"`

	HasMore bool `json:"hasMore"`

	NextOffset int32 `json:"nextOffset,omitempty"`
}
