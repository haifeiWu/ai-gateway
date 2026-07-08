package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/haifeiWu/ai-gateway/internal/model"
	"github.com/google/uuid"
)

// KeyStore API Key 存储接口。
type KeyStore interface {
	Create(k *model.APIKey) error
	GetByHash(hash string) (*model.APIKey, error)
	GetByID(id string) (*model.APIKey, error)
	ListByTenant(tenantID string) ([]model.APIKey, error)
	Update(k *model.APIKey) error
	Delete(id string) error
}

// APIKeyService API Key 业务逻辑。
type APIKeyService struct {
	store  KeyStore
	tenant TenantStore
}

// NewAPIKeyService 创建 Key 服务。
func NewAPIKeyService(store KeyStore, tenant TenantStore) *APIKeyService {
	return &APIKeyService{store: store, tenant: tenant}
}

// CreateKeyRequest 创建 Key 请求参数。
type CreateKeyRequest struct {
	Name      string       `json:"name"`
	Scopes    model.Scopes `json:"scopes"`
	ExpiresAt *time.Time   `json:"expires_at"`
}

// CreateKeyResult 创建 Key 结果（含明文 key）。
type CreateKeyResult struct {
	ID        string        `json:"id"`
	Key       string        `json:"key"`
	KeyPrefix string        `json:"key_prefix"`
	Name      string        `json:"name"`
	Scopes    model.Scopes  `json:"scopes"`
	Status    model.KeyStatus `json:"status"`
	ExpiresAt *time.Time    `json:"expires_at"`
	CreatedAt time.Time     `json:"created_at"`
}

// Create 创建 API Key，返回明文 key（仅此一次）。
func (s *APIKeyService) Create(tenantID string, req CreateKeyRequest) (*CreateKeyResult, error) {
	// 校验名称
	if req.Name == "" || len(req.Name) > 100 {
		return nil, fmt.Errorf("key name: must be 1-100 characters")
	}

	// 校验 Scopes
	if len(req.Scopes.AllowedModels) > 50 {
		return nil, fmt.Errorf("allowed_models: max 50")
	}
	if len(req.Scopes.AllowedEndpoints) > 20 {
		return nil, fmt.Errorf("allowed_endpoints: max 20")
	}

	// 校验租户存在
	if _, err := s.tenant.GetByID(tenantID); err != nil {
		return nil, fmt.Errorf("tenant not found: %w", err)
	}

	rawKey, err := generateKey()
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	hash := sha256Hex(rawKey)
	prefix := rawKey[:11] + "****"

	key := &model.APIKey{
		ID:        uuid.NewString(),
		TenantID:  tenantID,
		KeyHash:   hash,
		KeyPrefix: prefix,
		Name:      req.Name,
		Scopes:    req.Scopes,
		Status:    model.KeyStatusActive,
		ExpiresAt: req.ExpiresAt,
	}

	if err := s.store.Create(key); err != nil {
		return nil, fmt.Errorf("create key: %w", err)
	}

	return &CreateKeyResult{
		ID:        key.ID,
		Key:       rawKey,
		KeyPrefix: prefix,
		Name:      key.Name,
		Scopes:    key.Scopes,
		Status:    key.Status,
		ExpiresAt: key.ExpiresAt,
		CreatedAt: key.CreatedAt,
	}, nil
}

// UpdateRequest 更新 Key 请求参数。
type UpdateRequest struct {
	Status    *model.KeyStatus `json:"status,omitempty"`
	Scopes    *model.Scopes    `json:"scopes,omitempty"`
	ExpiresAt *time.Time       `json:"expires_at,omitempty"`
	Name      *string          `json:"name,omitempty"`
}

// Update 更新 Key。
func (s *APIKeyService) Update(id string, req UpdateRequest) (*model.APIKey, error) {
	key, err := s.store.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("key not found: %w", err)
	}

	if req.Status != nil {
		key.Status = *req.Status
	}
	if req.Scopes != nil {
		key.Scopes = *req.Scopes
	}
	if req.ExpiresAt != nil {
		key.ExpiresAt = req.ExpiresAt
	}
	if req.Name != nil {
		key.Name = *req.Name
	}

	if err := s.store.Update(key); err != nil {
		return nil, fmt.Errorf("update key: %w", err)
	}
	return key, nil
}

// Delete 删除 Key。
func (s *APIKeyService) Delete(id string) error {
	if _, err := s.store.GetByID(id); err != nil {
		return fmt.Errorf("key not found: %w", err)
	}
	return s.store.Delete(id)
}

// ListByTenant 列出租户的 Key。
func (s *APIKeyService) ListByTenant(tenantID string) ([]model.APIKey, error) {
	return s.store.ListByTenant(tenantID)
}

// GetByID 获取 Key 详情。
func (s *APIKeyService) GetByID(id string) (*model.APIKey, error) {
	return s.store.GetByID(id)
}

// GetByHash 根据 hash 获取 Key（鉴权用）。
func (s *APIKeyService) GetByHash(hash string) (*model.APIKey, error) {
	return s.store.GetByHash(hash)
}

// sha256Hex 计算 SHA-256 并返回 hex 字符串。
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// generateKey 生成 API Key 明文。
func generateKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("crypto/rand read: %w", err)
	}
	return "sk-agw-" + hex.EncodeToString(b)[:32], nil
}

// HashKey 对 API Key 做 SHA-256 哈希。
func HashKey(key string) string {
	return sha256Hex(key)
}
