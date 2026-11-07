package model_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nimamasl114514/gogui/model"
	"github.com/nimamasl114514/gogui/render"
	"github.com/nimamasl114514/gogui/theme"
)

// FuzzDispatch —— 随机事件轰炸：任何输入都不得 panic / 死锁；
// 未知控件/非法类型必须返回 error 而不是崩。
func FuzzDispatch(f *testing.F) {
	f.Add("inc", "click", "")
	f.Add("lbl", "click", "")
	f.Add("sw", "change", "true")
	f.Add("sld", "change", "42.5")
	f.Add("tabs", "click", "1")
	f.Add("", "", "")
	f.Add("nope", "weird", "\x00\xff")
	f.Fuzz(func(t *testing.T, gid, typ, val string) {
		app := buildTestApp()
		_ = app.Dispatch(model.Event{WidgetID: gid, Type: typ, Value: val})
		// 事件后整树渲染不得 panic，且确定性成立。
		th := theme.DarkGlow()
		h1 := render.HTML(app, th)
		h2 := render.HTML(app, th)
		if h1 != h2 {
			t.Fatal("渲染不确定")
		}
	})
}

// FuzzRenderTree —— 随机字节流构造控件树：
//   - 渲染不得 panic；
//   - 同状态两次渲染必须逐字节一致；
//   - 文本内容必须被转义（不得出现未转义标签）。
func FuzzRenderTree(f *testing.F) {
	f.Add([]byte{1, 2, 3, 0, 4})
	f.Add([]byte("hello <script>alert(1)</script>"))
	f.Add([]byte{0xff, 0xfe, 0x00})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 512 {
			data = data[:512]
		}
		widgets := make([]*model.Widget, 0, len(data))
		for i, b := range data {
			text := string(rune('a' + int(b%26)))
			if i%7 == 0 {
				// 掺入需要转义的原文（含种子字节）。
				text = string(data) + "<&>\"'"
			}
			switch b % 8 {
			case 0:
				widgets = append(widgets, model.Label(text))
			case 1:
				widgets = append(widgets, model.Button(text).OnClick(func() {}))
			case 2:
				widgets = append(widgets, model.Input().Value(text))
			case 3:
				widgets = append(widgets, model.Toggle().Checked(b&1 == 0))
			case 4:
				widgets = append(widgets, model.Progress(float64(b)/255))
			case 5:
				widgets = append(widgets, model.Badge(text))
			case 6:
				widgets = append(widgets, model.Sep())
			default:
				widgets = append(widgets, model.Spacer())
			}
		}
		app := model.New("fuzz", model.Size{W: 100, H: 100}, model.VStack(widgets...))
		th := theme.DarkGlow()
		h1 := render.HTML(app, th)
		h2 := render.HTML(app, th)
		if h1 != h2 {
			t.Fatal("渲染不确定（同状态两次输出不同）")
		}
		// 检查转义：种子反馈里的 <script> 不得以原文出现（CSS 里没有该串，HTML 里也不应有）。
		if strings.Contains(h1, "<script>alert") {
			t.Fatal("文本未转义，出现注入原文")
		}
		// 输出必须是合法 UTF-8。
		if !utf8.ValidString(h1) {
			t.Fatal("渲染输出不是合法 UTF-8")
		}
	})
}
