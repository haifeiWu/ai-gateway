package router

import (
	"github.com/ai-gateway/internal/config"
	"github.com/ai-gateway/internal/handler"
	"github.com/ai-gateway/internal/middleware"
	"github.com/ai-gateway/internal/repository"
	"github.com/ai-gateway/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Setup 注册所有路由并返回 Gin engine 和限流器（调用方需在退出前 Close 限流器）。
func Setup(db *gorm.DB, cfg *config.Config, usageWriter *service.UsageWriter) (*gin.Engine, *service.RateLimiter, *service.RateLimiter) {
	gin.SetMode(gin.ReleaseMode)

	// 初始化各层
	tenantStore := repository.NewTenantStore(db)
	keyStore := repository.NewKeyStore(db)
	usageStore := repository.NewUsageStore(db)

	tenantSvc := service.NewTenantService(tenantStore)
	keySvc := service.NewAPIKeyService(keyStore, tenantStore)
	rateLimiter := service.NewRateLimiter()

	adminH := handler.NewAdminHandler(tenantSvc, keySvc)
	proxyH := handler.NewProxyHandler(cfg.MockProviderURL, rateLimiter, usageWriter)
	usageH := handler.NewUsageHandler(usageStore)

	r := gin.New()
	r.Use(gin.Logger(), middleware.Recovery(), middleware.SecurityHeaders())

	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// Dashboard 静态文件
	r.Static("/dashboard", "./web")

	// 管理 API（Admin Token 鉴权 + 限流）
	adminLimiter := service.NewRateLimiter()
	admin := r.Group("/admin/v1")
	admin.Use(middleware.AdminAuth(cfg.AdminToken))
	admin.Use(middleware.RateLimit(adminLimiter, 20))
	{
		admin.POST("/tenants", adminH.CreateTenant)
		admin.GET("/tenants", adminH.ListTenants)
		admin.GET("/tenants/:id", adminH.GetTenant)
		admin.PATCH("/tenants/:id", adminH.UpdateTenant)
		admin.DELETE("/tenants/:id", adminH.DeleteTenant)
		admin.POST("/tenants/:id/keys", adminH.CreateKey)
		admin.GET("/tenants/:id/keys", adminH.ListKeys)
		admin.GET("/keys/:id", adminH.GetKey)
		admin.PATCH("/keys/:id", adminH.UpdateKey)
		admin.DELETE("/keys/:id", adminH.DeleteKey)
		admin.GET("/usage", usageH.Query)
	}

	// 代理 API（租户 API Key 鉴权）
	proxy := r.Group("/v1")
	proxy.Use(middleware.APIKeyAuth(keySvc, tenantSvc))
	{
		proxy.POST("/chat/completions", proxyH.ChatCompletions)
		proxy.POST("/embeddings", proxyH.Embeddings)
		proxy.GET("/models", proxyH.Models)
	}

	return r, rateLimiter, adminLimiter
}
