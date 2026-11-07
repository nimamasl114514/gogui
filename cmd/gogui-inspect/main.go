// gogui-inspect —— gogui 应用运行时可观测性检查器。
//
// 用法：
//
//	gogui-inspect -url http://127.0.0.1:PORT state      # 全量状态（计数/最近事件/渲染耗时/JS错误）
//	gogui-inspect -url ... health                       # 健康检查（退出码 0=ok / 1=unhealthy）
//	gogui-inspect -url ... log [-n 20]                  # 最近事件与前端错误
//	gogui-inspect -url ... metrics                      # Prometheus 文本指标
//	gogui-inspect -url ... watch                        # 每 2s 刷新状态（Ctrl+C 退出）
//	gogui-inspect -url ... tree                         # 控件树快照文本
//	gogui-inspect -url ... layout                       # 控件盒模型+计算样式（异常标⚠）
//	gogui-inspect -url ... trace [-n 20]                # 端到端事件追踪（含 morph/rtt）
//	gogui-inspect -url ... widget <gid>                 # 单控件透视（模型+解析值+布局）
//	gogui-inspect -url ... history [-n 60]              # 1Hz 历史采样
//
// URL 从应用 stdout 打印的「server listening on …」一行获取。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	url := flag.String("url", "", "应用 server URL（如 http://127.0.0.1:54321）")
	n := flag.Int("n", 20, "log 条数 / state 附带日志条数")
	asJSON := flag.Bool("json", false, "原样输出 JSON（layout/state）")
	timeout := flag.Duration("timeout", 3*time.Second, "单次请求超时")
	flag.Parse()

	cmd := "state"
	if args := flag.Args(); len(args) > 0 {
		cmd = args[0]
	}
	if *url == "" {
		fmt.Fprintln(os.Stderr, "缺少 -url（从应用 stdout 的『server listening on …』获取）")
		os.Exit(2)
	}

	switch cmd {
	case "state":
		body, err := get(*url+fmt.Sprintf("/gg/state?n=%d", *n), *timeout)
		must(err)
		prettyState(body)
	case "tree":
		body, err := get(*url+"/gg/state?tree=1&n=1", *timeout)
		must(err)
		var v struct {
			Tree string `json:"tree"`
		}
		must(json.Unmarshal(body, &v))
		fmt.Println(v.Tree)
	case "health":
		body, err := get(*url+"/gg/health", *timeout)
		must(err)
		var hv struct {
			OK       bool     `json:"ok"`
			Warnings []string `json:"warnings"`
		}
		_ = json.Unmarshal(body, &hv)
		prettyHealth(body)
		if !hv.OK {
			os.Exit(1)
		}
	case "log":
		body, err := get(*url+fmt.Sprintf("/gg/log?n=%d", *n), *timeout)
		must(err)
		prettyLog(body)
	case "metrics":
		body, err := get(*url+"/gg/metrics", *timeout)
		must(err)
		fmt.Println(string(body))
	case "layout":
		body, err := get(*url+"/gg/layout", *timeout+2*time.Second)
		must(err)
		if *asJSON {
			fmt.Println(string(body))
			break
		}
		prettyLayout(body)
	case "trace":
		body, err := get(*url+fmt.Sprintf("/gg/trace?n=%d", *n), *timeout)
		must(err)
		printTrace(body)
	case "widget":
		if flag.NArg() < 2 {
			fmt.Fprintln(os.Stderr, "用法: gogui-inspect -url ... widget <gid>")
			os.Exit(2)
		}
		body, err := get(*url+"/gg/widget/"+flag.Arg(1), *timeout)
		must(err)
		printWidget(body)
	case "history":
		body, err := get(*url+fmt.Sprintf("/gg/history?n=%d", *n), *timeout)
		must(err)
		printHistory(body)
	case "watch":
		watch(*url, *timeout)
	default:
		fmt.Fprintf(os.Stderr, "未知命令 %q（state|health|log|metrics|tree|layout|trace|widget|history|watch）\n", cmd)
		os.Exit(2)
	}
}

func get(u string, timeout time.Duration) ([]byte, error) {
	c := &http.Client{Timeout: timeout}
	resp, err := c.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 && resp.StatusCode != 503 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "inspect:", err)
		os.Exit(1)
	}
}

// stateView 与 server.stateResponse 对齐（只取展示所需字段）。
type stateView struct {
	Version string `json:"version"`
	Server  struct {
		URL        string `json:"url"`
		UptimeMS   int64  `json:"uptime_ms"`
		SSEClients int    `json:"sse_clients"`
		SSEPushes  uint64 `json:"sse_pushes"`
	} `json:"server"`
	App struct {
		Title        string `json:"title"`
		Widgets      int    `json:"widgets"`
		Events       uint64 `json:"events_total"`
		Failures     uint64 `json:"events_failed"`
		Renders      uint64 `json:"renders_total"`
		LastRenderNS int64  `json:"render_last_ns"`
		MaxRenderNS  int64  `json:"render_max_ns"`
		JSErrors     uint64 `json:"js_errors_total"`
		UptimeMS     int64  `json:"uptime_ms"`
		Goroutines   int    `json:"goroutines"`
		HeapKB       uint64 `json:"heap_kb"`
		LastEvent    *struct {
			Widget string `json:"widget"`
			Type   string `json:"type"`
			Value  string `json:"value"`
			Err    string `json:"err"`
			AgeMS  int64  `json:"age_ms"`
		} `json:"last_event"`
	} `json:"app"`
	Recent []logView `json:"recent_log"`
}

type logView struct {
	TS     string `json:"ts"`
	Kind   string `json:"kind"`
	Widget string `json:"widget"`
	Type   string `json:"type"`
	Value  string `json:"value"`
	Err    string `json:"err"`
	Msg    string `json:"msg"`
	Where  string `json:"where"`
}

// ms 把毫秒格式化为易读时长。
func ms(v int64) string {
	if v < 1000 {
		return fmt.Sprintf("%dms", v)
	}
	return fmt.Sprintf("%.1fs", float64(v)/1000)
}

func prettyState(body []byte) {
	var v stateView
	must(json.Unmarshal(body, &v))
	fmt.Printf("gogui v%s · %s\n", v.Version, v.App.Title)
	fmt.Printf("  server   %s  uptime=%s  sse_clients=%d  sse_pushes=%d\n",
		v.Server.URL, ms(v.Server.UptimeMS), v.Server.SSEClients, v.Server.SSEPushes)
	fmt.Printf("  widgets  %d   goroutines=%d   heap=%d KB\n",
		v.App.Widgets, v.App.Goroutines, v.App.HeapKB)
	fmt.Printf("  events   total=%d failed=%d   renders=%d   render_last=%.1fus render_max=%.1fus\n",
		v.App.Events, v.App.Failures, v.App.Renders, float64(v.App.LastRenderNS)/1000, float64(v.App.MaxRenderNS)/1000)
	jsMark := ""
	if v.App.JSErrors > 0 {
		jsMark = "  ⚠ 前端有错误"
	}
	fmt.Printf("  js_errors %d%s\n", v.App.JSErrors, jsMark)
	if v.App.LastEvent != nil {
		fmt.Printf("  last_event %s ← %s(%s)   %dms 前\n",
			v.App.LastEvent.Widget, v.App.LastEvent.Type, v.App.LastEvent.Value, v.App.LastEvent.AgeMS)
		if v.App.LastEvent.Err != "" {
			fmt.Printf("            误差: %s\n", v.App.LastEvent.Err)
		}
	}
	if len(v.Recent) > 0 {
		fmt.Println("  recent_log:")
		printLogs(v.Recent)
	}
}

func prettyLog(body []byte) {
	var arr []logView
	must(json.Unmarshal(body, &arr))
	printLogs(arr)
}

func printLogs(arr []logView) {
	for _, e := range arr {
		switch e.Kind {
		case "event":
			line := fmt.Sprintf("    %s  事件 %s(%s)=%q", e.TS, e.Widget, e.Type, e.Value)
			if e.Err != "" {
				line += "  误差: " + e.Err
			}
			fmt.Println(line)
		case "js_error":
			fmt.Printf("    %s  ⚠ JS错误 %s @ %s\n", e.TS, e.Msg, e.Where)
		default:
			fmt.Printf("    %s  %s %s @ %s\n", e.TS, e.Kind, e.Msg, e.Where)
		}
	}
}

func watch(url string, timeout time.Duration) {
	fmt.Println("watch:", url, "（Ctrl+C 退出）")
	fmt.Println("时间       ev/s  rnd/s  渲染耗时(60s)                                    堆(MB,60s)                              协程  JS错  最近事件")
	for {
		stBody, err := get(url+"/gg/state?n=0", timeout)
		if err != nil {
			fmt.Printf("[%s] ✗ %v\n", time.Now().Format("15:04:05"), err)
			time.Sleep(2 * time.Second)
			continue
		}
		var v stateView
		_ = json.Unmarshal(stBody, &v)
		histBody, herr := get(url+"/gg/history?n=60", timeout)
		var hs []historySample
		evRate, rndRate := uint64(0), uint64(0)
		sparkRender, sparkHeap := "", ""
		if herr == nil && len(hs) >= 2 {
			_ = json.Unmarshal(histBody, &hs)
			evRate = hs[len(hs)-1].EvDelta
			rndRate = hs[len(hs)-1].RndDelta
			rv := make([]float64, len(hs))
			hv := make([]float64, len(hs))
			for i, h := range hs {
				rv[i] = float64(h.RenderNS)
				hv[i] = float64(h.HeapKB) / 1024
			}
			sparkRender = sparkline(rv)
			sparkHeap = sparkline(hv)
		}
		last := "—"
		if v.App.LastEvent != nil {
			last = fmt.Sprintf("%s(%s) %dms前", v.App.LastEvent.Widget, v.App.LastEvent.Type, v.App.LastEvent.AgeMS)
		}
		fmt.Printf("%s  %4d  %5d  %-44s  %-38s  %4d  %4d  %s\n",
			time.Now().Format("15:04:05"), evRate, rndRate, sparkRender, sparkHeap,
			v.App.Goroutines, v.App.JSErrors, last)
		time.Sleep(2 * time.Second)
	}
}

// sparkline 把数值序列渲染成 ASCII 迷你图（8 档）。
func sparkline(vals []float64) string {
	blocks := []rune("▁▂▃▄▅▆▇█")
	maxV := 0.0
	for _, v := range vals {
		if v > maxV {
			maxV = v
		}
	}
	if maxV <= 0 {
		maxV = 1
	}
	out := make([]rune, len(vals))
	for i, v := range vals {
		idx := int(v / maxV * 7)
		if idx < 0 {
			idx = 0
		}
		if idx > 7 {
			idx = 7
		}
		out[i] = blocks[idx]
	}
	return string(out)
}

// ---- trace / widget / history ----

type traceSpan struct {
	ID         string `json:"id"`
	TS         string `json:"ts"`
	Widget     string `json:"widget"`
	Type       string `json:"type"`
	Value      string `json:"value"`
	DispatchUS int64  `json:"dispatch_us"`
	RenderNS   int64  `json:"render_ns"`
	PatchBytes int    `json:"patch_bytes"`
	Err        string `json:"err"`
	RTTUS      int64  `json:"rtt_us"`
	MorphUS    int64  `json:"morph_us"`
	Applied    bool   `json:"applied"`
}

func printTrace(body []byte) {
	var spans []traceSpan
	must(json.Unmarshal(body, &spans))
	if len(spans) == 0 {
		fmt.Println("（暂无事件）")
		return
	}
	fmt.Printf("  %-6s %-12s %-14s %-10s %8s %10s %7s %10s %6s\n",
		"id", "时间", "控件", "事件", "go耗时us", "渲染us", "补丁B", "rtt/morph", "状态")
	for _, s := range spans {
		front := "—"
		if s.Applied {
			front = fmt.Sprintf("%d/%d", s.RTTUS, s.MorphUS)
		}
		mark := "✓"
		if s.Err != "" {
			mark = "✗"
		}
		fmt.Printf("  %-6s %-12s %-14s %-10s %8d %10.1f %7d %10s %6s\n",
			s.ID, s.TS, trunc(s.Widget, 14), s.Type, s.DispatchUS, float64(s.RenderNS)/1000, s.PatchBytes, front, mark)
		if s.Err != "" {
			fmt.Printf("        误差: %s\n", trunc(s.Err, 110))
		}
	}
	// 汇总（只看已回填的）。
	var n int
	var rttSum, morphSum int64
	for _, s := range spans {
		if s.Applied {
			n++
			rttSum += s.RTTUS
			morphSum += s.MorphUS
		}
	}
	if n > 0 {
		fmt.Printf("\n已回填 %d/%d · 平均 rtt=%dus morph=%dus\n", n, len(spans), rttSum/int64(n), morphSum/int64(n))
	}
}

func printWidget(body []byte) {
	var v struct {
		ID       string            `json:"id"`
		Kind     string            `json:"kind"`
		Classes  []string          `json:"classes"`
		Props    map[string]string `json:"props"`
		Resolved map[string]string `json:"resolved"`
		Layout   *struct {
			Tag        string  `json:"tag"`
			X          float64 `json:"x"`
			Y          float64 `json:"y"`
			W          float64 `json:"w"`
			H          float64 `json:"h"`
			FontSize   string  `json:"fontSize"`
			Color      string  `json:"color"`
			Background string  `json:"background"`
			BorderRadius string `json:"borderRadius"`
		} `json:"layout"`
		Found bool `json:"found"`
	}
	must(json.Unmarshal(body, &v))
	if !v.Found {
		fmt.Printf("控件 %q 不存在\n", v.ID)
		os.Exit(1)
	}
	fmt.Printf("控件 %s   kind=%s   classes=%v\n", v.ID, v.Kind, v.Classes)
	for k, val := range v.Props {
		fmt.Printf("  prop %-14s = %s\n", k, trunc(val, 80))
	}
	for k, val := range v.Resolved {
		fmt.Printf("  解析 %-14s = %q\n", k, trunc(val, 80))
	}
	if v.Layout != nil {
		l := v.Layout
		fmt.Printf("  布局 <%s> %.0f×%.0f @ (%.0f,%.0f)  字号=%s 文字色=%s 背景=%s 圆角=%s\n",
			l.Tag, l.W, l.H, l.X, l.Y, l.FontSize, l.Color, l.Background, l.BorderRadius)
	} else {
		fmt.Println("  布局：（无缓存，先跑一次 layout）")
	}
}

type historySample struct {
	T         string `json:"t"`
	EvDelta   uint64 `json:"events_delta"`
	RndDelta  uint64 `json:"renders_delta"`
	RenderNS  int64  `json:"render_last_ns"`
	HeapKB    uint64 `json:"heap_kb"`
	Goroutine int    `json:"goroutines"`
}

func printHistory(body []byte) {
	var hs []historySample
	must(json.Unmarshal(body, &hs))
	rv := make([]float64, len(hs))
	hv := make([]float64, len(hs))
	for i, h := range hs {
		rv[i] = float64(h.RenderNS)
		hv[i] = float64(h.HeapKB) / 1024
	}
	if len(hs) > 0 {
		fmt.Printf("采样 %d 点（%s → %s）\n", len(hs), hs[0].T, hs[len(hs)-1].T)
	}
	fmt.Printf("渲染耗时(us) %s\n", sparkline(rv))
	fmt.Printf("堆(MB)       %s\n", sparkline(hv))
	fmt.Printf("\n  %-10s %6s %7s %10s %8s %5s\n", "时间", "ev/s", "rnd/s", "render_us", "heap_kb", "协程")
	for _, h := range hs {
		fmt.Printf("  %-10s %6d %7d %10.1f %8d %5d\n", h.T, h.EvDelta, h.RndDelta, float64(h.RenderNS)/1000, h.HeapKB, h.Goroutine)
	}
}

func prettyHealth(body []byte) {
	var v struct {
		OK           bool     `json:"ok"`
		Warnings     []string `json:"warnings"`
		Version      string   `json:"version"`
		UptimeMS     int64    `json:"uptime_ms"`
		Widgets      int      `json:"widgets"`
		Events       uint64   `json:"events_total"`
		Failures     uint64   `json:"events_failed"`
		JSErrors     uint64   `json:"js_errors_total"`
		LastRenderUS int64    `json:"last_render_us"`
		Goroutines   int      `json:"goroutines"`
		HeapKB       uint64   `json:"heap_kb"`
	}
	must(json.Unmarshal(body, &v))
	status := "✓ ok"
	if !v.OK {
		status = "✗ unhealthy"
	}
	fmt.Printf("%s   v%s  uptime=%s  控件=%d 事件=%d 失败=%d JS错=%d 渲染=%dus 协程=%d 堆=%dKB\n",
		status, v.Version, ms(v.UptimeMS), v.Widgets, v.Events, v.Failures, v.JSErrors, v.LastRenderUS, v.Goroutines, v.HeapKB)
	if len(v.Warnings) > 0 {
		fmt.Println("警告：")
		for _, w := range v.Warnings {
			fmt.Println("  ⚠ " + w)
		}
	} else {
		fmt.Println("无警告")
	}
}

// ---- layout ----

type layoutItem struct {
	GID          string  `json:"gid"`
	Tag          string  `json:"tag"`
	Cls          string  `json:"cls"`
	X            float64 `json:"x"`
	Y            float64 `json:"y"`
	W            float64 `json:"w"`
	H            float64 `json:"h"`
	Display      string  `json:"display"`
	Visibility   string  `json:"visibility"`
	Opacity      string  `json:"opacity"`
	FontSize     string  `json:"fontSize"`
	FontWeight   string  `json:"fontWeight"`
	Color        string  `json:"color"`
	Background   string  `json:"background"`
	BorderRadius string  `json:"borderRadius"`
}

type layoutReport struct {
	CollectedAt string `json:"collected_at"`
	Viewport    struct {
		W float64 `json:"w"`
		H float64 `json:"h"`
	} `json:"viewport"`
	Items []layoutItem `json:"items"`
}

// prettyLayout 打印布局表 + 异常判定（控件是否正常大小/样式）。
func prettyLayout(body []byte) {
	var rep layoutReport
	must(json.Unmarshal(body, &rep))
	fmt.Printf("viewport %.0f×%.0f   采集于 %s   控件 %d 个\n\n",
		rep.Viewport.W, rep.Viewport.H, rep.CollectedAt, len(rep.Items))
	fmt.Printf("  %-18s %-8s %10s %10s  %-7s %-22s %s\n",
		"gid", "tag", "尺寸(w×h)", "位置(x,y)", "字号", "文字色", "边框圆角")
	var problems []string
	visible := 0
	for _, it := range rep.Items {
		hidden := it.Display == "none" || it.Visibility == "hidden" || it.Opacity == "0"
		visible++
		if hidden {
			visible--
		}
		mark := ""
		// 交互控件必须有合理尺寸（滑杆按 range 形态另判：轨长≥80、盒高≥4）。
		if isInteractive(it) && !hidden {
			tooSmall := it.W < 8 || it.H < 8
			if strings.Contains(it.Cls, "gg-slider") {
				tooSmall = it.W < 80 || it.H < 4
			}
			if tooSmall {
				mark += " ⚠尺寸过小"
				problems = append(problems, fmt.Sprintf("%s(%s) 尺寸异常 %.0f×%.0f", it.GID, it.Tag, it.W, it.H))
			}
		}
		// 越界（可见元素）。
		if !hidden && (it.X < -0.5 || it.Y < -0.5 || it.X+it.W > rep.Viewport.W+0.5 || it.Y+it.H > rep.Viewport.H+0.5) {
			mark += " ⚠越界"
			problems = append(problems, fmt.Sprintf("%s(%s) 超出视口 @%.0f,%.0f", it.GID, it.Tag, it.X, it.Y))
		}
		// 字号异常。
		if fs := parsePx(it.FontSize); fs > 0 && fs < 10 && hasText(it) && !hidden {
			mark += " ⚠字号<10px"
			problems = append(problems, fmt.Sprintf("%s(%s) 字号 %s 过小", it.GID, it.Tag, it.FontSize))
		}
		fmt.Printf("  %-18s %-8s %6.0f×%-5.0f %6.0f,%-5.0f  %-7s %-22s %s%s\n",
			trunc(it.GID, 18), it.Tag, it.W, it.H, it.X, it.Y,
			it.FontSize, it.Color, it.BorderRadius, mark)
	}
	fmt.Printf("\n可见控件 %d/%d\n", visible, len(rep.Items))
	if len(problems) == 0 {
		fmt.Println("✓ 布局与样式未见异常（交互控件尺寸正常、无越界、字号达标）")
	} else {
		fmt.Printf("⚠ 发现 %d 处异常：\n", len(problems))
		for _, p := range problems {
			fmt.Println("  - " + p)
		}
	}
}

func isInteractive(it layoutItem) bool {
	if it.Tag == "button" || it.Tag == "input" || it.Tag == "textarea" || it.Tag == "select" {
		return true
	}
	return strings.Contains(it.Cls, "gg-tab") || strings.Contains(it.Cls, "gg-item")
}

func hasText(it layoutItem) bool {
	return it.Tag == "button" || it.Tag == "span" || it.Tag == "div"
}

func parsePx(v string) float64 {
	v = strings.TrimSuffix(strings.TrimSpace(v), "px")
	var f float64
	_, _ = fmt.Sscanf(v, "%f", &f)
	return f
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
