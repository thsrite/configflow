// Package difftest compares C37's HTTP responses and persisted side effects
// with Flask on copies of one synthetic data directory and a loopback Agent.
package difftest

import (
	"bytes"
	"configflow/internal/repo/repotest"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/api/domaindiscovery"
	"configflow/internal/auth"
	"configflow/internal/discovery"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
	"configflow/internal/model/modeltest"
	"configflow/internal/repo"
	"configflow/internal/tasks"
	"configflow/internal/testjson"
)

func python() string {
	if v := os.Getenv("CONFIGFLOW_DIFF_PYTHON"); v != "" {
		return v
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, "cf-test/.venv/bin/python")
}
func runPython(t *testing.T, dir string, args ...string) {
	t.Helper()
	script, _ := filepath.Abs("testdata/reference.py")
	root, _ := filepath.Abs("../../../..")
	cmd := exec.Command(python(), append([]string{script}, args...)...)
	cmd.Dir = root
	repotest.PythonLayout(t, dir) // Python reads the version-1 layout (O17)
	cmd.Env = append(os.Environ(), "PYTHONPATH="+root, "DATA_DIR="+dir, "ADMIN_USERNAME=", "ADMIN_PASSWORD=", "PYTHONDONTWRITEBYTECODE=1")
	b, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("reference: %v %s", e, b)
	}
}
func startPython(t *testing.T, dir string) string {
	t.Helper()
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	script, _ := filepath.Abs("testdata/reference.py")
	root, _ := filepath.Abs("../../../..")
	cmd := exec.Command(python(), script, "serve", strconv.Itoa(port))
	cmd.Dir = root
	repotest.PythonLayout(t, dir) // Python reads the version-1 layout (O17)
	cmd.Env = append(os.Environ(), "PYTHONPATH="+root, "DATA_DIR="+dir, "ADMIN_USERNAME=", "ADMIN_PASSWORD=", "PYTHONDONTWRITEBYTECODE=1", "LOG_FILE="+filepath.Join(dir, "app.log"))
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() {
			t.Log(output.String())
		}
	})
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	for end := time.Now().Add(30 * time.Second); time.Now().Before(end); {
		res, e := http.Get(base + "/api/domain-discovery/settings")
		if e == nil {
			res.Body.Close()
			return base
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("Flask startup: %s", output.String())
	return ""
}
func call(t *testing.T, base, method, path, body string) (int, any) {
	t.Helper()
	status, value, _, _ := callResponse(t, base, method, path, body)
	return status, value
}

func callResponse(t *testing.T, base, method, path, body string) (int, any, http.Header, []byte) {
	t.Helper()
	req, e := http.NewRequest(method, base+path, strings.NewReader(body))
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Content-Type", "application/json")
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	b, e := io.ReadAll(res.Body)
	if e != nil {
		t.Fatal(e)
	}
	var out any
	if json.Unmarshal(b, &out) != nil {
		out = string(b)
	}
	return res.StatusCode, out, res.Header, b
}

var generated = regexp.MustCompile(`^(?:lib|ruleset)_[0-9a-f-]{32,36}$`)

func normalized(v any, key string) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, v := range x {
			if k == "agent_id" && key == "probe" {
				continue
			}
			out[k] = normalized(v, k)
		}
		return out
	case []any:
		// Flask groups apply results by a set of targets. Only that grouping is
		// unordered; history entries with different timestamps retain their order.
		if key == "added" || key == "skipped" || key == "history" {
			x = append([]any{}, x...)
			sort.SliceStable(x, func(i, j int) bool {
				a, aok := x[i].(map[string]any)
				b, bok := x[j].(map[string]any)
				if !aok || !bok {
					return false
				}
				if key == "history" && a["applied_at"] != b["applied_at"] {
					return fmt.Sprint(a["applied_at"]) < fmt.Sprint(b["applied_at"])
				}
				return fmt.Sprint(a["target"], a["value"]) < fmt.Sprint(b["target"], b["value"])
			})
		}
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = normalized(v, key)
		}
		if key == "cache" {
			sort.Slice(out, func(i, j int) bool { return fmt.Sprint(out[i]) < fmt.Sprint(out[j]) })
		}
		return out
	case string:

		if key == "checked_at" || key == "applied_at" || key == "started_at" || key == "finished_at" || key == "updated_at" {
			return "<time>"
		}
		if key == "id" && len(x) == 32 {
			return "<job-id>"
		}
		return x
	}
	return v
}
func equal(t *testing.T, label string, a, b any) {
	t.Helper()
	a = normalized(a, "")
	b = normalized(b, "")
	if !reflect.DeepEqual(a, b) {
		aa, _ := json.MarshalIndent(a, "", "  ")
		bb, _ := json.MarshalIndent(b, "", "  ")
		t.Errorf("%s\nFlask: %s\nGo: %s", label, aa, bb)
	}
}
func state(t *testing.T, r *repo.Repository, dir string) any {
	t.Helper()
	ctx := context.Background()
	p, e := r.Profile(ctx, "default")
	if e != nil {
		t.Fatal(e)
	}
	shared, e := r.Shared(ctx)
	if e != nil {
		t.Fatal(e)
	}
	pn := nativeJSON(t, p).(map[string]any)
	sn := nativeJSON(t, shared).(map[string]any)
	files := map[string]any{}
	for _, folder := range []string{"probes", "regions"} {
		values := map[string]any{}
		paths, _ := filepath.Glob(filepath.Join(dir, folder, "*.json"))
		for _, path := range paths {
			b, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			var v any
			if e = json.Unmarshal(b, &v); e != nil {
				t.Fatal(e)
			}
			values[strings.TrimSuffix(filepath.Base(path), ".json")] = v
		}
		files[folder] = values
	}
	cache := []any{}
	paths, _ := filepath.Glob(filepath.Join(dir, "shared", "rules", "*"))
	for _, path := range paths {
		if strings.HasSuffix(path, ".lock") {
			continue
		}
		b, e := os.ReadFile(path)
		if e == nil {
			cache = append(cache, string(b))
		}
	}
	files["cache"] = cache
	return map[string]any{"profile": map[string]any{"domain_discovery": pn["domain_discovery"], "rule_configs": pn["rule_configs"]}, "library": sn["rule_library"], "files": files}
}

// storedThroughModel reads both stored states as the plain records hold them
// (O18 T1c): members no record declares and empty optional members are left
// out, values of the wrong type read with the stored-data rules.
func storedThroughModel(state any) any {
	m, ok := state.(map[string]any)
	if !ok {
		return state
	}
	out := map[string]any{"files": m["files"]}
	if profile, ok := m["profile"].(map[string]any); ok {
		out["profile"] = map[string]any{
			"domain_discovery": modeltest.RowThroughModel("profile", "domain_discovery", profile["domain_discovery"]),
			"rule_configs":     modeltest.RowThroughModel("profile", "rule_configs", profile["rule_configs"]),
		}
	}
	out["library"] = modeltest.RowThroughModel("shared", "rule_library", m["library"])
	return testjson.WithoutEmptyMembers(out)
}

func waitJob(t *testing.T, base string, j any, query string) any {
	t.Helper()
	m := j.(map[string]any)
	job, ok := m["job"].(map[string]any)
	if !ok {
		t.Fatalf("missing job: %v", j)
	}
	id := job["id"].(string)
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); {
		_, v := call(t, base, "GET", "/api/domain-discovery/probe/"+id+query, "")
		if v.(map[string]any)["job"].(map[string]any)["status"] != "running" {
			return v
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job timed out")
	return nil
}
func TestHTTPAndStorageParity(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("CONFIGFLOW_DIFF=1 enables Flask differential")
	}
	var mu sync.Mutex
	calls := []map[string]any{}
	var hold chan struct{}
	ready := make(chan struct{}, 2)
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" {
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "targets": []any{map[string]any{"name": "DIRECT", "type": "Direct"}, map[string]any{"name": "PROXY", "type": "Selector"}, map[string]any{"name": "Z-PREFERRED", "type": "Selector"}, map[string]any{"name": "A-OTHER", "type": "Selector"}}})
			return
		}
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		mu.Lock()
		calls = append(calls, p)
		release := hold
		mu.Unlock()
		if r.URL.Path == "/api/domain-probe" && release != nil {
			for _, target := range p["targets"].([]any) {
				if target.(map[string]any)["host"] == "hold.example.net" {
					ready <- struct{}{}
					<-release
					break
				}
			}
		}
		results := []any{}
		if r.URL.Path == "/api/domain-probe" {
			for i, target := range p["targets"].([]any) {
				paths := map[string]any{}
				for _, path := range p["paths"].([]any) {
					ok := 3
					if i >= 2 && path == "DIRECT" {
						ok = 0
					}
					paths[path.(string)] = map[string]any{"ok": ok, "total": 3, "delays": []int{100, 100, 100}, "errors": map[string]int{}}
				}
				results = append(results, map[string]any{"host": target.(map[string]any)["host"], "paths": paths})
			}
		} else {
			for _, request := range p["requests"].([]any) {
				rr := request.(map[string]any)
				entry := map[string]any{"id": rr["id"], "status": 200, "final_url": rr["url"]}
				if !strings.Contains(fmt.Sprint(rr["url"]), "nomarkers.example.net") {
					entry["markers"] = map[string]string{"title": "Synthetic Example", "loc": "US", "ip": "192.0.2.1"}
				}
				results = append(results, entry)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "results": results})
	}))
	defer agent.Close()
	port := agent.Listener.Addr().(*net.TCPAddr).Port
	seed := t.TempDir()
	runPython(t, seed, "seed", strconv.Itoa(port))
	pyDir, goDir := t.TempDir(), t.TempDir()
	for _, dir := range []string{pyDir, goDir} {
		if b, e := exec.Command("cp", "-a", seed+"/.", dir).CombinedOutput(); e != nil {
			t.Fatalf("copy: %v %s", e, b)
		}
	}
	py := startPython(t, pyDir)
	r, e := repo.OpenDefault(goDir)
	if e != nil {
		t.Fatal(e)
	}
	t.Setenv("DATA_DIR", goDir)
	prev := auth.Configure(auth.Config{})
	t.Cleanup(func() { auth.Configure(prev) })
	app := httpx.New()
	app.Repo = r
	disc := discovery.New(discovery.Options{Repo: r})
	domaindiscovery.NewHandler(r, disc).Register(app.Router)
	auth.Install(app)
	goServer := httptest.NewServer(app)
	defer goServer.Close()
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	disc.Register(rt, tasks.NewRegistry(r, rt))
	type request struct{ method, path, body string }
	rs := []request{
		{"POST", "apply", `[1]`}, {"POST", "probe", `[1]`}, {"PUT", "website/note", `[1]`}, {"POST", "website/identify", `[1]`},
		{"GET", "settings", ""}, {"GET", "agents", ""}, {"GET", "domains", ""}, {"GET", "domains?view=failing", ""}, {"GET", "domains?view=ignored", ""}, {"GET", "domains?view=all&days=1&agent_id=missing", ""}, {"GET", "region", ""}, {"GET", "history", ""}, {"GET", "probe/missing", ""},
		{"PUT", "settings", `[]`}, {"PUT", "settings", `{"proxy_ruleset":"u1"}`}, {"PUT", "settings", `{"auto_probe":1}`}, {"PUT", "settings", `{"auto_apply":null}`}, {"PUT", "settings", `{"region_check_enabled":"yes"}`}, {"PUT", "settings", `{"auto_apply_min_confidence":85}`}, {"PUT", "settings", `{"region_probe_port":80}`}, {"PUT", "settings", `{"region_probe_port":18001}`}, {"PUT", "settings", `{"region_probe_port":18002}`}, {"PUT", "settings", `{"region_probe_port":18003,"region_check_enabled":true,"auto_apply_min_confidence":95}`},
		{"POST", "ignore", `{"values":["bad host"]}`}, {"POST", "ignore", `{"values":["EXAMPLE.DEV","example.dev"]}`}, {"GET", "domains?view=ignored", ""}, {"DELETE", "ignore", `{"values":[]}`}, {"DELETE", "ignore", `{"values":["example.dev"]}`},
		{"PUT", "website/note", `[]`}, {"PUT", "website/note", `{"domain":"example.com","note":3}`}, {"PUT", "website/note", `{"domain":"Example.Com.","note":"  synthetic note  "}`}, {"POST", "website/identify", `{"domains":["missing.example.net"]}`}, {"POST", "website/identify", `{"domains":["example.com","missing.example.net"]}`}, {"GET", "domains?view=all", ""}, {"PUT", "website/note", `{"domain":"example.com","note":null}`}, {"PUT", "website/note", `{"domain":"example.com","note":" "}`},
		{"POST", "probe?profile_id=other", `{"domains":["example.com"]}`}, {"POST", "region/check?profile_id=other", `{"services":true}`}, {"POST", "website/identify?profile_id=other", `{"domains":["example.com"]}`}, {"POST", "probe", `{"domains":[]}`}, {"POST", "probe", `{"domains":["example.com"]}`}, {"GET", "domains?view=all", ""},
		{"POST", "apply", `{"items":[{"value":"null-type.example.net","target":"direct","rule_type":null}]}`}, {"POST", "apply", `{"items":[]}`}, {"POST", "apply", `{"items":[null]}`}, {"POST", "apply", `{"items":[{"value":"192.0.2.1","target":"direct"}]}`}, {"POST", "apply", `{"items":[{"value":"EXAMPLE.COM","target":"proxy"},{"value":"www.example.com","rule_type":"DOMAIN","target":"proxy"},{"value":"exists.example.net","target":"direct"},{"value":"new.example.net","rule_type":"DOMAIN","target":"direct"}]}`}, {"GET", "history", ""}, {"GET", "domains", ""}, {"POST", "apply", `{"items":[{"value":"example.com","target":"proxy"}]}`},
		{"POST", "undo", `{"value":"bad host","target":"proxy"}`}, {"POST", "undo", `{"value":"example.com","target":"proxy"}`}, {"POST", "undo", `{"value":"missing.example.net","target":"proxy"}`},
		{"POST", "region/check", `{"services":[]}`}, {"POST", "region/check", `{"services":{}}`}, {"POST", "region/check", `{"services":0.0}`}, {"POST", "region/check", `{"services":false,"domains":false}`}, {"POST", "region/check", `{"services":false,"domains":0}`}, {"POST", "region/check", `{"services":false,"domains":""}`}, {"POST", "region/check", `{"services":false}`}, {"POST", "region/check", `{"services":false,"domains":["example.com"]}`}, {"POST", "region/check", `{"services":false,"domains":["nomarkers.example.net"]}`}, {"POST", "region/check", `{"services":true}`}, {"GET", "region", ""}, {"POST", "region/route", `{"kind":"other"}`}, {"POST", "region/route", `{"kind":"service","value":"missing","policy":"Region"}`}, {"POST", "region/route", `{"kind":"domain","value":"example.com","policy":"missing"}`}, {"POST", "region/route", `{"kind":"domain","value":"example.com","policy":"Region"}`}, {"POST", "undo", `{"value":"example.com","target":"policy:Region"}`},
		{"POST", "apply", `{"items":[{"value":"pending.example.org","target":"proxy"}]}`},
		{"POST", "rulesets/init", `{"targets":false,"proxy_policy":"PROXY"}`}, {"POST", "rulesets/init", `{"targets":{},"proxy_policy":"PROXY"}`}, {"POST", "rulesets/init", `{"targets":["other"]}`}, {"POST", "rulesets/init", `{"proxy_policy":"missing"}`}, {"POST", "rulesets/init", `{"proxy_policy":"PROXY"}`}, {"POST", "rulesets/init", `{"targets":["direct"]}`},
		{"PUT", "settings", `{"direct_ruleset":false,"proxy_ruleset":0}`}, {"PUT", "settings", `{"direct_ruleset":{},"proxy_ruleset":[]}`},
	}
	pyIDs, goIDs := map[string]string{}, map[string]string{}
	for i, q := range rs {
		t.Run(fmt.Sprintf("%02d_%s_%s", i, q.method, q.path), func(t *testing.T) {
			path := "/api/domain-discovery/" + q.path
			scope := ""
			if strings.Contains(q.path, "profile_id=other") {
				scope = "?profile_id=other"
			}
			mu.Lock()
			before := len(calls)
			mu.Unlock()
			pc, pv, ph, pb := callResponse(t, py, q.method, path, q.body)
			if pc == 202 {
				pv = waitJob(t, py, pv, scope)
			}
			mu.Lock()
			middle := len(calls)
			mu.Unlock()
			gc, gv, gh, gb := callResponse(t, goServer.URL, q.method, path, q.body)
			typedRootError := q.method == "POST" && (q.path == "apply" || q.path == "probe") && q.body == `[1]`
			if typedRootError {
				if pc != 500 || gc != 400 {
					t.Fatalf("explicit non-object request: Python=%d Go=%d", pc, gc)
				}
				if body, ok := gv.(map[string]any); !ok || body["success"] != false || body["message"] != "请求必须是 JSON 对象" {
					t.Fatalf("typed root diagnostic: %v", gv)
				}
			} else if pc != gc {
				t.Errorf("status Flask=%d Go=%d", pc, gc)
			}
			if pc == 202 && gc == 202 {
				gv = waitJob(t, goServer.URL, gv, scope)
			}
			mu.Lock()
			left := append([]map[string]any{}, calls[before:middle]...)
			right := append([]map[string]any{}, calls[middle:]...)
			mu.Unlock()
			equal(t, "Agent requests", nativeJSON(t, left), nativeJSON(t, right))
			if q.method != "GET" {
				_, ps := call(t, py, "GET", "/__state", "")
				gs := state(t, r, goDir)
				pyIDs = identities(ps)
				goIDs = identities(gs)
				equal(t, "storage", remap(storedThroughModel(ps), pyIDs), remap(storedThroughModel(gs), goIDs))
			}
			if q.path == "history" {
				// The history endpoint wraps the same stored entries in items.
				// A plain history entry writes its always-written members
				// even when the stored entry lacked them (O18 T1c), so empty
				// members are left out on both sides.
				for _, v := range []any{pv, gv} {
					if m, ok := v.(map[string]any); ok {
						m["items"] = normalized(testjson.WithoutEmptyMembers(m["items"]), "history")
					}
				}
			}
			// Go-only M18 addition: traffic store health fields on discovery Agent entries.
			// Assert Python omits them and Go reports healthy values on the valid seed, then
			// compare every remaining field strictly.
			if (q.path == "agents" || strings.HasPrefix(q.path, "domains")) && pc == 200 && gc == 200 {
				stripHealthyStoreState(t, pv, gv)
			}
			// Step B #3: only the exact public error envelope changes format.
			if !typedRootError && !httpxtest.MigratedAPIError(q.method, pc, gc, ph, gh, pb, gb) {
				equal(t, "response", remap(pv, pyIDs), remap(gv, goIDs))
			}
		})
	}
	// Start both jobs without waiting, then compare same-profile and cross-profile conflicts.
	for _, base := range []string{py, goServer.URL} {
		status, _ := call(t, base, "PUT", "/api/domain-discovery/settings", `{"direct_ruleset":"d1","proxy_ruleset":"p1"}`)
		if status != 200 {
			t.Fatal("restore probe prerequisites")
		}
	}
	release := make(chan struct{})
	mu.Lock()
	hold = release
	mu.Unlock()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	pc, pj := call(t, py, "POST", "/api/domain-discovery/probe", `{"domains":["hold.example.net"]}`)
	gc, gj := call(t, goServer.URL, "POST", "/api/domain-discovery/probe", `{"domains":["hold.example.net"]}`)
	if pc != 202 || gc != 202 {
		t.Fatalf("blocked jobs: %d %d", pc, gc)
	}
	for range 2 {
		select {
		case <-ready:
		case <-time.After(3 * time.Second):
			t.Fatal("Agent requests did not reach barrier")
		}
	}
	for _, q := range []request{{"POST", "region/check", `{"services":true}`}, {"POST", "probe?profile_id=other", `{"domains":["example.com"]}`}} {
		pc, pv := call(t, py, q.method, "/api/domain-discovery/"+q.path, q.body)
		gc, gv := call(t, goServer.URL, q.method, "/api/domain-discovery/"+q.path, q.body)
		if pc != 409 || gc != 409 {
			t.Fatalf("busy response: %d %d", pc, gc)
		}
		equal(t, "busy isolation", pv, gv)
	}
	for _, server := range []struct {
		base string
		job  any
	}{{py, pj}, {goServer.URL, gj}} {
		id := server.job.(map[string]any)["job"].(map[string]any)["id"].(string)
		status, _ := call(t, server.base, "GET", "/api/domain-discovery/probe/"+id+"?profile_id=other", "")
		if status != 404 {
			t.Fatalf("cross-profile polling returned %d", status)
		}
	}
	close(release)
	equal(t, "finished blocked jobs", waitJob(t, py, pj, ""), waitJob(t, goServer.URL, gj, ""))

	for _, path := range []string{"settings", "agents", "domains", "history", "region", "probe/missing"} {
		pc, pv := call(t, py, "GET", "/api/domain-discovery/"+path+"?profile_id=missing", "")
		gc, gv := call(t, goServer.URL, "GET", "/api/domain-discovery/"+path+"?profile_id=missing", "")
		if pc != gc {
			t.Errorf("missing profile status: %d %d", pc, gc)
		}
		equal(t, "missing profile", pv, gv)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) == 0 {
		t.Fatal("Agent boundary was not exercised")
	}
	t.Logf("%d requests and storage snapshots, %d Agent calls", len(rs), len(calls))
}

func nativeJSON(t *testing.T, v any) any {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	var out any
	if e = json.Unmarshal(b, &out); e != nil {
		t.Fatal(e)
	}
	return out
}

func stripHealthyStoreState(t *testing.T, pyBody, goBody any) {
	t.Helper()
	pyMap, pyOK := pyBody.(map[string]any)
	goMap, goOK := goBody.(map[string]any)
	if !pyOK || !goOK {
		return
	}
	pyAgents, _ := pyMap["agents"].([]any)
	goAgents, _ := goMap["agents"].([]any)
	for i := 0; i < len(pyAgents) && i < len(goAgents); i++ {
		pa, pok := pyAgents[i].(map[string]any)
		ga, gok := goAgents[i].(map[string]any)
		if !pok || !gok {
			continue
		}
		if _, has := pa["store_state"]; has {
			t.Fatalf("unexpected Python store_state on agent[%d]: %v", i, pa["store_state"])
		}
		if _, has := pa["store_message"]; has {
			t.Fatalf("unexpected Python store_message on agent[%d]: %v", i, pa["store_message"])
		}
		if ga["store_state"] != "ok" || ga["store_message"] != "" {
			t.Fatalf("Go M18 store health on healthy seed agent[%d]: store_state=%v store_message=%v", i, ga["store_state"], ga["store_message"])
		}
		delete(ga, "store_state")
		delete(ga, "store_message")
	}
}

// Canonicalize generated identities by persisted library name and reference
// position, preserving relationships so cross-target writes cannot be masked.
func identities(v any) map[string]string {
	out := map[string]string{}
	s := v.(map[string]any)
	for _, entry := range s["library"].([]any) {
		item := entry.(map[string]any)
		id, _ := item["id"].(string)
		if generated.MatchString(id) {
			out[id] = "<library:" + item["name"].(string) + ">"
		}
	}
	for i, entry := range s["profile"].(map[string]any)["rule_configs"].([]any) {
		item := entry.(map[string]any)
		id, _ := item["id"].(string)
		if generated.MatchString(id) {
			out[id] = fmt.Sprintf("<reference:%d>", i)
		}
	}
	return out
}
func remap(v any, ids map[string]string) any {
	switch x := v.(type) {
	case string:
		if id, ok := ids[x]; ok {
			return id
		}
	case map[string]any:
		out := map[string]any{}
		for k, v := range x {
			out[k] = remap(v, ids)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = remap(v, ids)
		}
		return out
	}
	return v
}

func TestApplyNormalizationKeepsEmptyHistoryAndOrderedArrays(t *testing.T) {
	entry := func(target, at string) any {
		return map[string]any{"value": "example.net", "target": target, "applied_at": at}
	}
	a := []any{map[string]any{}, entry("proxy", "2026-01-01"), entry("direct", "2026-01-01")}
	b := []any{map[string]any{}, entry("direct", "2026-01-01"), entry("proxy", "2026-01-01")}
	if !reflect.DeepEqual(normalized(a, "history"), normalized(b, "history")) {
		t.Fatal("same-time target groups must compare equally with an empty legacy entry")
	}
	later := []any{entry("proxy", "2026-01-01"), entry("direct", "2026-01-02")}
	earlier := []any{entry("proxy", "2026-01-02"), entry("direct", "2026-01-01")}
	if reflect.DeepEqual(normalized(later, "history"), normalized(earlier, "history")) {
		t.Fatal("history chronology must remain observable")
	}
	if reflect.DeepEqual(normalized(a, "usable"), normalized(b, "usable")) {
		t.Fatal("ordered arrays must remain observable")
	}
}
