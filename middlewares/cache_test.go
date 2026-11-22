package middlewares

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func setupCacheTest(configMap map[string]CacheConfig) (*gin.Engine, map[string]CacheConfig) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(APICache(configMap))
	return engine, configMap
}

func TestAPICache_SetsETagAndCacheControl(t *testing.T) {
	configMap := map[string]CacheConfig{
		"/api": {MaxAge: 5 * time.Minute, Private: false},
	}
	engine, _ := setupCacheTest(configMap)

	engine.GET("/api/data", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "hello"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/data", nil)
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"message":"hello"`)

	// ETag should be set and properly quoted.
	etag := w.Header().Get("ETag")
	assert.NotEmpty(t, etag)
	assert.True(t, strings.HasPrefix(etag, `"`), "ETag should start with quote")
	assert.True(t, strings.HasSuffix(etag, `"`), "ETag should end with quote")

	// Cache-Control should be set.
	cacheControl := w.Header().Get("Cache-Control")
	assert.Contains(t, cacheControl, "public")
	assert.Contains(t, cacheControl, "max-age=300")
	assert.Contains(t, cacheControl, "must-revalidate")

	// Expires should be set.
	assert.NotEmpty(t, w.Header().Get("Expires"))
}

func TestAPICache_PrivateCache(t *testing.T) {
	configMap := map[string]CacheConfig{
		"/api": {MaxAge: 30 * time.Second, Private: true},
	}
	engine, _ := setupCacheTest(configMap)

	engine.GET("/api/search", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"results": []string{"a", "b"}})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/search", nil)
	engine.ServeHTTP(w, req)

	cacheControl := w.Header().Get("Cache-Control")
	assert.Contains(t, cacheControl, "private")
	assert.Contains(t, cacheControl, "max-age=30")
}

func TestAPICache_ConditionalRequest_Match(t *testing.T) {
	configMap := map[string]CacheConfig{
		"/api": {MaxAge: 1 * time.Minute, Private: false},
	}
	engine, _ := setupCacheTest(configMap)

	engine.GET("/api/item", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"id": 1, "name": "test"})
	})

	// First request — get the ETag.
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest(http.MethodGet, "/api/item", nil)
	engine.ServeHTTP(w1, req1)

	etag := w1.Header().Get("ETag")
	assert.NotEmpty(t, etag)

	// Second request with If-None-Match — should return 304.
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodGet, "/api/item", nil)
	req2.Header.Set("If-None-Match", etag)
	engine.ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusNotModified, w2.Code)
	assert.Empty(t, w2.Body.String(), "304 response should have no body")
	assert.Equal(t, etag, w2.Header().Get("ETag"))
}

func TestAPICache_ConditionalRequest_NoMatch(t *testing.T) {
	configMap := map[string]CacheConfig{
		"/api": {MaxAge: 1 * time.Minute, Private: false},
	}
	engine, _ := setupCacheTest(configMap)

	engine.GET("/api/item", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"id": 1, "name": "test"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/item", nil)
	req.Header.Set("If-None-Match", `"different-etag"`)
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, w.Body.String())
}

func TestAPICache_NoConfigForPath(t *testing.T) {
	configMap := map[string]CacheConfig{
		"/api": {MaxAge: 1 * time.Minute, Private: false},
	}
	engine, _ := setupCacheTest(configMap)

	engine.GET("/other/path", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "no cache"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/other/path", nil)
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, w.Header().Get("ETag"))
	assert.Empty(t, w.Header().Get("Cache-Control"))
}

func TestAPICache_ErrorResponse_NoCache(t *testing.T) {
	configMap := map[string]CacheConfig{
		"/api": {MaxAge: 1 * time.Minute, Private: false},
	}
	engine, _ := setupCacheTest(configMap)

	engine.GET("/api/error", func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/error", nil)
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Empty(t, w.Header().Get("ETag"))
	assert.Empty(t, w.Header().Get("Cache-Control"))
}

func TestAPICache_MostSpecificPrefix(t *testing.T) {
	configMap := map[string]CacheConfig{
		"/v1/gps-routes/ref-data": {MaxAge: 10 * time.Minute, Private: false},
		"/v1/gps-routes/":         {MaxAge: 1 * time.Minute, Private: false},
		"/v1/gps-routes/search":   {MaxAge: 30 * time.Second, Private: true},
	}
	engine, _ := setupCacheTest(configMap)

	engine.GET("/v1/gps-routes/ref-data", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ref": "data"})
	})
	engine.GET("/v1/gps-routes/abc123", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"id": "abc123"})
	})
	engine.POST("/v1/gps-routes/search", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"hits": []string{"a"}})
	})

	// Ref-data should match the most specific prefix (10 min, public).
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest(http.MethodGet, "/v1/gps-routes/ref-data", nil)
	engine.ServeHTTP(w1, req1)
	assert.Contains(t, w1.Header().Get("Cache-Control"), "max-age=600")
	assert.Contains(t, w1.Header().Get("Cache-Control"), "public")

	// Single route should match /v1/gps-routes/ prefix (1 min, public).
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodGet, "/v1/gps-routes/abc123", nil)
	engine.ServeHTTP(w2, req2)
	assert.Contains(t, w2.Header().Get("Cache-Control"), "max-age=60")
	assert.Contains(t, w2.Header().Get("Cache-Control"), "public")

	// Search should match /v1/gps-routes/search prefix (30s, private).
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest(http.MethodPost, "/v1/gps-routes/search", nil)
	engine.ServeHTTP(w3, req3)
	assert.Contains(t, w3.Header().Get("Cache-Control"), "max-age=30")
	assert.Contains(t, w3.Header().Get("Cache-Control"), "private")
}

func TestAPICache_ETagConsistency(t *testing.T) {
	configMap := map[string]CacheConfig{
		"/api": {MaxAge: 1 * time.Minute, Private: false},
	}
	engine, _ := setupCacheTest(configMap)

	engine.GET("/api/data", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"value": "constant"})
	})

	// Two requests with the same response should produce the same ETag.
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest(http.MethodGet, "/api/data", nil)
	engine.ServeHTTP(w1, req1)

	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodGet, "/api/data", nil)
	engine.ServeHTTP(w2, req2)

	assert.Equal(t, w1.Header().Get("ETag"), w2.Header().Get("ETag"))
}

func TestAPICache_MultipleETagsInIfNoneMatch(t *testing.T) {
	configMap := map[string]CacheConfig{
		"/api": {MaxAge: 1 * time.Minute, Private: false},
	}
	engine, _ := setupCacheTest(configMap)

	engine.GET("/api/item", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"id": 1})
	})

	// First request to get the ETag.
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest(http.MethodGet, "/api/item", nil)
	engine.ServeHTTP(w1, req1)
	etag := w1.Header().Get("ETag")

	// Second request with multiple ETags, one of which matches.
	// Note: etag already includes quotes, so we don't add extra ones.
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodGet, "/api/item", nil)
	req2.Header.Set("If-None-Match", `"other-etag", `+etag+`, "another"`)
	engine.ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusNotModified, w2.Code)
}

func TestMatchETag_Wildcard(t *testing.T) {
	assert.True(t, matchETag("*", `"any-etag"`))
	assert.True(t, matchETag(`"*"`, `"any-etag"`))
}

func TestMatchETag_WeakETag(t *testing.T) {
	etag := `"some-etag-value"`
	assert.True(t, matchETag("W/"+etag, etag))
}

func TestLookupConfig_NoMatch(t *testing.T) {
	configMap := map[string]CacheConfig{
		"/api": {MaxAge: 1 * time.Minute},
	}
	_, ok := lookupConfig("/other", configMap)
	assert.False(t, ok)
}

func TestLookupConfig_ZeroMaxAge(t *testing.T) {
	configMap := map[string]CacheConfig{
		"/api": {MaxAge: 0},
	}
	cfg, ok := lookupConfig("/api/test", configMap)
	assert.True(t, ok)
	assert.Equal(t, time.Duration(0), cfg.MaxAge)
}
