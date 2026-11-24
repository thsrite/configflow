package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func oxSourceTestRecord(id int, tag string) map[string]any {
	return map[string]any{"id": id, "created_at_ms": int64(1792100000123), "request_id": id,
		"client_ip": "192.0.2.1", "questions_json": []any{}, "answers_json": []any{},
		"elapsed_ms": 1, "has_response": true, "error": nil, "rcode": "No Error",
		"steps": []oxSourceStep{{Sequence: "main", Kind: "executor", Tag: tag, Outcome: "entered"},
			{Sequence: "main", Kind: "executor", Tag: tag, Outcome: "next"}}}
}

func oxSourceTestJSON(value any) []byte { data, _ := json.Marshal(value); return data }

func TestOxSourceSSEListReusesOneStreamForFiftyRows(t *testing.T) {
	var streams, details, lists atomic.Int32
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "monitor-user" || password != "core-secret" || r.Header.Get(oxSourceHeader) != "" || r.Header.Get("Cookie") != "" {
			t.Error("credentials/headers were not isolated")
		}
		switch r.URL.Path {
		case "/api/health":
			fmt.Fprint(w, oxMonitorTestHealth)
		case "/api/plugins/records/stream":
			streams.Add(1)
			if r.Header.Get("Accept") != "text/event-stream" || r.URL.Query().Get("tail") != "2000" {
				t.Error("wrong stream request")
			}
			w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
			for i := 1; i <= 50; i++ {
				fmt.Fprintf(w, "event: record\ndata: %s\n\n", oxSourceTestJSON(oxSourceTestRecord(i, "forward_remote")))
			}
			fmt.Fprint(w, ": heartbeat\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		case "/api/plugins/records/records":
			lists.Add(1)
			rows := []any{}
			for i := 1; i <= 50; i++ {
				row := oxSourceTestRecord(i, "")
				delete(row, "steps")
				rows = append(rows, row)
			}
			w.Write(oxSourceTestJSON(map[string]any{"ok": true, "records": rows, "next_cursor": nil}))
		default:
			details.Add(1)
			http.NotFound(w, r)
		}
	}))
	defer core.Close()
	cache := newOxSourceCache()
	defer cache.close()
	cfg := oxMonitorTestConfig(t, core.URL, "- {tag: records, type: query_recorder}\n")
	handler := oxidnsMonitorAuth(cfg, oxMonitorHandlerWithSources(cfg, cache))
	checkList := func() {
		r := httptest.NewRequest("GET", "/api/oxidns/queries?recorder=records", nil)
		r.Header.Set("Authorization", "Bearer agent-secret")
		r.Header.Set(oxSourceHeader, "1")
		w := httptest.NewRecorder()
		handler(w, r)
		var response struct {
			Records []map[string]any `json:"records"`
		}
		if json.Unmarshal(w.Body.Bytes(), &response) != nil || w.Code != 200 || len(response.Records) != 50 {
			t.Fatalf("bad list: %s", w.Body.String())
		}
		for _, row := range response.Records {
			if row["resolution_source"] != "remote" || row["steps"] != nil {
				t.Fatalf("expected compact cached source, got %v", row)
			}
		}
	}
	checkList()
	var viewers sync.WaitGroup
	for n := 0; n < 4; n++ {
		viewers.Add(1)
		go func() { defer viewers.Done(); checkList() }()
	}
	viewers.Wait()
	if streams.Load() != 1 || details.Load() != 0 || lists.Load() != 5 {
		t.Fatalf("stream=%d detail=%d list=%d", streams.Load(), details.Load(), lists.Load())
	}
}

func TestOxSourceUnavailableStreamPreservesList(t *testing.T) {
	for _, status := range []int{404, 401, 302} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/health":
					fmt.Fprint(w, oxMonitorTestHealth)
				case "/api/plugins/records/stream":
					w.WriteHeader(status)
				case "/api/plugins/records/records":
					fmt.Fprint(w, oxMonitorTestRecords)
				default:
					t.Errorf("unexpected %s", r.URL.Path)
				}
			}))
			defer core.Close()
			cache := newOxSourceCache()
			defer cache.close()
			cfg := oxMonitorTestConfig(t, core.URL, "- {tag: records, type: query_recorder}\n")
			r := httptest.NewRequest("GET", "/api/oxidns/queries", nil)
			r.Header.Set(oxSourceHeader, "1")
			w := httptest.NewRecorder()
			oxMonitorHandlerWithSources(cfg, cache)(w, r)
			if w.Code != 200 || w.Body.String() != oxMonitorTestRecords {
				t.Fatalf("optional stream broke list: %s", w.Body.String())
			}
		})
	}
}

func TestOxSourceContinuousStreamBeyondTailLimit(t *testing.T) {
	var streams atomic.Int32
	const total = oxSourceEntries + 500
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			fmt.Fprint(w, oxMonitorTestHealth)
			return
		}
		streams.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 1; i <= total; i++ {
			fmt.Fprintf(w, "event: record\ndata: %s\n\n", oxSourceTestJSON(oxSourceTestRecord(i, "forward_remote")))
		}
		fmt.Fprint(w, ": heartbeat\n\n")
		w.(http.Flusher).Flush()
		heartbeat := time.NewTicker(20 * time.Millisecond)
		defer heartbeat.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-heartbeat.C:
				fmt.Fprint(w, ": heartbeat\n\n")
				w.(http.Flusher).Flush()
			}
		}
	}))
	defer core.Close()
	cache := newOxSourceCache()
	defer cache.close()
	cache.silenceTimeout = 250 * time.Millisecond
	s := cache.subscribe(&oxMonitorConfig{endpoint: core.URL}, "records", "test-instance")
	select {
	case <-s.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not deliver its records")
	}
	cache.mu.Lock()
	count := len(s.rows)
	cache.mu.Unlock()
	if count != oxSourceEntries {
		t.Fatalf("expected bounded cache, got %d entries", count)
	}
	old, newest := oxSourceTestRecord(1, ""), oxSourceTestRecord(total, "")
	delete(old, "steps")
	delete(newest, "steps")
	list := oxSourceTestJSON(map[string]any{"records": []any{old, newest}})
	var response struct {
		Records []map[string]any `json:"records"`
	}
	if json.Unmarshal(s.enrich(context.Background(), list), &response) != nil || len(response.Records) != 2 {
		t.Fatal("cache eviction removed history from the list")
	}
	if response.Records[0]["resolution_source"] != nil || response.Records[1]["resolution_source"] != "remote" {
		t.Fatalf("old row must remain available for detail fallback, newest must have a summary: %v", response.Records)
	}
	// Several silence windows pass while heartbeats keep arriving. Healthy
	// connections must not close or replay their initial tail on a fixed timer.
	select {
	case <-s.ctx.Done():
		t.Fatal("healthy subscription was stopped")
	case <-time.After(3 * cache.silenceTimeout):
	}
	if streams.Load() != 1 {
		t.Fatalf("healthy stream reconnected %d times", streams.Load()-1)
	}
}

func TestOxSourceIdleStopsLiveConnectionAfterLastViewer(t *testing.T) {
	closed := make(chan struct{})
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			fmt.Fprint(w, oxMonitorTestHealth)
			return
		}
		defer close(closed)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, ": heartbeat\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer core.Close()
	cache := newOxSourceCache()
	defer cache.close()
	cache.idleTimeout = 200 * time.Millisecond
	cache.silenceTimeout = 5 * time.Second
	config := &oxMonitorConfig{endpoint: core.URL}
	s := cache.subscribe(config, "records", "test-instance")
	select {
	case <-s.ready:
	case <-time.After(2 * time.Second):
		t.Fatal("stream never became ready")
	}
	// Renew from the list while the upstream is quiet, for longer than one idle
	// interval. Viewer activity, not incoming records, controls the lifetime.
	renew := time.NewTicker(20 * time.Millisecond)
	defer renew.Stop()
	viewing := time.NewTimer(3 * cache.idleTimeout)
	defer viewing.Stop()
view:
	for {
		select {
		case <-closed:
			t.Fatal("subscription closed while a viewer was active")
		case <-renew.C:
			if cache.subscribe(config, "records", "test-instance") != s {
				t.Fatal("active viewer lost its shared stream")
			}
		case <-viewing.C:
			break view
		}
	}
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("idle stream stayed connected waiting for an upstream event")
	}
	cache.mu.Lock()
	remaining := len(cache.streams)
	cache.mu.Unlock()
	if remaining != 0 {
		t.Fatal("idle stream retained its cache")
	}
}

func TestOxSourceSilentConnectionIsCancelled(t *testing.T) {
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			fmt.Fprint(w, oxMonitorTestHealth)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer core.Close()
	cache := newOxSourceCache()
	defer cache.close()
	cache.silenceTimeout = 30 * time.Millisecond
	s := oxSourceTestStream(cache, "records")
	s.config = oxMonitorConfig{endpoint: core.URL}
	s.instance = "test-instance"
	done := make(chan error, 1)
	go func() { done <- s.connect() }()
	select {
	case err := <-done:
		if err == nil || s.ctx.Err() != nil {
			t.Fatal("silence must cancel this connection, leaving the subscription able to reconnect")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("silent connection did not time out")
	}
}

func TestOxSourceSSEFramingAndBounds(t *testing.T) {
	var records []string
	ready := 0
	input := ": heartbeat\r\n\r\nevent: record\r\ndata: {\"id\":1,\r\ndata: \"steps\":[]}\r\n\r\nevent: other\ndata: ignored\n\nevent: record\ndata: \n\nevent: record\ndata: incomplete"
	oxReadSourceEvents(strings.NewReader(input), func(data []byte) { records = append(records, string(data)) }, func() { ready++ })
	if len(records) != 1 || records[0] != "{\"id\":1,\n\"steps\":[]}" || ready != 1 {
		t.Fatalf("bad framing: %v %d", records, ready)
	}
	if err := oxReadSourceEvents(strings.NewReader("data: "+strings.Repeat("x", oxSourceEventLimit)), func([]byte) { t.Fatal("accepted oversized event") }, func() {}); err == nil {
		t.Fatal("missing event bound")
	}
}

func oxSourceTestStream(cache *oxSourceCache, tag string) *oxSourceStream {
	ctx, cancel := context.WithCancel(context.Background())
	s := &oxSourceStream{owner: cache, tag: tag, ctx: ctx, cancel: cancel, ready: make(chan struct{}), lastUsed: time.Now(), rows: make(map[[32]byte]string)}
	s.initialDone()
	cache.streams[tag] = s
	return s
}

func TestOxSourceCacheIdentityBoundAndInvalidation(t *testing.T) {
	cache := newOxSourceCache()
	defer cache.close()
	a := oxSourceTestStream(cache, "one")
	b := oxSourceTestStream(cache, "two")
	for i := 1; i <= oxSourceEntries+1; i++ {
		a.put(oxSourceTestJSON(oxSourceTestRecord(i, "forward_local")))
	}
	if len(a.rows) != oxSourceEntries || len(b.rows) != 0 {
		t.Fatal("unbounded or cross-recorder cache")
	}
	row := oxSourceTestRecord(oxSourceEntries+1, "")
	delete(row, "steps")
	list := func() []byte {
		return oxSourceTestJSON(map[string]any{"ok": true, "records": []any{row}, "next_cursor": nil})
	}
	if !strings.Contains(string(a.enrich(context.Background(), list())), `"resolution_source":"local"`) {
		t.Fatal("missing cache hit")
	}
	row["created_at_ms"] = int64(1792100000124)
	if strings.Contains(string(a.enrich(context.Background(), list())), "resolution_source") {
		t.Fatal("reused ID inherited stale source")
	}
	row["created_at_ms"] = int64(1792100000123)
	row["client_ip"] = "192.0.2.99"
	if strings.Contains(string(a.enrich(context.Background(), list())), "resolution_source") {
		t.Fatal("different query inherited source")
	}
	cache.reset()
	if a.ctx.Err() == nil || b.ctx.Err() == nil || len(cache.streams) != 0 {
		t.Fatal("reset retained old stream")
	}
	a.put(oxSourceTestJSON(oxSourceTestRecord(1, "forward_remote")))
	if strings.Contains(string(a.enrich(context.Background(), list())), "resolution_source") {
		t.Fatal("late stream result reused after reset")
	}
}

func TestOxSourceCacheCancelsOnRevisionAndInstanceChanges(t *testing.T) {
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, oxMonitorTestHealth) }))
	defer core.Close()
	cache := newOxSourceCache()
	defer cache.close()
	config := &oxMonitorConfig{endpoint: core.URL, sourceRevision: "one"}
	first := cache.subscribe(config, "recorder", "test-instance")
	if cache.subscribe(config, "recorder", "test-instance") != first {
		t.Fatal("duplicate stream per viewer")
	}
	config.sourceRevision = "two"
	second := cache.subscribe(config, "recorder", "test-instance")
	if first.ctx.Err() == nil || second == first {
		t.Fatal("config revision did not invalidate")
	}
	third := cache.subscribe(config, "recorder", "new-instance")
	if second.ctx.Err() == nil || third == second {
		t.Fatal("core instance did not invalidate")
	}
	for i := 0; i < 6; i++ {
		cache.subscribe(config, fmt.Sprint("recorder", i), "new-instance")
	}
	cache.mu.Lock()
	count := len(cache.streams)
	cache.mu.Unlock()
	if count > oxSourceStreams {
		t.Fatal("unbounded subscriptions")
	}
}

func TestOxSourceCacheIdleAndMalformedRecords(t *testing.T) {
	cache := newOxSourceCache()
	defer cache.close()
	s := oxSourceTestStream(cache, "recorder")
	for _, raw := range []string{`{`, `{"id":1}`, `{"id":-1,"created_at_ms":1,"client_ip":"x","questions_json":[],"steps":[]}`} {
		s.put([]byte(raw))
	}
	if len(s.rows) != 0 {
		t.Fatal("malformed records populated cache")
	}
	s.lastUsed = time.Now().Add(-2 * oxSourceIdle)
	s.run()
	if s.ctx.Err() == nil || len(cache.streams) != 0 {
		t.Fatal("idle subscriber was retained")
	}
}

func TestOxSourceCacheIPv4SuppressionSummary(t *testing.T) {
	cache := newOxSourceCache()
	defer cache.close()
	stream := oxSourceTestStream(cache, "queries")
	stream.put([]byte(oxIPv4PolicyUserTrace))
	var row map[string]json.RawMessage
	if err := json.Unmarshal([]byte(oxIPv4PolicyUserTrace), &row); err != nil {
		t.Fatal(err)
	}
	delete(row, "steps")
	list := oxSourceTestJSON(map[string]any{"ok": true, "records": []any{row}, "next_cursor": nil})
	var response struct {
		Records []map[string]any `json:"records"`
	}
	if err := json.Unmarshal(stream.enrich(context.Background(), list), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Records) != 1 || response.Records[0]["resolution_source"] != "ipv4_preferred" || response.Records[0]["steps"] != nil {
		t.Fatalf("expected compact IPv4 policy summary, got %v", response.Records)
	}
}

func (cache *oxSourceCache) close() {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.closed = true
	cache.clearLocked()
}
