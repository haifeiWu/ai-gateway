package service

import (
	"sync"
	"testing"
	"time"

	"github.com/ai-gateway/internal/model"
)

// mockUsageStore 模拟用量存储。
type mockUsageStore struct {
	mu      sync.Mutex
	records []*model.UsageRecord
}

func (m *mockUsageStore) BatchCreate(records []*model.UsageRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, records...)
	return nil
}

func (m *mockUsageStore) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.records)
}

func TestUsageWriter_Record_异步写入(t *testing.T) {
	store := &mockUsageStore{}
	w := NewUsageWriter(store)
	defer w.Shutdown()

	for i := 0; i < 5; i++ {
		w.Record(&model.UsageRecord{
			ID:        "r-" + string(rune('0'+i)),
			TenantID:  "t1",
			KeyID:     "k1",
			Model:     "gpt-4",
			TotalTokens: 100,
		})
	}

	// 通过 Shutdown 触发刷盘
	w.Shutdown()

	if count := store.count(); count != 5 {
		t.Errorf("记录数 = %d, want 5", count)
	}
}

func TestUsageWriter_Record_批量刷盘(t *testing.T) {
	store := &mockUsageStore{}
	w := NewUsageWriter(store)
	defer w.Shutdown()

	// 写入超过 batchSize (100) 条记录，触发批量刷盘
	for i := 0; i < 150; i++ {
		w.Record(&model.UsageRecord{
			ID:        "r",
			TenantID:  "t1",
			KeyID:     "k1",
			Model:     "gpt-4",
		})
	}

	// 等待异步写入
	time.Sleep(100 * time.Millisecond)

	if count := store.count(); count < 100 {
		t.Errorf("应至少刷入一批: got %d, want >= 100", count)
	}
}

func TestUsageWriter_Shutdown_排空(t *testing.T) {
	store := &mockUsageStore{}
	w := NewUsageWriter(store)

	n := 50
	for i := 0; i < n; i++ {
		w.Record(&model.UsageRecord{
			ID:        "r",
			TenantID:  "t1",
			KeyID:     "k1",
			Model:     "gpt-4",
		})
	}

	w.Shutdown()

	if count := store.count(); count != n {
		t.Errorf("Shutdown 后记录数 = %d, want %d", count, n)
	}
}

func TestUsageWriter_Shutdown_幂等(t *testing.T) {
	store := &mockUsageStore{}
	w := NewUsageWriter(store)

	w.Shutdown()
	// 第二次调用不应 panic
	w.Shutdown()
}

func TestUsageWriter_Record_空记录(t *testing.T) {
	store := &mockUsageStore{}
	w := NewUsageWriter(store)
	w.Shutdown()

	if count := store.count(); count != 0 {
		t.Errorf("无记录时 count = %d, want 0", count)
	}
}
