package routes

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/map-services/gps-routes-api/models/common"
	"github.com/map-services/gps-routes-api/models/request"
	"github.com/map-services/gps-routes-api/services"
)

type RoutesAPI struct {
	Service services.RoutesService
}

// GET /v1/gps-routes/:objectID
// Retrieve metadata for the specific walking route
func (api *RoutesAPI) FetchRecord(c *gin.Context) {
	objectID := c.Param("objectID")
	if objectID == "" {
		c.JSON(http.StatusBadRequest, common.NewErrorResponse("INVALID_REQUEST", "objectID is required"))
		return
	}
	route, err := api.Service.GetRouteByID(objectID)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}

	if route == nil {
		c.JSON(http.StatusNotFound, common.NewErrorResponse("NOT_FOUND", "The requested resource was not found"))
		return
	}

	c.JSON(http.StatusOK, route)
}

// POST /v1/gps-routes/search
// Search for routes according to various criteria
func (api *RoutesAPI) Search(c *gin.Context) {
	var payload request.SearchRequest
	err := c.ShouldBind(&payload)
	if err != nil {
		c.JSON(http.StatusBadRequest, common.NewErrorResponse("INVALID_REQUEST", "bad payload"))
		return
	}

	// If limit not specified, default to 10
	if payload.Limit == 0 {
		payload.Limit = 10
	}

	if payload.Limit < 0 || payload.Limit > 100 {
		c.JSON(http.StatusBadRequest, common.NewErrorResponse("INVALID_REQUEST", "limit is out of range - allowed values: 0..100"))
		return
	}

	if payload.Offset < 0 || payload.Offset > 100_000 {
		c.JSON(http.StatusBadRequest, common.NewErrorResponse("INVALID_REQUEST", "offset is out of range - allowed values: 0..100_000"))
		return
	}

	if payload.BoundingBox != nil && payload.Nearby != nil {
		c.JSON(http.StatusBadRequest, common.NewErrorResponse("INVALID_REQUEST", "cannot specify both bounding box and nearby attributes in same request"))
		return
	}

	for facetField := range payload.Facets {
		if !services.IsValidFacetField(facetField) {
			c.JSON(http.StatusBadRequest, common.NewErrorResponse("INVALID_REQUEST", fmt.Sprintf("invalid facet: %s", facetField)))
			return
		}
	}

	// TODO: sanitize the query string

	matches, err := api.Service.Search(&payload)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}

	c.JSON(http.StatusOK, matches)

}

// GET /v1/gps-routes/ref-data
// Retrieve reference data for the API
func (api *RoutesAPI) RefData(c *gin.Context) {

	refData, err := api.Service.RefData()
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}

	c.JSON(http.StatusOK, refData)

}
