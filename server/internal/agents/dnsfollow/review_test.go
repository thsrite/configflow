package dnsfollow

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/agents/agentfake"
	"configflow/internal/deploy"
	"configflow/internal/jsonx"
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
	_, err := f.r.UpdateSystemTransaction(f.ctx, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.Set("backup", jsonx.Obj("auto_backup", true))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	fired := false
	draft := func(document *jsonx.Object) string {
		return document.GetObject("profiles").GetObject("default").GetObject("oxidns").GetString("native_config")
	}
	previous := repo.SetHooks(repo.Hooks{ConfigurationCommitted: func(_ *repo.Repository, before, after *jsonx.Object) {
		if fired || before == nil || after == nil || draft(before) == draft(after) || !strings.Contains(draft(after), "127.0.0.1:2053") {
			return
		}
		fired = true
		// A new Mihomo publication reserves right after the draft commit.
		f.editAgent(t, "source", func(a *agents.Record) {
			a.Set("latest_deployment", jsonx.Obj("deployment_id", "source-newer", "status", "preparing"))
		})
	}})
	defer repo.SetHooks(previous)
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
// name says "Agent", and a stored empty name stays empty in the options.
func TestDeviceAndSourceNamesFollowStoredRecord(t *testing.T) {
	f := newFixture(t, Options{}, agentfake.Options{})
	f.editAgent(t, "target", func(a *agents.Record) { a.Delete("name") })
	f.editAgent(t, "source", func(a *agents.Record) { a.Set("name", "") })
	view, err := f.s.Options(f.ctx, "target")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Sources) != 1 || view.Sources[0].ID != "source" || view.Sources[0].Name != "" {
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
	restore := deploy.SetPairPublisher(func(_ context.Context, _ *agents.Manager, id string, _ *deploy.Bundle, _ deploy.PublishOptions) (*agents.Result, bool, error) {
		if id != "target" {
			return nil, false, nil
		}
		return nil, false, &deploy.Refusal{Message: "peer refused " + f.source.token}
	})
	defer restore()
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
	raw, err := f.m.GetAgentByID(f.ctx, "target", true)
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
