package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/map-services/gps-routes-api/models/common"
	"github.com/map-services/gps-routes-api/models/domain"
	"github.com/map-services/gps-routes-api/models/request"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// mockRoutesService implements services.RoutesService for testing
type mockRoutesService struct {
	mock.Mock
}

func (m *mockRoutesService) GetRouteByID(objectID string) (*domain.RouteMetadata, error) {
	args := m.Called(objectID)
	return args.Get(0).(*domain.RouteMetadata), args.Error(1)
}

func (m *mockRoutesService) Search(criteria *request.SearchRequest) (*domain.SearchResults, error) {
	args := m.Called(criteria)
	return args.Get(0).(*domain.SearchResults), args.Error(1)
}

func (m *mockRoutesService) RefData() (*domain.RefData, error) {
	args := m.Called()
	return args.Get(0).(*domain.RefData), args.Error(1)
}

func newTestSearchResults() *domain.SearchResults {
	imageURL := "http://example.com/image.jpg"
	return &domain.SearchResults{
		Total: 2,
		Hits: []domain.RouteSummary{
			{
				ObjectID:         "abc123",
				Ref:              "REF001",
				Title:            "Test Route One",
				Description:      "A test route",
				HeadlineImageUrl: &imageURL,
				DistanceKm:       5.5,
				StartPosition:    common.GeoLoc{Latitude: 51.5074, Longitude: -0.1278},
			},
			{
				ObjectID:      "def456",
				Ref:           "REF002",
				Title:         "Test Route Two",
				Description:   "Another test route",
				DistanceKm:    3.2,
				StartPosition: common.GeoLoc{Latitude: 52.4862, Longitude: -1.8904},
			},
		},
		Attribution: []string{"Test Attribution"},
		Offset:      0,
		Limit:       10,
		HasMore:     true,
		NextOffset:  10,
	}
}

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

func TestSearch_GeoJSONAcceptHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockService := new(mockRoutesService)
	results := newTestSearchResults()
	mockService.On("Search", mock.Anything).Return(results, nil)

	engine := gin.New()
	api := &RoutesAPI{Service: mockService}
	engine.POST("/search", api.Search)

	body := `{}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/search", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/geo+json")
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/geo+json")

	// Verify FeatureCollection structure
	var fc map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &fc); err != nil {
		t.Fatalf("failed to unmarshal GeoJSON response: %v", err)
	}
	assert.Equal(t, "FeatureCollection", fc["type"])

	features, ok := fc["features"].([]interface{})
	assert.True(t, ok, "features should be an array")
	assert.Equal(t, 2, len(features), "should have 2 features")

	// Verify first feature structure
	feature1 := features[0].(map[string]interface{})
	assert.Equal(t, "Feature", feature1["type"])

	geometry := feature1["geometry"].(map[string]interface{})
	assert.Equal(t, "Point", geometry["type"])

	coords := geometry["coordinates"].([]interface{})
	assert.Equal(t, 2, len(coords))
	// GeoJSON: [longitude, latitude]
	assert.InDelta(t, -0.1278, coords[0].(float64), 0.0001)
	assert.InDelta(t, 51.5074, coords[1].(float64), 0.0001)

	properties := feature1["properties"].(map[string]interface{})
	assert.Equal(t, "abc123", properties["objectID"])
	assert.Equal(t, "REF001", properties["ref"])
	assert.Equal(t, "Test Route One", properties["title"])
}

func TestSearch_DefaultJSONResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockService := new(mockRoutesService)
	results := newTestSearchResults()
	mockService.On("Search", mock.Anything).Return(results, nil)

	engine := gin.New()
	api := &RoutesAPI{Service: mockService}
	engine.POST("/search", api.Search)

	body := `{}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/search", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

	// Verify SearchResults structure (not GeoJSON)
	var sr domain.SearchResults
	if err := json.Unmarshal(w.Body.Bytes(), &sr); err != nil {
		t.Fatalf("failed to unmarshal SearchResults: %v", err)
	}
	assert.Equal(t, int64(2), sr.Total)
	assert.Equal(t, 2, len(sr.Hits))
	assert.Equal(t, "abc123", sr.Hits[0].ObjectID)
}

func TestSearch_NoAcceptHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockService := new(mockRoutesService)
	results := newTestSearchResults()
	mockService.On("Search", mock.Anything).Return(results, nil)

	engine := gin.New()
	api := &RoutesAPI{Service: mockService}
	engine.POST("/search", api.Search)

	body := `{}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/search", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// No Accept header set
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Should default to JSON SearchResults
	var sr domain.SearchResults
	if err := json.Unmarshal(w.Body.Bytes(), &sr); err != nil {
		t.Fatalf("failed to unmarshal SearchResults: %v", err)
	}
	assert.Equal(t, int64(2), sr.Total)
}

func TestSearch_CoordinateOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockService := new(mockRoutesService)
	results := newTestSearchResults()
	mockService.On("Search", mock.Anything).Return(results, nil)

	engine := gin.New()
	api := &RoutesAPI{Service: mockService}
	engine.POST("/search", api.Search)

	body := `{}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/search", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/geo+json")
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var fc map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &fc); err != nil {
		t.Fatalf("failed to unmarshal GeoJSON response: %v", err)
	}
	features := fc["features"].([]interface{})

	// Check second feature coordinates
	feature2 := features[1].(map[string]interface{})
	geometry := feature2["geometry"].(map[string]interface{})
	coords := geometry["coordinates"].([]interface{})

	// GeoJSON: [longitude, latitude] — must be swapped from internal [lat, lng]
	assert.InDelta(t, -1.8904, coords[0].(float64), 0.0001, "first coord should be longitude")
	assert.InDelta(t, 52.4862, coords[1].(float64), 0.0001, "second coord should be latitude")
}

func TestSearch_PaginationHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockService := new(mockRoutesService)
	results := newTestSearchResults()
	mockService.On("Search", mock.Anything).Return(results, nil)

	engine := gin.New()
	api := &RoutesAPI{Service: mockService}
	engine.POST("/search", api.Search)

	body := `{}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/search", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/geo+json")
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify pagination headers
	assert.Equal(t, "2", w.Header().Get("X-Total-Count"))
	assert.Equal(t, "true", w.Header().Get("X-Has-More"))
	assert.Equal(t, "10", w.Header().Get("X-Next-Offset"))
}

func TestSearch_EmptyResultsGeoJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockService := new(mockRoutesService)
	emptyResults := &domain.SearchResults{
		Total:       0,
		Hits:        []domain.RouteSummary{},
		Attribution: []string{"Test Attribution"},
		Offset:      0,
		Limit:       10,
		HasMore:     false,
	}
	mockService.On("Search", mock.Anything).Return(emptyResults, nil)

	engine := gin.New()
	api := &RoutesAPI{Service: mockService}
	engine.POST("/search", api.Search)

	body := `{}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/search", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/geo+json")
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var fc map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &fc); err != nil {
		t.Fatalf("failed to unmarshal GeoJSON response: %v", err)
	}
	assert.Equal(t, "FeatureCollection", fc["type"])

	features := fc["features"].([]interface{})
	assert.Equal(t, 0, len(features), "empty results should have no features")

	assert.Equal(t, "0", w.Header().Get("X-Total-Count"))
	assert.Equal(t, "false", w.Header().Get("X-Has-More"))
	assert.Empty(t, w.Header().Get("X-Next-Offset"))
}

func TestSearch_CompoundAcceptHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockService := new(mockRoutesService)
	results := newTestSearchResults()
	mockService.On("Search", mock.Anything).Return(results, nil)

	engine := gin.New()
	api := &RoutesAPI{Service: mockService}
	engine.POST("/search", api.Search)

	body := `{}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/search", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, application/geo+json")
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/geo+json")
}

func TestSearch_StarStarAcceptHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockService := new(mockRoutesService)
	results := newTestSearchResults()
	mockService.On("Search", mock.Anything).Return(results, nil)

	engine := gin.New()
	api := &RoutesAPI{Service: mockService}
	engine.POST("/search", api.Search)

	body := `{}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/search", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// */* should default to JSON SearchResults, not GeoJSON
	var sr domain.SearchResults
	if err := json.Unmarshal(w.Body.Bytes(), &sr); err != nil {
		t.Fatalf("failed to unmarshal SearchResults: %v", err)
	}
	assert.Equal(t, int64(2), sr.Total)
}

func TestSearch_NilHeadlineImageUrl(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockService := new(mockRoutesService)
	results := &domain.SearchResults{
		Total: 1,
		Hits: []domain.RouteSummary{
			{
				ObjectID:      "nil-img",
				Ref:           "REF003",
				Title:         "Route Without Image",
				Description:   "No image here",
				HeadlineImageUrl: nil,
				DistanceKm:    2.0,
				StartPosition: common.GeoLoc{Latitude: 53.4808, Longitude: -2.2426},
			},
		},
		Attribution: []string{"Test"},
		Offset:      0,
		Limit:       10,
		HasMore:     false,
	}
	mockService.On("Search", mock.Anything).Return(results, nil)

	engine := gin.New()
	api := &RoutesAPI{Service: mockService}
	engine.POST("/search", api.Search)

	body := `{}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/search", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/geo+json")
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var fc map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &fc); err != nil {
		t.Fatalf("failed to unmarshal GeoJSON response: %v", err)
	}
	features := fc["features"].([]interface{})
	assert.Equal(t, 1, len(features))

	feature := features[0].(map[string]interface{})
	properties := feature["properties"].(map[string]interface{})
	assert.Equal(t, "nil-img", properties["objectID"])
	// headline_image_url should be omitted due to omitempty
	_, hasImage := properties["headline_image_url"]
	assert.False(t, hasImage, "nil headline_image_url should be omitted")
}

