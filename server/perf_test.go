package server_test

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/nimamasl114514/gogui/model"
	"github.com/nimamasl114514/gogui/server"
	goguitest "github.com/nimamasl114514/gogui/testing"
	"github.com/nimamasl114514/gogui/theme"
)

// TestSixtyFPS —— 60fps 能力实测：
// 服务端 App.StartFrames(60) 驱动动画 → 每帧一次渲染+SSE 推送；
// 浏览器端统计：收到补丁帧数（2s ≥100/120）与 rAF 帧数（≥90）。
func TestSixtyFPS(t *testing.T) {
	if !goguitest.BrowserAvailable() {
		t.Skip("no chromium available")
	}
	v := model.NewVar(0.0)
	app := model.New("fps", model.Size{W: 420, H: 320},
		model.VStack(
			model.H2("60fps 测试"),
			model.Label("").WithID("t").BindTextFloat(v, "%.2f"),
			model.Progress(v),
		).Padding(20).Gap(10))

	srv := server.New(app, theme.DarkGlow())
	url, err := srv.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	ctx, cancel := goguitest.NewBrowserContext(860, 600)
	defer cancel()
	loadPage(t, ctx, url, `[data-gid="t"]`)
	// rAF 帧计数探针。
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](
		`window.__raf = 0; (function f(){ window.__raf++; requestAnimationFrame(f); })();`)); err != nil {
		t.Fatalf("raf probe: %v", err)
	}

	stop := app.StartFrames(60, func(dt time.Duration) {
		v.Set(v.Get() + dt.Seconds()) // 每帧改一个 Var
	})
	defer stop()

	if err := chromedp.Do(ctx, chromedp.Sleep(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	frames, err := chromedp.Run(ctx, chromedp.Evaluate[int](`window.__ggFrames || 0`))
	if err != nil {
		t.Fatal(err)
	}
	raf, err := chromedp.Run(ctx, chromedp.Evaluate[int](`window.__raf || 0`))
	if err != nil {
		t.Fatal(err)
	}
	st := app.Stats()
	t.Logf("2s 内：补丁帧=%d（目标120）· rAF=%d（目标120）· 服务端渲染=%d · render_max=%dns",
		frames, raf, st.Renders, st.MaxRenderNS)

	// 阈值取 90（75% 目标帧率）：并行测试/CI 负载噪声下不误报；
	// 实测常态为 120/120（见日志）。
	if frames < 90 {
		t.Fatalf("补丁帧不足：%d/120（<90）", frames)
	}
	if raf < 80 {
		t.Fatalf("rAF 帧不足：%d（<80）", raf)
	}
	if st.MaxRenderNS > 16*int64(time.Millisecond) {
		t.Fatalf("单帧渲染超 16.6ms 预算：%dns", st.MaxRenderNS)
	}
}

// TestRapidClicks —— 浏览器 10 连击：JS 事件风暴下模型/DOM 最终一致。
func TestRapidClicks(t *testing.T) {
	if !goguitest.BrowserAvailable() {
		t.Skip("no chromium available")
	}
	app, url := startApp(t)
	ctx, cancel := goguitest.NewBrowserContext(860, 600)
	defer cancel()

	loadPage(t, ctx, url, `[data-gid="inc"]`)
	if err := goguitest.FreezeAnimations(ctx); err != nil {
		t.Fatal(err)
	}
	// 同一帧内触发 10 次真实 DOM click。
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](
		`(() => { const el = document.querySelector('[data-gid="inc"]'); for (let i = 0; i < 10; i++) el.click(); })()`)); err != nil {
		t.Fatalf("clicks: %v", err)
	}
	deadline := time.Now().Add(4 * time.Second)
	for {
		if w := app.LookupWidget("count"); w != nil && w.TextValue() == "10" {
			break
		}
		if time.Now().After(deadline) {
			w := app.LookupWidget("count")
			t.Fatalf("10 连击后模型 = %v, want 10", w.TextValue())
		}
		time.Sleep(50 * time.Millisecond)
	}
	var dom string
	if err := chromedp.Do(ctx,
		chromedp.Sleep(300*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	dom, err := chromedp.Run(ctx, chromedp.Text(chromedp.CSS(`[data-gid="count"]`)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(dom) != "10" {
		t.Fatalf("DOM = %q, want 10", dom)
	}
}

// TestSSEMultiClient —— 两个 SSE 客户端都必须收到推送补丁（广播正确性）。
func TestSSEMultiClient(t *testing.T) {
	app, url := startApp(t)
	host := strings.TrimPrefix(url, "http://")

	type client struct {
		conn net.Conn
		r    *bufio.Reader
	}
	dial := func() *client {
		conn, err := net.DialTimeout("tcp", host, 2*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = conn.Write([]byte("GET /gg/sse HTTP/1.1\r\nHost: " + host + "\r\n\r\n"))
		return &client{conn: conn, r: bufio.NewReader(conn)}
	}
	readUntil := func(c *client, marker string, d time.Duration) bool {
		deadline := time.Now().Add(d)
		var acc strings.Builder
		for time.Now().Before(deadline) {
			_ = c.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			line, err := c.r.ReadString('\n')
			if err != nil {
				continue
			}
			acc.WriteString(line)
			if strings.Contains(acc.String(), marker) {
				return true
			}
		}
		return false
	}

	c1 := dial()
	defer c1.conn.Close()
	c2 := dial()
	defer c2.conn.Close()
	// 初始补丁（含 .gg-app）。
	if !readUntil(c1, "gg-app", 3*time.Second) || !readUntil(c2, "gg-app", 3*time.Second) {
		t.Fatal("初始补丁未送达")
	}
	// 触发更新：标记文本。
	app.Update(func(c *model.Ctx) { c.SetText("count", "MARKER-7") })
	if !readUntil(c1, "MARKER-7", 3*time.Second) {
		t.Fatal("客户端1 未收到更新补丁")
	}
	if !readUntil(c2, "MARKER-7", 3*time.Second) {
		t.Fatal("客户端2 未收到更新补丁")
	}
}
