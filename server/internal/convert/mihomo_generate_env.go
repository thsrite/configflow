package convert

// HookMissingError is returned where a hook of another domain is needed but
// not registered yet (fail closed).
type HookMissingError struct{ Hook, Provider string }

func (e *HookMissingError) Error() string {
	return "converters.mihomo: " + e.Hook + " is not available (provided by " + e.Provider + ")"
}

const (
	gatewayProvider   = "agents A19/A21 (common/linux_gateway.py)"
	routerProvider    = "routers clients/split (common/router_split.py)"
	onboardingProvide = "agents gateway onboarding (common/gateway_onboarding.py)"
)
