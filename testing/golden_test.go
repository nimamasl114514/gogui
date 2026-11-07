package testing_test

import (
	"os"
	"testing"

	"github.com/nimamasl114514/gogui/internal/demoapp"
	goguitest "github.com/nimamasl114514/gogui/testing"
	"github.com/nimamasl114514/gogui/theme"
)

// TestGoldenDemo —— 把 CLI 的对照基线并入 go test 体系：
// 同一份 golden/ 基线（demo）同时被 `go test` 与 `gogui-golden` 校验。
// GOGUI_UPDATE=1 go test 可刷新基线。
func TestGoldenDemo(t *testing.T) {
	if !goguitest.BrowserAvailable() {
		t.Skip("no chromium available")
	}
	if os.Getenv("GOGUI_GOLDEN") == "" {
		t.Setenv("GOGUI_GOLDEN", "../golden")
	}
	app := demoapp.Build()
	th := theme.DarkGlow()

	goguitest.AssertGoldenTree(t, "demo", app)
	goguitest.AssertGoldenImage(t, "demo", app, th, demoapp.W, demoapp.H, 0)
}
