package model_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nimamasl114514/gogui/model"
)

func mustNotHang(t *testing.T, name string, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s 挂死（>%s）——疑似死锁", name, d)
	}
}

// TestDispatchEarlyReturnWindowClosed —— 回归：根为 nil / 控件不存在 的早退
// 必须关闭事件窗口；否则后续 Var.Set 通知永久被吞。
func TestDispatchEarlyReturnWindowClosed(t *testing.T) {
	v := model.NewVar(0)
	app := model.New("w", model.Size{W: 10, H: 10},
		model.VStack(model.Label("").BindTextInt(v)))
	notified := 0
	app.OnUpdate(func() { notified++ })

	_ = app.Dispatch(model.Event{WidgetID: "ghost", Type: "click", Value: ""}) // 早退 1
	v.Set(1)
	if notified != 1 {
		t.Fatalf("早退后窗口泄漏：notified=%d, want 1", notified)
	}

	empty := model.New("e", model.Size{W: 10, H: 10}, nil)
	_ = empty.Dispatch(model.Event{WidgetID: "x", Type: "click"}) // 早退 2（无根）
	// 用带绑定的 app 反向验证：再 Set 一次仍要通知。
	v.Set(2)
	if notified != 2 {
		t.Fatalf("两次早退后通知异常：notified=%d, want 2", notified)
	}
}

// TestUpdatePanicNoDeadlock —— 回归 P0：Update 内 panic 后写锁必须释放，
// 且 panic 不传播（Go 侧回调线程不崩）。
func TestUpdatePanicNoDeadlock(t *testing.T) {
	app := model.New("u", model.Size{W: 10, H: 10},
		model.VStack(model.Label("x").WithID("lbl")))
	app.Update(func(c *model.Ctx) { panic("mutate-boom") }) // 不应把测试打崩
	mustNotHang(t, "Update panic 后 Dispatch", 2*time.Second, func() {
		_ = app.Dispatch(model.Event{WidgetID: "lbl", Type: "click"})
	})
}

// TestHandlerReentryPanicsFailFast —— 回归 P0：处理器内调用 App 只读入口
// 从「静默挂死 5 分钟」变为「立即 panic（被 safeApply 转成 error）」。
func TestHandlerReentryPanicsFailFast(t *testing.T) {
	var app *model.App
	app = model.New("r", model.Size{W: 10, H: 10},
		model.VStack(model.Button("b").WithID("btn").OnClick(func() {
			_ = app.LookupWidget("btn") // 写锁内重入只读入口 → 应 fail-fast panic
		})))
	mustNotHang(t, "处理器重入", 2*time.Second, func() {
		err := app.Dispatch(model.Event{WidgetID: "btn", Type: "click"})
		if err == nil || !strings.Contains(err.Error(), "gogui:") {
			t.Errorf("重入应返回含守门信息的 error，实得: %v", err)
		}
	})
	// 应用仍可用。
	if err := app.Dispatch(model.Event{WidgetID: "btn", Type: "click"}); err == nil {
		t.Log("第二次仍触发 handler（预期同错）")
	}
}

// TestCtxReadsBoundValues —— 回归 P1：处理器内 Ctx 读取必须走绑定解析。
func TestCtxReadsBoundValues(t *testing.T) {
	name := model.NewVar("")
	on := model.NewVar(false)
	gotName, gotOn := "", false
	app := model.New("c", model.Size{W: 10, H: 10},
		model.VStack(
			model.Input().WithID("in").BindValue(name),
			model.Toggle().WithID("sw").BindChecked(on),
			model.Button("读").WithID("btn").OnClick(func(c *model.Ctx) {
				gotName = c.Value("in")
				gotOn = c.Checked("sw")
			}),
		))
	_ = app.Dispatch(model.Event{WidgetID: "in", Type: "change", Value: "gogui"})
	_ = app.Dispatch(model.Event{WidgetID: "sw", Type: "change", Value: "true"})
	_ = app.Dispatch(model.Event{WidgetID: "btn", Type: "click"})
	if gotName != "gogui" || !gotOn {
		t.Fatalf("Ctx 未读到绑定值: name=%q on=%v", gotName, gotOn)
	}
}

// TestSliderBindTextFloatTwoWay —— 回归 P1：Slider 上 BindTextFloat 拖动不得被吞。
func TestSliderBindTextFloatTwoWay(t *testing.T) {
	v := model.NewVar(1.0)
	app := model.New("s", model.Size{W: 10, H: 10},
		model.VStack(model.Slider(1, 10, v).WithID("sld")))
	if err := app.Dispatch(model.Event{WidgetID: "sld", Type: "change", Value: "5"}); err != nil {
		t.Fatal(err)
	}
	if v.Get() != 5 {
		t.Fatalf("slider 拖动未写回: %v", v.Get())
	}
	v.Set(7)
	if w := app.LookupWidget("sld"); w.SliderValue() != 7 {
		t.Fatalf("Var.Set 未反映到滑杆: %v", w.SliderValue())
	}
}

// TestAutoIDAvoidsExplicit —— 回归 P1：自动 ID 不得与显式 ID 撞名。
func TestAutoIDAvoidsExplicit(t *testing.T) {
	app := model.New("a", model.Size{W: 10, H: 10},
		model.VStack(
			model.Label("explicit").WithID("label-1"),
			model.Label("auto1"),
			model.Label("auto2"),
		))
	ids := map[string]int{}
	app.View(func(root *model.Widget) {
		root.Walk(func(w *model.Widget) bool { ids[w.ID]++; return true })
	})
	for id, n := range ids {
		if n > 1 {
			t.Fatalf("ID 撞名: %q ×%d（全部: %v）", id, n, ids)
		}
	}
}

// TestAddCyclePanics —— 回归 P1：父子环必须 panic 而非栈溢出。
func TestAddCyclePanics(t *testing.T) {
	a := model.VStack()
	b := model.VStack()
	a.Add(b)
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("祖先挂到后代下未 panic")
			}
		}()
		b.Add(a)
	}()
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("自环未 panic")
			}
		}()
		a.Add(a)
	}()
}

// TestNotifyReentrantBounded —— 回归 P1：订阅者内 Set 触发重入通知应被合并，
// 不得无限递归（此前实测 10 万层）。
func TestNotifyReentrantBounded(t *testing.T) {
	v := model.NewVar(0)
	app := model.New("n", model.Size{W: 10, H: 10},
		model.VStack(model.Label("").BindTextInt(v)))
	calls := 0
	app.OnUpdate(func() {
		calls++
		if calls < 5 {
			v.Set(v.Get() + 1) // 订阅者内的 Set → 重入 notify
		}
	})
	v.Set(1)
	if calls > 10 {
		t.Fatalf("重入通知未合并: calls=%d", calls)
	}
	if v.Get() < 2 {
		t.Fatalf("重入 Set 未生效: %d", v.Get())
	}
}

// TestIncConcurrentAtomic —— 回归 P1：Inc 并发不丢更新。
func TestIncConcurrentAtomic(t *testing.T) {
	v := model.NewVar(0)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				model.Inc(v)
			}
		}()
	}
	wg.Wait()
	if v.Get() != 5000 {
		t.Fatalf("并发 Inc 丢更新: %d, want 5000", v.Get())
	}
}

// TestTabsListOnClickFires —— 回归 P1：Tabs/List 的 OnClick(func(int)) 此前永不触发。
func TestTabsListOnClickFires(t *testing.T) {
	var tabIdx, listIdx = -1, -1
	app := model.New("t", model.Size{W: 10, H: 10},
		model.VStack(
			model.Tabs(model.Tab{Title: "a"}, model.Tab{Title: "b"}).
				WithID("tabs").OnClick(func(i int) { tabIdx = i }),
			model.List("x", "y").WithID("list").OnClick(func(i int) { listIdx = i }),
		))
	_ = app.Dispatch(model.Event{WidgetID: "tabs", Type: "click", Value: "1"})
	_ = app.Dispatch(model.Event{WidgetID: "list", Type: "click", Value: "1"})
	if tabIdx != 1 || listIdx != 1 {
		t.Fatalf("OnClick 未触发: tab=%d list=%d", tabIdx, listIdx)
	}
}
