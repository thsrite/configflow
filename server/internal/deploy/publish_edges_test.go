package deploy

import (
	"configflow/internal/agents"
	"configflow/internal/agents/agentfake"
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
		r, e := New(Options{}).PublishDirect(f.ctx, f.m, "publish-agent", b, PublishOptions{Activate: false})
		if e != nil || resultView(fixtureTask(r)).HTTPStatus == 409 {
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
	dep, _ := f.agent(t).Deployments.Get(m.DeploymentID)
	if dep.PreparedContent != nil && dep.PreparedContent.Files != nil {
		if _, ok := dep.PreparedContent.Files.Get("geoip.metadb"); ok {
			t.Fatal("resource entered comparison identity")
		}
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
	f := newPublish(t, agentfake.Options{ServiceType: "oxidns"}, nil)
	b := f.bundle(t, "checked")
	r, e := New(Options{}).PublishDirect(f.ctx, f.m, "publish-agent", b, PublishOptions{SourceSnapshot: f.source})
	if e != nil || resultView(fixtureTask(r)).Status != "verifying" {
		t.Fatal(r, e)
	}
	a := f.agent(t)
	state, ok := a.Deployments.Get("checked")
	if !ok || !ReceiptMatches(f.m.Repository(), a, &state) {
		t.Fatal("trusted validation receipt missing")
	}
	if a.AppliedContent != nil {
		t.Fatal("staging advanced baseline")
	}
	_, e = f.m.GetDeploymentModel(f.ctx, "publish-agent", "checked")
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.m.ActivateCandidateModel(f.ctx, "publish-agent", "checked")
	if e != nil || resultView(fixtureTask(r)).Status != "succeeded" {
		t.Fatal(r, e)
	}
	if f.agent(t).AppliedContent == nil || textValue(f.agent(t).AppliedContent.DeployedConfigSHA256) == "" {
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
			record := map[string]any{"id": "a"}
			if tc.enabled {
				record["oxidns_ha"] = map[string]any{"enabled": true}
			}
			if tc.status != "" {
				record[agents.HAField] = map[string]any{"id": "pair", "status": tc.status}
			}
			if tc.summary {
				record["latest_deployment"] = map[string]any{"ha_publication": map[string]any{"status": "succeeded"}}
			}
			if got := pairRequired(testTarget(t, record)); got != tc.want {
				t.Fatal(got, tc.want)
			}
		})
	}
}
