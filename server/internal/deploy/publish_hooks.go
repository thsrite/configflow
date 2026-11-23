package deploy

import (
	"configflow/internal/agents"
	"configflow/internal/model"
	"context"
	"errors"
	"sync"
)

// ErrPairPublisherMissing refuses HA targets until the pair publisher is installed.
var ErrPairPublisherMissing = errors.New("deploy: DNS HA pair publication is not available (A18)")

var publicationHooks struct {
	sync.RWMutex
	nativePair func(context.Context, *agents.Manager, string, *Bundle, PublishOptions) (*model.Deployment, bool, error)
	ipv6       func(context.Context, *agents.Manager, string, string, *Source) error
}

// SetIPv6Enroll installs publication enrollment and returns a restore function.
func SetIPv6Enroll(fn func(context.Context, *agents.Manager, string, string, *Source) error) func() {
	publicationHooks.Lock()
	old := publicationHooks.ipv6
	publicationHooks.ipv6 = fn
	publicationHooks.Unlock()
	return func() { publicationHooks.Lock(); publicationHooks.ipv6 = old; publicationHooks.Unlock() }
}

// SetPairPublication installs the native HA publication owner. It takes precedence
// over the compatibility publisher and never invokes both for one request.
func SetPairPublication(fn func(context.Context, *agents.Manager, string, *Bundle, PublishOptions) (*model.Deployment, bool, error)) func() {
	publicationHooks.Lock()
	old := publicationHooks.nativePair
	publicationHooks.nativePair = fn
	publicationHooks.Unlock()
	return func() { publicationHooks.Lock(); publicationHooks.nativePair = old; publicationHooks.Unlock() }
}
