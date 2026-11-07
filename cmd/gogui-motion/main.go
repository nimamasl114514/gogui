// gogui-motion —— 动态 GUI 验收：连拍帧序列 → 分区运动能量分析 → 期望判定。
//
// 为什么需要它：静态截图对动态界面是失明的 —— 旋转、动画、60fps 播放头、
// 暂停冻结、状态切换瞬态，一张静帧都测不到。本工具按「该动的地方动、
// 该静的地方静」做机器判定，并产出 contact sheet 与运动热图供人复核。
//
// 用法：
//
//	# 通过窗口标题定位（需 -capture 指定外部截图助手），ROI 由 /gg/layout 自动映射
//	gogui-motion -title "Aurora Player" -url http://127.0.0.1:PORT \
//	    -frames 20 -interval 120 -out shots/motion \
//	    -expect-motion disc,viz,seek -expect-static foot
//
//	# 手动指定 ROI（css px，逗号分隔 x,y,w,h；名字=矩形）
//	gogui-motion -hwnd 12345 -rois "disc=100,100,300,300" ...
//
// 退出码：0 期望满足；1 期望违背或捕获失败；2 参数错误。
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/chromedp"

	goguitest "github.com/nimamasl114514/gogui/testing"
)

// ---- 截图助手 JSON 输出结构 ----

type shotInfo struct {
	OK      bool   `json:"ok"`
	Path    string `json:"path"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Mode    string `json:"mode"`
	OriginX int    `json:"origin_x"`
	OriginY int    `json:"origin_y"`
}

type winInfo struct {
	Count   int `json:"count"`
	Windows []struct {
		Hwnd  uint64                                 `json:"hwnd"`
		Title string                                 `json:"title"`
		Rect  struct{ Left, Top, Right, Bottom int } `json:"rect"`
	} `json:"windows"`
}

// ---- /gg/layout 结构（只取映射所需字段） ----

type layoutItem struct {
	GID string  `json:"gid"`
	X   float64 `json:"x"`
	Y   float64 `json:"y"`
	W   float64 `json:"w"`
	H   float64 `json:"h"`
}
type layoutReport struct {
	Viewport struct {
		W float64 `json:"w"`
		H float64 `json:"h"`
	} `json:"viewport"`
	Items []layoutItem `json:"items"`
}

// roi 是图像坐标系里的矩形。
type roi struct {
	Name       string
	X, Y, W, H int
}

func main() {
	title := flag.String("title", "", "窗口标题关键字（与 -hwnd 二选一）")
	hwnd := flag.Uint64("hwnd", 0, "窗口句柄（与 -title 二选一）")
	url := flag.String("url", "", "应用 URL（提供则用 /gg/layout 自动映射 ROI）")
	frames := flag.Int("frames", 16, "连拍帧数")
	interval := flag.Int("interval", 120, "帧间隔（毫秒）")
	outDir := flag.String("out", "shots/motion", "输出目录")
	roisFlag := flag.String("rois", "", "手动 ROI：name=x,y,w,h[;name2=...]（CSS px）")
	expectMotion := flag.String("expect-motion", "", "期望有运动的 ROI 名（逗号分隔；all=全帧）")
	expectStatic := flag.String("expect-static", "", "期望静止的 ROI 名（逗号分隔）")
	threshMove := flag.Float64("thresh-move", 1.2, "运动判定阈值（相邻帧平均通道差，0-255）")
	quiet := flag.Bool("quiet", true, "静默要求：拍摄期间不得有输入事件（否则判定无效重拍）")
	warmup := flag.Int("warmup", 2, "丢弃前 N 帧（页面首绘/字体落位；不计入判定）")
	retries := flag.Int("retries", 2, "静默重拍次数上限")
	threshStill := flag.Float64("thresh-still", 1.0, "静止判定阈值（相邻帧平均通道差上限；动画背景上的文字区需按噪声底上调）")
	captureBin := flag.String("capture", defaultCapture(), "窗口模式截图助手路径（默认取 GOGUI_CAPTURE_BIN，或 PATH 中的 bm）")
	useCDP := flag.Bool("cdp", false, "用 headless Chromium（CDP）直采：零偏移、可用 CI、不受真人点击干扰（需 -url）")
	vpW := flag.Int("vw", 1120, "CDP 视口宽（CSS px）")
	vpH := flag.Int("vh", 720, "CDP 视口高（CSS px）")
	flag.Parse()

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "建输出目录失败:", err)
		os.Exit(2)
	}

	if !*useCDP && *title == "" && *hwnd == 0 {
		fmt.Fprintln(os.Stderr, "需要 -title 或 -hwnd（或使用 -cdp 模式）")
		os.Exit(2)
	}

	if *useCDP {
		if *url == "" {
			fmt.Fprintln(os.Stderr, "-cdp 模式需要 -url")
			os.Exit(2)
		}
		os.Exit(runCDP(*url, *outDir, *frames, *interval, *vpW, *vpH,
			*expectMotion, *expectStatic, *threshMove, *threshStill, *quiet, *retries))
	}

	// 1) 定位窗口。
	h := *hwnd
	if h == 0 {
		var err error
		h, err = findWindow(*captureBin, *title)
		if err != nil {
			fmt.Fprintln(os.Stderr, "定位窗口失败:", err)
			os.Exit(1)
		}
	}
	// 1.5) 窗口可能被最小化（PrintWindow 无法后台截取）—— 先还原再开拍。
	_, _ = runCapture(*captureBin, "pilot", "winctl", "--state", "normal", "--hwnd", strconv.FormatUint(h, 10), "--bg")
	time.Sleep(250 * time.Millisecond)

	// 2) 连拍（可选静默守卫：拍摄前后事件计数必须一致，否则重拍）。
	var paths []string
	var info0 shotInfo
	for attempt := 0; ; attempt++ {
		evBefore := eventsTotal(*url)
		dir := *outDir
		if attempt > 0 {
			dir = *outDir + fmt.Sprintf("-retry%d", attempt)
			_ = os.MkdirAll(dir, 0o755)
		}
		ps, i0, err := burst(*captureBin, h, dir, *frames, *interval)
		if err != nil {
			fmt.Fprintf(os.Stderr, "帧捕获失败: %v\n", err)
			os.Exit(1)
		}
		evAfter := eventsTotal(*url)
		if !*quiet || evBefore < 0 || evAfter < 0 || evBefore == evAfter {
			paths, info0, *outDir = ps, i0, dir
			break
		}
		if attempt >= *retries {
			fmt.Fprintf(os.Stderr, "✗ 环境不安静：拍摄期间检测到 %d 个输入事件（%d→%d），静态判定无效\n", evAfter-evBefore, evBefore, evAfter)
			os.Exit(1)
		}
		fmt.Printf("⚠ 拍摄期间有 %d 个输入事件，重拍（%d/%d）…\n", evAfter-evBefore, attempt+1, *retries)
		time.Sleep(300 * time.Millisecond)
	}

	// 3) ROI：优先 /gg/layout 自动映射，否则手动；都没有 → 全帧。
	rois, err := buildROIs(*url, *roisFlag, info0, *expectMotion, *expectStatic)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ROI 构建失败:", err)
		os.Exit(1)
	}

	// 4) 读帧。
	imgs := make([]*image.RGBA, 0, len(paths))
	for _, p := range paths {
		im, err := readPNG(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "读帧 %s 失败: %v\n", p, err)
			os.Exit(1)
		}
		imgs = append(imgs, im)
	}

	// 5) 分析。
	rep := analyze(imgs, rois, *warmup)
	rep.Frames = len(imgs)
	rep.IntervalMS = *interval
	rep.Window = info0

	// 6) 判定。
	verdict := judge(rep, *expectMotion, *expectStatic, *threshMove, *threshStill)
	rep.Verdicts = verdict
	allOK := true
	for _, v := range verdict {
		if !v.OK {
			allOK = false
		}
	}

	// 7) 产物：contact sheet + 运动热图 + 报告 JSON。
	sheet := filepath.Join(*outDir, "contact-sheet.png")
	if err := writeContactSheet(imgs, sheet); err != nil {
		fmt.Fprintln(os.Stderr, "contact sheet 失败:", err)
	}
	heat := filepath.Join(*outDir, "motion-heat.png")
	if err := writeMotionHeat(imgs, heat); err != nil {
		fmt.Fprintln(os.Stderr, "运动热图失败:", err)
	}
	reportPath := filepath.Join(*outDir, "report.json")
	if b, err := json.MarshalIndent(rep, "", "  "); err == nil {
		_ = os.WriteFile(reportPath, b, 0o644)
	}

	printReport(rep, *threshMove, *threshStill)
	fmt.Printf("\n产物: %s（帧） · %s · %s · %s\n", *outDir, sheet, heat, reportPath)
	if !allOK {
		os.Exit(1)
	}
}

// defaultCapture 返回窗口模式截图助手的默认路径：
// GOGUI_CAPTURE_BIN → BM_BIN（兼容旧变量）→ PATH 中的 bm。
// 纯 headless 场景用 -cdp，不依赖外部助手。
func defaultCapture() string {
	for _, env := range []string{"GOGUI_CAPTURE_BIN", "BM_BIN"} {
		if v := os.Getenv(env); v != "" {
			return v
		}
	}
	if p, err := exec.LookPath("bm"); err == nil {
		return p
	}
	return "bm"
}

func runCapture(bin string, args ...string) ([]byte, error) {
	cmd := exec.Command(bin, args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("capture %s: %v (%s)（窗口模式需要外部截图助手，设置 GOGUI_CAPTURE_BIN；或改用 -cdp）", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

func findWindow(bin, keyword string) (uint64, error) {
	out, err := runCapture(bin, "pilot", "windows", "--keyword", keyword)
	if err != nil {
		return 0, err
	}
	var wi winInfo
	if err := json.Unmarshal(out, &wi); err != nil {
		return 0, fmt.Errorf("解析 windows 输出: %v (%s)", err, truncate(string(out), 200))
	}
	if wi.Count == 0 || len(wi.Windows) == 0 {
		return 0, fmt.Errorf("未找到标题包含 %q 的窗口", keyword)
	}
	return wi.Windows[0].Hwnd, nil
}

func shot(bin string, hwnd uint64, path string) (shotInfo, error) {
	out, err := runCapture(bin, "pilot", "shot", "--bg", "--hwnd", strconv.FormatUint(hwnd, 10),
		"-grid", "0", "-cursor=false", "-o", path)
	if err != nil {
		return shotInfo{}, err
	}
	var si shotInfo
	line := strings.TrimSpace(string(out))
	if i := strings.LastIndex(line, "\n"); i >= 0 {
		line = line[i+1:]
	}
	if err := json.Unmarshal([]byte(line), &si); err != nil {
		return shotInfo{}, fmt.Errorf("解析 shot 输出: %v (%s)", err, truncate(line, 200))
	}
	if !si.OK {
		return shotInfo{}, fmt.Errorf("shot 返回 ok=false")
	}
	return si, nil
}

// buildROIs 构建图像坐标系 ROI：
//   - 提供 -url：从 /gg/layout 取控件矩形（CSS px），按 图像宽/视口宽 缩放，
//     并按「图像高 - 视口高*scale」估算标题栏竖直偏移（窗口截图含标题栏）；
//   - 否则解析 -rois（CSS px，同样做缩放与偏移）；
//   - 都没有：期待名全解析成全帧 ROI。
func buildROIs(url, roisFlag string, info shotInfo, expectMotion, expectStatic string) ([]roi, error) {
	names := map[string]bool{}
	for _, s := range strings.Split(expectMotion+","+expectStatic, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if i := strings.IndexByte(s, ':'); i > 0 {
			s = s[:i]
		}
		names[s] = true
	}

	cssRects := map[string][4]float64{} // name -> x,y,w,h (CSS px)
	viewportW, viewportH := 0.0, 0.0

	if url != "" {
		lr, err := fetchLayout(url)
		if err == nil && lr.Viewport.W > 0 {
			viewportW, viewportH = lr.Viewport.W, lr.Viewport.H
			for _, it := range lr.Items {
				if names[it.GID] {
					cssRects[it.GID] = [4]float64{it.X, it.Y, it.W, it.H}
				}
			}
		}
	}
	if len(cssRects) == 0 && roisFlag != "" {
		for _, part := range strings.Split(roisFlag, ";") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			kv := strings.SplitN(part, "=", 2)
			if len(kv) != 2 {
				return nil, fmt.Errorf("非法 ROI 项: %q", part)
			}
			vals := strings.Split(kv[1], ",")
			if len(vals) != 4 {
				return nil, fmt.Errorf("ROI %q 需要 4 个数值", kv[0])
			}
			var f [4]float64
			for i := 0; i < 4; i++ {
				v, err := strconv.ParseFloat(strings.TrimSpace(vals[i]), 64)
				if err != nil {
					return nil, fmt.Errorf("ROI %q 数值非法: %v", kv[0], err)
				}
				f[i] = v
			}
			cssRects[kv[0]] = f
			names[kv[0]] = true
		}
	}

	// 缩放与偏移：优先用 viewport 推，其次假设无偏移直接铺满。
	scaleX := 1.0
	scaleY := 1.0
	offY := 0.0
	if viewportW > 0 && viewportH > 0 {
		scaleX = float64(info.Width) / viewportW
		scaleY = scaleX // 等比；高度用同一 scale 推标题栏偏移
		contentH := viewportH * scaleY
		if float64(info.Height) > contentH {
			offY = float64(info.Height) - contentH
		}
	}

	out := make([]roi, 0, len(names))
	add := func(name string, x, y, w, h float64) {
		r := roi{
			Name: name,
			X:    int(math.Round(x*scaleX)) - 4,
			Y:    int(math.Round(y*scaleY+offY)) - 4,
			W:    int(math.Round(w*scaleX)) + 8,
			H:    int(math.Round(h*scaleY)) + 8,
		}
		out = append(out, clampROI(r, info.Width, info.Height))
	}
	if len(cssRects) > 0 {
		for name := range names {
			if r, ok := cssRects[name]; ok {
				add(name, r[0], r[1], r[2], r[3])
			} else if name == "all" {
				add("all", 0, 0, float64(info.Width)/scaleX, float64(info.Height)/scaleY)
			}
		}
	} else {
		// 没有映射来源：全部解析为全帧。
		add("all", 0, 0, float64(info.Width)/scaleX, (float64(info.Height)-offY)/scaleY)
	}
	if len(out) == 0 {
		add("all", 0, 0, float64(info.Width)/scaleX, float64(info.Height)/scaleY)
	}
	return out, nil
}

func clampROI(r roi, w, h int) roi {
	if r.X < 0 {
		r.X = 0
	}
	if r.Y < 0 {
		r.Y = 0
	}
	if r.X+r.W > w {
		r.W = w - r.X
	}
	if r.Y+r.H > h {
		r.H = h - r.Y
	}
	if r.W < 2 {
		r.W = 2
	}
	if r.H < 2 {
		r.H = 2
	}
	return r
}

func fetchLayout(url string) (*layoutReport, error) {
	c := &http.Client{Timeout: 6 * time.Second}
	base := strings.TrimRight(url, "/")
	// 先取缓存；无缓存则触发一次实时采集（可能等 2s）。
	for _, suffix := range []string{"/gg/layout?cached=1", "/gg/layout"} {
		resp, err := c.Get(base + suffix)
		if err != nil {
			continue
		}
		var lr layoutReport
		derr := json.NewDecoder(resp.Body).Decode(&lr)
		resp.Body.Close()
		if derr == nil && lr.Viewport.W > 0 && len(lr.Items) > 0 {
			return &lr, nil
		}
	}
	return nil, fmt.Errorf("layout 拉取失败（缓存缺失且实时采集未返回）")
}

func readPNG(p string) (*image.RGBA, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	im, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	b := im.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), im, b.Min, draw.Src)
	return rgba, nil
}

// ---- 分析 ----

type roiStat struct {
	Name         string  `json:"name"`
	Rect         [4]int  `json:"rect"`
	MaxDiff      float64 `json:"max_diff"`      // 相邻帧最大平均差
	MeanDiff     float64 `json:"mean_diff"`     // 全部相邻帧平均
	ActiveFrames int     `json:"active_frames"` // 超过运动阈值的帧对数
}

type verdict struct {
	Name   string  `json:"name"`
	Expect string  `json:"expect"` // motion | static
	OK     bool    `json:"ok"`
	Actual float64 `json:"actual_max_diff"`
	Detail string  `json:"detail"`
}

type report struct {
	Frames       int       `json:"frames"`
	IntervalMS   int       `json:"interval_ms"`
	Window       shotInfo  `json:"window"`
	ROIs         []roiStat `json:"rois"`
	BlankFrame   []int     `json:"blank_frames,omitempty"` // 疑似黑屏帧下标
	FullFrameMax float64   `json:"full_frame_max_diff"`    // 全帧相邻差峰值（结构跳变参考）
	Verdicts     []verdict `json:"verdicts"`
}

func analyze(imgs []*image.RGBA, rois []roi, warmup int) report {
	rep := report{}
	if warmup > 0 && warmup < len(imgs) {
		imgs = imgs[warmup:]
	}
	if len(imgs) < 2 {
		return rep
	}
	// 黑屏检测：帧平均亮度。
	means := make([]float64, len(imgs))
	for i, im := range imgs {
		means[i] = frameMean(im)
	}
	for i, m := range means {
		if m < 8 && i > 0 && means[i-1] >= 8 {
			rep.BlankFrame = append(rep.BlankFrame, i)
		}
	}
	// 全帧结构性跳变（状态切换/闪烁）：相邻帧全幅平均差序列。
	fullMax := 0.0
	for i := 0; i+1 < len(imgs); i++ {
		d := roiDiff(imgs[i], imgs[i+1], roi{X: 0, Y: 0, W: imgs[i].Bounds().Dx(), H: imgs[i].Bounds().Dy()})
		if d > fullMax {
			fullMax = d
		}
	}
	rep.FullFrameMax = fullMax

	// 每 ROI 相邻帧差分。
	for _, r := range rois {
		st := roiStat{Name: r.Name, Rect: [4]int{r.X, r.Y, r.W, r.H}}
		sum := 0.0
		pairs := 0
		for i := 0; i+1 < len(imgs); i++ {
			d := roiDiff(imgs[i], imgs[i+1], r)
			sum += d
			pairs++
			if d > st.MaxDiff {
				st.MaxDiff = d
			}
			// ActiveFrames 用固定 0.8 的中位敏感阈值统计活动强度。
			if d > 0.8 {
				st.ActiveFrames++
			}
		}
		if pairs > 0 {
			st.MeanDiff = sum / float64(pairs)
		}
		rep.ROIs = append(rep.ROIs, st)
	}
	return rep
}

// roiDiff 计算两块区域的平均逐通道绝对差（0-255）。隔像素采样提速。
func roiDiff(a, b *image.RGBA, r roi) float64 {
	total := 0.0
	n := 0
	for y := r.Y; y < r.Y+r.H; y += 2 {
		for x := r.X; x < r.X+r.W; x += 2 {
			ca := a.RGBAAt(x, y)
			cb := b.RGBAAt(x, y)
			total += math.Abs(float64(ca.R)-float64(cb.R)) +
				math.Abs(float64(ca.G)-float64(cb.G)) +
				math.Abs(float64(ca.B)-float64(cb.B))
			n += 3
		}
	}
	if n == 0 {
		return 0
	}
	return total / float64(n)
}

func frameMean(im *image.RGBA) float64 {
	total := 0.0
	n := 0
	b := im.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y += 4 {
		for x := b.Min.X; x < b.Max.X; x += 4 {
			c := im.RGBAAt(x, y)
			total += float64(c.R) * 0.299 / 255
			total += float64(c.G) * 0.587 / 255 * 0 // 简化：仅取 R 通道足够判黑屏
			_ = c
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return total / float64(n) * 255
}

func judge(rep report, expectMotion, expectStatic string, threshMove, threshStill float64) []verdict {
	var out []verdict
	statOf := func(name string) *roiStat {
		for i := range rep.ROIs {
			if rep.ROIs[i].Name == name {
				return &rep.ROIs[i]
			}
		}
		return nil
	}
	// 支持每 ROI 阈值覆盖：name:thresh（如 seek:4 表示文字叠在动画背景上的噪声底）。
	parseExpect := func(spec string) (string, float64, bool) {
		parts := strings.SplitN(strings.TrimSpace(spec), ":", 2)
		name := parts[0]
		override := 0.0
		has := false
		if len(parts) == 2 {
			if v, err := strconv.ParseFloat(parts[1], 64); err == nil {
				override, has = v, true
			}
		}
		return name, override, has
	}
	for _, spec := range splitList(expectMotion) {
		name, ov, has := parseExpect(spec)
		thr := threshMove
		if has {
			thr = ov
		}
		if st := statOf(name); st != nil {
			ok := st.MaxDiff >= thr
			out = append(out, verdict{Name: name, Expect: "motion", OK: ok, Actual: st.MaxDiff,
				Detail: fmt.Sprintf("期望运动：maxDiff=%.2f 阈值=%.2f 活动帧=%d", st.MaxDiff, thr, st.ActiveFrames)})
		}
	}
	for _, spec := range splitList(expectStatic) {
		name, ov, has := parseExpect(spec)
		thr := threshStill
		if has {
			thr = ov
		}
		if st := statOf(name); st != nil {
			ok := st.MaxDiff <= thr
			out = append(out, verdict{Name: name, Expect: "static", OK: ok, Actual: st.MaxDiff,
				Detail: fmt.Sprintf("期望静止：maxDiff=%.2f 上限=%.2f", st.MaxDiff, thr)})
		}
	}
	return out
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ---- 产物 ----

func writeContactSheet(imgs []*image.RGBA, path string) error {
	if len(imgs) == 0 {
		return fmt.Errorf("no frames")
	}
	const cols = 4
	thumbW := 320
	ratio := float64(imgs[0].Bounds().Dy()) / float64(imgs[0].Bounds().Dx())
	thumbH := int(float64(thumbW) * ratio)
	rows := (len(imgs) + cols - 1) / cols
	out := image.NewRGBA(image.Rect(0, 0, cols*thumbW, rows*thumbH))
	for i, im := range imgs {
		cx := (i % cols) * thumbW
		cy := (i / cols) * thumbH
		drawScaled(out, im, cx, cy, thumbW, thumbH)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, out)
}

func drawScaled(dst *image.RGBA, src *image.RGBA, x0, y0, w, h int) {
	sb := src.Bounds()
	for y := 0; y < h; y++ {
		sy := sb.Min.Y + y*sb.Dy()/h
		for x := 0; x < w; x++ {
			sx := sb.Min.X + x*sb.Dx()/w
			dst.Set(x0+x, y0+y, src.RGBAAt(sx, sy))
		}
	}
}

func writeMotionHeat(imgs []*image.RGBA, path string) error {
	if len(imgs) < 2 {
		return fmt.Errorf("not enough frames")
	}
	b := imgs[len(imgs)-1].Bounds()
	out := image.NewRGBA(b)
	// 以最后一帧为底，把各像素的「历史最大逐帧差」放大后叠红。
	maxDiff := make([]float64, b.Dx()*b.Dy())
	for i := 0; i+1 < len(imgs); i++ {
		a, c := imgs[i], imgs[i+1]
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				ca := a.RGBAAt(x, y)
				cb := c.RGBAAt(x, y)
				d := math.Abs(float64(ca.R)-float64(cb.R)) + math.Abs(float64(ca.G)-float64(cb.G)) + math.Abs(float64(ca.B)-float64(cb.B))
				d /= 3
				idx := (y-b.Min.Y)*b.Dx() + (x - b.Min.X)
				if d > maxDiff[idx] {
					maxDiff[idx] = d
				}
			}
		}
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := imgs[len(imgs)-1].RGBAAt(x, y)
			d := maxDiff[(y-b.Min.Y)*b.Dx()+(x-b.Min.X)]
			alpha := math.Min(1, d/255*4) // 放大 4 倍便于人眼
			rc := float64(c.R)*(1-alpha) + 255*alpha
			gc := float64(c.G) * (1 - alpha)
			bc := float64(c.B) * (1 - alpha)
			out.SetRGBA(x, y, color.RGBA{R: uint8(rc), G: uint8(gc), B: uint8(bc), A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, out)
}

func printReport(rep report, threshMove, threshStill float64) {
	fmt.Printf("动态验收：%d 帧 · 间隔 %dms · 窗口 %d×%d\n", rep.Frames, rep.IntervalMS, rep.Window.Width, rep.Window.Height)
	if len(rep.BlankFrame) > 0 {
		fmt.Printf("⚠ 疑似黑屏帧: %v\n", rep.BlankFrame)
	}
	if rep.FullFrameMax > 8 {
		fmt.Printf("ℹ 全帧跳变峰值 %.2f（结构性切换，如换曲/换页；>8 时属预期）\n", rep.FullFrameMax)
	}
	fmt.Printf("\n  %-14s %10s %10s %10s\n", "ROI", "maxDiff", "meanDiff", "active帧")
	for _, r := range rep.ROIs {
		fmt.Printf("  %-14s %10.2f %10.2f %10d\n", r.Name, r.MaxDiff, r.MeanDiff, r.ActiveFrames)
	}
	if len(rep.Verdicts) == 0 {
		fmt.Println("\n（未设置期望；仅输出统计）")
		return
	}
	fmt.Println()
	okAll := true
	for _, v := range rep.Verdicts {
		mark := "✓"
		if !v.OK {
			mark = "✗"
			okAll = false
		}
		fmt.Printf("  %s [%s] %s\n", mark, v.Expect, v.Detail)
	}
	if okAll {
		fmt.Println("\nPASS：动态期望全部满足")
	} else {
		fmt.Println("\nFAIL：动态期望被违背")
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// burst 连拍 N 帧，返回帧路径与首帧信息。
func burst(captureBin string, hwnd uint64, dir string, frames, intervalMS int) ([]string, shotInfo, error) {
	_ = os.MkdirAll(dir, 0o755)
	paths := make([]string, 0, frames)
	var info0 shotInfo
	for i := 0; i < frames; i++ {
		p := filepath.Join(dir, fmt.Sprintf("f%03d.png", i))
		info, err := shot(captureBin, hwnd, p)
		if err != nil {
			return nil, shotInfo{}, fmt.Errorf("第 %d 帧: %w", i, err)
		}
		if i == 0 {
			info0 = info
		}
		paths = append(paths, p)
		if i < frames-1 {
			time.Sleep(time.Duration(intervalMS) * time.Millisecond)
		}
	}
	if info0.Width == 0 {
		return nil, shotInfo{}, fmt.Errorf("首帧尺寸为 0")
	}
	return paths, info0, nil
}

// eventsTotal 读取 /gg/state 的 events_total；失败返回 -1（守卫降级放行）。
func eventsTotal(url string) int64 {
	if url == "" {
		return -1
	}
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(strings.TrimRight(url, "/") + "/gg/state?n=0")
	if err != nil {
		return -1
	}
	defer resp.Body.Close()
	var st struct {
		App struct {
			Events uint64 `json:"events_total"`
		} `json:"app"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return -1
	}
	return int64(st.App.Events)
}

// runCDP 用 headless Chromium 直采帧并复用同一分析/判定管线。
// ROI 坐标系 = CSS 视口（无标题栏、无 DPI 缩放，映射零误差）。
func runCDP(url, outDir string, frames, intervalMS, vw, vh int,
	expectMotion, expectStatic string, threshMove, threshStill float64, quiet bool, retries int) int {

	ctx, cancel := goguitest.NewBrowserContext(vw, vh)
	defer cancel()

	var paths []string
	for attempt := 0; ; attempt++ {
		dir := outDir
		if attempt > 0 {
			dir = outDir + fmt.Sprintf("-retry%d", attempt)
		}
		_ = os.MkdirAll(dir, 0o755)
		evBefore := eventsTotal(url)
		if err := chromedp.Do(ctx,
			chromedp.EmulateViewport(int64(vw), int64(vh)),
			chromedp.Navigate(url),
			chromedp.WaitVisible(chromedp.CSS(`.gg-app`)),
		); err != nil {
			fmt.Fprintln(os.Stderr, "CDP 加载失败:", err)
			return 1
		}
		ps := make([]string, 0, frames)
		for i := 0; i < frames; i++ {
			buf, err := chromedp.Run(ctx, chromedp.CaptureScreenshot())
			if err != nil {
				fmt.Fprintln(os.Stderr, "CDP 截帧失败:", err)
				return 1
			}
			p := filepath.Join(dir, fmt.Sprintf("f%03d.png", i))
			if err := os.WriteFile(p, buf, 0o644); err != nil {
				return 1
			}
			ps = append(ps, p)
			if i < frames-1 {
				time.Sleep(time.Duration(intervalMS) * time.Millisecond)
			}
		}
		evAfter := eventsTotal(url)
		if !quiet || evBefore < 0 || evAfter < 0 || evBefore == evAfter {
			paths, outDir = ps, dir
			break
		}
		if attempt >= retries {
			fmt.Fprintf(os.Stderr, "✗ 环境不安静：拍摄期间检测到 %d 个输入事件\n", evAfter-evBefore)
			return 1
		}
		fmt.Printf("⚠ 拍摄期间有输入事件，重拍（%d/%d）…\n", attempt+1, retries)
	}

	imgs := make([]*image.RGBA, 0, len(paths))
	for _, p := range paths {
		im, err := readPNG(p)
		if err != nil {
			fmt.Fprintln(os.Stderr, "读帧失败:", err)
			return 1
		}
		imgs = append(imgs, im)
	}
	info := shotInfo{OK: true, Width: imgs[0].Bounds().Dx(), Height: imgs[0].Bounds().Dy(), Mode: "cdp"}
	rois, err := buildROIs(url, "", info, expectMotion, expectStatic)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ROI 构建失败:", err)
		return 1
	}
	rep := analyze(imgs, rois, 2)
	rep.Frames = len(imgs)
	rep.IntervalMS = intervalMS
	rep.Window = info
	rep.Verdicts = judge(rep, expectMotion, expectStatic, threshMove, threshStill)
	ok := true
	for _, v := range rep.Verdicts {
		if !v.OK {
			ok = false
		}
	}
	sheet := filepath.Join(outDir, "contact-sheet.png")
	_ = writeContactSheet(imgs, sheet)
	heat := filepath.Join(outDir, "motion-heat.png")
	_ = writeMotionHeat(imgs, heat)
	if b, err := json.MarshalIndent(rep, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(outDir, "report.json"), b, 0o644)
	}
	printReport(rep, threshMove, threshStill)
	fmt.Printf("\n产物: %s（帧） · %s · %s\n", outDir, sheet, heat)
	if !ok {
		return 1
	}
	return 0
}
