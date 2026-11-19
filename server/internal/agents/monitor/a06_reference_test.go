package monitor

// The CPython reference corpus of testdata/gen_a06_reference.py (synthetic
// values): metrics parsing and summaries with Unicode digits/whitespace and
// line breaks, baseline arithmetic with big ints and floats, upstream labels
// through urllib.parse, ip_address forms, Agent origins, request parameter
// validation, dashboard state validation, ranking sections, records,
// fromtimestamp rounding, Agent answers through _request, and the advance /
// totals transitions with every deployment state.

import (
	"errors"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
)

func loadReference(t *testing.T, fields ...string) *jsonx.Object {
	t.Helper()
	return referenceFields(t, "testdata/a06_reference.json.gz", fields...)
}

func dumpSorted(t *testing.T, v any) string {
	t.Helper()
	encoded, err := jsonx.Encode(v, jsonx.Options{SortKeys: true, EnsureASCII: true})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func cases(ref *jsonx.Object, key string) []*jsonx.Object {
	var out []*jsonx.Object
	for _, item := range ref.GetList(key) {
		out = append(out, item.(*jsonx.Object))
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

func TestReferenceMetrics(t *testing.T) {
	ref := loadReference(t, "metrics")
	for _, c := range cases(ref, "metrics") {
		text := c.GetString("text")
		var result any
		parsed, err := ParseMetrics(text)
		if err != nil {
			result = jsonx.Obj("error", err.Error())
		} else {
			summary, _ := SummarizeMetrics(text)
			result = jsonx.Obj("parsed", parsed, "summary", summary)
		}
		if want, bound := nativeStateExpectation(t, "metrics", c); bound {
			if got := dumpSorted(t, result); got != want {
				t.Errorf("native metric number: got %s want %s", got, want)
			}
			continue
		}
		if got := dumpSorted(t, result); got != c.GetString("result") {
			t.Errorf("%q:\n got  %s\n want %s", text, got, c.GetString("result"))
		}
	}
}

func TestReferenceBaselineArithmetic(t *testing.T) {
	ref := loadReference(t, "baseline")
	for _, c := range cases(ref, "baseline") {
		current := parse(t, c.GetString("current"))
		baseline := parse(t, c.GetString("baseline"))
		encoded, _ := jsonx.Compact(baseline)
		persisted, _ := jsonx.Parse(encoded)
		baseline = persisted.(*jsonx.Object)
		carried := parse(t, c.GetString("carried"))
		adjusted, partial, err := ApplyMetricsBaseline(current, baseline)
		if err != nil {
			t.Fatal(err)
		}
		added := AddObservations(adjusted, carried)
		result := jsonx.Obj("restarted", MetricsCountersRestarted(current, baseline), "adjusted", adjusted, "partial", partial,
			"zero", ZeroBaseline(current), "added", added, "summary", SummarizeObservations(AddObservations(adjusted, carried)))
		if got := dumpSorted(t, result); got != c.GetString("result") {
			t.Errorf("%q:\n got  %s\n want %s", c.GetString("current"), got, c.GetString("result"))
		}
	}
}

func TestReferenceUpstreamLabels(t *testing.T) {
	ref := loadReference(t, "public_upstreams", "upstream")
	if len(ref.GetList("upstream")) == 0 {
		t.Fatal("no cases")
	}
	for _, item := range ref.GetList("upstream") {
		pair := item.([]any)
		if got := displayUpstream(pair[0]); got != pair[1] && !labelEdgeCase(pair[0]) {
			t.Errorf("%s: got %q want %q", jsonx.Repr(pair[0]), got, pair[1])
		}
	}
	rows := []any{jsonx.Obj("tag", "r", "upstream", "é", "requests", 1), jsonx.Obj("tag", "ü", "upstream", "https://u:p@x/"),
		jsonx.Obj("tag", nil, "upstream", 7)}
	labels := []any{jsonx.Obj("plugin_tag", "r", "upstream", "é", "address", "https://a:b@addr.test:8443/q?x")}
	result, err := publicUpstreams(rows, labels)
	if err != nil {
		t.Fatal(err)
	}
	if got := dumpSorted(t, result); got != ref.GetString("public_upstreams") {
		t.Errorf("public upstreams:\n got  %s\n want %s", got, ref.GetString("public_upstreams"))
	}
}

func TestReferenceIPAddresses(t *testing.T) {
	ref := loadReference(t, "ip")
	if len(ref.GetList("ip")) == 0 {
		t.Fatal("no cases")
	}
	for _, item := range ref.GetList("ip") {
		pair := item.([]any)
		canonical, _, ok := pyIPAddress(pair[0].(string))
		var got any
		if ok {
			got = canonical
		}
		if !jsonx.Equal(got, pair[1]) {
			t.Errorf("%q: got %s want %s", pair[0], jsonx.Repr(got), jsonx.Repr(pair[1]))
		}
	}
}

func TestReferenceAgentOrigins(t *testing.T) {
	ref := loadReference(t, "origin")
	for _, c := range cases(ref, "origin") {
		origin, err := AgentOrigin(c.GetObject("agent"))
		var e *Error
		switch {
		case c.Has("error"):
			if !errors.As(err, &e) || e.Code != c.GetString("error") {
				t.Errorf("%s: got %q %v want %s", jsonx.Repr(c.Value("agent")), origin, err, c.GetString("error"))
			}
		case err != nil || origin != c.GetString("result"):
			t.Errorf("%s: got %q %v want %s", jsonx.Repr(c.Value("agent")), origin, err, c.GetString("result"))
		}
	}
}

func checkParams(t *testing.T, name string, c *jsonx.Object, result query, err error) {
	t.Helper()
	var e *Error
	if c.Has("error") {
		if !errors.As(err, &e) || e.Code != c.GetString("error") {
			t.Errorf("%s %s: got %v %v want %s", name, jsonx.Repr(c.Value("args")), result, err, c.GetString("error"))
		}
		return
	}
	want := pairs(c.Value("result"))
	if err != nil || len(result) != len(want) {
		t.Errorf("%s %s: got %v %v want %v", name, jsonx.Repr(c.Value("args")), result, err, want)
		return
	}
	for i := range want {
		if result[i] != want[i] {
			t.Errorf("%s %s: got %v want %v", name, jsonx.Repr(c.Value("args")), result, want)
		}
	}
}

func TestReferenceRequestParameters(t *testing.T) {
	ref := loadReference(t, "client_params", "dashboard_params", "query_params", "recorder_tags")
	for _, c := range cases(ref, "query_params") {
		detail := c.GetList("extra")[0] == true
		result, err := QueryParams(pairs(c.Value("args")), detail)
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
	if len(ref.GetList("recorder_tags")) == 0 {
		t.Fatal("no cases")
	}
	for _, item := range ref.GetList("recorder_tags") {
		pair := item.([]any)
		if ValidRecorderTag(pair[0]) != pair[1] {
			t.Errorf("recorder tag %q: want %v", pair[0], pair[1])
		}
	}
}

func TestReferenceStateValidation(t *testing.T) {
	ref := loadReference(t, "validate_state")
	if len(ref.GetList("validate_state")) == 0 {
		t.Fatal("no cases")
	}
	for _, item := range ref.GetList("validate_state") {
		pair := item.([]any)
		value, err := jsonx.ParseString(pair[0].(string))
		if err != nil {
			t.Fatal(err)
		}
		want := pair[1] == true // a TypeError reads as damaged, like False
		if got := ValidateState(value); got != want {
			text := pair[0].(string)
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
		section, err := rankingSection(c.Value("value"), c.GetString("kind"))
		if err != nil {
			result = jsonx.Obj("error", err.(*Error).Code)
		} else {
			result = section
		}
		if got := dumpSorted(t, result); got != c.GetString("result") {
			t.Errorf("%s %s:\n got  %s\n want %s", c.GetString("kind"), jsonx.Repr(c.Value("value")), got, c.GetString("result"))
		}
	}
	for _, c := range cases(ref, "record") {
		var result any
		out, err := record(c.Value("value"), c.Value("detail") == true)
		if err != nil {
			result = jsonx.Obj("error", err.(*Error).Code)
		} else {
			result = out
		}
		if got := dumpSorted(t, result); got != c.GetString("result") {
			t.Errorf("%s:\n got  %s\n want %s", jsonx.Repr(c.Value("value")), got, c.GetString("result"))
		}
	}
}

func TestReferenceTimestamps(t *testing.T) {
	ref := loadReference(t, "timestamps")
	if len(ref.GetList("timestamps")) == 0 {
		t.Fatal("no cases")
	}
	for _, item := range ref.GetList("timestamps") {
		pair := item.([]any)
		ms, _ := pair[0].(jsonx.Number).Int64()
		if got := isoUTC(fromTimestampMS(ms)); got != pair[1] {
			t.Errorf("%d: got %s want %s", ms, got, pair[1])
		}
	}
}

func TestReferenceAgentAnswers(t *testing.T) {
	ref := loadReference(t, "responses")
	agent := jsonx.Obj("id", "oxi", "host", "127.0.0.1", "port", 8082, "token", "tok", "service_type", "oxidns", "version", "1.8.14-go")
	for _, c := range cases(ref, "responses") {
		body := decodeHex(c.GetString("body"))
		status, _ := c.Value("status").(jsonx.Number).Int64()
		m := newManager(t, nil)
		install(m, &agents.Response{StatusCode: int(status), Body: body})
		var result any
		data, err := request(ctx, m, agent, "clients", query{{"search", "a b"}}, MinClientsAgentVersion)
		if err != nil {
			e := err.(*Error)
			result = jsonx.Obj("error", e.Code, "status", e.Status, "message", e.Message)
		} else {
			result = jsonx.Obj("data", data)
		}
		if want, bound := nativeStateExpectation(t, "responses", c); bound {
			if got := dumpSorted(t, result); got != want {
				t.Errorf("native response: got %s want %s", got, want)
			}
			continue
		}
		if got := dumpSorted(t, result); got != c.GetString("result") {
			t.Errorf("%d %q:\n got  %s\n want %s", status, body, got, c.GetString("result"))
		}
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

func TestReferenceAdvanceAndTotals(t *testing.T) {
	ref := loadReference(t, "advance")
	previous := tokenHex
	tokenHex = func() string { return strings.Repeat("f", 32) }
	t.Cleanup(func() { tokenHex = previous })
	failures := 0
	for _, c := range cases(ref, "advance") {
		prev, _ := c.Value("previous").(*jsonx.Object)
		agent, _ := c.Value("agent").(*jsonx.Object)
		var prevCopy *jsonx.Object
		if prev != nil {
			prevCopy = prev.DeepCopy()
		}
		var agentCopy *jsonx.Object
		if agent != nil {
			agentCopy = agent.DeepCopy()
		}
		var result any
		state, err := Advance(prevCopy, c.GetObject("snapshot"), c.GetObject("core"), c.GetObject("observations"),
			c.GetString("target"), agentCopy, c.Value("manual") == true)
		if err == nil {
			adjusted, partial, terr := Totals(state, c.GetObject("observations"))
			if terr != nil {
				err = terr
			} else {
				same := prev != nil && jsonx.Equal(state, prev)
				result = jsonx.Obj("state", state, "same", same, "totals", adjusted, "partial", partial)
			}
		}
		if err != nil {
			name := err.Error()
			if errors.Is(err, ErrPublicationPending) {
				name = "PublicationPending"
			} else if i := strings.Index(name, ":"); i >= 0 {
				name = name[:i]
			}
			result = jsonx.Obj("error", name)
		}
		if want, bound := nativeStateExpectation(t, "advance", c); bound {
			if got := dumpSorted(t, result); got != want {
				t.Errorf("native state refusal: got %s want %s", got, want)
			}
			continue
		}
		if got := dumpSorted(t, result); got != c.GetString("result") {
			failures++
			if failures <= 10 {
				t.Errorf("previous=%s snapshot=%s core=%s target=%s manual=%v agent=%s:\n got  %s\n want %s",
					jsonx.Repr(prev != nil), jsonx.Repr(c.Value("snapshot")), jsonx.Repr(c.Value("core")), c.GetString("target")[:4],
					c.Value("manual"), jsonx.Repr(c.Value("agent")), got, c.GetString("result"))
			}
		}
	}
	if failures > 0 {
		t.Errorf("%d advance cases differ", failures)
	}
}
