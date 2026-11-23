package routes

import (
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Ready candidates may outlive their LAN snapshot, including an explicit Agent IP.
// Check coverage before any activation work, without adapting immutable staged
// bytes to an address that was not present when the candidate was prepared.
func validateDeploymentAgentIPs(cfg *Config, state *DeploymentState) error {
	if cfg.ServiceType != "mihomo" {
		return nil
	}
	name := filepath.Join(deploymentPath(cfg, state.DeploymentID), "stage", state.Manifest.ConfigPath)
	data, err := os.ReadFile(name)
	if err != nil {
		return fmt.Errorf("激活前无法读取本机 DNS 排除配置：%w", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("激活前无法解析本机 DNS 排除配置：%w", err)
	}
	marker, _ := doc["x-configflow-linux-gateway"].(map[string]any)
	tun, _ := doc["tun"].(map[string]any)
	if marker["enabled"] != true && (!cfg.NativeAIO || tun["enable"] != true || tun["auto-redirect"] != true) {
		return nil
	}
	addresses, err := agentLANIPv4s(cfg)
	if err != nil {
		return fmt.Errorf("激活前检查本机 DNS 排除失败：%w；请修复地址后重新发布配置", err)
	}
	values, ok := tun["route-exclude-address"].([]any)
	if !ok {
		return fmt.Errorf("待激活配置缺少本机 DNS 排除地址，请重新发布配置")
	}
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("待激活配置的本机 DNS 排除地址无效，请重新发布配置")
		}
		prefix, err := netip.ParsePrefix(text)
		if err != nil {
			return fmt.Errorf("待激活配置的本机 DNS 排除地址无效，请重新发布配置")
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	var missing []string
	for _, ip := range addresses {
		address, err := netip.ParseAddr(ip.String())
		if err != nil {
			return fmt.Errorf("激活前无法获取有效的本机 LAN IPv4，请检查网络接口后重新发布配置")
		}
		covered := false
		for _, prefix := range prefixes {
			if prefix.Contains(address.Unmap()) {
				covered = true
				break
			}
		}
		if !covered {
			missing = append(missing, ip.String())
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("待激活配置未排除当前本机 LAN IPv4（%s）的 DNS；网络地址可能已变化，请重新发布配置", strings.Join(missing, "、"))
	}
	return nil
}
