package deploy

import (
	"configflow/internal/agents"
	"context"
	"errors"
	"sync"
)

// ErrPairPublisherMissing refuses HA targets until the pair publisher is installed.
var ErrPairPublisherMissing = errors.New("deploy: DNS HA pair publication is not available (A18)")

// ErrGatewayGuardMissing refuses a host transition that needs live forwarding evidence.
var ErrGatewayGuardMissing = errors.New("deploy: Linux gateway guards are not available (A20)")

// GatewayGuards verify host forwarding before preparing a TUN or gateway-off delivery.
type GatewayGuards struct {
	RequireForwardingKept func(context.Context, *agents.Manager, Target) error
	RequireGatewayOffKept func(context.Context, *agents.Manager, Target) error
}

var publicationHooks struct {
	sync.RWMutex
	guards GatewayGuards
	pair   func(context.Context, *agents.Manager, string, *Bundle, PublishOptions) (*agents.Result, bool, error)
	ipv6   func(context.Context, *agents.Manager, string, string, *Source) error
}

// SetGatewayGuards installs the host safety checks and returns a restore function.
func SetGatewayGuards(g GatewayGuards) func() {
	publicationHooks.Lock()
	old := publicationHooks.guards
	publicationHooks.guards = g
	publicationHooks.Unlock()
	return func() { publicationHooks.Lock(); publicationHooks.guards = old; publicationHooks.Unlock() }
}

// SetPairPublisher installs the optional HA publisher and returns a restore function.
func SetPairPublisher(fn func(context.Context, *agents.Manager, string, *Bundle, PublishOptions) (*agents.Result, bool, error)) func() {
	publicationHooks.Lock()
	old := publicationHooks.pair
	publicationHooks.pair = fn
	publicationHooks.Unlock()
	return func() { publicationHooks.Lock(); publicationHooks.pair = old; publicationHooks.Unlock() }
}

// SetIPv6Enroll installs publication enrollment and returns a restore function.
func SetIPv6Enroll(fn func(context.Context, *agents.Manager, string, string, *Source) error) func() {
	publicationHooks.Lock()
	old := publicationHooks.ipv6
	publicationHooks.ipv6 = fn
	publicationHooks.Unlock()
	return func() { publicationHooks.Lock(); publicationHooks.ipv6 = old; publicationHooks.Unlock() }
}
