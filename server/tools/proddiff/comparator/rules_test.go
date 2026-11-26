package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRules(t *testing.T, rules any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "proddiff-normalize.json")
	data, err := json.Marshal(rules)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func realRules(t *testing.T) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "server/parity/proddiff-normalize.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rules []map[string]any
	if err := json.Unmarshal(data, &rules); err != nil {
		t.Fatal(err)
	}
	return rules
}

func ruleSet(t *testing.T, fields ...map[string]any) []rule {
	t.Helper()
	var out []rule
	for _, f := range fields {
		out = append(out, mustRule(t, normalizeFields(f)))
	}
	return out
}

// normalizeFields turns encoding/json numbers into ints where integral.
func normalizeFields(f map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range f {
		if n, ok := v.(float64); ok && n == float64(int(n)) {
			v = int(n)
		}
		out[k] = v
	}
	return out
}

func TestNormalizationIsRouteAndPathSpecific(t *testing.T) {
	c := testComparator(t)
	a, b := jrep(t, `{"size":1,"data":{"size":2}}`), jrep(t, `{"size":3,"data":{"size":4}}`)
	rules := ruleSet(t, map[string]any{"route": "/synthetic", "path": "size"})
	assertDiffs(t, diffsOf(t, c, a, b, "/synthetic", rules), []diff{{"body.data.size", "value"}})
	if n := len(diffsOf(t, c, a, b, "/other", rules)); n != 2 {
		t.Fatalf("other route: %d differences", n)
	}
}

func TestNormalizationPreservesTypeChanges(t *testing.T) {
	c := testComparator(t)
	rules := ruleSet(t, map[string]any{"route": "/synthetic", "path": "size"})
	assertDiffs(t, diffsOf(t, c, jrep(t, `{"size":1}`), jrep(t, `{"size":{}}`), "/synthetic", rules), []diff{{"body.size", "type"}})
	assertDiffs(t, diffsOf(t, c, jrep(t, `{"size":1}`), jrep(t, `{}`), "/synthetic", rules), []diff{{"body.size", "missing-in-go"}})
}

func TestRequestClocksKeepExactFieldsAndNegativeControls(t *testing.T) {
	c := testComparator(t)
	expected := map[[2]string]bool{
		{"/api/agents/<agent_id>/ipv6-readiness", "refresh_error.at"}: true,
		{"/api/agents/<agent_id>/upstream-routers", "checked_at"}:     true,
		{"/api/client-devices/discovery", "checked_at"}:               true,
		{"/api/notifications", "checked_at"}:                          true,
		{"/api/routers/<router_id>/policy", "generated_at"}:           true,
	}
	configured := realRules(t)
	routes := map[string]bool{}
	for _, r := range configured {
		routes[r["route"].(string)] = true
	}
	selected := 0
	for _, fields := range configured {
		path, _ := fields["path"].(string)
		if !expected[[2]string{fields["route"].(string), path}] {
			continue
		}
		selected++
		fields := normalizeFields(fields)
		t.Run(fields["route"].(string), func(t *testing.T) {
			if fields["tolerance"] != 120 || fields["timestamp_format"] != "request-clock" || fields["reason"] == "" {
				t.Fatalf("rule %v", fields)
			}
			r := mustRule(t, fields)
			parts := strings.Split(path, ".")
			body := func(value any) reply {
				result := map[string]any{"success": true, "payload": map[string]any{"keep": "value"}}
				node := result
				for _, part := range parts[:len(parts)-1] {
					next := map[string]any{}
					node[part] = next
					node = next
				}
				if value != nil {
					node[parts[len(parts)-1]] = value
				}
				return jrep(t, result)
			}
			stamp := func(seconds string) string {
				suffix := "+00:00"
				if fields["route"] == "/api/routers/<router_id>/policy" {
					suffix = ""
				}
				micros := ""
				if fields["route"] == "/api/notifications" {
					micros = ".000000"
				}
				return "2026-01-01T00:" + seconds + micros + suffix
			}
			route := r.Route
			rules := []rule{r}
			source := body(stamp("00:00"))
			assertDiffs(t, diffsOf(t, c, source, body(stamp("00:01")), route, rules), nil)
			for _, invalid := range []any{"bad", 1, "2026-01-01", "2026-01-01T00:00:00Z", stamp("02:01")} {
				if len(diffsOf(t, c, source, body(invalid), route, rules)) == 0 {
					t.Fatalf("%v accepted", invalid)
				}
			}
			if len(diffsOf(t, c, body(1), body(2), route, rules)) == 0 {
				t.Fatal("numbers accepted")
			}
			if len(diffsOf(t, c, source, body(nil), route, rules)) == 0 {
				t.Fatal("missing field accepted")
			}
			changed := map[string]any{"success": true, "payload": map[string]any{"keep": "changed"}}
			node := changed
			for _, part := range parts[:len(parts)-1] {
				next := map[string]any{}
				node[part] = next
				node = next
			}
			node[parts[len(parts)-1]] = stamp("00:01")
			if len(diffsOf(t, c, source, jrep(t, changed), route, rules)) == 0 {
				t.Fatal("other field accepted")
			}
			if len(diffsOf(t, c, source, body(stamp("00:01")), "/other", rules)) == 0 {
				t.Fatal("other route normalized")
			}
			for _, mutation := range []map[string]any{{"route": "/other"}, {"path": "other"}, {"tolerance": 121}, {"timestamp_format": "other"}} {
				mutated := map[string]any{}
				for k, v := range fields {
					mutated[k] = v
				}
				for k, v := range mutation {
					mutated[k] = v
				}
				if _, err := loadRules(writeRules(t, []any{mutated}), routes, "go"); err == nil {
					t.Fatalf("mutation %v accepted", mutation)
				}
			}
		})
	}
	if selected != 5 {
		t.Fatalf("selected %d request-clock rules", selected)
	}
}

func TestDiscoveryComparisonAlignsColdAndHit(t *testing.T) {
	c := testComparator(t)
	route := "/api/client-devices/discovery"
	cases := []testCase{
		{Method: "GET", Route: "/ordinary", Path: "/ordinary"},
		{Method: "POST", Route: "/artifact", Path: "/artifact", Body: newMap()},
		{Method: "GET", Route: "/api/logs/info", Path: "/api/logs/info"},
		{Method: "GET", Route: route, Path: route + "?profile_id=synthetic"},
		{Method: "GET", Route: route, Path: route},
	}
	var calls []string
	seen := 0
	answers, err := comparisonResults(cases, func(tc testCase) (any, error) {
		calls = append(calls, tc.Path)
		if tc.Route == route {
			seen++
			return seen != 1, nil
		}
		return tc.Route, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls[0] != route+"?profile_id=synthetic" || calls[1] != route || calls[len(calls)-1] != "/api/logs/info" {
		t.Fatalf("calls %v", calls)
	}
	if answers[0] != false || answers[1] != true {
		t.Fatalf("answers %v", answers)
	}
	var rules []rule
	for _, fields := range realRules(t) {
		if fields["route"] == route {
			rules = append(rules, mustRule(t, normalizeFields(fields)))
		}
	}
	base := map[string]any{"cached": true, "stale": false, "refresh_error": "", "devices": []any{}, "checked_at": "2026-01-01T00:00:00+00:00"}
	for _, edit := range []map[string]any{{"cached": false}, {"stale": true}, {"refresh_error": "failed"}, {"devices": []any{map[string]any{"ip": "192.0.2.1"}}}} {
		changed := map[string]any{}
		for k, v := range base {
			changed[k] = v
		}
		for k, v := range edit {
			changed[k] = v
		}
		if len(diffsOf(t, c, jrep(t, base), jrep(t, changed), route, rules)) == 0 {
			t.Fatalf("%v accepted", edit)
		}
	}
	if phase(testCase{Method: "GET", Route: "/api/client-devices"}) != 0 || phase(testCase{Method: "POST", Route: route}) != 1 {
		t.Fatal("phase")
	}
}

func TestPD1HTTPDateToleranceIsExact(t *testing.T) {
	c := testComparator(t)
	fields := map[string]any{"route": "/api/generate/mihomo", "path": "header.last-modified", "timestamp_format": "http-date", "tolerance": 600, "reason": "PD-1"}
	rules := ruleSet(t, fields)
	stamp := "Thu, 01 Jan 2026 00:00:00 GMT"
	left := rep([]byte("{}"), jsonType, 200, map[string]string{"last-modified": stamp, "etag": "unchanged"})
	boundary := rep([]byte("{}"), jsonType, 200, map[string]string{"last-modified": "Thu, 01 Jan 2026 00:10:00 GMT", "etag": "unchanged"})
	assertDiffs(t, diffsOf(t, c, left, boundary, "/api/generate/mihomo", rules), nil)
	tests := []struct {
		headers map[string]string
		want    diff
	}{
		{map[string]string{"etag": "unchanged"}, diff{"header.Last-Modified", "missing-in-go"}},
		{map[string]string{"last-modified": "invalid", "etag": "unchanged"}, diff{"header.Last-Modified", "value"}},
		{map[string]string{"last-modified": "2026-01-01T00:00:30Z", "etag": "unchanged"}, diff{"header.Last-Modified", "value"}},
		{map[string]string{"last-modified": "Thu, 01 Jan 2026 00:10:01 GMT", "etag": "unchanged"}, diff{"header.Last-Modified", "value"}},
		{map[string]string{"last-modified": "Fri, 01 Jan 2026 00:01:00 GMT", "etag": "unchanged"}, diff{"header.Last-Modified", "value"}},
		{map[string]string{"last-modified": "Thu, 01 Jan 2026 00:01:00 GMT", "etag": "changed"}, diff{"header.ETag", "value"}},
	}
	for _, tt := range tests {
		if found := diffsOf(t, c, left, rep([]byte("{}"), jsonType, 200, tt.headers), "/api/generate/mihomo", rules); !contains(found, tt.want) {
			t.Fatalf("%v: %v not in %v", tt.headers, tt.want, found)
		}
	}
	if len(diffsOf(t, c, left, boundary, "/api/generate/surge", rules)) == 0 {
		t.Fatal("other route normalized")
	}
	routes := map[string]bool{"/api/generate/mihomo": true, "/api/other": true}
	if got, err := loadRules(writeRules(t, []any{fields}), routes, "go"); err != nil || len(got) != 1 {
		t.Fatalf("valid rule: %v %v", got, err)
	}
	for _, mutation := range []map[string]any{{"timestamp_format": "other"}, {"tolerance": 601}, {"path": "header.etag"}, {"route": "/api/other"}} {
		mutated := map[string]any{}
		for k, v := range fields {
			mutated[k] = v
		}
		for k, v := range mutation {
			mutated[k] = v
		}
		if _, err := loadRules(writeRules(t, []any{mutated}), routes, "go"); err == nil {
			t.Fatalf("mutation %v accepted", mutation)
		}
	}
}

func TestPatternAndToleranceNormalizationStayNarrow(t *testing.T) {
	c := testComparator(t)
	prefix := ruleSet(t, map[string]any{"route": "/r", "path": "message", "pattern": "(?s)(?<=^Connection error: ).+"})
	assertDiffs(t, diffsOf(t, c, jrep(t, `{"message":"Connection error: a"}`), jrep(t, `{"message":"Connection error: b"}`), "/r", prefix), nil)
	assertDiffs(t, diffsOf(t, c, jrep(t, `{"message":"Connection error: a"}`), jrep(t, `{"message":"Agent not found"}`), "/r", prefix),
		[]diff{{"body.message", "value"}})
	lines := ruleSet(t, map[string]any{"route": "/r", "path": "logs[]", "pattern": `^\d{4}-\d\d-\d\d \d\d:\d\d:\d\d,\d{3}`})
	a := jrep(t, map[string]any{"logs": []string{"2026-01-01 00:00:00,000 - x - ERROR - a", "2026-01-01 00:00:00,000 - x - ERROR - b"}})
	b := jrep(t, map[string]any{"logs": []string{"2026-01-02 01:00:00,111 - x - ERROR - a", "2026-01-02 01:00:00,111 - x - ERROR - c"}})
	assertDiffs(t, diffsOf(t, c, a, b, "/r", lines), []diff{{"body.logs[1]", "value"}})
	unordered := append(lines, ruleSet(t, map[string]any{"route": "/r", "path": "logs", "unordered": true})...)
	swapped := jrep(t, map[string]any{"logs": []string{"2026-01-02 01:00:00,111 - x - ERROR - b", "2026-01-02 01:00:00,111 - x - ERROR - a"}})
	assertDiffs(t, diffsOf(t, c, a, swapped, "/r", unordered), nil)
	if len(diffsOf(t, c, a, b, "/r", unordered)) == 0 {
		t.Fatal("unordered hid a change")
	}
	if found := diffsOf(t, c, a, jrep(t, map[string]any{"logs": []string{"2026-01-02 01:00:00,111 - x - ERROR - a"}}), "/r", unordered); !contains(found, diff{"body.logs", "length"}) {
		t.Fatal(found)
	}
	longer := jrep(t, map[string]any{"logs": []string{"2026-01-03 00:00:00,000 - a.b - ERROR - z", "continuation without timestamp",
		"2026-01-03 00:00:00,001 - x - ERROR - a", "2026-01-03 00:00:00,002 - x - ERROR - b"}})
	got := map[diff]bool{}
	for _, d := range diffsOf(t, c, a, longer, "/r", unordered) {
		got[d] = true
	}
	if len(got) != 3 || !got[diff{"body.logs", "length"}] || !got[diff{"body.logs[0]", "value"}] || !got[diff{"body.logs[1]", "value"}] {
		t.Fatalf("longer: %v", got)
	}
	clock := ruleSet(t, map[string]any{"route": "/r", "path": "start", "tolerance": 60})
	tests := []struct {
		left, right string
		want        []diff
	}{
		{`{"start":1000.5}`, `{"start":1030.25}`, nil},
		{`{"start":1000.5}`, `{"start":2000.5}`, []diff{{"body.start", "value"}}},
		{`{"start":"2026-01-01T00:00:00.000001"}`, `{"start":"2026-01-01T00:00:30.000002"}`, nil},
		{`{"start":"2026-01-01T00:00:00.000001"}`, `{"start":"2026-01-01T00:00:30Z"}`, []diff{{"body.start", "value"}}},
		{`{"start":"2026-01-01T00:00:00"}`, `{"start":"not a time"}`, []diff{{"body.start", "value"}}},
	}
	for _, tt := range tests {
		assertDiffs(t, diffsOf(t, c, jrep(t, tt.left), jrep(t, tt.right), "/r", clock), tt.want)
	}
	header := ruleSet(t, map[string]any{"route": "/r", "path": "header.content-disposition", "pattern": `\d{8}_\d{6}`})
	left := rep([]byte("{}"), jsonType, 200, map[string]string{"content-disposition": `inline; filename="p_20260101_000000.yaml"`})
	assertDiffs(t, diffsOf(t, c, left, rep([]byte("{}"), jsonType, 200, map[string]string{"content-disposition": `inline; filename="p_20260101_000001.yaml"`}), "/r", header), nil)
	if len(diffsOf(t, c, left, rep([]byte("{}"), jsonType, 200, map[string]string{"content-disposition": `attachment; filename="p_20260101_000001.yaml"`}), "/r", header)) == 0 {
		t.Fatal("attachment accepted")
	}
}

func TestRealSubscriptionFilenamePatternUsesLookarounds(t *testing.T) {
	c := testComparator(t)
	var rules []rule
	for _, fields := range realRules(t) {
		if fields["route"] == "/api/subscriptions/proxies" && fields["path"] == "header.content-disposition" {
			rules = append(rules, mustRule(t, normalizeFields(fields)))
		}
	}
	if len(rules) != 1 {
		t.Fatalf("rules %d", len(rules))
	}
	header := func(v string) reply {
		return rep([]byte("{}"), jsonType, 200, map[string]string{"content-disposition": v})
	}
	route := "/api/subscriptions/proxies"
	assertDiffs(t, diffsOf(t, c, header(`attachment; filename="sub_20260101_000000.yaml"`), header(`attachment; filename="sub_20260102_010101.yaml"`), route, rules), nil)
	for _, other := range []string{`attachment; filename="other_20260102_010101.yaml"`, `attachment; filename="sub_20260102_010101.txt.bak"`, `attachment; filename="sub20260102_010101.yaml"`} {
		if len(diffsOf(t, c, header(`attachment; filename="sub_20260101_000000.yaml"`), header(other), route, rules)) == 0 {
			t.Fatalf("%s accepted", other)
		}
	}
}

func TestRuleFileValidation(t *testing.T) {
	routes := map[string]bool{"/r": true}
	bad := [][]map[string]any{
		{{"route": "/r", "path": "a"}},
		{{"route": "/missing", "path": "a", "reason": "t"}},
		{{"route": "/r", "path": "a*", "reason": "t"}},
		{{"route": "/r", "path": "a", "bracket": true, "reason": "t"}},
		{{"route": "/r", "reason": "t"}},
		{{"route": "/r", "path": "a", "reason": "t", "ignore": true}},
		{{"route": "/r", "path": "a", "reason": "t", "pattern": "x", "tolerance": 1}},
		{{"route": "/r", "path": "a", "reason": "t", "pattern": "x", "unordered": true}},
		{{"route": "/r", "path": "a", "reason": "t", "mode": "base"}},
	}
	for _, rules := range bad {
		if _, err := loadRules(writeRules(t, rules), routes, "go"); err == nil {
			t.Fatalf("%v accepted", rules)
		}
	}
	got, err := loadRules(writeRules(t, []map[string]any{{"route": "/r", "bracket": true, "reason": "t"}}), routes, "go")
	if err != nil || len(got) != 1 || !got[0].Bracket {
		t.Fatalf("bracket rule: %v %v", got, err)
	}
}

func TestModeScopedNormalizationRules(t *testing.T) {
	routes := map[string]bool{"/api/notifications": true}
	base := map[string]any{"route": "/api/notifications", "path": "items[].version", "tolerance": 600, "reason": "r"}
	withMode := map[string]any{"mode": "go"}
	other := map[string]any{"path": "checked_at"}
	merge := func(extra map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	path := writeRules(t, []any{merge(withMode), merge(other)})
	if got, err := loadRules(path, routes, "go"); err != nil || len(got) != 2 {
		t.Fatalf("go: %v %v", got, err)
	}
	got, err := loadRules(path, routes, "python")
	if err != nil || len(got) != 1 || got[0].Path != "checked_at" {
		t.Fatalf("python: %v %v", got, err)
	}
}

func TestEditorRevisionNormalizationIsExactAndShapeChecked(t *testing.T) {
	c := testComparator(t)
	route := "/api/rule-library/<rule_id>/draft-content"
	var rules []rule
	for _, fields := range realRules(t) {
		if fields["route"] == route {
			if fields["path"] != "revision" || fields["pattern"] != "^[0-9a-f]{64}$" {
				t.Fatalf("rule %v", fields)
			}
			rules = append(rules, mustRule(t, normalizeFields(fields)))
		}
	}
	if len(rules) != 1 {
		t.Fatalf("rules %d", len(rules))
	}
	build := func(edit func(map[string]any)) reply {
		m := map[string]any{"success": true, "revision": strings.Repeat("b", 64),
			"rule": map[string]any{"id": "synthetic", "content": "a.example", "_revision": 2}, "usage_count": 1}
		if edit != nil {
			edit(m)
		}
		return jrep(t, m)
	}
	source := build(func(m map[string]any) { m["revision"] = strings.Repeat("a", 64) })
	assertDiffs(t, diffsOf(t, c, source, build(nil), route, rules), nil)
	edits := []func(map[string]any){
		func(m map[string]any) { m["revision"] = strings.Repeat("b", 63) },
		func(m map[string]any) { m["revision"] = strings.Repeat("B", 64) },
		func(m map[string]any) { m["revision"] = 42 },
		func(m map[string]any) { delete(m, "revision") },
		func(m map[string]any) { m["usage_count"] = 2 },
		func(m map[string]any) { m["rule"].(map[string]any)["content"] = "changed.example" },
		func(m map[string]any) { m["rule"].(map[string]any)["_revision"] = 3 },
	}
	for i, edit := range edits {
		if len(diffsOf(t, c, source, build(edit), route, rules)) == 0 {
			t.Fatalf("edit %d accepted", i)
		}
	}
	if len(diffsOf(t, c, source, build(nil), "/api/rule-library", rules)) == 0 {
		t.Fatal("other route normalized")
	}
}

func TestRealRuleFileLoadsInGoMode(t *testing.T) {
	rows, err := readRows(filepath.Join(repoRoot(t), "server/parity/routes.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	routes := map[string]bool{}
	for _, row := range rows {
		routes[row[1]] = true
	}
	rules, err := loadRules(filepath.Join(repoRoot(t), "server/parity/proddiff-normalize.json"), routes, "go")
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != len(realRules(t)) {
		t.Fatalf("go mode loaded %d of %d rules", len(rules), len(realRules(t)))
	}
}
