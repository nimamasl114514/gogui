// Package testing 提供 gogui 的无头测试工具集。
//
// 两层测试通道：
//   - L1 harness（本文件）：纯进程内驱动 model —— 零浏览器、零窗口、毫秒级；
//   - L2 browser（browser.go）：chromedp 驱动真实 Chromium —— 覆盖 JS 桥全链路；
//   - golden（golden.go）：像素级 / 树快照级对照测试基线管理。
//
// Harness 与浏览器 JS 桥共用 App.Dispatch 入口：
// 「无头点一下」与「真实鼠标点一下」在模型层是同一件事，测试天然不漂移。
package testing

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/nimamasl114514/gogui/model"
	"github.com/nimamasl114514/gogui/snapshot"
)

// Harness 是进程内无头驱动器：直接向模型注入事件并断言状态。
type Harness struct {
	App *model.App
}

// Open 打开一个无头测试实例（不开窗口、不启浏览器）。
func Open(app *model.App) *Harness {
	return &Harness{App: app}
}

// ---- 操作（与浏览器事件同链路：全部汇入 App.Dispatch） ----

// Click 模拟点击按钮/页签/列表项。对 Tabs/List 可传 value（页下标/项下标）。
func (h *Harness) Click(id string) error {
	return h.dispatch(model.Event{WidgetID: id, Type: "click"})
}

// ClickTabs 模拟点击页签下标。
func (h *Harness) ClickTabs(id string, idx int) error {
	return h.dispatch(model.Event{WidgetID: id, Type: "click", Value: strconv.Itoa(idx)})
}

// ClickList 模拟点击列表项下标。
func (h *Harness) ClickList(id string, idx int) error {
	return h.dispatch(model.Event{WidgetID: id, Type: "click", Value: strconv.Itoa(idx)})
}

// Toggle 模拟点击开关（切换状态）。want 可传期望终态，默认翻转。
func (h *Harness) Toggle(id string) error {
	return h.dispatch(model.Event{WidgetID: id, Type: "click"})
}

// Type 向输入框一次性输入文本（change 事件语义：完整值替换）。
func (h *Harness) Type(id, text string) error {
	return h.dispatch(model.Event{WidgetID: id, Type: "change", Value: text})
}

// Slide 把滑杆拨到指定值。
func (h *Harness) Slide(id string, val float64) error {
	return h.dispatch(model.Event{WidgetID: id, Type: "change", Value: strconv.FormatFloat(val, 'g', -1, 64)})
}

// Choose 选择下拉项。
func (h *Harness) Choose(id, option string) error {
	return h.dispatch(model.Event{WidgetID: id, Type: "change", Value: option})
}

func (h *Harness) dispatch(e model.Event) error {
	return h.App.Dispatch(e)
}

// ---- 断言（失败返回 error，适配 testing.T / 任意 runner） ----

// Widget 按 ID 取控件只读快照（树读锁内）。
func (h *Harness) Widget(id string) *model.Widget {
	return h.App.LookupWidget(id)
}

// MustWidget 同 Widget，未命中时 panic（测试内使用）。
func (h *Harness) MustWidget(id string) *model.Widget {
	w := h.Widget(id)
	if w == nil {
		panic(fmt.Sprintf("goguitest: widget %q not found", id))
	}
	return w
}

// AssertValue 断言输入控件文本值（App 安全入口：内部持读锁 + 绑定解析）。
func (h *Harness) AssertValue(id, want string) error {
	got, found := h.App.ResolveValue(id)
	if !found {
		return fmt.Errorf("widget %q not found", id)
	}
	if got != want {
		return fmt.Errorf("%s value = %q, want %q", id, got, want)
	}
	return nil
}

// AssertValue 断言输入控件文本值（App 安全入口：内部持读锁 + 绑定解析）。

// AssertChecked 断言开关状态。
func (h *Harness) AssertChecked(id string, want bool) error {
	got, found := h.App.ResolveChecked(id)
	if !found {
		return fmt.Errorf("widget %q not found", id)
	}
	if got != want {
		return fmt.Errorf("%s checked = %v, want %v", id, got, want)
	}
	return nil
}

// AssertText 断言 Label/Button/Badge 文本。
func (h *Harness) AssertText(id, want string) error {
	got, found := h.App.ResolveText(id)
	if !found {
		return fmt.Errorf("widget %q not found", id)
	}
	if got != want {
		return fmt.Errorf("%s text = %q, want %q", id, got, want)
	}
	return nil
}

// AssertSelected 断言 Select 当前选项。
func (h *Harness) AssertSelected(id, want string) error {
	w := h.Widget(id)
	if w == nil {
		return fmt.Errorf("widget %q not found", id)
	}
	got, _ := w.Props[model.PropSelected].(string)
	if got != want {
		return fmt.Errorf("%s selected = %q, want %q", id, got, want)
	}
	return nil
}

// AssertActiveTab 断言 Tabs 当前页。
func (h *Harness) AssertActiveTab(id string, want int) error {
	w := h.Widget(id)
	if w == nil {
		return fmt.Errorf("widget %q not found", id)
	}
	got, _ := w.Props[model.PropActiveTab].(int)
	if got != want {
		return fmt.Errorf("%s active = %d, want %d", id, got, want)
	}
	return nil
}

// AssertSelectedIndex 断言 List 当前选中下标。
func (h *Harness) AssertSelectedIndex(id string, want int) error {
	w := h.Widget(id)
	if w == nil {
		return fmt.Errorf("widget %q not found", id)
	}
	got, _ := w.Props[model.PropSelectedIdx].(int)
	if got != want {
		return fmt.Errorf("%s selidx = %d, want %d", id, got, want)
	}
	return nil
}

// AssertLevel 断言进度条进度。
func (h *Harness) AssertLevel(id string, want float64) error {
	got, found := h.App.ResolveLevel(id)
	if !found {
		return fmt.Errorf("widget %q not found", id)
	}
	if got != want {
		return fmt.Errorf("%s level = %v, want %v", id, got, want)
	}
	return nil
}

// ---- 快照 / 对照 ----

// Snapshot 返回控件树确定性文本快照（结构级对照测试的基线载体）。
func (h *Harness) Snapshot() string {
	return snapshot.Tree(h.App)
}

// AssertSnapshotEqual 对照两份快照文本（不落盘版本，见 golden.go 的落盘基线版）。
func (h *Harness) AssertSnapshotEqual(want string) error {
	got := h.Snapshot()
	if got != want {
		return errors.New("snapshot diff:\n--- want ---\n" + want + "\n--- got ---\n" + got)
	}
	return nil
}

// T 是最小测试接口（兼容 *testing.T 与自研 runner）。
type T interface {
	Fatalf(format string, args ...any)
	Logf(format string, args ...any)
	Name() string
	Helper()
}

// Check 把断言结果接到 testing.T：失败即 Fatal。
func Check(t T, err error) {
	if err != nil {
		t.Fatalf("[%s] %v", t.Name(), err)
	}
}
