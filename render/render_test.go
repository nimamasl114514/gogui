package render_test

import (
	"strings"
	"testing"

	"github.com/nimamasl114514/gogui/model"
	"github.com/nimamasl114514/gogui/render"
	"github.com/nimamasl114514/gogui/theme"
)

func demoApp() *model.App {
	return model.New("demo", model.Size{W: 400, H: 300},
		model.VStack(
			model.Label("标题 <script>").Class("h1"),
			model.Button("点我").Primary().WithID("btn"),
			model.Input().WithID("in").Value("已有值").Placeholder("提示 '引号'"),
			model.Toggle().WithID("sw").Checked(true),
			model.Slider(0, 100).WithID("sld").Value(30),
			model.Progress(0.55),
			model.Select("a", "b").WithID("sel").SelectOption("b"),
			model.Badge("新").Class("ok"),
			model.Tabs(
				model.Tab{Title: "一", Content: model.Label("A1")},
				model.Tab{Title: "二", Content: model.Label("B2")},
			).WithID("tabs").Active(1),
			model.List("l1", "l2").WithID("list"),
			model.Image("x.png").Alt("图"),
		),
	)
}

func TestHTMLDeterministic(t *testing.T) {
	app := demoApp()
	th := theme.DarkGlow()
	h1 := render.HTML(app, th)
	h2 := render.HTML(app, th)
	if h1 != h2 {
		t.Fatal("同一状态两次渲染 HTML 不一致（确定性破坏）")
	}
	// 状态变更后应改变输出。
	app.Dispatch(model.Event{WidgetID: "sw", Type: "change", Value: "false"})
	h3 := render.HTML(app, th)
	if h3 == h1 {
		t.Fatal("状态变更后 HTML 未变化")
	}
	if strings.Contains(h3, `aria-checked="true">`) {
		t.Fatal("toggle 仍为 true")
	}
	if !strings.Contains(h3, `aria-checked="false">`) {
		t.Fatal("toggle 未渲染为 false")
	}
}

func TestHTMLEscaping(t *testing.T) {
	app := demoApp()
	h := render.HTML(app, theme.DarkGlow())
	if strings.Contains(h, "<script>") {
		t.Fatal("未转义 <script> 注入")
	}
	if !strings.Contains(h, "&lt;script&gt;") {
		t.Fatal("label 文本未按预期转义")
	}
	if !strings.Contains(h, "&#39;引号&#39;") {
		t.Fatal("placeholder 引号未转义")
	}
}

func TestHTMLContainsEveryKind(t *testing.T) {
	h := render.HTML(demoApp(), theme.DarkGlow())
	for _, want := range []string{
		`class="gg-label h1"`,
		`class="gg-button gg-primary"`,
		`class="gg-toggle"`,
		`aria-checked="true"`,
		`class="gg-slider"`,
		`--fill:30.00%`,
		`class="gg-progress-fill" style="width:55.00%"`,
		`<option selected>b</option>`,
		`class="gg-badge ok"`,
		`class="gg-tab active"`,
		`data-gv="1"`,
		`<img class="gg-image" data-gid="image-1" src="x.png" alt="图">`,
	} {
		if !strings.Contains(h, want) {
			t.Fatalf("HTML 缺少片段 %q", want)
		}
	}
}

func TestActiveTabRendersActivePanel(t *testing.T) {
	h := render.HTML(demoApp(), theme.DarkGlow())
	if !strings.Contains(h, "B2") {
		t.Fatal("活动页内容未渲染")
	}
	if strings.Contains(h, "A1") {
		t.Fatal("非活动页内容不应渲染")
	}
}

func TestThemeVarsInjected(t *testing.T) {
	h := render.HTML(demoApp(), theme.CleanLight())
	if !strings.Contains(h, ":root{--bg:#f2f5fb;") {
		t.Fatal("主题变量未注入")
	}
	if !strings.Contains(h, `class="gg-body gg-light"`) {
		t.Fatal("浅色主题 body 类未设置")
	}
}
