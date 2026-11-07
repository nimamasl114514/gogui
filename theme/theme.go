// Package theme 定义 gogui 的设计系统变量层。
//
// Theme → CSS 自定义属性（:root 变量），控件样式全部只引用变量，
// 因此「换主题 = 换变量」，任何第三方都能扩展出自带主题。
// Vars 用有序切片而非 map：序列化输出确定，golden 树快照逐字节可复现。
package theme

// Var 是单个 CSS 自定义属性。
type Var struct{ Name, Value string }

// Theme 是一套完整设计变量。
type Theme struct {
	Name string
	Dark bool
	Vars []Var
	// ExtraCSS 是应用级外挂样式（追加在内置设计系统之后，可覆盖/新增任意规则）。
	ExtraCSS string
	// ForceAnimations 强制启用动效：某些 Windows/WebView2 环境把
	// prefers-reduced-motion 报成 reduce（系统「动画效果」关闭），内置设计系统会
	// 把动画压到 0.01ms（等于全灭）。展示型应用置 true 可无视该偏好。
	ForceAnimations bool
}

// DarkGlow 是默认深色主题：冷调深底 + 靛青-青渐变强调 + 玻璃表面。
func DarkGlow() *Theme {
	return &Theme{
		Name: "dark-glow",
		Dark: true,
		Vars: []Var{
			{"--bg", "#0a0e16"},
			{"--bg-glow-1", "rgba(124,92,255,0.16)"},
			{"--bg-glow-2", "rgba(34,211,238,0.10)"},
			{"--surface", "#121826"},
			{"--surface-2", "#1a2334"},
			{"--surface-3", "#222d42"},
			{"--border", "rgba(148,163,196,0.14)"},
			{"--text", "#e9edf8"},
			{"--text-muted", "#8e97ad"},
			{"--accent", "#7c5cff"},
			{"--accent-2", "#22d3ee"},
			{"--accent-grad", "linear-gradient(135deg, #7c5cff 0%, #22d3ee 100%)"},
			{"--accent-glow", "rgba(124,92,255,0.45)"},
			{"--danger", "#ff5c7a"},
			{"--danger-glow", "rgba(255,92,122,0.40)"},
			{"--ok", "#34d399"},
			{"--warn", "#fbbf24"},
			{"--shadow-lg", "0 18px 48px rgba(2,6,18,0.55)"},
			{"--shadow-md", "0 8px 24px rgba(2,6,18,0.40)"},
			{"--focus-ring", "rgba(124,92,255,0.30)"},
			{"--radius", "12px"},
			{"--font", `"Segoe UI Variable Text", "Segoe UI", "Microsoft YaHei UI", "PingFang SC", system-ui, -apple-system, sans-serif`},
		},
	}
}

// CleanLight 是浅色主题：冷白面板 + 靛蓝-天青强调。
func CleanLight() *Theme {
	return &Theme{
		Name: "clean-light",
		Dark: false,
		Vars: []Var{
			{"--bg", "#f2f5fb"},
			{"--bg-glow-1", "rgba(99,102,241,0.10)"},
			{"--bg-glow-2", "rgba(14,165,233,0.07)"},
			{"--surface", "#ffffff"},
			{"--surface-2", "#eef1f8"},
			{"--surface-3", "#e4e9f4"},
			{"--border", "rgba(23,35,61,0.10)"},
			{"--text", "#17233d"},
			{"--text-muted", "#5f6b84"},
			{"--accent", "#6366f1"},
			{"--accent-2", "#0ea5e9"},
			{"--accent-grad", "linear-gradient(135deg, #6366f1 0%, #0ea5e9 100%)"},
			{"--accent-glow", "rgba(99,102,241,0.35)"},
			{"--danger", "#ef4444"},
			{"--danger-glow", "rgba(239,68,68,0.30)"},
			{"--ok", "#10b981"},
			{"--warn", "#f59e0b"},
			{"--shadow-lg", "0 18px 44px rgba(23,35,61,0.14)"},
			{"--shadow-md", "0 8px 22px rgba(23,35,61,0.10)"},
			{"--focus-ring", "rgba(99,102,241,0.25)"},
			{"--radius", "12px"},
			{"--font", `"Segoe UI Variable Text", "Segoe UI", "Microsoft YaHei UI", "PingFang SC", system-ui, -apple-system, sans-serif`},
		},
	}
}

// CSSVars 渲染为 ":root{--k:v;...}"（确定性输出）。
// 安全：第三方主题的 name/value 会做词法净化 —— name 只允许 [A-Za-z0-9_-]，
// value 去掉 "</"、"}"、控制字符与换行（防闭合 <style> 注入脚本或跳出 :root 块）。
func (t *Theme) CSSVars() string {
	buf := make([]byte, 0, 512)
	buf = append(buf, ":root{"...)
	for _, v := range t.Vars {
		name := sanitizeVarName(v.Name)
		if name == "" {
			continue
		}
		buf = append(buf, name...)
		buf = append(buf, ':')
		buf = append(buf, sanitizeVarValue(v.Value)...)
		buf = append(buf, ';')
	}
	buf = append(buf, '}')
	return string(buf)
}

func sanitizeVarName(name string) string {
	var b []byte
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == '-' || c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			b = append(b, c)
		}
	}
	if len(b) == 0 || (b[0] >= '0' && b[0] <= '9') {
		return ""
	}
	return string(b)
}

func sanitizeVarValue(val string) string {
	out := make([]byte, 0, len(val))
	for i := 0; i < len(val); i++ {
		c := val[i]
		if c < 0x20 || c == '}' {
			continue
		}
		if c == '<' && i+1 < len(val) && val[i+1] == '/' {
			continue // 去掉 "</" 序列（闭合 style 标签的注入面）
		}
		out = append(out, c)
	}
	return string(out)
}

// BodyClass 返回 body 主题类（gg-dark / gg-light）。
func (t *Theme) BodyClass() string {
	if t.Dark {
		return "gg-dark"
	}
	return "gg-light"
}
