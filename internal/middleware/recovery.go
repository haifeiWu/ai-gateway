package middleware

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Recovery 返回 panic recovery 中间件，记录日志并返回 500。
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				slog.Error("panic recovered", "error", err, "path", c.Request.URL.Path)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error": gin.H{"message": "internal server error", "type": "server_error"},
				})
			}
		}()
		c.Next()
	}
}
