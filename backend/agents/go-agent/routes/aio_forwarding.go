package routes

// Shell Mihomo Agent IPv4 forwarding persistence, matching aio-ipv4-forwarding.sh
// (native AIO) and the single-service installer install-go.sh, so a host can be
// moved to a full TUN or serve as an upstream router's proxy gateway without
// reinstalling. The installers and this file must produce identical artifacts:
// the shared sysctl.d file, the boot hook and the in-place patched Agent service.
import (
	"agent/preflight"
	"agent/upgrade"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Paths are variables only so tests can redirect them into a temporary root.
var aioSysctlDir = "/etc/sysctl.d"
var aioSystemdUnitDir = "/etc/systemd/system"
var aioOpenRCDir = "/etc/init.d"
var aioAgentRoot = "/opt/configflow-agent"
var aioSysfsNetRoot = "/sys/class/net"
var aioPrepareTUN = preflight.PrepareTUNAssumeEnabled

// aioSystemForwarding reads whether the host's own sysctl configuration
// persists forwarding, for hosts that did so before ConfigFlow managed it.
var aioSystemForwarding = preflight.SystemForwardingPersistence

// aioSysctlBoot reports whether the boot-time sysctl loader runs (OpenRC's
// sysctl service may be in no runlevel, e.g. PVE's Alpine LXC template).
var aioSysctlBoot = preflight.SysctlBoot
var aioLookPath = exec.LookPath

// daemon-reload and the hook may exceed the 3s diagnostic command timeout.
var aioRun = func(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if detail := strings.TrimSpace(string(exitErr.Stderr)); detail != "" {
			err = fmt.Errorf("%w: %s", err, detail)
		}
	}
	return string(out), err
}

const aioForwardingMarker = "# Managed by ConfigFlow Shell AIO IPv4 forwarding v1"
const aioForwardingContent = aioForwardingMarker + "\nnet.ipv4.ip_forward=1\n"
const aioForwardingHookName = "enable-ipv4-forwarding.sh"

// Byte-for-byte the heredoc in aio-ipv4-forwarding.sh (a test enforces it).
const aioForwardingHook = `#!/bin/sh
set -eu
config=/etc/sysctl.d/99-configflow-aio-forward.conf
# The installer commits this file only after both Agents start successfully.
[ -e "$config" ] || exit 0
expected='# Managed by ConfigFlow Shell AIO IPv4 forwarding v1
net.ipv4.ip_forward=1'
if [ -L "$config" ] || [ ! -f "$config" ] || [ "$(cat "$config")" != "$expected" ]; then
    echo "ConfigFlow IPv4 转发持久化配置已被修改，拒绝覆盖：$config" >&2
    exit 1
fi
current=$(cat /proc/sys/net/ipv4/ip_forward)
case "$current" in
    1) exit 0 ;;
    0) ;;
    *) echo "无法确认当前网络命名空间的 IPv4 转发状态" >&2; exit 1 ;;
esac
# Apply only this key, not unrelated sysctl files. Changing 0 -> 1 also has the
# normal Linux host/router side effects; do not toggle it on every Agent restart.
if ! sysctl -q -w net.ipv4.ip_forward=1; then
    echo "无法开启 IPv4 转发，请检查当前 Linux/容器的 CAP_NET_ADMIN 和 /proc/sys 写权限" >&2
    exit 1
fi
if [ "$(cat /proc/sys/net/ipv4/ip_forward)" != 1 ]; then
    echo "IPv4 转发写入后读回不是 1，未能确认开启" >&2
    exit 1
fi
`

// The single-service installer writes the same hook; only this comment names
// its one Agent (go_install_script.STANDALONE_FORWARDING_HOOK_COMMENT).
const (
	aioHookComment           = "# The installer commits this file only after both Agents start successfully.\n"
	aioStandaloneHookComment = "# The installer commits this file only after the Agent starts successfully.\n"
)

var aioStandaloneForwardingHook = strings.Replace(aioForwardingHook, aioHookComment, aioStandaloneHookComment, 1)

func aioForwardingConfigPath() string {
	return filepath.Join(aioSysctlDir, "99-configflow-aio-forward.conf")
}

// aioForwardingTarget is the Mihomo Agent service whose start hook keeps IPv4
// forwarding on: the native AIO layout or the single-service Shell layout.
type aioForwardingTarget struct {
	Layout          string   // "native_aio" | "standalone"
	Directory       string   // the hook lives here; WorkingDirectory= in the unit
	Unit            string   // without .service
	Manager         string   // systemd | openrc
	ExecStart       string   // the complete expected ExecStart= line
	OpenRCOwnership []string // standalone only: exact lines the init script must contain
}

// The single-service installer always writes this unit and program path.
const aioStandaloneUnit = "configflow-agent"

var aioStandaloneBinary = "/usr/local/bin/configflow-agent"

func (t aioForwardingTarget) Hook() string { return filepath.Join(t.Directory, aioForwardingHookName) }

// HookContent is the exact start hook the installer of this layout writes.
func (t aioForwardingTarget) HookContent() string {
	if t.Layout == "standalone" {
		return aioStandaloneForwardingHook
	}
	return aioForwardingHook
}

func (t aioForwardingTarget) SystemdHookLine() string { return "ExecStartPre=/bin/sh " + t.Hook() }
func (t aioForwardingTarget) OpenRCHookLine() string {
	return "    /bin/sh " + t.Hook() + " || return 1"
}

func (t aioForwardingTarget) UnitPath() string {
	if t.Manager == "systemd" {
		return filepath.Join(aioSystemdUnitDir, t.Unit+".service")
	}
	return filepath.Join(aioOpenRCDir, t.Unit)
}

func aioNativeTarget(instance nativeAIOInstallation) aioForwardingTarget {
	return aioForwardingTarget{
		Layout: "native_aio", Directory: instance.Directory, Unit: instance.Unit, Manager: instance.Manager,
		ExecStart: "ExecStart=" + instance.Directory + "/configflow-agent -config " + instance.Directory + "/config.json",
	}
}

// aioDefaultTarget is the native AIO Mihomo Agent, used where no installation
// is recognised so status keeps describing the AIO hook path.
func aioDefaultTarget() aioForwardingTarget {
	return aioNativeTarget(nativeAIOInstallation{Directory: filepath.Join(aioAgentRoot, "mihomo"), Unit: "configflow-agent-mihomo", Manager: "systemd"})
}

// aioForwardingTargetFor accepts only the fixed layouts the installers write:
// native AIO, or the single-service Mihomo install in its own directory (never
// a mixed layout where an AIO Mihomo directory also exists).
func aioForwardingTargetFor(cfg *Config) (aioForwardingTarget, error) {
	if instance, err := nativeAIOInstallationFor(cfg); err == nil {
		return aioNativeTarget(instance), nil
	}
	if cfg == nil || cfg.NativeAIO || cfg.ServiceType != "mihomo" {
		return aioForwardingTarget{}, fmt.Errorf("not a supported Mihomo Agent installation")
	}
	config := filepath.Join(aioAgentRoot, "config-mihomo.json")
	if cfg.AgentConfigFile != config {
		return aioForwardingTarget{}, fmt.Errorf("single-service Agent config path does not match the installer")
	}
	if unit := strings.TrimSuffix(cfg.AgentServiceUnit, ".service"); unit != "" && unit != aioStandaloneUnit {
		return aioForwardingTarget{}, fmt.Errorf("single-service Agent unit does not match the installer")
	}
	if cfg.ServiceManager != "systemd" && cfg.ServiceManager != "openrc" {
		return aioForwardingTarget{}, fmt.Errorf("single-service forwarding requires systemd or OpenRC")
	}
	if _, err := os.Lstat(filepath.Join(aioAgentRoot, "mihomo")); !os.IsNotExist(err) {
		return aioForwardingTarget{}, fmt.Errorf("mixed single-service and native AIO layout")
	}
	return aioForwardingTarget{
		Layout: "standalone", Directory: aioAgentRoot, Unit: aioStandaloneUnit, Manager: cfg.ServiceManager,
		ExecStart: "ExecStart=" + aioStandaloneBinary + " -config " + config,
		OpenRCOwnership: []string{
			"command=\"" + aioStandaloneBinary + "\"",
			"command_args=\"-config " + config + "\"",
		},
	}, nil
}

// The zero-argument forms describe the native AIO Mihomo Agent.
func aioForwardingHookPath() string { return aioDefaultTarget().Hook() }
func aioSystemdHookLine() string    { return aioDefaultTarget().SystemdHookLine() }
func aioOpenRCHookLine() string     { return aioDefaultTarget().OpenRCHookLine() }

// The first line of start_pre() in OpenRC scripts written before the installer
// reserved a hook line; the hook is inserted above it, as the installer does now.
const aioOpenRCLegacyFirstLine = "    checkpath --directory --mode 0755 /var/log"

// aioForwardingConfigManaged reports whether IPv4 forwarding is owned by the
// persisted AIO configuration. Gateway removal then leaves forwarding alone.
func aioForwardingConfigManaged() bool {
	state, _ := aioForwardingConfigState()
	return state == "managed"
}

func aioForwardingConfigState() (string, error) {
	path := aioForwardingConfigPath()
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "absent", nil
	}
	if err != nil {
		return "foreign", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "symlink", nil
	}
	if !info.Mode().IsRegular() {
		return "foreign", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "foreign", err
	}
	if string(data) == aioForwardingContent {
		return "managed", nil
	}
	return "foreign", nil
}

func aioHookScriptState(t aioForwardingTarget) (string, error) {
	path := t.Hook()
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "absent", nil
	}
	if err != nil {
		return "modified", err
	}
	if !info.Mode().IsRegular() {
		return "modified", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "modified", err
	}
	if string(data) == t.HookContent() {
		return "managed", nil
	}
	return "modified", nil
}

// aioInstallation accepts only the fixed layouts that the installers write;
// any other installation keeps its own forwarding management.
func aioInstallation(cfg *Config) (aioForwardingTarget, error) {
	if err := gatewayPlatform(cfg); err != nil {
		return aioForwardingTarget{}, err
	}
	target, err := aioForwardingTargetFor(cfg)
	if err != nil {
		return target, fmt.Errorf("仅支持 Shell 安装（同机安装或 Mihomo 单独安装，systemd 或 OpenRC）的 Mihomo Agent 自动持久化 IPv4 转发；其他安装方式请手动配置")
	}
	return target, nil
}

// The merged unit view also covers drop-ins outside /etc. update-agent.sh
// accepts exactly one ExecStartPre, and only this hook.
func aioSystemdStartupHook(t aioForwardingTarget) (string, error) {
	out, err := aioRun("systemctl", "show", "-p", "ExecStartPre", "--value", t.Unit+".service")
	if err != nil {
		return "unknown", fmt.Errorf("无法读取 Agent 服务的 ExecStartPre：%w", err)
	}
	value := strings.TrimSpace(out)
	if value == "" {
		return "absent", nil
	}
	if strings.Count(value, "{ path=") == 1 && strings.Contains(value, "{ path=/bin/sh ; argv[]=/bin/sh "+t.Hook()+" ;") {
		return "registered", nil
	}
	return "foreign", nil
}

// aioOpenRCOwned reports whether every ownership line occurs as a whole line.
func aioOpenRCOwned(lines []string, t aioForwardingTarget) bool {
	for _, want := range t.OpenRCOwnership {
		found := false
		for _, line := range lines {
			if line == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func aioOpenRCStartupHook(t aioForwardingTarget) (string, error) {
	data, err := os.ReadFile(filepath.Join(aioOpenRCDir, t.Unit))
	if err != nil {
		return "unknown", fmt.Errorf("无法读取 Agent 服务脚本：%w", err)
	}
	lines := strings.Split(string(data), "\n")
	if !aioOpenRCOwned(lines, t) {
		return "unknown", nil
	}
	for index, line := range lines {
		if line != "start_pre() {" || index+1 >= len(lines) {
			continue
		}
		switch lines[index+1] {
		case t.OpenRCHookLine():
			return "registered", nil
		case "    ", aioOpenRCLegacyFirstLine:
			return "absent", nil
		}
		return "unknown", nil
	}
	return "unknown", nil
}

func aioStartupHookState(t aioForwardingTarget) (string, error) {
	switch t.Manager {
	case "systemd":
		return aioSystemdStartupHook(t)
	case "openrc":
		return aioOpenRCStartupHook(t)
	}
	return "unknown", nil
}

func aioForwardingStatus(cfg *Config) map[string]interface{} {
	result := map[string]interface{}{"protocol": 1, "current": "", "supported": true}
	errs := []string{}
	target, supportErr := aioInstallation(cfg)
	if supportErr != nil {
		result["supported"], result["unsupported_reason"] = false, supportErr.Error()
		target = aioDefaultTarget()
	} else {
		result["layout"] = target.Layout
	}
	if value, err := gatewayReadSysctl("net/ipv4/ip_forward"); err != nil {
		errs = append(errs, "无法读取 IPv4 转发状态："+err.Error())
	} else {
		result["current"] = value
	}
	config, err := aioForwardingConfigState()
	if err != nil {
		errs = append(errs, "无法检查 IPv4 转发持久化配置："+err.Error())
	}
	hook, err := aioHookScriptState(target)
	if err != nil {
		errs = append(errs, "无法检查 IPv4 转发启动脚本："+err.Error())
	}
	startup := "unknown"
	if supportErr == nil {
		startup, err = aioStartupHookState(target)
		if err != nil {
			errs = append(errs, err.Error())
		}
	}
	result["config"], result["hook_script"], result["startup_hook"] = config, hook, startup
	// The Agent's start hook alone stops keeping forwarding on once the Agent
	// is stopped or removed; the sysctl.d file must also be applied at boot.
	boot := aioSysctlBoot()
	result["boot_sysctl"] = string(boot)
	bootReady := boot != preflight.SysctlBootDisabled && boot != preflight.SysctlBootUnavailable
	if !bootReady {
		result["notice"] = aioSysctlBootNotice(boot)
	}
	result["persisted"] = config == "managed" && hook == "managed" && startup == "registered" && bootReady
	// Persistence the host configured itself keeps forwarding on across
	// reboots, but only ConfigFlow's own keeps it when a gateway record is
	// removed, so it is reported separately and never counts as "persisted".
	source, system := aioSystemForwarding()
	result["system_persisted"] = system
	if system {
		result["system_source"] = source
	}
	if len(errs) != 0 {
		result["error"] = strings.Join(errs, "；")
	}
	return result
}

var aioDevicePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,14}$`)

// aioTUNStatus describes the deployed configuration; the device name is never
// assumed because old gateway profiles used Mihomo and full TUN uses Meta.
func aioTUNStatus(cfg *Config) map[string]interface{} {
	result := map[string]interface{}{"configured": false, "device": "Meta", "present": false, "gateway_marker": false}
	if cfg == nil || cfg.ServiceType != "mihomo" {
		result["error"] = "仅 Mihomo Agent 使用 TUN"
		return result
	}
	routing, err := readMihomoRoutingStrict(cfg.ConfigPath)
	if err != nil {
		result["error"] = err.Error()
		return result
	}
	marker, doc, err := gatewayConfigEnabled(cfg)
	if err != nil {
		result["error"] = err.Error()
		return result
	}
	tun, _ := doc["tun"].(map[string]interface{})
	result["device"], result["gateway_marker"] = routing.tunDevice, marker
	result["configured"] = routing.tunEnabled && tun["auto-route"] == true && tun["auto-redirect"] == true
	if aioDevicePattern.MatchString(routing.tunDevice) {
		_, err := os.Stat(filepath.Join(aioSysfsNetRoot, routing.tunDevice))
		result["present"] = err == nil
	}
	return result
}

// aioPatchSystemd returns the patched unit, or nil when already registered.
func aioPatchSystemd(data []byte, t aioForwardingTarget) ([]byte, error) {
	lines := strings.Split(string(data), "\n")
	execStart := t.ExecStart
	hook, starts, at := t.SystemdHookLine(), 0, -1
	for index, line := range lines {
		if strings.HasPrefix(line, "ExecStartPre=") && line != hook {
			return nil, fmt.Errorf("Agent 服务存在未知 ExecStartPre，无法自动注册 IPv4 转发检查")
		}
		if strings.HasPrefix(line, "ExecStart=") {
			starts++
			at = index
		}
	}
	if starts != 1 || lines[at] != execStart || strings.Count(string(data), hook+"\n") > 1 {
		return nil, fmt.Errorf("Agent 服务的 ExecStart 不是安装器写入的格式，无法自动注册 IPv4 转发检查")
	}
	previous := ""
	if at > 0 {
		previous = lines[at-1]
	}
	switch {
	case previous == hook:
		return nil, nil
	case strings.Contains(string(data), hook+"\n"):
		return nil, fmt.Errorf("Agent 服务的 IPv4 转发检查位置异常，请重新运行原安装命令")
	case at > 0 && previous == "":
		lines[at-1] = hook
	case at > 0 && previous == "WorkingDirectory="+t.Directory:
		lines = append(lines[:at], append([]string{hook}, lines[at:]...)...)
	default:
		return nil, fmt.Errorf("Agent 服务文件结构无法识别，无法自动注册 IPv4 转发检查；请重新运行原安装命令")
	}
	return []byte(strings.Join(lines, "\n")), nil
}

func aioPatchOpenRC(data []byte, t aioForwardingTarget) ([]byte, error) {
	lines := strings.Split(string(data), "\n")
	if !aioOpenRCOwned(lines, t) {
		return nil, fmt.Errorf("Agent 服务脚本不是 ConfigFlow 单独安装写入的格式，无法自动注册 IPv4 转发检查")
	}
	hook, found := t.OpenRCHookLine(), -1
	for index, line := range lines {
		if line == "start_pre() {" {
			if found >= 0 {
				found = -2
				break
			}
			found = index
		}
	}
	if found >= 0 && found+1 < len(lines) {
		switch lines[found+1] {
		case hook:
			return nil, nil
		case "    ":
			lines[found+1] = hook
			return []byte(strings.Join(lines, "\n")), nil
		case aioOpenRCLegacyFirstLine:
			lines = append(lines[:found+1], append([]string{hook}, lines[found+1:]...)...)
			return []byte(strings.Join(lines, "\n")), nil
		}
	}
	return nil, fmt.Errorf("Agent 服务脚本已被修改，无法自动注册 IPv4 转发检查；请重新运行原安装命令")
}

func aioRegularFile(path, label string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("无法检查%s：%w", label, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s不是普通文件（不能是符号链接）：%s", label, path)
	}
	return nil
}

func aioSystemdDropInsWithPre(unit string) error {
	directory := filepath.Join(aioSystemdUnitDir, unit+".service.d")
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("无法检查 Agent 服务的附加配置：%w", err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil || strings.Contains(string(data), "ExecStartPre") {
			return fmt.Errorf("Agent 服务的附加配置 %s 含 ExecStartPre 或无法读取，无法自动注册 IPv4 转发检查", filepath.Join(directory, entry.Name()))
		}
	}
	return nil
}

func aioForwardingFailure(message string, err error) error {
	return fmt.Errorf("%s：%w。请检查当前 Linux/容器的 CAP_NET_ADMIN 和 /proc/sys 写权限；LXC 权限受限时需由宿主机管理员调整", message, err)
}

func summarizeTUNPrepare(report preflight.Report) map[string]interface{} {
	level, messages := "pass", []string{}
	for _, check := range report {
		switch check.Level {
		case "error":
			level = "error"
		case "warn":
			if level == "pass" {
				level = "warn"
			}
		}
		message := check.Message
		if check.Level != "pass" && check.Fix != "" {
			message += " " + check.Fix
		}
		messages = append(messages, message)
	}
	return map[string]interface{}{"level": level, "message": strings.Join(messages, "\n")}
}

// persistAIOForwarding enables and persists IPv4 forwarding the way the
// ordinary AIO and single-service Mihomo installers do. The caller holds gatewayMu, returnRoutingMu and
// the service operation lock. Every step is checked first, so a retry after
// any interruption converges. Forwarding is never written to 0.
func persistAIOForwarding(cfg *Config, prepareTUN bool) (result map[string]interface{}, resultErr error) {
	instance, err := aioInstallation(cfg)
	if err != nil {
		return nil, err
	}
	if err := (nativeAIOInstallation{Directory: instance.Directory}).validateDirectory(); err != nil {
		return nil, fmt.Errorf("Agent 安装目录无效：%w", err)
	}
	// The boot hook runs these; without them a later Agent restart would fail
	// its start check while ip_forward is still 0 (the installer checks too).
	for _, command := range []string{"sysctl", "cat"} {
		if _, err := aioLookPath(command); err != nil {
			return nil, fmt.Errorf("缺少 %s 命令，无法注册 IPv4 转发开机检查；请先安装该命令（Debian/Ubuntu 的 sysctl 由 procps 提供）后重试", command)
		}
	}
	if info, err := os.Lstat(aioSysctlDir); os.IsNotExist(err) {
		if err := os.Mkdir(aioSysctlDir, 0755); err != nil {
			return nil, fmt.Errorf("无法创建 IPv4 转发持久化目录：%w", err)
		}
	} else if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("sysctl.d 路径不是目录或是符号链接：%s", aioSysctlDir)
	}
	target := aioForwardingConfigPath()
	config, err := aioForwardingConfigState()
	if err != nil || (config != "absent" && config != "managed") {
		return nil, fmt.Errorf("IPv4 转发持久化路径已有非 ConfigFlow 配置，请先核对：%s", target)
	}
	// Writing the current value tests the real kernel permission without
	// toggling forwarding; a root uid is not sufficient inside LXC.
	const key = "net/ipv4/ip_forward"
	current, err := gatewayReadSysctl(key)
	if err != nil {
		return nil, aioForwardingFailure("无法读取 IPv4 转发状态", err)
	}
	if err := gatewayWriteSysctl(key, current); err != nil {
		return nil, aioForwardingFailure("无法写入 IPv4 转发参数", err)
	}
	if value, err := gatewayReadSysctl(key); err != nil || value != current {
		return nil, aioForwardingFailure("IPv4 转发权限检查后读回值不一致", fmt.Errorf("读回 %q：%v", value, err))
	}
	hook := instance.Hook()
	hookState, err := aioHookScriptState(instance)
	if err != nil {
		return nil, fmt.Errorf("无法检查 IPv4 转发启动脚本：%w", err)
	}
	if hookState == "modified" {
		return nil, fmt.Errorf("IPv4 转发启动脚本已被修改，拒绝覆盖：%s", hook)
	}

	// Inspect the service before changing anything; patched is nil when the
	// file already carries the hook in the installer's position.
	unitPath := instance.UnitPath()
	if err := aioRegularFile(unitPath, "Agent 服务文件"); err != nil {
		return nil, err
	}
	snapshot, err := upgrade.Capture(unitPath)
	if err != nil {
		return nil, fmt.Errorf("无法读取 Agent 服务文件：%w", err)
	}
	var patched []byte
	loaded := ""
	if instance.Manager == "systemd" {
		if err := aioSystemdDropInsWithPre(instance.Unit); err != nil {
			return nil, err
		}
		if patched, err = aioPatchSystemd(snapshot.Data, instance); err != nil {
			return nil, err
		}
		if loaded, err = aioSystemdStartupHook(instance); err != nil {
			return nil, err
		}
		if loaded == "foreign" || (patched != nil && loaded != "absent") {
			return nil, fmt.Errorf("Agent 服务已从其他位置加载 ExecStartPre，无法自动注册 IPv4 转发检查")
		}
	} else if patched, err = aioPatchOpenRC(snapshot.Data, instance); err != nil {
		return nil, err
	}

	// Until the sysctl.d commit point, undo this call's hook and unit changes.
	committed, hookCreated, reloaded := false, false, false
	var unitSnapshot *upgrade.Snapshot
	defer func() {
		if resultErr == nil || committed {
			return
		}
		rollback := []string{}
		if unitSnapshot != nil {
			if err := upgrade.Restore([]upgrade.Snapshot{*unitSnapshot}); err != nil {
				rollback = append(rollback, "恢复 Agent 服务文件失败："+err.Error())
			} else if reloaded {
				if _, err := aioRun("systemctl", "daemon-reload"); err != nil {
					rollback = append(rollback, "恢复后 systemctl daemon-reload 失败："+err.Error())
				}
			}
		}
		if hookCreated {
			if err := os.Remove(hook); err != nil && !os.IsNotExist(err) {
				rollback = append(rollback, "删除本次写入的启动脚本失败："+err.Error())
			}
		}
		if len(rollback) != 0 {
			resultErr = fmt.Errorf("%v；%s", resultErr, strings.Join(rollback, "；"))
		}
	}()
	if hookState == "absent" {
		if err := durableWrite(hook, []byte(instance.HookContent()), 0755); err != nil {
			return nil, fmt.Errorf("无法写入 IPv4 转发启动脚本：%w", err)
		}
		hookCreated = true
	}
	if patched != nil {
		if err := upgrade.AtomicWrite(unitPath, patched, snapshot.Mode); err != nil {
			return nil, fmt.Errorf("无法更新 Agent 服务文件：%w", err)
		}
		unitSnapshot = &snapshot
	}
	// A previous attempt may have patched the file but not reloaded systemd.
	if instance.Manager == "systemd" && (patched != nil || loaded != "registered") {
		reloaded = true
		if _, err := aioRun("systemctl", "daemon-reload"); err != nil {
			return nil, fmt.Errorf("systemctl daemon-reload 失败：%w", err)
		}
		if state, err := aioSystemdStartupHook(instance); err != nil || state != "registered" {
			return nil, fmt.Errorf("重新加载后 Agent 服务未注册 IPv4 转发检查（%s）：%v", state, err)
		}
	}

	if config == "absent" {
		if err := aioCommitForwardingConfig(target); err != nil {
			return nil, err
		}
	}
	committed = true
	if _, err := aioRun("/bin/sh", hook); err != nil {
		return nil, fmt.Errorf("IPv4 转发未能成功开启并验证：%w", err)
	}
	if !aioForwardingConfigManaged() {
		return nil, fmt.Errorf("IPv4 转发持久化配置在应用期间发生变化：%s", target)
	}
	if value, err := gatewayReadSysctl(key); err != nil || value != "1" {
		return nil, aioForwardingFailure("最终 IPv4 转发状态不是 1", fmt.Errorf("读回 %q：%v", value, err))
	}
	if err := aioEnsureSysctlBoot(); err != nil {
		return nil, err
	}
	result = map[string]interface{}{"tun_prepare": nil}
	if prepareTUN {
		// TUN preparation is advisory here: forwarding is already persisted.
		result["tun_prepare"] = summarizeTUNPrepare(aioPrepareTUN(deploymentDNSConfig(cfg)))
	}
	return result, nil
}

// The hard link is an atomic commit that refuses a file created concurrently.
func aioCommitForwardingConfig(target string) error {
	file, err := os.CreateTemp(aioSysctlDir, ".configflow-aio-forward.")
	if err != nil {
		return fmt.Errorf("IPv4 转发持久化目录不可写：%w", err)
	}
	temp := file.Name()
	defer os.Remove(temp)
	_, err = file.WriteString(aioForwardingContent)
	if err == nil {
		err = file.Chmod(0644)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("无法写入 IPv4 转发持久化文件：%w", err)
	}
	if err := os.Link(temp, target); err != nil {
		if aioForwardingConfigManaged() {
			return nil
		}
		return fmt.Errorf("无法提交 IPv4 转发持久化配置：%w", err)
	}
	return syncDirectory(aioSysctlDir)
}

func aioSysctlBootNotice(boot preflight.SysctlBootState) string {
	problem := preflight.SysctlBootProblem(boot)
	if boot == preflight.SysctlBootDisabled {
		return problem + "；开启并持久化时会自动执行 rc-update add sysctl boot。"
	}
	return problem + "；请在这台 Linux 上执行：" + strings.Join(preflight.SysctlBootManualCommands(boot), " && ") + "，重启后用 sysctl net.ipv4.ip_forward 确认为 1。"
}

// aioEnsureSysctlBoot makes OpenRC apply sysctl.d at boot, as the installers
// do (ensure_ipv4_sysctl_boot): the existing sysctl service is added to the
// boot runlevel. Forwarding is already on and kept by the Agent hook, so a
// failure only asks for the manual step.
func aioEnsureSysctlBoot() error {
	boot := aioSysctlBoot()
	if boot == preflight.SysctlBootDisabled {
		command := preflight.SysctlBootEnableCommand
		_, err := aioRun(command[0], command[1:]...)
		if boot = aioSysctlBoot(); boot == preflight.SysctlBootEnabled {
			return nil
		}
		if err == nil {
			err = fmt.Errorf("执行后 sysctl 服务仍不在开机运行级别")
		}
		return fmt.Errorf("IPv4 转发已开启并写入 %s，但无法把 OpenRC 的 sysctl 服务加入开机运行级别（%v），开机时不会应用该设置。请在这台 Linux 上执行：%s，重启后用 sysctl net.ipv4.ip_forward 确认为 1", aioForwardingConfigPath(), err, strings.Join(preflight.SysctlBootManualCommands(preflight.SysctlBootDisabled), " && "))
	}
	if boot == preflight.SysctlBootUnavailable {
		return fmt.Errorf("IPv4 转发已开启并写入 %s，但%s。请在这台 Linux 上执行：%s，重启后用 sysctl net.ipv4.ip_forward 确认为 1", aioForwardingConfigPath(), preflight.SysctlBootProblem(boot), strings.Join(preflight.SysctlBootManualCommands(boot), " && "))
	}
	return nil
}
