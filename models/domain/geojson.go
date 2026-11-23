package domain

import (
	"encoding/json"
	"fmt"

	"github.com/tidwall/geojson"
	"github.com/tidwall/geojson/geometry"
)

// FeatureProperties holds the route-specific properties for each GeoJSON Feature.
type FeatureProperties struct {
	ObjectID         string  `json:"objectID"`
	Ref              string  `json:"ref"`
	Title            string  `json:"title"`
	Description      string  `json:"description"`
	HeadlineImageUrl *string `json:"headline_image_url,omitempty"`
	DistanceKm       float64 `json:"distance_km,omitempty"`
}

// ToFeatureCollection converts SearchResults to a GeoJSON FeatureCollection.
// Each RouteSummary becomes a Feature with Point geometry (from start position)
// and FeatureProperties. Coordinates follow the GeoJSON convention: [longitude, latitude].
func ToFeatureCollection(searchResults *SearchResults) *geojson.FeatureCollection {
	var features []geojson.Object

	for _, hit := range searchResults.Hits {
		// GeoJSON coordinates: [longitude, latitude]
		point := geojson.NewPoint(geometry.Point{
			X: hit.StartPosition.Longitude,
			Y: hit.StartPosition.Latitude,
		})

		props := FeatureProperties{
			ObjectID:         hit.ObjectID,
			Ref:              hit.Ref,
			Title:            hit.Title,
			Description:      hit.Description,
			HeadlineImageUrl: hit.HeadlineImageUrl,
			DistanceKm:       hit.DistanceKm,
		}

		propsJSON, err := json.Marshal(props)
		if err != nil {
			// Should not happen with simple struct; skip this feature on error
			continue
		}

		// tidwall/geojson NewFeature expects members wrapped in "properties" key
		members := fmt.Sprintf(`{"properties":%s}`, string(propsJSON))
		feature := geojson.NewFeature(point, members)
		features = append(features, feature)
	}

	return geojson.NewFeatureCollection(features)
}
