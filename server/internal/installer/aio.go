package installer

import (
	"encoding/json"
	"path"
	"strconv"
	"strings"

	"configflow/internal/weburl"
)

// Port of backend/agents/go_install_script.py, generate_go_aio_install_script:
// two isolated native Agents (Mihomo + OxiDNS), optionally installing cores.

// AIOInstallOptions contains the native two-Agent renderer input.
// DefaultAIOOptions supplies the standard ports, paths and lifecycle settings.
type AIOInstallOptions struct {
	RegistrationTokens      map[string]string
	RegistrationKey         string
	ServerURL               string
	AgentName               string
	AgentIP                 string
	BinaryDownloadURL       string
	MihomoAgentPort         int
	OxidnsAgentPort         int
	MihomoConfigPath        string
	OxidnsConfigPath        string
	MihomoServiceUnit       string
	OxidnsServiceUnit       string
	MihomoServiceBinary     string
	OxidnsServiceBinary     string
	InstallCore             bool
	NonInteractive          bool
	VerifiedAgentPath       string
	InstallationID          string
	InstallationToken       string
	ManageIPv4Forwarding    bool
	ManageTun               bool
	InstallationDescription string
	TunPrepareAssumeEnabled bool
}

// DefaultAIODescription is the default installation_description.
const DefaultAIODescription = "此安装器只接管已安装的 Mihomo 和 OxiDNS，不安装核心、不改核心配置、不重启核心。"

// DefaultAIOOptions returns the standard settings for the given server and name.
func DefaultAIOOptions(serverURL, agentName string) AIOInstallOptions {
	return AIOInstallOptions{
		ServerURL: serverURL, AgentName: agentName,
		MihomoAgentPort: 8080, OxidnsAgentPort: 8082,
		MihomoConfigPath: "/etc/mihomo/config.yaml", OxidnsConfigPath: "/etc/oxidns/config.yaml",
		MihomoServiceUnit: "mihomo", OxidnsServiceUnit: "oxidns",
		MihomoServiceBinary: "mihomo", OxidnsServiceBinary: "oxidns",
		ManageIPv4Forwarding: true, ManageTun: true,
		InstallationDescription: DefaultAIODescription,
	}
}

// isInstallationID is re.fullmatch(r"[A-Za-z0-9_-]{16,80}", value).
func isInstallationID(value string) bool {
	return len(value) >= 16 && len(value) <= 80 && matchesClass(value, "_-")
}

// IsSessionToken is re.fullmatch(r"[A-Za-z0-9_.-]{16,512}", value).
func IsSessionToken(value string) bool {
	return len(value) >= 16 && len(value) <= 512 && matchesClass(value, "_.-")
}

// pathParts is PurePosixPath(value).parts without the root: "" and "."
// components are dropped, ".." is kept.
func pathParts(value string) []string {
	var parts []string
	for _, part := range strings.Split(value, "/") {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	return parts
}

// purePosixStr is str(PurePosixPath(value)) for an absolute value: POSIX keeps
// exactly two leading slashes, otherwise one.
func purePosixStr(value string) string {
	root := ""
	if strings.HasPrefix(value, "/") {
		root = "/"
		if strings.HasPrefix(value, "//") && !strings.HasPrefix(value, "///") {
			root = "//"
		}
	}
	joined := strings.Join(pathParts(value), "/")
	if root == "" && joined == "" {
		return "."
	}
	return root + joined
}

// insideAgentDirectory collapses Linux absolute-path aliases before
// comparing components with /opt/configflow-agent.
func insideAgentDirectory(value string) bool {
	normalized := path.Clean("/" + strings.TrimLeft(value, "/"))
	return normalized == "/opt/configflow-agent" || strings.HasPrefix(normalized, "/opt/configflow-agent/")
}

// RenderAIOInstall is generate_go_aio_install_script.
func RenderAIOInstall(o AIOInstallOptions) (string, error) {
	if o.TunPrepareAssumeEnabled && !(o.ManageTun && o.VerifiedAgentPath != "") {
		return "", invalidInput("assume-tun-enabled is only supported inside the gateway installer")
	}
	if o.VerifiedAgentPath != "" && o.VerifiedAgentPath != "$GATEWAY_STAGE/configflow-agent" {
		return "", invalidInput("Preverified Agent binary is only supported inside the gateway installer")
	}
	if o.VerifiedAgentPath != "" && !o.NonInteractive {
		return "", invalidInput("Preverified Agent requires non-interactive installation")
	}
	if (o.InstallationID != "") != (o.InstallationToken != "") {
		return "", invalidInput("Installation id and token must be supplied together")
	}
	if o.InstallationID != "" && !isInstallationID(o.InstallationID) {
		return "", invalidInput("Invalid installation id")
	}
	if o.InstallationToken != "" && !IsSessionToken(o.InstallationToken) {
		return "", invalidInput("Invalid installation token")
	}
	if !matchesClass(o.AgentName, "_-") {
		return "", invalidInput("Invalid agent name: use letters, numbers, hyphens or underscores")
	}
	var ports []string
	for _, port := range []int{o.MihomoAgentPort, o.OxidnsAgentPort} {
		if port < 1 || port > 65535 {
			return "", invalidInput("Invalid Agent port")
		}
		ports = append(ports, strconv.Itoa(port))
	}
	if ports[0] == ports[1] {
		return "", invalidInput("Mihomo and OxiDNS Agent ports must be different")
	}
	if o.InstallCore {
		for _, port := range ports {
			if port == "53" || port == "1053" {
				return "", invalidInput(msgAIOCoreDNSPorts)
			}
		}
	}
	var paths []string
	for _, value := range []string{o.MihomoConfigPath, o.OxidnsConfigPath} {
		bad := !strings.HasPrefix(value, "/") || weburl.HasControl(value) || purePosixStr(value) == "/" || strings.HasSuffix(value, "/")
		for _, part := range pathParts(value) {
			if part == ".." {
				bad = true
			}
		}
		if bad {
			return "", invalidInput("Core configuration paths must be absolute file paths without parent traversal")
		}
		if insideAgentDirectory(value) {
			return "", invalidInput("Core configuration paths must be outside the Agent directory")
		}
		paths = append(paths, purePosixStr("/"+strings.TrimLeft(value, "/")))
	}
	if paths[0] == paths[1] {
		return "", invalidInput("Mihomo and OxiDNS configuration paths must be different")
	}
	if path.Dir(paths[0]) == path.Dir(paths[1]) {
		return "", invalidInput("Core configurations require separate directories for deployment journals")
	}
	replacements := map[string]string{
		"binary_download_url_shell": shlexQuote(strings.TrimRight(o.BinaryDownloadURL, "/")),
		"mihomo_agent_port":         ports[0],
		"oxidns_agent_port":         ports[1],
	}
	tunFlags := "-prepare-tun"
	if o.TunPrepareAssumeEnabled {
		tunFlags = "-prepare-tun -assume-tun-enabled"
	}
	pick := func(on bool, value string) string {
		if on {
			return value
		}
		return ""
	}
	tun, fwd := o.ManageTun, o.ManageIPv4Forwarding
	for key, value := range map[string]string{
		"tun_prepare_notice": pick(tun, `cf_ui "Mihomo 已启用 TUN 时，自动检查运行环境；本机加载 TUN 驱动并设置开机加载，容器缺少设备时给出宿主机处理提示。"`),
		"tun_prepare": pick(tun, `cf_step "准备 TUN 驱动与开机加载"
if "$BINARY_TMP" -config "$STAGE_DIR/mihomo-config.json" `+tunFlags+`; then
    :
else
    tun_status=$?
    echo "TUN 准备未完成。若提示未知 -prepare-tun 参数，请使用包含 TUN 自动准备功能的新 Agent 构建；否则按上方原因处理后重跑。现有 Agent 尚未停止。" >&2
    exit "$tun_status"
fi
cf_step_done "TUN 准备完成（具体处理结果见日志）"`),
		"ipv4_forwarding_functions":       pick(fwd, installGoAIOForwarding),
		"ipv4_forwarding_dependencies":    pick(fwd, " sysctl cmp ln"),
		"ipv4_forwarding_environment":     pick(fwd, InstallerForwardingEnvironment),
		"ipv4_forwarding_environment_end": pick(fwd, InstallerForwardingEnvironmentEnd),
		"ipv4_forwarding_notice":          pick(fwd, `cf_ui "普通 Shell 同机安装将自动开启并持久化当前 Linux 的 IPv4 转发。"`),
		"ipv4_forwarding_prepare":         pick(fwd, "prepare_ipv4_forwarding"),
		"ipv4_forwarding_ownership":       pick(fwd, "check_ipv4_forwarding_ownership"),
		"ipv4_forwarding_hook":            pick(fwd, forwardingHookJournal+"    install_ipv4_forwarding_hook"),
		"ipv4_forwarding_apply":           pick(fwd, "persist_ipv4_forwarding"),
		"ipv4_forwarding_cleanup":         pick(fwd, `cleanup_ipv4_forwarding "$cleanup_status"`),
		"ipv4_forwarding_summary":         pick(fwd, "summarize_ipv4_forwarding"),
		"ipv4_forwarding_uninstall":       pick(fwd, `cf_ui "IPv4 转发及其 sysctl.d 持久化设置保留，避免影响仍在运行的核心。"`),
	} {
		replacements[key] = value
	}
	services := []struct{ name, port, path, unit, binary string }{
		{"mihomo", ports[0], paths[0], o.MihomoServiceUnit, o.MihomoServiceBinary},
		{"oxidns", ports[1], paths[1], o.OxidnsServiceUnit, o.OxidnsServiceBinary},
	}
	for _, s := range services {
		if !isUnitName(s.unit) {
			return "", invalidInput("Invalid service unit")
		}
		unit := strings.TrimSuffix(s.unit, ".service")
		if unit == "" || strings.HasPrefix(unit, "configflow-") {
			return "", invalidInput("Core service unit must not be a ConfigFlow Agent unit")
		}
		if s.binary == "" || weburl.HasControl(s.binary) {
			return "", invalidInput("Invalid core service binary")
		}
		if strings.HasPrefix(s.binary, "/") && insideAgentDirectory(s.binary) {
			return "", invalidInput("Core service binaries must be outside the Agent directory")
		}
		if o.InstallCore && (s.path != "/etc/"+s.name+"/config.yaml" || unit != s.name ||
			(s.binary != s.name && s.binary != "/usr/local/bin/"+s.name)) {
			return "", invalidInput(msgAIOCoreDefaults)
		}
		cfg := aioAgentConfig{
			ServerURL: o.ServerURL, AgentName: o.AgentName + "-" + s.name,
			AgentHost: "0.0.0.0", AgentPort: json.Number(s.port), AgentIP: o.AgentIP,
			ServiceType: s.name, ServiceName: s.name, DeploymentMethod: "shell",
			NativeAIO: true, AgentServiceUnit: "configflow-agent-" + s.name,
			ConfigPath: s.path, ServiceManager: "__CONFIGFLOW_INIT__", ServiceUnit: unit,
			ServiceBinary: s.binary, RestartCommand: "__CONFIGFLOW_RESTART__",
			HeartbeatInterval: 30, DeploymentHealthTimeout: 30,
			AgentID: "__CONFIGFLOW_AGENT_ID__", Token: "__CONFIGFLOW_TOKEN__",
			RegistrationToken: o.RegistrationTokens[s.name], RegistrationKey: o.RegistrationKey,
			InstallationID: o.InstallationID, InstallationToken: o.InstallationToken,
		}
		configJSON, err := renderConfigJSON(cfg)
		if err != nil {
			return "", err
		}
		replacements[s.name+"_config_json"] = string(configJSON)
		replacements[s.name+"_config_path_shell"] = shlexQuote(s.path)
		replacements[s.name+"_service_unit_shell"] = shlexQuote(unit)
		replacements[s.name+"_service_binary_shell"] = shlexQuote(s.binary)
	}
	if strings.TrimSuffix(o.MihomoServiceUnit, ".service") == strings.TrimSuffix(o.OxidnsServiceUnit, ".service") {
		return "", invalidInput("Mihomo and OxiDNS service units must be different")
	}
	archDetection := []string{
		`case "$(uname -m)" in`,
		`    x86_64|amd64) AGENT_ARCH=linux-amd64 ;;`,
		`    aarch64|arm64) AGENT_ARCH=linux-arm64 ;;`,
		`    armv7l|armhf) AGENT_ARCH=linux-armv7 ;;`,
		`    *) fail "不支持的 CPU 架构" ;;`,
		`esac`,
	}
	for i, line := range archDetection {
		archDetection[i] = "        " + line
	}
	replacements["init_detection"] = `if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
    INIT_SYSTEM=systemd
elif command -v rc-service >/dev/null 2>&1 && command -v rc-update >/dev/null 2>&1; then
    INIT_SYSTEM=openrc
elif command -v systemctl >/dev/null 2>&1; then
    INIT_SYSTEM=systemd
else
    fail "需要 systemd 或 OpenRC 管理已安装的 Mihomo 和 OxiDNS。"
fi`
	replacements["installation_description"] = "cf_ui " + shlexQuote(o.InstallationDescription)
	replacements["installation_action"] = "安装或升级两个独立 Agent"
	replacements["agent_arch_detection"] = strings.Join(archDetection, "\n")
	replacements["agent_install_dependencies"] = AgentInstallDependencies
	replacements["agent_install_rollback"] = agentInstallRollback
	replacements["core_phase_state"] = ""
	replacements["core_cleanup_message"] = ""
	replacements["core_install_phase"] = ""
	if o.InstallCore {
		core, err := GenerateCoreInstallScript(o.ManageIPv4Forwarding)
		if err != nil {
			return "", err
		}
		replacements["init_detection"] = coreInitDetection
		replacements["installation_description"] = `cf_ui "安装 Mihomo 与 OxiDNS，并自动安装管理 Agent；已有核心与配置保留。"`
		replacements["installation_action"] = "安装 Mihomo 与 OxiDNS 以及对应 Agent"
		replacements["core_phase_state"] = "CORE_PHASE_COMPLETED=false\n"
		replacements["core_cleanup_message"] = `        if [ "$CORE_PHASE_COMPLETED" = true ]; then
            echo "Agent 安装未完成；两个核心的安装阶段已结束，已安装或原有的核心予以保留。修复上方 Agent 错误后可重新运行。" >&2
        fi
`
		replacements["core_install_phase"] = "echo \"先安装或保留两个核心，成功后继续安装两个 Agent。\"\n" +
			"(\nset -- --core both\nCF_OUTPUT_OWNER=false\n" +
			trimScriptTail(core) + "\n)\n" +
			"CORE_PHASE_COMPLETED=true\n" +
			"# Minimal systems may not yet have /opt, needed for physical path checks.\nmkdir -p \"$(dirname \"$AGENT_ROOT\")\"\n"
	}
	template := installGoAIOScriptTemplate
	if o.NonInteractive {
		template = strings.ReplaceAll(template, "read -r choice < /dev/tty", "choice=1 # Gateway setup already selected install in the web wizard")
	}
	if o.VerifiedAgentPath != "" {
		start := strings.Index(template, "if command -v wget >/dev/null 2>&1; then\n    wget -q -O \"$BINARY_TMP\"")
		end := -1
		if start >= 0 {
			if i := strings.Index(template[start:], "\nchmod 755 \"$BINARY_TMP\""); i >= 0 {
				end = start + i
			}
		}
		if start < 0 || end < 0 {
			// str.index raises ValueError("substring not found").
			return "", invalidInput("substring not found")
		}
		template = template[:start] + `cp "$GATEWAY_STAGE/configflow-agent" "$BINARY_TMP"` + template[end:]
	}
	text := substitute(template, replacements)
	if o.InstallCore {
		text = replaceOnce(text, "#!/bin/sh\n", "#!/bin/sh\n"+AgentCoreInstallInterface+"\n")
	}
	return withCompensatingInstallOutput(text), nil
}
