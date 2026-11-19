package agents

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	registry "configflow/internal/agents"
	"configflow/internal/discovery"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/repo"
	"configflow/internal/yamlx"
)

type clientWorld struct {
	t         *testing.T
	s         *httpxtest.Server
	in        interface{ Write([]byte) (int, error) }
	out       *bufio.Scanner
	auth      httpxtest.Option
	headers   http.Header
	revisions map[string]string
}

func a24Fixture(t *testing.T) *httpxtest.Server {
	t.Helper()
	registry.ResetRuntime()
	t.Cleanup(registry.ResetRuntime)
	s := httpxtest.New(t, httpxtest.Options{Auth: true, Username: "a24-admin", Password: "a24-password", SecretKey: "a24-synthetic-secret", DefaultFactories: false, Register: httpxtest.Routes(Register)})
	ctx := s.Context()
	if _, err := s.Repo.CreateProfile(ctx, jsonx.Obj("id", "office", "name", "Office"), ""); err != nil {
		t.Fatal(err)
	}
	_, err := s.Repo.UpdateSharedTransaction(ctx, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		shared.Set("rule_library", append(shared.GetList("rule_library"), []any{jsonx.Obj("id", "domains", "name", "Domains", "source_type", "content", "behavior", "domain", "content", "+.exists.example\n", "enabled", true), jsonx.Obj("id", "ip", "name", "IPs", "source_type", "content", "behavior", "classical", "content", "", "enabled", true), jsonx.Obj("id", "cidr", "name", "CIDRs", "source_type", "content", "behavior", "ipcidr", "content", "192.0.2.0/24\n", "enabled", true)}...))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Repo.UpdateProfileTransaction(ctx, "office", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
		p.Set("domain_discovery", jsonx.Obj("direct_ruleset", "domains", "proxy_ruleset", "domains"))
		p.Set("rule_configs", []any{jsonx.Obj("id", "ref-ip", "itemType", "ruleset", "library_rule_id", "ip", "policy", "DIRECT", "enabled", true), jsonx.Obj("id", "ref-domain", "itemType", "ruleset", "library_rule_id", "domains", "policy", "DIRECT", "enabled", true), jsonx.Obj("id", "fallback", "itemType", "rule", "rule_type", "MATCH", "policy", "DIRECT", "enabled", true)})
		p.GetObject("oxidns").Set("device_policies", []any{jsonx.Obj("ip", "192.0.2.5", "name", "旧电脑", "mode", "direct")})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Repo.UpdateSystemTransaction(ctx, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		list := []any{}
		for _, v := range []struct{ id, kind, profile string }{{"m", "mihomo", "office"}, {"d", "oxidns", "office"}, {"peer", "oxidns", "office"}, {"default", "oxidns", "default"}, {"other", "mosdns", "office"}} {
			list = append(list, jsonx.Obj("id", v.id, "name", v.id, "profile_id", v.profile, "service_type", v.kind, "token", "secret-"+v.id, "host", "127.0.0.1", "port", 9, "enabled", true, "last_heartbeat", "2020-01-01T00:00:00", "version", "1.6.0-go"))
		}
		// Legacy Agent labels are not restricted to strings by registration/edit.
		list[2].(*jsonx.Object).Set("name", jsonx.Int(7))
		for i, name := range []any{nil, false, true, jsonx.Int(0), "", []any{}, jsonx.Obj(), jsonx.Obj("label", "future"), []any{"array"}} {
			label := list[2].(*jsonx.Object).Clone()
			label.Set("id", fmt.Sprintf("legacy-label-%d", i))
			label.Set("name", name)
			label.Set("token", fmt.Sprintf("secret-label-%d", i))
			list = append(list, label)
		}
		system.Set("agents", list)
		system.GetObject("system_config").Set("server_domain", "http://127.0.0.1:5001")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, folder := range []string{"traffic", "probes", "regions", "traffic_history"} {
		path := filepath.Join(s.DataDir, folder)
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "m.json"), []byte(`{}`), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return s
}
func newClientWorld(t *testing.T) *clientWorld {
	t.Helper()
	return newClientWorldSetup(t, nil)
}

func newClientWorldSetup(t *testing.T, setup func(*httpxtest.Server)) *clientWorld {
	t.Helper()
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1")
	}
	s := a24Fixture(t)
	if setup != nil {
		setup(s)
	}
	pyDir := t.TempDir()
	if output, err := exec.Command("cp", "-a", s.DataDir+"/.", pyDir).CombinedOutput(); err != nil {
		t.Fatalf("copy: %v %s", err, output)
	}
	root, _ := filepath.Abs("../../../..")
	python := os.Getenv("CONFIGFLOW_DIFF_PYTHON")
	if python == "" {
		home, _ := os.UserHomeDir()
		python = filepath.Join(home, "cf-test/.venv/bin/python")
	}
	cmd := exec.Command(python, filepath.Join(root, "server/internal/api/agents/testdata/client_tools.py"))
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "DATA_DIR="+pyDir, "PYTHONPATH="+root, "PYTHONDONTWRITEBYTECODE=1", "ADMIN_USERNAME=a24-admin", "ADMIN_PASSWORD=a24-password", "JWT_SECRET_KEY=a24-synthetic-secret", "LOG_FILE="+filepath.Join(pyDir, "app.log"))
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		input.Close()
		cmd.Process.Kill()
		cmd.Wait()
		if t.Failed() {
			t.Log(stderr.String())
		}
	})
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 65536), 16<<20)
	login := s.Post("/api/auth/login", httpxtest.JSON(jsonx.Obj("username", "a24-admin", "password", "a24-password"))).WantStatus(200).Object()
	token := login.GetString("token")
	if token == "" {
		t.Fatal("no JWT")
	}
	return &clientWorld{t, s, input, scanner, httpxtest.Bearer(token), http.Header{"Authorization": []string{"Bearer " + token}}, map[string]string{}}
}
func a24Normalized(v any) any {
	raw, _ := json.Marshal(v)
	var copy any
	_ = json.Unmarshal(raw, &copy)
	return a24NormalizeInPlace(copy)
}
func a24NormalizeInPlace(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, v := range x {
			if k == "updated_at" || k == "applied_at" || k == "created_at" {
				if text, ok := v.(string); ok && text != "" {
					x[k] = "<time>"
					continue
				}
			}
			if k == "native_config" || k == "template_base_config" {
				if text, ok := v.(string); ok && text != "" {
					var parsed any
					if yamlx.Unmarshal([]byte(text), &parsed) == nil {
						raw, err := json.Marshal(parsed)
						if err == nil {
							json.Unmarshal(raw, &parsed)
							x[k] = parsed
							continue
						}
					}
				}
			}
			x[k] = a24NormalizeInPlace(v)
		}
	case []any:
		for i, v := range x {
			x[i] = a24NormalizeInPlace(v)
		}
	}
	return v
}
func a24State(t *testing.T, s *httpxtest.Server) any {
	t.Helper()
	document, err := s.Repo.ExportAll(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]any{}
	err = filepath.WalkDir(s.DataDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".json" && ext != ".yaml" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var v any
		if ext == ".json" {
			err = json.Unmarshal(raw, &v)
		} else {
			err = yamlx.Unmarshal(raw, &v)
		}
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(s.DataDir, path)
		files[filepath.ToSlash(rel)] = v
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(map[string]any{"document": document, "files": files})
	var state any
	json.Unmarshal(wire, &state)
	return state
}
func (w *clientWorld) call(method, path string, body any, status int, authorization string) map[string]any {
	w.t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	headers := map[string]string{}
	options := []httpxtest.Option{}
	if authorization == "admin" {
		headers["Authorization"] = w.headers.Get("Authorization")
		options = append(options, w.auth)
	} else if authorization != "" {
		headers["Authorization"] = authorization
		options = append(options, httpxtest.Header("Authorization", authorization))
	}
	if raw != nil {
		options = append(options, httpxtest.JSON(json.RawMessage(raw)))
	}
	pythonBody := string(raw)
	if fields, ok := body.(map[string]any); ok {
		if revision, ok := fields["revision"].(string); ok {
			if replacement, known := w.revisions[revision]; known {
				clone := map[string]any{}
				for k, v := range fields {
					clone[k] = v
				}
				clone["revision"] = replacement
				b, _ := json.Marshal(clone)
				pythonBody = string(b)
			}
		}
	}
	request, _ := json.Marshal(map[string]any{"method": method, "path": path, "body": pythonBody, "headers": headers})
	if _, err := w.in.Write(append(request, '\n')); err != nil {
		w.t.Fatal(err)
	}
	if !w.out.Scan() {
		w.t.Fatal("Python exited")
	}
	var answer struct {
		Status int
		Body   any
		Cache  string
		State  any
		Error  string
	}
	if err := json.Unmarshal(w.out.Bytes(), &answer); err != nil {
		w.t.Fatalf("oracle %v: %s", err, w.out.Bytes())
	}
	if answer.Error != "" {
		w.t.Fatal(answer.Error)
	}
	got := w.s.Do(method, path, options...)
	var value any
	if method != "HEAD" {
		if err := json.Unmarshal(got.Body, &value); err != nil {
			w.t.Fatalf("nonJSON: %s", got.Body)
		}
	} else {
		value = ""
	}
	if got.Status != answer.Status || got.Status != status {
		w.t.Fatalf("%s %s: Go %d %s Python %d %v want%d", method, path, got.Status, got.Body, answer.Status, answer.Body, status)
	}
	comparison := a24Normalized(value)
	expectedBody := a24Normalized(answer.Body)
	if result, ok := value.(map[string]any); ok {
		if revision, ok := result["revision"].(string); ok {
			expected := answer.Body.(map[string]any)["revision"].(string)
			assertA24Revision(w.t, revision, a24State(w.t, w.s), result["profile_id"].(string))
			assertA24Revision(w.t, expected, answer.State, result["profile_id"].(string))
			w.revisions[revision] = expected
			comparison.(map[string]any)["revision"] = "<verified-settings-revision>"
			expectedBody.(map[string]any)["revision"] = "<verified-settings-revision>"
		}
	}
	if !reflect.DeepEqual(comparison, expectedBody) {
		g, _ := json.Marshal(value)
		p, _ := json.Marshal(answer.Body)
		w.t.Fatalf("%s %s body:\nGo %s\nPy %s", method, path, g, p)
	}
	if got.Header.Get("Cache-Control") != answer.Cache {
		w.t.Fatalf("cache header: %s != %s", got.Header.Get("Cache-Control"), answer.Cache)
	}
	state := a24Normalized(a24State(w.t, w.s))
	expected := a24Normalized(answer.State)
	if !reflect.DeepEqual(state, expected) {
		w.t.Fatalf("%s %s state: %s", method, path, a24Difference(expected, state, "state"))
	}
	result, _ := value.(map[string]any)
	return result
}

// Revisions hash the actual settings (including serialized native YAML). Each
// side must issue its own correct revision; only native YAML formatting may differ.
func assertA24Revision(t *testing.T, revision string, state any, id string) {
	t.Helper()
	settings := state.(map[string]any)["document"].(map[string]any)["profiles"].(map[string]any)[id].(map[string]any)["oxidns"]
	raw, _ := json.Marshal(settings)
	node, err := oxidns.FromJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if want := oxidns.Revision(node); revision != want {
		t.Fatalf("revision %s does not identify actual settings %s", revision, want)
	}
}

func a24Difference(a, b any, path string) string {
	if reflect.DeepEqual(a, b) {
		return ""
	}
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok {
			return path + " type"
		}
		for k, v := range x {
			other, ok := y[k]
			if !ok {
				return path + "." + k + " missing"
			}
			if !reflect.DeepEqual(v, other) {
				return a24Difference(v, other, path+"."+k)
			}
		}
		for k := range y {
			if _, ok := x[k]; !ok {
				return path + "." + k + " extra"
			}
		}
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return path + " length/type"
		}
		for i, v := range x {
			if !reflect.DeepEqual(v, y[i]) {
				return a24Difference(v, y[i], fmt.Sprintf("%s[%d]", path, i))
			}
		}
	default:
		return fmt.Sprintf("%s Python=%v Go=%v", path, a, b)
	}
	return path
}

func TestA24FlaskBoundaries(t *testing.T) {
	w := newClientWorld(t)
	paths := []struct{ method, path string }{{"GET", "m/client-notes"}, {"PUT", "m/client-notes"}, {"GET", "m/mihomo/rule-targets"}, {"POST", "m/mihomo/rule-targets/apply"}, {"PUT", "m/domain-discovery"}, {"POST", "m/domain-discovery/clear"}, {"GET", "d/oxidns/device-policies"}, {"PUT", "d/oxidns/device-policies"}}
	for _, p := range paths {
		for _, token := range []string{"", "Bearer secret-m"} {
			w.call(p.method, "/api/agents/"+p.path, map[string]any{}, 401, token)
		}
	}
	notes := "/api/agents/m/client-notes?profile=default"
	w.call("GET", notes, nil, 200, "admin")
	w.call("GET", "/api/agents/m/client-notes?profile=missing&profile_id=..", nil, 200, "admin")
	for _, agent := range []string{"missing", "other"} {
		w.call("GET", "/api/agents/"+agent+"/client-notes", nil, 400, "admin")
	}
	note := map[string]any{"profile_id": "office", "ip": "::ffff:192.0.2.5", "name": "工作电脑", "previous_name": "旧电脑"}
	for _, bad := range []any{[]any{}, map[string]any{}, map[string]any{"extra": true}} {
		w.call("PUT", notes, bad, 400, "admin")
	}
	for key, bad := range map[string]any{"ip": "bad.example", "name": "bad\nname", "previous_name": nil} {
		clone := map[string]any{}
		for k, v := range note {
			clone[k] = v
		}
		clone[key] = bad
		w.call("PUT", notes, clone, 400, "admin")
	}
	w.call("PUT", notes, note, 200, "admin")
	w.call("PUT", notes, note, 409, "admin")
	w.call("GET", "/api/agents/d/client-notes", nil, 200, "admin")
	note["previous_name"] = "工作电脑"
	note["name"] = ""
	w.call("PUT", notes, note, 200, "admin")
	w.call("HEAD", notes, nil, 200, "admin")
	note["profile_id"] = "default"
	w.call("PUT", notes, note, 409, "admin")
	targets := "/api/agents/m/mihomo/rule-targets"
	w.call("GET", targets+"?profile=default", nil, 200, "admin")
	w.call("GET", targets+"?profile=missing", nil, 200, "admin")
	w.call("GET", "/api/agents/d/mihomo/rule-targets", nil, 400, "admin")
	apply := targets + "/apply"
	w.call("POST", apply, map[string]any{}, 400, "admin")
	rule := map[string]any{"profile_id": "office", "ruleset_id": "domains", "target": "direct", "value": "new.example", "rule_type": "DOMAIN"}
	w.call("POST", apply, rule, 200, "admin")
	w.call("POST", apply, rule, 200, "admin")
	for _, v := range []any{"192.0.2.1", "198.18.0.1", "::1", "*.example.test", "", "a.test\nMATCH,DIRECT", nil, 7} {
		rule["value"] = v
		w.call("POST", apply, rule, 400, "admin")
	}
	rule["value"] = "new.example"
	rule["ruleset_id"] = "ip"
	w.call("POST", apply, rule, 409, "admin")
	rule["value"] = "198.51.100.7/24"
	rule["rule_type"] = "IP-CIDR"
	w.call("POST", apply, rule, 200, "admin")
	w.call("POST", apply, rule, 200, "admin")
	rule["value"] = "2001:db8::1"
	w.call("POST", apply, rule, 400, "admin")
	rule["rule_type"] = "IP-CIDR6"
	w.call("POST", apply, rule, 200, "admin")
	rule["ruleset_id"] = "cidr"
	rule["rule_type"] = "IP-CIDR"
	rule["value"] = "192.0.2.9"
	w.call("POST", apply, rule, 200, "admin")
	rule["value"] = "203.0.113.7"
	w.call("POST", apply, rule, 200, "admin")
	rule["ruleset_id"] = "ip"
	rule["target"] = "proxy"
	w.call("POST", apply, rule, 400, "admin")
	rule["ruleset_id"] = "domains"
	rule["target"] = "direct"
	w.call("POST", apply, rule, 400, "admin")
	rule["target"] = "invalid"
	w.call("POST", apply, rule, 400, "admin")
	for _, body := range []any{nil, []any{}, map[string]any{}, map[string]any{"enabled": nil}, map[string]any{"enabled": 1}, map[string]any{"enabled": "false"}} {
		w.call("PUT", "/api/agents/m/domain-discovery", body, 400, "admin")
	}
	for _, id := range []string{"m", "d", "missing"} {
		enabled := id != "d"
		status := 200
		if id == "missing" {
			status = 404
		}
		w.call("PUT", "/api/agents/"+id+"/domain-discovery", map[string]any{"enabled": enabled, "extra": true}, status, "admin")
	}
	w.call("PUT", "/api/agents/d/domain-discovery", map[string]any{"enabled": true}, 400, "admin")
	w.call("POST", "/api/agents/m/domain-discovery/clear", nil, 200, "admin")
	w.call("POST", "/api/agents/m/domain-discovery/clear", nil, 200, "admin")
	w.call("POST", "/api/agents/missing/domain-discovery/clear", nil, 404, "admin")
	policy := "/api/agents/d/oxidns/device-policies?profile=default"
	state := w.call("GET", policy, nil, 200, "admin")
	w.call("GET", "/api/agents/default/oxidns/device-policies", nil, 200, "admin")
	w.call("GET", "/api/agents/m/oxidns/device-policies", nil, 400, "admin")
	w.call("PUT", "/api/agents/missing/oxidns/device-policies", map[string]any{}, 404, "admin")
	for _, bad := range []any{[]any{}, map[string]any{"extra": true}, map[string]any{"base_url": nil}} {
		w.call("PUT", policy, bad, 400, "admin")
	}
	w.call("PUT", policy, map[string]any{}, 409, "admin")
	edit := map[string]any{"profile_id": "office", "revision": state["revision"], "notes_revision": state["notes_revision"], "policies": []any{map[string]any{"ip": "192.0.2.5", "name": "客厅电视", "mode": "direct"}}}
	for _, field := range []string{"revision", "notes_revision"} {
		clone := map[string]any{}
		for k, v := range edit {
			clone[k] = v
		}
		delete(clone, field)
		w.call("PUT", policy, clone, 409, "admin")
	}
	invalid := map[string]any{}
	for k, v := range edit {
		invalid[k] = v
	}
	invalid["policies"] = nil
	w.call("PUT", policy, invalid, 400, "admin")
	state = w.call("PUT", policy, edit, 200, "admin")
	w.call("PUT", policy, edit, 409, "admin")
	edit["revision"] = state["revision"]
	edit["notes_revision"] = state["notes_revision"]
	// An unrelated note is absent from the open policy editor's names revision.
	independent := map[string]any{"profile_id": "office", "ip": "192.0.2.9", "name": "手机", "previous_name": ""}
	w.call("PUT", notes, independent, 200, "admin")
	state = w.call("PUT", policy, edit, 200, "admin")
	// Editing an existing policy's name invalidates that editor without touching settings.
	rename := map[string]any{"profile_id": "office", "ip": "192.0.2.5", "name": "群晖", "previous_name": "客厅电视"}
	w.call("PUT", notes, rename, 200, "admin")
	w.call("PUT", policy, edit, 409, "admin")
	state = w.call("GET", policy, nil, 200, "admin")
	edit["notes_revision"] = state["notes_revision"]
	edit["policies"] = []any{map[string]any{"ip": "192.0.2.5", "name": "群晖", "mode": "smart"}}
	state = w.call("PUT", policy, edit, 200, "admin")
	edit["revision"] = state["revision"]
	edit["notes_revision"] = state["notes_revision"]
	// An empty name on a new row inherits the latest independently edited note.
	independent["name"] = "小米手机"
	independent["previous_name"] = "手机"
	w.call("PUT", notes, independent, 200, "admin")
	edit["policies"] = []any{map[string]any{"ip": "192.0.2.5", "name": "群晖", "mode": "smart"}, map[string]any{"ip": "192.0.2.9", "name": "", "mode": "smart"}}
	state = w.call("PUT", policy, edit, 200, "admin")
	edit["revision"] = state["revision"]
	edit["notes_revision"] = state["notes_revision"]
	edit["policies"] = []any{map[string]any{"ip": "192.0.2.7/24", "name": "NAS", "mode": "direct"}, map[string]any{"ip": "192.0.2.0/24", "name": "duplicate", "mode": "direct"}}
	w.call("PUT", policy, edit, 400, "admin")
	edit["policies"] = []any{}
	state = w.call("PUT", policy, edit, 200, "admin")
	w.call("HEAD", policy, nil, 200, "admin")
	_ = state
}

func TestClientNoteAndPolicyCancelledOwners(t *testing.T) {
	s := a24Fixture(t)
	ctx, cancel := context.WithCancel(s.Context())
	cancel()
	if err := (clientTools{}).SaveNote(ctx, "m", noteEdit{"office", "192.0.2.5", "renamed", "旧电脑"}); err != nil {
		t.Fatal(err)
	}
	p, err := clientConfig(s.Context(), "office", true)
	if err != nil {
		t.Fatal(err)
	}
	profile := nativeClientDNSProfile(t, p)
	revision := oxidns.Revision(profile.Settings())
	notes, err := oxidns.PolicyNamesRevision(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := (clientTools{}).SavePolicies(ctx, "d", policyEdit{ProfileID: "office", Revision: revision, NotesRevision: notes, Policies: json.RawMessage(`[{"ip":"192.0.2.5","mode":"direct","name":"second"}]`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := (clientTools{}).ApplyConnection(ctx, "m", connectionEdit{"office", "domains", "direct", json.RawMessage(`"cancelled.example"`), "DOMAIN"}); err != nil {
		t.Fatal(err)
	}
	if _, err := (clientTools{}).ApplyConnection(ctx, "m", connectionEdit{"office", "ip", "direct", json.RawMessage(`"198.51.100.7"`), "IP-CIDR"}); err != nil {
		t.Fatal(err)
	}
	if _, err := (clientTools{}).SetDiscovery(ctx, "m", true); err != nil {
		t.Fatal(err)
	}
	if found, err := (clientTools{}).ClearDiscovery(ctx, "m"); err != nil || !found {
		t.Fatalf("clear: %v %v", found, err)
	}
}

// FuzzClientConnectionEntries drives the shared parser used by the A24 domain
// action. Accepted entries must remain stable across the JSON API boundary.
func FuzzClientConnectionEntries(f *testing.F) {
	for _, seed := range []string{`[]`, `null`, `[{"target":"direct","value":"Example.COM.","rule_type":"DOMAIN"}]`, `[{"target":"proxy","value":"192.0.2.1","rule_type":"DOMAIN"}]`, `[{"target":"direct","value":"a\nMATCH,DIRECT","rule_type":"DOMAIN"}]`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		entries, err := discovery.ParseApplyItems(json.RawMessage(text))
		if err != nil {
			return
		}
		raw, err := json.Marshal(entries)
		if err != nil {
			t.Fatal(err)
		}
		again, err := discovery.ParseApplyItems(raw)
		if err != nil || !reflect.DeepEqual(entries, again) {
			t.Fatalf("unstable accepted entries: %v", err)
		}
	})
}

func TestClientToolsRejectStaleBindingsAndNames(t *testing.T) {
	for _, kind := range []string{"note", "policy", "domain-default"} {
		t.Run(kind, func(t *testing.T) {
			s := a24Fixture(t)
			ctx := s.Context()
			config, err := clientConfig(ctx, "office", false)
			if err != nil {
				t.Fatal(err)
			}
			profile := nativeClientDNSProfile(t, config)
			notes, err := oxidns.PolicyNamesRevision(profile)
			if err != nil {
				t.Fatal(err)
			}
			manager, err := registry.Get(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "domain-default" {
				_, _, err = s.Repo.UpdateProfileTransaction(ctx, "office", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
					p.GetObject("domain_discovery").Set("direct_ruleset", nil)
					return nil, nil
				})
			} else {
				id := "m"
				if kind == "policy" {
					id = "d"
				}
				_, err = manager.EditAgent(ctx, id, agentEditsForTest(t, jsonx.Obj("profile_id", "default")))
			}
			if err != nil {
				t.Fatal(err)
			}
			before := a24Normalized(a24State(t, s))
			switch kind {
			case "note":
				err = (clientTools{}).SaveNote(ctx, "m", noteEdit{"office", "192.0.2.5", "new", "旧电脑"})
			case "policy":
				err = (clientTools{}).SavePolicies(ctx, "d", policyEdit{ProfileID: "office", Revision: oxidns.Revision(profile.Settings()), NotesRevision: notes, Policies: json.RawMessage(`[]`)})
			case "domain-default":
				_, err = (clientTools{}).ApplyConnection(ctx, "m", connectionEdit{"office", "domains", "direct", json.RawMessage(`"new.example"`), "DOMAIN"})
			}
			if !errors.Is(err, repo.ErrInUse) {
				t.Fatalf("stale guard: %v", err)
			}
			if !reflect.DeepEqual(before, a24Normalized(a24State(t, s))) {
				t.Fatal("stale write changed state")
			}
		})
	}
}

func TestClientDiscoveryReturnsRuntimeOverlay(t *testing.T) {
	s := a24Fixture(t)
	ctx := s.Context()
	manager, err := registry.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.ObserveHeartbeat(ctx, "m", agentModelForTest(t, jsonx.Obj("version", "2.0.0-live", "service_status", "running", "system_metrics", jsonx.Obj("cpu", jsonx.Obj("usage_percent", 12))))); err != nil {
		t.Fatal(err)
	}
	record, err := (clientTools{}).SetDiscovery(ctx, "m", true)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := jsonx.Parse(encoded)
	if err != nil {
		t.Fatal(err)
	}
	legacy := parsed.(*jsonx.Object)
	if legacy.GetString("version") != "2.0.0-live" || legacy.GetString("service_status") != "running" || legacy.GetObject("system_metrics").GetObject("cpu").GetInt("usage_percent") != 12 {
		t.Fatal("lost runtime overlay")
	}
	public := publicAgent(legacy)
	if public.Has("token") {
		t.Fatal("credential escaped")
	}
}

func TestClientToolsConcurrentEditors(t *testing.T) {
	for _, kind := range []string{"notes", "policies"} {
		t.Run(kind, func(t *testing.T) {
			s := a24Fixture(t)
			ctx := s.Context()
			p, err := clientConfig(ctx, "office", true)
			if err != nil {
				t.Fatal(err)
			}
			profile := nativeClientDNSProfile(t, p)
			revision := oxidns.Revision(profile.Settings())
			notes, err := oxidns.PolicyNamesRevision(profile)
			if err != nil {
				t.Fatal(err)
			}
			results := make(chan error, 2)
			start := make(chan struct{})
			var workers sync.WaitGroup
			for _, name := range []string{"first", "second"} {
				workers.Go(func() {
					<-start
					var err error
					if kind == "notes" {
						err = (clientTools{}).SaveNote(ctx, "m", noteEdit{"office", "192.0.2.5", name, "旧电脑"})
					} else {
						rows, _ := json.Marshal([]map[string]string{{"ip": "192.0.2.5", "name": name, "mode": "direct"}})
						err = (clientTools{}).SavePolicies(ctx, "d", policyEdit{ProfileID: "office", Revision: revision, NotesRevision: notes, Policies: rows})
					}
					results <- err
				})
			}
			close(start)
			workers.Wait()
			close(results)
			successes, conflicts := 0, 0
			for err := range results {
				if err == nil {
					successes++
				} else if errors.Is(err, repo.ErrInUse) {
					conflicts++
				} else {
					t.Fatal(err)
				}
			}
			if successes != 1 || conflicts != 1 {
				t.Fatalf("success=%d conflict=%d", successes, conflicts)
			}
			saved, err := clientConfig(ctx, "office", true)
			if err != nil {
				t.Fatal(err)
			}
			if oxidns.Revision(nativeClientDNSProfile(t, saved).Settings()) != revision {
				t.Fatal("name-only concurrent save changed settings")
			}
		})
	}
}

func nativeClientDNSProfile(t *testing.T, config *model.Config) *oxidns.Profile {
	t.Helper()
	p, err := oxidns.NewTypedProfile(config)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func agentModelForTest(t testing.TB, value any) *model.Agent {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var a model.Agent
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	return &a
}
func agentEditsForTest(t testing.TB, value any) registry.AgentEdits {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	edits, err := registry.DecodeAgentEdits(raw)
	if err != nil {
		t.Fatal(err)
	}
	return edits
}
