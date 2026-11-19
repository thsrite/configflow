package agents

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"configflow/internal/model"
	"configflow/internal/store"
)

func TestTypedEnrollmentExactInstallationReplayKeepsOwnership(t *testing.T) {
	e, id := setupRuntime(t)
	if err := e.m.EditAgents(e.ctx, "", func(_ context.Context, records *[]model.Agent) error {
		a := registryAgent(*records, id)
		return a.SetJSON("gateway_installation_id", []byte(`"installation-proof"`))
	}); err != nil {
		t.Fatal(err)
	}
	a, err := e.m.ReadAgent(e.ctx, id, true)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"name": a.Name, "host": a.Host, "port": a.Port, "service_type": a.ServiceType, "deployment_method": a.DeploymentMethod, "profile_id": a.ProfileID})
	if err != nil {
		t.Fatal(err)
	}
	input, err := DecodeRegistrationInput(data)
	if err != nil {
		t.Fatal(err)
	}
	result, err := e.m.RegisterIdentity(e.ctx, input, RegistrationOptions{InstallationID: "installation-proof", ReplayInstallation: true})
	if err != nil || result.ID != id || result.IsNew || result.Token == nil || *result.Token != a.Token {
		t.Fatal(result, err)
	}
	after, err := e.m.ReadAgent(e.ctx, id, true)
	if err != nil || after.Token != a.Token || after.GatewayInstallationID == nil || *after.GatewayInstallationID != "installation-proof" {
		t.Fatal(after, err)
	}
}

func TestTypedRebindRequiresOriginalStringIdentity(t *testing.T) {
	for _, tt := range []struct {
		raw      string
		accepted bool
	}{
		{`{"id":"7","name":"device","profile_id":"7","service_type":"mihomo","port":8080}`, true},
		{`{"id":7,"name":"device","profile_id":"7","service_type":"mihomo","port":8080}`, false},
		{`{"id":"7","name":"device","profile_id":7,"service_type":"mihomo","port":8080}`, false},
	} {
		t.Run(tt.raw, func(t *testing.T) {
			e, _ := setupRuntime(t)
			e.createProfile("7")
			e.createProfile("target")
			// Import raw SQLite rows, as an older writer could have stored them.
			// The normal write validator correctly refuses malformed identities;
			// passing through it would fail to exercise the R5 read boundary.
			source := store.New(filepath.Join(e.repo.DataDir(), store.Filename), func() time.Duration { return time.Second })
			base, err := source.Load()
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for i := range base.Document.System.Fields {
				if base.Document.System.Fields[i].Name == "agents" {
					base.Document.System.Fields[i].Value = json.RawMessage("[" + tt.raw + "]")
					found = true
				}
			}
			if !found {
				t.Fatal("fixture has no Agent row")
			}
			if _, changed, err := source.Commit(base, base.Document, nil); err != nil || !changed {
				t.Fatal(changed, err)
			}
			head, _ := e.repo.Head(e.ctx)
			result, err := e.m.RebindProfiles(e.ctx, "7", "target", []string{"7"})
			if tt.accepted {
				if err != nil || result == nil || len(result.Agents) != 1 {
					t.Fatal(result, err)
				}
			} else {
				if err == nil {
					t.Fatal("projected identity authorized rebind")
				}
				if after, _ := e.repo.Head(e.ctx); after != head {
					t.Fatal("rejected identity committed")
				}
			}
		})
	}
}
