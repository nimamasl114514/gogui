// Package snapshot 把 model 控件树序列化为确定性的文本快照，
// 用于结构级对照测试（golden tree）。
//
// 格式（每控件一行，先序缩进树）：
//
//	vstack#root gap=16
//	├─ label.h1 text="设置"
//	├─ hstack.card gap=16 padding=20
//	│  ├─ toggle#theme-toggle checked=true on:change
//	│  └─ slider#opacity value=42 min=0 max=100
//
// 规则：值原样落地、Props 按键排序、handler 只标存在性（on:click,change）。
// 同一树状态 → 逐字节相同输出（对照测试的判定基础）。
package snapshot

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/nimamasl114514/gogui/model"
)

// Tree 返回整棵控件树的确定性文本快照。
func Tree(app *model.App) string {
	var b strings.Builder
	app.View(func(root *model.Widget) {
		writeNode(&b, root, "", true, 0)
	})
	return b.String()
}

// Widget 返回单棵子树的快照。
func Widget(w *model.Widget) string {
	var b strings.Builder
	writeNode(&b, w, "", true, 0)
	return b.String()
}

// writeNode 递归写出节点。depth 只影响缩进延伸：根与根的直接子节点不缩进，
// 其后每层按父链延伸「│/空格」前缀，与文档格式一致。
func writeNode(b *strings.Builder, w *model.Widget, prefix string, isLast bool, depth int) {
	if depth > 0 {
		if isLast {
			b.WriteString(prefix + "└─ ")
		} else {
			b.WriteString(prefix + "├─ ")
		}
	}
	b.WriteString(string(w.Kind))
	if w.ID != "" {
		b.WriteString("#" + w.ID)
	}
	for _, c := range w.Classes {
		if c == "" {
			continue // 跳过空类名，避免悬空点号
		}
		b.WriteString("." + c)
	}

	// 语义状态键（顺序固定）。
	if v := w.TextValue(); v != "" {
		b.WriteString(" text=" + strconv.Quote(v))
	}
	if w.Kind == model.KindInput || w.Kind == model.KindTextarea {
		b.WriteString(" value=" + strconv.Quote(w.ValueText()))
		if ph := propStr(w, model.PropPlaceholder); ph != "" {
			b.WriteString(" placeholder=" + strconv.Quote(ph))
		}
	}
	if w.Kind == model.KindToggle {
		b.WriteString(" checked=" + strconv.FormatBool(w.CheckedValue()))
	}
	if w.Kind == model.KindSlider {
		fmt.Fprintf(b, " value=%s min=%s max=%s step=%s",
			ft(w.SliderValue()), ft(propF(w, model.PropMin)),
			ft(propF(w, model.PropMax)), ft(propF(w, model.PropStep)))
	}
	if w.Kind == model.KindProgress {
		b.WriteString(" level=" + ft(w.LevelValue()))
	}
	if opts, ok := w.Props[model.PropOptions].([]string); ok {
		b.WriteString(" options=" + strconv.Quote(strings.Join(opts, "|")))
		if s := propStr(w, model.PropSelected); s != "" {
			b.WriteString(" selected=" + strconv.Quote(s))
		}
	}
	if w.Kind == model.KindTabs {
		tabs, _ := w.Props[model.PropTabs].([]model.Tab)
		titles := make([]string, len(tabs))
		for i, t := range tabs {
			titles[i] = t.Title
		}
		b.WriteString(" tabs=" + strconv.Quote(strings.Join(titles, "|")))
		if a, ok := w.Props[model.PropActiveTab].(int); ok {
			fmt.Fprintf(b, " active=%d", a)
		}
	}
	if w.Kind == model.KindList {
		items, _ := w.Props[model.PropItems].([]string)
		b.WriteString(" items=" + strconv.Quote(strings.Join(items, "|")))
		if a, ok := w.Props[model.PropSelectedIdx].(int); ok {
			fmt.Fprintf(b, " sel=%d", a)
		}
	}
	if v, ok := w.Props[model.PropVariant].(string); ok && v != "" {
		b.WriteString(" variant=" + v)
	}
	if d, ok := w.Props[model.PropDisabled].(bool); ok && d {
		b.WriteString(" disabled")
	}
	// 布局 props（容器）。
	if g := w.Props[model.PropGap]; g != nil {
		b.WriteString(" gap=" + fmtInt(g))
	}
	if p := w.Props[model.PropPadding]; p != nil {
		b.WriteString(" padding=" + fmtInt(p))
	}
	if a := propStr(w, model.PropAlign); a != "" {
		b.WriteString(" align=" + a)
	}
	if j := propStr(w, model.PropJustify); j != "" {
		b.WriteString(" justify=" + j)
	}
	if s, ok := w.Props[model.PropSrc].(string); ok && s != "" {
		b.WriteString(" src=" + strconv.Quote(s))
	}

	// handler 存在性标注。
	var evs []string
	if w.HasClickHandlers() {
		evs = append(evs, "click")
	}
	if w.HasChangeHandlers() {
		evs = append(evs, "change")
	}
	if len(evs) > 0 {
		b.WriteString(" on:" + strings.Join(evs, ","))
	}
	b.WriteString("\n")

	// 子节点（Tabs 的内容页只展开当前活动页，与 HTML 渲染口径一致）。
	var children []*model.Widget
	if w.Kind == model.KindTabs {
		tabs, _ := w.Props[model.PropTabs].([]model.Tab)
		active, _ := w.Props[model.PropActiveTab].(int)
		if active >= 0 && active < len(tabs) && tabs[active].Content != nil {
			children = []*model.Widget{tabs[active].Content}
		}
	} else {
		children = w.Children
	}
	childPrefix := prefix
	if depth > 0 {
		if isLast {
			childPrefix += "   "
		} else {
			childPrefix += "│  "
		}
	}
	for i, c := range children {
		writeNode(b, c, childPrefix, i == len(children)-1, depth+1)
	}
}

func propStr(w *model.Widget, key string) string {
	s, _ := w.Props[key].(string)
	return s
}

func propF(w *model.Widget, key string) float64 {
	switch v := w.Props[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case string:
		f, _ := strconv.ParseFloat(v, 64)
		return f
	}
	return 0
}

func fmtInt(v any) string {
	switch t := v.(type) {
	case int:
		return strconv.Itoa(t)
	case float64:
		return strconv.Itoa(int(t))
	}
	return "0"
}

func ft(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

// SortKeys 供外部复用的键排序工具。
func SortKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
