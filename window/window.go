// Package window 提供 gogui 的窗口后端：真窗口（WebView2 / Edge app）
// 与无窗口（Headless serve）三种运行形态。
//
// Backend 是各形态的统一入口：起本地 server → 打开（或不打开）窗口 →
// 阻塞至窗口关闭 / 收到退出信号。
package window

import (
	"fmt"

	"github.com/nimamasl114514/gogui/model"
	"github.com/nimamasl114514/gogui/server"
	"github.com/nimamasl114514/gogui/theme"
)

// Backend 是一种窗口形态。Run 阻塞运行，返回即整个应用结束。
type Backend interface {
	Name() string
	Run(app *model.App, th *theme.Theme) error
}

// serveApp 为应用起本地服务（全部后端共用），返回已启动的 Server。
func serveApp(app *model.App, th *theme.Theme) (*server.Server, string, error) {
	if app == nil || app.Root == nil {
		return nil, "", fmt.Errorf("window: app 未设置根控件")
	}
	if th == nil {
		th = theme.DarkGlow()
	}
	srv := server.New(app, th)
	url, err := srv.Start()
	if err != nil {
		return nil, "", err
	}
	// 可观测性入口：URL 打到 stdout（gogui-inspect / curl 直接可用）。
	fmt.Printf("gogui: server listening on %s (try: /gg/state /gg/health /gg/metrics /gg/log)\n", url)
	return srv, url, nil
}
