package middleware

import (
	"net/http"

	"github.com/ai-gateway/internal/service"
	"github.com/gin-gonic/gin"
)

// SecurityHeaders 添加安全响应头中间件。
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	}
}

// RateLimit 通用限流中间件（基于客户端 IP）。
func RateLimit(limiter *service.RateLimiter, rpm int) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !limiter.Allow(c.ClientIP(), rpm) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": gin.H{"message": "rate limit exceeded", "type": "rate_limit"},
			})
			return
		}
		c.Next()
	}
}
