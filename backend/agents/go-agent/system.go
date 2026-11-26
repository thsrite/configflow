package main

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	"agent/internal/localip"
	"agent/svc"
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
		logger.Errorf("Failed to detect local IPv4 address: %v", err)
		return "", err
	}
	logger.Infof("Found local IPv4 address: %s", ip)
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
		logger.Warnf("Service %s is inactive (rc-service)", serviceName)
		return "inactive", true
	}

	return "", false
}

// checkServiceWithContainer reads the state of a program supervised by the
// Docker image (s6). Outside the image the program is unknown.
func checkServiceWithContainer(serviceName string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	status, err := svc.NewS6().Status(ctx, serviceName)
	if err != nil {
		if !errors.Is(err, svc.ErrUnknown) {
			logger.Infof("Container service status of %s unavailable: %v", serviceName, err)
		}
		return "", false
	}
	// STARTING counts as active, as the panel expects.
	if status.State == svc.StateRunning || status.State == svc.StateStarting {
		logDebugf("Service %s is active (container)", serviceName)
		return "active", true
	}
	logger.Infof("Service %s is %s (container)", serviceName, status.State)
	return "inactive", true
}

// checkServiceWithProcess 检查服务进程是否存在
func checkServiceWithProcess(serviceName string) (string, bool) {
	cmd := exec.Command("sh", "-c", "ps aux | grep "+serviceName+" | grep -v grep")
	if err := cmd.Run(); err == nil {
		logger.Infof("Service %s process exists (process check)", serviceName)
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

	// Docker 镜像内由 s6 管理的程序
	if status, found := checkServiceWithContainer(serviceName); found {
		return status
	}

	// 最后尝试直接检查进程
	if status, found := checkServiceWithProcess(serviceName); found {
		return status
	}

	// 如果所有方法都失败，返回 inactive
	logger.Warnf("Service %s is not running", serviceName)
	return "inactive"
}
