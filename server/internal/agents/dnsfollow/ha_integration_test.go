package dnsfollow

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/agents/agentfake"
	"configflow/internal/agents/dnsha"
	"configflow/internal/deploy"
	"configflow/internal/jsonx"
)

type haFollowClient struct{ origins map[string]string }

func (c haFollowClient) Do(ctx context.Context, r agents.Request) (*agents.Response, error) {
	u, e := url.Parse(r.URL)
	if e != nil {
		return nil, e
	}
	if strings.HasPrefix(u.Path, "/api/oxidns-ha") {
		base := c.origins[u.Scheme+"://"+u.Host]
		if base == "" {
			return nil, &Error{Message: "unexpected HA test target"}
		}
		r.URL = base + u.RequestURI()
	}
	reply, e := (agents.HTTPClient{}).Do(ctx, r)
	if e == nil && u.Path == "/api/capabilities" {
		var raw map[string]json.RawMessage
		if json.Unmarshal(reply.Body, &raw) == nil {
			raw["oxidns_ha_protocols"] = json.RawMessage(`[1]`)
			raw["oxidns_ha_handoff_protocols"] = json.RawMessage(`[1]`)
			reply.Body, e = json.Marshal(raw)
		}
	}
	return reply, e
}
func TestDNSFollowPublishesBothHAMembersThroughRealOwner(t *testing.T) {
	f := newFixture(t, Options{}, agentfake.Options{})
	standby := newPeer(t, "standby", "oxidns", agentfake.Options{})
	_, e := f.m.UpdateAgents(f.ctx, "", func(_ context.Context, rows *[]any) (any, error) {
		*rows = append(*rows, standby.record())
		return nil, nil
	})
	if e != nil {
		t.Fatal(e)
	}

	// The follow-only fixture has no DNS listener. HA requires a wildcard UDP 53
	// listener in both baseline and candidate, so publish a real HA-capable draft.
	haDraft := nativeDraft + `  - tag: sequence_main
    type: sequence
    args:
      - exec: $forward_remote
  - tag: udp
    type: udp_server
    args:
      entry: sequence_main
      listen: 0.0.0.0:53
`
	_, _, e = f.r.UpdateProfileTransaction(f.ctx, "default", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
		p.GetObject("oxidns").Set("native_config", haDraft)
		return nil, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	current, rev, e := f.r.GetPublishSnapshot(f.ctx, "default", false)
	if e != nil {
		t.Fatal(e)
	}
	source := deploy.NewSource(current, &rev)
	prepared, e := deploy.PrepareContent(f.ctx, f.m, deploy.NewTarget(f.target.record()), source, deploy.PrepareOptions{DeploymentID: "target-ha-baseline", BaseURL: "https://panel.example"})
	if e != nil {
		t.Fatal(e)
	}
	published, e := deploy.PublishDirect(f.ctx, f.m, "target", prepared.Bundle, deploy.PublishOptions{Activate: true, SourceSnapshot: source, ResourceConfig: source, BaseURL: "https://panel.example"})
	if e != nil {
		t.Fatal(e)
	}
	for range 4 {
		if published.Status == "succeeded" {
			break
		}
		published, e = f.m.ReadDeploymentModel(f.ctx, "target", "target-ha-baseline")
		if e != nil {
			t.Fatal(e)
		}
	}
	if published.Status != "succeeded" {
		t.Fatal(published)
	}
	// Establish the same actual baseline on the peer before adding HA ownership.
	raw, rev, e := f.r.GetPublishSnapshot(f.ctx, "default", false)
	if e != nil {
		t.Fatal(e)
	}
	src := deploy.NewSource(raw, &rev)
	p, e := deploy.PrepareContent(f.ctx, f.m, deploy.NewTarget(standby.record()), src, deploy.PrepareOptions{DeploymentID: "standby-baseline", BaseURL: "https://panel.example"})
	if e != nil {
		t.Fatal(e)
	}
	result, e := deploy.PublishDirect(f.ctx, f.m, "standby", p.Bundle, deploy.PublishOptions{Activate: true, SourceSnapshot: src, ResourceConfig: src, BaseURL: "https://panel.example"})
	if e != nil {
		t.Fatal(e)
	}
	for range 4 {
		if result.Status == "succeeded" {
			break
		}
		result, e = f.m.ReadDeploymentModel(f.ctx, "standby", "standby-baseline")
		if e != nil {
			t.Fatal(e)
		}
	}
	if result.Status != "succeeded" {
		t.Fatal(result)
	}
	segment := agentfake.NewSegment("target")
	origins := map[string]string{}
	for _, peer := range []*peer{f.target, standby} {
		haServer := httptest.NewServer(segment.Handler(peer.id, peer.token, peer.fake))
		t.Cleanup(haServer.Close)
		origins[peer.server.URL] = haServer.URL
	}
	segment.Configure("target", "standby")
	f.m.SetClient(haFollowClient{origins})
	_, e = f.m.UpdateAgents(f.ctx, "", func(_ context.Context, rows *[]any) (any, error) {
		for _, v := range *rows {
			a := v.(*jsonx.Object)
			id := a.GetString("id")
			if id != "target" && id != "standby" {
				continue
			}
			other, role := "standby", "primary"
			if id == "standby" {
				other, role = "target", "secondary"
			}
			a.Set("oxidns_ha", jsonx.Obj("enabled", true, "group_id", "synthetic-follow-pair", "peer_id", other, "vip", "192.0.2.53", "vrid", 53, "role", role))
			a.Set("oxidns_ha_state", jsonx.Obj("status", "succeeded", "error", ""))
		}
		return nil, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	oldHooks := agents.CurrentHooks()
	dnsha.Register()
	t.Cleanup(func() { agents.ResetHooks(); agents.RegisterHooks(oldHooks) })
	_, e = f.s.Configure(f.ctx, "target", json.RawMessage(`{"source_agent_id":"source","upstream_tag":"forward_remote","upstream_index":0,"connection":"network","address":"192.0.2.10"}`))
	if e != nil {
		t.Fatal(e)
	}
	beforeTarget, beforeStandby := f.target.uploads(), standby.uploads()
	f.publishSource(t, "source-ha-next")
	if e = f.s.Tick(f.ctx); e != nil {
		t.Fatal(e)
	}
	for range 14 {
		if _, e = dnsha.ForManager(f.m).Advance(f.ctx, "target"); e != nil {
			t.Fatal(e)
		}
		if e = f.s.Tick(f.ctx); e != nil {
			t.Fatal(e)
		}
		a, e := f.s.get(f.ctx, "target")
		if e != nil {
			t.Fatal(e)
		}
		if state(a).Status == "succeeded" {
			if f.target.uploads() != beforeTarget+1 || standby.uploads() != beforeStandby+1 {
				t.Fatal("follow did not publish exactly one pair")
			}
			peer, e := f.m.GetAgentByID(f.ctx, "standby", true)
			if e != nil || peer.GetObject("oxidns_ha_publication").GetString("status") != "succeeded" {
				t.Fatal("peer result", e)
			}
			return
		}
	}
	a, _ := f.s.get(f.ctx, "target")
	t.Fatal("HA follow did not converge", state(a).Status, state(a).Message)
}
