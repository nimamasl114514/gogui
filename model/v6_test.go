package model_test

import (
	"strings"
	"testing"

	"github.com/nimamasl114514/gogui/model"
	"github.com/nimamasl114514/gogui/render"
	"github.com/nimamasl114514/gogui/theme"
)

// Vars5 演示 v5 状态容器：值类型 Var 字段 + 零值可用。
type Vars5 struct {
	CPU   model.FloatVar
	Clock model.StringVar
	Name  model.StringVar
}

// TestAutoDependencyTracking —— v6 自动依赖追踪：
// 闭包在构建时求值一次，读到的 Var 自动登记 —— 无 AttachAll、无 Depends。
func TestAutoDependencyTracking(t *testing.T) {
	v := &Vars5{}
	app := model.New("v6", model.Size{W: 10, H: 10},
		model.VStack(
			model.TextFunc(func() string { return "名字：" + v.Name.Get() }).WithID("auto"),
			model.Label("").WithID("cpu").BindTextFloat(&v.CPU, "%.1f GB"),
		))

	notified := 0
	app.OnUpdate(func() { notified++ })

	// Bind* 与 BindFunc 都自动挂载 → Set 均触发通知。
	v.CPU.Set(12.5)
	if notified != 1 {
		t.Fatalf("BindTextFloat 的 Var Set 应通知: %d", notified)
	}
	v.Name.Set("gogui") // 只在 TextFunc 闭包里读过 → 自动追踪应命中
	if notified != 2 {
		t.Fatalf("自动追踪的 Var Set 应通知: %d", notified)
	}
	if w := app.LookupWidget("auto"); w.TextValue() != "名字：gogui" {
		t.Fatalf("auto text = %q", w.TextValue())
	}
}

// TestBranchBlindSpotAndDepends —— 自动追踪的分支盲区 + Depends 逃生舱。
func TestBranchBlindSpotAndDepends(t *testing.T) {
	v := &Vars5{}
	show := false
	mk := func(extra bool) *model.App {
		w := model.TextFunc(func() string {
			if show {
				return v.Name.Get()
			}
			return "—"
		}).WithID("t")
		if extra {
			w.Depends(&v.Name)
		}
		return model.New("b", model.Size{W: 10, H: 10}, model.VStack(w))
	}

	// 盲区：构建时 show=false → v.Name 未被读到 → Set 不通知。
	app1 := mk(false)
	n1 := 0
	app1.OnUpdate(func() { n1++ })
	v.Name.Set("x")
	if n1 != 0 {
		t.Fatalf("盲区 Var 不应通知: %d", n1)
	}

	// Depends 补依赖后：Set 通知。
	app2 := mk(true)
	n2 := 0
	app2.OnUpdate(func() { n2++ })
	v.Name.Set("y")
	if n2 != 1 {
		t.Fatalf("Depends 后应通知: %d", n2)
	}
}

// TestFormatStringBindings —— 格式串绑定（string 与 func 两种形态）。
func TestFormatStringBindings(t *testing.T) {
	f := model.NewVar(3.14159)
	n := model.NewVar(7)
	s := model.NewVar("gogui")

	app := model.New("f", model.Size{W: 10, H: 10},
		model.VStack(
			model.Label("").WithID("f").BindTextFloat(f, "%.1f 秒"),
			model.Label("").WithID("i").BindTextInt(n, "%d 个"),
			model.Label("").WithID("s").BindTextF(s, "你好，%s"),
			model.Label("").WithID("pct").BindTextFloat(f, func(x float64) string { return "函数形态" }),
		))
	checks := map[string]string{"f": "3.1 秒", "i": "7 个", "s": "你好，gogui", "pct": "函数形态"}
	for id, want := range checks {
		if w := app.LookupWidget(id); w.TextValue() != want {
			t.Fatalf("%s = %q, want %q", id, w.TextValue(), want)
		}
	}
}

// TestStyleEscapeHatch —— Style 渲染进 style 属性（叶子与容器都对）。
func TestStyleEscapeHatch(t *testing.T) {
	app := model.New("sty", model.Size{W: 10, H: 10},
		model.VStack(
			model.Label("x").WithID("lbl").Style("letter-spacing:2px;color:#ff00ff"),
			model.Button("b").WithID("btn").Style("padding:1px"),
		).Style("border:1px solid red"))
	h := render.HTML(app, theme.DarkGlow())
	if !strings.Contains(h, `style="letter-spacing:2px;color:#ff00ff"`) {
		t.Fatal("叶子 Style 未渲染")
	}
	if !strings.Contains(h, `style="padding:1px"`) {
		t.Fatal("按钮 Style 未渲染")
	}
	if !strings.Contains(h, ";min-width:0;border:1px solid red") {
		t.Fatal("容器 Style 未合并")
	}
}

// TestTextPct —— 百分比糖（Ratio 0..1 → "42%"）；类型系统禁止裸 float64Var。
func TestTextPct(t *testing.T) {
	r := model.NewVar(model.RatioOf(0.42))
	w := model.TextPct(r)
	if w.TextValue() != "42%" {
		t.Fatalf("pct = %q", w.TextValue())
	}
	if model.RatioOf(0.42).Percent() != 42 {
		t.Fatal("Ratio.Percent 计算错误")
	}
}
