package service

import (
	"testing"

	"github.com/ai-gateway/internal/model"
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

func TestTenantService_Create_成功(t *testing.T) {
	store := &mockTenantStore{tenants: make(map[string]*model.Tenant)}
	svc := NewTenantService(store)

	tenant, err := svc.Create("测试租户")
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if tenant.Name != "测试租户" {
		t.Errorf("名称 = %q, want %q", tenant.Name, "测试租户")
	}
	if tenant.Status != model.TenantStatusActive {
		t.Errorf("状态 = %q, want %q", tenant.Status, model.TenantStatusActive)
	}
	if tenant.ID == "" {
		t.Error("ID 不应为空")
	}
}

func TestTenantService_List_成功(t *testing.T) {
	store := &mockTenantStore{tenants: make(map[string]*model.Tenant)}
	svc := NewTenantService(store)

	svc.Create("租户A")
	svc.Create("租户B")

	tenants, err := svc.List()
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if len(tenants) != 2 {
		t.Errorf("租户数 = %d, want 2", len(tenants))
	}
}

func TestTenantService_List_空列表(t *testing.T) {
	store := &mockTenantStore{tenants: make(map[string]*model.Tenant)}
	svc := NewTenantService(store)

	tenants, err := svc.List()
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if tenants == nil {
		t.Error("空列表应返回空切片而非 nil")
	}
}

func TestTenantService_GetByID_成功(t *testing.T) {
	store := &mockTenantStore{tenants: make(map[string]*model.Tenant)}
	svc := NewTenantService(store)

	created, _ := svc.Create("测试租户")

	got, err := svc.GetByID(created.ID)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if got.Name != "测试租户" {
		t.Errorf("名称 = %q, want %q", got.Name, "测试租户")
	}
}

func TestTenantService_GetByID_不存在(t *testing.T) {
	store := &mockTenantStore{tenants: make(map[string]*model.Tenant)}
	svc := NewTenantService(store)

	_, err := svc.GetByID("nonexistent")
	if err != gorm.ErrRecordNotFound {
		t.Errorf("期望 ErrRecordNotFound，got %v", err)
	}
}

func TestTenantService_Update_成功(t *testing.T) {
	store := &mockTenantStore{tenants: make(map[string]*model.Tenant)}
	svc := NewTenantService(store)

	created, _ := svc.Create("原始名称")

	t.Run("更新名称", func(t *testing.T) {
		name := "新名称"
		updated, err := svc.Update(created.ID, UpdateTenantRequest{Name: &name})
		if err != nil {
			t.Fatalf("更新失败: %v", err)
		}
		if updated.Name != "新名称" {
			t.Errorf("名称 = %q, want %q", updated.Name, "新名称")
		}
	})

	t.Run("禁用租户", func(t *testing.T) {
		status := model.TenantStatusDisabled
		updated, err := svc.Update(created.ID, UpdateTenantRequest{Status: &status})
		if err != nil {
			t.Fatalf("更新失败: %v", err)
		}
		if updated.Status != model.TenantStatusDisabled {
			t.Errorf("状态 = %q, want %q", updated.Status, model.TenantStatusDisabled)
		}
	})

	t.Run("启用租户", func(t *testing.T) {
		status := model.TenantStatusActive
		updated, err := svc.Update(created.ID, UpdateTenantRequest{Status: &status})
		if err != nil {
			t.Fatalf("更新失败: %v", err)
		}
		if updated.Status != model.TenantStatusActive {
			t.Errorf("状态 = %q, want %q", updated.Status, model.TenantStatusActive)
		}
	})
}

func TestTenantService_Update_不存在(t *testing.T) {
	store := &mockTenantStore{tenants: make(map[string]*model.Tenant)}
	svc := NewTenantService(store)

	_, err := svc.Update("nonexistent", UpdateTenantRequest{})
	if err == nil {
		t.Error("更新不存在的租户应返回错误")
	}
}

func TestTenantService_Update_无效名称(t *testing.T) {
	store := &mockTenantStore{tenants: make(map[string]*model.Tenant)}
	svc := NewTenantService(store)

	created, _ := svc.Create("test")

	t.Run("空名称", func(t *testing.T) {
		name := ""
		_, err := svc.Update(created.ID, UpdateTenantRequest{Name: &name})
		if err == nil {
			t.Error("空名称应返回错误")
		}
	})

	t.Run("超长名称", func(t *testing.T) {
		name := string(make([]byte, 101))
		_, err := svc.Update(created.ID, UpdateTenantRequest{Name: &name})
		if err == nil {
			t.Error("超长名称应返回错误")
		}
	})
}

func TestTenantService_Update_无效状态(t *testing.T) {
	store := &mockTenantStore{tenants: make(map[string]*model.Tenant)}
	svc := NewTenantService(store)

	created, _ := svc.Create("test")

	status := model.TenantStatus("invalid")
	_, err := svc.Update(created.ID, UpdateTenantRequest{Status: &status})
	if err == nil {
		t.Error("无效状态应返回错误")
	}
}

func TestTenantService_Delete_成功(t *testing.T) {
	store := &mockTenantStore{tenants: make(map[string]*model.Tenant)}
	svc := NewTenantService(store)

	created, _ := svc.Create("待删除")

	err := svc.Delete(created.ID)
	if err != nil {
		t.Fatalf("删除失败: %v", err)
	}

	// 验证已删除
	_, err = svc.GetByID(created.ID)
	if err != gorm.ErrRecordNotFound {
		t.Errorf("删除后应查询不到: %v", err)
	}
}

func TestTenantService_Delete_不存在(t *testing.T) {
	store := &mockTenantStore{tenants: make(map[string]*model.Tenant)}
	svc := NewTenantService(store)

	err := svc.Delete("nonexistent")
	if err == nil {
		t.Error("删除不存在的租户应返回错误")
	}
}
