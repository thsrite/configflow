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
	"slices"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/jsonx"
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
	_, e = r.UpdateSystemTransaction(ctx, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.Set("agents", []any{jsonx.Obj("id", "device", "name", "Synthetic", "host", "127.0.0.1", "port", 18080, "token", "synthetic-rule-token", "profile_id", "default", "service_type", "mihomo", "version", MinAgentVersion, "enabled", true)})
		return nil, nil
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
	record, e := s.m.GetAgentByID(ctx, "device", false)
	if e != nil {
		t.Fatal(e)
	}
	if SettingsFor(record) != (Settings{Library: true}) {
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
	_, e := s.m.UpdateAgents(ctx, "", func(_ context.Context, list *[]any) (any, error) {
		a := (*list)[0].(*jsonx.Object)
		a.Set("service_type", "oxidns")
		a.Set("oxidns_ha", jsonx.Obj("enabled", true, "peer_id", "peer", "group_id", "group", "role", "primary"))
		b := a.DeepCopy()
		b.Set("id", "peer")
		b.GetObject("oxidns_ha").Set("peer_id", "device")
		b.GetObject("oxidns_ha").Set("role", "secondary")
		*list = append(*list, b)
		return nil, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	pair, e := s.m.HAPair(ctx, "device")
	if e != nil || len(pair) != 2 {
		t.Fatal(pair, e)
	}
	_, e = s.Configure(ctx, "device", []byte(`{"library":true,"rules":false}`))
	if e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{"device", "peer"} {
		a, e := s.m.GetAgentByID(ctx, id, false)
		if e != nil || SettingsFor(a) != (Settings{Library: true}) {
			t.Fatal("pair settings", e)
		}
	}

	_, e = s.m.UpdateAgents(ctx, "", func(_ context.Context, list *[]any) (any, error) {
		(*list)[0].(*jsonx.Object).Set(agents.HAField, jsonx.Obj("status", "preparing", "enable", jsonx.Obj("peer_id", "peer")))
		return nil, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	beforeEnable := systemBytes(t, s)
	if e = configureLegacyPair(ctx, s.m, pair, Settings{Rules: true}); e == nil || e.Error() != msgPairEnabling {
		t.Fatalf("concurrent enable guard: %v", e)
	}
	if e = persistSync(ctx, s.m, "device", &Settings{Rules: true}, nil); e == nil || e.Error() != msgPairEnabling {
		t.Fatalf("single settings enable guard: %v", e)
	}
	if !bytes.Equal(beforeEnable, systemBytes(t, s)) {
		t.Fatal("enable race overwrote settings")
	}
	_, e = s.m.UpdateAgents(ctx, "", func(_ context.Context, list *[]any) (any, error) {
		(*list)[0].(*jsonx.Object).Delete(agents.HAField)
		return nil, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	// A changed relationship cannot be overwritten by an already captured pair.
	_, e = s.m.UpdateAgents(ctx, "", func(_ context.Context, list *[]any) (any, error) {
		(*list)[1].(*jsonx.Object).GetObject("oxidns_ha").Set("group_id", "other")
		return nil, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	before := systemBytes(t, s)
	if e = configureLegacyPair(ctx, s.m, pair, Settings{Rules: true}); e == nil {
		t.Fatal("changed pair accepted")
	}
	if !reflect.DeepEqual(before, systemBytes(t, s)) {
		t.Fatal("partial pair write")
	}
}

func TestTaskRowAndPinnedWithheld(t *testing.T) {
	f := newWorkerFixture(t, "oxidns")
	ctx := f.ctx
	record, e := f.s.m.GetAgentByID(ctx, "device", false)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.saveState(ctx, "device", model.RuleSyncState{Status: "blocked", Message: msgBaseline}); e != nil {
		t.Fatal(e)
	}
	record, e = f.s.m.GetAgentByID(ctx, "device", false)
	if e != nil {
		t.Fatal(e)
	}
	row, e := taskRow(ctx, record, jsonx.NewObject())
	if e != nil {
		t.Fatal(e)
	}
	if row == nil || row.GetString("key") != "agent_rule_sync:device" || row.GetString("last_status") != "skipped" || row.GetString("last_error") != msgBaseline {
		t.Fatal(row)
	}
	_, _, e = f.s.m.Repository().UpdateProfileTransaction(ctx, "default", func(_ context.Context, doc *jsonx.Object) (*jsonx.Object, error) {
		doc.GetObject("oxidns").Set("rule_actions", jsonx.Obj("set-a", jsonx.Obj("action", "upstream", "source", "local", "upstream", jsonx.Obj("addr", "udp://192.0.2.99"))))
		return nil, nil
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
	_, e := s.m.UpdateAgents(ctx, "", func(_ context.Context, list *[]any) (any, error) {
		first := (*list)[0].(*jsonx.Object)
		first.Set("rule_sync", jsonx.Obj("settings", jsonx.Obj("library", true, "rules", false)))
		for i := 1; i < 5; i++ {
			record := first.DeepCopy()
			record.Set("id", fmt.Sprintf("device-%d", i))
			*list = append(*list, record)
		}
		return nil, nil
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
		records, e := s.m.GetAllAgents(ctx, false)
		if e != nil {
			t.Fatal(e)
		}
		changed := 0
		for _, r := range records {
			a, e := decodeAgent(r)
			if e != nil {
				t.Fatal(e)
			}
			if stateOf(a).Message == msgOffline {
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
	record := jsonx.Obj("id", "a16-secret-presence", "name", "alice https://uri-user:presence-password@example.com/?token=synthetic-rule-token", "token", "synthetic-rule-token", "status", "online", "last_heartbeat", "2026-01-02T03:04:05Z")
	observeLegacyPresence(ctx, s.m, []*agents.Record{record})
	record.Set("status", "offline")
	observeLegacyPresence(ctx, s.m, []*agents.Record{record})
	output := logs.String()
	if !strings.Contains(output, "已离线") || !strings.Contains(output, "alice") {
		t.Fatal("presence transition/username missing", output)
	}
	if strings.Contains(output, "presence-password") || strings.Contains(output, "synthetic-rule-token") {
		t.Fatal("presence credential leak", output)
	}
	if record.GetString("name") != "alice https://uri-user:presence-password@example.com/?token=synthetic-rule-token" {
		t.Fatal("presence mutated registry")
	}
	// N8's shared redactor returns native JSON; the adapter must still handle
	// historical non-string metadata without exposing nested credentials.
	logs.Reset()
	nested := jsonx.Obj("id", "a16-nested-presence", "name", jsonx.Obj("label", "alice", "token", "synthetic-rule-token"), "token", "synthetic-rule-token", "status", "online")
	before, e := json.Marshal(nested)
	if e != nil {
		t.Fatal(e)
	}
	observeLegacyPresence(ctx, s.m, []*agents.Record{nested})
	nested.Set("status", "offline")
	observeLegacyPresence(ctx, s.m, []*agents.Record{nested})
	if output := logs.String(); !strings.Contains(output, "已离线") || !strings.Contains(output, "alice") || strings.Contains(output, "synthetic-rule-token") {
		t.Fatal("nested presence metadata was lost or leaked", output)
	}
	nested.Set("status", "online")
	after, e := json.Marshal(nested)
	if e != nil || !bytes.Equal(before, after) {
		t.Fatal("nested presence metadata mutated registry", e)
	}
}

func TestProfileFilteringKeepsLiteralDefaultAndDisabledAgents(t *testing.T) {
	ctx, s := fixtureService(t)
	r := s.m.Repository()
	if _, e := r.CreateProfile(ctx, jsonx.Obj("id", "other", "name", "Other"), ""); e != nil {
		t.Fatal(e)
	}
	if _, e := r.SetDefaultProfile(ctx, "other"); e != nil {
		t.Fatal(e)
	}
	_, e := s.m.UpdateAgents(ctx, "", func(_ context.Context, list *[]any) (any, error) {
		original := (*list)[0].(*jsonx.Object)
		unbound := original.DeepCopy()
		unbound.Set("id", "other-device")
		unbound.Set("profile_id", "other")
		disabled := original.DeepCopy()
		disabled.Set("id", "disabled")
		disabled.Set("enabled", false)
		*list = append(*list, unbound, disabled)
		return nil, nil
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

type unreadHistory struct{}

func (unreadHistory) MarshalJSON() ([]byte, error) {
	return nil, errors.New("history must not be encoded")
}
func TestSettingsAndAgentViewNeverReadHistory(t *testing.T) {
	tests := []struct {
		name     string
		settings *jsonx.Object
		want     Settings
	}{
		{"strict booleans", jsonx.Obj("library", true, "rules", false), Settings{Library: true}},
		{"historical wrong type", jsonx.Obj("library", true, "rules", "true"), Settings{Library: true}},
		{"null", jsonx.Obj("library", nil, "rules", nil), Settings{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := jsonx.Obj("id", "device", "rule_sync", jsonx.Obj("settings", tt.settings), "deployments", unreadHistory{}, "ipv6_verifications", unreadHistory{})
			if got := SettingsFor(record); got != tt.want {
				t.Fatalf("%+v want %+v", got, tt.want)
			}
			if _, e := decodeAgent(record); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func decodeAgent(record *agents.Record) (*model.Agent, error) {
	raw, err := json.Marshal(agentView(record))
	if err != nil {
		return nil, fmt.Errorf("encode agent snapshot: %w", err)
	}
	var a model.Agent
	if err = json.Unmarshal(raw, &a); err != nil {
		return nil, fmt.Errorf("decode agent snapshot: %w", err)
	}
	return &a, nil
}

// SettingsFor reads strictly boolean scopes; historical wrong types are off.
func SettingsFor(record *agents.Record) Settings {
	sync := record.GetObject("rule_sync")
	if sync == nil {
		return Settings{}
	}
	raw, err := json.Marshal(sync.Value("settings"))
	if err != nil {
		return Settings{}
	}
	var s model.RuleSyncSettings
	if json.Unmarshal(raw, &s) != nil {
		return Settings{}
	}
	return Settings{Library: s.Library != nil && *s.Library, Rules: s.Rules != nil && *s.Rules}
}

// agentView deliberately omits history members; none of this package's typed
// reads needs them, and the light-read sentinel must never enter a JSON codec.
func agentView(record *agents.Record) *jsonx.Object {
	out := jsonx.NewObject()
	if record == nil {
		return out
	}
	for key, v := range record.All() {
		if !slices.Contains(repo.AgentHistoryFields, key) {
			out.Set(key, v)
		}
	}
	return out
}

func taskRow(ctx context.Context, record *agents.Record, statuses *jsonx.Object) (*jsonx.Object, error) {
	a, err := decodeAgent(record)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(statuses)
	if err != nil {
		return nil, err
	}
	var receipts taskstatus.Snapshot
	if err := json.Unmarshal(data, &receipts); err != nil {
		return nil, err
	}
	row, err := agentTaskRow(ctx, a, receipts)
	if err != nil || row == nil {
		return nil, err
	}
	data, err = json.Marshal(row)
	if err != nil {
		return nil, err
	}
	var out jsonx.Object
	err = json.Unmarshal(data, &out)
	return &out, err
}
func configureLegacyPair(ctx context.Context, m *agents.Manager, records []*agents.Record, settings Settings) error {
	rows := make([]*model.Agent, 0, len(records))
	for _, record := range records {
		a, err := decodeAgent(record)
		if err != nil {
			return err
		}
		rows = append(rows, a)
	}
	return configurePair(ctx, m, rows, settings)
}
func observeLegacyPresence(ctx context.Context, m *agents.Manager, records []*agents.Record) {
	rows := make([]*model.Agent, 0, len(records))
	for _, record := range records {
		a, err := decodeAgent(record)
		if err != nil {
			panic(err)
		}
		rows = append(rows, a)
	}
	observePresence(ctx, m, rows)
}
func fetchLegacySnapshot(ctx context.Context, m *agents.Manager, record *agents.Record, baseline Baseline) (*deploy.Bundle, error) {
	a, err := decodeAgent(record)
	if err != nil {
		return nil, err
	}
	return FetchSnapshot(ctx, m, a, baseline)
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
	legacy, err := s.m.Repository().GetSystemSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := redact.ExternalPayloadSecrets(ctx, legacy)
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
