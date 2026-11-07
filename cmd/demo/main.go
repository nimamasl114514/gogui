// gogui 控件工坊 —— 框架演示应用入口。
package main

import (
	"github.com/nimamasl114514/gogui"
	"github.com/nimamasl114514/gogui/internal/demoapp"
	"github.com/nimamasl114514/gogui/theme"
)

func main() {
	app := demoapp.Build()
	if err := gogui.Run(app, theme.DarkGlow(), gogui.Webview2()); err != nil {
		panic(err)
	}
}
