package render_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/nimamasl114514/gogui/model"
	"github.com/nimamasl114514/gogui/render"
	"github.com/nimamasl114514/gogui/theme"
)

// TestStyleInjectionEscaped —— 回归 P0：Style 逃生舱不得注入属性。
func TestStyleInjectionEscaped(t *testing.T) {
	app := model.New("x", model.Size{W: 10, H: 10},
		model.VStack(model.Label("x").
			Style(`color:red" onmouseover="alert(1)`)))
	h := render.HTML(app, theme.DarkGlow())
	if strings.Contains(h, `onmouseover="alert(1)"`) {
		t.Fatal("Style 注入未被转义")
	}
	if !strings.Contains(h, `&#34;`) {
		t.Fatal("Style 引号未按实体转义")
	}
}

// TestAlignJustifyWhitelist —— 回归 P0：布局枚举白名单，非法值丢弃。
func TestAlignJustifyWhitelist(t *testing.T) {
	app := model.New("x", model.Size{W: 10, H: 10},
		model.VStack(model.Label("x")).
			Align(`flex-start" onmouseover="alert(1)`).
			Justify("space-between"))
	h := render.HTML(app, theme.DarkGlow())
	if strings.Contains(h, "onmouseover") {
		t.Fatal("align 注入未被白名单拦截")
	}
	if !strings.Contains(h, "justify-content:space-between") {
		t.Fatal("合法 justify 被误杀")
	}
}

// TestThemeSanitized —— 回归 P0：第三方主题的恶意变量值不得闭合 <style>。
func TestThemeSanitized(t *testing.T) {
	th := theme.DarkGlow()
	th.Vars = append(th.Vars,
		theme.Var{Name: "--evil", Value: `red}</style><script>alert(1)</script>`},
		theme.Var{Name: "--bad name", Value: "x"},
	)
	app := model.New("x", model.Size{W: 10, H: 10}, model.VStack(model.Label("x")))
	h := render.HTML(app, th)
	if strings.Contains(h, "</style><script>") {
		t.Fatal("主题变量闭合 style 注入未拦截")
	}
	if strings.Contains(h, "--bad name") {
		t.Fatal("非法变量名未拦截")
	}
}

// TestUniqueDataGID —— 回归：data-gid 全文档唯一（Tabs/List 子按钮走 data-gg-id）。
func TestUniqueDataGID(t *testing.T) {
	app := model.New("x", model.Size{W: 10, H: 10},
		model.VStack(
			model.Tabs(
				model.Tab{Title: "a", Content: model.Label("A")},
				model.Tab{Title: "b", Content: model.Label("B")},
			).WithID("tabs"),
			model.List("x", "y").WithID("list"),
			model.Select("a", "b").WithID("sel"),
		))
	h := render.HTML(app, theme.DarkGlow())
	re := regexp.MustCompile(`data-gid="([^"]+)"`)
	seen := map[string]int{}
	for _, m := range re.FindAllStringSubmatch(h, -1) {
		seen[m[1]]++
	}
	for id, n := range seen {
		if n > 1 {
			t.Fatalf("data-gid 重复: %q ×%d（全部: %v）", id, n, seen)
		}
	}
	// Tabs/List 子按钮必须用 data-gg-id 保持事件可达。
	if !strings.Contains(h, `data-gg-id="tabs"`) || !strings.Contains(h, `data-gg-id="list"`) {
		t.Fatal("子按钮缺 data-gg-id")
	}
}

// TestSelectWrapperClass —— 回归：Select 根是 gg-selectwrap，内层才是 gg-select（无双边框）。
func TestSelectWrapperClass(t *testing.T) {
	app := model.New("x", model.Size{W: 10, H: 10},
		model.VStack(model.Select("a").WithID("sel")))
	h := render.HTML(app, theme.DarkGlow())
	if !strings.Contains(h, `class="gg-selectwrap"`) {
		t.Fatal("缺 gg-selectwrap 根类")
	}
	if strings.Count(h, "gg-selectwrap") < 1 || strings.Count(h, `class="gg-select"`) < 1 {
		t.Fatal("select 结构异常")
	}
}

// TestLabelSingleStyleAttr —— 回归：Label 的 Width+Style 合并为单个 style 属性。
func TestLabelSingleStyleAttr(t *testing.T) {
	app := model.New("x", model.Size{W: 10, H: 10},
		model.VStack(model.Label("x").WithID("lbl").Width(120).Style("color:red")))
	h := render.HTML(app, theme.DarkGlow())
	// 只截取 lbl 元素本身（从开标签到其 ">" 结束）。
	i := strings.Index(h, `data-gid="lbl"`)
	start := strings.LastIndex(h[:i], "<")
	end := strings.Index(h[i:], ">")
	seg := h[start : i+end+1]
	if strings.Count(seg, "style=") != 1 {
		t.Fatalf("label 出现多个 style 属性: %s", seg)
	}
	if !strings.Contains(seg, "width:120px") || !strings.Contains(seg, "color:red") {
		t.Fatalf("width/style 未合并: %s", seg)
	}
}

// TestTabsDisabled —— 回归：Tabs 置灰后子按钮带 disabled。
func TestTabsDisabled(t *testing.T) {
	app := model.New("x", model.Size{W: 10, H: 10},
		model.VStack(model.Tabs(model.Tab{Title: "a"}).WithID("tabs").Disabled()))
	h := render.HTML(app, theme.DarkGlow())
	if !strings.Contains(h, `data-gg-id="tabs"`) || !strings.Contains(h, "disabled") {
		t.Fatal("tabs 子按钮未渲染 disabled")
	}
}

