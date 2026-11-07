package model

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// statsState 是 App 的运行时计数器（可观测性底座）。
// 热路径只用 atomic（零锁竞争）；快照读取才做一次性的锁与 runtime 采样。
type statsState struct {
	events    atomic.Uint64
	failures  atomic.Uint64
	renders   atomic.Uint64
	lastRender atomic.Int64 // 纳秒
	maxRender  atomic.Int64 // 纳秒
	jsErrors   atomic.Uint64

	mu          sync.Mutex
	lastEvent   Event
	lastEventAt time.Time
	lastErr     string
	createdAt   time.Time
}

// recordEvent 记录一次事件分派（Dispatch 内调用，热路径零分配）。
func (a *App) recordEvent(e Event, err error) {
	a.stats.events.Add(1)
	if err != nil {
		a.stats.failures.Add(1)
	}
	a.stats.mu.Lock()
	a.stats.lastEvent = e
	a.stats.lastEventAt = time.Now()
	if err != nil {
		a.stats.lastErr = err.Error()
	} else {
		a.stats.lastErr = ""
	}
	a.stats.mu.Unlock()
}

// RecordRender 记录一次渲染耗时（presenter 调用；精度受 time.Duration 限制）。
// 建议用 RecordRenderNS + model.NowNS 获得 QPC 级精度。
func (a *App) RecordRender(d time.Duration) { a.RecordRenderNS(d.Nanoseconds()) }

// RecordRenderNS 记录一次渲染耗时（纳秒，QPC 精度）。
func (a *App) RecordRenderNS(ns int64) {
	a.stats.renders.Add(1)
	a.stats.lastRender.Store(ns)
	for {
		cur := a.stats.maxRender.Load()
		if ns <= cur || a.stats.maxRender.CompareAndSwap(cur, ns) {
			break
		}
	}
}

// RecordJSError 记录一次前端 JS 错误（server 收到 /gg/log 时调用）。
func (a *App) RecordJSError() { a.stats.jsErrors.Add(1) }

// AppStats 是 /gg/state 里的应用段快照（JSON 直接序列化）。
type AppStats struct {
	Title        string     `json:"title"`
	Widgets      int        `json:"widgets"`
	Events       uint64     `json:"events_total"`
	Failures     uint64     `json:"events_failed"`
	Renders      uint64     `json:"renders_total"`
	LastRenderNS int64      `json:"render_last_ns"`
	MaxRenderNS  int64      `json:"render_max_ns"`
	JSErrors     uint64     `json:"js_errors_total"`
	LastEvent    *EventInfo `json:"last_event,omitempty"`
	UptimeMS     int64      `json:"uptime_ms"`
	Goroutines   int        `json:"goroutines"`
	HeapKB       uint64     `json:"heap_kb"`
}

// EventInfo 是最近一次事件的摘要。
type EventInfo struct {
	Widget string `json:"widget"`
	Type   string `json:"type"`
	Value  string `json:"value"`
	Err    string `json:"err,omitempty"`
	AgeMS  int64  `json:"age_ms"`
}

// Stats 返回当前计数的快照（线程安全；runtime 采样按需读取）。
// memCache 缓存 ReadMemStats 结果（该调用有 STW 成本；/gg/state 轮询会放大）。
type memCache struct {
	mu        sync.Mutex
	at        time.Time
	heapKB    uint64
	goroutine int
}

func (a *App) readMemCached() (heapKB uint64, goroutines int) {
	a.mem.mu.Lock()
	defer a.mem.mu.Unlock()
	if time.Since(a.mem.at) < time.Second && a.mem.at != (time.Time{}) {
		return a.mem.heapKB, a.mem.goroutine
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	a.mem.at = time.Now()
	a.mem.heapKB = ms.HeapAlloc / 1024
	a.mem.goroutine = runtime.NumGoroutine()
	return a.mem.heapKB, a.mem.goroutine
}

func (a *App) Stats() AppStats {
	heapKB, goroutines := a.readMemCached()

	s := AppStats{
		Title:        a.Title,
		Events:       a.stats.events.Load(),
		Failures:     a.stats.failures.Load(),
		Renders:      a.stats.renders.Load(),
		LastRenderNS: a.stats.lastRender.Load(),
		MaxRenderNS:  a.stats.maxRender.Load(),
		JSErrors:     a.stats.jsErrors.Load(),
		Goroutines:   goroutines,
		HeapKB:       heapKB,
	}
	s.UptimeMS = time.Since(a.stats.createdAt).Milliseconds()

	n := 0
	a.View(func(root *Widget) {
		root.Walk(func(*Widget) bool { n++; return true })
	})
	s.Widgets = n

	a.stats.mu.Lock()
	if !a.stats.lastEventAt.IsZero() {
		s.LastEvent = &EventInfo{
			Widget: a.stats.lastEvent.WidgetID,
			Type:   a.stats.lastEvent.Type,
			Value:  a.stats.lastEvent.Value,
			Err:    a.stats.lastErr,
			AgeMS:  time.Since(a.stats.lastEventAt).Milliseconds(),
		}
	}
	a.stats.mu.Unlock()
	return s
}
