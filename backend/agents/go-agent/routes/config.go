package routes

import (
	"net/http"
)

// RulesetDownloadItem 定义规则集下载项的结构
type RulesetDownloadItem struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	LocalPath string `json:"local_path"`
	Content   string `json:"content,omitempty"`
}

// Config 结构体定义
type Config struct {
	AgentConfigFile string `json:"-"`
	ServerURL       string `json:"server_url"`
	AgentName       string `json:"agent_name"`
	AgentHost       string `json:"agent_host"`
	AgentPort       int    `json:"agent_port"`
	AgentIP         string `json:"agent_ip,omitempty"`
	// AgentIPSource, when set, is read instead of AgentIP: the address can
	// change while the Agent runs (renumbered host, address set from the panel).
	AgentIPSource     func() string `json:"-"`
	ServiceType       string        `json:"service_type"`
	ServiceName       string        `json:"service_name"`
	ConfigPath        string        `json:"config_path"`
	RestartCommand    string        `json:"restart_command"`
	HeartbeatInterval int           `json:"heartbeat_interval"`
	AgentID           string        `json:"agent_id,omitempty"`
	AgentServiceUnit  string        `json:"agent_service_unit,omitempty"`
	NativeAIO         bool          `json:"native_aio,omitempty"`
	Token             string        `json:"token,omitempty"`

	ServiceManager          string `json:"service_manager,omitempty"`
	ServiceBinary           string `json:"service_binary,omitempty"`
	ServiceUnit             string `json:"service_unit,omitempty"`
	StopCommand             string `json:"stop_command,omitempty"`
	StartCommand            string `json:"start_command,omitempty"`
	StatusCommand           string `json:"status_command,omitempty"`
	HealthURL               string `json:"health_url,omitempty"`
	HealthDNSAddress        string `json:"health_dns_address,omitempty"`
	HealthDNSName           string `json:"health_dns_name,omitempty"`
	DeploymentHealthTimeout int    `json:"deployment_health_timeout,omitempty"`
	DNSWatchdogDisabled     bool   `json:"dns_watchdog_disabled,omitempty"`

	// MosDNS 特殊功能字段
	Directories      []string              `json:"directories,omitempty"`
	RulesetDownloads []RulesetDownloadItem `json:"ruleset_downloads,omitempty"`
}

// ConfigUpdateHandler 处理配置更新请求
func ConfigUpdateHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		JsonResponse(w, http.StatusConflict, MessageResponse{Success: false, Message: "Legacy configuration updates are disabled; upgrade ConfigFlow and use deployment protocol 1"})
	}
}

func (c *Config) agentIP() string {
	if c.AgentIPSource != nil {
		return c.AgentIPSource()
	}
	return c.AgentIP
}
