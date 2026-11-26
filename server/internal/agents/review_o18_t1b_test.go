package agents

import (
	"errors"
	"testing"

	"configflow/internal/model"
)

// HR O18 T1b L2: a registration session whose stored host had another type
// (read as model.DamagedText) cannot be used, even by a claim reporting that
// text as its host; an intact host-less session still admits.
func TestReviewO18T1bDamagedSessionHostRefused(t *testing.T) {
	e, _ := setupRuntime(t)
	identity := model.AgentRegistrationSession{Name: "gw-mihomo", Port: 9090, ServiceType: "mihomo", DeploymentMethod: "docker"}
	tokens, err := e.m.CreateRegistrationCredentials(e.ctx, []model.AgentRegistrationSession{identity})
	if err != nil {
		t.Fatal(err)
	}
	token := tokens["mihomo"]
	system := func(host string) *model.System {
		var current *model.System
		e.editSystem(func(s *model.System) {
			for i := range s.RegistrationSessions {
				s.RegistrationSessions[i].Host = host
			}
			copied := *s
			copied.RegistrationSessions = append([]model.AgentRegistrationSession(nil), s.RegistrationSessions...)
			current = &copied
		})
		return current
	}
	claim := func(host string) registrationClaim {
		return registrationClaim{Name: identity.Name, Host: host, ServiceType: "mihomo", DeploymentMethod: "docker", Port: 9090}
	}
	admission := RegistrationAuthorization{Credential: token}
	for _, host := range []string{"192.0.2.5", model.DamagedText} {
		if _, err := e.m.admitRegistrationClaim(e.ctx, system(model.DamagedText), claim(host), admission, nil); !errors.Is(err, ErrRegistrationAuthorization) {
			t.Errorf("damaged session host, claim %q: %v, want ErrRegistrationAuthorization", host, err)
		}
	}
	if _, err := e.m.admitRegistrationClaim(e.ctx, system(""), claim("192.0.2.5"), admission, nil); err != nil {
		t.Fatalf("host-less session refused: %v", err)
	}
}

// HR O18 T1b L4: a damaged (status-less) DNS follow state keeps the Agent
// and its source locked like a run; a missing state does not.
func TestReviewO18T1bDamagedDNSFollowStateLocks(t *testing.T) {
	damaged := model.Agent{ID: "target", DNSFollowState: &model.DNSFollowState{}, DNSFollow: &model.DNSFollowBinding{SourceAgentID: "source"}}
	if !dnsFollowBusyModel([]model.Agent{damaged}, "target") || !dnsFollowBusyModel([]model.Agent{damaged}, "source") {
		t.Error("damaged state does not lock target and source")
	}
	if followOwnsDeployment(&damaged, "any") || !followUsesSource(&damaged, "source") {
		t.Error("damaged state does not hold deployments and source")
	}
	idle := model.Agent{ID: "target", DNSFollow: &model.DNSFollowBinding{SourceAgentID: "source"}}
	if dnsFollowBusyModel([]model.Agent{idle}, "target") || !followOwnsDeployment(&idle, "any") || followUsesSource(&idle, "source") {
		t.Error("missing state locks")
	}
}
