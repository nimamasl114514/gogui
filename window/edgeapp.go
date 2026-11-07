package window

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/nimamasl114514/gogui/browserfind"
	"github.com/nimamasl114514/gogui/model"
	"github.com/nimamasl114514/gogui/theme"
)

// edgeAppBackend：用本机 Edge/Chrome 的 --app 窗口打开 UI。
// WebView2 运行时不可用时的兜底形态（依据 JX-235：Start 不 Wait、
// 端口就绪再开窗，浏览器进程 fork 后宿主不与其生命周期耦合）。
type edgeAppBackend struct{}

// EdgeApp 返回 Edge/Chrome --app 窗口后端。
func EdgeApp() Backend { return edgeAppBackend{} }

func (edgeAppBackend) Name() string { return "edgeapp" }

func (edgeAppBackend) Run(app *model.App, th *theme.Theme) error {
	browser := browserfind.Edge()
	if browser == "" {
		browser = browserfind.Chromium()
	}
	if browser == "" {
		return fmt.Errorf("edgeapp: 未找到 Edge/Chrome")
	}
	srv, url, err := serveApp(app, th)
	if err != nil {
		return err
	}
	defer func() { _ = srv.Close() }()

	// 端口就绪后再开窗（serveApp 已保证监听成功）。
	cmd := exec.Command(browser,
		"--app="+url,
		fmt.Sprintf("--window-size=%d,%d", app.Size.W, app.Size.H),
		"--no-first-run",
		"--no-default-browser-check",
	)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("edgeapp: 启动浏览器失败: %w", err)
	}
	// 注意：不 Wait —— 浏览器进程会 fork，宿主不与其生命周期耦合。

	fmt.Printf("gogui[%s] 已启动: %s\n", app.Title, url)
	// 阻塞至 Ctrl+C / 终端信号。
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	_ = cmd.Process.Release()
	return nil
}

// headlessBackend：不起任何窗口，只起 server（打印 URL）。
// 供 CI、远程访问、golden 截图等场景。
type headlessBackend struct{}

// Headless 返回无窗口后端（阻塞至 Ctrl+C）。
func Headless() Backend { return headlessBackend{} }

func (headlessBackend) Name() string { return "headless" }

func (headlessBackend) Run(app *model.App, th *theme.Theme) error {
	srv, url, err := serveApp(app, th)
	if err != nil {
		return err
	}
	defer func() { _ = srv.Close() }()

	fmt.Printf("gogui[%s] headless: %s (Ctrl+C 退出)\n", app.Title, url)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	return nil
}
