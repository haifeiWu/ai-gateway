package handler

import (
	"log/slog"
	"net/http"

	"github.com/ai-gateway/internal/model"
	"github.com/ai-gateway/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// TenantSvc 租户服务接口。
type TenantSvc interface {
	Create(name string) (*model.Tenant, error)
	List() ([]model.Tenant, error)
	GetByID(id string) (*model.Tenant, error)
	Update(id string, req service.UpdateTenantRequest) (*model.Tenant, error)
	Delete(id string) error
}

// KeySvc API Key 服务接口。
type KeySvc interface {
	Create(tenantID string, req service.CreateKeyRequest) (*service.CreateKeyResult, error)
	Update(id string, req service.UpdateRequest) (*model.APIKey, error)
	Delete(id string) error
	ListByTenant(tenantID string) ([]model.APIKey, error)
	GetByID(id string) (*model.APIKey, error)
}

// AdminHandler 管理 API 处理器。
type AdminHandler struct {
	tenantSvc TenantSvc
	keySvc    KeySvc
}

// NewAdminHandler 创建管理 handler。
func NewAdminHandler(tenantSvc TenantSvc, keySvc KeySvc) *AdminHandler {
	return &AdminHandler{tenantSvc: tenantSvc, keySvc: keySvc}
}

// CreateTenant POST /admin/v1/tenants
func (h *AdminHandler) CreateTenant(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required,min=1,max=100"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "name is required (1-100 chars)", "type": "invalid_request_error"},
		})
		return
	}

	tenant, err := h.tenantSvc.Create(req.Name)
	if err != nil {
		slog.Error("create tenant failed", "error", err, "name", req.Name)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": "failed to create tenant", "type": "server_error"},
		})
		return
	}

	slog.Info("tenant created", "tenant_id", tenant.ID, "name", tenant.Name, "admin_ip", c.ClientIP())
	c.JSON(http.StatusCreated, tenant)
}

// ListTenants GET /admin/v1/tenants
func (h *AdminHandler) ListTenants(c *gin.Context) {
	tenants, err := h.tenantSvc.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": "failed to list tenants", "type": "server_error"},
		})
		return
	}
	if tenants == nil {
		tenants = []model.Tenant{}
	}
	c.JSON(http.StatusOK, tenants)
}

// GetTenant GET /admin/v1/tenants/:id
func (h *AdminHandler) GetTenant(c *gin.Context) {
	tenant, err := h.tenantSvc.GetByID(c.Param("id"))
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"message": "tenant not found", "type": "not_found"},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": "failed to get tenant", "type": "server_error"},
		})
		return
	}
	c.JSON(http.StatusOK, tenant)
}

// UpdateTenant PATCH /admin/v1/tenants/:id
func (h *AdminHandler) UpdateTenant(c *gin.Context) {
	var req service.UpdateTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "invalid request body", "type": "invalid_request_error"},
		})
		return
	}

	tenant, err := h.tenantSvc.Update(c.Param("id"), req)
	if err != nil {
		slog.Error("update tenant failed", "error", err, "tenant_id", c.Param("id"))
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": err.Error(), "type": "invalid_request_error"},
		})
		return
	}

	slog.Info("tenant updated", "tenant_id", tenant.ID, "name", tenant.Name, "status", tenant.Status, "admin_ip", c.ClientIP())
	c.JSON(http.StatusOK, tenant)
}

// DeleteTenant DELETE /admin/v1/tenants/:id
func (h *AdminHandler) DeleteTenant(c *gin.Context) {
	tenantID := c.Param("id")
	if err := h.tenantSvc.Delete(tenantID); err != nil {
		slog.Error("delete tenant failed", "error", err, "tenant_id", tenantID)
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{"message": "tenant not found", "type": "not_found"},
		})
		return
	}
	slog.Info("tenant deleted", "tenant_id", tenantID, "admin_ip", c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// CreateKey POST /admin/v1/tenants/:id/keys
func (h *AdminHandler) CreateKey(c *gin.Context) {
	tenantID := c.Param("id")

	var req service.CreateKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "invalid request body", "type": "invalid_request_error"},
		})
		return
	}

	result, err := h.keySvc.Create(tenantID, req)
	if err != nil {
		slog.Error("create key failed", "error", err, "tenant_id", tenantID, "name", req.Name)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "failed to create key", "type": "invalid_request_error"},
		})
		return
	}

	slog.Info("key created", "key_id", result.ID, "tenant_id", tenantID, "name", result.Name, "admin_ip", c.ClientIP())
	c.JSON(http.StatusCreated, result)
}

// ListKeys GET /admin/v1/tenants/:id/keys
func (h *AdminHandler) ListKeys(c *gin.Context) {
	keys, err := h.keySvc.ListByTenant(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": "failed to list keys", "type": "server_error"},
		})
		return
	}
	if keys == nil {
		keys = []model.APIKey{}
	}
	c.JSON(http.StatusOK, keys)
}

// GetKey GET /admin/v1/keys/:id
func (h *AdminHandler) GetKey(c *gin.Context) {
	key, err := h.keySvc.GetByID(c.Param("id"))
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"message": "key not found", "type": "not_found"},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": "failed to get key", "type": "server_error"},
		})
		return
	}
	c.JSON(http.StatusOK, key)
}

// UpdateKey PATCH /admin/v1/keys/:id
func (h *AdminHandler) UpdateKey(c *gin.Context) {
	var req service.UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "invalid request body", "type": "invalid_request_error"},
		})
		return
	}

	key, err := h.keySvc.Update(c.Param("id"), req)
	if err != nil {
		slog.Error("update key failed", "error", err, "key_id", c.Param("id"))
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "failed to update key", "type": "invalid_request_error"},
		})
		return
	}

	slog.Info("key updated", "key_id", key.ID, "status", key.Status, "admin_ip", c.ClientIP())
	c.JSON(http.StatusOK, key)
}

// DeleteKey DELETE /admin/v1/keys/:id
func (h *AdminHandler) DeleteKey(c *gin.Context) {
	keyID := c.Param("id")
	if err := h.keySvc.Delete(keyID); err != nil {
		slog.Error("delete key failed", "error", err, "key_id", keyID)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": "failed to delete key", "type": "server_error"},
		})
		return
	}
	slog.Info("key deleted", "key_id", keyID, "admin_ip", c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}
