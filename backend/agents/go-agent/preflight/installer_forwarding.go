package preflight

import (
	"agent/netfilter"
	"net"
	"strings"
)

// installerForwardingEnv is set, for the environment check only, by installers
// that enable and persist IPv4 forwarding themselves (ordinary Shell AIO, the
// gateway wizard's full TUN install and standalone Mihomo by default).
const installerForwardingEnv = "CONFIGFLOW_INSTALLER_FORWARDING"

const installerForwardingConfig = "/etc/sysctl.d/99-configflow-aio-forward.conf"

const installerForwardingName = "IPv4 转发（安装器管理）"

// The installer turns forwarding on itself, so the current value is a fact to
// report, not a manual repair to ask for.
func inspectInstallerForwarding(probe systemProbe) []Check {
	data, err := probe.readFile("/proc/sys/net/ipv4/ip_forward")
	value := strings.TrimSpace(string(data))
	check := Check{Name: installerForwardingName, Level: "pass"}
	switch {
	case err == nil && value == "0":
		check.Message = "当前 net.ipv4.ip_forward=0，安装器将开启并持久化（" + installerForwardingConfig + "）；Mihomo Agent 每次启动前会再次检查。"
	case err == nil && value == "1":
		check.Message = "当前 net.ipv4.ip_forward=1，安装器将持久化（" + installerForwardingConfig + "）；Mihomo Agent 每次启动前会再次检查。"
	default:
		check.Level = "warn"
		check.Message = "无法读取当前 net.ipv4.ip_forward；安装器稍后会再次检查，无法开启时会停止安装。"
	}
	checks := []Check{check}
	if conflict, ok := inspectInstallerForwardingConflict(probe); ok {
		checks = append(checks, conflict)
	}
	if boot, ok := inspectInstallerSysctlBoot(probe); ok {
		checks = append(checks, boot)
	}
	return checks
}

const installerSysctlBootName = "IPv4 转发开机加载"

// The installers add the OpenRC sysctl service to the boot runlevel
// (ensure_ipv4_sysctl_boot); only a host without that service needs a manual
// step.
func inspectInstallerSysctlBoot(probe systemProbe) (Check, bool) {
	switch state := detectSysctlBootState(probe); state {
	case SysctlBootDisabled:
		return Check{Name: installerSysctlBootName, Level: "pass",
			Message: SysctlBootProblem(state) + "；安装器会执行 rc-update add sysctl boot，让持久化的转发设置开机生效。"}, true
	case SysctlBootUnavailable:
		return Check{Name: installerSysctlBootName, Level: "warn",
			Message: SysctlBootProblem(state) + "；安装器无法自动设置，开机后转发只能靠 Mihomo Agent 启动前检查补设。",
			Fix:     "在这台 Linux 上执行下面的命令，让开机时开启转发。",
			Groups:  []CommandGroup{sysctlBootGroup(state)}}, true
	}
	return Check{}, false
}

// inspectInstallerForwardingConflict recomputes the boot-time value as if the
// installer's file already existed, and reports only a later '=0'.
func inspectInstallerForwardingConflict(probe systemProbe) (Check, bool) {
	system := readSystemForwardingWith(probe, map[string][]byte{installerForwardingConfig: []byte("net.ipv4.ip_forward=1\n")})
	if system.effective.value != "0" {
		return Check{}, false
	}
	return Check{
		Name:    "IPv4 转发冲突",
		Level:   "warn",
		Message: system.effective.where + " 在 ConfigFlow 的持久化文件之后加载，并设置 net.ipv4.ip_forward=0，重启后转发可能被关闭。",
		Fix:     "把该行改为 1 或删除，重启后用 sysctl net.ipv4.ip_forward 确认为 1。",
	}, true
}

const forwardFilterFix = "需要放行时在这台 Linux 上执行下面的命令；发布 Mihomo 配置后以 ConfigFlow 页面显示的命令为准（TUN 设备名可能不同）。"

// installerForwardTarget uses the install configuration's TUN settings when it
// enables TUN, otherwise ConfigFlow's full-TUN default (Meta, mixed stack,
// auto-redirect, dns-hijack any:53). It follows the scope of the Agent status
// (routes/forward_filter.go): a selective Linux gateway configuration (marker
// present, enabled or not), a route-restricted TUN and an enabled TUN without
// auto-route are not full TUN and are not inspected. With a DNS service on the
// same host (AIO), DNS to this host is served by it through the input hook,
// not hijacked into the TUN.
func installerForwardTarget(configs []Config, local []net.IP) (netfilter.Target, bool) {
	target := netfilter.Target{Device: "Meta", AutoRedirect: true, Stack: "mixed", DNSHijack: true}
	dnsService := false
	for _, cfg := range configs {
		if cfg.ServiceType == "oxidns" || cfg.ServiceType == "mosdns" {
			dnsService = true
		}
	}
	for _, cfg := range configs {
		if cfg.ServiceType != "mihomo" {
			continue
		}
		doc, err := loadCoreConfig(cfg)
		if err != nil {
			break
		}
		if _, marker := doc["x-configflow-linux-gateway"]; marker {
			return netfilter.Target{}, false
		}
		tun, _ := doc["tun"].(map[string]interface{})
		for _, key := range []string{"route-address", "route-address-set", "inet4-route-address"} {
			if entries, ok := tun[key].([]interface{}); ok && len(entries) > 0 {
				return netfilter.Target{}, false
			}
		}
		if tun["enable"] == true {
			if tun["auto-route"] != true {
				return netfilter.Target{}, false
			}
			target = netfilter.TUNTarget(tun, local)
		}
		break
	}
	target.LAN = local
	if dnsService {
		// The DNS service of an all-in-one install answers port 53 on this
		// host itself, through the input hook; it may not listen yet.
		target.DNSHijack, target.ServesDNS = false, true
	}
	return target, true
}

// inspectForwardFilter only ever warns; an unknown result is informational.
// Each source keeps its own commands and persistence note.
func inspectForwardFilter(probe systemProbe, configs []Config) (Check, bool) {
	if probe.forwardFilter == nil {
		return Check{}, false
	}
	var local []net.IP
	if probe.lanAddresses != nil {
		local = probe.lanAddresses()
	}
	target, applicable := installerForwardTarget(configs, local)
	if !applicable {
		return Check{}, false
	}
	result := probe.forwardFilter(target)
	check := Check{Name: "转发过滤"}
	switch result.State {
	case netfilter.StateMayDrop:
		check.Level, check.Fix = "warn", forwardFilterFix
		check.Message = strings.Replace(result.Hint, "，需要时在这台 Linux 上执行下面的命令放行。", "。", 1)
		check.Message = strings.Replace(check.Message, "，请在这台 Linux 上手动放行这些流量。", "。", 1)
		for _, remedy := range result.Remedies {
			if len(remedy.Commands) > 0 {
				check.Groups = append(check.Groups, CommandGroup{Label: remedy.Label, Commands: remedy.Commands, Note: remedy.PersistNote})
			}
		}
		if len(check.Groups) == 0 {
			check.Fix = "请在这台 Linux 上手动放行上述流量；发布 Mihomo 配置后可在 ConfigFlow 页面查看检查结果。"
		}
	case netfilter.StateOK:
		check.Level, check.Message = "pass", "未发现会拦截 TUN 转发或本机入站的防火墙规则。"
	default:
		reason := result.Reason
		if reason == "" {
			reason = "unknown"
		}
		check.Level, check.Message = "info", "未能确认转发过滤（"+reason+"），不影响安装。"
	}
	return check, true
}
