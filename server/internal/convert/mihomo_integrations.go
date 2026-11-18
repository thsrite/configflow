package convert

import (
	"bytes"
	"net/netip"
	"sync"

	"configflow/internal/model"
	"configflow/internal/yamlx"
)

// MihomoIntegrations are typed collaborators owned by gateway/router modules.
// Missing required integrations fail closed; absent feature settings are no-ops.
type MihomoIntegrations struct {
	ApplyGateway       func(*yamlx.Node, *model.Config, string) error
	GatewayEnabled     func(*model.Config) (bool, error)
	FakeIPNetworkError func(string) error
	DNSLoop            func(*yamlx.Node, []netip.Addr) (string, string, bool, error)
	FullTun            func(*model.Config) (bool, error)
}

var integrationState struct {
	sync.RWMutex
	value MihomoIntegrations
}

// SetMihomoIntegrations installs nonnil collaborators and returns a restoration.
func SetMihomoIntegrations(hooks MihomoIntegrations) func() {
	integrationState.Lock()
	previous := integrationState.value
	if hooks.ApplyGateway != nil {
		integrationState.value.ApplyGateway = hooks.ApplyGateway
	}
	if hooks.GatewayEnabled != nil {
		integrationState.value.GatewayEnabled = hooks.GatewayEnabled
	}
	if hooks.FakeIPNetworkError != nil {
		integrationState.value.FakeIPNetworkError = hooks.FakeIPNetworkError
	}
	if hooks.DNSLoop != nil {
		integrationState.value.DNSLoop = hooks.DNSLoop
	}
	if hooks.FullTun != nil {
		integrationState.value.FullTun = hooks.FullTun
	}
	integrationState.Unlock()
	return func() { integrationState.Lock(); integrationState.value = previous; integrationState.Unlock() }
}
func currentIntegrations() MihomoIntegrations {
	integrationState.RLock()
	defer integrationState.RUnlock()
	return integrationState.value
}
func gatewaySetting(config *model.Config) (present, marker bool) {
	if config == nil || config.Profile == nil {
		return false, false
	}
	raw, ok := config.Profile.Mihomo.Fields.Get("linux_gateway")
	present = ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
	marker = config.Profile.Mihomo.Fields.Has("router_split")
	return
}
func gatewayActive(config *model.Config) (bool, error) {
	if hook := currentIntegrations().GatewayEnabled; hook != nil {
		return hook(config)
	}
	present, _ := gatewaySetting(config)
	if !present {
		return false, nil
	}
	return false, &HookMissingError{Hook: "gateway_settings", Provider: gatewayProvider}
}
func fullTunActive(config *model.Config) (bool, error) {
	if hook := currentIntegrations().FullTun; hook != nil {
		return hook(config)
	}
	_, marker := gatewaySetting(config)
	if !marker {
		return false, nil
	}
	return false, &HookMissingError{Hook: "is_full_tun", Provider: routerProvider}
}
func applyGateway(document *yamlx.Node, config *model.Config, baseURL string) error {
	if hook := currentIntegrations().ApplyGateway; hook != nil {
		return hook(document, config, baseURL)
	}
	present, marker := gatewaySetting(config)
	if !present && !marker {
		return nil
	}
	return &HookMissingError{Hook: "apply_linux_gateway", Provider: gatewayProvider + " + " + routerProvider}
}
