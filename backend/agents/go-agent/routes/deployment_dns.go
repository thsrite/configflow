package routes

import (
	"agent/preflight"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DNS reports describe the staged configuration and a read-only snapshot of
// current listeners. They never claim that an unstarted candidate was probed.
type DeploymentDNSCheck struct {
	Name    string `json:"name"`
	Level   string `json:"level"`
	Message string `json:"message"`
	Fix     string `json:"fix,omitempty"`
}

var runDeploymentDNSChecks = preflight.CheckDNS

func deploymentDNSConfig(cfg *Config) preflight.Config {
	return preflight.Config{
		AgentConfigFile: cfg.AgentConfigFile, AgentPort: cfg.AgentPort,
		AgentServiceUnit: cfg.AgentServiceUnit, ConfigPath: cfg.ConfigPath,
		ServiceType: cfg.ServiceType, ServiceManager: cfg.ServiceManager,
		ServiceUnit: cfg.ServiceUnit, ServiceBinary: cfg.ServiceBinary,
		NativeAIO: dnsPeerService(cfg) != "",
	}
}

func dnsPeerService(cfg *Config) string {
	if cfg.NativeAIO {
		switch cfg.ServiceType {
		case "mihomo":
			return "oxidns"
		case "oxidns":
			return "mihomo"
		}
	}
	if cfg.ServiceType == "mihomo" {
		if os.Getenv("ENABLE_OXIDNS") == "true" {
			return "oxidns"
		}
		if os.Getenv("ENABLE_MOSDNS") == "true" {
			return "mosdns"
		}
	}
	if os.Getenv("ENABLE_MIHOMO") == "true" &&
		(cfg.ServiceType == "oxidns" && os.Getenv("ENABLE_OXIDNS") == "true" ||
			cfg.ServiceType == "mosdns" && os.Getenv("ENABLE_MOSDNS") == "true") {
		return "mihomo"
	}
	return ""
}

func readDNSConfigFile(name string) ([]byte, error) {
	info, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 4<<20 {
		return nil, fmt.Errorf("DNS configuration unavailable")
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (4<<20)+1))
	if len(data) > 4<<20 {
		return nil, fmt.Errorf("DNS configuration too large")
	}
	return data, err
}

func deploymentDNSPeer(cfg *Config, read func(string) ([]byte, error)) (*preflight.DNSCandidate, preflight.Report) {
	service := dnsPeerService(cfg)
	if service == "" {
		return nil, nil
	}
	unknown := preflight.Report{{Name: "同机 DNS 配合", Level: "warn",
		Message: "暂时无法读取同机另一核心的配置，尚未核实上下游是否对应。",
		Fix:     "确认另一 Agent 已安装且配置文件可读；安装完成或发布对端配置后重新检查。"}}
	name := filepath.Join("/opt/configflow-agent", "config-"+service+".json")
	if cfg.NativeAIO {
		name = filepath.Join("/opt/configflow-agent", service, "config.json")
	}
	data, err := read(name)
	if err != nil {
		return nil, unknown
	}
	var peer preflight.Config
	if json.Unmarshal(data, &peer) != nil || peer.ServiceType != service || !filepath.IsAbs(peer.ConfigPath) ||
		(cfg.NativeAIO && !peer.NativeAIO) {
		return nil, unknown
	}
	peer.AgentConfigFile, peer.NativeAIO = name, true
	content, err := read(peer.ConfigPath)
	if err != nil {
		return nil, unknown
	}
	var doc map[string]any
	if yaml.Unmarshal(content, &doc) != nil || len(doc) == 0 {
		return nil, unknown
	}
	return &preflight.DNSCandidate{Config: peer, Content: content}, nil
}

func validateDeploymentDNS(cfg *Config, state *DeploymentState) error {
	name := filepath.Join(deploymentPath(cfg, state.DeploymentID), "stage", state.Manifest.ConfigPath)
	// The staged file has already passed the deployment size/checksum limits
	// and native validation. Do not add a smaller publication limit here.
	content, err := os.ReadFile(name)
	if err != nil {
		return fmt.Errorf("无法读取待发布配置以检查 DNS")
	}
	peer, extra := deploymentDNSPeer(cfg, readDNSConfigFile)
	report := runDeploymentDNSChecks(preflight.DNSCandidate{Config: deploymentDNSConfig(cfg), Content: content},
		preflight.DNSOptions{Peer: peer, Probe: true, QueryName: cfg.HealthDNSName})
	report = append(report, extra...)
	secrets := []string{cfg.Token}
	for _, data := range [][]byte{content, func() []byte {
		if peer != nil {
			return peer.Content
		}
		return nil
	}()} {
		var doc map[string]any
		if yaml.Unmarshal(data, &doc) == nil {
			secrets = append(secrets, configurationDiagnosticSecrets(doc)...)
		}
	}
	sanitize := func(value string) string {
		value = sanitizeDiagnostic(value)
		for _, secret := range diagnosticSecretParts(secrets) {
			if secret != "" {
				value = strings.ReplaceAll(value, secret, "[REDACTED]")
			}
		}
		return boundedDiagnostic(value, 2048)
	}
	state.DNSChecks = nil
	// Keep errors visible even if a deliberately huge configuration generates
	// more individual findings than the panel should display.
	for _, level := range []string{"error", "warn", "pass"} {
		for _, check := range report {
			if check.Level != level || len(state.DNSChecks) >= 64 {
				continue
			}
			state.DNSChecks = append(state.DNSChecks, DeploymentDNSCheck{
				Name: sanitize(check.Name), Level: level, Message: sanitize(check.Message), Fix: sanitize(check.Fix),
			})
		}
	}
	state.DNSCheckedAt = time.Now().UTC().Format(time.RFC3339)
	if report.HasErrors() {
		return fmt.Errorf("DNS 检查未通过，请处理发布记录中的冲突后重试；当前运行配置未修改")
	}
	// Persist warnings before activation so polling can display them while the
	// normal transactional lifecycle proceeds without another confirmation.
	return saveDeployment(cfg, state, state.Status)
}
