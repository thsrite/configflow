package upgrades

import (
	"context"

	"configflow/internal/agents"
	"configflow/internal/model"
)

// Service binds upgrade and address-follow operations to a single agents.Manager.
type Service struct {
	Manager *agents.Manager
}

// New constructs an upgrades Service for m.
func New(m *agents.Manager) *Service {
	return &Service{Manager: m}
}

// FollowModel updates the Agent's followed host address on heartbeat.
func (s *Service) FollowModel(ctx context.Context, a *model.Agent, reportedHost string) error {
	return FollowModel(ctx, s.Manager, a, reportedHost)
}

// ConfirmedModel returns the confirmed host address if it matches reportedHost.
func (s *Service) ConfirmedModel(a *model.Agent, reportedHost string) string {
	return ConfirmedModel(a, reportedHost)
}
