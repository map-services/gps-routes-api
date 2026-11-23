package domain

import (
	"time"

	"github.com/map-services/gps-routes-api/models/common"
)

type RouteMetadata struct {

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

	StartPosition common.GeoLoc `json:"_geoloc"`

	// The time the route was generated.
	CreatedAt time.Time `json:"created_at"`

	Nearby []RouteSummary `json:"nearby,omitempty"`

	Details []Detail `json:"details,omitempty"`

	// URL pointing to the associated GPX route data
	GpxUrl string `json:"gpx_url,omitempty"`

	Images []Image `json:"images,omitempty"`

	// A rough distance indicating the length of the route
	DistanceKm float64 `json:"distance_km,omitempty"`

	// A link to a (YouTube) video associated with the route
	VideoUrl *string `json:"video_url,omitempty"`

	DisplayAddress *string `json:"display_address,omitempty"`

	// Where exists, a nearby postcode
	Postcode *string `json:"postcode,omitempty"`

	// The district in which the route starts
	District *string `json:"district,omitempty"`

	// The county in which the route starts
	County *string `json:"county,omitempty"`

	// The region in which the route starts
	Region *string `json:"region,omitempty"`

	State *string `json:"state,omitempty"`

	// The country in which the route starts
	Country *string `json:"country,omitempty"`

	EstimatedDuration *string `json:"estimated_duration,omitempty"`

	Difficulty *string `json:"difficulty,omitempty"`

	Terrain []string `json:"terrain,omitempty"`

	PointsOfInterest []string `json:"points_of_interest,omitempty"`

	Facilities []string `json:"facilities,omitempty"`

	RouteType *string `json:"route_type,omitempty"`

	Activities []string `json:"activities,omitempty"`
}
