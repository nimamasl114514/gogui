package server_test

import (
	"context"
	"errors"
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

// buildServerApp 构建带完整交互的宿主测试树。
func buildServerApp() *model.App {
	return model.New("bridge 测试", model.Size{W: 800, H: 520},
		model.VStack(
			model.Card(
				model.Label("桥接全链路").Class("h2"),
				model.HStack(
					model.Button("＋1").Primary().WithID("inc").OnClick(func(c *model.Ctx) {
						c.SetText("count", itoa(c.Int("count")+1))
					}),
					model.Label("0").WithID("count").Class("strong"),
				),
				model.Input().WithID("echo").Placeholder("输入").OnChange(func(c *model.Ctx) {
					c.SetText("echo-out", c.Value("echo"))
				}),
				model.Label("").WithID("echo-out").Class("muted"),
				model.Toggle().WithID("sw"),
				model.Slider(0, 100).WithID("sld").Value(10),
			).Padding(20).Gap(12),
		).Padding(16),
	)
}

func itoa(n int) string {
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

func errTimeout(msg string) error { return errors.New(msg) }

// waitLoopback 等待本机 loopback 恢复可用（listen+dial 自检）：
// 本机在 Chromium 高频创建/销毁下会出现数秒级的 127.0.0.1 连接超时窗口
// （Go 自身 dial 亦超时，属环境级故障，与框架无关），测试在此期间等待恢复。
// waitSSEClient 等待 /gg/state 报告至少 1 个 SSE 客户端（避免对齐窗口竞态）。
func waitSSEClient(t *testing.T, url string, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		code, body := httpGet(t, url+"/gg/state?n=0")
		if code == 200 && strings.Contains(body, `"sse_clients": 1`) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("等 SSE 客户端超时（%s）", d)
}

func waitLoopback(t *testing.T, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if ln, err := net.Listen("tcp", "127.0.0.1:0"); err == nil {
			addr := ln.Addr().String()
			if c, derr := net.DialTimeout("tcp", addr, 500*time.Millisecond); derr == nil {
				_ = c.Close()
				_ = ln.Close()
				return
			}
			_ = ln.Close()
		}
		if time.Now().After(deadline) {
			t.Fatalf("loopback 环境在 %s 内不可用（非框架问题）", timeout)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// loadPage 打开页面：导航失败自动重试（本机 localhost 偶发 SYN 抖动），
// 并等待 bridge 就绪（__ggBridgeReady）后再返回，消除交互竞态。
func loadPage(t *testing.T, ctx context.Context, url, waitSel string) {
	t.Helper()
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		// 导航前用 Go 侧 TCP 探活：本机 localhost 在高速 churn 下偶发 SYN 抖动，
		// 先确认目标端口可连，避免把环境抖动记成 bridge 竞态。
		if c, derr := net.DialTimeout("tcp", strings.TrimPrefix(url, "http://"), 2*time.Second); derr != nil {
			lastErr = derr
			waitLoopback(t, 20*time.Second)
			continue
		} else {
			_ = c.Close()
		}
		err := chromedp.Do(ctx,
			chromedp.Navigate(url),
			chromedp.WaitVisible(chromedp.CSS(waitSel)),
		)
		if err == nil {
			// 等 bridge 就绪（JS 轮询，避免 WaitFunc 签名差异）。
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				ok, e2 := chromedp.Run(ctx, chromedp.Evaluate[bool](`window.__ggBridgeReady === true`))
				if e2 == nil && ok {
					_ = chromedp.Do(ctx, chromedp.Sleep(60*time.Millisecond))
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
			lastErr = errTimeout("bridge not ready")
		} else {
			lastErr = err
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("loadPage: %v", lastErr)
}

// waitModel 轮询等待模型状态达成（替代固定 sleep，消灭慢机抖动）。
func waitModel(t *testing.T, app *model.App, id string, cond func(*model.Widget) bool, timeout time.Duration) *model.Widget {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if w := app.LookupWidget(id); w != nil && cond(w) {
			return w
		}
		if time.Now().After(deadline) {
			t.Fatalf("waitModel timeout: %s", id)
		}
		time.Sleep(30 * time.Millisecond)
	}
}

// waitDOMText 轮询 DOM 文本直到等于 want（或超时），返回最后一次读到的值。
// 固定 sleep 在慢机/高负载下会读到 morph 前的旧值；轮询把时序抖动吸收在测试基建里。
func waitDOMText(t *testing.T, ctx context.Context, sel, want string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for {
		got, err := chromedp.Run(ctx, chromedp.Text(chromedp.CSS(sel)))
		if err == nil {
			last = strings.TrimSpace(got)
			if last == want {
				return last
			}
		}
		if time.Now().After(deadline) {
			return last
		}
		time.Sleep(40 * time.Millisecond)
	}
}

// startApp 起服务并做「端口探活」后再交付 URL：
// 本机 loopback 在高速 churn 下偶发 connect 超时（OS 层），
// 不通就换随机端口重建（上限 6 次）——把环境抖动吸收在基建里。
func startApp(t *testing.T) (*model.App, string) {
	t.Helper()
	app := buildServerApp()
	for attempt := 0; attempt < 6; attempt++ {
		waitLoopback(t, 20*time.Second)
		srv := server.New(app, theme.DarkGlow())
		url, err := srv.Start()
		if err != nil {
			continue
		}
		ok := false
		for probe := 0; probe < 3; probe++ {
			c, derr := net.DialTimeout("tcp", strings.TrimPrefix(url, "http://"), 400*time.Millisecond)
			if derr == nil {
				_ = c.Close()
				ok = true
				break
			}
			time.Sleep(60 * time.Millisecond)
		}
		if ok {
			t.Cleanup(func() { _ = srv.Close() })
			return app, url
		}
		_ = srv.Close()
	}
	t.Fatal("startApp: 连续 6 个端口探活失败（本机 loopback 异常）")
	return nil, ""
}

// TestBrowserClickRoundTrip —— L2 全链路实弹：
// 真实 headless Chromium → 真实鼠标点击 → JS 委托捕获 → POST →
// App.Dispatch → 补丁 morph → DOM 更新 → 模型状态同步验证。
func TestBrowserClickRoundTrip(t *testing.T) {
	if !goguitest.BrowserAvailable() {
		t.Skip("no chromium available")
	}
	app, url := startApp(t)
	ctx, cancel := goguitest.NewBrowserContext(860, 600)
	defer cancel()

	loadPage(t, ctx, url, `[data-gid="inc"]`)
	if err := goguitest.FreezeAnimations(ctx); err != nil {
		t.Fatalf("freeze: %v", err)
	}
	err := chromedp.Do(ctx,
		chromedp.Click(chromedp.CSS(`[data-gid="inc"]`)),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Click(chromedp.CSS(`[data-gid="inc"]`)),
		chromedp.Sleep(150*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("browser run: %v", err)
	}
	domCount := waitDOMText(t, ctx, `[data-gid="count"]`, "2", 3*time.Second)
	if domCount != "2" {
		t.Fatalf("DOM count = %q, want 2", domCount)
	}
	// 模型状态与 DOM 必须一致（同一链路的两侧）。
	waitModel(t, app, "count", func(w *model.Widget) bool { return w.TextValue() == "2" }, 3*time.Second)
}

// TestBrowserInputRoundTrip —— 键盘输入走 input 委托 → 模型 → 回显。
func TestBrowserInputRoundTrip(t *testing.T) {
	if !goguitest.BrowserAvailable() {
		t.Skip("no chromium available")
	}
	app, url := startApp(t)
	ctx, cancel := goguitest.NewBrowserContext(860, 600)
	defer cancel()

	loadPage(t, ctx, url, `[data-gid="echo"]`)
	if err := goguitest.FreezeAnimations(ctx); err != nil {
		t.Fatalf("freeze: %v", err)
	}
	err := chromedp.Do(ctx,
		chromedp.SendKeys(chromedp.CSS(`[data-gid="echo"]`), "gogui"),
		// 等桥的 40ms debounce + 网络 + morph 完成。
		chromedp.Sleep(350*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("browser run: %v", err)
	}
	out := waitDOMText(t, ctx, `[data-gid="echo-out"]`, "gogui", 3*time.Second)
	domValue, err := chromedp.Run(ctx, chromedp.Evaluate[string](`document.querySelector('[data-gid="echo"]').value`))
	if err != nil {
		t.Fatalf("eval value: %v", err)
	}
	if strings.TrimSpace(out) != "gogui" {
		t.Fatalf("echo-out = %q, want gogui", out)
	}
	if domValue != "gogui" {
		t.Fatalf("input value = %q, want gogui", domValue)
	}
	waitModel(t, app, "echo", func(w *model.Widget) bool { return w.ValueText() == "gogui" }, 3*time.Second)
}

// TestBrowserToggleAndSlider —— 开关 change 与滑杆 input 数值链路。
func TestBrowserToggleAndSlider(t *testing.T) {
	if !goguitest.BrowserAvailable() {
		t.Skip("no chromium available")
	}
	app, url := startApp(t)
	ctx, cancel := goguitest.NewBrowserContext(860, 600)
	defer cancel()

	loadPage(t, ctx, url, `[data-gid="sw"]`)
	if err := goguitest.FreezeAnimations(ctx); err != nil {
		t.Fatalf("freeze: %v", err)
	}
	err := chromedp.Do(ctx,
		chromedp.Click(chromedp.CSS(`[data-gid="sw"]`)),
	)
	if err != nil {
		t.Fatalf("browser run: %v", err)
	}
	// 滑杆：派发 input 事件（CDP 拖拽 range 原生手势跨实现不稳定）。
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](`(() => {
		const el = document.querySelector('[data-gid="sld"]');
		el.value = 55;
		el.dispatchEvent(new Event('input', {bubbles: true}));
	})()`)); err != nil {
		t.Fatalf("eval slider: %v", err)
	}
	if err := chromedp.Do(ctx, chromedp.Sleep(300*time.Millisecond)); err != nil {
		t.Fatalf("sleep: %v", err)
	}
	if err != nil {
		t.Fatalf("browser run: %v", err)
	}
	// 轮询 aria（瞬断/重试下 morph 落地时间不定）。
	deadline := time.Now().Add(3 * time.Second)
	aria := ""
	for {
		var e2 error
		aria, e2 = chromedp.Run(ctx, chromedp.Evaluate[string](`document.querySelector('[data-gid="sw"]').getAttribute('aria-checked')`))
		if e2 != nil {
			t.Fatalf("eval aria: %v", e2)
		}
		if aria == "true" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("switch aria-checked = %q, want true", aria)
		}
		time.Sleep(50 * time.Millisecond)
	}
	waitModel(t, app, "sw", func(w *model.Widget) bool { return w.CheckedValue() }, 3*time.Second)
	if w := app.LookupWidget("sld"); w == nil || w.SliderValue() != 55 {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if w2 := app.LookupWidget("sld"); w2 != nil && w2.SliderValue() == 55 {
				break
			}
			time.Sleep(30 * time.Millisecond)
		}
		if w := app.LookupWidget("sld"); w == nil || w.SliderValue() != 55 {
			probe, _ := chromedp.Run(ctx, chromedp.Evaluate[string](`(async () => {
				let net = 'untested';
				try {
					const r = await fetch('/gg/state?n=0', {cache: 'no-store'});
					net = 'ok:' + r.status;
				} catch (e) { net = 'fail:' + e; }
				return JSON.stringify({
					attempts: window.__ggPostAttempts || 0,
					ok: window.__ggPostOk || 0,
					frames: window.__ggFrames || 0,
					ready: window.__ggBridgeReady === true,
					lastErr: window.__ggLastPostErr || '',
					liveNet: net,
					sliderClass: (document.querySelector('[data-gid="sld"]')||{}).className || 'missing'
				});
			})()`, chromedp.EvalAwaitPromise))
			_, traceBody := httpGet(t, url+"/gg/trace?n=5")
			_, stateBody := httpGet(t, url+"/gg/state?n=0")
			t.Fatalf("model slider = %v, want 55\nPROBE: %s\nTRACE: %s\nSTATE: %s", w.SliderValue(), probe, traceBody, stateBody)
		}
	}
}

// TestSSEGoSideUpdate —— Go 侧 Update 推送 → SSE → DOM 实时更新。
func TestSSEGoSideUpdate(t *testing.T) {
	if !goguitest.BrowserAvailable() {
		t.Skip("no chromium available")
	}
	app, url := startApp(t)
	ctx, cancel := goguitest.NewBrowserContext(860, 600)
	defer cancel()

	loadPage(t, ctx, url, `[data-gid="count"]`)
	if err := goguitest.FreezeAnimations(ctx); err != nil {
		t.Fatalf("freeze: %v", err)
	}
	waitSSEClient(t, url, 3*time.Second)
	// Go 侧直接改状态（模拟定时器/网络回调）。
	app.Update(func(c *model.Ctx) { c.SetText("count", "99") })
	if err := chromedp.Do(ctx,
		chromedp.Sleep(450*time.Millisecond),
	); err != nil {
		t.Fatalf("sleep: %v", err)
	}
	domCount := waitDOMText(t, ctx, `[data-gid="count"]`, "99", 3*time.Second)
	if domCount != "99" {
		t.Fatalf("DOM count = %q, want 99 (SSE 未生效)", domCount)
	}
}
