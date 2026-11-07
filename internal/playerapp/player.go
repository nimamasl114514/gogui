// Package playerapp —— 「Aurora Player」极限美丽 showcase：
// 全 SVG 生成专辑艺术 + 极光背景 + 旋转碟片 + CSS 频谱 + 60fps 播放头。
// 零外部素材、零第三方依赖，只依赖 gogui 框架本身。
package playerapp

import (
	"fmt"
	"strings"
	"time"

	"github.com/nimamasl114514/gogui"
	"github.com/nimamasl114514/gogui/model"
)

// Track 是一首演示曲目。
type Track struct {
	Title  string
	Artist string
	Dur    float64 // 秒
	// 三色渐变（专辑艺术 / 强调光）
	C1, C2, C3 string
}

// Tracks 是内置演示曲库（4 首，四套配色）。
var Tracks = []Track{
	{"Aurora Drive", "Neon Division", 222, "#7C5CFF", "#22D3EE", "#FF5CAA"},
	{"Midnight Bloom", "Vela", 248, "#FF5CAA", "#FFB86B", "#7C5CFF"},
	{"Glass Horizon", "Kite Theory", 207, "#22D3EE", "#34D399", "#7C5CFF"},
	{"Solar Rain", "Aya Mori", 301, "#FFB86B", "#FF5C7A", "#22D3EE"},
}

// Vars 是播放器全部状态（v6 依赖自动追踪）。
type Vars struct {
	Idx     gogui.IntVar
	Playing gogui.BoolVar
	Pos     gogui.FloatVar // 秒
	Dur     gogui.FloatVar
	Vol     gogui.FloatVar // 0..100
	Title   gogui.StringVar
	Artist  gogui.StringVar
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

func mmss(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	s := int(sec)
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// svgArt 生成 400×400 的专辑艺术（三色极光丝带 + 暗角）。
func svgArt(t Track) string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 400">`+
		`<defs>`+
		`<linearGradient id="g" x1="0" y1="0" x2="1" y2="1">`+
		`<stop offset="0" stop-color="%s"/><stop offset="1" stop-color="%s"/></linearGradient>`+
		`<filter id="b" x="-40%%" y="-40%%" width="180%%" height="180%%"><feGaussianBlur stdDeviation="16"/></filter>`+
		`<radialGradient id="v" cx="0.5" cy="0.42" r="0.78">`+
		`<stop offset="0.5" stop-color="#000" stop-opacity="0"/><stop offset="1" stop-color="#000" stop-opacity="0.6"/></radialGradient>`+
		`</defs>`+
		`<rect width="400" height="400" fill="url(#g)"/>`+
		`<g filter="url(#b)">`+
		`<ellipse cx="120" cy="150" rx="150" ry="40" fill="%s" opacity="0.55" transform="rotate(-18 120 150)"/>`+
		`<ellipse cx="290" cy="255" rx="170" ry="34" fill="#FFFFFF" opacity="0.16" transform="rotate(14 290 255)"/>`+
		`<ellipse cx="210" cy="80" rx="120" ry="22" fill="%s" opacity="0.45"/>`+
		`<ellipse cx="80" cy="320" rx="130" ry="26" fill="%s" opacity="0.35" transform="rotate(8 80 320)"/>`+
		`</g>`+
		`<rect width="400" height="400" fill="url(#v)"/>`+
		`</svg>`, t.C1, t.C2, t.C3, t.C2, t.C1)
}

// ExtraCSS 是 showcase 的应用级样式（框架设计系统之上的「美丽上限」层）。
func ExtraCSS() string {
	var b strings.Builder
	b.WriteString(`
/* ===== Aurora Player ===== */
.gg-app{position:relative;overflow:hidden;
  background-image:
    radial-gradient(1200px 700px at 50% 120%, rgba(124,92,255,.10), transparent 60%),
    linear-gradient(160deg, #0b1020 0%, #0a0e16 45%, #0c1224 100%) !important}
.gg-app>*{position:relative;z-index:1}
.gg-app::before,.gg-app::after{content:"";position:absolute;inset:-25%;pointer-events:none;z-index:0}
.gg-app::before{background:
  radial-gradient(42% 48% at 22% 28%, rgba(124,92,255,.38), transparent 62%),
  radial-gradient(38% 44% at 78% 72%, rgba(34,211,238,.30), transparent 62%);
  filter:blur(64px);animation:aurora1 26s ease-in-out infinite alternate}
.gg-app::after{background:
  radial-gradient(34% 40% at 72% 16%, rgba(255,92,170,.20), transparent 62%),
  radial-gradient(40% 44% at 62% 88%, rgba(34,211,238,.16), transparent 62%),
  radial-gradient(26% 30% at 18% 82%, rgba(52,211,153,.14), transparent 60%);
  filter:blur(84px);animation:aurora2 34s ease-in-out infinite alternate}
@keyframes aurora1{0%{transform:translate(-4%,-3%) rotate(0deg)}100%{transform:translate(5%,4%) rotate(9deg)}}
@keyframes aurora2{0%{transform:translate(4%,3%) rotate(0deg)}100%{transform:translate(-5%,-4%) rotate(-10deg)}}

.player-hero{display:flex;flex-direction:column;align-items:center;justify-content:center;position:relative}
.disc-wrap{position:relative;width:min(360px,36vh);aspect-ratio:1;margin-bottom:26px;flex:none}
.disc-glow{position:absolute;inset:-16px;border-radius:50%;display:block;
  background:conic-gradient(from 0deg, rgba(124,92,255,.35), rgba(34,211,238,.20), rgba(255,92,170,.25), rgba(124,92,255,.35));
  filter:blur(24px);animation:spin 20s linear infinite;opacity:.9}
.disc-wrap::after{content:"";position:absolute;left:50%;top:50%;width:56px;height:56px;margin:-28px;border-radius:50%;
  background:radial-gradient(circle at 50% 42%, #202a40 0 36%, #0a0e16 64%);
  box-shadow:inset 0 0 0 6px rgba(255,255,255,.05), 0 4px 18px rgba(0,0,0,.6)}
.disc{width:100%;height:100%;border-radius:50%;display:block;animation:spin 16s linear infinite;object-fit:cover;
  box-shadow:0 32px 90px rgba(2,6,18,.65), 0 0 0 1px rgba(255,255,255,.06)}
.disc.paused,.disc-glow.paused{animation-play-state:paused}
@keyframes spin{to{transform:rotate(360deg)}}

.hero-badges{display:flex;gap:8px;margin-top:4px}
.hero-foot{margin-top:22px;letter-spacing:.22em;font-size:10px;color:var(--text-muted);opacity:.75}
.viz{display:flex;align-items:flex-end;gap:4px;height:64px;margin-top:24px;width:min(420px,40vw);justify-content:center}
.viz-bar{flex:0 0 5px}
.viz-bar{width:4px;min-height:10px;border-radius:2px;
  background:linear-gradient(180deg, #22D3EE, #7C5CFF 70%);
  opacity:.85;animation:viz 1s ease-in-out infinite alternate}
.viz.paused .viz-bar{animation-play-state:paused}
@keyframes viz{from{height:14%}to{height:100%}}
.viz-bar:nth-child(3n){animation-duration:1.35s}
.viz-bar:nth-child(4n+1){animation-duration:.82s}
.viz-bar:nth-child(5n+2){animation-duration:1.6s}
`)
	for i := 1; i <= 48; i++ {
		delay := float64((i*7)%17) * 0.06
		fmt.Fprintf(&b, ".viz-bar:nth-child(%d){animation-delay:%.2fs}\n", i, delay)
	}
	b.WriteString(`
.player-panel{display:flex;flex-direction:column;justify-content:center}
.panel-kicker{letter-spacing:.10em;text-transform:uppercase;font-size:12px}
.now-title{font-size:33px;font-weight:800;letter-spacing:-.025em;margin-top:12px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.now-artist{color:var(--text-muted);font-size:15px;margin-top:6px}
.seek{margin-top:22px;height:9px !important;
  background:linear-gradient(90deg,#7C5CFF,#22D3EE) no-repeat, rgba(255,255,255,.20) !important;
  background-size:var(--fill,0%) 100% !important;background-color:transparent !important}
.seek::-webkit-slider-thumb{width:20px;height:20px;
  box-shadow:0 0 0 4px rgba(124,92,255,.22), 0 2px 10px rgba(0,0,0,.5) !important}
.time-row{display:flex;justify-content:space-between;margin-top:8px}
.ctl-row{display:flex;align-items:center;justify-content:center;margin-top:20px}
.ctl{width:54px;height:54px;border-radius:50%;font-size:20px;padding:0;color:var(--text)}
.ctl-main{width:76px;height:76px;border-radius:50%;font-size:24px;padding:0;
  animation:breath 3.2s ease-in-out infinite}
@keyframes breath{0%,100%{box-shadow:0 6px 26px rgba(124,92,255,.45)}50%{box-shadow:0 10px 44px rgba(124,92,255,.8)}}

.pl-card{margin-top:16px}
.track-row{display:flex;align-items:center;gap:14px;padding:9px 12px;border-radius:12px;cursor:pointer;
  transition:background .18s ease, transform .18s ease, box-shadow .18s ease}
.track-row:hover{background:rgba(255,255,255,.05);transform:translateX(3px)}
.track-row.sel{background:linear-gradient(90deg, rgba(124,92,255,.24), rgba(34,211,238,.10));
  box-shadow:inset 0 0 0 1px rgba(124,92,255,.35)}
.track-idx{width:26px;font-size:12px;color:var(--text-muted);font-variant-numeric:tabular-nums}
.track-name{font-weight:600;font-size:14px}
.track-artist{color:var(--text-muted);font-size:12px;margin-top:2px}
.track-dur{margin-left:auto;color:var(--text-muted);font-size:12px;font-variant-numeric:tabular-nums}
.track-playing{width:6px;height:6px;border-radius:50%;background:var(--accent);
  box-shadow:0 0 8px var(--accent-glow);margin-left:2px;opacity:0}
.track-row.playing .track-playing{opacity:1}
.vol-row{display:flex;align-items:center;margin-top:12px}
.vol-row .gg-label{white-space:nowrap}
.vol-row .gg-slider{background:linear-gradient(90deg,#7C5CFF,#22D3EE) no-repeat, rgba(255,255,255,.20) !important;
  background-size:var(--fill,0%) 100% !important;background-color:transparent !important}
`)
	return b.String()
}

// Build 构建播放器应用。
func Build(v *Vars) *model.App {
	v.Idx = *gogui.NewVar(0)
	v.Playing = *gogui.NewVar(false)
	v.Pos = *gogui.NewVar(0.0)
	v.Dur = *gogui.NewVar(Tracks[0].Dur)
	v.Vol = *gogui.NewVar(72.0)
	v.Title = *gogui.NewVar(Tracks[0].Title)
	v.Artist = *gogui.NewVar(Tracks[0].Artist)

	rows := make([]*model.Widget, 0, len(Tracks))
	for i, t := range Tracks {
		idx := i
		cls := "track-row"
		if idx == 0 {
			cls += " sel"
		}
		rows = append(rows, gogui.Row(
			gogui.Muted(itoa(idx + 1)).Class("track-idx"),
			gogui.VStack(
				gogui.Label(t.Title).Class("track-name"),
				gogui.Muted(t.Artist).Class("track-artist"),
			).Gap(0).Class("track-meta").Grow(),
			gogui.Muted(mmss(t.Dur)).Class("track-dur"),
			gogui.Label("").Class("track-playing"),
		).Class(cls).WithID("row-"+itoa(idx)).Gap(14).Align(gogui.Center).
			OnClick(func() { SetTrack(appHolder, v, idx, true) }))
	}

	bars := make([]*model.Widget, 0, 48)
	for i := 0; i < 48; i++ {
		bars = append(bars, gogui.Label("").Class("viz-bar"))
	}

	app := gogui.New("Aurora Player", gogui.Size{W: 1120, H: 720},
		gogui.Row(
			// ---------- 左：碟片 + 曲目 + 频谱 ----------
			gogui.VStack(
				gogui.VStack(
					gogui.Label("").WithID("disc-glow").Class("disc-glow", "paused"),
					gogui.SVGImage(svgArt(Tracks[0])).WithID("disc").Class("disc", "paused"),
				).Gap(0).Class("disc-wrap").WithID("disc-wrap"),
				gogui.Row(
					gogui.OkBadge("Hi-Res"),
					gogui.Badge("24bit · 96kHz"),
				).Gap(gogui.SM).Class("hero-badges"),
				gogui.Row(bars...).Class("viz", "paused").WithID("viz").Gap(4),
				gogui.Muted("AURORA SESSIONS · 2026").Class("hero-foot"),
			).Class("player-hero").Grow().Padding(32).Align(gogui.Center),
			// ---------- 右：控制面板 ----------
			gogui.VStack(
				gogui.Row(
					gogui.Muted("正在播放").Class("panel-kicker"),
					gogui.Spacer(),
					gogui.WarnBadge("PLAYLIST · 4"),
				).Align(gogui.Center),
				gogui.Text(&v.Title).WithID("now-title").Class("now-title"),
				gogui.Text(&v.Artist).WithID("now-artist").Class("now-artist"),
				gogui.Slider(0, Tracks[0].Dur, &v.Pos).WithID("seek").Class("seek"),
				gogui.Row(
					gogui.SmallFunc(func() string { return mmss(v.Pos.Get()) }).WithID("t-cur").Mono(),
					gogui.Spacer(),
					gogui.SmallFunc(func() string { return mmss(v.Dur.Get()) }).WithID("t-total").Mono(),
				).Class("time-row"),
				gogui.Row(
					gogui.Button("⏮").Ghost().WithID("prev").Class("ctl").
						OnClick(func(c *model.Ctx) { SetTrackC(c, v, v.Idx.Get()-1, v.Playing.Get()) }),
					gogui.Button("▶").Primary().WithID("play").Class("ctl-main").
						OnClick(func(c *model.Ctx) { TogglePlayC(c, v) }),
					gogui.Button("⏭").Ghost().WithID("next").Class("ctl").
						OnClick(func(c *model.Ctx) { SetTrackC(c, v, v.Idx.Get()+1, v.Playing.Get()) }),
				).Class("ctl-row").Gap(22),
				gogui.Card(
					gogui.Row(
						gogui.H3("播放列表"),
						gogui.Spacer(),
						gogui.Muted("4 首 · 16:18").Class("small"),
					).Align(gogui.Center),
					gogui.VStack(rows...).Gap(2),
				).Class("pl-card").WithID("pl-card").Gap(10).Padding(16),
				gogui.Row(
					gogui.Muted("音量").Class("small"),
					gogui.Slider(0, 100, &v.Vol).WithID("vol").Grow(),
					gogui.SmallFunc(func() string { return itoa(int(v.Vol.Get())) }).WithID("vol-num").Mono(),
				).Class("vol-row"),
			).Class("player-panel").Grow().Padding(34).Gap(6).Align(gogui.Stretch),
		).Gap(0).Align(gogui.Stretch),
	)
	appHolder = app
	return app
}

// appHolder 供构建期闭包引用（构建完成后赋值）。
var appHolder *model.App

// SetTrackC 切歌（Ctx 版：供事件处理器内部调用，避免重入 App 锁）。
func SetTrackC(c *model.Ctx, v *Vars, i int, autoplay bool) {
	n := len(Tracks)
	i = ((i % n) + n) % n
	t := Tracks[i]
	v.Idx.Set(i)
	v.Title.Set(t.Title)
	v.Artist.Set(t.Artist)
	v.Dur.Set(t.Dur)
	v.Pos.Set(0)
	c.SetProp("disc", model.PropSrc, svgDataURI(t))
	c.SetProp("seek", model.PropMax, t.Dur)
	for j := 0; j < n; j++ {
		c.RemoveClass("row-"+itoa(j), "sel")
		c.RemoveClass("row-"+itoa(j), "playing")
	}
	c.AddClass("row-"+itoa(i), "sel")
	if autoplay {
		c.AddClass("row-"+itoa(i), "playing")
	}
}

// SetTrack 切歌（App 版：供帧线程/外部 goroutine 调用）。
func SetTrack(app *model.App, v *Vars, i int, autoplay bool) {
	if app == nil {
		return
	}
	app.Update(func(c *model.Ctx) { SetTrackC(c, v, i, autoplay) })
}

// TogglePlayC 播放/暂停（Ctx 版）：按钮文案、碟片旋转、频谱冻结、行指示联动。
func TogglePlayC(c *model.Ctx, v *Vars) {
	on := !v.Playing.Get()
	v.Playing.Set(on)
	if on {
		c.SetProp("play", model.PropText, "❚❚")
		c.RemoveClass("disc", "paused")
		c.RemoveClass("disc-glow", "paused")
		c.RemoveClass("viz", "paused")
		c.AddClass("row-"+itoa(v.Idx.Get()), "playing")
	} else {
		c.SetProp("play", model.PropText, "▶")
		c.AddClass("disc", "paused")
		c.AddClass("disc-glow", "paused")
		c.AddClass("viz", "paused")
		c.RemoveClass("row-"+itoa(v.Idx.Get()), "playing")
	}
}

// TogglePlay 播放/暂停（App 版：供外部 goroutine 调用）。
func TogglePlay(app *model.App, v *Vars) {
	if app == nil {
		return
	}
	app.Update(func(c *model.Ctx) { TogglePlayC(c, v) })
}

// StartClock 启动 60fps 播放头：推进位置、曲终自动下一首。
func StartClock(app *model.App, v *Vars) (stop func()) {
	return app.StartFrames(60, func(dt time.Duration) {
		if !v.Playing.Get() {
			return
		}
		p := v.Pos.Get() + dt.Seconds()
		if p >= v.Dur.Get() {
			SetTrack(app, v, v.Idx.Get()+1, true)
			return
		}
		v.Pos.Set(p)
	})
}

// svgDataURI 把 SVG 编码成 data URI。
func svgDataURI(t Track) string { return model.SVGDataURI(svgArt(t)) }
