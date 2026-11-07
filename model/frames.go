package model

import (
	"sync"
	"time"
)

// StartFrames 启动固定帧率驱动器：每帧调用 fn(dt)，整帧的所有变更
// 经 App.Batch 合并为一次渲染 + 一次 SSE 推送（60fps 的基石）。
//
// 用法：
//
//	stop := app.StartFrames(60, func(dt time.Duration) {
//	    phase += dt.Seconds()
//	    v.Progress.Set(0.5 + 0.5*math.Sin(phase*2*math.Pi))
//	})
//	defer stop()
//
// 纪律：fn 里只写 Var / 改状态，不做阻塞 IO；单个 tick 内异常被 recover，
// 不终止帧循环（该帧丢弃，下一帧继续）。
func (a *App) StartFrames(hz int, fn func(dt time.Duration)) (stop func()) {
	if hz <= 0 {
		hz = 60
	}
	if hz > 240 {
		hz = 240 // 上限：更大频率无意义且 time.Second/hz 会退化为 0（NewTicker panic）
	}
	interval := time.Second / time.Duration(hz)
	tick := time.NewTicker(interval)
	done := make(chan struct{})
	var once sync.Once
	var wg sync.WaitGroup
	last := time.Now()
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case t := <-tick.C:
				dt := t.Sub(last)
				last = t
				func() {
					defer func() { _ = recover() }() // 单帧异常不杀帧循环
					a.Batch(func() { fn(dt) })
				}()
			}
		}
	}()
	return func() {
		once.Do(func() { close(done) })
		wg.Wait() // join：确保停止后不再有帧回调执行（避免测试间串扰）
	}
}
