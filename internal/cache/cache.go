// Package cache 提供简单的内存 TTL 缓存，用于减少高频 DB 查询。
package cache

import (
	"sync"
	"time"
)

// entry 缓存条目。
type entry[T any] struct {
	value    T
	expireAt time.Time
}

// Cache 泛型 TTL 缓存，支持惰性过期和定期清理。
type Cache[T any] struct {
	mu       sync.RWMutex
	items    map[string]*entry[T]
	ttl      time.Duration
	stopCh   chan struct{}
	stopOnce sync.Once
}

// New 创建缓存实例，ttl 为条目过期时间。
func New[T any](ttl time.Duration) *Cache[T] {
	c := &Cache[T]{
		items:  make(map[string]*entry[T]),
		ttl:    ttl,
		stopCh: make(chan struct{}),
	}
	go c.cleanupLoop()
	return c
}

// Get 获取缓存值，若不存在或已过期返回零值和 false。
func (c *Cache[T]) Get(key string) (T, bool) {
	c.mu.RLock()
	e, ok := c.items[key]
	c.mu.RUnlock()
	if !ok || time.Now().After(e.expireAt) {
		var zero T
		return zero, false
	}
	return e.value, true
}

// Set 设置缓存值。
func (c *Cache[T]) Set(key string, value T) {
	c.mu.Lock()
	c.items[key] = &entry[T]{value: value, expireAt: time.Now().Add(c.ttl)}
	c.mu.Unlock()
}

// Delete 删除缓存条目（用于更新/删除时的主动失效）。
func (c *Cache[T]) Delete(key string) {
	c.mu.Lock()
	delete(c.items, key)
	c.mu.Unlock()
}

// Stop 停止后台清理 goroutine。
func (c *Cache[T]) Stop() {
	c.stopOnce.Do(func() {
		close(c.stopCh)
	})
}

// cleanupLoop 定期清理过期条目。
func (c *Cache[T]) cleanupLoop() {
	ticker := time.NewTicker(c.ttl)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.cleanup()
		case <-c.stopCh:
			return
		}
	}
}

// cleanup 删除所有过期条目。
func (c *Cache[T]) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, e := range c.items {
		if now.After(e.expireAt) {
			delete(c.items, k)
		}
	}
}
