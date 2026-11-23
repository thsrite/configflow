package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// The host can acquire IPv6 after Mihomo has initialized its TUN. Reading only
// the YAML would then misreport Fake-IPv6 readiness, including after a reboot.
type ipv6TUNReport struct {
	Required  bool     `json:"required"`
	Status    string   `json:"status"`
	Device    string   `json:"device,omitempty"`
	Addresses []string `json:"addresses"`
	Message   string   `json:"message"`
}

var ipv6CoreRunning = ipv6ManagedCoreRunning
var ipv6TUNPause = time.Sleep
var ipv6TUNReloadTimeout = 12 * time.Second

// Keep this read bounded independently of the longer deployment lifecycle
// timeout. A stopped/unknown service is never started by IPv6 preparation.
func ipv6ManagedCoreRunning(cfg *Config) (bool, error) {
	if cfg == nil || !regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@:-]*$`).MatchString(cfg.ServiceUnit) {
		return false, fmt.Errorf("无法识别受管 Mihomo 服务")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var command *exec.Cmd
	switch cfg.ServiceManager {
	case "systemd":
		command = exec.CommandContext(ctx, "systemctl", "is-active", "--", cfg.ServiceUnit)
	case "openrc":
		command = exec.CommandContext(ctx, "rc-service", cfg.ServiceUnit, "status")
	default:
		return false, fmt.Errorf("仅支持原生受管 Mihomo 服务的启动恢复")
	}
	output, err := command.Output()
	if ctx.Err() != nil {
		return false, fmt.Errorf("受管 Mihomo 服务状态检查超时")
	}
	if cfg.ServiceManager == "systemd" {
		return err == nil && strings.TrimSpace(string(output)) == "active", nil
	}
	return err == nil, nil
}

func ipv6TUNStatus(cfg *Config, network ipv6Network) ipv6TUNReport {
	result := ipv6TUNReport{Status: "not_required", Addresses: []string{}, Message: "当前配置未启用客户端 IPv6 Fake-IP TUN 接入"}
	if cfg == nil || cfg.ServiceType != "mihomo" {
		return result
	}
	doc, _, err := readYAMLDocument(cfg.ConfigPath)
	if err != nil {
		result.Status = "unknown"
		result.Message = "无法读取 Mihomo 配置，尚未检查实际 TUN IPv6"
		return result
	}
	dns, _ := doc["dns"].(map[string]any)
	tun, _ := doc["tun"].(map[string]any)
	fake, _ := dns["fake-ip-range6"].(string)
	ipv6, explicit := doc["ipv6"]
	if !explicit {
		ipv6 = true
	} // Mihomo core default, matching the saved-config inspection.
	if ipv6 != true || dns["enable"] != true || dns["ipv6"] != true || dns["enhanced-mode"] != "fake-ip" || tun["enable"] != true || fake == "" {
		return result
	}
	result.Required = true
	if _, prefix, e := net.ParseCIDR(fake); e != nil || prefix.IP.To4() != nil {
		result.Status = "unknown"
		result.Message = "IPv6 Fake-IP 范围无效，无法确认 TUN 接入"
		return result
	}
	device, _ := tun["device"].(string)
	if device == "" {
		device = "Meta"
	}
	result.Device = device
	if !ipv6InterfacePattern.MatchString(device) || device == "lo" {
		result.Status = "unknown"
		result.Message = "无法确认当前配置的 TUN 设备名"
		return result
	}
	for _, address := range network.TUNAddresses[device] {
		if address.Usable {
			result.Addresses = append(result.Addresses, fmt.Sprintf("%s/%d", address.Address, address.PrefixLength))
		}
	}
	if len(result.Addresses) > 0 {
		result.Status = "ready"
		result.Message = "实际 TUN 设备已有 IPv6 地址；客户端分流仍需验证"
	} else {
		result.Status = "missing"
		result.Message = "Mihomo TUN 尚无 IPv6 地址，可能早于主机 IPv6 就绪启动；主机就绪后需要重新加载配置"
	}
	return result
}

type ipv6TUNWaiting struct{ error }

// recoverIPv6TUN is called with the gateway/service operation locks held. Only
// an owned IPv6 preparation may automatically reload an already-running core.
// It never edits the unit, starts a stopped service, or falls back to restart.
func recoverIPv6TUN(cfg *Config, network ipv6Network) (bool, error) {
	status := ipv6TUNStatus(cfg, network)
	if !status.Required || status.Status == "ready" {
		return false, nil
	}
	if status.Status != "missing" {
		return false, errors.New(status.Message)
	}
	state, err := loadIPv6Managed(cfg)
	if err != nil {
		return false, err
	}
	if state == nil || state.Phase != "active" {
		return false, fmt.Errorf("实际 TUN 尚未启用 IPv6；此主机没有 ConfigFlow IPv6 管理记录，请在主机 IPv6 就绪后重新发布 Mihomo 配置")
	}
	public := false
	identity := false
	for _, iface := range network.Interfaces {
		if iface.Name == state.Interface && iface.MAC == state.MAC {
			identity = true
			for _, address := range iface.Addresses {
				public = public || address.Public
			}
		}
	}
	if network.Upstream != state.Interface || !identity {
		return false, fmt.Errorf("上联网卡已变化，停止自动重新加载 Mihomo")
	}
	if !public || len(network.Routes) == 0 {
		return false, ipv6TUNWaiting{fmt.Errorf("主机 IPv6 地址或默认路由尚未就绪，保留现有 IPv4 服务，稍后再检查")}
	}
	running, err := ipv6CoreRunning(cfg)
	if err != nil || !running {
		return false, ipv6TUNWaiting{fmt.Errorf("无法确认本 Agent 管理的 Mihomo 正在运行，未重新加载或启动服务")}
	}
	before, err := os.ReadFile(cfg.ConfigPath)
	if err != nil {
		return false, fmt.Errorf("无法读取已部署的 Mihomo 配置")
	}
	doc, _, err := readYAMLDocument(cfg.ConfigPath)
	if err != nil {
		return false, fmt.Errorf("无法解析已部署的 Mihomo 配置")
	}
	controller, err := newMihomoReloader(doc)
	if err != nil {
		return false, fmt.Errorf("无法使用本机 Mihomo 控制器自动恢复 TUN IPv6，请重新发布配置")
	}
	code, data, err := controller.request(http.MethodGet, "/version", nil, 2*time.Second)
	var version struct {
		Version string `json:"version"`
	}
	if err != nil || code != 200 || json.Unmarshal(data, &version) != nil || version.Version == "" {
		return false, fmt.Errorf("Mihomo 控制器身份检查失败，未重新加载配置")
	}
	code, data, err = controller.request(http.MethodGet, "/configs", nil, 2*time.Second)
	var current struct {
		IPv6 bool `json:"ipv6"`
		TUN  struct {
			Enable bool     `json:"enable"`
			Device string   `json:"device"`
			IPv6   []string `json:"inet6-address"`
		} `json:"tun"`
	}
	if err != nil || code != 200 || json.Unmarshal(data, &current) != nil || !current.IPv6 || !current.TUN.Enable || current.TUN.Device != status.Device {
		return false, fmt.Errorf("运行中的 Mihomo IPv6/TUN 设置与已部署配置不一致，请显式重新发布配置")
	}
	if len(current.TUN.IPv6) > 0 {
		return false, fmt.Errorf("Mihomo 报告 TUN 已有 IPv6，但实际网卡未就绪，请检查设备状态")
	}
	// Do not apply an externally edited file that changed during the preflight.
	after, err := os.ReadFile(cfg.ConfigPath)
	if err != nil || string(before) != string(after) {
		return false, fmt.Errorf("Mihomo 配置在检查期间发生变化，未重新加载")
	}
	configPath, err := filepath.Abs(cfg.ConfigPath)
	if err != nil {
		return false, fmt.Errorf("无法确定已部署配置的绝对路径")
	}
	body, _ := json.Marshal(map[string]string{"path": configPath, "payload": ""})
	code, _, err = controller.request(http.MethodPut, "/configs?force=true", body, ipv6TUNReloadTimeout)
	if err != nil || code != http.StatusNoContent {
		return true, fmt.Errorf("主机 IPv6 已就绪，但 Mihomo 热重载未确认成功；保留文件，请检查核心状态并重新发布")
	}
	for attempt := 0; attempt < 10; attempt++ {
		fresh, e := detectIPv6Network()
		if e == nil && ipv6TUNStatus(cfg, fresh).Status == "ready" {
			return true, nil
		}
		ipv6TUNPause(100 * time.Millisecond)
	}
	return true, fmt.Errorf("Mihomo 已接受热重载，但实际 TUN IPv6 尚未就绪，请重新检查或发布配置")
}

func ipv6TUNCheck(report *ipv6Report, network ipv6Network, cfg *Config) {
	report.TUNIPv6 = ipv6TUNStatus(cfg, network)
	if !report.TUNIPv6.Required {
		return
	}
	status := "warn"
	if report.TUNIPv6.Status == "ready" {
		status = "pass"
	}
	report.Checks = append(report.Checks, ipv6Check{ID: "tun_ipv6", Status: status, Message: report.TUNIPv6.Message})
	if report.TUNIPv6.Status != "ready" {
		report.ConfigurationReady = false
	}
}

// Helpers below keep diagnostics free of config/controller secrets.
func ipv6TUNRecoveryMessage(err error) string {
	if err == nil {
		return "IPv6 网关参数与实际 TUN 已检查；节点连接和重启恢复需要分别验证"
	}
	return "主机参数已准备；" + strings.TrimSpace(err.Error())
}
