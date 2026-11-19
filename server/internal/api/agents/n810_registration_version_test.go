package agents

import (
	"encoding/json"
	"testing"

	core "configflow/internal/agents"
	"configflow/internal/httpx/httpxtest"
)

func TestN810RegistrationVersionValidationFollowsAuthorization(t *testing.T) {
	for _, replacement := range []string{"missing", "null"} {
		t.Run(replacement, func(t *testing.T) {
			s := o9Server(t)
			_, tokens := o9Command(t, s)
			body := o9Body()
			body.Set("version", 7)
			s.Post("/api/agents/register", httpxtest.JSON(body)).WantStatus(403)
			body.Set("registration_token", tokens[0])
			before, err := s.Repo.Store().Fingerprint()
			if err != nil {
				t.Fatal(err)
			}
			response := s.Post("/api/agents/register", httpxtest.JSON(body)).WantStatus(400).Object()
			if response.GetString("message") != core.ErrRegistrationVersion.Error() {
				t.Fatal(response)
			}
			after, err := s.Repo.Store().Fingerprint()
			if err != nil || before != after {
				t.Fatal("invalid version consumed proof or wrote Agent", err)
			}
			if state := mustO9System(t, s); len(state.Agents) != 0 || len(state.RegistrationSessions) != 1 {
				t.Fatal("invalid version consumed proof")
			}
			if replacement == "missing" {
				body.Delete("version")
			} else {
				body.Set("version", nil)
			}
			s.Post("/api/agents/register", httpxtest.JSON(body)).WantStatus(200)
			state := mustO9System(t, s)
			if len(state.Agents) != 1 || len(state.RegistrationSessions) != 0 || state.Agents[0].Version != "1.0.0" {
				t.Fatal("default or proof reuse changed")
			}
		})
	}
}
func TestN810ManualRegistrationUsesSharedVersionValidation(t *testing.T) {
	s := o9Server(t)
	admin := s.Login()
	for _, raw := range []string{`7`, `"invalid version"`, `"１２.0"`} {
		s.Post("/api/agents", admin, httpxtest.Body([]byte(`{"name":"manual","version":`+raw+`}`), "application/json")).WantStatus(400)
	}
	s.Post("/api/agents", admin, httpxtest.Body([]byte(`{"name":"manual","version":null}`), "application/json")).WantStatus(200)
	state := mustO9System(t, s)
	if len(state.Agents) != 1 || state.Agents[0].Version != "1.0.0" {
		t.Fatal("manual version default changed")
	}
	raw, present, err := state.Agents[0].MemberJSON("version")
	if err != nil || !present || string(raw) != string(json.RawMessage(`"1.0.0"`)) {
		t.Fatal("version retained wrong original type")
	}
}
