package middlewares

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// CacheConfig holds the caching configuration for a given route pattern.
type CacheConfig struct {
	// MaxAge is the duration for which the response may be cached.
	MaxAge time.Duration
	// Private indicates whether the response is intended for a single user
	// and must not be stored by shared caches (e.g. CDNs).
	Private bool
}

// APICache returns a middleware that adds Cache-Control and ETag headers to
// API responses. It computes an ETag from the response body and supports
// conditional requests via If-None-Match (returning 304 Not Modified when
// the client's ETag matches).
//
// The configMap maps route path prefixes to their cache configuration.
// Routes that do not match any prefix are passed through without caching headers.
//
// Example:
//
//	mw := middlewares.APICache(map[string]middlewares.CacheConfig{
//	    "/v1/gps-routes/ref-data": {MaxAge: 10 * time.Minute, Private: false},
//	    "/v1/gps-routes/":         {MaxAge: 1 * time.Minute, Private: false},
//	    "/v1/gps-routes/search":   {MaxAge: 30 * time.Second, Private: true},
//	})
func APICache(configMap map[string]CacheConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Look up the cache config for this request path.
		cfg, ok := lookupConfig(c.Request.URL.Path, configMap)
		if !ok || cfg.MaxAge == 0 {
			c.Next()
			return
		}

		ifNoneMatch := c.GetHeader("If-None-Match")

		// Keep a reference to the original ResponseWriter so we can write the
		// final response directly to it after computing the ETag.
		originalWriter := c.Writer

		// Buffer the entire response so we can compute the ETag before writing
		// anything to the real ResponseWriter. This is essential for 304 support.
		buf := &bytes.Buffer{}
		statusCode := http.StatusOK
		wr := &bufferedWriter{ResponseWriter: originalWriter, buf: buf, code: &statusCode}
		c.Writer = wr

		c.Next()

		// Skip caching for error responses — pass through unchanged.
		if statusCode >= 400 {
			// Forward the buffered response to the real writer so the client
			// still sees the error response.
			originalWriter.Header().Set("Content-Type", wr.Header().Get("Content-Type"))
			originalWriter.WriteHeader(statusCode)
			originalWriter.Write(buf.Bytes()) //nolint:errcheck
			return
		}

		body := buf.Bytes()

		// Compute ETag from the captured response body.
		etag := computeETag(body)

		// Check If-None-Match for conditional requests.
		if ifNoneMatch != "" && matchETag(ifNoneMatch, etag) {
			// Client already has the current version — return 304 Not Modified.
			originalWriter.Header().Set("ETag", etag)
			setCacheControlHeaders(originalWriter.Header(), cfg)
			originalWriter.WriteHeader(http.StatusNotModified)
			return
		}

		// Write the cached response with ETag and Cache-Control headers.
		originalWriter.Header().Set("ETag", etag)
		setCacheControlHeaders(originalWriter.Header(), cfg)
		originalWriter.WriteHeader(statusCode)
		originalWriter.Write(body) //nolint:errcheck
	}
}

// lookupConfig finds the most specific matching cache config for a given path.
// It returns the config with the longest matching prefix.
func lookupConfig(path string, configMap map[string]CacheConfig) (CacheConfig, bool) {
	var bestMatch CacheConfig
	bestLen := 0

	for prefix, cfg := range configMap {
		if strings.HasPrefix(path, prefix) && len(prefix) > bestLen {
			bestMatch = cfg
			bestLen = len(prefix)
		}
	}

	if bestLen == 0 {
		return CacheConfig{}, false
	}
	return bestMatch, true
}

// computeETag generates an ETag from the response body bytes.
func computeETag(body []byte) string {
	hash := sha256.Sum256(body)
	encoded := base64.StdEncoding.EncodeToString(hash[:])
	return `"` + encoded + `"`
}

// matchETag checks whether the If-None-Match header value matches the given ETag.
// It handles quoted ETags and the wildcard "*".
func matchETag(ifNoneMatch, etag string) bool {
	if ifNoneMatch == "*" {
		return true
	}

	// If-None-Match can contain multiple ETags comma-separated.
	parts := strings.Split(ifNoneMatch, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		// Strip W/ prefix for weak comparison if present.
		part = strings.TrimPrefix(part, "W/")
		if part == etag || part == `"*"` {
			return true
		}
	}
	return false
}

// setCacheControlHeaders sets the Cache-Control and related headers.
func setCacheControlHeaders(h http.Header, cfg CacheConfig) {
	directive := "public"
	if cfg.Private {
		directive = "private"
	}
	maxAge := int(cfg.MaxAge.Seconds())
	h.Set("Cache-Control", directive+", max-age="+formatInt(maxAge)+", must-revalidate")
	h.Set("Expires", time.Now().Add(cfg.MaxAge).UTC().Format(http.TimeFormat))
}

// formatInt is a simple int-to-string formatter to avoid importing strconv
// in the hot path. It handles non-negative integers.
func formatInt(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf) - 1
	for n > 0 {
		buf[i] = byte('0' + n%10)
		i--
		n /= 10
	}
	return string(buf[i+1:])
}

// bufferedWriter wraps gin.ResponseWriter but buffers all writes instead of
// forwarding them immediately. This allows the middleware to compute the ETag
// before anything reaches the client, enabling proper 304 Not Modified support.
type bufferedWriter struct {
	gin.ResponseWriter
	buf  *bytes.Buffer
	code *int
}

func (w *bufferedWriter) Write(b []byte) (int, error) {
	return w.buf.Write(b)
}

func (w *bufferedWriter) WriteHeader(code int) {
	*w.code = code
}
