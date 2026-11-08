package preflight

import (
	"agent/netfilter"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const netAdminCapability = uint64(1 << 12)

type tunState uint8

const (
	tunUnknown tunState = iota
	tunDisabled
	tunEnabled
)

const tunDeviceFix = "普通 Linux/虚拟机先检查内核 TUN 驱动（modprobe tun）。PVE LXC 应先在 PVE 宿主机执行 modprobe tun，检查 pct config <CTID> 的空闲 devN 槽，再用 pct set <CTID> --devN /dev/net/tun 添加设备（例如未占用的 dev0）；完整停止并启动容器后，在容器内重新检查。不要改为特权容器或放宽整个设备目录权限。"
const tunCapabilityFix = "分别核对当前检查进程与 Mihomo 核心进程的 /proc/<PID>/status 中 CapEff、CapBnd 和启动权限；UID 为 root 不能证明具有 CAP_NET_ADMIN。若运行在容器内，还需核对容器保留的能力及 /dev/net/tun 的设备访问策略。按实际服务管理方式做最小调整。"
const systemdTunCapabilityFix = "检查 Mihomo 服务的 CapabilityBoundingSet 是否包含 CAP_NET_ADMIN；非 root 服务还需核对 AmbientCapabilities 或程序文件能力，并以核心进程的 CapEff 确认实际能力。设备受限制时，核对 DeviceAllow=/dev/net/tun rw。若运行在容器内，还需核对容器保留的能力。保留其他必要权限，按现有服务配置做最小调整。"
const openRCTunCapabilityFix = "核对 /etc/init.d/ 中对应的 Mihomo 服务脚本与 /etc/conf.d/ 配置，包括 command_user、capabilities 及其启动命令；从该服务实际运行进程的 /proc/<PID>/status 读取 CapEff，确认 CAP_NET_ADMIN，并核对该进程对 /dev/net/tun 的设备访问权限。UID 为 root 不能证明具备该能力；若运行在容器内，还需核对容器保留的能力和设备映射。"
const gatewayForwardingFix = "在运行网关的 Linux 内检查 /proc/sys 的写权限和 CAP_NET_ADMIN；LXC 需要宿主机管理员允许容器修改网络 sysctl。本安装不会修改 PVE 宿主机。"
const forwardingFix = "仅当此 Linux/LXC 承担旁路网关或路由转发时，在该系统内执行 sysctl -w net.ipv4.ip_forward=1，并把 net.ipv4.ip_forward=1 写入 /etc/sysctl.d/99-configflow-forward.conf；同时处理报告中的冲突设置及后加载文件，避免被改回 0。重启后用 sysctl net.ipv4.ip_forward 再验证。PVE 仅做二层网桥时不因此需要开启宿主机转发。"

type systemProbe struct {
	goos      string
	readFile  func(string) ([]byte, error)
	readDir   func(string) ([]os.DirEntry, error)
	stat      func(string) (os.FileInfo, error)
	glob      func(string) ([]string, error)
	tun       func(string) (uint32, error)
	showUnit  func(string) ([]byte, error)
	getenv    func(string) string
	writeFile func(string, []byte) error
	readlink  func(string) (string, error)
	resolve   func(string) (string, error)
	// forwardFilter inspects FORWARD filtering; nil skips the check.
	forwardFilter func(netfilter.Target) netfilter.Result
	lanAddresses  func() []net.IP
}

func defaultSystemProbe() systemProbe {
	return systemProbe{
		goos: runtime.GOOS, readFile: os.ReadFile, readDir: os.ReadDir, stat: os.Stat, glob: filepath.Glob, tun: systemTunFeatures,
		getenv: os.Getenv, writeFile: func(path string, data []byte) error { return os.WriteFile(path, data, 0o644) },
		readlink: os.Readlink, resolve: filepath.EvalSymlinks,
		forwardFilter: boundedForwardFilter, lanAddresses: netfilter.LANAddresses,
		showUnit: func(unit string) ([]byte, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			return exec.CommandContext(ctx, "systemctl", "show",
				"--property=LoadState,MainPID,CapabilityBoundingSet,AmbientCapabilities,PrivateDevices,DevicePolicy,DeviceAllow,PrivateNetwork,NetworkNamespacePath,User,NoNewPrivileges,RootDirectory,RootImage",
				"--", unit).Output()
		},
	}
}

// boundedForwardFilter never holds the environment check past the inspection
// budget, even if a command outlives its context; it then reports unknown.
func boundedForwardFilter(target netfilter.Target) netfilter.Result {
	return boundedForwardFilterWith(target, func(ctx context.Context) netfilter.Result {
		return netfilter.Inspect(ctx, netfilter.DefaultProbe(), target)
	})
}

func boundedForwardFilterWith(target netfilter.Target, inspect func(context.Context) netfilter.Result) netfilter.Result {
	budget := netfilter.TotalBudget
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	done := make(chan netfilter.Result, 1)
	go func() { done <- inspect(ctx) }()
	timer := time.NewTimer(budget + 250*time.Millisecond)
	defer timer.Stop()
	select {
	case result := <-done:
		return result
	case <-timer.C:
		return netfilter.Result{Protocol: 1, State: netfilter.StateUnknown, Reason: "timeout", Device: target.Device, AutoRedirect: target.AutoRedirect}
	}
}

func systemChecks(configs []Config) []Check {
	return systemChecksWithProbe(configs, defaultSystemProbe())
}

func systemChecksWithProbe(configs []Config, probe systemProbe) []Check {
	if probe.goos != "linux" {
		return []Check{{Name: "Linux 网络环境", Level: "warn", Message: "当前不是 Linux；无法确认目标机的 TUN、Linux capabilities 与 IPv4 转发状态。", Fix: "请在实际安装 Agent 的 Linux/LXC 内运行环境检查。"}}
	}
	var checks []Check
	hasMihomo := false
	coreTunStates := make(map[int]tunState)
	for i, cfg := range configs {
		if cfg.ServiceType != "mihomo" {
			continue
		}
		hasMihomo = true
		coreTunStates[i] = tunUnknown
		doc, err := loadCoreConfig(cfg)
		if err != nil {
			// Run reports the configuration error separately; it is not safe to
			// infer TUN requirements from unreadable or invalid configuration.
			continue
		}
		state, configurationChecks := configuredMihomoTUN(doc)
		coreTunStates[i] = state
		checks = append(checks, configurationChecks...)
	}
	if hasMihomo {
		tunRequirement := tunDisabled
		var serviceChecks []Check
		allRequiredCoresHaveCapability := true
		for i, cfg := range configs {
			if cfg.ServiceType == "mihomo" {
				state := coreTunStates[i]
				if state == tunEnabled || state == tunUnknown && tunRequirement != tunEnabled {
					tunRequirement = state
				}
				coreChecks, hasCapability := inspectCoreService(probe, cfg, state)
				serviceChecks = append(serviceChecks, coreChecks...)
				if state == tunEnabled && !hasCapability {
					allRequiredCoresHaveCapability = false
				}
			}
		}
		capability := inspectInstallerCapability(probe, tunRequirement)
		if tunRequirement == tunEnabled && allRequiredCoresHaveCapability && capability.Level == "error" {
			capability.Level = "warn"
			capability.Message = "当前检查进程没有 CAP_NET_ADMIN，但已核实启用 TUN 的核心进程持有该能力；安装进程的权限不能代表核心服务，不据此阻止安装。"
		}
		checks = append(checks, inspectTunDevice(probe, tunRequirement), capability)
		checks = append(checks, serviceChecks...)
	} else {
		checks = append(checks, Check{Name: "TUN / CAP_NET_ADMIN", Level: "pass", Message: "本次仅接管 DNS 核心，不需要 TUN 或网络管理能力。"})
	}
	// A DNS core never forwards other hosts' traffic; only Mihomo (alone or
	// beside a DNS core) needs IPv4 forwarding.
	if len(configs) > 0 && !hasMihomo {
		return checks
	}
	switch {
	case probe.getenv != nil && probe.getenv("CONFIGFLOW_FORWARDING_OWNER") == "gateway":
		checks = append(checks, inspectGatewayForwarding(probe))
	case probe.getenv != nil && probe.getenv(installerForwardingEnv) == "persist":
		checks = append(checks, inspectInstallerForwarding(probe)...)
		if hasMihomo {
			if check, ok := inspectForwardFilter(probe, configs); ok {
				checks = append(checks, check)
			}
		}
	default:
		checks = append(checks, inspectIPv4Forwarding(probe)...)
	}
	return checks
}

// The selective gateway turns forwarding on when it is applied, restores the
// value it saved when it is removed, and reapplies its journal at boot. A
// manual sysctl change would only become the "original" value it restores, so
// only the permission to change it is checked here.
func inspectGatewayForwarding(probe systemProbe) Check {
	const path = "/proc/sys/net/ipv4/ip_forward"
	check := Check{Name: "IPv4 转发权限", Level: "pass"}
	data, err := probe.readFile(path)
	value := strings.TrimSpace(string(data))
	if err != nil || value != "0" && value != "1" {
		check.Level = "warn"
		check.Message = "无法确认 net.ipv4.ip_forward 的当前值；应用网关转发时会再次检查。"
		return check
	}
	// Writing the current value proves the kernel permission without toggling it.
	if probe.writeFile == nil || probe.writeFile(path, []byte(value+"\n")) != nil {
		check.Level = "error"
		check.Message = "无法写入 net.ipv4.ip_forward（当前为 " + value + "），应用网关转发时将无法开启转发。"
		check.Fix = gatewayForwardingFix
		return check
	}
	check.Message = "当前 net.ipv4.ip_forward=" + value + "；网页「应用网关转发」时开启，撤销时恢复原值，重启后由 Agent 自动恢复，无需手动修改或写入 sysctl.d。"
	return check
}

// A configured TUN listener is active independently of the legacy top-level
// tun.enable switch. Keep an unknown state for malformed settings, but one
// definitely active TUN entry is enough to require its environment prerequisites.
func configuredMihomoTUN(doc map[string]interface{}) (tunState, []Check) {
	state := tunDisabled
	var checks []Check
	unknown := func(message, fix string) {
		if state != tunEnabled {
			state = tunUnknown
		}
		checks = append(checks, Check{Name: "Mihomo TUN 配置", Level: "warn", Message: message, Fix: fix})
	}
	if rawTun, present := doc["tun"]; present {
		tun, ok := rawTun.(map[string]interface{})
		if !ok {
			unknown("tun 不是有效的配置对象，无法判断是否需要 TUN。", "检查 Mihomo 配置中的 tun 结构，并将 tun.enable 设置为 true 或 false。")
		} else if rawEnabled, present := tun["enable"]; present {
			if enabled, ok := rawEnabled.(bool); !ok {
				unknown("tun.enable 不是明确的布尔值，无法判断是否需要 TUN。", "检查 Mihomo 配置中的 tun.enable，使用 true 或 false。")
			} else if enabled {
				state = tunEnabled
			}
		}
	}
	if rawListeners, present := doc["listeners"]; present && rawListeners != nil {
		listeners, ok := rawListeners.([]interface{})
		if !ok {
			unknown("listeners 不是有效的列表，无法判断是否配置了 TUN 入口。", "检查 Mihomo 配置中的 listeners 列表及 type: tun 入口。")
			return state, checks
		}
		for _, rawListener := range listeners {
			listener, ok := rawListener.(map[string]interface{})
			if !ok {
				unknown("listeners 中存在无效入口，无法完整判断 TUN 状态。", "检查 Mihomo listeners 中每个入口的配置对象。")
				continue
			}
			if kind, _ := listener["type"].(string); kind != "tun" {
				continue
			}
			if rawDisabled, present := listener["disabled"]; present {
				disabled, ok := rawDisabled.(bool)
				if !ok {
					unknown("TUN 入口的 disabled 不是明确的布尔值，无法判断该入口是否启用。", "检查 listeners 中 type: tun 入口的 disabled，使用 true 或 false。")
					continue
				}
				if disabled {
					continue
				}
			}
			state = tunEnabled
		}
	}
	return state, checks
}

func tunFailureLevel(state tunState) string {
	if state == tunEnabled {
		return "error"
	}
	return "warn"
}

func inspectTunDevice(probe systemProbe, state tunState) Check {
	check := Check{Name: "TUN 设备", Level: tunFailureLevel(state), Fix: tunDeviceFix}
	info, err := probe.stat("/dev/net/tun")
	if err != nil {
		check.Message = "无法访问 /dev/net/tun；设备未提供或访问受限。"
	} else if info.Mode()&os.ModeCharDevice == 0 {
		check.Message = "/dev/net/tun 不是字符设备，不能作为 TUN 使用。"
	} else if features, err := probe.tun("/dev/net/tun"); err != nil {
		check.Message = "TUN 字符设备存在，但当前进程打开设备或读取驱动能力失败；设备映射、cgroup 或安全策略可能限制访问。"
		check.Fix += " 同时核对核心服务的启动用户、设备映射及访问策略；检查进程的访问结果不能代表核心进程。"
	} else if features&1 == 0 { // IFF_TUN, shared by supported Linux architectures.
		check.Message = "设备可打开，但驱动没有报告 IFF_TUN 支持。"
	} else {
		return Check{Name: check.Name, Level: "pass", Message: "/dev/net/tun 为字符设备，当前进程可以打开并读取 TUNGETFEATURES；未创建网络接口。"}
	}
	if state == tunEnabled {
		check.Message += " 当前 Mihomo 配置已启用 TUN。"
	} else if state == tunUnknown {
		check.Message += " 当前配置的 TUN 状态未知；需先修复核心配置后重新检查。"
	} else {
		check.Message += " 当前配置未启用 TUN，不阻止安装；以后开启前需处理。"
	}
	return check
}

func capabilityMask(data []byte, field string) (uint64, error) {
	for _, line := range strings.Split(string(data), "\n") {
		name, value, ok := strings.Cut(line, ":")
		if ok && name == field {
			return strconv.ParseUint(strings.TrimSpace(value), 16, 64)
		}
	}
	return 0, fmt.Errorf("capability field unavailable")
}

func inspectInstallerCapability(probe systemProbe, state tunState) Check {
	data, err := probe.readFile("/proc/self/status")
	mask, parseErr := capabilityMask(data, "CapEff")
	if err != nil || parseErr != nil {
		return Check{Name: "当前进程 CAP_NET_ADMIN", Level: "warn", Message: "无法读取当前进程的有效能力，不能确认是否可创建 TUN 或调整路由。", Fix: tunCapabilityFix}
	}
	if mask&netAdminCapability != 0 {
		return Check{Name: "当前进程 CAP_NET_ADMIN", Level: "pass", Message: "当前检查进程持有有效 CAP_NET_ADMIN；核心服务权限另行检查，不以 root 身份替代能力检查。"}
	}
	message := "当前检查进程未持有有效 CAP_NET_ADMIN。"
	level := "warn"
	if state == tunEnabled {
		bounding, boundErr := capabilityMask(data, "CapBnd")
		if boundErr == nil && bounding&netAdminCapability == 0 {
			level = "error"
			message += " 当前进程的 CapBnd 也不包含该能力，且尚未证明核心具备 TUN 所需权限。"
		} else {
			message += " 仅凭检查进程的 CapEff 不能确认核心缺少权限；需结合核心进程及服务能力设置验证。"
		}
	} else if state == tunUnknown {
		message += " 当前配置的 TUN 状态未知；需先修复核心配置后重新检查。"
	} else {
		message += " 当前未启用 TUN，不阻止安装；以后开启前需处理。"
	}
	return Check{Name: "当前进程 CAP_NET_ADMIN", Level: level, Message: message, Fix: tunCapabilityFix}
}

func systemdProperties(data []byte) map[string]string {
	values := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = value
		}
	}
	return values
}

func hasNamedCapability(value, capability string) bool {
	for _, name := range strings.Fields(strings.ToLower(value)) {
		if name == capability {
			return true
		}
	}
	return false
}

func inspectCoreService(probe systemProbe, cfg Config, state tunState) ([]Check, bool) {
	name := "Mihomo 服务权限"
	if state == tunUnknown {
		return []Check{{Name: name, Level: "warn", Message: "无法从核心配置判断 TUN 是否启用，尚未验证对应服务的 TUN 权限。", Fix: "先修复核心配置路径、读取权限或 tun 配置结构，再重新运行环境检查。"}}, false
	}
	if state == tunDisabled {
		return []Check{{Name: name, Level: "pass", Message: "当前配置未启用 TUN；不以 TUN 服务权限作为安装前提。"}}, false
	}
	if cfg.ServiceManager == "openrc" {
		pid, identity, reason := openRCServicePID(probe, cfg)
		if pid <= 0 {
			return []Check{{Name: name, Level: "warn", Message: "核心由 OpenRC 管理；尚未取得并验证其实际运行 PID（" + reason + "），无法确认核心获得的能力与设备访问权限。当前检查进程的权限不能代表核心服务。", Fix: openRCTunCapabilityFix},
				tunRuntimeWarning("未取得 OpenRC 管理的 Mihomo 核心 PID（"+reason+"），无法读取其持有的 TUN 接口。", openRCTunCapabilityFix)}, false
		}
		checks := []Check{{Name: name, Level: "pass", Message: "已按 OpenRC 服务 " + cfg.ServiceUnit + " 的 " + identity + "取得核心 PID " + strconv.Itoa(pid) + "，以下检查读取该进程的实际权限。"}}
		processChecks, hasCapability := inspectCoreProcess(probe, pid, openRCTunCapabilityFix, openRCTunDeviceFix)
		checks = append(checks, processChecks...)
		return append(checks, inspectCoreTUNRuntime(probe, pid)), hasCapability
	}
	if cfg.ServiceManager != "systemd" {
		return []Check{{Name: name, Level: "warn", Message: "当前服务管理方式未提供可验证的核心 PID，无法确认核心实际获得的能力与设备访问权限；当前检查进程的权限不能代表核心服务。", Fix: tunCapabilityFix},
			tunRuntimeWarning("当前服务管理方式未提供可验证的 Mihomo 核心 PID，无法读取其持有的 TUN 接口。", tunCapabilityFix)}, false
	}
	if cfg.ServiceUnit == "" {
		return []Check{{Name: name, Level: "warn", Message: "未指定 Mihomo systemd 服务单元，无法读取核心实际获得的能力与设备访问权限；当前检查进程的权限不能代表核心服务。", Fix: systemdTunCapabilityFix},
			tunRuntimeWarning("未指定 Mihomo systemd 服务单元，无法确定核心 PID 并检查其 TUN 接口。", systemdTunCapabilityFix)}, false
	}
	data, err := probe.showUnit(cfg.ServiceUnit)
	if err != nil {
		return []Check{{Name: name, Level: "warn", Message: "无法读取 Mihomo systemd 服务的权限与隔离配置，不能确认核心能够访问 TUN。", Fix: systemdTunCapabilityFix},
			tunRuntimeWarning("无法读取 Mihomo systemd 服务信息，无法确定核心 PID 并检查其 TUN 接口。", systemdTunCapabilityFix)}, false
	}
	properties := systemdProperties(data)
	var checks []Check
	coreHasCapability := false
	if bound, present := properties["CapabilityBoundingSet"]; !present {
		checks = append(checks, Check{Name: name + "上限", Level: "warn", Message: "服务没有返回 CapabilityBoundingSet，无法确认能力上限。", Fix: systemdTunCapabilityFix})
	} else if !hasNamedCapability(bound, "cap_net_admin") {
		checks = append(checks, Check{Name: name + "上限", Level: "error", Message: "Mihomo 已启用 TUN，但服务的 CapabilityBoundingSet 未包含 CAP_NET_ADMIN。", Fix: systemdTunCapabilityFix})
	} else {
		checks = append(checks, Check{Name: name + "上限", Level: "pass", Message: "服务 CapabilityBoundingSet 允许 CAP_NET_ADMIN；能力上限本身不等于已授予有效能力。"})
	}
	pid, _ := strconv.Atoi(properties["MainPID"])
	if pid <= 0 {
		checks = append(checks, Check{Name: "Mihomo 运行权限", Level: "warn", Message: "未取得运行中的核心 PID，无法验证实际 CapEff 或服务内的 TUN 设备视图。", Fix: systemdTunCapabilityFix})
	} else {
		processChecks, hasCapability := inspectCoreProcess(probe, pid, systemdTunCapabilityFix, systemdTunDeviceFix)
		checks = append(checks, processChecks...)
		coreHasCapability = hasCapability
	}
	policy := properties["DevicePolicy"]
	if properties["PrivateDevices"] == "yes" || policy == "closed" || policy == "strict" || properties["DeviceAllow"] != "" {
		checks = append(checks, Check{Name: "Mihomo 设备隔离", Level: "warn", Message: "服务启用了设备隔离或访问规则；检查进程成功打开 TUN，不能证明核心服务也可打开。", Fix: "核对 PrivateDevices 及设备绑定是否提供 /dev/net/tun，并检查 DevicePolicy/DeviceAllow 是否允许该字符设备读写；不要整体关闭服务沙箱。"})
	}
	if properties["PrivateNetwork"] == "yes" || properties["NetworkNamespacePath"] != "" {
		checks = append(checks, Check{Name: "Mihomo 网络隔离", Level: "warn", Message: "核心服务使用独立网络命名空间；外部检查的路由和 ip_forward 不代表服务内部状态，不能确认可承担 LAN 旁路网关。", Fix: "核对服务的 PrivateNetwork/NetworkNamespacePath 和预期网络拓扑，在对应网络命名空间内验证转发。"})
	}
	checks = append(checks, inspectCoreTUNRuntime(probe, pid))
	return checks, coreHasCapability
}

const systemdTunDeviceFix = "检查服务 PrivateDevices、RootDirectory/RootImage 和设备绑定；仅向该服务提供 /dev/net/tun，并核对 DeviceAllow。"
const openRCTunDeviceFix = "核对 Mihomo 服务脚本是否使用 chroot 或独立挂载命名空间；在容器内还需核对 /dev/net/tun 的设备映射（PVE LXC 的 devN 设置）。"

// inspectCoreProcess reads the running core's effective capabilities and its
// view of /dev/net/tun, whichever service manager supplied the PID.
func inspectCoreProcess(probe systemProbe, pid int, fix, deviceFix string) ([]Check, bool) {
	var checks []Check
	coreHasCapability := false
	status, readErr := probe.readFile(fmt.Sprintf("/proc/%d/status", pid))
	mask, parseErr := capabilityMask(status, "CapEff")
	if readErr != nil || parseErr != nil {
		checks = append(checks, Check{Name: "Mihomo 运行权限", Level: "warn", Message: "无法读取核心进程的有效能力，不能用安装进程的权限推断核心权限。", Fix: fix})
	} else if mask&netAdminCapability == 0 {
		checks = append(checks, Check{Name: "Mihomo 运行权限", Level: "warn", Message: fmt.Sprintf("运行中的核心未持有有效 CAP_NET_ADMIN（PID %d，CapEff=%016x）；它可能已降权，无法据此确认重启后能重新创建 TUN。", pid, mask), Fix: fix})
	} else {
		coreHasCapability = true
		checks = append(checks, Check{Name: "Mihomo 运行权限", Level: "pass", Message: fmt.Sprintf("当前核心进程持有有效 CAP_NET_ADMIN（PID %d，CapEff=%016x）。", pid, mask)})
	}
	device, statErr := probe.stat(fmt.Sprintf("/proc/%d/root/dev/net/tun", pid))
	if readErr != nil {
		checks = append(checks, Check{Name: "Mihomo 设备视图", Level: "warn", Message: "核心 PID 已退出或无法检查，不能确认运行服务内的 TUN 设备视图。", Fix: fix})
	} else if os.IsNotExist(statErr) || statErr == nil && device.Mode()&os.ModeCharDevice == 0 {
		checks = append(checks, Check{Name: "Mihomo 设备视图", Level: "error", Message: "已启用 TUN，但当前核心进程的文件系统视图内没有 TUN 字符设备。", Fix: deviceFix})
	} else if statErr != nil {
		checks = append(checks, Check{Name: "Mihomo 设备视图", Level: "warn", Message: "无法读取核心服务内的设备视图，不能确认服务能够访问 /dev/net/tun。", Fix: fix})
	} else {
		checks = append(checks, Check{Name: "Mihomo 设备视图", Level: "pass", Message: "当前核心进程的文件系统视图内可见 TUN 字符设备；设备访问策略仍可能限制打开。"})
	}
	return checks, coreHasCapability
}

var openRCServiceName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]*$`)

// openRCScriptValue returns the last top-level assignment of key in an OpenRC
// service script or conf.d file, with quotes removed. RC_SVCNAME expands to the
// service name; any other expansion yields an empty (unusable) value.
func openRCScriptValue(data []byte, key, service string) (string, bool) {
	value, found := "", false
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		name, rest, ok := strings.Cut(line, "=")
		if !ok || name != key {
			continue
		}
		rest = strings.TrimSpace(strings.SplitN(rest, " #", 2)[0])
		if len(rest) >= 2 && (rest[0] == '"' && rest[len(rest)-1] == '"' || rest[0] == '\'' && rest[len(rest)-1] == '\'') {
			rest = rest[1 : len(rest)-1]
		}
		for _, variable := range []string{"${RC_SVCNAME}", "$RC_SVCNAME", "${SVCNAME}", "$SVCNAME"} {
			rest = strings.ReplaceAll(rest, variable, service)
		}
		value, found = rest, true
	}
	if strings.ContainsAny(value, "$`\"' ") {
		// Assigned, but not to a value this check can resolve.
		return "", found
	}
	return value, found
}

// openRCServicePID finds the core's PID from its OpenRC pidfile (the service
// script's pidfile=, else /etc/conf.d, else ConfigFlow's /run/<service>.pid)
// and accepts it only if /proc/<PID>/exe is the service's command, so a stale
// pidfile or a reused PID is never read as the core.
func openRCServicePID(probe systemProbe, cfg Config) (int, string, string) {
	service := cfg.ServiceUnit
	if !openRCServiceName.MatchString(service) {
		return 0, "", "未指定有效的 OpenRC 服务名"
	}
	script, err := probe.readFile("/etc/init.d/" + service)
	if err != nil {
		return 0, "", "无法读取 /etc/init.d/" + service
	}
	confd, _ := probe.readFile("/etc/conf.d/" + service)
	pidfile, ok := openRCScriptValue(script, "pidfile", service)
	if !ok {
		if pidfile, ok = openRCScriptValue(confd, "pidfile", service); !ok {
			pidfile = "/run/" + service + ".pid"
		}
	}
	if !filepath.IsAbs(pidfile) || filepath.Clean(pidfile) != pidfile {
		return 0, "", "服务脚本的 pidfile 不是可识别的绝对路径"
	}
	data, err := probe.readFile(pidfile)
	if err != nil {
		return 0, "", "无法读取 " + pidfile + "，服务可能未运行"
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, "", pidfile + " 中不是有效的 PID"
	}
	if probe.readlink == nil {
		return 0, "", "无法核对 PID 对应的程序"
	}
	exe, err := probe.readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return 0, "", fmt.Sprintf("%s 中的 PID %d 已退出或无法读取其程序路径", pidfile, pid)
	}
	exe = strings.TrimSuffix(exe, " (deleted)")
	command, _ := openRCScriptValue(script, "command", service)
	if !openRCCommandMatches(probe, exe, command, cfg.ServiceBinary) {
		return 0, "", fmt.Sprintf("%s 中的 PID %d 运行的是 %s，不是该服务的核心程序", pidfile, pid, exe)
	}
	return pid, "pidfile（" + pidfile + "）", ""
}

func openRCCommandMatches(probe systemProbe, exe, command, binary string) bool {
	if filepath.IsAbs(command) {
		if exe == command {
			return true
		}
		if probe.resolve != nil {
			if resolved, err := probe.resolve(command); err == nil && resolved == exe {
				return true
			}
		}
		return false
	}
	if binary == "" {
		binary = "mihomo"
	}
	return filepath.Base(exe) == filepath.Base(binary)
}

func inspectIPv4Forwarding(probe systemProbe) []Check {
	data, err := probe.readFile("/proc/sys/net/ipv4/ip_forward")
	check := Check{Name: "IPv4 转发", Level: "warn", Fix: forwardingFix}
	if err != nil {
		check.Message = "无法读取当前网络命名空间的 net.ipv4.ip_forward，转发状态未确认。"
	} else if strings.TrimSpace(string(data)) == "1" {
		check.Level = "pass"
		check.Message = "当前网络命名空间的 net.ipv4.ip_forward=1；这里只确认运行值，不代表重启后仍开启。"
		check.Fix = ""
	} else if strings.TrimSpace(string(data)) == "0" {
		check.Message = "当前 net.ipv4.ip_forward=0；作为旁路网关转发其他设备流量时需开启。仅本机 TUN 代理或 DNS 服务并不因此需要开启。"
	} else {
		check.Message = "当前 net.ipv4.ip_forward 不是可识别的 0/1 值，转发状态未确认。"
	}
	return []Check{check, inspectForwardingPersistence(probe)}
}

type forwardingSetting struct {
	value string
	where string
}

func parseForwardingSetting(data []byte, path string) forwardingSetting {
	var result forwardingSetting
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.SplitN(strings.SplitN(raw, "#", 2)[0], ";", 2)[0])
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.ReplaceAll(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(key), "-"), "/"), "/", ".")
		if key == "net.ipv4.ip_forward" {
			value = strings.TrimSpace(value)
			if value != "0" && value != "1" {
				value = "未知值"
			}
			result = forwardingSetting{value: value, where: fmt.Sprintf("%s:%d", path, i+1)}
		}
	}
	return result
}

// sysctlLoader is what applies the sysctl configuration at boot; each reads
// different files in a different order.
type sysctlLoader string

const (
	// systemd-sysctl: one file per basename (/etc, /run, /usr/local/lib,
	// /usr/lib, /lib), applied in filename order. It never reads
	// /etc/sysctl.conf itself; Debian and Fedora link it in as 99-sysctl.conf.
	loaderSystemd sysctlLoader = "systemd-sysctl"
	// procps "sysctl --system" (OpenRC outside Alpine): the same selection and
	// order, then /etc/sysctl.conf last.
	loaderProcps sysctlLoader = "sysctl --system"
	// Alpine's OpenRC sysctl service (BusyBox sysctl has no --system): /lib
	// and /usr/lib unless shadowed in /etc or /run, then /etc/sysctl.d unless
	// shadowed in /run, then /etc/sysctl.conf, then /run/sysctl.d. It does not
	// read /usr/local/lib/sysctl.d.
	loaderAlpine sysctlLoader = "Alpine sysctl 服务"
)

func detectSysctlLoader(probe systemProbe) sysctlLoader {
	// sd_booted(): systemd is the running init.
	if info, err := probe.stat("/run/systemd/system"); err == nil && info.IsDir() {
		return loaderSystemd
	}
	if _, err := probe.readFile("/etc/alpine-release"); err == nil {
		return loaderAlpine
	}
	return loaderProcps
}

// sysctlBootFiles lists, in application order, the files the loader applies
// at boot. extra adds files that do not exist yet (the installer's own).
func sysctlBootFiles(probe systemProbe, loader sysctlLoader, extra []string) ([]string, bool) {
	unreadable := false
	list := func(dir string) []string {
		paths, err := probe.glob(dir + "/*.conf")
		unreadable = unreadable || err != nil
		for _, path := range extra {
			if filepath.Dir(path) == dir {
				paths = append(paths, path)
			}
		}
		sort.Strings(paths)
		return paths
	}
	names := func(paths []string) map[string]bool {
		set := make(map[string]bool, len(paths))
		for _, path := range paths {
			set[filepath.Base(path)] = true
		}
		return set
	}
	if loader == loaderAlpine {
		etc, run := list("/etc/sysctl.d"), list("/run/sysctl.d")
		etcNames, runNames := names(etc), names(run)
		var files []string
		for _, path := range append(list("/lib/sysctl.d"), list("/usr/lib/sysctl.d")...) {
			if base := filepath.Base(path); !etcNames[base] && !runNames[base] {
				files = append(files, path)
			}
		}
		for _, path := range etc {
			if !runNames[filepath.Base(path)] {
				files = append(files, path)
			}
		}
		files = append(files, "/etc/sysctl.conf")
		return append(files, run...), unreadable
	}
	selected := make(map[string]string)
	for _, dir := range []string{"/etc/sysctl.d", "/run/sysctl.d", "/usr/local/lib/sysctl.d", "/usr/lib/sysctl.d", "/lib/sysctl.d"} {
		for _, path := range list(dir) {
			if base := filepath.Base(path); selected[base] == "" {
				selected[base] = path
			}
		}
	}
	bases := make([]string, 0, len(selected))
	for base := range selected {
		bases = append(bases, base)
	}
	sort.Strings(bases)
	files := make([]string, 0, len(bases)+1)
	for _, base := range bases {
		files = append(files, selected[base])
	}
	if loader == loaderProcps {
		files = append(files, "/etc/sysctl.conf")
	}
	return files, unreadable
}

// systemForwarding is the IPv4 forwarding the boot-time loader leaves: the
// last setting in the files it applies, in its order.
type systemForwarding struct {
	loader     sysctlLoader
	effective  forwardingSetting
	unreadable bool
	boot       SysctlBootState
}

// persisted reports whether the configuration certainly turns forwarding on
// at boot: the last applied setting is '=1', it is not in /run (emptied at
// boot), every file was readable and the loader actually runs at boot.
func (s systemForwarding) persisted() bool {
	return s.effective.value == "1" && !strings.HasPrefix(s.effective.where, "/run/") && !s.unreadable && !s.boot.needsAction()
}

func readSystemForwarding(probe systemProbe) systemForwarding {
	return readSystemForwardingWith(probe, nil)
}

// readSystemForwardingWith treats each path in pending as if it held the given
// content, whether or not it exists yet.
func readSystemForwardingWith(probe systemProbe, pending map[string][]byte) systemForwarding {
	loader := detectSysctlLoader(probe)
	extra := make([]string, 0, len(pending))
	for path := range pending {
		extra = append(extra, path)
	}
	files, unreadable := sysctlBootFiles(probe, loader, extra)
	result := systemForwarding{loader: loader, unreadable: unreadable, boot: detectSysctlBootState(probe)}
	seen := make(map[string]bool, len(files))
	for _, path := range files {
		if seen[path] {
			continue
		}
		seen[path] = true
		data, ok := pending[path]
		if !ok {
			var err error
			if data, err = probe.readFile(path); err != nil {
				result.unreadable = result.unreadable || !os.IsNotExist(err)
				continue
			}
		}
		if setting := parseForwardingSetting(data, path); setting.where != "" {
			result.effective = setting
		}
	}
	return result
}

// SystemForwardingPersistence reports where the host's own sysctl
// configuration persists net.ipv4.ip_forward=1 (file:line), if it certainly
// does. It only reads files; services that change the value after boot are not
// considered.
func SystemForwardingPersistence() (string, bool) {
	return systemForwardingPersistence(defaultSystemProbe())
}

func systemForwardingPersistence(probe systemProbe) (string, bool) {
	system := readSystemForwarding(probe)
	if !system.persisted() {
		return "", false
	}
	return system.effective.where, true
}

func inspectForwardingPersistence(probe systemProbe) Check {
	system := readSystemForwarding(probe)
	effective := system.effective
	check := Check{Name: "IPv4 转发持久化", Level: "warn", Fix: forwardingFix}
	if system.persisted() {
		check.Level = "pass"
		check.Message = "按开机时的加载方式（" + string(system.loader) + "）计算，最后生效的设置为 net.ipv4.ip_forward=1（" + effective.where + "）；仅确认文件设置，启动服务仍可能覆盖，重启后仍需验证。"
		check.Fix = ""
		return check
	}
	fileOn := effective.value == "1" && !strings.HasPrefix(effective.where, "/run/") && !system.unreadable
	if fileOn && system.boot.needsAction() {
		check.Message = "按开机时的加载方式（" + string(system.loader) + "）计算，最后生效的设置为 net.ipv4.ip_forward=1（" + effective.where + "），但 " + SysctlBootProblem(system.boot) + "；当前的转发只能靠其他服务（如 Mihomo Agent 启动前检查）在启动后补设，该服务停止或卸载后重启就会关闭。"
	} else if effective.where != "" {
		check.Message = "按开机时的加载方式（" + string(system.loader) + "）计算，最后生效的设置为 net.ipv4.ip_forward=" + effective.value + "（" + effective.where + "）；不能确认重启后的转发状态。"
	} else {
		check.Message = "未在常见 sysctl 配置中找到开机时会加载的 IPv4 转发设置（" + string(system.loader) + "）；可能由其他服务管理，无法确认重启后的状态。"
	}
	if system.unreadable {
		check.Message += " 部分配置无法读取。"
	}
	if strings.HasPrefix(effective.where, "/run/") {
		check.Message += " 该设置位于临时目录 /run，重启后不保证保留。"
	}
	if system.boot.needsAction() {
		bootFix := "并让开机时实际应用该设置：在这台 Linux 上执行下面的命令。"
		if fileOn {
			check.Fix = "让开机时实际应用该设置：在这台 Linux 上执行下面的命令。"
		} else {
			check.Message += " 另外，" + SysctlBootProblem(system.boot) + "。"
			check.Fix = forwardingFix + " " + bootFix
		}
		check.Groups = append(check.Groups, sysctlBootGroup(system.boot))
	}
	return check
}
