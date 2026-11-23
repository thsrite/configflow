package convert

import (
	"bytes"
	"errors"
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

// ErrMihomoIntegrationsAlreadySet rejects a second SetMihomoIntegrations call
// (O17 D2: gateway integrations are configured once at startup).
var ErrMihomoIntegrationsAlreadySet = errors.New("convert: mihomo integrations already set")

var integrationState struct {
	sync.RWMutex
	value MihomoIntegrations
	set   bool
}

// SetMihomoIntegrations installs the process MihomoIntegrations once at startup.
// A second call returns ErrMihomoIntegrationsAlreadySet without modifying state.
func SetMihomoIntegrations(hooks MihomoIntegrations) error {
	integrationState.Lock()
	defer integrationState.Unlock()
	if integrationState.set {
		return ErrMihomoIntegrationsAlreadySet
	}
	applyMihomoIntegrations(&integrationState.value, hooks)
	integrationState.set = true
	return nil
}

// OverrideMihomoIntegrationsForTest replaces non-nil MihomoIntegrations fields
// in tests and returns a restore function that resets both value and set state.
func OverrideMihomoIntegrationsForTest(tb interface{ Helper() }, hooks MihomoIntegrations) func() {
	tb.Helper()
	integrationState.Lock()
	previous := integrationState.value
	previousSet := integrationState.set
	applyMihomoIntegrations(&integrationState.value, hooks)
	integrationState.set = true
	integrationState.Unlock()
	return func() {
		integrationState.Lock()
		integrationState.value = previous
		integrationState.set = previousSet
		integrationState.Unlock()
	}
}

// ResetMihomoIntegrationsForTest clears the set-once latch and integrations for
// tests that exercise SetMihomoIntegrations directly, returning a restore func.
func ResetMihomoIntegrationsForTest(tb interface{ Helper() }) func() {
	tb.Helper()
	integrationState.Lock()
	previous := integrationState.value
	previousSet := integrationState.set
	integrationState.value = MihomoIntegrations{}
	integrationState.set = false
	integrationState.Unlock()
	return func() {
		integrationState.Lock()
		integrationState.value = previous
		integrationState.set = previousSet
		integrationState.Unlock()
	}
}

func applyMihomoIntegrations(dst *MihomoIntegrations, hooks MihomoIntegrations) {
	if hooks.ApplyGateway != nil {
		dst.ApplyGateway = hooks.ApplyGateway
	}
	if hooks.GatewayEnabled != nil {
		dst.GatewayEnabled = hooks.GatewayEnabled
	}
	if hooks.FakeIPNetworkError != nil {
		dst.FakeIPNetworkError = hooks.FakeIPNetworkError
	}
	if hooks.DNSLoop != nil {
		dst.DNSLoop = hooks.DNSLoop
	}
	if hooks.FullTun != nil {
		dst.FullTun = hooks.FullTun
	}
}

// CurrentMihomoIntegrations returns a snapshot of the installed integrations
// (wiring checks; transitional with the package-level state, O17 D2).
func CurrentMihomoIntegrations() MihomoIntegrations { return currentIntegrations() }

func currentIntegrations() MihomoIntegrations {
	integrationState.RLock()
	defer integrationState.RUnlock()
	return integrationState.value
}
func gatewaySetting(config *model.Config) (present, marker bool) {
	if config == nil || config.Profile == nil {
		return false, false
	}
	raw := config.Profile.Mihomo.LinuxGateway.JSON()
	present = raw != nil && !bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
	marker = !config.Profile.Mihomo.RouterSplit.IsZero()
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
