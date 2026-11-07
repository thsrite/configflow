package preflight

// The sysctl.d files only take effect at boot if the loader actually runs.
// systemd always runs systemd-sysctl; under OpenRC the sysctl service must be
// in a runlevel, and PVE's Alpine LXC template ships it in none.

// SysctlBootState says whether the boot-time sysctl loader runs.
type SysctlBootState string

const (
	// SysctlBootNotApplicable: not OpenRC (systemd or another init applies
	// sysctl.d its own way).
	SysctlBootNotApplicable SysctlBootState = ""
	// SysctlBootEnabled: the OpenRC sysctl service is in sysinit, boot or default.
	SysctlBootEnabled SysctlBootState = "enabled"
	// SysctlBootDisabled: the service exists but is in no runlevel; adding it
	// to the boot runlevel fixes it.
	SysctlBootDisabled SysctlBootState = "disabled"
	// SysctlBootUnavailable: OpenRC without a sysctl service script.
	SysctlBootUnavailable SysctlBootState = "unavailable"
)

const openRCSysctlScript = "/etc/init.d/sysctl"

// openRCSysctlRunlevels are the runlevels whose services run on every boot.
var openRCSysctlRunlevels = []string{"sysinit", "boot", "default"}

// SysctlBootEnableCommand adds the OpenRC sysctl service to the boot runlevel.
var SysctlBootEnableCommand = []string{"rc-update", "add", "sysctl", "boot"}

// openRCRunning reports whether OpenRC is the running init: it keeps its
// runtime state in /run/openrc.
func openRCRunning(probe systemProbe) bool {
	if probe.stat == nil {
		return false
	}
	info, err := probe.stat("/run/openrc")
	return err == nil && info.IsDir()
}

func detectSysctlBootState(probe systemProbe) SysctlBootState {
	if detectSysctlLoader(probe) == loaderSystemd || !openRCRunning(probe) {
		return SysctlBootNotApplicable
	}
	if _, err := probe.readFile(openRCSysctlScript); err != nil {
		return SysctlBootUnavailable
	}
	for _, level := range openRCSysctlRunlevels {
		// The runlevel entry is a symlink to the service script; reading it
		// succeeds only while both exist.
		if _, err := probe.readFile("/etc/runlevels/" + level + "/sysctl"); err == nil {
			return SysctlBootEnabled
		}
	}
	return SysctlBootDisabled
}

// SysctlBoot reports this host's boot-time sysctl loader state.
func SysctlBoot() SysctlBootState {
	return detectSysctlBootState(defaultSystemProbe())
}

// needsAction: sysctl.d is not applied at boot until the host is changed.
func (s SysctlBootState) needsAction() bool {
	return s == SysctlBootDisabled || s == SysctlBootUnavailable
}

const openRCLocalForwardScript = "/etc/local.d/configflow-ip-forward.start"

// SysctlBootManualCommands are the copyable commands that make the persisted
// forwarding apply at boot; nil when nothing is needed.
func SysctlBootManualCommands(state SysctlBootState) []string {
	switch state {
	case SysctlBootDisabled:
		return []string{"rc-update add sysctl boot", "rc-update show boot | grep sysctl"}
	case SysctlBootUnavailable:
		// Without a sysctl service, OpenRC's local service runs /etc/local.d.
		return []string{
			"mkdir -p /etc/local.d",
			"printf '#!/bin/sh\\nsysctl -w net.ipv4.ip_forward=1\\n' > " + openRCLocalForwardScript,
			"chmod 755 " + openRCLocalForwardScript,
			"rc-update add local default",
		}
	}
	return nil
}

// SysctlBootProblem describes why sysctl.d is not applied at boot; empty when
// it is.
func SysctlBootProblem(state SysctlBootState) string {
	switch state {
	case SysctlBootDisabled:
		return "OpenRC 的 sysctl 服务没有加入任何运行级别（rc-update show 中 sysctl 无运行级别），开机时不会应用 /etc/sysctl.d 中的设置"
	case SysctlBootUnavailable:
		return "系统由 OpenRC 启动，但没有 " + openRCSysctlScript + " 服务，开机时不会应用 /etc/sysctl.d 中的设置"
	}
	return ""
}

func sysctlBootGroup(state SysctlBootState) CommandGroup {
	group := CommandGroup{Label: "开机应用 IPv4 转发", Commands: SysctlBootManualCommands(state),
		Note: "重启后执行 sysctl net.ipv4.ip_forward 确认为 1。"}
	if state == SysctlBootUnavailable {
		group.Note = "没有 sysctl 服务时由 OpenRC local 服务在开机时开启转发；" + group.Note
	}
	return group
}
