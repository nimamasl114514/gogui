// Package gogui 是声明式 Go GUI 框架：Go 定义控件树，WebView 渲染出
// 现代质感的界面；无头模式可零窗口、零浏览器地驱动控件做测试，
// golden 工具链提供像素级与树快照级双通道对照测试。
//
// 最小示例：
//
//	app := gogui.New("演示", gogui.Size{W: 860, H: 560},
//	    gogui.VStack(
//	        gogui.Label("你好，gogui").Class("h1"),
//	        gogui.Button("点我").Primary().ID("btn").OnClick(func(c *gogui.Ctx) {
//	            c.SetText("lbl", "点过了")
//	        }),
//	        gogui.Label("").ID("lbl"),
//	    ),
//	)
//	app.Run(gogui.Webview2()) // 或 Headless() / EdgeApp()
package gogui

import (
	"github.com/nimamasl114514/gogui/model"
	"github.com/nimamasl114514/gogui/theme"
	"github.com/nimamasl114514/gogui/window"
)

// 类型别名：直接使用 model 的核心类型。
type (
	App    = model.App
	Size   = model.Size
	Widget = model.Widget
	Tab    = model.Tab
	Ctx    = model.Ctx
	Event  = model.Event

	// 响应式值（绑定体系）
	Var       = model.Var[any]
	StringVar = model.StringVar
	FloatVar  = model.FloatVar
	BoolVar   = model.BoolVar
	IntVar    = model.IntVar
	RatioVar  = model.RatioVar
)

// Ratio 是 0..1 比例语义类型（百分比展示专用，防口径错配）。
type Ratio = model.Ratio

// RatioOf 把裸 float64 显式转成 Ratio。
func RatioOf(f float64) model.Ratio { return model.RatioOf(f) }

// NewVar 创建响应式值（配合 BindText/BindLevel/BindChecked/BindValue/BindSlider）。
func NewVar[T comparable](v T) *model.Var[T] { return model.NewVar(v) }

// Var 便捷算子。
func Inc(v *model.Var[int]) int { return model.Inc(v) }

// Add 数值累加（int/float64）。
func Add[T int | float64](v *model.Var[T], d T) { model.Add(v, d) }

// Flip 翻转布尔并返回新值。
func Flip(v *model.Var[bool]) bool { return model.Flip(v) }

// Theme 别名。
type Theme = theme.Theme

// New 创建应用。
func New(title string, size Size, root *Widget) *App {
	return model.New(title, size, root)
}

// 控件构造器别名（保持调用侧一行一个控件的流式体验）。
var (
	VStack   = model.VStack
	HStack   = model.HStack
	Col      = model.Col
	Row      = model.Row

	// 样式构造糖
	H1      = model.H1
	H2      = model.H2
	H3      = model.H3
	Muted   = model.Muted
	Small   = model.Small
	Mono    = model.Mono
	Metric  = model.Metric
	Field   = model.Field
	OkBadge     = model.OkBadge
	WarnBadge   = model.WarnBadge
	DangerBadge = model.DangerBadge

	// 绑定文本构造糖
	Text      = model.Text
	TextF     = model.TextF
	TextFloat = model.TextFloat
	TextInt   = model.TextInt
	TextPct   = model.TextPct
	TextFunc  = model.TextFunc
	TextLarge = model.MetricFunc // 别名：TextLarge == MetricFunc（大字）

	// v6 绑定构造糖
	MetricFunc = model.MetricFunc
	MetricPct  = model.MetricPct
	SmallFunc  = model.SmallFunc
	MutedFunc  = model.MutedFunc
	Spacer   = model.Spacer
	Card     = model.Card
	Label    = model.Label
	Button   = model.Button
	Input    = model.Input
	Textarea = model.Textarea
	Toggle   = model.Toggle
	Slider   = model.Slider
	Progress = model.Progress
	Select   = model.Select
	Badge    = model.Badge
	Tabs     = model.Tabs
	List     = model.List
	Image    = model.Image
	SVGImage = model.SVGImage
	Sep      = model.Sep
)

// 布局常量（类型别名：旧字符串写法照常可用）。
const (
	XS = model.XS
	SM = model.SM
	MD = model.MD
	LG = model.LG
	XL = model.XL

	Start   = model.Start
	Center  = model.Center
	End     = model.End
	Stretch = model.Stretch

	JustifyStart   = model.JustifyStart
	JustifyCenter  = model.JustifyCenter
	JustifyEnd     = model.JustifyEnd
	JustifyBetween = model.JustifyBetween
	JustifyAround  = model.JustifyAround
)

// 主题预设别名。
var (
	DarkGlow   = theme.DarkGlow
	CleanLight = theme.CleanLight
)

// 窗口后端别名。
var (
	Webview2 = window.Webview2
	EdgeApp  = window.EdgeApp
	Headless = window.Headless
)

// Run 运行应用：起本地 server → 打开窗口 → 阻塞至窗口关闭。
// theme 可为 nil（默认 DarkGlow）；backend 可为 nil（默认 Webview2）。
func Run(app *App, th *Theme, backend window.Backend) error {
	if th == nil {
		th = theme.DarkGlow()
	}
	if backend == nil {
		backend = window.Webview2()
	}
	return backend.Run(app, th)
}
