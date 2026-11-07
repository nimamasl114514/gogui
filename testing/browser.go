package testing

import (
	"context"
	"strings"

	"github.com/chromedp/chromedp"

	"github.com/nimamasl114514/gogui/browserfind"
)

// BrowserPath 自动探测本机可用的 Chromium 内核浏览器（详见 browserfind）。
func BrowserPath() string { return browserfind.Chromium() }

// BrowserAvailable 报告是否找到可用浏览器（找不到时浏览器级测试跳过）。
func BrowserAvailable() bool { return browserfind.Available() }

// FreezeAnimations 在已加载页面上冻结全部动效/过渡（消除点击时序竞态）。
func FreezeAnimations(ctx context.Context) error {
	_, err := chromedp.Run(ctx, chromedp.Evaluate[any](
		`document.documentElement.classList.add('gg-static')`))
	return err
}

// NewBrowserContext 创建一个 headless Chromium 会话（固定视口）。
// 返回 (ctx, cancel)：ctx 直接喂给 chromedp.Run / chromedp.Do。
//
// 典型用法：
//
//	ctx, cancel := goguitest.NewBrowserContext(900, 600)
//	defer cancel()
//	chromedp.Do(ctx, chromedp.Navigate(url))
func NewBrowserContext(width, height int) (context.Context, context.CancelFunc) {
	browser := browserfind.Chromium()
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
		chromedp.Flag("disable-background-timer-throttling", true),
		chromedp.Flag("force-device-scale-factor", "1"),
		chromedp.WindowSize(width, height),
	)
	if browser != "" {
		opts = append(opts, chromedp.ExecPath(browser))
	}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	return browserCtx, func() {
		cancelBrowser()
		cancelAlloc()
	}
}

// LayoutOf 读取指定控件在浏览器里的真实盒模型与关键计算样式
// （测试断言「大小/样式是否正常」用；运行时对照走 /gg/layout 端点）。
func LayoutOf(ctx context.Context, gid string) (map[string]any, error) {
	js := `(() => {
		const el = document.querySelector('[data-gid="' + gid + '"]');
		if (!el) return null;
		const b = el.getBoundingClientRect();
		const cs = getComputedStyle(el);
		return {
			x: b.x, y: b.y, w: b.width, h: b.height,
			display: cs.display, visibility: cs.visibility, opacity: cs.opacity,
			fontSize: cs.fontSize, fontWeight: cs.fontWeight,
			color: cs.color, background: cs.backgroundColor, borderRadius: cs.borderRadius
		};
	})()`
	return chromedp.Run(ctx, chromedp.Evaluate[map[string]any](strings.ReplaceAll(js, "' + gid + '", gid)))
}
