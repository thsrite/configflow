package routes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"log"
	"net"
	"regexp"
	"net/url"
	"unicode/utf8"
	"os"
	"strings"
	"io/ioutil"
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

		// 如果指定了 log_path，直接读取该文件
		if logPath := r.URL.Query().Get("log_path"); logPath != "" {
			logs := readLogFile(logPath, lines)
			JsonResponse(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"logs":    escapeLogsForJSON(logs),
				"service_name": cfg.ServiceName,
			})
			return
		}

		// 未指定路径，使用自动检测
		logs, errLogs := getLogData(cfg.ServiceType, cfg.ConfigPath, lines)

		JsonResponse(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"logs": logs,
			"error_logs": errLogs,
			"service_name": cfg.ServiceName,
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
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LogPath == "" {
			JsonResponse(w, http.StatusBadRequest, map[string]interface{}{
				"success": false,
				"message": "log_path is required",
			})
			return
		}

		// 检查文件是否存在
		if _, err := os.Stat(req.LogPath); os.IsNotExist(err) {
			JsonResponse(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"message": "日志文件不存在，无需清空",
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

		if port == getEnvPort("AGENT_MIHOMO_PORT", 8080) {
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
				"success": true,
				"logs":    escapeLogsForJSON(logs),
				"service_name": serviceName,
			})
			return
		}

		// 未指定路径，使用自动检测
		logs, errLogs := getLogData(serviceType, configPath, lines)

		JsonResponse(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"logs": logs,
			"error_logs": errLogs,
			"service_name": serviceName,
		})
	}
}

// checkMosdnsStatus 检查 mosdns 服务状态
func checkMosdnsStatus() (bool, string) {
	cmd := exec.Command("supervisorctl", "-c", "/etc/supervisor/supervisord.conf", "status", "mosdns")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)
	
	// 即使命令返回错误，我们也检查输出内容
	// 因为 supervisorctl 在服务处于 FATAL 状态时也会返回非零退出码
	if strings.Contains(outputStr, "RUNNING") {
		return true, ""
	}
	
	// 如果服务不是 RUNNING 状态，返回错误信息
	if err != nil {
		return false, fmt.Sprintf("mosdns 服务未运行: %s", outputStr)
	}
	
	return false, fmt.Sprintf("mosdns 服务未运行: %s", outputStr)
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
func getLogData(serviceType string, configPath string, lines int) (string, string) {
	var logFilePath, errFilePath string

	// 统一的日志查找逻辑（适用于 mihomo 和 mosdns）
	// 按优先级查找日志文件
	// 1. Supervisor 路径 (Docker 环境)
	supervisorLogPath := fmt.Sprintf("/var/log/supervisor/%s.log", serviceType)
	supervisorErrPath := fmt.Sprintf("/var/log/supervisor/%s.err.log", serviceType)

	// 2. ConfigFlow Agent 特殊路径 (Shell 安装)
	agentLogPath := "/var/log/configflow-agent.log"

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
	if _, err := os.Stat(supervisorLogPath); err == nil {
		// Supervisor 日志存在（Docker 环境）
		logFilePath = supervisorLogPath
		errFilePath = supervisorErrPath
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
			// 默认使用 supervisor 路径（即使文件不存在）
			logFilePath = supervisorLogPath
			errFilePath = supervisorErrPath
		}
	} else {
		// 默认使用 supervisor 路径（即使文件不存在）
		logFilePath = supervisorLogPath
		errFilePath = supervisorErrPath
	}

	logs := readLogFile(logFilePath, lines)
	errLogs := readLogFile(errFilePath, lines)

	return escapeLogsForJSON(logs), escapeLogsForJSON(errLogs)
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