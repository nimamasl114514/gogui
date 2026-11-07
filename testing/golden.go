package testing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/nimamasl114514/gogui/model"
	"github.com/nimamasl114514/gogui/server"
	"github.com/nimamasl114514/gogui/snapshot"
	"github.com/nimamasl114514/gogui/theme"
)

// ===================== 像素 golden：采集 =====================

// CaptureImage 在 headless Chromium 中渲染应用并返回整视口 PNG。
//
// 确定性纪律（与 CompareImages 配合构成像素对照）：
//   - 固定视口 w×h + 强制 deviceScaleFactor=1；
//   - 注入 .gg-static 冻结全部 transition/animation；
//   - await document.fonts.ready 后再截屏；
//   - 截屏前静置 120ms 等布局稳定。
func CaptureImage(app *model.App, th *theme.Theme, width, height int) ([]byte, error) {
	if th == nil {
		th = theme.DarkGlow()
	}
	srv := server.New(app, th)
	url, err := srv.Start()
	if err != nil {
		return nil, err
	}
	defer func() { _ = srv.Close() }()

	ctx, cancel := NewBrowserContext(width, height)
	defer cancel()

	if err := chromedp.Do(ctx,
		chromedp.EmulateViewport(int64(width), int64(height)),
		chromedp.Navigate(url),
		chromedp.WaitVisible(chromedp.CSS(`.gg-app`)),
	); err != nil {
		return nil, fmt.Errorf("golden: 页面加载失败: %w", err)
	}
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](
		`document.documentElement.classList.add('gg-static')`,
	)); err != nil {
		return nil, err
	}
	// 等字体就绪（Promise 需要 EvalAwaitPromise）。
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[any](
		`document.fonts.ready.then(() => "ok")`,
		chromedp.EvalAwaitPromise,
	)); err != nil {
		return nil, err
	}
	if err := chromedp.Do(ctx, chromedp.Sleep(120*time.Millisecond)); err != nil {
		return nil, err
	}
	pngBytes, err := chromedp.Run(ctx, chromedp.CaptureScreenshot())
	if err != nil {
		return nil, fmt.Errorf("golden: 截屏失败: %w", err)
	}
	return pngBytes, nil
}

// ===================== 像素 golden：对比 =====================

// CompareResult 是两次像素对比的量化结果。
type CompareResult struct {
	Match        bool    // 是否在容差内逐像素一致
	MaxDiff      int     // 单通道最大差值（0..255）
	DiffPixels   int     // 超容差像素数
	TotalPixels  int     // 总像素数
	DiffPercent  float64 // 超容差像素占比
	SizeMismatch bool    // 尺寸不一致
}

// CompareImages 对拍两份 PNG：逐像素、逐通道（RGBA）比较。
// tolerance=0 表示逐字节像素级（无任何容差）。
func CompareImages(want, got []byte, tolerance int) (CompareResult, error) {
	var res CompareResult
	imgA, err := decodePNG(want)
	if err != nil {
		return res, fmt.Errorf("golden: 解码基线失败: %w", err)
	}
	imgB, err := decodePNG(got)
	if err != nil {
		return res, fmt.Errorf("golden: 解码当前图失败: %w", err)
	}
	if imgA.Bounds() != imgB.Bounds() {
		res.SizeMismatch = true
		return res, nil
	}
	res.TotalPixels = imgA.Bounds().Dx() * imgA.Bounds().Dy()
	res.Match = true
	for y := imgA.Bounds().Min.Y; y < imgA.Bounds().Max.Y; y++ {
		for x := imgA.Bounds().Min.X; x < imgA.Bounds().Max.X; x++ {
			ra, ga, ba, aa := imgA.At(x, y).RGBA()
			rb, gb, bb, ab := imgB.At(x, y).RGBA()
			d := max4(abs8(ra>>8, rb>>8), abs8(ga>>8, gb>>8), abs8(ba>>8, bb>>8), abs8(aa>>8, ab>>8))
			if d > res.MaxDiff {
				res.MaxDiff = d
			}
			if d > tolerance {
				res.DiffPixels++
				res.Match = false
			}
		}
	}
	if res.TotalPixels > 0 {
		res.DiffPercent = float64(res.DiffPixels) / float64(res.TotalPixels) * 100
	}
	return res, nil
}

// DiffOverlay 生成差异可视化图：got 为底，超容差像素标红（diff 高亮定位用）。
func DiffOverlay(want, got []byte, tolerance int) ([]byte, error) {
	imgA, err := decodePNG(want)
	if err != nil {
		return nil, err
	}
	imgB, err := decodePNG(got)
	if err != nil {
		return nil, err
	}
	if imgA.Bounds() != imgB.Bounds() {
		return nil, fmt.Errorf("golden: 尺寸不一致 %v vs %v", imgA.Bounds(), imgB.Bounds())
	}
	out := image.NewRGBA(imgA.Bounds())
	for y := imgA.Bounds().Min.Y; y < imgA.Bounds().Max.Y; y++ {
		for x := imgA.Bounds().Min.X; x < imgA.Bounds().Max.X; x++ {
			ra, ga, ba, aa := imgA.At(x, y).RGBA()
			rb, gb, bb, ab := imgB.At(x, y).RGBA()
			d := max4(abs8(ra>>8, rb>>8), abs8(ga>>8, gb>>8), abs8(ba>>8, bb>>8), abs8(aa>>8, ab>>8))
			if d > tolerance {
				out.Set(x, y, color.RGBA{R: 255, A: 255}) // 红色差异高亮
			} else {
				br, bg, bb2, ba2 := imgB.At(x, y).RGBA()
				out.Set(x, y, color.RGBA{R: uint8(br >> 8), G: uint8(bg >> 8), B: uint8(bb2 >> 8), A: uint8(ba2 >> 8)})
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ===================== 像素 golden：基线落盘 / 校验 =====================

// GoldenMeta 是像素基线的 sidecar（记录采集环境，便于跨机器解释差异）。
type GoldenMeta struct {
	Name      string `json:"name"`
	Browser   string `json:"browser"`
	Viewport  string `json:"viewport"`
	Tolerance int    `json:"tolerance"`
	Captured  string `json:"captured"`
}

// UpdateEnv 环境变量打开时（GOGUI_UPDATE=1），Assert* 会更新基线而非校验。
func UpdateEnv() bool { return os.Getenv("GOGUI_UPDATE") == "1" }

// GoldenDir 解析基线目录（GOGUI_GOLDEN 覆盖，默认 "golden"）。
func GoldenDir() string {
	if d := os.Getenv("GOGUI_GOLDEN"); d != "" {
		return d
	}
	return "golden"
}

// AssertGoldenImage 像素对照测试：
//   - 基线缺失或 GOGUI_UPDATE=1 → 采集当前渲染并写入基线（通过）；
//   - 否则与基线逐像素对拍（tolerance 容差），失败落盘 fail/ 三件套并 Fatal。
func AssertGoldenImage(t T, name string, app *model.App, th *theme.Theme, width, height, tolerance int) {
	t.Helper()
	if os.Getenv("GOGUI_SKIP_IMAGE") == "1" {
		t.Logf("golden[%s]: GOGUI_SKIP_IMAGE=1，跳过像素对照（跨机字体栈不同时使用）", name)
		return
	}
	dir := GoldenDir()
	imgDir := filepath.Join(dir, "img")
	base := filepath.Join(imgDir, name+".png")
	metaFile := filepath.Join(imgDir, name+".json")

	got, err := CaptureImage(app, th, width, height)
	if err != nil {
		t.Fatalf("golden[%s]: 采集失败: %v", name, err)
	}

	if UpdateEnv() || !fileExists(base) {
		if err := os.MkdirAll(imgDir, 0o755); err != nil {
			t.Fatalf("golden[%s]: 建目录失败: %v", name, err)
		}
		if err := os.WriteFile(base, got, 0o644); err != nil {
			t.Fatalf("golden[%s]: 基线写入失败: %v", name, err)
		}
		meta := GoldenMeta{
			Name:      name,
			Browser:   filepath.Base(BrowserPath()),
			Viewport:  fmt.Sprintf("%dx%d", width, height),
			Tolerance: tolerance,
			Captured:  time.Now().Format(time.RFC3339),
		}
		mb, _ := json.MarshalIndent(meta, "", "  ")
		_ = os.WriteFile(metaFile, mb, 0o644)
		return
	}

	want, err := os.ReadFile(base)
	if err != nil {
		t.Fatalf("golden[%s]: 读基线失败: %v", name, err)
	}
	res, err := CompareImages(want, got, tolerance)
	if err != nil {
		t.Fatalf("golden[%s]: %v", name, err)
	}
	if !res.Match {
		// 落盘三件套：当前图 / diff 高亮图 / 元数据。
		failDir := filepath.Join(imgDir, "fail")
		_ = os.MkdirAll(failDir, 0o755)
		ts := time.Now().Format("20060102-150405")
		_ = os.WriteFile(filepath.Join(failDir, name+"."+ts+".png"), got, 0o644)
		if diff, derr := DiffOverlay(want, got, tolerance); derr == nil {
			_ = os.WriteFile(filepath.Join(failDir, name+"."+ts+".diff.png"), diff, 0o644)
		}
		t.Fatalf("golden[%s]: 像素不匹配（尺寸不一致=%v）: diff=%dpx/%dpx (%.3f%%) maxDiff=%d tol=%d；失败样张见 %s/fail/",
			name, res.SizeMismatch, res.DiffPixels, res.TotalPixels, res.DiffPercent, res.MaxDiff, tolerance, imgDir)
	}
}

// AssertGoldenTree 树快照对照测试（结构级）：
//   - 基线缺失或 GOGUI_UPDATE=1 → 写入当前树快照（通过）；
//   - 否则文本逐字节对拍，失败打印 want/got 两份全文。
func AssertGoldenTree(t T, name string, app *model.App) {
	t.Helper()
	dir := GoldenDir()
	treeDir := filepath.Join(dir, "tree")
	base := filepath.Join(treeDir, name+".txt")

	got := snapshot.Tree(app)
	if UpdateEnv() || !fileExists(base) {
		if err := os.MkdirAll(treeDir, 0o755); err != nil {
			t.Fatalf("golden[%s]: 建目录失败: %v", name, err)
		}
		if err := os.WriteFile(base, []byte(got), 0o644); err != nil {
			t.Fatalf("golden[%s]: 基线写入失败: %v", name, err)
		}
		return
	}
	want, err := os.ReadFile(base)
	if err != nil {
		t.Fatalf("golden[%s]: 读基线失败: %v", name, err)
	}
	if string(want) != got {
		t.Fatalf("golden[%s]: 树快照不一致\n--- want ---\n%s\n--- got ---\n%s", name, want, got)
	}
}

// ===================== 工具 =====================

func decodePNG(b []byte) (image.Image, error) {
	return png.Decode(bytes.NewReader(b))
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func abs8(a, b uint32) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

func max4(a, b, c, d int) int {
	m := a
	if b > m {
		m = b
	}
	if c > m {
		m = c
	}
	if d > m {
		m = d
	}
	return m
}
