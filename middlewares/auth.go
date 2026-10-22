package middlewares

import (
	"crypto/subtle"
	"net/http"
	"os"
	"slices"

	"github.com/gin-gonic/gin"

	"github.com/map-services/gps-routes-api/models/common"
)

func AuthMiddleware(excluded ...string) gin.HandlerFunc {
	apiKey := os.Getenv("GPS_ROUTES_API_KEY")
	if apiKey == "" {
		panic("GPS_ROUTES_API_KEY environment variable is not set")
	}

	return func(c *gin.Context) {

		// Always allow requests for CORS preflight checks
		if c.Request.Method == "OPTIONS" {
			c.Next()
			return
		}

		if slices.Contains(excluded, c.Request.URL.Path) {
			c.Next() // Skip authentication for this path
			return
		}

		clientKey := c.GetHeader("X-API-Key")

		if clientKey == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, common.NewErrorResponse("UNAUTHORIZED", "API Key Required"))
			return
		}

		if subtle.ConstantTimeCompare([]byte(clientKey), []byte(apiKey)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, common.NewErrorResponse("UNAUTHORIZED", "Invalid API Key"))
			return
		}

		c.Next() // Proceed to the next handler
	}
}
