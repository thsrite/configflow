package agents

import (
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

func TestTypedDeploymentActionsUseOriginalOwnershipMembers(t *testing.T) {
	for _, tt := range []struct {
		field, raw, alias string
		accepted          bool
	}{
		{"profile_id", `"7"`, ``, true},
		{"profile_id", `7`, `"7"`, false},
		{"deployment_id", `"7"`, ``, true},
		{"deployment_id", `7`, `"7"`, false},
		{"service_type", `"mihomo"`, `"forged"`, true},
		{"service_type", `7`, `"mihomo"`, false},
	} {
		t.Run(tt.field+tt.raw, func(t *testing.T) {
			e := newEnv(t)
			e.createProfile("7")
			if err := e.m.EditAgents(e.ctx, "7", func(_ context.Context, records *[]model.Agent) error {
				registryAgent(*records, e.id()).ProfileID = "7"
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			e.begin("7")
			defer e.finish("7")
			if err := e.m.EditAgents(e.ctx, "", func(_ context.Context, records *[]model.Agent) error {
				a := registryAgent(*records, e.id())
				task, _ := a.Deployments.Get("7")
				if err := task.SetJSON(tt.field, []byte(tt.raw)); err != nil {
					return err
				}
				if tt.alias != "" {
					aliases := map[string]string{"profile_id": "Profile_ID", "deployment_id": "Deployment_ID", "service_type": "Service_Type"}
					if err := task.Fields.Set(aliases[tt.field], []byte(tt.alias)); err != nil {
						return err
					}
				}
				a.Deployments.Set("7", task)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			e.fake.requests = nil
			state, err := e.m.ReadDeploymentModel(e.ctx, e.id(), "7")
			if err != nil {
				t.Fatal(err)
			}
			accepted := state.HTTPStatus == nil
			if accepted != tt.accepted {
				t.Fatalf("field=%s raw=%s accepted=%v err=%v", tt.field, tt.raw, accepted, err)
			}
			if len(e.fake.requests) != 0 {
				t.Fatal("unsubmitted task reached device")
			}
		})
	}
}

func TestTypedDeploymentHTTPStatusDoesNotUseScalarProjection(t *testing.T) {
	for _, tt := range []struct {
		raw   string
		match bool
	}{
		{`404`, true}, {`404.0`, true}, {`4.04e2`, true}, {`"404"`, false}, {`true`, false}, {`null`, false}, {`[]`, false},
	} {
		var state model.Deployment
		if err := json.Unmarshal([]byte(`{"http_status":`+tt.raw+`}`), &state); err != nil {
			t.Fatal(err)
		}
		if got := deploymentHTTPIn(&state, 404); got != tt.match {
			t.Fatalf("raw=%s matches=%v", tt.raw, got)
		}
	}
}
