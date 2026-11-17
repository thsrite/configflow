package deploy

import (
	"configflow/internal/agents"
	"configflow/internal/agents/agentfake"
	"configflow/internal/jsonx"
	"encoding/json"
	"testing"
)

func TestFinalResourceRetryUsesContentIdentity(t *testing.T) {
	f := newPublish(t, agentfake.Options{}, nil)
	base := f.bundle(t, "frozen-resource")
	m := base.Manifest()
	m.FinalConfig = true
	files, e := base.Contents()
	if e != nil {
		t.Fatal(e)
	}
	geo := []byte("synthetic Geo bytes")
	files["geoip.metadb"] = Content{Data: geo, Role: RoleResource}
	m.Files = append(m.Files, File{Path: "geoip.metadb", Size: int64(len(geo)), SHA256: digest(geo), Role: RoleResource})
	b, e := Pack(m, files, base.ConfigVersion(), base.Comparison())
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		r, e := PublishDirect(f.ctx, f.m, "publish-agent", b, PublishOptions{Activate: false})
		if e != nil || resultView(r).HTTPStatus == 409 {
			t.Fatal("identical frozen candidate was rejected", r, e)
		}
	}
	uploads := 0
	for _, call := range f.fake.Calls() {
		if call.Path == "/api/deployments" {
			uploads++
		}
	}
	if uploads != 1 {
		t.Fatal("duplicate upload", uploads)
	}
	prepared := f.agent(t).GetObject("deployments").GetObject(m.DeploymentID).GetObject("prepared_content")
	if prepared.GetObject("files").Has("geoip.metadb") {
		t.Fatal("resource entered comparison identity")
	}
}
func TestCapabilityAbsenceAndMalformedFields(t *testing.T) {
	for _, tc := range []struct {
		raw          string
		nilResources bool
		protocols    int
	}{
		{`{}`, true, 0}, {`{"mihomo_resources":null}`, true, 0}, {`{"mihomo_resources":[]}`, false, 0}, {`{"deployment_protocols":["1",{},1],"mihomo_resources":["geoip.dat",1]}`, false, 1},
	} {
		var c Capabilities
		if e := json.Unmarshal([]byte(tc.raw), &c); e != nil {
			t.Fatal(e)
		}
		if (c.MihomoResources == nil) != tc.nilResources || len(c.DeploymentProtocols) != tc.protocols {
			t.Fatal(tc.raw, c)
		}
	}
}
func TestOxiDNSPublicationAttestsBeforeActivation(t *testing.T) {
	Wire()
	f := newPublish(t, agentfake.Options{ServiceType: "oxidns"}, nil)
	b := f.bundle(t, "checked")
	r, e := PublishDirect(f.ctx, f.m, "publish-agent", b, PublishOptions{SourceSnapshot: f.source})
	if e != nil || resultView(r).Status != "verifying" {
		t.Fatal(r, e)
	}
	a := f.agent(t)
	state := a.GetObject("deployments").GetObject("checked")
	if !ReceiptMatches(a, state) {
		t.Fatal("trusted validation receipt missing")
	}
	if a.Has("applied_content") {
		t.Fatal("staging advanced baseline")
	}
	_, e = f.m.GetDeployment(f.ctx, "publish-agent", "checked")
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.m.ActivateDeployment(f.ctx, "publish-agent", "checked")
	if e != nil || resultView(r).Status != "succeeded" {
		t.Fatal(r, e)
	}
	if f.agent(t).GetObject("applied_content").GetString("deployed_config_sha256") == "" {
		t.Fatal("installed YAML digest missing")
	}
}

func TestPairGateUsesAuthoritativeActiveJournal(t *testing.T) {
	for _, tc := range []struct {
		name, status           string
		enabled, summary, want bool
	}{
		{name: "absent"}, {name: "enabled", enabled: true, want: true}, {name: "active-journal", status: "preparing", want: true}, {name: "completed-journal", status: "succeeded"}, {name: "failed-journal", status: "failed"}, {name: "old-public-summary", summary: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := jsonx.Obj("id", "a")
			if tc.enabled {
				record.Set("oxidns_ha", jsonx.Obj("enabled", true))
			}
			if tc.status != "" {
				record.Set(agents.HAField, jsonx.Obj("id", "pair", "status", tc.status))
			}
			if tc.summary {
				record.Set("latest_deployment", jsonx.Obj("ha_publication", jsonx.Obj("status", "succeeded")))
			}
			if got := pairRequired(NewTarget(record)); got != tc.want {
				t.Fatal(got, tc.want)
			}
		})
	}
}
