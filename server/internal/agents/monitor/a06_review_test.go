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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"configflow/internal/model"
	"configflow/internal/testjson"
)

func loadReview(t *testing.T, fields ...string) map[string]any {
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

func errorResult(err error) map[string]any {
	var e *Error
	if errors.As(err, &e) {
		return map[string]any{"error": e.Code, "status": e.Status, "message": e.Message}
	}
	name := err.Error()
	if i := strings.Index(name, ":"); i >= 0 {
		name = name[:i]
	}
	return map[string]any{"error": name}
}

func TestReviewMetrics(t *testing.T) {
	ref := loadReview(t, "metrics")
	f := &failures{t: t}
	all := cases(ref, "metrics")
	for _, c := range all {
		text := asStr(c["text"])
		var result any
		parsed, err := ReadMetrics(text)
		if err != nil {
			result = map[string]any{"error": err.Error()}
		} else {
			result = map[string]any{"parsed": parsed, "summary": Summarize(parsed)}
		}
		if want, bound := asciiReviewExpectation(t, "metrics", c); bound {
			if diff := diffRefVal(t, result, want); diff != "" {
				t.Fatalf("ASCII native row: %s", diff)
			}
			continue
		}
		if want, bound := nativeStateExpectation(t, "metrics", c); bound {
			if diff := diffRefVal(t, result, want); diff != "" {
				t.Errorf("native metric: %s", diff)
			}
			continue
		}
		if diff := diffRefJSON(t, result, asStr(c["result"])); diff != "" {
			f.add("%q: %s", text, diff)
		}
	}
	f.done(len(all))
}

func TestReviewBaselineArithmetic(t *testing.T) {
	ref := loadReview(t, "baseline")
	f := &failures{t: t}
	all := cases(ref, "baseline")
	for _, c := range all {
		var result any
		var current, baseline, carried Observations
		err := jsondecode.Unmarshal([]byte(asStr(c["current"])), &current)
		if err == nil {
			err = jsondecode.Unmarshal([]byte(asStr(c["baseline"])), &baseline)
		}
		if err == nil {
			_ = jsondecode.Unmarshal([]byte(asStr(c["carried"])), &carried)
		}
		if err != nil {
			result = errorResult(err)
		} else {
			adjusted, partial, err := ApplyBaseline(&current, &baseline)
			if err != nil {
				result = errorResult(err)
			} else {
				added := CarryObservations(adjusted, &carried)
				var addedObs Observations
				_ = jsondecode.Unmarshal(mustMarshal(t, added), &addedObs)
				result = map[string]any{
					"restarted":   CountersRestarted(&current, &baseline),
					"adjusted":    adjusted,
					"partial":     partial,
					"zero":        ZeroObservations(&current),
					"added":       added,
					"summary":     Summarize(&addedObs),
					"raw_summary": Summarize(&current),
				}
			}
		}
		if diff := diffRefJSON(t, result, asStr(c["result"])); diff != "" {
			f.add("current=%s baseline=%s carried=%s: %s", asStr(c["current"]),
				asStr(c["baseline"]), asStr(c["carried"]), diff)
		}
	}
	f.done(len(all))
}

func TestReviewIPAddresses(t *testing.T) {
	ref := loadReview(t, "ip")
	f := &failures{t: t}
	all := asList(ref["ip"])
	for _, item := range all {
		pair := item.([]any)
		canonical, _, ok := parseIP(pair[0].(string))
		var got any
		if ok {
			got = canonical
		}
		if got != pair[1] {
			f.add("%q: got %#v want %#v", pair[0], got, pair[1])
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
	all := asList(ref["upstream"])
	for _, item := range all {
		pair := item.([]any)
		if got := displayUpstream(pair[0]); got != pair[1] && !labelEdgeCase(pair[0]) {
			f.add("%#v: got %q want %q", pair[0], got, pair[1])
		}
	}
	f.done(len(all))
}

func TestReviewRequestParameters(t *testing.T) {
	ref := loadReview(t, "client_params", "dashboard_params", "query_params")
	for _, c := range cases(ref, "query_params") {
		result, err := QueryParams(pairs(c["args"]), asList(c["extra"])[0] == true)
		checkParams(t, "query_params", c, result, err)
	}
	for _, c := range cases(ref, "client_params") {
		result, err := ClientParams(pairs(c["args"]))
		checkParams(t, "client_params", c, result, err)
	}
	for _, c := range cases(ref, "dashboard_params") {
		result, err := dashboardParams(pairs(c["args"]))
		checkParams(t, "dashboard_params", c, result, err)
	}
}

func TestReviewRankingSections(t *testing.T) {
	ref := loadReview(t, "ranking_section")
	f := &failures{t: t}
	all := cases(ref, "ranking_section")
	for _, c := range all {
		var result any
		section, err := decodeRankingSection([]byte(asStr(c["value"])), asStr(c["kind"]), 10)
		if err != nil {
			result = map[string]any{"error": err.(*Error).Code}
		} else {
			result = section
		}
		if diff := diffRefJSON(t, result, asStr(c["result"])); diff != "" {
			f.add("%s %s: %s", asStr(c["kind"]), asStr(c["value"]), diff)
		}
	}
	f.done(len(all))
}

// answering replaces requestNative with an Agent answering dataText and returns
// the calls seen, as gen_a06_review.with_answer records them.
func answering(t *testing.T, dataText string) *[]any {
	t.Helper()
	seen := &[]any{}
	previous := requestNative
	requestNative = func(_ context.Context, _ Registry, _ *model.Agent, endpoint string, params query, minVersion string) (model.Fields, error) {
		list := []any{}
		for _, pair := range params {
			list = append(list, []any{pair[0], pair[1]})
		}
		*seen = append(*seen, map[string]any{"endpoint": endpoint, "params": list, "min_version": minVersion})
		raw, _ := jsonresp.NormalizeNonFinite([]byte(dataText))
		var fields model.Fields
		if err := jsondecode.Unmarshal(raw, &fields); err != nil {
			return model.Fields{}, NewError("INVALID_RESPONSE", 0, "")
		}
		return fields, nil
	}
	t.Cleanup(func() { requestNative = previous })
	return seen
}

func reviewAgent(version string) *model.Agent {
	return mustAgentMap(map[string]any{
		"id": "oxi", "name": "DNS", "host": "127.0.0.1", "port": 8082, "profile_id": "default",
		"service_type": "oxidns", "version": version, "status": "online", "token": "tok",
	})
}

func answerPayload(result any, err error, seen *[]any) map[string]any {
	if err != nil {
		result = errorResult(err)
	}
	return map[string]any{"result": result, "seen": *seen}
}

func TestReviewClientsQueriesAndQuery(t *testing.T) {
	ref := loadReview(t, "clients", "queries", "query")
	f := &failures{t: t}
	all := cases(ref, "clients")
	for _, c := range all {
		seen := answering(t, asStr(c["data"]))
		m := &fakeManager{agents: map[string]*model.Agent{"oxi": reviewAgent(asStr(c["version"]))}, dataDir: t.TempDir()}
		result, err := ReadClients(ctx, m, "oxi", pairs(c["args"]))
		if diff := diffRefJSON(t, answerPayload(result, err, seen), asStr(c["result"])); diff != "" {
			f.add("clients %s %v %s: %s", asStr(c["version"]), c["args"], asStr(c["data"]), diff)
		}
	}
	for _, c := range cases(ref, "queries") {
		seen := answering(t, asStr(c["data"]))
		m := &fakeManager{agents: map[string]*model.Agent{"oxi": reviewAgent(asStr(c["version"]))}, dataDir: t.TempDir()}
		result, err := ReadQueries(ctx, m, "oxi", pairs(c["args"]))
		if diff := diffRefJSON(t, answerPayload(result, err, seen), asStr(c["result"])); diff != "" {
			f.add("queries %v %s: %s", c["args"], asStr(c["data"]), diff)
		}
	}
	for _, c := range cases(ref, "query") {
		seen := answering(t, asStr(c["data"]))
		m := &fakeManager{agents: map[string]*model.Agent{"oxi": reviewAgent("1.8.20-go")}, dataDir: t.TempDir()}
		result, err := ReadQuery(ctx, m, "oxi", asStr(c["record_id"]), pairs(c["args"]))
		if diff := diffRefJSON(t, answerPayload(result, err, seen), asStr(c["result"])); diff != "" {
			f.add("query %s %s: %s", asStr(c["record_id"]), asStr(c["data"]), diff)
		}
	}
	f.done(len(all) + len(asList(ref["queries"])) + len(asList(ref["query"])))
}

func TestReviewOverviewSnapshots(t *testing.T) {
	ref := loadReview(t, "overview_snapshot")
	f := &failures{t: t}
	all := cases(ref, "overview_snapshot")
	for _, c := range all {
		seen := answering(t, asStr(c["data"]))
		m := &fakeManager{agents: map[string]*model.Agent{}, dataDir: t.TempDir()}
		var result any
		snap, err := readOverviewSnapshot(ctx, m, reviewAgent("1.8.20-go"), c["dashboard"] == true)
		if err == nil {
			summary := toJSONMap(t, Summarize(snap.Observations))
			labels := make([]any, len(snap.Labels))
			for i, l := range snap.Labels {
				labels[i] = map[string]any{"plugin_tag": l.PluginTag, "upstream": l.Upstream, "address": l.Address}
			}
			var upstreams []any
			if upstreams, err = publicUpstreams(asList(summary["upstreams"]), labels); err == nil {
				summary["upstreams"] = upstreams
				recorders := snap.Recorders
				if recorders == nil {
					recorders = []string{}
				}
				result = map[string]any{"core": snap.Core, "recorders": recorders, "summary": summary}
			}
		}
		payload := answerPayload(result, err, seen)
		if want, bound := asciiReviewExpectation(t, "overview_snapshot", c); bound {
			if diff := diffRefVal(t, payload, want); diff != "" {
				t.Fatalf("ASCII native row: %s", diff)
			}
			continue
		}
		if want, bound := nativeStateExpectation(t, "overview_snapshot", c); bound {
			if diff := diffRefVal(t, payload, want); diff != "" {
				t.Errorf("native overview: %s", diff)
			}
			continue
		}
		if diff := diffRefJSON(t, payload, asStr(c["result"])); diff != "" {
			f.add("dashboard=%v %s: %s", c["dashboard"], asStr(c["data"]), diff)
		}
	}
	f.done(len(all))
}

func TestReviewDashboardRankings(t *testing.T) {
	ref := loadReview(t, "dashboard_rankings")
	f := &failures{t: t}
	all := cases(ref, "dashboard_rankings")
	for _, c := range all {
		seen := answering(t, asStr(c["data"]))
		m := &fakeManager{agents: map[string]*model.Agent{}, dataDir: t.TempDir()}
		result, err := dashboardRankings(ctx, m, reviewAgent("1.8.20-go"), pairs(c["params"]), asMap(c["snapshot"]),
			asMap(c["state"]), asList(c["recorders"]))
		if diff := diffRefJSON(t, answerPayload(result, err, seen), asStr(c["result"])); diff != "" {
			f.add("params=%v recorders=%v %s: %s", c["params"], c["recorders"], asStr(c["data"]), diff)
		}
	}
	f.done(len(all))
}

func TestReviewStateValidation(t *testing.T) {
	ref := loadReview(t, "validate_state")
	f := &failures{t: t}
	all := asList(ref["validate_state"])
	for _, item := range all {
		pair := item.([]any)
		want := pair[1] == true // a TypeError reads as damaged, like False
		_, err := DecodeState([]byte(pair[0].(string)))
		if got := err == nil; got != want {
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
		raw := decodeHex(asStr(c["raw"]))
		if strings.Contains(string(raw), `\ud800`) || strings.Contains(string(raw), "\xed\xa0\x80") {
			// Lone surrogates: the accepted deviation (U+FFFD / refused
			// bytes); only a hand-edited state file can hold them.
			continue
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		var result any
		state, err := store.readState(path)
		var stateErr *StateError
		switch {
		case errors.As(err, &stateErr):
			result = map[string]any{"error": stateErr.Message}
		case err != nil:
			result = errorResult(err)
		default:
			result = map[string]any{"state": state}
		}
		if want, bound := nativeStateExpectation(t, "state_files", c); bound {
			if diff := diffRefVal(t, result, want); diff != "" {
				t.Errorf("native duplicate state: %s", diff)
			}
			continue
		}
		if diff := diffRefJSON(t, result, asStr(c["result"])); diff != "" {
			shown := raw
			if len(shown) > 200 {
				shown = shown[:200]
			}
			f.add("%q: %s", shown, diff)
		}
	}
	f.done(len(all))
}

func TestReviewTargetDigests(t *testing.T) {
	ref := loadReview(t, "target")
	f := &failures{t: t}
	all := asList(ref["target"])
	refusedWant := testjson.Obj(testjson.Field{Key: "refused", Value: true})
	for _, item := range all {
		pair := item.([]any)
		var a model.Agent
		err := jsondecode.Unmarshal([]byte(pair[0].(string)), &a)
		var digest string
		if err == nil {
			digest, err = TargetIdentity(&a)
		}
		if want, bound := nativeStateExpectation(t, "target", pair); bound {
			var syntax *json.SyntaxError
			if testjson.Diff(refusedWant, want) != "" || err == nil || digest != "" || !errors.As(err, &syntax) {
				t.Errorf("invalid stored identity was hashed: digest=%s error=%v", digest, err)
			}
			continue
		}
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
		setAgentField(agent, "host", c.host)
		setAgentField(agent, "port", port)
		m := &fakeManager{agents: map[string]*model.Agent{"oxi": agent}, dataDir: t.TempDir(), client: agents.HTTPClient{}}
		result, err := ReadClients(ctx, m, "oxi", nil)
		if c.ok && (err != nil || result.Recorder != "r") {
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
	root := parseJSONMap(t, data)
	previousRequest, previousClock, previousToken := requestNative, monotonic, tokenHex
	reuse, backoff := RankingReuseSeconds, RankingBackoffSeconds
	RankingReuseSeconds, RankingBackoffSeconds = 60, 180
	t.Cleanup(func() {
		requestNative, monotonic, tokenHex = previousRequest, previousClock, previousToken
		RankingReuseSeconds, RankingBackoffSeconds = reuse, backoff
	})
	fails := &failures{t: t}
	total := 0
	for index, item := range asList(root["flows"]) {
		flow := asMap(item)
		ResetRankingsCache()
		counter := 0
		tokenHex = func() string {
			counter++
			return fmt.Sprintf("%032x", counter)
		}
		clock := 1000.0
		monotonic = func() float64 { return clock }
		m := &fakeManager{agents: map[string]*model.Agent{}, dataDir: t.TempDir()}
		store := NewStateStore(m.dataDir)
		outputs := asList(flow["outputs"])
		for i, s := range asList(flow["steps"]) {
			total++
			step := asMap(s)
			want := asMap(outputs[i])
			clock += asFloat(step["dt"])
			m.mu.Lock()
			m.agents["oxi"] = mustAgentMap(asMap(step["agent"]))
			m.mu.Unlock()
			seen := []any{}
			requestNative = func(_ context.Context, _ Registry, _ *model.Agent, endpoint string, params query, minVersion string) (model.Fields, error) {
				list := []any{}
				for _, pair := range params {
					list = append(list, []any{pair[0], pair[1]})
				}
				seen = append(seen, map[string]any{"endpoint": endpoint, "params": list, "min_version": minVersion})
				if endpoint == "overview" {
					return fieldsFromValue(cloneMap(asMap(step["overview"])))
				}
				mode := asStr(step["mode"])
				if mode == "core_unreachable" || mode == "agent_unreachable" {
					return model.Fields{}, NewError(strings.ToUpper(mode), 0, "")
				}
				answer := cloneMap(asMap(step["rankings"]))
				recorder, _ := params.get("recorder")
				if recorder == "" {
					recorder = "r"
					caps := asMap(asMap(step["overview"])["capabilities"])
					if recorders := asList(caps["query_recorders"]); len(recorders) > 0 {
						recorder = recorders[0].(string)
					}
				}
				answer["recorder"] = recorder
				return fieldsFromValue(answer)
			}
			action := asMap(step["action"])
			var result any
			var err error
			if asStr(action["kind"]) == "overview" {
				result, err = ReadOverview(ctx, m, "oxi", pairs(action["args"]))
			} else {
				result, err = ResetStatistics(ctx, m, "oxi", mustMarshal(t, action["body"]))
			}
			if err != nil {
				result = errorResult(err)
			}
			var state any
			if content, err := os.ReadFile(store.Path("oxi")); err == nil {
				state = string(content)
			}
			resDiff := diffRefJSON(t, result, asStr(want["result"]))
			seenDiff := diffRefJSON(t, seen, asStr(want["seen"]))
			if resDiff != "" || seenDiff != "" || state != want["state"] {
				fails.add("flow %d step %d %v:\n resultDiff=%s\n seenDiff=%s\n state=%v want=%v",
					index, i, action, resDiff, seenDiff, state, want["state"])
				break
			}
		}
	}
	fails.done(total)
	if total < 2000 {
		t.Fatalf("only %d steps replayed", total)
	}
	t.Logf("%d flows, %d steps", len(asList(root["flows"])), total)
}

type asciiReviewChange struct {
	Path      []string `json:"path"`
	EmptyList bool     `json:"empty_list"`
}

var asciiReviewOnce sync.Once
var asciiReviewRows map[string][]asciiReviewChange
var asciiReviewError error

func loadASCIIReview() {
	data, err := os.ReadFile("testdata/n811_ascii_native.json")
	if err != nil {
		asciiReviewError = err
		return
	}
	var fixture struct {
		SourceSHA string `json:"source_sha256"`
		Rows      []struct {
			Section string              `json:"section"`
			SHA     string              `json:"sha256"`
			Changes []asciiReviewChange `json:"changes"`
		} `json:"rows"`
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		asciiReviewError = err
		return
	}
	source, err := os.ReadFile("testdata/a06_review.json.gz")
	if err != nil {
		asciiReviewError = err
		return
	}
	digest := sha256.Sum256(source)
	if hex.EncodeToString(digest[:]) != fixture.SourceSHA || len(fixture.Rows) != 44 {
		asciiReviewError = fmt.Errorf("ASCII review archive/count binding changed")
		return
	}
	asciiReviewRows = map[string][]asciiReviewChange{}
	for _, row := range fixture.Rows {
		key := row.Section + ":" + row.SHA
		if len(row.Changes) == 0 || asciiReviewRows[key] != nil {
			asciiReviewError = fmt.Errorf("invalid ASCII row binding")
			return
		}
		asciiReviewRows[key] = row.Changes
	}
}

func asciiReviewExpectation(t *testing.T, section string, row map[string]any) (any, bool) {
	t.Helper()
	asciiReviewOnce.Do(loadASCIIReview)
	if asciiReviewError != nil {
		t.Fatal(asciiReviewError)
	}
	data, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	data, err = jsonresp.Canonical(data)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	changes, found := asciiReviewRows[section+":"+hex.EncodeToString(sum[:])]
	if !found {
		return nil, false
	}
	decoder := json.NewDecoder(strings.NewReader(asStr(row["result"])))
	decoder.UseNumber()
	var expected any
	if err := decoder.Decode(&expected); err != nil {
		t.Fatal(err)
	}
	for _, change := range changes {
		parent := expected
		for _, step := range change.Path[:len(change.Path)-1] {
			switch v := parent.(type) {
			case map[string]any:
				var ok bool
				parent, ok = v[step]
				if !ok {
					t.Fatal("missing native path", change.Path)
				}
			case []any:
				index, err := strconv.Atoi(step)
				if err != nil || index < 0 || index >= len(v) {
					t.Fatal("invalid native index", change.Path)
				}
				parent = v[index]
			default:
				t.Fatal("invalid native path", change.Path)
			}
		}
		object, ok := parent.(map[string]any)
		if !ok {
			t.Fatal("invalid native patch parent", change.Path)
		}
		key := change.Path[len(change.Path)-1]
		old, ok := object[key]
		if !ok {
			t.Fatal("missing native patch field", change.Path)
		}
		if change.EmptyList {
			list, ok := old.([]any)
			if !ok || len(list) != 1 {
				t.Fatal("invalid timestamp row expectation", change.Path)
			}
			object[key] = []any{}
		} else {
			if _, ok := old.(json.Number); !ok {
				t.Fatal("native numeric expectation was not numeric", change.Path)
			}
			object[key] = nil
		}
	}
	data, err = json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	value, err := testjson.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return value, true
}

func TestASCIIReviewBindingsHaveNoStaleRows(t *testing.T) {
	counts := map[string]int{}
	for _, section := range []string{"metrics", "overview_snapshot"} {
		for _, row := range cases(loadReview(t, section), section) {
			if _, found := asciiReviewExpectation(t, section, row); found {
				counts[section]++
			}
		}
	}
	if counts["metrics"] != 40 || counts["overview_snapshot"] != 4 {
		t.Fatal("stale/missing ASCII review rows", counts)
	}
}
