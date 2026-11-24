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
	"configflow/internal/model"
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
	e := f.r.UpdateSystem(f.ctx, func(_ context.Context, sys *model.System) error {
		sys.Agents = append(sys.Agents, standby.record())
		return nil
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
	_, e = f.r.UpdateProfile(f.ctx, "default", func(_ context.Context, p *model.Profile) error {
		o := oxidnsMap(t, p)
		o["native_config"] = haDraft
		setOxidnsMap(t, p, o)
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	current, rev, e := f.r.PublishConfig(f.ctx, "default", false)
	if e != nil {
		t.Fatal(e)
	}
	currentRaw, e := json.Marshal(current)
	if e != nil {
		t.Fatal(e)
	}
	source, e := deploy.SourceFromJSON(currentRaw, &rev)
	if e != nil {
		t.Fatal(e)
	}
	tRec := f.target.record()
	tTarget, e := deploy.NewTypedTarget(&tRec)
	if e != nil {
		t.Fatal(e)
	}
	dp := deploy.New(deploy.Options{})
	prepared, e := dp.PrepareContent(f.ctx, f.m, tTarget, source, deploy.PrepareOptions{DeploymentID: "target-ha-baseline", BaseURL: "https://panel.example"})
	if e != nil {
		t.Fatal(e)
	}
	published, e := dp.PublishDirect(f.ctx, f.m, "target", prepared.Bundle, deploy.PublishOptions{Activate: true, SourceSnapshot: source, ResourceConfig: source, BaseURL: "https://panel.example"})
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
	rawCfg, rev, e := f.r.PublishConfig(f.ctx, "default", false)
	if e != nil {
		t.Fatal(e)
	}
	rawBytes, e := json.Marshal(rawCfg)
	if e != nil {
		t.Fatal(e)
	}
	src, e := deploy.SourceFromJSON(rawBytes, &rev)
	if e != nil {
		t.Fatal(e)
	}
	sRec := standby.record()
	sTarget, e := deploy.NewTypedTarget(&sRec)
	if e != nil {
		t.Fatal(e)
	}
	p, e := dp.PrepareContent(f.ctx, f.m, sTarget, src, deploy.PrepareOptions{DeploymentID: "standby-baseline", BaseURL: "https://panel.example"})
	if e != nil {
		t.Fatal(e)
	}
	result, e := dp.PublishDirect(f.ctx, f.m, "standby", p.Bundle, deploy.PublishOptions{Activate: true, SourceSnapshot: src, ResourceConfig: src, BaseURL: "https://panel.example"})
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
	e = f.r.UpdateSystem(f.ctx, func(_ context.Context, sys *model.System) error {
		for i := range sys.Agents {
			id := sys.Agents[i].ID
			if id != "target" && id != "standby" {
				continue
			}
			other, role := "standby", "primary"
			if id == "standby" {
				other, role = "target", "secondary"
			}
			haRaw, err := json.Marshal(map[string]any{"enabled": true, "group_id": "synthetic-follow-pair", "peer_id": other, "vip": "192.0.2.53", "vrid": 53, "role": role})
			if err != nil {
				return err
			}
			if err := model.SetStoredMember(&sys.Agents[i], "oxidns_ha", haRaw); err != nil {
				return err
			}
			if err := model.SetStoredMember(&sys.Agents[i], "oxidns_ha_state", []byte(`{"status":"succeeded","error":""}`)); err != nil {
				return err
			}
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	oldHooks := agents.CurrentHooks()
	dnsha.Register()
	t.Cleanup(func() { agents.ResetHooks(); agents.RegisterHooks(oldHooks) })
	f.s.Deploy = deploy.New(deploy.Options{Pair: func(ctx context.Context, m *agents.Manager, id string, b *deploy.Bundle, o deploy.PublishOptions) (*model.Deployment, bool, error) {
		return dnsha.ForManager(m).PublishPair(ctx, id, b, o)
	}})
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
			peer, e := f.m.ReadAgent(f.ctx, "standby", true)
			if e != nil || peer.OxiDNSHAPublication == nil || peer.OxiDNSHAPublication.Status != "succeeded" {
				t.Fatal("peer result", e)
			}
			return
		}
	}
	a, _ := f.s.get(f.ctx, "target")
	t.Fatal("HA follow did not converge", state(a).Status, state(a).Message)
}
