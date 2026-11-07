package model

import (
	"encoding/base64"
	"fmt"
)

// 控件构造器（fluent）。视觉细节由 theme 设计系统类控制，结构属性走 Props。

// VStack 垂直容器。
func VStack(children ...*Widget) *Widget {
	return newWidget(KindVStack, map[string]any{PropGap: 16}, children...)
}

// HStack 水平容器。
func HStack(children ...*Widget) *Widget {
	return newWidget(KindHStack, map[string]any{PropGap: 16}, children...)
}

// Spacer 弹性占位（flex:1）。
func Spacer() *Widget { return newWidget(KindSpacer, nil) }

// Card 卡片容器（玻璃质感表面）。
func Card(children ...*Widget) *Widget {
	return newWidget(KindCard, map[string]any{PropGap: 16, PropPadding: 20}, children...)
}

// Label 文本标签。排版级别用 .Class("h1"/"h2"/"muted"...) 控制。
func Label(text string) *Widget {
	return newWidget(KindLabel, map[string]any{PropText: text})
}

// Button 按钮。变体：.Primary()/.Danger()/.Ghost()，默认 secondary。
func Button(text string) *Widget {
	return newWidget(KindButton, map[string]any{PropText: text, PropVariant: "secondary"})
}

// Input 单行输入框。
func Input() *Widget {
	return newWidget(KindInput, map[string]any{PropValue: "", PropPlaceholder: ""})
}

// Textarea 多行输入框。
func Textarea() *Widget {
	return newWidget(KindTextarea, map[string]any{PropValue: "", PropPlaceholder: ""})
}

// Toggle 开关。
func Toggle() *Widget { return newWidget(KindToggle, map[string]any{PropChecked: false}) }

// Slider 滑杆（min/max 为数值端点，step 默认 1）。
// 第三个参数可选传 *Var[float64]：构造即双向绑定。
func Slider(min, max float64, bind ...*Var[float64]) *Widget {
	w := newWidget(KindSlider, map[string]any{PropMin: min, PropMax: max, PropStep: 1.0, PropValue: min})
	if len(bind) > 0 && bind[0] != nil {
		w.BindSlider(bind[0])
	}
	return w
}

// Progress 进度条：可直接接 *Var[float64]（构造即绑定）或静态数值（0..1）。
func Progress(arg any) *Widget {
	switch t := arg.(type) {
	case *Var[Ratio]:
		w := newWidget(KindProgress, map[string]any{PropLevel: 0.0})
		w.levelSrc = func() float64 { return float64(t.Get()) }
		w.addAttach(t.attach)
		return w
	case *Var[float64]:
		return newWidget(KindProgress, map[string]any{PropLevel: 0.0}).BindLevel(t)
	case float64:
		return newWidget(KindProgress, map[string]any{PropLevel: t})
	case int:
		return newWidget(KindProgress, map[string]any{PropLevel: float64(t)})
	default:
		panic("gogui.Progress: 参数须为 *Var[float64] / float64")
	}
}

// Select 下拉选择。
func Select(options ...string) *Widget {
	return newWidget(KindSelect, map[string]any{PropOptions: append([]string{}, options...), PropSelected: ""})
}

// Badge 徽标（小圆角胶囊）。
func Badge(text string) *Widget {
	return newWidget(KindBadge, map[string]any{PropText: text})
}

// Tabs 页签。active 从 0 起；点击页签触发 OnChange。
func Tabs(tabs ...Tab) *Widget {
	return newWidget(KindTabs, map[string]any{PropTabs: append([]Tab{}, tabs...), PropActiveTab: 0})
}

// List 可选列表（单选）。selected 为当前选中项下标，-1 表示无选中。
func List(items ...string) *Widget {
	return newWidget(KindList, map[string]any{PropItems: append([]string{}, items...), PropSelectedIdx: -1})
}

// Image 图片（src 支持 http(s) URL 或 data: URI）。
func Image(src string) *Widget {
	return newWidget(KindImage, map[string]any{PropSrc: src, PropAlt: ""})
}

// Sep 分隔线。
func Sep() *Widget { return newWidget(KindSep, nil) }

// SVGDataURI 把 SVG 源码编码成 base64 data-URI。
func SVGDataURI(svg string) string {
	const prefix = "data:image/svg+xml;base64,"
	return prefix + base64.StdEncoding.EncodeToString([]byte(svg))
}

// SVGImage 以 SVG 源码生成图像控件（base64 data-URI，免素材资产管理）。
func SVGImage(svg string) *Widget { return Image(SVGDataURI(svg)) }

// Col 是 VStack 的短别名；Row 是 HStack 的短别名。
func Col(children ...*Widget) *Widget { return VStack(children...) }
func Row(children ...*Widget) *Widget { return HStack(children...) }

// ---- 布局常量（类型别名：旧字符串写法照常可用） ----

// GapSize 是间距刻度（SM/MD/LG/XL 等），也接受任意 int 像素值。
type GapSize = int

// 间距刻度。
const (
	XS GapSize = 4
	SM GapSize = 8
	MD GapSize = 14
	LG GapSize = 18
	XL GapSize = 26
)

// AlignMode 是交叉轴对齐取值（Center/Stretch/Start/End），也接受原始 CSS 字符串。
type AlignMode = string

// 对齐常量。
const (
	Start   AlignMode = "flex-start"
	Center  AlignMode = "center"
	End     AlignMode = "flex-end"
	Stretch AlignMode = "stretch"
)

// JustifyMode 是主轴分布取值（Between/Around），也接受原始 CSS 字符串。
type JustifyMode = string

// 分布常量。
const (
	JustifyStart   JustifyMode = "flex-start"
	JustifyCenter  JustifyMode = "center"
	JustifyEnd     JustifyMode = "flex-end"
	JustifyBetween JustifyMode = "space-between"
	JustifyAround  JustifyMode = "space-around"
)

// ---- 样式构造糖（等价于 Label(...).Class(...)，读起来短一半） ----

// H1/H2/H3 标题级别标签。
func H1(text string) *Widget { return Label(text).Class("h1") }
func H2(text string) *Widget { return Label(text).Class("h2") }
func H3(text string) *Widget { return Label(text).Class("h3") }

// Muted 弱化文本。
func Muted(text string) *Widget { return Label(text).Class("muted") }

// Small 小号文本。
func Small(text string) *Widget { return Label(text).Class("small") }

// Mono 等宽文本。
func Mono(text string) *Widget { return Label(text).Class("mono") }

// Metric 指标大数字。两种形态（构造即绑定）：
//
//	gogui.Metric("—")                          // 静态文本
//	gogui.Metric(&v.MemUsed, "%.1f GB")        // 绑定 *Var[float64] + 格式串
//	gogui.MetricFunc(func() string { ... })    // 任意取值函数（自动追踪依赖）
func Metric(args ...any) *Widget {
	switch len(args) {
	case 1:
		switch t := args[0].(type) {
		case string:
			return Label(t).Class("metric")
		case func() string:
			return MetricFunc(t)
		}
	case 2:
		if v, ok := args[0].(*Var[float64]); ok {
			if f, ok2 := args[1].(string); ok2 {
				return Label("").BindTextFloat(v, f).Class("metric")
			}
		}
	}
	panic("gogui.Metric: 参数须为 (text string) / (*Var[float64], format string) / (func() string)")
}

// MetricFunc 绑定取值函数的指标大字（闭包内读到的 Var 自动收集）。
func MetricFunc(f func() string) *Widget {
	return Label("").BindFunc(f).Class("metric")
}

// TextFunc 绑定取值函数的标签（闭包内读到的 Var 自动收集）。
func TextFunc(f func() string) *Widget { return Label("").BindFunc(f) }

// SmallFunc 绑定取值函数的小号标签。
func SmallFunc(f func() string) *Widget { return Label("").BindFunc(f).Class("small") }

// MutedFunc 绑定取值函数的弱化标签。
func MutedFunc(f func() string) *Widget { return Label("").BindFunc(f).Class("muted") }

// Field 表单行：左侧说明 + 右侧控件（水平居中对齐的标准写法）。
func Field(label string, control *Widget) *Widget {
	return HStack(Label(label).Class("muted"), control).Gap(12).Align("center")
}

// ---- 绑定文本构造糖 ----

// Text 绑定字符串 Var 的标签。
func Text(v *Var[string]) *Widget { return Label("").BindText(v) }

// TextF 绑定 + 格式化的标签（格式串或函数）。
func TextF(v *Var[string], f any) *Widget { return Label("").BindTextF(v, f) }

// TextFloat 绑定数值 Var 的标签（格式串如 "%.0f%%" 或函数）。
func TextFloat(v *Var[float64], f ...any) *Widget {
	return Label("").BindTextFloat(v, f...)
}

// TextInt 绑定整数 Var 的标签（格式串如 "%d 个" 或函数）。
func TextInt(v *Var[int], f ...any) *Widget {
	return Label("").BindTextInt(v, f...)
}

// TextPct 百分比标签：*Var[Ratio]（0..1）→ "42%"。
// 类型系统保证：裸 float64Var 无法传入（口径错配编译期报错）。
func TextPct(v *Var[Ratio]) *Widget {
	return Label("").BindFunc(func() string { return fmt.Sprintf("%.0f%%", v.Get().Percent()) })
}

// MetricPct 百分比指标大字：*Var[Ratio]（0..1）→ "42%"。
func MetricPct(v *Var[Ratio]) *Widget {
	return Label("").BindFunc(func() string { return fmt.Sprintf("%.0f%%", v.Get().Percent()) }).Class("metric")
}

// OkBadge / WarnBadge / DangerBadge 语义色徽标。
func OkBadge(text string) *Widget     { return Badge(text).Class("ok") }
func WarnBadge(text string) *Widget   { return Badge(text).Class("warn") }
func DangerBadge(text string) *Widget { return Badge(text).Class("danger") }

// ---- 控件级样式 fluent（绑定文本等动态控件也能直接点用） ----

// H1/H2/H3 标题级别。
func (w *Widget) H1() *Widget { return w.Class("h1") }
func (w *Widget) H2() *Widget { return w.Class("h2") }
func (w *Widget) H3() *Widget { return w.Class("h3") }

// Muted 弱化文本；Small 小号；Mono 等宽；Metric 指标大字。
func (w *Widget) Muted() *Widget  { return w.Class("muted") }
func (w *Widget) Small() *Widget  { return w.Class("small") }
func (w *Widget) Mono() *Widget   { return w.Class("mono") }
func (w *Widget) Metric() *Widget { return w.Class("metric") }

// ---- 布局 fluent ----

// Gap 设置容器子间距（px）。
func (w *Widget) Gap(px int) *Widget { w.Props[PropGap] = px; return w }

// Padding 设置容器内边距（px）。
func (w *Widget) Padding(px int) *Widget { w.Props[PropPadding] = px; return w }

// Align 设置交叉轴对齐：start/center/end/stretch。
func (w *Widget) Align(a string) *Widget { w.Props[PropAlign] = a; return w }

// Justify 设置主轴分布：start/center/end/between/around。
func (w *Widget) Justify(j string) *Widget { w.Props[PropJustify] = j; return w }

// Grow 让容器在父 flex 中占满剩余空间（flex:1，仪表盘等宽卡片场景）。
func (w *Widget) Grow() *Widget { w.Props[PropFlex] = true; return w }

// Width 设置控件期望宽度（px），滑杆/输入框等需要定宽的控件用。
func (w *Widget) Width(px int) *Widget { w.Props[PropWidth] = px; return w }

// Style 内联样式逃生舱：直接追加 CSS 到该控件的 style 属性（本地可信代码）。
// 例：.Style("letter-spacing:2px;text-transform:uppercase")
func (w *Widget) Style(css string) *Widget { w.Props[PropStyle] = css; return w }

// ---- 控件值 fluent ----

// Value 设置 Input/Textarea/Slider 的当前值。
func (w *Widget) Value(v any) *Widget { w.Props[PropValue] = v; return w }

// Placeholder 设置输入提示。
func (w *Widget) Placeholder(p string) *Widget { w.Props[PropPlaceholder] = p; return w }

// Checked 设置开关状态。
func (w *Widget) Checked(on bool) *Widget { w.Props[PropChecked] = on; return w }

// Step 设置滑杆步长。
func (w *Widget) Step(step float64) *Widget { w.Props[PropStep] = step; return w }

// SelectOption 预选 Select 的某一项。
func (w *Widget) SelectOption(opt string) *Widget { w.Props[PropSelected] = opt; return w }

// Active 预选 Tabs 页（从 0 起）。
func (w *Widget) Active(idx int) *Widget { w.Props[PropActiveTab] = idx; return w }

// Level 设置进度条进度（0..1）。
func (w *Widget) Level(l float64) *Widget { w.Props[PropLevel] = l; return w }

// Alt 设置图片替代文本。
func (w *Widget) Alt(a string) *Widget { w.Props[PropAlt] = a; return w }

// ---- 按钮变体 fluent ----

// Primary 主按钮（强调渐变）。
func (w *Widget) Primary() *Widget { w.Props[PropVariant] = "primary"; return w }

// Danger 危险按钮。
func (w *Widget) Danger() *Widget { w.Props[PropVariant] = "danger"; return w }

// Ghost 幽灵按钮（无边框文本按钮）。
func (w *Widget) Ghost() *Widget { w.Props[PropVariant] = "ghost"; return w }
