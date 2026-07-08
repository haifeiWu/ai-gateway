package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/haifeiWu/ai-gateway/internal/model"
)

// UsageWriterStore 用量写入器存储接口。
type UsageWriterStore interface {
	BatchCreate(records []*model.UsageRecord) error
}

const (
	batchSize     = 100
	flushInterval = 5 * time.Second
)

// UsageWriter 异步批量写入用量记录。
type UsageWriter struct {
	store   UsageWriterStore
	ch      chan *model.UsageRecord
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	closed  bool
	mu      sync.Mutex
}

// NewUsageWriter 创建用量写入器并启动后台 goroutine。
func NewUsageWriter(store UsageWriterStore) *UsageWriter {
	ctx, cancel := context.WithCancel(context.Background())
	w := &UsageWriter{
		store:  store,
		ch:     make(chan *model.UsageRecord, 1000),
		ctx:    ctx,
		cancel: cancel,
	}
	w.wg.Add(1)
	go w.run()
	return w
}

// Record 异步记录一条用量，非阻塞。
func (w *UsageWriter) Record(r *model.UsageRecord) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.mu.Unlock()

	select {
	case w.ch <- r:
	default:
		slog.Warn("usage channel full, dropping record", "request_id", r.RequestID)
	}
}

// Shutdown 优雅关闭，等待 channel 排空后写入剩余数据。可安全多次调用。
func (w *UsageWriter) Shutdown() {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.closed = true
	w.mu.Unlock()

	w.cancel()
	close(w.ch)
	w.wg.Wait()
}

func (w *UsageWriter) run() {
	defer w.wg.Done()

	batch := make([]*model.UsageRecord, 0, batchSize)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := w.store.BatchCreate(batch); err != nil {
			slog.Error("failed to flush usage records", "count", len(batch), "error", err)
		} else {
			slog.Debug("flushed usage records", "count", len(batch))
		}
		batch = batch[:0]
	}

	for {
		select {
		case r, ok := <-w.ch:
			if !ok {
				// Channel 已关闭，刷盘后退出。
				flush()
				return
			}
			batch = append(batch, r)
			if len(batch) >= batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-w.ctx.Done():
			// Drain 剩余 channel 数据后退出。
			for r := range w.ch {
				batch = append(batch, r)
			}
			flush()
			return
		}
	}
}
