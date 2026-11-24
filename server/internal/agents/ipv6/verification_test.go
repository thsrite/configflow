package ipv6

import (
	"configflow/internal/jsonresp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

func waitReport(t *testing.T, s *Service, ctx context.Context, key string) Document {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a, e := s.agent(ctx, "source", "default")
		if e != nil {
			t.Fatal(e)
		}
		b, _ := runs(a).Get(key)
		r := object(b)
		if text(r["status"]) != "running" && r != nil {
			return r
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("worker did not finish")
	return nil
}
func TestManualVerificationSeparateStateAndStaleness(t *testing.T) {
	startRuntime(t)
	s, ctx := fixture(t)
	_, revision, e := s.snapshot(ctx, "default")
	if e != nil {
		t.Fatal(e)
	}
	s.Manager.SetClient(clientFunc(func(ctx context.Context, r agents.Request) (*agents.Response, error) {
		if strings.HasSuffix(r.URL, "/verify") {
			checks := []Check{}
			for _, id := range []string{"deployed_config", "runtime_core", "runtime_ipv6", "runtime_tun", "proxy_http", "node_ipv6_transport", "dns_a", "dns_aaaa"} {
				checks = append(checks, check(id, id, "ok", "synthetic evidence"))
			}
			return response(Document{"success": jsonresp.Value(true), "checks": jsonresp.Value(checks)}), nil
		}
		return response(hostStatus()), nil
	}))
	_, e = s.Start(ctx, "source", "default", jsonresp.Value(Document{"expected_config_revision": jsonresp.Value(revision)}), false)
	if e != nil {
		t.Fatal(e)
	}
	report := waitReport(t, s, ctx, "manual")
	if text(report["status"]) != "warning" || text(report["scope"]) != "mihomo_runtime" || !strings.Contains(string(report["checks"]), "runtime_expected_config") || !strings.Contains(string(report["checks"]), "client_not_configured") {
		t.Fatal(string(jsonresp.Value(report)))
	}
	_, after, e := s.snapshot(ctx, "default")
	if e != nil || after != revision {
		t.Fatal("verification changed draft revision", after, e)
	}
	a, _ := s.agent(ctx, "source", "default")
	if a["latest_deployment"] != nil {
		t.Fatal("unrelated registry members changed")
	}
	_, e = s.Start(ctx, "source", "default", jsonresp.Value(Document{"expected_config_revision": jsonresp.Value(revision + 1)}), false)
	requireCode(t, e, "CONFIG_CHANGED", 409)
	if e = s.update(ctx, "source", func(_ context.Context, a Document, _ []Document) error {
		a["gateway_installation_id"] = jsonresp.Value("replacement-installation")
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	before := s.Manager.Repository().CommitCount()
	result, e := s.Result(ctx, "source", "default", "")
	if e != nil || !strings.Contains(string(result.Verification), `"stale":true`) {
		t.Fatal(result, e)
	}
	if s.Manager.Repository().CommitCount() != before {
		t.Fatal("result GET wrote stale overlay")
	}
}
func seedPublication(t *testing.T, s *Service, ctx context.Context, did string) {
	t.Helper()
	config, revision, e := s.snapshot(ctx, "default")
	if e != nil {
		t.Fatal(e)
	}
	source, e := deploy.SourceFromJSON(jsonresp.Value(config), &revision)
	if e != nil {
		t.Fatal(e)
	}
	data := []byte("ipv6: true\n")
	sum := sha256.Sum256(data)
	uploaded := hex.EncodeToString(sum[:])
	file := deploy.File{Path: "config.yaml", SHA256: uploaded, Size: int64(len(data)), Role: deploy.RoleConfig}
	manifest := deploy.Manifest{ProtocolVersion: 1, DeploymentID: did, AgentID: "source", ProfileID: "default", ServiceType: "mihomo", ConfigPath: "config.yaml", Files: []deploy.File{file}}
	bundle, e := deploy.Pack(manifest, map[string]deploy.Content{"config.yaml": {Data: data, Role: deploy.RoleConfig}}, uploaded, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = deploy.NewReceipts(s.Manager.Repository()).Capture(ctx, "source", bundle, source, "https://panel.example", nil); e != nil {
		t.Fatal(e)
	}
	e = s.update(ctx, "source", func(_ context.Context, a Document, _ []Document) error {
		final := strings.Repeat("a", 64)
		task := Document{"deployment_id": jsonresp.Value(did), "status": jsonresp.Value("succeeded"), "profile_id": jsonresp.Value("default"), "service_type": jsonresp.Value("mihomo"), "config_revision": jsonresp.Value(revision), "config_version": jsonresp.Value(final), "ipv6_verification_requested": jsonresp.Value(true)}
		a["deployments"] = jsonresp.Value(Document{did: jsonresp.Value(task)})
		a["latest_deployment"] = jsonresp.Value(task)
		a["config_revision"] = jsonresp.Value(revision)
		a["config_version"] = jsonresp.Value(final)
		a["applied_content"] = jsonresp.Value(Document{"deployment_id": jsonresp.Value(did), "profile_id": jsonresp.Value("default"), "service_type": jsonresp.Value("mihomo"), "config_version": jsonresp.Value(final)})
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
}
func TestPublicationSnapshotAndAdaptedDigest(t *testing.T) {
	startRuntime(t)
	s, ctx := fixture(t)
	seedPublication(t, s, ctx, "publication-one")
	_, e := s.Manager.Repository().UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		p.Mihomo.CustomConfig = "ipv6: false\n"
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	hashes := make(chan string, 1)
	s.Manager.SetClient(clientFunc(func(_ context.Context, r agents.Request) (*agents.Response, error) {
		if strings.HasSuffix(r.URL, "/verify") {
			hashes <- text(object(r.Body)["expected_config_sha256"])
			return response(Document{"success": jsonresp.Value(true), "checks": jsonresp.Value([]Check{})}), nil
		}
		return response(hostStatus()), nil
	}))
	result, e := s.Start(ctx, "source", "default", jsonresp.Value(Document{"deployment_id": jsonresp.Value("publication-one")}), false)
	if e != nil || result.Applicable != nil {
		t.Fatal(result, e)
	}
	report := waitReport(t, s, ctx, "publication-one")
	if got := <-hashes; got != strings.Repeat("a", 64) {
		t.Fatal("used uploaded instead of adapted digest", got)
	}
	if text(report["status"]) == "ok" || !strings.Contains(string(report["checks"]), "runtime_evidence") {
		t.Fatal("incomplete Agent reply passed")
	}
	notApplicable, e := s.Result(ctx, "source", "default", "")
	if e != nil || notApplicable.Applicable == nil || *notApplicable.Applicable {
		t.Fatal(notApplicable, e)
	}
	before := s.Manager.Repository().CommitCount()
	_, e = s.Start(ctx, "source", "default", jsonresp.Value(Document{"deployment_id": jsonresp.Value("publication-one")}), true)
	if e != nil || s.Manager.Repository().CommitCount() != before {
		t.Fatal("automatic re-enrolled existing publication", e)
	}
}
func TestSchedulerUnavailableExpiryAndBadRows(t *testing.T) {
	startRuntime(t)
	s, ctx := fixture(t)
	e := s.update(ctx, "source", func(_ context.Context, a Document, _ []Document) error {
		task := Document{"deployment_id": jsonresp.Value("missing-snapshot"), "status": jsonresp.Value("succeeded"), "profile_id": jsonresp.Value("default"), "service_type": jsonresp.Value("mihomo"), "config_revision": jsonresp.Value(1), "ipv6_verification_requested": jsonresp.Value(true)}
		a["deployments"] = jsonresp.Value(Document{"missing-snapshot": jsonresp.Value(task)})
		a["latest_deployment"] = jsonresp.Value(task)
		a["ipv6_verifications"] = json.RawMessage(`{"bad":false,"expired":{"status":"running","started_epoch":"bad","future":{"keep":true}}}`)
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	idle, e := s.Tick(ctx)
	if e != nil || idle {
		t.Fatal(idle, e)
	}
	a, _ := s.agent(ctx, "source", "default")
	m := runs(a)
	b, _ := m.Get("expired")
	if text(object(b)["status"]) != "error" {
		t.Fatal("expiry discarded unknown fields")
	}
	b, _ = m.Get("bad")
	// A stored entry that is not an object reads as an empty one (O18 T1b).
	if string(b) != "{}" {
		t.Fatal("bad peer lost")
	}
	b, _ = m.Get("missing-snapshot")
	if text(object(b)["error_code"]) != "SNAPSHOT_UNAVAILABLE" {
		t.Fatal(string(b))
	}
	before := s.Manager.Repository().CommitCount()
	idle, e = s.Tick(ctx)
	if e != nil || !idle || s.Manager.Repository().CommitCount() != before {
		t.Fatal("scheduler repeated definite failure", idle, e)
	}
}
func TestQueueBoundAndShutdownDoesNotFinishRun(t *testing.T) {
	startRuntime(t)
	s, ctx := fixture(t)
	for range cap(verificationSlots) {
		verificationSlots <- struct{}{}
	}
	_, e := s.Start(ctx, "source", "default", json.RawMessage(`{}`), false)
	requireCode(t, e, "BUSY", 409)
	for range cap(verificationSlots) {
		<-verificationSlots
	}
	entered := make(chan struct{})
	s.Manager.SetClient(clientFunc(func(ctx context.Context, r agents.Request) (*agents.Response, error) {
		if strings.HasSuffix(r.URL, "/verify") {
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return response(hostStatus()), nil
	}))
	bound := lifecycle.BindRequest(ctx)
	_, e = s.Start(bound, "source", "default", json.RawMessage(`{}`), false)
	if e != nil {
		t.Fatal(e)
	}
	<-entered
	lifecycle.Stop(time.Second)
	a, _ := s.agent(ctx, "source", "default")
	b, _ := runs(a).Get("manual")
	if text(object(b)["status"]) != "running" {
		t.Fatal("shutdown wrote terminal evidence", string(b))
	}
	if len(verificationSlots) != 0 {
		t.Fatal("shutdown leaked queue slot")
	}
	before := s.Manager.Repository().CommitCount()
	_, e = s.VerificationSettings(bound, "source", "default", json.RawMessage(`{"client_agent_id":"","test_url":""}`))
	if e == nil || before != s.Manager.Repository().CommitCount() {
		t.Fatal("post-shutdown settings wrote", e)
	}
}
func TestTwoLevelRedactionKeepsProtocolFields(t *testing.T) {
	t.Setenv("AGENT_REGISTRATION_KEY", "synthetic-registration-key")
	s, ctx := fixture(t)
	e := s.update(ctx, "source", func(_ context.Context, a Document, _ []Document) error {
		a["legacy"] = jsonresp.Value(Document{"password": jsonresp.Value("ok")})
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	e = s.Manager.Repository().UpdateSystem(ctx, func(_ context.Context, sys *model.System) error {
		var peer model.Agent
		if err := json.Unmarshal([]byte(`{"id":"peer","token":"synthetic-peer-token","profile_id":"default","password":"synthetic-peer-password"}`), &peer); err != nil {
			return err
		}
		sys.Agents = append(sys.Agents, peer)
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	value := Document{"outside": jsonresp.Value("synthetic-registration-key"), "status": jsonresp.Value("ok"), "agent_id": jsonresp.Value("ok"), "check_token": jsonresp.Value(strings.Repeat("a", 64)), "checks": jsonresp.Value([]Check{check("ok", "ok", "ok", "ok synthetic-peer-token synthetic-peer-password synthetic-source-token")})}
	public, e := s.Public(ctx, "default", value)
	if e != nil {
		t.Fatal(e)
	}
	d := object(public)
	checks := documents(d["checks"])
	if len(checks) != 1 || text(checks[0]["status"]) != "ok" || text(checks[0]["id"]) != "ok" {
		t.Fatal("short credential broke check protocol", string(public))
	}
	if text(d["status"]) != "ok" || text(d["agent_id"]) != "ok" || text(d["check_token"]) != strings.Repeat("a", 64) {
		t.Fatal("short credential broke protocol", string(public))
	}
	if strings.Contains(string(public), "synthetic-registration-key") || strings.Contains(string(public), "synthetic-peer-") || strings.Contains(string(public), "synthetic-source-token") {
		t.Fatal("cross-Agent credential leak", string(public))
	}
}

func addClient(t *testing.T, s *Service, ctx context.Context) {
	t.Helper()
	e := s.Manager.Repository().UpdateSystem(ctx, func(_ context.Context, sys *model.System) error {
		var client model.Agent
		data := jsonresp.Value(map[string]any{
			"id": "client", "host": "192.0.2.3", "port": 8080, "token": "synthetic-client-token",
			"profile_id": "default", "service_type": "oxidns", "deployment_method": "shell",
			"enabled": true, "status": "online", "last_heartbeat": time.Now().Format(time.RFC3339),
		})
		if err := json.Unmarshal(data, &client); err != nil {
			return err
		}
		sys.Agents = append(sys.Agents, client)
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
}
func TestSettingsIndependentClientAndCapabilities(t *testing.T) {
	startRuntime(t)
	s, ctx := fixture(t)
	addClient(t, s, ctx)
	s.Manager.SetClient(clientFunc(func(_ context.Context, r agents.Request) (*agents.Response, error) {
		if !strings.HasSuffix(r.URL, "/api/capabilities") || r.Method != "GET" || len(r.Body) != 0 || r.Timeout != agents.Pair(2, 3) {
			t.Errorf("wrong capability request %+v", r)
		}
		return response(Document{"success": jsonresp.Value(true), "platform": jsonresp.Value("linux"), "ipv6_client_probe_protocols": jsonresp.Value([]int{1})}), nil
	}))
	_, e := s.VerificationSettings(ctx, "source", "default", json.RawMessage(`{"client_agent_id":"source","test_url":""}`))
	requireCode(t, e, "CLIENT_UNAVAILABLE", 409)
	result, e := s.VerificationSettings(ctx, "source", "default", json.RawMessage(`{"client_agent_id":" client ","test_url":" https://site.example/ "}`))
	if e != nil || result.Settings.ClientAgentID != "client" || len(result.Candidates) != 1 || !result.Candidates[0].Supported {
		t.Fatal(result, e)
	}
	if e = s.update(ctx, "source", func(_ context.Context, a Document, _ []Document) error {
		a["ipv6_verifications"] = jsonresp.Value(Document{"manual": jsonresp.Value(Document{"status": jsonresp.Value("running")})})
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	_, e = s.VerificationSettings(ctx, "source", "default", json.RawMessage(`{"client_agent_id":"","test_url":""}`))
	requireCode(t, e, "BUSY", 409)
}
func TestClientChecksRequireMatchingRunAndIdentity(t *testing.T) {
	for _, mode := range []string{"matched", "different-run", "changed-client"} {
		t.Run(mode, func(t *testing.T) {
			startRuntime(t)
			s, ctx := fixture(t)
			addClient(t, s, ctx)
			a, e := s.agent(ctx, "source", "default")
			if e != nil {
				t.Fatal(e)
			}
			config, _, e := s.snapshot(ctx, "default")
			if e != nil {
				t.Fatal(e)
			}
			clientDone := make(chan struct{})
			runID := strings.Repeat("b", 32)
			s.Manager.SetClient(clientFunc(func(ctx context.Context, r agents.Request) (*agents.Response, error) {
				if strings.HasSuffix(r.URL, "/capabilities") {
					return response(Document{"platform": jsonresp.Value("linux"), "ipv6_client_probe_protocols": jsonresp.Value([]int{1})}), nil
				}
				if strings.HasSuffix(r.URL, "/verify-connections") {
					<-clientDone
					id := runID
					if mode == "different-run" {
						id = "previous"
					}
					return response(Document{"success": jsonresp.Value(true), "request_id": jsonresp.Value(id), "target_host": jsonresp.Value("site.example"), "matches": jsonresp.Value([]Document{{"source_ip": jsonresp.Value("192.0.2.3"), "source_port": jsonresp.Value(34567), "host": jsonresp.Value("site.example"), "network": jsonresp.Value("tcp"), "destination_port": jsonresp.Value(443), "proxy_name": jsonresp.Value("node"), "proxy_type": jsonresp.Value("ss"), "chains": jsonresp.Value([]string{"node"}), "ipv6_transport_verified": jsonresp.Value(true)}})}), nil
				}
				if strings.HasSuffix(r.URL, "/verify-client") {
					if mode == "changed-client" {
						if e := s.update(ctx, "client", func(_ context.Context, a Document, _ []Document) error {
							a["token"] = jsonresp.Value("synthetic-replaced-client")
							return nil
						}); e != nil {
							t.Error(e)
						}
					}
					close(clientDone)
					return response(Document{"success": jsonresp.Value(true), "request_id": jsonresp.Value(runID), "target_host": jsonresp.Value("site.example"), "requests": jsonresp.Value([]Document{{"status": jsonresp.Value("ok"), "source_ip": jsonresp.Value("192.0.2.3"), "source_port": jsonresp.Value(34567), "address": jsonresp.Value("198.51.100.9"), "family": jsonresp.Value("ipv4")}})}), nil
				}
				return response(hostStatus()), nil
			}))
			var checks []Check
			e = deploy.RunDetached(ctx, "test-client-evidence", func(ctx context.Context) error {
				var err error
				checks, err = s.clientChecks(ctx, a, config, Settings{ClientAgentID: "client"}, "https://site.example/", runID)
				return err
			})
			if mode == "changed-client" {
				requireCode(t, e, "CLIENT_CHANGED", 409)
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			states := map[string]string{}
			for _, c := range checks {
				states[c.ID] = c.Status
			}
			if mode == "matched" {
				if states["client_mihomo_path"] != "ok" || states["client_ipv6_transport"] != "ok" || states["client_ros_path"] != "unknown" {
					t.Fatal(states)
				}
			} else if states["client_request_identity"] != "unknown" || states["client_mihomo_path"] == "ok" {
				t.Fatal("stale request counted", states)
			}
		})
	}
}

func TestVerificationRunsAtMostTwoWorkers(t *testing.T) {
	startRuntime(t)
	s, ctx := fixture(t)
	a, e := s.Manager.ReadAgent(ctx, "source", true)
	if e != nil {
		t.Fatal(e)
	}
	baseRaw, e := json.Marshal(a)
	if e != nil {
		t.Fatal(e)
	}
	e = s.Manager.Repository().UpdateSystem(ctx, func(_ context.Context, sys *model.System) error {
		for _, id := range []string{"source-two", "source-three"} {
			var clone model.Agent
			if err := json.Unmarshal(baseRaw, &clone); err != nil {
				return err
			}
			clone.ID = id
			sys.Agents = append(sys.Agents, clone)
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	entered := make(chan struct{}, 3)
	var active, maxActive atomic.Int32
	s.Manager.SetClient(clientFunc(func(ctx context.Context, _ agents.Request) (*agents.Response, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := maxActive.Load(); n > old; old = maxActive.Load() {
			if maxActive.CompareAndSwap(old, n) {
				break
			}
		}
		entered <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}))
	for _, id := range []string{"source", "source-two", "source-three"} {
		if _, e = s.Start(lifecycle.BindRequest(ctx), id, "default", json.RawMessage(`{}`), false); e != nil {
			t.Fatal(e)
		}
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("workers did not start")
		}
	}
	if len(verificationWorkers) != 2 || len(verificationSlots) != 3 {
		t.Fatal("worker/queue bounds", len(verificationWorkers), len(verificationSlots))
	}
	select {
	case <-entered:
		t.Fatal("third worker ran concurrently")
	default:
	}
	lifecycle.Stop(time.Second)
	if maxActive.Load() != 2 || len(verificationWorkers) != 0 || len(verificationSlots) != 0 {
		t.Fatal("worker release", maxActive.Load(), len(verificationWorkers), len(verificationSlots))
	}
}

func TestClientRequiresStoredOnlineStatus(t *testing.T) {
	s, ctx := fixture(t)
	addClient(t, s, ctx)
	if e := s.update(ctx, "client", func(_ context.Context, a Document, _ []Document) error {
		a["status"] = jsonresp.Value("preparing")
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	source, e := s.agent(ctx, "source", "default")
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.client(ctx, source, "client", false)
	requireCode(t, e, "CLIENT_UNAVAILABLE", 409)
}

func TestEnrollOnlyValidNewIPv6Publications(t *testing.T) {
	startRuntime(t)
	s, ctx := fixture(t)
	task := Document{"deployment_id": jsonresp.Value("new"), "status": jsonresp.Value("preparing"), "future": jsonresp.Value(Document{"keep": jsonresp.Value(true)})}
	if e := s.update(ctx, "source", func(_ context.Context, a Document, _ []Document) error {
		a["deployments"] = jsonresp.Value(Document{"new": jsonresp.Value(task)})
		a["latest_deployment"] = jsonresp.Value(task)
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	for _, yaml := range []string{"ipv6: false", "ipv6: 'true'"} {
		if e := s.Enroll(ctx, "source", "new", Document{"mihomo": jsonresp.Value(map[string]string{"custom_config": yaml})}); e != nil {
			t.Fatal(e)
		}
		a, _ := s.agent(ctx, "source", "default")
		if yes(object(a["latest_deployment"])["ipv6_verification_requested"]) {
			t.Fatal("enrolled invalid/disabled IPv6")
		}
	}
	config, _, e := s.snapshot(ctx, "default")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Enroll(ctx, "source", "new", config); e != nil {
		t.Fatal(e)
	}
	a, _ := s.agent(ctx, "source", "default")
	latest := object(a["latest_deployment"])
	stored := object(object(a["deployments"])["new"])
	if !yes(latest["ipv6_verification_requested"]) || string(jsonresp.Value(stored)) != string(jsonresp.Value(latest)) {
		t.Fatal("enrollment lost task/mirror", latest)
	}
	bound := lifecycle.BindRequest(ctx)
	lifecycle.Stop(time.Second)
	before := s.Manager.Repository().CommitCount()
	if e = s.Enroll(bound, "source", "new", config); e == nil || s.Manager.Repository().CommitCount() != before {
		t.Fatal("stopped enrollment wrote", e)
	}
}
func TestSchedulerStartsCapturedPublicationOnce(t *testing.T) {
	startRuntime(t)
	s, ctx := fixture(t)
	ctx = lifecycle.BindRequest(ctx)
	runtime, ok := lifecycle.SpawnerFrom(ctx).(*lifecycle.Runtime)
	if !ok {
		t.Fatal("scheduler request has no originating Runtime")
	}
	seedPublication(t, s, ctx, "automatic-publication")
	var verifications atomic.Int32
	s.Manager.SetClient(clientFunc(func(_ context.Context, r agents.Request) (*agents.Response, error) {
		if strings.HasSuffix(r.URL, "/verify") {
			verifications.Add(1)
			return response(Document{"success": jsonresp.Value(true), "checks": jsonresp.Value([]Check{})}), nil
		}
		return response(hostStatus()), nil
	}))
	idle, e := s.Tick(ctx)
	if e != nil || idle {
		t.Fatal("automatic publication not scheduled", idle, e)
	}
	// Join the actual owner instead of polling report visibility for a fixed
	// wall-clock interval. This also joins final transaction accounting.
	if err := runtime.WaitIdle(ctx); err != nil {
		t.Fatal(err)
	}
	agent, err := s.agent(ctx, "source", "default")
	if err != nil {
		t.Fatal(err)
	}
	stored, ok := runs(agent).Get("automatic-publication")
	if !ok || text(object(stored)["status"]) == "running" {
		t.Fatal("joined publication did not finish", string(stored))
	}
	before := s.Manager.Repository().CommitCount()
	idle, e = s.Tick(ctx)
	if e != nil || !idle || verifications.Load() != 1 || s.Manager.Repository().CommitCount() != before {
		t.Fatal("publication was scheduled repeatedly", idle, e, verifications.Load())
	}
}

func TestSettingsCapabilityPreflightBelongsToWriteOwner(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect", true: "shutdown"}[shutdown], func(t *testing.T) {
			startRuntime(t)
			s, ctx := fixture(t)
			addClient(t, s, ctx)
			entered, gate := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			s.Manager.SetClient(clientFunc(func(ctx context.Context, _ agents.Request) (*agents.Response, error) {
				if calls.Add(1) == 1 {
					close(entered)
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-gate:
					}
				}
				return response(Document{"platform": jsonresp.Value("linux"), "ipv6_client_probe_protocols": jsonresp.Value([]int{1})}), nil
			}))
			bound, cancel := context.WithCancel(lifecycle.BindRequest(ctx))
			defer cancel()
			before := s.Manager.Repository().CommitCount()
			done := make(chan error, 1)
			go func() {
				_, e := s.VerificationSettings(bound, "source", "default", json.RawMessage(`{"client_agent_id":"client","test_url":"https://site.example/"}`))
				done <- e
			}()
			select {
			case <-entered:
			case e := <-done:
				t.Fatalf("preflight exited: %v", e)
			case <-time.After(5 * time.Second):
				t.Fatal("no capability preflight")
			}
			if shutdown {
				lifecycle.Stop(time.Second)
			} else {
				cancel()
			}
			close(gate)
			e := <-done
			a, _ := s.agent(ctx, "source", "default")
			if shutdown {
				if e == nil || settings(a).ClientAgentID != "" || s.Manager.Repository().CommitCount() != before {
					t.Fatal("shutdown saved settings", e)
				}
			} else if e != nil || settings(a).ClientAgentID != "client" || s.Manager.Repository().CommitCount() != before+1 {
				t.Fatal("disconnect interrupted capability/save", e, settings(a))
			}
		})
	}
}

func TestClientStoredOnlineSurvivesAnOldHeartbeat(t *testing.T) {
	s, ctx := fixture(t)
	addClient(t, s, ctx)
	if err := s.update(ctx, "client", func(_ context.Context, a Document, _ []Document) error {
		a["last_heartbeat"] = jsonresp.Value("2000-01-01T00:00:00+00:00")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	source, err := s.agent(ctx, "source", "default")
	if err != nil {
		t.Fatal(err)
	}
	client, err := s.client(ctx, source, "client", false)
	if err != nil || text(client["status"]) != "online" {
		t.Fatalf("stored online client: %v", err)
	}
	result, err := s.VerificationSettings(ctx, "source", "default", nil)
	if err != nil || len(result.Candidates) != 1 || result.Candidates[0].Status != "online" {
		t.Fatalf("candidates: %+v %v", result, err)
	}
}

func TestD2VerificationReportMigration(t *testing.T) {
	s, ctx := fixture(t)
	a, err := s.agent(ctx, "source", "default")
	if err != nil {
		t.Fatal(err)
	}
	_, revision, err := s.snapshot(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	all, err := s.all(ctx)
	if err != nil {
		t.Fatal(err)
	}
	native := s.newRun(ctx, a, &revision, "", all)
	native["status"] = jsonresp.Value("ok")
	native["message"] = jsonresp.Value(msgManualRunning)
	native["checks"] = jsonresp.Value([]Check{check("native_result", "native", "ok", "synthetic")})
	for _, tt := range []struct {
		name   string
		legacy bool
	}{{"native", false}, {"legacy", true}} {
		t.Run(tt.name, func(t *testing.T) {
			report := object(jsonresp.Value(native))
			if tt.legacy {
				for _, key := range []string{"settings_fingerprint", "target_fingerprint", "applied_fingerprint"} {
					report[key] = jsonresp.Value("old-python-fingerprint")
				}
			}
			if err := s.update(ctx, "source", func(_ context.Context, current Document, _ []Document) error {
				current["ipv6_verifications"] = jsonresp.Value(Document{"manual": jsonresp.Value(report)})
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before := s.Manager.Repository().CommitCount()
			result, err := s.Result(ctx, "source", "default", "")
			if err != nil {
				t.Fatal(err)
			}
			actual := object(result.Verification)
			if yes(actual["stale"]) != tt.legacy {
				t.Fatalf("stale result: %s", jsonresp.Value(actual))
			}
			if tt.legacy {
				var checks []Check
				if err := json.Unmarshal(actual["checks"], &checks); err != nil {
					t.Fatal(err)
				}
				want := []string{msgChangedSettings, msgChangedTarget, msgChangedApplied}
				if len(checks) != 1 || strings.Join(checks[0].Details, "|") != strings.Join(want, "|") {
					t.Fatalf("changed inputs: %+v", checks)
				}
			} else if text(actual["status"]) != "ok" || string(actual["checks"]) != string(native["checks"]) {
				t.Fatalf("native result changed: %s", jsonresp.Value(actual))
			}
			if s.Manager.Repository().CommitCount() != before {
				t.Fatal("result GET rewrote stored report")
			}
		})
	}
}
