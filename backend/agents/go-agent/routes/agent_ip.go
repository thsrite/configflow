package routes

import (
	"fmt"
	"net"
	"strings"

	"agent/internal/localip"
)

var detectAgentIPv4s = localip.DetectAll

// Resolve the complete local LAN snapshot at publication and activation time.
// An explicit agent_ip must belong to that snapshot; it cannot replace the
// other addresses that need DNS protection. Do not persist detected addresses:
// DHCP and interface availability can change between publishes.
func agentLANIPv4s(cfg *Config) ([]net.IP, error) {
	value := strings.TrimSpace(cfg.agentIP())
	var explicit net.IP
	if value != "" {
		explicit = net.ParseIP(value)
		if explicit == nil || explicit.To4() == nil || !explicit.IsGlobalUnicast() || explicit.IsLoopback() {
			return nil, fmt.Errorf("Agent 配置中的 agent_ip 不是有效的 LAN IPv4；请填写本机地址或留空自动获取")
		}
	}
	values, err := detectAgentIPv4s()
	if err != nil {
		return nil, fmt.Errorf("获取本机 LAN IPv4 失败：%w；请检查网络接口和 Agent 配置中的 agent_ip", err)
	}
	// A DNS HA virtual IP floats between hosts and is not this Agent's address.
	if excluded := OxiDNSHAVirtualIPs(cfg); len(excluded) > 0 {
		kept := make([]string, 0, len(values))
		for _, value := range values {
			skip := false
			for _, vip := range excluded {
				skip = skip || strings.TrimSpace(value) == vip
			}
			if !skip {
				kept = append(kept, value)
			}
		}
		values = kept
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("无法获得有效的本机 LAN IPv4；请检查 Agent 配置中的 agent_ip 和网络接口")
	}
	addresses := make([]net.IP, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		address := net.ParseIP(strings.TrimSpace(value))
		if address == nil || address.To4() == nil || !address.IsGlobalUnicast() || address.IsLoopback() {
			return nil, fmt.Errorf("无法获得有效的本机 LAN IPv4；请检查 Agent 配置中的 agent_ip 和网络接口")
		}
		address = address.To4()
		if !seen[address.String()] {
			addresses = append(addresses, address)
			seen[address.String()] = true
		}
	}
	if explicit != nil && !seen[explicit.String()] {
		return nil, fmt.Errorf("Agent 配置中的 agent_ip（%s）不属于当前本机 LAN IPv4；请填写本机地址或留空后重新发布", value)
	}
	return addresses, nil
}
