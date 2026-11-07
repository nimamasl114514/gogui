package monitorapp_test

import (
	"strings"
	"testing"

	"github.com/nimamasl114514/gogui/internal/monitorapp"
	goguitest "github.com/nimamasl114514/gogui/testing"
)

// TestDisplayedValuesMatchDataSource —— 回归「CPU 显示 0%」类 bug：
// 面板上的每个数值文本必须与数据源一致（比例要乘 100 再显示）。
// 这层测试是 golden/布局测试覆盖不到的语义面：显示值 == 数据源。
func TestDisplayedValuesMatchDataSource(t *testing.T) {
	v := &monitorapp.Vars{}
	app := monitorapp.Build(v, nil)
	h := goguitest.Open(app)

	// 写入确定值。
	v.CPU.Set(0.37)
	v.MemUsed.Set(8.8)
	v.MemTotal.Set(15.7)
	v.MemRatio.Set(8.8 / 15.7)
	v.DiskUsed.Set(300)
	v.DiskTotal.Set(444)
	v.DiskRatio.Set(300.0 / 444.0)
	v.Goro.Set(5)
	v.Uptime.Set("2m41s")

	// 大字数值断言（此前 CPU 会显示 0%）。
	if err := h.AssertText("cpu-val", "37%"); err != nil {
		t.Fatal(err)
	}
	if err := h.AssertText("mem-val", "8.8 GB"); err != nil {
		t.Fatal(err)
	}
	if err := h.AssertText("disk-val", "300 GB"); err != nil {
		t.Fatal(err)
	}
	if err := h.AssertText("goro-val", "5"); err != nil {
		t.Fatal(err)
	}

	// 副行断言（繁忙 xx% / 使用 xx% 同一口径）。
	snap := h.Snapshot()
	for _, want := range []string{
		"繁忙 37%", // CPU 副行必须与大字一致
		"共 15.7 GB", "使用 56%",
		"共 444 GB", "使用 68%",
		"运行 2m41s",
	} {
		if !strings.Contains(snap, want) {
			t.Fatalf("快照缺少 %q：\n%s", want, snap)
		}
	}
	// 反向断言：绝不能出现「大字 0% 但副行 37%」的矛盾组合。
	if strings.Contains(snap, `text="0%"`) && strings.Contains(snap, "繁忙 37%") {
		t.Fatal("CPU 大字与副行口径不一致（0% vs 37%）—— Metric 百分比未乘 100")
	}
}
