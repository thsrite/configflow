package setup

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"configflow/internal/repo"
	"configflow/internal/routers"
	"configflow/internal/yamlx"
)

// DNSOwner supplies A21's pure DNS helpers and defaults. The adapter supplies
// the reviewed DNS-loop checker; documents are independently owned temporaries.
type DNSOwner struct {
	LocalUpstreams   func(string) (*yamlx.Node, error)
	PrepareMihomoDNS func(*yamlx.Node) (*yamlx.Node, error)
	DefaultLocalDNS  string
	DNSTarget        string
	Nameservers      func() []string
}

func (d *DNSOwner) ready() bool {
	return d != nil && d.LocalUpstreams != nil && d.PrepareMihomoDNS != nil && d.DefaultLocalDNS != "" && d.DNSTarget != "" && d.Nameservers != nil
}

// InstalledAgent is the installation owner's freshness-checked public identity.
type InstalledAgent struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Host    string `json:"host"`
	Status  string `json:"status"`
	Enabled *bool  `json:"enabled,omitempty"`
}

// AgentPair is the two services confirmed by A22 for this profile.
type AgentPair struct {
	Mihomo InstalledAgent `json:"mihomo"`
	OxiDNS InstalledAgent `json:"oxidns"`
}

// InstallationState is real A22 publication evidence, not wizard state.
type InstallationState struct {
	ID            string    `json:"id"`
	Phase         string    `json:"phase"`
	ConfigChanged bool      `json:"config_changed"`
	TargetChanged bool      `json:"target_changed"`
	Agents        AgentPair `json:"agents"`
}

// ForwardingStatus is the host owner's IPv4 forwarding evidence.
type ForwardingStatus struct {
	Current   string `json:"current"`
	Persisted *bool  `json:"persisted"`
}

// TUNStatus contains installed Mihomo TUN observations.
type TUNStatus struct {
	Configured    *bool  `json:"configured"`
	Present       *bool  `json:"present"`
	GatewayMarker bool   `json:"gateway_marker"`
	Device        string `json:"device"`
}

// ManagedStatus is the existing gateway owner's journal evidence.
type ManagedStatus struct {
	Active  bool   `json:"active"`
	Pending bool   `json:"pending"`
	Healthy *bool  `json:"healthy"`
	Error   string `json:"error"`
}

// GatewayStatus is a validated reply supplied by the gateway owner.
type GatewayStatus struct {
	IPv4Forwarding  *ForwardingStatus `json:"ipv4_forwarding"`
	TUN             *TUNStatus        `json:"tun"`
	Managed         ManagedStatus     `json:"managed"`
	EnabledInConfig bool              `json:"enabled_in_config"`
}

// InstallationOwner supplies A22 state and the existing gateway status read.
// A nil Latest result means no installation; a nil callback means unavailable.
type InstallationOwner struct {
	Latest          func(context.Context, *repo.Repository, string) (*InstallationState, error)
	HasInstallation func(context.Context, *repo.Repository, string) (bool, error)
	GatewayStatus   func(context.Context, *repo.Repository, string, string) (GatewayStatus, error)
}

// RouterVerification is the router owner's read-only inspection/plan result:
// the plan's blockers and whether publishing it would change nothing.
type RouterVerification struct {
	Blockers []string
	Empty    bool
}

// RouteReadOptions carries the wizard's shrinking download budget to C32.
type RouteReadOptions struct {
	AllowNetwork   bool
	ConnectTimeout time.Duration
	ReadTimeout    time.Duration
}

// ExternalOwners connects real shared owners. Missing callbacks fail closed503;
// there is no substitute installation or router state.
type ExternalOwners struct {
	DNS              *DNSOwner
	Installation     *InstallationOwner
	ReadRouteContent func(context.Context, json.RawMessage, routers.RuleSet, *routers.LibraryRule, RouteReadOptions) (string, error)
	// VerifyRouter inspects the device read-only and plans a release for the
	// setup profile. A device failure is a *Error with status 502.
	VerifyRouter func(ctx context.Context, r *repo.Repository, profileID string, config json.RawMessage, router *routers.Router) (RouterVerification, error)
}

var ownersState struct {
	sync.RWMutex
	value ExternalOwners
}

// SetExternalOwners atomically installs native callbacks and returns a restore
// function. Server wiring joins A21/A22/C32 to this package without import cycles.
func SetExternalOwners(owners ExternalOwners) func() {
	ownersState.Lock()
	previous := ownersState.value
	ownersState.value = copyOwners(owners)
	ownersState.Unlock()
	return func() {
		ownersState.Lock()
		ownersState.value = previous
		ownersState.Unlock()
	}
}

func copyOwners(owners ExternalOwners) ExternalOwners {
	if owners.DNS != nil {
		dns := *owners.DNS
		owners.DNS = &dns
	}
	if owners.Installation != nil {
		installation := *owners.Installation
		owners.Installation = &installation
	}
	return owners
}

// RegisteredOwners returns a copy of the currently installed owners, so the
// server can assert at startup and in tests that every owner is wired.
func RegisteredOwners() ExternalOwners { return externalOwners() }

func externalOwners() ExternalOwners {
	ownersState.RLock()
	defer ownersState.RUnlock()
	return copyOwners(ownersState.value)
}

func unavailable() error { return failure(msgAwaitOwner, 503) }

// RegisterExternalOwners installs only the non-nil owners in a server wiring
// step, retaining callbacks registered by earlier modules. SetExternalOwners is
// available to isolate tests or replace the entire configuration explicitly.
func RegisterExternalOwners(owners ExternalOwners) func() {
	ownersState.Lock()
	previous := ownersState.value
	next := copyOwners(previous)
	if owners.DNS != nil {
		next.DNS = owners.DNS
	}
	if owners.Installation != nil {
		next.Installation = owners.Installation
	}
	if owners.ReadRouteContent != nil {
		next.ReadRouteContent = owners.ReadRouteContent
	}
	if owners.VerifyRouter != nil {
		next.VerifyRouter = owners.VerifyRouter
	}
	ownersState.value = copyOwners(next)
	ownersState.Unlock()
	return func() { ownersState.Lock(); ownersState.value = previous; ownersState.Unlock() }
}

// ownerValidation preserves a pure owner's error chain and the wizard's 400
// boundary. Transport/status callbacks return *Error for their public status.
func ownerValidation(err error) error { return &Error{Message: err.Error(), Status: 400, Cause: err} }
