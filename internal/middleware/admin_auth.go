package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// AdminAuth 返回管理 API 的 Admin Token 鉴权中间件。
func AdminAuth(adminToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if auth == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"message": "missing admin token", "type": "auth_error"},
			})
			return
		}
		token := ""
		if len(auth) > 7 && strings.EqualFold(auth[:7], "Bearer ") {
			token = auth[7:]
		} else {
			token = auth
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(adminToken)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"message": "invalid admin token", "type": "auth_error"},
			})
			return
		}
		c.Next()
	}
}
