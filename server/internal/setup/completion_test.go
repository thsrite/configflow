package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	routers "configflow/internal/routers/spec"
)

func installedOwner() *InstallationOwner {
	return &InstallationOwner{Latest: func(context.Context, *repo.Repository, string) (*InstallationState, error) {
		return &InstallationState{ID: "synthetic-installation", Phase: "published", Agents: AgentPair{Mihomo: InstalledAgent{ID: "synthetic-mihomo", Name: "Mihomo", Host: "192.0.2.10", Status: "online"}, OxiDNS: InstalledAgent{ID: "synthetic-oxidns", Name: "OxiDNS", Host: "192.0.2.11", Status: "online"}}}, nil
	}, HasInstallation: func(context.Context, *repo.Repository, string) (bool, error) { return false, nil }, GatewayStatus: func(context.Context, *repo.Repository, string, string) (GatewayStatus, error) {
		return GatewayStatus{IPv4Forwarding: &ForwardingStatus{Current: "1", Persisted: pointer(true)}, TUN: &TUNStatus{Configured: pointer(true), Present: pointer(true)}, Managed: ManagedStatus{Active: true, Healthy: pointer(true)}}, nil
	}}
}
func TestCoreFingerprintMatchesPython(t *testing.T) {
	raw := json.RawMessage(`{"proxy_groups":[],"mihomo":{"custom_config":"dns: {}\n"},"rule_configs":[{"id":"synthetic","number":1234567890123456789}],"system_config":{"future":"中文"},"_revision":123,"routers":{"items":[]}}`)
	want := pythonSetup(t, `import json,sys;from backend.common.setup_completion import core_fingerprint;json.dump(core_fingerprint(json.load(sys.stdin)),sys.stdout)`, raw)
	got, err := CoreFingerprint(raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(got)
	equalJSON(t, encoded, want)
}
func TestCompletionChecksAndPublicationContentGuards(t *testing.T) {
	s, ctx := preparedFixture(t, "gateway")
	body := json.RawMessage(`{"domain_rules":{"direct":[],"proxy":[]},"local_dns":"https://192.0.2.53/dns-query"}`)
	if _, err := s.Prepare(ctx, testSessionID, body, "http://localhost"); err != nil {
		t.Fatal(err)
	}
	path, _ := storeFor(s.repository).path(testSessionID)
	before, _ := os.ReadFile(path)
	commits := s.repository.CommitCount()
	if _, err := s.Action(ctx, testSessionID, "check", json.RawMessage(`{}`), ""); err == nil || err.Error() != msgAwaitOwner {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) || s.repository.CommitCount() != commits {
		t.Fatal("missing owner committed")
	}
	s.owners.Installation = installedOwner()
	if _, err := s.Action(ctx, testSessionID, "check", json.RawMessage(`{}`), ""); err != nil {
		t.Fatal(err)
	}
	record, _ := storeFor(s.repository).read(ctx, testSessionID)
	if record.Step != 4 {
		t.Fatal("check did not advance")
	}
	for _, body := range []string{`{"make_default":1}`, `{"make_default":null}`} {
		if _, err := s.Action(ctx, testSessionID, "finish", json.RawMessage(body), ""); err == nil || err.Error() != msgMakeDefault {
			t.Fatal(err)
		}
	}
	// Receipts and router edits may increase revision without requiring republish.
	s.owners.Installation.Latest = func(context.Context, *repo.Repository, string) (*InstallationState, error) {
		owner := installedOwner()
		state, _ := owner.Latest(ctx, s.repository, record.ProfileID)
		state.ConfigChanged = true
		return state, nil
	}
	if _, err := s.Action(ctx, testSessionID, "finish", json.RawMessage(`{"make_default":true}`), ""); err != nil {
		t.Fatal(err)
	}
	record, _ = storeFor(s.repository).read(ctx, testSessionID)
	defaultID, _ := s.repository.DefaultProfileID(ctx)
	if !enabled(record.Completed) || record.MaxStep != 5 || defaultID != record.ProfileID || text(record.PreviousDefault) != "default" {
		t.Fatal("finish did not preserve default choice")
	}
	_, err := s.repository.UpdateProfile(ctx, record.ProfileID, func(_ context.Context, p *model.Profile) error {
		oxRaw, err := p.OxiDNS.MarshalJSON()
		if err != nil {
			return err
		}
		var ox map[string]any
		if err := json.Unmarshal(oxRaw, &ox); err != nil {
			return err
		}
		ox["query_recorder"].(map[string]any)["enabled"] = false
		raw, err := json.Marshal(ox)
		if err != nil {
			return err
		}
		return p.SetJSON("oxidns", raw)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Action(ctx, testSessionID, "check", json.RawMessage(`{}`), ""); err == nil || err.Error() != msgNeedPublish {
		t.Fatal("changed published content accepted", err)
	}
}
func TestFullTUNStatusFailures(t *testing.T) {
	valid := func() GatewayStatus {
		return GatewayStatus{IPv4Forwarding: &ForwardingStatus{Current: "1", Persisted: pointer(true)}, TUN: &TUNStatus{Configured: pointer(true), Present: pointer(true)}}
	}
	tests := []struct {
		name, message string
		change        func(*GatewayStatus)
	}{
		{"old-agent", msgOldAgent, func(s *GatewayStatus) { s.TUN = nil }},
		{"forwarding", msgForwardingOff, func(s *GatewayStatus) { s.IPv4Forwarding.Current = "0" }},
		{"persist", msgForwardingNotPersisted, func(s *GatewayStatus) { s.IPv4Forwarding.Persisted = pointer(false) }},
		{"managed", msgGatewayRemaining, func(s *GatewayStatus) { s.Managed.Pending = true }},
		{"gateway", msgPublishedGateway, func(s *GatewayStatus) { s.TUN.GatewayMarker = true }},
		{"configured", msgTUNNotConfigured, func(s *GatewayStatus) { s.TUN.Configured = pointer(false) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status := valid()
			test.change(&status)
			err := checkFullTUN(status)
			if err == nil || err.Error() != test.message {
				t.Fatal(err)
			}
		})
	}
	if err := checkFullTUN(valid()); err != nil {
		t.Fatal(err)
	}
}
func TestRouteWarmupBudgetAndOriginShutdown(t *testing.T) {
	s, base := serviceFixture(t, "router")
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: 2 * time.Second})
	runtime.Start(t.Context())
	defer runtime.Shutdown(t.Context())
	s.spawner = runtime
	s.routeBudget = 40 * time.Millisecond
	origin := lifecycle.Detach(runtime.BindRequest(base))
	var network, offline atomic.Int32
	entered := make(chan struct{}, 1)
	stopped := make(chan struct{}, 1)
	s.owners.ReadRouteContent = func(ctx context.Context, _ json.RawMessage, _ routers.RuleSet, _ *routers.LibraryRule, opts RouteReadOptions) (string, error) {
		if !opts.AllowNetwork {
			offline.Add(1)
			return "", nil
		}
		network.Add(1)
		if opts.ConnectTimeout > s.routeBudget || opts.ReadTimeout > s.routeBudget {
			t.Error("timeouts exceeded total budget")
		}
		entered <- struct{}{}
		<-ctx.Done()
		stopped <- struct{}{}
		return "", ctx.Err()
	}
	missing := []string{}
	load := s.routeLoader(origin, json.RawMessage(`{}`), &missing)
	if text, err := load(routers.RuleSet{ID: "synthetic"}, nil); err != nil || text != "" {
		t.Fatal(err)
	}
	<-entered
	<-stopped
	if _, err := load(routers.RuleSet{ID: "other"}, nil); err != nil {
		t.Fatal(err)
	}
	if network.Load() != 1 || offline.Load() != 1 || len(missing) != 2 {
		t.Fatal("budget allowed another network request")
	}
	// A real shutdown cancels the generation retained by the worker.
	s.routeBudget = time.Second
	stopped = make(chan struct{}, 1)
	done := make(chan error, 1)
	if err := runtime.Spawn(origin, "synthetic-check", func(job context.Context) {
		missing := []string{}
		_, err := s.routeLoader(origin, json.RawMessage(`{}`), &missing)(routers.RuleSet{ID: "shutdown"}, nil)
		done <- err
	}); err != nil {
		t.Fatal(err)
	}
	<-entered
	runtime.Shutdown(t.Context())
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("worker escaped origin shutdown", err)
	}
	<-stopped
}

type canceledRouteJobSpawner struct{}

func (canceledRouteJobSpawner) Spawn(origin context.Context, _ string, run func(context.Context)) error {
	job, cancel := context.WithCancel(origin)
	cancel()
	run(job)
	return nil
}

func TestRouteLoaderPropagatesJobCancellationBeforeOrigin(t *testing.T) {
	origin := t.Context()
	s := Service{
		spawner: canceledRouteJobSpawner{},
		owners: ExternalOwners{
			ReadRouteContent: func(ctx context.Context, _ json.RawMessage, _ routers.RuleSet, _ *routers.LibraryRule, _ RouteReadOptions) (string, error) {
				<-ctx.Done()
				return "", ctx.Err()
			},
		},
	}
	var missing []string
	load := s.routeLoader(origin, nil, &missing)
	content, err := load(routers.RuleSet{ID: "shutdown"}, nil)
	if origin.Err() != nil {
		t.Fatalf("origin canceled before stopDetached: %v", origin.Err())
	}
	if content != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("routeLoader returned (%q, %v), want empty content and context.Canceled", content, err)
	}
}

func TestRouterDraftAttachmentAndManualManagedFinish(t *testing.T) {
	s, ctx := preparedFixture(t, "router")
	body := json.RawMessage(`{"domain_rules":{"direct":[],"proxy":[]},"local_dns":"https://192.0.2.53/dns-query"}`)
	if _, err := s.Prepare(ctx, testSessionID, body, "http://localhost"); err != nil {
		t.Fatal(err)
	}
	s.owners.Installation = installedOwner()
	s.owners.Installation.GatewayStatus = func(context.Context, *repo.Repository, string, string) (GatewayStatus, error) {
		return GatewayStatus{IPv4Forwarding: &ForwardingStatus{Current: "1", Persisted: pointer(true)}, TUN: &TUNStatus{Configured: pointer(true), Present: pointer(true)}}, nil
	}
	s.owners.ReadRouteContent = func(context.Context, json.RawMessage, routers.RuleSet, *routers.LibraryRule, RouteReadOptions) (string, error) {
		t.Fatal("empty marker unexpectedly loaded rule content")
		return "", nil
	}
	if _, err := s.SaveRouterDraft(ctx, testSessionID, json.RawMessage(`{"router":{"name":"Synthetic router","type":"ikuai","endpoint":"http://192.0.2.1","username":"operator","password":"synthetic-router-private"}}`)); err != nil {
		t.Fatal(err)
	}
	record, _ := storeFor(s.repository).read(ctx, testSessionID)
	var draft routers.Router
	json.Unmarshal(record.RouterDraft, &draft)
	for range 2 {
		if _, err := s.Action(ctx, testSessionID, "check", json.RawMessage(`{}`), ""); err != nil {
			t.Fatal(err)
		}
	}
	record, _ = storeFor(s.repository).read(ctx, testSessionID)
	profile, _ := s.repository.Profile(ctx, record.ProfileID)
	raw, _ := json.Marshal(profile)
	attached, err := findRouter(raw, draft.ID)
	if err != nil || attached == nil || attached.ProxyGatewayAgentID != "synthetic-mihomo" || attached.DNSAgentID != "synthetic-oxidns" || len(record.RouterDraft) != 0 || len(profile.Routers.Items) != 1 {
		t.Fatal("draft did not attach idempotently", err)
	}
	if _, err := s.Action(ctx, testSessionID, "finish", json.RawMessage(`{"router_mode":"manual"}`), ""); err == nil || err.Error() != msgAcknowledgeRoutes {
		t.Fatal(err)
	}
	if _, err := s.Action(ctx, testSessionID, "finish", json.RawMessage(`{"router_mode":"manual","ack_routes":true,"ack_dns":true}`), ""); err != nil {
		t.Fatal(err)
	}
	finish, _ := json.Marshal(map[string]string{"router_mode": "managed", "router_id": draft.ID})
	if _, err := s.Action(ctx, testSessionID, "finish", finish, ""); err == nil || err.Error() != msgAwaitOwner {
		t.Fatal("unregistered inspection owner succeeded", err)
	}
	deviceFailure := &Error{Message: "synthetic device failure", Status: 502}
	refusals := []struct {
		name    string
		result  RouterVerification
		err     error
		message string
		status  int
	}{
		{name: "unsynced", result: RouterVerification{Empty: false}, message: msgRouterUnsynced, status: 409},
		{name: "blockers", result: RouterVerification{Empty: true, Blockers: []string{"synthetic blocker"}}, message: msgRouterUnsynced, status: 409},
		{name: "device failure", err: deviceFailure, message: deviceFailure.Message, status: 502},
	}
	for _, tt := range refusals {
		t.Run(tt.name, func(t *testing.T) {
			s.owners.VerifyRouter = func(_ context.Context, _ *repo.Repository, profileID string, _ json.RawMessage, router *routers.Router) (RouterVerification, error) {
				if profileID != record.ProfileID || router == nil || router.ID != draft.ID {
					t.Errorf("verification got profile %q router %v", profileID, router)
				}
				return tt.result, tt.err
			}
			_, err := s.Action(ctx, testSessionID, "finish", finish, "")
			var failure *Error
			if !errors.As(err, &failure) || failure.Message != tt.message || failure.Status != tt.status {
				t.Fatalf("finish = %v, want %d %q", err, tt.status, tt.message)
			}
			after, _ := storeFor(s.repository).read(ctx, testSessionID)
			if text(after.RouterVerification) == "verified" {
				t.Fatal("refused verification was recorded")
			}
		})
	}
	s.owners.VerifyRouter = func(context.Context, *repo.Repository, string, json.RawMessage, *routers.Router) (RouterVerification, error) {
		return RouterVerification{Empty: true}, nil
	}
	if _, err := s.Action(ctx, testSessionID, "finish", finish, ""); err != nil {
		t.Fatal(err)
	}
	record, _ = storeFor(s.repository).read(ctx, testSessionID)
	if text(record.RouterVerification) != "verified" {
		t.Fatal("managed completion did not record proof")
	}
}

func TestCheckPreservesAbandonedSessionAndRequiresFreshRouteAcknowledgement(t *testing.T) {
	s, ctx := preparedFixture(t, "router")
	body := json.RawMessage(`{"domain_rules":{"direct":[],"proxy":[]},"local_dns":"https://192.0.2.53/dns-query"}`)
	if _, err := s.Prepare(ctx, testSessionID, body, "http://localhost"); err != nil {
		t.Fatal(err)
	}
	s.owners.Installation = installedOwner()
	s.owners.Installation.GatewayStatus = func(context.Context, *repo.Repository, string, string) (GatewayStatus, error) {
		return GatewayStatus{IPv4Forwarding: &ForwardingStatus{Current: "1", Persisted: pointer(true)}, TUN: &TUNStatus{Configured: pointer(true), Present: pointer(true)}}, nil
	}
	s.owners.ReadRouteContent = func(context.Context, json.RawMessage, routers.RuleSet, *routers.LibraryRule, RouteReadOptions) (string, error) {
		return "", nil
	}
	store := storeFor(s.repository)
	record, _ := store.read(ctx, testSessionID)
	record.Abandoned = pointer(true)
	record.RouteInstructions = json.RawMessage(`{"missing_rule_sets":["Synthetic networks"]}`)
	if err := store.write(ctx, record); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Action(ctx, testSessionID, "finish", json.RawMessage(`{"router_mode":"manual","ack_routes":true,"ack_dns":true}`), ""); err == nil || err.Error() != fmt.Sprintf(msgNewRoutes, "Synthetic networks") {
		t.Fatal("new route table completed without acknowledgement", err)
	}
	if _, err := s.Action(ctx, testSessionID, "check", json.RawMessage(`{}`), ""); err != nil {
		t.Fatal(err)
	}
	record, _ = store.read(ctx, testSessionID)
	if !enabled(record.Abandoned) {
		t.Fatal("automatic check reopened abandoned wizard")
	}
	if _, err := s.Action(ctx, testSessionID, "finish", json.RawMessage(`{"router_mode":"","ack_routes":true,"ack_dns":true}`), ""); err == nil || err.Error() != msgFinishRouterMode {
		t.Fatal("explicit empty router mode became default", err)
	}
	if _, err := s.Action(ctx, testSessionID, "finish", json.RawMessage(`{"router_mode":"manual","ack_routes":true,"ack_dns":true}`), ""); err != nil {
		t.Fatal(err)
	}
}
