package service

import (
	"testing"
	"time"

	"github.com/haifeiWu/ai-gateway/internal/model"
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

func TestAPIKeyService_Create(t *testing.T) {
	expires := time.Now().Add(30 * 24 * time.Hour)

	tests := []struct {
		name    string
		tenants map[string]*model.Tenant
		req     CreateKeyRequest
		wantErr bool
		check   func(t *testing.T, resp *CreateKeyResult, ks *mockKeyStore)
	}{
		{
			name: "成功创建_完整参数",
			tenants: map[string]*model.Tenant{
				"t1": {ID: "t1", Name: "Test", Status: model.TenantStatusActive},
			},
			req: CreateKeyRequest{
				Name: "test-key",
				Scopes: model.Scopes{
					AllowedModels:    []string{"gpt-4"},
					AllowedEndpoints: []string{"/v1/chat/completions"},
					RateLimitRPM:     100,
				},
				ExpiresAt: &expires,
			},
			wantErr: false,
			check: func(t *testing.T, resp *CreateKeyResult, ks *mockKeyStore) {
				if resp.Key == "" {
					t.Error("明文 Key 不应为空")
				}
				if len(resp.Key) != 39 { // "sk-agw-"(7) + 32 hex chars
					t.Errorf("Key 长度 = %d, want 39", len(resp.Key))
				}
				if resp.Key[:7] != "sk-agw-" {
					t.Errorf("Key 前缀错误: %q", resp.Key[:7])
				}
				if resp.KeyPrefix == "" {
					t.Error("KeyPrefix 不应为空")
				}
				if resp.Name != "test-key" {
					t.Errorf("名称 = %q, want %q", resp.Name, "test-key")
				}

				stored, err := ks.GetByHash(HashKey(resp.Key))
				if err != nil {
					t.Fatalf("通过 hash 查询失败: %v", err)
				}
				if stored.Name != "test-key" {
					t.Errorf("存储的名称 = %q, want test-key", stored.Name)
				}
			},
		},
		{
			name: "租户不存在_返回错误",
			tenants: map[string]*model.Tenant{},
			req: CreateKeyRequest{
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "无限期Key_过期时间为nil",
			tenants: map[string]*model.Tenant{
				"t1": {ID: "t1", Name: "Test", Status: model.TenantStatusActive},
			},
			req: CreateKeyRequest{
				Name:      "forever-key",
				ExpiresAt: nil,
			},
			wantErr: false,
			check: func(t *testing.T, resp *CreateKeyResult, ks *mockKeyStore) {
				if resp.ExpiresAt != nil {
					t.Error("未设过期时间时应为 nil")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keyStore := &mockKeyStore{keys: make(map[string]*model.APIKey)}
			tenantStore := &mockTenantStore{tenants: tt.tenants}
			svc := NewAPIKeyService(keyStore, tenantStore)

			resp, err := svc.Create("t1", tt.req)
			if tt.wantErr {
				if err == nil {
					t.Error("期望返回错误但未返回")
				}
				return
			}
			if err != nil {
				t.Fatalf("不期望错误: %v", err)
			}
			if tt.check != nil {
				tt.check(t, resp, keyStore)
			}
		})
	}
}

func TestAPIKeyService_Update(t *testing.T) {
	statusDisabled := model.KeyStatusDisabled
	nameRenamed := "renamed"
	scopesUpdated := model.Scopes{
		AllowedModels: []string{"gpt-3.5-turbo"},
		RateLimitRPM:  50,
	}
	expiresFuture := time.Now().Add(365 * 24 * time.Hour)

	tests := []struct {
		name    string
		update  UpdateRequest
		wantErr bool
		check   func(t *testing.T, updated *model.APIKey)
	}{
		{
			name: "更新状态_成功",
			update: UpdateRequest{
				Status: &statusDisabled,
			},
			check: func(t *testing.T, k *model.APIKey) {
				if k.Status != model.KeyStatusDisabled {
					t.Errorf("状态 = %q, want %q", k.Status, model.KeyStatusDisabled)
				}
			},
		},
		{
			name: "更新名称_成功",
			update: UpdateRequest{
				Name: &nameRenamed,
			},
			check: func(t *testing.T, k *model.APIKey) {
				if k.Name != "renamed" {
					t.Errorf("名称 = %q, want %q", k.Name, "renamed")
				}
			},
		},
		{
			name: "更新Scopes_成功",
			update: UpdateRequest{
				Scopes: &scopesUpdated,
			},
			check: func(t *testing.T, k *model.APIKey) {
				if k.Scopes.RateLimitRPM != 50 {
					t.Errorf("RateLimitRPM = %d, want 50", k.Scopes.RateLimitRPM)
				}
			},
		},
		{
			name: "更新过期时间_成功",
			update: UpdateRequest{
				ExpiresAt: &expiresFuture,
			},
			check: func(t *testing.T, k *model.APIKey) {
				if k.ExpiresAt == nil {
					t.Error("过期时间不应为 nil")
				}
			},
		},
		{
			name: "更新不存在的Key_返回错误",
			update: UpdateRequest{
				Name: &nameRenamed,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keyStore := &mockKeyStore{keys: make(map[string]*model.APIKey)}
			tenantStore := &mockTenantStore{tenants: map[string]*model.Tenant{
				"t1": {ID: "t1", Name: "Test", Status: model.TenantStatusActive},
			}}
			svc := NewAPIKeyService(keyStore, tenantStore)

			result, err := svc.Create("t1", CreateKeyRequest{Name: "original"})
			if err != nil {
				t.Fatalf("创建 Key 失败: %v", err)
			}

			var targetID string
			if tt.wantErr {
				targetID = "nonexistent"
			} else {
				targetID = result.ID
			}

			updated, err := svc.Update(targetID, tt.update)
			if tt.wantErr {
				if err == nil {
					t.Error("期望返回错误但未返回")
				}
				return
			}
			if err != nil {
				t.Fatalf("更新失败: %v", err)
			}
			if tt.check != nil {
				tt.check(t, updated)
			}
		})
	}
}

func TestAPIKeyService_Delete(t *testing.T) {
	tests := []struct {
		name   string
		verify func(t *testing.T, svc *APIKeyService, keyID string)
	}{
		{
			name: "删除成功",
			verify: func(t *testing.T, svc *APIKeyService, keyID string) {
				err := svc.Delete(keyID)
				if err != nil {
					t.Fatalf("删除失败: %v", err)
				}
				// 验证已删除
				_, err = svc.GetByID(keyID)
				if err != gorm.ErrRecordNotFound {
					t.Errorf("删除后应查询不到: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keyStore := &mockKeyStore{keys: make(map[string]*model.APIKey)}
			tenantStore := &mockTenantStore{tenants: map[string]*model.Tenant{
				"t1": {ID: "t1", Name: "Test", Status: model.TenantStatusActive},
			}}
			svc := NewAPIKeyService(keyStore, tenantStore)

			result, err := svc.Create("t1", CreateKeyRequest{Name: "to-delete"})
			if err != nil {
				t.Fatalf("创建 Key 失败: %v", err)
			}

			tt.verify(t, svc, result.ID)
		})
	}
}

func TestAPIKeyService_ListByTenant(t *testing.T) {
	tests := []struct {
		name       string
		createKeys []struct {
			tenantID string
			name     string
		}
		queryTenant string
		wantCount   int
	}{
		{
			name: "按租户筛选_返回对应Key",
			createKeys: []struct {
				tenantID string
				name     string
			}{
				{"t1", "key-1"},
				{"t1", "key-2"},
				{"t2", "key-3"},
			},
			queryTenant: "t1",
			wantCount:   2,
		},
		{
			name: "无Key的租户_返回空列表",
			createKeys: []struct {
				tenantID string
				name     string
			}{
				{"t1", "key-1"},
			},
			queryTenant: "t2",
			wantCount:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keyStore := &mockKeyStore{keys: make(map[string]*model.APIKey)}
			tenantStore := &mockTenantStore{tenants: map[string]*model.Tenant{
				"t1": {ID: "t1", Name: "A", Status: model.TenantStatusActive},
				"t2": {ID: "t2", Name: "B", Status: model.TenantStatusActive},
			}}
			svc := NewAPIKeyService(keyStore, tenantStore)

			for _, k := range tt.createKeys {
				if _, err := svc.Create(k.tenantID, CreateKeyRequest{Name: k.name}); err != nil {
					t.Fatalf("创建 Key 失败: %v", err)
				}
			}

			keys, err := svc.ListByTenant(tt.queryTenant)
			if err != nil {
				t.Fatalf("列表失败: %v", err)
			}
			if len(keys) != tt.wantCount {
				t.Errorf("%s 的 Key 数 = %d, want %d", tt.queryTenant, len(keys), tt.wantCount)
			}
		})
	}
}

func TestHashKey(t *testing.T) {
	tests := []struct {
		name      string
		input1    string
		input2    string
		wantEqual bool
		wantLen   int
	}{
		{
			name:      "相同输入产生相同hash",
			input1:    "sk-agw-test-key-12345678",
			input2:    "sk-agw-test-key-12345678",
			wantEqual: true,
			wantLen:   64,
		},
		{
			name:      "不同输入产生不同hash",
			input1:    "key-a",
			input2:    "key-b",
			wantEqual: false,
			wantLen:   64,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h1 := HashKey(tt.input1)
			h2 := HashKey(tt.input2)

			if tt.wantEqual && h1 != h2 {
				t.Error("相同输入应产生相同的 hash")
			}
			if !tt.wantEqual && h1 == h2 {
				t.Error("不同输入应产生不同的 hash")
			}
			if len(h1) != tt.wantLen {
				t.Errorf("hash 长度 = %d, want %d", len(h1), tt.wantLen)
			}
		})
	}
}
