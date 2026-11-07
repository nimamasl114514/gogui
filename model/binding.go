package model

import (
	"fmt"
	"strconv"
	"sync"
)

// Var 是可绑定到控件的响应式值。Set 自动触发应用重渲染（invalidation）；
// 在事件处理器（Dispatch/Update 窗口）内 Set 会排队合并，锁安全、不重入。
//
// 典型写法：
//
//	count := gogui.NewVar(0)
//	gogui.Label("").BindTextInt(count, func(n int) string { return strconv.Itoa(n) })
//	gogui.Button("+1").OnClick(func(c *Ctx) { count.Set(count.Get() + 1) })
//	gogui.Slider(0, 100).BindSlider(v)        // 双向：拖动写回 Var
//	gogui.Progress(0).BindLevel(ratio)        // 读方向：Var → 控件
type Var[T comparable] struct {
	mu  sync.Mutex
	v   T
	app *App
}

// NewVar 创建响应式值（类型推断）。
func NewVar[T comparable](v T) *Var[T] { return &Var[T]{v: v} }

// Get 读取当前值。若处于绑定注册的依赖收集窗口，会把自己登记进当前控件
// 的依赖集（v6 自动追踪：调用侧无需 AttachAll）。
func (vr *Var[T]) Get() T {
	vr.mu.Lock()
	v := vr.v
	vr.mu.Unlock()
	if depCollecting.Load() {
		collectDep(vr)
	}
	return v
}

// Set 写入并触发重渲染（值未变化时不触发）。
func (vr *Var[T]) Set(v T) {
	vr.mu.Lock()
	if vr.v == v {
		vr.mu.Unlock()
		return
	}
	vr.v = v
	app := vr.app
	vr.mu.Unlock()
	if app != nil {
		app.Invalidate()
	}
}

// mutate 在值锁内完成「读-改-写」（原子 RMW），避免并发丢更新；
// 锁外通知，避免与 invalidation 队列互相等待。
func (vr *Var[T]) mutate(f func(T) T) {
	vr.mu.Lock()
	old := vr.v
	nv := f(old)
	if nv == old {
		vr.mu.Unlock()
		return
	}
	vr.v = nv
	app := vr.app
	vr.mu.Unlock()
	if app != nil {
		app.Invalidate()
	}
}

// Inc 整数自增并返回新值：Inc(count)。原子（并发 Inc 不丢更新）。
func Inc(v *Var[int]) int {
	var out int
	v.mutate(func(n int) int { out = n + 1; return out })
	return out
}

// Add 数值累加（int/float64）：Add(count, -1) / Add(ratio, 0.05)。原子。
func Add[T int | float64](v *Var[T], d T) {
	v.mutate(func(x T) T { return x + d })
}

// Flip 翻转布尔并返回新值：on := Flip(dark)（Toggle 名字留给控件构造器）。原子。
func Flip(v *Var[bool]) bool {
	var out bool
	v.mutate(func(b bool) bool { out = !b; return out })
	return out
}

// Update 以当前值计算并写回（原子 RMW）。
func (vr *Var[T]) Update(f func(T) T) { vr.mutate(f) }

// attach 由 App.SetRoot 在挂树时调用（绑定注册）。
func (vr *Var[T]) attach(a *App) {
	vr.mu.Lock()
	vr.app = a
	vr.mu.Unlock()
}

// Attach 显式绑定到 App：变量即使只被 BindFunc 闭包读取（未直接 Bind*），
// Set 也能触发重渲染。构建树后调用：app := New(...); v.Attach(app)。
func (vr *Var[T]) Attach(a *App) { vr.attach(a) }

// Ratio 是 0..1 的比例语义类型。与裸 float64 严格区分：
// 百分比展示（MetricPct/TextPct）只接受 *Var[Ratio]，裸 float64 无法传入，
// 「把 0.37 用 %.0f%% 格式化成 0%」这类口径错配在编译期即被杜绝。
type Ratio float64

// Percent 返回 0..100 的百分比数值。
func (r Ratio) Percent() float64 { return float64(r) * 100 }

// RatioOf 把裸 float64 显式转成 Ratio（写出来就是一次语义声明）。
func RatioOf(f float64) Ratio { return Ratio(f) }

// 常用类型别名。
type (
	StringVar = Var[string]
	FloatVar  = Var[float64]
	BoolVar   = Var[bool]
	IntVar    = Var[int]
	RatioVar  = Var[Ratio]
)

// ---- 控件绑定 API ----

// BindText 文本绑定（Label/Button/Badge）：Var[string] 直接作为文本。
func (w *Widget) BindText(v *Var[string]) *Widget { return w.BindTextF(v, nil) }

// BindFunc 文本绑定到任意取值函数（派生文本）。
// v6：注册时求值一次并自动收集闭包内读到的 Var —— 无需 AttachAll / Depends；
// 仅分支盲区需要 .Depends 补依赖。
func (w *Widget) BindFunc(fn func() string) *Widget {
	w.trackDeps(func() { _ = fn() })
	w.textSrc = fn
	return w
}

// BindTextF 文本绑定 + 格式化：f 接受 func(string) string 或 printf 格式串（如 "前缀 %s"）。
func (w *Widget) BindTextF(v *Var[string], f any) *Widget {
	var fn func(string) string
	switch t := f.(type) {
	case nil:
		fn = nil
	case string:
		fn = func(s string) string { return fmt.Sprintf(t, s) }
	case func(string) string:
		fn = t
	default:
		panic("gogui.BindTextF: 格式化参数须为 nil / string / func(string) string")
	}
	w.textSrc = func() string {
		s := v.Get()
		if fn != nil {
			return fn(s)
		}
		return s
	}
	w.addAttach(v.attach)
	return w
}

// BindTextFloat 数值→文本绑定（如进度百分比、容量）。
// f 接受 printf 格式串（"%.0f%%"）或 func(float64) string；省略时用通用数值格式。
func (w *Widget) BindTextFloat(v *Var[float64], f ...any) *Widget {
	format := func(x float64) string { return FormatFloat(x) }
	if len(f) > 0 && f[0] != nil {
		switch t := f[0].(type) {
		case string:
			format = func(x float64) string { return fmt.Sprintf(t, x) }
		case func(float64) string:
			format = t
		default:
			panic("gogui.BindTextFloat: 格式化参数须为 string / func(float64) string")
		}
	}
	w.textSrc = func() string { return format(v.Get()) }
	if w.Kind == KindSlider {
		// 同一 Var 同时承担「数值源 + 写回」：拖动不会静默丢失。
		w.sliderNumSrc = func() float64 { return v.Get() }
		w.sliderSink = func(f float64) { v.Set(f) }
	}
	w.addAttach(v.attach)
	return w
}

// BindTextInt 整数→文本绑定（如计数、协程数）。
// f 接受 printf 格式串（"%d 个"）或 func(int) string；省略时用十进制。
func (w *Widget) BindTextInt(v *Var[int], f ...any) *Widget {
	format := func(n int) string { return strconv.Itoa(n) }
	if len(f) > 0 && f[0] != nil {
		switch t := f[0].(type) {
		case string:
			format = func(n int) string { return fmt.Sprintf(t, n) }
		case func(int) string:
			format = t
		default:
			panic("gogui.BindTextInt: 格式化参数须为 string / func(int) string")
		}
	}
	w.textSrc = func() string { return format(v.Get()) }
	w.addAttach(v.attach)
	return w
}

// BindLevel 进度条绑定（0..1，渲染时自动夹紧）。
func (w *Widget) BindLevel(v *Var[float64]) *Widget {
	w.levelSrc = func() float64 { return v.Get() }
	w.addAttach(v.attach)
	return w
}

// BindChecked 开关绑定（双向：读展示 + 用户点击写回 Var）。
func (w *Widget) BindChecked(v *Var[bool]) *Widget {
	w.checkedSrc = func() bool { return v.Get() }
	w.checkedSink = func(b bool) { v.Set(b) }
	w.addAttach(v.attach)
	return w
}

// BindValue 输入框绑定（双向：读展示 + 输入写回 Var）。
func (w *Widget) BindValue(v *Var[string]) *Widget {
	w.valueSrc = func() string { return v.Get() }
	w.valueSink = func(s string) { v.Set(s) }
	w.addAttach(v.attach)
	return w
}

// BindSlider 滑杆绑定（双向：读展示 + 拖动写回 Var）。
func (w *Widget) BindSlider(v *Var[float64]) *Widget {
	w.sliderNumSrc = func() float64 { return v.Get() }
	w.sliderSink = func(f float64) { v.Set(f) }
	w.addAttach(v.attach)
	return w
}

func (w *Widget) addAttach(fn func(*App)) {
	w.attachFns = append(w.attachFns, fn)
}

// ---- 解析器：渲染 / 快照统一从这里取值（绑定优先于静态 Props） ----
// 锁前提：以下 TextValue/CheckedValue/LevelValue/ValueText/SliderValue 会读
// Widget 内部状态（Props/闭包），必须在 App.View 读锁、事件处理器或 Update
// 窗口内调用；裸 goroutine 直接调用与 Dispatch 并发会触发 map 并发读写 fatal。
// App 侧的安全入口：App.ResolveText/ResolveValue/ResolveChecked/ResolveLevel。

// TextValue 返回 Label/Button/Badge 当前文本。
func (w *Widget) TextValue() string {
	if w.textSrc != nil {
		return w.textSrc()
	}
	s, _ := w.Props[PropText].(string)
	return s
}

// CheckedValue 返回开关当前状态。
func (w *Widget) CheckedValue() bool {
	if w.checkedSrc != nil {
		return w.checkedSrc()
	}
	b, _ := w.Props[PropChecked].(bool)
	return b
}

// LevelValue 返回进度条当前进度（0..1）。
func (w *Widget) LevelValue() float64 {
	if w.levelSrc != nil {
		return w.levelSrc()
	}
	return propFloatAny(w.Props[PropLevel])
}

// ValueText 返回输入框当前文本。
func (w *Widget) ValueText() string {
	if w.valueSrc != nil {
		return w.valueSrc()
	}
	s, _ := w.Props[PropValue].(string)
	return s
}

// SliderValue 返回滑杆当前数值。
func (w *Widget) SliderValue() float64 {
	if w.sliderNumSrc != nil {
		return w.sliderNumSrc()
	}
	return propFloatAny(w.Props[PropValue])
}

// propFloatAny 把 Props 里的数值形态统一成 float64。
func propFloatAny(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case string:
		f, _ := strconv.ParseFloat(t, 64)
		return f
	}
	return 0
}

// FormatFloat 是框架统一的数值格式化口径（渲染/绑定共用）。
func FormatFloat(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
