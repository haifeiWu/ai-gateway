package service

import (
	"testing"

	"github.com/haifeiWu/ai-gateway/internal/model"
	"gorm.io/gorm"
)

// mockTenantStore 模拟租户存储。
type mockTenantStore struct {
	tenants map[string]*model.Tenant
	err     error
}

func (m *mockTenantStore) Create(t *model.Tenant) error {
	if m.err != nil {
		return m.err
	}
	m.tenants[t.ID] = t
	return nil
}

func (m *mockTenantStore) List() ([]model.Tenant, error) {
	if m.err != nil {
		return nil, m.err
	}
	var result []model.Tenant
	for _, t := range m.tenants {
		result = append(result, *t)
	}
	if result == nil {
		result = []model.Tenant{}
	}
	return result, nil
}

func (m *mockTenantStore) GetByID(id string) (*model.Tenant, error) {
	if m.err != nil {
		return nil, m.err
	}
	t, ok := m.tenants[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return t, nil
}

func (m *mockTenantStore) Update(t *model.Tenant) error {
	if m.err != nil {
		return m.err
	}
	m.tenants[t.ID] = t
	return nil
}

func (m *mockTenantStore) Delete(id string) error {
	if m.err != nil {
		return m.err
	}
	delete(m.tenants, id)
	return nil
}

func TestTenantService_Create(t *testing.T) {
	tests := []struct {
		name    string
		reqName string
		wantErr bool
		check   func(t *testing.T, tenant *model.Tenant)
	}{
		{
			name:    "创建成功",
			reqName: "测试租户",
			check: func(t *testing.T, tenant *model.Tenant) {
				if tenant.Name != "测试租户" {
					t.Errorf("名称 = %q, want %q", tenant.Name, "测试租户")
				}
				if tenant.Status != model.TenantStatusActive {
					t.Errorf("状态 = %q, want %q", tenant.Status, model.TenantStatusActive)
				}
				if tenant.ID == "" {
					t.Error("ID 不应为空")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &mockTenantStore{tenants: make(map[string]*model.Tenant)}
			svc := NewTenantService(store)

			tenant, err := svc.Create(tt.reqName)
			if tt.wantErr {
				if err == nil {
					t.Error("期望返回错误但未返回")
				}
				return
			}
			if err != nil {
				t.Fatalf("创建失败: %v", err)
			}
			if tt.check != nil {
				tt.check(t, tenant)
			}
		})
	}
}

func TestTenantService_List(t *testing.T) {
	tests := []struct {
		name      string
		preCreate []string // 预创建的租户名列表
		wantCount int
	}{
		{
			name:      "有租户时返回列表",
			preCreate: []string{"租户A", "租户B"},
			wantCount: 2,
		},
		{
			name:      "无租户时返回空切片",
			preCreate: nil,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &mockTenantStore{tenants: make(map[string]*model.Tenant)}
			svc := NewTenantService(store)

			for _, name := range tt.preCreate {
				if _, err := svc.Create(name); err != nil {
					t.Fatalf("预创建失败: %v", err)
				}
			}

			tenants, err := svc.List()
			if err != nil {
				t.Fatalf("列表失败: %v", err)
			}
			if len(tenants) != tt.wantCount {
				t.Errorf("租户数 = %d, want %d", len(tenants), tt.wantCount)
			}
			if tt.wantCount == 0 && tenants == nil {
				t.Error("空列表应返回空切片而非 nil")
			}
		})
	}
}

func TestTenantService_GetByID(t *testing.T) {
	tests := []struct {
		name      string
		queryID   string
		preCreate bool
		wantErr   bool
	}{
		{
			name:      "查询存在的租户",
			queryID:   "",
			preCreate: true,
			wantErr:   false,
		},
		{
			name:      "查询不存在的租户",
			queryID:   "nonexistent",
			preCreate: false,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &mockTenantStore{tenants: make(map[string]*model.Tenant)}
			svc := NewTenantService(store)

			var targetID string
			if tt.preCreate {
				created, err := svc.Create("测试租户")
				if err != nil {
					t.Fatalf("预创建失败: %v", err)
				}
				targetID = created.ID
			} else {
				targetID = tt.queryID
			}

			got, err := svc.GetByID(targetID)
			if tt.wantErr {
				if err != gorm.ErrRecordNotFound {
					t.Errorf("期望 ErrRecordNotFound，got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("查询失败: %v", err)
			}
			if got.Name != "测试租户" {
				t.Errorf("名称 = %q, want %q", got.Name, "测试租户")
			}
		})
	}
}

func TestTenantService_Update(t *testing.T) {
	nameNew := "新名称"
	nameEmpty := ""
	nameTooLong := string(make([]byte, 101))
	statusDisabled := model.TenantStatusDisabled
	statusActive := model.TenantStatusActive
	statusInvalid := model.TenantStatus("invalid")

	tests := []struct {
		name    string
		req     UpdateTenantRequest
		useReal bool // true: 用真实创建的 ID, false: 用 "nonexistent"
		wantErr bool
		check   func(t *testing.T, updated *model.Tenant)
	}{
		{
			name:    "更新名称_成功",
			req:     UpdateTenantRequest{Name: &nameNew},
			useReal: true,
			check: func(t *testing.T, tnt *model.Tenant) {
				if tnt.Name != "新名称" {
					t.Errorf("名称 = %q, want %q", tnt.Name, "新名称")
				}
			},
		},
		{
			name:    "禁用租户",
			req:     UpdateTenantRequest{Status: &statusDisabled},
			useReal: true,
			check: func(t *testing.T, tnt *model.Tenant) {
				if tnt.Status != model.TenantStatusDisabled {
					t.Errorf("状态 = %q, want %q", tnt.Status, model.TenantStatusDisabled)
				}
			},
		},
		{
			name:    "启用租户",
			req:     UpdateTenantRequest{Status: &statusActive},
			useReal: true,
			check: func(t *testing.T, tnt *model.Tenant) {
				if tnt.Status != model.TenantStatusActive {
					t.Errorf("状态 = %q, want %q", tnt.Status, model.TenantStatusActive)
				}
			},
		},
		{
			name:    "更新不存在的租户",
			req:     UpdateTenantRequest{},
			useReal: false,
			wantErr: true,
		},
		{
			name:    "空名称_拒绝",
			req:     UpdateTenantRequest{Name: &nameEmpty},
			useReal: true,
			wantErr: true,
		},
		{
			name:    "超长名称_拒绝",
			req:     UpdateTenantRequest{Name: &nameTooLong},
			useReal: true,
			wantErr: true,
		},
		{
			name:    "无效状态_拒绝",
			req:     UpdateTenantRequest{Status: &statusInvalid},
			useReal: true,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &mockTenantStore{tenants: make(map[string]*model.Tenant)}
			svc := NewTenantService(store)

			created, err := svc.Create("原始名称")
			if err != nil {
				t.Fatalf("预创建失败: %v", err)
			}

			targetID := created.ID
			if !tt.useReal {
				targetID = "nonexistent"
			}

			updated, err := svc.Update(targetID, tt.req)
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

func TestTenantService_Delete(t *testing.T) {
	tests := []struct {
		name    string
		useReal bool
		wantErr bool
	}{
		{
			name:    "删除成功",
			useReal: true,
			wantErr: false,
		},
		{
			name:    "删除不存在的租户",
			useReal: false,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &mockTenantStore{tenants: make(map[string]*model.Tenant)}
			svc := NewTenantService(store)

			created, err := svc.Create("待删除")
			if err != nil {
				t.Fatalf("预创建失败: %v", err)
			}

			targetID := created.ID
			if !tt.useReal {
				targetID = "nonexistent"
			}

			err = svc.Delete(targetID)
			if tt.wantErr {
				if err == nil {
					t.Error("期望返回错误但未返回")
				}
				return
			}
			if err != nil {
				t.Fatalf("删除失败: %v", err)
			}

			// 验证已删除
			_, err = svc.GetByID(created.ID)
			if err != gorm.ErrRecordNotFound {
				t.Errorf("删除后应查询不到: %v", err)
			}
		})
	}
}
