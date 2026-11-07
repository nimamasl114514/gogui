// gogui-dev —— gogui 开发循环监管器：保存即编译、编译过即热重启。
//
// 用法：
//
//	gogui-dev -target ./cmd/monitor          # 监视当前目录 *.go，热重启 monitor
//	gogui-dev -target ./cmd/demo -dir .      # 显式指定监视根
//
// 行为：
//   - 轮询 *.go 的 mtime（300ms 间隔 + 200ms 防抖，零第三方依赖）；
//   - `go build` 到 .gogui-dev/app-<ts>.exe（Windows 不能覆盖运行中的 exe）；
//   - 构建成功 → 结束旧进程 → 启动新进程（子进程 stdout/stderr 直通，URL 照样打印）；
//   - 构建失败 → 旧窗口保持存活，终端打印原始编译错误（不打断手上的事）；
//   - Ctrl+C → 结束子进程退出。
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

func main() {
	target := flag.String("target", "", "要构建并运行的 main 包路径（如 ./cmd/monitor）")
	dir := flag.String("dir", ".", "监视 *.go 的根目录")
	debounce := flag.Duration("debounce", 200*time.Millisecond, "变更防抖")
	poll := flag.Duration("poll", 300*time.Millisecond, "轮询间隔")
	flag.Parse()
	if *target == "" {
		fmt.Fprintln(os.Stderr, "用法: gogui-dev -target ./cmd/yourapp [-dir .]")
		os.Exit(2)
	}

	outDir := ".gogui-dev"
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "创建构建目录失败:", err)
		os.Exit(1)
	}

	goBin := resolveGo()
	if goBin == "" {
		fmt.Fprintln(os.Stderr, "[dev] 找不到 go 可执行文件（PATH/GOROOT 均未命中）")
		os.Exit(1)
	}
	d := &devServer{target: *target, outDir: outDir, debounce: *debounce, poll: *poll, goBin: goBin}
	d.run(*dir)
}

// resolveGo 解析 go 可执行文件绝对路径：
// 子进程环境与 Git Bash 不同（R20 教训：PATH 不继承），必须显式定位。
func resolveGo() string {
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	var candidates []string
	if gr := os.Getenv("GOROOT"); gr != "" {
		candidates = append(candidates, filepath.Join(gr, "bin", "go.exe"), filepath.Join(gr, "bin", "go"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, "AppData", "Local", "Programs", "Go", "bin", "go.exe"),
			filepath.Join(home, "sdk", "go", "bin", "go.exe"))
	}
	candidates = append(candidates,
		`C:\Program Files\Go\bin\go.exe`,
		`C:\Go\bin\go.exe`,
	)
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

type devServer struct {
	target   string
	outDir   string
	debounce time.Duration
	poll     time.Duration
	goBin    string

	mu       sync.Mutex
	child    *exec.Cmd
	builds   int
	restarts int
	lastExe  string
}

func (d *devServer) run(root string) {
	// 初次构建 + 启动。
	if !d.buildAndRestart("初始构建") {
		fmt.Println("[dev] 首次构建失败——修正代码后保存会自动重试")
	}

	last := d.snapshot(root)
	lastChange := time.Time{}
	pending := false

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	ticker := time.NewTicker(d.poll)
	defer ticker.Stop()

	fmt.Printf("[dev] 监视 %s 下的 *.go（%s 轮询 / %s 防抖）\n", root, d.poll, d.debounce)
	for {
		select {
		case <-sig:
			d.killChild()
			fmt.Println("\n[dev] 已退出")
			return
		case <-ticker.C:
			cur := d.snapshot(root)
			if !equalSnapshots(last, cur) {
				last = cur
				lastChange = time.Now()
				pending = true
			}
			if pending && time.Since(lastChange) >= d.debounce {
				pending = false
				d.buildAndRestart("检测到变更")
			}
		}
	}
}

// snapshot 采集 root 下所有 .go 文件的 mtime+size（跳过构建产物/临时目录）。
func (d *devServer) snapshot(root string) map[string]fileStamp {
	out := map[string]fileStamp{}
	_ = filepath.WalkDir(root, func(path string, de os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := de.Name()
		if de.IsDir() {
			if name == ".git" || name == ".gogui-dev" || name == "tmp" || name == "golden" || name == "node_modules" || name == "shots" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		info, err := de.Info()
		if err != nil {
			return nil
		}
		out[path] = fileStamp{mtime: info.ModTime().UnixNano(), size: info.Size()}
		return nil
	})
	return out
}

type fileStamp struct {
	mtime int64
	size  int64
}

func equalSnapshots(a, b map[string]fileStamp) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// buildAndRestart 构建目标并热重启；返回是否成功（失败保留旧进程）。
func (d *devServer) buildAndRestart(reason string) bool {
	d.builds++
	exe := filepath.Join(d.outDir, fmt.Sprintf("app-%d.exe", time.Now().UnixNano()))
	start := time.Now()

	cmd := exec.Command(d.goBin, "build", "-o", exe, d.target)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "GOGUI_DEV=1")
	err := cmd.Run()
	elapsed := time.Since(start)
	if err != nil {
		fmt.Printf("[dev] ✗ 构建失败（%s，用时 %s）：%v —— 保留旧窗口\n", reason, elapsed.Round(time.Millisecond), err)
		_ = os.Remove(exe)
		return false
	}

	d.mu.Lock()
	d.killChildLocked()
	child := exec.Command(exe)
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	child.Env = append(os.Environ(), "GOGUI_DEV=1")
	if err := child.Start(); err != nil {
		d.mu.Unlock()
		fmt.Printf("[dev] ✗ 启动失败: %v\n", err)
		return false
	}
	d.child = child
	d.restarts++
	d.lastExe = exe
	d.mu.Unlock()

	fmt.Printf("[dev] ✓ %s：构建 %s → 重启 #%d（pid %d）\n",
		reason, elapsed.Round(time.Millisecond), d.restarts, child.Process.Pid)

	// 清理旧构建产物（保留当前一个）。
	if entries, err := os.ReadDir(d.outDir); err == nil {
		var exes []string
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".exe") || strings.HasSuffix(e.Name(), ".bin") {
				exes = append(exes, filepath.Join(d.outDir, e.Name()))
			}
		}
		sort.Strings(exes)
		for _, p := range exes {
			if p != d.lastExe {
				_ = os.Remove(p)
			}
		}
	}
	return true
}

func (d *devServer) killChild() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.killChildLocked()
}

func (d *devServer) killChildLocked() {
	if d.child == nil || d.child.Process == nil {
		return
	}
	_ = d.child.Process.Kill()
	_, _ = d.child.Process.Wait()
	d.child = nil
}
