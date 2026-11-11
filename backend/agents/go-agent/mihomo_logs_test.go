package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeLogCore 模拟控制接口：每次订阅记录等级，并通过管道推送日志
type fakeLogCore struct {
	mu      sync.Mutex
	levels  []string
	writers []*io.PipeWriter
	opened  chan string
}

func newFakeLogCore() *fakeLogCore { return &fakeLogCore{opened: make(chan string, 16)} }

func (f *fakeLogCore) dial(ctx context.Context, _ *mihomoController, level string) (io.ReadCloser, error) {
	reader, writer := io.Pipe()
	f.mu.Lock()
	f.levels = append(f.levels, level)
	f.writers = append(f.writers, writer)
	f.mu.Unlock()
	go func() { <-ctx.Done(); writer.CloseWithError(ctx.Err()) }()
	f.opened <- level
	return reader, nil
}

func (f *fakeLogCore) push(t *testing.T, lines ...string) {
	t.Helper()
	f.mu.Lock()
	writer := f.writers[len(f.writers)-1]
	f.mu.Unlock()
	for _, line := range lines {
		if _, err := writer.Write([]byte(line + "\n")); err != nil {
			t.Fatalf("push: %v", err)
		}
	}
}

func waitOpened(t *testing.T, f *fakeLogCore, want string) {
	t.Helper()
	select {
	case level := <-f.opened:
		if level != want {
			t.Fatalf("subscribed at %q, want %q", level, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("no subscription at %q", want)
	}
}

func waitEntries(t *testing.T, tail *mihomoLogTail, n int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		tail.mu.Lock()
		got := tail.nextSeq - 1
		tail.mu.Unlock()
		if got >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected %d entries", n)
}

func event(level, payload string) string {
	data, _ := json.Marshal(map[string]string{"type": level, "payload": payload})
	return string(data)
}

func newTestLogTail(core *fakeLogCore) *mihomoLogTail {
	tail := newMihomoLogTail(http.DefaultClient)
	tail.dial = core.dial
	return tail
}

func payloads(entries []mihomoLogEntry) string {
	out := make([]string, len(entries))
	for i, entry := range entries {
		out[i] = entry.Level + ":" + entry.Payload
	}
	return strings.Join(out, ",")
}

func TestMihomoLogsSubscribeOnPollAndReturnIncrementally(t *testing.T) {
	core := newFakeLogCore()
	tail := newTestLogTail(core)
	controller := &mihomoController{BaseURL: "http://127.0.0.1:9090"}

	first := tail.poll(controller, "info", 0)
	if first.Stream != "info" || first.Status != "connecting" || len(first.Entries) != 0 || first.Next != 0 {
		t.Fatalf("first poll = %+v", first)
	}
	waitOpened(t, core, "info")
	core.push(t, event("info", "[TCP] a"), "not json", event("silent", "x"), event("warning", "[DNS] b"))
	waitEntries(t, tail, 2)

	reply := tail.poll(controller, "info", first.Next)
	if reply.Status != "streaming" || payloads(reply.Entries) != "info:[TCP] a,warning:[DNS] b" || reply.Next != 2 {
		t.Fatalf("reply = %+v", reply)
	}
	core.push(t, event("error", "c"))
	waitEntries(t, tail, 3)
	if again := tail.poll(controller, "info", reply.Next); payloads(again.Entries) != "error:c" || again.Next != 3 {
		t.Fatalf("incremental = %+v", again)
	}
	// 只看警告以上的请求按自己的等级过滤
	if warn := tail.poll(controller, "warning", 0); payloads(warn.Entries) != "warning:[DNS] b,error:c" || warn.Stream != "info" {
		t.Fatalf("warning filter = %+v", warn)
	}
}

func TestMihomoLogsUseMostVerboseRequestedLevelAndDowngradeWhenIdle(t *testing.T) {
	core := newFakeLogCore()
	tail := newTestLogTail(core)
	now := time.Unix(1000, 0)
	tail.now = func() time.Time { return now }
	controller := &mihomoController{}

	tail.poll(controller, "warning", 0)
	waitOpened(t, core, "warning")
	if reply := tail.poll(controller, "debug", 0); reply.Stream != "debug" {
		t.Fatalf("debug viewer must upgrade the subscription: %+v", reply)
	}
	waitOpened(t, core, "debug")
	core.push(t, event("debug", "d"), event("info", "i"))
	waitEntries(t, tail, 2)

	// debug 页面离开后，剩下的 warning 页面让订阅降回 warning，缓存保留
	now = now.Add(mihomoLogIdle)
	reply := tail.poll(controller, "warning", 0)
	if reply.Stream != "warning" || len(reply.Entries) != 0 || reply.Next != 2 {
		t.Fatalf("downgrade = %+v", reply)
	}
	waitOpened(t, core, "warning")
}

func TestMihomoLogsStopAndClearWhenNobodyPolls(t *testing.T) {
	core := newFakeLogCore()
	tail := newTestLogTail(core)
	tail.idleAfter = 30 * time.Millisecond
	controller := &mihomoController{}

	tail.poll(controller, "info", 0)
	waitOpened(t, core, "info")
	core.push(t, event("info", "a"))
	waitEntries(t, tail, 1)

	deadline := time.Now().Add(2 * time.Second)
	for {
		tail.mu.Lock()
		stopped := tail.cancel == nil && tail.entries == nil
		tail.mu.Unlock()
		if stopped {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("idle subscription was not stopped")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// 再次打开时重新订阅；编号继续递增，旧编号不会被误认为新日志
	reply := tail.poll(controller, "info", 0)
	waitOpened(t, core, "info")
	if len(reply.Entries) != 0 || reply.Next != 1 {
		t.Fatalf("after idle = %+v", reply)
	}
}

func TestMihomoLogsReportSkippedEntries(t *testing.T) {
	core := newFakeLogCore()
	tail := newTestLogTail(core)
	tail.buffer = 600
	controller := &mihomoController{}

	tail.poll(controller, "debug", 0)
	waitOpened(t, core, "debug")
	lines := make([]string, 1300)
	for i := range lines {
		lines[i] = event("debug", fmt.Sprint(i+1))
	}
	core.push(t, lines...)
	waitEntries(t, tail, 1300)

	// 缓存只留最近 600 条，单次最多 500 条：1..700 中跳过的数量都要报告
	reply := tail.poll(controller, "debug", 1)
	if reply.Skipped != 799 || len(reply.Entries) != mihomoLogReplyLimit || reply.Entries[0].Payload != "801" || reply.Next != 1300 {
		t.Fatalf("skipped = %d entries = %d first = %s next = %d", reply.Skipped, len(reply.Entries), reply.Entries[0].Payload, reply.Next)
	}
	// Agent 重启后编号比面板记住的小，从头返回
	if reset := tail.poll(controller, "debug", 99999); len(reset.Entries) != mihomoLogReplyLimit || reset.Skipped != 100 {
		t.Fatalf("reset = %+v", reset.Skipped)
	}
}

func TestMihomoLogsReportControllerErrors(t *testing.T) {
	tail := newMihomoLogTail(http.DefaultClient)
	tail.dial = func(context.Context, *mihomoController, string) (io.ReadCloser, error) { return nil, errMihomoLogAuth }
	tail.poll(&mihomoController{}, "info", 0)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if reply := tail.poll(&mihomoController{}, "info", 0); reply.Status == "error" {
			if reply.Code != "CORE_AUTH_FAILED" {
				t.Fatalf("code = %q", reply.Code)
			}
			tail.mu.Lock()
			tail.cancel()
			tail.mu.Unlock()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("auth failure was not reported")
}

func TestMihomoLogsStreamFromController(t *testing.T) {
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/logs" || r.URL.Query().Get("level") != "debug" || r.Header.Get("Authorization") != "Bearer s3cret" {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, event("debug", "[DNS] resolve example.com"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer core.Close()
	tail := newMihomoLogTail(&http.Client{})
	controller := &mihomoController{BaseURL: core.URL, Secret: "s3cret"}
	tail.poll(controller, "debug", 0)
	waitEntries(t, tail, 1)
	reply := tail.poll(controller, "debug", 0)
	if payloads(reply.Entries) != "debug:[DNS] resolve example.com" || reply.Status != "streaming" {
		t.Fatalf("reply = %+v", reply)
	}
	if _, err := time.Parse(time.RFC3339Nano, reply.Entries[0].Time); err != nil {
		t.Fatalf("time = %q", reply.Entries[0].Time)
	}
	tail.mu.Lock()
	tail.cancel()
	tail.mu.Unlock()
}

func TestMihomoPanelLogsRouteValidatesLevel(t *testing.T) {
	if _, ok := matchMihomoPanelRoute("logs", http.MethodGet); !ok {
		t.Fatal("logs route must allow GET")
	}
	tail := newMihomoLogTail(http.DefaultClient)
	recorder := httptest.NewRecorder()
	tail.serve(recorder, httptest.NewRequest(http.MethodGet, "/api/mihomo/logs?level=trace", nil), &mihomoController{})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", recorder.Code)
	}
}
