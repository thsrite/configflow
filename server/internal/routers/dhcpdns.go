package routers

import (
	"math/big"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/pyip"
)

// Port of backend/routers/dhcp_dns.py: DHCP DNS scope and drift shared by
// both router drivers.

// clientReachable is the rule shared by dns_agent_settings and _client_ipv4.
func clientReachable(address pyip.Addr) bool {
	return !(address.IsUnspecified() || address.IsLoopback() || address.IsMulticast() || address.Int().Lo == 0xffffffff)
}

// DNSAgentSettings is dns_agent_settings(agent_id, agent).
func DNSAgentSettings(agentID string, agent *jsonx.Object) *jsonx.Object {
	settings := jsonx.Obj("dns_agent_id", agentID, "dns_server", "", "dns_error", "")
	if agentID == "" {
		return settings
	}
	switch {
	case agent == nil || agent.Len() == 0:
		settings.Set("dns_error", "所选 DNS Agent 不存在，请重新选择")
	case !jsonx.Equal(agent.Value("service_type"), "oxidns"):
		settings.Set("dns_error", "DHCP DNS 必须选择 OxiDNS Agent")
	case !jsonx.Equal(agent.Value("status"), "online"):
		settings.Set("dns_error", "所选 DNS Agent 离线，无法下发 DHCP DNS")
	default:
		host, ok := agent.Value("host").(string)
		var address pyip.Addr
		if ok {
			address, ok = pyip.ParseIPv4Address(host)
		}
		if ok && clientReachable(address) {
			settings.Set("dns_server", address.String())
		} else {
			settings.Set("dns_error", "所选 DNS Agent 的主机地址必须是客户端可访问的 IPv4 地址")
		}
	}
	return settings
}

// clientIPv4 is _client_ipv4(value).
func clientIPv4(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	address, ok := pyip.ParseIPv4Address(text)
	if !ok || !clientReachable(address) {
		return ""
	}
	return address.String()
}

// objectOr is `value or {}` for a value that must then answer .get().
func objectOr(value any) (*jsonx.Object, error) {
	if !jsonx.Truthy(value) {
		return jsonx.NewObject(), nil
	}
	o, ok := value.(*jsonx.Object)
	if !ok {
		return nil, pyErr("AttributeError", "'"+typeName(value)+"' object has no attribute 'get'")
	}
	return o, nil
}

// DNSHASettings is dns_ha_settings(agent_id, agent, peer): DHCP DNS for an
// OxiDNS pair sharing a virtual IP (only called while HA is enabled).
// Errors are the Python exceptions malformed records raise.
func DNSHASettings(agentID string, agent, peer *jsonx.Object) (*jsonx.Object, error) {
	settings := jsonx.Obj("dns_agent_id", agentID, "dns_server", "", "dns_error", "")
	// `x or {}` keeps a truthy non-dict; its .get() raises only when reached.
	haRaw := agent.Value("oxidns_ha")
	var peerHARaw any
	if peer != nil {
		peerHARaw = peer.Value("oxidns_ha")
	}
	get := func(raw any, key string) (any, error) {
		o, err := objectOr(raw)
		if err != nil {
			return nil, err
		}
		return o.Value(key), nil
	}
	pair := []*jsonx.Object{agent, peer}
	if !jsonx.Equal(agent.Value("service_type"), "oxidns") {
		settings.Set("dns_error", "DHCP DNS 必须选择 OxiDNS Agent")
		return settings, nil
	}
	incomplete := peer == nil || peer.Len() == 0 || !jsonx.Equal(peer.Value("service_type"), "oxidns")
	checks := []func() (bool, error){
		func() (bool, error) { v, err := get(peerHARaw, "enabled"); return !isTrue(v), err },
		func() (bool, error) { v, err := get(haRaw, "group_id"); return !jsonx.Truthy(v), err },
		func() (bool, error) {
			a, err := get(peerHARaw, "group_id")
			if err != nil {
				return false, err
			}
			b, err := get(haRaw, "group_id")
			return !jsonx.Equal(a, b), err
		},
		func() (bool, error) { v, err := get(haRaw, "peer_id"); return !jsonx.Equal(v, peer.Value("id")), err },
		func() (bool, error) { v, err := get(peerHARaw, "peer_id"); return !jsonx.Equal(v, agent.Value("id")), err },
		func() (bool, error) {
			a, err := get(peerHARaw, "vip")
			if err != nil {
				return false, err
			}
			b, err := get(haRaw, "vip")
			return !jsonx.Equal(a, b), err
		},
	}
	for _, check := range checks {
		if incomplete {
			break
		}
		failed, err := check()
		if err != nil {
			return nil, err
		}
		incomplete = failed
	}
	if incomplete {
		settings.Set("dns_error", "所选 DNS Agent 的高可用配对不完整，请到 Agent 页重新设置 DNS 高可用")
		return settings, nil
	}
	haObject, _ := objectOr(haRaw)
	for _, item := range pair {
		state, err := objectOr(item.Value("oxidns_ha_state"))
		if err != nil {
			return nil, err
		}
		if !jsonx.Equal(state.Value("status"), "succeeded") {
			settings.Set("dns_error", "所选 DNS Agent 的高可用尚未部署成功，请到 Agent 页查看")
			return settings, nil
		}
	}
	for _, item := range pair {
		publication, err := objectOr(item.Value("oxidns_ha_publication"))
		if err != nil {
			return nil, err
		}
		status := publication.Value("status")
		if status != nil && !jsonx.Equal(status, "succeeded") {
			settings.Set("dns_error", "主备配置同步尚未成功，请先确认两台发布结果再下发虚拟 IP")
			return settings, nil
		}
	}
	if !jsonx.Equal(agent.Value("status"), "online") && !jsonx.Equal(peer.Value("status"), "online") {
		settings.Set("dns_error", "所选 DNS 高可用的两台 Agent 均离线，无法下发 DHCP DNS")
		return settings, nil
	}
	vip := clientIPv4(haObject.Value("vip"))
	hosts := []any{clientIPv4(agent.Value("host")), clientIPv4(peer.Value("host"))}
	if vip == "" || hosts[0] == "" || hosts[1] == "" {
		settings.Set("dns_error", "所选 DNS 高可用的虚拟 IP 和两台主机地址必须是客户端可访问的 IPv4 地址")
		return settings, nil
	}
	peerID, ok := peer.Get("id")
	if !ok {
		return nil, pyErr("KeyError", "'id'")
	}
	settings.Set("dns_server", vip)
	settings.Set("dns_ha", jsonx.Obj("vip", vip, "hosts", hosts, "peer_id", peerID))
	return settings, nil
}

// ipv4Of is IPv4Address(value) for any value: an int in range, or str(value)
// parsed; ok false where Python raises ValueError/TypeError.
func ipv4Of(value any) (pyip.Addr, bool) {
	switch v := value.(type) {
	case bool:
		if v {
			return pyip.IPv4FromInt(1)
		}
		return pyip.IPv4FromInt(0)
	case jsonx.Number:
		if v.IsInt() {
			n, _ := v.BigInt()
			if n.Sign() < 0 || n.Cmp(big.NewInt(0xffffffff)) > 0 {
				return pyip.Addr{}, false
			}
			return pyip.IPv4FromInt(n.Uint64())
		}
	}
	return pyip.ParseIPv4Address(jsonx.Str(value))
}

// MatchingNetworks is matching_networks(state, gateway): the enabled DHCP
// entries whose network contains the gateway.
func MatchingNetworks(state *DhcpDnsState, gateway any) ([]*DhcpDnsEntry, error) {
	address, ok := ipv4Of(gateway)
	if !ok {
		return nil, NewRouterError("无法确定 DHCP DNS 下发范围：mihomo 网关不是有效 IPv4 地址")
	}
	matches := []*DhcpDnsEntry{}
	seen := map[string]bool{}
	for _, entry := range state.Entries {
		if !entry.Enabled {
			continue
		}
		if entry.ID == "" || seen[entry.ID] {
			return nil, NewRouterError("DHCP 配置缺少唯一标识，无法安全下发 DNS")
		}
		seen[entry.ID] = true
		network, ok := pyip.ParseIPv4Network(entry.Network, false)
		if !ok {
			detail := strings.Join(entry.Blockers, "；")
			message := "DHCP 配置包含无法识别的网段，无法安全确定 DNS 下发范围"
			if detail != "" {
				message += "：" + detail
			}
			return nil, NewRouterError(message)
		}
		if network.Contains(address) {
			matches = append(matches, entry)
		}
	}
	return matches, nil
}

func dnsMatches(dns []string, target any) bool {
	return len(dns) == 1 && jsonx.Equal(dns[0], target)
}

// BuildDNSReport is build_dns_report(desired, state).
func BuildDNSReport(desired *jsonx.Object, state *DhcpDnsState) *jsonx.Object {
	var target any = ""
	if jsonx.Truthy(desired.Value("dns_server")) {
		target = desired.Value("dns_server")
	}
	report := jsonx.Obj("status", "disabled", "target_dns", target,
		"message", "未选择 DHCP DNS 服务器，本次保留现有 DHCP DNS 配置。", "items", []any{}, "blockers", []any{})
	if !jsonx.Truthy(desired.Value("dns_agent_id")) {
		return report
	}
	report.Set("status", "unknown")
	errValue := desired.Value("dns_error")
	var matches []*DhcpDnsEntry
	if !jsonx.Truthy(errValue) {
		switch {
		case !jsonx.Truthy(target):
			errValue = "无法确定所选 DNS Agent 的 IPv4 地址"
		case state == nil:
			errValue = "无法读取 DHCP DNS 配置"
		default:
			var gateway any = ""
			if jsonx.Truthy(desired.Value("proxy_gateway")) {
				gateway = desired.Value("proxy_gateway")
			}
			found, err := MatchingNetworks(state, gateway)
			if err != nil {
				errValue = err.Error()
			} else {
				matches = found
			}
		}
	}
	if !jsonx.Truthy(errValue) && len(matches) != 1 {
		if len(matches) == 0 {
			errValue = "未找到包含所选 mihomo 网关的已启用 DHCP 网段，不会自动创建 DHCP 配置"
		} else {
			errValue = "多个已启用 DHCP 网段包含所选 mihomo 网关，无法确定唯一 DNS 下发范围"
		}
	}
	if !jsonx.Truthy(errValue) && len(matches[0].Blockers) > 0 {
		errValue = strings.Join(matches[0].Blockers, "；")
	}
	failed := jsonx.Truthy(errValue)
	items := []any{}
	for _, entry := range matches {
		status := "unknown"
		if !failed {
			if dnsMatches(entry.DNS, target) && entry.DNSExplicit {
				status = "synced"
			} else {
				status = "pending"
			}
		}
		var detail any = errValue
		if !failed {
			if entry.DNSExplicit {
				detail = ""
			} else {
				detail = "DNS 槽位仍含自动/未指定值，将明确设置为所选服务器"
			}
		}
		items = append(items, jsonx.Obj("id", entry.ID, "network", entry.Network, "current_dns", anyStrings(entry.DNS),
			"desired_dns", []any{target}, "status", status, "detail", detail))
	}
	report.Set("items", items)
	if failed {
		report.Set("message", errValue)
		report.Set("blockers", []any{errValue})
	} else {
		status := items[0].(*jsonx.Object).GetString("status")
		report.Set("status", status)
		if status == "synced" {
			report.Set("message", "该 DHCP 网段的 DNS 已与所选服务器一致。")
		} else {
			report.Set("message", "将替换该 DHCP 网段的 DNS 列表；客户端续租或重新连接后获取新 DNS。")
		}
	}
	return report
}

func keyError(key string) error { return pyErr("KeyError", jsonx.Repr(key)) }

// CheckedDNSTarget is checked_dns_target(check, state).
func CheckedDNSTarget(check *jsonx.Object, state *DhcpDnsState) (*DhcpDnsEntry, error) {
	gateway, ok := check.Get("gateway")
	if !ok {
		return nil, keyError("gateway")
	}
	matches, err := MatchingNetworks(state, gateway)
	if err != nil {
		return nil, err
	}
	changed := len(matches) != 1
	if !changed {
		id, ok := check.Get("id")
		if !ok {
			return nil, keyError("id")
		}
		changed = !jsonx.Equal(matches[0].ID, id)
		if !changed {
			network, ok := check.Get("network")
			if !ok {
				return nil, keyError("network")
			}
			changed = !jsonx.Equal(matches[0].Network, network)
		}
	}
	if changed {
		return nil, NewRouterError("回读校验失败：DHCP DNS 的目标网段发生变化")
	}
	entry := matches[0]
	if len(entry.Blockers) > 0 {
		return nil, NewRouterError("回读校验失败：" + strings.Join(entry.Blockers, "；"))
	}
	return entry, nil
}

// VerifyDNS is verify_dns(check, state).
func VerifyDNS(check *jsonx.Object, state *DhcpDnsState) error {
	entry, err := CheckedDNSTarget(check, state)
	if err != nil {
		return err
	}
	dns, ok := check.Get("dns")
	if !ok {
		return keyError("dns")
	}
	if !jsonx.Equal(anyStrings(entry.DNS), dns) || !entry.DNSExplicit {
		return NewRouterError("回读校验失败：DHCP DNS 与所选服务器不一致")
	}
	return nil
}
