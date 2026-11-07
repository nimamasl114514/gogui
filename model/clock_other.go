//go:build !windows

package model

import "time"

// NowNS 返回单调时间戳（纳秒；非 Windows 平台用 time.Now）。
func NowNS() int64 { return time.Now().UnixNano() }
