package routes

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const haServiceUnit = "keepalived"
const haBlockBegin = "# BEGIN CONFIGFLOW OXIDNS-HA"
const haBlockEnd = "# END CONFIGFLOW OXIDNS-HA"
const haHeaderPrefix = "# configflow-oxidns-ha:"

var haKeepalivedDir = defaultHAKeepalivedDirectory()

// haScriptSecure mirrors keepalived's script security: the health check binary
// and every parent directory must be root owned and not writable by others.
var haScriptSecure = checkHAScriptSecure

func haConfigPath() string     { return filepath.Join(haKeepalivedDir, "configflow-oxidns.conf") }
func haMainConfigPath() string { return filepath.Join(haKeepalivedDir, "keepalived.conf") }
func haBlock() string {
	return haBlockBegin + "\ninclude " + haConfigPath() + "\n" + haBlockEnd + "\n"
}

func haDigest(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

func haScriptArgument(value string) (string, error) {
	if !filepath.IsAbs(value) || strings.ContainsAny(value, "\"'\\#!") {
		return "", fmt.Errorf("路径 %q 含有引号、反斜杠、# 或 !，keepalived 无法安全调用健康检查", value)
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("路径 %q 含有控制字符，keepalived 无法安全调用健康检查", value)
		}
	}
	if strings.ContainsAny(value, " \t") {
		return "'" + value + "'", nil
	}
	return value, nil
}

// haScriptCommand is the keepalived track_script: the Agent binary itself.
func haScriptCommand(cfg *Config) (string, error) {
	if cfg == nil || cfg.AgentConfigFile == "" {
		return "", fmt.Errorf("Agent 配置路径未知，无法生成健康检查命令")
	}
	binary, err := haExecutable()
	if err != nil {
		return "", fmt.Errorf("无法确定 Agent 程序路径：%w", err)
	}
	if binary, err = filepath.Abs(binary); err != nil {
		return "", err
	}
	config, err := filepath.Abs(cfg.AgentConfigFile)
	if err != nil {
		return "", err
	}
	binaryArg, err := haScriptArgument(binary)
	if err != nil {
		return "", err
	}
	configArg, err := haScriptArgument(config)
	if err != nil {
		return "", err
	}
	if err := haScriptSecure(binary); err != nil {
		return "", err
	}
	return binaryArg + " -config " + configArg + " -ha-check", nil
}

func checkHAScriptSecure(path string) error {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Stat(current)
		if err != nil {
			return fmt.Errorf("无法检查健康检查程序路径 %s：%w", current, err)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if info.Mode().Perm()&0022 != 0 || (ok && stat.Uid != 0) {
			return fmt.Errorf("%s 可被非 root 用户修改，keepalived 会拒绝执行健康检查；请调整为 root 所有且其他用户不可写", current)
		}
		if current == filepath.Dir(current) {
			return nil
		}
	}
}

func renderHAConfig(state *haState, script string) string {
	_, network, _ := net.ParseCIDR(state.CIDR)
	prefix, _ := network.Mask.Size()
	spec := state.Spec
	var b strings.Builder
	b.WriteString(haHeaderPrefix + state.Token + "\n")
	b.WriteString("# 由 ConfigFlow 管理；请在 ConfigFlow 中修改或停用 DNS 高可用，不要手动编辑。\n")
	b.WriteString("vrrp_script cf_oxidns_check {\n")
	b.WriteString("    script \"" + script + "\"\n")
	b.WriteString("    interval 1\n    timeout 2\n    fall 2\n    rise 2\n    weight -60\n}\n")
	b.WriteString("vrrp_instance cf_oxidns {\n")
	b.WriteString("    state BACKUP\n")
	b.WriteString("    interface " + state.Interface + "\n")
	b.WriteString("    virtual_router_id " + strconv.Itoa(spec.VRID) + "\n")
	b.WriteString("    priority " + strconv.Itoa(spec.Priority) + "\n")
	b.WriteString("    advert_int 1\n")
	b.WriteString("    unicast_src_ip " + spec.SelfIP + "\n")
	b.WriteString("    unicast_peer {\n        " + spec.PeerIP + "\n    }\n")
	b.WriteString(fmt.Sprintf("    virtual_ipaddress {\n        %s/%d dev %s\n    }\n", spec.VIP, prefix, state.Interface))
	b.WriteString("    track_script {\n        cf_oxidns_check\n    }\n}\n")
	return b.String()
}

// haStripSection removes the marked block with the same rule as recovery
// markers: a malformed marker leaves the content untouched and is reported.
func haStripSection(content string) (string, bool, error) {
	lines := strings.SplitAfter(content, "\n")
	var output strings.Builder
	skipping, found := false, false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == haBlockBegin {
			if skipping || found {
				return content, true, fmt.Errorf("%s 中 ConfigFlow 标记重复或不完整，请修复后重试", haMainConfigPath())
			}
			skipping, found = true, true
			continue
		}
		if trimmed == haBlockEnd {
			if !skipping {
				return content, true, fmt.Errorf("%s 中 ConfigFlow 标记不完整，请修复后重试", haMainConfigPath())
			}
			skipping = false
			continue
		}
		if !skipping {
			output.WriteString(line)
		}
	}
	if skipping {
		return content, true, fmt.Errorf("%s 中 ConfigFlow 标记不完整，请修复后重试", haMainConfigPath())
	}
	return output.String(), found, nil
}

// haUpsertSection keeps an existing block in place or appends a new one.
func haUpsertSection(content string, exists bool) (string, error) {
	if !exists {
		return haBlock(), nil
	}
	if _, found, err := haStripSection(content); err != nil {
		return content, err
	} else if !found {
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		return content + haBlock(), nil
	}
	var output strings.Builder
	skipping := false
	for _, line := range strings.SplitAfter(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == haBlockBegin {
			skipping = true
			output.WriteString(haBlock())
			continue
		}
		if skipping {
			if trimmed == haBlockEnd {
				skipping = false
			}
			continue
		}
		output.WriteString(line)
	}
	return output.String(), nil
}

func haHasBlock(content string) bool {
	if _, found, err := haStripSection(content); err != nil || !found {
		return false
	}
	return strings.HasPrefix(content, haBlock()) || strings.Contains(content, "\n"+haBlock())
}

// haConfigFields splits keepalived configuration into words per line, without
// comments ("#" and "!" start a comment in keepalived).
func haConfigFields(content string) [][]string {
	result := [][]string{}
	for _, line := range strings.Split(content, "\n") {
		if index := strings.IndexAny(line, "#!"); index >= 0 {
			line = line[:index]
		}
		if fields := strings.Fields(line); len(fields) > 0 {
			result = append(result, fields)
		}
	}
	return result
}

// haVRIDConflict looks for another vrrp_instance using the same router ID in
// the user's own keepalived.conf, outside the ConfigFlow block.
func haVRIDConflict(content string, vrid int) bool {
	for _, fields := range haConfigFields(content) {
		for i := 0; i+1 < len(fields); i++ {
			if fields[i] == "virtual_router_id" && fields[i+1] == strconv.Itoa(vrid) {
				return true
			}
		}
	}
	return false
}

// haStrictMode reports vrrp_strict: strict mode drops instances with unicast
// peers and adds firewall rules that discard traffic to the virtual IP.
func haStrictMode(content string) bool {
	for _, fields := range haConfigFields(content) {
		for _, field := range fields {
			if field == "vrrp_strict" {
				return true
			}
		}
	}
	return false
}

// haDormantSections lists the user's own sections that would start together
// with ours, such as the sample VI_1 and virtual_server shipped by RPM packages.
func haDormantSections(content string) []string {
	found := []string{}
	for _, fields := range haConfigFields(content) {
		for _, field := range fields {
			switch field {
			case "vrrp_instance", "vrrp_sync_group", "virtual_server", "virtual_server_group", "include":
				found = append(found, field)
			}
		}
	}
	return found
}

func haHeaderOwned(content, token string) bool {
	first, _, _ := strings.Cut(content, "\n")
	return strings.TrimSpace(first) == haHeaderPrefix+token
}

func readOptional(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(data), true, nil
}

// haCheckKeepalivedFiles reports files that already exist but cannot be proven
// to belong to this journal, and problems in the user's main config: VRID
// conflicts, strict mode, and sections that starting keepalived would activate.
func haCheckKeepalivedFiles(cfg *Config, spec OxiDNSHASpec, state *haState) error {
	include, exists, err := readOptional(haConfigPath())
	if err != nil {
		return fmt.Errorf("无法读取 %s：%w", haConfigPath(), err)
	}
	if exists && (state == nil || !haHeaderOwned(include, state.Token)) {
		return fmt.Errorf("%s 已存在，但不属于当前 DNS 高可用记录；请确认后手动删除再重试", haConfigPath())
	}
	main, exists, err := readOptional(haMainConfigPath())
	if err != nil {
		return fmt.Errorf("无法读取 %s：%w", haMainConfigPath(), err)
	}
	if !exists {
		return nil
	}
	clean, found, err := haStripSection(main)
	if err != nil {
		return err
	}
	if found && state == nil {
		return fmt.Errorf("%s 中已有 ConfigFlow 标记块，但没有对应的 DNS 高可用记录；请确认后手动删除标记块再重试", haMainConfigPath())
	}
	if haVRIDConflict(clean, spec.VRID) {
		return fmt.Errorf("%s 中已有 virtual_router_id 为 %d 的 vrrp_instance，请换一个 VRID", haMainConfigPath(), spec.VRID)
	}
	if haStrictMode(clean) {
		return fmt.Errorf("%s 开启了 vrrp_strict：严格模式不支持单播对端，还会丢弃发往虚拟 IP 的流量；请删除 vrrp_strict 后重试", haMainConfigPath())
	}
	// Only the first apply sees keepalived as the user left it; afterwards we
	// started it ourselves. A stopped keepalived would bring these sections up.
	if sections := haDormantSections(clean); state == nil && len(sections) > 0 && cfg != nil && !haServiceRunning(cfg.ServiceManager) {
		return fmt.Errorf("keepalived 当前未运行，%s 中已有 %s 配置（可能是安装包自带的示例），开启后会被一并启动；"+
			"请删除这些配置，或确认它们可以运行并先自行启动 keepalived，再重试", haMainConfigPath(), strings.Join(haUnique(sections), "、"))
	}
	return nil
}

func haUnique(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

// haFilesDrift reports whether the keepalived files differ from the journal.
func haFilesDrift(cfg *Config, state *haState) error {
	script, err := haScriptCommand(cfg)
	if err != nil {
		return err
	}
	include, _, err := readOptional(haConfigPath())
	if err != nil {
		return err
	}
	if include != renderHAConfig(state, script) {
		return fmt.Errorf("keepalived 配置文件缺失或与记录不一致，请在 ConfigFlow 重新开启 DNS 高可用以修复")
	}
	main, _, err := readOptional(haMainConfigPath())
	if err != nil {
		return err
	}
	if !haHasBlock(main) {
		return fmt.Errorf("%s 缺少 ConfigFlow 引用块，请在 ConfigFlow 重新开启 DNS 高可用以修复", haMainConfigPath())
	}
	return nil
}

// Messages for removals that could not release keepalived.
const (
	haMsgStopBeforeRemove = "停用 DNS 高可用前停止 keepalived 失败，配置未删除，可重试停用：%v"
	haMsgStillRunning     = "停用 DNS 高可用后 keepalived 仍在运行（服务管理器无法停止它），虚拟 IP %s 可能仍由本机持有；请检查 keepalived 进程后重试停用"
	haMsgVIPStillPresent  = "停用 DNS 高可用后虚拟 IP %s 仍在本机网卡上；请确认 keepalived 已停止并移除该地址后重试停用"
)

// haOpenRCPIDFile is where Alpine's keepalived init script (command_background)
// records the daemon. It still names the daemon when the init script itself
// refuses to run because required_files (keepalived.conf) is missing.
var haOpenRCPIDFile = "/run/keepalived.pid"

// haProcRoot is replaceable in tests.
var haProcRoot = "/proc"

// haKill is replaceable in tests.
var haKill = syscall.Kill

// haOpenRCPID returns the pid of a live keepalived recorded in the OpenRC
// pidfile, or 0.
func haOpenRCPID() int {
	data, err := os.ReadFile(haOpenRCPIDFile)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 {
		return 0
	}
	comm, err := os.ReadFile(filepath.Join(haProcRoot, strconv.Itoa(pid), "comm"))
	if err != nil || strings.TrimSpace(string(comm)) != haServiceUnit {
		return 0
	}
	return pid
}

// haOpenRCCrashed reports OpenRC's "crashed" state: the service is recorded as
// started but its daemon is gone. start, stop and restart all fail in that
// state until "rc-service keepalived zap" resets it.
func haOpenRCCrashed() bool {
	// openrc-run prints the state to stdout or stderr depending on the release;
	// haRun appends stderr to the error.
	out, err := haRun("rc-service", haServiceUnit, "status")
	return err != nil && (strings.Contains(out, "crashed") || strings.Contains(err.Error(), "crashed"))
}

// haDaemonAlive reports a keepalived daemon the service manager may not see:
// on OpenRC the status command fails once keepalived.conf is gone, while the
// daemon keeps running and holding the virtual IP.
func haDaemonAlive(manager string) bool {
	return haServiceRunning(manager) || (manager == "openrc" && haOpenRCPID() != 0)
}

// haStopOpenRC stops keepalived through OpenRC, resetting a crashed state, and
// falls back to the recorded pidfile when the init script cannot run.
func haStopOpenRC() error {
	if haOpenRCCrashed() {
		if _, err := haRun("rc-service", haServiceUnit, "zap"); err != nil {
			return fmt.Errorf("keepalived zap 失败：%w", err)
		}
	}
	_, stopErr := haRun("rc-service", haServiceUnit, "stop")
	pid := haOpenRCPID()
	if pid == 0 {
		if stopErr != nil && haServiceRunning("openrc") {
			return fmt.Errorf("keepalived stop 失败：%w", stopErr)
		}
		return nil
	}
	if err := haKill(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("keepalived stop 失败：%v；按 pidfile 结束进程 %d 也失败：%w", stopErr, pid, err)
	}
	for attempt := 0; attempt < 20 && haOpenRCPID() != 0; attempt++ {
		haSleep(250 * time.Millisecond)
	}
	if haOpenRCPID() != 0 {
		return fmt.Errorf("keepalived 进程 %d 收到结束信号后仍在运行", pid)
	}
	// The daemon is gone but OpenRC still records it as started; its status
	// may be unreadable without keepalived.conf, so reset it unconditionally.
	if _, err := haRun("rc-service", haServiceUnit, "zap"); err != nil {
		return fmt.Errorf("keepalived 进程已结束，但重置 OpenRC 状态失败：%w", err)
	}
	return nil
}

// haStopService stops keepalived if it runs, including a daemon the service
// manager has lost track of.
func haStopService(manager string) error {
	if manager == "openrc" {
		if !haDaemonAlive(manager) && !haOpenRCCrashed() {
			return nil
		}
		return haStopOpenRC()
	}
	if !haServiceRunning(manager) {
		return nil
	}
	return haServiceAction(manager, "stop")
}

// haVerifyReleased confirms after a removal that keepalived no longer runs
// (unless it ran before DNS HA) and that the virtual IP left this host.
func haVerifyReleased(state *haState) error {
	var lastErr error
	for attempt := 0; attempt < 10; attempt++ {
		if attempt > 0 {
			haSleep(500 * time.Millisecond)
		}
		if !state.Service.WasRunning && haDaemonAlive(state.Service.Manager) {
			lastErr = fmt.Errorf(haMsgStillRunning, state.Spec.VIP)
			continue
		}
		addresses, err := readHAAddresses()
		if err != nil {
			return err
		}
		if haVIPPresent(state.Spec, addresses) {
			lastErr = fmt.Errorf(haMsgVIPStillPresent, state.Spec.VIP)
			continue
		}
		return nil
	}
	return lastErr
}

func haServiceRunning(manager string) bool {
	switch manager {
	case "supervisor":
		return haSupervisorState() == "RUNNING"
	case "systemd":
		_, err := haRun("systemctl", "is-active", "--quiet", haServiceUnit)
		return err == nil
	case "openrc":
		_, err := haRun("rc-service", haServiceUnit, "status")
		return err == nil
	}
	return false
}

func haServiceEnabled(manager string) bool {
	switch manager {
	case "supervisor":
		// The managed journal, rather than Supervisor autostart, drives recovery.
		return haSupervisorState() != ""
	case "systemd":
		_, err := haRun("systemctl", "is-enabled", "--quiet", haServiceUnit)
		return err == nil
	case "openrc":
		out, err := haRun("rc-update", "show", "default")
		if err != nil {
			return false
		}
		for _, line := range strings.Split(out, "\n") {
			name, _, _ := strings.Cut(line, "|")
			if strings.TrimSpace(name) == haServiceUnit {
				return true
			}
		}
	}
	return false
}

func haServiceAction(manager, action string) error {
	var err error
	switch manager {
	case "supervisor":
		if action == "enable" || action == "disable" {
			return nil
		}
		if action != "start" && action != "stop" && action != "restart" {
			return fmt.Errorf("不支持的 HA 服务操作")
		}
		_, err = haRun("supervisorctl", haSupervisorArgs(action)...)
	case "systemd":
		_, err = haRun("systemctl", action, haServiceUnit)
	case "openrc":
		switch action {
		case "enable":
			_, err = haRun("rc-update", "add", haServiceUnit, "default")
		case "disable":
			_, err = haRun("rc-update", "del", haServiceUnit, "default")
		case "stop":
			return haStopOpenRC()
		default:
			// A crashed service refuses to start until its state is reset.
			if haOpenRCCrashed() {
				if _, zapErr := haRun("rc-service", haServiceUnit, "zap"); zapErr != nil {
					return fmt.Errorf("keepalived zap 失败：%w", zapErr)
				}
			}
			_, err = haRun("rc-service", haServiceUnit, action)
		}
	default:
		return fmt.Errorf("不支持的服务管理方式：%s", manager)
	}
	if err != nil {
		return fmt.Errorf("keepalived %s 失败：%w", action, err)
	}
	return nil
}

func haWaitRunning(manager string) bool {
	for attempt := 0; attempt < 10; attempt++ {
		if haServiceRunning(manager) {
			return true
		}
		haSleep(500 * time.Millisecond)
	}
	return false
}

// haStartService enables keepalived and loads the new configuration.
func haStartService(manager string) error {
	if !haServiceEnabled(manager) {
		if err := haServiceAction(manager, "enable"); err != nil {
			return err
		}
	}
	action := "start"
	if haServiceRunning(manager) {
		action = "restart"
	}
	if err := haServiceAction(manager, action); err != nil {
		return err
	}
	if !haWaitRunning(manager) {
		return fmt.Errorf("keepalived 启动后未保持运行，请检查 keepalived 日志")
	}
	return nil
}

// haRestoreService returns keepalived to a recorded running/enabled state.
func haRestoreService(manager string, running, enabled bool) error {
	var errs []string
	if running {
		if err := haServiceAction(manager, "restart"); err != nil {
			errs = append(errs, err.Error())
		}
	} else if err := haStopService(manager); err != nil {
		errs = append(errs, err.Error())
	}
	if !enabled && haServiceEnabled(manager) {
		if err := haServiceAction(manager, "disable"); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "；"))
	}
	return nil
}

// haFileMode is the permission an existing file keeps when rewritten.
func haFileMode(path string) os.FileMode {
	if info, err := os.Stat(path); err == nil {
		return info.Mode().Perm()
	}
	return 0644
}

func haWriteFile(path, content string, exists bool) error {
	mode := os.FileMode(0644)
	if exists {
		mode = haFileMode(path)
	}
	if err := durableWrite(path, []byte(content), mode); err != nil {
		return err
	}
	readBack, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if haDigest(string(readBack)) != haDigest(content) {
		return fmt.Errorf("%s 写入后回读校验失败", path)
	}
	return nil
}

// haRestoreFile puts back a file as read before apply, with its original mode.
func haRestoreFile(path, content string, existed bool, mode os.FileMode) error {
	if existed {
		return durableWrite(path, []byte(content), mode)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func applyOxiDNSHA(cfg *Config, spec OxiDNSHASpec) error {
	if err := haPlatform(cfg); err != nil {
		return err
	}
	if err := validateOxiDNSHASpec(spec); err != nil {
		return err
	}
	haMu.Lock()
	defer haMu.Unlock()
	unlock, err := lockHAJournal(cfg)
	if err != nil {
		return err
	}
	defer unlock()
	previous, err := loadHAState(cfg)
	if err != nil {
		return err
	}
	blockers, iface, cidr := haPreflight(cfg, spec, previous)
	if len(blockers) > 0 {
		return fmt.Errorf("%s", strings.Join(blockers, "；"))
	}
	script, err := haScriptCommand(cfg)
	if err != nil {
		return err
	}
	manager := cfg.ServiceManager
	wasRunning, wasEnabled := haServiceRunning(manager), haServiceEnabled(manager)
	state := &haState{Version: 1, Spec: spec, Interface: iface, CIDR: cidr, Service: haService{Manager: manager, Unit: haServiceUnit, WasEnabled: wasEnabled, WasRunning: wasRunning}}
	if cfg.ServiceManager == "supervisor" {
		state.ContainerHostID = haEnvironment(cfg).HostID
	}
	if previous != nil {
		// The first apply recorded keepalived's original state; keep it for removal.
		state.Token, state.Service = previous.Token, previous.Service
	} else {
		var token [16]byte
		if _, err := rand.Read(token[:]); err != nil {
			return err
		}
		state.Token = hex.EncodeToString(token[:])
	}
	oldInclude, includeExisted, err := readOptional(haConfigPath())
	if err != nil {
		return err
	}
	oldMain, mainExisted, err := readOptional(haMainConfigPath())
	if err != nil {
		return err
	}
	includeMode, mainMode := haFileMode(haConfigPath()), haFileMode(haMainConfigPath())
	include := renderHAConfig(state, script)
	main, err := haUpsertSection(oldMain, mainExisted)
	if err != nil {
		return err
	}
	mainCreated := !mainExisted || (previous != nil && previous.Files[1].Created)
	state.Files = []haFile{{Path: haConfigPath(), SHA256: haDigest(include), Created: true}, {Path: haMainConfigPath(), SHA256: haDigest(main), Created: mainCreated}}
	if previous != nil && previous.Active && !previous.Pending && previous.Spec == spec && previous.Interface == iface && previous.CIDR == cidr &&
		includeExisted && oldInclude == include && mainExisted && oldMain == main && wasRunning && wasEnabled {
		// Already applied: restarting keepalived would only move the virtual IP.
		return nil
	}
	state.Pending = true
	if err := saveHAState(cfg, state); err != nil {
		return err
	}
	rollback := func(cause error) error {
		var errs []string
		// Stop first while keepalived.conf still exists: OpenRC's init script
		// refuses status and stop once required_files is gone.
		if !wasRunning {
			if err := haStopService(manager); err != nil {
				errs = append(errs, err.Error())
			}
		}
		if err := haRestoreFile(haConfigPath(), oldInclude, includeExisted, includeMode); err != nil {
			errs = append(errs, err.Error())
		}
		if err := haRestoreFile(haMainConfigPath(), oldMain, mainExisted, mainMode); err != nil {
			errs = append(errs, err.Error())
		}
		if err := haRestoreService(manager, wasRunning, wasEnabled); err != nil {
			errs = append(errs, err.Error())
		}
		if len(errs) > 0 {
			// Keep the pending journal: it still proves which files are ours.
			return fmt.Errorf("DNS 高可用应用失败：%v；恢复原配置时出错，已保留记录以便停用：%s", cause, strings.Join(errs, "；"))
		}
		var err error
		if previous != nil {
			err = saveHAState(cfg, previous)
		} else {
			err = deleteHAState(cfg)
		}
		if err != nil {
			return fmt.Errorf("DNS 高可用应用失败：%v；已恢复 keepalived 原配置，但更新记录失败：%v", cause, err)
		}
		return fmt.Errorf("DNS 高可用应用失败，已恢复 keepalived 原配置：%w", cause)
	}
	if err := haWriteFile(haConfigPath(), include, includeExisted); err != nil {
		return rollback(err)
	}
	if err := haWriteFile(haMainConfigPath(), main, mainExisted); err != nil {
		return rollback(err)
	}
	if err := haStartService(manager); err != nil {
		return rollback(err)
	}
	state.Active, state.Pending, state.AppliedAt = true, false, haNow()
	return saveHAState(cfg, state)
}

// removeOxiDNSHA deletes only files proven to be ours and restores keepalived
// to the state recorded before the first apply.
func removeOxiDNSHA(cfg *Config) (bool, error) {
	if err := haPlatform(cfg); err != nil {
		return false, err
	}
	haMu.Lock()
	defer haMu.Unlock()
	unlock, err := lockHAJournal(cfg)
	if err != nil {
		return false, err
	}
	defer unlock()
	state, err := loadHAState(cfg)
	if err != nil {
		return false, haOwnershipError{err}
	}
	if state == nil {
		return false, nil
	}
	include, includeExists, err := readOptional(haConfigPath())
	if err != nil {
		return false, err
	}
	if includeExists {
		if !haHeaderOwned(include, state.Token) {
			return false, haOwnershipError{fmt.Errorf("%s 的归属标记已变化，无法确认属于 DNS 高可用，已保留", haConfigPath())}
		}
		if haDigest(include) != state.Files[0].SHA256 && !state.Pending {
			return false, haOwnershipError{fmt.Errorf("%s 已被手动修改，无法确认可以删除，已保留；请恢复或手动删除后重试", haConfigPath())}
		}
	}
	main, mainExists, err := readOptional(haMainConfigPath())
	if err != nil {
		return false, err
	}
	clean, deleteMain := main, false
	if mainExists {
		var found bool
		clean, found, err = haStripSection(main)
		if err != nil {
			return false, haOwnershipError{err}
		}
		deleteMain = state.Files[1].Created && (haDigest(main) == state.Files[1].SHA256 || (found && strings.TrimSpace(clean) == ""))
	}
	state.Pending = true
	if err := saveHAState(cfg, state); err != nil {
		return false, err
	}
	// keepalived removed from the host has no service state left to restore.
	_, lookErr := haLookPath("keepalived")
	installed := lookErr == nil
	// Stop before touching the configuration: OpenRC's init script has
	// required_files=keepalived.conf, so once it is gone status and stop fail
	// and the daemon keeps holding the virtual IP.
	if installed && !state.Service.WasRunning {
		if err := haStopService(state.Service.Manager); err != nil {
			return false, fmt.Errorf(haMsgStopBeforeRemove, err)
		}
	}
	if mainExists {
		if deleteMain {
			err = os.Remove(haMainConfigPath())
		} else if clean != main {
			err = haWriteFile(haMainConfigPath(), clean, true)
		}
		if err != nil {
			return false, err
		}
	}
	if includeExists {
		if err := os.Remove(haConfigPath()); err != nil && !os.IsNotExist(err) {
			return false, err
		}
	}
	if installed {
		if err := haRestoreService(state.Service.Manager, state.Service.WasRunning, state.Service.WasEnabled); err != nil {
			return false, fmt.Errorf("keepalived 配置已移除，但恢复服务原状态失败，可重试停用：%w", err)
		}
	}
	if err := haVerifyReleased(state); err != nil {
		return false, err
	}
	if err := deleteHAState(cfg); err != nil {
		return false, err
	}
	return true, nil
}

// repairOxiDNSHA rewrites recorded files only when ownership is still provable,
// then makes sure keepalived is enabled and running.
func repairOxiDNSHA(cfg *Config, state *haState) error {
	script, err := haScriptCommand(cfg)
	if err != nil {
		return err
	}
	include, includeExists, err := readOptional(haConfigPath())
	if err != nil {
		return err
	}
	desired := renderHAConfig(state, script)
	if includeExists && include != desired && (!haHeaderOwned(include, state.Token) || haDigest(include) != state.Files[0].SHA256) {
		return fmt.Errorf("%s 已被手动修改，保留现状；请在 ConfigFlow 重新开启或停用 DNS 高可用", haConfigPath())
	}
	main, mainExists, err := readOptional(haMainConfigPath())
	if err != nil {
		return err
	}
	desiredMain, err := haUpsertSection(main, mainExists)
	if err != nil {
		return err
	}
	changed := include != desired || main != desiredMain || !includeExists || !mainExists
	if changed {
		state.Files[0].SHA256 = haDigest(desired)
		state.Files[1].SHA256 = haDigest(desiredMain)
		if !mainExists {
			state.Files[1].Created = true
		}
		if err := saveHAState(cfg, state); err != nil {
			return err
		}
		if err := haWriteFile(haConfigPath(), desired, includeExists); err != nil {
			return err
		}
		if err := haWriteFile(haMainConfigPath(), desiredMain, mainExists); err != nil {
			return err
		}
	}
	manager := state.Service.Manager
	if !haServiceEnabled(manager) {
		if err := haServiceAction(manager, "enable"); err != nil {
			return err
		}
	}
	running := haServiceRunning(manager)
	if running && !changed {
		return nil
	}
	action := "start"
	if running {
		action = "restart"
	}
	if err := haServiceAction(manager, action); err != nil {
		return haNotReady{err}
	}
	if !haWaitRunning(manager) {
		return haNotReady{fmt.Errorf("keepalived 未保持运行")}
	}
	return nil
}
