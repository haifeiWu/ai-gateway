package repository

import (
	"github.com/haifeiWu/ai-gateway/internal/model"
	"gorm.io/gorm"
)

// TenantStore 租户数据访问。
type TenantStore struct {
	db *gorm.DB
}

// NewTenantStore 创建租户存储实例。
func NewTenantStore(db *gorm.DB) *TenantStore {
	return &TenantStore{db: db}
}

// Create 创建租户。
func (s *TenantStore) Create(t *model.Tenant) error {
	return s.db.Create(t).Error
}

// List 列出租户，按创建时间降序。
func (s *TenantStore) List() ([]model.Tenant, error) {
	var tenants []model.Tenant
	err := s.db.Order("created_at DESC").Find(&tenants).Error
	return tenants, err
}

// GetByID 根据 ID 获取租户。
func (s *TenantStore) GetByID(id string) (*model.Tenant, error) {
	var t model.Tenant
	err := s.db.Where("id = ?", id).First(&t).Error
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// Update 更新租户的部分字段（name, status）。
func (s *TenantStore) Update(t *model.Tenant) error {
	return s.db.Model(t).Select("name", "status").Updates(t).Error
}

// Delete 删除租户及其关联的所有 API Key（硬删除，在事务中执行）。
func (s *TenantStore) Delete(id string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&model.APIKey{}, "tenant_id = ?", id).Error; err != nil {
			return err
		}
		if err := tx.Delete(&model.Tenant{}, "id = ?", id).Error; err != nil {
			return err
		}
		return nil
	})
}
