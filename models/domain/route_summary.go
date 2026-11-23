package domain

import (
	"github.com/map-services/gps-routes-api/models/common"
)

type RouteSummary struct {

	// The object ID identifies the GPS route (typically this is the MD5 hash of the route reference)
	ObjectID string `json:"objectID"`

	// A human friendly unique identifier for the route.
	Ref string `json:"ref"`

	// The route title
	Title string `json:"title"`

	// Typically a long passage of text describing aspects of the route
	Description string `json:"description"`

	// The main image URL associated with the route
	HeadlineImageUrl *string `json:"headline_image_url"`

	// A rough distance indicating the length of the route
	DistanceKm float64 `json:"distance_km,omitempty"`

	StartPosition common.GeoLoc `json:"_geoloc"`
}
