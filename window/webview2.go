//go:build windows

package window

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"

	"github.com/nimamasl114514/gogui/model"
	"github.com/nimamasl114514/gogui/theme"
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	procSetDpiCtx        = user32.NewProc("SetProcessDpiAwarenessContext")
	procGetDpiForSystem  = user32.NewProc("GetDpiForSystem")
	procAdjustWindowRect = user32.NewProc("AdjustWindowRect")
	procSetWindowPos     = user32.NewProc("SetWindowPos")
)

// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4
const dpiAwarenessPerMonitorV2 = ^uintptr(3) // 0xFFFFFFFFFFFFFFFC

// enableHighDPI 在创建任何窗口前调用：声明 per-monitor-v2 DPI 感知。
// 不做这一步，Windows 会把 150%/200% 缩放下的窗口做位图拉伸 —— 文字必糊。
func enableHighDPI() {
	procSetDpiCtx.Call(dpiAwarenessPerMonitorV2)
}

// dpiScale 返回系统 DPI 缩放（96dpi=1.0；150% 界面=1.5）。
func dpiScale() float64 {
	dpi, _, _ := procGetDpiForSystem.Call()
	if dpi == 0 {
		return 1
	}
	return float64(dpi) / 96.0
}

// fixClientSize 把窗口外框调到「客户区 = 逻辑尺寸 × DPI」。
// jchv 用外框尺寸直接创建窗口，客户区会被标题栏/边框吃掉 ~32×39px；
// 这里按 WS_OVERLAPPEDWINDOW 精确换算并 SetWindowPos。
func fixClientSize(hwnd uintptr, logicalW, logicalH int) {
	scale := dpiScale()
	w := int32(float64(logicalW) * scale)
	h := int32(float64(logicalH) * scale)
	type rect struct{ Left, Top, Right, Bottom int32 }
	rc := rect{0, 0, w, h}
	const wsOverlappedWindow = 0x00CF0000
	procAdjustWindowRect.Call(uintptr(unsafe.Pointer(&rc)), wsOverlappedWindow, 0)
	const swpNoMove, swpNoZOrder, swpNoActivate = 0x0002, 0x0004, 0x0010
	procSetWindowPos.Call(hwnd, 0, 0, 0,
		uintptr(rc.Right-rc.Left), uintptr(rc.Bottom-rc.Top),
		swpNoMove|swpNoZOrder|swpNoActivate)
}

// Webview2 后端：WebView2 原生窗口（Win10/11 自带运行时）。
// 事件桥、补丁、SSE 全部走内置 server；窗口关闭即返回。
type webview2Backend struct{}

// Webview2 返回 WebView2 原生窗口后端（Windows 默认）。
func Webview2() Backend { return webview2Backend{} }

func (webview2Backend) Name() string { return "webview2" }

func (webview2Backend) Run(app *model.App, th *theme.Theme) error {
	srv, url, err := serveApp(app, th)
	if err != nil {
		return err
	}
	defer func() { _ = srv.Close() }()

	// 高 DPI 感知必须在窗口创建前声明（文字清晰度的第一根因）。
	enableHighDPI()

	// WebView2 用户数据目录放临时区，不污染 exe 所在目录。
	dataPath := filepath.Join(os.TempDir(), "gogui-webview2")
	scale := dpiScale()
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug: false,
		WindowOptions: webview2.WindowOptions{
			Title:  app.Title,
			Width:  uint(float64(app.Size.W) * scale),
			Height: uint(float64(app.Size.H) * scale),
			Center: true,
		},
		DataPath: dataPath,
	})
	if w == nil {
		return fmt.Errorf("webview2: 创建窗口失败（WebView2 运行时缺失？可用 EdgeApp() 兜底）")
	}
	defer w.Destroy()

	// 客户区精确对齐逻辑尺寸（去掉标题栏/边框吃掉的像素）。
	if hwnd := uintptr(w.Window()); hwnd != 0 {
		fixClientSize(hwnd, app.Size.W, app.Size.H)
	}

	w.Navigate(url)
	w.Run() // 阻塞消息循环；窗口关闭 → WMQuit → 返回
	return nil
}
