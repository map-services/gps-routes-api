package cmds

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	ratelimit "github.com/JGLTechnologies/gin-rate-limit"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestRateLimitHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rlStore := ratelimit.InMemoryStore(&ratelimit.InMemoryOptions{
		Rate:  time.Second,
		Limit: 5,
	})
	rateLimiter := ratelimit.RateLimiter(rlStore, &ratelimit.Options{
		KeyFunc: func(c *gin.Context) string {
			return c.ClientIP()
		},
		BeforeResponse: func(c *gin.Context, info ratelimit.Info) {
			c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", info.Limit))
			c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", info.RemainingHits))
			c.Header("X-RateLimit-Reset", fmt.Sprintf("%d", info.ResetTime.Unix()))
		},
	})

	engine := gin.New()
	engine.Use(rateLimiter)
	engine.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify rate limit headers are present
	assert.Equal(t, "5", w.Header().Get("X-RateLimit-Limit"), "X-RateLimit-Limit should be 5")
	assert.NotEmpty(t, w.Header().Get("X-RateLimit-Remaining"), "X-RateLimit-Remaining should be present")
	assert.NotEmpty(t, w.Header().Get("X-RateLimit-Reset"), "X-RateLimit-Reset should be present")

	// Verify reset time is a valid Unix timestamp (positive int64)
	reset := w.Header().Get("X-RateLimit-Reset")
	resetTime, err := strconv.ParseInt(reset, 10, 64)
	assert.NoError(t, err, "X-RateLimit-Reset should be a valid integer")
	assert.True(t, resetTime > 0, "X-RateLimit-Reset should be a positive Unix timestamp")
}
