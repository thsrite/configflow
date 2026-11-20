package dnsha

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/model"
)

func TestHAValidationLockOrderingAndAllOrNothingLeases(t *testing.T) {
	f := fixtureNew(t, Options{}, false, nil)
	held, release, ok := f.s.haLease(f.ctx)
	if !ok {
		t.Fatal("HA lease")
	}
	_, e := f.s.Configure(held, "missing", []byte(`[]`), "")
	release()
	if e == nil || e.Error() != msgBusy {
		t.Fatal("validation bypassed busy HA lock", e)
	}
	key := f.r.DataDir() + "\x00member:secondary"
	_, unlock, ok := pairLocks.TryLock(f.ctx, key)
	if !ok {
		t.Fatal("member lease")
	}
	if _, _, ok = f.s.lease(f.ctx, []string{"primary", "secondary"}); ok {
		t.Fatal("partial pair lock succeeded")
	}
	unlock()
	_, release, ok = f.s.lease(f.ctx, []string{"primary", "secondary"})
	if !ok {
		t.Fatal("failed acquisition leaked earlier keys")
	}
	release()
}
func TestMemberIdentityComparisonAndUnknownFields(t *testing.T) {
	f := fixtureNew(t, Options{}, false, nil)
	e := f.r.UpdateSystem(f.ctx, func(_ context.Context, sys *model.System) error {
		for i := range sys.Agents {
			_ = sys.Agents[i].Fields.Set("future_agent", json.RawMessage(`{"opaque":true}`))
			_ = sys.Agents[i].SetJSON("port", []byte(`8082`))
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.s.Configure(f.ctx, "primary", []byte(`{"peer_id":"secondary","vip":"192.0.2.53"}`), "")
	if e != nil {
		t.Fatal(e)
	}
	e = f.r.UpdateSystem(f.ctx, func(_ context.Context, sys *model.System) error {
		for i := range sys.Agents {
			_ = sys.Agents[i].SetJSON("port", []byte(`8082.0`))
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	a, _ := f.s.get(f.ctx, "primary")
	if _, e = f.s.members(f.ctx, a.OxiDNSHAPublication); e != nil {
		t.Fatalf("numeric identity falsely changed: %v", e)
	}
	e = f.r.UpdateSystem(f.ctx, func(_ context.Context, sys *model.System) error {
		for i := range sys.Agents {
			_ = sys.Agents[i].SetJSON("port", []byte(`8082`))
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	last := f.drain(t, 12)
	if last.Status != "succeeded" {
		t.Fatalf("identity journal: %s", last.Message)
	}
	for _, id := range []string{"primary", "secondary"} {
		a, e := f.m.ReadAgent(f.ctx, id, true)
		if e != nil {
			t.Fatal(e)
		}
		extra, _ := a.Fields.Get("future_agent")
		if string(extra) != `{"opaque":true}` {
			t.Fatal("unknown member lost", string(extra))
		}
		jRaw, _ := json.Marshal(a.OxiDNSHAPublication)
		j := parseObject(jRaw)
		if value, ok := j["active_agent_id"]; !ok || string(value) != "null" {
			t.Fatal("completion lost explicit active_agent_id:null", string(jRaw))
		}
	}
}
func TestRawClientRequestsAndMalformedReplies(t *testing.T) {
	f := fixtureNew(t, Options{}, true, nil)
	a, _ := f.s.get(f.ctx, "primary")
	var got []agents.Request
	f.client.setAfter(func(ctx context.Context, r agents.Request, res *agents.Response) error {
		if strings.HasSuffix(r.URL, "/api/oxidns-ha") {
			got = append(got, r)
			res.Body = []byte(`{"runtime":{},"managed":{},"unrelated":"kept"}`)
		}
		return nil
	})
	rt, e := f.s.runtime(f.ctx, a)
	if e != nil || rt == nil {
		t.Fatal("empty runtime object is valid", e)
	}
	m := f.s.member(f.ctx, a, ptr("primary"), "")
	if m.Error != msgPendingManaged || m.Runtime == nil {
		t.Fatal(m.Error)
	}
	for _, r := range got {
		if r.Method != "GET" || len(r.Body) > 0 || len(r.Header.Values("Authorization")) != 1 || r.Header.Get("Authorization") != "Bearer synthetic-primary-token" {
			t.Fatal("raw GET", r)
		}
	}
	f.client.setAfter(func(ctx context.Context, r agents.Request, res *agents.Response) error {
		res.StatusCode = 409
		res.Body = []byte(`{"error":"synthetic-primary-token/synthetic-secondary-token"}`)
		return nil
	})
	_, e = f.s.call(f.ctx, a, "PUT", "", rawObject(spec(vip, 53, "primary", a, a)), 90)
	if e == nil || strings.Contains(e.Error(), "synthetic-") {
		t.Fatal("device error exposed credentials", e)
	}
	var he *Error
	if !errors.As(e, &he) {
		t.Fatal("write error lost refusal type")
	}
}
func TestUpstreamAndLoopbackBlockBeforeDeviceCalls(t *testing.T) {
	f := fixtureNew(t, Options{}, false, nil)
	_, e := f.r.UpdateProfile(f.ctx, "default", func(_ context.Context, p *model.Profile) error {
		raw, _ := json.Marshal(p.OxiDNS)
		o := parseObject(raw)
		if o == nil {
			o = object{}
		}
		o["native_config"] = rawObject(`plugins:
  - tag: forward_remote
    type: forward
    args:
      upstreams: [{addr: udp://127.0.0.1:1053}]
`)
		return p.SetJSON("oxidns", rawObject(o))
	})
	if e != nil {
		t.Fatal(e)
	}
	before := len(f.client.calls())
	_, e = f.s.Configure(f.ctx, "primary", []byte(`{"peer_id":"secondary","vip":"192.0.2.53"}`), "")
	if e == nil || len(f.client.calls()) != before {
		t.Fatal("loopback findings called devices", e)
	}
	var he *Error
	if !errors.As(e, &he) || len(he.Blockers) != 1 {
		t.Fatal(e)
	}
}
func TestPayloadResumeKeepsPythonPresenceAndUnknownFields(t *testing.T) {
	f := fixtureNew(t, Options{}, false, nil)
	_, e := f.s.Configure(f.ctx, "primary", []byte(`{"peer_id":"secondary","vip":"192.0.2.53"}`), "")
	if e != nil {
		t.Fatal(e)
	}
	a, _ := f.s.get(f.ctx, "primary")
	j := a.OxiDNSHAPublication
	p, _, e := f.s.readPayload(f.ctx, j)
	if e != nil {
		t.Fatal(e)
	}
	var copy model.HAJournal
	if e = json.Unmarshal(journalBytes(j), &copy); e != nil {
		t.Fatal(e)
	}
	_ = copy.Fields.Set("future_journal", json.RawMessage(`{"opaque":true}`))
	if e = f.s.save(f.ctx, &copy); e != nil {
		t.Fatal(e)
	}
	if p.ID != j.ID || copy.HandoffProtocol == nil {
		t.Fatal("Python journal lost fields")
	}
	last := f.drain(t, 12)
	if last.Status != "succeeded" {
		t.Fatalf("resume %s", last.Message)
	}
	a, _ = f.s.get(f.ctx, "primary")
	extra, _ := a.OxiDNSHAPublication.Fields.Get("future_journal")
	if string(extra) != `{"opaque":true}` {
		t.Fatal("journal extra lost")
	}
}
func TestFinalGenerationRequiresMatchingPeerAndPinsBothIdentities(t *testing.T) {
	f := fixtureNew(t, Options{}, true, nil)
	req := deploy.GenerateRequest{Body: []byte(`{"expected_profile_id":"default","expected_service_type":"oxidns","base_url":"https://panel.example"}`), HostURL: "https://panel.example"}
	response := deploy.GenerateFinal(f.ctx, f.m, "primary", req)
	if response.Err != nil || response.Status != 200 {
		t.Fatal(response.Status, response.Err, response.Body)
	}
	raw := parseObject(rawObject(response.Body))
	ids := raw.strings("target_ids")
	if !reflect.DeepEqual(ids, []string{"primary", "secondary"}) {
		t.Fatal(ids)
	}
	calls := f.segment.Calls()
	n := 0
	for _, c := range calls {
		if c.Path == "/api/config/final-preview" {
			n++
		}
	}
	if n != 2 || len(f.uploads()) != 0 {
		t.Fatal("generation omitted peer or deployed", n)
	}
	f.client.setAfter(func(ctx context.Context, r agents.Request, res *agents.Response) error {
		if strings.Contains(r.URL, secondaryHost) && strings.HasSuffix(r.URL, "/api/capabilities") {
			o := parseObject(res.Body)
			o["config_path"] = json.RawMessage(`"other.yaml"`)
			res.Body = rawObject(o)
		}
		return nil
	})
	response = deploy.GenerateFinal(f.ctx, f.m, "primary", req)
	if response.Status != 400 {
		t.Fatal("different peer target allowed", response.Status)
	}
}
func TestAffectedRoutersAcrossProfiles(t *testing.T) {
	f := fixtureNew(t, Options{}, false, nil)
	_, e := f.r.UpdateProfile(f.ctx, "default", func(_ context.Context, p *model.Profile) error {
		p.Routers.Items = []model.Router{
			{ID: "router-example", DNSAgentID: "secondary"},
			{ID: "other", DNSAgentID: "unrelated"},
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	v, e := f.s.Configure(f.ctx, "primary", []byte(`{"peer_id":"secondary","vip":"192.0.2.53"}`), "")
	if e != nil {
		t.Fatal(e)
	}
	if len(v.Routers) != 1 || v.Routers[0].ID != "router-example" || v.Routers[0].Name != "router-example" || v.Routers[0].ProfileID != "default" {
		t.Fatal(v.Routers)
	}
	last := f.drain(t, 12)
	if len(last.Routers) != 1 || last.Routers[0].ID != "router-example" {
		t.Fatal("journal lost affected routers")
	}
}
