package routers

import (
	"net/netip"
	"slices"
	"strings"

	"configflow/internal/netipx"
)

// DNSAgent is what DHCP DNS needs to know about an Agent record.
type DNSAgent struct {
	ID          string   `json:"id"`
	Host        string   `json:"host"`
	ServiceType string   `json:"service_type"`
	Status      string   `json:"status"`
	HA          *AgentHA `json:"oxidns_ha"`
	HAState     *struct {
		Status string `json:"status"`
	} `json:"oxidns_ha_state"`
	HAPublication *struct {
		Status *string `json:"status"`
	} `json:"oxidns_ha_publication"`
}

// AgentHA is an Agent's OxiDNS high-availability pairing.
type AgentHA struct {
	Enabled bool   `json:"enabled"`
	GroupID string `json:"group_id"`
	PeerID  string `json:"peer_id"`
	VIP     string `json:"vip"`
}

// DNSHA is the virtual IP of an OxiDNS pair and its members' addresses.
type DNSHA struct {
	VIP    string   `json:"vip"`
	Hosts  []string `json:"hosts"`
	PeerID string   `json:"peer_id"`
}

// DNSSettings is the DHCP DNS target of a router (part of the desired state).
type DNSSettings struct {
	AgentID string `json:"dns_agent_id"`
	Server  string `json:"dns_server"`
	Error   string `json:"dns_error"`
	HA      *DNSHA `json:"dns_ha,omitempty"`
}

// clientIPv4 is the canonical text of an IPv4 address clients can reach, or "".
func clientIPv4(host string) string {
	a, ok := netipx.ParseAddr4(host)
	if !ok || a.IsUnspecified() || a.IsLoopback() || a.IsMulticast() || a == netip.AddrFrom4([4]byte{255, 255, 255, 255}) {
		return ""
	}
	return a.String()
}

// DNSAgentSettings is the DHCP DNS target for a single OxiDNS Agent (nil: gone).
func DNSAgentSettings(agentID string, agent *DNSAgent) DNSSettings {
	s := DNSSettings{AgentID: agentID}
	switch {
	case agentID == "":
	case agent == nil:
		s.Error = "所选 DNS Agent 不存在，请重新选择"
	case agent.ServiceType != "oxidns":
		s.Error = "DHCP DNS 必须选择 OxiDNS Agent"
	case agent.Status != "online":
		s.Error = "所选 DNS Agent 离线，无法下发 DHCP DNS"
	default:
		if s.Server = clientIPv4(agent.Host); s.Server == "" {
			s.Error = "所选 DNS Agent 的主机地址必须是客户端可访问的 IPv4 地址"
		}
	}
	return s
}

// DNSHASettings is the DHCP DNS target of an OxiDNS pair sharing a virtual IP
// (while HA is enabled); one member being offline does not block publishing.
func DNSHASettings(agentID string, agent, peer *DNSAgent) DNSSettings {
	s := DNSSettings{AgentID: agentID}
	ha := agent.HA
	if ha == nil {
		ha = &AgentHA{}
	}
	pair := []*DNSAgent{agent, peer}
	switch {
	case agent.ServiceType != "oxidns":
		s.Error = "DHCP DNS 必须选择 OxiDNS Agent"
	case peer == nil || peer.ServiceType != "oxidns" || peer.HA == nil || !peer.HA.Enabled || ha.GroupID == "" ||
		peer.HA.GroupID != ha.GroupID || ha.PeerID != peer.ID || peer.HA.PeerID != agent.ID || peer.HA.VIP != ha.VIP:
		s.Error = "所选 DNS Agent 的高可用配对不完整，请到 Agent 页重新设置 DNS 高可用"
	case slices.ContainsFunc(pair, func(a *DNSAgent) bool { return a.HAState == nil || a.HAState.Status != "succeeded" }):
		s.Error = "所选 DNS Agent 的高可用尚未部署成功，请到 Agent 页查看"
	case slices.ContainsFunc(pair, func(a *DNSAgent) bool {
		return a.HAPublication != nil && a.HAPublication.Status != nil && *a.HAPublication.Status != "succeeded"
	}):
		s.Error = "主备配置同步尚未成功，请先确认两台发布结果再下发虚拟 IP"
	case agent.Status != "online" && peer.Status != "online":
		s.Error = "所选 DNS 高可用的两台 Agent 均离线，无法下发 DHCP DNS"
	default:
		vip, hosts := clientIPv4(ha.VIP), []string{clientIPv4(agent.Host), clientIPv4(peer.Host)}
		if vip == "" || hosts[0] == "" || hosts[1] == "" {
			s.Error = "所选 DNS 高可用的虚拟 IP 和两台主机地址必须是客户端可访问的 IPv4 地址"
		} else {
			s.Server, s.HA = vip, &DNSHA{VIP: vip, Hosts: hosts, PeerID: peer.ID}
		}
	}
	return s
}

// MatchingNetworks are the enabled DHCP networks containing the gateway (the
// unique one gets the selected DNS; none is ever created).
func MatchingNetworks(state *DhcpDnsState, gateway string) ([]*DhcpDnsEntry, error) {
	address, ok := netipx.ParseAddr4(gateway)
	if !ok {
		return nil, routerError("无法确定 DHCP DNS 下发范围：mihomo 网关不是有效 IPv4 地址")
	}
	matches := []*DhcpDnsEntry{}
	seen := map[string]bool{}
	for _, e := range state.Entries {
		if !e.Enabled {
			continue
		}
		if e.ID == "" || seen[e.ID] {
			return nil, routerError("DHCP 配置缺少唯一标识，无法安全下发 DNS")
		}
		seen[e.ID] = true
		network, ok := netipx.ParseNetwork4(e.Network, false)
		if !ok {
			message := "DHCP 配置包含无法识别的网段，无法安全确定 DNS 下发范围"
			if len(e.Blockers) > 0 {
				message += "：" + strings.Join(e.Blockers, "；")
			}
			return nil, routerError(message)
		}
		if network.Contains(address) {
			matches = append(matches, e)
		}
	}
	return matches, nil
}

// DNSReportItem is the drift of one DHCP network.
type DNSReportItem struct {
	ID         string   `json:"id"`
	Network    string   `json:"network"`
	CurrentDNS []string `json:"current_dns"`
	DesiredDNS []string `json:"desired_dns"`
	Status     string   `json:"status"` // synced, pending, unknown
	Detail     string   `json:"detail"`
}

// DNSReport is the DHCP DNS part of the drift report.
type DNSReport struct {
	Status    string          `json:"status"` // disabled, unknown, synced, pending
	TargetDNS string          `json:"target_dns"`
	Message   string          `json:"message"`
	Items     []DNSReportItem `json:"items"`
	Blockers  []string        `json:"blockers"`
}

// BuildDNSReport compares the DHCP DNS readback (nil: not readable) with the selected DNS target.
func BuildDNSReport(dns DNSSettings, gateway string, state *DhcpDnsState) DNSReport {
	report := DNSReport{Status: "disabled", TargetDNS: dns.Server, Items: []DNSReportItem{}, Blockers: []string{},
		Message: "未选择 DHCP DNS 服务器，本次保留现有 DHCP DNS 配置。"}
	if dns.AgentID == "" {
		return report
	}
	failure, matches := dns.Error, []*DhcpDnsEntry{}
	switch {
	case failure != "":
	case dns.Server == "":
		failure = "无法确定所选 DNS Agent 的 IPv4 地址"
	case state == nil:
		failure = "无法读取 DHCP DNS 配置"
	default:
		var err error
		if matches, err = MatchingNetworks(state, gateway); err != nil {
			failure = err.Error()
		} else if len(matches) == 0 {
			failure = "未找到包含所选 mihomo 网关的已启用 DHCP 网段，不会自动创建 DHCP 配置"
		} else if len(matches) > 1 {
			failure = "多个已启用 DHCP 网段包含所选 mihomo 网关，无法确定唯一 DNS 下发范围"
		} else {
			failure = strings.Join(matches[0].Blockers, "；")
		}
	}
	report.Status, report.Message = "unknown", failure
	for _, e := range matches {
		item := DNSReportItem{ID: e.ID, Network: e.Network, CurrentDNS: append([]string{}, e.DNS...),
			DesiredDNS: []string{dns.Server}, Status: "unknown", Detail: failure}
		if failure == "" {
			report.Status, report.Message = "pending", "将替换该 DHCP 网段的 DNS 列表；客户端续租或重新连接后获取新 DNS。"
			if !e.DNSExplicit {
				item.Detail = "DNS 槽位仍含自动/未指定值，将明确设置为所选服务器"
			} else if slices.Equal(e.DNS, item.DesiredDNS) {
				report.Status, report.Message = "synced", "该 DHCP 网段的 DNS 已与所选服务器一致。"
			}
			item.Status = report.Status
		}
		report.Items = append(report.Items, item)
	}
	if failure != "" {
		report.Blockers = []string{failure}
	}
	return report
}

// DNSCheck is the readback check a release plan stores for DHCP DNS.
type DNSCheck struct {
	ID      string   `json:"id"`
	Network string   `json:"network"`
	Gateway string   `json:"gateway"`
	DNS     []string `json:"dns"`
}

// CheckedDNSTarget re-finds the planned DHCP network in a fresh readback.
func CheckedDNSTarget(check DNSCheck, state *DhcpDnsState) (*DhcpDnsEntry, error) {
	matches, err := MatchingNetworks(state, check.Gateway)
	if err != nil {
		return nil, err
	}
	if len(matches) != 1 || matches[0].ID != check.ID || matches[0].Network != check.Network {
		return nil, routerError("回读校验失败：DHCP DNS 的目标网段发生变化")
	}
	if e := matches[0]; len(e.Blockers) > 0 {
		return nil, routerError("回读校验失败：" + strings.Join(e.Blockers, "；"))
	}
	return matches[0], nil
}

// VerifyDNS checks that the DHCP network now hands out exactly check.DNS.
func VerifyDNS(check DNSCheck, state *DhcpDnsState) error {
	e, err := CheckedDNSTarget(check, state)
	if err != nil {
		return err
	}
	if !slices.Equal(e.DNS, check.DNS) || !e.DNSExplicit {
		return routerError("回读校验失败：DHCP DNS 与所选服务器不一致")
	}
	return nil
}
