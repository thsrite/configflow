package upgrades

import (
	"context"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
)

// Wire registers address_follow.follow / confirmed as the registry's
// heartbeat hooks (listed in internal/server wirePackages).
func Wire() {
	agents.RegisterHooks(agents.Hooks{
		AddressFollow: func(ctx context.Context, m *agents.Manager, agent *jsonx.Object, host string) error {
			return Follow(ctx, m, agent, host)
		},
		AddressConfirmed: Confirmed,
	})
}
