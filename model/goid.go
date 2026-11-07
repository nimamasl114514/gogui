package model

import (
	"runtime"
	"sync"
)

// goid 返回当前 goroutine 的编号（用于「树写锁重入」fail-fast 守卫）。
// 从 runtime.Stack 头部解析，约 0.3-1µs；只在 App 锁入口调用。
var goidBuf = sync.Pool{New: func() any { b := make([]byte, 64); return &b }}

func goid() uint64 {
	bp := goidBuf.Get().(*[]byte)
	b := *bp
	n := runtime.Stack(b, false)
	var id uint64
	for i := 0; i < n; i++ {
		c := b[i]
		if c >= '0' && c <= '9' {
			id = id*10 + uint64(c-'0')
		} else if id != 0 {
			break
		}
	}
	goidBuf.Put(bp)
	return id
}
