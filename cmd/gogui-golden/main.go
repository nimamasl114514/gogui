// gogui-golden —— 对照测试基线管理 CLI。
//
// 工作目录下维护 golden/ 基线库：
//
//	golden/img/<name>.png        像素基线（headless Chromium 截屏）
//	golden/img/<name>.json       采集环境 sidecar
//	golden/img/fail/…            校验失败三件套（当前图 + diff 高亮）
//	golden/tree/<name>.txt       控件树快照基线
//
// 用法：
//
//	gogui-golden            # check 模式：全量对照（不一致退出码 1）
//	gogui-golden -update    # 采集/刷新全部基线
//	gogui-golden -name demo # 只跑指定用例
//
// 采集环境变量：GOGUI_BROWSER 覆盖浏览器路径；GOGUI_GOLDEN 覆盖基线目录。
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/nimamasl114514/gogui/internal/demoapp"
	goguitest "github.com/nimamasl114514/gogui/testing"
	"github.com/nimamasl114514/gogui/theme"
)

// miniT 把 testing.T 的最小面适配为 golden 断言的 runner。
type miniT struct {
	name string
	err  string
}

func (m *miniT) Helper()                 {}
func (m *miniT) Name() string            { return m.name }
func (m *miniT) Logf(f string, a ...any) { fmt.Printf(f+"\n", a...) }
func (m *miniT) Fatalf(f string, a ...any) {
	m.err = fmt.Sprintf(f, a...)
}

func main() {
	update := flag.Bool("update", false, "采集/刷新基线（而非校验）")
	name := flag.String("name", "demo", "用例名（默认 demo）")
	flag.Parse()

	os.Setenv("GOGUI_UPDATE", map[bool]string{true: "1", false: "0"}[*update])

	app := demoapp.Build()
	th := theme.DarkGlow()

	type job struct {
		label string
		run   func(t goguitest.T)
	}
	jobs := []job{
		{"树快照对照", func(t goguitest.T) {
			goguitest.AssertGoldenTree(t, *name, app)
		}},
		{"像素对照", func(t goguitest.T) {
			goguitest.AssertGoldenImage(t, *name, app, th, demoapp.W, demoapp.H, 0)
		}},
	}

	rc := 0
	for _, j := range jobs {
		mt := &miniT{name: j.label}
		j.run(mt)
		if mt.err != "" {
			rc = 1
			fmt.Printf("✗ %s: %s\n", j.label, mt.err)
		} else {
			mode := "check"
			if *update {
				mode = "update"
			}
			fmt.Printf("✓ %s（%s）\n", j.label, mode)
		}
	}
	if rc == 0 {
		fmt.Println("MATCH：全部基线一致")
	} else {
		fmt.Println("FAIL：基线不一致（见上方详情）")
	}
	os.Exit(rc)
}
