package rulesync

import (
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
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
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
	// A changed relationship cannot be overwritten by an already captured pair.
	_, e = s.m.UpdateAgents(ctx, "", func(_ context.Context, list *[]any) (any, error) {
		(*list)[1].(*jsonx.Object).GetObject("oxidns_ha").Set("group_id", "other")
		return nil, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	before := systemBytes(t, s)
	if e = configurePair(ctx, s.m, pair, Settings{Rules: true}); e == nil {
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
