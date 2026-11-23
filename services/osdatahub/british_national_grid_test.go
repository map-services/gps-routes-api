package osdatahub

import (
	"testing"

	"github.com/map-services/gps-routes-api/models/common"
	"github.com/stretchr/testify/assert"
)

func TestToWSG84(t *testing.T) {
	tests := []struct {
		name     string
		input    *Result
		expected *common.GeoLoc
	}{
		{
			name: "London - Trafalgar Square",
			input: &Result{
				GazetteerEntry: GazetteerEntry{
					GeometryX: 530012, // Easting
					GeometryY: 180446, // Northing
				},
			},
			expected: &common.GeoLoc{
				Latitude:  51.5079941,
				Longitude: -0.1280116,
			},
		},
		{
			name: "Edinburgh Castle",
			input: &Result{
				GazetteerEntry: GazetteerEntry{
					GeometryX: 325122, // Easting
					GeometryY: 673483, // Northing
				},
			},
			expected: &common.GeoLoc{
				Latitude:  55.9485258,
				Longitude: -3.2005757,
			},
		},
		{
			name: "Cardiff Castle",
			input: &Result{
				GazetteerEntry: GazetteerEntry{
					GeometryX: 318028, // Easting
					GeometryY: 176630, // Northing
				},
			},
			expected: &common.GeoLoc{
				Latitude:  51.4826620,
				Longitude: -3.1818654,
			},
		},
		{
			name: "Belfast City Hall",
			input: &Result{
				GazetteerEntry: GazetteerEntry{
					GeometryX: 146610, // Easting
					GeometryY: 520850, // Northing
				},
			},
			expected: &common.GeoLoc{
				Latitude:  54.5192536,
				Longitude: -5.9169090,
			},
		},
		{
			name: "Zero values",
			input: &Result{
				GazetteerEntry: GazetteerEntry{
					GeometryX: 0,
					GeometryY: 0,
				},
			},
			expected: &common.GeoLoc{
				Latitude:  49.766827,
				Longitude: -7.556855,
			},
		},
		{
			name:     "Nil values",
			input:    nil,
			expected: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ToWSG84(tc.input)

			if tc.input == nil && tc.expected == nil {
				assert.Equal(t, tc.expected, result)
			} else {
				assert.InDelta(t, tc.expected.Latitude, result.Latitude, 0.001, "Latitude should match within delta")
				assert.InDelta(t, tc.expected.Longitude, result.Longitude, 0.001, "Longitude should match within delta")
			}
		})
	}
}
