package deploy

import (
	"context"
	"errors"

	"configflow/internal/agents"
	"configflow/internal/model"
)

// ErrPairPublisherMissing refuses HA targets until the pair publisher is installed.
var ErrPairPublisherMissing = errors.New("deploy: DNS HA pair publication is not available (A18)")

// ErrIPv6EnrollerMissing reports a Service whose publication IPv6 enrollment
// was never installed (Validate only; publication itself skips enrollment).
var ErrIPv6EnrollerMissing = errors.New("deploy: IPv6 publication enrollment is not installed (A23)")

// SetIPv6Enroll installs publication enrollment on the default service and
// returns a restore function.
func SetIPv6Enroll(fn func(context.Context, *agents.Manager, string, string, *Source) error) func() {
	return Default().setIPv6Enroll(fn)
}

// SetPairPublication installs the native HA publication owner on the default
// service. It takes precedence over the compatibility publisher and never
// invokes both for one request.
func SetPairPublication(fn func(context.Context, *agents.Manager, string, *Bundle, PublishOptions) (*model.Deployment, bool, error)) func() {
	return Default().setPairPublication(fn)
}
