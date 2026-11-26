package convert

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"

	"configflow/internal/model"
	"configflow/internal/subs"
	"configflow/internal/yamlx"
)

// CacheReader loads a subscription cache as opaque JSON. Its owner migrates in N8-9.
type CacheReader func(context.Context, string) (json.RawMessage, error)

// TypedCacheReader loads a read-only decoded subscription cache directly from
// its owner without JSON round-tripping. Callers must not mutate the result.
type TypedCacheReader func(context.Context, string) (*subs.SubscriptionCache, error)

// InlineNode pairs a typed source node with its emitted proxy.
type InlineNode struct {
	Node  model.Node
	Proxy *yamlx.Node
}

// HostsResult is the host merger's report and effective merged host graph.
type HostsResult struct{ Report, Merged *yamlx.Node }

// DialerCollaborators supplies typed cache/conversion collaborators to the owner.
type DialerCollaborators struct {
	LoadCache      CacheReader
	LoadCacheTyped TypedCacheReader
	Convert        func(context.Context, model.Node) (*yamlx.Node, error)
	DownloadedText func(context.Context, string) (string, bool)
}

// MihomoEnvironment contains config-domain and temporary N8-9 collaborations.
// Each callback has one implementation in its owning package. Legacy adapters
// only translate its parameters/results; missing callbacks never skip a step.
type MihomoEnvironment struct {
	Clients                    ClientCollaborators
	Converter                  NodeConverter
	ProviderInterval           func(model.MihomoSettings) (int64, error)
	LoadSubscriptionCache      CacheReader
	LoadSubscriptionCacheTyped TypedCacheReader
	// typedCacheFor records the LoadSubscriptionCache paired with
	// LoadSubscriptionCacheTyped. If a caller replaces LoadSubscriptionCache
	// (e.g. a test stub), the typed fast path is bypassed automatically.
	typedCacheFor     CacheReader
	DownloadedText    func(context.Context, string) (string, bool)
	Preflight         func(context.Context, *model.Config, *yamlx.Node) error
	LowerChains       func(*model.Config) (*model.Config, error)
	RenderMembers     func(model.ProxyGroup, *model.Config) (*yamlx.Node, error)
	ProviderHealth    func(*model.ProviderHealthCheck) (*yamlx.Node, error)
	FilterGroups      func(*model.Config, *yamlx.Node) error
	ChainProviders    func(*model.Config, *yamlx.Node) error
	MergeHosts        func(context.Context, *yamlx.Node, *model.Config, []string, []string, []InlineNode, CacheReader) (*HostsResult, error)
	NodeDNS           func(*yamlx.Node, *yamlx.Node, string) (*yamlx.Node, error)
	LogHosts          func(string, *yamlx.Node) error
	DialerGroups      func(context.Context, *yamlx.Node, *model.Config, DialerCollaborators) (*yamlx.Node, error)
	ValidateRules     func(*model.Config, []string, string) error
	ValidateListeners func(*yamlx.Node) error
}

// ErrMihomoEnvironmentAlreadySet rejects a second SetMihomoEnvironment call
// (O17 D2: the process generation environment is configured once at startup).
var ErrMihomoEnvironmentAlreadySet = errors.New("convert: mihomo environment already set")

var environmentState = struct {
	sync.RWMutex
	value MihomoEnvironment
	set   bool
}{value: MihomoEnvironment{Clients: defaultClientCollaborators()}}

// SetMihomoEnvironment installs the process MihomoEnvironment once at startup.
// A second call returns ErrMihomoEnvironmentAlreadySet without modifying state.
func SetMihomoEnvironment(env MihomoEnvironment) error {
	environmentState.Lock()
	defer environmentState.Unlock()
	if environmentState.set {
		return ErrMihomoEnvironmentAlreadySet
	}
	environmentState.value = env
	environmentState.set = true
	return nil
}

// OverrideMihomoEnvironmentForTest replaces the package MihomoEnvironment in
// tests and returns a restore function that resets both value and set state.
func OverrideMihomoEnvironmentForTest(tb interface{ Helper() }, env MihomoEnvironment) func() {
	tb.Helper()
	environmentState.Lock()
	old := environmentState.value
	oldSet := environmentState.set
	environmentState.value = env
	environmentState.set = true
	environmentState.Unlock()
	return func() {
		environmentState.Lock()
		environmentState.value = old
		environmentState.set = oldSet
		environmentState.Unlock()
	}
}

// ResetMihomoEnvironmentForTest clears the set-once latch and environment for
// tests that exercise SetMihomoEnvironment directly, returning a restore func.
func ResetMihomoEnvironmentForTest(tb interface{ Helper() }) func() {
	tb.Helper()
	environmentState.Lock()
	old := environmentState.value
	oldSet := environmentState.set
	environmentState.value = MihomoEnvironment{Clients: defaultClientCollaborators()}
	environmentState.set = false
	environmentState.Unlock()
	return func() {
		environmentState.Lock()
		environmentState.value = old
		environmentState.set = oldSet
		environmentState.Unlock()
	}
}

// CurrentMihomoEnvironment returns a snapshot of the registered collaborators.
func CurrentMihomoEnvironment() MihomoEnvironment {
	environmentState.RLock()
	defer environmentState.RUnlock()
	return environmentState.value
}

// BindTypedSubscriptionCache registers raw and typed readers for the same
// subscription cache owner. Replacing LoadSubscriptionCache later disables
// the typed fast path automatically.
func (e *MihomoEnvironment) BindTypedSubscriptionCache(raw CacheReader, typed TypedCacheReader) {
	e.LoadSubscriptionCache = raw
	e.LoadSubscriptionCacheTyped = typed
	e.typedCacheFor = raw
}

func (e MihomoEnvironment) activeTypedSubscriptionCache() TypedCacheReader {
	if e.LoadSubscriptionCacheTyped == nil {
		return nil
	}
	if e.LoadSubscriptionCache == nil {
		return e.LoadSubscriptionCacheTyped
	}
	if e.typedCacheFor != nil &&
		reflect.ValueOf(e.LoadSubscriptionCache).Pointer() == reflect.ValueOf(e.typedCacheFor).Pointer() {
		return e.LoadSubscriptionCacheTyped
	}
	return nil
}

// CheckGeneration verifies the callbacks required by every generated document.
func (e MihomoEnvironment) CheckGeneration() error {
	checks := []struct {
		name    string
		present bool
	}{
		{"lower_proxy_chains", e.LowerChains != nil}, {"render_members", e.RenderMembers != nil},
		{"provider_health_check", e.ProviderHealth != nil}, {"validate_refresh_interval", e.ProviderInterval != nil},
		{"lower_group_provider_filters", e.FilterGroups != nil}, {"lower_chain_providers", e.ChainProviders != nil},
		{"merge_subscription_hosts", e.MergeHosts != nil}, {"apply_node_dns", e.NodeDNS != nil}, {"log_report", e.LogHosts != nil},
		{"add_dialer_groups", e.DialerGroups != nil}, {"validate_rule_policies", e.ValidateRules != nil}, {"validate_listener_policies", e.ValidateListeners != nil},
	}
	for _, check := range checks {
		if !check.present {
			return &HookMissingError{Hook: check.name, Provider: "convert/mihomoenv"}
		}
	}
	return nil
}
