package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"agent/internal/localip"
)

var detectLocalIPv4 = localip.Detect
var detectLocalIPv4Except = localip.DetectExcept
var detectAllLocalIPv4 = localip.DetectAll

// getLocalIP 优先使用 LAN IPv4，兼容仅有 VPN 或点对点接口的注册。
// excluded 是本机可能临时持有的 DNS 高可用虚拟 IP，不能当作 Agent 地址上报。
func getLocalIP(excluded ...string) (string, error) {
	detect := detectLocalIPv4
	if len(excluded) > 0 {
		detect = func() (string, error) { return detectLocalIPv4Except(excluded) }
	}
	ip, err := detect()
	if err != nil {
		log.Printf("Failed to detect local IPv4 address: %v", err)
		return "", err
	}
	log.Printf("Found local IPv4 address: %s", ip)
	return ip, nil
}

// checkServiceWithSystemctl 使用 systemctl 检查服务状态
func checkServiceWithSystemctl(serviceName string) (string, bool) {
	// 先检查 systemctl 是否存在
	if _, err := exec.LookPath("systemctl"); err != nil {
		// systemctl 不存在，直接返回 false，不输出错误日志
		return "", false
	}

	cmd := exec.Command("systemctl", "is-active", "--quiet", serviceName)
	if err := cmd.Run(); err == nil {
		logDebugf("Service %s is active (systemctl)", serviceName)
		return "active", true
	}
	return "", false
}

// checkServiceWithOpenRC 使用 rc-service 检查服务状态 (Alpine Linux)
func checkServiceWithOpenRC(serviceName string) (string, bool) {
	// 先检查 rc-service 是否存在
	if _, err := exec.LookPath("rc-service"); err != nil {
		// rc-service 不存在，直接返回 false，不输出错误日志
		return "", false
	}

	cmd := exec.Command("rc-service", serviceName, "status")
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	logDebugf("rc-service output for %s: %s", serviceName, outputStr)

	// 检查服务状态
	if err == nil && (strings.Contains(outputStr, "started") || strings.Contains(outputStr, "running")) {
		logDebugf("Service %s is active (rc-service)", serviceName)
		return "active", true
	} else if strings.Contains(outputStr, "stopped") || strings.Contains(outputStr, "crashed") {
		log.Printf("Service %s is inactive (rc-service)", serviceName)
		return "inactive", true
	}

	return "", false
}

// checkServiceWithSupervisorctl 使用 supervisorctl 检查服务状态
func checkServiceWithSupervisorctl(serviceName string) (string, bool) {
	// 先检查 supervisorctl 是否存在
	if _, err := exec.LookPath("supervisorctl"); err != nil {
		// supervisorctl 不存在，直接返回 false，不输出错误日志
		return "", false
	}

	cmd := exec.Command("supervisorctl", "-c", "/etc/supervisor/supervisord.conf", "status", serviceName)
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	// 无论命令是否成功，都检查输出内容
	log.Printf("Supervisorctl output for %s: %s", serviceName, outputStr)

	// 检查输出中是否包含服务状态信息
	if strings.Contains(outputStr, serviceName) {
		// 检查输出中是否包含 RUNNING
		if strings.Contains(outputStr, "RUNNING") {
			logDebugf("Service %s is active (supervisorctl)", serviceName)
			return "active", true
		} else if strings.Contains(outputStr, "FATAL") {
			log.Printf("Service %s is in FATAL state (supervisorctl) - reporting as inactive", serviceName)
			// FATAL 状态表示服务启动失败，应该返回 inactive
			return "inactive", true
		} else if strings.Contains(outputStr, "STOPPED") {
			log.Printf("Service %s is STOPPED (supervisorctl)", serviceName)
			return "inactive", true
		} else if strings.Contains(outputStr, "STARTING") {
			log.Printf("Service %s is STARTING (supervisorctl)", serviceName)
			// 将 STARTING 状态统一为 active，以保持与服务器一致
			return "active", true
		} else {
			log.Printf("Service %s is in unknown state (supervisorctl): %s", serviceName, outputStr)
			// 将 unknown 状态统一为 inactive，以保持与服务器一致
			return "inactive", true
		}
	} else {
		// 如果输出中不包含服务名称，说明命令执行失败
		log.Printf("Supervisorctl command failed for %s: %v, output: %s", serviceName, err, outputStr)
		// 命令执行失败时，不进行进程检查，直接返回 false 让其他方法处理
		return "", false
	}
}

// checkServiceWithProcess 检查服务进程是否存在
func checkServiceWithProcess(serviceName string) (string, bool) {
	cmd := exec.Command("sh", "-c", "ps aux | grep "+serviceName+" | grep -v grep")
	if err := cmd.Run(); err == nil {
		log.Printf("Service %s process exists (process check)", serviceName)
		// 进程存在，视为 active
		return "active", true
	}
	return "", false
}

// getServiceStatus 检查服务状态（支持多种 init 系统）
func getServiceStatus(serviceName string) string {
	logDebugf("Checking service status for: %s", serviceName)

	// 首先尝试使用 systemctl (systemd)
	if status, found := checkServiceWithSystemctl(serviceName); found {
		return status
	}

	// 尝试使用 rc-service (OpenRC/Alpine)
	if status, found := checkServiceWithOpenRC(serviceName); found {
		return status
	}

	// 尝试使用 supervisorctl (Supervisor)
	if status, found := checkServiceWithSupervisorctl(serviceName); found {
		return status
	}

	// 最后尝试直接检查进程
	if status, found := checkServiceWithProcess(serviceName); found {
		return status
	}

	// 如果所有方法都失败，返回 inactive
	log.Printf("Service %s is not running", serviceName)
	return "inactive"
}

// executeCommand 执行一个shell命令
func executeCommand(command string) ([]byte, error) {
	log.Printf("Executing shell command: %s", command)

	// 特殊处理 supervisorctl 命令，确保使用正确的配置文件路径
	if strings.Contains(command, "supervisorctl") && !strings.Contains(command, "-c") {
		// 在命令中添加配置文件路径
		command = strings.Replace(command, "supervisorctl", "supervisorctl -c /etc/supervisor/supervisord.conf", 1)
		log.Printf("Modified supervisorctl command: %s", command)
	}

	cmd := exec.Command("sh", "-c", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Command failed: %v, output: %s", err, string(output))
		// 返回错误信息和输出，而不是直接返回错误
		return output, fmt.Errorf("command failed: %v, output: %s", err, string(output))
	}
	log.Printf("Command executed successfully")
	return output, nil
}

// writeLogToFile 将日志写入指定文件
func writeLogToFile(message, logFile string) {
	log.Printf("Writing log message to file: %s", logFile)

	// 确保日志目录存在
	logDir := filepath.Dir(logFile)
	log.Printf("Ensuring log directory exists: %s", logDir)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.Printf("Failed to create log directory %s: %v", logDir, err)
		return
	}

	// 打开或创建日志文件（追加模式）
	log.Printf("Opening log file: %s", logFile)
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("Failed to open log file %s: %v", logFile, err)
		return
	}
	defer file.Close()

	// 写入日志消息
	log.Printf("Writing message to log file")
	if _, err := file.WriteString(message + "\n"); err != nil {
		log.Printf("Failed to write to log file %s: %v", logFile, err)
	}
}
