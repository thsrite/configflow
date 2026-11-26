package main

import (
	"archive/zip"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"strings"
	"testing"
)

func TestMutationsAreDetected(t *testing.T) {
	c := testComparator(t)
	good := `{"success":true,"data":{"count":3,"name":"a","items":["x","y"],"note":""}}`
	g := jrep(t, good)
	tests := []struct {
		name string
		bad  reply
		want diff
	}{
		{"status", rep([]byte(good), jsonType, 500, nil), diff{"status", "value"}},
		{"content-type", rep([]byte(good), "text/plain", 200, nil), diff{"Content-Type", "value"}},
		{"value", jrep(t, strings.Replace(good, "3", "4", 1)), diff{"body.data.count", "value"}},
		{"missing key", jrep(t, strings.Replace(good, `"count":3,`, "", 1)), diff{"body.data.count", "missing-in-go"}},
		{"extra key", jrep(t, strings.Replace(good, `"count":3`, `"count":3,"success":1`, 1)), diff{"body.data.success", "extra-in-go"}},
		{"type bool", jrep(t, strings.Replace(good, "3", "true", 1)), diff{"body.data.count", "type"}},
		{"int to string", jrep(t, strings.Replace(good, "3", `"3"`, 1)), diff{"body.data.count", "type"}},
		{"int to float", jrep(t, strings.Replace(good, "3", "3.0", 1)), diff{"body.data.count", "int-float"}},
		{"list order", jrep(t, strings.Replace(good, `["x","y"]`, `["y","x"]`, 1)), diff{"body.data.items[0]", "value"}},
		{"list length", jrep(t, strings.Replace(good, `["x","y"]`, `["x"]`, 1)), diff{"body.data.items", "length"}},
		{"empty vs missing", jrep(t, strings.Replace(good, `,"note":""`, "", 1)), diff{"body.data.note", "missing-in-go"}},
		{"empty vs null", jrep(t, strings.Replace(good, `"note":""`, `"note":null`, 1)), diff{"body.data.note", "type"}},
		{"duplicate key", jrep(t, strings.Replace(good, `"count":3`, `"count":3,"count":3`, 1)), diff{"body", "parse-go"}},
		{"one-sided transport", transportFailure, diff{"status", "transport-one-sided"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if found := diffsOf(t, c, g, tt.bad, "/synthetic", nil); !contains(found, tt.want) {
				t.Fatalf("%v not in %v", tt.want, found)
			}
		})
	}
}

func TestBothSidedTransportFailureIsADifference(t *testing.T) {
	c := testComparator(t)
	assertDiffs(t, diffsOf(t, c, transportFailure, transportFailure, "/synthetic", nil), []diff{{"status", "transport-both"}})
}

func TestHeadersAreCompared(t *testing.T) {
	c := testComparator(t)
	base := map[string]string{"x-configflow-profile": "one", "etag": `"a"`, "date": "d1", "server": "waitress", "content-length": "2"}
	with := func(edit func(map[string]string)) map[string]string {
		out := map[string]string{}
		for k, v := range base {
			out[k] = v
		}
		edit(out)
		return out
	}
	left := rep([]byte("{}"), jsonType, 200, base)
	tests := []struct {
		name    string
		headers map[string]string
		want    diff
	}{
		{"value", with(func(h map[string]string) { h["x-configflow-profile"] = "two" }), diff{"header.X-ConfigFlow-Profile", "value"}},
		{"missing", with(func(h map[string]string) { delete(h, "etag") }), diff{"header.ETag", "missing-in-go"}},
		{"extra", with(func(h map[string]string) { h["content-disposition"] = "attachment" }), diff{"header.Content-Disposition", "extra-in-go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if found := diffsOf(t, c, left, rep([]byte("{}"), jsonType, 200, tt.headers), "/synthetic", nil); !contains(found, tt.want) {
				t.Fatalf("%v not in %v", tt.want, found)
			}
		})
	}
	transportOnly := map[string]string{"x-configflow-profile": "one", "etag": `"a"`, "date": "d2", "server": "go", "content-length": "3"}
	assertDiffs(t, diffsOf(t, c, left, rep([]byte("{}"), jsonType, 200, transportOnly), "/synthetic", nil), nil)
}

func TestProviderStaleHeaderAndBodyAreStillCompared(t *testing.T) {
	c := testComparator(t)
	stale := rep([]byte("proxies: [{name: One}, {name: Two}]\n"), "text/yaml", 200, map[string]string{"x-configflow-provider": "last-good"})
	fresh := rep([]byte("proxies: [{name: One}]\n"), "text/yaml", 200, nil)
	for _, route := range []string{"/api/aggregations/<agg_id>/provider", "/api/profiles/<profile_id>/aggregations/<agg_id>/provider"} {
		found := diffsOf(t, c, stale, fresh, route, nil)
		for _, want := range []diff{{"header.X-ConfigFlow-Provider", "missing-in-go"}, {"body.proxies", "length"}} {
			if !contains(found, want) {
				t.Fatalf("%s: %v not in %v", route, want, found)
			}
		}
	}
}

func TestStructuralFormats(t *testing.T) {
	c := testComparator(t)
	tests := []struct {
		left, right reply
		route       string
	}{
		{jrep(t, `{"b":2,"a":1}`), jrep(t, `{"a":1,"b":2}`), "/synthetic"},
		{rep([]byte("a: 1\nb: [2, 3]\n"), "text/yaml", 200, nil), rep([]byte("b:\n- 2\n- 3\na: 1\n"), "text/yaml", 200, nil), "/synthetic"},
		{rep([]byte("a\nb\n\n"), "text/plain", 200, nil), rep([]byte("a\nb"), "text/plain", 200, nil), "/synthetic"},
		{jrep(t, `{"content":"a: 1\nb: 2\n"}`), jrep(t, `{"content":"b: 2\na: 1\n"}`), "/api/generate/mihomo/preview"},
		{rep([]byte("base: &b {x: 1}\nitem:\n  <<: *b\n  x: 2\n"), "text/yaml", 200, nil), rep([]byte("base: {x: 1}\nitem: {x: 2}\n"), "text/yaml", 200, nil), "/synthetic"},
	}
	for _, tt := range tests {
		t.Run(string(tt.left.Body), func(t *testing.T) {
			assertDiffs(t, diffsOf(t, c, tt.left, tt.right, tt.route, nil), nil)
		})
	}
}

func TestLineEndingsAndDuplicateYAMLKeys(t *testing.T) {
	c := testComparator(t)
	assertDiffs(t, diffsOf(t, c, rep([]byte("#!/bin/sh\necho\n"), "text/plain", 200, nil),
		rep([]byte("#!/bin/sh\r\necho\r\n"), "text/plain", 200, nil), "/synthetic", nil), []diff{{"body", "line-endings"}})
	if found := diffsOf(t, c, jrep(t, `{"content":"a\nb\n"}`), jrep(t, `{"content":"a\r\nb\r\n"}`), "/synthetic", nil); !contains(found, diff{"body.content", "line-endings"}) {
		t.Fatal(found)
	}
	for _, right := range []string{"dns: {a: 1}\ndns: {a: 1}\n", "a: 1\nb: {c: 1, c: 2}\n"} {
		left := "dns: {a: 1}\n"
		if strings.HasPrefix(right, "a:") {
			left = "a: 1\n"
		}
		if found := diffsOf(t, c, rep([]byte(left), "text/yaml", 200, nil), rep([]byte(right), "text/yaml", 200, nil), "/synthetic", nil); !contains(found, diff{"body", "parse-go"}) {
			t.Fatalf("%q: %v", right, found)
		}
	}
}

func TestInvalidDocumentsCannotPass(t *testing.T) {
	c := testComparator(t)
	bad := jrep(t, "{")
	assertDiffs(t, diffsOf(t, c, bad, bad, "/synthetic", nil), []diff{{"body", "parse-base"}, {"body", "parse-go"}})
	if found := diffsOf(t, c, bad, jrep(t, "{}"), "/api/x", nil); !contains(found, diff{"body", "parse-base"}) {
		t.Fatal(found)
	}
	if found := diffsOf(t, c, jrep(t, "{}"), bad, "/api/x", nil); !contains(found, diff{"body", "parse-go"}) {
		t.Fatal(found)
	}
}

func zipArchive(t *testing.T, content string, mode uint32, crlf bool) reply {
	t.Helper()
	var buffer bytes.Buffer
	w := zip.NewWriter(&buffer)
	f, err := w.Create("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("a: 1\n"))
	header := &zip.FileHeader{Name: "install.sh", Method: zip.Deflate}
	header.ExternalAttrs = mode << 16
	f, err = w.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if crlf {
		content = strings.ReplaceAll(content, "\n", "\r\n")
	}
	f.Write([]byte(content))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return rep(buffer.Bytes(), "application/zip", 200, nil)
}

func TestZIPMembersUseTheirOwnFormat(t *testing.T) {
	c := testComparator(t)
	route := "/api/generate/oxidns"
	assertDiffs(t, diffsOf(t, c, zipArchive(t, "a\nb\n", 0o644, false), zipArchive(t, "a\nb", 0o644, false), route, nil), nil)
	for name, pair := range map[string][2]reply{
		"line": {zipArchive(t, "a\nb", 0o644, false), zipArchive(t, "a\nc", 0o644, false)},
		"mode": {zipArchive(t, "a\nb", 0o755, false), zipArchive(t, "a\nb", 0o644, false)},
		"crlf": {zipArchive(t, "a\nb\n", 0o644, false), zipArchive(t, "a\nb\n", 0o644, true)},
	} {
		if len(diffsOf(t, c, pair[0], pair[1], route, nil)) == 0 {
			t.Fatalf("%s: change not detected", name)
		}
	}
}

func TestAgentPullYAMLContractAndMutations(t *testing.T) {
	c := testComparator(t)
	route := "/api/agents/<agent_id>/config"
	content := func(s string) reply { return jrep(t, map[string]any{"content": s}) }
	left := content("plugins:\n- tag: example\n  type: sequence\n")
	assertDiffs(t, diffsOf(t, c, left, content("plugins: [{tag: example, type: sequence}]\n"), route, nil), nil)
	for _, changed := range []string{"plugins: [{tag: changed, type: sequence}]\n", "plugins: [{tag: example}]\n"} {
		if len(diffsOf(t, c, left, content(changed), route, nil)) == 0 {
			t.Fatalf("%q not detected", changed)
		}
	}
	// Scope: another route retains the text policy for this shape.
	if len(diffsOf(t, c, content("plugins:\n- {tag: example}\n"), content("plugins: [{tag: example}]\n"), "/api/other", nil)) == 0 {
		t.Fatal("other route accepted reformatting")
	}
	if len(diffsOf(t, c, content("[General]\nloglevel = warning\n"), content("[General]\nloglevel = info\n"), route, nil)) == 0 {
		t.Fatal("surge change not detected")
	}
}

func TestAgentPullDigestIsVerifiedNotIgnored(t *testing.T) {
	c := testComparator(t)
	route := "/api/agents/<agent_id>/config"
	payload := func(content string) map[string]any {
		sum := md5.Sum([]byte(content))
		digest := hex.EncodeToString(sum[:])
		return map[string]any{"content": content, "md5": digest, "version": digest[:8]}
	}
	a, b := payload("plugins:\n- tag: example\n"), payload("plugins: [{tag: example}]\n")
	assertDiffs(t, diffsOf(t, c, jrep(t, a), jrep(t, b), route, nil), nil)
	for _, name := range []string{"md5", "version"} {
		broken := map[string]any{}
		for k, v := range b {
			broken[k] = v
		}
		broken[name] = "wrong"
		if len(diffsOf(t, c, jrep(t, a), jrep(t, broken), route, nil)) == 0 {
			t.Fatalf("broken %s accepted", name)
		}
	}
	for _, changed := range []string{"plugins: [{tag: changed}]\n", "plugins: []\n"} {
		if len(diffsOf(t, c, jrep(t, a), jrep(t, payload(changed)), route, nil)) == 0 {
			t.Fatalf("%q accepted", changed)
		}
	}
}

func TestAgentComparisonIdentityPairingHasProofs(t *testing.T) {
	c := testComparator(t)
	identities := newAgentIdentities()
	route := "/api/agents/<agent_id>/push-diff"
	base := func(edit func(map[string]any)) map[string]any {
		m := map[string]any{"status": "changed", "checked_at": "2026-01-01T00:00:00.000001Z", "version": strings.Repeat("a", 16), "files": []any{}}
		if edit != nil {
			edit(m)
		}
		return m
	}
	a := base(nil)
	b := base(func(m map[string]any) {
		m["checked_at"] = "2026-01-01T00:00:00.000002Z"
		m["version"] = strings.Repeat("b", 16)
	})
	compare := func(route string, left, right any) []diff {
		found, err := c.responseDiffs(jrep(t, left), jrep(t, right), route, nil, identities, nil)
		if err != nil {
			t.Fatal(err)
		}
		return found
	}
	assertDiffs(t, compare(route, a, b), nil)
	assertDiffs(t, compare(route, a, b), nil)
	edits := []func(map[string]any){
		func(m map[string]any) { m["version"] = strings.Repeat("c", 16) },
		func(m map[string]any) { m["version"] = "bad" },
		func(m map[string]any) { m["checked_at"] = "bad" },
		func(m map[string]any) { m["status"] = "synced" },
		func(m map[string]any) { m["files"] = []any{map[string]any{"path": "missing"}} },
		func(m map[string]any) { delete(m, "checked_at") },
	}
	for i, edit := range edits {
		changed := base(func(m map[string]any) {
			m["checked_at"] = "2026-01-01T00:00:00.000002Z"
			m["version"] = strings.Repeat("b", 16)
			edit(m)
		})
		if len(compare(route, a, changed)) == 0 {
			t.Fatalf("edit %d accepted", i)
		}
	}
	collision := base(func(m map[string]any) { m["version"] = strings.Repeat("d", 16) })
	if len(compare(route, collision, b)) == 0 {
		t.Fatal("collision accepted")
	}
	if len(compare("/api/other", a, b)) == 0 {
		t.Fatal("other route paired")
	}
	assertDiffs(t, compare("/api/agents", []any{map[string]any{"content_sync": a}}, []any{map[string]any{"content_sync": b}}), nil)
	assertDiffs(t, compare("/api/agents/<agent_id>", map[string]any{"content_sync": a}, map[string]any{"content_sync": b}), nil)
	if len(compare("/api/agents/<agent_id>", a, b)) == 0 {
		t.Fatal("detail route paired a top-level identity")
	}
}

func TestUnknownKeysNeverEscape(t *testing.T) {
	c := testComparator(t)
	left, right := newMap(), newMap()
	left.Set("private.example", int64(1))
	right.Set("private.example", int64(2))
	assertDiffs(t, c.differences(left, right, "body"), []diff{{"body.<key>", "value"}})
	assertDiffs(t, diffsOf(t, c, rep([]byte("{}"), jsonType, 200, map[string]string{"x-private-example": "a"}),
		rep([]byte("{}"), jsonType, 200, map[string]string{"x-private-example": "b"}), "/r", nil), []diff{{"header.<key>", "value"}})
}

func TestUnfrozenProviderDocumentStillDetectsChangedContent(t *testing.T) {
	c := testComparator(t)
	doc := func(port int) reply {
		return jrep(t, map[string]any{"files": []any{map[string]any{"path": "providers/a.yaml", "role": "provider",
			"document": map[string]any{"proxies": []any{map[string]any{"name": "Node", "port": port}}}}}})
	}
	found, err := c.responseDiffs(bodyReply(200, jsonType, doc(80).Body), bodyReply(200, jsonType, doc(81).Body), "/generator/agent-deployment-bundle", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(found, diff{"body.files[0].document.proxies[0].port", "value"}) {
		t.Fatal(found)
	}
}

func TestGoModeAppliesNoPythonReferenceDeltas(t *testing.T) {
	c := testComparator(t)
	// Python mode accepted the Flask-CORS → Go security-header delta; Go-vs-Go must not.
	source := map[string]string{"access-control-allow-origin": "*", "vary": "Origin, Accept-Encoding"}
	target := map[string]string{"vary": "Accept-Encoding", "x-content-type-options": "nosniff"}
	left := rep([]byte("rule text"), "text/plain; charset=utf-8", 200, source)
	right := rep(left.Body, left.ContentType, 200, target)
	if found := diffsOf(t, c, left, right, "/api/rules/local/<name>", nil); !contains(found, diff{"header.Access-Control-Allow-Origin", "missing-in-go"}) {
		t.Fatal(found)
	}
	assertDiffs(t, diffsOf(t, c, right, right, "/api/rules/local/<name>", nil), nil)
	// The Agent release pairing and M18 store_state were Python-mode exemptions as well.
	if len(diffsOf(t, c, jrep(t, `{"success":true,"version":"1.8.61-go"}`), jrep(t, `{"success":true,"version":"1.9.3-go"}`), "/api/agents/latest-version", nil)) == 0 {
		t.Fatal("agent release paired")
	}
	if len(diffsOf(t, c, jrep(t, `{"success":true,"agents":[{"id":"a"}]}`),
		jrep(t, `{"success":true,"agents":[{"id":"a","store_state":"ok","store_message":""}]}`), "/api/domain-discovery/agents", nil)) == 0 {
		t.Fatal("store_state stripped")
	}
	// Offline transport texts and the A18 refusal compare raw.
	route := "/api/agents/<agent_id>/generate-config"
	py := rep([]byte(`{"success":false,"message":"HTTPConnectionPool(host='agent.example.com', port=8080): x"}`), jsonType, 400, nil)
	native := rep([]byte(`{"success":false,"message":"Get \"http://agent.example.com:8080/api/capabilities\": dial tcp: refused"}`), jsonType, 400, nil)
	assertDiffs(t, diffsOf(t, c, py, native, route, nil), []diff{{"body.message", "value"}})
}

func TestRegistrationTokenIsMaskedExactly(t *testing.T) {
	c := testComparator(t)
	tokenA, tokenB := strings.Repeat("a", 43), "b-_"+strings.Repeat("c", 40)
	tests := []struct{ route, template string }{
		{"/api/agents/install-script", "cfg = {\n  \"registration_token\": \"%s\"\n}\n"},
		{"/api/agents/docker-agent-compose", "services:\n  agent:\n    environment:\n      - AGENT_MIHOMO_REGISTRATION_TOKEN=%s\n"},
		{"/api/agents/docker-agent-run", "docker run \\\n  -e 'AGENT_OXIDNS_REGISTRATION_TOKEN=%s' \\\n"},
	}
	for _, tt := range tests {
		t.Run(tt.route, func(t *testing.T) {
			body := func(token string) reply {
				return rep([]byte(strings.Replace(tt.template, "%s", token, 1)), "text/plain; charset=utf-8", 200, nil)
			}
			left, right := body(tokenA), body(tokenB)
			assertDiffs(t, diffsOf(t, c, left, right, tt.route, nil), nil)
			extra := rep(append(append([]byte{}, right.Body...), "# extra\n"...), right.ContentType, 200, nil)
			if len(diffsOf(t, c, left, extra, tt.route, nil)) == 0 {
				t.Fatal("extra line accepted")
			}
			if len(diffsOf(t, c, left, body(tokenB[:42]), tt.route, nil)) == 0 {
				t.Fatal("short token accepted")
			}
			if len(diffsOf(t, c, left, right, "/api/rules/local/<name>", nil)) == 0 {
				t.Fatal("other route masked")
			}
		})
	}
}
