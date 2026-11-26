package routes

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// sing-tun's auto-redirect installs a fixed port-53 DNAT before its local-IP
// and route-address-set exclusions. dns-hijack: any:1053 does not disable it.
// Exclude only this Agent's LAN address (and loopback) before that DNAT so LAN
// clients reach OxiDNS. Adapt only explicitly marked gateway publications.
func adaptGatewayDNS(cfg *Config, content string) (string, error) {
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return "", err
	}
	marker, _ := doc["x-configflow-linux-gateway"].(map[string]any)
	if marker["enabled"] != true {
		return content, nil
	}
	addresses, err := agentLANIPv4s(cfg)
	if err != nil {
		return "", fmt.Errorf("为 OxiDNS 排除网关 DNS 时失败：%w", err)
	}
	tun, tunOK := doc["tun"].(map[string]any)
	dns, dnsOK := doc["dns"].(map[string]any)
	if !tunOK || !dnsOK {
		return "", fmt.Errorf("Linux 网关配置缺少 tun 或 dns 对象")
	}
	excluded := []string{"127.0.0.0/8"}
	for _, address := range addresses {
		excluded = append(excluded, address.String()+"/32")
	}
	tun["route-exclude-address"] = excluded
	tun["dns-hijack"] = []string{}
	dns["listen"] = "127.0.0.1:1053"
	result, err := yaml.Marshal(doc)
	return string(result), err
}
