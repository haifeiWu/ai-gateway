package service

import (
	"fmt"

	"github.com/ai-gateway/internal/model"
	"github.com/google/uuid"
)

// TenantStore 租户存储接口。
type TenantStore interface {
	Create(t *model.Tenant) error
	List() ([]model.Tenant, error)
	GetByID(id string) (*model.Tenant, error)
	Update(t *model.Tenant) error
	Delete(id string) error
}

// TenantService 租户业务逻辑。
type TenantService struct {
	store TenantStore
}

// NewTenantService 创建租户服务。
func NewTenantService(store TenantStore) *TenantService {
	return &TenantService{store: store}
}

// Create 创建租户。
func (s *TenantService) Create(name string) (*model.Tenant, error) {
	tenant := &model.Tenant{
		ID:     uuid.NewString(),
		Name:   name,
		Status: model.TenantStatusActive,
	}
	if err := s.store.Create(tenant); err != nil {
		return nil, err
	}
	return tenant, nil
}

// List 列出租户。
func (s *TenantService) List() ([]model.Tenant, error) {
	return s.store.List()
}

// GetByID 获取租户。
func (s *TenantService) GetByID(id string) (*model.Tenant, error) {
	return s.store.GetByID(id)
}

// UpdateTenantRequest 更新租户请求参数。
type UpdateTenantRequest struct {
	Name   *string            `json:"name,omitempty"`
	Status *model.TenantStatus `json:"status,omitempty"`
}

// Update 更新租户。
func (s *TenantService) Update(id string, req UpdateTenantRequest) (*model.Tenant, error) {
	tenant, err := s.store.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("tenant not found: %w", err)
	}

	if req.Name != nil {
		if *req.Name == "" || len(*req.Name) > 100 {
			return nil, fmt.Errorf("name: must be 1-100 characters")
		}
		tenant.Name = *req.Name
	}
	if req.Status != nil {
		if *req.Status != model.TenantStatusActive && *req.Status != model.TenantStatusDisabled {
			return nil, fmt.Errorf("status: must be 'active' or 'disabled'")
		}
		tenant.Status = *req.Status
	}

	if err := s.store.Update(tenant); err != nil {
		return nil, fmt.Errorf("update tenant: %w", err)
	}
	return tenant, nil
}

// Delete 删除租户。
func (s *TenantService) Delete(id string) error {
	if _, err := s.store.GetByID(id); err != nil {
		return fmt.Errorf("tenant not found: %w", err)
	}
	return s.store.Delete(id)
}
