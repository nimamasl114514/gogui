package monitorapp_test

import (
	"os"
	"testing"

	"github.com/nimamasl114514/gogui/internal/monitorapp"
	goguitest "github.com/nimamasl114514/gogui/testing"
)

// TestMonitorTreeGolden —— 监控面板「全文 golden」：
// 固定全部数据源（含环境量 Cores）后整树快照对照基线。
// 任何显示文本（格式/单位/口径/布局文案）变化都会让本用例 FAIL ——
// 这是「CPU 显示 0%」类语义 bug 的第三层防线（前两层：Ratio 类型系统 + 数值对账断言）。
func TestMonitorTreeGolden(t *testing.T) {
	v := &monitorapp.Vars{}
	app := monitorapp.Build(v, nil)
	// 固定数据源（与 monitor_test.go 的断言同一组值）。
	v.Cores.Set(24)
	v.CPU.Set(0.37)
	v.MemUsed.Set(8.8)
	v.MemTotal.Set(15.7)
	v.MemRatio.Set(8.8 / 15.7)
	v.DiskUsed.Set(300)
	v.DiskTotal.Set(444)
	v.DiskRatio.Set(300.0 / 444.0)
	v.Goro.Set(5)
	v.GoroRatio.Set(5.0 / 64.0)
	v.Uptime.Set("2m41s")
	v.Clock.Set("12:00:00")
	v.Events.Set("12:00:00  CPU  37%   MEM  8.8/15.7 GB   协程 5\n")
	v.Interval.Set(1)

	if os.Getenv("GOGUI_GOLDEN") == "" {
		t.Setenv("GOGUI_GOLDEN", "../../golden")
	}
	goguitest.AssertGoldenTree(t, "monitor", app)
}
