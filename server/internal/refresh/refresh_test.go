package refresh

// Ports of backend/test_scheduled_tasks.py and
// backend/test_subscription_refresh_policy.py (the cases that run without
// the later resource routes; see PROGRESS-config.md C15 for the deferred
// ones). Python's monkeypatched get_subscription_proxies_yaml /
// request_rule are the Env fakes here; nothing contacts a network.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/instance"
	"configflow/internal/jsonresp"
	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
	"configflow/internal/lockx"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/rules"
	"configflow/internal/substore"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

func TestMain(m *testing.M) {
	logx.Setup(logx.Config{Level: logx.Info, Console: io.Discard})
	code := m.Run()
	if violations := lockx.Violations(); violations != 0 {
		logger.Errorf("Lock order violations: %d", violations)
		code = 1
	}
	os.Exit(code)
}

const (
	subURL  = "https://airport.test/sub?token=private"
	ruleURL = "https://rules.test/list.txt?key=private"
)

func proxiesYAML(t *testing.T) string {
	t.Helper()
	text, err := yamlx.Marshal(yamlx.Map(yamlx.Str("proxies"), yamlx.Seq(yamlx.Map(
		yamlx.Str("name"), yamlx.Str("Remote"), yamlx.Str("type"), yamlx.Str("ss"),
		yamlx.Str("server"), yamlx.Str("example.test"), yamlx.Str("port"), yamlx.Int(443),
		yamlx.Str("cipher"), yamlx.Str("aes-128-gcm"), yamlx.Str("password"), yamlx.Str("p")))))
	if err != nil {
		t.Fatal(err)
	}
	return string(text)
}

func ruleDecl(t *testing.T) model.RuleLibraryItem {
	t.Helper()
	return testjson.Decode[model.RuleLibraryItem](t, map[string]any{
		"id": "rule", "name": "Streaming", "source_type": "url", "url": ruleURL,
		"behavior": "domain", "format": "text", "enabled": true,
	})
}

func refreshCacheRule(tb testing.TB, value any) model.RuleConfig {
	tb.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		tb.Fatal(err)
	}
	var rule model.RuleConfig
	if err := json.Unmarshal(data, &rule); err != nil {
		tb.Fatal(err)
	}
	return rule
}

// fakeClock is wall time that tests move, and a monotonic counter.
type fakeClock struct {
	mu   sync.Mutex
	now  time.Time
	mono float64
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Monotonic() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mono += 0.0125
	return c.mono
}

func (c *fakeClock) Sleep(time.Duration) {}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// fakeTransport answers rule downloads.
type fakeTransport struct {
	mu    sync.Mutex
	urls  []string
	reply func(url string) (int, string, error)
}

func (f *fakeTransport) Do(_ context.Context, req rulefetch.TransportRequest) (*rulefetch.Response, error) {
	f.mu.Lock()
	f.urls = append(f.urls, req.URL)
	f.mu.Unlock()
	status, body, err := f.reply(req.URL)
	if err != nil {
		return nil, err
	}
	return &rulefetch.Response{StatusCode: status, Reason: http.StatusText(status), Header: http.Header{}, URL: req.URL,
		Body: io.NopCloser(strings.NewReader(body))}, nil
}

type fixture struct {
	t         *testing.T
	ctx       context.Context
	repo      *repo.Repository
	clock     *fakeClock
	env       *RefreshEnv
	transport *fakeTransport
	fetches   []string
	fetchMu   sync.Mutex
}

// setup is the Python fixture: one subscription (interval 86400) and one
// library rule; downloads fail loudly unless a test installs a reply.
// Tests using setup stay sequential: Env, task claims and lifecycle wiring are
// process-wide. The recorded replay also keeps Env calls sequential.
func setup(t *testing.T, subInterval any) *fixture {
	t.Helper()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, repo: r, clock: &fakeClock{now: time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)}}
	f.ctx = repo.WithRepository(context.Background(), r)
	err = r.UpdateShared(f.ctx, func(_ context.Context, shared *model.Shared) error {
		shared.Subscriptions = []model.Subscription{testjson.Decode[model.Subscription](t, map[string]any{
			"id": "s", "name": "Feed", "url": subURL, "enabled": true, "interval": subInterval,
		})}
		shared.Nodes = []model.Node{}
		shared.Aggregations = []model.Aggregation{}
		shared.RuleLibrary = []model.RuleLibraryItem{ruleDecl(t)}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	f.transport = &fakeTransport{reply: func(url string) (int, string, error) {
		t.Errorf("unexpected rule request %s", url)
		return 0, "", errors.New("no request")
	}}
	hex := 0
	f.env = DefaultRefreshEnv()
	f.env.Clock = f.clock
	f.env.RuleClient = &rulefetch.Client{Transport: f.transport, Sleep: func(time.Duration) {}}
	f.env.RandomHex = func() string { hex++; return strings.Repeat("0", 7) + string(rune('0'+hex%10)) }
	f.env.Sources.Fetch = func(_ context.Context, subID, url string, opts substore.FetchOptions) (*SourceResult, error) {
		t.Errorf("unexpected subscription download %s", url)
		return nil, errors.New("no request")
	}
	t.Cleanup(SetRefreshEnv(f.env))
	return f
}

// download installs a subscription download answer and records calls.
func (f *fixture) download(text, source string, err error) {
	f.env.Sources.Fetch = func(_ context.Context, subID, url string, opts substore.FetchOptions) (*SourceResult, error) {
		f.fetchMu.Lock()
		f.fetches = append(f.fetches, subID+" "+url+" refresh="+map[bool]string{true: "True", false: "False"}[opts.Refresh])
		f.fetchMu.Unlock()
		if err != nil {
			return nil, err
		}
		return &SourceResult{Text: text, Source: source}, nil
	}
}

func (f *fixture) status(key string) map[string]any {
	snap, err := taskstatus.New(f.repo.DataDir()).Read()
	if err != nil {
		f.t.Fatal(err)
	}
	raw, ok := snap[key]
	if !ok || len(raw) == 0 {
		return map[string]any{}
	}
	m, err := decodeMap(raw)
	if err != nil {
		f.t.Fatal(err)
	}
	return m
}

func (f *fixture) cache() *substore.Cache {
	cache, err := substore.LoadCache(f.ctx, "s")
	if err != nil {
		f.t.Fatal(err)
	}
	return cache
}

func (f *fixture) updateShared(fn func(shared *model.Shared)) {
	err := f.repo.UpdateShared(f.ctx, func(_ context.Context, shared *model.Shared) error {
		fn(shared)
		return nil
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) sub() model.Subscription {
	shared, err := f.repo.Shared(f.ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	return shared.Subscriptions[0]
}

// cacheSource is test_subscription_refresh_policy.cache_source: stored
// nodes of url whose updated_at is age seconds ago.
func (f *fixture) cacheSource(age time.Duration, url string) *substore.Cache {
	proxies, err := substore.ParseProxyNodes(proxiesYAML(f.t))
	if err != nil {
		f.t.Fatal(err)
	}
	nodes, err := substore.ProxyNodes(proxies)
	if err != nil {
		f.t.Fatal(err)
	}
	value, err := substore.SaveCache(f.ctx, "s", nodes, &substore.CacheMetadata{URL: new(url)})
	if err != nil {
		f.t.Fatal(err)
	}
	value.UpdatedAt = isoMicro(f.clock.Now().Add(-age))
	data, err := jsonresp.Encode(value)
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.repo.WriteSharedText(f.ctx, "subscribes/s.json", data); err != nil {
		f.t.Fatal(err)
	}
	return f.cache()
}

func timeoutError() error {
	return &rulefetch.FetchError{Class: "ConnectTimeout", Message: "https://rules.test?key=private"}
}

// test_scheduled_tasks.py::test_record_counts_runs_failures_and_skips
func TestRecordCountsRunsFailuresAndSkips(t *testing.T) {
	f := setup(t, 86400)
	retained := true
	for _, r := range []taskstatus.Result{
		{Error: "boom", CacheRetained: &retained, CacheUpdatedAt: "2026-01-01T00:00:00Z"},
		{Error: "boom again", CacheRetained: &retained},
		{Skipped: true, Error: "nothing to do"},
	} {
		if err := recordStatus(f.ctx, "x", r); err != nil {
			t.Fatal(err)
		}
	}
	entry := f.status("x")
	if mapInt(entry, "run_count") != 2 || mapInt(entry, "failure_count") != 2 || mapInt(entry, "consecutive_failures") != 2 ||
		entry["last_status"] != "skipped" || entry["last_error"] != "nothing to do" {
		t.Fatal(compactJSON(entry))
	}
	_ = recordStatus(f.ctx, "x", taskstatus.Result{OK: true, DurationMs: 12.6})
	entry = f.status("x")
	_, hasRetained := entry["cache_retained"]
	if mapInt(entry, "run_count") != 3 || mapInt(entry, "failure_count") != 2 || mapInt(entry, "consecutive_failures") != 0 ||
		entry["last_error"] != "" || hasRetained || mapInt(entry, "last_duration_ms") != 12 {
		t.Fatal(compactJSON(entry))
	}
	_ = taskstatus.New(f.repo.DataDir()).Forget("x", "missing")
	if len(f.status("x")) != 0 {
		t.Fatal("forget")
	}
}

func mustTime(t *testing.T, text string) time.Time {
	value, ok := taskstatus.ParseTimestamp(text)
	if !ok {
		t.Fatalf("parse %s", text)
	}
	return value
}

// test_scheduled_tasks.py::test_next_run_backs_off_after_failures_but_never_beyond_the_interval
func TestNextRunBacksOffAfterFailuresButNeverBeyondTheInterval(t *testing.T) {
	t.Parallel()
	start := mustTime(t, "2026-10-24T00:00:00Z")
	base := func(kv ...any) map[string]any {
		m := map[string]any{"last_run_at": "2026-10-24T00:00:00Z"}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	}
	cases := []struct {
		entry    map[string]any
		interval int64
		baseline any
		want     *time.Time
	}{
		{base("last_status", "success"), 3600, nil, ptr(start.Add(time.Hour))},
		{base("last_status", "failed", "consecutive_failures", 1), 86400, nil, ptr(start.Add(5 * time.Minute))},
		{base("last_status", "failed", "consecutive_failures", 3), 86400, nil, ptr(start.Add(20 * time.Minute))},
		{base("last_status", "failed", "consecutive_failures", 30), 3600, nil, ptr(start.Add(time.Hour))},
		{base(), 0, nil, nil},
		{map[string]any{}, 3600, "2026-10-24T01:00:00Z", ptr(start.Add(2 * time.Hour))},
	}
	for i, c := range cases {
		got, err := nextRunAt(c.entry, c.interval, c.baseline, nil)
		if err != nil {
			t.Fatal(err)
		}
		if (got == nil) != (c.want == nil) || (got != nil && !got.Equal(*c.want)) {
			t.Errorf("case %d: %v, want %v", i, got, c.want)
		}
	}
}

func ptr[T any](v T) *T { return &v }

// noRules is the policy tests' fixture: no library rules.
func (f *fixture) noRules() {
	f.updateShared(func(shared *model.Shared) { shared.RuleLibrary = []model.RuleLibraryItem{} })
}

// test_scheduled_tasks.py::test_failure_reasons_are_readable_and_never_render_the_address
func TestFailureReasonsAreReadableAndNeverRenderTheAddress(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		want string
	}{
		{&substore.RequestError{Class: "MissingSchema", Message: "Invalid URL subscribe?token=private"}, "地址无效，请检查是否为完整的 http(s) 地址"},
		{&rulefetch.FetchError{Class: "ConnectionError", Message: "https://rules.test?key=private"}, "无法连接上游"},
		{&rulefetch.FetchError{Class: "HTTPError", Status: 403, Message: "403 for https://rules.test?key=private"}, "上游返回 HTTP 403"},
		{&PyError{Class: "RuntimeError", Message: "https://rules.test?key=private"}, "更新失败（RuntimeError）"},
		{&substore.SubscriptionFetchError{SubStoreError: &PyError{Class: "RuntimeError", Message: "x"},
			DirectError: &substore.RequestError{Class: "Timeout", Message: "y"}}, "连接上游超时"},
	}
	for _, c := range cases {
		if got := FailureReason(errorFromLegacy(c.err)); got != c.want {
			t.Errorf("%T %v: %q, want %q", c.err, c.err, got, c.want)
		}
	}
}

// listRows is the subscription and rule rows the way /api/tasks lists
// them, by key.
func (f *fixture) listRows() map[string]map[string]any {
	statuses, err := taskstatus.New(f.repo.DataDir()).Read()
	if err != nil {
		f.t.Fatal(err)
	}
	out := map[string]map[string]any{}
	for _, build := range []func(context.Context, taskstatus.Snapshot) ([]*tasks.Task, error){SubscriptionTasks, RuleTasks} {
		rows, err := build(f.ctx, statuses)
		if err != nil {
			f.t.Fatal(err)
		}
		for _, row := range rows {
			data, err := jsonresp.Encode(row)
			if err != nil {
				f.t.Fatal(err)
			}
			m, err := decodeMap(data)
			if err != nil {
				f.t.Fatal(err)
			}
			out[mapStr(m, "key")] = m
		}
	}
	return out
}

// test_scheduled_tasks.py::test_failed_rule_refresh_survives_a_reload_with_reason_and_retained_cache
func TestFailedRuleRefreshSurvivesAReloadWithReasonAndRetainedCache(t *testing.T) {
	f := setup(t, 86400)
	rule := ruleDecl(t)
	if _, err := rulefetch.WriteCache(f.ctx, refreshCacheRule(t, rule), rulefetch.Content{Data: []byte("+.old.test\n")}, nil); err != nil {
		t.Fatal(err)
	}
	_, _, err := RefreshRemote(f.ctx, rule, "schedule", RuleReadOptions{
		Fetch: func(context.Context, string) (rulefetch.Content, error) { return rulefetch.Content{}, timeoutError() },
	})
	if err == nil {
		t.Fatal("refresh succeeded")
	}
	// A restart or page reload.
	reopened, err := repo.Open(f.repo.DataDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	f.repo, f.ctx = reopened, repo.WithRepository(context.Background(), reopened)
	rows := f.listRows()
	row := rows["rule_source:rule"]
	want := map[string]any{
		"category": "rule_source", "name": "Streaming", "detail": "rules.test", "last_status": "failed",
		"last_error": "连接上游超时", "cache_retained": true, "failure_count": 1, "last_trigger": "schedule",
		"interval_seconds": 86400, "enabled": true, "editable": true, "can_run": true,
	}
	for key, value := range want {
		if !testjson.Equal(row[key], value) {
			t.Errorf("%s = %s, want %s", key, compactJSON(row[key]), compactJSON(value))
		}
	}
	if !pyTruthy(row["cache_updated_at"]) {
		t.Error("cache_updated_at")
	}
	if mustTime(t, mapStr(row, "next_run_at")).Sub(mustTime(t, mapStr(row, "last_run_at"))) != 5*time.Minute {
		t.Error(compactJSON(row))
	}
	if strings.Contains(compactJSON(row), "private") {
		t.Error("the address leaked")
	}
	cached, _ := rulefetch.ReadCache(f.ctx, refreshCacheRule(t, rule), nil)
	if string(cached) != "+.old.test\n" {
		t.Errorf("cache %q", cached)
	}
}

// test_scheduled_tasks.py::test_rule_without_valid_cache_reports_that_nothing_is_retained
func TestRuleWithoutValidCacheReportsThatNothingIsRetained(t *testing.T) {
	f := setup(t, 86400)
	_, _, err := RefreshRemote(f.ctx, ruleDecl(t), "schedule", RuleReadOptions{
		Fetch: func(context.Context, string) (rulefetch.Content, error) {
			return rulefetch.Content{Data: []byte("DOMAIN,bad.test\n")}, nil
		},
	})
	if err == nil {
		t.Fatal("refresh succeeded")
	}
	entry := f.status("rule_source:rule")
	if entry["cache_retained"] != false || !strings.Contains(mapStr(entry, "last_error"), "第 1 行") {
		t.Fatal(compactJSON(entry))
	}
}

// test_scheduled_tasks.py::test_device_reads_use_the_cache_and_only_a_missing_cache_downloads_now
func TestDeviceReadsUseTheCacheAndOnlyAMissingCacheDownloadsNow(t *testing.T) {
	f := setup(t, 86400)
	calls := 0
	fetch := func(body string) func(context.Context, string) (rulefetch.Content, error) {
		return func(context.Context, string) (rulefetch.Content, error) {
			calls++
			return rulefetch.Content{Data: []byte(body)}, nil
		}
	}
	rule := ruleDecl(t)
	content, stale, err := ReadRemote(f.ctx, rule, RuleReadOptions{Fetch: fetch("+.new.test\n")})
	if err != nil || string(content) != "+.new.test\n" || stale != nil {
		t.Fatal(content, stale, err)
	}
	if f.status("rule_source:rule")["last_trigger"] != "pull" {
		t.Fatal(compactJSON(f.status("rule_source:rule")))
	}
	content, stale, err = ReadRemote(f.ctx, rule, RuleReadOptions{Fetch: func(context.Context, string) (rulefetch.Content, error) {
		t.Error("cached")
		return rulefetch.Content{}, errors.New("cached")
	}})
	if err != nil || string(content) != "+.new.test\n" || stale != nil {
		t.Fatal(content, stale, err)
	}
	// A declaration outside the library has no task and refreshes on each read.
	outside := testjson.Decode[model.RuleLibraryItem](t, map[string]any{
		"id": "profile-only", "name": "Streaming", "source_type": "url", "url": "https://other.test/list",
		"behavior": "domain", "format": "text", "enabled": true,
	})
	calls = 0
	_, _, _ = ReadRemote(f.ctx, outside, RuleReadOptions{Fetch: fetch("+.other.test\n")})
	_, _, _ = ReadRemote(f.ctx, outside, RuleReadOptions{Fetch: fetch("+.other.test\n")})
	if calls != 2 || len(f.status("rule_source:profile-only")) != 0 {
		t.Fatal(calls, compactJSON(f.status("rule_source:profile-only")))
	}
}

// A failed last refresh of a library source marks the cached bytes stale
// (StaleRuleCache with the recorded reason).
func TestDeviceReadOfAFailedSourceIsStale(t *testing.T) {
	f := setup(t, 86400)
	rule := ruleDecl(t)
	_, _ = rulefetch.WriteCache(f.ctx, refreshCacheRule(t, rule), rulefetch.Content{Data: []byte("+.old.test\n")}, nil)
	_, _, _ = RefreshRemote(f.ctx, rule, "schedule", RuleReadOptions{
		Fetch: func(context.Context, string) (rulefetch.Content, error) { return rulefetch.Content{}, timeoutError() },
	})
	content, stale, err := ReadRemote(f.ctx, rule, RuleReadOptions{})
	var staleErr *StaleRuleCache
	if err != nil || string(content) != "+.old.test\n" || !errors.As(stale, &staleErr) || stale.Error() != "连接上游超时" {
		t.Fatal(content, stale, err)
	}
}

// test_scheduled_tasks.py::test_scheduler_skips_disabled_sources_and_sources_without_automatic_updates
func TestSchedulerSkipsDisabledSourcesAndSourcesWithoutAutomaticUpdates(t *testing.T) {
	f := setup(t, 86400)
	f.updateShared(func(shared *model.Shared) {
		zero := model.Int(0)
		shared.Subscriptions[0].Interval = &zero
		disabled := false
		shared.RuleLibrary[0].Enabled = &disabled
	})
	if n, err := RunRound(f.ctx, nil); n != 0 || err != nil {
		t.Fatal(n, err)
	}
}

// test_scheduled_tasks.py::test_rule_source_due_on_first_round_without_cache
func TestRuleSourceDueOnFirstRoundWithoutCache(t *testing.T) {
	f := setup(t, 0)
	f.transport.reply = func(string) (int, string, error) { return 200, "+.new.test\n", nil }
	if n, err := RunRound(f.ctx, nil); n != 1 || err != nil {
		t.Fatal(n, err)
	}
	cached, _ := rulefetch.ReadCache(f.ctx, refreshCacheRule(t, ruleDecl(t)), nil)
	if string(cached) != "+.new.test\n" {
		t.Fatalf("cache %q", cached)
	}
	if n, err := RunRound(f.ctx, nil); n != 0 || err != nil {
		t.Fatal(n, err)
	}
	if len(f.transport.urls) != 1 || f.transport.urls[0] != ruleURL {
		t.Fatal(f.transport.urls)
	}
}

// test_subscription_refresh_policy.py::test_auto_endpoints_read_cache_without_upstream_or_extending_age
// (fetch_subscription_source half; the provider endpoints are C17/C30/C31).
func TestAutoDeliveryReadsCacheWithoutUpstreamOrExtendingAge(t *testing.T) {
	for _, interval := range []int64{0, 259200} {
		f := setup(t, interval)
		before := f.cacheSource(120*time.Second, subURL)
		for range 2 {
			result, err := FetchSource(f.ctx, f.sub(), func(context.Context, string, string) (*SourceResult, error) {
				t.Error("unexpected upstream request")
				return nil, errors.New("no")
			})
			if err != nil {
				t.Fatal(err)
			}
			var parsed struct {
				Proxies []struct {
					Name string `yaml:"name"`
				} `yaml:"proxies"`
			}
			if err := yamlx.Unmarshal([]byte(result.Text), &parsed); err != nil {
				t.Fatal(err)
			}
			proxies := parsed.Proxies
			if result.Source != "cache" || len(proxies) != 1 || proxies[0].Name != "Remote" {
				t.Fatal(result)
			}
		}
		if !testjson.Equal(f.cache(), before) {
			t.Fatal("cache changed")
		}
	}
}

// test_subscription_refresh_policy.py::test_manual_only_without_cache_explains_action_without_fetching
// (fetch_subscription_source half).
func TestManualOnlyWithoutCacheExplainsActionWithoutFetching(t *testing.T) {
	f := setup(t, 0)
	_, err := FetchSource(f.ctx, f.sub(), nil)
	var ref *rules.StrategyReferenceError
	if !errors.As(err, &ref) || !strings.Contains(err.Error(), "手动更新") {
		t.Fatal(err)
	}
	if err.Error() != "订阅「Feed」已关闭自动更新，且没有当前订阅地址的可用缓存。请先在「订阅来源」中手动更新，再重新发布或同步。" {
		t.Fatal(err)
	}
}

// test_subscription_refresh_policy.py::test_expired_cache_is_served_and_the_scheduled_task_refreshes_it
func TestExpiredCacheIsServedAndTheScheduledTaskRefreshesIt(t *testing.T) {
	f := setup(t, 259200)
	f.noRules()
	before := f.cacheSource(259201*time.Second, subURL)
	result, err := FetchSource(f.ctx, f.sub(), nil)
	if err != nil || result.Source != "cache" || !testjson.Equal(f.cache(), before) {
		t.Fatal(result, err)
	}
	f.download(proxiesYAML(t), "direct_yaml", nil)
	f.clock.advance(time.Second)
	if n, err := RunRound(f.ctx, nil); n != 1 || err != nil {
		t.Fatal(n, err)
	}
	if len(f.fetches) != 1 || f.fetches[0] != "s "+subURL+" refresh=True" {
		t.Fatal(f.fetches)
	}
	if n, err := RunRound(f.ctx, nil); n != 0 || err != nil { // due again only after the interval
		t.Fatal(n, err)
	}
	entry := f.status("subscription:s")
	if entry["last_status"] != "success" || mapInt(entry, "run_count") != 1 || entry["last_trigger"] != "schedule" {
		t.Fatal(compactJSON(entry))
	}
	if !(f.cache().UpdatedAt > before.UpdatedAt) {
		t.Fatal(f.cache().UpdatedAt, before.UpdatedAt)
	}
}

// test_subscription_refresh_policy.py::test_failed_scheduled_refresh_keeps_nodes_and_retries_sooner
func TestFailedScheduledRefreshKeepsNodesAndRetriesSooner(t *testing.T) {
	f := setup(t, 86400)
	f.noRules()
	before := f.cacheSource(86401*time.Second, subURL)
	f.download("proxies: []", "direct_yaml", nil)
	if n, err := RunRound(f.ctx, nil); n != 1 || err != nil {
		t.Fatal(n, err)
	}
	if !testjson.Equal(f.cache(), before) {
		t.Fatal("cache changed")
	}
	entry := f.status("subscription:s")
	if entry["last_status"] != "failed" || entry["cache_retained"] != true ||
		entry["last_error"] != "订阅没有返回任何节点，已保留原有缓存" {
		t.Fatal(compactJSON(entry))
	}
	due, err := nextRunAt(entry, 86400, nil, nil)
	if err != nil || due.Sub(mustTime(t, mapStr(entry, "last_run_at"))) != RetrySeconds*time.Second {
		t.Fatal(due, err)
	}
}

// test_subscription_refresh_policy.py::test_dry_previews_share_download_across_restart_without_changing_validated_nodes
// (fetch_subscription_source half; prepare_subscription_provider is C17).
func TestDryPreviewsShareDownloadAcrossRestartWithoutChangingValidatedNodes(t *testing.T) {
	f := setup(t, 259200)
	calls := 0
	fetch := func(context.Context, string, string) (*SourceResult, error) {
		calls++
		return &SourceResult{Text: proxiesYAML(t), Source: "direct_yaml"}, nil
	}
	first, err := FetchSource(f.ctx, f.sub(), fetch)
	if err != nil {
		t.Fatal(err)
	}
	reopened, _ := repo.Open(f.repo.DataDir(), repo.Options{})
	f.repo, f.ctx = reopened, repo.WithRepository(context.Background(), reopened)
	second, err := FetchSource(f.ctx, f.sub(), fetch)
	if err != nil {
		t.Fatal(err)
	}
	if first.Text != second.Text || first.FetchedAt != second.FetchedAt || second.Source != "source_cache" || calls != 1 {
		t.Fatal(first, second, calls)
	}
	if f.cache() != nil {
		t.Fatal("validated nodes written")
	}
}

// test_subscription_refresh_policy.py::test_expired_download_is_refetched_and_manual_mode_never_uses_it_without_nodes
func TestExpiredDownloadIsRefetchedAndManualModeNeverUsesItWithoutNodes(t *testing.T) {
	f := setup(t, 60)
	calls := 0
	fetch := func(context.Context, string, string) (*SourceResult, error) {
		calls++
		return &SourceResult{Text: proxiesYAML(t), Source: "direct_yaml"}, nil
	}
	sub := f.sub()
	if _, err := FetchSource(f.ctx, sub, fetch); err != nil {
		t.Fatal(err)
	}
	raw, err := readSharedData(f.ctx, DownloadCachePath(subURL))
	if err != nil {
		t.Fatal(err)
	}
	entry, err := decodeMap(raw)
	if err != nil {
		t.Fatal(err)
	}
	entry["fetched_at"] = isoMicro(f.clock.Now().Add(-61 * time.Second))
	updated, err := jsonresp.Encode(entry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.WriteSharedText(f.ctx, DownloadCachePath(subURL), updated); err != nil {
		t.Fatal(err)
	}
	if _, err := FetchSource(f.ctx, sub, fetch); err != nil || calls != 2 {
		t.Fatal(err, calls)
	}
	zero := model.Int(0)
	sub.Interval = &zero
	_, err = FetchSource(f.ctx, sub, fetch)
	if err == nil || !strings.Contains(err.Error(), "手动更新") || calls != 2 {
		t.Fatal(err, calls)
	}
}

// test_subscription_refresh_policy.py::test_changed_url_in_manual_mode_needs_manual_refresh
func TestChangedURLInManualModeNeedsManualRefresh(t *testing.T) {
	f := setup(t, 0)
	f.cacheSource(0, subURL)
	sub := f.sub()
	sub.URL = subURL + "&new=1"
	_, err := FetchSource(f.ctx, sub, func(context.Context, string, string) (*SourceResult, error) {
		t.Error("no request")
		return nil, errors.New("no")
	})
	if err == nil || !strings.Contains(err.Error(), "当前订阅地址") {
		t.Fatal(err)
	}
}

// test_subscription_refresh_policy.py::test_invalid_source_intervals_are_rejected_without_saving
// and test_invalid_mihomo_sync_setting_does_not_save_yaml (the validator;
// the routes are C30/C34).
func TestInvalidSourceIntervalsAreRejected(t *testing.T) {
	bad := []any{nil, "", "0", true, -1, 1, 59, 60.5, 604801, 0.5}
	for _, value := range bad {
		if _, err := parseIntervalAny(value, ""); err == nil || err.Error() != "订阅源更新间隔必须为 0（关闭自动更新）或 60 ~ 604800 秒的整数" {
			t.Errorf("%s: %v", compactJSON(value), err)
		}
	}
	if _, err := parseIntervalAny(1, "Mihomo 订阅同步间隔"); err == nil || err.Error() != "Mihomo 订阅同步间隔必须为 0（关闭自动更新）或 60 ~ 604800 秒的整数" {
		t.Error(err)
	}
	for _, value := range []int64{0, 60, 3600, 604800} {
		if got, err := parseIntervalAny(value, ""); err != nil || got != value {
			t.Errorf("%d: %d %v", value, got, err)
		}
	}
}

// RefreshSource records the fetch and the run: success with the
// filtered node count and traffic, failure without a cache (not retained).
func TestRefreshSubscriptionRecordsHealthAndStatus(t *testing.T) {
	f := setup(t, 86400)
	f.download("", "", &substore.SubscriptionFetchError{DirectError: &substore.RequestError{Class: "HTTPError", Status: 403}})
	_, _, _, err := RefreshSource(f.ctx, f.sub(), "manual")
	if err == nil {
		t.Fatal("no error")
	}
	entry := f.status("subscription:s")
	_, hasCacheUpdated := entry["cache_updated_at"]
	if entry["last_error"] != "上游返回 HTTP 403" || entry["cache_retained"] != false || entry["last_trigger"] != "manual" ||
		hasCacheUpdated || mapInt(entry, "last_duration_ms") != 12 {
		t.Fatal(compactJSON(entry))
	}
	health, _ := ReadHealth(f.ctx)
	sEntry := health.Entry("s")
	if sEntry == nil || len(sEntry.History) != 1 || sEntry.History[0].Status != "fail" {
		t.Fatal(compactJSON(health))
	}
	traffic := testjson.Decode[Traffic](t, map[string]any{"upload": 1, "total": 5})
	f.env.Sources.Fetch = func(context.Context, string, string, substore.FetchOptions) (*SourceResult, error) {
		return &SourceResult{Text: proxiesYAML(t), Source: "direct_yaml", Traffic: &traffic}, nil
	}
	nodes, result, cache, err := RefreshSource(f.ctx, f.sub(), "schedule")
	if err != nil || len(nodes) != 1 || result.FetchedAt != "2026-10-24T00:00:00+00:00" || cache.UpdatedAt == nil || *cache.UpdatedAt != result.FetchedAt {
		t.Fatal(nodes, result, cache, err)
	}
	nodeParsed, err := testjson.Parse(mustMarshalBytes(t, nodes[0]))
	if err != nil {
		t.Fatal(err)
	}
	nodeID, _ := testjson.Path(nodeParsed, "id")
	subID, _ := testjson.Path(nodeParsed, "subscription_id")
	subName, _ := testjson.Path(nodeParsed, "subscription_name")
	if nodeID != "node_00000001" || subID != "s" || subName != "Feed" {
		t.Fatal(compactJSON(nodes[0]))
	}
	entry = f.status("subscription:s")
	_, hasRetained := entry["cache_retained"]
	if entry["last_status"] != "success" || entry["cache_updated_at"] != result.FetchedAt || hasRetained {
		t.Fatal(compactJSON(entry))
	}
	health, _ = ReadHealth(f.ctx)
	got := health.Entry("s")
	if got == nil || len(got.History) < 2 ||
		!testjson.Equal(got.History[1], map[string]any{"at": "2026-10-24T00:00:00Z", "status": "ok", "count": 1}) ||
		!testjson.Equal(got.Traffic, map[string]any{"upload": 1, "total": 5}) ||
		got.TrafficAt == nil || *got.TrafficAt != "2026-10-24T00:00:00Z" {
		t.Fatal(compactJSON(got))
	}
	downloadedRaw, err := readSharedData(f.ctx, DownloadCachePath(subURL))
	if err != nil {
		t.Fatal(err)
	}
	downloaded, err := decodeMap(downloadedRaw)
	if err != nil || downloaded["text"] != proxiesYAML(t) || downloaded["fetched_at"] != result.FetchedAt {
		t.Fatal(downloaded, err)
	}
}

// Run now: the background refresh claims the task (a second start is
// busy), runs with trigger "manual" and releases it.
func TestRunNowClaimsTheTask(t *testing.T) {
	f := setup(t, 86400)
	gate := make(chan struct{})
	f.env.Sources.Fetch = func(ctx context.Context, _ string, _ string, _ substore.FetchOptions) (*SourceResult, error) {
		<-gate
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return &SourceResult{Text: proxiesYAML(t), Source: "direct_yaml"}, nil
	}
	request, cancel := context.WithCancel(f.ctx)
	defer cancel()
	key, err := RunResourceNow(request, "subscription", "s")
	cancel()
	if err != nil || key != "subscription:s" || !IsRunning(key) {
		t.Fatal(key, err)
	}
	var busy *TaskBusy
	if _, err := RunResourceNow(f.ctx, "subscription", "s"); !errors.As(err, &busy) || err.Error() != msgTaskBusy {
		t.Fatal(err)
	}
	if err := runTask(f.ctx, "subscription:s", "s"); err == nil || err.Error() != msgTaskBusy {
		t.Fatal(err)
	}
	close(gate)
	waitIdle(t, key)
	if f.status(key)["last_trigger"] != "manual" || f.status(key)["last_status"] != "success" {
		t.Fatal(compactJSON(f.status(key)))
	}
	var lookup *NotFoundError
	if _, err := RunResourceNow(f.ctx, "subscription", "gone"); !errors.As(err, &lookup) {
		t.Fatal(err)
	}
	f.updateShared(func(shared *model.Shared) { shared.Subscriptions[0].URL = "" })
	if _, err := RunResourceNow(f.ctx, "subscription", "s"); err == nil || err.Error() != "这个来源没有订阅地址，不需要更新" {
		t.Fatal(err)
	}
}

func waitIdle(t *testing.T, key string) {
	deadline := time.Now().Add(10 * time.Second)
	for IsRunning(key) {
		if time.Now().After(deadline) {
			t.Fatal("still running")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Instance ownership must not cause the scheduler to skip its rounds.
func TestLoopRunsUnderTheInstanceLock(t *testing.T) {
	f := setup(t, 60)
	f.updateShared(func(shared *model.Shared) {
		disabled := false
		shared.RuleLibrary[0].Enabled = &disabled
	})
	f.download(proxiesYAML(t), "direct_yaml", nil)
	owner, err := instance.Acquire(context.Background(), f.repo.DataDir())
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Release()
	clock := lifecycletest.NewClock(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	reg := lifecycle.NewRegistry()
	spec := periodic()
	original := spec.Round
	rounds := make(chan time.Time, 3)
	spec.Round = func(ctx context.Context) error {
		err := original(ctx)
		clock.Advance(7 * time.Second) // round duration is excluded from the pause
		rounds <- clock.Now()
		return err
	}
	reg.RegisterPeriodic(spec)
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: reg, Clock: clock})
	runtime.Start(f.ctx)
	defer runtime.Shutdown(context.Background())
	// The first round follows FirstDelay; the second waits for the next due
	// time, here beyond MaxPause, so MaxPause (O17: paced by due times).
	for _, pause := range []time.Duration{FirstDelaySeconds * time.Second, MaxPause} {
		awaitRefresh(t, func() bool { return clock.Pending() == 1 })
		clock.Advance(pause - time.Second)
		select {
		case <-rounds:
			t.Fatal("early round")
		default:
		}
		clock.Advance(time.Second)
		select {
		case <-rounds:
		case <-time.After(3 * time.Second):
			t.Fatal("missing round under instance lock")
		}
		if err := runtime.WaitIdle(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if entry := f.status(SourceTaskKey("s")); mapInt(entry, "run_count") != 1 || mapStr(entry, "last_trigger") != "schedule" {
		t.Fatal("scheduler did not refresh under the instance lock", entry)
	}
}

func awaitRefresh(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition timed out")
		}
		time.Sleep(time.Millisecond)
	}
}

// test_remote_rule_consumers.py::test_rule_proxy_uses_valid_cache_after_invalid_refresh
// (the refresh half; the MosDNS rule proxy is C28): an invalid response is
// rejected, the valid cache kept, and the run recorded as failed with the
// cache retained and the offending line in the reason.
func TestInvalidRefreshKeepsTheValidCache(t *testing.T) {
	f := setup(t, 86400)
	rule := ruleDecl(t)
	_, _ = rulefetch.WriteCache(f.ctx, refreshCacheRule(t, rule), rulefetch.Content{Data: []byte("+.valid.example\n")}, nil)
	_, _, err := RefreshRemote(f.ctx, rule, "schedule", RuleReadOptions{
		Fetch: func(context.Context, string) (rulefetch.Content, error) {
			return rulefetch.Content{Data: []byte("DOMAIN,bad.example")}, nil
		},
	})
	var invalid *rulefetch.RemoteRuleValidationError
	if !errors.As(err, &invalid) {
		t.Fatal(err)
	}
	cached, _ := rulefetch.ReadCache(f.ctx, refreshCacheRule(t, rule), nil)
	entry := f.status("rule_source:rule")
	if string(cached) != "+.valid.example\n" || entry["last_status"] != "failed" || entry["cache_retained"] != true ||
		!strings.Contains(mapStr(entry, "last_error"), "DOMAIN,bad.example") || !pyTruthy(entry["cache_updated_at"]) {
		t.Fatal(string(cached), compactJSON(entry))
	}
	// A device read now gets the valid bytes marked stale.
	content, stale, err := ReadRemote(f.ctx, rule, RuleReadOptions{})
	if err != nil || string(content) != "+.valid.example\n" || stale == nil || !strings.Contains(stale.Error(), "DOMAIN,bad.example") {
		t.Fatal(content, stale, err)
	}
}
