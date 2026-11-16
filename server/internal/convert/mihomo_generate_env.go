package convert

import (
	"context"
	"net/netip"
	"sync"

	"configflow/internal/jsonx"
)

// Collaborators of the Mihomo generator (C18) that live in packages which
// import convert (refresh, provider, substore, repo) or in other domains
// (agents, routers). Python imported them lazily inside the functions; Go
// needs them injected.
//
//   - MihomoEnv: config-domain collaborators, registered by
//     internal/convert/mihomoenv.Wire (listed in server.wirePackages).
//   - MihomoHooks: functions owned by the agents and routers domains,
//     registered by the module that ports them (decision 3). Each unset hook
//     has a documented default that is exact where the Python function is
//     an exact no-op and fails closed (*HookMissingError) otherwise.

// MihomoEnv holds the config-domain collaborators of generation.
type MihomoEnv struct {
	// Converter is Sub-Store's convert_proxy_string (substore.Converter()).
	Converter ProxyStringConverter
	// ProviderInterval is validate_refresh_interval(
	// mihomo.get('provider_update_interval', DEFAULT_PROVIDER_INTERVAL),
	// 'Mihomo 订阅同步间隔') (C15 refresh) for the profile's `mihomo` object.
	ProviderInterval func(mihomo *jsonx.Object) (int64, error)
	// LoadSubscriptionCache is subscription_cache.load_subscription_cache
	// (C13 substore): nil when there is no cache.
	LoadSubscriptionCache func(ctx context.Context, subID string) (*jsonx.Object, error)
	// DownloadedText is the `text` of the shared download cache of a URL
	// (subscription_dialers.dialer_record's fallback read; C15 refresh's
	// _download_cache_path through the repository); ok false when it cannot
	// be read or holds no text.
	DownloadedText func(ctx context.Context, url string) (string, bool)
	// PrepareProviderBundle is provider_delivery.prepare_provider_bundle(
	// logical_config, mihomo_config) with default options (C17 provider).
	PrepareProviderBundle func(ctx context.Context, config, main *jsonx.Object) error
}

// MihomoHooks are the functions of other domains the generator and the base
// settings checks call. Register them with RegisterMihomoHooks from the
// providing module; a nil field keeps the default described on it.
type MihomoHooks struct {
	// ApplyLinuxGateway is common/linux_gateway.apply_linux_gateway(mihomo,
	// config, base_url), which first calls routers' router_split
	// .check_generation(config). Provided by agents A19/A21 (Linux gateway)
	// together with routers (router_split).
	// Default: a no-op when config.mihomo has no `linux_gateway` (or it is
	// null) and no `router_split` marker — exactly where Python does nothing;
	// otherwise *HookMissingError.
	ApplyLinuxGateway func(mihomo, config *jsonx.Object, baseURL string) error
	// GatewayEnabled is linux_gateway.gateway_settings(config)['enabled']
	// (agents A19/A21); the error is the LinuxGatewayError of a malformed
	// setting. Default: false when config.mihomo has no `linux_gateway` or
	// it is null; otherwise *HookMissingError.
	GatewayEnabled func(config *jsonx.Object) (bool, error)
	// FakeIPNetworkError is linux_gateway.fake_ip_network({'dns':
	// {'fake-ip-range': value}}) for a string value: nil when Python returns,
	// non-nil when it raises LinuxGatewayError (agents A19/A21).
	// Default: *HookMissingError.
	FakeIPNetworkError func(value string) error
	// MihomoDNSLoop is gateway_onboarding.mihomo_dns_loop(dns, host_addresses)
	// (agents, gateway onboarding): the first (key, upstream) that resolves
	// through this host. Default: *HookMissingError.
	MihomoDNSLoop func(dns *jsonx.Object, hostAddresses []netip.Addr) (key string, upstream any, found bool, err error)
	// IsFullTun is router_split.is_full_tun(config) (routers, clients/split).
	// Default: false when config.mihomo has no `router_split` marker;
	// otherwise *HookMissingError.
	IsFullTun func(config *jsonx.Object) (bool, error)
}

// HookMissingError is returned where a hook of another domain is needed but
// not registered yet (fail closed).
type HookMissingError struct{ Hook, Provider string }

func (e *HookMissingError) Error() string {
	return "converters.mihomo: " + e.Hook + " is not available (provided by " + e.Provider + ")"
}

var (
	mihomoMu    sync.RWMutex
	mihomoEnv   MihomoEnv
	mihomoHooks MihomoHooks
)

// SetMihomoEnv installs the config-domain collaborators and returns a
// function that restores the previous ones (for tests).
func SetMihomoEnv(env MihomoEnv) (restore func()) {
	mihomoMu.Lock()
	previous := mihomoEnv
	mihomoEnv = env
	mihomoMu.Unlock()
	return func() {
		mihomoMu.Lock()
		mihomoEnv = previous
		mihomoMu.Unlock()
	}
}

// CurrentMihomoEnv returns the installed collaborators.
func CurrentMihomoEnv() MihomoEnv {
	mihomoMu.RLock()
	defer mihomoMu.RUnlock()
	return mihomoEnv
}

// RegisterMihomoHooks sets the non-nil fields of hooks and returns a function
// that restores the previous hooks (for tests).
func RegisterMihomoHooks(hooks MihomoHooks) (restore func()) {
	mihomoMu.Lock()
	previous := mihomoHooks
	if hooks.ApplyLinuxGateway != nil {
		mihomoHooks.ApplyLinuxGateway = hooks.ApplyLinuxGateway
	}
	if hooks.GatewayEnabled != nil {
		mihomoHooks.GatewayEnabled = hooks.GatewayEnabled
	}
	if hooks.FakeIPNetworkError != nil {
		mihomoHooks.FakeIPNetworkError = hooks.FakeIPNetworkError
	}
	if hooks.MihomoDNSLoop != nil {
		mihomoHooks.MihomoDNSLoop = hooks.MihomoDNSLoop
	}
	if hooks.IsFullTun != nil {
		mihomoHooks.IsFullTun = hooks.IsFullTun
	}
	mihomoMu.Unlock()
	return func() {
		mihomoMu.Lock()
		mihomoHooks = previous
		mihomoMu.Unlock()
	}
}

func currentHooks() MihomoHooks {
	mihomoMu.RLock()
	defer mihomoMu.RUnlock()
	return mihomoHooks
}

const (
	gatewayProvider   = "agents A19/A21 (common/linux_gateway.py)"
	routerProvider    = "routers clients/split (common/router_split.py)"
	onboardingProvide = "agents gateway onboarding (common/gateway_onboarding.py)"
)

// mihomoSettingPresent reports whether config['mihomo'] (a dict) has key
// with a value other than None (linux_gateway) or at all (router_split).
func mihomoSetting(config *jsonx.Object, key string) (value any, present bool) {
	mihomo, ok := config.Value("mihomo").(*jsonx.Object)
	if !ok {
		return nil, false
	}
	return mihomo.Get(key)
}

func applyLinuxGateway(mihomo, config *jsonx.Object, baseURL string) error {
	if hook := currentHooks().ApplyLinuxGateway; hook != nil {
		return hook(mihomo, config, baseURL)
	}
	gateway, hasGateway := mihomoSetting(config, "linux_gateway")
	_, hasMarker := mihomoSetting(config, "router_split")
	if (!hasGateway || gateway == nil) && !hasMarker {
		return nil
	}
	return &HookMissingError{Hook: "apply_linux_gateway", Provider: gatewayProvider + " + " + routerProvider}
}

func gatewayEnabled(config *jsonx.Object) (bool, error) {
	if hook := currentHooks().GatewayEnabled; hook != nil {
		return hook(config)
	}
	gateway, hasGateway := mihomoSetting(config, "linux_gateway")
	if !hasGateway || gateway == nil {
		return false, nil
	}
	return false, &HookMissingError{Hook: "gateway_settings", Provider: gatewayProvider}
}

func isFullTun(config *jsonx.Object) (bool, error) {
	if hook := currentHooks().IsFullTun; hook != nil {
		return hook(config)
	}
	if _, hasMarker := mihomoSetting(config, "router_split"); !hasMarker {
		return false, nil
	}
	return false, &HookMissingError{Hook: "is_full_tun", Provider: routerProvider}
}
