package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/map-services/gps-routes-api/models/common"
	"github.com/stretchr/testify/assert"
)

func TestSearchInvalidFacetIncludesValidFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	api := &RoutesAPI{}
	engine.POST("/search", api.Search)

	body := `{"facets":{"invalid_field":["value"]}}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/search", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp common.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal error response: %v", err)
	}

	assert.Equal(t, "VALIDATION_ERROR", resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "invalid_field")
	assert.Equal(t, "facets", resp.Error.Field)

	// The error message should contain the list of valid facet fields
	assert.Contains(t, resp.Error.Message, "activities")
	assert.Contains(t, resp.Error.Message, "country")
	assert.Contains(t, resp.Error.Message, "difficulty")
	assert.Contains(t, resp.Error.Message, "region")
}
