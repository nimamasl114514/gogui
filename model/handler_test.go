package model_test

import (
	"strings"
	"testing"

	"github.com/nimamasl114514/gogui/model"
)

// TestNoArgClickHandler —— OnClick(func()) 零样板。
func TestNoArgClickHandler(t *testing.T) {
	n := 0
	app := model.New("h", model.Size{W: 10, H: 10},
		model.VStack(model.Button("x").WithID("b").OnClick(func() { n++ })))
	if err := app.Dispatch(model.Event{WidgetID: "b", Type: "click"}); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("n = %d", n)
	}
}

// TestTypedChangeHandlers —— func(bool)/func(string)/func(float64)/func(int) 各取所需。
func TestTypedChangeHandlers(t *testing.T) {
	var gotBool bool
	var gotText string
	var gotF float64
	var gotI int
	app := model.New("h", model.Size{W: 10, H: 10},
		model.VStack(
			model.Toggle().WithID("sw").OnChange(func(on bool) { gotBool = on }),
			model.Input().WithID("in").OnChange(func(s string) { gotText = s }),
			model.Slider(0, 100).WithID("sld").OnChange(func(v float64) { gotF = v }),
			model.Tabs(model.Tab{Title: "a"}, model.Tab{Title: "b"}).WithID("tabs").
				OnChange(func(i int) { gotI = i }),
		))
	_ = app.Dispatch(model.Event{WidgetID: "sw", Type: "change", Value: "true"})
	_ = app.Dispatch(model.Event{WidgetID: "in", Type: "change", Value: "hi"})
	_ = app.Dispatch(model.Event{WidgetID: "sld", Type: "change", Value: "42.5"})
	_ = app.Dispatch(model.Event{WidgetID: "tabs", Type: "click", Value: "1"})
	if !gotBool || gotText != "hi" || gotF != 42.5 || gotI != 1 {
		t.Fatalf("bool=%v text=%q f=%v i=%d", gotBool, gotText, gotF, gotI)
	}
	// click 翻转语义下 func(bool) 也要拿到翻转后的正确值。
	_ = app.Dispatch(model.Event{WidgetID: "sw", Type: "click"})
	if gotBool {
		t.Fatal("toggle click 后 func(bool) 未收到 false")
	}
}

// TestInvalidHandlerPanics —— 错误签名注册期 panic（fail fast）。
func TestInvalidHandlerPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("错误签名应 panic")
		}
	}()
	model.Button("x").OnClick(func(a, b int) {})
}

// TestVarHelpers —— Inc/Add/Toggle/Update。
func TestVarHelpers(t *testing.T) {
	n := model.NewVar(1)
	if model.Inc(n) != 2 || n.Get() != 2 {
		t.Fatal("Inc")
	}
	model.Add(n, -3)
	if n.Get() != -1 {
		t.Fatal("Add")
	}
	b := model.NewVar(false)
	if !model.Flip(b) || !b.Get() {
		t.Fatal("Toggle")
	}
	n.Update(func(v int) int { return v * 10 })
	if n.Get() != -10 {
		t.Fatal("Update")
	}
}

// TestHandlerPanicRecovered —— 处理器 panic 被兜住：进程不死、计数入账、错误可见。
func TestHandlerPanicRecovered(t *testing.T) {
	app := model.New("p", model.Size{W: 10, H: 10},
		model.VStack(
			model.Button("炸").WithID("boom").OnClick(func() { panic("kaboom") }),
			model.Button("活").WithID("ok").OnClick(func() {}),
		))
	err := app.Dispatch(model.Event{WidgetID: "boom", Type: "click"})
	if err == nil || !strings.Contains(err.Error(), "handler panic") {
		t.Fatalf("panic 未转为错误: %v", err)
	}
	// 应用仍可用。
	if err := app.Dispatch(model.Event{WidgetID: "ok", Type: "click"}); err != nil {
		t.Fatalf("panic 后应用不可用: %v", err)
	}
	st := app.Stats()
	if st.Failures != 1 {
		t.Fatalf("failures = %d, want 1", st.Failures)
	}
}
