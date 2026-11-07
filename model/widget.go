// Package model 是 gogui 的控件树模型层：单一事实源（Source of Truth）。
//
// 设计约束：
//   - 纯 Go，零第三方依赖，零窗口依赖 —— 可在无浏览器/无显示环境完整运行与测试；
//   - 控件树不可被 UI 层直接改写，一切状态变更走 App.Dispatch / Ctx 变更助手，
//     保证「无头操作控件」与「真实浏览器操作」走同一条事件与状态链路；
//   - 所有序列化输出（HTML / 树快照）均为确定性输出：Props 按键排序渲染。
package model

import (
	"sync"
	"time"
)

// Kind 枚举全部控件种类。快照与 HTML 渲染都以 Kind 分派。
type Kind string

const (
	KindApp      Kind = "app"
	KindVStack   Kind = "vstack"
	KindHStack   Kind = "hstack"
	KindSpacer   Kind = "spacer"
	KindCard     Kind = "card"
	KindLabel    Kind = "label"
	KindButton   Kind = "button"
	KindInput    Kind = "input"
	KindTextarea Kind = "textarea"
	KindToggle   Kind = "toggle"
	KindSlider   Kind = "slider"
	KindProgress Kind = "progress"
	KindSelect   Kind = "select"
	KindBadge    Kind = "badge"
	KindTabs     Kind = "tabs"
	KindList     Kind = "list"
	KindImage    Kind = "image"
	KindSep      Kind = "sep"
)

// 内部使用的 Props 键（外部通过 fluent API / Ctx 访问，不直接拼字符串）。
const (
	PropText        = "text"
	PropValue       = "value"   // input 文本 / slider 数值
	PropPlaceholder = "placeholder"
	PropChecked     = "checked" // toggle
	PropDisabled    = "disabled"
	PropMin         = "min"
	PropMax         = "max"
	PropStep        = "step"
	PropLevel       = "level" // progress 0..1
	PropOptions     = "options"
	PropSelected    = "selected"
	PropSrc         = "src"
	PropAlt         = "alt"
	PropGap         = "gap"
	PropAlign       = "align"
	PropJustify     = "justify"
	PropPadding     = "padding"
	PropTabs        = "tabs"
	PropActiveTab   = "activetab"
	PropItems       = "items"
	PropSelectedIdx = "selidx"
	PropVariant     = "variant" // button: primary/secondary/danger/ghost
	PropFlex        = "flex"    // 容器匿名 flex 增长（Grow）
	PropStyle       = "style"   // 内联样式逃生舱（Style，追加到 style 属性）
	PropWidth       = "width"   // 期望宽度 px（Width）
)

// Widget 是所有控件的统一载体：Kind + 有序可读的 Props + Children。
// fluent 构造器见 builders.go；状态变更见 app.go（Ctx 助手）。
type Widget struct {
	Kind     Kind
	ID       string
	Classes  []string
	Props    map[string]any
	Children []*Widget

	parent *Widget

	onClick  []any // func() / func(*Ctx) / func(string) / func(bool) / func(float64) / func(int)
	onChange []any

	// 响应式绑定（见 binding.go）：读方向 src，写方向 sink。
	textSrc      func() string
	levelSrc     func() float64
	checkedSrc   func() bool
	valueSrc     func() string
	sliderNumSrc func() float64
	checkedSink  func(bool)
	valueSink    func(string)
	sliderSink   func(float64)
	attachFns    []func(*App)
}

// Parent 返回父控件（根返回 nil）。
func (w *Widget) Parent() *Widget { return w.parent }

// HasClickHandlers / HasChangeHandlers 供快照序列化标注 handler 存在性。
func (w *Widget) HasClickHandlers() bool  { return len(w.onClick) > 0 }
func (w *Widget) HasChangeHandlers() bool { return len(w.onChange) > 0 }

func newWidget(k Kind, props map[string]any, children ...*Widget) *Widget {
	if props == nil {
		props = map[string]any{}
	}
	w := &Widget{Kind: k, Props: props, Children: children}
	for _, c := range children {
		c.parent = w
	}
	return w
}

// Add 向容器追加子控件（运行期动态加子树时使用；常规构建期用构造器嵌套）。
// 自环/祖先环直接 panic（否则 Walk 无限递归 → 栈溢出）。
func (w *Widget) Add(children ...*Widget) *Widget {
	for _, c := range children {
		if c == nil {
			continue
		}
		for p := w; p != nil; p = p.parent {
			if p == c {
				panic("gogui.Widget.Add: 形成父子环（把祖先挂到自己后代下）会导致无限递归")
			}
		}
		c.parent = w
		w.Children = append(w.Children, c)
	}
	return w
}

// Walk 以确定性的先序遍历访问全部控件（含根）。返回 false 中止。
func (w *Widget) Walk(visit func(cur *Widget) bool) {
	if !visit(w) {
		return
	}
	for _, c := range w.Children {
		c.Walk(visit)
	}
}

// FindByID 在子树内按 ID 查找控件（先序首个命中）。
func (w *Widget) FindByID(id string) *Widget {
	var hit *Widget
	w.Walk(func(cur *Widget) bool {
		if hit != nil {
			return false
		}
		if cur.ID == id {
			hit = cur
			return false
		}
		return true
	})
	return hit
}

// ---- fluent 通用设置 ----

// WithID 设置控件 ID（事件寻址、测试 harness、golden 树快照都以 ID 为锚点）。
func (w *Widget) WithID(id string) *Widget { w.ID = id; return w }

// Class 追加样式类（视觉变体、排版级别等，见 theme 设计系统）。
func (w *Widget) Class(c ...string) *Widget { w.Classes = append(w.Classes, c...); return w }

// Disabled 将控件置为不可用态。
func (w *Widget) Disabled() *Widget { w.Props[PropDisabled] = true; return w }

// OnClick 注册点击处理器。支持多种签名（按需取用，无需 Ctx 时零样板）：
//
//	.OnClick(func())                  // 无参：最常用
//	.OnClick(func(c *Ctx))            // 需要读写控件时
//	.OnClick(func(s string))          // Tabs/List：拿到 index 文本
//	.OnClick(func(i int))             // Tabs/List：拿到下标
//
// 不支持的签名在注册时 panic（fail fast）。
func (w *Widget) OnClick(fn any) *Widget {
	checkHandler(fn, "OnClick")
	w.onClick = append(w.onClick, fn)
	return w
}

// OnChange 注册值变更处理器。支持多种签名：
//
//	.OnChange(func())                 // 无参
//	.OnChange(func(c *Ctx))           // 全上下文
//	.OnChange(func(on bool))          // Toggle
//	.OnChange(func(text string))      // Input/Textarea/Select
//	.OnChange(func(v float64))        // Slider
//	.OnChange(func(i int))            // Slider（取整）/Tabs/List
func (w *Widget) OnChange(fn any) *Widget {
	checkHandler(fn, "OnChange")
	w.onChange = append(w.onChange, fn)
	return w
}

// checkHandler 注册期校验处理器签名（fail fast，避免运行期才发现）。
func checkHandler(fn any, where string) {
	switch fn.(type) {
	case func(), func(*Ctx), func(string), func(bool), func(float64), func(int):
		// ok
	default:
		panic("gogui." + where + ": 不支持的处理器签名（可用 func() / func(*Ctx) / func(string) / func(bool) / func(float64) / func(int)）")
	}
}

// Size 设置窗口/容器尺寸类控件的期望尺寸（px）。
type Size struct{ W, H int }

// Tab 是 Tabs 控件的单页定义。
type Tab struct {
	Title   string
	Content *Widget
}

// App 是一次 gogui 应用的根：持有控件树、主题与事件分发。
// 所有状态变更（浏览器事件、无头 harness、Go 侧定时器）都汇入 Dispatch，
// 这是「无头可测」的结构性保证：两条通道无法漂移。
type App struct {
	Title string
	Size  Size
	Root  *Widget

	treeMu   sync.RWMutex // 树读写锁：Dispatch/Update 持写锁，渲染/快照持读锁
	lockMu   sync.Mutex   // 保护 lockOwner（树写锁持有者的 goroutine 编号）
	lockOwner uint64      // 0 = 无人持有；用于重入 fail-fast 守卫
	handlers []func()      // invalidation 订阅者（server / presenter）
	notifyMu    sync.Mutex // notify 重入合并
	notifying   bool
	notifyDirty bool
	mem         memCache // ReadMemStats 1s 缓存（stats.go）
	stats    statsState    // 运行时计数器（可观测性，见 stats.go）

	// 事件窗口内的 invalidation 排队（防重入 notify → 死锁）。
	// eventDepth 是嵌套计数而非布尔：并发 Dispatch/Update/Batch 的窗口
	// 可以交错，计数保证「只要还有窗口开着，Var.Set 就只排队不通知」。
	evMu       sync.Mutex
	eventDepth int
	pendingNt  bool
}

// New 创建应用。Options 中未给 Root 时可用 SetRoot 稍后设置。
func New(title string, size Size, root *Widget) *App {
	a := &App{Title: title, Size: size}
	a.stats.createdAt = time.Now()
	a.SetRoot(root)
	return a
}

// SetRoot 设置/替换根控件，并给缺失 ID 的控件补齐确定性自动 ID。
// 自动 ID = "<kind>-<序号>"，序号按先序遍历顺序分配，同一构建顺序 → 同一 ID。
func (a *App) SetRoot(root *Widget) {
	a.treeMu.Lock()
	defer a.treeMu.Unlock()
	a.Root = root
	if root == nil {
		return
	}
	if root.ID == "" {
		root.ID = "root"
	}
	// 先收集显式 ID，自动 ID 跳过已占用者（防撞名：自动 label-1 撞用户显式 label-1）。
	used := map[string]bool{}
	root.Walk(func(cur *Widget) bool {
		if cur.ID != "" {
			used[cur.ID] = true
		}
		return true
	})
	counter := map[Kind]int{}
	root.Walk(func(cur *Widget) bool {
		if cur.ID == "" {
			for {
				counter[cur.Kind]++
				cand := string(cur.Kind) + "-" + itoa(counter[cur.Kind])
				if !used[cand] {
					cur.ID = cand
					used[cand] = true
					break
				}
			}
		}
		return true
	})
	// 绑定注册：把 App 指针交给所有 Var（Set 才能触发 invalidation）。
	root.Walk(func(cur *Widget) bool {
		for _, fn := range cur.attachFns {
			fn(a)
		}
		return true
	})
}

// OnUpdate 订阅「模型已变更」通知（server 用来触发补丁下发）。
func (a *App) OnUpdate(fn func()) {
	a.treeMu.Lock()
	defer a.treeMu.Unlock()
	a.handlers = append(a.handlers, fn)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
