package common

// Sort specifies a field and direction for ordering search results.
type Sort struct {
	// Field to sort by. Must be one of: distance_km, difficulty, estimated_duration, title, relevance.
	Field string `json:"field"`

	// Direction: ASC (ascending) or DESC (descending). Defaults to ASC if omitted.
	Direction string `json:"direction,omitempty"`
}

// Valid sort fields
var validSortFields = map[string]bool{
	"distance_km":        true,
	"difficulty":         true,
	"estimated_duration": true,
	"title":              true,
	"relevance":          true,
}

// Valid sort directions
var validSortDirections = map[string]bool{
	"ASC":  true,
	"DESC": true,
}

// IsValidSortField returns true if the field is a valid sort field.
func IsValidSortField(field string) bool {
	return validSortFields[field]
}

// IsValidSortDirection returns true if the direction is a valid sort direction.
func IsValidSortDirection(dir string) bool {
	return validSortDirections[dir]
}
