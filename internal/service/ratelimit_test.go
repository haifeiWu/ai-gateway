package service

import (
	"sync"
	"testing"
)

func TestRateLimiter_Allow_zeroRPM(t *testing.T) {
	rl := NewRateLimiter()

	// rpm=0 表示不限流，始终允许
	for i := 0; i < 1000; i++ {
		if !rl.Allow("key-1", 0) {
			t.Errorf("rpm=0 应始终允许，第 %d 次被拒绝", i)
		}
	}
}

func TestRateLimiter_Allow_basicLimit(t *testing.T) {
	rl := NewRateLimiter()
	keyID := "key-basic"
	rpm := 10

	// 前 10 次应允许
	for i := 0; i < rpm; i++ {
		if !rl.Allow(keyID, rpm) {
			t.Errorf("前 %d 次应允许，第 %d 次被拒绝", rpm, i+1)
		}
	}

	// 第 11 次应拒绝
	if rl.Allow(keyID, rpm) {
		t.Error("超出限制应拒绝")
	}
}

func TestRateLimiter_Allow_multipleKeys(t *testing.T) {
	rl := NewRateLimiter()
	rpm := 5

	// Key A 用完额度
	for i := 0; i < rpm; i++ {
		rl.Allow("key-a", rpm)
	}
	// Key A 应被拒绝
	if rl.Allow("key-a", rpm) {
		t.Error("key-a 应被限流")
	}

	// Key B 应不受影响
	for i := 0; i < rpm; i++ {
		if !rl.Allow("key-b", rpm) {
			t.Error("key-b 不应受 key-a 限流影响")
		}
	}
}

func TestRateLimiter_CleanupExpired(t *testing.T) {
	rl := NewRateLimiter()

	// 写入一个窗口
	rl.Allow("key-x", 5)

	// 手动将窗口的 resetAt 设为过期时间
	rl.mu.Lock()
	for _, w := range rl.windows {
		w.counter = 100 // 模拟已达限制
	}
	rl.mu.Unlock()

	// 清理（此时不应清理，因为 resetAt 在未来）
	rl.CleanupExpired()
	rl.mu.Lock()
	if len(rl.windows) != 1 {
		t.Errorf("未过期的窗口应保留: got %d", len(rl.windows))
	}
	rl.mu.Unlock()
}

func TestRateLimiter_Concurrent(t *testing.T) {
	rl := NewRateLimiter()
	rpm := 100
	keyID := "key-concurrent"

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
