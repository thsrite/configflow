package deploy

import "errors"

// ErrPairPublisherMissing refuses HA targets until the pair publisher is installed.
var ErrPairPublisherMissing = errors.New("deploy: DNS HA pair publication is not available (A18)")

// ErrIPv6EnrollerMissing reports a Service whose publication IPv6 enrollment
// was never installed (Validate only; publication itself skips enrollment).
var ErrIPv6EnrollerMissing = errors.New("deploy: IPv6 publication enrollment is not installed (A23)")
