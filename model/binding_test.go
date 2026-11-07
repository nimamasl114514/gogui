package model_test

import (
	"strings"
	"testing"

	"github.com/nimamasl114514/gogui/model"
	"github.com/nimamasl114514/gogui/snapshot"
)

// buildBoundApp 用绑定体系构建：计数 Var + 双向输入 + 开关 + 滑杆 + 进度。
func buildBoundApp() (*model.App, *model.Var[int], *model.Var[string], *model.Var[bool], *model.Var[float64]) {
	count := model.NewVar(0)
	name := model.NewVar("")
	dark := model.NewVar(false)
	ratio := model.NewVar(0.3)

	app := model.New("bound", model.Size{W: 400, H: 300},
		model.VStack(
			model.Button("＋1").WithID("inc").OnClick(func(c *model.Ctx) {
				count.Set(count.Get() + 1)
			}),
			model.Label("0").WithID("count").BindTextInt(count, func(n int) string { return "计数：" + model.FormatFloat(float64(n)) }),
			model.Input().WithID("name").BindValue(name),
			model.Label("").WithID("echo").BindText(name),
			model.Toggle().WithID("sw").BindChecked(dark),
			model.Label("").WithID("sw-state").BindTextFloat(ratio, func(f float64) string {
				if dark.Get() {
					return "on"
				}
				return "off"
			}),
			model.Slider(0, 100).WithID("sld").BindSlider(ratio),
			model.Progress(0).WithID("bar").BindLevel(ratio),
		),
	)
	return app, count, name, dark, ratio
}

func TestBindTextReactive(t *testing.T) {
	app, count, _, _, _ := buildBoundApp()
	// 点击 → handler 改 Var → invalidation → 渲染取新值。
	if err := app.Dispatch(model.Event{WidgetID: "inc", Type: "click"}); err != nil {
		t.Fatalf("click: %v", err)
	}
	if count.Get() != 1 {
		t.Fatalf("count = %d, want 1", count.Get())
	}
	if w := app.LookupWidget("count"); w.TextValue() != "计数：1" {
		t.Fatalf("text = %q", w.TextValue())
	}
}

func TestBindValueTwoWay(t *testing.T) {
	app, _, name, _, _ := buildBoundApp()
	// 浏览器方向：change 事件写回 Var。
	if err := app.Dispatch(model.Event{WidgetID: "name", Type: "change", Value: "gogui"}); err != nil {
		t.Fatalf("change: %v", err)
	}
	if name.Get() != "gogui" {
		t.Fatalf("var = %q, want gogui", name.Get())
	}
	if w := app.LookupWidget("echo"); w.TextValue() != "gogui" {
		t.Fatalf("echo = %q", w.TextValue())
	}
	// Go 方向：Var.Set 直接反映到控件值。
	name.Set("反向")
	if w := app.LookupWidget("name"); w.ValueText() != "反向" {
		t.Fatalf("input value = %q", w.ValueText())
	}
}

func TestBindCheckedTwoWay(t *testing.T) {
	app, _, _, dark, _ := buildBoundApp()
	if err := app.Dispatch(model.Event{WidgetID: "sw", Type: "click"}); err != nil {
		t.Fatalf("click: %v", err)
	}
	if !dark.Get() {
		t.Fatal("toggle click 未写回 Var")
	}
	if w := app.LookupWidget("sw"); !w.CheckedValue() {
		t.Fatal("控件读值未跟随")
	}
	dark.Set(false)
	if w := app.LookupWidget("sw"); w.CheckedValue() {
		t.Fatal("Var.Set 未反映到控件")
	}
}

func TestBindSliderTwoWay(t *testing.T) {
	app, _, _, _, ratio := buildBoundApp()
	if err := app.Dispatch(model.Event{WidgetID: "sld", Type: "change", Value: "66"}); err != nil {
		t.Fatalf("slide: %v", err)
	}
	if ratio.Get() != 66 {
		t.Fatalf("ratio = %v, want 66", ratio.Get())
	}
	if w := app.LookupWidget("sld"); w.SliderValue() != 66 {
		t.Fatalf("slider value = %v", w.SliderValue())
	}
	if w := app.LookupWidget("bar"); w.LevelValue() != 66 {
		t.Fatalf("bar level = %v, want 66（绑定应直读 Var）", w.LevelValue())
	}
}

func TestBindSnapshotResolves(t *testing.T) {
	app, _, _, dark, _ := buildBoundApp()
	dark.Set(true)
	snap := snapshot.Tree(app)
	if !strings.Contains(snap, `checked=true`) {
		t.Fatalf("快照未解析绑定值:\n%s", snap)
	}
}
