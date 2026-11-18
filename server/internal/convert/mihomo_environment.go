package convert

import (
	"context"
	"encoding/json"
	"sync"

	"configflow/internal/model"
	"configflow/internal/yamlx"
)

// CacheReader loads a subscription cache as opaque JSON. Its owner migrates in N8-9.
type CacheReader func(context.Context, string) (json.RawMessage, error)

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
	Convert        func(context.Context, model.Node) (*yamlx.Node, error)
	DownloadedText func(context.Context, string) (string, bool)
}

// MihomoEnvironment contains config-domain and temporary N8-9 collaborations.
// Each callback has one implementation in its owning package. Legacy adapters
// only translate its parameters/results; missing callbacks never skip a step.
type MihomoEnvironment struct {
	Converter             NodeConverter
	ProviderInterval      func(model.MihomoSettings) (int64, error)
	LoadSubscriptionCache CacheReader
	DownloadedText        func(context.Context, string) (string, bool)
	Preflight             func(context.Context, *model.Config, *yamlx.Node) error
	LowerChains           func(*model.Config) (*model.Config, error)
	RenderMembers         func(model.ProxyGroup, *model.Config) (*yamlx.Node, error)
	ProviderHealth        func(json.RawMessage) (*yamlx.Node, error)
	FilterGroups          func(*model.Config, *yamlx.Node) error
	ChainProviders        func(*model.Config, *yamlx.Node) error
	MergeHosts            func(context.Context, *yamlx.Node, *model.Config, []string, []string, []InlineNode, CacheReader) (*HostsResult, error)
	NodeDNS               func(*yamlx.Node, *yamlx.Node, string) (*yamlx.Node, error)
	LogHosts              func(string, *yamlx.Node) error
	DialerGroups          func(context.Context, *yamlx.Node, *model.Config, DialerCollaborators) (*yamlx.Node, error)
	ValidateRules         func(*model.Config, []string, string) error
	ValidateListeners     func(*yamlx.Node) error
}

var environmentState struct {
	sync.RWMutex
	value MihomoEnvironment
}

// SetMihomoEnvironment installs a typed environment and returns a restoration.
func SetMihomoEnvironment(env MihomoEnvironment) func() {
	environmentState.Lock()
	old := environmentState.value
	environmentState.value = env
	environmentState.Unlock()
	return func() { environmentState.Lock(); environmentState.value = old; environmentState.Unlock() }
}

// CurrentMihomoEnvironment returns a snapshot of the registered collaborators.
func CurrentMihomoEnvironment() MihomoEnvironment {
	environmentState.RLock()
	defer environmentState.RUnlock()
	return environmentState.value
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
