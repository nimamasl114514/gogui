package model

import (
	"sync"
	"sync/atomic"
)

// 依赖自动追踪（v6 reactivity 核心）：
//
// 绑定注册时把闭包求值一次，期间所有 Var.Get() 自动向「当前收集器」登记；
// 控件据此把依赖的 Var 全部 Attach —— 调用侧不再需要 AttachAll。
//
// 边界（reactivity 经典限制）：构建时未走到的分支里读到的 Var 不会被收集，
// 用 .Depends(&v.X) 显式补充。

var depCollecting atomic.Bool

var (
	depMu    sync.Mutex
	depStack []*[]Attacher
)

// Attacher 由绑定变量实现（*Var[T].Attach）。
type Attacher interface {
	Attach(a *App)
}

// beginCollect 开始一次依赖收集。
// 并发构建（两个 goroutine 同时注册绑定）会让依赖串到错误控件上 ——
// 直接 fail-fast：界面树请在单 goroutine 构建（本项目所有示例均如此）。
func beginCollect() *[]Attacher {
	if !depCollecting.CompareAndSwap(false, true) {
		panic("gogui: 检测到并发构建（依赖收集窗口重叠）——请在单 goroutine 构建界面树")
	}
	depMu.Lock()
	s := &[]Attacher{}
	depStack = append(depStack, s)
	depMu.Unlock()
	return s
}

// endCollect 结束收集并返回命中的依赖（去重）。
func endCollect(s *[]Attacher) []Attacher {
	depMu.Lock()
	if n := len(depStack); n > 0 {
		depStack = depStack[:n-1]
	}
	empty := len(depStack) == 0
	depMu.Unlock()
	if empty {
		depCollecting.Store(false)
	}
	seen := map[Attacher]bool{}
	out := make([]Attacher, 0, len(*s))
	for _, a := range *s {
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	return out
}

// collectDep 由 Var.Get 在收集窗口内调用。
func collectDep(a Attacher) {
	depMu.Lock()
	if n := len(depStack); n > 0 {
		s := depStack[n-1]
		*s = append(*s, a)
	}
	depMu.Unlock()
}

// Depends 显式声明依赖（自动追踪的分支盲区逃生舱）：
//
//	gogui.TextFunc(func() string { if flag { return v.Name.Get() }; return "—" }).
//	    Depends(&v.Name)
func (w *Widget) Depends(vars ...Attacher) *Widget {
	for _, v := range vars {
		if v != nil {
			w.addAttach(v.Attach)
		}
	}
	return w
}

// trackDeps 执行一次求值并把期间读到的 Var 全部挂到控件上。
func (w *Widget) trackDeps(run func()) {
	s := beginCollect()
	func() {
		defer func() { _ = recover() }() // 求值异常不阻断构建（渲染时还会再算）
		run()
	}()
	for _, a := range endCollect(s) {
		w.addAttach(a.Attach)
	}
}
