package routers

import (
	"strconv"

	"configflow/internal/jsonx"
	"configflow/internal/pyip"
	"configflow/internal/pystr"
)

// The settings validators of dns_guard.py (R09) and ipv6_pd_guard.py (R11),
// ported early because store.normalize/public_view (R01) always call them.
// The R09/R11 porters keep them here (or move them into their files).

// DNSGuardMaxBypass is dns_guard.MAX_BYPASS.
const DNSGuardMaxBypass = 64

// DNSGuardDefaults is a fresh copy of dns_guard.DEFAULTS.
func DNSGuardDefaults() *jsonx.Object {
	return jsonx.Obj("enabled", false, "block_dot", false, "failover", true, "bypass", []any{})
}

// PDGuardDefaults is a fresh copy of ipv6_pd_guard.DEFAULTS.
func PDGuardDefaults() *jsonx.Object { return jsonx.Obj("enabled", false) }

// SettingsError is dns_guard.SettingsError / ipv6_pd_guard.SettingsError
// (ValueError subclasses; Module tells which).
type SettingsError struct {
	Module  string // "dns_guard" or "ipv6_pd_guard"
	Message string
}

func (e *SettingsError) Error() string { return e.Message }

// PythonClass is "SettingsError".
func (e *SettingsError) PythonClass() string { return "SettingsError" }

func keysWithin(value *jsonx.Object, allowed *jsonx.Object) bool {
	for key := range value.All() {
		if !allowed.Has(key) {
			return false
		}
	}
	return true
}

// NormalizeDNSGuardSettings is dns_guard.normalize_settings(value,
// router_type, dns_agent_id).
func NormalizeDNSGuardSettings(value any, routerType, dnsAgentID string) (*jsonx.Object, error) {
	fail := func(message string) (*jsonx.Object, error) {
		return nil, &SettingsError{Module: "dns_guard", Message: message}
	}
	if value == nil {
		return DNSGuardDefaults(), nil
	}
	object, ok := value.(*jsonx.Object)
	if !ok || object == nil || !keysWithin(object, DNSGuardDefaults()) {
		return fail("强制 DNS 设置格式无效")
	}
	settings := DNSGuardDefaults()
	for key, v := range object.All() {
		settings.Set(key, v)
	}
	for _, key := range []string{"enabled", "block_dot", "failover"} {
		if _, ok := settings.Value(key).(bool); !ok {
			return fail("强制 DNS 的开关必须为布尔值")
		}
	}
	bypass, ok := settings.Value("bypass").([]any)
	if ok {
		for _, item := range bypass {
			if _, isText := item.(string); !isText {
				ok = false
			}
		}
	}
	if !ok {
		return fail("放行设备必须是 IPv4 地址或网段列表")
	}
	var networks []string
	for _, item := range bypass {
		network, ok := pyip.ParseIPv4Network(pystr.Strip(item.(string)), false)
		if !ok {
			return fail("放行设备「" + item.(string) + "」不是有效的 IPv4 地址或网段")
		}
		networks = append(networks, network.String())
	}
	if len(networks) > DNSGuardMaxBypass {
		return fail("放行设备最多 " + strconv.Itoa(DNSGuardMaxBypass) + " 项")
	}
	unique := []any{}
	seen := map[string]bool{}
	for _, text := range networks {
		if !seen[text] {
			seen[text] = true
			unique = append(unique, text)
		}
	}
	settings.Set("bypass", unique)
	if settings.GetBool("enabled") {
		if routerType != "routeros" {
			return fail("强制局域网 DNS 走 OxiDNS 仅支持 RouterOS")
		}
		if dnsAgentID == "" {
			return fail("请先选择 DHCP DNS 服务器（OxiDNS Agent），强制 DNS 使用同一台设备")
		}
	}
	return settings, nil
}

// NormalizePDGuardSettings is ipv6_pd_guard.normalize_settings(value,
// router_type).
func NormalizePDGuardSettings(value any, routerType string) (*jsonx.Object, error) {
	if value == nil {
		return PDGuardDefaults(), nil
	}
	object, ok := value.(*jsonx.Object)
	if ok && object != nil && keysWithin(object, PDGuardDefaults()) {
		_, ok = object.GetOr("enabled", false).(bool)
	} else {
		ok = false
	}
	if !ok {
		return nil, &SettingsError{Module: "ipv6_pd_guard", Message: "IPv6 前缀变化保护的开关必须为布尔值"}
	}
	settings := PDGuardDefaults()
	for key, v := range object.All() {
		settings.Set(key, v)
	}
	if settings.GetBool("enabled") && routerType != "routeros" {
		return nil, &SettingsError{Module: "ipv6_pd_guard", Message: "IPv6 前缀变化保护仅支持 RouterOS"}
	}
	return settings, nil
}

// mergedSettings is copy.deepcopy({**defaults, **(router.get(key) or {})}).
func mergedSettings(defaults *jsonx.Object, router *jsonx.Object, key string) (*jsonx.Object, error) {
	result := defaults
	if raw := router.Value(key); jsonx.Truthy(raw) {
		mapping, err := mappingOf(raw)
		if err != nil {
			return nil, err
		}
		for k, v := range mapping.All() {
			result.Set(k, v)
		}
	}
	return result.DeepCopy(), nil
}

// PDGuardSettingsOf is ipv6_pd_guard.settings_of(router).
func PDGuardSettingsOf(router *jsonx.Object) (*jsonx.Object, error) {
	return mergedSettings(PDGuardDefaults(), router, "ipv6_pd_guard")
}

// GuardSettings is store.guard_settings(router).
func GuardSettings(router *jsonx.Object) (*jsonx.Object, error) {
	return mergedSettings(DNSGuardDefaults(), router, "dns_guard")
}
