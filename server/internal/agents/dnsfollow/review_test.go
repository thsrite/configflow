package dnsfollow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/agents/agentfake"
	"configflow/internal/deploy"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/yamlx"
)

// Standing checklist item 2: the raw DNS requests carry exactly the
// deployment the decision is about. Both fakes ignore the query, so only the
// recorded request URIs can catch a wrong or missing deployment_id.
func TestDNSRequestsCarryExactDeploymentQuery(t *testing.T) {
	f := newFixture(t, Options{}, agentfake.Options{})
	f.bind(t)
	f.source.mu.Lock()
	f.source.paths = nil
	f.source.mu.Unlock()
	f.target.mu.Lock()
	f.target.paths = nil
	f.target.mu.Unlock()
	f.publishSource(t, "source-next")
	if e := f.s.Tick(f.ctx); e != nil {
		t.Fatal(e)
	}
	if st := state(f.agent(t)); st.Status != "succeeded" {
		t.Fatal(st)
	}
	dns := func(p *peer) []string {
		p.mu.Lock()
		defer p.mu.Unlock()
		var out []string
		for _, path := range p.paths {
			if strings.Contains(path, "/api/dns/") {
				out = append(out, path)
			}
		}
		return out
	}
	if got, want := dns(f.source), []string{"GET /api/dns/listener?deployment_id=source-next"}; !slices.Equal(got, want) {
		t.Fatalf("source DNS requests %q, want %q", got, want)
	}
	if got, want := dns(f.target), []string{"GET /api/dns/peer", "GET /api/dns/active?deployment_id=target-baseline"}; !slices.Equal(got, want) {
		t.Fatalf("target DNS requests %q, want %q", got, want)
	}
}

// R04-F1 class: a listener reply with a wrong member type keeps Python's
// specific refusal instead of collapsing into the generic unexpected error.
func TestMalformedListenerReplyKeepsSpecificRefusal(t *testing.T) {
	tests := []struct {
		name  string
		extra map[string]any
		want  string
	}{
		{"numeric-host", map[string]any{"host": 53}, msgFailedPrefix + msgListenerInvalid},
		{"null-host", map[string]any{"host": nil}, msgFailedPrefix + msgListenerInvalid},
		{"empty-host", map[string]any{"host": ""}, msgFailedPrefix + msgListenerIP},
		{"string-enabled", map[string]any{"enabled": "yes"}, msgFailedPrefix + msgListenerDisabled},
		{"numeric-agent", map[string]any{"agent_id": 7}, msgFailedPrefix + msgListenerDisabled},
		{"list-deployment", map[string]any{"deployment_id": []any{"source-next"}}, msgFailedPrefix + msgListenerDisabled},
		{"numeric-success", map[string]any{"success": 1}, msgUnchanged},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, Options{}, agentfake.Options{})
			f.bind(t)
			f.source.mu.Lock()
			f.source.listenerPort = 1053
			f.source.listenerExtra = tc.extra
			f.source.mu.Unlock()
			f.publishSource(t, "source-next")
			if e := f.s.Tick(f.ctx); e != nil {
				t.Fatal(e)
			}
			if st := state(f.agent(t)); st.Message != tc.want {
				t.Fatalf("state %q %q, want %q", st.Status, st.Message, tc.want)
			}
		})
	}
}

// The OxiDNS publication succeeded and its draft was written. A Mihomo
// publication that starts right after the draft commit must not leave the
// binding signature on the old address: Python rebinds whenever the binding
// is still the job's, and a stale signature breaks every later follow with
// msgEdited until the binding is selected again.
func TestSourceMovingAfterDraftWriteStillRebindsSignature(t *testing.T) {
	f := newFixture(t, Options{}, agentfake.Options{})
	f.bind(t)
	err := f.r.UpdateSystem(f.ctx, func(_ context.Context, sys *model.System) error {
		on := true
		sys.Backup.AutoBackup = &on
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	fired := false
	draft := func(doc *model.Document) string {
		if doc == nil {
			return ""
		}
		var p *model.Profile
		for _, item := range doc.Profiles {
			if item != nil && item.ID == "default" {
				p = item
				break
			}
		}
		if p == nil {
			return ""
		}
		raw, _ := json.Marshal(p.OxiDNS)
		var o map[string]any
		_ = json.Unmarshal(raw, &o)
		s, _ := o["native_config"].(string)
		return s
	}
	unsub := f.r.OnConfigurationCommitted(func(_ *repo.Repository, before, after *model.Document) {
		if fired || before == nil || after == nil || draft(before) == draft(after) || !strings.Contains(draft(after), "127.0.0.1:2053") {
			return
		}
		fired = true
		// A new Mihomo publication reserves right after the draft commit.
		f.editAgent(t, "source", func(a *model.Agent) {
			_ = model.SetStoredMember(&a, "latest_deployment", []byte(`{"deployment_id":"source-newer","status":"preparing"}`))
		})
	})
	defer unsub()
	f.publishSource(t, "source-next")
	if e := f.s.Tick(f.ctx); e != nil {
		t.Fatal(e)
	}
	if !fired {
		t.Skip("repository document layout did not expose the draft commit")
	}
	doc, err := yamlx.Parse([]byte(f.profile(t)))
	if err != nil {
		t.Fatal(err)
	}
	a := f.agent(t)
	entry, err := boundEntry(doc, a.DNSFollow)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := signature(entry)
	if err != nil {
		t.Fatal(err)
	}
	if text(yamlx.Get(entry, "addr")) != "udp://127.0.0.1:2053" || a.DNSFollow.UpstreamSignature != sig {
		t.Fatalf("draft %q written but binding signature not rebound (state %q %q)", text(yamlx.Get(entry, "addr")), state(a).Status, state(a).Message)
	}
	if st := state(a); st.Status != "succeeded" || st.Message != msgSucceeded || !value(st.DraftSynced) {
		t.Fatalf("state %q %q", st.Status, st.Message)
	}
}

// Python labels Agent call failures with agent.get("name", "Agent") and
// selectable sources with agent.get("name", agent["id"]): a record without a
// name says "Agent". A stored empty name is a missing one (O18 T1b), so the
// option falls back to the ID.
func TestDeviceAndSourceNamesFollowStoredRecord(t *testing.T) {
	f := newFixture(t, Options{}, agentfake.Options{})
	f.editAgent(t, "target", func(a *model.Agent) { _ = model.ZeroMember(&a, "name") })
	f.editAgent(t, "source", func(a *model.Agent) { a.Name = "" })
	view, err := f.s.Options(f.ctx, "target")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Sources) != 1 || view.Sources[0].ID != "source" || view.Sources[0].Name != "source" {
		t.Fatalf("sources %+v", view.Sources)
	}
	f.target.server.Close()
	_, err = f.s.Configure(f.ctx, "target", json.RawMessage(`{"source_agent_id":"source","upstream_tag":"forward_remote","upstream_index":1,"connection":"auto"}`))
	wantRefusal(t, err, fmt.Sprintf(msgUnreachable, msgDefaultAgentName))
}

// Same class as the task-row fix: the child record that marks an unsubmitted
// follow as failed is projected later with the target's own credentials only
// (public Agent view, A01 redaction), so its error_details must already hide
// every other Agent's credential.
func TestPreparationFailureRecordHidesOtherAgentCredentials(t *testing.T) {
	f := newFixture(t, Options{}, agentfake.Options{})
	f.bind(t)
	f.s.Deploy = deploy.New(deploy.Options{Pair: func(_ context.Context, _ *agents.Manager, id string, _ *deploy.Bundle, _ deploy.PublishOptions) (*model.Deployment, bool, error) {
		if id != "target" {
			return nil, false, nil
		}
		return nil, false, &deploy.Refusal{Message: "peer refused " + f.source.token}
	}})
	f.publishSource(t, "source-next")
	if e := f.s.Tick(f.ctx); e != nil {
		t.Fatal(e)
	}
	a := f.agent(t)
	st := state(a)
	child, ok := a.Deployments.Get(value(st.TargetDeploymentID))
	if st.Status != "failed" || !ok || child.Status != "failed" {
		t.Fatalf("state %q %q child %+v", st.Status, st.Message, child)
	}
	raw, err := f.m.ReadAgent(f.ctx, "target", true)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), f.source.token) {
		t.Fatal("source credential stored in the target's records")
	}
}

// R05-3 regression: a private job whose candidate is missing or not an object
// never replaces the OxiDNS draft. The profile validation refuses it and the
// follow reports the kept draft (draft_synced=false), as Python does for null.
func TestCorruptJobCandidateNeverReplacesDraft(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*model.Agent)
	}{
		{"missing", func(a *model.Agent) { _ = model.ZeroMember(&a.DNSFollowJob, "candidate_settings") }},
		{"null", func(a *model.Agent) {
			raw, _ := json.Marshal(a.DNSFollowJob)
			var m map[string]any
			_ = json.Unmarshal(raw, &m)
			m["candidate_settings"] = nil
			b, _ := json.Marshal(m)
			_ = model.SetStoredMember(&a, "dns_follow_job", b)
		}},
		{"text", func(a *model.Agent) {
			raw, _ := json.Marshal(a.DNSFollowJob)
			var m map[string]any
			_ = json.Unmarshal(raw, &m)
			m["candidate_settings"] = "synthetic"
			b, _ := json.Marshal(m)
			_ = model.SetStoredMember(&a, "dns_follow_job", b)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, Options{}, agentfake.Options{})
			f.bind(t)
			f.publishSource(t, "source-next")
			f.target.mu.Lock()
			f.target.loseUploadResponse = true
			f.target.pollOffline = true
			f.target.mu.Unlock()
			if err := f.s.Tick(f.ctx); err != nil {
				t.Fatal(err)
			}
			if st := state(f.agent(t)); st.Status != "publishing" || f.agent(t).DNSFollowJob == nil {
				t.Fatal(st)
			}
			f.editAgent(t, "target", tc.edit)
			before := f.profile(t)
			f.target.mu.Lock()
			f.target.pollOffline = false
			f.target.mu.Unlock()
			if err := f.s.Tick(f.ctx); err != nil {
				t.Fatal(err)
			}
			p, err := f.r.Profile(f.ctx, "default")
			if err != nil {
				t.Fatal(err)
			}
			if len(oxidnsMap(t, p)) == 0 || f.profile(t) != before {
				t.Fatalf("corrupt job replaced the draft: %v", p.OxiDNS)
			}
			a := f.agent(t)
			if st := state(a); value(st.DraftSynced) || a.DNSFollow.UpstreamSignature == a.DNSFollowJob.CandidateSignature {
				t.Fatalf("kept draft reported as synchronized: %q %q", st.Status, st.Message)
			}
		})
	}
}

// Design 3.7 row "draft written, state not yet succeeded" on a real Runtime:
// shutdown right after the draft commit records nothing more; after a
// restart the next round finalizes without uploading again and rebinds.
func TestShutdownAfterDraftCommitResumesWithoutSecondUpload(t *testing.T) {
	f := newFixture(t, Options{}, agentfake.Options{})
	f.bind(t)
	err := f.r.UpdateSystem(f.ctx, func(_ context.Context, sys *model.System) error {
		on := true
		sys.Backup.AutoBackup = &on
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	f.publishSource(t, "source-next")
	startRuntime(t, f.ctx)
	arrived, release := make(chan struct{}), make(chan struct{})
	fired := false
	draft := func(doc *model.Document) string {
		if doc == nil {
			return ""
		}
		var p *model.Profile
		for _, item := range doc.Profiles {
			if item != nil && item.ID == "default" {
				p = item
				break
			}
		}
		if p == nil {
			return ""
		}
		raw, _ := json.Marshal(p.OxiDNS)
		var o map[string]any
		_ = json.Unmarshal(raw, &o)
		s, _ := o["native_config"].(string)
		return s
	}
	unsub := f.r.OnConfigurationCommitted(func(_ *repo.Repository, before, after *model.Document) {
		if fired || draft(before) == draft(after) || !strings.Contains(draft(after), "127.0.0.1:2053") {
			return
		}
		fired = true
		close(arrived)
		<-release
	})
	defer unsub()
	done := make(chan error, 1)
	go func() { done <- f.s.Tick(lifecycle.BindRequest(f.ctx)) }()
	select {
	case <-arrived:
	case <-time.After(30 * time.Second):
		t.Fatal("follow never committed the draft")
	}
	lifecycle.Stop(20 * time.Millisecond)
	before := durable(t, f)
	close(release)
	if e := <-done; !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, durable(t, f)) {
		t.Fatal("late commit after shutdown")
	}
	a := f.agent(t)
	if st := state(a); st.Status != "publishing" || a.DNSFollow.UpstreamSignature == a.DNSFollowJob.CandidateSignature {
		t.Fatalf("unexpected stored stage %q %q", st.Status, st.Message)
	}
	// Restart: fresh Runtime, Manager and owner over the same data directory.
	startRuntime(t, f.ctx)
	f.m.Close()
	m, err := agents.New(f.ctx, f.r)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	f.m, f.s = m, New(m, Options{})
	if err = f.s.Tick(lifecycle.BindRequest(f.ctx)); err != nil {
		t.Fatal(err)
	}
	a = f.agent(t)
	if st := state(a); st.Status != "succeeded" || st.Message != msgSucceeded || !value(st.DraftSynced) || a.DNSFollow.UpstreamSignature != a.DNSFollowJob.CandidateSignature || f.target.uploads() != 2 {
		t.Fatalf("restart did not finish: %q %q uploads %d", st.Status, st.Message, f.target.uploads())
	}
}
