package installation

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/jsonx"
)

// A22 review F1: Python compares Agent members with dict.get equality. An Agent
// whose port an administrator saved as text (PUT /api/agents/<id>) must neither
// block another installation's registration nor turn status reads into 500s.
func TestWronglyTypedAgentMembersFollowPythonEquality(t *testing.T) {
	f := newFixture(t)
	_, err := f.s.Repository.UpdateSystemTransaction(f.ctx, func(_ context.Context, c *jsonx.Object) (*jsonx.Object, error) {
		c.Set("agents", append(c.GetList("agents"), jsonx.Obj("id", "agent_legacy", "name", "legacy", "host", "192.0.2.50", "port", "8080", "token", "synthetic-legacy-token", "service_type", "mihomo", "profile_id", "default", "latest_deployment", jsonx.Obj("deployment_id", "x", "http_status", "not-a-number"))))
		return nil, nil
	})
	must(t, err)
	proofs := f.enroll(t) // registers both cores while the unrelated record exists
	mihomoID := text(object(proofs["mihomo"])["id"])
	_, err = f.s.Manager.UpdateAgent(f.ctx, mihomoID, jsonx.Obj("port", "8080"))
	must(t, err)
	view, err := f.s.Get(f.ctx, f.r.ID)
	must(t, err)
	if !flag(object(view)["target_changed"]) {
		t.Fatal("a text port still matched the installation target")
	}
	_, err = f.s.Publish(f.ctx, f.r.ID, raw(map[string]any{}))
	refusal(t, err, 409, msgIdentity)

	// Integral float ports are equal in Python; a non-string service is a 403.
	data := raw(map[string]any{"name": f.r.Name + "-oxidns", "host": f.r.Network.Address, "port": 8082.0, "service_type": "oxidns", "deployment_method": "shell"})
	_, _, err = f.s.RegistrationProfile(f.ctx, f.r.ID, f.r.Token, data, "")
	must(t, err)
	data = raw(map[string]any{"name": f.r.Name + "-oxidns", "host": f.r.Network.Address, "port": 8082, "service_type": []string{"oxidns"}, "deployment_method": "shell"})
	_, _, err = f.s.RegistrationProfile(f.ctx, f.r.ID, f.r.Token, data, "")
	refusal(t, err, 403, msgService)
}

// A22 review F2: Python's enrollment refuses when any other Agent already holds
// the exact name/address target, regardless of list order.
func TestEnrollRefusesEarlierDuplicateTarget(t *testing.T) {
	f := newFixture(t)
	proofs := map[string]json.RawMessage{}
	for _, service := range services {
		o := object(f.register(t, service))
		proofs[service] = raw(map[string]json.RawMessage{"id": o["id"], "token": o["token"]})
	}
	// An administrator edit can give a second record the same name and host;
	// placing it first is the order a last-wins index cannot see.
	_, err := f.s.Repository.UpdateSystemTransaction(f.ctx, func(_ context.Context, c *jsonx.Object) (*jsonx.Object, error) {
		duplicate := jsonx.Obj("id", "agent_duplicate", "name", f.r.Name+"-mihomo", "host", f.r.Network.Address, "port", 9000, "token", "synthetic-duplicate-token", "service_type", "mihomo", "profile_id", "default")
		c.Set("agents", append([]any{duplicate}, c.GetList("agents")...))
		return nil, nil
	})
	must(t, err)
	before := document(t, f)
	record := recordBytes(t, f)
	_, err = f.s.Enroll(f.ctx, f.r.ID, f.r.Token, raw(map[string]any{"agents": proofs}))
	refusal(t, err, 409, msgDuplicate)
	if !bytes.Equal(before, document(t, f)) || !bytes.Equal(record, recordBytes(t, f)) {
		t.Fatal("refused enrollment wrote")
	}
}
