package service

import (
	"sync"
	"testing"
)

func TestRateLimiter_Allow(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		rpm       int
		preCalls  int
		wantAllow bool
	}{
		{
			name:      "零RPM不限流_多次调用",
			key:       "key-zero",
			rpm:       0,
			preCalls:  999,
			wantAllow: true,
		},
		{
			name:      "未达限制_允许",
			key:       "key-under",
			rpm:       5,
			preCalls:  4,
			wantAllow: true,
		},
		{
			name:      "达到限制_拒绝",
			key:       "key-at-limit",
			rpm:       3,
			preCalls:  3,
			wantAllow: false,
		},
		{
			name:      "超出限制_拒绝",
			key:       "key-over",
			rpm:       1,
			preCalls:  1,
			wantAllow: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rl := NewRateLimiter()
			for i := 0; i < tt.preCalls; i++ {
				if !rl.Allow(tt.key, tt.rpm) {
					t.Fatalf("第 %d 次调用应允许但被拒绝", i+1)
				}
			}
			got := rl.Allow(tt.key, tt.rpm)
			if got != tt.wantAllow {
				t.Errorf("Allow() = %v, want %v", got, tt.wantAllow)
			}
		})
	}
}

func TestRateLimiter_Allow_multipleKeys(t *testing.T) {
	rl := NewRateLimiter()
	rpm := 5

	// Key A 用完额度
	for i := 0; i < rpm; i++ {
		if !rl.Allow("key-a", rpm) {
			t.Fatalf("key-a 第 %d 次应允许", i+1)
		}
	}

	// Key A 应被拒绝
	if rl.Allow("key-a", rpm) {
		t.Error("key-a 应被限流")
	}

	// Key B 应不受影响
	for i := 0; i < rpm; i++ {
		if !rl.Allow("key-b", rpm) {
			t.Errorf("key-b 第 %d 次不应受 key-a 限流影响", i+1)
		}
	}
}

func TestRateLimiter_CleanupExpired(t *testing.T) {
	rl := NewRateLimiter()

	// 写入一个窗口
	rl.Allow("key-x", 5)

	// 手动将窗口的 counter 设为已达限制
	rl.mu.Lock()
	for _, w := range rl.windows {
		w.counter = 100
	}
	rl.mu.Unlock()

	// 清理（此时不应清理，因为 resetAt 在未来）
	rl.CleanupExpired()
	rl.mu.Lock()
	if len(rl.windows) != 1 {
		t.Errorf("未过期的窗口应保留: got %d, want 1", len(rl.windows))
	}
	rl.mu.Unlock()
}

func TestRateLimiter_Concurrent(t *testing.T) {
	rl := NewRateLimiter()
	const rpm = 100
	const keyID = "key-concurrent"

	var wg sync.WaitGroup
	allowed := make(chan bool, rpm+50)

	// 并发 150 个请求
	for i := 0; i < 150; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowed <- rl.Allow(keyID, rpm)
		}()
	}
	wg.Wait()
	close(allowed)

	count := 0
	for a := range allowed {
		if a {
			count++
		}
	}

	if count > rpm {
		t.Errorf("并发场景下允许 %d 次，超出 rpm=%d", count, rpm)
	}
	if count < rpm-5 {
		t.Errorf("允许次数 %d 远低于 rpm=%d，可能存在并发问题", count, rpm)
	}
}
