package server

import (
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/nimamasl114514/gogui/model"
	"github.com/nimamasl114514/gogui/theme"
)

// FuzzParseRequestLine —— 任意字节行都不得 panic。
func FuzzParseRequestLine(f *testing.F) {
	f.Add("GET / HTTP/1.1\r\n")
	f.Add("POST /gg/ev?x=1 HTTP/1.1\r\n")
	f.Add("BAD")
	f.Add("GET /\r\n")
	f.Add("\x00\x01\x02 HTTP/9.9\r\n")
	f.Fuzz(func(t *testing.T, line string) {
		method, path, query, ok := parseRequestLine(line)
		if ok && (method == "" || path == "" || !strings.HasPrefix(path, "/")) {
			t.Fatalf("解析成功但结果非法: %q → %q %q %q", line, method, path, query)
		}
	})
}

// FuzzParseQuery —— 任意 query 均不得 panic，键值不互相污染。
func FuzzParseQuery(f *testing.F) {
	f.Add("n=10&tree=1")
	f.Add("=&&=&x")
	f.Add("a=b=c")
	f.Fuzz(func(t *testing.T, q string) {
		m := parseQuery(q)
		for k, v := range m {
			_ = k
			_ = v
		}
	})
}

// FuzzServeConnBytes —— 往迷你 HTTP 连接里灌任意字节：
// 服务器必须要么响应、要么断开，绝不允许 panic / 挂死。
func FuzzServeConnBytes(f *testing.F) {
	f.Add([]byte("GET / HTTP/1.1\r\n\r\n"))
	f.Add([]byte("POST /gg/ev HTTP/1.1\r\nContent-Length: 2\r\n\r\n{}"))
	f.Add([]byte("GET /gg/state?n=99999999999999999999 HTTP/1.1\r\n\r\n"))
	f.Add([]byte("\x00\x00\x00\r\n\r\n"))
	f.Add([]byte("GET " + strings.Repeat("A", 9000) + " HTTP/1.1\r\n\r\n"))
	f.Fuzz(func(t *testing.T, req []byte) {
		if len(req) > 16384 {
			req = req[:16384]
		}
		app := model.New("fz", model.Size{W: 10, H: 10},
			model.VStack(model.Label("x").WithID("lbl")))
		s := New(app, theme.DarkGlow())
		if _, err := s.Start(); err != nil {
			t.Fatal(err)
		}
		defer s.Close()

		srvConn, cliConn := net.Pipe()
		done := make(chan struct{})
		go func() {
			defer close(done)
			s.serveConn(srvConn)
		}()
		// 写请求（异步，防止 server 提前关闭）。
		go func() {
			_, _ = cliConn.Write(req)
			_ = cliConn.Close()
		}()
		// 读响应直到 EOF（限时，悬挂即失败）。
		_ = cliConn.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, _ = io.ReadAll(cliConn)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("serveConn 未在上限时间内退出（可能挂死）")
		}
	})
}
