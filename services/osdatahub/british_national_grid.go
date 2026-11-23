package osdatahub

import (
	"github.com/map-services/gps-routes-api/models/common"
	"github.com/wroge/wgs84"
)

var epsg = wgs84.EPSG().Code(4326)
var bng = wgs84.OSGB36NationalGrid()

func ToWSG84(result *Result) *common.GeoLoc {
	if result == nil {
		return nil
	}

	lng, lat, _ := bng.To(epsg)(
		result.GazetteerEntry.GeometryX,
		result.GazetteerEntry.GeometryY,
		0,
	)

	return &common.GeoLoc{Latitude: lat, Longitude: lng}
}
