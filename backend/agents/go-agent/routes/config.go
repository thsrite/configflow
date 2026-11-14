package routes

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// UpdateRequest 定义配置更新请求结构
type UpdateRequest struct {
	Config            string                 `json:"config"`
	Directories       []string               `json:"directories,omitempty"`
	ProviderDownloads []ProviderDownloadItem `json:"provider_downloads,omitempty"`
	RulesetDownloads  []RulesetDownloadItem  `json:"ruleset_downloads,omitempty"`
	CustomFiles       []CustomFileItem       `json:"custom_files,omitempty"`
	// RestartAfterUpdate 为真时，由 Agent 在配置与规则集全部落盘后自行重启服务。
	// 配置更新是异步的（HTTP 200 仅表示任务已启动），服务端收到响应后立即重启
	// 会撞上「旧配置已清理、新配置未写入」的窗口，导致服务起不来。
	RestartAfterUpdate bool `json:"restart_after_update,omitempty"`
}

// ProviderDownloadItem 定义 provider 下载项的结构
type ProviderDownloadItem struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	LocalPath string `json:"local_path"`
	Content   string `json:"content,omitempty"`
}

// RulesetDownloadItem 定义规则集下载项的结构
type RulesetDownloadItem struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	LocalPath string `json:"local_path"`
	Content   string `json:"content,omitempty"`
}

// CustomFileItem 定义自定义文件项的结构
type CustomFileItem struct {
	Path    string `json:"path"`
	Content string `json:"content"`
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

	filePath string
}

// ConfigUpdateHandler 处理配置更新请求
func ConfigUpdateHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		JsonResponse(w, http.StatusConflict, map[string]interface{}{"success": false, "message": "Legacy configuration updates are disabled; upgrade ConfigFlow and use deployment protocol 1"})
	}
}

// handleMosdnsFeatures 处理MosDNS特殊功能
func handleMosdnsFeatures(cfg *Config, req UpdateRequest) error {
	// 获取实际的配置文件目录（从 ConfigPath 中提取）
	configDir := filepath.Dir(cfg.ConfigPath)

	// 1. 仅在配置启用缓存持久化时预创建 dump 文件
	cacheDumpFile, err := mosdnsCacheDumpFile(req.Config)
	if err != nil {
		log.Printf("Warning: failed to inspect MosDNS cache settings: %v", err)
	} else if cacheDumpFile != "" {
		cacheDumpPath := cacheDumpFile
		if !filepath.IsAbs(cacheDumpPath) {
			cacheDumpPath = filepath.Join(configDir, cacheDumpPath)
		}

		if err := os.MkdirAll(filepath.Dir(cacheDumpPath), 0755); err != nil {
			log.Printf("Warning: failed to create cache dump directory: %v", err)
		} else if file, err := os.OpenFile(cacheDumpPath, os.O_CREATE|os.O_WRONLY, 0644); err != nil {
			log.Printf("Warning: failed to create cache dump file: %v", err)
		} else {
			file.Close()
			log.Printf("Cache dump file ready: %s", cacheDumpPath)
		}
	}

	// 2. 处理 MosDNS 目录创建
	if len(req.Directories) > 0 {
		log.Printf("Creating directories for MosDNS...")
		if err := createDirectories(configDir, req.Directories); err != nil {
			log.Printf("Error creating directories: %v", err)
			return fmt.Errorf("failed to create directories: %w", err)
		}
	}

	// 3. 处理自定义文件写入（hosts 和单个规则）
	if len(req.CustomFiles) > 0 {
		log.Printf("Writing custom files for MosDNS...")
		if err := writeCustomFiles(configDir, req.CustomFiles); err != nil {
			log.Printf("Error writing custom files: %v", err)
			return fmt.Errorf("failed to write custom files: %w", err)
		}
	}

	// 4. 处理规则集下载
	if len(req.RulesetDownloads) > 0 {
		log.Printf("Downloading rulesets for MosDNS...")
		if err := downloadRulesets(configDir, req.RulesetDownloads); err != nil {
			log.Printf("Error downloading rulesets: %v", err)
			return fmt.Errorf("failed to download rulesets: %w", err)
		}
	}

	return nil
}

func mosdnsCacheDumpFile(configContent string) (string, error) {
	var config struct {
		Plugins []struct {
			Type string `yaml:"type"`
			Args struct {
				DumpFile string `yaml:"dump_file"`
			} `yaml:"args"`
		} `yaml:"plugins"`
	}

	if err := yaml.Unmarshal([]byte(configContent), &config); err != nil {
		return "", err
	}

	for _, plugin := range config.Plugins {
		if plugin.Type == "cache" {
			return strings.TrimSpace(plugin.Args.DumpFile), nil
		}
	}

	return "", nil
}

// handleMihomoFeatures 处理Mihomo特殊功能
func handleMihomoFeatures(cfg *Config, req UpdateRequest) error {
	// 获取实际的配置文件目录（从 ConfigPath 中提取）
	configDir := filepath.Dir(cfg.ConfigPath)

	// 1. 处理 Mihomo 目录创建（providers 和 ruleset）
	if len(req.Directories) > 0 {
		log.Printf("Creating directories for Mihomo...")
		if err := createDirectories(configDir, req.Directories); err != nil {
			log.Printf("Error creating directories: %v", err)
			return fmt.Errorf("failed to create directories: %w", err)
		}
	}

	// 2. 处理 provider 下载
	if len(req.ProviderDownloads) > 0 {
		log.Printf("Downloading providers for Mihomo...")
		if err := downloadProviders(configDir, req.ProviderDownloads); err != nil {
			log.Printf("Error downloading providers: %v", err)
			return fmt.Errorf("failed to download providers: %w", err)
		}
	}

	// 3. 处理规则集下载
	if len(req.RulesetDownloads) > 0 {
		log.Printf("Downloading rulesets for Mihomo...")
		if err := downloadRulesets(configDir, req.RulesetDownloads); err != nil {
			log.Printf("Error downloading rulesets: %v", err)
			return fmt.Errorf("failed to download rulesets: %w", err)
		}
	}

	return nil
}

// createDirectories 创建指定的目录
func createDirectories(configDir string, directories []string) error {
	for _, dir := range directories {
		// 将相对路径转换为绝对路径
		var fullPath string
		if filepath.IsAbs(dir) {
			fullPath = dir
		} else {
			// 使用实际的配置文件目录作为基础路径
			fullPath = filepath.Join(configDir, dir)
		}

		log.Printf("Creating directory: %s (from %s, base: %s)", fullPath, dir, configDir)
		if err := os.MkdirAll(fullPath, 0755); err != nil {
			log.Printf("Failed to create directory %s: %v", fullPath, err)
			return fmt.Errorf("failed to create directory %s: %w", fullPath, err)
		}
		log.Printf("Successfully created directory: %s", fullPath)
	}
	return nil
}

// downloadRulesets 下载所有规则集文件
func downloadRulesets(configDir string, rulesets []RulesetDownloadItem) error {
	const maxConcurrentDownloads = 3 // 限制最大并发下载数
	log.Printf("Downloading %d rulesets with a maximum concurrency of %d", len(rulesets), maxConcurrentDownloads)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var failedDownloads []string

	// 使用带缓冲的channel作为信号量来限制并发
	semaphore := make(chan struct{}, maxConcurrentDownloads)

	for _, item := range rulesets {
		wg.Add(1)
		go func(ruleset RulesetDownloadItem) {
			defer wg.Done()

			// 获取信号量，如果channel满了则会阻塞
			semaphore <- struct{}{}
			defer func() { <-semaphore }() // 释放信号量

			if err := downloadRuleset(configDir, ruleset); err != nil {
				log.Printf("Failed to download ruleset %s: %v", ruleset.Name, err)
				mu.Lock()
				failedDownloads = append(failedDownloads, ruleset.Name)
				mu.Unlock()
			} else {
				log.Printf("Successfully downloaded ruleset %s", ruleset.Name)
			}
		}(item)
	}

	wg.Wait()

	if len(failedDownloads) > 0 {
		log.Printf("Warning: %d rulesets failed to download: %v. Continuing with config update.", len(failedDownloads), failedDownloads)
	} else {
		log.Printf("Successfully downloaded all rulesets")
	}

	return nil
}

// downloadRuleset 下载规则集文件
func downloadRuleset(configDir string, item RulesetDownloadItem) error {
	// 确保LocalPath是绝对路径
	localPath := item.LocalPath
	if !filepath.IsAbs(localPath) {
		// 如果是相对路径，使用实际的配置文件目录作为基础路径
		localPath = filepath.Join(configDir, localPath)
	}

	// 如果已提供内容，直接写入文件
	if item.Content != "" {
		log.Printf("Writing ruleset content directly: %s -> %s", item.Name, localPath)
		dir := filepath.Dir(localPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
		return os.WriteFile(localPath, []byte(item.Content), 0644)
	}

	// 否则从 URL 下载（向后兼容）
	safeURL := redactURLForLog(item.URL)
	log.Printf("Downloading ruleset: %s from %s to %s (base: %s)", item.Name, safeURL, localPath, configDir)

	// 创建目标目录
	dir := filepath.Dir(localPath)
	log.Printf("Creating directory for ruleset: %s", dir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("Failed to create directory %s: %v", dir, err)
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// 创建自定义 Dialer，使用公共 DNS 服务器（避免使用 127.0.0.1:53 导致死锁）
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Resolver: &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				// 使用公共 DNS 服务器，避免解析死锁
				// 优先使用 Google DNS 8.8.8.8，备用阿里 DNS 223.5.5.5
				d := net.Dialer{Timeout: time.Second * 5}
				conn, err := d.DialContext(ctx, "udp", "8.8.8.8:53")
				if err != nil {
					// 如果 Google DNS 失败，尝试阿里 DNS
					conn, err = d.DialContext(ctx, "udp", "223.5.5.5:53")
				}
				return conn, err
			},
		},
	}

	// 创建带有自定义 DNS 解析器的 HTTP 客户端
	transport := &http.Transport{
		DialContext: dialer.DialContext,
	}
	client := http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}

	// 创建带有超时的上下文
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", item.URL, nil)
	if err != nil {
		log.Printf("Failed to create request for %s", safeURL)
		return fmt.Errorf("failed to create request for %s", safeURL)
	}

	// 设置一个标准的 User-Agent，以避免被某些服务器拒绝
	req.Header.Set("User-Agent", "configflow-agent/1.0")

	// 下载文件
	log.Printf("Starting download from: %s", safeURL)
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to download %s", safeURL)
		return fmt.Errorf("failed to download %s", safeURL)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Failed to download %s: status code %d", safeURL, resp.StatusCode)
		return fmt.Errorf("failed to download %s: status code %d", safeURL, resp.StatusCode)
	}

	// 保存文件
	log.Printf("Saving downloaded content to: %s", localPath)
	file, err := os.Create(localPath)
	if err != nil {
		log.Printf("Failed to create file %s: %v", localPath, err)
		return fmt.Errorf("failed to create file %s: %w", localPath, err)
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		log.Printf("Failed to save file %s: %v", localPath, err)
		return fmt.Errorf("failed to save file %s: %w", localPath, err)
	}

	log.Printf("Successfully downloaded ruleset: %s -> %s", safeURL, localPath)
	return nil
}

// downloadProviders 下载所有 provider 文件
func downloadProviders(configDir string, providers []ProviderDownloadItem) error {
	const maxConcurrentDownloads = 3 // 限制最大并发下载数
	log.Printf("Downloading %d providers with a maximum concurrency of %d", len(providers), maxConcurrentDownloads)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var failedDownloads []string

	// 使用带缓冲的channel作为信号量来限制并发
	semaphore := make(chan struct{}, maxConcurrentDownloads)

	for _, item := range providers {
		wg.Add(1)
		go func(provider ProviderDownloadItem) {
			defer wg.Done()

			// 获取信号量，如果channel满了则会阻塞
			semaphore <- struct{}{}
			defer func() { <-semaphore }() // 释放信号量

			if err := downloadProvider(configDir, provider); err != nil {
				log.Printf("Failed to download provider %s: %v", provider.Name, err)
				mu.Lock()
				failedDownloads = append(failedDownloads, provider.Name)
				mu.Unlock()
			} else {
				log.Printf("Successfully downloaded provider %s", provider.Name)
			}
		}(item)
	}

	wg.Wait()

	if len(failedDownloads) > 0 {
		log.Printf("Warning: %d providers failed to download: %v. Continuing with config update.", len(failedDownloads), failedDownloads)
	} else {
		log.Printf("Successfully downloaded all providers")
	}

	return nil
}

// downloadProvider 下载单个 provider 文件
func downloadProvider(configDir string, item ProviderDownloadItem) error {
	// 确保LocalPath是绝对路径
	localPath := item.LocalPath
	if !filepath.IsAbs(localPath) {
		// 如果是相对路径，使用实际的配置文件目录作为基础路径
		localPath = filepath.Join(configDir, localPath)
	}

	// 如果已提供内容，直接写入文件
	if item.Content != "" {
		log.Printf("Writing provider content directly: %s -> %s", item.Name, localPath)
		dir := filepath.Dir(localPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
		return os.WriteFile(localPath, []byte(item.Content), 0644)
	}

	// 否则从 URL 下载（向后兼容）
	safeURL := redactURLForLog(item.URL)
	log.Printf("Downloading provider: %s from %s to %s (base: %s)", item.Name, safeURL, localPath, configDir)

	// 创建目标目录
	dir := filepath.Dir(localPath)
	log.Printf("Creating directory for provider: %s", dir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("Failed to create directory %s: %v", dir, err)
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// 创建自定义 Dialer，使用公共 DNS 服务器（避免使用 127.0.0.1:53 导致死锁）
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Resolver: &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				// 使用公共 DNS 服务器，避免解析死锁
				// 优先使用 Google DNS 8.8.8.8，备用阿里 DNS 223.5.5.5
				d := net.Dialer{Timeout: time.Second * 5}
				conn, err := d.DialContext(ctx, "udp", "8.8.8.8:53")
				if err != nil {
					// 如果 Google DNS 失败，尝试阿里 DNS
					conn, err = d.DialContext(ctx, "udp", "223.5.5.5:53")
				}
				return conn, err
			},
		},
	}

	// 创建带有自定义 DNS 解析器的 HTTP 客户端
	transport := &http.Transport{
		DialContext: dialer.DialContext,
	}
	client := http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}

	// 创建带有超时的上下文
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "GET", item.URL, nil)
	if err != nil {
		log.Printf("Failed to create request for %s", safeURL)
		return fmt.Errorf("failed to create request for %s", safeURL)
	}

	// 下载文件
	log.Printf("Sending HTTP request to: %s", safeURL)
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to download %s", safeURL)
		return fmt.Errorf("failed to download %s", safeURL)
	}
	defer resp.Body.Close()

	// 检查状态码
	log.Printf("HTTP response status for %s: %d", safeURL, resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		log.Printf("Failed to download %s: status code %d", safeURL, resp.StatusCode)
		return fmt.Errorf("failed to download %s: status code %d", safeURL, resp.StatusCode)
	}

	// 保存文件
	log.Printf("Saving downloaded content to: %s", localPath)
	file, err := os.Create(localPath)
	if err != nil {
		log.Printf("Failed to create file %s: %v", localPath, err)
		return fmt.Errorf("failed to create file %s: %w", localPath, err)
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		log.Printf("Failed to save file %s: %v", localPath, err)
		return fmt.Errorf("failed to save file %s: %w", localPath, err)
	}

	log.Printf("Successfully downloaded provider: %s -> %s", safeURL, localPath)
	return nil
}

// writeCustomFiles 写入自定义文件（hosts 和单个规则）
func writeCustomFiles(configDir string, customFiles []CustomFileItem) error {
	log.Printf("Writing %d custom files...", len(customFiles))

	for _, item := range customFiles {
		// 确保 Path 是相对路径或绝对路径
		filePath := item.Path
		if !filepath.IsAbs(filePath) {
			// 如果是相对路径（如 ./rules/custom_hosts.txt），使用配置目录作为基础路径
			// 先移除可能的 ./ 前缀
			if len(filePath) >= 2 && filePath[:2] == "./" {
				filePath = filePath[2:]
			} else if len(filePath) >= 3 && filePath[:3] == ".\\" {
				filePath = filePath[2:]
			}
			filePath = filepath.Join(configDir, filePath)
		}

		log.Printf("Writing custom file: %s (original path: %s)", filePath, item.Path)

		// 创建文件所在目录
		dir := filepath.Dir(filePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Printf("Failed to create directory %s: %v", dir, err)
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}

		// 写入文件内容
		if err := os.WriteFile(filePath, []byte(item.Content), 0644); err != nil {
			log.Printf("Failed to write custom file %s: %v", filePath, err)
			return fmt.Errorf("failed to write custom file %s: %w", filePath, err)
		}

		log.Printf("Successfully wrote custom file: %s (size: %d bytes)", filePath, len(item.Content))
	}

	log.Printf("Successfully wrote all %d custom files", len(customFiles))
	return nil
}

// backupConfig 根据服务类型备份和清理配置文件和目录
// Mihomo: 只备份和删除 providers, ruleset, config.yaml
// MosDNS: 只备份和删除 rules, config.yaml
func backupConfig(configPath string, serviceType string) string {
	// 获取配置文件所在目录
	configDir := filepath.Dir(configPath)
	configFileName := filepath.Base(configPath)

	// 根据服务类型定义需要备份和清理的目标
	var targetsToBackup []string
	switch serviceType {
	case "mihomo":
		targetsToBackup = []string{"providers", "ruleset", configFileName}
		log.Printf("Service type: Mihomo - will backup and clean: providers/, ruleset/, %s", configFileName)
	case "mosdns":
		targetsToBackup = []string{"rules", configFileName}
		log.Printf("Service type: MosDNS - will backup and clean: rules/, %s", configFileName)
	default:
		// 默认备份所有内容（兼容其他服务类型）
		log.Printf("Service type: %s - will backup and clean all files", serviceType)
		targetsToBackup = nil // nil 表示备份所有
	}

	// 创建备份目录：backup/<timestamp>
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	backupRootDir := filepath.Join(configDir, "backup")
	backupDir := filepath.Join(backupRootDir, timestamp)

	log.Printf("Starting selective backup of directory %s to %s", configDir, backupDir)

	// 创建备份目录
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		log.Printf("Error: failed to create backup directory %s: %v", backupDir, err)
		log.Printf("Warning: backup failed, continuing without backup")
		return backupDir
	}

	// 读取配置目录下的所有文件和文件夹
	entries, err := os.ReadDir(configDir)
	if err != nil {
		log.Printf("Error: failed to read config directory: %v", err)
		log.Printf("Warning: backup failed, continuing without backup")
		return backupDir
	}

	// 辅助函数：检查是否需要备份此项
	shouldBackup := func(name string) bool {
		if name == "backup" {
			return false // 永远不备份 backup 目录本身
		}
		if targetsToBackup == nil {
			return true // nil 表示备份所有
		}
		for _, target := range targetsToBackup {
			if name == target {
				return true
			}
		}
		return false
	}

	// 遍历并备份指定的内容
	backupCount := 0
	for _, entry := range entries {
		if !shouldBackup(entry.Name()) {
			log.Printf("Skipping (not in backup list): %s", entry.Name())
			continue
		}

		sourcePath := filepath.Join(configDir, entry.Name())
		destPath := filepath.Join(backupDir, entry.Name())

		if entry.IsDir() {
			// 备份目录
			log.Printf("Backing up directory: %s", entry.Name())
			if err := copyDir(sourcePath, destPath); err != nil {
				log.Printf("Warning: failed to backup directory %s: %v", entry.Name(), err)
			} else {
				log.Printf("Successfully backed up directory: %s", entry.Name())
				backupCount++
			}
		} else {
			// 备份文件
			log.Printf("Backing up file: %s", entry.Name())
			if err := copyFile(sourcePath, destPath); err != nil {
				log.Printf("Warning: failed to backup file %s: %v", entry.Name(), err)
			} else {
				log.Printf("Successfully backed up file: %s", entry.Name())
				backupCount++
			}
		}
	}

	log.Printf("Backup completed successfully to %s (%d items backed up)", backupDir, backupCount)

	// 清理旧备份，只保留最新的3个
	cleanOldBackups(backupRootDir, 3)

	// 备份完成后，清理配置目录中已备份的内容
	log.Printf("Cleaning backed up items from config directory...")
	cleanCount := 0
	for _, entry := range entries {
		if !shouldBackup(entry.Name()) {
			log.Printf("Skipping cleanup (not in backup list): %s", entry.Name())
			continue
		}

		targetPath := filepath.Join(configDir, entry.Name())
		log.Printf("Removing: %s", targetPath)
		if err := os.RemoveAll(targetPath); err != nil {
			log.Printf("Warning: failed to remove %s: %v", targetPath, err)
		} else {
			log.Printf("Successfully removed: %s", targetPath)
			cleanCount++
		}
	}
	log.Printf("Config directory cleaned successfully (%d items removed)", cleanCount)

	return backupDir
}

// writeConfig 写入新配置
func writeConfig(cfg *Config, configContent string, backupPath string) error {
	log.Printf("Writing new config to %s", cfg.ConfigPath)
	configDir := filepath.Dir(cfg.ConfigPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		log.Printf("Error: failed to create config directory %s: %v", configDir, err)
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// 对于MosDNS，确保配置目录有正确的权限
	if cfg.ServiceType == "mosdns" {
		// 获取当前用户ID和组ID
		uid := os.Getuid()
		gid := os.Getgid()

		log.Printf("Setting ownership for %s to uid=%d, gid=%d", configDir, uid, gid)
		if err := os.Chown(configDir, uid, gid); err != nil {
			log.Printf("Warning: failed to set ownership for %s: %v", configDir, err)
		}

		// 递归设置目录中所有文件的权限
		err := filepath.Walk(configDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			return os.Chown(path, uid, gid)
		})
		if err != nil {
			log.Printf("Warning: failed to set ownership recursively for %s: %v", configDir, err)
		}
	}

	err := os.WriteFile(cfg.ConfigPath, []byte(configContent), 0644)
	if err != nil {
		log.Printf("Error: failed to write new config file: %v", err)
		// 尝试恢复备份
		if _, backupErr := os.Stat(backupPath); backupErr == nil {
			log.Printf("Attempting to restore backup...")
			os.Rename(backupPath, cfg.ConfigPath)
		}
		return fmt.Errorf("failed to write new config: %w", err)
	}

	return nil
}

// handleConfigUpdateAsync 异步处理配置更新请求
func handleConfigUpdateAsync(cfg *Config, req UpdateRequest) {
	log.Print("Legacy configuration update refused; use deployment protocol 1")
}

func handleConfigUpdate(cfg *Config, req UpdateRequest) error {
	return fmt.Errorf("legacy configuration update refused; use deployment protocol 1")
}

// copyFile 复制文件的辅助函数
func copyFile(src, dst string) error {
	// 打开源文件
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	// 创建目标文件
	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	// 复制内容
	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	// 同步写入
	err = destFile.Sync()
	return err
}

// copyDir 递归复制整个目录
func copyDir(src, dst string) error {
	// 获取源目录信息
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	// 创建目标目录
	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	// 读取源目录内容
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	// 遍历并复制所有内容
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			// 递归复制子目录
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			// 复制文件
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// cleanOldBackups 清理旧备份，只保留最新的 maxBackups 个备份
func cleanOldBackups(backupRootDir string, maxBackups int) {
	// 检查备份根目录是否存在
	if _, err := os.Stat(backupRootDir); os.IsNotExist(err) {
		return
	}

	// 读取备份目录
	entries, err := os.ReadDir(backupRootDir)
	if err != nil {
		log.Printf("Warning: failed to read backup directory: %v", err)
		return
	}

	// 过滤出备份目录（以时间戳命名的目录）
	type backupInfo struct {
		name      string
		timestamp int64
		path      string
	}
	var backups []backupInfo

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// 尝试解析目录名为时间字符串(格式: 2006-01-02_15-04-05)
		t, err := time.Parse("2006-01-02_15-04-05", entry.Name())
		if err == nil {
			backups = append(backups, backupInfo{
				name:      entry.Name(),
				timestamp: t.Unix(),
				path:      filepath.Join(backupRootDir, entry.Name()),
			})
		}
	}

	// 如果备份数量不超过最大值，不需要清理
	if len(backups) <= maxBackups {
		log.Printf("Current backups: %d, max: %d, no cleanup needed", len(backups), maxBackups)
		return
	}

	// 按时间戳排序（从旧到新）
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].timestamp < backups[j].timestamp
	})

	// 计算需要删除的备份数量
	toDelete := len(backups) - maxBackups
	log.Printf("Found %d backups, keeping %d, deleting %d oldest backups", len(backups), maxBackups, toDelete)

	// 删除最旧的备份
	for i := 0; i < toDelete; i++ {
		backup := backups[i]
		log.Printf("Deleting old backup: %s (timestamp: %d)", backup.name, backup.timestamp)
		if err := os.RemoveAll(backup.path); err != nil {
			log.Printf("Warning: failed to delete backup %s: %v", backup.name, err)
		} else {
			log.Printf("Successfully deleted old backup: %s", backup.name)
		}
	}
}

func (c *Config) agentIP() string {
	if c.AgentIPSource != nil {
		return c.AgentIPSource()
	}
	return c.AgentIP
}
