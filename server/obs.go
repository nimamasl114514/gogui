package server

// 可观测性端点：/gg/state /gg/health /gg/metrics /gg/log。
//
// 设计目标（用户反馈「不截图就不知道是否正常」）：
//   - 任意时刻 curl 一个 URL 就能拿到：状态计数、最近事件、渲染耗时、
//     JS 错误、SSE 连接数、协程数、堆大小 —— 语义是否正确不再靠肉眼截图；
//   - /gg/metrics 给监控系统（Prometheus 文本口径）；
//   - /gg/log 环形缓冲保留最近 256 条事件与前端错误。

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/nimamasl114514/gogui/model"
	"github.com/nimamasl114514/gogui/render"
	"github.com/nimamasl114514/gogui/snapshot"
)

// logEntry 是事件/前端错误日志条目。
type logEntry struct {
	TS     string `json:"ts"`
	Kind   string `json:"kind"` // event | js_error
	Widget string `json:"widget,omitempty"`
	Type   string `json:"type,omitempty"`
	Value  string `json:"value,omitempty"`
	Err    string `json:"err,omitempty"`
	Msg    string `json:"msg,omitempty"`
	Where  string `json:"where,omitempty"`
	Stack  string `json:"stack,omitempty"`
}

// logCap 是环形缓冲容量。
const logCap = 256

// appendLog 追加一条日志（环形覆盖，超限丢最旧）。
func (s *Server) appendLog(e logEntry) {
	s.obsMu.Lock()
	if len(s.logs) >= logCap {
		copy(s.logs, s.logs[1:])
		s.logs[len(s.logs)-1] = e
	} else {
		s.logs = append(s.logs, e)
	}
	s.obsMu.Unlock()
}

// renderBody / renderHTML 统一渲染计时（RecordRender 用于 /gg/state 指标）。
func (s *Server) renderBody() string {
	t0 := model.NowNS()
	b := render.Body(s.app)
	s.app.RecordRenderNS(model.NowNS() - t0)
	s.markFirstRender()
	return b
}

func (s *Server) renderHTML() string {
	t0 := model.NowNS()
	h := render.HTML(s.app, s.theme)
	s.app.RecordRenderNS(model.NowNS() - t0)
	s.markFirstRender()
	return h
}

// markFirstRender 记录首次渲染时刻（启动时序）。
func (s *Server) markFirstRender() {
	s.mu.Lock()
	if s.firstRenderAt.IsZero() {
		s.firstRenderAt = time.Now()
	}
	s.mu.Unlock()
}

// ---- /gg/state ----

type stateServer struct {
	URL        string      `json:"url"`
	UptimeMS   int64       `json:"uptime_ms"`
	SSEClients int         `json:"sse_clients"`
	SSEPushes  uint64      `json:"sse_pushes"`
	LogCount   int         `json:"log_count"`
	Accepts    uint64      `json:"accepts_total"`
	Conns      uint64      `json:"conns_total"`
	AcceptErrs uint64      `json:"accept_errs_total"`
	Startup    startupInfo `json:"startup"`
}

type startupInfo struct {
	FirstClientMS int64 `json:"first_client_ms"`
	FirstRenderMS int64 `json:"first_render_ms"`
	FirstEventMS  int64 `json:"first_event_ms"`
}

type stateResponse struct {
	Version string       `json:"version"`
	Server  stateServer  `json:"server"`
	App     model.AppStats `json:"app"`
	Tree    string       `json:"tree,omitempty"`
	Recent  []logEntry   `json:"recent_log"`
}

// handleState 输出全量运行时状态；?tree=1 附控件树快照文本；?n=N 控制日志条数。
func (s *Server) handleState(bw *bufio.Writer, query string) {
	q := parseQuery(query)
	n := queryInt(q, "n", 20, logCap)

	s.obsMu.Lock()
	acceptsN, connsN, acceptErrsN := s.accepts, s.conns, s.acceptErrs
	recent := make([]logEntry, 0, n)
	start := len(s.logs) - n
	if start < 0 {
		start = 0
	}
	recent = append(recent, s.logs[start:]...)
	pushes := s.ssePushes
	logCount := len(s.logs)
	s.obsMu.Unlock()

	s.mu.Lock()
	clients := len(s.clients)
	url := s.url
	startedAt := s.startAt
	fc, fr, fe := s.firstClientAt, s.firstRenderAt, s.firstEventAt
	s.mu.Unlock()

	var su startupInfo
	if !fc.IsZero() {
		su.FirstClientMS = fc.Sub(startedAt).Milliseconds()
	}
	if !fr.IsZero() {
		su.FirstRenderMS = fr.Sub(startedAt).Milliseconds()
	}
	if !fe.IsZero() {
		su.FirstEventMS = fe.Sub(startedAt).Milliseconds()
	}

	resp := stateResponse{
		Version: Version,
		Server: stateServer{
			URL:        url,
			UptimeMS:   time.Since(startedAt).Milliseconds(),
			SSEClients: clients,
			SSEPushes:  pushes,
			LogCount:   logCount,
			Accepts:    acceptsN,
			Conns:      connsN,
			AcceptErrs: acceptErrsN,
			Startup:    su,
		},
		App:    s.app.Stats(),
		Recent: recent,
	}
	if q["tree"] == "1" {
		resp.Tree = snapshot.Tree(s.app)
	}
	payload, _ := json.MarshalIndent(resp, "", "  ")
	writeResponse(bw, 200, "application/json; charset=utf-8", payload)
}

// ---- /gg/health ----

type healthResponse struct {
	OK           bool     `json:"ok"`
	Warnings     []string `json:"warnings,omitempty"`
	Version      string   `json:"version"`
	UptimeMS     int64  `json:"uptime_ms"`
	Widgets      int    `json:"widgets"`
	Events       uint64 `json:"events_total"`
	Failures     uint64 `json:"events_failed"`
	JSErrors     uint64 `json:"js_errors_total"`
	LastRenderNS int64  `json:"render_last_ns"`
	LastEventAgeMS int64 `json:"last_event_age_ms,omitempty"`
	Goroutines   int    `json:"goroutines"`
	HeapKB       uint64 `json:"heap_kb"`
}

// handleHealth 健康判定：服务在听 + 有根控件 + 渲染过一次即为 ok。
// 计数器原样呈现（events_failed / js_errors_total > 0 由调用方判读）。
func (s *Server) handleHealth(bw *bufio.Writer) {
	st := s.app.Stats()
	h := healthResponse{
		OK:           true,
		Version:      Version,
		UptimeMS:     time.Since(s.startAt).Milliseconds(),
		Widgets:      st.Widgets,
		Events:       st.Events,
		Failures:     st.Failures,
		JSErrors:     st.JSErrors,
		LastRenderNS: st.LastRenderNS,
		Goroutines:   st.Goroutines,
		HeapKB:       st.HeapKB,
	}
	if st.Widgets == 0 || st.Renders == 0 {
		h.OK = false
	}
	if st.LastEvent != nil {
		h.LastEventAgeMS = st.LastEvent.AgeMS
	}
	// 健康不变量（warn 级，不影响 ok 判定）。
	if st.Failures > 0 {
		h.Warnings = append(h.Warnings, fmt.Sprintf("events_failed=%d（处理器错误或 panic，见 /gg/log）", st.Failures))
	}
	if st.JSErrors > 0 {
		h.Warnings = append(h.Warnings, fmt.Sprintf("js_errors=%d（前端错误，见 /gg/log）", st.JSErrors))
	}
	if st.MaxRenderNS > 50_000_000 {
		h.Warnings = append(h.Warnings, fmt.Sprintf("render_max=%dms 超预算 50ms（检查大数据量/慢处理器）", st.MaxRenderNS/1_000_000))
	}
	s.mu.Lock()
	clientsNow := len(s.clients)
	s.mu.Unlock()
	if time.Since(s.startAt) > 3*time.Second && clientsNow == 0 {
		h.Warnings = append(h.Warnings, "无 SSE 客户端连接（headless 模式属正常；窗口模式应检查 bridge 加载）")
	}
	status := 200
	if !h.OK {
		status = 503
	}
	payload, _ := json.MarshalIndent(h, "", "  ")
	writeResponse(bw, status, "application/json; charset=utf-8", payload)
}

// ---- /gg/metrics ----

// handleMetrics 输出 Prometheus 文本口径指标（每行 name value）。
func (s *Server) handleMetrics(bw *bufio.Writer) {
	st := s.app.Stats()
	s.obsMu.Lock()
	pushes := s.ssePushes
	s.obsMu.Unlock()
	s.mu.Lock()
	clients := len(s.clients)
	s.mu.Unlock()

	var b strings.Builder
	b.Grow(768)
	w := func(name string, v int64) {
		b.WriteString(name)
		b.WriteString(" ")
		b.WriteString(strconv.FormatInt(v, 10))
		b.WriteString("\n")
	}
	w("gogui_uptime_seconds", time.Since(s.startAt).Milliseconds()/1000)
	w("gogui_widgets", int64(st.Widgets))
	w("gogui_events_total", int64(st.Events))
	w("gogui_events_failed_total", int64(st.Failures))
	w("gogui_renders_total", int64(st.Renders))
	w("gogui_render_last_ns", st.LastRenderNS)
	w("gogui_render_max_ns", st.MaxRenderNS)
	w("gogui_js_errors_total", int64(st.JSErrors))
	w("gogui_sse_clients", int64(clients))
	w("gogui_sse_pushes_total", int64(pushes))
	w("gogui_goroutines", int64(st.Goroutines))
	w("gogui_heap_kb", int64(st.HeapKB))
	writeResponse(bw, 200, "text/plain; version=0.0.4; charset=utf-8", []byte(b.String()))
}

// ---- /gg/log ----

// handleLogGet 返回最近 N 条日志（?n=N，默认 50）。
func (s *Server) handleLogGet(bw *bufio.Writer, query string) {
	q := parseQuery(query)
	n := queryInt(q, "n", 50, logCap)
	s.obsMu.Lock()
	start := len(s.logs) - n
	if start < 0 {
		start = 0
	}
	out := make([]logEntry, len(s.logs)-start)
	copy(out, s.logs[start:])
	s.obsMu.Unlock()
	payload, _ := json.MarshalIndent(out, "", "  ")
	writeResponse(bw, 200, "application/json; charset=utf-8", payload)
}

// logPostBody 是前端错误回流请求体。
type logPostBody struct {
	Level string `json:"level"` // error | warn | info
	Msg   string `json:"msg"`
	Where string `json:"where"`
	Stack string `json:"stack"`
}

// handleLogPost 接收前端 JS 错误（window.onerror / unhandledrejection / bridge 失败）。
func (s *Server) handleLogPost(br *bufio.Reader, bw *bufio.Writer, headers map[string]string) {
	n, clErr := contentLength(headers)
	if clErr != nil {
		writeError(bw, 400, clErr.Error())
		return
	}
	const maxBody = 64 << 10
	if n <= 0 || n > maxBody {
		writeError(bw, 400, "bad body size")
		return
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(br, buf); err != nil {
		writeError(bw, 400, "short body")
		return
	}
	var body logPostBody
	if err := json.Unmarshal(buf, &body); err != nil {
		writeError(bw, 400, "bad json")
		return
	}
	kind := "js_error"
	if body.Level == "warn" || body.Level == "info" {
		kind = "js_" + body.Level
	}
	s.appendLog(logEntry{
		TS:    time.Now().Format("15:04:05.000"),
		Kind:  kind,
		Msg:   truncate(body.Msg, 4096),
		Where: truncate(body.Where, 512),
		Stack: truncate(body.Stack, 4096),
	})
	if kind == "js_error" {
		s.app.RecordJSError()
	}
	writeResponse(bw, 200, "application/json; charset=utf-8", []byte(`{"ok":true}`))
}

// ---- 工具 ----

// parseQuery 解析 k=v&k2=v2（不做 URL 解码——本地固定参数足够）。
func parseQuery(raw string) map[string]string {
	m := map[string]string{}
	for _, kv := range strings.Split(raw, "&") {
		if kv == "" {
			continue
		}
		if i := strings.IndexByte(kv, '='); i > 0 {
			m[kv[:i]] = kv[i+1:]
		} else {
			m[kv] = ""
		}
	}
	return m
}

func queryInt(q map[string]string, key string, def, max int) int {
	v, ok := q[key]
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

// ---- /gg/layout：控件盒模型与计算样式内省 ----

// layoutItem 是单个控件的布局/样式快照（JS collectLayout 上报）。
type layoutItem struct {
	GID        string  `json:"gid"`
	Tag        string  `json:"tag"`
	Cls        string  `json:"cls"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	W          float64 `json:"w"`
	H          float64 `json:"h"`
	Display    string  `json:"display"`
	Visibility string  `json:"visibility"`
	Opacity    string  `json:"opacity"`
	FontSize   string  `json:"fontSize"`
	FontWeight string  `json:"fontWeight"`
	Color      string  `json:"color"`
	Background string  `json:"background"`
	BorderRadius string `json:"borderRadius"`
	Animation    string `json:"animation,omitempty"` // name|duration|play-state
}

// layoutReport 是一次完整采集。
type layoutReport struct {
	Nonce      string       `json:"nonce"`
	CollectedAt string      `json:"collected_at,omitempty"`
	Viewport   struct {
		W float64 `json:"w"`
		H float64 `json:"h"`
	} `json:"viewport"`
	Items []layoutItem `json:"items"`
}

// broadcastCmd 把命令帧（JSON）发到所有 SSE 客户端（与补丁复用同一通道，JS 以 '{' 区分）。
func (s *Server) broadcastCmd(payload string) {
	s.broadcast(payload)
}

// requestLayout 向浏览器发采集命令并等待上报（无客户端时超时）。
func (s *Server) requestLayout(timeout time.Duration) (*layoutReport, error) {
	nonce := strconv.FormatInt(time.Now().UnixNano(), 10)
	wait := make(chan *layoutReport, 1)
	s.layoutMu.Lock()
	if s.layoutWait != nil {
		s.layoutMu.Unlock()
		return nil, errLayoutBusy
	}
	s.layoutWait = wait
	s.layoutNonce = nonce
	s.layoutMu.Unlock()
	defer func() {
		s.layoutMu.Lock()
		if s.layoutWait == wait {
			s.layoutWait = nil // 超时/完成都要清（防陈旧 nonce 残留）
		}
		s.layoutMu.Unlock()
	}()

	cmd, _ := json.Marshal(map[string]string{"cmd": "layout", "nonce": nonce})
	s.broadcastCmd(string(cmd))

	select {
	case rep := <-wait:
		return rep, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("layout: 无客户端在 %s 内上报（bridge 未加载或当前为无窗口模式）", timeout)
	}
}

var errLayoutBusy = fmt.Errorf("layout: 采集进行中（并发请求请稍后重试或使用 ?cached=1）")

// handleLayoutGet 触发一次采集（?cached=1 直接返回上次结果）。
func (s *Server) handleLayoutGet(bw *bufio.Writer, query string) {
	q := parseQuery(query)
	if q["cached"] == "1" {
		s.layoutMu.Lock()
		last := s.lastLayout
		s.layoutMu.Unlock()
		if last == nil {
			writeError(bw, 503, "no cached layout yet")
			return
		}
		payload, _ := json.MarshalIndent(last, "", "  ")
		writeResponse(bw, 200, "application/json; charset=utf-8", payload)
		return
	}
	rep, err := s.requestLayout(2 * time.Second)
	if err != nil {
		if err == errLayoutBusy {
			writeError(bw, 409, err.Error())
			return
		}
		writeError(bw, 503, err.Error())
		return
	}
	payload, _ := json.MarshalIndent(rep, "", "  ")
	writeResponse(bw, 200, "application/json; charset=utf-8", payload)
}

// handleLayoutReport 接收浏览器上报（存缓存 + 唤醒等待者）。
func (s *Server) handleLayoutReport(br *bufio.Reader, bw *bufio.Writer, headers map[string]string) {
	n, clErr := contentLength(headers)
	if clErr != nil {
		writeError(bw, 400, clErr.Error())
		return
	}
	const maxBody = 4 << 20 // 万级控件约 1-2MB，4MB 上限
	if n <= 0 || n > maxBody {
		writeError(bw, 400, "bad body size")
		return
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(br, buf); err != nil {
		writeError(bw, 400, "short body")
		return
	}
	var rep layoutReport
	if err := json.Unmarshal(buf, &rep); err != nil {
		writeError(bw, 400, "bad json")
		return
	}
	rep.CollectedAt = time.Now().Format("15:04:05.000")

	s.layoutMu.Lock()
	s.lastLayout = &rep
	if s.layoutWait != nil && rep.Nonce == s.layoutNonce {
		select {
		case s.layoutWait <- &rep:
		default:
		}
		s.layoutWait = nil
	}
	s.layoutMu.Unlock()

	writeResponse(bw, 200, "application/json; charset=utf-8", []byte(`{"ok":true}`))
}

// ===================== A. 端到端事件追踪 =====================

// eventSpan 是一次事件的端到端追踪记录（Go 侧耗时 + 前端回填）。
type eventSpan struct {
	ID         string `json:"id"`
	TS         string `json:"ts"`
	Widget     string `json:"widget"`
	Type       string `json:"type"`
	Value      string `json:"value"`
	DispatchUS int64  `json:"dispatch_us"`
	RenderNS   int64  `json:"render_ns"`
	PatchBytes int    `json:"patch_bytes"`
	Err        string `json:"err,omitempty"`
	// 前端回填（POST /gg/perf）：事件发生→响应到达→morph 完成。
	RTTUS   int64 `json:"rtt_us,omitempty"`
	MorphUS int64 `json:"morph_us,omitempty"`
	Applied bool  `json:"applied"`
}

const spanCap = 200

// appendSpanAssign 分配 ID 并追加追踪记录（同一把锁，保证 ID 顺序 == 入 slice 顺序）。
func (s *Server) appendSpanAssign(sp *eventSpan) {
	s.traceMu.Lock()
	s.traceSeq++
	sp.ID = "t" + strconv.FormatUint(s.traceSeq, 10)
	if len(s.spans) >= spanCap {
		copy(s.spans, s.spans[1:])
		s.spans[len(s.spans)-1] = *sp
	} else {
		s.spans = append(s.spans, *sp)
	}
	s.traceMu.Unlock()
}

// handleTrace 返回最近 N 条追踪（默认 20）。
func (s *Server) handleTrace(bw *bufio.Writer, query string) {
	q := parseQuery(query)
	n := queryInt(q, "n", 20, spanCap)
	s.traceMu.Lock()
	start := len(s.spans) - n
	if start < 0 {
		start = 0
	}
	out := make([]eventSpan, len(s.spans)-start)
	copy(out, s.spans[start:])
	s.traceMu.Unlock()
	payload, _ := json.MarshalIndent(out, "", "  ")
	writeResponse(bw, 200, "application/json; charset=utf-8", payload)
}

// perfBody 是前端耗时回填（morph/rtt）。
type perfBody struct {
	TraceID string `json:"trace_id"`
	RTTUS   int64  `json:"rtt_us"`
	MorphUS int64  `json:"morph_us"`
}

// handlePerf 把前端 morph/rtt 耗时 join 回对应 span。
func (s *Server) handlePerf(br *bufio.Reader, bw *bufio.Writer, headers map[string]string) {
	n, clErr := contentLength(headers)
	if clErr != nil {
		writeError(bw, 400, clErr.Error())
		return
	}
	if n <= 0 || n > 4096 {
		writeError(bw, 400, "bad body size")
		return
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(br, buf); err != nil {
		writeError(bw, 400, "short body")
		return
	}
	var body perfBody
	if err := json.Unmarshal(buf, &body); err != nil {
		writeError(bw, 400, "bad json")
		return
	}
	s.traceMu.Lock()
	for i := len(s.spans) - 1; i >= 0; i-- {
		if s.spans[i].ID == body.TraceID {
			s.spans[i].RTTUS = body.RTTUS
			s.spans[i].MorphUS = body.MorphUS
			s.spans[i].Applied = true
			break
		}
	}
	s.traceMu.Unlock()
	writeResponse(bw, 200, "application/json; charset=utf-8", []byte(`{"ok":true}`))
}

// ===================== B. 1Hz 历史曲线 =====================

// historySample 是每秒一个的采样点。
type historySample struct {
	T         string `json:"t"`
	EvDelta   uint64 `json:"events_delta"`
	RndDelta  uint64 `json:"renders_delta"`
	RenderNS  int64  `json:"render_last_ns"`
	HeapKB    uint64 `json:"heap_kb"`
	Goroutine int    `json:"goroutines"`
}

const histCap = 300 // 5 分钟

// sampleLoop 每 1s 采样一次历史（服务生命周期内常驻）。
func (s *Server) sampleLoop() {
	tk := time.NewTicker(time.Second)
	defer tk.Stop()
	for range tk.C {
		s.mu.Lock()
		closed := s.closed
		s.mu.Unlock()
		if closed {
			return
		}
		st := s.app.Stats()
		evD := st.Events - s.prevEv
		rndD := st.Renders - s.prevRnd
		s.prevEv, s.prevRnd = st.Events, st.Renders
		sample := historySample{
			T:         time.Now().Format("15:04:05"),
			EvDelta:   evD,
			RndDelta:  rndD,
			RenderNS:  st.LastRenderNS,
			HeapKB:    st.HeapKB,
			Goroutine: st.Goroutines,
		}
		s.histMu.Lock()
		if len(s.history) >= histCap {
			copy(s.history, s.history[1:])
			s.history[len(s.history)-1] = sample
		} else {
			s.history = append(s.history, sample)
		}
		s.histMu.Unlock()
	}
}

// handleHistory 返回最近 N 个采样点（默认 120 = 2 分钟）。
func (s *Server) handleHistory(bw *bufio.Writer, query string) {
	q := parseQuery(query)
	n := queryInt(q, "n", 120, histCap)
	s.histMu.Lock()
	start := len(s.history) - n
	if start < 0 {
		start = 0
	}
	out := make([]historySample, len(s.history)-start)
	copy(out, s.history[start:])
	s.histMu.Unlock()
	payload, _ := json.MarshalIndent(out, "", "  ")
	writeResponse(bw, 200, "application/json; charset=utf-8", payload)
}

// ===================== C. 单控件透视 =====================

// widgetView 合并「模型属性/绑定解析值/缓存布局」的单控件视图。
type widgetView struct {
	ID       string            `json:"id"`
	Kind     string            `json:"kind"`
	Classes  []string          `json:"classes,omitempty"`
	Props    map[string]string `json:"props,omitempty"`
	Resolved map[string]string `json:"resolved,omitempty"`
	Layout   *layoutItem       `json:"layout,omitempty"`
	Found    bool              `json:"found"`
}

// handleWidget 输出单控件透视（/gg/widget/<gid>）。
func (s *Server) handleWidget(bw *bufio.Writer, gid string) {
	view := widgetView{ID: gid, Props: map[string]string{}, Resolved: map[string]string{}}
	s.app.View(func(root *model.Widget) {
		w := root.FindByID(gid)
		if w == nil {
			return
		}
		view.Found = true
		view.Kind = string(w.Kind)
		view.Classes = append([]string(nil), w.Classes...)
		for k, v := range w.Props {
			switch t := v.(type) {
			case string:
				view.Props[k] = t
			case bool, int, float64:
				view.Props[k] = fmt.Sprint(t)
			case []string:
				view.Props[k] = strings.Join(t, "|")
			case []model.Tab:
				titles := make([]string, len(t))
				for i, tb := range t {
					titles[i] = tb.Title
				}
				view.Props[k] = strings.Join(titles, "|")
			}
		}
		switch w.Kind {
		case model.KindLabel, model.KindButton, model.KindBadge:
			view.Resolved["text"] = w.TextValue()
		case model.KindInput, model.KindTextarea:
			view.Resolved["value"] = w.ValueText()
		case model.KindToggle:
			view.Resolved["checked"] = fmt.Sprint(w.CheckedValue())
		case model.KindSlider:
			view.Resolved["value"] = model.FormatFloat(w.SliderValue())
		case model.KindProgress:
			view.Resolved["level"] = fmt.Sprintf("%.3f", w.LevelValue())
		}
	})
	s.layoutMu.Lock()
	if last := s.lastLayout; last != nil {
		for i := range last.Items {
			if last.Items[i].GID == gid {
				it := last.Items[i]
				view.Layout = &it
				break
			}
		}
	}
	s.layoutMu.Unlock()
	status := 200
	if !view.Found {
		status = 404
	}
	payload, _ := json.MarshalIndent(view, "", "  ")
	writeResponse(bw, status, "application/json; charset=utf-8", payload)
}
