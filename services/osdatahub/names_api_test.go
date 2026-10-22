package osdatahub

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewNamesApi(t *testing.T) {
	api := NewNamesApi("https://api.os.uk/search/names/v1", "test-api-key")
	assert.NotNil(t, api)
	assert.Equal(t, "https://api.os.uk/search/names/v1", api.baseUrl)
	assert.Equal(t, "test-api-key", api.apiKey)
}

func TestFind_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/find", r.URL.Path)
		assert.Equal(t, "1", r.URL.Query().Get("maxresults"))
		assert.Equal(t, "JSON", r.URL.Query().Get("format"))
		assert.Equal(t, "London", r.URL.Query().Get("query"))
		assert.Equal(t, "test-api-key", r.URL.Query().Get("key"))
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{
			"header": {
				"uri": "https://api.os.uk/search/names/v1/find?query=London",
				"query": "London",
				"format": "JSON",
				"maxresults": 1,
				"offset": 0,
				"totalresults": 1
			},
			"results": [
				{
					"GAZETTEER_ENTRY": {
						"ID": "123",
						"NAME1": "London",
						"TYPE": "City",
						"LOCAL_TYPE": "Capital",
						"GEOMETRY_X": 529824,
						"GEOMETRY_Y": 180433,
						"COUNTRY": "England"
					}
				}
			]
		}`))
		if err != nil {
			t.Fatalf("Failed to write response: %v", err)
		}
	}))

	defer server.Close()
	api := NewNamesApi(server.URL, "test-api-key")
	result, err := api.Find(t.Context(), "London")
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "123", result.GazetteerEntry.ID)
	assert.Equal(t, "London", result.GazetteerEntry.Name1)
	assert.Equal(t, float64(529824), result.GazetteerEntry.GeometryX)
	assert.Equal(t, float64(180433), result.GazetteerEntry.GeometryY)
}

func TestFind_NoResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/find", r.URL.Path)

		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{
			"header": {
				"uri": "https://api.os.uk/search/names/v1/find?query=NonExistentPlace",
				"query": "NonExistentPlace",
				"format": "JSON",
				"maxresults": 1,
				"offset": 0,
				"totalresults": 0
			},
			"results": []
		}`))
		if err != nil {
			t.Fatalf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()
	api := NewNamesApi(server.URL, "test-api-key")
	result, err := api.Find(t.Context(), "NonExistentPlace")
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestFind_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/find", r.URL.Path)

		w.WriteHeader(http.StatusUnauthorized)
		_, err := w.Write([]byte(`{
			"fault": {
				"faultstring": "Invalid API key",
				"detail": {
					"errorcode": "AUTH_ERROR"
				}
			}
		}`))
		if err != nil {
			t.Fatalf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()
	api := NewNamesApi(server.URL, "invalid-api-key")
	result, err := api.Find(t.Context(), "London")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "Invalid API key")
}

func TestFind_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/find", r.URL.Path)

		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`invalid json response`))
		if err != nil {
			t.Fatalf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()
	api := NewNamesApi(server.URL, "test-api-key")
	result, err := api.Find(t.Context(), "London")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "error parsing JSON")
}

func TestFind_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/find", r.URL.Path)

		w.WriteHeader(http.StatusInternalServerError)
		_, err := w.Write([]byte(`{
			"fault": {
				"faultstring": "Internal server error",
				"detail": {
					"errorcode": "SERVER_ERROR"
				}
			}
		}`))
		if err != nil {
			t.Fatalf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()
	api := NewNamesApi(server.URL, "test-api-key")
	result, err := api.Find(t.Context(), "London")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "Internal server error")
}

func TestFind_RequestError(t *testing.T) {
	api := NewNamesApi("http://invalid-url-that-should-cause-an-error", "test-api-key")
	result, err := api.Find(t.Context(), "London")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "error making request")
}
