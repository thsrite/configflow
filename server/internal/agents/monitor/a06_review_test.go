package monitor

// The adversarial review corpus of testdata/gen_a06_review.py: randomised
// CPython 3.11 results (synthetic values) for metrics texts, baseline
// arithmetic, ip_address, upstream labels, request parameters, ranking
// sections, the clients / queries / query / overview snapshot / live
// rankings answers after _request, state validation, raw state files and
// target digests.

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
)

func loadReview(t *testing.T, fields ...string) *jsonx.Object {
	t.Helper()
	return referenceFields(t, "testdata/a06_review.json.gz", fields...)
}

// failures bounds the reported differences per test.
type failures struct {
	t     *testing.T
	count int
}

func (f *failures) add(format string, args ...any) {
	f.t.Helper()
	f.count++
	if f.count <= 15 {
		f.t.Errorf(format, args...)
	}
}

func (f *failures) done(total int) {
	f.t.Helper()
	if f.count > 0 {
		f.t.Errorf("%d of %d cases differ", f.count, total)
	}
}

func parseText(t *testing.T, text string) any {
	t.Helper()
	value, err := jsonx.ParseString(text)
	if err != nil {
		t.Fatalf("%s: %v", text, err)
	}
	return value
}

func errorResult(err error) *jsonx.Object {
	var e *Error
	if errors.As(err, &e) {
		return jsonx.Obj("error", e.Code, "status", e.Status, "message", e.Message)
	}
	name := err.Error()
	if i := strings.Index(name, ":"); i >= 0 {
		name = name[:i]
	}
	return jsonx.Obj("error", name)
}

func TestReviewMetrics(t *testing.T) {
	ref := loadReview(t, "metrics")
	f := &failures{t: t}
	all := cases(ref, "metrics")
	for _, c := range all {
		text := c.GetString("text")
		var result any
		parsed, err := ParseMetrics(text)
		if err != nil {
			result = jsonx.Obj("error", err.Error())
		} else {
			summary, _ := SummarizeMetrics(text)
			result = jsonx.Obj("parsed", parsed, "summary", summary)
		}
		if got := dumpSorted(t, result); got != c.GetString("result") {
			f.add("%q:\n got  %s\n want %s", text, got, c.GetString("result"))
		}
	}
	f.done(len(all))
}

func TestReviewBaselineArithmetic(t *testing.T) {
	ref := loadReview(t, "baseline")
	f := &failures{t: t}
	all := cases(ref, "baseline")
	for _, c := range all {
		current := parseText(t, c.GetString("current")).(*jsonx.Object)
		baseline := parseText(t, c.GetString("baseline")).(*jsonx.Object)
		carried := parseText(t, c.GetString("carried")).(*jsonx.Object)
		var result any
		adjusted, partial, err := ApplyMetricsBaseline(current, baseline)
		if err != nil {
			result = errorResult(err)
		} else {
			added := AddObservations(adjusted, carried)
			result = jsonx.Obj("restarted", MetricsCountersRestarted(current, baseline), "adjusted", adjusted,
				"partial", partial, "zero", ZeroBaseline(current), "added", added,
				"summary", SummarizeObservations(added), "raw_summary", SummarizeObservations(current))
		}
		if got := dumpSorted(t, result); got != c.GetString("result") {
			f.add("current=%s baseline=%s carried=%s:\n got  %s\n want %s", c.GetString("current"),
				c.GetString("baseline"), c.GetString("carried"), got, c.GetString("result"))
		}
	}
	f.done(len(all))
}

func TestReviewIPAddresses(t *testing.T) {
	ref := loadReview(t, "ip")
	f := &failures{t: t}
	all := ref.GetList("ip")
	for _, item := range all {
		pair := item.([]any)
		canonical, _, ok := pyIPAddress(pair[0].(string))
		var got any
		if ok {
			got = canonical
		}
		if !jsonx.Equal(got, pair[1]) {
			f.add("%q: got %s want %s", pair[0], jsonx.Repr(got), jsonx.Repr(pair[1]))
		}
	}
	f.done(len(all))
}

// labelEdgeCase reports the inputs whose upstream label may differ from
// Python since N2 (net/url instead of urlsplit), all display-only: a
// percent sign in the authority (IPv6 zone written "%eth0" or "%25eth0",
// stray "%"), unbalanced brackets, an IPvFuture literal, a space in the host
// or an unbracketed "host::port", and non-ASCII
// text (CPython-only case mappings such as "İ" and final sigma, NFKC-unsafe
// characters such as "℀").
func labelEdgeCase(input any) bool {
	text, ok := input.(string)
	if !ok {
		return false
	}
	for _, r := range text {
		if r >= 0x80 {
			return true
		}
	}
	return strings.Contains(text, "%") || strings.Count(text, "[") != strings.Count(text, "]") || strings.Contains(text, "[v") ||
		strings.Contains(text, " ") || (strings.Contains(text, "::") && !strings.Contains(text, "["))
}

func TestReviewUpstreamLabels(t *testing.T) {
	ref := loadReview(t, "upstream")
	f := &failures{t: t}
	all := ref.GetList("upstream")
	for _, item := range all {
		pair := item.([]any)
		if got := displayUpstream(pair[0]); got != pair[1] && !labelEdgeCase(pair[0]) {
			f.add("%s: got %q want %q", jsonx.Repr(pair[0]), got, pair[1])
		}
	}
	f.done(len(all))
}

func TestReviewRequestParameters(t *testing.T) {
	ref := loadReview(t, "client_params", "dashboard_params", "query_params")
	for _, c := range cases(ref, "query_params") {
		result, err := QueryParams(pairs(c.Value("args")), c.GetList("extra")[0] == true)
		checkParams(t, "query_params", c, result, err)
	}
	for _, c := range cases(ref, "client_params") {
		result, err := ClientParams(pairs(c.Value("args")))
		checkParams(t, "client_params", c, result, err)
	}
	for _, c := range cases(ref, "dashboard_params") {
		result, err := dashboardParams(pairs(c.Value("args")))
		checkParams(t, "dashboard_params", c, result, err)
	}
}

func TestReviewRankingSections(t *testing.T) {
	ref := loadReview(t, "ranking_section")
	f := &failures{t: t}
	all := cases(ref, "ranking_section")
	for _, c := range all {
		var result any
		section, err := rankingSection(parseText(t, c.GetString("value")), c.GetString("kind"))
		if err != nil {
			result = jsonx.Obj("error", err.(*Error).Code)
		} else {
			result = section
		}
		if got := dumpSorted(t, result); got != c.GetString("result") {
			f.add("%s %s:\n got  %s\n want %s", c.GetString("kind"), c.GetString("value"), got, c.GetString("result"))
		}
	}
	f.done(len(all))
}

// answering replaces doRequest with an Agent answering dataText and returns
// the calls seen, as gen_a06_review.with_answer records them.
func answering(t *testing.T, dataText string) *[]any {
	t.Helper()
	seen := &[]any{}
	previous := doRequest
	doRequest = func(_ context.Context, _ Manager, _ *jsonx.Object, endpoint string, params query, minVersion string) (*jsonx.Object, error) {
		list := []any{}
		for _, pair := range params {
			list = append(list, []any{pair[0], pair[1]})
		}
		*seen = append(*seen, jsonx.Obj("endpoint", endpoint, "params", list, "min_version", minVersion))
		return parseText(t, dataText).(*jsonx.Object), nil
	}
	t.Cleanup(func() { doRequest = previous })
	return seen
}

func reviewAgent(version string) *jsonx.Object {
	return jsonx.Obj("id", "oxi", "name", "DNS", "host", "127.0.0.1", "port", 8082, "profile_id", "default",
		"service_type", "oxidns", "version", version, "status", "online", "token", "tok")
}

func answerResult(t *testing.T, result any, err error, seen *[]any) string {
	t.Helper()
	if err != nil {
		result = errorResult(err)
	}
	return dumpSorted(t, jsonx.Obj("result", result, "seen", *seen))
}

func TestReviewClientsQueriesAndQuery(t *testing.T) {
	ref := loadReview(t, "clients", "queries", "query")
	f := &failures{t: t}
	all := cases(ref, "clients")
	for _, c := range all {
		seen := answering(t, c.GetString("data"))
		m := &fakeManager{agents: map[string]*jsonx.Object{"oxi": reviewAgent(c.GetString("version"))}, dataDir: t.TempDir()}
		result, err := Clients(ctx, m, "oxi", pairs(c.Value("args")))
		if got := answerResult(t, result, err, seen); got != c.GetString("result") {
			f.add("clients %s %s %s:\n got  %s\n want %s", c.GetString("version"), jsonx.Repr(c.Value("args")),
				c.GetString("data"), got, c.GetString("result"))
		}
	}
	for _, c := range cases(ref, "queries") {
		seen := answering(t, c.GetString("data"))
		m := &fakeManager{agents: map[string]*jsonx.Object{"oxi": reviewAgent(c.GetString("version"))}, dataDir: t.TempDir()}
		result, err := Queries(ctx, m, "oxi", pairs(c.Value("args")))
		if want, bound := nativeStateExpectation(t, "queries", c); bound {
			if got := answerResult(t, result, err, seen); got != want {
				t.Errorf("native JSON refusal: got %s want %s", got, want)
			}
			continue
		}
		if got := answerResult(t, result, err, seen); got != c.GetString("result") {
			f.add("queries %s %s:\n got  %s\n want %s", jsonx.Repr(c.Value("args")), c.GetString("data"), got, c.GetString("result"))
		}
	}
	for _, c := range cases(ref, "query") {
		seen := answering(t, c.GetString("data"))
		m := &fakeManager{agents: map[string]*jsonx.Object{"oxi": reviewAgent("1.8.20-go")}, dataDir: t.TempDir()}
		result, err := Query(ctx, m, "oxi", c.GetString("record_id"), pairs(c.Value("args")))
		if got := answerResult(t, result, err, seen); got != c.GetString("result") {
			f.add("query %s %s:\n got  %s\n want %s", c.GetString("record_id"), c.GetString("data"), got, c.GetString("result"))
		}
	}
	f.done(len(all) + len(ref.GetList("queries")) + len(ref.GetList("query")))
}

func TestReviewOverviewSnapshots(t *testing.T) {
	ref := loadReview(t, "overview_snapshot")
	f := &failures{t: t}
	all := cases(ref, "overview_snapshot")
	for _, c := range all {
		seen := answering(t, c.GetString("data"))
		m := &fakeManager{agents: map[string]*jsonx.Object{}, dataDir: t.TempDir()}
		var result any
		snap, err := overviewSnapshot(ctx, m, reviewAgent("1.8.20-go"), c.Value("dashboard") == true)
		if err == nil {
			summary := SummarizeObservations(snap.observations)
			var upstreams []any
			if upstreams, err = publicUpstreams(summary.GetList("upstreams"), snap.labels); err == nil {
				summary.Set("upstreams", upstreams)
				result = jsonx.Obj("core", snap.core, "recorders", snap.recorders, "summary", summary)
			}
		}
		if got := answerResult(t, result, err, seen); got != c.GetString("result") {
			f.add("dashboard=%v %s:\n got  %s\n want %s", c.Value("dashboard"), c.GetString("data"), got, c.GetString("result"))
		}
	}
	f.done(len(all))
}

func TestReviewDashboardRankings(t *testing.T) {
	ref := loadReview(t, "dashboard_rankings")
	f := &failures{t: t}
	all := cases(ref, "dashboard_rankings")
	for _, c := range all {
		seen := answering(t, c.GetString("data"))
		m := &fakeManager{agents: map[string]*jsonx.Object{}, dataDir: t.TempDir()}
		result, err := dashboardRankings(ctx, m, reviewAgent("1.8.20-go"), pairs(c.Value("params")), c.GetObject("snapshot"),
			c.GetObject("state"), c.GetList("recorders"))
		if got := answerResult(t, result, err, seen); got != c.GetString("result") {
			f.add("params=%s recorders=%s %s:\n got  %s\n want %s", jsonx.Repr(c.Value("params")),
				jsonx.Repr(c.Value("recorders")), c.GetString("data"), got, c.GetString("result"))
		}
	}
	f.done(len(all))
}

func TestReviewStateValidation(t *testing.T) {
	ref := loadReview(t, "validate_state")
	f := &failures{t: t}
	all := ref.GetList("validate_state")
	for _, item := range all {
		pair := item.([]any)
		want := pair[1] == true // a TypeError reads as damaged, like False
		if got := ValidateState(parseText(t, pair[0].(string))); got != want {
			f.add("%s: got %v want %v", pair[0], got, pair[1])
		}
	}
	f.done(len(all))
}

func TestReviewStateFiles(t *testing.T) {
	ref := loadReview(t, "state_files")
	f := &failures{t: t}
	all := cases(ref, "state_files")
	store := NewStateStore(t.TempDir())
	path := filepath.Join(t.TempDir(), "state.json")
	for _, c := range all {
		raw := decodeHex(c.GetString("raw"))
		if strings.Contains(string(raw), `\ud800`) || strings.Contains(string(raw), "\xed\xa0\x80") {
			// Lone surrogates: the accepted jsonx deviation (U+FFFD / refused
			// bytes); only a hand-edited state file can hold them.
			continue
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		var result any
		state, err := store.read(path)
		var stateErr *StateError
		switch {
		case errors.As(err, &stateErr):
			result = jsonx.Obj("error", stateErr.Message)
		case err != nil:
			result = errorResult(err)
		default:
			result = jsonx.Obj("state", state)
		}
		if want, bound := nativeStateExpectation(t, "state_files", c); bound {
			if got := dumpSorted(t, result); got != want {
				t.Errorf("native duplicate state: got %s want %s", got, want)
			}
			continue
		}
		if got := dumpSorted(t, result); got != c.GetString("result") {
			shown := raw
			if len(shown) > 200 {
				shown = shown[:200]
			}
			f.add("%q:\n got  %s\n want %s", shown, got, c.GetString("result"))
		}
	}
	f.done(len(all))
}

func TestReviewTargetDigests(t *testing.T) {
	ref := loadReview(t, "target")
	f := &failures{t: t}
	all := ref.GetList("target")
	for _, item := range all {
		pair := item.([]any)
		digest, err := target(parseText(t, pair[0].(string)).(*jsonx.Object))
		if err != nil || digest != pair[1] {
			f.add("%s: got %s %v want %s", pair[0], digest, err, pair[1])
		}
	}
	f.done(len(all))
}

// TestScopedAgentHostIsReachedLikePython: _agent_origin keeps an IPv6
// scope ("[::1%1]"); requests reaches that Agent (glibc accepts a decimal
// zone), and a named zone on a global address fails resolution. Go's URL
// parser refused the "%" zone before the review fix in agents.HTTPClient.
func TestScopedAgentHostIsReachedLikePython(t *testing.T) {
	listener, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("no IPv6 loopback: %v", err)
	}
	var seen []string
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.RequestURI())
		w.Write([]byte(`{"ok": true, "recorder": "r", "sample_size": 1, "rows": [{"key": "192.0.2.1", "count": 1}]}`))
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	for _, c := range []struct {
		host string
		ok   bool
	}{{"::1%1", true}, {"[::1%1]", true}, {"::1%lo", false}} {
		agent := reviewAgent("1.8.20-go")
		agent.Set("host", c.host)
		agent.Set("port", port)
		m := &fakeManager{agents: map[string]*jsonx.Object{"oxi": agent}, dataDir: t.TempDir(), client: agents.HTTPClient{}}
		result, err := Clients(ctx, m, "oxi", nil)
		if c.ok && (err != nil || result.GetString("recorder") != "r") {
			t.Fatalf("%s: %v %v", c.host, result, err)
		}
		if !c.ok {
			wantMonitorError(t, err, "AGENT_UNREACHABLE", 502)
		}
	}
	if len(seen) != 2 || seen[0] != "/api/oxidns/clients" {
		t.Fatalf("%v", seen)
	}
}

// TestReviewOverviewFlows replays testdata/gen_a06_flows.py: random
// sequences of overview GETs and dashboard resets on one data directory
// (counters rising and restarting, core/configuration/runtime changes,
// clock regressions, token and version changes, rankings that fail, time
// out, change or answer for another recorder) with a scripted monotonic
// clock and numbered generations. Every answer, the Agent calls and the
// state file bytes after each step must equal Python's.
func TestReviewOverviewFlows(t *testing.T) {
	f, err := os.Open("testdata/a06_flows.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	previousRequest, previousClock, previousToken := doRequest, monotonic, tokenHex
	reuse, backoff := RankingReuseSeconds, RankingBackoffSeconds
	RankingReuseSeconds, RankingBackoffSeconds = 60, 180
	t.Cleanup(func() {
		doRequest, monotonic, tokenHex = previousRequest, previousClock, previousToken
		RankingReuseSeconds, RankingBackoffSeconds = reuse, backoff
	})
	fails := &failures{t: t}
	total := 0
	for index, item := range value.(*jsonx.Object).GetList("flows") {
		flow := item.(*jsonx.Object)
		ResetRankingsCache()
		counter := 0
		tokenHex = func() string {
			counter++
			return fmt.Sprintf("%032x", counter)
		}
		clock := 1000.0
		monotonic = func() float64 { return clock }
		m := &fakeManager{agents: map[string]*jsonx.Object{}, dataDir: t.TempDir()}
		store := NewStateStore(m.dataDir)
		outputs := flow.GetList("outputs")
		for i, s := range flow.GetList("steps") {
			total++
			step := s.(*jsonx.Object)
			want := outputs[i].(*jsonx.Object)
			clock += step.GetFloat("dt")
			m.mu.Lock()
			m.agents["oxi"] = step.GetObject("agent").DeepCopy()
			m.mu.Unlock()
			seen := []any{}
			doRequest = func(_ context.Context, _ Manager, _ *jsonx.Object, endpoint string, params query, minVersion string) (*jsonx.Object, error) {
				list := []any{}
				for _, pair := range params {
					list = append(list, []any{pair[0], pair[1]})
				}
				seen = append(seen, jsonx.Obj("endpoint", endpoint, "params", list, "min_version", minVersion))
				if endpoint == "overview" {
					return step.GetObject("overview").DeepCopy(), nil
				}
				mode := step.GetString("mode")
				if mode == "core_unreachable" || mode == "agent_unreachable" {
					return nil, NewError(strings.ToUpper(mode), 0, "")
				}
				answer := step.GetObject("rankings").DeepCopy()
				recorder, _ := params.get("recorder")
				if recorder == "" {
					recorder = "r"
					if recorders := jsonx.Path(step.Value("overview"), "capabilities", "query_recorders").([]any); len(recorders) > 0 {
						recorder = recorders[0].(string)
					}
				}
				answer.Set("recorder", recorder)
				return answer, nil
			}
			action := step.GetObject("action")
			var result any
			var err error
			if action.GetString("kind") == "overview" {
				result, err = Overview(ctx, m, "oxi", pairs(action.Value("args")))
			} else {
				result, err = ResetDashboard(ctx, m, "oxi", action.GetObject("body").DeepCopy())
			}
			if err != nil {
				result = errorResult(err)
			}
			var state any
			if content, err := os.ReadFile(store.Path("oxi")); err == nil {
				state = string(content)
			}
			got := jsonx.Obj("result", dumpSorted(t, result), "seen", dumpSorted(t, seen), "state", state)
			expected := jsonx.Obj("result", want.Value("result"), "seen", want.Value("seen"), "state", want.Value("state"))
			if !jsonx.Equal(got, expected) {
				fails.add("flow %d step %d %s:\n got  %s\n want %s", index, i, jsonx.Repr(action), jsonx.Repr(got), jsonx.Repr(expected))
				break
			}
		}
	}
	fails.done(total)
	if total < 2000 {
		t.Fatalf("only %d steps replayed", total)
	}
	t.Logf("%d flows, %d steps", len(value.(*jsonx.Object).GetList("flows")), total)
}
