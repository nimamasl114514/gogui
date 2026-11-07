package testing_test

import (
	"strings"
	"testing"

	"github.com/nimamasl114514/gogui/model"
	goguitest "github.com/nimamasl114514/gogui/testing"
)

// buildDemo 构建一个带交互的演示树：按钮累加、输入回显、开关变色、页签切换。
func buildDemo() *model.App {
	return model.New("demo", model.Size{W: 480, H: 400},
		model.VStack(
			model.Card(
				model.Label("无头测试演示").Class("h2"),
				model.Label("点击按钮累加").Class("muted"),
				model.HStack(
					model.Button("＋1").Primary().WithID("inc").OnClick(func(c *model.Ctx) {
						c.SetText("count", itoaS(c.Int("count")+1))
					}),
					model.Button("清零").Ghost().WithID("reset").OnClick(func(c *model.Ctx) {
						c.SetText("count", "0")
					}),
					model.Label("0").WithID("count").Class("strong"),
				),
				model.Input().WithID("echo-in").Placeholder("输入点什么…").OnChange(func(c *model.Ctx) {
					c.SetText("echo-out", c.Value("echo-in"))
				}),
				model.Label("（待输入）").WithID("echo-out").Class("muted"),
				model.HStack(
					model.Label("深色开关").Class("small"),
					model.Toggle().WithID("dark"),
				),
				model.Tabs(
					model.Tab{Title: "甲", Content: model.Label("甲内容").WithID("pa")},
					model.Tab{Title: "乙", Content: model.Label("乙内容").WithID("pb")},
				).WithID("tabs"),
				model.List("条目一", "条目二", "条目三").WithID("list"),
			).Padding(24).Gap(12),
		).Padding(24),
	)
}

func itoaS(n int) string { return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(intStr(n), "\n", ""), " ", "")) }

func intStr(n int) string {
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

func TestHeadlessFullFlow(t *testing.T) {
	h := goguitest.Open(buildDemo())

	// 1. 点击累加三次。
	for i := 0; i < 3; i++ {
		if err := h.Click("inc"); err != nil {
			t.Fatalf("click inc #%d: %v", i+1, err)
		}
	}
	if err := h.AssertText("count", "3"); err != nil {
		t.Fatal(err)
	}

	// 2. 清零。
	if err := h.Click("reset"); err != nil {
		t.Fatal(err)
	}
	if err := h.AssertText("count", "0"); err != nil {
		t.Fatal(err)
	}

	// 3. 输入回显。
	if err := h.Type("echo-in", "你好 gogui"); err != nil {
		t.Fatal(err)
	}
	if err := h.AssertText("echo-out", "你好 gogui"); err != nil {
		t.Fatal(err)
	}

	// 4. 开关翻转。
	if err := h.Toggle("dark"); err != nil {
		t.Fatal(err)
	}
	if err := h.AssertChecked("dark", true); err != nil {
		t.Fatal(err)
	}

	// 5. 页签切换 + 列表选中。
	if err := h.ClickTabs("tabs", 1); err != nil {
		t.Fatal(err)
	}
	if err := h.AssertActiveTab("tabs", 1); err != nil {
		t.Fatal(err)
	}
	if err := h.ClickList("list", 2); err != nil {
		t.Fatal(err)
	}
	if err := h.AssertSelectedIndex("list", 2); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotDeterministicAndReactive(t *testing.T) {
	app := buildDemo()
	h := goguitest.Open(app)
	s1 := h.Snapshot()
	s2 := h.Snapshot()
	if s1 != s2 {
		t.Fatal("同状态两次快照不一致")
	}
	if !strings.Contains(s1, `label#count.strong text="0"`) {
		t.Fatalf("快照缺 count 文本行:\n%s", s1)
	}
	_ = h.Click("inc")
	s3 := h.Snapshot()
	if s3 == s1 {
		t.Fatal("事件后快照未变化")
	}
	if !strings.Contains(s3, `label#count.strong text="1"`) {
		t.Fatalf("快照未反映状态:\n%s", s3)
	}
}

func TestHarnessUnknownWidget(t *testing.T) {
	h := goguitest.Open(buildDemo())
	if err := h.Click("ghost-id"); err == nil {
		t.Fatal("未知控件应报错")
	}
	if err := h.AssertText("ghost-id", ""); err == nil {
		t.Fatal("断言未知控件应报错")
	}
}
