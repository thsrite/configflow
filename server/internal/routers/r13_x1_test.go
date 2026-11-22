package routers

import (
	"encoding/json"
	"testing"

	"configflow/internal/repo"
)

// X1 (R13 review, coordinator 2026-11-18): Router decoding follows the R5
// fallback and routersOf follows Python routers_of, so one malformed stored
// record only affects its own row in R12 and R13.

func TestX1RouterDecodeFallsBackPerMember(t *testing.T) {
	data := []byte(`{"id":"synthetic","name":"kept","type":"routeros","endpoint":"http://192.0.2.1","rule_sync_interval":"sixty","verify_tls":"no","ip_rule_sets":[1],"dns_guard":{"enabled":"yes"},"proxy_gateway_agent_id":"gateway","future":{"keep":true}}`)
	var r Router
	if e := json.Unmarshal(data, &r); e != nil {
		t.Fatal("malformed member failed the record:", e)
	}
	// O18 T1c: ip_rule_sets [1] reads the number as its text but the router
	// keeps identities only (idList); dns_guard.enabled is switch:"off", so
	// "yes" reads as a disabled guard; the undeclared member is dropped.
	if r.ID != "synthetic" || r.Name != "kept" || r.ProxyGatewayAgentID != "gateway" || r.RuleSyncInterval != 60 || !r.VerifyTLS || r.DNSGuard == nil || r.DNSGuard.Enabled {
		t.Fatalf("fallback %+v %+v", r.RouterSettings, r.DNSGuard)
	}
	if _, kept := r.Extra["future"]; kept {
		t.Fatal("undeclared member kept", r.Extra)
	}
	stored, e := releaseRouterRecord(&r)
	if e != nil || string(stored) != string(data) {
		t.Fatalf("stored bytes not kept for CAS: %s %v", stored, e)
	}
	if e := json.Unmarshal([]byte(`{"id":`), &r); e == nil {
		t.Fatal("syntax error accepted")
	}
}

func TestX1RoutersOfLikePython(t *testing.T) {
	tests := []struct {
		name   string
		config string
		ids    []string
	}{
		{"null and non-object items skipped", `{"routers":{"items":[null,"x",1,[],{"id":"a"},{"id":"b","rule_sync_interval":"bad"}]}}`, []string{"a", "b"}},
		{"routers not an object", `{"routers":[{"id":"a"}]}`, nil},
		{"items not a list", `{"routers":{"items":{"id":"a"}}}`, nil},
		{"missing", `{}`, nil},
		{"null routers", `{"routers":null}`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ids []string
			for _, r := range routersOf(json.RawMessage(tt.config)) {
				ids = append(ids, r.ID)
			}
			if len(ids) != len(tt.ids) || (len(ids) > 0 && (ids[0] != tt.ids[0] || ids[len(ids)-1] != tt.ids[len(tt.ids)-1])) {
				t.Fatalf("got %v want %v", ids, tt.ids)
			}
		})
	}
}

// A malformed record next to the published router must not fail its release.
func TestX1PublishWithMalformedNeighbour(t *testing.T) {
	_, db, req, _ := r12ReviewSetup(t, nil)
	good, e := releaseRouterRecord(req.Router)
	if e != nil {
		t.Fatal(e)
	}
	var neighbour map[string]any
	if e = json.Unmarshal(good, &neighbour); e != nil {
		t.Fatal(e)
	}
	neighbour["id"], neighbour["name"], neighbour["rule_sync_interval"], neighbour["proxy_gateway_agent_id"] = "neighbour", "synthetic neighbour", "sixty", ""
	// Input is strict (O18 T1c), so the malformed neighbour is seeded as a stored row.
	seedStoredRow(t, db, "profile", "default", "routers", string(mustPolicyJSON(map[string]any{"items": []any{json.RawMessage(good), neighbour}})))
	outcome := Publish(t.Context(), req)
	if outcome.Err != nil || outcome.Result == nil || !outcome.Result.OK {
		t.Fatalf("publish failed beside a malformed record: %+v", outcome)
	}
}

// A malformed record in another profile must not fail the return-cleanup check.
func TestX1ReturnCleanupWithMalformedProfile(t *testing.T) {
	owner := r10Router()
	owner.Name = "synthetic default owner"
	owner.ManageReturnRouting = true
	owner.ProxyGatewayAgentID = "gateway"
	db := r12Repository(t, owner, "{}")
	if _, e := db.CreateProfileRecord(t.Context(), repo.ProfileCreation{ID: "other", Name: "Synthetic other"}, ""); e != nil {
		t.Fatal(e)
	}
	other := r10Router()
	other.Name = "synthetic other owner"
	other.ProxyGatewayAgentID = "gateway"
	if _, _, e := db.SaveProfileFieldsJSON(t.Context(), "other", mustPolicyJSON(map[string]any{"routers": map[string]any{"items": []*Router{other}}}), nil); e != nil {
		t.Fatal(e)
	}
	if _, e := db.CreateProfileRecord(t.Context(), repo.ProfileCreation{ID: "legacy", Name: "Synthetic legacy"}, ""); e != nil {
		t.Fatal(e)
	}
	bad := map[string]any{"id": "bad", "name": "synthetic legacy owner", "type": "routeros", "endpoint": "http://192.0.2.9", "manage_return_routing": true, "proxy_gateway_agent_id": "gateway", "rule_sync_interval": "sixty"}
	seedStoredRow(t, db, "profile", "legacy", "routers", string(mustPolicyJSON(map[string]any{"items": []any{bad}})))
	a := r12Gateway()
	a.Status = "online"
	status := r12SafeStatus()
	status.Managed = &ReturnManaged{Active: true, TablePresent: true, Spec: json.RawMessage(`{"router_ip":"192.0.2.1","table":104,"priority":104,"mark":"0x400000","safety_version":2}`)}
	source := r12Source{gateway: a, client: &r12Agent{s: status}}
	cleanup, e := returnCleanup(t.Context(), InspectRequest{ProfileID: "other", Repo: db, Agents: source}, other, a)
	if e != nil || cleanup == nil || len(cleanup.SharedWith) != 2 || cleanup.SharedWith[0] != owner.Name || cleanup.SharedWith[1] != "synthetic legacy owner" {
		t.Fatalf("malformed profile broke return cleanup: %+v %v", cleanup, e)
	}
}
