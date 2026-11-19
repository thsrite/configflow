package upgrades

import "configflow/internal/agents"

// Wire registers typed heartbeat address hooks without a JSON facade round trip.
func Wire() {
	agents.RegisterHooks(agents.Hooks{AddressFollowTyped: FollowModel, AddressConfirmedTyped: ConfirmedModel})
}
