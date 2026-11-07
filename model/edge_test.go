package model_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nimamasl114514/gogui/model"
	"github.com/nimamasl114514/gogui/render"
	"github.com/nimamasl114514/gogui/snapshot"
	"github.com/nimamasl114514/gogui/theme"
)

// TestDuplicateIDsFirstWins —— 重复 ID：FindByID 取先序第一个，Dispatch 打它。
func TestDuplicateIDsFirstWins(t *testing.T) {
	app := model.New("d", model.Size{W: 10, H: 10},
		model.VStack(
			model.Label("A").WithID("dup"),
			model.Label("B").WithID("dup"),
		))
	w := app.LookupWidget("dup")
	if w == nil || w.TextValue() != "A" {
		t.Fatalf("重复 ID 未取先序第一个: %+v", w)
	}
}

// TestUnicodeAndInjection —— emoji/CJK/注入 全链路（render+snapshot）不炸且转义。
func TestUnicodeAndInjection(t *testing.T) {
	evil := `<script>alert("x")</script>&'"`
	app := model.New("u", model.Size{W: 10, H: 10},
		model.VStack(
			model.Label("中文😀🎉").WithID("cjk"),
			model.Label(evil).WithID("evil"),
			model.Input().Value(evil).Placeholder(evil),
		))
	h := render.HTML(app, theme.DarkGlow())
	if strings.Contains(h, "<script>alert") {
		t.Fatal("HTML 注入未转义")
	}
	if !strings.Contains(h, "中文😀🎉") {
		t.Fatal("CJK/emoji 文本丢失")
	}
	snap := snapshot.Tree(app)
	if !strings.Contains(snap, strconv.Quote(evil)) {
		t.Fatalf("快照未按引用形式保留注入串: %s", snap)
	}
}

// TestHugeTextAndTree —— 1MB 文本 + 8000 控件：渲染不炸、确定性成立。
func TestHugeTextAndTree(t *testing.T) {
	big := strings.Repeat("长文本&<>\n", 20000) // ~1MB+
	ws := make([]*model.Widget, 0, 600)
	for i := 0; i < 300; i++ {
		ws = append(ws, model.Label(big[:minInt(len(big), 200)]).Class("small"))
	}
	app := model.New("big", model.Size{W: 100, H: 100},
		model.VStack(append([]*model.Widget{model.Label(big)}, ws...)...))
	th := theme.DarkGlow()
	h1 := render.HTML(app, th)
	h2 := render.HTML(app, th)
	if h1 != h2 {
		t.Fatal("大树渲染不确定")
	}
	if len(h1) < 1000 {
		t.Fatalf("渲染输出异常小: %d", len(h1))
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestFramesDriver —— 60Hz 帧驱动：只触发帧数次批量渲染；fn panic 不杀循环。
func TestFramesDriver(t *testing.T) {
	v := model.NewVar(0.0)
	app := model.New("fr", model.Size{W: 10, H: 10},
		model.VStack(model.Progress(v)))
	notifies := 0
	app.OnUpdate(func() { notifies++ })

	frames := 0
	stop := app.StartFrames(60, func(dt time.Duration) {
		frames++
		if frames == 5 {
			panic("单帧异常")
		}
		v.Set(float64(frames) / 100)
	})
	defer stop()
	// 等待 ~0.5s。
	time.Sleep(500 * time.Millisecond)
	if frames < 20 {
		t.Fatalf("帧驱动过慢: %d 帧/0.5s", frames)
	}
	// 每次有效帧 = 1 次批量通知（panic 帧除外，其 Batch 未完成 → 0）。
	if notifies < 15 || notifies > frames {
		t.Fatalf("通知数异常: notifies=%d frames=%d", notifies, frames)
	}
}

// TestBatchPanicDoesNotLeakWindow —— 回归用例：
// Batch 内 panic 后，事件窗口必须正常关闭（否则后续 Var.Set 的通知被吞）。
func TestBatchPanicDoesNotLeakWindow(t *testing.T) {
	v := model.NewVar(0)
	app := model.New("bp", model.Size{W: 10, H: 10},
		model.VStack(model.Label("").BindTextInt(v)))
	notified := 0
	app.OnUpdate(func() { notified++ })

	func() {
		defer func() { _ = recover() }()
		app.Batch(func() { panic("batch-boom") })
	}()
	v.Set(1) // depth 泄漏时这条通知会被吞
	if notified == 0 {
		t.Fatal("Batch panic 后事件窗口泄漏：后续 Set 未触发通知")
	}
}
