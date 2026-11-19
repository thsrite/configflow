package agents

import (
	"fmt"
	"testing"

	"configflow/internal/httpx/httpxtest"
)

func TestN810RegistrationProfileOptionalMachineValues(t *testing.T) {
	s := o9Server(t)
	for i, tt := range []struct {
		raw    string
		status int
	}{
		{`false`, 400}, {`0`, 400}, {`[]`, 400}, {`{}`, 400}, {`true`, 400}, {`7`, 400},
		{`["default"]`, 400}, {`{"value":"default"}`, 400},
		{`null`, 200}, {`""`, 200}, {`"default"`, 200},
	} {
		body := fmt.Sprintf(`{"name":"machine-%d","host":"192.0.2.40","profile_id":%s}`, i, tt.raw)
		result := s.Post("/api/agents/register", s.Login(), httpxtest.Body([]byte(body), "application/json")).WantStatus(tt.status).Object()
		if tt.status == 400 && result.GetString("message") != "Invalid profile id" {
			t.Fatal(result)
		}
	}
	stored := mustO9System(t, s)
	if len(stored.Agents) != 3 {
		t.Fatal("invalid profile created an Agent")
	}
	for _, a := range stored.Agents {
		if a.ProfileID != "default" {
			t.Fatal("optional profile changed binding")
		}
	}
}

func TestN810RegistrationNullProfileConsumesExactProof(t *testing.T) {
	s := o9Server(t)
	_, tokens := o9Command(t, s)
	body := fmt.Sprintf(`{"name":"new-device","host":"192.0.2.30","port":8080,"service_type":"mihomo","deployment_method":"shell","profile_id":null,"registration_token":%q}`, tokens[0])
	s.Post("/api/agents/register", httpxtest.Body([]byte(body), "application/json")).WantStatus(200)
	if stored := mustO9System(t, s); len(stored.RegistrationSessions) != 0 || len(stored.Agents) != 1 || stored.Agents[0].ProfileID != "default" {
		t.Fatal("optional profile lost atomic proof binding")
	}
}
