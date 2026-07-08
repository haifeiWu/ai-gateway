package service

import (
	"sync"
	"testing"
	"time"

	"github.com/haifeiWu/ai-gateway/internal/model"
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

func TestUsageWriter_Record(t *testing.T) {
	tests := []struct {
		name      string
		recordCnt int
		wantMin   int // 最少期望记录数（批量刷盘可能触发部分写入）
		wantExact int // 精确期望记录数（Shutdown 后）
	}{
		{
			name:      "少量记录_Shutdown后精确",
			recordCnt: 5,
			wantMin:   0,
			wantExact: 5,
		},
		{
			name:      "超过批量阈值_触发批量刷盘",
			recordCnt: 150,
			wantMin:   100,
			wantExact: 150,
		},
		{
			name:      "中等数量_Shutdown排空",
			recordCnt: 50,
			wantMin:   0,
			wantExact: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &mockUsageStore{}
			w := NewUsageWriter(store)

			for i := 0; i < tt.recordCnt; i++ {
				w.Record(&model.UsageRecord{
					ID:          "r",
					TenantID:    "t1",
					KeyID:       "k1",
					Model:       "gpt-4",
					TotalTokens: 100,
				})
			}

			if tt.wantMin > 0 {
				time.Sleep(100 * time.Millisecond)
				if count := store.count(); count < tt.wantMin {
					t.Errorf("应至少刷入一批: got %d, want >= %d", count, tt.wantMin)
				}
			}

			w.Shutdown()

			if count := store.count(); count != tt.wantExact {
				t.Errorf("Shutdown 后记录数 = %d, want %d", count, tt.wantExact)
			}
		})
	}
}

func TestUsageWriter_Shutdown(t *testing.T) {
	tests := []struct {
		name    string
		records int
	}{
		{
			name:    "空记录_Shutdown不报错",
			records: 0,
		},
		{
			name:    "重复Shutdown_不panic",
			records: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &mockUsageStore{}
			w := NewUsageWriter(store)

			for i := 0; i < tt.records; i++ {
				w.Record(&model.UsageRecord{
					ID:       "r",
					TenantID: "t1",
					KeyID:    "k1",
					Model:    "gpt-4",
				})
			}

			w.Shutdown()
			// 第二次调用不应 panic
			w.Shutdown()

			if tt.records == 0 {
				if count := store.count(); count != 0 {
					t.Errorf("无记录时 count = %d, want 0", count)
				}
			}
		})
	}
}
