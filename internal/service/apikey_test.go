package service

import (
	"testing"
	"time"

	"github.com/ai-gateway/internal/model"
	"gorm.io/gorm"
)

// mockKeyStore 模拟 Key 存储。
type mockKeyStore struct {
	keys map[string]*model.APIKey
	err  error
}

func (m *mockKeyStore) Create(k *model.APIKey) error {
	if m.err != nil {
		return m.err
	}
	m.keys[k.ID] = k
	return nil
}

func (m *mockKeyStore) GetByHash(hash string) (*model.APIKey, error) {
	if m.err != nil {
		return nil, m.err
	}
	for _, k := range m.keys {
		if k.KeyHash == hash {
			return k, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (m *mockKeyStore) GetByID(id string) (*model.APIKey, error) {
	if m.err != nil {
		return nil, m.err
	}
	k, ok := m.keys[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return k, nil
}

func (m *mockKeyStore) ListByTenant(tenantID string) ([]model.APIKey, error) {
	if m.err != nil {
		return nil, m.err
	}
	var result []model.APIKey
	for _, k := range m.keys {
		if k.TenantID == tenantID {
			result = append(result, *k)
		}
	}
	if result == nil {
		result = []model.APIKey{}
	}
	return result, nil
}

func (m *mockKeyStore) Update(k *model.APIKey) error {
	if m.err != nil {
		return m.err
	}
	m.keys[k.ID] = k
	return nil
}

func (m *mockKeyStore) Delete(id string) error {
	if m.err != nil {
		return m.err
	}
	delete(m.keys, id)
	return nil
}

func TestAPIKeyService_Create_成功(t *testing.T) {
	tenantStore := &mockTenantStore{tenants: map[string]*model.Tenant{
		"t1": {ID: "t1", Name: "Test", Status: model.TenantStatusActive},
	}}
	keyStore := &mockKeyStore{keys: make(map[string]*model.APIKey)}
	svc := NewAPIKeyService(keyStore, tenantStore)

	expires := time.Now().Add(30 * 24 * time.Hour)
	result, err := svc.Create("t1", CreateKeyRequest{
		Name: "test-key",
		Scopes: model.Scopes{
			AllowedModels:    []string{"gpt-4"},
			AllowedEndpoints: []string{"/v1/chat/completions"},
			RateLimitRPM:     100,
		},
		ExpiresAt: &expires,
	})

	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if result.Key == "" {
		t.Error("明文 Key 不应为空")
	}
	if len(result.Key) != 39 { // "sk-agw-"(7) + 32 hex chars
		t.Errorf("Key 长度 = %d, want 39", len(result.Key))
	}
	if result.Key[:7] != "sk-agw-" {
		t.Errorf("Key 前缀错误: %q", result.Key[:7])
	}
	if result.KeyPrefix == "" {
		t.Error("KeyPrefix 不应为空")
	}
	if result.Name != "test-key" {
		t.Errorf("名称 = %q, want %q", result.Name, "test-key")
	}

	// 验证 Key 已存储（hash 方式）
	stored, err := keyStore.GetByHash(HashKey(result.Key))
	if err != nil {
		t.Fatalf("通过 hash 查询失败: %v", err)
	}
	if stored.Name != "test-key" {
		t.Errorf("存储的名称 = %q", stored.Name)
	}
}

func TestAPIKeyService_Create_租户不存在(t *testing.T) {
	tenantStore := &mockTenantStore{tenants: make(map[string]*model.Tenant)}
	keyStore := &mockKeyStore{keys: make(map[string]*model.APIKey)}
	svc := NewAPIKeyService(keyStore, tenantStore)

	_, err := svc.Create("nonexistent", CreateKeyRequest{Name: "test"})
	if err == nil {
		t.Error("租户不存在时应返回错误")
	}
}

func TestAPIKeyService_Create_无限期Key(t *testing.T) {
	tenantStore := &mockTenantStore{tenants: map[string]*model.Tenant{
		"t1": {ID: "t1", Name: "Test", Status: model.TenantStatusActive},
	}}
	keyStore := &mockKeyStore{keys: make(map[string]*model.APIKey)}
	svc := NewAPIKeyService(keyStore, tenantStore)

	result, err := svc.Create("t1", CreateKeyRequest{
		Name:      "forever-key",
		ExpiresAt: nil,
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if result.ExpiresAt != nil {
		t.Error("未设过期时间时应为 nil")
	}
}

func TestAPIKeyService_Update_各字段(t *testing.T) {
	keyStore := &mockKeyStore{keys: make(map[string]*model.APIKey)}
	tenantStore := &mockTenantStore{tenants: map[string]*model.Tenant{
		"t1": {ID: "t1", Name: "Test", Status: model.TenantStatusActive},
	}}
	svc := NewAPIKeyService(keyStore, tenantStore)

	result, _ := svc.Create("t1", CreateKeyRequest{Name: "original"})

	t.Run("更新状态", func(t *testing.T) {
		status := model.KeyStatusDisabled
		updated, err := svc.Update(result.ID, UpdateRequest{Status: &status})
		if err != nil {
			t.Fatalf("更新失败: %v", err)
		}
		if updated.Status != model.KeyStatusDisabled {
			t.Errorf("状态 = %q, want %q", updated.Status, model.KeyStatusDisabled)
		}
	})

	t.Run("更新名称", func(t *testing.T) {
		name := "renamed"
		updated, err := svc.Update(result.ID, UpdateRequest{Name: &name})
		if err != nil {
			t.Fatalf("更新失败: %v", err)
		}
		if updated.Name != "renamed" {
			t.Errorf("名称 = %q, want %q", updated.Name, "renamed")
		}
	})

	t.Run("更新 Scopes", func(t *testing.T) {
		scopes := model.Scopes{
			AllowedModels: []string{"gpt-3.5-turbo"},
			RateLimitRPM:  50,
		}
		updated, err := svc.Update(result.ID, UpdateRequest{Scopes: &scopes})
		if err != nil {
			t.Fatalf("更新失败: %v", err)
		}
		if updated.Scopes.RateLimitRPM != 50 {
			t.Errorf("RateLimitRPM = %d, want 50", updated.Scopes.RateLimitRPM)
		}
	})

	t.Run("更新过期时间", func(t *testing.T) {
		newExp := time.Now().Add(365 * 24 * time.Hour)
		updated, err := svc.Update(result.ID, UpdateRequest{ExpiresAt: &newExp})
		if err != nil {
			t.Fatalf("更新失败: %v", err)
		}
		if updated.ExpiresAt == nil {
			t.Error("过期时间不应为 nil")
		}
	})
}

func TestAPIKeyService_Update_不存在(t *testing.T) {
	keyStore := &mockKeyStore{keys: make(map[string]*model.APIKey)}
	tenantStore := &mockTenantStore{tenants: make(map[string]*model.Tenant)}
	svc := NewAPIKeyService(keyStore, tenantStore)

	_, err := svc.Update("nonexistent", UpdateRequest{})
	if err == nil {
		t.Error("更新不存在的 Key 应返回错误")
	}
}

func TestAPIKeyService_Delete_成功(t *testing.T) {
	keyStore := &mockKeyStore{keys: make(map[string]*model.APIKey)}
	tenantStore := &mockTenantStore{tenants: map[string]*model.Tenant{
		"t1": {ID: "t1", Name: "Test", Status: model.TenantStatusActive},
	}}
	svc := NewAPIKeyService(keyStore, tenantStore)

	result, _ := svc.Create("t1", CreateKeyRequest{Name: "to-delete"})

	err := svc.Delete(result.ID)
	if err != nil {
		t.Fatalf("删除失败: %v", err)
	}

	// 验证已删除
	_, err = svc.GetByID(result.ID)
	if err != gorm.ErrRecordNotFound {
		t.Errorf("删除后应查询不到: %v", err)
	}
}

func TestAPIKeyService_ListByTenant_按租户筛选(t *testing.T) {
	keyStore := &mockKeyStore{keys: make(map[string]*model.APIKey)}
	tenantStore := &mockTenantStore{tenants: map[string]*model.Tenant{
		"t1": {ID: "t1", Name: "A", Status: model.TenantStatusActive},
		"t2": {ID: "t2", Name: "B", Status: model.TenantStatusActive},
	}}
	svc := NewAPIKeyService(keyStore, tenantStore)

	svc.Create("t1", CreateKeyRequest{Name: "key-1"})
	svc.Create("t1", CreateKeyRequest{Name: "key-2"})
	svc.Create("t2", CreateKeyRequest{Name: "key-3"})

	keys, err := svc.ListByTenant("t1")
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("t1 的 Key 数 = %d, want 2", len(keys))
	}

	keys, _ = svc.ListByTenant("t2")
	if len(keys) != 1 {
		t.Errorf("t2 的 Key 数 = %d, want 1", len(keys))
	}
}

func TestHashKey_确定性(t *testing.T) {
	key := "sk-agw-test-key-12345678"

	h1 := HashKey(key)
	h2 := HashKey(key)

	if h1 != h2 {
		t.Error("相同输入应产生相同的 hash")
	}
	if len(h1) != 64 {
		t.Errorf("SHA-256 hash 长度 = %d, want 64", len(h1))
	}
}

func TestHashKey_不同输入(t *testing.T) {
	h1 := HashKey("key-a")
	h2 := HashKey("key-b")
	if h1 == h2 {
		t.Error("不同输入应产生不同的 hash")
	}
}
