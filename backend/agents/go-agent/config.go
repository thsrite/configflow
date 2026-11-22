package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"agent/svc"
)

// Config 结构体完整定义了 agent 的所有配置项
type Config struct {
	ServerURL               string `json:"server_url"`
	AgentName               string `json:"agent_name"`
	AgentHost               string `json:"agent_host"`
	AgentPort               int    `json:"agent_port"`
	AgentIP                 string `json:"agent_ip,omitempty"` // omitempty: 如果为空则不在JSON中显示
	ServiceType             string `json:"service_type"`
	DeploymentMethod        string `json:"deployment_method,omitempty"`
	ServiceName             string `json:"service_name"`
	ConfigPath              string `json:"config_path"`
	RestartCommand          string `json:"restart_command"`
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
	HeartbeatInterval       int    `json:"heartbeat_interval"`
	AgentID                 string `json:"agent_id,omitempty"`
	UpgradeSchema           int    `json:"upgrade_schema,omitempty"`
	AgentServiceUnit        string `json:"agent_service_unit,omitempty"`
	NativeAIO               bool   `json:"native_aio,omitempty"`
	Token                   string `json:"token,omitempty"`
	InstallationID          string `json:"installation_id,omitempty"`
	InstallationToken       string `json:"installation_token,omitempty"`
	RegistrationToken       string `json:"registration_token,omitempty"`
	RegistrationKey         string `json:"registration_key,omitempty"`

	// 监控功能配置（使用指针以区分未设置和 false）
	EnableMetrics *bool `json:"enable_metrics,omitempty"` // 是否启用系统监控，默认 true

	// MosDNS 特殊功能字段
	Directories      []string              `json:"directories,omitempty"`
	RulesetDownloads []RulesetDownloadItem `json:"ruleset_downloads,omitempty"`

	// 用于保护配置文件读写的互斥锁
	mu       sync.Mutex `json:"-"` // - 表示不参与JSON序列化
	filePath string     `json:"-"`
}

// RulesetDownloadItem 定义规则集下载项的结构
type RulesetDownloadItem struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	LocalPath string `json:"local_path"`
}

// LoadConfig 从指定路径加载配置
func LoadConfig(path string) (*Config, error) {
	var cfg Config

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = 30 // 设置默认心跳间隔
	}
	// Images before 1.9.3-go wrote "supervisor"; the next Save writes "s6".
	cfg.ServiceManager = svc.NormalizeManager(cfg.ServiceManager)

	cfg.filePath = path
	return &cfg, nil
}

// Save 将当前配置写回到文件
func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 使用 MarshalIndent 美化输出格式
	// Keep extension fields supplied by older/newer installers during registration.
	values := map[string]interface{}{}
	if old, readErr := os.ReadFile(c.filePath); readErr == nil {
		if err := json.Unmarshal(old, &values); err != nil {
			return err
		}
	} else if !os.IsNotExist(readErr) {
		return readErr
	}
	known, err := json.Marshal(c)
	if err != nil {
		return err
	}
	var current map[string]interface{}
	if err = json.Unmarshal(known, &current); err != nil {
		return err
	}
	for key, value := range current {
		values[key] = value
	}
	if c.InstallationID == "" {
		delete(values, "installation_id")
	}
	if c.InstallationToken == "" {
		delete(values, "installation_token")
	}
	if c.RegistrationToken == "" {
		delete(values, "registration_token")
	}
	if c.RegistrationKey == "" {
		delete(values, "registration_key")
	}
	data, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return err
	}

	// Registration writes must not truncate the Agent configuration.
	f, err := os.CreateTemp(filepath.Dir(c.filePath), ".agent-config-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(tmp, c.filePath); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(c.filePath))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// IsMetricsEnabled 检查是否启用系统监控
// 如果未设置（nil），默认返回 true（启用）
func (c *Config) IsMetricsEnabled() bool {
	if c.EnableMetrics == nil {
		return true // 默认启用监控
	}
	return *c.EnableMetrics
}

// ConvertRulesetDownloads 转换规则集下载项
func (c *Config) ConvertRulesetDownloads() []RulesetDownloadItem {
	return c.RulesetDownloads
}
