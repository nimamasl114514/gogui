//go:build windows

// gogui 系统监控 —— 一个活的 GUI 程序。
//
// 写法要点（对比第 2 轮）：采样侧只写 Var，界面侧只声明绑定；
// 没有一处 c.SetText / app.Update —— 数据一 Set，SSE 自动推、DOM 自动刷。
// 纯 stdlib syscall 取数（零第三方依赖）；关窗或 Ctrl+C 退出。
package main

import (
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/nimamasl114514/gogui"
	"github.com/nimamasl114514/gogui/internal/monitorapp"
	"github.com/nimamasl114514/gogui/theme"
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procGlobalMem      = kernel32.NewProc("GlobalMemoryStatusEx")
	procGetSystemTimes = kernel32.NewProc("GetSystemTimes")
	procGetDiskFree    = kernel32.NewProc("GetDiskFreeSpaceExW")
)

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPagefile        uint64
	AvailPagefile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

type filetime struct{ Low, High uint32 }

func (f filetime) u64() uint64 { return uint64(f.High)<<32 | uint64(f.Low) }

// sampleCPU 通过 GetSystemTimes 两次差值算 CPU 繁忙率（0..1）。
func sampleCPU(prevIdle, prevTotal uint64) (busy float64, idle, total uint64) {
	var idleT, kernelT, userT filetime
	procGetSystemTimes.Call(uintptr(unsafe.Pointer(&idleT)),
		uintptr(unsafe.Pointer(&kernelT)), uintptr(unsafe.Pointer(&userT)))
	idle = idleT.u64()
	total = kernelT.u64() + userT.u64() // kernel 时间包含 idle 时间
	dIdle := idle - prevIdle
	dTotal := total - prevTotal
	if dTotal == 0 {
		return 0, idle, total
	}
	return float64(dTotal-dIdle) / float64(dTotal), idle, total
}

// sampleMem 物理内存使用量与使用率。
func sampleMem() (used, total uint64, ratio float64) {
	var ms memoryStatusEx
	ms.Length = uint32(unsafe.Sizeof(ms))
	procGlobalMem.Call(uintptr(unsafe.Pointer(&ms)))
	if ms.TotalPhys == 0 {
		return 0, 0, 0
	}
	return ms.TotalPhys - ms.AvailPhys, ms.TotalPhys, float64(ms.TotalPhys-ms.AvailPhys) / float64(ms.TotalPhys)
}

// sampleDisk C 盘使用量与使用率。
func sampleDisk() (used, total uint64, ratio float64) {
	p, _ := syscall.UTF16PtrFromString(`C:\`)
	var freeAvail, totalBytes, totalFree uint64
	procGetDiskFree.Call(uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&freeAvail)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFree)))
	if totalBytes == 0 {
		return 0, 0, 0
	}
	return totalBytes - totalFree, totalBytes, float64(totalBytes-totalFree) / float64(totalBytes)
}

func gb(n uint64) float64 { return float64(n) / (1 << 30) }

func main() {
	v := &monitorapp.Vars{} // 零值即可用（v5）
	v.Interval.Set(1)
	force := make(chan struct{}, 1)
	app := monitorapp.Build(v, func() {
		select {
		case force <- struct{}{}:
		default:
		}
	})

	start := time.Now()
	stop := make(chan struct{})
	go func() {
		var prevIdle, prevTotal uint64
		var events []string
		last := time.Now()
		for {
			select {
			case <-stop:
				return
			case <-force:
			case <-time.After(200 * time.Millisecond):
			}
			if time.Since(last) < time.Duration(v.Interval.Get()*float64(time.Second)) {
				continue
			}
			last = time.Now()

			busy, idle, total := sampleCPU(prevIdle, prevTotal)
			prevIdle, prevTotal = idle, total
			memUsed, memTotal, memRatio := sampleMem()
			diskUsed, diskTotal, diskRatio := sampleDisk()
			now := time.Now()
			goro := float64(runtime.NumGoroutine())

			events = append(events, fmt.Sprintf("%s  CPU %3.0f%%   MEM %4.1f/%.1f GB   协程 %.0f",
				now.Format("15:04:05"), busy*100, gb(memUsed), gb(memTotal), goro))
			if len(events) > 8 {
				events = events[len(events)-8:]
			}
			evText := ""
			for _, e := range events {
				evText += e + "\n"
			}

			// —— 只写 Var：界面自动刷新（无一处 c.SetText）；
			// Batch 把 13 个 Var 的变更合并成 1 次渲染/SSE 推送。 ——
			app.Batch(func() {
				v.CPU.Set(gogui.RatioOf(busy))
				v.MemUsed.Set(gb(memUsed))
				v.MemTotal.Set(gb(memTotal))
				v.MemRatio.Set(gogui.RatioOf(memRatio))
				v.DiskUsed.Set(gb(diskUsed))
				v.DiskTotal.Set(gb(diskTotal))
				v.DiskRatio.Set(gogui.RatioOf(diskRatio))
				v.Goro.Set(goro)
				lvl := goro / 64.0
				if lvl > 1 {
					lvl = 1
				}
				v.GoroRatio.Set(gogui.RatioOf(lvl))
				v.Uptime.Set(time.Since(start).Truncate(time.Second).String())
				v.Clock.Set(now.Format("15:04:05"))
				v.Events.Set(evText)
			})
		}
	}()
	defer close(stop)

	if err := gogui.Run(app, theme.DarkGlow(), gogui.Webview2()); err != nil {
		panic(err)
	}
}
