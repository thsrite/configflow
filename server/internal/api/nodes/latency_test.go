package nodes_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/api/nodes"
	"configflow/internal/health"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/substore"
	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

// Ports of backend/test_node_latency.py and backend/test_node_dns.py. The
// Python tests monkeypatch subprocess.run / _run_probe / _probe_binary /
// load_subscription_cache / sub_store_client.convert_proxy_string; here the
// worker is a fake health.Runner, Sub-Store a fake converter and the
// subscription caches real files.

const nodeA = `["node","a"]`

type runner struct {
	mu    sync.Mutex
	calls []health.Command
	fn    func(health.Command) (health.Result, error)
}

func (r *runner) Run(_ context.Context, command health.Command) (health.Result, error) {
	r.mu.Lock()
	r.calls = append(r.calls, command)
	r.mu.Unlock()
	return r.fn(command)
}

func (r *runner) payloads(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, call := range r.calls {
		out = append(out, decodeMap(t, call.Stdin))
	}
	return out
}

func decodeMap(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal %s: %v", data, err)
	}
	return out
}

func toMap(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return decodeMap(t, raw)
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func asSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func asInt(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}

func stdout(v any) health.Result {
	data, _ := json.Marshal(v)
	return health.Result{Stdout: data}
}

// probeBy answers like the Python tests' fake _run_probe: latency per node.
func probeBy(latency func(proxy map[string]any) any) func(health.Command) (health.Result, error) {
	return func(command health.Command) (health.Result, error) {
		var payload map[string]any
		_ = json.Unmarshal(command.Stdin, &payload)
		var results []any
		for _, node := range asSlice(payload["nodes"]) {
			proxy := asMap(node)
			results = append(results, map[string]any{"name": proxy["name"], "latency": latency(proxy)})
		}
		return stdout(map[string]any{"version": 4, "results": results}), nil
	}
}

func constant(n int) func(map[string]any) any {
	return func(map[string]any) any { return n }
}

type converter struct {
	mu    sync.Mutex
	calls []string
	fn    func(string) map[string]any
}

func (c *converter) ConvertProxyString(_ context.Context, s string) map[string]any {
	c.mu.Lock()
	c.calls = append(c.calls, s)
	c.mu.Unlock()
	if c.fn == nil {
		return nil
	}
	return c.fn(s)
}

type fixture struct {
	t      *testing.T
	srv    *httpxtest.Server
	runner *runner
	conv   *converter
	env    *health.ProbeEnv
	vars   map[string]string
}

func nodeA_() map[string]any {
	return map[string]any{
		"id": "a", "name": "A", "type": "trojan", "server": "example.invalid",
		"port": 443, "password": "secret", "tls": true,
	}
}

func setup(t *testing.T) *fixture {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(nodes.Register)})
	f := &fixture{t: t, srv: srv, runner: &runner{fn: probeBy(constant(10))}, conv: &converter{}, vars: map[string]string{}}
	f.env = &health.ProbeEnv{
		LookupEnv:   func(key string) (string, bool) { v, ok := f.vars[key]; return v, ok },
		Environ:     func() []string { return nil },
		Runner:      f.runner,
		Now:         time.Now,
		ProbeBinary: func() (string, error) { return "/test/probe", nil },
	}
	f.env.Collaborators = health.CurrentProbeEnv().Collaborators
	f.env.Collaborators.ConvertNode = func(ctx context.Context, raw json.RawMessage) (model.Fields, bool) {
		return health.ConvertNodeForProbe(ctx, raw, f.conv)
	}

	t.Cleanup(health.SetProbeEnv(f.env))
	f.saveShared("nodes", []any{nodeA_()})
	return f
}

func (f *fixture) ctx() context.Context { return f.srv.Context() }

func (f *fixture) saveShared(pairs ...any) {
	f.t.Helper()
	err := f.srv.Repo.UpdateShared(context.Background(), func(_ context.Context, shared *model.Shared) error {
		for i := 0; i < len(pairs); i += 2 {
			raw, err := json.Marshal(pairs[i+1])
			if err != nil {
				return err
			}
			if err := shared.SetJSON(pairs[i].(string), raw); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) cache(subID string, nodeList ...any) {
	f.t.Helper()
	if err := fixtureSaveSubscriptionNodes(f.ctx(), subID, nodeList, nil); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) post(body any) *httpxtest.Result {
	return f.srv.Post("/api/nodes/latency", httpxtest.JSON(body))
}

func (f *fixture) stored() map[string]any { return f.loadResults() }

func (f *fixture) writeLatency(v any) {
	if _, err := f.srv.Repo.WriteSharedJSON(context.Background(), health.LatencyFile, v); err != nil {
		f.t.Fatal(err)
	}
}

func results(t *testing.T, r *httpxtest.Result) map[string]any {
	t.Helper()
	return asMap(decodeMap(t, r.Body)["results"])
}

func resultKeys(t *testing.T, r *httpxtest.Result) []string {
	t.Helper()
	var envelope struct {
		Results json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(r.Body, &envelope); err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(strings.NewReader(string(envelope.Results)))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		t.Fatalf("results not object: %s (%v)", envelope.Results, err)
	}
	var out []string
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, k.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func names(list ...string) any { return map[string]any{"names": list} }
func keys(list ...string) any  { return map[string]any{"keys": list} }

// test_previous_probe_results_are_hidden_and_not_mixed[None / proxy_http_v1]
func TestPreviousProbeResultsAreHiddenAndNotMixed(t *testing.T) {
	for _, method := range []any{nil, "proxy_http_v1"} {
		f := setup(t)
		settings, _ := f.healthSettings()
		f.writeLatency(map[string]any{
			nodeA: map[string]any{
				"latency": nil, "history": []any{1, nil}, "method": method,
				"url": settings["url"], "unified_delay": true,
			},
		})
		if len(f.stored()) != 0 {
			t.Fatal("old method visible")
		}
		f.runner.fn = probeBy(constant(150))
		out, err := f.runTests([]string{"A", "A"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		a := asMap(asMap(out["results"])["A"])
		if encode(a["history"]) != "[150]" || asString(a["method"]) != health.Method {
			t.Fatalf("%v", encode(a))
		}
		f.vars["CONFIGFLOW_NODE_TEST_URL"] = "https://test.example/generate_204"
		out, _ = f.runTests([]string{"A"}, nil)
		if encode(asMap(asMap(out["results"])["A"])["history"]) != "[150]" {
			t.Fatal("history mixed across URLs")
		}
	}
}

func encode(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func TestWorkerGetsStdinJSONAndNoCredentialsInCommand(t *testing.T) {
	f := setup(t)
	f.runner.fn = func(command health.Command) (health.Result, error) {
		payload := decodeMap(t, command.Stdin)
		node := asMap(asSlice(payload["nodes"])[0])
		if asString(node["password"]) != "secret" || asString(node["name"]) != nodeA || command.Timeout != 10*time.Second {
			t.Errorf("payload %s timeout %v", command.Stdin, command.Timeout)
		}
		return stdout(map[string]any{
			"version": 4,
			"results": []any{
				map[string]any{"name": nodeA, "latency": 99, "first_latency": 250},
			},
		}), nil
	}
	out, err := f.runTests([]string{"A"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.runner.calls) != 1 || strings.Join(f.runner.calls[0].Argv, " ") != "/test/probe" {
		t.Fatalf("argv %v", f.runner.calls)
	}
	a := asMap(asMap(out["results"])["A"])
	if asInt(a["latency"]) != 99 || asInt(a["first_latency"]) != 250 {
		t.Fatal(encode(a))
	}
	env := strings.Join(f.runner.calls[0].Env, " ")
	if !strings.Contains(env, "GOMAXPROCS=2") || !strings.Contains(env, "GOMEMLIMIT=128MiB") {
		t.Fatalf("env %s", env)
	}
}

func TestSubscriptionCacheKeepsProtocolOptionsAndDistinctSources(t *testing.T) {
	f := setup(t)
	f.saveShared("subscriptions", []any{map[string]any{"id": "sub", "name": "test", "enabled": true}})
	f.cache("sub",
		map[string]any{"name": "A", "type": "http", "server": "other.invalid", "port": 443},
		map[string]any{"name": "cached", "type": "trojan", "server": "proxy.invalid", "port": 443,
			"params": map[string]any{"password": "cached-secret", "sni": "tls.invalid"}})
	_, candidates, err := f.collectCandidates()
	if err != nil {
		t.Fatal(err)
	}
	if asString(candidates[nodeA]["password"]) != "secret" || asString(candidates[`["subscription","sub","A"]`]["server"]) != "other.invalid" {
		t.Fatal("candidates")
	}
	var seen map[string]any
	f.runner.fn = func(command health.Command) (health.Result, error) {
		payload := decodeMap(t, command.Stdin)
		seen = asMap(asSlice(payload["nodes"])[0])
		return probeBy(constant(1))(command)
	}
	if _, err := f.runTests(nil, []string{`["subscription","sub","cached"]`}); err != nil {
		t.Fatal(err)
	}
	if asString(seen["password"]) != "cached-secret" || asString(seen["sni"]) != "tls.invalid" {
		t.Fatal(encode(seen))
	}
}

func TestInvalidWorkerOutputDoesNotReplaceHistory(t *testing.T) {
	for _, output := range []string{"not JSON", "{}", `{"version":4,"results":[]}`,
		`{"version": 4, "results": [{"name": "[\"node\",\"a\"]", "latency": true}]}`,
		`{"version": 4, "results": [{"name": "[\"node\",\"a\"]", "latency": null}]}`} {
		f := setup(t)
		f.runner.fn = func(health.Command) (health.Result, error) { return health.Result{Stdout: []byte(output)}, nil }
		_, err := f.runTests([]string{"A"}, nil)
		if _, ok := err.(*health.ProbeUnavailable); !ok {
			t.Fatalf("%s: %v", output, err)
		}
		if len(f.stored()) != 0 {
			t.Fatal("history replaced")
		}
	}
}

func TestWorkerTimeoutReleasesLockAndPreservesResults(t *testing.T) {
	f := setup(t)
	f.runner.fn = func(health.Command) (health.Result, error) { return health.Result{}, health.ErrTimeout }
	for i := 0; i < 2; i++ {
		r := f.post(names("A")).WantStatus(503)
		if r.Contains("secret") {
			t.Fatal("leak")
		}
	}
	if len(f.stored()) != 0 {
		t.Fatal("stored")
	}
}

func TestInvalidWorkerDNSIsExplainedWithoutLeakingSettings(t *testing.T) {
	f := setup(t)
	previous := map[string]any{
		nodeA: map[string]any{"latency": 100, "history": []any{100}, "method": health.Method},
	}
	f.writeLatency(previous)
	f.runner.fn = func(health.Command) (health.Result, error) {
		return health.Result{ReturnCode: 3, Stderr: []byte("https://user:secret@invalid-dns.example")}, nil
	}
	r := f.post(names("A")).WantStatus(503)
	if !strings.Contains(asString(decodeMap(t, r.Body)["message"]), "节点测速 DNS 设置无效") || r.Contains("secret") || r.Contains("invalid-dns.example") {
		t.Fatal(r.Text())
	}
	if diff := testjson.Diff(previous, f.stored()); diff != "" {
		t.Fatalf("history changed: %s", diff)
	}
}

func TestOldWorkerIsRejectedWithoutReplacingCurrentHistory(t *testing.T) {
	for _, version := range []int{1, 2, 3} {
		f := setup(t)
		previous := map[string]any{
			nodeA: map[string]any{"latency": 100, "history": []any{100}, "method": health.Method},
		}
		f.writeLatency(previous)
		f.runner.fn = func(health.Command) (health.Result, error) {
			return stdout(map[string]any{
				"version": version,
				"results": []any{map[string]any{"name": "A", "latency": 1}},
			}), nil
		}
		r := f.post(names("A")).WantStatus(503)
		if !strings.Contains(asString(decodeMap(t, r.Body)["message"]), "版本不匹配") || !testjson.Equal(f.stored(), previous) {
			t.Fatal(r.Text())
		}
	}
}

func TestFullBatchUsesOneProbeTimeoutAndPreservesEveryResult(t *testing.T) {
	f := setup(t)
	var list []any
	var nameList []string
	for i := 0; i < 20; i++ {
		node := nodeA_()
		node["id"] = "id-" + itoa(i)
		node["name"] = "node-" + itoa(i)
		list = append(list, node)
		nameList = append(nameList, "node-"+itoa(i))
	}
	f.saveShared("nodes", list)
	f.runner.fn = func(command health.Command) (health.Result, error) {
		payload := decodeMap(t, command.Stdin)
		timeout := asInt(payload["timeout_ms"])
		if command.Timeout != time.Duration(timeout)*time.Millisecond+5*time.Second {
			t.Errorf("timeout %v", command.Timeout)
		}
		var out []any
		for _, node := range asSlice(payload["nodes"]) {
			out = append(out, map[string]any{
				"name": asMap(node)["name"], "latency": nil,
				"error_code": "timeout", "error": "连接或请求超时",
			})
		}
		return stdout(map[string]any{"version": 4, "results": out}), nil
	}
	r := f.post(names(nameList...)).WantStatus(200)
	payloads := f.runner.payloads(t)
	if len(payloads) != 1 || len(asSlice(payloads[0]["nodes"])) != 20 || len(results(t, r)) != 20 {
		t.Fatal(len(payloads))
	}
	for _, entry := range f.stored() {
		if asString(asMap(entry)["error_code"]) != "timeout" {
			t.Fatal("error_code")
		}
	}
}

func itoa(i int) string { return strconv.Itoa(i) }

func TestMoreThanFiveRemoteConversionsAreRejectedBeforeConversionOrProbe(t *testing.T) {
	f := setup(t)
	var list []any
	for i := 0; i < 6; i++ {
		list = append(list, map[string]any{"id": "uri-" + itoa(i), "name": "URI " + itoa(i), "proxy_string": "ss://example"})
	}
	f.saveShared("nodes", list)
	binaryAsked := false
	f.env.ProbeBinary = func() (string, error) { binaryAsked = true; return "/test/probe", nil }
	r := f.post(map[string]any{}).WantStatus(400)
	if !strings.Contains(asString(decodeMap(t, r.Body)["message"]), "5 个需要 Sub-Store 转换") || len(f.conv.calls) > 0 ||
		len(f.runner.calls) > 0 || binaryAsked || len(f.stored()) != 0 {
		t.Fatal(r.Text())
	}
}

func TestFiveURIAndFifteenLocalNodesShareOneProbeBatch(t *testing.T) {
	f := setup(t)
	var list []any
	for i := 0; i < 5; i++ {
		list = append(list, map[string]any{"id": "uri-" + itoa(i), "name": "URI " + itoa(i), "proxy_string": "ss://example-" + itoa(i)})
	}
	for i := 0; i < 15; i++ {
		node := nodeA_()
		node["id"] = "local-" + itoa(i)
		node["name"] = "Local " + itoa(i)
		list = append(list, node)
	}
	f.saveShared("nodes", list)
	f.conv.fn = func(string) map[string]any {
		return map[string]any{"type": "http", "server": "converted.invalid", "port": 8080}
	}
	r := f.post(map[string]any{}).WantStatus(200)
	payloads := f.runner.payloads(t)
	if len(f.conv.calls) != 5 || len(payloads) != 1 || len(asSlice(payloads[0]["nodes"])) != 20 || len(results(t, r)) != 20 {
		t.Fatal(r.Text())
	}
}

func TestTwentyJSONAndYAMLNodesNeedNoRemoteConversion(t *testing.T) {
	f := setup(t)
	rawJSON := `{"type":"trojan","server":"local.invalid","port":443,"password":"test-only"}`
	rawYAML := "type: trojan\nserver: local.invalid\nport: 443\npassword: test-only\n"
	var list []any
	for i := 0; i < 20; i++ {
		raw := rawYAML
		if i%2 == 1 {
			raw = rawJSON
		}
		list = append(list, map[string]any{"id": "local-" + itoa(i), "name": "Local " + itoa(i), "proxy_string": raw})
	}
	f.saveShared("nodes", list)
	r := f.post(map[string]any{}).WantStatus(200)
	if len(f.conv.calls) != 0 || len(results(t, r)) != 20 {
		t.Fatal(r.Text())
	}
	for _, entry := range results(t, r) {
		if asInt(asMap(entry)["latency"]) != 10 {
			t.Fatal("latency")
		}
	}
}

func TestParseFailureIsAResultNotMissing(t *testing.T) {
	f := setup(t)
	f.saveShared("nodes", []any{map[string]any{"id": "a", "name": "A", "proxy_string": "ss://unconvertible"}})
	out, err := f.runTests([]string{"A", "deleted"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if asString(asMap(asMap(out["results"])["A"])["error_code"]) != "config" || encode(out["missing"]) != `["deleted"]` {
		t.Fatal(encode(out))
	}
	if len(f.runner.calls) != 0 {
		t.Fatal("worker called without proxies")
	}
}

func TestBusyAndBatchLimits(t *testing.T) {
	f := setup(t)
	release := health.LockForTest()
	f.post(names("A")).WantStatus(409)
	release()
	var many []string
	for i := 0; i < 21; i++ {
		many = append(many, itoa(i))
	}
	f.post(names(many...)).WantStatus(400)
	f.post(keys(many...)).WantStatus(400)
	for _, body := range []any{
		[]any{}, "text",
		map[string]any{"names": []any{nil}}, map[string]any{"names": "A"},
		map[string]any{"keys": []any{nil}}, map[string]any{"keys": nodeA},
		map[string]any{"names": []any{"A"}, "keys": []any{nodeA}},
		map[string]any{"names": []any{}, "keys": []any{}},
	} {
		f.post(body).WantStatus(400)
	}
}

func TestSameNameSourcesHaveIndependentMeasurementsAndHistory(t *testing.T) {
	f := setup(t)
	f.saveShared("subscriptions", []any{map[string]any{"id": "first"}, map[string]any{"id": "second"}})
	for _, id := range []string{"first", "second"} {
		f.cache(id, map[string]any{"name": "A", "type": "http", "server": id + ".invalid", "port": 8080})
	}
	first, second := `["subscription","first","A"]`, `["subscription","second","A"]`
	latencies := map[string]int{"example.invalid": 10, "first.invalid": 20, "second.invalid": 30}
	f.runner.fn = probeBy(func(proxy map[string]any) any { return latencies[asString(proxy["server"])] })
	r := f.post(keys(nodeA, first, second)).WantStatus(200)
	got := map[string]int64{}
	for key, entry := range results(t, r) {
		got[key] = asInt(asMap(entry)["latency"])
	}
	if got[nodeA] != 10 || got[first] != 20 || got[second] != 30 {
		t.Fatal(got)
	}
	f.post(keys(second, second))
	last := asSlice(f.runner.payloads(t)[len(f.runner.calls)-1]["nodes"])
	if len(last) != 1 || asString(asMap(last[0])["name"]) != second {
		t.Fatal(encode(last))
	}
	stored := results(t, f.srv.Get("/api/nodes/latency"))
	if encode(asMap(stored[nodeA])["history"]) != "[10]" || encode(asMap(stored[first])["history"]) != "[20]" ||
		encode(asMap(stored[second])["history"]) != "[30,30]" || len(stored) != 3 {
		t.Fatal(encode(stored))
	}
}

func TestKeysReportDeletedDisabledAndFilteredNodesAsMissing(t *testing.T) {
	f := setup(t)
	f.saveShared(
		"nodes", []any{nodeA_(), map[string]any{"id": "disabled", "name": "Disabled", "enabled": false}},
		"subscriptions", []any{map[string]any{"id": "off", "enabled": false}, map[string]any{"id": "filtered", "regex_filter": "^Keep$"}},
	)
	for _, id := range []string{"off", "filtered"} {
		f.cache(id, map[string]any{"name": "Excluded", "type": "http", "server": "unused.invalid", "port": 8080})
	}
	missing := []string{`["node","deleted"]`, `["node","disabled"]`, `["subscription","off","Excluded"]`,
		`["subscription","filtered","Excluded"]`, "A"}
	r := f.post(keys(append([]string{nodeA}, missing...)...)).WantStatus(200)
	if encode(resultKeys(t, r)) != encode([]string{nodeA}) || encode(decodeMap(t, r.Body)["missing"]) != encode(missing) {
		t.Fatal(r.Text())
	}
}

func TestImportedNodeUsesLibraryIDEvenWithASubscriptionSource(t *testing.T) {
	f := setup(t)
	node := nodeA_()
	node["subscription_id"] = "sub"
	f.saveShared("nodes", []any{node}, "subscriptions", []any{map[string]any{"id": "sub"}})
	f.cache("sub", map[string]any{"name": "A", "type": "http", "server": "cached.invalid", "port": 8080})
	f.runner.fn = func(command health.Command) (health.Result, error) {
		payload := decodeMap(t, command.Stdin)
		list := asSlice(payload["nodes"])
		if len(list) != 1 || asString(asMap(list[0])["name"]) != nodeA || asString(asMap(list[0])["server"]) != "example.invalid" {
			t.Errorf("%s", command.Stdin)
		}
		return probeBy(constant(10))(command)
	}
	r := f.post(keys(nodeA)).WantStatus(200)
	if len(results(t, r)) != 1 {
		t.Fatal(r.Text())
	}
}

func TestLegacyNamesAreRejectedWhenTheSourceIsAmbiguous(t *testing.T) {
	f := setup(t)
	f.saveShared("subscriptions", []any{map[string]any{"id": "sub"}})
	f.cache("sub", map[string]any{"name": "A", "type": "http", "server": "other.invalid", "port": 8080})
	r := f.post(names("A")).WantStatus(400)
	if !strings.Contains(asString(decodeMap(t, r.Body)["message"]), "同名节点") || len(f.runner.calls) != 0 || len(f.stored()) != 0 {
		t.Fatal(r.Text())
	}
}

func TestLegacyNamesResponseUsesNamesButPersistsSourceKeys(t *testing.T) {
	f := setup(t)
	r := f.post(names("A", "missing")).WantStatus(200)
	if encode(resultKeys(t, r)) != `["A"]` || encode(decodeMap(t, r.Body)["missing"]) != `["missing"]` {
		t.Fatal(r.Text())
	}
	if encode(resultKeys(t, f.srv.Get("/api/nodes/latency"))) != encode([]string{nodeA}) {
		t.Fatal("persisted keys")
	}
}

func TestSourceKeyEncodingAndDefaultRequestIncludeAllCandidates(t *testing.T) {
	f := setup(t)
	f.saveShared("subscriptions", []any{map[string]any{"id": "机场"}})
	f.cache("机场", map[string]any{"name": `香港 "01"`, "type": "http", "server": "other.invalid", "port": 8080})
	r := f.post(map[string]any{}).WantStatus(200)
	if encode(resultKeys(t, r)) != encode([]string{nodeA, `["subscription","机场","香港 \"01\""]`}) {
		t.Fatal(r.Text())
	}
}

func TestMissingWorkerIsExplicitAndNeverFallsBackToTCP(t *testing.T) {
	f := setup(t)
	f.vars["CONFIGFLOW_NODE_PROBE_BIN"] = filepath.Join(t.TempDir(), "missing")
	_, err := health.FindBinary(f.env)
	if err == nil || !strings.Contains(err.Error(), "未安装") {
		t.Fatal(err)
	}
	// An existing non-executable file is not accepted either; an executable one is.
	path := filepath.Join(t.TempDir(), "probe")
	os.WriteFile(path, []byte("#!/bin/sh\n"), 0o644)
	f.vars["CONFIGFLOW_NODE_PROBE_BIN"] = path
	if _, err := health.FindBinary(f.env); err == nil {
		t.Fatal("non-executable accepted")
	}
	os.Chmod(path, 0o755)
	if found, err := health.FindBinary(f.env); err != nil || found != path {
		t.Fatal(found, err)
	}
}

func TestInvalidURLIsRejected(t *testing.T) {
	for _, url := range []string{"file:///etc/passwd", "https://user:secret@example.com", "not-a-url"} {
		f := setup(t)
		f.vars["CONFIGFLOW_NODE_TEST_URL"] = url
		if _, err := f.healthSettings(); err == nil {
			t.Fatal(url)
		}
	}
}

func systemWithoutRevision(t *testing.T, r *repo.Repository) map[string]any {
	t.Helper()
	system, err := r.System(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := toMap(t, system)
	delete(out, "_revision")
	return out
}

func TestDNSSettingsPersistWithoutChangingNodesOrOtherSettings(t *testing.T) {
	f := setup(t)
	err := f.srv.Repo.UpdateSystem(context.Background(), func(_ context.Context, system *model.System) error {
		system.SystemConfig.RuleFetchProxy = "http://127.0.0.1:7890"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	original := systemWithoutRevision(t, f.srv.Repo)
	shared, err := f.srv.Repo.Shared(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if encode(decodeMap(t, f.srv.Get("/api/nodes/latency/settings").Body)["dns_servers"]) != "[]" {
		t.Fatal("initial dns")
	}
	r := f.srv.Post("/api/nodes/latency/settings", httpxtest.JSON(map[string]any{
		"dns_servers": []any{" 192.0.2.53 ", "192.0.2.53:53", "127.0.0.1:5353"},
	})).WantStatus(200)
	want := []any{"192.0.2.53", "127.0.0.1:5353"}
	if diff := testjson.Diff(want, decodeMap(t, r.Body)["dns_servers"]); diff != "" {
		t.Fatalf("%s: %s", r.Text(), diff)
	}
	expected := toMap(t, original)
	asMap(expected["system_config"])["node_test_dns"] = want
	asMap(expected["system_config"])["node_test_dns_config"] = map[string]any{"mode": "custom", "servers": want, "bootstrap": []any{}}
	if diff := testjson.Diff(expected, systemWithoutRevision(t, f.srv.Repo)); diff != "" {
		t.Fatalf("system diff: %s", diff)
	}
	after, err := f.srv.Repo.Shared(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if diff := testjson.Diff(shared, after); diff != "" {
		t.Fatalf("shared changed: %s", diff)
	}
	fresh := httpxtest.New(t, httpxtest.Options{DataDir: f.srv.DataDir, Register: httpxtest.Routes(nodes.Register)})
	if diff := testjson.Diff(want, decodeMap(t, fresh.Get("/api/nodes/latency/settings").Body)["dns_servers"]); diff != "" {
		t.Fatalf("reload: %s", diff)
	}
}

func TestInvalidDNSSettingsPreserveSavedValue(t *testing.T) {
	values := []any{nil, "192.0.2.53", []any{nil}, []any{""}, []any{"dns.example"},
		[]any{"https://user:secret@dns.example/query"}, []any{"192.0.2.53:0"}, []any{"192.0.2.53:65536"},
		[]any{"192.0.2.53:abc"}, []any{"::1"}, []any{"0.0.0.0"}, []any{"224.0.0.1"},
		[]any{"192.0.2.53", "192.0.2.53", "192.0.2.53", "192.0.2.53", "192.0.2.53"}}
	for _, value := range values {
		f := setup(t)
		f.srv.Post("/api/nodes/latency/settings", httpxtest.JSON(map[string]any{"dns_servers": []any{"127.0.0.1:5353"}})).WantStatus(200)
		r := f.srv.Post("/api/nodes/latency/settings", httpxtest.JSON(map[string]any{"dns_servers": value})).WantStatus(400)
		if r.Contains("secret") {
			t.Fatal("leak")
		}
		if encode(decodeMap(t, f.srv.Get("/api/nodes/latency/settings").Body)["dns_servers"]) != `["127.0.0.1:5353"]` {
			t.Fatal("changed")
		}
	}
}

func TestSavedDNSReachesWorkerAndChangingItStartsNewHistory(t *testing.T) {
	f := setup(t)
	history := func() string {
		r := f.post(names("A")).WantStatus(200)
		return encode(asMap(results(t, r)["A"])["history"])
	}
	lastDNS := func() string { p := f.runner.payloads(t); return encode(p[len(p)-1]["dns_servers"]) }
	history()
	if lastDNS() != "[]" {
		t.Fatal(lastDNS())
	}
	f.srv.Post("/api/nodes/latency/settings", httpxtest.JSON(map[string]any{"dns_servers": []any{"192.0.2.53"}})).WantStatus(200)
	if h := history(); h != "[10]" || lastDNS() != `["192.0.2.53"]` {
		t.Fatal(h)
	}
	p := f.runner.payloads(t)
	if asString(asMap(asSlice(p[len(p)-1]["nodes"])[0])["server"]) != "example.invalid" {
		t.Fatal("server")
	}
	if h := history(); h != "[10,10]" {
		t.Fatal(h)
	}
	f.srv.Post("/api/nodes/latency/settings", httpxtest.JSON(map[string]any{"dns_servers": []any{}})).WantStatus(200)
	if h := history(); h != "[10]" || lastDNS() != "[]" {
		t.Fatal(h)
	}
}

// ---------- test_node_dns.py ----------

func setSystem(t *testing.T, r *repo.Repository, pairs ...any) {
	t.Helper()
	err := r.UpdateSystem(context.Background(), func(_ context.Context, system *model.System) error {
		for i := 0; i < len(pairs); i += 2 {
			raw, err := json.Marshal(pairs[i+1])
			if err != nil {
				return err
			}
			if err := system.SystemConfig.SetJSON(pairs[i].(string), raw); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLegacyMigrationIsReadOnlyAndDoesNotAdoptManualListener(t *testing.T) {
	f := setup(t)
	setSystem(t, f.srv.Repo, "node_test_dns", []any{"192.168.0.2:15353"})
	before := systemWithoutRevision(t, f.srv.Repo)
	config, err := f.loadConfig()
	if err != nil || !testjson.Equal(config, map[string]any{"mode": "custom", "servers": []any{"192.168.0.2:15353"}, "bootstrap": []any{}}) {
		t.Fatal(encode(config), err)
	}
	if diff := testjson.Diff(before, systemWithoutRevision(t, f.srv.Repo)); diff != "" {
		t.Fatalf("rewritten: %s", diff)
	}
}

func TestCanonicalDNSAddresses(t *testing.T) {
	for raw, want := range map[string]string{
		"https://DNS.EXAMPLE": "https://dns.example/dns-query", "https://192.0.2.1:443/dns-query": "https://192.0.2.1/dns-query",
		"tls://DNS.EXAMPLE:853": "tls://dns.example", "tls://192.0.2.1:8853": "tls://192.0.2.1:8853", "192.0.2.53:053": "192.0.2.53",
	} {
		got, err := normalizeServers([]any{raw, raw}, false)
		if err != nil || encode(got) != encode([]any{want}) {
			t.Errorf("%s: %v %v", raw, got, err)
		}
	}
}

func TestInvalidUpstreamsAreRejectedWithoutEchoingSecrets(t *testing.T) {
	for _, raw := range []string{"https://user:password@dns.example/dns-query", "https://dns.example/dns-query?",
		"https://dns.example/dns-query#", "https://dns.example/dns-query?token=secret", "https://dns.example:0",
		"https://dns.example:", "https://dns.example:65536", "https://dns.example/a\nb", "https://dns.example/dns%zz",
		"https://dns.example/dns\x7f", "https://dns.example\\evil", "tls://dns.example/", "http://192.0.2.53",
		"udp://192.0.2.53", "https://[::1]/dns-query", "https://0.0.0.0/dns-query", "https://224.0.0.1/dns-query",
		"tls://bad_host", "tls://dns.example..", "192.0.2.53:0", "192.0.2.53:65536", "192.0.2.53:", "dns.example"} {
		_, err := normalizeServers([]any{raw}, false)
		if err == nil || strings.Contains(err.Error(), "password") || strings.Contains(err.Error(), "secret") {
			t.Errorf("%q: %v", raw, err)
		}
	}
}

func TestCustomEncryptedHostnameRequiresExplicitLiteralBootstrap(t *testing.T) {
	for _, value := range []any{
		map[string]any{"mode": "custom", "servers": []any{"tls://dns.example"}},
		map[string]any{"mode": "custom", "servers": []any{"tls://dns.example"}, "bootstrap": []any{"tls://dns.example"}},
	} {
		if _, err := normalizeConfig(t, value); err == nil || !strings.Contains(err.Error(), "引导") {
			t.Fatal(err)
		}
	}
	config, err := normalizeConfig(t, map[string]any{"mode": "custom", "servers": []any{"tls://192.0.2.1"}})
	if err != nil || encode(config["bootstrap"]) != "[]" {
		t.Fatal(err)
	}
}

func TestCustomSaveIsGlobalAndDoesNotPublishOrChangeProfile(t *testing.T) {
	f := setup(t)
	shared, err := f.srv.Repo.Shared(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	profileObj, err := f.srv.Repo.Profile(context.Background(), "default")
	if err != nil {
		t.Fatal(err)
	}
	profile := toMap(t, profileObj)
	value := map[string]any{"mode": "custom", "servers": []any{"https://DNS.EXAMPLE"}, "bootstrap": []any{"192.0.2.53:53"}}
	body := decodeMap(t, f.srv.Post("/api/nodes/latency/settings", httpxtest.JSON(map[string]any{"dns": value})).WantStatus(200).Body)
	if encode(asMap(body["dns"])["servers"]) != `["https://dns.example/dns-query"]` || encode(body["dns_bootstrap"]) != `["192.0.2.53"]` {
		t.Fatal(encode(body))
	}
	after, err := f.srv.Repo.Shared(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	currentObj, err := f.srv.Repo.Profile(context.Background(), "default")
	if err != nil {
		t.Fatal(err)
	}
	current := toMap(t, currentObj)
	if diff := testjson.Diff(shared, after); diff != "" {
		t.Fatalf("shared changed: %s", diff)
	}
	for key, v := range profile {
		if key != "system_config" && key != "_revision" && !testjson.Equal(current[key], v) {
			t.Fatal("profile changed: " + key)
		}
	}
	loaded, _ := f.loadConfig()
	settings, _ := f.healthSettings()
	if !testjson.Equal(loaded, body["dns"]) || encode(settings["dns_bootstrap"]) != `["192.0.2.53"]` {
		t.Fatal("reload")
	}
	system := f.srv.Post("/api/nodes/latency/settings", httpxtest.JSON(map[string]any{
		"dns": map[string]any{
			"mode": "system", "servers": []any{"invalid"}, "bootstrap": []any{"invalid"},
			"managed": map[string]any{"agent_id": "old"},
		},
	})).WantStatus(200)
	loaded, _ = f.loadConfig()
	if encode(decodeMap(t, system.Body)["dns_servers"]) != "[]" || !testjson.Equal(loaded, map[string]any{"mode": "system", "servers": []any{}, "bootstrap": []any{}}) {
		t.Fatal(system.Text())
	}
}

func TestRemovedManagedModeKeepsQueryingItsListenerAsCustomDNS(t *testing.T) {
	f := setup(t)
	setSystem(t, f.srv.Repo,
		"node_test_dns_config", map[string]any{
			"mode": "managed", "servers": []any{}, "bootstrap": []any{},
			"managed": map[string]any{"agent_id": "dns", "endpoint_id": "e"},
		},
		"node_test_dns", []any{"192.0.2.2:15353"},
	)
	before := systemWithoutRevision(t, f.srv.Repo)
	expected := map[string]any{"mode": "custom", "servers": []any{"192.0.2.2:15353"}, "bootstrap": []any{}}
	loaded, _ := f.loadConfig()
	effective, _ := f.effectiveSettings(nil)
	if !testjson.Equal(loaded, expected) || !testjson.Equal(effective, map[string]any{"dns_servers": []any{"192.0.2.2:15353"}, "dns_bootstrap": []any{}}) {
		t.Fatal(encode(loaded))
	}
	shown := decodeMap(t, f.srv.Get("/api/nodes/latency/settings").Body)
	if _, hasManaged := shown["managed_status"]; !testjson.Equal(shown["dns"], expected) || hasManaged {
		t.Fatal(encode(shown))
	}
	if !testjson.Equal(systemWithoutRevision(t, f.srv.Repo), before) {
		t.Fatal("migrated on read")
	}
	f.srv.Post("/api/nodes/latency/settings", httpxtest.JSON(map[string]any{"dns": shown["dns"]})).WantStatus(200)
	if !testjson.Equal(asMap(systemWithoutRevision(t, f.srv.Repo)["system_config"])["node_test_dns_config"], expected) {
		t.Fatal("saved form")
	}
}

func TestRemovedManagedModeWithoutARecordedAddressFallsBackToSystemDNS(t *testing.T) {
	f := setup(t)
	setSystem(t, f.srv.Repo, "node_test_dns_config", map[string]any{
		"mode": "managed", "servers": []any{}, "bootstrap": []any{},
		"managed": map[string]any{"agent_id": "missing", "endpoint_id": "gone"},
	})
	loaded, err := f.loadConfig()
	if err != nil || !testjson.Equal(loaded, map[string]any{"mode": "system", "servers": []any{}, "bootstrap": []any{}}) {
		t.Fatal(encode(loaded), err)
	}
}

func TestManagedModeCanNoLongerBeSavedOrReached(t *testing.T) {
	f := setup(t)
	before := systemWithoutRevision(t, f.srv.Repo)
	r := f.srv.Post("/api/nodes/latency/settings", httpxtest.JSON(map[string]any{
		"dns": map[string]any{
			"mode": "managed", "managed": map[string]any{"agent_id": "a", "endpoint_id": "e"},
		},
	})).WantStatus(400)
	if !strings.Contains(asString(decodeMap(t, r.Body)["message"]), "系统 DNS 或自定义 DNS") || !testjson.Equal(systemWithoutRevision(t, f.srv.Repo), before) {
		t.Fatal(r.Text())
	}
	f.srv.Get("/api/nodes/latency/dns/managed").WantStatus(404)
	if status := f.srv.Post("/api/nodes/latency/dns/managed", httpxtest.JSON(map[string]any{})).Status; status != 404 && status != 405 {
		t.Fatal(status)
	}
}

func TestDNSCheckUsesOnlyUnsavedDNSConfigAndPreservesHistory(t *testing.T) {
	f := setup(t)
	f.writeLatency(map[string]any{"unchanged": map[string]any{"history": []any{12}}})
	before := systemWithoutRevision(t, f.srv.Repo)
	f.runner.fn = func(health.Command) (health.Result, error) {
		return stdout(map[string]any{
			"version": 4,
			"dns_check": map[string]any{
				"success": true, "addresses": []any{"192.0.2.1", "2001:db8::1"}, "latency_ms": 12,
			},
		}), nil
	}
	r := f.srv.Post("/api/nodes/latency/dns/check", httpxtest.JSON(map[string]any{
		"dns": map[string]any{
			"mode": "custom", "servers": []any{"tls://dns.example"}, "bootstrap": []any{"192.0.2.53"},
		},
	})).WantStatus(200)
	if decodeMap(t, r.Body)["success"] != true {
		t.Fatal(r.Text())
	}
	if len(f.runner.calls) != 1 {
		t.Fatalf("worker calls=%d", len(f.runner.calls))
	}
	want := map[string]any{
		"operation": "dns_check", "timeout_ms": 5000,
		"dns_servers": []any{"tls://dns.example"}, "dns_bootstrap": []any{"192.0.2.53"},
	}
	if diff := testjson.Diff(want, json.RawMessage(f.runner.calls[0].Stdin)); diff != "" {
		t.Fatalf("worker input=%s (%s)", f.runner.calls[0].Stdin, diff)
	}

	if !testjson.Equal(systemWithoutRevision(t, f.srv.Repo), before) {
		t.Fatal("saved")
	}
	stored, err := os.ReadFile(filepath.Join(f.srv.DataDir, "shared", health.LatencyFile))
	if err != nil {
		t.Fatal(err)
	}
	if diff := testjson.Diff(map[string]any{"unchanged": map[string]any{"history": []any{12}}}, json.RawMessage(stored)); diff != "" {
		t.Fatalf("stored diff: %s", diff)
	}
}

func checkWith(t *testing.T, result any) *httpxtest.Result {
	f := setup(t)
	f.runner.fn = func(health.Command) (health.Result, error) {
		return stdout(map[string]any{"version": 4, "dns_check": result}), nil
	}
	return f.srv.Post("/api/nodes/latency/dns/check", httpxtest.JSON(map[string]any{"dns": map[string]any{"mode": "system"}}))
}

func TestCheckFailureIsSafeAndNotASave(t *testing.T) {
	for _, code := range []string{"dns_fake_ip", "unknown"} {
		r := checkWith(t, map[string]any{
			"success": false, "addresses": []any{}, "error_code": code, "error": "private raw exception",
		}).WantStatus(200)
		if decodeMap(t, r.Body)["success"] != false || r.Contains("private raw exception") {
			t.Fatal(r.Text())
		}
	}
}

func TestMalformedCheckDoesNotClaimSuccess(t *testing.T) {
	for _, result := range []any{
		map[string]any{"success": true, "addresses": []any{}},
		map[string]any{"success": true, "addresses": []any{"198.19.0.1"}},
		map[string]any{"success": true, "addresses": []any{"invalid"}},
		map[string]any{"success": true, "addresses": []any{true}},
		map[string]any{"success": true, "addresses": []any{"192.0.2.1"}, "latency_ms": true},
	} {
		checkWith(t, result).WantStatus(503)
	}
}

func TestDNSCheckTimeoutIsBounded(t *testing.T) {
	f := setup(t)
	f.runner.fn = func(command health.Command) (health.Result, error) {
		if command.Timeout != 10*time.Second {
			t.Errorf("timeout %v", command.Timeout)
		}
		return health.Result{Stderr: []byte("secret")}, health.ErrTimeout
	}
	r := f.srv.Post("/api/nodes/latency/dns/check", httpxtest.JSON(map[string]any{"dns": map[string]any{"mode": "system"}})).WantStatus(200)
	if asString(decodeMap(t, r.Body)["error_code"]) != "timeout" || r.Contains("secret") {
		t.Fatal(r.Text())
	}
}

func TestChangingOnlyBootstrapResetsLatencyHistory(t *testing.T) {
	f := setup(t)
	f.saveShared("nodes", []any{map[string]any{"id": "a", "name": "A", "type": "http", "server": "proxy.invalid", "port": 8080}})
	f.runner.fn = probeBy(constant(12))
	value := map[string]any{"mode": "custom", "servers": []any{"tls://dns.example"}, "bootstrap": []any{"192.0.2.53"}}
	if _, err := f.saveConfig(value); err != nil {
		t.Fatal(err)
	}
	f.runTests([]string{"A"}, nil)
	out, _ := f.runTests([]string{"A"}, nil)
	if encode(asMap(asMap(out["results"])["A"])["history"]) != "[12,12]" {
		t.Fatal(encode(out))
	}
	value["bootstrap"] = []any{"192.0.2.54"}
	f.saveConfig(value)
	out, _ = f.runTests([]string{"A"}, nil)
	a := asMap(asMap(out["results"])["A"])
	if encode(a["history"]) != "[12]" || encode(a["dns_bootstrap"]) != `["192.0.2.54"]` {
		t.Fatal(encode(a))
	}
}

func (c *converter) ConvertProxy(ctx context.Context, text string) (*yamlx.Node, error) {
	proxy := c.ConvertProxyString(ctx, text)
	if proxy == nil {
		return nil, nil
	}
	raw, err := json.Marshal(proxy)
	if err != nil {
		return nil, err
	}
	return yamlx.FromJSON(raw)
}

func testObject(t *testing.T, value any, err error) (map[string]any, error) {
	t.Helper()
	if err != nil {
		return nil, err
	}
	return toMap(t, value), nil
}

func (f *fixture) loadResults() map[string]any {
	f.t.Helper()
	out, _ := testObject(f.t, health.LoadLatencyResults(f.ctx(), f.srv.Repo), nil)
	return out
}

func (f *fixture) runTests(names, keys []string) (map[string]any, error) {
	f.t.Helper()
	v, err := health.RunLatencyTests(f.ctx(), f.srv.Repo, names, keys)
	return testObject(f.t, v, err)
}

func (f *fixture) healthSettings() (map[string]any, error) {
	f.t.Helper()
	v, err := health.ResolveSettings(f.ctx(), f.srv.Repo)
	return testObject(f.t, v, err)
}

func (f *fixture) loadConfig() (map[string]any, error) {
	f.t.Helper()
	v, err := health.LoadDNSConfig(f.ctx(), f.srv.Repo)
	return testObject(f.t, v, err)
}

func normalizeConfig(t *testing.T, value any) (map[string]any, error) {
	t.Helper()
	raw, _ := json.Marshal(value)
	v, err := health.NormalizeDNSConfigJSON(raw)
	return testObject(t, v, err)
}

func (f *fixture) effectiveSettings(value any) (map[string]any, error) {
	f.t.Helper()
	raw, _ := json.Marshal(value)
	v, err := health.EffectiveDNSSettingsJSON(f.ctx(), f.srv.Repo, raw)
	return testObject(f.t, v, err)
}

func (f *fixture) saveConfig(value any) (map[string]any, error) {
	f.t.Helper()
	raw, _ := json.Marshal(value)
	v, err := health.SaveDNSConfigJSON(f.ctx(), f.srv.Repo, raw)
	return testObject(f.t, v, err)
}

func normalizeServers(value any, bootstrap bool) ([]any, error) {
	raw, _ := json.Marshal(value)
	servers, err := health.NormalizeDNSServersJSON(raw, bootstrap)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(servers))
	for i, server := range servers {
		out[i] = server
	}
	return out, nil
}

func (f *fixture) collectCandidates() ([]string, map[string]map[string]any, error) {
	f.t.Helper()
	list, err := health.CollectCandidateNodes(f.ctx(), f.srv.Repo)
	if err != nil {
		return nil, nil, err
	}
	keys := make([]string, len(list))
	out := map[string]map[string]any{}
	for i, item := range list {
		keys[i] = item.Key
		out[item.Key] = decodeMap(f.t, item.Raw)
	}
	return keys, out, nil
}

func fixtureSaveSubscriptionNodes(ctx context.Context, id string, value any, metadata map[string]any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var nodes []model.Node
	if err := json.Unmarshal(raw, &nodes); err != nil {
		return err
	}
	var receipt *substore.CacheMetadata
	if metadata != nil {
		raw, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		receipt = &substore.CacheMetadata{}
		if err := json.Unmarshal(raw, receipt); err != nil {
			return err
		}
	}
	_, err = substore.SaveCache(ctx, id, nodes, receipt)
	return err
}
