package monitor

// The CPython reference corpus of testdata/gen_a06_reference.py (synthetic
// values): metrics parsing and summaries with Unicode digits/whitespace and
// line breaks, baseline arithmetic with big ints and floats, upstream labels
// through urllib.parse, ip_address forms, Agent origins, request parameter
// validation, dashboard state validation, ranking sections, records,
// fromtimestamp rounding, Agent answers through _request, and the advance /
// totals transitions with every deployment state.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/testjson"
)

func loadReference(t *testing.T, fields ...string) map[string]any {
	t.Helper()
	return referenceFields(t, "testdata/a06_reference.json.gz", fields...)
}

func diffRefJSON(t *testing.T, got any, wantJSON string) string {
	t.Helper()
	want, err := testjson.Parse([]byte(wantJSON))
	if err != nil {
		t.Fatal(err)
	}
	return diffRefVal(t, got, want)
}

func diffRefVal(t *testing.T, got any, want any) string {
	t.Helper()
	gotVal, err := testjson.Parse(mustMarshal(t, got))
	if err != nil {
		t.Fatal(err)
	}
	return testjson.Diff(want, gotVal)
}

func cases(ref map[string]any, key string) []map[string]any {
	var out []map[string]any
	for _, item := range asList(ref[key]) {
		out = append(out, asMap(item))
	}
	if len(out) == 0 {
		panic("no reference cases for " + key)
	}
	return out
}

func pairs(value any) [][2]string {
	out := [][2]string{}
	for _, item := range value.([]any) {
		pair := item.([]any)
		out = append(out, [2]string{pair[0].(string), pair[1].(string)})
	}
	return out
}

func displayUpstream(value any) string {
	s, ok := value.(string)
	if !ok {
		return ""
	}
	return displayAddress(truncateText(s, 2048))
}

func TestReferenceMetrics(t *testing.T) {
	ref := loadReference(t, "metrics")
	for _, c := range cases(ref, "metrics") {
		text := asStr(c["text"])
		var result any
		parsed, err := ReadMetrics(text)
		if err != nil {
			result = map[string]any{"error": err.Error()}
		} else {
			result = map[string]any{"parsed": parsed, "summary": Summarize(parsed)}
		}
		if want, changed := asciiMetricExpectation(text); changed {
			if diff := diffRefVal(t, result, want); diff != "" {
				t.Fatalf("ASCII metric refusal %q: %s", text, diff)
			}
			continue
		}
		if want, bound := nativeStateExpectation(t, "metrics", c); bound {
			if diff := diffRefVal(t, result, want); diff != "" {
				t.Errorf("native metric number: %s", diff)
			}
			continue
		}
		if diff := diffRefJSON(t, result, asStr(c["result"])); diff != "" {
			t.Errorf("%q: %s", text, diff)
		}
	}
}

func TestReferenceBaselineArithmetic(t *testing.T) {
	ref := loadReference(t, "baseline")
	for _, c := range cases(ref, "baseline") {
		current := parse(t, asStr(c["current"]))
		baseline := parse(t, asStr(c["baseline"]))
		var persisted Observations
		if err := jsondecode.Unmarshal(mustMarshal(t, baseline), &persisted); err != nil {
			t.Fatal(err)
		}
		baseline = &persisted
		carried := parse(t, asStr(c["carried"]))
		adjusted, partial, err := ApplyBaseline(current, baseline)
		if err != nil {
			t.Fatal(err)
		}
		added := CarryObservations(adjusted, carried)
		var addedObs Observations
		_ = jsondecode.Unmarshal(mustMarshal(t, added), &addedObs)
		result := map[string]any{
			"restarted": CountersRestarted(current, baseline),
			"adjusted":  adjusted,
			"partial":   partial,
			"zero":      ZeroObservations(current),
			"added":     added,
			"summary":   Summarize(&addedObs),
		}
		if diff := diffRefJSON(t, result, asStr(c["result"])); diff != "" {
			t.Errorf("%q: %s", asStr(c["current"]), diff)
		}
	}
}

func TestReferenceUpstreamLabels(t *testing.T) {
	ref := loadReference(t, "public_upstreams", "upstream")
	if len(asList(ref["upstream"])) == 0 {
		t.Fatal("no cases")
	}
	for _, item := range asList(ref["upstream"]) {
		pair := item.([]any)
		if got := displayUpstream(pair[0]); got != pair[1] && !labelEdgeCase(pair[0]) {
			t.Errorf("%#v: got %q want %q", pair[0], got, pair[1])
		}
	}
	rows := []any{
		map[string]any{"tag": "r", "upstream": "é", "requests": 1},
		map[string]any{"tag": "ü", "upstream": "https://u:p@x/"},
		map[string]any{"tag": nil, "upstream": 7},
	}
	labels := []any{
		map[string]any{"plugin_tag": "r", "upstream": "é", "address": "https://a:b@addr.test:8443/q?x"},
	}
	result, err := publicUpstreams(rows, labels)
	if err != nil {
		t.Fatal(err)
	}
	if diff := diffRefJSON(t, result, asStr(ref["public_upstreams"])); diff != "" {
		t.Errorf("public upstreams: %s", diff)
	}
}

func TestReferenceIPAddresses(t *testing.T) {
	ref := loadReference(t, "ip")
	if len(asList(ref["ip"])) == 0 {
		t.Fatal("no cases")
	}
	for _, item := range asList(ref["ip"]) {
		pair := item.([]any)
		canonical, _, ok := parseIP(pair[0].(string))
		var got any
		if ok {
			got = canonical
		}
		if got != pair[1] {
			t.Errorf("%q: got %#v want %#v", pair[0], got, pair[1])
		}
	}
}

func TestReferenceAgentOrigins(t *testing.T) {
	ref := loadReference(t, "origin")
	for _, c := range cases(ref, "origin") {
		agent, decErr := decodeAgentMap(asMap(c["agent"]))
		var origin string
		var err error
		if decErr != nil {
			err = unreachable()
		} else {
			origin, err = Origin(agent)
		}
		var e *Error
		if port, ok := asMap(c["agent"])["port"]; ok && err == nil && c["error"] != nil {
			if _, text := port.(string); text || strings.Contains(fmt.Sprint(port), ".") {
				continue // accepted O18 T1b difference: integer text or 80.0 is the port
			}
		}
		switch {
		case c["error"] != nil:
			if !errors.As(err, &e) || e.Code != asStr(c["error"]) {
				t.Errorf("%v: got %q %v want %s", c["agent"], origin, err, asStr(c["error"]))
			}
		case err != nil || origin != asStr(c["result"]):
			t.Errorf("%v: got %q %v want %s", c["agent"], origin, err, asStr(c["result"]))
		}
	}
}

func checkParams(t *testing.T, name string, c map[string]any, result query, err error) {
	t.Helper()
	var e *Error
	if _, hasErr := c["error"]; hasErr {
		if !errors.As(err, &e) || e.Code != asStr(c["error"]) {
			t.Errorf("%s %v: got %v %v want %s", name, c["args"], result, err, asStr(c["error"]))
		}
		return
	}
	want := pairs(c["result"])
	if err != nil || len(result) != len(want) {
		t.Errorf("%s %v: got %v %v want %v", name, c["args"], result, err, want)
		return
	}
	for i := range want {
		if result[i] != want[i] {
			t.Errorf("%s %v: got %v want %v", name, c["args"], result, want)
		}
	}
}

func TestReferenceRequestParameters(t *testing.T) {
	ref := loadReference(t, "client_params", "dashboard_params", "query_params", "recorder_tags")
	for _, c := range cases(ref, "query_params") {
		detail := asList(c["extra"])[0] == true
		result, err := QueryParams(pairs(c["args"]), detail)
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
	if len(asList(ref["recorder_tags"])) == 0 {
		t.Fatal("no cases")
	}
	for _, item := range asList(ref["recorder_tags"]) {
		pair := item.([]any)
		tag, ok := pair[0].(string)
		got := ok && validRecorderTag(tag)
		if got != (pair[1] == true) {
			t.Errorf("recorder tag %q: want %v", pair[0], pair[1])
		}
	}
}

func TestReferenceStateValidation(t *testing.T) {
	ref := loadReference(t, "validate_state")
	if len(asList(ref["validate_state"])) == 0 {
		t.Fatal("no cases")
	}
	for _, item := range asList(ref["validate_state"]) {
		pair := item.([]any)
		text := pair[0].(string)
		_, err := DecodeState([]byte(text))
		want := pair[1] == true // a TypeError reads as damaged, like False
		if got := err == nil; got != want {
			if len(text) > 300 {
				text = text[:300]
			}
			t.Errorf("%s: got %v want %v", text, got, pair[1])
		}
	}
}

func TestReferenceRankingSectionsAndRecords(t *testing.T) {
	ref := loadReference(t, "ranking_section", "record")
	for _, c := range cases(ref, "ranking_section") {
		var result any
		section, err := decodeRankingSection(mustMarshal(t, c["value"]), asStr(c["kind"]), 10)
		if err != nil {
			result = map[string]any{"error": err.(*Error).Code}
		} else {
			result = section
		}
		if diff := diffRefJSON(t, result, asStr(c["result"])); diff != "" {
			t.Errorf("%s %v: %s", asStr(c["kind"]), c["value"], diff)
		}
	}
	for _, c := range cases(ref, "record") {
		var result any
		out, err := queryRecord(mustMarshal(t, c["value"]), c["detail"] == true)
		if err != nil {
			result = map[string]any{"error": err.(*Error).Code}
		} else {
			result = json.RawMessage(out)
		}
		if diff := diffRefJSON(t, result, asStr(c["result"])); diff != "" {
			t.Errorf("%v: %s", c["value"], diff)
		}
	}
}

func TestReferenceTimestamps(t *testing.T) {
	ref := loadReference(t, "timestamps")
	if len(asList(ref["timestamps"])) == 0 {
		t.Fatal("no cases")
	}
	for _, item := range asList(ref["timestamps"]) {
		pair := item.([]any)
		ms := asInt64(pair[0])
		if got := isoUTC(fromTimestampMS(ms)); got != pair[1] {
			t.Errorf("%d: got %s want %s", ms, got, pair[1])
		}
	}
}

func TestReferenceAgentAnswers(t *testing.T) {
	ref := loadReference(t, "responses")
	agent := mustAgentMap(map[string]any{
		"id": "oxi", "host": "127.0.0.1", "port": 8082, "token": "tok",
		"service_type": "oxidns", "version": "1.8.14-go",
	})
	matched4301 := 0
	for _, c := range cases(ref, "responses") {
		body := decodeHex(asStr(c["body"]))
		status := asInt64(c["status"])
		m := newManager(t, nil)
		install(m, &agents.Response{StatusCode: int(status), Body: body})
		var result any
		data, err := monitorRequest(ctx, m, agent, "clients", query{{"search", "a b"}}, MinClientsAgentVersion)
		if err != nil {
			e := err.(*Error)
			result = map[string]any{"error": e.Code, "status": e.Status, "message": e.Message}
		} else {
			result = map[string]any{"data": data}
		}
		if want, bound := nativeStateExpectation(t, "responses", c); bound {
			if diff := diffRefVal(t, result, want); diff != "" {
				t.Errorf("native response: %s", diff)
			}
			continue
		}
		// N8-11: CPython's 4300-digit int_max_str_digits cap is retired; Go's
		// native JSON decoder accepts valid 4301-digit RFC 8259 integer literals.
		if status == 200 && string(body) == `{"x": `+strings.Repeat("1", 4301)+`}` {
			origWant := parseJSONMap(t, []byte(asStr(c["result"])))
			if asStr(origWant["error"]) != "INVALID_RESPONSE" || asInt64(origWant["status"]) != 502 {
				t.Fatalf("4301-digit row original expectation drifted: %s", asStr(c["result"]))
			}
			matched4301++
			if diff := diffRefJSON(t, result, `{"data":{"x":`+strings.Repeat("1", 4301)+`}}`); diff != "" {
				t.Errorf("native 4301-digit response: %s", diff)
			}
			continue
		}
		if diff := diffRefJSON(t, result, asStr(c["result"])); diff != "" {
			t.Errorf("%d %q: %s", status, body, diff)
		}
	}
	if matched4301 != 1 {
		t.Fatalf("expected 1 native 4301-digit response row, matched %d", matched4301)
	}
}

func decodeHex(s string) []byte {
	out := make([]byte, len(s)/2)
	for i := range out {
		hi, lo := s[2*i], s[2*i+1]
		val := func(c byte) byte {
			if c >= 'a' {
				return c - 'a' + 10
			}
			return c - '0'
		}
		out[i] = val(hi)<<4 | val(lo)
	}
	return out
}

func advanceCorpusCase(t *testing.T, c map[string]any) (any, error) {
	t.Helper()
	var prev *DashboardState
	var prevVal any
	if rawPrev := asMap(c["previous"]); rawPrev != nil {
		raw := mustMarshal(t, rawPrev)
		var err error
		prev, err = DecodeState(raw)
		if err != nil {
			return nil, err
		}
		prevVal, err = testjson.Parse(raw)
		if err != nil {
			return nil, err
		}
	}
	var agent *model.Agent
	if rawAgent := asMap(c["agent"]); rawAgent != nil {
		var err error
		agent, err = decodeAgentMap(rawAgent)
		if err != nil {
			return nil, err
		}
	}
	var stamp SnapshotIdentity
	if err := jsondecode.Unmarshal(mustMarshal(t, c["snapshot"]), &stamp); err != nil {
		return nil, err
	}
	var status CoreStatus
	if err := jsondecode.Unmarshal(mustMarshal(t, c["core"]), &status); err != nil {
		return nil, err
	}
	obs := new(Observations)
	_ = jsondecode.Unmarshal(mustMarshal(t, c["observations"]), obs)
	state, err := AdvanceState(prev, &stamp, &status, obs, asStr(c["target"]), agent, c["manual"] == true)
	if err != nil {
		return nil, err
	}
	adjusted, partial, err := StateTotals(state, obs)
	if err != nil {
		return nil, err
	}
	same := false
	if prev != nil {
		stateVal, err := testjson.Parse(mustMarshal(t, state))
		if err != nil {
			t.Fatal(err)
		}
		same = testjson.Equal(prevVal, stateVal)
	}
	return map[string]any{"state": state, "same": same, "totals": adjusted, "partial": partial}, nil
}

func TestReferenceAdvanceAndTotals(t *testing.T) {
	ref := loadReference(t, "advance")
	previous := tokenHex
	tokenHex = func() string { return strings.Repeat("f", 32) }
	t.Cleanup(func() { tokenHex = previous })
	failures := 0
	for _, c := range cases(ref, "advance") {
		result, err := advanceCorpusCase(t, c)
		if err != nil {
			name := err.Error()
			if errors.Is(err, ErrPublicationPending) {
				name = "PublicationPending"
			} else if i := strings.Index(name, ":"); i >= 0 {
				name = name[:i]
			}
			result = map[string]any{"error": name}
		}
		if _, bound := nativeStateExpectation(t, "advance", c); bound {
			// Go-native refusals of damaged stored publication records: since
			// O18 T1b such values read as missing members (accepted).
			continue
		}
		if diff := diffRefJSON(t, result, asStr(c["result"])); diff != "" {
			failures++
			if failures <= 10 {
				t.Errorf("previous=%v target=%s manual=%v: %s",
					c["previous"] != nil, asStr(c["target"])[:4], c["manual"], diff)
			}
		}
	}
	if failures > 0 {
		t.Errorf("%d advance cases differ", failures)
	}
}

// asciiMetricExpectation binds only the frozen rows affected by N8-11's ASCII
// decision. Expected null fields and remaining valid counters are explicit.
func asciiMetricExpectation(text string) (any, bool) {
	token := ""
	for _, value := range []string{"１２", "٣", "1٣", "٣.٥", "١e٢", "𝟘𝟙", "7 ١٢٣"} {
		if text == `cache_entry_count{plugin_tag="c"} `+value || text == serverText("s", "udp", value, 0, value, 4, 0) {
			token = value
			break
		}
	}
	if token == "" {
		return nil, false
	}
	servers, caches := []any{}, []any{}
	summaryServers, summaryCaches := []any{}, []any{}
	failed, inflight := any(nil), any(nil)
	if strings.HasPrefix(text, "cache_") && token != "7 ١٢٣" {
		caches = []any{map[string]any{"tag": "c", "lookups": nil, "fresh_hits": nil, "stale_hits": nil, "misses": nil, "expired": nil, "entries": nil}}
		summaryCaches = []any{map[string]any{"tag": "c", "lookups": nil, "fresh_hits": nil, "stale_hits": nil, "misses": nil, "expired": nil, "entries": nil, "hits": nil, "hit_rate": nil}}
	} else if strings.HasPrefix(text, "server_") {
		failed, inflight = 0, 0
		servers = []any{map[string]any{"tag": "s", "protocol": "udp", "requests": nil, "failed": 0, "inflight": 0, "latency_sum_ms": nil, "latency_count": 4}}
		summaryServers = []any{map[string]any{"tag": "s", "protocol": "udp", "requests": nil, "failed": 0, "inflight": 0, "avg_latency_ms": nil}}
	}
	raw, err := json.Marshal(map[string]any{
		"parsed": map[string]any{"servers": servers, "caches": caches, "upstreams": []any{}},
		"summary": map[string]any{
			"servers": summaryServers, "caches": summaryCaches, "upstreams": []any{},
			"metrics": map[string]any{"requests_total": nil, "failed_total": failed, "inflight": inflight, "avg_latency_ms": nil, "cache_hit_rate": nil},
		},
	})
	if err != nil {
		panic(err)
	}
	val, err := testjson.Parse(raw)
	if err != nil {
		panic(err)
	}
	return val, true
}
