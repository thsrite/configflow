package installation

import (
	"bytes"
	agentcore "configflow/internal/agents"
	"configflow/internal/jsonresp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/agents/agentfake"
	"configflow/internal/convert"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/deploy"
	"configflow/internal/gateway"
	"configflow/internal/gateway/host"
	"configflow/internal/gateway/onboarding"
	"configflow/internal/installer"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/model"
	"configflow/internal/repo"
)

const testOrigin = "https://panel.example.com"

var testNetwork = host.Spec{Interface: "eth0", Address: "192.0.2.10", LANCIDR: "192.0.2.0/24", UpstreamGateway: "192.0.2.1"}

type fixture struct {
	s    *Service
	ctx  context.Context
	body map[string]json.RawMessage
	r    *Record
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	_ = convert.SetMihomoIntegrations(gateway.Hooks())
	mihomoenv.OverrideForTest(t, nil, nil, nil)
	r, err := repo.Open(t.TempDir(), repo.Options{})
	must(t, err)
	ctx := repo.WithRepository(repo.WithScope(context.Background()), r)
	err = r.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error {
		return json.Unmarshal([]byte(`[{"id":"n","name":"Synthetic","type":"ss","server":"192.0.2.2","port":443,"cipher":"aes-128-gcm","password":"synthetic-node-secret"}]`), &shared.Nodes)
	})
	must(t, err)
	_, err = r.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		if err := json.Unmarshal([]byte(`[{"id":"g","name":"Proxy","type":"select","manual_nodes":["n"]}]`), &p.ProxyGroups); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(`[{"id":"match","itemType":"rule","rule_type":"MATCH","policy":"Proxy"}]`), &p.RuleConfigs); err != nil {
			return err
		}
		p.Mihomo.CustomConfig = "mixed-port: 7890\n"
		return nil
	})
	must(t, err)
	on := onboarding.Service{Repository: r}
	_, err = on.Execute(ctx, onboarding.Operation{ProfileID: "default", BaseURL: testOrigin, Write: true, Body: jsonresp.Value(map[string]any{"rule_set_ids": []string{}, "direct_rule_set_ids": []string{}, "proxy_rule_set_ids": []string{}, "local_dns": "https://192.0.2.53/dns-query"})})
	must(t, err)
	m, err := agents.New(ctx, r, agents.Options{ReceiptMatches: deploy.ReceiptMatches})
	must(t, err)
	t.Cleanup(m.Close)
	s := &Service{Repository: r, Manager: m, Render: func(o installer.GatewayInstallOptions) (string, error) {
		o.AgentChecksums = map[string]string{"linux-amd64": strings.Repeat("a", 64)}
		return installer.RenderGatewayInstall(o)
	}}
	revision, err := r.ConfigRevision(ctx, "default")
	must(t, err)
	body := map[string]json.RawMessage{"profile_id": jsonresp.Value("default"), "name": jsonresp.Value("synthetic-linux"), "server_url": jsonresp.Value(testOrigin), "network": jsonresp.Value(testNetwork), "config_revision": jsonresp.Value(revision)}
	out, err := s.Create(ctx, jsonresp.Value(body))
	must(t, err)
	id := text(object(out)["id"])
	record, err := s.read(ctx, id)
	must(t, err)
	return &fixture{s, ctx, body, record}
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func refusal(t *testing.T, err error, status int, message string) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Status != status || message != "" && e.Message != message {
		t.Fatalf("refusal %T %v want %d %s", err, err, status, message)
	}
}
func (f *fixture) register(t *testing.T, service string) json.RawMessage {
	t.Helper()
	data := jsonresp.Value(map[string]any{"name": f.r.Name + "-" + service, "host": f.r.Network.Address, "port": f.r.Ports[service], "service_type": service, "deployment_method": "shell", "version": "1.8.60-go"})
	_, data, err := f.s.RegistrationProfile(f.ctx, f.r.ID, f.r.Token, data, "")
	must(t, err)
	out, err := f.s.Register(f.ctx, f.r.ID, f.r.Token, data, "")
	must(t, err)
	return out
}
func (f *fixture) enroll(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	proofs := map[string]json.RawMessage{}
	for _, service := range services {
		proofs[service] = f.register(t, service)
		o := object(proofs[service])
		proofs[service] = jsonresp.Value(map[string]json.RawMessage{"id": o["id"], "token": o["token"]})
	}
	_, err := f.s.Enroll(f.ctx, f.r.ID, f.r.Token, jsonresp.Value(map[string]any{"agents": proofs}))
	must(t, err)
	f.r, err = f.s.read(f.ctx, f.r.ID)
	must(t, err)
	return proofs
}
func recordBytes(t *testing.T, f *fixture) []byte {
	t.Helper()
	p, _ := f.s.path(f.r.ID)
	b, e := os.ReadFile(p)
	must(t, e)
	return b
}
func document(t *testing.T, f *fixture) []byte {
	t.Helper()
	b, e := f.s.Repository.ExportAllJSON(f.ctx, false)
	must(t, e)
	return b
}
func TestCreationPrivacyReuseAndValidation(t *testing.T) {
	f := newFixture(t)
	before := document(t, f)
	first, err := f.s.Get(f.ctx, f.r.ID)
	must(t, err)
	if bytes.Contains(first, []byte(`"token":`)) || bytes.Contains(first, []byte(`"script":`)) || bytes.Contains(first, []byte("synthetic-node-secret")) {
		t.Fatal("private session leaked")
	}
	for _, dir := range []string{f.s.root(), filepath.Join(f.s.Repository.DataDir(), "install-commands")} {
		st, e := os.Stat(dir)
		must(t, e)
		if st.Mode().Perm() != 0700 {
			t.Fatal("directory mode", st.Mode())
		}
	}
	p, _ := f.s.path(f.r.ID)
	st, err := os.Stat(p)
	must(t, err)
	if st.Mode().Perm() != 0600 {
		t.Fatal("session mode")
	}
	again, err := f.s.Create(f.ctx, jsonresp.Value(f.body))
	must(t, err)
	if text(object(again)["id"]) != f.r.ID || text(object(again)["command"]) != text(object(first)["command"]) {
		t.Fatal("lost response created a second capability")
	}
	script, err := f.s.Script(f.ctx, f.r.ID, f.r.Token)
	must(t, err)
	if !strings.Contains(script, f.r.Token) || !strings.Contains(script, f.r.ID) || strings.Contains(script, "sysctl -w") {
		t.Fatal("wrong scoped selective script")
	}
	u, _ := url.Parse(text(object(first)["download_url"]))
	store := installer.Store{DataDir: f.s.Repository.DataDir()}
	saved, found, err := store.LoadInstallScript(f.ctx, filepath.Base(u.Path))
	must(t, err)
	if !found || saved != script {
		t.Fatal("short link did not reuse installer owner")
	}
	for _, tc := range []struct {
		name, key string
		value     any
		message   string
	}{
		{"unknown", "unexpected", true, msgParams}, {"revision", "config_revision", 1.5, msgRevision}, {"name", "name", "bad;name", msgName}, {"auto-type", "auto_network", 1, msgAuto}, {"auto-network", "auto_network", true, msgAutoFields}, {"port", "mihomo_agent_port", 1053, msgPorts}, {"same-port", "mihomo_agent_port", 8082, msgPorts}, {"base-port", "mihomo_agent_port", 7890, ""}, {"server", "server_url", "http://localhost", msgServerLocal}, {"server-credentials", "server_url", "https://user:secret@panel.example", msgServerShape}, {"domain-type", "use_as_server_domain", nil, msgUseDomain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := object(jsonresp.Value(f.body))
			o[tc.key] = jsonresp.Value(tc.value)
			snapshot := recordBytes(t, f)
			_, e := f.s.Create(f.ctx, jsonresp.Value(o))
			refusal(t, e, 400, tc.message)
			if !bytes.Equal(before, document(t, f)) || !bytes.Equal(snapshot, recordBytes(t, f)) {
				t.Fatal("invalid creation changed state")
			}
		})
	}
	// A raw unknown member, including opaque credentials, survives an update.
	r := object(recordBytes(t, f))
	r["future"] = json.RawMessage(`{"private_token":"synthetic-future","unknown":false}`)
	must(t, os.WriteFile(p, jsonresp.Value(r), 0600))
	_, err = f.s.Create(f.ctx, jsonresp.Value(f.body))
	must(t, err)
	if !bytes.Equal(object(recordBytes(t, f))["future"], r["future"]) {
		t.Fatal("unknown session field lost")
	}
	count := f.s.Repository.CommitCount()
	_, err = f.s.Get(f.ctx, "bad")
	refusal(t, err, 404, msgID)
	if f.s.Repository.CommitCount() != count {
		t.Fatal("read committed")
	}
	if lockx.Violations() != 0 {
		t.Fatal("lock order violations", lockx.Violations())
	}
}
func TestRegistrationCapabilitiesAndAtomicEnrollment(t *testing.T) {
	f := newFixture(t)
	proofs := map[string]json.RawMessage{}
	for _, kind := range services {
		proofs[kind] = f.register(t, kind)
	}
	// Exact marker replays the original credential after a lost session receipt.
	r := f.r
	r.Registered = map[string]string{}
	must(t, f.s.write(f.ctx, r))
	replayed := f.register(t, "mihomo")
	if text(object(replayed)["token"]) != text(object(proofs["mihomo"])["token"]) {
		t.Fatal("owned replay token changed")
	}
	foreign := *r
	foreign.ID = strings.Repeat("b", 32)
	foreign.Token = "synthetic-foreign-capability"
	must(t, f.s.write(f.ctx, &foreign))
	data := jsonresp.Value(map[string]any{"name": r.Name + "-mihomo", "host": r.Network.Address, "port": r.Ports["mihomo"], "service_type": "mihomo", "profile_id": r.ProfileID, "deployment_method": "shell"})
	_, err := f.s.Register(f.ctx, foreign.ID, foreign.Token, data, "")
	if !errors.Is(err, agents.ErrUnauthorized) {
		t.Fatalf("foreign capability recovered another credential: %v", err)
	}
	// The first proof would change metadata; an invalid second proof must keep the
	// entire registry document unchanged, proving the callback is atomic.
	mihomoID := text(object(proofs["mihomo"])["id"])
	_, err = f.s.Manager.EditAgent(f.ctx, mihomoID, agentEditsForTest(t, map[string]any{"name": "prior-name"}))
	must(t, err)
	enrollment := map[string]json.RawMessage{}
	for _, service := range services {
		o := object(proofs[service])
		enrollment[service] = jsonresp.Value(map[string]any{"id": text(o["id"]), "token": text(o["token"])})
	}
	bad := object(enrollment["oxidns"])
	bad["token"] = jsonresp.Value("synthetic-wrong")
	enrollment["oxidns"] = jsonresp.Value(bad)
	before := document(t, f)
	_, err = f.s.Enroll(f.ctx, r.ID, r.Token, jsonresp.Value(map[string]any{"agents": enrollment}))
	refusal(t, err, 401, msgProofFailed)
	if !bytes.Equal(before, document(t, f)) {
		t.Fatal("invalid second proof partially rebound the first")
	}
	bad["token"] = object(proofs["oxidns"])["token"]
	enrollment["oxidns"] = jsonresp.Value(bad)
	_, err = f.s.Enroll(f.ctx, r.ID, r.Token, jsonresp.Value(map[string]any{"agents": enrollment}))
	must(t, err)
	a, err := f.s.agent(f.ctx, mihomoID)
	must(t, err)
	if a.Name != r.Name+"-mihomo" {
		t.Fatal("proven metadata not rebound")
	}
	r, err = f.s.read(f.ctx, r.ID)
	must(t, err)
	if r.Phase != "enrolled" || len(r.AgentIDs) != 2 {
		t.Fatal("pair not durable")
	}
	r.Expires = 1
	must(t, f.s.write(f.ctx, r))
	_, err = f.s.Script(f.ctx, r.ID, r.Token)
	refusal(t, err, 401, msgAuth)
	_, err = f.s.Enroll(f.ctx, r.ID, r.Token, jsonresp.Value(map[string]any{"agents": enrollment}))
	refusal(t, err, 401, msgAuth)
}
func TestAutoHostPinSurvivesMissingReceipt(t *testing.T) {
	f := newFixture(t)
	o := object(jsonresp.Value(f.body))
	delete(o, "network")
	o["auto_network"] = jsonresp.Value(true)
	o["name"] = jsonresp.Value("auto-linux")
	out, err := f.s.Create(f.ctx, jsonresp.Value(o))
	must(t, err)
	f.r, err = f.s.read(f.ctx, text(object(out)["id"]))
	must(t, err)
	data := jsonresp.Value(map[string]any{"name": f.r.Name + "-mihomo", "host": "", "port": 8080, "service_type": "mihomo", "deployment_method": "shell"})
	_, data, err = f.s.RegistrationProfile(f.ctx, f.r.ID, f.r.Token, data, testNetwork.Address)
	must(t, err)
	_, err = f.s.Register(f.ctx, f.r.ID, f.r.Token, data, "")
	must(t, err)
	r, err := f.s.read(f.ctx, f.r.ID)
	must(t, err)
	if r.Network.Address != testNetwork.Address {
		t.Fatal("auto host not pinned")
	}
	r.Network.Address = ""
	r.Registered = map[string]string{}
	must(t, f.s.write(f.ctx, r))
	second := object(data)
	second["name"] = jsonresp.Value(f.r.Name + "-oxidns")
	second["service_type"] = jsonresp.Value("oxidns")
	second["port"] = jsonresp.Value(8082)
	second["host"] = jsonresp.Value("192.0.2.11")
	_, err = f.s.Register(f.ctx, f.r.ID, f.r.Token, jsonresp.Value(second), "")
	refusal(t, err, 409, msgHostPinned)
}

type routeClient struct {
	dest    map[string]string
	inspect func(agents.Request)
	client  agents.HTTPClient
}

func (c *routeClient) Do(ctx context.Context, r agents.Request) (*agents.Response, error) {
	if c.inspect != nil {
		c.inspect(r)
	}
	u, err := url.Parse(r.URL)
	if err != nil {
		return nil, err
	}
	target, ok := c.dest[u.Host]
	if !ok {
		return nil, fmt.Errorf("test blocked unregistered origin")
	}
	v, _ := url.Parse(target)
	u.Scheme = v.Scheme
	u.Host = v.Host
	r.URL = u.String()
	return c.client.Do(ctx, r)
}
func detection(spec host.Spec) map[string]any {
	return map[string]any{"suggested": spec, "interfaces": []any{map[string]any{"name": spec.Interface, "addresses": []string{spec.Address + "/24"}}}, "default_routes": []any{map[string]any{"interface": spec.Interface, "gateway": spec.UpstreamGateway}}}
}
func TestNetworkUsesRealOwnerAndNeverApplies(t *testing.T) {
	f := newFixture(t)
	f.enroll(t)
	before := document(t, f)
	revision := f.r.ConfigRevision
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		b, _ := io.ReadAll(r.Body)
		if r.Method != "GET" || r.URL.Path != "/api/linux-gateway/status" || len(b) != 0 || r.Header.Get("Authorization") == "" {
			t.Error("wrong raw host request")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jsonresp.Value(map[string]any{"success": true, "supported": true, "managed": map[string]bool{"active": false}, "detection": detection(testNetwork)}))
	}))
	defer srv.Close()
	f.s.Manager.SetClient(&routeClient{dest: map[string]string{testNetwork.Address + ":8080": srv.URL}, inspect: func(r agents.Request) {
		if r.Timeout.Connect != 3*time.Second || r.Timeout.Read != 45*time.Second || !r.NoProxy || !r.NoRedirects || r.MaxBody != host.MaxResponseBytes {
			t.Error("production timeout/transport options changed", r.Timeout)
		}
	}})
	out, err := f.s.UpdateNetwork(f.ctx, f.r.ID, jsonresp.Value(map[string]bool{"auto_network": true}))
	must(t, err)
	if !flag(object(out)["auto_network"]) || calls != 1 || !bytes.Equal(before, document(t, f)) {
		t.Fatal("network confirmation changed config or used extra probe")
	}
	changed := testNetwork
	changed.Interface = "eth1"
	recordBefore := recordBytes(t, f)
	_, err = f.s.UpdateNetwork(f.ctx, f.r.ID, jsonresp.Value(map[string]any{"network": changed}))
	refusal(t, err, 409, msgNetworkActual)
	if !bytes.Equal(recordBefore, recordBytes(t, f)) {
		t.Fatal("failed detection wrote")
	}
	got, _ := f.s.Repository.ConfigRevision(f.ctx, "default")
	if got != revision {
		t.Fatal("network update changed revision")
	}
}
func TestCrossAgentCredentialsRemovedBeforeSessionProjection(t *testing.T) {
	f := newFixture(t)
	f.enroll(t)
	f.prepareRun(t)
	err := f.s.Repository.UpdateSystem(f.ctx, func(_ context.Context, sys *model.System) error {
		sys.Agents[1].LoggingConfig = &model.AgentLoggingConfig{Enabled: json.RawMessage(`{"password":"synthetic-hidden-credential"}`)}
		return nil
	})
	must(t, err)
	must(t, f.s.progress(f.ctx, f.r.ID, "mihomo", jsonresp.Value(map[string]any{"deployment_id": "synthetic-deployment", "status": "failed", "message": "failure synthetic-hidden-credential", "error": "encoded synthetic%2Dhidden%2Dcredential"})))
	b := recordBytes(t, f)
	view, err := f.s.Get(f.ctx, f.r.ID)
	must(t, err)
	for _, data := range [][]byte{b, view} {
		if bytes.Contains(data, []byte("synthetic-hidden-credential")) || bytes.Contains(data, []byte("synthetic%2Dhidden%2Dcredential")) {
			t.Fatal("cross Agent credential leaked")
		}
	}
	if !bytes.Contains(b, []byte("[REDACTED]")) {
		t.Fatal("redaction guard did not run")
	}
}
func TestRuntimeWriteOwnersShutdownAndDisconnect(t *testing.T) {
	for _, stage := range []string{"shutdown-before", "shutdown-render", "disconnect-render"} {
		t.Run(stage, func(t *testing.T) {
			f := newFixture(t)
			runtime := lifecycle.NewRuntime(lifecycle.Options{Mode: lifecycle.ModeManual, Registry: lifecycle.NewRegistry()})
			runtime.Start(context.Background())
			defer runtime.Shutdown(context.Background())
			request, cancel := context.WithCancel(f.ctx)
			defer cancel()
			bound := runtime.BindRequest(request)
			body := object(jsonresp.Value(f.body))
			body["name"] = jsonresp.Value("runtime-linux")
			before := document(t, f)
			recordsBefore, err := os.ReadDir(f.s.root())
			must(t, err)
			if stage == "shutdown-before" {
				runtime.Shutdown(context.Background())
				_, err = f.s.Create(bound, jsonresp.Value(body))
				if !errors.Is(err, context.Canceled) {
					t.Fatal("shutdown creation", err)
				}
				for _, write := range []func() error{func() error {
					_, e := f.s.Register(bound, f.r.ID, f.r.Token, jsonresp.Value(map[string]any{}), "")
					return e
				}, func() error { _, e := f.s.Enroll(bound, f.r.ID, f.r.Token, jsonresp.Value(map[string]any{})); return e }, func() error { _, e := f.s.UpdateNetwork(bound, f.r.ID, jsonresp.Value(map[string]any{})); return e }, func() error { _, e := f.s.Publish(bound, f.r.ID, jsonresp.Value(map[string]any{})); return e }, func() error { return f.s.RunPublication(bound, f.r.ID) }} {
					if e := write(); !errors.Is(e, context.Canceled) {
						t.Fatal("shutdown owner", e)
					}
				}
			} else {
				original := f.s.Render
				f.s.Render = func(o installer.GatewayInstallOptions) (string, error) {
					if strings.HasPrefix(stage, "shutdown") {
						runtime.Shutdown(context.Background())
					} else {
						cancel()
					}
					return original(o)
				}
				_, err = f.s.Create(bound, jsonresp.Value(body))
				if strings.HasPrefix(stage, "shutdown") {
					if !errors.Is(err, context.Canceled) {
						t.Fatal("shutdown render", err)
					}
				} else {
					must(t, err)
				}
			}
			recordsAfter, e := os.ReadDir(f.s.root())
			must(t, e)
			if stage != "disconnect-render" {
				if len(recordsBefore) != len(recordsAfter) || !bytes.Equal(before, document(t, f)) {
					t.Fatal("shutdown landed a write")
				}
			} else if len(recordsAfter) != len(recordsBefore)+1 {
				t.Fatal("disconnect aborted capability write")
			}
		})
	}
}
func TestSessionWaitRechecksShutdown(t *testing.T) {
	f := newFixture(t)
	rt := lifecycle.NewRuntime(lifecycle.Options{Mode: lifecycle.ModeManual, Registry: lifecycle.NewRegistry()})
	rt.Start(context.Background())
	bound := rt.BindRequest(f.ctx)
	path, _ := f.s.path(f.r.ID)
	_, release, err := sessions.Lock(f.ctx, path, time.Second)
	must(t, err)
	done := make(chan error, 1)
	go func() {
		_, e := f.s.Enroll(bound, f.r.ID, f.r.Token, jsonresp.Value(map[string]any{"agents": map[string]any{"mihomo": map[string]string{}, "oxidns": map[string]string{}}}))
		done <- e
	}()
	rt.Shutdown(context.Background())
	release()
	if err = <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("session wait wrote after shutdown", err)
	}
}
func TestRecordUnknownFieldsAndMalformedOwnership(t *testing.T) {
	f := newFixture(t)
	b := recordBytes(t, f)
	var r Record
	must(t, json.Unmarshal(b, &r))
	again, e := json.Marshal(r)
	must(t, e)
	if !reflect.DeepEqual(object(b), object(again)) {
		t.Fatal("record round trip lost fields")
	}
	for _, patch := range []string{`{"id":"wrong"}`, `{"config_revision":true}`, `{"token":null}`, `{"agent_ids":null}`} {
		o := object(b)
		for k, v := range object([]byte(patch)) {
			o[k] = v
		}
		p, _ := f.s.path(f.r.ID)
		must(t, os.WriteFile(p, jsonresp.Value(o), 0600))
		_, err := f.s.Get(f.ctx, f.r.ID)
		refusal(t, err, 409, msgCorrupt)
	}
}

func (f *fixture) fakePair(t *testing.T, options map[string]agentfake.Options) map[string]*agentfake.Agent {
	t.Helper()
	dest := map[string]string{}
	fakes := map[string]*agentfake.Agent{}
	for _, service := range services {
		a, err := f.s.agent(f.ctx, f.r.AgentIDs[service])
		must(t, err)
		o := options[service]
		o.AgentID = a.ID
		o.Token = a.Token
		o.ServiceType = service
		fake := agentfake.New(o)
		srv := httptest.NewServer(fake)
		t.Cleanup(srv.Close)
		dest[fmt.Sprintf("%s:%d", a.Host, a.Port)] = srv.URL
		fakes[service] = fake
	}
	f.s.Manager.SetClient(&routeClient{dest: dest, inspect: func(r agents.Request) {
		if r.Timeout.Connect <= 0 || r.Timeout.Read <= 0 {
			t.Error("unbounded device request")
		}
		if r.Method == "GET" && len(r.Body) != 0 {
			t.Error("GET has a JSON body")
		}
	}})
	return fakes
}
func (f *fixture) prepareRun(t *testing.T) {
	t.Helper()
	f.r.Phase = "publishing"
	for _, service := range services {
		f.r.Deployments[service] = jsonresp.Value(map[string]string{"deployment_id": "job-" + service, "status": "preparing"})
	}
	must(t, f.s.write(f.ctx, f.r))
}
func uploads(a *agentfake.Agent) []agentfake.Call {
	out := []agentfake.Call{}
	for _, c := range a.Calls() {
		if c.Method == "POST" && c.Path == "/api/deployments" {
			out = append(out, c)
		}
	}
	return out
}
func TestRealOrderedPublicationFailureAndResume(t *testing.T) {
	for _, scenario := range []string{"normal", "mihomo-failed", "oxidns-failed", "uncertain-upload"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t)
			f.enroll(t)
			f.prepareRun(t)
			opts := map[string]agentfake.Options{}
			switch scenario {
			case "mihomo-failed":
				opts["mihomo"] = agentfake.Options{ActivationOutcome: "rolled_back"}
			case "oxidns-failed":
				opts["oxidns"] = agentfake.Options{ActivationOutcome: "rolled_back"}
			case "uncertain-upload":
				opts["mihomo"] = agentfake.Options{Script: []agentfake.Fault{{Method: "POST", Path: "/api/deployments", LoseResponse: true}}}
			}
			fake := f.fakePair(t, opts)
			err := f.s.RunPublication(f.ctx, f.r.ID)
			failed := strings.HasSuffix(scenario, "failed")
			if failed {
				if err == nil {
					t.Fatal("failed core accepted")
				}
			} else {
				must(t, err)
			}
			r, e := f.s.read(f.ctx, f.r.ID)
			must(t, e)
			a, b := uploads(fake["mihomo"]), uploads(fake["oxidns"])
			if len(a) != 1 {
				t.Fatal("Mihomo upload count", len(a))
			}
			if scenario == "mihomo-failed" {
				if len(b) != 0 {
					t.Fatal("OxiDNS started after Mihomo failed")
				}
			} else {
				if len(b) != 1 || !b[0].At.After(a[0].At) {
					t.Fatal("publication did not preserve order")
				}
			}
			for kind, receiver := range fake {
				for _, c := range receiver.Calls() {
					if strings.Contains(c.Path, "linux-gateway/") {
						t.Fatal("publication implicitly applied networking")
					}
					if c.Manifest != nil {
						if c.Manifest.DeploymentID != "job-"+kind || c.Manifest.AgentID != f.r.AgentIDs[kind] || string(c.Manifest.ProfileID) != `"default"` || c.Manifest.ServiceType != kind || c.Headers.Get("X-Activate") != "true" {
							t.Fatal("raw upload identity/activation mismatch", c.Manifest, c.Headers)
						}
					}
				}
			}
			if failed {
				if r.Phase != "publish_failed" || !terminal(taskOf(r.Deployments[map[bool]string{true: "mihomo", false: "oxidns"}[scenario == "mihomo-failed"]]).Status) {
					t.Fatal("failure checkpoint missing")
				}
				_, e = f.s.Publish(f.ctx, r.ID, jsonresp.Value(map[string]any{}))
				refusal(t, e, 409, msgRetry)
			} else {
				if r.Phase != "published" || r.Message != msgPublished {
					t.Fatal("completion checkpoint", r.Phase, r.Message)
				}
				view, e := f.s.Get(f.ctx, r.ID)
				must(t, e)
				if flag(object(view)["config_changed"]) || flag(object(view)["target_changed"]) {
					t.Fatal("fresh success target changed")
				}
				must(t, f.s.RunPublication(f.ctx, r.ID))
				if len(uploads(fake["mihomo"])) != 1 || len(uploads(fake["oxidns"])) != 1 {
					t.Fatal("resume reuploaded a successful core")
				}
			}
			if scenario == "oxidns-failed" {
				// The retry entry point replaces only the failed OxiDNS ID; capture its worker
				// through the real Runtime and wait for completion.
				replacement := agentfake.New(agentfake.Options{AgentID: f.r.AgentIDs["oxidns"], Token: func() string { a, _ := f.s.agent(f.ctx, f.r.AgentIDs["oxidns"]); return a.Token }(), ServiceType: "oxidns"})
				srv := httptest.NewServer(replacement)
				defer srv.Close()
				client := f.s.Manager.Client().(*routeClient)
				client.dest[testNetwork.Address+":8082"] = srv.URL
				runtime := lifecycle.NewRuntime(lifecycle.Options{Mode: lifecycle.ModeManual, Registry: lifecycle.NewRegistry()})
				runtime.Start(context.Background())
				defer runtime.Shutdown(context.Background())
				f.s.Spawner = runtime
				_, e = f.s.Publish(runtime.BindRequest(f.ctx), r.ID, jsonresp.Value(map[string]bool{"retry_failed": true}))
				must(t, e)
				waitCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				must(t, runtime.WaitIdle(waitCtx))
				retried, e := f.s.read(f.ctx, r.ID)
				must(t, e)
				if retried.Phase != "published" || taskOf(retried.Deployments["mihomo"]).DeploymentID != "job-mihomo" || taskOf(retried.Deployments["oxidns"]).DeploymentID == "job-oxidns" || len(uploads(fake["mihomo"])) != 1 {
					t.Fatal("retry did not retain healthy first core", retried.Phase, retried.Message)
				}
			}
			if lockx.Violations() != 0 {
				t.Fatal("lock inversion", lockx.Violations())
			}
		})
	}
}
func TestPublishLeaseAndRevisionGuard(t *testing.T) {
	f := newFixture(t)
	f.enroll(t)
	f.prepareRun(t)
	f.fakePair(t, nil)
	path, _ := f.s.path(f.r.ID)
	_, release, ok := publications.TryLock(f.ctx, path)
	if !ok {
		t.Fatal("lease busy")
	}
	before := recordBytes(t, f)
	_, err := f.s.Publish(f.ctx, f.r.ID, jsonresp.Value(map[string]any{}))
	release()
	must(t, err)
	if !bytes.Equal(before, recordBytes(t, f)) {
		t.Fatal("contending request reset durable task IDs")
	}
	_, err = f.s.Repository.UpdateProfile(f.ctx, "default", func(_ context.Context, p *model.Profile) error {
		p.Description = "changed"
		return nil
	})
	must(t, err)
	_, err = f.s.Publish(f.ctx, f.r.ID, jsonresp.Value(map[string]any{}))
	refusal(t, err, 409, msgConfigChanged)
}
func TestReprepareReusesInstalledPairAndKeepsScript(t *testing.T) {
	f := newFixture(t)
	f.enroll(t)
	old := *f.r
	_, err := f.s.Repository.UpdateProfile(f.ctx, "default", func(_ context.Context, p *model.Profile) error {
		p.Description = "reviewed revision"
		return nil
	})
	must(t, err)
	revision, err := f.s.Repository.ConfigRevision(f.ctx, "default")
	must(t, err)
	f.body["config_revision"] = jsonresp.Value(revision)
	out, err := f.s.Create(f.ctx, jsonresp.Value(f.body))
	must(t, err)
	if text(object(out)["id"]) != old.ID {
		t.Fatal("new revision required reinstall")
	}
	current, err := f.s.read(f.ctx, old.ID)
	must(t, err)
	if current.Script != old.Script || current.Token != old.Token || current.DownloadPath != old.DownloadPath || !reflect.DeepEqual(current.AgentIDs, old.AgentIDs) || current.Phase != "enrolled" || current.ConfigRevision != revision {
		t.Fatal("reprepare changed capability or target")
	}
}
func FuzzInstallationRecord(f *testing.F) {
	f.Add([]byte(`{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","profile_id":"default","token":"synthetic-capability","config_revision":1,"network":{},"ports":{"mihomo":8080,"oxidns":8082},"agent_ids":{},"registered":{},"deployments":{},"future":{"secret":"synthetic"}}`))
	f.Add([]byte(`{"config_revision":true}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > maxRecordBytes {
			return
		}
		var r Record
		if json.Unmarshal(b, &r) != nil {
			return
		}
		out, e := json.Marshal(r)
		if e != nil {
			t.Fatal(e)
		}
		var again Record
		if e = json.Unmarshal(out, &again); e != nil {
			t.Fatal(e)
		}
		if r.ID != again.ID || r.Token != again.Token || r.ConfigRevision != again.ConfigRevision {
			t.Fatal("durable identity changed")
		}
	})
}

func TestReviewedA21ValidationClassAndTwoLevelRedaction(t *testing.T) {
	for _, kind := range []string{"oxidns", "mihomo"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			_, err := f.s.Repository.UpdateProfile(f.ctx, "default", func(_ context.Context, p *model.Profile) error {
				if kind == "oxidns" {
					o := object(jsonresp.Value(p.OxiDNS))
					if o == nil {
						o = map[string]json.RawMessage{}
					}
					o["remote_dns"] = jsonresp.Value("[")
					return p.SetJSON("oxidns", jsonresp.Value(o))
				}
				p.Mihomo.CustomConfig = "dns: ["
				return nil
			})
			must(t, err)
			revision, err := f.s.Repository.ConfigRevision(f.ctx, "default")
			must(t, err)
			f.body["config_revision"] = jsonresp.Value(revision)
			before := document(t, f)
			checkpoint := recordBytes(t, f)
			_, err = f.s.Create(f.ctx, jsonresp.Value(f.body))
			refusal(t, err, 400, "")
			if !bytes.Equal(before, document(t, f)) || !bytes.Equal(checkpoint, recordBytes(t, f)) {
				t.Fatal("invalid saved draft wrote")
			}
		})
	}
	f := newFixture(t)
	f.enroll(t)
	err := f.s.Repository.UpdateShared(f.ctx, func(_ context.Context, shared *model.Shared) error {
		return shared.Nodes[0].Params.UnmarshalJSON([]byte(`{"password":"linux"}`))
	})
	must(t, err)
	view, err := f.s.Get(f.ctx, f.r.ID)
	must(t, err)
	if text(object(view)["name"]) != f.r.Name || object(view)["network"] == nil || !bytes.Contains(view, []byte(`"mihomo"`)) {
		t.Fatal("short user credential corrupted structural fields")
	}
	safe := f.s.PublicError(f.ctx, &Error{Message: "failure linux", Status: 400})
	refusal(t, safe, 400, "[REDACTED]")
}

func TestPublicationWorkerKeepsRuntimeAfterDisconnectAndShutdown(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(fmt.Sprint(shutdown), func(t *testing.T) {
			f := newFixture(t)
			f.enroll(t)
			f.prepareRun(t)
			fake := f.fakePair(t, nil)
			runtime := lifecycle.NewRuntime(lifecycle.Options{Mode: lifecycle.ModeManual, Registry: lifecycle.NewRegistry(), Grace: 2100 * time.Millisecond})
			runtime.Start(context.Background())
			defer runtime.Shutdown(context.Background())
			f.s.Spawner = runtime
			request, cancel := context.WithCancel(f.ctx)
			defer cancel()
			bound := runtime.BindRequest(request)
			client := f.s.Manager.Client().(*routeClient)
			entered := make(chan struct{})
			resume := make(chan struct{})
			var once sync.Once
			var checkpoint []byte
			fingerprint, _ := f.s.Repository.Store().Fingerprint()
			previous := client.inspect
			client.inspect = func(r agents.Request) {
				previous(r)
				if r.Method == "POST" && strings.HasSuffix(r.URL, "/api/deployments") {
					once.Do(func() {
						checkpoint = recordBytes(t, f)
						var e error
						fingerprint, e = f.s.Repository.Store().Fingerprint()
						must(t, e)
						close(entered)
						<-resume
					})
				}
			}
			_, err := f.s.Publish(bound, f.r.ID, jsonresp.Value(map[string]any{}))
			must(t, err)
			select {
			case <-entered:
			case <-time.After(10 * time.Second):
				t.Fatal("worker did not reach upload")
			}
			if shutdown {
				runtime.Shutdown(context.Background())
				close(resume)
			} else {
				cancel()
				close(resume)
			}
			wait, cancelWait := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancelWait()
			must(t, runtime.WaitIdle(wait))
			final, err := f.s.read(f.ctx, f.r.ID)
			must(t, err)
			if shutdown {
				after, e := f.s.Repository.Store().Fingerprint()
				must(t, e)
				if final.Phase != "publishing" || !bytes.Equal(checkpoint, recordBytes(t, f)) || fingerprint != after || len(uploads(fake["mihomo"])) != 0 || len(uploads(fake["oxidns"])) != 0 {
					t.Fatal("shutdown crossed a commit/device boundary", final.Phase)
				}
			} else if final.Phase != "published" || len(uploads(fake["mihomo"])) != 1 || len(uploads(fake["oxidns"])) != 1 {
				t.Fatal("disconnect lost tracked publication", final.Phase, final.Message)
			}
		})
	}
}
func TestFullTUNTakeoverNeedsDistinctCapability(t *testing.T) {
	f := newFixture(t)
	f.enroll(t)
	snapshot, _, err := f.s.Repository.PublishConfig(f.ctx, "default", true)
	must(t, err)
	var doc onboarding.Document
	must(t, json.Unmarshal(jsonresp.Value(snapshot), &doc))
	form, err := onboarding.ParseRequest(jsonresp.Value(map[string]any{"rule_set_ids": []string{}, "direct_rule_set_ids": []string{}, "proxy_rule_set_ids": []string{}, "local_dns": "https://192.0.2.53/dns-query"}))
	must(t, err)
	candidate, report, err := onboarding.Prepare(f.ctx, doc, form, onboarding.Options{BaseURL: testOrigin, Takeover: "full_tun"})
	must(t, err)
	if !report.Ready {
		t.Fatal(report.Blockers)
	}
	revision, err := f.s.Repository.UpdateProfile(f.ctx, "default", func(_ context.Context, p *model.Profile) error {
		if e := p.SetJSON("mihomo", candidate["mihomo"]); e != nil {
			return e
		}
		return p.SetJSON("oxidns", candidate["oxidns"])
	})
	must(t, err)
	f.body["config_revision"] = jsonresp.Value(revision)
	out, err := f.s.Create(f.ctx, jsonresp.Value(f.body))
	must(t, err)
	id := text(object(out)["id"])
	if id == f.r.ID || text(object(out)["takeover"]) != "full_tun" {
		t.Fatal("takeover reused selective capability")
	}
	r, err := f.s.read(f.ctx, id)
	must(t, err)
	if !strings.Contains(r.Script, "-prepare-tun -assume-tun-enabled") || !strings.Contains(r.Script, "persist_ipv4_forwarding") || strings.Contains(r.Script, "CONFIGFLOW_FORWARDING_OWNER") {
		t.Fatal("full TUN installer was not reused")
	}
	f.r.ConfigRevision = revision
	_, _, err = f.s.savedSnapshot(f.ctx, f.r)
	refusal(t, err, 409, msgTakeoverChanged)
}

func agentEditsForTest(t testing.TB, value any) agentcore.AgentEdits {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	edits, err := agentcore.DecodeAgentEdits(raw)
	if err != nil {
		t.Fatal(err)
	}
	return edits
}

func TestNativeSystemCredentialsReachSharedRedactor(t *testing.T) {
	f := newFixture(t)
	err := f.s.Repository.UpdateSystem(f.ctx, func(_ context.Context, sys *model.System) error {
		sys.SystemConfig.RuleFetchProxy = "http://operator:synthetic-native-system-secret@proxy.example.com"
		return nil
	})
	must(t, err)
	secrets, err := f.s.secrets(f.ctx)
	must(t, err)
	found := false
	for _, value := range secrets {
		found = found || value == "http://operator:synthetic-native-system-secret@proxy.example.com"
	}
	if !found {
		t.Fatal("typed system credential bypassed shared redactor")
	}
}
