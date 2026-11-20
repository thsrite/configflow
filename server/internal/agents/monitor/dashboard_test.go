package monitor

// Port of backend/test_oxidns_dashboard.py: durable dashboard boundaries
// never clear core DNS records or caches. The Python tests replace
// _request; here requestNative. The reset route test is in
// internal/api/agents/oxidns_test.go.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"configflow/internal/model"
)

// freshRankings is the autouse fixture: every poll asks for rankings again.
func freshRankings(t *testing.T) {
	reuse, backoff, clock := RankingReuseSeconds, RankingBackoffSeconds, monotonic
	RankingReuseSeconds, RankingBackoffSeconds = 0, 0
	ResetRankingsCache()
	t.Cleanup(func() {
		RankingReuseSeconds, RankingBackoffSeconds, monotonic = reuse, backoff, clock
		ResetRankingsCache()
	})
}

func dashboardManager(t *testing.T, dir string, ids ...string) *fakeManager {
	if len(ids) == 0 {
		ids = []string{"one"}
	}
	m := &fakeManager{agents: map[string]*model.Agent{}, dataDir: dir}
	for _, id := range ids {
		a := baseAgent()
		setAgentField(a, "id", id)
		setAgentField(a, "version", "1.8.14-go")
		m.agents[id] = a
	}
	return m
}

func upstreamMetrics(count, elapsed int) string {
	return strings.Join([]string{
		fmt.Sprintf(`forward_upstream_query_total{plugin_tag="remote",upstream="cloud"} %d`, count),
		`forward_upstream_error_total{plugin_tag="remote",upstream="cloud"} 0`,
		fmt.Sprintf(`forward_upstream_latency_count{plugin_tag="remote",upstream="cloud"} %d`, count),
		fmt.Sprintf(`forward_upstream_latency_sum_ms{plugin_tag="remote",upstream="cloud"} %d`, elapsed),
	}, "\n")
}

func snapshotData(now int64, count, elapsed int) map[string]any {
	return map[string]any{
		"success": true,
		"health": map[string]any{
			"status": "ok", "version": "1.6.1", "uptime_ms": 500, "instance_id": "core-one",
		},
		"capabilities":     map[string]any{"query_recorders": []any{"records"}},
		"collected_at_ms":  now,
		"source_revision":  strings.Repeat("a", 64),
		"runtime_revision": strings.Repeat("d", 64),
		"upstream_labels": []any{map[string]any{
			"plugin_tag": "remote", "upstream": "cloud", "address": "https://dns.test/dns-query",
		}},
		"metrics": strings.Join([]string{
			serverText("dns", "udp", count, 0, elapsed, count, 2),
			cacheText("cache", count, count/2, 0, 0, 0, 30),
			upstreamMetrics(count, elapsed),
		}, "\n"),
	}
}

func defaultSnapshot() map[string]any { return snapshotData(1000, 10, 100) }

func rankingData(now int64) map[string]any {
	section := func() map[string]any { return map[string]any{"sample_size": 0, "rows": []any{}, "at_limit": false} }
	blocked := map[string]any{"available": true, "matcher_tag": "adguard_match"}
	updateMap(blocked, section())
	return map[string]any{
		"success": true, "recorder": "records", "collected_at_ms": now,
		"source_revision": strings.Repeat("a", 64), "runtime_revision": strings.Repeat("d", 64),
		"instance_id": "core-one", "retention_days": 7, "sample_limit": 10000,
		"clients": section(), "domains": section(), "blocked": blocked,
	}
}

type dashCall struct {
	agent, endpoint string
	params          query
}

func fieldsFromValue(value any) (model.Fields, error) {
	var fields model.Fields
	raw, err := json.Marshal(value)
	if err != nil {
		return fields, err
	}
	raw, _ = jsonresp.NormalizeNonFinite(raw)
	if err := jsondecode.Unmarshal(raw, &fields); err != nil {
		return fields, NewError("INVALID_RESPONSE", 0, "")
	}
	return fields, nil
}

// fakeTransport replaces requestNative; current/ranks are answered as deep
// copies (or returned as errors), like the Python transport fixture.
type fakeTransport struct {
	mu      sync.Mutex
	current any
	ranks   any
	hook    func(agent *model.Agent, endpoint string, params query)
	calls   []dashCall
}

func (f *fakeTransport) do(callCtx context.Context, _ Registry, agent *model.Agent, endpoint string, params query, _ string) (model.Fields, error) {
	var fields model.Fields
	if err := callCtx.Err(); err != nil {
		return fields, err
	}
	f.mu.Lock()
	f.calls = append(f.calls, dashCall{agent.ID, endpoint, append(query{}, params...)})
	hook, current, ranks := f.hook, f.current, f.ranks
	f.mu.Unlock()
	if hook != nil {
		hook(agent, endpoint, params)
	}
	result := ranks
	if endpoint == "overview" {
		result = current
	}
	if err, ok := result.(error); ok {
		return fields, err
	}
	if err := callCtx.Err(); err != nil {
		return fields, err
	}
	return fieldsFromValue(result)
}

func (f *fakeTransport) cur() map[string]any { return f.current.(map[string]any) }
func (f *fakeTransport) rk() map[string]any  { return f.ranks.(map[string]any) }

func (f *fakeTransport) rankingCalls() []query {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []query
	for _, c := range f.calls {
		if c.endpoint == "rankings" {
			out = append(out, c.params)
		}
	}
	return out
}

func (f *fakeTransport) endpoints() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		out = append(out, c.endpoint)
	}
	return out
}

func transport(t *testing.T, current, ranks any) *fakeTransport {
	if current == nil {
		current = defaultSnapshot()
	}
	if ranks == nil {
		ranks = rankingData(1000)
	}
	f := &fakeTransport{current: current, ranks: ranks}
	previous := requestNative
	requestNative = f.do
	t.Cleanup(func() { requestNative = previous })
	return f
}

func overview(t *testing.T, m Registry, id string, a ...string) map[string]any {
	t.Helper()
	result, err := ReadOverview(ctx, m, id, args(a...))
	if err != nil {
		t.Fatal(err)
	}
	return toJSONMap(t, result)
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func paramsEqual(t *testing.T, got query, want ...string) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(query(args(want...))) {
		t.Fatalf("params %v want %v", got, want)
	}
}

func TestInitialBoundaryIsDurableDoesNotRewriteOnPollAndPreservesGauges(t *testing.T) {
	freshRankings(t)
	dir := t.TempDir()
	m := dashboardManager(t, dir)
	f := transport(t, nil, nil)
	initial := overview(t, m, "one")
	stats := asMap(initial["statistics"])
	if stats["reason"] != "initial" || asInt64(stats["started_at_ms"]) != 1000 {
		t.Fatal(stats)
	}
	wantJSON(t, pick(metricsOf(initial), "requests_total", "avg_latency_ms", "inflight"),
		map[string]any{"requests_total": 0, "avg_latency_ms": nil, "inflight": 2})
	wantJSON(t, row(initial, "caches", 0)["entries"], 30)
	store := NewStateStore(dir)
	saved := readFile(t, store.Path("one"))
	info, _ := os.Stat(store.Path("one"))
	before := info.ModTime()
	replaceMap(f.cur(), snapshotData(2000, 14, 180))
	f.rk()["collected_at_ms"] = 2000
	// A new manager simulates a ConfigFlow restart without keeping a memory cache.
	second := overview(t, dashboardManager(t, dir), "one")
	wantJSON(t, second["statistics"], initial["statistics"])
	wantJSON(t, pick(metricsOf(second), "requests_total", "avg_latency_ms"), map[string]any{"requests_total": 4, "avg_latency_ms": 20})
	wantJSON(t, pick(row(second, "upstreams", 0), "requests", "avg_latency_ms"), map[string]any{"requests": 4, "avg_latency_ms": 20})
	calls := f.rankingCalls()
	paramsEqual(t, calls[len(calls)-1], "since_ms", "1000", "until_ms", "2000")
	if !bytes.Equal(readFile(t, store.Path("one")), saved) {
		t.Fatal("state rewritten")
	}
	info, _ = os.Stat(store.Path("one"))
	if !info.ModTime().Equal(before) {
		t.Fatal("mtime changed")
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "dns-monitor", "*.bak")); len(matches) != 0 {
		t.Fatal(matches)
	}
	for _, e := range f.endpoints() {
		if e != "overview" && e != "rankings" {
			t.Fatal(e)
		}
	}
}

func TestManualResetChangesGenerationAndOnlyTheSelectedAgent(t *testing.T) {
	freshRankings(t)
	dir := t.TempDir()
	m := dashboardManager(t, dir, "one", "two")
	f := transport(t, nil, nil)
	first := overview(t, m, "one")
	other := overview(t, m, "two")
	store := NewStateStore(dir)
	otherSaved := readFile(t, store.Path("two"))
	replaceMap(f.cur(), snapshotData(2000, 20, 100))
	f.rk()["collected_at_ms"] = 2000
	changedRaw, err := ResetStatistics(ctx, m, "one", []byte(`{"recorder":"records"}`))
	if err != nil {
		t.Fatal(err)
	}
	changed := toJSONMap(t, changedRaw)
	stats := asMap(changed["statistics"])
	if stats["generation"] == asMap(first["statistics"])["generation"] ||
		stats["reason"] != "manual" || asInt64(stats["started_at_ms"]) != 2000 {
		t.Fatal(stats)
	}
	wantJSON(t, pick(metricsOf(changed), "requests_total", "inflight"), map[string]any{"requests_total": 0, "inflight": 2})
	wantJSON(t, row(changed, "caches", 0)["entries"], 30)
	if !bytes.Equal(readFile(t, store.Path("two")), otherSaved) {
		t.Fatal("other agent changed")
	}
	wantJSON(t, overview(t, m, "two")["statistics"], other["statistics"])
}

func TestIdentityChangesOpenANewWindow(t *testing.T) {
	for _, tc := range [][2]string{{"instance_id", "core_changed"}, {"source_revision", "configuration_changed"}, {"host", "target_changed"}} {
		t.Run(tc[0], func(t *testing.T) {
			freshRankings(t)
			m := dashboardManager(t, t.TempDir())
			f := transport(t, nil, nil)
			previous := overview(t, m, "one")
			replaceMap(f.cur(), snapshotData(2000, 40, 100))
			f.rk()["collected_at_ms"] = 2000
			switch tc[0] {
			case "instance_id":
				asMap(f.cur()["health"])["instance_id"] = "core-two"
				f.rk()["instance_id"] = "core-two"
			case "source_revision":
				f.cur()["source_revision"] = strings.Repeat("b", 64)
				f.rk()["source_revision"] = strings.Repeat("b", 64)
			default:
				setAgentField(m.agent("one"), "host", "192.0.2.10")
			}
			result := overview(t, m, "one")
			stats := asMap(result["statistics"])
			if stats["reason"] != tc[1] || stats["generation"] == asMap(previous["statistics"])["generation"] ||
				asInt64(stats["started_at_ms"]) != 2000 {
				t.Fatal(stats)
			}
			wantJSON(t, metricsOf(result)["requests_total"], 0)
		})
	}
}

func TestFailedManualResetPreservesPreviousBytes(t *testing.T) {
	for _, stage := range []string{"overview", "rankings", "target", "mismatch"} {
		t.Run(stage, func(t *testing.T) {
			freshRankings(t)
			dir := t.TempDir()
			m := dashboardManager(t, dir)
			f := transport(t, nil, nil)
			overview(t, m, "one")
			store := NewStateStore(dir)
			saved := readFile(t, store.Path("one"))
			replaceMap(f.cur(), snapshotData(2000, 20, 100))
			f.rk()["collected_at_ms"] = 2000
			switch stage {
			case "overview":
				f.current = NewError("AGENT_UNREACHABLE", 0, "")
			case "rankings":
				f.ranks = NewError("AGENT_UNREACHABLE", 0, "")
			case "target":
				f.hook = func(_ *model.Agent, endpoint string, _ query) {
					if endpoint == "rankings" {
						setAgentField(m.agent("one"), "host", "192.0.2.50")
					}
				}
			default:
				f.rk()["source_revision"] = strings.Repeat("c", 64)
			}
			_, err := ResetStatistics(ctx, m, "one", []byte(`{}`))
			var e *Error
			if !errors.As(err, &e) {
				t.Fatal(err)
			}
			if !bytes.Equal(readFile(t, store.Path("one")), saved) {
				t.Fatal("state changed")
			}
		})
	}
}

func TestOldAgentsKeepOriginalCountersWithoutStateAndCannotReset(t *testing.T) {
	freshRankings(t)
	dir := t.TempDir()
	m := dashboardManager(t, dir)
	setAgentField(m.agent("one"), "version", "1.8.13-go")
	f := transport(t, nil, nil)
	result := overview(t, m, "one")
	wantJSON(t, metricsOf(result)["requests_total"], 10)
	if result["statistics"] != nil || asMap(result["rankings"])["code"] != "upgrade_required" {
		t.Fatal(result)
	}
	if _, err := os.Stat(filepath.Join(dir, "dns-monitor")); !os.IsNotExist(err) {
		t.Fatal("state directory created")
	}
	if fmt.Sprint(f.endpoints()) != "[overview]" {
		t.Fatal(f.endpoints())
	}
	_, err := ResetStatistics(ctx, m, "one", []byte(`{}`))
	e := wantMonitorError(t, err, "UPGRADE_REQUIRED", 0)
	if !strings.Contains(e.Message, "1.8.14-go") || len(f.endpoints()) != 1 {
		t.Fatal(e)
	}
}

func TestUnavailableRankingsLeaveExistingMetricsUsable(t *testing.T) {
	freshRankings(t)
	m := dashboardManager(t, t.TempDir())
	f := transport(t, nil, nil)
	previous := overview(t, m, "one")
	replaceMap(f.cur(), snapshotData(2000, 20, 100))
	f.ranks = NewError("CORE_UNREACHABLE", 0, "")
	ResetRankingsCache()
	result := overview(t, m, "one")
	wantJSON(t, result["statistics"], previous["statistics"])
	wantJSON(t, metricsOf(result)["requests_total"], 10)
	rankings := asMap(result["rankings"])
	if rankings["available"] != false || rankings["success"] != false {
		t.Fatal(rankings)
	}
}

func TestGenerationCompareAndSetRejectsStaleSnapshot(t *testing.T) {
	freshRankings(t)
	dir := t.TempDir()
	m := dashboardManager(t, dir)
	f := transport(t, nil, nil)
	overview(t, m, "one")
	store := NewStateStore(dir)
	original, err := store.ReadState(ctx, "one")
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := DecodeState(mustMarshal(t, original))
	if err != nil {
		t.Fatal(err)
	}
	replacement.Generation = strings.Repeat("f", 32)
	replacement.Reason = "manual"
	replacement.StartedAtMS = 2000
	replaceMap(f.cur(), snapshotData(3000, 10, 100))
	f.rk()["collected_at_ms"] = 3000
	f.hook = func(_ *model.Agent, endpoint string, _ query) {
		if endpoint == "rankings" {
			if _, err := store.CompareState(ctx, "one", original.Generation, replacement); err != nil {
				t.Error(err)
			}
		}
	}
	_, err = ReadOverview(ctx, m, "one", nil)
	wantMonitorError(t, err, "DASHBOARD_CHANGED", 0)
	current, _ := store.ReadState(ctx, "one")
	wantJSON(t, current, replacement)
}

func TestDamagedStateDoesNotSilentlyRestoreOldCounts(t *testing.T) {
	for _, bad := range []string{"not json", "{}", "[]", `{"schema":1}`} {
		freshRankings(t)
		dir := t.TempDir()
		m := dashboardManager(t, dir)
		store := NewStateStore(dir)
		os.MkdirAll(store.Directory, 0o755)
		os.WriteFile(store.Path("one"), []byte(bad), 0o644)
		f := transport(t, nil, nil)
		_, err := ReadOverview(ctx, m, "one", nil)
		wantMonitorError(t, err, "DASHBOARD_STATE_ERROR", 500)
		if string(readFile(t, store.Path("one"))) != bad || len(f.endpoints()) != 0 {
			t.Fatal(bad)
		}
	}
}

func TestFailedAtomicReplaceKeepsPriorGenerationAndCleansTemp(t *testing.T) {
	freshRankings(t)
	dir := t.TempDir()
	m := dashboardManager(t, dir)
	f := transport(t, nil, nil)
	overview(t, m, "one")
	store := NewStateStore(dir)
	saved := readFile(t, store.Path("one"))
	replaceMap(f.cur(), snapshotData(2000, 10, 100))
	f.rk()["collected_at_ms"] = 2000
	previous := replaceFile
	replaceFile = func(string, string) error { return errors.New("disk unavailable") }
	t.Cleanup(func() { replaceFile = previous })
	_, err := ResetStatistics(ctx, m, "one", []byte(`{}`))
	wantMonitorError(t, err, "DASHBOARD_STATE_ERROR", 0)
	if !bytes.Equal(readFile(t, store.Path("one")), saved) {
		t.Fatal("state changed")
	}
	if matches, _ := filepath.Glob(filepath.Join(store.Directory, "*.tmp")); len(matches) != 0 {
		t.Fatal(matches)
	}
	if matches, _ := filepath.Glob(filepath.Join(store.Directory, ".*.tmp")); len(matches) != 0 {
		t.Fatal(matches)
	}
}

func TestStoreSafeFilenameAndParallelGenerationCheck(t *testing.T) {
	freshRankings(t)
	dir := t.TempDir()
	m := dashboardManager(t, dir)
	transport(t, nil, nil)
	overview(t, m, "one")
	store := NewStateStore(dir)
	state, _ := store.ReadState(ctx, "one")
	if filepath.Dir(store.Path("../../escape")) != filepath.Join(dir, "dns-monitor") ||
		filepath.Base(store.Path("../../escape")) == filepath.Base(store.Path(`..\..\escape`)) {
		t.Fatal(store.Path("../../escape"))
	}
	rawState := mustMarshal(t, state)
	var wins int32
	var wg sync.WaitGroup
	for index := 1; index <= 8; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			replacement, err := DecodeState(rawState)
			if err != nil {
				t.Error(err)
				return
			}
			replacement.Generation = fmt.Sprintf("%032x", index)
			replacement.Reason = "manual"
			if _, err := store.CompareState(ctx, "one", state.Generation, replacement); err == nil {
				atomic.AddInt32(&wins, 1)
			} else if !errors.Is(err, ErrStateConflict) {
				t.Error(err)
			}
		}(index)
	}
	wg.Wait()
	current, err := store.ReadState(ctx, "one")
	if err != nil || wins != 1 || current == nil {
		t.Fatal(wins, err, current)
	}
}

func TestDomainRankingsSupportMultipleQuestionsPerRecord(t *testing.T) {
	value := map[string]any{"sample_size": 1, "rows": []any{map[string]any{"key": "many.test", "count": 2}}, "at_limit": false}
	section, err := decodeRankingSection(mustMarshal(t, value), "domains", 10)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON(t, asMap(asList(toJSONMap(t, section)["rows"])[0])["share"], 2)
	bad := cloneMap(value)
	bad["rows"] = []any{map[string]any{"key": "192.0.2.1", "count": 2}}
	if _, err := decodeRankingSection(mustMarshal(t, bad), "clients", 10); err == nil {
		t.Fatal("accepted")
	}
}

func TestRankingsRecomputeShareAndPreserveSamplingLimit(t *testing.T) {
	freshRankings(t)
	m := dashboardManager(t, t.TempDir())
	f := transport(t, nil, nil)
	f.rk()["clients"] = map[string]any{
		"sample_size": 10000, "at_limit": true,
		"rows": []any{
			map[string]any{"key": "192.0.2.2", "count": 5, "share": "bad"},
			map[string]any{"key": "192.0.2.1", "count": 20},
		},
	}
	f.rk()["blocked"] = map[string]any{"available": false, "reason": "private password and configuration"}
	result := asMap(overview(t, m, "one")["rankings"])
	if result["success"] != true || asInt64(result["sample_limit"]) != 10000 || asMap(result["clients"])["at_limit"] != true {
		t.Fatal(result)
	}
	wantJSON(t, asList(asMap(result["clients"])["rows"])[0], map[string]any{"key": "192.0.2.1", "count": 20, "share": 0.002})
	blocked := asMap(result["blocked"])
	if blocked["available"] != false || len(asList(blocked["rows"])) != 0 {
		t.Fatal(blocked)
	}
	encoded := mustMarshal(t, result)
	if strings.Contains(string(encoded), "password") {
		t.Fatal(string(encoded))
	}
}

func TestUpstreamRawLabelsNeverLeakURLCredentials(t *testing.T) {
	freshRankings(t)
	m := dashboardManager(t, t.TempDir())
	f := transport(t, nil, nil)
	raw := "https://user:password@dns.test/dns-query?key=secret#fragment"
	f.cur()["metrics"] = strings.ReplaceAll(asStr(f.cur()["metrics"]), `upstream="cloud"`, `upstream="`+raw+`"`)
	label := asMap(asList(f.cur()["upstream_labels"])[0])
	label["upstream"] = raw
	label["address"] = raw
	result := overview(t, m, "one")
	r := row(result, "upstreams", 0)
	if r["upstream"] != "https://dns.test/dns-query" || r["address"] != "https://dns.test/dns-query" {
		t.Fatal(r)
	}
	encoded := mustMarshal(t, result)
	for _, secret := range []string{"password", "secret", "fragment"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal(secret)
		}
	}
}

func TestResetInputValidationBeforeNetwork(t *testing.T) {
	for _, body := range []string{`null`, `[]`, `{"url":"http://elsewhere"}`, `{"recorder":"../private"}`, `{"recorder":1}`} {
		freshRankings(t)
		f := transport(t, nil, nil)
		_, err := ResetStatistics(ctx, dashboardManager(t, t.TempDir()), "one", []byte(body))
		wantMonitorError(t, err, "INVALID_QUERY", 0)
		if len(f.endpoints()) != 0 {
			t.Fatal(body)
		}
	}
}

func TestCounterRestartOpensDurableWindowAndNeverReusesOldBaseline(t *testing.T) {
	freshRankings(t)
	dir := t.TempDir()
	m := dashboardManager(t, dir)
	f := transport(t, snapshotData(1000, 100, 1000), nil)
	previous := overview(t, m, "one")
	replaceMap(f.cur(), snapshotData(2000, 10, 100))
	f.rk()["collected_at_ms"] = 2000
	restarted := overview(t, m, "one")
	stats := asMap(restarted["statistics"])
	if stats["reason"] != "counters_changed" || stats["generation"] == asMap(previous["statistics"])["generation"] {
		t.Fatal(stats)
	}
	wantJSON(t, metricsOf(restarted)["requests_total"], 0)
	replaceMap(f.cur(), snapshotData(3000, 110, 1100))
	f.rk()["collected_at_ms"] = 3000
	continued := overview(t, dashboardManager(t, dir), "one")
	wantJSON(t, continued["statistics"], restarted["statistics"])
	wantJSON(t, pick(metricsOf(continued), "requests_total", "avg_latency_ms"), map[string]any{"requests_total": 100, "avg_latency_ms": 10})
}

func TestUnknownRetentionIsPreservedAndOverlongTopListIsRejected(t *testing.T) {
	freshRankings(t)
	m := dashboardManager(t, t.TempDir())
	f := transport(t, nil, nil)
	f.rk()["retention_days"] = nil
	if asMap(overview(t, m, "one")["rankings"])["retention_days"] != nil {
		t.Fatal("retention")
	}
	rows := []any{}
	for i := 0; i < 11; i++ {
		rows = append(rows, map[string]any{"key": strconv.Itoa(i) + ".test", "count": 1})
	}
	f.rk()["domains"] = map[string]any{"sample_size": 20, "at_limit": false, "rows": rows}
	_, err := ResetStatistics(ctx, m, "one", []byte(`{}`))
	wantMonitorError(t, err, "INVALID_RESPONSE", 0)
}

func TestUpstreamHashSuffixKeepsDistinctStableIDs(t *testing.T) {
	rows := []any{map[string]any{"tag": "forward", "upstream": "dns#1"}, map[string]any{"tag": "forward", "upstream": "dns#2"}}
	result, _ := publicUpstreams(rows, nil)
	again, _ := publicUpstreams(rows, nil)
	a, b := asMap(result[0]), asMap(result[1])
	if a["upstream"] != "dns#1" || b["upstream"] != "dns#2" || a["id"] == b["id"] {
		t.Fatal(result)
	}
	wantJSON(t, result, again)
}

func TestMissingRecorderStillAllowsResetOfMetricsWithoutCoreMutation(t *testing.T) {
	freshRankings(t)
	m := dashboardManager(t, t.TempDir())
	f := transport(t, nil, nil)
	asMap(f.cur()["capabilities"])["query_recorders"] = []any{}
	raw, err := ResetStatistics(ctx, m, "one", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	result := toJSONMap(t, raw)
	wantJSON(t, metricsOf(result)["requests_total"], 0)
	if asMap(result["rankings"])["code"] != "recorder_not_configured" || fmt.Sprint(f.endpoints()) != "[overview]" {
		t.Fatal(result["rankings"], f.endpoints())
	}
}

func TestInitialSnapshotRequiresValidClockAndKeepsStateAbsentOnError(t *testing.T) {
	freshRankings(t)
	dir := t.TempDir()
	m := dashboardManager(t, dir)
	f := transport(t, nil, nil)
	f.cur()["collected_at_ms"] = int64(1)<<53 - 1
	_, err := ReadOverview(ctx, m, "one", nil)
	wantMonitorError(t, err, "INVALID_RESPONSE", 0)
	if _, err := os.Stat(NewStateStore(dir).Path("one")); !os.IsNotExist(err) {
		t.Fatal("state written")
	}
}

func TestRemovedSelectedRecorderDoesNotTrapOverviewInErrors(t *testing.T) {
	freshRankings(t)
	dir := t.TempDir()
	m := dashboardManager(t, dir)
	f := transport(t, nil, nil)
	overview(t, m, "one", "recorder", "records")
	asMap(f.cur()["capabilities"])["query_recorders"] = []any{"replacement"}
	f.cur()["source_revision"] = strings.Repeat("b", 64)
	f.rk()["source_revision"] = strings.Repeat("b", 64)
	f.cur()["collected_at_ms"] = 2000
	f.rk()["collected_at_ms"] = 2000
	f.rk()["recorder"] = "replacement"
	result := overview(t, m, "one", "recorder", "records")
	wantJSON(t, result["recorders"], []any{"replacement"})
	calls := f.rankingCalls()
	if asMap(result["rankings"])["recorder"] != "replacement" || func() bool { _, ok := calls[len(calls)-1].get("recorder"); return ok }() {
		t.Fatal(result["rankings"], calls)
	}
	if asMap(result["statistics"])["reason"] != "configuration_changed" {
		t.Fatal(result["statistics"])
	}
	store := NewStateStore(dir)
	saved, _ := store.ReadState(ctx, "one")
	_, err := ResetStatistics(ctx, m, "one", []byte(`{"recorder":"records"}`))
	wantMonitorError(t, err, "INVALID_QUERY", 0)
	current, _ := store.ReadState(ctx, "one")
	wantJSON(t, current, saved)
}

func TestSameFileRuntimeReloadStartsNewWindowEvenWhenCountersCaughtUp(t *testing.T) {
	freshRankings(t)
	m := dashboardManager(t, t.TempDir())
	f := transport(t, nil, nil)
	previous := overview(t, m, "one")
	replaceMap(f.cur(), snapshotData(2000, 100, 100))
	f.cur()["runtime_revision"] = strings.Repeat("e", 64)
	f.rk()["runtime_revision"] = strings.Repeat("e", 64)
	f.rk()["collected_at_ms"] = 2000
	result := overview(t, m, "one")
	stats := asMap(result["statistics"])
	if stats["reason"] != "core_changed" || stats["generation"] == asMap(previous["statistics"])["generation"] {
		t.Fatal(stats)
	}
	wantJSON(t, metricsOf(result)["requests_total"], 0)
}

func TestRuntimeGenerationChangeBetweenOverviewAndRankingsBlocksManualCommit(t *testing.T) {
	freshRankings(t)
	dir := t.TempDir()
	m := dashboardManager(t, dir)
	f := transport(t, nil, nil)
	overview(t, m, "one")
	store := NewStateStore(dir)
	saved := readFile(t, store.Path("one"))
	f.rk()["runtime_revision"] = strings.Repeat("e", 64)
	_, err := ResetStatistics(ctx, m, "one", []byte(`{}`))
	wantMonitorError(t, err, "SNAPSHOT_CHANGED", 0)
	if !bytes.Equal(readFile(t, store.Path("one")), saved) {
		t.Fatal("state changed")
	}
}

func TestIndependentStoresSerializeResets(t *testing.T) {
	freshRankings(t)
	dir := t.TempDir()
	m := dashboardManager(t, dir)
	transport(t, nil, nil)
	overview(t, m, "one")
	store := NewStateStore(dir)
	state, err := store.ReadState(ctx, "one")
	if err != nil {
		t.Fatal(err)
	}
	rawState := mustMarshal(t, state)
	var wins atomic.Int32
	var wg sync.WaitGroup
	ready := make(chan struct{})
	for index := 1; index <= 4; index++ {
		wg.Go(func() {
			<-ready
			replacement, err := DecodeState(rawState)
			if err != nil {
				t.Error(err)
				return
			}
			replacement.Generation = fmt.Sprintf("%032x", index)
			replacement.Reason = "manual"
			_, err = NewStateStore(dir).CompareState(ctx, "one", state.Generation, replacement)
			if err == nil {
				wins.Add(1)
			} else if !errors.Is(err, ErrStateConflict) {
				t.Error(err)
			}
		})
	}
	close(ready)
	wg.Wait()
	current, err := store.ReadState(ctx, "one")
	if err != nil || wins.Load() != 1 || current == nil {
		t.Fatal(wins.Load(), err, current)
	}
}

func TestMissingRetentionMetadataIsUnknown(t *testing.T) {
	freshRankings(t)
	f := transport(t, nil, nil)
	delete(f.rk(), "retention_days")
	result := asMap(overview(t, dashboardManager(t, t.TempDir()), "one")["rankings"])
	if result["success"] != true || result["retention_days"] != nil {
		t.Fatal(result)
	}
}

func TestBlockedRankingReasonFollowsAgent(t *testing.T) {
	cases := []struct {
		blocked      map[string]any
		code, reason string
	}{
		{map[string]any{}, "standard_adguard", "标准 AdGuard 拦截路径"},
		{map[string]any{"reason": "standard_adguard"}, "standard_adguard", "标准 AdGuard 拦截路径"},
		{map[string]any{"reason": "rule_dns", "matcher_tags": []any{"qs.match.sequence_main.3.0.qname", "qs.match.sequence_main.4.0.qname"}},
			"rule_dns", "规则拦截（拒绝/黑洞）"},
		{map[string]any{"reason": "standard_adguard_rule_dns", "matcher_tags": []any{"qs.match.sequence_main.2.0.question"}},
			"standard_adguard_rule_dns", "标准 AdGuard 与规则拦截"},
	}
	for _, tc := range cases {
		freshRankings(t)
		m := dashboardManager(t, t.TempDir())
		f := transport(t, nil, nil)
		updateMap(asMap(f.rk()["blocked"]), tc.blocked)
		result := asMap(asMap(overview(t, m, "one")["rankings"])["blocked"])
		if result["available"] != true || result["code"] != tc.code || result["reason"] != tc.reason {
			t.Fatal(result)
		}
		wantJSON(t, result["matcher_tags"], tc.blocked["matcher_tags"])
	}
}

func TestBlockedRankingRejectsMalformedReasonOrMatchers(t *testing.T) {
	many := []any{}
	for i := 0; i < 33; i++ {
		many = append(many, fmt.Sprintf("m%d", i))
	}
	for _, blocked := range []map[string]any{
		{"reason": "Rule DNS"},
		{"reason": "rule_dns", "matcher_tags": []any{"../x"}},
		{"reason": "rule_dns", "matcher_tags": []any{}},
		{"reason": "rule_dns", "matcher_tags": many},
	} {
		freshRankings(t)
		m := dashboardManager(t, t.TempDir())
		f := transport(t, nil, nil)
		updateMap(asMap(f.rk()["blocked"]), blocked)
		if asMap(overview(t, m, "one")["rankings"])["available"] != false {
			t.Fatal(blocked)
		}
	}
}

func TestTooManyBlockRulesReasonIsActionable(t *testing.T) {
	freshRankings(t)
	m := dashboardManager(t, t.TempDir())
	f := transport(t, nil, nil)
	f.rk()["blocked"] = map[string]any{"available": false, "reason": "too_many_block_rules", "sample_size": 0, "rows": []any{}, "at_limit": false}
	result := asMap(asMap(overview(t, m, "one")["rankings"])["blocked"])
	if result["available"] != false || result["code"] != "too_many_block_rules" ||
		!strings.HasPrefix(asStr(result["reason"]), "拦截规则分组超过 32 个") || strings.Contains(asStr(result["reason"]), "刷新") {
		t.Fatal(result)
	}
	// The agent's sentence fallback is not a code; the generic text covers rule blocks too.
	f.rk()["blocked"] = map[string]any{"available": false, "reason": "当前配置未包含可确认的标准 AdGuard 拦截路径"}
	result = asMap(asMap(overview(t, m, "one")["rankings"])["blocked"])
	if result["code"] != "unsupported_configuration" || !strings.Contains(asStr(result["reason"]), "拒绝/黑洞") {
		t.Fatal(result)
	}
}

// --- Rankings: bounded window, reuse between polls, pause after a timeout ----

func clock(t *testing.T) *float64 {
	now := 1000.0
	monotonic = func() float64 { return now }
	RankingReuseSeconds, RankingBackoffSeconds = 60, 180
	return &now
}

func TestRankingsCoverAtMostTheLastDayOfALongRound(t *testing.T) {
	freshRankings(t)
	day := int64(RankingWindowMS)
	f := transport(t, snapshotData(1000, 10, 100), rankingData(1000))
	m := dashboardManager(t, t.TempDir())
	overview(t, m, "one")
	calls := f.rankingCalls()
	paramsEqual(t, calls[len(calls)-1], "since_ms", "1000", "until_ms", "1000")
	f.cur()["collected_at_ms"] = 1000 + day/2
	f.rk()["collected_at_ms"] = 1000 + day/2
	result := overview(t, m, "one")
	calls = f.rankingCalls()
	paramsEqual(t, calls[len(calls)-1], "since_ms", "1000", "until_ms", strconv.FormatInt(1000+day/2, 10))
	if asInt64(asMap(result["statistics"])["started_at_ms"]) != 1000 {
		t.Fatal("started")
	}
	later := 1000 + 23*day
	f.cur()["collected_at_ms"] = later
	f.rk()["collected_at_ms"] = later
	result = overview(t, m, "one")
	calls = f.rankingCalls()
	paramsEqual(t, calls[len(calls)-1], "since_ms", strconv.FormatInt(later-day, 10), "until_ms", strconv.FormatInt(later, 10))
	r := asMap(result["rankings"])
	if asInt64(r["since_ms"]) != later-day || asInt64(r["until_ms"]) != later || asInt64(asMap(result["statistics"])["started_at_ms"]) != 1000 {
		t.Fatal(r)
	}
}

func TestRankingsAreReusedBetweenPollsForAMinute(t *testing.T) {
	freshRankings(t)
	now := clock(t)
	f := transport(t, nil, nil)
	m := dashboardManager(t, t.TempDir())
	first := overview(t, m, "one")
	if len(f.rankingCalls()) != 1 || asMap(first["rankings"])["success"] != true {
		t.Fatal(f.rankingCalls())
	}
	for step := 0; step < 5; step++ { // the panel polls every 10 s
		*now += 10
		f.cur()["collected_at_ms"] = asInt64(f.cur()["collected_at_ms"]) + 10_000
		polled := overview(t, m, "one")
		wantJSON(t, polled["rankings"], first["rankings"])
		if polled["collected_at"] == first["collected_at"] {
			t.Fatal("counters not fresh")
		}
	}
	if len(f.rankingCalls()) != 1 {
		t.Fatal(f.rankingCalls())
	}
	*now += 10 // 60 s after the first
	f.cur()["collected_at_ms"] = asInt64(f.cur()["collected_at_ms"]) + 10_000
	f.rk()["collected_at_ms"] = f.cur()["collected_at_ms"]
	overview(t, m, "one")
	if len(f.rankingCalls()) != 2 {
		t.Fatal(f.rankingCalls())
	}
}

func TestAResetOrAnotherRecorderAsksAgainAtOnce(t *testing.T) {
	freshRankings(t)
	now := clock(t)
	f := transport(t, nil, nil)
	m := dashboardManager(t, t.TempDir())
	overview(t, m, "one")
	f.cur()["capabilities"] = map[string]any{"query_recorders": []any{"records", "other"}}
	overview(t, m, "one")
	if len(f.rankingCalls()) != 2 { // the recorder list changed
		t.Fatal(f.rankingCalls())
	}
	overview(t, m, "one")
	if len(f.rankingCalls()) != 2 {
		t.Fatal(f.rankingCalls())
	}
	*now++
	if _, err := ResetStatistics(ctx, m, "one", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if len(f.rankingCalls()) != 3 { // a manual reset never reuses
		t.Fatal(f.rankingCalls())
	}
	overview(t, m, "one")
	if len(f.rankingCalls()) != 3 { // and its answer serves the new round
		t.Fatal(f.rankingCalls())
	}
	f.rk()["recorder"] = "other"
	overview(t, m, "one", "recorder", "other")
	calls := f.rankingCalls()
	if recorder, _ := calls[len(calls)-1].get("recorder"); recorder != "other" || len(calls) != 4 {
		t.Fatal(calls)
	}
}

func failingRankings(f *fakeTransport, answer any) {
	f.mu.Lock()
	f.ranks = answer
	f.mu.Unlock()
}

func TestAfterATimeoutRankingsRestAndTheOverviewStillAnswers(t *testing.T) {
	for _, code := range []string{"CORE_UNREACHABLE", "AGENT_UNREACHABLE"} {
		t.Run(code, func(t *testing.T) {
			freshRankings(t)
			now := clock(t)
			f := transport(t, nil, nil)
			m := dashboardManager(t, t.TempDir())
			overview(t, m, "one")
			*now += 61
			good := cloneMap(f.rk())
			failingRankings(f, NewError(code, 0, ""))
			failed := overview(t, m, "one")
			if len(f.rankingCalls()) != 2 {
				t.Fatal(f.rankingCalls())
			}
			// The last answer of this round stays on screen; the counters keep updating.
			if asMap(failed["rankings"])["success"] != true || metricsOf(failed)["requests_total"] == nil {
				t.Fatal(failed["rankings"])
			}
			for i := 0; i < 17; i++ {
				*now += 10
				if asMap(overview(t, m, "one")["rankings"])["success"] != true {
					t.Fatal(i)
				}
			}
			if len(f.rankingCalls()) != 2 {
				t.Fatal(f.rankingCalls())
			}
			*now += 10 // 180 s after the timeout
			failingRankings(f, good)
			overview(t, m, "one")
			if len(f.rankingCalls()) != 3 {
				t.Fatal(f.rankingCalls())
			}
		})
	}
}

func TestATimeoutWithoutAnEarlierAnswerSaysWhyRankingsWait(t *testing.T) {
	freshRankings(t)
	now := clock(t)
	f := transport(t, nil, NewError("CORE_UNREACHABLE", 0, ""))
	m := dashboardManager(t, t.TempDir())
	first := asMap(overview(t, m, "one")["rankings"])
	if first["available"] != false || first["code"] != "core_unreachable" {
		t.Fatal(first)
	}
	*now += 10
	paused := asMap(overview(t, m, "one")["rankings"])
	if paused["code"] != "rankings_paused" || !strings.Contains(asStr(paused["reason"]), "查询记录") || len(f.rankingCalls()) != 1 {
		t.Fatal(paused)
	}
	// A manual reset is asked for explicitly and does not wait.
	if _, err := ResetStatistics(ctx, m, "one", []byte(`{}`)); err == nil {
		t.Fatal("reset succeeded")
	}
	if len(f.rankingCalls()) != 2 {
		t.Fatal(f.rankingCalls())
	}
}

func TestOtherRankingErrorsDoNotPause(t *testing.T) {
	freshRankings(t)
	now := clock(t)
	f := transport(t, nil, NewError("INVALID_RESPONSE", 0, ""))
	m := dashboardManager(t, t.TempDir())
	for i := 0; i < 3; i++ {
		if asMap(overview(t, m, "one")["rankings"])["code"] != "invalid_response" {
			t.Fatal(i)
		}
		*now += 10
	}
	if len(f.rankingCalls()) != 3 {
		t.Fatal(f.rankingCalls())
	}
}

func TestKeptRankingsBelongToOneDevice(t *testing.T) {
	freshRankings(t)
	clock(t)
	f := transport(t, nil, nil)
	m := dashboardManager(t, t.TempDir(), "one", "two")
	setAgentField(m.agent("two"), "host", "192.0.2.99")
	overview(t, m, "one")
	overview(t, m, "two")
	overview(t, m, "one")
	var agents []string
	for _, c := range f.calls {
		if c.endpoint == "rankings" {
			agents = append(agents, c.agent)
		}
	}
	if fmt.Sprint(agents) != "[one two]" {
		t.Fatal(agents)
	}
}
