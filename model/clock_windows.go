//go:build windows

package model

import (
	"syscall"
	"unsafe"
)

// QPC（QueryPerformanceCounter）提供百纳秒级单调高精度计时：
// Windows 上 time.Since 在微秒级渲染测量中会出现 0ns/15ms 的量化毛刺，
// 渲染耗时/事件追踪统一走 QPC。
var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	procQPC  = kernel32.NewProc("QueryPerformanceCounter")
	procQPF  = kernel32.NewProc("QueryPerformanceFrequency")
	qpcFreq  uint64
)

func init() {
	var f uint64
	procQPF.Call(uintptr(unsafe.Pointer(&f)))
	qpcFreq = f
}

// NowNS 返回高精度单调时间戳（纳秒）。
func NowNS() int64 {
	if qpcFreq == 0 {
		return 0
	}
	var c uint64
	procQPC.Call(uintptr(unsafe.Pointer(&c)))
	// 整数精确换算（商/余数拆分，无 float64 精度损失、无溢出）：
	// ns = (c/freq)*1e9 + (c%freq)*1e9/freq，后半段 < 1e9*freq ≤ 1e16 不溢出。
	q := c / qpcFreq
	r := c % qpcFreq
	return int64(q*1e9 + r*1e9/qpcFreq)
}
