package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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
	ServerURL          string `json:"server_url"`
	AgentName          string `json:"agent_name"`
	AgentHost          string `json:"agent_host"`
	AgentPort          int    `json:"agent_port"`
	AgentIP            string `json:"agent_ip,omitempty"`
	ServiceType        string `json:"service_type"`
	ServiceName        string `json:"service_name"`
	ConfigPath         string `json:"config_path"`
	RestartCommand     string `json:"restart_command"`
	HeartbeatInterval  int    `json:"heartbeat_interval"`
	AgentID            string `json:"agent_id,omitempty"`
	Token              string `json:"token,omitempty"`
	
	// MosDNS 特殊功能字段
	Directories       []string              `json:"directories,omitempty"`
	RulesetDownloads  []RulesetDownloadItem `json:"ruleset_downloads,omitempty"`
	
	filePath string
}

// ConfigUpdateHandler 处理配置更新请求
func ConfigUpdateHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Config update requested from %s", r.RemoteAddr)
		
		var req UpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("Failed to decode request body: %v", err)
			JsonResponse(w, http.StatusBadRequest, map[string]string{"success": "false", "message": "Invalid request body"})
			return
		}
		
		log.Printf("Request parsed. Config length: %d, Directories: %d, RulesetDownloads: %d, CustomFiles: %d",
			len(req.Config), len(req.Directories), len(req.RulesetDownloads), len(req.CustomFiles))

		// 立即启动后台配置更新任务
		go handleConfigUpdateAsync(cfg, req)

		// 立即返回成功响应
		log.Printf("Config update task started in background.")
		JsonResponse(w, http.StatusOK, map[string]string{"success": "true", "message": "Config update task started"})
	}
}

// handleMosdnsFeatures 处理MosDNS特殊功能
func handleMosdnsFeatures(cfg *Config, req UpdateRequest) error {
	// 获取实际的配置文件目录（从 ConfigPath 中提取）
	configDir := filepath.Dir(cfg.ConfigPath)

	// 1. 创建 cache.dump 文件（如果不存在）
	cacheDumpPath := filepath.Join(configDir, "cache.dump")
	if _, err := os.Stat(cacheDumpPath); os.IsNotExist(err) {
		log.Printf("cache.dump not found, creating empty file: %s", cacheDumpPath)
		// 创建空的 cache.dump 文件
		file, err := os.Create(cacheDumpPath)
		if err != nil {
			log.Printf("Warning: failed to create cache.dump: %v", err)
			// 不返回错误，只记录警告，因为这不是致命错误
		} else {
			file.Close()
			// 设置文件权限为 0644
			if err := os.Chmod(cacheDumpPath, 0644); err != nil {
				log.Printf("Warning: failed to set permissions for cache.dump: %v", err)
			}
			log.Printf("Successfully created empty cache.dump file")
		}
	} else {
		log.Printf("cache.dump already exists: %s", cacheDumpPath)
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
	log.Printf("Downloading ruleset: %s from %s to %s (base: %s)", item.Name, item.URL, localPath, configDir)
	
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
		log.Printf("Failed to create request for %s: %v", item.URL, err)
		return fmt.Errorf("failed to create request for %s: %w", item.URL, err)
	}

	// 设置一个标准的 User-Agent，以避免被某些服务器拒绝
	req.Header.Set("User-Agent", "configflow-agent/1.0")

	// 下载文件
	log.Printf("Starting download from: %s", item.URL)
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to download %s: %v", item.URL, err)
		return fmt.Errorf("failed to download %s: %w", item.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Failed to download %s: status code %d", item.URL, resp.StatusCode)
		return fmt.Errorf("failed to download %s: status code %d", item.URL, resp.StatusCode)
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

	log.Printf("Successfully downloaded ruleset: %s -> %s", item.URL, localPath)
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
	log.Printf("Downloading provider: %s from %s to %s (base: %s)", item.Name, item.URL, localPath, configDir)

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
		log.Printf("Failed to create request for %s: %v", item.URL, err)
		return fmt.Errorf("failed to create request for %s: %w", item.URL, err)
	}

	// 下载文件
	log.Printf("Sending HTTP request to: %s", item.URL)
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to download %s: %v", item.URL, err)
		return fmt.Errorf("failed to download %s: %w", item.URL, err)
	}
	defer resp.Body.Close()

	// 检查状态码
	log.Printf("HTTP response status for %s: %d", item.URL, resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		log.Printf("Failed to download %s: status code %d", item.URL, resp.StatusCode)
		return fmt.Errorf("failed to download %s: status code %d", item.URL, resp.StatusCode)
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

	log.Printf("Successfully downloaded provider: %s -> %s", item.URL, localPath)
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

// modifyMihomoConfig 修改 mihomo 配置中的 DNS 端口
func modifyMihomoConfig(configContent string) (string, error) {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(configContent), &root); err != nil {
		return "", fmt.Errorf("failed to unmarshal mihomo config: %w", err)
	}

	// 修改 tun.dns-hijack
	if tunNode := findNode(&root, "tun"); tunNode != nil {
		if dnsHijackNode := findNode(tunNode, "dns-hijack"); dnsHijackNode != nil {
			// 确保节点是序列节点
			dnsHijackNode.Kind = yaml.SequenceNode
			dnsHijackNode.Content = []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "any:1053", Style: yaml.FlowStyle},
				{Kind: yaml.ScalarNode, Value: "tcp://any:1053", Style: yaml.FlowStyle},
			}
		}
	}

	// 修改 dns.listen
	if dnsNode := findNode(&root, "dns"); dnsNode != nil {
		if listenNode := findNode(dnsNode, "listen"); listenNode != nil {
			listenNode.Value = "0.0.0.0:1053"
		}
	}

	modifiedContent, err := yaml.Marshal(&root)
	if err != nil {
		return "", fmt.Errorf("failed to marshal modified mihomo config: %w", err)
	}

	return string(modifiedContent), nil
}

// findNode 在 YAML 节点中查找指定键的子节点
func findNode(parent *yaml.Node, key string) *yaml.Node {
	// 如果父节点是文档节点，则查找其内容中的第一个映射节点
	if parent.Kind == yaml.DocumentNode {
		if len(parent.Content) > 0 {
			parent = parent.Content[0]
		} else {
			return nil
		}
	}

	if parent.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			return parent.Content[i+1]
		}
	}
	return nil
}

// handleConfigUpdateAsync 异步处理配置更新请求
func handleConfigUpdateAsync(cfg *Config, req UpdateRequest) {
	log.Printf("Starting async config update...")

	// 检查是否需要修改 mihomo 配置（在备份之前处理）
	if cfg.ServiceType == "mihomo" && os.Getenv("ENABLE_MOSDNS") == "true" {
		log.Printf("ENABLE_MOSDNS is true, modifying mihomo config...")
		modifiedConfig, err := modifyMihomoConfig(req.Config)
		if err != nil {
			log.Printf("Error modifying mihomo config asynchronously: %v", err)
			return
		}
		req.Config = modifiedConfig
		log.Printf("Mihomo config modified successfully.")
	}

	// 第一步：备份旧配置（根据服务类型有选择地备份和清理）
	log.Printf("Step 1: Backing up and cleaning old config...")
	backupPath := backupConfig(cfg.ConfigPath, cfg.ServiceType)

	// 第二步：下载 providers/rulesets（在清理之后）
	// 只有 MosDNS 服务才处理 MosDNS 特殊功能
	if cfg.ServiceType == "mosdns" {
		log.Printf("Step 2: Handling MosDNS features (after backup)...")
		if err := handleMosdnsFeatures(cfg, req); err != nil {
			log.Printf("Error handling MosDNS features asynchronously: %v", err)
			return
		}
	}

	// 只有 Mihomo 服务才处理 Mihomo 特殊功能
	if cfg.ServiceType == "mihomo" {
		log.Printf("Step 2: Handling Mihomo features (after backup)...")
		if err := handleMihomoFeatures(cfg, req); err != nil {
			log.Printf("Error handling Mihomo features asynchronously: %v", err)
			return
		}
	}

	// 第三步：写入新配置
	log.Printf("Step 3: Writing new config...")
	if err := writeConfig(cfg, req.Config, backupPath); err != nil {
		log.Printf("Error writing config file asynchronously: %v", err)
		return
	}

	log.Printf("Async config update completed successfully.")
}

// handleConfigUpdate 处理配置更新请求 (同步版本，保留用于兼容)
func handleConfigUpdate(cfg *Config, req UpdateRequest) error {
	// 检查是否需要修改 mihomo 配置（在备份之前处理）
	if cfg.ServiceType == "mihomo" && os.Getenv("ENABLE_MOSDNS") == "true" {
		log.Printf("ENABLE_MOSDNS is true, modifying mihomo config...")
		modifiedConfig, err := modifyMihomoConfig(req.Config)
		if err != nil {
			log.Printf("Error modifying mihomo config: %v", err)
			return fmt.Errorf("failed to modify mihomo config: %w", err)
		}
		req.Config = modifiedConfig
		log.Printf("Mihomo config modified successfully.")
	}

	// 第一步：备份旧配置（根据服务类型有选择地备份和清理）
	log.Printf("Step 1: Backing up and cleaning old config...")
	backupPath := backupConfig(cfg.ConfigPath, cfg.ServiceType)

	// 第二步：下载 providers/rulesets（在清理之后）
	// 处理 MosDNS 特殊功能
	if cfg.ServiceType == "mosdns" {
		log.Printf("Step 2: Handling MosDNS features (after backup)...")
		if err := handleMosdnsFeatures(cfg, req); err != nil {
			return err
		}
	}

	// 处理 Mihomo 特殊功能
	if cfg.ServiceType == "mihomo" {
		log.Printf("Step 2: Handling Mihomo features (after backup)...")
		if err := handleMihomoFeatures(cfg, req); err != nil {
			return err
		}
	}

	// 第三步：写入新配置
	log.Printf("Step 3: Writing new config...")
	if err := writeConfig(cfg, req.Config, backupPath); err != nil {
		return err
	}

	return nil
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