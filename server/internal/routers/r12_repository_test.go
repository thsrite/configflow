package routers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"configflow/internal/repo"
)

func TestR12ReturnCleanupUsesExplicitProfilePair(t *testing.T) {
	defaultRouter := r10Router()
	defaultRouter.Name = "synthetic default owner"
	defaultRouter.ManageReturnRouting = true
	defaultRouter.ProxyGatewayAgentID = "gateway"
	db := r12Repository(t, defaultRouter, "{}")
	if _, e := db.CreateProfileRecord(t.Context(), repo.ProfileCreation{ID: "other", Name: "Synthetic other"}, ""); e != nil {
		t.Fatal(e)
	}
	other := r10Router()
	other.Name = "synthetic other owner"
	other.ProxyGatewayAgentID = "gateway"
	if _, _, e := db.SaveProfileFieldsJSON(t.Context(), "other", mustPolicyJSON(map[string]any{"routers": map[string]any{"items": []*Router{other}}}), nil); e != nil {
		t.Fatal(e)
	}
	a := r12Gateway()
	a.Status = "online"
	status := r12SafeStatus()
	status.Managed = &ReturnManaged{Active: true, TablePresent: true, Spec: json.RawMessage(`{"router_ip":"192.0.2.1","table":104,"priority":104,"mark":"0x400000","safety_version":2}`)}
	source := r12Source{gateway: a, client: &r12Agent{s: status}}
	cleanup, e := returnCleanup(t.Context(), InspectRequest{ProfileID: "other", Repo: db, Agents: source}, other, a)
	if e != nil || cleanup == nil || len(cleanup.SharedWith) != 1 || cleanup.SharedWith[0] != defaultRouter.Name {
		t.Fatalf("implicit profile exclusion returned %+v %v", cleanup, e)
	}
}
func TestR12PublicationThreeStatesAndConnectionCAS(t *testing.T) {
	r := r10Router()
	r.Endpoint = "http://192.0.2.1"
	r.Extra["future"] = json.RawMessage(`{"large":9007199254740993,"keep":null}`)
	r.Extra["dns_guard_state"] = json.RawMessage(`null`)
	db := r12Repository(t, r, "{}")
	_, r, before := r12Config(t, db)
	expected := mustPolicyJSON(r)
	proof := json.RawMessage(`{"version":1,"resources":[],"spec":null,"future":{"keep":true}}`)
	if e := db.SaveRouterPublicationJSON(t.Context(), "default", expected, repo.RouterPublicationJSON{IPv6FakeIP: repo.JSONUpdate{Set: true, Value: proof}}); e != nil {
		t.Fatal(e)
	}
	_, stored, one := r12Config(t, db)
	if !jsonEqual(stored.Extra["ipv6_fakeip_state"], proof) || string(stored.Extra["dns_guard_state"]) != "null" || one.Revision != before.Revision+1 {
		t.Fatal("replace or untouched/null lost")
	}
	if e := db.SaveRouterPublicationJSON(t.Context(), "default", expected, repo.RouterPublicationJSON{LastRelease: repo.JSONUpdate{Set: true, Value: json.RawMessage(`{"ok":false,"message":"synthetic"}`)}}); e != nil {
		t.Fatal(e)
	}
	_, stored, two := r12Config(t, db)
	if !jsonEqual(stored.Extra["ipv6_fakeip_state"], proof) || two.Revision != one.Revision+1 {
		t.Fatal("unset manifest inferred clear")
	}
	if e := db.SaveRouterPublicationJSON(t.Context(), "default", expected, repo.RouterPublicationJSON{IPv6FakeIP: repo.JSONUpdate{Set: true}}); e != nil {
		t.Fatal(e)
	}
	_, stored, three := r12Config(t, db)
	if _, present := stored.Extra["ipv6_fakeip_state"]; present || three.Revision != two.Revision+1 || !jsonEqual(stored.Extra["future"], r.Extra["future"]) {
		t.Fatal("explicit clear or unknown field lost")
	}
	stale := *r
	stale.Endpoint = "http://192.0.2.9"
	if e := db.SaveRouterPublicationJSON(t.Context(), "default", mustPolicyJSON(stale), repo.RouterPublicationJSON{IPv6Routes: repo.JSONUpdate{Set: true, Value: proof}}); !errors.Is(e, repo.ErrInUse) {
		t.Fatal("stale connection accepted", e)
	}
	_, _, last := r12Config(t, db)
	if last.Revision != three.Revision {
		t.Fatal("failed CAS committed")
	}
}

func TestR12LegacyRouterPublicationPreservesAbsentDefaults(t *testing.T) {
	raw := []byte(`{"mihomo":{"custom_config":"{}"},"routers":{"items":[{"id":"synthetic","type":"routeros","endpoint":"http://192.0.2.1","username":"synthetic-user","password":"synthetic-password"}]}}`)
	db, e := repo.Open(t.TempDir(), repo.Options{})
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = db.SaveProfileFieldsJSON(t.Context(), "default", raw, nil); e != nil {
		t.Fatal(e)
	}
	_, r, before := r12Config(t, db)
	expected, e := releaseRouterRecord(r)
	if e != nil {
		t.Fatal(e)
	}
	var stored map[string]json.RawMessage
	if e = json.Unmarshal(expected, &stored); e != nil {
		t.Fatal(e)
	}
	if _, present := stored["verify_tls"]; present {
		t.Fatal("invented default in connection CAS")
	}
	request, cancel := context.WithCancel(t.Context())
	cancel()
	if e = db.SaveRouterPublicationJSON(request, "default", expected, repo.RouterPublicationJSON{LastRelease: repo.JSONUpdate{Set: true, Value: json.RawMessage(`{"ok":true}`)}}); e != nil {
		t.Fatal("request disconnect or absent defaults rejected publication", e)
	}
	_, _, after := r12Config(t, db)
	if after.Revision != before.Revision+1 {
		t.Fatal("publication did not commit once")
	}
	withDefault := *r
	withDefault.source = nil
	if sameReleaseRouter(r, &withDefault) {
		t.Fatal("whole-record comparison ignored missing vs explicit default")
	}
}
