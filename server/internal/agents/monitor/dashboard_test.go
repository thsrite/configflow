package monitor

// Port of backend/test_oxidns_dashboard.py: durable dashboard boundaries
// never clear core DNS records or caches. The Python tests replace
// _request; here doRequest. The reset route test is in
// internal/api/agents/oxidns_test.go.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/jsonx"
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
	m := &fakeManager{agents: map[string]*jsonx.Object{}, dataDir: dir}
	for _, id := range ids {
		a := baseAgent()
		a.Set("id", id)
		a.Set("version", "1.8.14-go")
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

func snapshotData(now int64, count, elapsed int) *jsonx.Object {
	return jsonx.Obj("success", true, "health", jsonx.Obj("status", "ok", "version", "1.6.1",
		"uptime_ms", 500, "instance_id", "core-one"),
		"capabilities", jsonx.Obj("query_recorders", []any{"records"}),
		"collected_at_ms", now, "source_revision", strings.Repeat("a", 64), "runtime_revision", strings.Repeat("d", 64),
		"upstream_labels", []any{jsonx.Obj("plugin_tag", "remote", "upstream", "cloud", "address", "https://dns.test/dns-query")},
		"metrics", strings.Join([]string{serverText("dns", "udp", count, 0, elapsed, count, 2),
			cacheText("cache", count, count/2, 0, 0, 0, 30), upstreamMetrics(count, elapsed)}, "\n"))
}

func defaultSnapshot() *jsonx.Object { return snapshotData(1000, 10, 100) }

func rankingData(now int64) *jsonx.Object {
	section := func() *jsonx.Object { return jsonx.Obj("sample_size", 0, "rows", []any{}, "at_limit", false) }
	blocked := jsonx.Obj("available", true, "matcher_tag", "adguard_match")
	blocked.Update(section())
	return jsonx.Obj("success", true, "recorder", "records", "collected_at_ms", now,
		"source_revision", strings.Repeat("a", 64), "runtime_revision", strings.Repeat("d", 64), "instance_id", "core-one",
		"retention_days", 7, "sample_limit", 10000, "clients", section(), "domains", section(), "blocked", blocked)
}

type dashCall struct {
	agent, endpoint string
	params          query
}

// fakeTransport replaces doRequest; current/ranks are answered as deep
// copies (or returned as errors), like the Python transport fixture.
type fakeTransport struct {
	mu      sync.Mutex
	current any
	ranks   any
	hook    func(agent *jsonx.Object, endpoint string, params query)
	calls   []dashCall
}

func (f *fakeTransport) do(_ context.Context, _ Manager, agent *jsonx.Object, endpoint string, params query, _ string) (*jsonx.Object, error) {
	f.mu.Lock()
	f.calls = append(f.calls, dashCall{jsonx.Str(agent.Value("id")), endpoint, append(query{}, params...)})
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
		return nil, err
	}
	return result.(*jsonx.Object).DeepCopy(), nil
}

func (f *fakeTransport) cur() *jsonx.Object { return f.current.(*jsonx.Object) }
func (f *fakeTransport) rk() *jsonx.Object  { return f.ranks.(*jsonx.Object) }

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
	previous := doRequest
	doRequest = f.do
	t.Cleanup(func() { doRequest = previous })
	return f
}

// replace sets every key of src into dst (dict.update).
func replace(dst, src *jsonx.Object) {
	for _, key := range dst.Keys() {
		dst.Delete(key)
	}
	dst.Update(src)
}

func overview(t *testing.T, m Manager, id string, a ...string) *jsonx.Object {
	t.Helper()
	result, err := Overview(ctx, m, id, args(a...))
	if err != nil {
		t.Fatal(err)
	}
	return result
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
	stats := initial.GetObject("statistics")
	if stats.Value("reason") != "initial" || stats.GetInt("started_at_ms") != 1000 {
		t.Fatal(jsonx.Repr(stats))
	}
	wantJSON(t, pick(metricsOf(initial), "requests_total", "avg_latency_ms", "inflight"),
		jsonx.Obj("requests_total", 0, "avg_latency_ms", nil, "inflight", 2))
	wantJSON(t, row(initial, "caches", 0).Value("entries"), 30)
	store := NewStateStore(dir)
	saved := readFile(t, store.Path("one"))
	info, _ := os.Stat(store.Path("one"))
	before := info.ModTime()
	replace(f.cur(), snapshotData(2000, 14, 180))
	f.rk().Set("collected_at_ms", 2000)
	// A new manager simulates a ConfigFlow restart without keeping a memory cache.
	second := overview(t, dashboardManager(t, dir), "one")
	wantJSON(t, second.Value("statistics"), initial.Value("statistics"))
	wantJSON(t, pick(metricsOf(second), "requests_total", "avg_latency_ms"), jsonx.Obj("requests_total", 4, "avg_latency_ms", 20))
	wantJSON(t, pick(row(second, "upstreams", 0), "requests", "avg_latency_ms"), jsonx.Obj("requests", 4, "avg_latency_ms", 20))
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
	replace(f.cur(), snapshotData(2000, 20, 100))
	f.rk().Set("collected_at_ms", 2000)
	changed, err := ResetDashboard(ctx, m, "one", jsonx.Obj("recorder", "records"))
	if err != nil {
		t.Fatal(err)
	}
	stats := changed.GetObject("statistics")
	if jsonx.Equal(stats.Value("generation"), first.GetObject("statistics").Value("generation")) ||
		stats.Value("reason") != "manual" || stats.GetInt("started_at_ms") != 2000 {
		t.Fatal(jsonx.Repr(stats))
	}
	wantJSON(t, pick(metricsOf(changed), "requests_total", "inflight"), jsonx.Obj("requests_total", 0, "inflight", 2))
	wantJSON(t, row(changed, "caches", 0).Value("entries"), 30)
	if !bytes.Equal(readFile(t, store.Path("two")), otherSaved) {
		t.Fatal("other agent changed")
	}
	wantJSON(t, overview(t, m, "two").Value("statistics"), other.Value("statistics"))
}

func TestIdentityChangesOpenANewWindow(t *testing.T) {
	for _, tc := range [][2]string{{"instance_id", "core_changed"}, {"source_revision", "configuration_changed"}, {"host", "target_changed"}} {
		t.Run(tc[0], func(t *testing.T) {
			freshRankings(t)
			m := dashboardManager(t, t.TempDir())
			f := transport(t, nil, nil)
			previous := overview(t, m, "one")
			replace(f.cur(), snapshotData(2000, 40, 100))
			f.rk().Set("collected_at_ms", 2000)
			switch tc[0] {
			case "instance_id":
				f.cur().GetObject("health").Set("instance_id", "core-two")
				f.rk().Set("instance_id", "core-two")
			case "source_revision":
				f.cur().Set("source_revision", strings.Repeat("b", 64))
				f.rk().Set("source_revision", strings.Repeat("b", 64))
			default:
				m.agent("one").Set("host", "192.0.2.10")
			}
			result := overview(t, m, "one")
			stats := result.GetObject("statistics")
			if stats.Value("reason") != tc[1] || jsonx.Equal(stats.Value("generation"), previous.GetObject("statistics").Value("generation")) ||
				stats.GetInt("started_at_ms") != 2000 {
				t.Fatal(jsonx.Repr(stats))
			}
			wantJSON(t, metricsOf(result).Value("requests_total"), 0)
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
			replace(f.cur(), snapshotData(2000, 20, 100))
			f.rk().Set("collected_at_ms", 2000)
			switch stage {
			case "overview":
				f.current = NewError("AGENT_UNREACHABLE", 0, "")
			case "rankings":
				f.ranks = NewError("AGENT_UNREACHABLE", 0, "")
			case "target":
				f.hook = func(_ *jsonx.Object, endpoint string, _ query) {
					if endpoint == "rankings" {
						m.agent("one").Set("host", "192.0.2.50")
					}
				}
			default:
				f.rk().Set("source_revision", strings.Repeat("c", 64))
			}
			_, err := ResetDashboard(ctx, m, "one", jsonx.NewObject())
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
	m.agent("one").Set("version", "1.8.13-go")
	f := transport(t, nil, nil)
	result := overview(t, m, "one")
	wantJSON(t, metricsOf(result).Value("requests_total"), 10)
	if result.Value("statistics") != nil || result.GetObject("rankings").Value("code") != "upgrade_required" {
		t.Fatal(jsonx.Repr(result))
	}
	if _, err := os.Stat(filepath.Join(dir, "dns-monitor")); !os.IsNotExist(err) {
		t.Fatal("state directory created")
	}
	if fmt.Sprint(f.endpoints()) != "[overview]" {
		t.Fatal(f.endpoints())
	}
	_, err := ResetDashboard(ctx, m, "one", jsonx.NewObject())
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
	replace(f.cur(), snapshotData(2000, 20, 100))
	f.ranks = NewError("CORE_UNREACHABLE", 0, "")
	ResetRankingsCache()
	result := overview(t, m, "one")
	wantJSON(t, result.Value("statistics"), previous.Value("statistics"))
	wantJSON(t, metricsOf(result).Value("requests_total"), 10)
	rankings := result.GetObject("rankings")
	if rankings.Value("available") != false || rankings.Value("success") != false {
		t.Fatal(jsonx.Repr(rankings))
	}
}

func TestGenerationCompareAndSetRejectsStaleSnapshot(t *testing.T) {
	freshRankings(t)
	dir := t.TempDir()
	m := dashboardManager(t, dir)
	f := transport(t, nil, nil)
	overview(t, m, "one")
	store := NewStateStore(dir)
	original, err := store.Read("one")
	if err != nil {
		t.Fatal(err)
	}
	replacement := original.Clone()
	replacement.Set("generation", strings.Repeat("f", 32))
	replacement.Set("reason", "manual")
	replacement.Set("started_at_ms", 2000)
	replace(f.cur(), snapshotData(3000, 10, 100))
	f.rk().Set("collected_at_ms", 3000)
	f.hook = func(_ *jsonx.Object, endpoint string, _ query) {
		if endpoint == "rankings" {
			if _, err := store.CompareAndSet("one", original.GetString("generation"), replacement, nil); err != nil {
				t.Error(err)
			}
		}
	}
	_, err = Overview(ctx, m, "one", nil)
	wantMonitorError(t, err, "DASHBOARD_CHANGED", 0)
	current, _ := store.Read("one")
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
		_, err := Overview(ctx, m, "one", nil)
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
	replace(f.cur(), snapshotData(2000, 10, 100))
	f.rk().Set("collected_at_ms", 2000)
	previous := replaceFile
	replaceFile = func(string, string) error { return errors.New("disk unavailable") }
	t.Cleanup(func() { replaceFile = previous })
	_, err := ResetDashboard(ctx, m, "one", jsonx.NewObject())
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
	state, _ := store.Read("one")
	if filepath.Dir(store.Path("../../escape")) != filepath.Join(dir, "dns-monitor") ||
		filepath.Base(store.Path("../../escape")) == filepath.Base(store.Path(`..\..\escape`)) {
		t.Fatal(store.Path("../../escape"))
	}
	var wins int32
	var wg sync.WaitGroup
	for index := 1; index <= 8; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			replacement := state.Clone()
			replacement.Set("generation", fmt.Sprintf("%032x", index))
			replacement.Set("reason", "manual")
			if _, err := store.CompareAndSet("one", state.GetString("generation"), replacement, nil); err == nil {
				atomic.AddInt32(&wins, 1)
			} else if !errors.Is(err, ErrStateConflict) {
				t.Error(err)
			}
		}(index)
	}
	wg.Wait()
	current, _ := store.Read("one")
	if wins != 1 || !ValidateState(current) {
		t.Fatal(wins)
	}
}

func TestDomainRankingsSupportMultipleQuestionsPerRecord(t *testing.T) {
	value := jsonx.Obj("sample_size", 1, "rows", []any{jsonx.Obj("key", "many.test", "count", 2)}, "at_limit", false)
	section, err := rankingSection(value, "domains")
	if err != nil {
		t.Fatal(err)
	}
	wantJSON(t, section.GetList("rows")[0].(*jsonx.Object).Value("share"), 2)
	bad := value.Clone()
	bad.Set("rows", []any{jsonx.Obj("key", "192.0.2.1", "count", 2)})
	if _, err := rankingSection(bad, "clients"); err == nil {
		t.Fatal("accepted")
	}
}

func TestRankingsRecomputeShareAndPreserveSamplingLimit(t *testing.T) {
	freshRankings(t)
	m := dashboardManager(t, t.TempDir())
	f := transport(t, nil, nil)
	f.rk().Set("clients", jsonx.Obj("sample_size", 10000, "at_limit", true,
		"rows", []any{jsonx.Obj("key", "192.0.2.2", "count", 5, "share", "bad"), jsonx.Obj("key", "192.0.2.1", "count", 20)}))
	f.rk().Set("blocked", jsonx.Obj("available", false, "reason", "private password and configuration"))
	result := overview(t, m, "one").GetObject("rankings")
	if result.Value("success") != true || result.GetInt("sample_limit") != 10000 || result.GetObject("clients").Value("at_limit") != true {
		t.Fatal(jsonx.Repr(result))
	}
	wantJSON(t, result.GetObject("clients").GetList("rows")[0], jsonx.Obj("key", "192.0.2.1", "count", 20, "share", 0.002))
	blocked := result.GetObject("blocked")
	if blocked.Value("available") != false || len(blocked.GetList("rows")) != 0 {
		t.Fatal(jsonx.Repr(blocked))
	}
	encoded, _ := jsonx.Default(result)
	if strings.Contains(string(encoded), "password") {
		t.Fatal(string(encoded))
	}
}

func TestUpstreamRawLabelsNeverLeakURLCredentials(t *testing.T) {
	freshRankings(t)
	m := dashboardManager(t, t.TempDir())
	f := transport(t, nil, nil)
	raw := "https://user:password@dns.test/dns-query?key=secret#fragment"
	f.cur().Set("metrics", strings.ReplaceAll(f.cur().GetString("metrics"), `upstream="cloud"`, `upstream="`+raw+`"`))
	label := f.cur().GetList("upstream_labels")[0].(*jsonx.Object)
	label.Set("upstream", raw)
	label.Set("address", raw)
	result := overview(t, m, "one")
	r := row(result, "upstreams", 0)
	if r.Value("upstream") != "https://dns.test/dns-query" || r.Value("address") != "https://dns.test/dns-query" {
		t.Fatal(jsonx.Repr(r))
	}
	encoded, _ := jsonx.Default(result)
	for _, secret := range []string{"password", "secret", "fragment"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal(secret)
		}
	}
}

func TestResetInputValidationBeforeNetwork(t *testing.T) {
	for _, body := range []any{nil, []any{}, jsonx.Obj("url", "http://elsewhere"), jsonx.Obj("recorder", "../private"), jsonx.Obj("recorder", 1)} {
		freshRankings(t)
		f := transport(t, nil, nil)
		_, err := ResetDashboard(ctx, dashboardManager(t, t.TempDir()), "one", body)
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
	replace(f.cur(), snapshotData(2000, 10, 100))
	f.rk().Set("collected_at_ms", 2000)
	restarted := overview(t, m, "one")
	stats := restarted.GetObject("statistics")
	if stats.Value("reason") != "counters_changed" || jsonx.Equal(stats.Value("generation"), previous.GetObject("statistics").Value("generation")) {
		t.Fatal(jsonx.Repr(stats))
	}
	wantJSON(t, metricsOf(restarted).Value("requests_total"), 0)
	replace(f.cur(), snapshotData(3000, 110, 1100))
	f.rk().Set("collected_at_ms", 3000)
	continued := overview(t, dashboardManager(t, dir), "one")
	wantJSON(t, continued.Value("statistics"), restarted.Value("statistics"))
	wantJSON(t, pick(metricsOf(continued), "requests_total", "avg_latency_ms"), jsonx.Obj("requests_total", 100, "avg_latency_ms", 10))
}

func TestUnknownRetentionIsPreservedAndOverlongTopListIsRejected(t *testing.T) {
	freshRankings(t)
	m := dashboardManager(t, t.TempDir())
	f := transport(t, nil, nil)
	f.rk().Set("retention_days", nil)
	if overview(t, m, "one").GetObject("rankings").Value("retention_days") != nil {
		t.Fatal("retention")
	}
	rows := []any{}
	for i := 0; i < 11; i++ {
		rows = append(rows, jsonx.Obj("key", strconv.Itoa(i)+".test", "count", 1))
	}
	f.rk().Set("domains", jsonx.Obj("sample_size", 20, "at_limit", false, "rows", rows))
	_, err := ResetDashboard(ctx, m, "one", jsonx.NewObject())
	wantMonitorError(t, err, "INVALID_RESPONSE", 0)
}

func TestUpstreamHashSuffixKeepsDistinctStableIDs(t *testing.T) {
	rows := []any{jsonx.Obj("tag", "forward", "upstream", "dns#1"), jsonx.Obj("tag", "forward", "upstream", "dns#2")}
	result, _ := publicUpstreams(rows, nil)
	again, _ := publicUpstreams(rows, nil)
	a, b := result[0].(*jsonx.Object), result[1].(*jsonx.Object)
	if a.Value("upstream") != "dns#1" || b.Value("upstream") != "dns#2" || a.Value("id") == b.Value("id") {
		t.Fatal(jsonx.Repr(result))
	}
	wantJSON(t, result, again)
}

func TestMissingRecorderStillAllowsResetOfMetricsWithoutCoreMutation(t *testing.T) {
	freshRankings(t)
	m := dashboardManager(t, t.TempDir())
	f := transport(t, nil, nil)
	f.cur().GetObject("capabilities").Set("query_recorders", []any{})
	result, err := ResetDashboard(ctx, m, "one", jsonx.NewObject())
	if err != nil {
		t.Fatal(err)
	}
	wantJSON(t, metricsOf(result).Value("requests_total"), 0)
	if result.GetObject("rankings").Value("code") != "recorder_not_configured" || fmt.Sprint(f.endpoints()) != "[overview]" {
		t.Fatal(jsonx.Repr(result.Value("rankings")), f.endpoints())
	}
}

func TestInitialSnapshotRequiresValidClockAndKeepsStateAbsentOnError(t *testing.T) {
	freshRankings(t)
	dir := t.TempDir()
	m := dashboardManager(t, dir)
	f := transport(t, nil, nil)
	f.cur().Set("collected_at_ms", int64(1)<<53-1)
	_, err := Overview(ctx, m, "one", nil)
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
	f.cur().GetObject("capabilities").Set("query_recorders", []any{"replacement"})
	f.cur().Set("source_revision", strings.Repeat("b", 64))
	f.rk().Set("source_revision", strings.Repeat("b", 64))
	f.cur().Set("collected_at_ms", 2000)
	f.rk().Set("collected_at_ms", 2000)
	f.rk().Set("recorder", "replacement")
	result := overview(t, m, "one", "recorder", "records")
	wantJSON(t, result.Value("recorders"), []any{"replacement"})
	calls := f.rankingCalls()
	if result.GetObject("rankings").Value("recorder") != "replacement" || func() bool { _, ok := calls[len(calls)-1].get("recorder"); return ok }() {
		t.Fatal(jsonx.Repr(result.Value("rankings")), calls)
	}
	if result.GetObject("statistics").Value("reason") != "configuration_changed" {
		t.Fatal(jsonx.Repr(result.Value("statistics")))
	}
	store := NewStateStore(dir)
	saved, _ := store.Read("one")
	_, err := ResetDashboard(ctx, m, "one", jsonx.Obj("recorder", "records"))
	wantMonitorError(t, err, "INVALID_QUERY", 0)
	current, _ := store.Read("one")
	wantJSON(t, current, saved)
}

func TestSameFileRuntimeReloadStartsNewWindowEvenWhenCountersCaughtUp(t *testing.T) {
	freshRankings(t)
	m := dashboardManager(t, t.TempDir())
	f := transport(t, nil, nil)
	previous := overview(t, m, "one")
	replace(f.cur(), snapshotData(2000, 100, 100))
	f.cur().Set("runtime_revision", strings.Repeat("e", 64))
	f.rk().Set("runtime_revision", strings.Repeat("e", 64))
	f.rk().Set("collected_at_ms", 2000)
	result := overview(t, m, "one")
	stats := result.GetObject("statistics")
	if stats.Value("reason") != "core_changed" || jsonx.Equal(stats.Value("generation"), previous.GetObject("statistics").Value("generation")) {
		t.Fatal(jsonx.Repr(stats))
	}
	wantJSON(t, metricsOf(result).Value("requests_total"), 0)
}

func TestRuntimeGenerationChangeBetweenOverviewAndRankingsBlocksManualCommit(t *testing.T) {
	freshRankings(t)
	dir := t.TempDir()
	m := dashboardManager(t, dir)
	f := transport(t, nil, nil)
	overview(t, m, "one")
	store := NewStateStore(dir)
	saved := readFile(t, store.Path("one"))
	f.rk().Set("runtime_revision", strings.Repeat("e", 64))
	_, err := ResetDashboard(ctx, m, "one", jsonx.NewObject())
	wantMonitorError(t, err, "SNAPSHOT_CHANGED", 0)
	if !bytes.Equal(readFile(t, store.Path("one")), saved) {
		t.Fatal("state changed")
	}
}

// TestFileLockSerializesResetsAcrossProcesses runs four copies of the test
// binary (TestHelperReplaceGeneration) against one state file.
func TestFileLockSerializesResetsAcrossProcesses(t *testing.T) {
	freshRankings(t)
	dir := t.TempDir()
	m := dashboardManager(t, dir)
	transport(t, nil, nil)
	overview(t, m, "one")
	store := NewStateStore(dir)
	state, _ := store.Read("one")
	ready := filepath.Join(dir, "ready")
	var commands []*exec.Cmd
	var outputs []*bytes.Buffer
	for index := 1; index <= 4; index++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperReplaceGeneration$")
		cmd.Env = append(os.Environ(), "MONITOR_HELPER_DIR="+dir, "MONITOR_HELPER_INDEX="+strconv.Itoa(index),
			"MONITOR_HELPER_EXPECTED="+state.GetString("generation"), "MONITOR_HELPER_READY="+ready)
		out := &bytes.Buffer{}
		cmd.Stdout, cmd.Stderr = out, out
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands, outputs = append(commands, cmd), append(outputs, out)
	}
	os.WriteFile(ready, nil, 0o644)
	wins := 0
	for i, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatal(err, outputs[i].String())
		}
		if strings.Contains(outputs[i].String(), "RESULT=won") {
			wins++
		}
	}
	current, _ := store.Read("one")
	if wins != 1 || !ValidateState(current) {
		t.Fatal(wins)
	}
}

func TestHelperReplaceGeneration(t *testing.T) {
	dir := os.Getenv("MONITOR_HELPER_DIR")
	if dir == "" {
		t.Skip("helper process")
	}
	for {
		if _, err := os.Stat(os.Getenv("MONITOR_HELPER_READY")); err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	store := NewStateStore(dir)
	state, err := store.Read("one")
	if err != nil {
		t.Fatal(err)
	}
	index, _ := strconv.Atoi(os.Getenv("MONITOR_HELPER_INDEX"))
	replacement := state.Clone()
	replacement.Set("generation", fmt.Sprintf("%032x", index))
	replacement.Set("reason", "manual")
	_, err = store.CompareAndSet("one", os.Getenv("MONITOR_HELPER_EXPECTED"), replacement, nil)
	switch {
	case err == nil:
		fmt.Println("RESULT=won")
	case errors.Is(err, ErrStateConflict):
		fmt.Println("RESULT=lost")
	default:
		t.Fatal(err)
	}
}

func TestMissingRetentionMetadataIsUnknown(t *testing.T) {
	freshRankings(t)
	f := transport(t, nil, nil)
	f.rk().Delete("retention_days")
	result := overview(t, dashboardManager(t, t.TempDir()), "one").GetObject("rankings")
	if result.Value("success") != true || result.Value("retention_days") != nil {
		t.Fatal(jsonx.Repr(result))
	}
}

func TestBlockedRankingReasonFollowsAgent(t *testing.T) {
	cases := []struct {
		blocked      *jsonx.Object
		code, reason string
	}{
		{jsonx.NewObject(), "standard_adguard", "标准 AdGuard 拦截路径"},
		{jsonx.Obj("reason", "standard_adguard"), "standard_adguard", "标准 AdGuard 拦截路径"},
		{jsonx.Obj("reason", "rule_dns", "matcher_tags", []any{"qs.match.sequence_main.3.0.qname", "qs.match.sequence_main.4.0.qname"}),
			"rule_dns", "规则拦截（拒绝/黑洞）"},
		{jsonx.Obj("reason", "standard_adguard_rule_dns", "matcher_tags", []any{"qs.match.sequence_main.2.0.question"}),
			"standard_adguard_rule_dns", "标准 AdGuard 与规则拦截"},
	}
	for _, tc := range cases {
		freshRankings(t)
		m := dashboardManager(t, t.TempDir())
		f := transport(t, nil, nil)
		f.rk().GetObject("blocked").Update(tc.blocked)
		result := overview(t, m, "one").GetObject("rankings").GetObject("blocked")
		if result.Value("available") != true || result.Value("code") != tc.code || result.Value("reason") != tc.reason {
			t.Fatal(jsonx.Repr(result))
		}
		wantJSON(t, result.Value("matcher_tags"), tc.blocked.Value("matcher_tags"))
	}
}

func TestBlockedRankingRejectsMalformedReasonOrMatchers(t *testing.T) {
	many := []any{}
	for i := 0; i < 33; i++ {
		many = append(many, fmt.Sprintf("m%d", i))
	}
	for _, blocked := range []*jsonx.Object{jsonx.Obj("reason", "Rule DNS"), jsonx.Obj("reason", "rule_dns", "matcher_tags", []any{"../x"}),
		jsonx.Obj("reason", "rule_dns", "matcher_tags", []any{}), jsonx.Obj("reason", "rule_dns", "matcher_tags", many)} {
		freshRankings(t)
		m := dashboardManager(t, t.TempDir())
		f := transport(t, nil, nil)
		f.rk().GetObject("blocked").Update(blocked)
		if overview(t, m, "one").GetObject("rankings").Value("available") != false {
			t.Fatal(jsonx.Repr(blocked))
		}
	}
}

func TestTooManyBlockRulesReasonIsActionable(t *testing.T) {
	freshRankings(t)
	m := dashboardManager(t, t.TempDir())
	f := transport(t, nil, nil)
	f.rk().Set("blocked", jsonx.Obj("available", false, "reason", "too_many_block_rules", "sample_size", 0, "rows", []any{}, "at_limit", false))
	result := overview(t, m, "one").GetObject("rankings").GetObject("blocked")
	if result.Value("available") != false || result.Value("code") != "too_many_block_rules" ||
		!strings.HasPrefix(result.GetString("reason"), "拦截规则分组超过 32 个") || strings.Contains(result.GetString("reason"), "刷新") {
		t.Fatal(jsonx.Repr(result))
	}
	// The agent's sentence fallback is not a code; the generic text covers rule blocks too.
	f.rk().Set("blocked", jsonx.Obj("available", false, "reason", "当前配置未包含可确认的标准 AdGuard 拦截路径"))
	result = overview(t, m, "one").GetObject("rankings").GetObject("blocked")
	if result.Value("code") != "unsupported_configuration" || !strings.Contains(result.GetString("reason"), "拒绝/黑洞") {
		t.Fatal(jsonx.Repr(result))
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
	f.cur().Set("collected_at_ms", 1000+day/2)
	f.rk().Set("collected_at_ms", 1000+day/2)
	result := overview(t, m, "one")
	calls = f.rankingCalls()
	paramsEqual(t, calls[len(calls)-1], "since_ms", "1000", "until_ms", strconv.FormatInt(1000+day/2, 10))
	if result.GetObject("statistics").GetInt("started_at_ms") != 1000 {
		t.Fatal("started")
	}
	later := 1000 + 23*day
	f.cur().Set("collected_at_ms", later)
	f.rk().Set("collected_at_ms", later)
	result = overview(t, m, "one")
	calls = f.rankingCalls()
	paramsEqual(t, calls[len(calls)-1], "since_ms", strconv.FormatInt(later-day, 10), "until_ms", strconv.FormatInt(later, 10))
	r := result.GetObject("rankings")
	if r.GetInt("since_ms") != later-day || r.GetInt("until_ms") != later || result.GetObject("statistics").GetInt("started_at_ms") != 1000 {
		t.Fatal(jsonx.Repr(r))
	}
}

func TestRankingsAreReusedBetweenPollsForAMinute(t *testing.T) {
	freshRankings(t)
	now := clock(t)
	f := transport(t, nil, nil)
	m := dashboardManager(t, t.TempDir())
	first := overview(t, m, "one")
	if len(f.rankingCalls()) != 1 || first.GetObject("rankings").Value("success") != true {
		t.Fatal(f.rankingCalls())
	}
	for step := 0; step < 5; step++ { // the panel polls every 10 s
		*now += 10
		f.cur().Set("collected_at_ms", f.cur().GetInt("collected_at_ms")+10_000)
		polled := overview(t, m, "one")
		wantJSON(t, polled.Value("rankings"), first.Value("rankings"))
		if polled.Value("collected_at") == first.Value("collected_at") {
			t.Fatal("counters not fresh")
		}
	}
	if len(f.rankingCalls()) != 1 {
		t.Fatal(f.rankingCalls())
	}
	*now += 10 // 60 s after the first
	f.cur().Set("collected_at_ms", f.cur().GetInt("collected_at_ms")+10_000)
	f.rk().Set("collected_at_ms", f.cur().Value("collected_at_ms"))
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
	f.cur().Set("capabilities", jsonx.Obj("query_recorders", []any{"records", "other"}))
	overview(t, m, "one")
	if len(f.rankingCalls()) != 2 { // the recorder list changed
		t.Fatal(f.rankingCalls())
	}
	overview(t, m, "one")
	if len(f.rankingCalls()) != 2 {
		t.Fatal(f.rankingCalls())
	}
	*now++
	if _, err := ResetDashboard(ctx, m, "one", jsonx.NewObject()); err != nil {
		t.Fatal(err)
	}
	if len(f.rankingCalls()) != 3 { // a manual reset never reuses
		t.Fatal(f.rankingCalls())
	}
	overview(t, m, "one")
	if len(f.rankingCalls()) != 3 { // and its answer serves the new round
		t.Fatal(f.rankingCalls())
	}
	f.rk().Set("recorder", "other")
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
			good := f.rk().DeepCopy()
			failingRankings(f, NewError(code, 0, ""))
			failed := overview(t, m, "one")
			if len(f.rankingCalls()) != 2 {
				t.Fatal(f.rankingCalls())
			}
			// The last answer of this round stays on screen; the counters keep updating.
			if failed.GetObject("rankings").Value("success") != true || metricsOf(failed).Value("requests_total") == nil {
				t.Fatal(jsonx.Repr(failed.Value("rankings")))
			}
			for i := 0; i < 17; i++ {
				*now += 10
				if overview(t, m, "one").GetObject("rankings").Value("success") != true {
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
	first := overview(t, m, "one").GetObject("rankings")
	if first.Value("available") != false || first.Value("code") != "core_unreachable" {
		t.Fatal(jsonx.Repr(first))
	}
	*now += 10
	paused := overview(t, m, "one").GetObject("rankings")
	if paused.Value("code") != "rankings_paused" || !strings.Contains(paused.GetString("reason"), "查询记录") || len(f.rankingCalls()) != 1 {
		t.Fatal(jsonx.Repr(paused))
	}
	// A manual reset is asked for explicitly and does not wait.
	if _, err := ResetDashboard(ctx, m, "one", jsonx.NewObject()); err == nil {
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
		if overview(t, m, "one").GetObject("rankings").Value("code") != "invalid_response" {
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
	m.agent("two").Set("host", "192.0.2.99")
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
