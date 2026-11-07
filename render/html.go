// Package render 把 model 控件树确定性渲染为 HTML。
//
// 确定性纪律（golden 像素对比的前提）：
//   - 属性按固定顺序输出；文本/属性一处不漏地转义；
//   - 不注入时间戳/随机数/自增序号；
//   - 同一树状态 + 同一主题 → 逐字节相同的 HTML。
//
// 性能纪律（渲染热路径）：转义零分配直写 Builder、容量一次预估 Grow、
// 数值格式化走 strconv —— 基准见 bench_test.go。
package render

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/nimamasl114514/gogui/model"
	"github.com/nimamasl114514/gogui/theme"
)

// estBytesPerWidget 是 HTML 输出的每控件容量预估（只影响 Grow，不影响正确性）。
const estBytesPerWidget = 140

// bridgeScript 由 server 包在进程内注册（避免 render→server 反向依赖）。
// 未注册时退回外链 /gg-bridge.js（供纯静态渲染调试）。
var bridgeScript = ""

// RegisterBridge 注册内联桥脚本（server 包 init 调用）。
func RegisterBridge(js string) { bridgeScript = js }

// HTML 渲染整页文档（含主题变量、设计系统 CSS、bridge 脚本挂载）。
func HTML(app *model.App, th *theme.Theme) string {
	var b strings.Builder
	b.Grow(estimate(app) + len(DesignCSS) + 8192)
	htmlCls := ""
	if th.ForceAnimations {
		htmlCls = ` class="gg-force-anim"`
	}
	b.WriteString("<!doctype html><html" + htmlCls + "><head><meta charset=\"utf-8\">")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">")
	b.WriteString("<title>")
	writeEscaped(&b, app.Title)
	b.WriteString("</title><style>")
	b.WriteString(th.CSSVars())
	b.WriteString(DesignCSS)
	b.WriteString(th.ExtraCSS)
	b.WriteString("</style></head><body class=\"gg-body ")
	b.WriteString(th.BodyClass())
	b.WriteString("\">")
	b.WriteString(renderRoot(app))
	if bridgeScript != "" {
		// 桥脚本内联：少一次子资源请求，且页面加载阶段任何一个子请求抖动
		// 都不会导致「桥整段没跑」。/gg-bridge.js 路由保留供调试/缓存外链。
		b.WriteString("<script>")
		b.WriteString(bridgeScript)
		b.WriteString("</script>")
	} else {
		b.WriteString(`<script src="/gg-bridge.js"></script>`)
	}
	b.WriteString("</body></html>")
	return b.String()
}

// Body 渲染根容器 HTML 片段（server 补丁下发用：morph 的 source）。
func Body(app *model.App) string {
	return renderRoot(app)
}

// estimate 预估输出容量（控件数 × 每控件预算）。
func estimate(app *model.App) int {
	n := 0
	app.View(func(root *model.Widget) {
		root.Walk(func(*model.Widget) bool { n++; return true })
	})
	return n*estBytesPerWidget + 2048
}

func renderRoot(app *model.App) string {
	var b strings.Builder
	b.Grow(estimate(app))
	app.View(func(root *model.Widget) {
		// 注意：wrapper 只挂 class，不挂 data-gid —— 根控件的 ID 由控件自身承载，
		// 否则 DOM 会出现两个同 ID 元素（布局采集/防注入均会踩坑）。
		b.WriteString(`<div class="gg-app">`)
		renderWidget(&b, root)
		b.WriteString("</div>")
	})
	return b.String()
}

// renderWidget 渲染单个控件（含子树）。属性顺序固定，保证逐字节确定性。
func renderWidget(b *strings.Builder, w *model.Widget) {
	// cls 组装：gg-<kind> + 控件语义类 + 自定义类。
	cls := "gg-" + string(w.Kind)
	if w.Kind == model.KindSelect {
		cls = "gg-selectwrap" // 根是包裹层，内层 <select> 才是 gg-select
	}
	switch w.Kind {
	case model.KindButton:
		if v, _ := w.Props[model.PropVariant].(string); v != "" {
			cls += " gg-" + v
		}
	case model.KindLabel, model.KindBadge:
		// Label 的排版级别、Badge 的语义色类都走 Classes。
	}
	for _, c := range w.Classes {
		if c != "" {
			cls += " " + c
		}
	}

	tag := htmlTag(w.Kind)
	b.WriteString("<" + tag + ` class="`)
	writeEscaped(b, cls)
	b.WriteString(`"`)
	disabled := false
	if d, _ := w.Props[model.PropDisabled].(bool); d {
		disabled = true
	}
	if w.ID != "" && w.Kind != model.KindSelect {
		b.WriteString(` data-gid="`)
		writeEscaped(b, w.ID)
		b.WriteString(`"`)
	}
	// 任意注册了 onClick 的控件（含容器）都可被事件桥命中。
	if w.HasClickHandlers() && w.Kind != model.KindButton && w.Kind != model.KindToggle &&
		w.Kind != model.KindTabs && w.Kind != model.KindList {
		b.WriteString(` data-ev="click"`)
	}
	// 内联样式逃生舱：叶子元素在这里统一追加；容器由 writeContainerStyle 合并。
	if w.Kind != model.KindVStack && w.Kind != model.KindHStack && w.Kind != model.KindCard &&
		w.Kind != model.KindSpacer && w.Kind != model.KindSlider {
		seg := ""
		if wd := intProp(w, model.PropWidth); wd > 0 && w.Kind == model.KindLabel {
			seg = "width:" + strconv.Itoa(wd) + "px"
		}
		if css := propString(w, model.PropStyle); css != "" {
			if seg != "" {
				seg += ";"
			}
			seg += css
		}
		if seg != "" {
			// 逃生舱也要转义：CSS 里的引号会被 HTML 解析器还原，注入面闭合。
			b.WriteString(` style="`)
			writeEscaped(b, seg)
			b.WriteString(`"`)
		}
	}

	switch w.Kind {
	case model.KindLabel:
		b.WriteString(">")
		writeEscaped(b, w.TextValue())
		b.WriteString("</div>")

	case model.KindButton:
		b.WriteString(` data-ev="click"`)
		if disabled {
			b.WriteString(" disabled")
		}
		b.WriteString(">")
		writeEscaped(b, w.TextValue())
		b.WriteString("</button>")

	case model.KindInput:
		b.WriteString(` data-ev="input change" type="text" value="`)
		writeEscaped(b, w.ValueText())
		b.WriteString(`"`)
		if ph := propString(w, model.PropPlaceholder); ph != "" {
			b.WriteString(` placeholder="`)
			writeEscaped(b, ph)
			b.WriteString(`"`)
		}
		if disabled {
			b.WriteString(" disabled")
		}
		b.WriteString(">")

	case model.KindTextarea:
		b.WriteString(` data-ev="input change" rows="4"`)
		if disabled {
			b.WriteString(" disabled")
		}
		b.WriteString(">")
		writeEscaped(b, w.ValueText())
		b.WriteString("</textarea>")

	case model.KindToggle:
		b.WriteString(` data-ev="click" role="switch" aria-checked="` + boolStr(w.CheckedValue()) + `"`)
		if disabled {
			b.WriteString(" disabled")
		}
		b.WriteString("></button>")

	case model.KindSlider:
		v := w.SliderValue()
		b.WriteString(` data-ev="input change" type="range"`)
		b.WriteString(` min="` + ft(propFloat(w.Props[model.PropMin])) + `"`)
		b.WriteString(` max="` + ft(propFloat(w.Props[model.PropMax])) + `"`)
		b.WriteString(` step="` + ft(propFloat(w.Props[model.PropStep])) + `"`)
		b.WriteString(` value="` + ft(v) + `"`)
		b.WriteString(` style="--fill:` + sliderFillPct(w, v) + `%`)
		if css := propString(w, model.PropStyle); css != "" {
			b.WriteString(";")
			writeEscaped(b, css)
		}
		if wd := intProp(w, model.PropWidth); wd > 0 {
			b.WriteString(`;width:` + strconv.Itoa(wd) + `px;flex:none`)
		}
		b.WriteString(`"`)
		if disabled {
			b.WriteString(" disabled")
		}
		b.WriteString(">")

	case model.KindProgress:
		lvl := clamp01(w.LevelValue())
		b.WriteString(`><div class="gg-progress-fill" style="width:`)
		writeF2(b, lvl*100)
		b.WriteString(`%"></div></div>`)

	case model.KindSelect:
		opts, _ := w.Props[model.PropOptions].([]string)
		sel := propString(w, model.PropSelected)
		b.WriteString(`><select class="gg-select" data-gid="`)
		writeEscaped(b, w.ID)
		b.WriteString(`" data-ev="change"`)
		if disabled {
			b.WriteString(" disabled")
		}
		b.WriteString(">")
		for _, o := range opts {
			b.WriteString("<option")
			if o == sel {
				b.WriteString(" selected")
			}
			b.WriteString(">")
			writeEscaped(b, o)
			b.WriteString("</option>")
		}
		b.WriteString("</select></div>")

	case model.KindBadge:
		b.WriteString(">")
		writeEscaped(b, w.TextValue())
		b.WriteString("</span>")

	case model.KindTabs:
		tabs, _ := w.Props[model.PropTabs].([]model.Tab)
		active, _ := w.Props[model.PropActiveTab].(int)
		b.WriteString(`><div class="gg-tabbar">`)
		for i, t := range tabs {
			b.WriteString(`<button type="button" class="gg-tab`)
			if i == active {
				b.WriteString(" active")
			}
			b.WriteString(`" data-gg-id="`)
			writeEscaped(b, w.ID)
			b.WriteString(`" data-gv="` + strconv.Itoa(i) + `" data-ev="click"`)
			if disabled {
				b.WriteString(" disabled")
			}
			b.WriteString(">")
			writeEscaped(b, t.Title)
			b.WriteString("</button>")
		}
		b.WriteString(`</div><div class="gg-tabpanel">`)
		if active >= 0 && active < len(tabs) && tabs[active].Content != nil {
			renderWidget(b, tabs[active].Content)
		}
		b.WriteString("</div></div>")

	case model.KindList:
		items, _ := w.Props[model.PropItems].([]string)
		selIdx, _ := w.Props[model.PropSelectedIdx].(int)
		b.WriteString(">")
		for i, it := range items {
			b.WriteString(`<button type="button" class="gg-item`)
			if i == selIdx {
				b.WriteString(" selected")
			}
			b.WriteString(`" data-gg-id="`)
			writeEscaped(b, w.ID)
			b.WriteString(`" data-gv="` + strconv.Itoa(i) + `" data-ev="click"`)
			if disabled {
				b.WriteString(" disabled")
			}
			b.WriteString(">")
			writeEscaped(b, it)
			b.WriteString("</button>")
		}
		b.WriteString("</div>")

	case model.KindImage:
		b.WriteString(` src="`)
		writeEscaped(b, propString(w, model.PropSrc))
		b.WriteString(`" alt="`)
		writeEscaped(b, propString(w, model.PropAlt))
		b.WriteString(`">`)

	case model.KindSep:
		b.WriteString("></div>")

	case model.KindSpacer, model.KindVStack, model.KindHStack, model.KindCard:
		b.WriteString(" ")
		writeContainerStyle(b, w)
		b.WriteString(">")
		for _, c := range w.Children {
			renderWidget(b, c)
		}
		b.WriteString("</div>")

	default:
		b.WriteString(">")
		for _, c := range w.Children {
			renderWidget(b, c)
		}
		b.WriteString("</div>")
	}
}

// 白名单：布局枚举只接受固定集合，非法值丢弃（防属性注入）。
var alignSet = map[string]bool{
	"flex-start": true, "start": true, "center": true, "flex-end": true, "end": true, "stretch": true, "baseline": true,
}
var justifySet = map[string]bool{
	"flex-start": true, "start": true, "center": true, "flex-end": true, "end": true,
	"space-between": true, "space-around": true, "space-evenly": true,
}

func validAlign(v string) bool    { return alignSet[v] }
func validJustify(v string) bool  { return justifySet[v] }

func htmlTag(k model.Kind) string {
	switch k {
	case model.KindButton, model.KindToggle:
		return "button"
	case model.KindInput, model.KindSlider:
		return "input"
	case model.KindTextarea:
		return "textarea"
	case model.KindBadge:
		return "span"
	case model.KindImage:
		return "img"
	default:
		return "div"
	}
}

// writeContainerStyle 直写容器 inline 布局样式（gap/align/justify/padding/flex）。
func writeContainerStyle(b *strings.Builder, w *model.Widget) {
	flexDir := "column"
	if w.Kind == model.KindHStack {
		flexDir = "row"
	}
	b.WriteString(`style="display:flex;flex-direction:` + flexDir)
	if g := intProp(w, model.PropGap); g > 0 {
		b.WriteString(";gap:")
		b.WriteString(strconv.Itoa(g))
		b.WriteString("px")
	}
	if a := propString(w, model.PropAlign); validAlign(a) {
		b.WriteString(";align-items:" + a)
	}
	if j := propString(w, model.PropJustify); validJustify(j) {
		b.WriteString(";justify-content:" + j)
	}
	if p := intProp(w, model.PropPadding); p > 0 {
		b.WriteString(";padding:")
		b.WriteString(strconv.Itoa(p))
		b.WriteString("px")
	}
	if g, _ := w.Props[model.PropFlex].(bool); g {
		b.WriteString(";flex:1 1 0")
	}
	b.WriteString(`;min-width:0`)
	if css := propString(w, model.PropStyle); css != "" {
		b.WriteString(";")
		writeEscaped(b, css)
	}
	b.WriteString(`"`)
}

func sliderFillPct(w *model.Widget, v float64) string {
	min := propFloat(w.Props[model.PropMin])
	max := propFloat(w.Props[model.PropMax])
	if max <= min {
		return "0"
	}
	pct := (v - min) / (max - min) * 100
	if pct < 0 {
		pct = 0
	} else if pct > 100 {
		pct = 100
	}
	// 与 fmt "%.2f" 同口径的零分配实现。
	var buf [24]byte
	return string(strconv.AppendFloat(buf[:0], pct, 'f', 2, 64))
}

// writeF2 以 "%.2f" 口径写数值（进度条宽度）。
func writeF2(b *strings.Builder, v float64) {
	var buf [24]byte
	b.Write(strconv.AppendFloat(buf[:0], v, 'f', 2, 64))
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func intProp(w *model.Widget, key string) int {
	switch v := w.Props[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	}
	return 0
}

func propFloat(v any) float64 {
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

func propString(w *model.Widget, key string) string {
	s, _ := w.Props[key].(string)
	return s
}

func ft(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// writeEscaped 零分配 HTML 转义（直写 Builder）。
// 转义规则与 HTML5 一致：& < > " ' 五个字符；
// 输入含非法 UTF-8 时先净化为 U+FFFD（保证输出永远是合法 UTF-8）。
func writeEscaped(b *strings.Builder, s string) {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "\uFFFD")
	}
	last := 0
	for i := 0; i < len(s); i++ {
		var esc string
		switch s[i] {
		case '&':
			esc = "&amp;"
		case '<':
			esc = "&lt;"
		case '>':
			esc = "&gt;"
		case '"':
			esc = "&#34;"
		case '\'':
			esc = "&#39;"
		default:
			continue
		}
		b.WriteString(s[last:i])
		b.WriteString(esc)
		last = i + 1
	}
	b.WriteString(s[last:])
}

// escape 返回转义后的字符串（保留给测试与外部调用；热路径用 writeEscaped）。
func escape(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	writeEscaped(&b, s)
	return b.String()
}
