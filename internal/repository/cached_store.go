package repository

import (
	"time"

	"github.com/haifeiWu/ai-gateway/internal/cache"
	"github.com/haifeiWu/ai-gateway/internal/model"
	"gorm.io/gorm"
)

// CachedKeyStore 包装 KeyStore，为 GetByHash 添加 TTL 缓存，减少高频鉴权请求的 DB 查询。
type CachedKeyStore struct {
	*KeyStore
	cache *cache.Cache[*model.APIKey]
}

// NewCachedKeyStore 创建带缓存的 KeyStore。
func NewCachedKeyStore(db *gorm.DB) *CachedKeyStore {
	return &CachedKeyStore{
		KeyStore: NewKeyStore(db),
		cache:    cache.New[*model.APIKey](30 * time.Second), // 30s TTL（纳秒）
	}
}

// GetByHash 先从缓存查，未命中再查 DB 并回填缓存。
func (c *CachedKeyStore) GetByHash(hash string) (*model.APIKey, error) {
	if v, ok := c.cache.Get(hash); ok {
		return v, nil
	}
	key, err := c.KeyStore.GetByHash(hash)
	if err != nil {
		return nil, err
	}
	c.cache.Set(hash, key)
	return key, nil
}

// Update 更新后清除缓存。
func (c *CachedKeyStore) Update(k *model.APIKey) error {
	if err := c.KeyStore.Update(k); err != nil {
		return err
	}
	c.cache.Delete(k.KeyHash)
	return nil
}

// Delete 删除后清除缓存。
func (c *CachedKeyStore) Delete(id string) error {
	// 先查出来以便获取 hash 来清缓存
	key, err := c.KeyStore.GetByID(id)
	if err != nil {
		return err
	}
	if err := c.KeyStore.Delete(id); err != nil {
		return err
	}
	c.cache.Delete(key.KeyHash)
	return nil
}

// CachedTenantStore 包装 TenantStore，为 GetByID 添加 TTL 缓存。
type CachedTenantStore struct {
	*TenantStore
	cache *cache.Cache[*model.Tenant]
}

// NewCachedTenantStore 创建带缓存的 TenantStore。
func NewCachedTenantStore(db *gorm.DB) *CachedTenantStore {
	return &CachedTenantStore{
		TenantStore: NewTenantStore(db),
		cache:       cache.New[*model.Tenant](60 * time.Second), // 60s TTL
	}
}

// GetByID 先从缓存查，未命中再查 DB 并回填缓存。
func (c *CachedTenantStore) GetByID(id string) (*model.Tenant, error) {
	if v, ok := c.cache.Get(id); ok {
		return v, nil
	}
	tenant, err := c.TenantStore.GetByID(id)
	if err != nil {
		return nil, err
	}
	c.cache.Set(id, tenant)
	return tenant, nil
}

// Update 更新后清除缓存。
func (c *CachedTenantStore) Update(t *model.Tenant) error {
	if err := c.TenantStore.Update(t); err != nil {
		return err
	}
	c.cache.Delete(t.ID)
	return nil
}

// Delete 删除后清除缓存。
func (c *CachedTenantStore) Delete(id string) error {
	if err := c.TenantStore.Delete(id); err != nil {
		return err
	}
	c.cache.Delete(id)
	return nil
}
