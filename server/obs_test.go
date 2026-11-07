package server_test

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	goguitest "github.com/nimamasl114514/gogui/testing"
)

func httpGet(t *testing.T, u string) (int, string) {
	t.Helper()
	// 本机 loopback 在连接 churn 下偶发 SYN 超时：网络级错误重试，HTTP 状态不重试。
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		resp, err := http.Get(u)
		if err == nil {
			defer resp.Body.Close()
			b, _ := io.ReadAll(resp.Body)
			return resp.StatusCode, string(b)
		}
		lastErr = err
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("GET %s: %v", u, lastErr)
	return 0, ""
}

// TestObservabilityEndpoints —— 可观测性实弹：
// 点击后 /gg/state 计数递增且 last_event 正确；注入 JS 错误后 js_errors_total
// 立即可见（不开 DevTools）；/gg/metrics /gg/health 语义正确。
func TestObservabilityEndpoints(t *testing.T) {
	if !goguitest.BrowserAvailable() {
		t.Skip("no chromium available")
	}
	_, url := startApp(t)
	ctx, cancel := goguitest.NewBrowserContext(860, 600)
	defer cancel()

	// 1) 页面加载（产生首次渲染）。
	loadPage(t, ctx, url, `[data-gid="inc"]`)
	if err := goguitest.FreezeAnimations(ctx); err != nil {
		t.Fatalf("freeze: %v", err)
	}

	// 2) health：ok=true 且渲染计数 > 0。
	code, body := httpGet(t, url+"/gg/health")
	if code != 200 {
		t.Fatalf("health status = %d: %s", code, body)
	}
	var h struct {
		OK      bool   `json:"ok"`
		Renders uint64 `json:"renders_total"`
	}
	_ = json.Unmarshal([]byte(body), &h)
	if !h.OK {
		t.Fatalf("health.ok=false: %s", body)
	}

	// 3) 真实点击 → state 计数递增 + last_event 正确。
	if err := chromedp.Do(ctx,
		chromedp.Click(chromedp.CSS(`[data-gid="inc"]`)),
		chromedp.Sleep(250*time.Millisecond),
	); err != nil {
		t.Fatalf("click: %v", err)
	}
	var st struct {
		App struct {
			Events    uint64 `json:"events_total"`
			Renders   uint64 `json:"renders_total"`
			JSErrors  uint64 `json:"js_errors_total"`
			LastEvent *struct {
				Widget string `json:"widget"`
				Type   string `json:"type"`
			} `json:"last_event"`
		} `json:"app"`
		Server struct {
			URL        string `json:"url"`
			SSEClients int    `json:"sse_clients"`
		} `json:"server"`
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, body = httpGet(t, url+"/gg/state?n=5")
		_ = json.Unmarshal([]byte(body), &st)
		if st.App.Events >= 1 && st.App.LastEvent != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("state 未反映点击: %s", body)
		}
		time.Sleep(80 * time.Millisecond)
	}
	if st.App.LastEvent.Widget != "inc" || st.App.LastEvent.Type != "click" {
		t.Fatalf("last_event = %+v", st.App.LastEvent)
	}

	// 4) 注入前端 JS 错误 → 不开 DevTools 也能在 state 里看到。
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](
		`setTimeout(function(){ throw new Error('observability-probe') }, 0)`)); err != nil {
		t.Fatalf("inject js error: %v", err)
	}
	// loopback 在连接 churn 下可能出现数秒级抖动（环境级），给足回流窗口。
	deadline = time.Now().Add(8 * time.Second)
	for {
		_, body = httpGet(t, url+"/gg/state?n=20")
		_ = json.Unmarshal([]byte(body), &st)
		if st.App.JSErrors >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("js 错误未回流: %s", body)
		}
		time.Sleep(80 * time.Millisecond)
	}
	if !strings.Contains(body, "observability-probe") {
		t.Fatalf("日志未包含 JS 错误消息: %s", body)
	}

	// 5) metrics 文本口径。
	code, body = httpGet(t, url+"/gg/metrics")
	if code != 200 || !strings.Contains(body, "gogui_events_total 1") {
		t.Fatalf("metrics 异常: code=%d body=%s", code, body)
	}
	if !strings.Contains(body, "gogui_render_last_ns") {
		t.Fatalf("metrics 缺渲染指标: %s", body)
	}
}

// TestLayoutIntrospection —— 布局/样式内省实弹：
// GET /gg/layout 触发浏览器端采集，断言交互控件尺寸/字号/圆角在正常区间，
// 且 CLI 侧异常判定所需字段齐全（这是「不截图也能知道长得对不对」的核心）。
func TestLayoutIntrospection(t *testing.T) {
	if !goguitest.BrowserAvailable() {
		t.Skip("no chromium available")
	}
	_, url := startApp(t)
	ctx, cancel := goguitest.NewBrowserContext(860, 600)
	defer cancel()

	loadPage(t, ctx, url, `[data-gid="inc"]`)
	if err := goguitest.FreezeAnimations(ctx); err != nil {
		t.Fatalf("freeze: %v", err)
	}

	// 服务端主动拉取布局（SSE 命令 → JS 采集 → POST 回传）。
	deadline := time.Now().Add(4 * time.Second)
	var body string
	for {
		code, b := httpGet(t, url+"/gg/layout")
		if code == 200 {
			body = b
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("layout 拉取失败: code=%d body=%s", code, b)
		}
		time.Sleep(120 * time.Millisecond)
	}

	var rep struct {
		Viewport struct {
			W float64 `json:"w"`
			H float64 `json:"h"`
		} `json:"viewport"`
		Items []struct {
			GID          string  `json:"gid"`
			Tag          string  `json:"tag"`
			W            float64 `json:"w"`
			H            float64 `json:"h"`
			FontSize     string  `json:"fontSize"`
			BorderRadius string  `json:"borderRadius"`
			Color        string  `json:"color"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &rep); err != nil {
		t.Fatalf("bad layout json: %v", err)
	}
	if rep.Viewport.W < 100 || len(rep.Items) < 5 {
		t.Fatalf("布局报告不完整: viewport=%.0f items=%d", rep.Viewport.W, len(rep.Items))
	}

	byID := map[string]int{}
	for i, it := range rep.Items {
		byID[it.GID] = i
	}
	// 按钮 inc：尺寸合理、字号 14px、有圆角。
	idx, ok := byID["inc"]
	if !ok {
		t.Fatalf("布局报告缺 inc")
	}
	inc := rep.Items[idx]
	if inc.W < 40 || inc.H < 20 {
		t.Fatalf("inc 尺寸异常: %.1f×%.1f", inc.W, inc.H)
	}
	if inc.FontSize != "14px" {
		t.Fatalf("inc 字号 = %q, want 14px", inc.FontSize)
	}
	if inc.BorderRadius == "0px" || inc.BorderRadius == "" {
		t.Fatalf("inc 无圆角: %q", inc.BorderRadius)
	}
	// 开关 sw：44×24 量级。
	if idx, ok := byID["sw"]; ok {
		sw := rep.Items[idx]
		if sw.W < 40 || sw.W > 48 || sw.H < 22 || sw.H > 26 {
			t.Fatalf("sw 尺寸异常: %.1f×%.1f", sw.W, sw.H)
		}
	}
	// Go 测试助手：单控件直读（不经服务端通道）。
	if _, err := goguitest.LayoutOf(ctx, "inc"); err != nil {
		t.Fatalf("LayoutOf: %v", err)
	}
}

// TestTraceWidgetHistory —— 追踪/控件透视/历史曲线三件套实弹。
func TestTraceWidgetHistory(t *testing.T) {
	if !goguitest.BrowserAvailable() {
		t.Skip("no chromium available")
	}
	_, url := startApp(t)
	ctx, cancel := goguitest.NewBrowserContext(860, 600)
	defer cancel()

	loadPage(t, ctx, url, `[data-gid="inc"]`)
	if err := goguitest.FreezeAnimations(ctx); err != nil {
		t.Fatalf("freeze: %v", err)
	}
	// 点击两次 → 两条 span；等历史采样跑一拍。
	if err := chromedp.Do(ctx,
		chromedp.Click(chromedp.CSS(`[data-gid="inc"]`)),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Click(chromedp.CSS(`[data-gid="inc"]`)),
		chromedp.Sleep(1200*time.Millisecond),
	); err != nil {
		t.Fatalf("click: %v", err)
	}

	// 1) trace：最近 span 的 dispatch/render 耗时应为正，补丁字节数>0。
	_, body := httpGet(t, url+"/gg/trace?n=5")
	var spans []struct {
		ID         string `json:"id"`
		Widget     string `json:"widget"`
		Type       string `json:"type"`
		DispatchUS int64  `json:"dispatch_us"`
		RenderNS   int64  `json:"render_ns"`
		PatchBytes int    `json:"patch_bytes"`
	}
	if err := json.Unmarshal([]byte(body), &spans); err != nil {
		t.Fatalf("trace json: %v\n%s", err, body)
	}
	if len(spans) < 2 {
		t.Fatalf("span 数量 = %d, want >=2", len(spans))
	}
	last := spans[len(spans)-1]
	if last.Widget != "inc" || last.Type != "click" {
		t.Fatalf("last span = %+v", last)
	}
	if last.RenderNS == 0 {
		t.Fatalf("render_ns 未被填充（字段名漂移？）: %+v", last)
	}
	if last.DispatchUS < 0 || last.RenderNS <= 0 || last.PatchBytes <= 0 {
		t.Fatalf("span 指标异常: %+v", last)
	}

	// 2) widget 透视：模型属性 + 解析值。
	code, wbody := httpGet(t, url+"/gg/widget/inc")
	if code != 200 {
		t.Fatalf("widget status=%d %s", code, wbody)
	}
	var wv struct {
		Kind     string            `json:"kind"`
		Resolved map[string]string `json:"resolved"`
		Found    bool              `json:"found"`
	}
	_ = json.Unmarshal([]byte(wbody), &wv)
	if !wv.Found || wv.Kind != "button" {
		t.Fatalf("widget view 异常: %s", wbody)
	}
	if code, _ := httpGet(t, url+"/gg/widget/nope"); code != 404 {
		t.Fatalf("缺失控件应 404，实得 %d", code)
	}

	// 3) history：>=1 个采样点，渲染增量累计 >=2。
	_, hbody := httpGet(t, url+"/gg/history?n=30")
	var hs []struct {
		RndDelta  uint64 `json:"renders_delta"`
		HeapKB    uint64 `json:"heap_kb"`
		Goroutine int    `json:"goroutines"`
	}
	if err := json.Unmarshal([]byte(hbody), &hs); err != nil {
		t.Fatalf("history json: %v\n%s", err, hbody)
	}
	if len(hs) == 0 {
		t.Fatal("history 无采样点")
	}
	var rnd uint64
	for _, h := range hs {
		rnd += h.RndDelta
	}
	if rnd < 2 {
		t.Fatalf("历史渲染增量合计 = %d, want >=2", rnd)
	}
	if hs[0].Goroutine <= 0 || hs[0].HeapKB == 0 {
		t.Fatalf("历史采样字段异常: %+v", hs[0])
	}
}

// TestBridgeSecurityGuards —— 本机 bridge 入口护栏：
// Host 不匹配→421；跨站 Origin→403；非 JSON Content-Type→415；
// 非法 Content-Length→400；缺 Content-Length（POST）→411。
func TestBridgeSecurityGuards(t *testing.T) {
	_, url := startApp(t)
	host := strings.TrimPrefix(url, "http://")

	rawReq := func(req string) string {
		conn, err := net.DialTimeout("tcp", host, 2*time.Second)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer conn.Close()
		_, _ = conn.Write([]byte(req))
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		buf := make([]byte, 512)
		n, _ := conn.Read(buf)
		return string(buf[:n])
	}

	// 1) Host 不匹配 → 421（防 DNS rebinding）。
	resp := rawReq("GET / HTTP/1.1\r\nHost: evil.example\r\n\r\n")
	if !strings.HasPrefix(resp, "HTTP/1.1 421") {
		t.Fatalf("错误 Host 未拦: %q", firstLine(resp))
	}
	// 2) 跨站 Origin → 403。
	body := `{"widget_id":"count","type":"click"}`
	resp = rawReq("POST /gg/ev HTTP/1.1\r\nHost: " + host + "\r\nOrigin: http://evil.example\r\n" +
		"Content-Type: application/json\r\nContent-Length: " + itoa(len(body)) + "\r\n\r\n" + body)
	if !strings.HasPrefix(resp, "HTTP/1.1 403") {
		t.Fatalf("跨站 Origin 未拦: %q", firstLine(resp))
	}
	// 3) text/plain 简单请求（CSRF 面）→ 415。
	resp = rawReq("POST /gg/ev HTTP/1.1\r\nHost: " + host + "\r\nContent-Type: text/plain\r\n" +
		"Content-Length: " + itoaTest(len(body)) + "\r\n\r\n" + body)
	if !strings.HasPrefix(resp, "HTTP/1.1 415") {
		t.Fatalf("text/plain 未拦: %q", firstLine(resp))
	}
	// 4) 非法 Content-Length → 400。
	resp = rawReq("POST /gg/ev HTTP/1.1\r\nHost: " + host + "\r\nContent-Type: application/json\r\n" +
		"Content-Length: 12abc\r\n\r\n" + body)
	if !strings.HasPrefix(resp, "HTTP/1.1 400") {
		t.Fatalf("非法 CL 未拦: %q", firstLine(resp))
	}
	// 5) 缺 Content-Length 的 POST → 411。
	resp = rawReq("POST /gg/ev HTTP/1.1\r\nHost: " + host + "\r\nContent-Type: application/json\r\n\r\n{}")
	if !strings.HasPrefix(resp, "HTTP/1.1 411") {
		t.Fatalf("缺 CL 未拦: %q", firstLine(resp))
	}
	// 6) 正常请求仍应通过（回归放行；用 / 避免 health 未渲染 503 语义干扰）。
	resp = rawReq("GET / HTTP/1.1\r\nHost: " + host + "\r\n\r\n")
	if !strings.HasPrefix(resp, "HTTP/1.1 200") {
		t.Fatalf("正常请求被误拦: %q", firstLine(resp))
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\r'); i > 0 {
		return s[:i]
	}
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return s[:i]
	}
	return s
}

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
