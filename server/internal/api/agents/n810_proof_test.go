package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	core "configflow/internal/agents"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
)

func TestN810StoredBootstrapIdentityCannotUseProjection(t *testing.T) {
	for _, tt := range []struct{ member, poisoned, restored string }{
		{"name", `7`, `"7"`}, {"port", `"8080"`, `8080`},
	} {
		t.Run(tt.member, func(t *testing.T) {
			s := o9Server(t)
			m, err := core.Get(s.Context())
			if err != nil {
				t.Fatal(err)
			}
			issued, err := m.CreateRegistrationCredentials(s.Context(), []model.AgentRegistrationSession{{Name: "7", Host: "192.0.2.30", Port: 8080, ServiceType: "mihomo", DeploymentMethod: "shell"}})
			if err != nil {
				t.Fatal(err)
			}
			setOriginal := func(original string) {
				t.Helper()
				if err := s.Repo.UpdateSystem(s.Context(), func(_ context.Context, system *model.System) error {
					raw, err := json.Marshal(system.RegistrationSessions[0])
					if err != nil {
						return err
					}
					var members map[string]json.RawMessage
					if err := json.Unmarshal(raw, &members); err != nil {
						return err
					}
					members[tt.member] = json.RawMessage(original)
					raw, err = json.Marshal(members)
					if err != nil {
						return err
					}
					return json.Unmarshal(raw, &system.RegistrationSessions[0])
				}); err != nil {
					t.Fatal(err)
				}
			}
			setOriginal(tt.poisoned)
			body := fmt.Sprintf(`{"name":"7","host":"192.0.2.30","port":8080,"service_type":"mihomo","deployment_method":"shell","registration_token":%q}`, issued["mihomo"])
			s.Post("/api/agents/register", httpxtest.Body([]byte(body), "application/json")).WantStatus(403)
			if stored := mustO9System(t, s); len(stored.Agents) != 0 || len(stored.RegistrationSessions) != 1 {
				t.Fatal("projection proof committed")
			}
			setOriginal(tt.restored)
			s.Post("/api/agents/register", httpxtest.Body([]byte(body), "application/json")).WantStatus(200)
			if stored := mustO9System(t, s); len(stored.Agents) != 1 || len(stored.RegistrationSessions) != 0 {
				t.Fatal("original identity did not atomically consume")
			}
		})
	}
}
