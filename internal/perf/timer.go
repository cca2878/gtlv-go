// Package perf 提供轻量级性能计时器，用于开发调试阶段耗时统计。
package perf

import (
	"sync"
	"time"
)

// Timer 是一个轻量级阶段耗时统计器。
// 支持嵌套计时、多阶段统计，开发模式下输出详细耗时，生产模式可关闭。
type Timer struct {
	mu      sync.Mutex
	stages  map[string]time.Duration
	starts  map[string]time.Time
	enabled bool
}

// New 创建一个新的计时器。enabled 控制是否记录耗时（生产模式可传 false）。
func New(enabled bool) *Timer {
	return &Timer{
		stages:  make(map[string]time.Duration),
		starts:  make(map[string]time.Time),
		enabled: enabled,
	}
}

// Start 开始一个阶段的计时。如果计时器未启用，此操作为空操作。
func (t *Timer) Start(name string) {
	if !t.enabled {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.starts[name] = time.Now()
}

// Stop 结束一个阶段的计时并记录耗时。如果计时器未启用或阶段未开始，此操作为空操作。
func (t *Timer) Stop(name string) {
	if !t.enabled {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if start, ok := t.starts[name]; ok {
		t.stages[name] = time.Since(start)
		delete(t.starts, name)
	}
}

// Elapsed 返回指定阶段的耗时（毫秒）。如果阶段不存在，返回 0。
func (t *Timer) Elapsed(name string) int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	if d, ok := t.stages[name]; ok {
		return d.Milliseconds()
	}
	return 0
}
