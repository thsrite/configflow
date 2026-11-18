package dnsfollow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/agents/agentfake"
	"configflow/internal/deploy"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

func (f *fixture) editAgent(t *testing.T, id string, edit func(*agents.Record)) {
	t.Helper()
	_, err := f.m.UpdateAgents(f.ctx, "", func(_ context.Context, rows *[]any) (any, error) {
		for _, row := range *rows {
			if a, ok := row.(*jsonx.Object); ok && a.GetString("id") == id {
				edit(a)
			}
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func (f *fixture) editDraft(t *testing.T, edit func(*jsonx.Object)) {
	t.Helper()
	_, _, err := f.r.UpdateProfileTransaction(f.ctx, "default", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) { edit(p); return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
}
func wantRefusal(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || err.Error() != want {
		t.Fatalf("refusal: %v; want %s", err, want)
	}
}

func TestBindingValidationIsStrictAndDoesNotWrite(t *testing.T) {
	f := newFixture(t, Options{}, agentfake.Options{})
	for _, tc := range []struct{ name, body, want string }{
		{"array", `[]`, msgParameters}, {"null", `null`, msgParameters}, {"unknown", `{"extra":1}`, msgParameters},
		{"missing-source", `{}`, "mihomo" + msgMissingAgent},
		{"null-index", `{"source_agent_id":"source","upstream_tag":"forward_remote","upstream_index":null,"connection":"network"}`, msgSelection},
		{"bool-index", `{"source_agent_id":"source","upstream_tag":"forward_remote","upstream_index":true,"connection":"network"}`, msgSelection},
		{"float-index", `{"source_agent_id":"source","upstream_tag":"forward_remote","upstream_index":1.0,"connection":"network"}`, msgSelection},
		{"null-tag", `{"source_agent_id":"source","upstream_tag":null,"upstream_index":1,"connection":"network"}`, msgSelection},
		{"loopback", `{"source_agent_id":"source","upstream_tag":"forward_remote","upstream_index":1,"connection":"network","address":"127.0.0.1"}`, msgHostLocal},
		{"url-host", `{"source_agent_id":"source","upstream_tag":"forward_remote","upstream_index":1,"connection":"network","address":"https://dns.example"}`, msgHostFormat},
		{"removed", `{"source_agent_id":"source","upstream_tag":"missing","upstream_index":1,"connection":"network","address":"dns.example"}`, msgRemoved},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := f.r.CommitCount()
			_, err := f.s.Configure(f.ctx, "target", json.RawMessage(tc.body))
			wantRefusal(t, err, tc.want)
			if f.r.CommitCount() != before {
				t.Fatal("invalid request committed")
			}
		})
	}
	fixtureBytes, err := os.ReadFile("testdata/pinned.json")
	if err != nil {
		t.Fatal(err)
	}
	fixtureValue, err := jsonx.Parse(fixtureBytes)
	if err != nil {
		t.Fatal(err)
	}
	fixtures := fixtureValue.(*jsonx.Object)
	// The complete records and DNS actions come from backend.test_dns_follow.pin_rule.
	for _, name := range []string{"active", "parked"} {
		t.Run(name, func(t *testing.T) {
			sample := fixtures.GetObject(name)
			rules, _ := jsonx.AsList(sample.Value("rules"))
			pinnedIndex := 0
			f.editDraft(t, func(p *jsonx.Object) {
				existing, _ := jsonx.AsList(p.Value("rule_configs"))
				kept := []any{}
				for _, row := range existing {
					a, ok := row.(*jsonx.Object)
					if !ok || (a.GetString("id") != "rule-a" && a.GetString("id") != "rule-b") {
						kept = append(kept, row)
					}
				}
				pinnedIndex = len(kept) + 2
				p.Set("rule_configs", append(kept, rules...))
				p.GetObject("oxidns").Set(sample.GetString("key"), sample.Value("actions"))
			})
			_, err := f.s.Configure(f.ctx, "target", json.RawMessage(`{"source_agent_id":"source","upstream_tag":"forward_remote","upstream_index":1,"connection":"auto"}`))
			wantRefusal(t, err, fmt.Sprintf(msgPinned, pinnedIndex))
			f.editDraft(t, func(p *jsonx.Object) {
				p.GetObject("oxidns").Delete("rule_actions")
				p.GetObject("oxidns").Delete("rule_dns_parked")
			})
		})
	}

}

func TestBusyOwnerAcrossManagersPrecedesValidation(t *testing.T) {
	f := newFixture(t, Options{}, agentfake.Options{})
	m, err := agents.New(f.ctx, f.r)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	_, release, ok := followLocks.TryLock(f.ctx, f.r.DataDir())
	if !ok {
		t.Fatal("lock")
	}
	defer release()
	_, err = New(m, Options{}).Configure(f.ctx, "absent", json.RawMessage(`[]`))
	wantRefusal(t, err, msgBusy)
	if err = f.s.Tick(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.s.rounds.Len() != 0 {
		t.Fatal("busy round incorrectly rested")
	}
}

func TestNetworkAndHASelection(t *testing.T) {
	f := newFixture(t, Options{}, agentfake.Options{})
	f.editAgent(t, "target", func(a *agents.Record) { a.Set("oxidns_ha", jsonx.Obj("enabled", true)) })
	_, err := f.s.Configure(f.ctx, "target", json.RawMessage(`{"source_agent_id":"source","upstream_tag":"forward_remote","upstream_index":1,"connection":"auto"}`))
	wantRefusal(t, err, msgHANetwork)
	_, err = f.s.Configure(f.ctx, "target", json.RawMessage(`{"source_agent_id":"source","upstream_tag":"forward_remote","upstream_index":1,"connection":"network","address":"2001:db8::7"}`))
	if err != nil {
		t.Fatal(err)
	}
	f.editAgent(t, "target", func(a *agents.Record) { a.Delete("oxidns_ha") })
	f.publishSource(t, "source-next")
	if err = f.s.Tick(f.ctx); err != nil {
		t.Fatal(err)
	}
	if got := value(state(f.agent(t)).Address); got != "udp://[2001:db8::7]:2053" {
		t.Fatal(got)
	}
}

func TestDraftDriftAndConcurrentEditPreserved(t *testing.T) {
	for _, during := range []bool{false, true} {
		t.Run(map[bool]string{false: "unpublished", true: "during-publication"}[during], func(t *testing.T) {
			f := newFixture(t, Options{}, agentfake.Options{})
			f.bind(t)
			f.publishSource(t, "source-next")
			edit := func() {
				f.editDraft(t, func(p *jsonx.Object) {
					o := p.GetObject("oxidns")
					o.Set("native_config", strings.Replace(o.GetString("native_config"), "worker_threads: 2", "worker_threads: 3", 1))
				})
			}
			if during {
				f.target.mu.Lock()
				f.target.hook = func(r *http.Request) {
					if r.Method == http.MethodPost && r.URL.Path == "/api/deployments" {
						edit()
					}
				}
				f.target.mu.Unlock()
			} else {
				edit()
			}
			if err := f.s.Tick(f.ctx); err != nil {
				t.Fatal(err)
			}
			st := state(f.agent(t))
			if during {
				if st.Status != "succeeded" || value(st.DraftSynced) || !strings.Contains(st.Message, msgDraftEdited) {
					t.Fatal(st)
				}
			} else {
				if st.Status != "failed" || !strings.Contains(st.Message, UnpublishedChanges) || f.target.uploads() != 1 {
					t.Fatal(st)
				}
			}
			if !strings.Contains(f.profile(t), "worker_threads: 3") {
				t.Fatal("concurrent draft overwritten")
			}
		})
	}
}

func TestOwnershipChangedDuringVerificationRejectsChild(t *testing.T) {
	f := newFixture(t, Options{}, agentfake.Options{})
	f.bind(t)
	f.publishSource(t, "source-next")
	f.target.mu.Lock()
	f.target.hook = func(r *http.Request) {
		if r.URL.Path == "/api/dns/active" {
			f.editAgent(t, "source", func(a *agents.Record) { a.GetObject("latest_deployment").Set("manual_rollback", true) })
		}
	}
	f.target.mu.Unlock()
	if err := f.s.Tick(f.ctx); err != nil {
		t.Fatal(err)
	}
	st := state(f.agent(t))
	if st.Status != "failed" || !strings.Contains(st.Message, msgSourceChanged) || f.target.uploads() != 1 {
		t.Fatal(st)
	}
}

func TestSettledAndBrokenRoundsRestWithoutWrites(t *testing.T) {
	clock := 0.0
	f := newFixture(t, Options{Monotonic: func() float64 { return clock }}, agentfake.Options{})
	f.bind(t)
	if err := f.s.Tick(f.ctx); err != nil {
		t.Fatal(err)
	}
	before := f.r.CommitCount()
	paths := len(f.source.paths) + len(f.target.paths)
	for i := 0; i < 3; i++ {
		if err := f.s.Tick(f.ctx); err != nil {
			t.Fatal(err)
		}
	}
	if f.r.CommitCount() != before || len(f.source.paths)+len(f.target.paths) != paths || f.s.rounds.Len() != 1 {
		t.Fatal("settled round performed work")
	}
	f.editAgent(t, "source", func(a *agents.Record) { a.Set("enabled", false) })
	for i := 0; i < 2; i++ {
		if err := f.s.Tick(f.ctx); err != nil {
			t.Fatal(err)
		}
	}
	if state(f.agent(t)).Status != "failed" {
		t.Fatal("broken binding not recorded")
	}
	before = f.r.CommitCount()
	for i := 0; i < 3; i++ {
		if err := f.s.Tick(f.ctx); err != nil {
			t.Fatal(err)
		}
	}
	if f.r.CommitCount() != before || f.s.rounds.Len() != 1 {
		t.Fatal("unchanged failure rewrote state")
	}
	clock = agents.RecheckSeconds + 1
	if err := f.s.Tick(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.r.CommitCount() != before {
		t.Fatal("recheck rewrote unchanged failure")
	}
	f.editAgent(t, "source", func(a *agents.Record) { a.Set("enabled", true) })
	f.publishSource(t, "repaired-source")
	if err := f.s.Tick(f.ctx); err != nil {
		t.Fatal(err)
	}
	if state(f.agent(t)).Status != "succeeded" {
		t.Fatal(state(f.agent(t)))
	}
}

func TestRetryNullSourceAndPrivateProjection(t *testing.T) {
	f := newFixture(t, Options{}, agentfake.Options{})
	f.bind(t)
	f.editAgent(t, "target", func(a *agents.Record) {
		a.Set("dns_follow_state", jsonx.Obj("status", "failed", "message", "synthetic-target-token https://user:secret@dns.example/a?token=credential", "events", []any{}))
		a.Set("dns_follow_job", jsonx.Obj("candidate_settings", jsonx.Obj("token", "synthetic-private-job")))
	})
	view, err := f.s.Options(f.ctx, "target")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(view)
	if strings.Contains(string(b), "synthetic-target-token") || strings.Contains(string(b), "synthetic-private-job") || strings.Contains(string(b), "secret@") {
		t.Fatal(string(b))
	}
	out, err := f.s.Retry(f.ctx, "target")
	if err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(out.State)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(b, &fields)
	if string(fields["source_deployment_id"]) != "null" || value(out.State.Attempt) != 1 || f.agent(t).DNSFollowJob != nil {
		t.Fatal(string(b))
	}
	row, err := taskRow(f.ctx, f.target.record(), jsonx.Obj())
	if err != nil || row != nil {
		t.Fatal(row, err)
	}
	raw, _ := f.m.GetAgentByID(f.ctx, "target", true)
	row, err = taskRow(f.ctx, raw, jsonx.Obj())
	if err != nil || row.GetBool("can_run") || row.GetString("state_message") != msgRetryQueued {
		t.Fatal(row, err)
	}
	_, err = f.s.Disable(f.ctx, "target")
	if err != nil {
		t.Fatal(err)
	}
	if f.agent(t).DNSFollow != nil || f.agent(t).DNSFollowState != nil {
		t.Fatal("disable retained state")
	}
}

func TestStateDedupeIncludesFieldsButIgnoresTime(t *testing.T) {
	f := newFixture(t, Options{}, agentfake.Options{})
	f.bind(t)
	_, written, err := f.s.saveState(f.ctx, "target", "waiting", "synthetic state", statePatch{address: ptr("udp://192.0.2.1:53")}, nil)
	if err != nil || !written {
		t.Fatal(written, err)
	}
	before := f.r.CommitCount()
	_, written, err = f.s.saveState(f.ctx, "target", "waiting", "synthetic state", statePatch{}, nil)
	if err != nil || written || before != f.r.CommitCount() {
		t.Fatal(written, err)
	}
	_, written, err = f.s.saveState(f.ctx, "target", "waiting", "synthetic state", statePatch{attempt: ptr(model.Int(2))}, nil)
	if err != nil || !written {
		t.Fatal(written, err)
	}
}

// Keep test seams on the real A14 preparation boundary; this wrapper observes
// the production options rather than constructing an alternate candidate.
func TestPrepareUsesProductionOptionsAndRechecksOwnership(t *testing.T) {
	calls := 0
	f := newFixture(t, Options{}, agentfake.Options{})
	f.bind(t)
	f.publishSource(t, "source-next")
	f.s.prepare = func(ctx context.Context, m *agents.Manager, a deploy.Target, src *deploy.Source, o deploy.PrepareOptions) (*deploy.Prepared, error) {
		calls++
		if o.BaseURL != "https://panel.example" || o.DeploymentID == "" || o.PreferCached || o.StoreDownloads {
			return nil, fmt.Errorf("unexpected preparation options: %#v", o)
		}
		p, err := deploy.PrepareContent(ctx, m, a, src, o)
		if calls == 2 {
			f.editAgent(t, "source", func(a *agents.Record) { a.GetObject("latest_deployment").Set("manual_rollback", true) })
		}
		return p, err
	}
	if err := f.s.Tick(f.ctx); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || f.target.uploads() != 1 || !strings.Contains(state(f.agent(t)).Message, msgSourceChanged) {
		t.Fatal(calls, state(f.agent(t)))
	}
}

func TestCancelledContextDoesNotEnterOwner(t *testing.T) {
	f := newFixture(t, Options{}, agentfake.Options{})
	ctx, cancel := context.WithCancel(f.ctx)
	cancel()
	before := f.r.CommitCount()
	// Explicit cancellation belongs to the owner when the work is already tracked.
	err := f.s.leased(ctx, func(context.Context) error { t.Fatal("entered cancelled lease"); return nil })
	if !errors.Is(err, context.Canceled) || before != f.r.CommitCount() {
		t.Fatal(err)
	}
	if err = f.s.run(f.ctx, nil, nil, nil, model.Deployment{}); err == nil {
		t.Fatal("missing target accepted")
	}
}

func TestStoredIndexFallbackIsNeverUpstreamZeroEvidence(t *testing.T) {
	doc, err := yamlx.Parse([]byte(nativeDraft))
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"upstream_tag":"forward_remote","upstream_index":"0"}`, `{"upstream_tag":"forward_remote","upstream_index":null}`, `{"upstream_tag":"forward_remote"}`} {
		var b model.DNSFollowBinding
		if err := json.Unmarshal([]byte(raw), &b); err != nil {
			t.Fatal(err)
		}
		_, err := boundEntry(doc, &b)
		wantRefusal(t, err, msgRemoved)
	}
	var b model.DNSFollowBinding
	_ = json.Unmarshal([]byte(`{"upstream_tag":"forward_remote","upstream_index":0}`), &b)
	if _, err := boundEntry(doc, &b); err != nil {
		t.Fatal("genuine zero rejected", err)
	}
}
