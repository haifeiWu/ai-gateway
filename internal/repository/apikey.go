package repository

import (
	"github.com/ai-gateway/internal/model"
	"gorm.io/gorm"
)

// KeyStore API Key 数据访问。
type KeyStore struct {
	db *gorm.DB
}

// NewKeyStore 创建 Key 存储实例。
func NewKeyStore(db *gorm.DB) *KeyStore {
	return &KeyStore{db: db}
}

// Create 创建 API Key。
func (s *KeyStore) Create(k *model.APIKey) error {
	return s.db.Create(k).Error
}

// GetByHash 根据 hash 获取 Key（鉴权用）。
func (s *KeyStore) GetByHash(hash string) (*model.APIKey, error) {
	var k model.APIKey
	err := s.db.Where("key_hash = ?", hash).First(&k).Error
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// GetByID 根据 ID 获取 Key。
func (s *KeyStore) GetByID(id string) (*model.APIKey, error) {
	var k model.APIKey
	err := s.db.Where("id = ?", id).First(&k).Error
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// ListByTenant 列出指定租户下的 Key。
func (s *KeyStore) ListByTenant(tenantID string) ([]model.APIKey, error) {
	var keys []model.APIKey
	err := s.db.Where("tenant_id = ?", tenantID).Order("created_at DESC").Find(&keys).Error
	return keys, err
}

// Update 更新 Key 的部分字段（status, scopes, expires_at, name）。
func (s *KeyStore) Update(k *model.APIKey) error {
	return s.db.Model(k).Select("status", "scopes", "expires_at", "name").Updates(k).Error
}

// Delete 删除 Key。
func (s *KeyStore) Delete(id string) error {
	return s.db.Delete(&model.APIKey{}, "id = ?", id).Error
}
