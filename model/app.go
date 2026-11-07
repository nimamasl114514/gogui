package model

import (
	"fmt"
	"runtime/debug"
	"strconv"
	"strings"
)

// Event 是控件事件的统一信封。浏览器 JS 桥、无头 harness、Go 侧代码
// 都把交互折叠成 Event 汇入 App.Dispatch —— 三条通道同一链路，天然一致。
type Event struct {
	WidgetID string  `json:"widget_id"`
	Type     string  `json:"type"`  // click | change | input
	Value    string  `json:"value"` // 文本 / "true|false" / 数值 / 选项 / 下标
}

// Ctx 是事件处理器内操作应用的句子：读写控件状态、改文本、切页签。
//
// 锁纪律：Ctx 的写助手只在「持树写锁的窗口内」被调用 —— Dispatch（事件）
// 与 Update（Go 侧变更）都已持有写锁，助手内部不再加锁、也不再通知；
// 通知由外层在解锁后统一触发一次，因此「一个事件 = 一次重渲染」。
type Ctx struct {
	App   *App
	Src   *Widget // 事件源；Update 窗口内为 nil
	Event Event
}

// ---- Dispatch：事件链路唯一入口（浏览器 / harness / Go 侧共用） ----

// Dispatch 应用状态变更并调用处理器。一个事件 = 一次 invalidation 通知。
func (a *App) Dispatch(e Event) error {
	a.assertNotReentrant("App.Dispatch")
	a.beginEvent()
	a.lockTree()
	if a.Root == nil {
		a.unlockTree()
		a.finishEvent(false) // 早退也必须关闭事件窗口（否则 depth 泄漏，后续通知全被吞）
		err := fmt.Errorf("app has no root")
		a.recordEvent(e, err)
		return err
	}
	w := a.Root.FindByID(e.WidgetID)
	if w == nil {
		a.unlockTree()
		a.finishEvent(false) // 同上
		err := fmt.Errorf("widget %q not found", e.WidgetID)
		a.recordEvent(e, err)
		return err
	}
	ctx := &Ctx{App: a, Src: w, Event: e}
	ok, panicked := safeApply(a, w, ctx, e)
	a.unlockTree()

	if panicked != nil {
		a.finishEvent(false)
		a.recordEvent(e, panicked)
		return panicked
	}
	if !ok {
		a.finishEvent(false)
		// click 到非可点击控件不算错误链路：允许 probe，返回 not-applicable。
		err := fmt.Errorf("event %q not applicable to widget %s(%s)", e.Type, e.WidgetID, w.Kind)
		a.recordEvent(e, err)
		return err
	}
	a.finishEvent(true)
	a.recordEvent(e, nil)
	return nil
}

// beginEvent/endEvent 维护「事件窗口」深度：窗口内 Var.Set 的 invalidation
// 排队合并，最外层窗口关闭时统一通知一次。
//
// 为什么是计数不是布尔（真 bug 修复）：并发 Dispatch A/B 的窗口会交错
// ——A 先结束若把布尔清掉，B 处理器里的 Var.Set 就会误判「不在窗口内」
// 而直接 notify；此时 B 自己正持树写锁，notify 取读锁 → 自死锁。
// 计数保证只要还有任何窗口开着，Set 都只排队。
func (a *App) beginEvent() {
	a.evMu.Lock()
	a.eventDepth++
	a.evMu.Unlock()
}

func (a *App) endEvent() { a.finishEvent(true) }

// finishEvent 关闭一层事件窗口；最外层关闭且（请求通知或存在排队）时统一通知一次
// （事件处理器无论走 Props 还是走 Var，最终都只渲染一次）。
func (a *App) finishEvent(notify bool) {
	a.evMu.Lock()
	if a.eventDepth > 0 {
		a.eventDepth--
	}
	fire := a.eventDepth == 0 && (notify || a.pendingNt)
	if a.eventDepth == 0 {
		a.pendingNt = false
	}
	a.evMu.Unlock()
	if fire {
		a.notify()
	}
}

// Batch 把一组 Go 侧变更合并为一次重渲染（如采样循环一次写多个 Var）：
//
//	app.Batch(func() {
//	    v.CPU.Set(busy)
//	    v.Mem.Set(used)
//	    v.Clock.Set(now)
//	})   // 只触发一次渲染/SSE 推送
func (a *App) Batch(fn func()) {
	a.beginEvent()
	defer a.endEvent() // panic 也要关闭窗口：否则 depth 泄漏，后续通知全被吞
	fn()
}

// Invalidate 请求整树重渲染。事件窗口内排队，窗口外立即通知。
func (a *App) Invalidate() {
	a.evMu.Lock()
	if a.eventDepth > 0 {
		a.pendingNt = true
		a.evMu.Unlock()
		return
	}
	a.evMu.Unlock()
	a.notify()
}

// safeApply 执行 applyEvent 并兜住处理器 panic：
// panic 转成 error 上报（failed 事件 + 日志），进程不退出。
// 锁纪律：recover 发生在函数内部，控制权正常返回 Dispatch ——
// 解锁只由 Dispatch 做一次（此处绝不能解锁，否则 RWMutex 双解锁致命错误）。
func safeApply(a *App, w *Widget, ctx *Ctx, e Event) (ok bool, panErr error) {
	defer func() {
		if r := recover(); r != nil {
			panErr = fmt.Errorf("handler panic: %v\n%s", r, debug.Stack())
		}
	}()
	return applyEvent(w, ctx), nil
}

// applyEvent 按控件种类应用事件（调用方持树写锁）。
func applyEvent(w *Widget, ctx *Ctx) bool {
	switch w.Kind {
	case KindButton:
		if ctx.Event.Type == "click" {
			fireHandlers(w.onClick, ctx)
			return true
		}
	case KindInput, KindTextarea:
		if ctx.Event.Type == "change" || ctx.Event.Type == "input" {
			if w.valueSrc != nil {
				// 绑定优先：写回 Var（Props 不再作为事实源）。
			} else {
				w.Props[PropValue] = ctx.Event.Value
			}
			if w.valueSink != nil {
				w.valueSink(ctx.Event.Value)
			}
			fireHandlers(w.onChange, ctx)
			return true
		}
	case KindToggle:
		// 浏览器桥发 change（携带新状态）；无头手势发 click（翻转语义）。
		if ctx.Event.Type == "change" {
			on := ctx.Event.Value == "true"
			if w.checkedSrc == nil {
				w.Props[PropChecked] = on
			}
			if w.checkedSink != nil {
				w.checkedSink(on)
			}
			fireHandlers(w.onChange, ctx)
			return true
		}
		if ctx.Event.Type == "click" {
			on := !w.CheckedValue()
			if w.checkedSrc == nil {
				w.Props[PropChecked] = on
			}
			if w.checkedSink != nil {
				w.checkedSink(on)
			}
			fireHandlers(w.onChange, ctx)
			return true
		}
	case KindSlider:
		if ctx.Event.Type == "change" || ctx.Event.Type == "input" {
			if v, err := strconv.ParseFloat(ctx.Event.Value, 64); err == nil {
				if w.sliderNumSrc == nil {
					w.Props[PropValue] = v
				}
				if w.sliderSink != nil {
					w.sliderSink(v)
				}
				fireHandlers(w.onChange, ctx)
				return true
			}
		}
	case KindSelect:
		if ctx.Event.Type == "change" {
			w.Props[PropSelected] = ctx.Event.Value
			fireHandlers(w.onChange, ctx)
			return true
		}
	case KindTabs:
		if ctx.Event.Type == "click" {
			if idx, err := strconv.Atoi(ctx.Event.Value); err == nil {
				tabs, _ := w.Props[PropTabs].([]Tab)
				if idx >= 0 && idx < len(tabs) {
					w.Props[PropActiveTab] = idx
					fireHandlers(w.onClick, ctx) // OnClick(func(int)) 语义：点击即触发
					fireHandlers(w.onChange, ctx)
					return true
				}
			}
		}
	case KindList:
		if ctx.Event.Type == "click" {
			if idx, err := strconv.Atoi(ctx.Event.Value); err == nil {
				items, _ := w.Props[PropItems].([]string)
				if idx >= 0 && idx < len(items) {
					w.Props[PropSelectedIdx] = idx
					fireHandlers(w.onClick, ctx)
					fireHandlers(w.onChange, ctx)
					return true
				}
			}
		}
	}
	return false
}

// fireHandlers 按处理器签名分派（见 OnClick/OnChange 文档）。
func fireHandlers(list []any, ctx *Ctx) {
	for _, h := range list {
		switch fn := h.(type) {
		case func(*Ctx):
			fn(ctx)
		case func():
			fn()
		case func(string):
			fn(ctx.Event.Value)
		case func(bool):
			fn(ctx.Event.Value == "true")
		case func(float64):
			if f, err := strconv.ParseFloat(ctx.Event.Value, 64); err == nil {
				fn(f)
			}
		case func(int):
			if n, err := strconv.Atoi(ctx.Event.Value); err == nil {
				fn(n)
			}
		}
	}
}

// Update 是 Go 侧（定时器、网络回调、初始化）修改树的统一入口：
//   - 持树写锁执行 mutate，解锁后触发一次 invalidation；
//   - 与浏览器事件共享同一套 Ctx 助手，Go 侧改状态 = 浏览器事件改状态。
func (a *App) Update(mutate func(c *Ctx)) {
	a.assertNotReentrant("App.Update")
	a.beginEvent()
	defer a.endEvent()          // LIFO：最后执行
	a.lockTree()
	defer a.unlockTree() // LIFO：先执行 —— mutate panic 时锁必须释放（否则进程级死锁）
	defer func() {
		if r := recover(); r != nil {
			// Go 侧回调 panic：记录失败，不让定时器/回调线程崩溃。
			// 注意：若 panic 发生在 mutate 中途，树可能处于部分变更状态。
			a.stats.failures.Add(1)
			a.stats.mu.Lock()
			a.stats.lastErr = fmt.Sprintf("update panic: %v", r)
			a.stats.mu.Unlock()
		}
	}()
	mutate(&Ctx{App: a})
}

// ---- Ctx 读助手 ----

func (c *Ctx) widget(id string) *Widget {
	if c.Src != nil && c.Src.ID == id {
		return c.Src
	}
	if c.App.Root == nil {
		return nil
	}
	return c.App.Root.FindByID(id)
}

// Widget 按 ID 取控件（在 Dispatch/Update 窗口内使用）。
func (c *Ctx) Widget(id string) *Widget { return c.widget(id) }

// Prop 读取控件属性。
func (c *Ctx) Prop(id, key string) any {
	if w := c.widget(id); w != nil {
		return w.Props[key]
	}
	return nil
}

// Value 读取 Input/Textarea/Slider 当前值（走绑定解析：绑定的控件也能读到真值）。
func (c *Ctx) Value(id string) string {
	if w := c.widget(id); w != nil {
		return w.ValueText()
	}
	return ""
}

// FloatValue 读取 Slider 数值（非数值返回 0；走绑定解析）。
func (c *Ctx) FloatValue(id string) float64 {
	if w := c.widget(id); w != nil {
		return w.SliderValue()
	}
	return 0
}

// Int 读取控件的整型值：优先解析文本（Label 计数器场景），其次解析数值属性。
func (c *Ctx) Int(id string) int {
	if w := c.widget(id); w != nil {
		if s, ok := w.Props[PropText].(string); ok {
			n, _ := strconv.Atoi(strings.TrimSpace(s))
			return n
		}
		switch v := w.Props[PropValue].(type) {
		case float64:
			return int(v)
		case string:
			n, _ := strconv.Atoi(strings.TrimSpace(v))
			return n
		}
	}
	return 0
}

// Checked 读取开关状态（走绑定解析）。
func (c *Ctx) Checked(id string) bool {
	if w := c.widget(id); w != nil {
		return w.CheckedValue()
	}
	return false
}

// Selected 读取 Select 当前选项。
func (c *Ctx) Selected(id string) string {
	if w := c.widget(id); w != nil {
		v, _ := w.Props[PropSelected].(string)
		return v
	}
	return ""
}

// SelectedIndex 读取 List 当前选中下标（-1 无选中）。
func (c *Ctx) SelectedIndex(id string) int {
	if w := c.widget(id); w != nil {
		v, _ := w.Props[PropSelectedIdx].(int)
		return v
	}
	return -1
}

// ActiveTab 读取 Tabs 当前页下标。
func (c *Ctx) ActiveTab(id string) int {
	if w := c.widget(id); w != nil {
		v, _ := w.Props[PropActiveTab].(int)
		return v
	}
	return 0
}

// Text 读取 Label/Button/Badge 文本（走绑定解析）。
func (c *Ctx) Text(id string) string {
	if w := c.widget(id); w != nil {
		return w.TextValue()
	}
	return ""
}

// ---- Ctx 写助手（仅限 Dispatch/Update 窗口内；不加锁、不通知） ----

func (c *Ctx) setProp(id, key string, val any) {
	if w := c.widget(id); w != nil {
		w.Props[key] = val
	}
}

// SetValue 修改 Input/Textarea 文本。
func (c *Ctx) SetValue(id, v string) { c.setProp(id, PropValue, v) }

// SetSliderValue 修改滑杆数值。
func (c *Ctx) SetSliderValue(id string, v float64) { c.setProp(id, PropValue, v) }

// SetChecked 修改开关。
func (c *Ctx) SetChecked(id string, on bool) { c.setProp(id, PropChecked, on) }

// SetSelected 修改下拉选中项。
func (c *Ctx) SetSelected(id, opt string) { c.setProp(id, PropSelected, opt) }

// SetText 修改 Label/Button/Badge 文本。
func (c *Ctx) SetText(id, text string) { c.setProp(id, PropText, text) }

// SetInt 把整数写入 Label 文本（计数器场景）。
func (c *Ctx) SetInt(id string, n int) { c.setProp(id, PropText, strconv.Itoa(n)) }

// SetLevel 修改进度条进度（0..1，自动夹紧）。
func (c *Ctx) SetLevel(id string, level float64) {
	if level < 0 {
		level = 0
	} else if level > 1 {
		level = 1
	}
	c.setProp(id, PropLevel, level)
}

// SetActiveTab 切换页签（带范围校验）。
func (c *Ctx) SetActiveTab(id string, idx int) {
	if w := c.widget(id); w != nil {
		tabs, _ := w.Props[PropTabs].([]Tab)
		if idx >= 0 && idx < len(tabs) {
			c.setProp(id, PropActiveTab, idx)
		}
	}
}

// SetSelectedIndex 修改列表选中下标。
func (c *Ctx) SetSelectedIndex(id string, idx int) { c.setProp(id, PropSelectedIdx, idx) }

// SetDisabled 切换控件禁用态。
func (c *Ctx) SetDisabled(id string, dis bool) { c.setProp(id, PropDisabled, dis) }

// SetProp 通用属性写入逃生舱（运行期换图/换文案/任意 Props）。
// 例：c.SetProp("art", model.PropSrc, dataURI)
func (c *Ctx) SetProp(id, key string, val any) { c.setProp(id, key, val) }

// AddClass / RemoveClass 运行期增删样式类（切换动画状态、选中态等）。
// 例：c.AddClass("disc", "spin-off") 让 CSS 动画暂停。
func (c *Ctx) AddClass(id, cls string) {
	if w := c.widget(id); w != nil {
		for _, existing := range w.Classes {
			if existing == cls {
				return
			}
		}
		w.Classes = append(w.Classes, cls)
	}
}

// RemoveClass 移除样式类。
func (c *Ctx) RemoveClass(id, cls string) {
	if w := c.widget(id); w != nil {
		out := w.Classes[:0]
		for _, existing := range w.Classes {
			if existing != cls {
				out = append(out, existing)
			}
		}
		w.Classes = out
	}
}

// ---- 线程安全的树读取（presenter / server / harness 用） ----

// View 在树读锁内执行 fn(root)，用于渲染、快照、查询等只读操作。
// 不能在事件处理器 / Update 窗口内调用（会 panic：树写锁不可重入）。
func (a *App) View(fn func(root *Widget)) {
	a.assertNotReentrant("App.View")
	a.treeMu.RLock()
	defer a.treeMu.RUnlock()
	if a.Root != nil {
		fn(a.Root)
	}
}

// LookupWidget 线程安全地按 ID 查找控件，返回 nil 表示未命中。
// 返回的控件指针仅只读使用；要修改请走 Update/Dispatch。
func (a *App) LookupWidget(id string) *Widget {
	a.assertNotReentrant("App.LookupWidget")
	var hit *Widget
	a.View(func(root *Widget) { hit = root.FindByID(id) })
	return hit
}

// notify 必须在无锁状态调用（拷贝订阅者后逐个回调）。
//
// 防重入：订阅者内部再次触发 Var.Set → Invalidate → notify 时，不递归执行，
// 而是置 dirty 由外层循环再跑一轮（实测递归可在 10 万层后爆栈）。
// 每个订阅者独立 recover：单个订阅者 panic 不中断其余订阅者。
func (a *App) notify() {
	a.notifyMu.Lock()
	if a.notifying {
		a.notifyDirty = true
		a.notifyMu.Unlock()
		return
	}
	a.notifying = true
	a.notifyMu.Unlock()

	for {
		a.treeMu.RLock()
		handlers := make([]func(), len(a.handlers))
		copy(handlers, a.handlers)
		a.treeMu.RUnlock()
		for _, h := range handlers {
			func() {
				defer func() { _ = recover() }()
				h()
			}()
		}
		a.notifyMu.Lock()
		if !a.notifyDirty {
			a.notifying = false
			a.notifyMu.Unlock()
			return
		}
		a.notifyDirty = false
		a.notifyMu.Unlock()
	}
}

// ---- 树锁重入守卫（P0：RWMutex 不可重入，处理器内调 App 只读入口会永久挂死） ----

// lockTree/unlockTree 记录写锁持有者的 goroutine 编号。
func (a *App) lockTree() {
	a.treeMu.Lock()
	a.lockMu.Lock()
	a.lockOwner = goid()
	a.lockMu.Unlock()
}

func (a *App) unlockTree() {
	a.lockMu.Lock()
	a.lockOwner = 0
	a.lockMu.Unlock()
	a.treeMu.Unlock()
}

// assertNotReentrant 在 App 锁入口做 fail-fast：
// 若当前 goroutine 正是树写锁持有者（即正处于事件处理器 / Update 窗口内），
// sync.RWMutex 的二次加锁必然永久挂死 —— 直接 panic 把 5 分钟静默挂死
// 变成 5 秒堆栈报错。处理器内请使用 Ctx 助手。
func (a *App) assertNotReentrant(where string) {
	a.lockMu.Lock()
	owner := a.lockOwner
	a.lockMu.Unlock()
	if owner != 0 && owner == goid() {
		panic("gogui: " + where + " 不能在事件处理器/Update 窗口内调用（树写锁不可重入）；" +
			"请用 Ctx 助手读写控件，把跨控件逻辑移到处理器外")
	}
}

// ---- App 级安全读取入口（外部 goroutine 用；内部会持树读锁） ----

// ResolveText 线程安全读取 Label/Button/Badge 文本。
func (a *App) ResolveText(id string) (string, bool) {
	var v string
	found := false
	a.View(func(root *Widget) {
		if w := root.FindByID(id); w != nil {
			v, found = w.TextValue(), true
		}
	})
	return v, found
}

// ResolveValue 线程安全读取输入控件值。
func (a *App) ResolveValue(id string) (string, bool) {
	var v string
	found := false
	a.View(func(root *Widget) {
		if w := root.FindByID(id); w != nil {
			v, found = w.ValueText(), true
		}
	})
	return v, found
}

// ResolveChecked 线程安全读取开关状态。
func (a *App) ResolveChecked(id string) (bool, bool) {
	var v bool
	found := false
	a.View(func(root *Widget) {
		if w := root.FindByID(id); w != nil {
			v, found = w.CheckedValue(), true
		}
	})
	return v, found
}

// ResolveLevel 线程安全读取进度条进度。
func (a *App) ResolveLevel(id string) (float64, bool) {
	var v float64
	found := false
	a.View(func(root *Widget) {
		if w := root.FindByID(id); w != nil {
			v, found = w.LevelValue(), true
		}
	})
	return v, found
}
