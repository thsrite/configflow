package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v3"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"agent/svc"
)

// GetLogsHandler 获取指定服务的日志
func GetLogsHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 默认获取100行日志
		lines := 100
		if lineStr := r.URL.Query().Get("lines"); lineStr != "" {
			if parsedLines, err := strconv.Atoi(lineStr); err == nil && parsedLines > 0 {
				lines = parsedLines
			}
		}

		// 按来源读取：路径由本机实际的服务定义解析
		if sourceID := r.URL.Query().Get("source"); sourceID != "" {
			source, ok := findLogSource(cfg, sourceID)
			if !ok {
				JsonResponse(w, http.StatusOK, map[string]interface{}{"success": false, "message": "未知的日志来源: " + sourceID})
				return
			}
			logs, err := ReadLogSource(source, lines)
			if err != nil {
				JsonResponse(w, http.StatusOK, map[string]interface{}{"success": false, "message": err.Error(), "source": source})
				return
			}
			JsonResponse(w, http.StatusOK, map[string]interface{}{
				"success":      true,
				"logs":         logs,
				"source":       source,
				"service_name": cfg.ServiceName,
			})
			return
		}

		// 如果指定了 log_path，直接读取该文件
		if logPath := r.URL.Query().Get("log_path"); logPath != "" {
			logs, err := ReadLogSource(LogSource{Kind: "file", Path: logPath}, lines)
			if err != nil {
				JsonResponse(w, http.StatusOK, map[string]interface{}{"success": false, "message": err.Error()})
				return
			}
			JsonResponse(w, http.StatusOK, map[string]interface{}{
				"success":      true,
				"logs":         logs,
				"service_name": cfg.ServiceName,
			})
			return
		}

		// 未指定路径，使用自动检测
		logs, errLogs := getLogDataWithAgentLog(cfg.ServiceType, cfg.ConfigPath, lines, AgentLogPath(cfg), cfg.ServiceUnit)

		JsonResponse(w, http.StatusOK, map[string]interface{}{
			"success":      true,
			"logs":         logs,
			"error_logs":   errLogs,
			"service_name": cfg.ServiceName,
		})
	}
}

// LogSourcesHandler 列出本机可读的日志来源
func LogSourcesHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		JsonResponse(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"sources": ResolveLogSources(cfg),
		})
	}
}

// ClearLogHandler 清空指定日志文件
func ClearLogHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			JsonResponse(w, http.StatusMethodNotAllowed, map[string]interface{}{
				"success": false,
				"message": "Method not allowed",
			})
			return
		}

		var req struct {
			LogPath string `json:"log_path"`
			Source  string `json:"source"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.LogPath == "" && req.Source == "") {
			JsonResponse(w, http.StatusBadRequest, map[string]interface{}{
				"success": false,
				"message": "source or log_path is required",
			})
			return
		}
		if req.Source != "" {
			source, ok := findLogSource(cfg, req.Source)
			if !ok || !source.Clearable {
				JsonResponse(w, http.StatusOK, map[string]interface{}{
					"success": false,
					"message": "该日志来源不支持清空",
				})
				return
			}
			req.LogPath = source.Path
		}

		// 检查文件是否存在
		info, err := os.Stat(req.LogPath)
		if os.IsNotExist(err) {
			JsonResponse(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"message": "日志文件不存在，无需清空",
			})
			return
		}
		// 只清空普通的日志文件，避免误清配置或设备文件
		if err != nil || !info.Mode().IsRegular() || !strings.Contains(filepath.Base(req.LogPath), ".log") {
			JsonResponse(w, http.StatusOK, map[string]interface{}{
				"success": false,
				"message": "只能清空文件名包含 .log 的普通文件",
			})
			return
		}

		// 清空文件内容
		if err := os.WriteFile(req.LogPath, []byte(""), 0644); err != nil {
			log.Printf("清空日志文件失败: %s, error: %v", req.LogPath, err)
			JsonResponse(w, http.StatusInternalServerError, map[string]interface{}{
				"success": false,
				"message": fmt.Sprintf("清空日志失败: %v", err),
			})
			return
		}

		log.Printf("日志文件已清空: %s", req.LogPath)
		JsonResponse(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "日志已清空",
		})
	}
}

// PortBasedLogsHandler 根据请求端口返回对应服务的日志
func PortBasedLogsHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 获取请求的端口
		_, portStr, _ := net.SplitHostPort(r.Host)
		if portStr == "" {
			if r.TLS == nil {
				portStr = "80"
			} else {
				portStr = "443"
			}
		}

		port, _ := strconv.Atoi(portStr)
		if port == 0 {
			port = cfg.AgentPort
		}

		// 确定服务类型和配置路径（根据端口动态确定）
		serviceType := cfg.ServiceType
		configPath := cfg.ConfigPath
		serviceName := cfg.ServiceName

		if cfg.NativeAIO || cfg.ServiceType == "oxidns" {
			// The configured core wins over legacy port heuristics.
		} else if port == getEnvPort("AGENT_MIHOMO_PORT", 8080) {
			serviceType = "mihomo"
			serviceName = "mihomo"
			configPath = "/etc/mihomo/config.yaml"
		} else if port == getEnvPort("AGENT_MOSDNS_PORT", 8081) {
			serviceType = "mosdns"
			serviceName = "mosdns"
			configPath = "/etc/mosdns/config.yaml"
		}

		// 读取日志
		lines := 100
		if lineStr := r.URL.Query().Get("lines"); lineStr != "" {
			if parsedLines, err := strconv.Atoi(lineStr); err == nil && parsedLines > 0 {
				lines = parsedLines
			}
		}

		// 如果指定了 log_path，直接读取该文件
		if logPath := r.URL.Query().Get("log_path"); logPath != "" {
			logs := readLogFile(logPath, lines)
			JsonResponse(w, http.StatusOK, map[string]interface{}{
				"success":      true,
				"logs":         escapeLogsForJSON(logs),
				"service_name": serviceName,
			})
			return
		}

		// 未指定路径，使用自动检测
		logs, errLogs := getLogDataWithAgentLog(serviceType, configPath, lines, AgentLogPath(cfg), cfg.ServiceUnit)

		JsonResponse(w, http.StatusOK, map[string]interface{}{
			"success":      true,
			"logs":         logs,
			"error_logs":   errLogs,
			"service_name": serviceName,
		})
	}
}

// getConfigDir 从配置文件路径中提取目录路径
func getConfigDir(configPath string) string {
	// 查找最后一个斜杠的位置
	lastSlash := strings.LastIndex(configPath, "/")
	if lastSlash == -1 {
		// 如果没有斜杠，返回当前目录
		return "."
	}

	// 返回斜杠之前的部分作为目录路径
	return configPath[:lastSlash]
}

// getLogData 获取指定服务的日志数据
func getLogData(serviceType string, configPath string, lines int, serviceUnits ...string) (string, string) {
	return getLogDataWithAgentLog(serviceType, configPath, lines, "/var/log/configflow-agent.log", serviceUnits...)
}

func getLogDataWithAgentLog(serviceType string, configPath string, lines int, agentLogPath string, serviceUnits ...string) (string, string) {
	if serviceType == "oxidns" {
		return getOxiDNSLogs(configPath, lines, serviceUnits...)
	}
	var logFilePath, errFilePath string

	// 统一的日志查找逻辑（适用于 mihomo 和 mosdns）
	// 按优先级查找日志文件
	// 1. Docker 镜像（s6-log，标准输出与错误输出合并在一个文件）
	containerLogPath := svc.LogFile(serviceType)

	// 2. ConfigFlow Agent 特殊路径 (Shell 安装)
	// 3. 系统服务路径 (OpenRC/systemd)
	systemLogPath := fmt.Sprintf("/var/log/%s.log", serviceType)

	// 4. 配置目录路径（MosDNS 特有，作为最后的 fallback）
	var configDirLogPath, configDirErrPath string
	if serviceType == "mosdns" {
		configDir := getConfigDir(configPath)
		configDirLogPath = findMosdnsStdLog(configDir)
		configDirErrPath = findMosdnsErrLog(configDir)
	}

	// 按优先级检查哪个日志文件存在
	if _, err := os.Stat(containerLogPath); containerLogPath != "" && err == nil {
		// Docker 镜像日志存在
		logFilePath = containerLogPath
		errFilePath = ""
	} else if _, err := os.Stat(agentLogPath); err == nil {
		// ConfigFlow Agent 日志存在 (Shell 安装)
		logFilePath = agentLogPath
		errFilePath = "" // Shell 安装不区分标准输出和错误输出
	} else if _, err := os.Stat(systemLogPath); err == nil {
		// 系统服务日志存在
		logFilePath = systemLogPath
		errFilePath = ""
	} else if serviceType == "mosdns" && configDirLogPath != "" {
		// MosDNS 特有：尝试配置目录下的日志
		if _, err := os.Stat(configDirLogPath); err == nil {
			logFilePath = configDirLogPath
			errFilePath = configDirErrPath
		} else {
			// 默认使用 Docker 镜像路径（即使文件不存在）
			logFilePath = containerLogPath
		}
	} else {
		// 默认使用 Docker 镜像路径（即使文件不存在）
		logFilePath = containerLogPath
	}

	logs := readLogFile(logFilePath, lines)
	errLogs := readLogFile(errFilePath, lines)

	return escapeLogsForJSON(logs), escapeLogsForJSON(errLogs)
}

// Read OxiDNS core logs rather than presenting the Agent's unrelated log.
func getOxiDNSLogs(configPath string, lines int, serviceUnits ...string) (string, string) {
	if lines > 10000 {
		lines = 10000
	}
	if data, err := os.ReadFile(configPath); err == nil {
		var doc struct {
			Log struct {
				File string `yaml:"file"`
			} `yaml:"log"`
		}
		if yaml.Unmarshal(data, &doc) == nil && doc.Log.File != "" {
			path := doc.Log.File
			if !filepath.IsAbs(path) {
				path = filepath.Join(filepath.Dir(configPath), path)
			}
			if _, err := os.Stat(path); err == nil {
				return readLogFile(path, lines), ""
			}
		}
	}
	for _, path := range []string{svc.LogFile("oxidns"), filepath.Join(filepath.Dir(configPath), "oxidns.log"), "/var/log/oxidns.log"} {
		if _, err := os.Stat(path); err == nil {
			return readLogFile(path, lines), ""
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	unit := "oxidns"
	if len(serviceUnits) > 0 && serviceUnits[0] != "" {
		unit = serviceUnits[0]
	}
	if data, err := exec.CommandContext(ctx, "journalctl", "-u", unit, "-n", strconv.Itoa(lines), "--no-pager", "-o", "cat").Output(); err == nil {
		return string(data), ""
	}
	return "OxiDNS log file or system journal unavailable", ""
}

// findMosdnsStdLog 查找mosdns标准日志文件
func findMosdnsStdLog(dir string) string {
	// 优先查找 mosdns.log 文件
	logPath := fmt.Sprintf("%s/mosdns.log", dir)
	if _, err := os.Stat(logPath); err == nil {
		return logPath
	}

	// 如果 mosdns.log 不存在，检查目录是否存在
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		// 目录不存在，返回默认路径
		return logPath
	}

	// 读取目录内容，查找其他可能的日志文件
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		// 读取目录失败，返回默认路径
		return logPath
	}

	// 查找标准日志文件（必须以 .log 结尾，但不能是 .err.log 或 .backup.）
	for _, file := range files {
		fileName := file.Name()
		if !file.IsDir() &&
			strings.HasSuffix(fileName, ".log") &&
			!strings.Contains(fileName, ".err.log") &&
			!strings.Contains(fileName, ".backup.") {
			return fmt.Sprintf("%s/%s", dir, fileName)
		}
	}

	// 未找到匹配的文件，返回默认路径
	return logPath
}

// findMosdnsErrLog 查找mosdns错误日志文件
func findMosdnsErrLog(dir string) string {
	// 优先查找 mosdns.err.log 文件
	errLogPath := fmt.Sprintf("%s/mosdns.err.log", dir)
	if _, err := os.Stat(errLogPath); err == nil {
		return errLogPath
	}

	// 如果 mosdns.err.log 不存在，检查目录是否存在
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		// 目录不存在，返回默认路径
		return errLogPath
	}

	// 读取目录内容，查找其他可能的错误日志文件
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		// 读取目录失败，返回默认路径
		return errLogPath
	}

	// 查找错误日志文件（必须以 .err.log 结尾，但不能包含 .backup.）
	for _, file := range files {
		fileName := file.Name()
		if !file.IsDir() &&
			strings.HasSuffix(fileName, ".err.log") &&
			!strings.Contains(fileName, ".backup.") {
			return fmt.Sprintf("%s/%s", dir, fileName)
		}
	}

	// 未找到匹配的文件，返回默认路径
	return errLogPath
}

// readLogFile 读取指定的日志文件
func readLogFile(filePath string, lines int) string {
	if _, err := os.Stat(filePath); err == nil {
		cmd := exec.Command("tail", fmt.Sprintf("-%d", lines), filePath)
		output, err := cmd.CombinedOutput()
		if err == nil {
			return string(output)
		}
	}
	return ""
}

// escapeLogsForJSON 转义日志内容用于JSON
func escapeLogsForJSON(logs string) string {
	// 先尝试解码可能的URL编码内容
	logs = tryDecodeURL(logs)

	// 确保日志内容是有效的UTF-8
	logs = ensureValidUTF8(logs)

	// 不需要手动转义，json.Encode会自动处理
	// 但我们需要确保日志中的换行符能被正确处理
	return logs
}

// tryDecodeURL 尝试解码URL编码的内容
func tryDecodeURL(logs string) string {
	// 查找并解码可能的URL编码内容
	re := regexp.MustCompile(`%[0-9A-Fa-f]{2}`)

	// 只对可能的URL编码部分进行解码
	decoded := re.ReplaceAllStringFunc(logs, func(s string) string {
		decoded, err := url.QueryUnescape(s)
		if err != nil {
			// 解码失败则返回原字符串
			return s
		}
		return decoded
	})

	return decoded
}

// ensureValidUTF8 确保字符串是有效的UTF-8
func ensureValidUTF8(s string) string {
	// 如果已经是有效的UTF-8，直接返回
	if utf8.ValidString(s) {
		return s
	}

	// 尝试修复无效的UTF-8序列
	buf := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			// 无效的UTF-8字节，替换为问号
			buf = append(buf, '?')
			i++
		} else {
			// 有效的UTF-8字符
			buf = append(buf, s[i:i+size]...)
			i += size
		}
	}
	return string(buf)
}

// getEnvPort 从环境变量获取端口配置
func getEnvPort(envVar string, defaultPort int) int {
	if envPort := os.Getenv(envVar); envPort != "" {
		if parsedPort, err := strconv.Atoi(envPort); err == nil {
			return parsedPort
		}
	}
	return defaultPort
}
