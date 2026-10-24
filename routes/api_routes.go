package routes

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/map-services/gps-routes-api/models/common"
	"github.com/map-services/gps-routes-api/models/domain"
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
		c.JSON(http.StatusBadRequest, common.NewErrorResponse("INVALID_REQUEST", fmt.Sprintf("invalid request body: %s", err.Error())))
		return
	}

	// If limit not specified, default to 10
	if payload.Limit == 0 {
		payload.Limit = 10
	}

	if payload.Limit < 0 || payload.Limit > 100 {
		c.JSON(http.StatusBadRequest, common.NewValidationError("limit", "limit must be between 0 and 100"))
		return
	}

	if payload.Offset < 0 || payload.Offset > 100_000 {
		c.JSON(http.StatusBadRequest, common.NewValidationError("offset", "offset must be between 0 and 100000"))
		return
	}

	if payload.BoundingBox != nil && payload.Nearby != nil {
		c.JSON(http.StatusBadRequest, common.NewErrorResponse("INVALID_REQUEST", "cannot specify both 'boundingBox' and 'nearby' in the same request"))
		return
	}

	for facetField := range payload.Facets {
		if !services.IsValidFacetField(facetField) {
			validFields := services.ValidFacetFields()
			c.JSON(http.StatusBadRequest, common.NewValidationError("facets", fmt.Sprintf("invalid facet field '%s'; valid fields are: %s", facetField, validFields)))
			return
		}
	}

	if payload.Sort != nil {
		if !common.IsValidSortField(payload.Sort.Field) {
			c.JSON(http.StatusBadRequest, common.NewValidationError("sort.field", fmt.Sprintf("invalid sort field '%s'; valid fields are: distance_km, difficulty, estimated_duration, title, relevance", payload.Sort.Field)))
			return
		}
		if payload.Sort.Direction != "" && !common.IsValidSortDirection(payload.Sort.Direction) {
			c.JSON(http.StatusBadRequest, common.NewValidationError("sort.direction", fmt.Sprintf("invalid sort direction '%s'; valid values are: ASC, DESC", payload.Sort.Direction)))
			return
		}
	}

	matches, err := api.Service.Search(&payload)
	if err != nil {
		c.Error(err) //nolint:errcheck
		return
	}

	// Check Accept header for GeoJSON content negotiation
	if strings.Contains(c.GetHeader("Accept"), "application/geo+json") {
		fc := domain.ToFeatureCollection(matches)

		// Set pagination metadata via HTTP response headers
		c.Header("X-Total-Count", strconv.FormatInt(matches.Total, 10))
		c.Header("X-Has-More", strconv.FormatBool(matches.HasMore))
		if matches.HasMore {
			c.Header("X-Next-Offset", strconv.FormatInt(int64(matches.NextOffset), 10))
		}

		c.Header("Content-Type", "application/geo+json")
		c.JSON(http.StatusOK, fc)
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
