package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// WriteError 写入 OpenAI 兼容的错误 JSON 响应并终止请求处理。
// 供 handler 和中间件统一使用，避免重复定义。
func WriteError(c *gin.Context, code int, message, errType string) {
	c.AbortWithStatusJSON(code, gin.H{
		"error": gin.H{"message": message, "type": errType},
	})
}

// WriteJSONError 写入标准 JSON 错误响应（不 abort，仅设置状态码和 JSON body）。
func WriteJSONError(c *gin.Context, code int, message, errType string) {
	c.JSON(code, gin.H{
		"error": gin.H{"message": message, "type": errType},
	})
}

// BadRequest 返回 400 错误。
func BadRequest(c *gin.Context, message string) {
	WriteError(c, http.StatusBadRequest, message, "invalid_request_error")
}

// NotFound 返回 404 错误。
func NotFound(c *gin.Context, message string) {
	WriteError(c, http.StatusNotFound, message, "not_found")
}

// InternalError 返回 500 错误。
func InternalError(c *gin.Context, message string) {
	WriteError(c, http.StatusInternalServerError, message, "server_error")
}
