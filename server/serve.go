// Package server 把 model 控件树架到本地 HTTP 上（内置微型 HTTP/1.1 实现，
// 不依赖 net/http —— 省 ~5MB 二进制与 crypto/tls 的 32MB 虚拟内存）。
//
// 路由（仅 127.0.0.1，单客户端 bridge，非通用 Web 服务器）：
//
//	GET  /              → 整页 HTML
//	GET  /gg-bridge.js  → 事件桥脚本
//	POST /gg/ev         → 事件 JSON → App.Dispatch → 返回补丁 JSON
//	GET  /gg/sse        → 服务端推送（chunked 流）
//
// 明确的边界（README 同步声明）：不支持 TLS / HTTP2 / chunked 请求体 /
// keep-alive（每请求一连接，Connection: close）。请求体只按 Content-Length 读。
package server

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nimamasl114514/gogui/model"
	"github.com/nimamasl114514/gogui/theme"
)

// Version 是框架版本（/gg/state 暴露）。
const Version = "1.4.0"

// Server 是 gogui 应用的本地 HTTP 宿主。
type Server struct {
	app   *model.App
	theme *theme.Theme

	mu      sync.Mutex
	ln      net.Listener
	url     string
	clients map[chan string]struct{}
	closed  bool
	startAt time.Time

	// 可观测性状态（见 obs.go）。
	obsMu     sync.Mutex
	logs      []logEntry
	ssePushes uint64
	accepts   uint64
	conns     uint64
	acceptErrs uint64

	layoutMu    sync.Mutex
	lastLayout  *layoutReport
	layoutWait  chan *layoutReport
	layoutNonce string

	// 活跃连接集（Close 时强制断开，防 SSE 阻塞写泄漏 goroutine）。
	connMu      sync.Mutex
	activeConns map[net.Conn]struct{}

	// 最近一次广播补丁（/gg/ev 复用，避免每事件渲染两次）。
	patchMu     sync.Mutex
	lastPatch   string
	lastPatchNS int64
	patchSeq    uint64
	renderMu  sync.Mutex // 串行化「渲染+入队」，保证补丁顺序 == 状态顺序

	// 事件追踪 / 历史采样 / 启动时序（见 obs.go）。
	traceMu   sync.Mutex
	spans     []eventSpan
	histMu    sync.Mutex
	history   []historySample
	startOnce sync.Once
	prevEv    uint64
	prevRnd   uint64

	firstClientAt time.Time
	firstRenderAt time.Time
	firstEventAt  time.Time
	traceSeq      uint64
}

// New 创建宿主（未启动）。app 必须已 SetRoot。
func New(app *model.App, th *theme.Theme) *Server {
	if th == nil {
		th = theme.DarkGlow()
	}
	return &Server{app: app, theme: th, clients: map[chan string]struct{}{}}
}

// Start 监听 127.0.0.1 随机端口并开始服务；返回可访问 URL。
func (s *Server) Start() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln != nil {
		return s.url, nil
	}
	if s.app == nil {
		return "", fmt.Errorf("server: app is nil")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	s.ln = ln
	s.url = "http://" + ln.Addr().String()
	s.startAt = time.Now()
	s.activeConns = map[net.Conn]struct{}{}

	// 模型变更 → 渲染 → 缓存补丁 → 广播。
	// renderMu 串行化：并发 notify 时补丁入队顺序 == 状态版本顺序，
	// 客户端不会先收新帧再收旧帧（否则 UI 回滚且无后续帧纠正）。
	s.app.OnUpdate(func() {
		s.mu.Lock()
		closed := s.closed
		s.mu.Unlock()
		if closed {
			return // Close 后不再渲染（避免广播已关闭的通道 + 白付渲染成本）
		}
		s.renderMu.Lock()
		r0 := model.NowNS()
		patch := s.renderBody()
		renderNS := model.NowNS() - r0
		s.patchMu.Lock()
		s.patchSeq++
		s.lastPatch = patch
		s.lastPatchNS = renderNS
		seq := s.patchSeq
		s.patchMu.Unlock()
		s.broadcastWithSeq(patch, seq)
		s.renderMu.Unlock()
	})
	go s.acceptLoop(ln)
	go s.sampleLoop() // 1Hz 历史曲线采样
	return s.url, nil
}

// URL 返回当前监听 URL（未启动为空串）。
func (s *Server) URL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.url
}

// Close 停止服务。
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	ln := s.ln
	for ch := range s.clients {
		close(ch)
	}
	s.clients = map[chan string]struct{}{}
	s.mu.Unlock()
	// 交换并断开活跃连接（connMu 保护，避免与 serveConn 的注册/注销竞争）。
	s.connMu.Lock()
	conns := s.activeConns
	s.activeConns = map[net.Conn]struct{}{}
	s.connMu.Unlock()
	// 强制断开活跃连接：阻塞在 SSE 写上的 goroutine 立即解除（否则永久泄漏）。
	for c := range conns {
		_ = c.Close()
	}
	if ln != nil {
		return ln.Close()
	}
	return nil
}

func (s *Server) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err == nil {
			s.obsMu.Lock()
			s.accepts++
			s.conns++
			s.obsMu.Unlock()
		}
		if err != nil {
			s.obsMu.Lock()
			s.acceptErrs++
			s.obsMu.Unlock()
			// 监听关闭 = 唯一合法退出。
			if errors.Is(err, net.ErrClosed) {
				return
			}
			// 关键（真 bug 修复）：Windows 上浏览器中止握手会让 Accept 返回
			// WSAECONNRESET 等瞬时错误 —— 若当作致命退出，accept 循环死亡，
			// 监听器仍在但再没人接受连接，后续所有连接全部超时（实锤：
			// Chromium 连杀页面时随机出现 dial timeout / ERR_CONNECTION_TIMED_OUT）。
			// 瞬时错误：小睡后继续接受。
			time.Sleep(10 * time.Millisecond)
			continue
		}
		go s.serveConn(conn)
	}
}

// serveConn 处理单个连接（一个请求一条连接）。
func (s *Server) serveConn(conn net.Conn) {
	defer conn.Close()
	// 请求级 panic 兜底：任何解析/渲染/处理器 panic 只死一条连接，不杀进程。
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("gogui: request panic: %v\n", r)
		}
	}()
	s.connMu.Lock()
	if s.activeConns != nil {
		s.activeConns[conn] = struct{}{}
	}
	s.connMu.Unlock()
	defer func() {
		s.connMu.Lock()
		delete(s.activeConns, conn)
		s.connMu.Unlock()
	}()
	// 请求读取上限 10s（SSE 转入流式后重设）。
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	br := bufio.NewReaderSize(conn, 32*1024)
	bw := bufio.NewWriterSize(conn, 32*1024)

	line, err := readLine(br, maxLineLen)
	if err != nil {
		return
	}
	method, path, query, ok := parseRequestLine(line)
	if !ok {
		writeError(bw, 400, "bad request line")
		return
	}
	headers := parseHeaders(br)

	// —— 本机 bridge 的入口安全校验 ——
	if !s.checkHost(headers["host"]) {
		writeError(bw, 421, "misdirected request")
		return
	}
	if method == "POST" && !s.checkPostOrigin(headers) {
		writeError(bw, 403, "forbidden origin")
		return
	}
	if method == "POST" && !isJSONContentType(headers["content-type"]) {
		writeError(bw, 415, "content-type must be application/json")
		return
	}

	switch {
	case method == "GET" && path == "/":
		body := s.renderHTML()
		writeResponse(bw, 200, "text/html; charset=utf-8", []byte(body))
	case method == "GET" && path == "/gg-bridge.js":
		writeResponse(bw, 200, "application/javascript; charset=utf-8", []byte(BridgeJS))
	case method == "POST" && path == "/gg/ev":
		s.handleEvent(br, bw, headers)
	case method == "GET" && path == "/gg/patch":
		// 当前完整 body（SSE onopen 对齐用，解决「连上瞬间漏帧」竞态）。
		writeResponse(bw, 200, "text/html; charset=utf-8", []byte(s.renderBody()))
	case method == "GET" && path == "/gg/sse":
		s.handleSSE(conn, bw) // 阻塞至断开
	case method == "GET" && path == "/gg/state":
		s.handleState(bw, query)
	case method == "GET" && path == "/gg/health":
		s.handleHealth(bw)
	case method == "GET" && path == "/gg/metrics":
		s.handleMetrics(bw)
	case method == "GET" && path == "/gg/log":
		s.handleLogGet(bw, query)
	case method == "POST" && path == "/gg/log":
		s.handleLogPost(br, bw, headers)
	case method == "GET" && path == "/gg/layout":
		s.handleLayoutGet(bw, query)
	case method == "POST" && path == "/gg/layout-report":
		s.handleLayoutReport(br, bw, headers)
	case method == "GET" && path == "/gg/trace":
		s.handleTrace(bw, query)
	case method == "POST" && path == "/gg/perf":
		s.handlePerf(br, bw, headers)
	case method == "GET" && path == "/gg/history":
		s.handleHistory(bw, query)
	case method == "GET" && strings.HasPrefix(path, "/gg/widget/"):
		s.handleWidget(bw, strings.TrimPrefix(path, "/gg/widget/"))
	default:
		writeError(bw, 404, "not found")
	}
}

// ---- 请求解析 ----

// maxLineLen 是单行（请求行/每个头）的长度上限，防恶意长行撑爆内存。
const maxLineLen = 8 << 10

// readLine 读一行（含换行），超过 max 字节报错（bufio.ReadSlice 分段，不无限增长）。
func readLine(br *bufio.Reader, max int) (string, error) {
	var sb strings.Builder
	for {
		chunk, err := br.ReadSlice('\n')
		if sb.Len()+len(chunk) > max {
			return "", fmt.Errorf("line too long (>%d)", max)
		}
		sb.Write(chunk)
		if err == nil {
			return sb.String(), nil
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		return "", err
	}
}

// parseRequestLine 解析 "GET /path?query HTTP/1.1"，并做严格合法性校验：
// 方法必须是 1-16 个大写字母 token；路径必须以 "/" 开头；版本必须是 HTTP/1.x。
func parseRequestLine(line string) (method, path, query string, ok bool) {
	parts := strings.Fields(strings.TrimSpace(line))
	if len(parts) != 3 || !strings.HasPrefix(parts[2], "HTTP/1.") {
		return "", "", "", false
	}
	m := parts[0]
	if len(m) == 0 || len(m) > 16 {
		return "", "", "", false
	}
	for i := 0; i < len(m); i++ {
		if m[i] < 'A' || m[i] > 'Z' {
			return "", "", "", false
		}
	}
	p := parts[1]
	if !strings.HasPrefix(p, "/") {
		return "", "", "", false
	}
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p, query = p[:i], p[i+1:]
	}
	return m, p, query, true
}

// parseHeaders 读到空行为止，键名统一小写；单行限长、头数限 100。
func parseHeaders(br *bufio.Reader) map[string]string {
	headers := make(map[string]string, 8)
	for i := 0; i < 100; i++ {
		line, err := readLine(br, maxLineLen)
		if err != nil {
			return headers
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			return headers
		}
		if j := strings.IndexByte(line, ':'); j > 0 {
			k := strings.ToLower(strings.TrimSpace(line[:j]))
			v := strings.TrimSpace(line[j+1:])
			headers[k] = v
		}
	}
	return headers
}

// ---- 事件与 SSE ----

type evRequest struct {
	WidgetID string `json:"widget_id"`
	Type     string `json:"type"`
	Value    string `json:"value"`
}

type evResponse struct {
	OK      bool   `json:"ok"`
	Err     string `json:"err,omitempty"`
	Patch   string `json:"patch"`
	TraceID string `json:"trace_id,omitempty"`
}

func (s *Server) handleEvent(br *bufio.Reader, bw *bufio.Writer, headers map[string]string) {
	n, clErr := contentLength(headers)
	if clErr == errNoContentLength {
		writeError(bw, 411, "content-length required")
		return
	}
	if clErr != nil {
		writeError(bw, 400, clErr.Error())
		return
	}
	const maxBody = 1 << 20 // 1MB 上限（本地 bridge 的合理上限）
	if n > maxBody {
		writeError(bw, 413, "body too large")
		return
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(br, buf); err != nil {
		writeError(bw, 400, "short body")
		return
	}
	var req evRequest
	if err := json.Unmarshal(buf, &req); err != nil {
		writeError(bw, 400, "bad json")
		return
	}
	t0 := time.Now()
	s.patchMu.Lock()
	seqBefore := s.patchSeq
	s.patchMu.Unlock()
	dispatchErr := s.app.Dispatch(model.Event{WidgetID: req.WidgetID, Type: req.Type, Value: req.Value})
	dispatchUS := time.Since(t0).Microseconds()
	// 补丁复用：Dispatch 内部 notify 已渲染并缓存过补丁（seq 前进）→ 直接复用，
	// 避免每事件渲染两次（拖动/连点场景渲染量减半）。
	s.patchMu.Lock()
	patch := s.lastPatch
	reused := s.patchSeq != seqBefore && s.lastPatch != ""
	renderNS := s.lastPatchNS // 复用路径：沿用产生该补丁的那次渲染耗时
	s.patchMu.Unlock()
	if !reused {
		r0 := model.NowNS()
		patch = s.renderBody()
		renderNS = model.NowNS() - r0
		s.patchMu.Lock()
		s.patchSeq++
		s.lastPatch = patch
		s.lastPatchNS = renderNS
		s.patchMu.Unlock()
	}

	s.mu.Lock()
	if s.firstEventAt.IsZero() {
		s.firstEventAt = t0
	}
	s.mu.Unlock()
	span := eventSpan{
		TS:         t0.Format("15:04:05.000"),
		Widget:     req.WidgetID,
		Type:       req.Type,
		Value:      req.Value,
		DispatchUS: dispatchUS,
		RenderNS:   renderNS,
		PatchBytes: len(patch),
	}
	if dispatchErr != nil {
		span.Err = dispatchErr.Error()
	}
	s.appendSpanAssign(&span)

	resp := evResponse{OK: dispatchErr == nil, Patch: patch, TraceID: span.ID}
	if dispatchErr != nil {
		resp.Err = dispatchErr.Error()
	}
	entry := logEntry{TS: span.TS, Kind: "event",
		Widget: truncate(req.WidgetID, 128), Type: truncate(req.Type, 32), Value: truncate(req.Value, 4096)}
	if dispatchErr != nil {
		entry.Err = dispatchErr.Error()
	}
	s.appendLog(entry)
	payload, _ := json.Marshal(resp)
	writeResponse(bw, 200, "application/json; charset=utf-8", payload)
}

// handleSSE 以 chunked 流推送补丁（阻塞至连接断开或服务关闭）。
func (s *Server) handleSSE(conn net.Conn, bw *bufio.Writer) {
	_ = conn.SetReadDeadline(time.Time{}) // 清除读超时（流式长连接）

	ch := make(chan string, 8)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		writeError(bw, 503, "closed")
		return
	}
	if len(s.clients) == 0 && s.firstClientAt.IsZero() {
		s.firstClientAt = time.Now()
	}
	s.clients[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.clients, ch)
		s.mu.Unlock()
	}()

	defer func() {
		// 正常结束/断开前补 last-chunk（严格客户端不再报 unexpected EOF）。
		_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		_, _ = bw.WriteString("0\r\n\r\n")
		_ = bw.Flush()
	}()
	bw.WriteString("HTTP/1.1 200 OK\r\n" +
		"Content-Type: text/event-stream\r\n" +
		"Cache-Control: no-store\r\n" +
		"Connection: close\r\n" +
		"Transfer-Encoding: chunked\r\n\r\n")
	if err := bw.Flush(); err != nil {
		return
	}
	// 每帧写超时：浏览器标签页被挂起/网络栈卡死时，阻塞写不得永久占住 goroutine。
	if !writeSSEChunkConn(conn, bw, s.renderBody()) {
		return
	}
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case patch, ok := <-ch:
			if !ok || !writeSSEChunkConn(conn, bw, patch) {
				return
			}
			s.obsMu.Lock()
			s.ssePushes++
			s.obsMu.Unlock()
		case <-ping.C:
			if !writeChunkConn(conn, bw, []byte(": ping\n\n")) {
				return
			}
		}
	}
}

// writeSSEChunkConn 带写超时的 SSE data 帧。
func writeSSEChunkConn(conn net.Conn, bw *bufio.Writer, payload string) bool {
	var b strings.Builder
	b.Grow(len(payload) + 16)
	for _, line := range strings.Split(payload, "\n") {
		b.WriteString("data: ")
		// SSE 规范：CR 也是行终止符，payload 里的裸 CR 会被切断 —— 去掉。
		b.WriteString(strings.ReplaceAll(line, "\r", ""))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	return writeChunkConn(conn, bw, []byte(b.String()))
}

// writeChunkConn 写一个 HTTP chunk（带 5s 写超时）并 flush。
func writeChunkConn(conn net.Conn, bw *bufio.Writer, p []byte) bool {
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	bw.WriteString(strconv.FormatInt(int64(len(p)), 16))
	bw.WriteString("\r\n")
	bw.Write(p)
	bw.WriteString("\r\n")
	return bw.Flush() == nil
}

// writeSSEChunk 把 payload 编成 SSE data 帧后按 chunk 写出。
func writeSSEChunk(bw *bufio.Writer, payload string) bool {
	var b strings.Builder
	b.Grow(len(payload) + 16)
	for _, line := range strings.Split(payload, "\n") {
		b.WriteString("data: ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	return writeChunk(bw, []byte(b.String()))
}

// writeChunk 写一个 HTTP chunk 并 flush。
func writeChunk(bw *bufio.Writer, p []byte) bool {
	bw.WriteString(strconv.FormatInt(int64(len(p)), 16))
	bw.WriteString("\r\n")
	bw.Write(p)
	bw.WriteString("\r\n")
	return bw.Flush() == nil
}

// ---- 响应写出 ----

func writeResponse(bw *bufio.Writer, status int, contentType string, body []byte) {
	bw.WriteString("HTTP/1.1 ")
	bw.WriteString(strconv.Itoa(status))
	bw.WriteString(" ")
	bw.WriteString(statusText(status))
	bw.WriteString("\r\nContent-Type: ")
	bw.WriteString(contentType)
	bw.WriteString("\r\nContent-Length: ")
	bw.WriteString(strconv.Itoa(len(body)))
	bw.WriteString("\r\nCache-Control: no-store\r\nConnection: close\r\n\r\n")
	bw.Write(body)
	_ = bw.Flush()
}

func writeError(bw *bufio.Writer, status int, msg string) {
	writeResponse(bw, status, "text/plain; charset=utf-8", []byte(msg))
}

// truncate 截断字符串（日志防内存放大：Value/Msg/Stack 全部限长）。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func statusText(status int) string {
	switch status {
	case 200:
		return "OK"
	case 400:
		return "Bad Request"
	case 404:
		return "Not Found"
	case 405:
		return "Method Not Allowed"
	case 413:
		return "Payload Too Large"
	case 503:
		return "Service Unavailable"
	default:
		return "Status"
	}
}

// broadcast 把补丁投递给全部 SSE 客户端（慢客户端丢帧，morph 幂等可自愈）。
// broadcast 投递一条原始 payload（命令帧复用同一数据通道）。
func (s *Server) broadcast(payload string) { s.broadcastWithSeq(payload, 0) }

func (s *Server) broadcastWithSeq(patch string, seq uint64) {
	s.mu.Lock()
	for ch := range s.clients {
		select {
		case ch <- patch:
		default: // 慢客户端丢帧（视为断开由写超时兜底；丢帧后由写超时/重连拿全量）
		}
	}
	s.mu.Unlock()
	_ = seq
}

// checkHost 防 DNS rebinding：Host 必须等于监听地址（或缺失时按宽松处理）。
func (s *Server) checkHost(host string) bool {
	if host == "" {
		return true // HTTP/1.0 客户端可不带 Host
	}
	s.mu.Lock()
	addr := ""
	if s.ln != nil {
		addr = s.ln.Addr().String()
	}
	s.mu.Unlock()
	return host == addr
}

// checkPostOrigin 拦截跨站表单/脚本（Origin 必须缺省或等于本机源）。
func (s *Server) checkPostOrigin(headers map[string]string) bool {
	origin := headers["origin"]
	if origin == "" {
		return true // 同源 fetch 可能不带 Origin（GET 类）；POST 来自本页 fetch 通常带
	}
	s.mu.Lock()
	addr := ""
	if s.ln != nil {
		addr = s.ln.Addr().String()
	}
	s.mu.Unlock()
	return origin == "http://"+addr
}

// isJSONContentType 校验 POST 的 Content-Type（挡住 text/plain 简单请求的 CSRF 面）。
func isJSONContentType(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(ct))
	return strings.HasPrefix(ct, "application/json")
}

// contentLength 严格解析 Content-Length：
//   - 缺失（POST 需要 body）→ errMissing=True → 411
//   - 非法/负数 → error → 400
func contentLength(headers map[string]string) (n int, err error) {
	cl, ok := headers["content-length"]
	if !ok {
		return 0, errNoContentLength
	}
	v, perr := strconv.Atoi(cl)
	if perr != nil || v < 0 {
		return 0, fmt.Errorf("bad content-length %q", cl)
	}
	return v, nil
}

var errNoContentLength = fmt.Errorf("missing content-length")
