package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Mihomo 实时日志：面板轮询时 Agent 才订阅控制接口的 /logs?level=，把收到的日志编号后
// 缓存在内存里，面板按编号增量取走。多个页面选了不同等级时只订阅一次，取其中最详细的
// 等级，再按各自的等级过滤；一段时间没人轮询就断开订阅、清空缓存，平时不占资源。
// 控制接口推送的日志不受配置文件 log-level 限制，因此可以临时查看 debug 日志。

const (
	mihomoLogBuffer = 1000
	// 每次最多返回的条数；debug 日志很多时只给最新的部分，并告诉面板跳过了多少
	mihomoLogReplyLimit = 500
	mihomoLogIdle       = 15 * time.Second
	mihomoLogPayloadMax = 4096
	mihomoLogRetryMax   = 10 * time.Second
)

// 数值越小越详细，与 Mihomo 的等级顺序一致
var mihomoLogRank = map[string]int{"debug": 0, "info": 1, "warning": 2, "error": 3}

type mihomoLogEntry struct {
	Seq     int64  `json:"seq"`
	Time    string `json:"time"`
	Level   string `json:"level"`
	Payload string `json:"payload"`
}

type mihomoLogReply struct {
	// 当前订阅的等级（多个页面时可能比请求的更详细）
	Stream  string           `json:"stream"`
	Status  string           `json:"status"`
	Code    string           `json:"code,omitempty"`
	Entries []mihomoLogEntry `json:"entries"`
	// 最新编号，下次请求带 after=next
	Next int64 `json:"next"`
	// 本次请求与上次之间，因缓存溢出或单次条数上限而没有返回的条数（已知的部分）
	Skipped int64 `json:"skipped"`
}

type mihomoLogDial func(ctx context.Context, controller *mihomoController, level string) (io.ReadCloser, error)

type mihomoLogTail struct {
	mu         sync.Mutex
	entries    []mihomoLogEntry
	nextSeq    int64
	seen       map[string]time.Time
	level      string
	cancel     context.CancelFunc
	controller *mihomoController
	status     string
	code       string
	idle       *time.Timer
	now        func() time.Time
	dial       mihomoLogDial
	idleAfter  time.Duration
	buffer     int
}

func newMihomoLogTail(client *http.Client) *mihomoLogTail {
	return &mihomoLogTail{
		nextSeq:   1,
		seen:      map[string]time.Time{},
		now:       time.Now,
		dial:      httpMihomoLogDial(client),
		idleAfter: mihomoLogIdle,
		buffer:    mihomoLogBuffer,
	}
}

var errMihomoLogAuth = errors.New("mihomo controller rejected the secret")

func httpMihomoLogDial(client *http.Client) mihomoLogDial {
	return func(ctx context.Context, controller *mihomoController, level string) (io.ReadCloser, error) {
		req, err := controller.request(ctx, http.MethodGet, "/logs?level="+level)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			resp.Body.Close()
			return nil, errMihomoLogAuth
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
		}
		return resp.Body, nil
	}
}

// desiredLevel 返回仍在轮询的页面里最详细的等级；调用方持有锁
func (t *mihomoLogTail) desiredLevel(now time.Time) string {
	best := ""
	for level, at := range t.seen {
		if now.Sub(at) >= t.idleAfter {
			delete(t.seen, level)
			continue
		}
		if best == "" || mihomoLogRank[level] < mihomoLogRank[best] {
			best = level
		}
	}
	return best
}

func (t *mihomoLogTail) poll(controller *mihomoController, level string, after int64) mihomoLogReply {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	t.seen[level] = now
	t.controller = controller
	if desired := t.desiredLevel(now); t.cancel == nil || desired != t.level {
		t.start(desired)
	}
	if t.idle == nil {
		t.idle = time.AfterFunc(t.idleAfter, t.checkIdle)
	} else {
		t.idle.Reset(t.idleAfter)
	}

	latest := t.nextSeq - 1
	if after > latest || after < 0 {
		// Agent 重启过，编号从头开始
		after = 0
	}
	reply := mihomoLogReply{Stream: t.level, Status: t.status, Code: t.code, Entries: []mihomoLogEntry{}, Next: latest}
	buffered := t.trimmed()
	if after > 0 && len(buffered) > 0 && buffered[0].Seq > after+1 {
		reply.Skipped = buffered[0].Seq - after - 1
	}
	rank := mihomoLogRank[level]
	for _, entry := range buffered {
		if entry.Seq > after && mihomoLogRank[entry.Level] >= rank {
			reply.Entries = append(reply.Entries, entry)
		}
	}
	if extra := len(reply.Entries) - mihomoLogReplyLimit; extra > 0 {
		reply.Skipped += int64(extra)
		reply.Entries = reply.Entries[extra:]
	}
	return reply
}

// start 按新等级重新订阅；缓存保留，编号继续递增。调用方持有锁
func (t *mihomoLogTail) start(level string) {
	if t.cancel != nil {
		t.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel, t.level, t.status, t.code = cancel, level, "connecting", ""
	go t.run(ctx, level)
}

func (t *mihomoLogTail) checkIdle() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.desiredLevel(t.now()) != "" {
		t.idle.Reset(t.idleAfter)
		return
	}
	if t.cancel != nil {
		t.cancel()
	}
	t.cancel, t.level, t.status, t.code, t.entries, t.idle = nil, "", "", "", nil, nil
}

func (t *mihomoLogTail) setStatus(ctx context.Context, status, code string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if ctx.Err() == nil {
		t.status, t.code = status, code
	}
}

func (t *mihomoLogTail) run(ctx context.Context, level string) {
	backoff := time.Second
	for ctx.Err() == nil {
		t.mu.Lock()
		controller := t.controller
		t.mu.Unlock()
		body, err := t.dial(ctx, controller, level)
		if err == nil {
			t.setStatus(ctx, "streaming", "")
			backoff = time.Second
			err = t.read(ctx, body)
			body.Close()
		}
		if ctx.Err() != nil {
			return
		}
		code := "CORE_UNREACHABLE"
		if errors.Is(err, errMihomoLogAuth) {
			code = "CORE_AUTH_FAILED"
		}
		t.setStatus(ctx, "error", code)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < mihomoLogRetryMax {
			backoff *= 2
		}
	}
}

func (t *mihomoLogTail) read(ctx context.Context, body io.Reader) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var event struct {
			Type    string `json:"type"`
			Payload string `json:"payload"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		if _, ok := mihomoLogRank[event.Type]; !ok {
			continue
		}
		if len(event.Payload) > mihomoLogPayloadMax {
			event.Payload = event.Payload[:mihomoLogPayloadMax] + "…"
		}
		t.mu.Lock()
		if ctx.Err() != nil {
			t.mu.Unlock()
			return nil
		}
		t.entries = append(t.entries, mihomoLogEntry{
			Seq: t.nextSeq, Time: t.now().Format(time.RFC3339Nano), Level: event.Type, Payload: event.Payload,
		})
		t.nextSeq++
		if len(t.entries) >= 2*t.buffer {
			t.entries = append([]mihomoLogEntry(nil), t.entries[len(t.entries)-t.buffer:]...)
		}
		t.mu.Unlock()
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return io.EOF
}

// trimmed 返回缓存里最近 buffer 条（缓存按 2 倍容量批量裁剪）。调用方持有锁
func (t *mihomoLogTail) trimmed() []mihomoLogEntry {
	if len(t.entries) > t.buffer {
		return t.entries[len(t.entries)-t.buffer:]
	}
	return t.entries
}

func (t *mihomoLogTail) serve(w http.ResponseWriter, r *http.Request, controller *mihomoController) {
	level := r.URL.Query().Get("level")
	if level == "" {
		level = "info"
	}
	if _, ok := mihomoLogRank[level]; !ok {
		mihomoPanelFail(w, http.StatusBadRequest, "INVALID_REQUEST", "level must be debug, info, warning or error")
		return
	}
	var after int64
	if raw := r.URL.Query().Get("after"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			mihomoPanelFail(w, http.StatusBadRequest, "INVALID_REQUEST", "after must be a number")
			return
		}
		after = parsed
	}
	reply := t.poll(controller, level, after)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(reply)
}
