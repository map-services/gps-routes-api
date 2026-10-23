package common

// BoundingBox represents a geographic bounding box with min/max latitude and longitude.
type BoundingBox struct {
	MinLat float64 `json:"minLat"`
	MinLng float64 `json:"minLng"`
	MaxLat float64 `json:"maxLat"`
	MaxLng float64 `json:"maxLng"`
}

// NewBoundingBox creates an initial BoundingBox from a single GeoLoc point.
func NewBoundingBox(loc GeoLoc) *BoundingBox {
	return &BoundingBox{
		MinLat: loc.Latitude,
		MinLng: loc.Longitude,
		MaxLat: loc.Latitude,
		MaxLng: loc.Longitude,
	}
}

// Expand grows the BoundingBox to include the given GeoLoc point.
// If the BoundingBox is nil, it creates a new one from the point.
func (b *BoundingBox) Expand(loc GeoLoc) {
	if loc.Latitude < b.MinLat {
		b.MinLat = loc.Latitude
	}
	if loc.Latitude > b.MaxLat {
		b.MaxLat = loc.Latitude
	}
	if loc.Longitude < b.MinLng {
		b.MinLng = loc.Longitude
	}
	if loc.Longitude > b.MaxLng {
		b.MaxLng = loc.Longitude
	}
}
