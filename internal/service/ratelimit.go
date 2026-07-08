package service

import (
	"sync"
	"time"
)

// RateLimiter 基于 key_id 的内存固定窗口限流器。
type RateLimiter struct {
	mu      sync.Mutex
	windows map[string]*rateWindow
	closed  chan struct{}
}

type rateWindow struct {
	counter int
	resetAt time.Time
}

// NewRateLimiter 创建限流器并启动定时清理 goroutine。
func NewRateLimiter() *RateLimiter {
	rl := &RateLimiter{
		windows: make(map[string]*rateWindow),
		closed:  make(chan struct{}),
	}
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				rl.CleanupExpired()
			case <-rl.closed:
				return
			}
		}
	}()
	return rl
}

// Close 停止后台清理 goroutine。
func (rl *RateLimiter) Close() {
	close(rl.closed)
}

// Allow 检查 key_id 是否允许通过。rpm 为 0 表示不限流。
func (rl *RateLimiter) Allow(keyID string, rpm int) bool {
	if rpm <= 0 {
		return true
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	w, ok := rl.windows[keyID]
	if !ok || now.After(w.resetAt) {
		rl.windows[keyID] = &rateWindow{counter: 1, resetAt: now.Add(time.Minute)}
		return true
	}

	if w.counter < rpm {
		w.counter++
		return true
	}
	return false
}

// CleanupExpired 清理过期窗口，可定期调用防止内存泄漏。
func (rl *RateLimiter) CleanupExpired() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for k, w := range rl.windows {
		if now.After(w.resetAt) {
			delete(rl.windows, k)
		}
	}
}

// Reset 清除所有限流窗口。
func (rl *RateLimiter) Reset() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.windows = make(map[string]*rateWindow)
}
