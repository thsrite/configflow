package nodes_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/api/nodes"
	"configflow/internal/health"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/substore"
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

func (r *runner) payloads(t *testing.T) []*jsonx.Object {
	var out []*jsonx.Object
	for _, call := range r.calls {
		value, err := jsonx.Parse(call.Stdin)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, value.(*jsonx.Object))
	}
	return out
}

func stdout(v any) health.Result {
	data, _ := jsonx.Default(v)
	return health.Result{Stdout: data}
}

// probeBy answers like the Python tests' fake _run_probe: latency per node.
func probeBy(latency func(proxy *jsonx.Object) any) func(health.Command) (health.Result, error) {
	return func(command health.Command) (health.Result, error) {
		payload, _ := jsonx.Parse(command.Stdin)
		var results []any
		for _, node := range payload.(*jsonx.Object).GetList("nodes") {
			proxy := node.(*jsonx.Object)
			results = append(results, jsonx.Obj("name", proxy.Value("name"), "latency", latency(proxy)))
		}
		return stdout(jsonx.Obj("version", jsonx.Int(4), "results", results)), nil
	}
}

func constant(n int) func(*jsonx.Object) any {
	return func(*jsonx.Object) any { return jsonx.Int(n) }
}

type converter struct {
	mu    sync.Mutex
	calls []string
	fn    func(string) *jsonx.Object
}

func (c *converter) ConvertProxyString(_ context.Context, s string) *jsonx.Object {
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
	env    *health.Env
	vars   map[string]string
}

func nodeA_() *jsonx.Object {
	return jsonx.Obj("id", "a", "name", "A", "type", "trojan", "server", "example.invalid", "port", jsonx.Int(443),
		"password", "secret", "tls", true)
}

func setup(t *testing.T) *fixture {
	srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(nodes.Register)})
	f := &fixture{t: t, srv: srv, runner: &runner{fn: probeBy(constant(10))}, conv: &converter{}, vars: map[string]string{}}
	f.env = &health.Env{
		LookupEnv:   func(key string) (string, bool) { v, ok := f.vars[key]; return v, ok },
		Environ:     func() []string { return nil },
		Runner:      f.runner,
		Now:         time.Now,
		ProbeBinary: func() (string, error) { return "/test/probe", nil },
		Converter:   f.conv,
	}
	t.Cleanup(health.SetEnv(f.env))
	f.saveShared("nodes", []any{nodeA_()})
	return f
}

func (f *fixture) ctx() context.Context { return f.srv.Context() }

func (f *fixture) saveShared(pairs ...any) {
	f.t.Helper()
	_, err := f.srv.Repo.UpdateSharedTransaction(context.Background(), func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		for i := 0; i < len(pairs); i += 2 {
			shared.Set(pairs[i].(string), jsonx.DeepCopy(pairs[i+1]))
		}
		return nil, nil
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) cache(subID string, nodeList ...any) {
	f.t.Helper()
	if _, err := substore.SaveSubscriptionNodes(f.ctx(), subID, nodeList, nil); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) post(body any) *httpxtest.Result {
	return f.srv.Post("/api/nodes/latency", httpxtest.JSON(body))
}

func (f *fixture) stored() *jsonx.Object { return health.LoadResults(f.ctx()) }

func (f *fixture) writeLatency(v any) {
	if _, err := f.srv.Repo.WriteSharedJSON(context.Background(), health.LatencyFile, v); err != nil {
		f.t.Fatal(err)
	}
}

func results(r *httpxtest.Result) *jsonx.Object { return r.Object().GetObject("results") }

func names(list ...string) any { return jsonx.Obj("names", jsonx.ListOf(list)) }
func keys(list ...string) any  { return jsonx.Obj("keys", jsonx.ListOf(list)) }

// test_previous_probe_results_are_hidden_and_not_mixed[None / proxy_http_v1]
func TestPreviousProbeResultsAreHiddenAndNotMixed(t *testing.T) {
	for _, method := range []any{nil, "proxy_http_v1"} {
		f := setup(t)
		settings, _ := health.Settings(f.ctx())
		f.writeLatency(jsonx.Obj(nodeA, jsonx.Obj("latency", nil, "history", []any{jsonx.Int(1), nil}, "method", method,
			"url", settings.Value("url"), "unified_delay", true)))
		if f.stored().Len() != 0 {
			t.Fatal("old method visible")
		}
		f.runner.fn = probeBy(constant(150))
		out, err := health.RunTests(f.ctx(), []string{"A", "A"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		a := out.GetObject("results").GetObject("A")
		if encode(a.Value("history")) != "[150]" || a.GetString("method") != health.Method {
			t.Fatalf("%v", encode(a))
		}
		f.vars["CONFIGFLOW_NODE_TEST_URL"] = "https://test.example/generate_204"
		out, _ = health.RunTests(f.ctx(), []string{"A"}, nil)
		if encode(out.GetObject("results").GetObject("A").Value("history")) != "[150]" {
			t.Fatal("history mixed across URLs")
		}
	}
}

func encode(v any) string {
	if list, ok := v.([]string); ok {
		v = jsonx.ListOf(list)
	}
	data, _ := jsonx.Compact(v)
	return string(data)
}

func parse(text string) any { v, _ := jsonx.Parse([]byte(text)); return v }

func TestWorkerGetsStdinJSONAndNoCredentialsInCommand(t *testing.T) {
	f := setup(t)
	f.runner.fn = func(command health.Command) (health.Result, error) {
		payload, _ := jsonx.Parse(command.Stdin)
		node := payload.(*jsonx.Object).GetList("nodes")[0].(*jsonx.Object)
		if node.GetString("password") != "secret" || node.GetString("name") != nodeA || command.Timeout != 10*time.Second {
			t.Errorf("payload %s timeout %v", command.Stdin, command.Timeout)
		}
		return stdout(jsonx.Obj("version", jsonx.Int(4), "results", []any{
			jsonx.Obj("name", nodeA, "latency", jsonx.Int(99), "first_latency", jsonx.Int(250))})), nil
	}
	out, err := health.RunTests(f.ctx(), []string{"A"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.runner.calls) != 1 || strings.Join(f.runner.calls[0].Argv, " ") != "/test/probe" {
		t.Fatalf("argv %v", f.runner.calls)
	}
	a := out.GetObject("results").GetObject("A")
	if a.GetInt("latency") != 99 || a.GetInt("first_latency") != 250 {
		t.Fatal(encode(a))
	}
	env := strings.Join(f.runner.calls[0].Env, " ")
	if !strings.Contains(env, "GOMAXPROCS=2") || !strings.Contains(env, "GOMEMLIMIT=128MiB") {
		t.Fatalf("env %s", env)
	}
}

func TestSubscriptionCacheKeepsProtocolOptionsAndDistinctSources(t *testing.T) {
	f := setup(t)
	f.saveShared("subscriptions", []any{jsonx.Obj("id", "sub", "name", "test", "enabled", true)})
	f.cache("sub", jsonx.Obj("name", "A", "type", "http", "server", "other.invalid", "port", jsonx.Int(443)),
		jsonx.Obj("name", "cached", "type", "trojan", "server", "proxy.invalid", "port", jsonx.Int(443),
			"params", jsonx.Obj("password", "cached-secret", "sni", "tls.invalid")))
	_, candidates, err := health.CollectCandidates(f.ctx())
	if err != nil {
		t.Fatal(err)
	}
	if candidates[nodeA].GetString("password") != "secret" || candidates[`["subscription","sub","A"]`].GetString("server") != "other.invalid" {
		t.Fatal("candidates")
	}
	var seen *jsonx.Object
	f.runner.fn = func(command health.Command) (health.Result, error) {
		payload, _ := jsonx.Parse(command.Stdin)
		seen = payload.(*jsonx.Object).GetList("nodes")[0].(*jsonx.Object)
		return probeBy(constant(1))(command)
	}
	if _, err := health.RunTests(f.ctx(), nil, []string{`["subscription","sub","cached"]`}); err != nil {
		t.Fatal(err)
	}
	if seen.GetString("password") != "cached-secret" || seen.GetString("sni") != "tls.invalid" {
		t.Fatal(encode(seen))
	}
}

func TestInvalidWorkerOutputDoesNotReplaceHistory(t *testing.T) {
	for _, output := range []string{"not JSON", "{}", `{"version":4,"results":[]}`,
		`{"version": 4, "results": [{"name": "[\"node\",\"a\"]", "latency": true}]}`,
		`{"version": 4, "results": [{"name": "[\"node\",\"a\"]", "latency": null}]}`} {
		f := setup(t)
		f.runner.fn = func(health.Command) (health.Result, error) { return health.Result{Stdout: []byte(output)}, nil }
		_, err := health.RunTests(f.ctx(), []string{"A"}, nil)
		if _, ok := err.(*health.ProbeUnavailable); !ok {
			t.Fatalf("%s: %v", output, err)
		}
		if f.stored().Len() != 0 {
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
	if f.stored().Len() != 0 {
		t.Fatal("stored")
	}
}

func TestInvalidWorkerDNSIsExplainedWithoutLeakingSettings(t *testing.T) {
	f := setup(t)
	previous := jsonx.Obj(nodeA, jsonx.Obj("latency", jsonx.Int(100), "history", []any{jsonx.Int(100)}, "method", health.Method))
	f.writeLatency(previous)
	f.runner.fn = func(health.Command) (health.Result, error) {
		return health.Result{ReturnCode: 3, Stderr: []byte("https://user:secret@invalid-dns.example")}, nil
	}
	r := f.post(names("A")).WantStatus(503)
	if !strings.Contains(r.Object().GetString("message"), "节点测速 DNS 设置无效") || r.Contains("secret") || r.Contains("invalid-dns.example") {
		t.Fatal(r.Text())
	}
	if !jsonx.Equal(f.stored(), previous) {
		t.Fatal("history changed")
	}
}

func TestOldWorkerIsRejectedWithoutReplacingCurrentHistory(t *testing.T) {
	for _, version := range []int{1, 2, 3} {
		f := setup(t)
		previous := jsonx.Obj(nodeA, jsonx.Obj("latency", jsonx.Int(100), "history", []any{jsonx.Int(100)}, "method", health.Method))
		f.writeLatency(previous)
		f.runner.fn = func(health.Command) (health.Result, error) {
			return stdout(jsonx.Obj("version", jsonx.Int(version), "results", []any{jsonx.Obj("name", "A", "latency", jsonx.Int(1))})), nil
		}
		r := f.post(names("A")).WantStatus(503)
		if !strings.Contains(r.Object().GetString("message"), "版本不匹配") || !jsonx.Equal(f.stored(), previous) {
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
		node.Set("id", "id-"+itoa(i))
		node.Set("name", "node-"+itoa(i))
		list = append(list, node)
		nameList = append(nameList, "node-"+itoa(i))
	}
	f.saveShared("nodes", list)
	f.runner.fn = func(command health.Command) (health.Result, error) {
		payload, _ := jsonx.Parse(command.Stdin)
		timeout := payload.(*jsonx.Object).GetInt("timeout_ms")
		if command.Timeout != time.Duration(timeout)*time.Millisecond+5*time.Second {
			t.Errorf("timeout %v", command.Timeout)
		}
		var out []any
		for _, node := range payload.(*jsonx.Object).GetList("nodes") {
			out = append(out, jsonx.Obj("name", node.(*jsonx.Object).Value("name"), "latency", nil, "error_code", "timeout", "error", "连接或请求超时"))
		}
		return stdout(jsonx.Obj("version", jsonx.Int(4), "results", out)), nil
	}
	r := f.post(names(nameList...)).WantStatus(200)
	payloads := f.runner.payloads(t)
	if len(payloads) != 1 || len(payloads[0].GetList("nodes")) != 20 || results(r).Len() != 20 {
		t.Fatal(len(payloads))
	}
	for _, entry := range f.stored().Values() {
		if entry.(*jsonx.Object).GetString("error_code") != "timeout" {
			t.Fatal("error_code")
		}
	}
}

func itoa(i int) string { return jsonx.Int(i).String() }

func TestMoreThanFiveRemoteConversionsAreRejectedBeforeConversionOrProbe(t *testing.T) {
	f := setup(t)
	var list []any
	for i := 0; i < 6; i++ {
		list = append(list, jsonx.Obj("id", "uri-"+itoa(i), "name", "URI "+itoa(i), "proxy_string", "ss://example"))
	}
	f.saveShared("nodes", list)
	binaryAsked := false
	f.env.ProbeBinary = func() (string, error) { binaryAsked = true; return "/test/probe", nil }
	r := f.post(jsonx.NewObject()).WantStatus(400)
	if !strings.Contains(r.Object().GetString("message"), "5 个需要 Sub-Store 转换") || len(f.conv.calls) > 0 ||
		len(f.runner.calls) > 0 || binaryAsked || f.stored().Len() != 0 {
		t.Fatal(r.Text())
	}
}

func TestFiveURIAndFifteenLocalNodesShareOneProbeBatch(t *testing.T) {
	f := setup(t)
	var list []any
	for i := 0; i < 5; i++ {
		list = append(list, jsonx.Obj("id", "uri-"+itoa(i), "name", "URI "+itoa(i), "proxy_string", "ss://example-"+itoa(i)))
	}
	for i := 0; i < 15; i++ {
		node := nodeA_()
		node.Set("id", "local-"+itoa(i))
		node.Set("name", "Local "+itoa(i))
		list = append(list, node)
	}
	f.saveShared("nodes", list)
	f.conv.fn = func(string) *jsonx.Object {
		return jsonx.Obj("type", "http", "server", "converted.invalid", "port", jsonx.Int(8080))
	}
	r := f.post(jsonx.NewObject()).WantStatus(200)
	payloads := f.runner.payloads(t)
	if len(f.conv.calls) != 5 || len(payloads) != 1 || len(payloads[0].GetList("nodes")) != 20 || results(r).Len() != 20 {
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
		list = append(list, jsonx.Obj("id", "local-"+itoa(i), "name", "Local "+itoa(i), "proxy_string", raw))
	}
	f.saveShared("nodes", list)
	r := f.post(jsonx.NewObject()).WantStatus(200)
	if len(f.conv.calls) != 0 || results(r).Len() != 20 {
		t.Fatal(r.Text())
	}
	for _, entry := range results(r).Values() {
		if entry.(*jsonx.Object).GetInt("latency") != 10 {
			t.Fatal("latency")
		}
	}
}

func TestParseFailureIsAResultNotMissing(t *testing.T) {
	f := setup(t)
	f.saveShared("nodes", []any{jsonx.Obj("id", "a", "name", "A", "proxy_string", "ss://unconvertible")})
	out, err := health.RunTests(f.ctx(), []string{"A", "deleted"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.GetObject("results").GetObject("A").GetString("error_code") != "config" || encode(out.Value("missing")) != `["deleted"]` {
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
	for _, body := range []any{[]any{}, "text", jsonx.Obj("names", []any{nil}), jsonx.Obj("names", "A"),
		jsonx.Obj("keys", []any{nil}), jsonx.Obj("keys", nodeA), jsonx.Obj("names", []any{"A"}, "keys", []any{nodeA}),
		jsonx.Obj("names", []any{}, "keys", []any{})} {
		f.post(body).WantStatus(400)
	}
}

func TestSameNameSourcesHaveIndependentMeasurementsAndHistory(t *testing.T) {
	f := setup(t)
	f.saveShared("subscriptions", []any{jsonx.Obj("id", "first"), jsonx.Obj("id", "second")})
	for _, id := range []string{"first", "second"} {
		f.cache(id, jsonx.Obj("name", "A", "type", "http", "server", id+".invalid", "port", jsonx.Int(8080)))
	}
	first, second := `["subscription","first","A"]`, `["subscription","second","A"]`
	latencies := map[string]int{"example.invalid": 10, "first.invalid": 20, "second.invalid": 30}
	f.runner.fn = probeBy(func(proxy *jsonx.Object) any { return jsonx.Int(latencies[proxy.GetString("server")]) })
	r := f.post(keys(nodeA, first, second)).WantStatus(200)
	got := map[string]int64{}
	for key, entry := range results(r).All() {
		got[key] = entry.(*jsonx.Object).GetInt("latency")
	}
	if got[nodeA] != 10 || got[first] != 20 || got[second] != 30 {
		t.Fatal(got)
	}
	f.post(keys(second, second))
	last := f.runner.payloads(t)[len(f.runner.calls)-1].GetList("nodes")
	if len(last) != 1 || last[0].(*jsonx.Object).GetString("name") != second {
		t.Fatal(encode(last))
	}
	stored := f.srv.Get("/api/nodes/latency").Object().GetObject("results")
	if encode(stored.GetObject(nodeA).Value("history")) != "[10]" || encode(stored.GetObject(first).Value("history")) != "[20]" ||
		encode(stored.GetObject(second).Value("history")) != "[30,30]" || stored.Len() != 3 {
		t.Fatal(encode(stored))
	}
}

func TestKeysReportDeletedDisabledAndFilteredNodesAsMissing(t *testing.T) {
	f := setup(t)
	f.saveShared("nodes", []any{nodeA_(), jsonx.Obj("id", "disabled", "name", "Disabled", "enabled", false)},
		"subscriptions", []any{jsonx.Obj("id", "off", "enabled", false), jsonx.Obj("id", "filtered", "regex_filter", "^Keep$")})
	for _, id := range []string{"off", "filtered"} {
		f.cache(id, jsonx.Obj("name", "Excluded", "type", "http", "server", "unused.invalid", "port", jsonx.Int(8080)))
	}
	missing := []string{`["node","deleted"]`, `["node","disabled"]`, `["subscription","off","Excluded"]`,
		`["subscription","filtered","Excluded"]`, "A"}
	r := f.post(keys(append([]string{nodeA}, missing...)...)).WantStatus(200)
	if encode(results(r).Keys()) != encode([]string{nodeA}) || encode(r.Object().Value("missing")) != encode(missing) {
		t.Fatal(r.Text())
	}
}

func TestImportedNodeUsesLibraryIDEvenWithASubscriptionSource(t *testing.T) {
	f := setup(t)
	node := nodeA_()
	node.Set("subscription_id", "sub")
	f.saveShared("nodes", []any{node}, "subscriptions", []any{jsonx.Obj("id", "sub")})
	f.cache("sub", jsonx.Obj("name", "A", "type", "http", "server", "cached.invalid", "port", jsonx.Int(8080)))
	f.runner.fn = func(command health.Command) (health.Result, error) {
		payload, _ := jsonx.Parse(command.Stdin)
		list := payload.(*jsonx.Object).GetList("nodes")
		if len(list) != 1 || list[0].(*jsonx.Object).GetString("name") != nodeA || list[0].(*jsonx.Object).GetString("server") != "example.invalid" {
			t.Errorf("%s", command.Stdin)
		}
		return probeBy(constant(10))(command)
	}
	r := f.post(keys(nodeA)).WantStatus(200)
	if results(r).Len() != 1 {
		t.Fatal(r.Text())
	}
}

func TestLegacyNamesAreRejectedWhenTheSourceIsAmbiguous(t *testing.T) {
	f := setup(t)
	f.saveShared("subscriptions", []any{jsonx.Obj("id", "sub")})
	f.cache("sub", jsonx.Obj("name", "A", "type", "http", "server", "other.invalid", "port", jsonx.Int(8080)))
	r := f.post(names("A")).WantStatus(400)
	if !strings.Contains(r.Object().GetString("message"), "同名节点") || len(f.runner.calls) != 0 || f.stored().Len() != 0 {
		t.Fatal(r.Text())
	}
}

func TestLegacyNamesResponseUsesNamesButPersistsSourceKeys(t *testing.T) {
	f := setup(t)
	r := f.post(names("A", "missing")).WantStatus(200)
	if encode(results(r).Keys()) != `["A"]` || encode(r.Object().Value("missing")) != `["missing"]` {
		t.Fatal(r.Text())
	}
	if encode(f.srv.Get("/api/nodes/latency").Object().GetObject("results").Keys()) != encode([]string{nodeA}) {
		t.Fatal("persisted keys")
	}
}

func TestSourceKeyEncodingAndDefaultRequestIncludeAllCandidates(t *testing.T) {
	f := setup(t)
	f.saveShared("subscriptions", []any{jsonx.Obj("id", "机场")})
	f.cache("机场", jsonx.Obj("name", `香港 "01"`, "type", "http", "server", "other.invalid", "port", jsonx.Int(8080)))
	r := f.post(jsonx.NewObject()).WantStatus(200)
	if encode(results(r).Keys()) != encode([]string{nodeA, `["subscription","机场","香港 \"01\""]`}) {
		t.Fatal(r.Text())
	}
}

func TestMissingWorkerIsExplicitAndNeverFallsBackToTCP(t *testing.T) {
	f := setup(t)
	f.vars["CONFIGFLOW_NODE_PROBE_BIN"] = filepath.Join(t.TempDir(), "missing")
	_, err := health.FindProbeBinary(f.env)
	if err == nil || !strings.Contains(err.Error(), "未安装") {
		t.Fatal(err)
	}
	// An existing non-executable file is not accepted either; an executable one is.
	path := filepath.Join(t.TempDir(), "probe")
	os.WriteFile(path, []byte("#!/bin/sh\n"), 0o644)
	f.vars["CONFIGFLOW_NODE_PROBE_BIN"] = path
	if _, err := health.FindProbeBinary(f.env); err == nil {
		t.Fatal("non-executable accepted")
	}
	os.Chmod(path, 0o755)
	if found, err := health.FindProbeBinary(f.env); err != nil || found != path {
		t.Fatal(found, err)
	}
}

func TestInvalidURLIsRejected(t *testing.T) {
	for _, url := range []string{"file:///etc/passwd", "https://user:secret@example.com", "not-a-url"} {
		f := setup(t)
		f.vars["CONFIGFLOW_NODE_TEST_URL"] = url
		if _, err := health.Settings(f.ctx()); err == nil {
			t.Fatal(url)
		}
	}
}

func systemWithoutRevision(t *testing.T, r *repo.Repository) *jsonx.Object {
	system, err := r.GetSystem(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	system = system.DeepCopy()
	system.Delete("_revision")
	return system
}

func TestDNSSettingsPersistWithoutChangingNodesOrOtherSettings(t *testing.T) {
	f := setup(t)
	_, err := f.srv.Repo.UpdateSystemTransaction(context.Background(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.GetObject("system_config").Set("rule_fetch_proxy", "http://127.0.0.1:7890")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	original := systemWithoutRevision(t, f.srv.Repo)
	shared, _ := f.srv.Repo.GetShared(context.Background())
	if encode(f.srv.Get("/api/nodes/latency/settings").Object().Value("dns_servers")) != "[]" {
		t.Fatal("initial dns")
	}
	r := f.srv.Post("/api/nodes/latency/settings", httpxtest.JSON(jsonx.Obj("dns_servers",
		[]any{" 192.0.2.53 ", "192.0.2.53:53", "127.0.0.1:5353"}))).WantStatus(200)
	want := []any{"192.0.2.53", "127.0.0.1:5353"}
	if !jsonx.Equal(r.Object().Value("dns_servers"), want) {
		t.Fatal(r.Text())
	}
	expected := original.DeepCopy()
	expected.GetObject("system_config").Set("node_test_dns", want)
	expected.GetObject("system_config").Set("node_test_dns_config", jsonx.Obj("mode", "custom", "servers", want, "bootstrap", []any{}))
	if !jsonx.Equal(systemWithoutRevision(t, f.srv.Repo), expected) {
		t.Fatalf("system %s", encode(systemWithoutRevision(t, f.srv.Repo)))
	}
	after, _ := f.srv.Repo.GetShared(context.Background())
	if !jsonx.Equal(after, shared) {
		t.Fatal("shared changed")
	}
	fresh := httpxtest.New(t, httpxtest.Options{DataDir: f.srv.DataDir, Register: httpxtest.Routes(nodes.Register)})
	if !jsonx.Equal(fresh.Get("/api/nodes/latency/settings").Object().Value("dns_servers"), want) {
		t.Fatal("reload")
	}
}

func TestInvalidDNSSettingsPreserveSavedValue(t *testing.T) {
	values := []any{nil, "192.0.2.53", []any{nil}, []any{""}, []any{"dns.example"},
		[]any{"https://user:secret@dns.example/query"}, []any{"192.0.2.53:0"}, []any{"192.0.2.53:65536"},
		[]any{"192.0.2.53:abc"}, []any{"::1"}, []any{"0.0.0.0"}, []any{"224.0.0.1"},
		[]any{"192.0.2.53", "192.0.2.53", "192.0.2.53", "192.0.2.53", "192.0.2.53"}}
	for _, value := range values {
		f := setup(t)
		f.srv.Post("/api/nodes/latency/settings", httpxtest.JSON(jsonx.Obj("dns_servers", []any{"127.0.0.1:5353"}))).WantStatus(200)
		r := f.srv.Post("/api/nodes/latency/settings", httpxtest.JSON(jsonx.Obj("dns_servers", value))).WantStatus(400)
		if r.Contains("secret") {
			t.Fatal("leak")
		}
		if encode(f.srv.Get("/api/nodes/latency/settings").Object().Value("dns_servers")) != `["127.0.0.1:5353"]` {
			t.Fatal("changed")
		}
	}
}

func TestSavedDNSReachesWorkerAndChangingItStartsNewHistory(t *testing.T) {
	f := setup(t)
	history := func() string {
		r := f.post(names("A")).WantStatus(200)
		return encode(results(r).GetObject("A").Value("history"))
	}
	lastDNS := func() string { p := f.runner.payloads(t); return encode(p[len(p)-1].Value("dns_servers")) }
	history()
	if lastDNS() != "[]" {
		t.Fatal(lastDNS())
	}
	f.srv.Post("/api/nodes/latency/settings", httpxtest.JSON(jsonx.Obj("dns_servers", []any{"192.0.2.53"}))).WantStatus(200)
	if h := history(); h != "[10]" || lastDNS() != `["192.0.2.53"]` {
		t.Fatal(h)
	}
	p := f.runner.payloads(t)
	if p[len(p)-1].GetList("nodes")[0].(*jsonx.Object).GetString("server") != "example.invalid" {
		t.Fatal("server")
	}
	if h := history(); h != "[10,10]" {
		t.Fatal(h)
	}
	f.srv.Post("/api/nodes/latency/settings", httpxtest.JSON(jsonx.Obj("dns_servers", []any{}))).WantStatus(200)
	if h := history(); h != "[10]" || lastDNS() != "[]" {
		t.Fatal(h)
	}
}

// ---------- test_node_dns.py ----------

func setSystem(t *testing.T, r *repo.Repository, pairs ...any) {
	_, err := r.UpdateSystemTransaction(context.Background(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		for i := 0; i < len(pairs); i += 2 {
			system.GetObject("system_config").Set(pairs[i].(string), pairs[i+1])
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLegacyMigrationIsReadOnlyAndDoesNotAdoptManualListener(t *testing.T) {
	f := setup(t)
	setSystem(t, f.srv.Repo, "node_test_dns", []any{"192.168.0.2:15353"})
	before := systemWithoutRevision(t, f.srv.Repo)
	config, err := health.LoadConfig(f.ctx())
	if err != nil || encode(config) != `{"mode":"custom","servers":["192.168.0.2:15353"],"bootstrap":[]}` {
		t.Fatal(encode(config), err)
	}
	if !jsonx.Equal(systemWithoutRevision(t, f.srv.Repo), before) {
		t.Fatal("rewritten")
	}
}

func TestCanonicalDNSAddresses(t *testing.T) {
	for raw, want := range map[string]string{
		"https://DNS.EXAMPLE": "https://dns.example/dns-query", "https://192.0.2.1:443/dns-query": "https://192.0.2.1/dns-query",
		"tls://DNS.EXAMPLE:853": "tls://dns.example", "tls://192.0.2.1:8853": "tls://192.0.2.1:8853", "192.0.2.53:053": "192.0.2.53",
	} {
		got, err := health.NormalizeDNSServers([]any{raw, raw}, false)
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
		_, err := health.NormalizeDNSServers([]any{raw}, false)
		if err == nil || strings.Contains(err.Error(), "password") || strings.Contains(err.Error(), "secret") {
			t.Errorf("%q: %v", raw, err)
		}
	}
}

func TestCustomEncryptedHostnameRequiresExplicitLiteralBootstrap(t *testing.T) {
	for _, value := range []any{jsonx.Obj("mode", "custom", "servers", []any{"tls://dns.example"}),
		jsonx.Obj("mode", "custom", "servers", []any{"tls://dns.example"}, "bootstrap", []any{"tls://dns.example"})} {
		if _, err := health.NormalizeConfig(value); err == nil || !strings.Contains(err.Error(), "引导") {
			t.Fatal(err)
		}
	}
	config, err := health.NormalizeConfig(jsonx.Obj("mode", "custom", "servers", []any{"tls://192.0.2.1"}))
	if err != nil || encode(config.Value("bootstrap")) != "[]" {
		t.Fatal(err)
	}
}

func TestCustomSaveIsGlobalAndDoesNotPublishOrChangeProfile(t *testing.T) {
	f := setup(t)
	shared, _ := f.srv.Repo.GetShared(context.Background())
	profile, _ := f.srv.Repo.GetProfile(context.Background(), "default")
	value := jsonx.Obj("mode", "custom", "servers", []any{"https://DNS.EXAMPLE"}, "bootstrap", []any{"192.0.2.53:53"})
	body := f.srv.Post("/api/nodes/latency/settings", httpxtest.JSON(jsonx.Obj("dns", value))).WantStatus(200).Object()
	if encode(body.GetObject("dns").Value("servers")) != `["https://dns.example/dns-query"]` || encode(body.Value("dns_bootstrap")) != `["192.0.2.53"]` {
		t.Fatal(encode(body))
	}
	after, _ := f.srv.Repo.GetShared(context.Background())
	current, _ := f.srv.Repo.GetProfile(context.Background(), "default")
	if !jsonx.Equal(after, shared) {
		t.Fatal("shared changed")
	}
	for key, v := range profile.All() {
		if key != "system_config" && key != "_revision" && !jsonx.Equal(current.Value(key), v) {
			t.Fatal("profile changed: " + key)
		}
	}
	loaded, _ := health.LoadConfig(f.ctx())
	settings, _ := health.Settings(f.ctx())
	if !jsonx.Equal(loaded, body.Value("dns")) || encode(settings.Value("dns_bootstrap")) != `["192.0.2.53"]` {
		t.Fatal("reload")
	}
	system := f.srv.Post("/api/nodes/latency/settings", httpxtest.JSON(jsonx.Obj("dns", jsonx.Obj("mode", "system",
		"servers", []any{"invalid"}, "bootstrap", []any{"invalid"}, "managed", jsonx.Obj("agent_id", "old"))))).WantStatus(200)
	loaded, _ = health.LoadConfig(f.ctx())
	if encode(system.Object().Value("dns_servers")) != "[]" || encode(loaded) != `{"mode":"system","servers":[],"bootstrap":[]}` {
		t.Fatal(system.Text())
	}
}

func TestRemovedManagedModeKeepsQueryingItsListenerAsCustomDNS(t *testing.T) {
	f := setup(t)
	setSystem(t, f.srv.Repo, "node_test_dns_config", jsonx.Obj("mode", "managed", "servers", []any{}, "bootstrap", []any{},
		"managed", jsonx.Obj("agent_id", "dns", "endpoint_id", "e")), "node_test_dns", []any{"192.0.2.2:15353"})
	before := systemWithoutRevision(t, f.srv.Repo)
	expected := `{"mode":"custom","servers":["192.0.2.2:15353"],"bootstrap":[]}`
	loaded, _ := health.LoadConfig(f.ctx())
	effective, _ := health.EffectiveSettings(f.ctx(), nil)
	if encode(loaded) != expected || encode(effective) != `{"dns_servers":["192.0.2.2:15353"],"dns_bootstrap":[]}` {
		t.Fatal(encode(loaded))
	}
	shown := f.srv.Get("/api/nodes/latency/settings").Object()
	if !jsonx.Equal(shown.Value("dns"), parse(expected)) || shown.Has("managed_status") {
		t.Fatal(encode(shown))
	}
	if !jsonx.Equal(systemWithoutRevision(t, f.srv.Repo), before) {
		t.Fatal("migrated on read")
	}
	f.srv.Post("/api/nodes/latency/settings", httpxtest.JSON(jsonx.Obj("dns", shown.Value("dns")))).WantStatus(200)
	if encode(systemWithoutRevision(t, f.srv.Repo).GetObject("system_config").Value("node_test_dns_config")) != expected {
		t.Fatal("saved form")
	}
}

func TestRemovedManagedModeWithoutARecordedAddressFallsBackToSystemDNS(t *testing.T) {
	f := setup(t)
	setSystem(t, f.srv.Repo, "node_test_dns_config", jsonx.Obj("mode", "managed", "servers", []any{}, "bootstrap", []any{},
		"managed", jsonx.Obj("agent_id", "missing", "endpoint_id", "gone")))
	loaded, err := health.LoadConfig(f.ctx())
	if err != nil || encode(loaded) != `{"mode":"system","servers":[],"bootstrap":[]}` {
		t.Fatal(encode(loaded), err)
	}
}

func TestManagedModeCanNoLongerBeSavedOrReached(t *testing.T) {
	f := setup(t)
	before := systemWithoutRevision(t, f.srv.Repo)
	r := f.srv.Post("/api/nodes/latency/settings", httpxtest.JSON(jsonx.Obj("dns", jsonx.Obj("mode", "managed",
		"managed", jsonx.Obj("agent_id", "a", "endpoint_id", "e"))))).WantStatus(400)
	if !strings.Contains(r.Object().GetString("message"), "系统 DNS 或自定义 DNS") || !jsonx.Equal(systemWithoutRevision(t, f.srv.Repo), before) {
		t.Fatal(r.Text())
	}
	f.srv.Get("/api/nodes/latency/dns/managed").WantStatus(404)
	if status := f.srv.Post("/api/nodes/latency/dns/managed", httpxtest.JSON(jsonx.NewObject())).Status; status != 404 && status != 405 {
		t.Fatal(status)
	}
}

func TestDNSCheckUsesOnlyUnsavedDNSConfigAndPreservesHistory(t *testing.T) {
	f := setup(t)
	f.writeLatency(jsonx.Obj("unchanged", jsonx.Obj("history", []any{jsonx.Int(12)})))
	before := systemWithoutRevision(t, f.srv.Repo)
	f.runner.fn = func(health.Command) (health.Result, error) {
		return stdout(jsonx.Obj("version", jsonx.Int(4), "dns_check", jsonx.Obj("success", true,
			"addresses", []any{"192.0.2.1", "2001:db8::1"}, "latency_ms", jsonx.Int(12)))), nil
	}
	r := f.srv.Post("/api/nodes/latency/dns/check", httpxtest.JSON(jsonx.Obj("dns", jsonx.Obj("mode", "custom",
		"servers", []any{"tls://dns.example"}, "bootstrap", []any{"192.0.2.53"})))).WantStatus(200)
	if r.Object().Value("success") != true {
		t.Fatal(r.Text())
	}
	if len(f.runner.calls) != 1 || string(f.runner.calls[0].Stdin) !=
		`{"operation": "dns_check", "timeout_ms": 5000, "dns_servers": ["tls://dns.example"], "dns_bootstrap": ["192.0.2.53"]}` {
		t.Fatalf("%s", f.runner.calls[0].Stdin)
	}
	if !jsonx.Equal(systemWithoutRevision(t, f.srv.Repo), before) {
		t.Fatal("saved")
	}
	stored, _ := f.srv.Repo.ReadSharedJSON(context.Background(), health.LatencyFile)
	if encode(stored) != `{"unchanged":{"history":[12]}}` {
		t.Fatal(encode(stored))
	}
}

func checkWith(t *testing.T, result any) *httpxtest.Result {
	f := setup(t)
	f.runner.fn = func(health.Command) (health.Result, error) {
		return stdout(jsonx.Obj("version", jsonx.Int(4), "dns_check", result)), nil
	}
	return f.srv.Post("/api/nodes/latency/dns/check", httpxtest.JSON(jsonx.Obj("dns", jsonx.Obj("mode", "system"))))
}

func TestCheckFailureIsSafeAndNotASave(t *testing.T) {
	for _, code := range []string{"dns_fake_ip", "unknown"} {
		r := checkWith(t, jsonx.Obj("success", false, "addresses", []any{}, "error_code", code, "error", "private raw exception")).WantStatus(200)
		if r.Object().Value("success") != false || r.Contains("private raw exception") {
			t.Fatal(r.Text())
		}
	}
}

func TestMalformedCheckDoesNotClaimSuccess(t *testing.T) {
	for _, result := range []any{
		jsonx.Obj("success", true, "addresses", []any{}), jsonx.Obj("success", true, "addresses", []any{"198.19.0.1"}),
		jsonx.Obj("success", true, "addresses", []any{"invalid"}), jsonx.Obj("success", true, "addresses", []any{true}),
		jsonx.Obj("success", true, "addresses", []any{"192.0.2.1"}, "latency_ms", true),
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
	r := f.srv.Post("/api/nodes/latency/dns/check", httpxtest.JSON(jsonx.Obj("dns", jsonx.Obj("mode", "system")))).WantStatus(200)
	if r.Object().GetString("error_code") != "timeout" || r.Contains("secret") {
		t.Fatal(r.Text())
	}
}

func TestChangingOnlyBootstrapResetsLatencyHistory(t *testing.T) {
	f := setup(t)
	f.saveShared("nodes", []any{jsonx.Obj("id", "a", "name", "A", "type", "http", "server", "proxy.invalid", "port", jsonx.Int(8080))})
	f.runner.fn = probeBy(constant(12))
	value := jsonx.Obj("mode", "custom", "servers", []any{"tls://dns.example"}, "bootstrap", []any{"192.0.2.53"})
	if _, err := health.SaveConfig(f.ctx(), value); err != nil {
		t.Fatal(err)
	}
	health.RunTests(f.ctx(), []string{"A"}, nil)
	out, _ := health.RunTests(f.ctx(), []string{"A"}, nil)
	if encode(out.GetObject("results").GetObject("A").Value("history")) != "[12,12]" {
		t.Fatal(encode(out))
	}
	value.Set("bootstrap", []any{"192.0.2.54"})
	health.SaveConfig(f.ctx(), value)
	out, _ = health.RunTests(f.ctx(), []string{"A"}, nil)
	a := out.GetObject("results").GetObject("A")
	if encode(a.Value("history")) != "[12]" || encode(a.Value("dns_bootstrap")) != `["192.0.2.54"]` {
		t.Fatal(encode(a))
	}
}
