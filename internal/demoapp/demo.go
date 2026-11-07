// Package demoapp 提供 gogui 的官方演示应用「控件工坊」。
// 同一棵树被 cmd/demo（真窗展示）与 cmd/gogui-golden（对照基线）共用，
// 保证「看到的界面」与「被测试的界面」永远是同一份。
package demoapp

import (
	"github.com/nimamasl114514/gogui/model"
)

// Title / Size 是演示应用的窗口元信息。
const (
	Title = "gogui 控件工坊"
	W, H  = 880, 600
)

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

// Build 构建演示应用（确定性初始状态：计数 0 / 滑杆 42% / 页签 0）。
func Build() *model.App {
	return model.New("gogui 控件工坊", model.Size{W: 880, H: 600},
		model.HStack(
			// ---- 侧栏 ----
			model.VStack(
				model.Label("gogui").Class("h2"),
				model.Label("控件工坊 · WebView 渲染").Class("muted small"),
				model.Sep(),
				model.List("外观", "交互", "关于").WithID("nav").Gap(4),
				model.Spacer(),
				model.Label("v1.0 · 声明式").Class("small"),
			).Gap(10).Padding(22).Align("stretch").Justify("start"),
			// ---- 主区 ----
			model.VStack(
				model.HStack(
					model.Label("设置中心").Class("h1"),
					model.Spacer(),
					model.Badge("无头可测").Class("ok"),
					model.Badge("golden 对照").Class("warn"),
				).Gap(10),
				model.Tabs(
					model.Tab{
						Title: "外观",
						Content: model.VStack(
							model.Card(
								model.Label("主题与强调色").Class("h3"),
								model.HStack(
									model.Label("动态效果").Class("muted"),
									model.Toggle().WithID("fx-toggle").OnChange(func(c *model.Ctx) {
										if c.Checked("fx-toggle") {
											c.SetText("fx-state", "已开启")
										} else {
											c.SetText("fx-state", "已关闭")
										}
									}),
									model.Label("已开启").WithID("fx-state").Class("muted small"),
								).Gap(14).Align("center"),
								model.Sep(),
								model.HStack(
									model.Label("强调色").Class("muted"),
									model.Select("靛紫 · 闪烁", "青蓝 · 冰川", "绯红 · 熔岩").WithID("accent").SelectOption("靛紫 · 闪烁").OnChange(func(c *model.Ctx) {
										c.SetText("accent-tip", "已选：" + c.Selected("accent"))
									}),
									model.Label("已选：靛紫 · 闪烁").WithID("accent-tip").Class("muted small"),
								).Gap(14).Align("center"),
							).Gap(10),
							model.Card(
								model.Label("进度演示（拖动滑杆）").Class("h3"),
								model.Slider(0, 100).WithID("fill-slider").Value(42).OnChange(func(c *model.Ctx) {
									c.SetLevel("fill-progress", c.FloatValue("fill-slider")/100)
									c.SetText("fill-pct", itoa(int(c.FloatValue("fill-slider")))+"%")
								}),
								model.HStack(
									model.Progress(0.42).WithID("fill-progress"),
									model.Label("42%").WithID("fill-pct").Class("muted small"),
								).Gap(10).Align("center"),
							).Gap(10),
						).Gap(14),
					},
					model.Tab{
						Title: "交互",
						Content: model.VStack(
							model.Card(
								model.Label("计数器").Class("h3"),
								model.HStack(
									model.Button("－").WithID("dec").OnClick(func(c *model.Ctx) {
										c.SetInt("counter", c.Int("counter")-1)
									}),
									model.Label("0").WithID("counter").Class("h2"),
									model.Button("＋").Primary().WithID("inc").OnClick(func(c *model.Ctx) {
										c.SetInt("counter", c.Int("counter")+1)
									}),
									model.Button("重置").Ghost().WithID("reset").OnClick(func(c *model.Ctx) {
										c.SetInt("counter", 0)
									}),
								).Gap(12).Align("center"),
							).Gap(10),
							model.Card(
								model.Label("输入回显").Class("h3"),
								model.Input().WithID("name").Placeholder("输入你的名字…").OnChange(func(c *model.Ctx) {
									c.SetText("greet", "你好，"+c.Value("name")+"！")
								}),
								model.Textarea().WithID("notes").Placeholder("备注（多行）…"),
								model.Label("你好！").WithID("greet").Class("muted"),
							).Gap(10),
							model.Card(
								model.HStack(
									model.Label("危险区").Class("h3"),
									model.Spacer(),
									model.Button("清空数据").Danger(),
								).Gap(10).Align("center"),
							).Gap(10),
						).Gap(14),
					},
					model.Tab{
						Title: "关于",
						Content: model.VStack(
							model.Card(
								model.Label("gogui 框架").Class("h3"),
								model.Label("声明式 Go 控件树 → HTML/CSS 渲染").Class("muted"),
								model.Label("同一条事件链路：浏览器点击 = 无头 harness 点击").Class("muted"),
								model.Sep(),
								model.HStack(
									model.Badge("model"),
									model.Badge("theme").Class("ok"),
									model.Badge("render").Class("warn"),
									model.Badge("server").Class("danger"),
									model.Badge("window").Class("ok"),
								).Gap(8),
								model.Label("无头操作 · golden 像素对照 · 树快照对照").Class("small"),
							).Gap(10),
						).Gap(14),
					},
				).WithID("tabs").Gap(0),
			).Gap(14).Padding(26).Align("stretch").Justify("start"),
		).Gap(0).Align("stretch").Justify("start"),
	)
}
