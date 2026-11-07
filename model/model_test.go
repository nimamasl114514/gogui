package model_test

import (
	"strings"
	"testing"

	"github.com/nimamasl114514/gogui/model"
)

func buildTestApp() *model.App {
	return model.New("t", model.Size{W: 400, H: 300},
		model.VStack(
			model.Label("计数：0").WithID("lbl"),
			model.Button("加一").WithID("inc").OnClick(func(c *model.Ctx) {
				n := c.FloatValue("val") + 1
				c.SetSliderValue("val", n)
				c.SetText("lbl", "计数："+itoa(int(n)))
			}),
			model.Slider(0, 100).WithID("val"),
			model.Input().WithID("name"),
			model.Toggle().WithID("sw"),
			model.Select("red", "green", "blue").WithID("color"),
			model.Tabs(
				model.Tab{Title: "A", Content: model.Label("内容A").WithID("pa")},
				model.Tab{Title: "B", Content: model.Label("内容B").WithID("pb")},
			).WithID("tabs"),
			model.List("x", "y", "z").WithID("list"),
		),
	)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

func TestClickUpdatesTree(t *testing.T) {
	app := buildTestApp()
	if err := app.Dispatch(model.Event{WidgetID: "inc", Type: "click"}); err != nil {
		t.Fatalf("click: %v", err)
	}
	w := app.LookupWidget("lbl")
	if w == nil {
		t.Fatal("lbl not found")
	}
	if got := w.Props[model.PropText]; got != "计数：1" {
		t.Fatalf("lbl text = %v, want 计数：1", got)
	}
}

func TestToggleAndSliderAndSelect(t *testing.T) {
	app := buildTestApp()
	if err := app.Dispatch(model.Event{WidgetID: "sw", Type: "change", Value: "true"}); err != nil {
		t.Fatalf("toggle: %v", err)
	}
	if w := app.LookupWidget("sw"); w.Props[model.PropChecked] != true {
		t.Fatal("toggle not on")
	}
	if err := app.Dispatch(model.Event{WidgetID: "val", Type: "change", Value: "42.5"}); err != nil {
		t.Fatalf("slide: %v", err)
	}
	if w := app.LookupWidget("val"); w.Props[model.PropValue] != 42.5 {
		t.Fatalf("slider value = %v, want 42.5", w.Props[model.PropValue])
	}
	if err := app.Dispatch(model.Event{WidgetID: "color", Type: "change", Value: "green"}); err != nil {
		t.Fatalf("choose: %v", err)
	}
	if w := app.LookupWidget("color"); w.Props[model.PropSelected] != "green" {
		t.Fatal("select not green")
	}
}

func TestTabsAndList(t *testing.T) {
	app := buildTestApp()
	if err := app.Dispatch(model.Event{WidgetID: "tabs", Type: "click", Value: "1"}); err != nil {
		t.Fatalf("tabs: %v", err)
	}
	if w := app.LookupWidget("tabs"); w.Props[model.PropActiveTab] != 1 {
		t.Fatal("tabs active != 1")
	}
	if err := app.Dispatch(model.Event{WidgetID: "tabs", Type: "click", Value: "9"}); err == nil {
		t.Fatal("out-of-range tab click should be rejected")
	}
	if err := app.Dispatch(model.Event{WidgetID: "list", Type: "click", Value: "2"}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if w := app.LookupWidget("list"); w.Props[model.PropSelectedIdx] != 2 {
		t.Fatal("list selidx != 2")
	}
}

func TestInputEvents(t *testing.T) {
	app := buildTestApp()
	for _, typ := range []string{"input", "change"} {
		if err := app.Dispatch(model.Event{WidgetID: "name", Type: typ, Value: "gogui"}); err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
	}
	if w := app.LookupWidget("name"); w.Props[model.PropValue] != "gogui" {
		t.Fatalf("input value = %v", w.Props[model.PropValue])
	}
}

func TestUnknownWidgetRejected(t *testing.T) {
	app := buildTestApp()
	if err := app.Dispatch(model.Event{WidgetID: "nope", Type: "click"}); err == nil {
		t.Fatal("unknown widget should error")
	}
}

func TestUpdateFromGoSide(t *testing.T) {
	app := buildTestApp()
	app.Update(func(c *model.Ctx) {
		c.SetText("lbl", "来自 Go 侧")
	})
	if w := app.LookupWidget("lbl"); w.Props[model.PropText] != "来自 Go 侧" {
		t.Fatal("Update mutation failed")
	}
}

func TestAutoIDDeterministic(t *testing.T) {
	a1 := buildTestApp()
	a2 := buildTestApp()
	// 同一构建顺序 → 同一 ID 集合。
	c1, c2 := map[string]bool{}, map[string]bool{}
	a1.View(func(r *model.Widget) { r.Walk(func(w *model.Widget) bool { c1[w.ID] = true; return true }) })
	a2.View(func(r *model.Widget) { r.Walk(func(w *model.Widget) bool { c2[w.ID] = true; return true }) })
	if len(c1) != len(c2) {
		t.Fatalf("id count %d != %d", len(c1), len(c2))
	}
	for k := range c1 {
		if !c2[k] {
			t.Fatalf("id %q missing in second app", k)
		}
	}
}

func TestDisabledLabelPath(t *testing.T) {
	// label 没有 click 语义：应返回 not-applicable 而不是 panic。
	app := buildTestApp()
	err := app.Dispatch(model.Event{WidgetID: "lbl", Type: "click"})
	if err == nil {
		t.Fatal("label click should be not-applicable")
	}
	if !strings.Contains(err.Error(), "not applicable") {
		t.Fatalf("err = %v", err)
	}
}
