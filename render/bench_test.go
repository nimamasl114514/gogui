package render

import (
	"testing"

	"github.com/nimamasl114514/gogui/model"
	"github.com/nimamasl114514/gogui/theme"
)

// buildBenchTree 构造 n 张卡片（每卡 6 控件）的典型仪表盘树。
func buildBenchTree(n int) *model.App {
	cards := make([]*model.Widget, 0, n)
	for i := 0; i < n; i++ {
		cards = append(cards, model.Card(
			model.Label("指标 <&> 名称").Class("h3"),
			model.Label("数值 12345").Class("metric"),
			model.Progress(0.42),
			model.HStack(
				model.Button("操作").Primary(),
				model.Badge("ok").Class("ok"),
			).Gap(8),
			model.Input().Value("用户输入 '引号'").Placeholder("请输入…"),
		).Gap(8).Padding(16))
	}
	return model.New("bench", model.Size{W: 980, H: 640},
		model.VStack(cards...).Gap(12).Padding(24))
}

func BenchmarkRenderSmall(b *testing.B) {
	app := buildBenchTree(3)
	th := theme.DarkGlow()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Body(app)
	}
	_ = th
}

func BenchmarkRenderMedium(b *testing.B) {
	app := buildBenchTree(40) // ~240 控件
	th := theme.DarkGlow()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = HTML(app, th)
	}
}

func BenchmarkRenderLarge(b *testing.B) {
	app := buildBenchTree(200) // ~1200 控件
	th := theme.DarkGlow()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = HTML(app, th)
	}
}

func BenchmarkEscapeHeavy(b *testing.B) {
	s := "普通文本没有特殊字符，只有中文和一些标点符号。"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = escape(s)
	}
}

func BenchmarkEscapeSpecials(b *testing.B) {
	s := `<div class="x">'a & b'</div>` + "中文内容"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = escape(s)
	}
}
