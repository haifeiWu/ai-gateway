package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/ai-gateway/internal/model"
	"github.com/ai-gateway/internal/service"
	"github.com/gin-gonic/gin"
)

// KeyLookup Key 查询接口。
type KeyLookup interface {
	GetByHash(hash string) (*model.APIKey, error)
}

// TenantStatusGetter 租户状态查询接口。
type TenantStatusGetter interface {
	GetByID(id string) (*model.Tenant, error)
}

// APIKeyAuth 返回代理 API 的 Key 鉴权中间件。
// 校验流程：提取 Bearer token → SHA-256 hash → 查 DB → 校验过期/状态/tenant。
func APIKeyAuth(keySvc KeyLookup, tenantSvc TenantStatusGetter) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if auth == "" {
			writeAuthError(c, http.StatusUnauthorized, "missing API key", "auth_error")
			return
		}

		rawKey := extractBearer(auth)
		if rawKey == "" || !strings.HasPrefix(rawKey, "sk-agw-") || len(rawKey) < 39 {
			writeAuthError(c, http.StatusUnauthorized, "invalid API key format", "auth_error")
			return
		}

		hash := service.HashKey(rawKey)
		key, err := keySvc.GetByHash(hash)
		if err != nil {
			writeAuthError(c, http.StatusUnauthorized, "invalid API key", "auth_error")
			return
		}

		// 检查 Key 状态
		if key.Status == model.KeyStatusDisabled {
			writeAuthError(c, http.StatusForbidden, "API key disabled", "forbidden")
			return
		}

		// 检查过期
		if key.ExpiresAt != nil && time.Now().After(*key.ExpiresAt) {
			writeAuthError(c, http.StatusUnauthorized, "API key expired", "auth_error")
			return
		}

		// 检查租户状态
		tenant, err := tenantSvc.GetByID(key.TenantID)
		if err != nil {
			writeAuthError(c, http.StatusUnauthorized, "invalid API key", "auth_error")
			return
		}
		if tenant.Status == model.TenantStatusDisabled {
			writeAuthError(c, http.StatusForbidden, "tenant disabled", "forbidden")
			return
		}

		// 存储 key 信息到 context，供后续 handler 使用
		c.Set("api_key", key)
		c.Next()
	}
}

// writeAuthError 写入 OpenAI 兼容的错误响应（使用原子操作）。
func writeAuthError(c *gin.Context, code int, message, errType string) {
	c.AbortWithStatusJSON(code, gin.H{
		"error": gin.H{"message": message, "type": errType},
	})
}

// extractBearer 从 Authorization header 提取 Bearer token。
func extractBearer(auth string) string {
	if len(auth) > 7 && strings.EqualFold(auth[:7], "Bearer ") {
		return auth[7:]
	}
	return ""
}
