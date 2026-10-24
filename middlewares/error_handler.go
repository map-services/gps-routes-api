package middlewares

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/map-services/gps-routes-api/models/common"
)

func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if len(c.Errors) > 0 {
			for _, ginErr := range c.Errors {
				slog.Error("Handler error", "error", ginErr.Err)
			}

			c.JSON(http.StatusInternalServerError, common.NewErrorResponse("INTERNAL_ERROR", "An unexpected error occurred"))
		}
	}
}
