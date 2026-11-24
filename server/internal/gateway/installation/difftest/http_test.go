// Package difftest compares installation HTTP status, JSON and durable changes
// with real Flask on an independent copy of the same synthetic DATA_DIR.
package difftest

import (
	"bufio"
	"bytes"
	"configflow/internal/repo/repotest"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/agents/agentfake"
	apiagents "configflow/internal/api/agents"
	"configflow/internal/convert"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/deploy"
	"configflow/internal/gateway"
	"configflow/internal/gateway/host"
	"configflow/internal/gateway/installation"
	"configflow/internal/gateway/onboarding"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/installer"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

const base = "/api/agents/gateway-installations"
const origin = "https://panel.example.com"

func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func obj(b []byte) map[string]json.RawMessage {
	var v map[string]json.RawMessage
	_ = json.Unmarshal(b, &v)
	return v
}
func str(b []byte) string { var v string; _ = json.Unmarshal(b, &v); return v }

type pyAnswer struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body"`
	State   struct {
		Document json.RawMessage            `json:"document"`
		Files    map[string]json.RawMessage `json:"files"`
	} `json:"state"`
	Error string `json:"oracle_error"`
}
type oracle struct {
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	stderr  bytes.Buffer
	cmd     *exec.Cmd
}

func newOracle(t *testing.T, s *httpxtest.Server, destinations map[string]string) *oracle {
	t.Helper()
	dir := t.TempDir()
	out, e := exec.Command("cp", "-a", s.DataDir+"/.", dir).CombinedOutput()
	if e != nil {
		t.Fatal(e, string(out))
	}
	root, e := filepath.Abs("../../../../..")
	must(t, e)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	o := &oracle{}
	o.cmd = exec.CommandContext(ctx, filepath.Join(os.Getenv("HOME"), "cf-test/.venv/bin/python"), filepath.Join(root, "server/internal/gateway/installation/testdata/flask_reference.py"))
	o.cmd.Dir = root
	repotest.PythonLayout(t, dir) // Python reads the version-1 layout (O17)
	o.cmd.Env = append(os.Environ(), "PYTHONPATH="+root, "DATA_DIR="+dir, "ADMIN_USERNAME="+httpxtest.Username, "ADMIN_PASSWORD="+httpxtest.Password, "JWT_SECRET_KEY=synthetic-a22-oracle-key", "LOG_FILE="+filepath.Join(dir, "app.log"), "PYTHONDONTWRITEBYTECODE=1", "A22_LOOPBACK_DESTINATIONS="+string(raw(destinations)))
	o.cmd.Stderr = &o.stderr
	o.stdin, e = o.cmd.StdinPipe()
	must(t, e)
	stdout, e := o.cmd.StdoutPipe()
	must(t, e)
	o.scanner = bufio.NewScanner(stdout)
	o.scanner.Buffer(make([]byte, 65536), 16<<20)
	must(t, o.cmd.Start())
	t.Cleanup(func() {
		o.stdin.Close()
		o.cmd.Process.Kill()
		o.cmd.Wait()
		if t.Failed() {
			t.Log(o.stderr.String())
		}
	})
	return o
}
func (o *oracle) call(t *testing.T, method, path, body, token string) pyAnswer {
	t.Helper()
	_, e := o.stdin.Write(append(raw(map[string]any{"method": method, "path": path, "body": body, "headers": map[string]string{"Authorization": "Bearer " + token}}), '\n'))
	must(t, e)
	if !o.scanner.Scan() {
		t.Fatal("Flask ended", o.stderr.String())
	}
	var answer pyAnswer
	must(t, json.Unmarshal(o.scanner.Bytes(), &answer))
	if answer.Error != "" {
		t.Fatal("Flask oracle", answer.Error)
	}
	return answer
}
func setup(t *testing.T) (*httpxtest.Server, *agents.Manager, string, installation.Record, map[string]json.RawMessage) {
	t.Helper()
	_ = convert.SetMihomoIntegrations(gateway.Hooks())
	mihomoenv.OverrideForTest(t, nil, nil, nil)
	s := httpxtest.New(t, httpxtest.Options{Auth: true})
	static := t.TempDir()
	t.Setenv("AGENTS_STATIC_DIR", static)
	must(t, os.WriteFile(filepath.Join(static, "configflow-agent-linux-amd64"), []byte("synthetic-agent-binary"), 0600))
	ctx := s.Context()
	e := s.Repo.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error {
		return json.Unmarshal([]byte(`[{"id":"n","name":"Example","type":"ss","server":"192.0.2.2","port":443,"cipher":"aes-128-gcm","password":"synthetic-node-secret"}]`), &shared.Nodes)
	})
	must(t, e)
	_, e = s.Repo.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		if err := json.Unmarshal([]byte(`[{"id":"g","name":"Proxy","type":"select","manual_nodes":["n"]}]`), &p.ProxyGroups); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(`[{"id":"match","itemType":"rule","rule_type":"MATCH","policy":"Proxy"}]`), &p.RuleConfigs); err != nil {
			return err
		}
		p.Mihomo.CustomConfig = "mixed-port: 7890\n"
		return nil
	})
	must(t, e)
	on := onboarding.Service{Repository: s.Repo}
	_, e = on.Execute(ctx, onboarding.Operation{ProfileID: "default", BaseURL: origin, Write: true, Body: raw(map[string]any{"rule_set_ids": []string{}, "direct_rule_set_ids": []string{}, "proxy_rule_set_ids": []string{}, "local_dns": "https://192.0.2.53/dns-query"})})
	must(t, e)
	m, e := agents.New(ctx, s.Repo, agents.Options{ReceiptMatches: deploy.ReceiptMatcher(s.Repo)})
	must(t, e)
	t.Cleanup(m.Close)
	apiagents.NewHandler(apiagents.Deps{Repo: s.Repo, Agents: m}).Register(s.App.Router)
	service := installation.Service{Repository: s.Repo, Manager: m}
	revision, e := s.Repo.ConfigRevision(ctx, "default")
	must(t, e)
	body := map[string]json.RawMessage{"profile_id": raw("default"), "name": raw("synthetic-linux"), "server_url": raw(origin), "network": raw(host.Spec{Interface: "eth0", Address: "192.0.2.10", LANCIDR: "192.0.2.0/24", UpstreamGateway: "192.0.2.1"}), "config_revision": raw(revision)}
	out, e := service.Create(ctx, raw(body))
	must(t, e)
	path := filepath.Join(s.DataDir, "gateway-installations", str(obj(out)["id"])+".json")
	data, e := os.ReadFile(path)
	must(t, e)
	var r installation.Record
	must(t, json.Unmarshal(data, &r))
	// Recovery checkpoints use fixed task IDs on both independent sides; the
	// actual HTTP publish route must retain them before starting its worker.
	for _, kind := range []string{"mihomo", "oxidns"} {
		r.Deployments[kind] = raw(map[string]string{"deployment_id": "job-" + kind, "status": "preparing"})
	}
	must(t, os.WriteFile(path, raw(r), 0600))
	e = s.Repo.UpdateSystem(ctx, func(_ context.Context, sys *model.System) error {
		var list []model.Agent
		for _, kind := range []string{"mihomo", "oxidns"} {
			var agent model.Agent
			b := raw(map[string]any{"id": "synthetic-" + kind, "name": r.Name + "-" + kind, "host": r.Network.Address, "port": r.Ports[kind], "token": "synthetic-token-" + kind, "service_type": kind, "profile_id": r.ProfileID, "deployment_method": "shell", "version": "1.8.60-go", "enabled": true, "gateway_installation_id": r.ID, "created_at": "2026-01-01T00:00:00+00:00"})
			if err := json.Unmarshal(b, &agent); err != nil {
				return err
			}
			list = append(list, agent)
		}
		sys.Agents = list
		return nil
	})
	must(t, e)
	login := s.Post("/api/auth/login", httpxtest.JSON(map[string]string{"username": httpxtest.Username, "password": httpxtest.Password})).WantStatus(200)
	token := str(obj(login.Body)["token"])
	return s, m, token, r, body
}
func normalize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, b := range x {
			if m, empty := b.(map[string]any); b == nil || empty && len(m) == 0 && k == "deployments" {
				// A stored null or empty history is a missing member since O18 T1b.
				delete(x, k)
				continue
			}
			if k == "updated_at" || k == "created_at" || k == "last_heartbeat" {
				x[k] = "@time"
			} else {
				x[k] = normalize(b)
			}
		}
	case []any:
		for i, b := range x {
			x[i] = normalize(b)
		}
	}
	return v
}
func equal(t *testing.T, label string, a, b json.RawMessage) {
	t.Helper()
	var x, y any
	must(t, json.Unmarshal(a, &x))
	must(t, json.Unmarshal(b, &y))
	x = normalize(x)
	y = normalize(y)
	if !reflect.DeepEqual(x, y) {
		t.Fatalf("%s mismatch at %s", label, firstDifference(x, y, "$"))
	}
}
func TestGatewayFlaskHTTPAndDurableState(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("CONFIGFLOW_DIFF=1")
	}
	s, manager, admin, r, body := setup(t)
	store := installer.Store{DataDir: s.DataDir}
	for range installer.MaxSnapshots - 1 {
		_, _, e := store.SaveInstallScript(s.Context(), "#!/bin/sh\n# synthetic frozen installer\n", nil)
		must(t, e)
	}
	goDest, pyDest := map[string]string{}, map[string]string{}
	for _, kind := range []string{"mihomo", "oxidns"} {
		for _, dest := range []map[string]string{goDest, pyDest} {
			fake := agentfake.New(agentfake.Options{AgentID: "synthetic-" + kind, Token: "synthetic-token-" + kind, ServiceType: kind})
			handler := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/api/linux-gateway/status" {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(raw(map[string]any{"success": true, "supported": true, "managed": map[string]bool{"active": false}, "detection": map[string]any{"suggested": r.Network, "interfaces": []any{map[string]any{"name": r.Network.Interface, "addresses": []string{r.Network.Address + "/24"}}}, "default_routes": []any{map[string]any{"interface": r.Network.Interface, "gateway": r.Network.UpstreamGateway}}}}))
					return
				}
				fake.ServeHTTP(w, request)
			})
			receiver := httptest.NewServer(handler)
			defer receiver.Close()
			dest[fmt.Sprintf("%s:%d", r.Network.Address, r.Ports[kind])] = receiver.URL
		}
	}
	manager.SetClient(diffClient{goDest})
	py := newOracle(t, s, pyDest)
	pyLogin := py.call(t, "POST", "/api/auth/login", string(raw(map[string]string{"username": httpxtest.Username, "password": httpxtest.Password})), "")
	pyAdmin := str(obj(pyLogin.Body)["token"])
	published := false
	check := func(method, path, body, goToken, pyToken string, status int) {
		t.Helper()
		expected := py.call(t, method, path, body, pyToken)
		actual := s.Do(method, path, httpxtest.Body([]byte(body), "text/plain"), httpxtest.Bearer(goToken))
		if actual.Status != status || actual.Status != expected.Status {
			t.Fatalf("%s %s status Go=%d Python=%d expected=%d\n%s\n%s", method, path, actual.Status, expected.Status, status, actual.Body, expected.Body)
		}
		if method != "HEAD" {
			equal(t, "HTTP "+path, actual.Body, expected.Body)
		}
		for _, header := range []string{"Cache-Control", "Referrer-Policy"} {
			if actual.Header.Get(header) != expected.Headers[header] {
				t.Fatal("header mismatch", header)
			}
		}
		doc, e := s.Repo.DiagnosticExportJSON(s.Context())
		must(t, e)
		if !published {
			equal(t, "document", doc, expected.State.Document)
		} else {
			equal(t, "stored publication evidence", publicationEvidence(t, doc), publicationEvidence(t, expected.State.Document))
		}
		pathFile := "gateway-installations/" + r.ID + ".json"
		b, e := os.ReadFile(filepath.Join(s.DataDir, pathFile))
		must(t, e)
		equal(t, "durable session", b, expected.State.Files[pathFile])
	}
	path := base + "/" + r.ID
	check("GET", base+"?profile_id=default", "", admin, pyAdmin, 200)
	check("GET", path, "", admin, pyAdmin, 200)
	check("HEAD", path, "", admin, pyAdmin, 200)
	check("POST", base, string(raw(body)), admin, pyAdmin, 200) // exact lost-response replay
	full := obj(raw(body))
	full["name"] = raw("different-linux")
	check("POST", base, string(raw(full)), admin, pyAdmin, 503)
	check("POST", base, "{", admin, pyAdmin, 400)
	check("POST", base, "[]", admin, pyAdmin, 400)
	invalid := obj(raw(body))
	invalid["auto_network"] = raw("true")
	check("POST", base, string(raw(invalid)), admin, pyAdmin, 400)
	invalid = obj(raw(body))
	delete(invalid, "config_revision")
	check("POST", base, string(raw(invalid)), admin, pyAdmin, 400)
	check("POST", base, strings.Repeat(" ", 16385), admin, pyAdmin, 413)
	check("GET", path, "", "", "", 401)
	check("POST", path+"/enroll", `{}`, r.Token, r.Token, 400)
	check("POST", path+"/enroll", `{"agents":{"mihomo":{"id":"synthetic-mihomo","token":"wrong"},"oxidns":{"id":"synthetic-oxidns","token":"wrong"}}}`, r.Token, r.Token, 401)
	check("GET", path+"/script?token=wrong", "", admin, pyAdmin, 401)
	script := py.call(t, "GET", path+"/script?token="+r.Token, "", "")
	got := s.Get(path + "/script?token=" + r.Token)
	got.WantStatus(200)
	if string(got.Body) != str(script.Body) || got.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("frozen script mismatch")
	}
	check("POST", path+"/network", `{"auto_network":true}`, admin, pyAdmin, 409)
	check("POST", path+"/publish", `{}`, admin, pyAdmin, 409)
	for _, kind := range []string{"mihomo", "oxidns"} {
		registration := raw(map[string]any{"name": r.Name + "-" + kind, "host": r.Network.Address, "port": r.Ports[kind], "service_type": kind, "deployment_method": "shell", "version": "1.8.60-go", "installation_id": r.ID, "installation_token": r.Token})
		check("POST", "/api/agents/register", string(registration), "", "", 200)
	}
	proofs := map[string]any{}
	for _, kind := range []string{"mihomo", "oxidns"} {
		proofs[kind] = map[string]string{"id": "synthetic-" + kind, "token": "synthetic-token-" + kind}
	}
	check("POST", path+"/enroll", string(raw(map[string]any{"agents": proofs})), r.Token, r.Token, 200)
	check("GET", base+"?profile_id=default", "", admin, pyAdmin, 200)
	check("POST", path+"/publish", `{"retry_failed":1}`, admin, pyAdmin, 400)
	check("GET", base+"/bad", "", admin, pyAdmin, 404)
	check("POST", path+"/network", `{"auto_network":true}`, admin, pyAdmin, 200)
	expected := py.call(t, "POST", path+"/publish", `{}`, pyAdmin)
	actual := s.Post(path+"/publish", httpxtest.JSON(map[string]any{}), httpxtest.Bearer(admin)).WantStatus(202)
	if expected.Status != 202 {
		t.Fatal("Flask did not start publication", string(expected.Body))
	}
	equal(t, "publish accepted", actual.Body, expected.Body)
	// Poll the real progress endpoint to completion before stopping the Runtime.
	// Shutdown is a cancellation operation, not an unbounded join under race.
	waitPublished(t, s, path, admin)
	lifecycle.Stop(10 * time.Second)
	published = true
	settled := py.call(t, "WAIT", "", "", "")
	checkpoint, e := os.ReadFile(filepath.Join(s.DataDir, "gateway-installations", r.ID+".json"))
	must(t, e)
	equal(t, "settled publication session", checkpoint, settled.State.Files["gateway-installations/"+r.ID+".json"])
	if str(obj(checkpoint)["phase"]) != "published" {
		t.Fatal("HTTP publication did not finish", string(checkpoint))
	}
	// Publication artifact and receipt differences belong to A14's corpus; here
	// compare the exact session and public status after both workers are idle.
	expected = py.call(t, "GET", path, "", pyAdmin)
	actual = s.Get(path, httpxtest.Bearer(admin)).WantStatus(200)
	equal(t, "published HTTP status", actual.Body, expected.Body)
	// A real unrelated profile change makes the existing session stale before
	// any attempted Agent operation. Both reject and retain the checkpoint.
	py.call(t, "EDIT", "default", `{"description":"changed"}`, "")
	_, e = s.Repo.UpdateProfile(s.Context(), "default", func(_ context.Context, p *model.Profile) error {
		p.Description = "changed"
		return nil
	})
	must(t, e)
	check("POST", path+"/publish", `{}`, admin, pyAdmin, 409)
}

type diffClient struct{ destinations map[string]string }

func (c diffClient) Do(ctx context.Context, r agents.Request) (*agents.Response, error) {
	u, e := url.Parse(r.URL)
	if e != nil {
		return nil, e
	}
	destination, ok := c.destinations[u.Host]
	if !ok {
		return nil, fmt.Errorf("unregistered origin forbidden")
	}
	v, _ := url.Parse(destination)
	u.Scheme = v.Scheme
	u.Host = v.Host
	r.URL = u.String()
	return (agents.HTTPClient{}).Do(ctx, r)
}

// A14 verifies its opaque fingerprints, event histories and bundle receipts.
// A22 must store exact pair bindings, profile revision and successful IDs; the
// rest of the profile/shared document must remain unchanged by publication.
func publicationEvidence(t *testing.T, document json.RawMessage) json.RawMessage {
	t.Helper()
	root := obj(document)
	system := obj(root["system"])
	var records []json.RawMessage
	must(t, json.Unmarshal(system["agents"], &records))
	agents := []any{}
	for _, record := range records {
		a := obj(record)
		view := map[string]json.RawMessage{}
		for _, key := range []string{"id", "name", "host", "port", "token", "profile_id", "service_type", "deployment_method", "enabled", "version", "config_revision"} {
			view[key] = a[key]
		}
		latest := obj(a["latest_deployment"])
		applied := obj(a["applied_content"])
		if str(latest["status"]) != "succeeded" || str(latest["deployment_id"]) == "" || str(latest["deployment_id"]) != str(applied["deployment_id"]) {
			t.Fatal("publication evidence incomplete")
		}
		view["latest"] = raw(map[string]json.RawMessage{"deployment_id": latest["deployment_id"], "status": latest["status"]})
		agents = append(agents, view)
	}
	return raw(map[string]any{"profiles": root["profiles"], "shared": root["shared"], "system_config": system["system_config"], "agents": agents})
}

func waitPublished(t *testing.T, s *httpxtest.Server, path, admin string) {
	t.Helper()
	deadline := time.NewTimer(time.Minute)
	defer deadline.Stop()
	poll := time.NewTicker(100 * time.Millisecond)
	defer poll.Stop()
	for {
		reply := s.Get(path, httpxtest.Bearer(admin)).WantStatus(200)
		phase := str(obj(reply.Body)["phase"])
		if phase == "published" {
			return
		}
		if phase == "publish_failed" {
			t.Fatalf("publication failed: %s", str(obj(reply.Body)["message"]))
		}
		select {
		case <-deadline.C:
			t.Fatal("publication did not settle")
		case <-poll.C:
		}
	}
}

func firstDifference(a, b any, path string) string {
	if reflect.DeepEqual(a, b) {
		return ""
	}
	if x, ok := a.(map[string]any); ok {
		if y, ok := b.(map[string]any); ok {
			for k, v := range x {
				if _, ok := y[k]; !ok {
					return path + "." + k + " (missing)"
				}
				if d := firstDifference(v, y[k], path+"."+k); d != "" {
					return d
				}
			}
			for k := range y {
				if _, ok := x[k]; !ok {
					return path + "." + k + " (additional key)"
				}
			}
		}
	}
	if x, ok := a.([]any); ok {
		if y, ok := b.([]any); ok && len(x) == len(y) {
			for i := range x {
				if d := firstDifference(x[i], y[i], fmt.Sprintf("%s[%d]", path, i)); d != "" {
					return d
				}
			}
		}
	}
	return fmt.Sprintf("%s (%v vs %v)", path, a, b)
}
