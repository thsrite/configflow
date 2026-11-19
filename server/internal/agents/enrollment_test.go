package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo"
)

func TestTypedEnrollmentRequiresOriginalStoredOwnership(t *testing.T) {
	for _, tt := range []struct {
		field, original string
		replay          bool
	}{
		{"token", `7`, false}, {"token", `null`, false},
		{"gateway_installation_id", `7`, true}, {"gateway_installation_id", `null`, true},
	} {
		t.Run(tt.field+"_"+tt.original, func(t *testing.T) {
			e, id := setupRuntime(t)
			if err := e.m.EditAgents(e.ctx, "", func(_ context.Context, records *[]model.Agent) error {
				return registryAgent(*records, id).SetJSON(tt.field, []byte(tt.original))
			}); err != nil {
				t.Fatal(err)
			}
			head, _ := e.repo.Head(e.ctx)
			input, err := DecodeRegistrationInput([]byte(`{"name":"edge","host":"10.0.0.8","port":8080,"service_type":"mihomo","profile_id":"default","deployment_method":"shell"}`))
			if err != nil {
				t.Fatal(err)
			}
			// setupRuntime's identity is taken from the original stored JSON, so
			// this test never depends on its projected string convenience fields.
			a, err := e.m.ReadAgent(e.ctx, id, true)
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"name", "host", "port", "service_type", "profile_id", "deployment_method"} {
				raw, present, err := a.MemberJSON(key)
				if err != nil {
					t.Fatal(err)
				}
				if present {
					if err := input.record.SetJSON(key, raw); err != nil {
						t.Fatal(err)
					}
				}
			}
			_, err = e.m.RegisterIdentity(e.ctx, input, RegistrationOptions{ExistingToken: "7", InstallationID: "7", ReplayInstallation: tt.replay, Authorization: &RegistrationAuthorization{}})
			if !errors.Is(err, ErrRegistrationAuthorization) {
				t.Fatal(err)
			}
			if after, _ := e.repo.Head(e.ctx); after != head {
				t.Fatal("invalid ownership mutated registry")
			}
		})
	}
}

func TestTypedEnrollmentKeepsUnchangedStoredTokens(t *testing.T) {
	e, id := setupRuntime(t)
	if err := e.m.EditAgents(e.ctx, "", func(_ context.Context, records *[]model.Agent) error {
		a := registryAgent(*records, id)
		for _, tt := range []struct{ name, raw string }{
			{"future", `{"number":2.00}`}, {"config_version", `7`}, {"traffic_history_enabled", `"false"`},
			{"deployments", `{"old":{"status":"succeeded","future":3e0}}`},
		} {
			if tt.name == "future" {
				if err := a.Fields.Set(tt.name, []byte(tt.raw)); err != nil {
					return err
				}
			} else if err := a.SetJSON(tt.name, []byte(tt.raw)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	a, err := e.m.ReadAgent(e.ctx, id, true)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"name": a.Name, "host": a.Host, "port": a.Port, "service_type": a.ServiceType, "version": "2.0.0", "gateway_installation_id": "forged"})
	if err != nil {
		t.Fatal(err)
	}
	input, err := DecodeRegistrationInput(data)
	if err != nil {
		t.Fatal(err)
	}
	result, err := e.m.RegisterIdentity(e.ctx, input, RegistrationOptions{ExistingToken: a.Token})
	if err != nil || result.IsNew || result.Token != nil {
		t.Fatal(result, err)
	}
	a, err = e.m.ReadAgent(e.ctx, id, true)
	if err != nil {
		t.Fatal(err)
	}
	// Python's register_agent drops unknown members; see
	// TestReviewN810ReregistrationKeepsOnlyPythonMembers.
	if _, present, err := a.MemberJSON("future"); err != nil || present {
		t.Fatal("re-registration kept an unknown member", err)
	}
	for _, tt := range []struct{ key, expected string }{
		{"config_version", `7`}, {"traffic_history_enabled", `"false"`}, {"deployments", `{"old":{"status":"succeeded","future":3e0}}`},
	} {
		raw, present, err := a.MemberJSON(tt.key)
		if err != nil || !present || !bytes.Equal(raw, []byte(tt.expected)) {
			t.Fatalf("key=%s raw=%s expected=%s err=%v", tt.key, raw, tt.expected, err)
		}
	}
	if a.GatewayInstallationID != nil {
		t.Fatal("request installed a trusted marker")
	}
}

func TestTypedEnrollmentRejectsNonTextProfileBindings(t *testing.T) {
	for _, raw := range []string{`false`, `0`, `[]`, `{}`, `true`, `7`} {
		if _, err := DecodeRegistrationInput([]byte(`{"profile_id":` + raw + `}`)); !errors.Is(err, repo.ErrValidation) {
			t.Fatalf("raw=%s err=%v", raw, err)
		}
	}
	for _, raw := range []string{`null`, `""`, `"default"`} {
		if _, err := DecodeRegistrationInput([]byte(`{"profile_id":` + raw + `}`)); err != nil {
			t.Fatalf("raw=%s err=%v", raw, err)
		}
	}
}

func TestRegistrationSessionPortUsesOriginalNumericType(t *testing.T) {
	for _, tt := range []struct {
		raw   string
		valid bool
	}{
		{`8080`, true}, {`8080.0`, true}, {`8.08e3`, true}, {`"8080"`, false}, {`true`, false}, {`null`, false},
	} {
		var session model.AgentRegistrationSession
		if err := json.Unmarshal([]byte(`{"hash":"synthetic","port":`+tt.raw+`,"expires":9999999999}`), &session); err != nil {
			t.Fatal(err)
		}
		if got := registrationSessionPortMatches(session, 8080); got != tt.valid {
			t.Fatalf("raw=%s valid=%v want=%v", tt.raw, got, tt.valid)
		}
	}
}
