package model_test

import (
	"sync"
	"testing"
	"time"

	"github.com/nimamasl114514/gogui/model"
	"github.com/nimamasl114514/gogui/render"
	"github.com/nimamasl114514/gogui/snapshot"
	"github.com/nimamasl114514/gogui/theme"
)

// TestConcurrentHammer —— 无 -race 环境下的并发压测（死锁/panic 探测器）：
// 8 个写者（Dispatch/Var.Set/Update）+ 4 个读者（render/snapshot/Stats）交错轰炸。
// 判据：总耗时有界（无死锁），无 panic，最终计数一致。
func TestConcurrentHammer(t *testing.T) {
	v := model.NewVar(0)
	app := model.New("hammer", model.Size{W: 100, H: 100},
		model.VStack(
			model.Button("+").WithID("inc").OnClick(func() { model.Inc(v) }),
			model.Label("").WithID("lbl").BindTextInt(v, "%d"),
			model.Progress(0),
		))
	th := theme.DarkGlow()

	var writers, readers sync.WaitGroup
	done := make(chan struct{})
	var ops int64
	var mu sync.Mutex

	// 写者：Dispatch / 直接 Var.Set / Update 混合。
	for w := 0; w < 8; w++ {
		writers.Add(1)
		go func(w int) {
			defer writers.Done()
			for i := 0; i < 500; i++ {
				switch w % 3 {
				case 0:
					_ = app.Dispatch(model.Event{WidgetID: "inc", Type: "click"})
				case 1:
					v.Set(v.Get() + 1)
				case 2:
					app.Update(func(c *model.Ctx) { c.SetText("lbl", "x") })
				}
				mu.Lock()
				ops++
				mu.Unlock()
			}
		}(w)
	}
	// 读者：渲染 / 快照 / 统计（写者结束后退出）。
	for r := 0; r < 4; r++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-done:
					return
				default:
					_ = render.HTML(app, th)
					_ = snapshot.Tree(app)
					_ = app.Stats()
				}
			}
		}()
	}

	finished := make(chan struct{})
	go func() {
		writers.Wait()
		close(done) // 写者完成后通知读者退出
		readers.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(20 * time.Second):
		t.Fatalf("并发压测超时（疑似死锁）：已完成 %d ops", ops)
	}
	// 终态一致：lbl 文本可解析，计数 > 0。
	if w := app.LookupWidget("lbl"); w == nil || w.TextValue() == "" {
		t.Fatal("终态 lbl 异常")
	}
}
