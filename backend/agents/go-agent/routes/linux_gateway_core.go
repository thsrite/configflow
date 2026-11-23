package routes

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"time"
)

// Old cores silently ignore unknown TUN fields. Their successful -t check is
// therefore insufficient: auto-route without route-address-set captures all IPs.
var gatewayCoreVersion = func(binary string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, binary, "-v").CombinedOutput()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return string(output), err
}

func checkLinuxGatewayCore(cfg *Config, doc map[string]any) error {
	marker, _ := doc["x-configflow-linux-gateway"].(map[string]any)
	if marker["enabled"] != true {
		return nil
	}
	binary := cfg.ServiceBinary
	if binary == "" {
		binary = "mihomo"
	}
	output, err := gatewayCoreVersion(binary)
	if err != nil {
		return fmt.Errorf("无法检查 Mihomo 核心版本；Linux 网关要求 Mihomo 稳定版 v1.19.0 或更新，请先检查核心可执行文件")
	}
	version := regexp.MustCompile(`(?i)\bmihomo\s+(?:meta\s+)?v(\d+)\.(\d+)\.(\d+)(?:\s|$)`).FindStringSubmatch(output)
	if len(version) != 4 {
		return fmt.Errorf("无法确认 Mihomo 核心支持选择性接管；请使用稳定版 v1.19.0 或更新版本（不自动接受未知版本或 alpha 构建）")
	}
	major, _ := strconv.Atoi(version[1])
	minor, _ := strconv.Atoi(version[2])
	if major < 1 || (major == 1 && minor < 19) {
		return fmt.Errorf("Mihomo 核心过旧，可能忽略选择性接管并接管全量流量；请先升级至稳定版 v1.19.0 或更新版本，再发布网关配置")
	}
	return nil
}
