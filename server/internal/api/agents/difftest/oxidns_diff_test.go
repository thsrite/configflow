package difftest

// Flask↔Go differential of the DNS monitoring routes (A06): overview,
// dashboard reset, clients, queries, query detail. Both servers point their
// OxiDNS Agents at one loopback fake Agent; its answer depends on the phase
// the test sets before each pair of requests. Compared: status, headers and
// JSON bodies (random generations masked), the requests the fake Agent
// received from each server (path, raw query, Authorization, Accept,
// X-ConfigFlow-Query-Sources), the files under DATA_DIR and the dashboard
// state files (generation masked).
//
//	rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v -run Oxidns ./internal/api/agents/difftest/'

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

// fakeOxidns is the loopback OxiDNS Agent.
type fakeOxidns struct {
	mu    sync.Mutex
	phase string
	log   []string
}

func (f *fakeOxidns) setPhase(phase string) {
	f.mu.Lock()
	f.phase = phase
	f.log = nil
	f.mu.Unlock()
}

func (f *fakeOxidns) take() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.log
	f.log = nil
	return out
}

func section(rows ...any) *jsonx.Object {
	sample := 0
	for _, r := range rows {
		n, _ := r.(*jsonx.Object).Value("count").(jsonx.Number).Int64()
		sample += int(n)
	}
	if rows == nil {
		rows = []any{}
	}
	return jsonx.Obj("sample_size", sample, "rows", rows, "at_limit", false)
}

func oxidnsMetrics(count int) string {
	return strings.Join([]string{
		fmt.Sprintf(`server_request_total{plugin_tag="dns",protocol="udp"} %d`, count),
		`server_failed_total{plugin_tag="dns",protocol="udp"} 1`,
		`server_inflight{plugin_tag="dns",protocol="udp"} 2`,
		fmt.Sprintf(`server_latency_sum_ms{plugin_tag="dns",protocol="udp"} %d.5`, count*3),
		fmt.Sprintf(`server_latency_count{plugin_tag="dns",protocol="udp"} %d`, count),
		fmt.Sprintf(`cache_lookup_total{plugin_tag="cache"} %d`, count),
		fmt.Sprintf(`cache_hit_total{plugin_tag="cache",kind="fresh"} %d`, count/2),
		`cache_hit_total{plugin_tag="cache",kind="stale"} 1`,
		`cache_entry_count{plugin_tag="cache"} 30`,
		fmt.Sprintf(`forward_upstream_query_total{plugin_tag="remote",upstream="https://u:p@dns.test/dns-query?k=s"} %d`, count),
		`forward_upstream_error_total{plugin_tag="remote",upstream="https://u:p@dns.test/dns-query?k=s"} 0`,
		fmt.Sprintf(`forward_upstream_latency_sum_ms{plugin_tag="remote",upstream="https://u:p@dns.test/dns-query?k=s"} %d`, count*7),
		fmt.Sprintf(`forward_upstream_latency_count{plugin_tag="remote",upstream="https://u:p@dns.test/dns-query?k=s"} %d`, count),
		`forward_upstream_query_total{plugin_tag="local",upstream="udp://192.0.2.1:53"} 3`,
	}, "\n")
}

func (f *fakeOxidns) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/api/oxidns/") {
		http.NotFound(w, r)
		return
	}
	f.mu.Lock()
	phase := f.phase
	f.log = append(f.log, fmt.Sprintf("%s %s ?%s auth=%q accept=%q sources=%q", r.Method, r.URL.Path, r.URL.RawQuery,
		r.Header.Get("Authorization"), r.Header.Get("Accept"), r.Header.Get("X-ConfigFlow-Query-Sources")))
	f.mu.Unlock()
	send := func(status int, v any) {
		body, _ := jsonx.Default(v)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write(body)
	}
	raw := func(status int, contentType string, body []byte) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.WriteHeader(status)
		w.Write(body)
	}
	collected, count, instance, revision := 1000, 10, "core-one", strings.Repeat("a", 64)
	switch phase {
	case "p2", "rankfail":
		collected, count = 2000, 14
	case "p3":
		collected, count, instance = 3000, 20, "core-two"
	case "p4":
		collected, count, instance, revision = 4000, 25, "core-two", strings.Repeat("b", 64)
	}
	switch phase {
	case "401":
		send(401, jsonx.Obj("message", "core-password"))
		return
	case "302":
		http.Redirect(w, r, "/api/oxidns/overview", http.StatusFound)
		return
	case "404text":
		raw(404, "text/plain", []byte("404 page not found"))
		return
	case "big":
		raw(200, "application/json", []byte(strings.Repeat(" ", 5*1024*1024+1)))
		return
	case "coreauth":
		send(502, jsonx.Obj("success", false, "code", "CORE_AUTH_FAILED", "message", "core-password"))
		return
	case "codelist":
		send(502, jsonx.Obj("success", false, "code", []any{"x"}))
		return
	case "bom":
		raw(200, "application/json", append([]byte("\xef\xbb\xbf"), []byte(`{"ok": true, "records": [], "next_cursor": null}`)...))
		return
	case "badutf8":
		raw(200, "application/json", []byte("{\"ok\": true, \"records\": [], \"next_cursor\": \"\xff\"}"))
		return
	case "notfound":
		send(404, jsonx.Obj("success", false, "code", "RECORD_NOT_FOUND", "message", "private trace"))
		return
	case "nan":
		raw(200, "application/json", []byte(`{"ok": true, "records": [{"id": 1, "created_at_ms": NaN, "elapsed_ms": 0, "questions_json": [], "answers_json": []}], "next_cursor": null}`))
		return
	}
	switch strings.TrimPrefix(r.URL.Path, "/api/oxidns/") {
	case "overview":
		send(200, jsonx.Obj("success", true,
			"health", jsonx.Obj("version", "1.6.1", "status", "ok", "uptime_ms", 1234.5, "instance_id", instance),
			"capabilities", jsonx.Obj("query_recorders", []any{"records", "other"}),
			"collected_at_ms", collected, "source_revision", revision, "runtime_revision", strings.Repeat("d", 64),
			"upstream_labels", []any{jsonx.Obj("plugin_tag", "remote", "upstream", "https://u:p@dns.test/dns-query?k=s",
				"address", "https://u:p@[2001:DB8::1]:8443/dns-query#f")},
			"metrics", oxidnsMetrics(count), "config", "private configuration"))
	case "rankings":
		if phase == "rankfail" {
			send(502, jsonx.Obj("success", false, "code", "CORE_UNREACHABLE"))
			return
		}
		recorder := r.URL.Query().Get("recorder")
		if recorder == "" {
			recorder = "records"
		}
		blocked := jsonx.Obj("available", true, "reason", "rule_dns", "matcher_tags", []any{"qs.match.a"})
		blocked.Update(section(jsonx.Obj("key", "ads.test", "count", 3)))
		send(200, jsonx.Obj("success", true, "recorder", recorder, "collected_at_ms", collected,
			"source_revision", revision, "runtime_revision", strings.Repeat("d", 64), "instance_id", instance,
			"retention_days", 7, "sample_limit", 10000,
			"clients", section(jsonx.Obj("key", "192.0.2.2", "count", 2), jsonx.Obj("key", "2001:db8::2", "count", 1)),
			"domains", section(jsonx.Obj("key", "b.test", "count", 2), jsonx.Obj("key", "a.test", "count", 2)),
			"blocked", blocked))
	case "clients":
		send(200, jsonx.Obj("ok", true, "recorder", "records", "sample_size", 3,
			"rows", []any{jsonx.Obj("key", "192.168.0.2", "count", 2), jsonx.Obj("key", "2001:db8::2", "count", 1)},
			"token", "core-private-token"))
	case "queries":
		row := jsonx.Obj("id", 42, "created_at_ms", 1792100000123, "elapsed_ms", 0.25, "client_ip", "192.0.2.1",
			"questions_json", []any{jsonx.Obj("name", "例子.test.", "qtype", "A")}, "answers_json", []any{},
			"rcode", "No Error", "resolution_source", "cache", "private_field", "omit")
		send(200, jsonx.Obj("ok", true, "records", []any{row}, "next_cursor", "1792100000123:42"))
	default:
		row := jsonx.Obj("id", 42, "created_at_ms", 1792100000123, "elapsed_ms", 0, "questions_json", []any{},
			"answers_json", []any{}, "steps", []any{jsonx.Obj("tag", "cache")}, "resolution_source", "bogus")
		send(200, jsonx.Obj("ok", true, "record", row))
	}
}

func oxidnsSeed(t *testing.T, dir string, port, closed int) {
	r, err := repo.OpenDefault(dir)
	if err != nil {
		t.Fatal(err)
	}
	agent := func(id, version, service string, p int) *jsonx.Object {
		return jsonx.Obj("id", id, "name", id, "token", "tok-"+id, "profile_id", "default", "service_type", service,
			"host", "127.0.0.1", "port", p, "version", version, "config_version", "0", "enabled", true, "status", "online")
	}
	_, err = r.UpdateSystemTransaction(context.Background(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.Set("agents", []any{
			agent("oxi-old", "1.7.0-go", "oxidns", port), agent("oxi-cli", "1.7.1-go", "oxidns", port),
			agent("oxi-dash", "1.8.14-go", "oxidns", port), agent("oxi-fail", "1.8.20-go", "oxidns", port),
			agent("oxi-ancient", "1.6.0-go", "oxidns", port), agent("mihomo", "1.8.20-go", "mihomo", port),
			agent("oxi-gone", "1.8.14-go", "oxidns", closed),
		})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

type oxidnsRequest struct {
	request
	phase string
}

func oxidnsRequests() []oxidnsRequest {
	J := "application/json"
	gen := []string{"statistics.generation"}
	now := []string{"collected_at"}
	get := func(name, path, phase string, mask ...string) oxidnsRequest {
		return oxidnsRequest{request{name: name, method: "GET", path: path, admin: true, mask: mask}, phase}
	}
	reset := func(name, path, body, contentType, phase string) oxidnsRequest {
		return oxidnsRequest{request{name: name, method: "POST", path: path, admin: true, body: body, contentType: contentType, mask: gen}, phase}
	}
	base := "/api/agents/"
	return []oxidnsRequest{
		get("old overview", base+"oxi-old/oxidns/overview", "p1", now...),
		{request{name: "overview unauthenticated", method: "GET", path: base + "oxi-old/oxidns/overview"}, "p1"},
		{request{name: "overview agent token", method: "GET", path: base + "oxi-old/oxidns/overview",
			headers: map[string]string{"Authorization": "Bearer tok-oxi-old"}}, "p1"},
		{request{name: "overview POST", method: "POST", path: base + "oxi-old/oxidns/overview", admin: true}, "p1"},
		get("overview missing agent", base+"missing/oxidns/overview", "p1"),
		get("overview mihomo agent", base+"mihomo/oxidns/overview", "p1"),
		get("overview ancient agent", base+"oxi-ancient/oxidns/overview", "p1"),
		get("overview unknown arg", base+"oxi-dash/oxidns/overview?unknown=1", "p1"),
		get("overview bad recorder", base+"oxi-dash/oxidns/overview?recorder=a..b", "p1"),
		get("overview bad range", base+"oxi-dash/oxidns/overview?range=1h", "p1"),
		get("overview repeated range", base+"oxi-dash/oxidns/overview?range=24h&range=7d", "p1"),
		get("overview unreachable", base+"oxi-gone/oxidns/overview", "p1"),
		get("dash overview initial", base+"oxi-dash/oxidns/overview", "p1", gen...),
		get("dash overview again", base+"oxi-dash/oxidns/overview?range=24h", "p1", gen...),
		get("dash overview counters", base+"oxi-dash/oxidns/overview", "p2", gen...),
		get("dash overview unknown recorder", base+"oxi-dash/oxidns/overview?recorder=gone", "p2", gen...),
		get("dash overview other recorder", base+"oxi-dash/oxidns/overview?recorder=other&range=7d", "p2", gen...),
		reset("reset empty object", base+"oxi-dash/oxidns/dashboard/reset", "{}", J, "p2"),
		reset("reset recorder", base+"oxi-dash/oxidns/dashboard/reset", `{"recorder": "records"}`, J, "p2"),
		reset("reset removed recorder", base+"oxi-dash/oxidns/dashboard/reset", `{"recorder": "gone"}`, J, "p2"),
		reset("reset no body", base+"oxi-dash/oxidns/dashboard/reset", "", "", "p2"),
		reset("reset BOM body", base+"oxi-dash/oxidns/dashboard/reset", "\ufeff{}", J, "p2"),
		reset("reset bad json", base+"oxi-dash/oxidns/dashboard/reset", "{", J, "p2"),
		reset("reset text body", base+"oxi-dash/oxidns/dashboard/reset", "{}", "text/plain", "p2"),
		reset("reset bad recorder", base+"oxi-dash/oxidns/dashboard/reset", `{"recorder": "../x"}`, J, "p2"),
		reset("reset list", base+"oxi-dash/oxidns/dashboard/reset", `[]`, J, "p2"),
		reset("reset unknown key", base+"oxi-dash/oxidns/dashboard/reset", `{"x": 1}`, J, "p2"),
		reset("reset oversize", base+"oxi-dash/oxidns/dashboard/reset", `{"recorder": "`+strings.Repeat("x", 3000)+`"}`, J, "p2"),
		reset("reset with query", base+"oxi-dash/oxidns/dashboard/reset?a=1", "{}", J, "p2"),
		reset("reset huge int", base+"oxi-dash/oxidns/dashboard/reset", `{"recorder": `+strings.Repeat("1", 1500)+`}`, J, "p2"),
		reset("reset old agent", base+"oxi-cli/oxidns/dashboard/reset", "{}", J, "p2"),
		{request{name: "reset GET", method: "GET", path: base + "oxi-dash/oxidns/dashboard/reset", admin: true}, "p2"},
		get("dash overview core changed", base+"oxi-dash/oxidns/overview", "p3", gen...),
		get("dash overview config changed", base+"oxi-dash/oxidns/overview", "p4", gen...),
		get("fail overview", base+"oxi-fail/oxidns/overview", "rankfail", gen...),
		get("fail overview paused", base+"oxi-fail/oxidns/overview", "rankfail", gen...),
		reset("fail reset", base+"oxi-fail/oxidns/dashboard/reset", "{}", J, "rankfail"),
		get("clients", base+"oxi-cli/oxidns/clients?recorder=records&search=%E6%B5%8B%E8%AF%95+x&since_ms=1&until_ms=2", "p1"),
		get("clients bad search", base+"oxi-cli/oxidns/clients?search=%C2%85", "p1"),
		get("clients unrelated filter", base+"oxi-cli/oxidns/clients?qname=x", "p1"),
		get("clients old agent", base+"oxi-old/oxidns/clients", "p1"),
		get("queries", base+"oxi-old/oxidns/queries?qname=%E3%80%80example&qtype=aaaa&rcode=nxdomain&limit=7&status=all&cursor=1:2", "p1"),
		get("queries defaults", base+"oxi-old/oxidns/queries?recorder=records&qname=+&matcher_tag=m.1", "p1"),
		get("queries exact ip old", base+"oxi-old/oxidns/queries?client_ip=192.0.2.1", "p1"),
		get("queries exact ip", base+"oxi-cli/oxidns/queries?client_ip=2001:0DB8::2", "p1"),
		get("queries bad limit", base+"oxi-old/oxidns/queries?limit=101", "p1"),
		get("queries bad rcode", base+"oxi-old/oxidns/queries?rcode=%E6%8B%92", "p1"),
		get("query detail", base+"oxi-old/oxidns/queries/42?recorder=records", "p1"),
		get("query detail zero", base+"oxi-old/oxidns/queries/0", "p1"),
		get("query detail text", base+"oxi-old/oxidns/queries/abc", "p1"),
		get("query detail extra arg", base+"oxi-old/oxidns/queries/42?limit=5", "p1"),
		get("query not found", base+"oxi-old/oxidns/queries/43", "notfound"),
		get("agent 401", base+"oxi-old/oxidns/queries", "401"),
		get("agent redirect", base+"oxi-old/oxidns/queries", "302"),
		get("agent 404 text", base+"oxi-cli/oxidns/clients", "404text"),
		get("agent big body", base+"oxi-old/oxidns/queries", "big"),
		get("agent core auth", base+"oxi-old/oxidns/overview", "coreauth"),
		get("agent code list", base+"oxi-old/oxidns/overview", "codelist"),
		get("agent BOM", base+"oxi-old/oxidns/queries", "bom"),
		get("agent invalid utf-8", base+"oxi-old/oxidns/queries", "badutf8"),
		get("agent NaN time", base+"oxi-old/oxidns/queries", "nan"),
	}
}

// oxidnsCompare is compare, with masked JSON bodies compared as sorted
// encodings (so 1 and 1.0 differ, unlike jsonx.Equal).
func oxidnsCompare(r request, py, gor response) []string {
	unmasked := r
	unmasked.mask = nil
	diffs := compare(unmasked, py, gor)
	if len(r.mask) == 0 {
		return diffs
	}
	var kept []string
	for _, d := range diffs {
		if !strings.HasPrefix(d, "body ") {
			kept = append(kept, d)
		}
	}
	pv, perr := jsonx.Parse(py.body)
	gv, gerr := jsonx.Parse(gor.body)
	if perr != nil || gerr != nil {
		return diffs
	}
	options := jsonx.Options{SortKeys: true, EnsureASCII: true}
	pt, _ := jsonx.Encode(maskJSON(pv, r.mask), options)
	gt, _ := jsonx.Encode(maskJSON(gv, r.mask), options)
	if string(pt) != string(gt) {
		kept = append(kept, fmt.Sprintf("json python %s go %s", pt, gt))
	}
	return kept
}

// stateFiles is the dashboard state directory: file names (with .lock
// files) and the state documents with the generation masked.
func stateFiles(t *testing.T, dir string) map[string]string {
	out := map[string]string{}
	entries, _ := os.ReadDir(filepath.Join(dir, "dns-monitor"))
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(dir, "dns-monitor", e.Name()))
		if strings.HasSuffix(e.Name(), ".json") {
			value, err := jsonx.Parse(data)
			if err != nil {
				out[e.Name()] = "unreadable: " + string(data)
				continue
			}
			object := value.(*jsonx.Object)
			object.Set("generation", "<masked>")
			text, _ := jsonx.Compact(object)
			out[e.Name()] = string(text)
		} else {
			out[e.Name()] = fmt.Sprintf("%d bytes", len(data))
		}
	}
	return out
}

func TestOxidnsDifferentialAgainstFlask(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on the test server")
	}
	fake := &fakeOxidns{}
	agentServer := httptest.NewServer(fake)
	defer agentServer.Close()
	port, _ := strconv.Atoi(agentServer.URL[strings.LastIndex(agentServer.URL, ":")+1:])
	closedListener, _ := net.Listen("tcp4", "127.0.0.1:0")
	closed := closedListener.Addr().(*net.TCPAddr).Port
	closedListener.Close()

	base := t.TempDir()
	seedDir, pyDir, goDir := filepath.Join(base, "seed"), filepath.Join(base, "python"), filepath.Join(base, "go")
	for _, dir := range []string{seedDir, pyDir, goDir} {
		os.MkdirAll(dir, 0o755)
	}
	oxidnsSeed(t, seedDir, port, closed)
	copyDir(t, seedDir, pyDir)
	copyDir(t, seedDir, goDir)
	pyBase, stopPython := startPython(t, pyDir)
	defer stopPython()
	goBase, stopGo := startGo(t, goDir)
	defer stopGo()
	pyToken, goToken := login(t, pyBase), login(t, goBase)
	failed := 0
	for _, r := range oxidnsRequests() {
		fake.setPhase(r.phase)
		py := send(t, pyBase, r.request, pyToken, nil)
		pyCalls := fake.take()
		gor := send(t, goBase, r.request, goToken, nil)
		goCalls := fake.take()
		diffs := oxidnsCompare(r.request, py, gor)
		if strings.Join(pyCalls, "\n") != strings.Join(goCalls, "\n") {
			diffs = append(diffs, fmt.Sprintf("agent requests\n    python %q\n    go     %q", pyCalls, goCalls))
		}
		if len(diffs) > 0 {
			failed++
			t.Errorf("DIFF %s %s %s:\n  %s", r.name, r.method, r.path, strings.Join(diffs, "\n  "))
		} else {
			t.Logf("same %-34s %s %s → %d (%d agent requests)", r.name, r.method, r.path, py.status, len(pyCalls))
		}
	}
	pyState, goState := stateFiles(t, pyDir), stateFiles(t, goDir)
	names := func(m map[string]string) []string {
		var out []string
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	if fmt.Sprint(names(pyState)) != fmt.Sprint(names(goState)) {
		failed++
		t.Errorf("DIFF dns-monitor files:\n  python %v\n  go     %v", names(pyState), names(goState))
	}
	for name, py := range pyState {
		if goState[name] != py {
			failed++
			t.Errorf("DIFF %s:\n  python %s\n  go     %s", name, py, goState[name])
		} else {
			t.Logf("same dns-monitor/%s", name)
		}
	}
	if py, gofiles := strings.Join(dataFiles(t, pyDir), " "), strings.Join(dataFiles(t, goDir), " "); py != gofiles {
		failed++
		t.Errorf("DIFF files under DATA_DIR:\n  python %s\n  go     %s", py, gofiles)
	}
	t.Logf("%d requests + agent requests + dns-monitor files + DATA_DIR files, %d differences", len(oxidnsRequests()), failed)
}
