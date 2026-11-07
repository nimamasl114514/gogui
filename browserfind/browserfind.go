// Package browserfind 在本机探测可用的 Chromium 内核浏览器。
// testing（L2 浏览器测试）与 window（EdgeApp 窗口后端）共用。
package browserfind

import (
	"os"
	"path/filepath"
)

// Chromium 优先探测本机 Chromium 内核：环境变量 GOGUI_BROWSER →
// Playwright 缓存 chromium → headless shell → Microsoft Edge → Google Chrome。
func Chromium() string {
	if p := os.Getenv("GOGUI_BROWSER"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	var candidates []string
	if local, err := os.UserCacheDir(); err == nil {
		pw := filepath.Join(local, "ms-playwright")
		if entries, err := filepath.Glob(filepath.Join(pw, "chromium-*")); err == nil {
			for _, e := range entries {
				candidates = append(candidates,
					filepath.Join(e, "chrome-win64", "chrome.exe"),
					filepath.Join(e, "chrome-win", "chrome.exe"))
			}
		}
		if entries, err := filepath.Glob(filepath.Join(pw, "chromium_headless_shell-*")); err == nil {
			for _, e := range entries {
				candidates = append(candidates,
					filepath.Join(e, "chrome-win64", "headless_shell.exe"),
					filepath.Join(e, "chrome-win", "headless_shell.exe"))
			}
		}
	}
	candidates = append(candidates,
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
	)
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// Available 报告是否找到可用浏览器。
func Available() bool { return Chromium() != "" }

// Edge 专找 Edge（EdgeApp 窗口后端）。
func Edge() string {
	for _, c := range []string{
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
	} {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}
