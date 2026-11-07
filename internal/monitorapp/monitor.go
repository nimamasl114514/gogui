// Package monitorapp 提供「系统监控」演示应用的控件树（v6 写法）。
//
// 心智模型只有一句：写树，挂 Var，闭包里随便读。
// 没有 AttachAll、没有占位值 + Bind 两段式、没有魔法串。
package monitorapp

import (
	"fmt"
	"runtime"

	"github.com/nimamasl114514/gogui"
	"github.com/nimamasl114514/gogui/model"
)

// Vars 是监控面板的全部数据源。零值可用；依赖由构建时的求值自动收集。
type Vars struct {
	CPU       gogui.RatioVar // 0..1（语义类型：只能喂给百分比展示）
	MemRatio  gogui.RatioVar // 0..1
	MemUsed   gogui.FloatVar // GB
	MemTotal  gogui.FloatVar // GB
	DiskRatio gogui.RatioVar
	DiskUsed  gogui.FloatVar // GB
	DiskTotal gogui.FloatVar // GB
	Goro      gogui.FloatVar
	GoroRatio gogui.RatioVar // 0..1
	Uptime    gogui.StringVar
	Clock     gogui.StringVar
	Events    gogui.StringVar
	Interval  gogui.FloatVar // 秒
	Cores     gogui.IntVar   // 逻辑核数（默认 runtime.NumCPU，golden 测试可固定）
}

// Build 构建监控面板。构造即绑定：控件直接接 *Var / 闭包，
// 依赖在构建时被自动收集 —— 没有挂载步骤。
func Build(v *Vars, onRefresh func()) *model.App {
	if onRefresh == nil {
		onRefresh = func() {}
	}
	if v.Cores.Get() == 0 {
		v.Cores.Set(runtime.NumCPU())
	}
	return gogui.New("gogui · 系统监控", gogui.Size{W: 980, H: 640},
		gogui.Col(
			gogui.Row(
				gogui.H1("系统监控"),
				gogui.OkBadge("实时"),
				gogui.Spacer(),
				gogui.Text(&v.Clock).Muted(),
			).Gap(gogui.SM).Align(gogui.Center),

			gogui.Row(
				statCard("CPU 使用率", &v.CPU, gogui.MetricPct(&v.CPU).WithID("cpu-val"),
					func() string {
						return fmt.Sprintf("%d 逻辑核心 · 繁忙 %.0f%%",
							v.Cores.Get(), v.CPU.Get().Percent())
					}),
				statCard("内存", &v.MemRatio, gogui.Metric(&v.MemUsed, "%.1f GB").WithID("mem-val"),
					func() string {
						return fmt.Sprintf("共 %.1f GB · 使用 %.0f%%",
							v.MemTotal.Get(), v.MemRatio.Get().Percent())
					}),
				statCard("磁盘 C:", &v.DiskRatio, gogui.Metric(&v.DiskUsed, "%.0f GB").WithID("disk-val"),
					func() string {
						return fmt.Sprintf("共 %.0f GB · 使用 %.0f%%",
							v.DiskTotal.Get(), v.DiskRatio.Get().Percent())
					}),
				statCard("Goroutines", &v.GoroRatio, gogui.Metric(&v.Goro, "%.0f").WithID("goro-val"),
					func() string { return "运行 " + v.Uptime.Get() }),
			).Gap(gogui.MD).Align(gogui.Stretch),

			gogui.Card(
				gogui.Row(
					gogui.H3("刷新间隔"),
					gogui.Slider(1, 10, &v.Interval).Width(280),
					gogui.TextFloat(&v.Interval, "%.0f 秒").Muted(),
					gogui.Spacer(),
					gogui.Button("立即刷新").Primary().OnClick(onRefresh),
				).Gap(gogui.MD).Align(gogui.Center),
			).Padding(gogui.LG),

			gogui.Card(
				gogui.H3("最近采样"),
				gogui.Text(&v.Events).Mono().Small().Muted(),
			).Gap(gogui.SM).Padding(gogui.LG).Grow(),
		).Padding(gogui.XL).Gap(gogui.MD).Align(gogui.Stretch),
	)
}

// statCard 指标卡：值 widget（预构造绑定）/进度条（Ratio）/说明组合。
func statCard(title string, level *gogui.RatioVar, val *model.Widget, sub func() string) *model.Widget {
	return gogui.Card(
		gogui.Muted(title),
		val,
		gogui.Progress(level),
		gogui.SmallFunc(sub),
	).Gap(gogui.SM).Padding(gogui.MD).Grow()
}
