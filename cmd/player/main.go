// Aurora Player —— gogui 极限美丽 showcase 入口。
package main

import (
	"github.com/nimamasl114514/gogui"
	"github.com/nimamasl114514/gogui/internal/playerapp"
	"github.com/nimamasl114514/gogui/theme"
)

func main() {
	v := &playerapp.Vars{}
	app := playerapp.Build(v)
	th := theme.DarkGlow()
	th.ExtraCSS = playerapp.ExtraCSS()
	th.ForceAnimations = true // showcase：本机 WebView2 报 reduce，不强制就全灭

	stop := playerapp.StartClock(app, v)
	defer stop()

	if err := gogui.Run(app, th, gogui.Webview2()); err != nil {
		panic(err)
	}
}
