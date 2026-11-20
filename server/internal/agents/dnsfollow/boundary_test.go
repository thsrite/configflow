package dnsfollow

import (
	"configflow/internal/taskstatus"
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
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

func (f *fixture) editAgent(t *testing.T, id string, edit func(*model.Agent)) {
	t.Helper()
	err := f.r.UpdateSystem(f.ctx, func(_ context.Context, sys *model.System) error {
		for i := range sys.Agents {
			if sys.Agents[i].ID == id {
				edit(&sys.Agents[i])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func (f *fixture) editDraft(t *testing.T, edit func(*model.Profile)) {
	t.Helper()
	_, err := f.r.UpdateProfile(f.ctx, "default", func(_ context.Context, p *model.Profile) error {
		edit(p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func oxidnsMap(t *testing.T, p *model.Profile) map[string]any {
	t.Helper()
	raw, err := json.Marshal(p.OxiDNS)
	if err != nil {
		t.Fatal(err)
	}
	var o map[string]any
	if err := json.Unmarshal(raw, &o); err != nil {
		t.Fatal(err)
	}
	if o == nil {
		o = map[string]any{}
	}
	return o
}
func setOxidnsMap(t *testing.T, p *model.Profile, o map[string]any) {
	t.Helper()
	raw, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SetJSON("oxidns", raw); err != nil {
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
	var fixtures map[string]struct {
		Key     string             `json:"key"`
		Rules   []model.RuleConfig `json:"rules"`
		Actions json.RawMessage    `json:"actions"`
	}
	if err := json.Unmarshal(fixtureBytes, &fixtures); err != nil {
		t.Fatal(err)
	}
	// The complete records and DNS actions come from backend.test_dns_follow.pin_rule.
	for _, name := range []string{"active", "parked"} {
		t.Run(name, func(t *testing.T) {
			sample := fixtures[name]
			pinnedIndex := 0
			f.editDraft(t, func(p *model.Profile) {
				kept := []model.RuleConfig{}
				for _, row := range p.RuleConfigs {
					if row.ID != "rule-a" && row.ID != "rule-b" {
						kept = append(kept, row)
					}
				}
				pinnedIndex = len(kept) + 2
				p.RuleConfigs = append(kept, sample.Rules...)
				o := oxidnsMap(t, p)
				var actions any
				_ = json.Unmarshal(sample.Actions, &actions)
				o[sample.Key] = actions
				setOxidnsMap(t, p, o)
			})
			_, err := f.s.Configure(f.ctx, "target", json.RawMessage(`{"source_agent_id":"source","upstream_tag":"forward_remote","upstream_index":1,"connection":"auto"}`))
			wantRefusal(t, err, fmt.Sprintf(msgPinned, pinnedIndex))
			f.editDraft(t, func(p *model.Profile) {
				o := oxidnsMap(t, p)
				delete(o, "rule_actions")
				delete(o, "rule_dns_parked")
				setOxidnsMap(t, p, o)
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
	f.editAgent(t, "target", func(a *model.Agent) { _ = a.SetJSON("oxidns_ha", []byte(`{"enabled":true}`)) })
	_, err := f.s.Configure(f.ctx, "target", json.RawMessage(`{"source_agent_id":"source","upstream_tag":"forward_remote","upstream_index":1,"connection":"auto"}`))
	wantRefusal(t, err, msgHANetwork)
	_, err = f.s.Configure(f.ctx, "target", json.RawMessage(`{"source_agent_id":"source","upstream_tag":"forward_remote","upstream_index":1,"connection":"network","address":"2001:db8::7"}`))
	if err != nil {
		t.Fatal(err)
	}
	f.editAgent(t, "target", func(a *model.Agent) { _ = a.Clear("oxidns_ha") })
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
				f.editDraft(t, func(p *model.Profile) {
					o := oxidnsMap(t, p)
					s, _ := o["native_config"].(string)
					o["native_config"] = strings.Replace(s, "worker_threads: 2", "worker_threads: 3", 1)
					setOxidnsMap(t, p, o)
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
			f.editAgent(t, "source", func(a *model.Agent) {
				yes := true
				a.LatestDeployment.ManualRollback = &yes
			})
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
	f.editAgent(t, "source", func(a *model.Agent) { off := false; a.Enabled = &off })
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
	f.editAgent(t, "source", func(a *model.Agent) { on := true; a.Enabled = &on })
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
	f.editAgent(t, "target", func(a *model.Agent) {
		_ = a.SetJSON("dns_follow_state", []byte(`{"status":"failed","message":"synthetic-target-token https://user:secret@dns.example/a?token=credential","events":[]}`))
		_ = a.SetJSON("dns_follow_job", []byte(`{"candidate_settings":{"token":"synthetic-private-job"}}`))
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
	rec := f.target.record()
	row, err := agentTaskRow(f.ctx, &rec, taskstatus.Snapshot{})
	if err != nil || row != nil {
		t.Fatal(row, err)
	}
	raw, _ := f.m.ReadAgent(f.ctx, "target", true)
	row, err = agentTaskRow(f.ctx, raw, taskstatus.Snapshot{})
	if err != nil || row.CanRun || row.StateMessage != msgRetryQueued {
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
			f.editAgent(t, "source", func(a *model.Agent) {
				yes := true
				a.LatestDeployment.ManualRollback = &yes
			})
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
