package rulesync

import (
	"bytes"
	"configflow/internal/logx"
	"configflow/internal/taskstatus"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/redact"
	"configflow/internal/repo"
)

func fixtureService(t *testing.T) (context.Context, *Service) {
	t.Helper()
	r, e := repo.OpenDefault(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	ctx := repo.WithRepository(context.Background(), r)
	e = r.UpdateSystem(ctx, func(_ context.Context, sys *model.System) error {
		enabled := true
		sys.Agents = []model.Agent{{
			ID: "device", Name: "Synthetic", Host: "127.0.0.1", Port: 18080,
			Token: "synthetic-rule-token", ProfileID: "default", ServiceType: "mihomo",
			Version: MinAgentVersion, Enabled: &enabled,
		}}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	m, e := agents.New(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(m.Close)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return ctx, New(m, nil, Options{Now: func() time.Time { return now }})
}
func systemBytes(t *testing.T, s *Service) []byte {
	t.Helper()
	state, e := s.m.Repository().Store().Load()
	if e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(state)
	if e != nil {
		t.Fatal(e)
	}
	return raw
}
func TestConfigureAndStateCommitBoundaries(t *testing.T) {
	ctx, s := fixtureService(t)
	view, e := s.Configure(ctx, "device", []byte(`{"library":true,"rules":false}`))
	if e != nil || !view.Settings.Library || view.State.Status != "pending" {
		t.Fatalf("configure %+v %v", view, e)
	}
	record, e := s.m.ReadAgent(ctx, "device", false)
	if e != nil {
		t.Fatal(e)
	}
	if AgentSettings(record) != (Settings{Library: true}) {
		t.Fatal("stored settings")
	}
	before := string(systemBytes(t, s))
	for _, body := range []string{`null`, `[]`, `{}`, `{"library":1,"rules":false}`} {
		if _, e = s.Configure(ctx, "device", []byte(body)); e == nil {
			t.Fatalf("accepted %s", body)
		}
		if string(systemBytes(t, s)) != before {
			t.Fatal("invalid body wrote database")
		}
	}
	if e = s.saveState(ctx, "device", model.RuleSyncState{Status: "failed", Message: "URL https://user:private-password@example.com/path?token=synthetic-rule-token"}); e != nil {
		t.Fatal(e)
	}
	view, e = s.Status(ctx, "device")
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(view.State.Message, "private-password") || strings.Contains(view.State.Message, "synthetic-rule-token") {
		t.Fatal("public secret leaked")
	}
	raw, e := os.ReadFile(filepath.Join(s.m.Repository().DataDir(), "task-status.json"))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(raw), "private-password") || strings.Contains(string(raw), "synthetic-rule-token") {
		t.Fatal("task secret leaked")
	}
	old := string(systemBytes(t, s))
	if e = s.saveState(ctx, "device", model.RuleSyncState{Status: "failed", Message: view.State.Message}); e != nil {
		t.Fatal(e)
	}
	if old != string(systemBytes(t, s)) {
		t.Fatal("identical state wrote again")
	}
}
func TestRealRuntimeShutdownAndDisconnect(t *testing.T) {
	ctx, s := fixtureService(t)
	lifecycle.Stop(3 * time.Second)
	if e := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); e != nil {
		t.Fatal(e)
	}
	lifecycle.Start(ctx)
	t.Cleanup(func() {
		lifecycle.Stop(3 * time.Second)
		_ = lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	})
	bound := lifecycle.BindRequest(ctx)
	lifecycle.Stop(3 * time.Second)
	before := string(systemBytes(t, s))
	checks := []struct {
		name string
		run  func() error
	}{
		{"configure", func() error { _, e := s.Configure(bound, "device", []byte(`{"library":true,"rules":true}`)); return e }},
		{"state", func() error {
			return s.saveState(lifecycle.Detach(bound), "device", model.RuleSyncState{Status: "failed", Message: msgFailed})
		}},
		{"worker", func() error { return s.Run(bound, "device") }},
		{"candidate", func() error {
			_, e := PrepareCandidate(bound, deploy.Target{ID: "device"}, nil, Baseline{}, Settings{}, "closed", false)
			return e
		}},
	}
	for _, tt := range checks {
		t.Run(tt.name, func(t *testing.T) {
			if e := tt.run(); !errors.Is(e, context.Canceled) {
				t.Fatalf("got %v", e)
			}
			if string(systemBytes(t, s)) != before {
				t.Fatal("shutdown committed")
			}
		})
	}
	lifecycle.Start(ctx)
	request, cancel := context.WithCancel(lifecycle.BindRequest(ctx))
	cancel()
	view, e := s.Configure(request, "device", []byte(`{"library":true,"rules":true}`))
	if e != nil || view.Settings != (Settings{true, true}) {
		t.Fatalf("disconnected accepted write %+v %v", view, e)
	}
}
func TestHASettingsPairAtomicity(t *testing.T) {
	ctx, s := fixtureService(t)
	e := s.m.Repository().UpdateSystem(ctx, func(_ context.Context, sys *model.System) error {
		sys.Agents[0].ServiceType = "oxidns"
		if err := sys.Agents[0].SetJSON("oxidns_ha", []byte(`{"enabled":true,"peer_id":"peer","group_id":"group","role":"primary"}`)); err != nil {
			return err
		}
		raw, err := json.Marshal(sys.Agents[0])
		if err != nil {
			return err
		}
		var b model.Agent
		if err := json.Unmarshal(raw, &b); err != nil {
			return err
		}
		b.ID = "peer"
		if err := b.SetJSON("oxidns_ha", []byte(`{"enabled":true,"peer_id":"device","group_id":"group","role":"secondary"}`)); err != nil {
			return err
		}
		sys.Agents = append(sys.Agents, b)
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	pair, e := s.m.ReadHAPair(ctx, "device")
	if e != nil || len(pair) != 2 {
		t.Fatal(pair, e)
	}
	_, e = s.Configure(ctx, "device", []byte(`{"library":true,"rules":false}`))
	if e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{"device", "peer"} {
		a, e := s.m.ReadAgent(ctx, id, false)
		if e != nil || AgentSettings(a) != (Settings{Library: true}) {
			t.Fatal("pair settings", e)
		}
	}

	e = s.m.Repository().UpdateSystem(ctx, func(_ context.Context, sys *model.System) error {
		return sys.Agents[0].SetJSON(agents.HAField, []byte(`{"status":"preparing","enable":{"peer_id":"peer"}}`))
	})
	if e != nil {
		t.Fatal(e)
	}
	pairPtrs := make([]*model.Agent, len(pair))
	for i := range pair {
		pairPtrs[i] = &pair[i]
	}
	beforeEnable := systemBytes(t, s)
	if e = configurePair(ctx, s.m, pairPtrs, Settings{Rules: true}); e == nil || e.Error() != msgPairEnabling {
		t.Fatalf("concurrent enable guard: %v", e)
	}
	if e = persistSync(ctx, s.m, "device", &Settings{Rules: true}, nil); e == nil || e.Error() != msgPairEnabling {
		t.Fatalf("single settings enable guard: %v", e)
	}
	if !bytes.Equal(beforeEnable, systemBytes(t, s)) {
		t.Fatal("enable race overwrote settings")
	}
	e = s.m.Repository().UpdateSystem(ctx, func(_ context.Context, sys *model.System) error {
		return sys.Agents[0].Clear(agents.HAField)
	})
	if e != nil {
		t.Fatal(e)
	}
	// A changed relationship cannot be overwritten by an already captured pair.
	e = s.m.Repository().UpdateSystem(ctx, func(_ context.Context, sys *model.System) error {
		sys.Agents[1].OxiDNSHA.GroupID = "other"
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	before := systemBytes(t, s)
	if e = configurePair(ctx, s.m, pairPtrs, Settings{Rules: true}); e == nil {
		t.Fatal("changed pair accepted")
	}
	if !reflect.DeepEqual(before, systemBytes(t, s)) {
		t.Fatal("partial pair write")
	}
}

func TestTaskRowAndPinnedWithheld(t *testing.T) {
	f := newWorkerFixture(t, "oxidns")
	ctx := f.ctx
	if e := f.s.saveState(ctx, "device", model.RuleSyncState{Status: "blocked", Message: msgBaseline}); e != nil {
		t.Fatal(e)
	}
	record, e := f.s.m.ReadAgent(ctx, "device", false)
	if e != nil {
		t.Fatal(e)
	}
	row, e := agentTaskRow(ctx, record, taskstatus.Snapshot{})
	if e != nil {
		t.Fatal(e)
	}
	if row == nil || row.Key != "agent_rule_sync:device" || row.LastStatus == nil || *row.LastStatus != "skipped" || row.LastError == nil || *row.LastError != msgBaseline {
		t.Fatal(row)
	}
	_, e = f.s.m.Repository().UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		raw, err := json.Marshal(p.OxiDNS)
		if err != nil {
			return err
		}
		var o map[string]any
		if err := json.Unmarshal(raw, &o); err != nil {
			return err
		}
		o["rule_actions"] = map[string]any{"set-a": map[string]any{"action": "upstream", "source": "local", "upstream": map[string]any{"addr": "udp://192.0.2.99"}}}
		updated, err := json.Marshal(o)
		if err != nil {
			return err
		}
		return p.SetJSON("oxidns", updated)
	})
	if e != nil {
		t.Fatal(e)
	}
	held, e := PinnedWithheld(ctx, f.s.m.Repository(), f.s.receipts, "default", []string{"set-a"})
	if e != nil || !held {
		t.Fatalf("held %v %v", held, e)
	}
	held, e = PinnedWithheld(ctx, f.s.m.Repository(), f.s.receipts, "default", []string{"single"})
	if e != nil || held {
		t.Fatalf("unrelated held %v %v", held, e)
	}
	if _, e = f.s.Configure(ctx, "device", []byte(`{"library":false,"rules":false}`)); e != nil {
		t.Fatal(e)
	}
	held, e = PinnedWithheld(ctx, f.s.m.Repository(), f.s.receipts, "default", []string{"set-a"})
	if e != nil || held {
		t.Fatal("disabled scopes still withheld", e)
	}
}

func TestRoundAdmissionRotationAndDrain(t *testing.T) {
	ctx, s := fixtureService(t)
	e := s.m.Repository().UpdateSystem(ctx, func(_ context.Context, sys *model.System) error {
		if err := sys.Agents[0].SetJSON("rule_sync", []byte(`{"settings":{"library":true,"rules":false}}`)); err != nil {
			return err
		}
		raw, err := json.Marshal(sys.Agents[0])
		if err != nil {
			return err
		}
		for i := 1; i < 5; i++ {
			var record model.Agent
			if err := json.Unmarshal(raw, &record); err != nil {
				return err
			}
			record.ID = fmt.Sprintf("device-%d", i)
			sys.Agents = append(sys.Agents, record)
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	lifecycle.Stop(20 * time.Second)
	if e = lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		lifecycle.Stop(20 * time.Second)
		_ = lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	})
	for round, want := range []int{2, 4, 5} {
		lifecycle.Start(ctx)
		if e = s.Round(lifecycle.BindRequest(ctx)); e != nil {
			t.Fatal(e)
		}
		lifecycle.Stop(20 * time.Second)
		records, e := s.m.ReadAgents(ctx, false, nil)
		if e != nil {
			t.Fatal(e)
		}
		changed := 0
		for i := range records {
			if stateOf(&records[i]).Message == msgOffline {
				changed++
			}
		}
		if changed != want {
			t.Fatalf("round %d touched %d want %d", round, changed, want)
		}
		s.mu.Lock()
		if len(s.pending) > 2 {
			t.Error("more than two admitted jobs")
		}
		for id, running := range s.pending {
			if running {
				t.Errorf("worker %s leaked through drain", id)
			}
		}
		s.mu.Unlock()
	}
}

func TestPresenceUsesSharedCredentialRedaction(t *testing.T) {
	ctx, s := fixtureService(t)
	var logs bytes.Buffer
	root, e := logx.Setup(logx.Config{Level: logx.Info, Console: &logs})
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	record := agentModelForTest(t, map[string]any{"id": "a16-secret-presence", "name": "alice https://uri-user:presence-password@example.com/?token=synthetic-rule-token", "token": "synthetic-rule-token", "status": "online", "last_heartbeat": "2026-01-02T03:04:05Z"})
	observePresence(ctx, s.m, []*model.Agent{record})
	record.Status = "offline"
	observePresence(ctx, s.m, []*model.Agent{record})
	output := logs.String()
	if !strings.Contains(output, "已离线") || !strings.Contains(output, "alice") {
		t.Fatal("presence transition/username missing", output)
	}
	if strings.Contains(output, "presence-password") || strings.Contains(output, "synthetic-rule-token") {
		t.Fatal("presence credential leak", output)
	}
	if record.Name != "alice https://uri-user:presence-password@example.com/?token=synthetic-rule-token" {
		t.Fatal("presence mutated registry")
	}
	// N8's shared redactor returns native JSON; the adapter must still handle
	// historical non-string metadata without exposing nested credentials.
	logs.Reset()
	nested := agentModelForTest(t, map[string]any{"id": "a16-nested-presence", "name": map[string]any{"label": "alice", "token": "synthetic-rule-token"}, "token": "synthetic-rule-token", "status": "online"})
	before, e := json.Marshal(nested)
	if e != nil {
		t.Fatal(e)
	}
	observePresence(ctx, s.m, []*model.Agent{nested})
	nested.Status = "offline"
	observePresence(ctx, s.m, []*model.Agent{nested})
	if output := logs.String(); !strings.Contains(output, "已离线") || !strings.Contains(output, "alice") || strings.Contains(output, "synthetic-rule-token") {
		t.Fatal("nested presence metadata was lost or leaked", output)
	}
	nested.Status = "online"
	after, e := json.Marshal(nested)
	if e != nil || !bytes.Equal(before, after) {
		t.Fatal("nested presence metadata mutated registry", e)
	}
}

func TestProfileFilteringKeepsLiteralDefaultAndDisabledAgents(t *testing.T) {
	ctx, s := fixtureService(t)
	r := s.m.Repository()
	if _, e := r.CreateProfileRecord(ctx, repo.ProfileCreation{ID: "other", Name: "Other"}, ""); e != nil {
		t.Fatal(e)
	}
	if _, e := r.SetDefaultProfile(ctx, "other"); e != nil {
		t.Fatal(e)
	}
	e := s.m.Repository().UpdateSystem(ctx, func(_ context.Context, sys *model.System) error {
		raw, err := json.Marshal(sys.Agents[0])
		if err != nil {
			return err
		}
		var unbound, disabled model.Agent
		if err := json.Unmarshal(raw, &unbound); err != nil {
			return err
		}
		unbound.ID = "other-device"
		unbound.ProfileID = "other"
		if err := json.Unmarshal(raw, &disabled); err != nil {
			return err
		}
		disabled.ID = "disabled"
		off := false
		disabled.Enabled = &off
		sys.Agents = append(sys.Agents, unbound, disabled)
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	for _, tt := range []struct{ profile, want string }{{"default", "device"}, {"other", "other-device"}, {"", "other-device"}} {
		records, e := profileAgents(ctx, r, tt.profile)
		if e != nil {
			t.Fatal(e)
		}
		if len(records) != 1 || recordID(records[0]) != tt.want {
			t.Fatalf("profile %s matched %v want %s", tt.profile, records, tt.want)
		}
	}
}

func TestAgentSettingsStrictBooleans(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want Settings
	}{
		{"strict booleans", `{"id":"device","rule_sync":{"settings":{"library":true,"rules":false}}}`, Settings{Library: true}},
		{"historical wrong type", `{"id":"device","rule_sync":{"settings":{"library":true,"rules":"true"}}}`, Settings{Library: true}},
		{"null", `{"id":"device","rule_sync":{"settings":{"library":null,"rules":null}}}`, Settings{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var record model.Agent
			if err := json.Unmarshal([]byte(tt.raw), &record); err != nil {
				t.Fatal(err)
			}
			if got := AgentSettings(&record); got != tt.want {
				t.Fatalf("%+v want %+v", got, tt.want)
			}
		})
	}
}

func TestTypedRuleSyncSecretsUseSystemConfig(t *testing.T) {
	ctx, s := fixtureService(t)
	const proxy = "http://synthetic-user:synthetic-proxy-password@proxy.example.com:1080"
	if err := s.m.Repository().UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		system.SystemConfig.RuleFetchProxy = proxy
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	record, err := s.m.ReadAgent(ctx, "device", false)
	if err != nil {
		t.Fatal(err)
	}
	sys, err := s.m.Repository().System(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settingsRaw, err := json.Marshal(sys.SystemConfig)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := redact.ExternalPayloadSecrets(ctx, json.RawMessage(settingsRaw))
	if err != nil {
		t.Fatal(err)
	}
	actual, err := systemSecrets(ctx, s.m)
	if err != nil || !actual.Equal(expected) {
		t.Fatalf("native system secret projection changed: %v", err)
	}
	text := "prefix " + proxy + " suffix"
	if got, want := safeText(ctx, s.m, record, text), safeDisplay(text, expected); got != want {
		t.Fatalf("native redaction changed: %q vs %q", got, want)
	}
}
