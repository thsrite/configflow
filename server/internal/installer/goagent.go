package installer

import (
	"configflow/internal/textx"
	"errors"
	"math"
	"math/big"
	"strings"
	"time"

	"encoding/json"
)

// Port of backend/agents/go_install_script.py, generate_go_agent_install_script:
// the canonical Shell installer for one Go Agent.

// Forwarding is the manage_ipv4_forwarding argument: False (the default),
// None (enable for Mihomo under systemd/OpenRC) or True (require it).
type Forwarding int

const (
	ForwardingOff  Forwarding = iota // False
	ForwardingAuto                   // None
	ForwardingOn                     // True
)

// AgentInstallOptions contains the native single-Agent renderer input.
// A nil DeploymentHealthTimeout selects 30 seconds.
type AgentInstallOptions struct {
	ServerURL               string
	AgentName               string
	ServiceType             string
	AgentPort               int
	AgentIP                 string
	ConfigPath              string
	RestartCommand          string
	BinaryDownloadURL       string
	ServiceManager          string
	ServiceUnit             string
	ServiceBinary           string
	StopCommand             string
	StartCommand            string
	StatusCommand           string
	HealthURL               string
	HealthDNSAddress        string
	HealthDNSName           string
	DeploymentHealthTimeout *big.Int
	InstallCore             bool
	ManageIPv4Forwarding    Forwarding
	PrepareDNSHA            bool
	RegistrationToken       string
	RegistrationKey         string
}

// standaloneForwardingFunctions is _standalone_forwarding_functions().
func standaloneForwardingFunctions() (string, error) {
	if !strings.HasPrefix(installGoAIOForwarding, aioForwardingHead) ||
		strings.Count(installGoAIOForwarding, aioForwardingRoot) != 6 ||
		strings.Count(installGoAIOForwarding, AIOForwardingHookComment) != 1 {
		return "", errors.New("aio-ipv4-forwarding.sh changed; review the single-service forwarding helpers")
	}
	text := "# Single-service Mihomo installer: the AIO forwarding helpers, rooted at $AGENT_DIR.\n" +
		strings.ReplaceAll(strings.ReplaceAll(installGoAIOForwarding[len(aioForwardingHead):], aioForwardingRoot, "$AGENT_DIR/"),
			AIOForwardingHookComment, StandaloneForwardingHookComment)
	if strings.Contains(text, "$AGENT_ROOT") {
		return "", errors.New("single-service forwarding helpers still reference the AIO root")
	}
	return text, nil
}

var serviceLabels = map[string]string{"mihomo": "Mihomo", "oxidns": "OxiDNS", "mosdns": "MosDNS"}

// RenderAgentInstall is generate_go_agent_install_script: an
// installer with explicit service lifecycle and deployment checks.
func RenderAgentInstall(o AgentInstallOptions) (string, error) {
	agentPort := o.AgentPort
	timeoutValue := o.DeploymentHealthTimeout
	if timeoutValue == nil {
		timeoutValue = big.NewInt(30)
	}
	serviceType := o.ServiceType
	if serviceType != "mihomo" && serviceType != "mosdns" && serviceType != "oxidns" {
		return "", invalidInput("Go Agent transactional deployment supports mihomo, mosdns and oxidns")
	}
	if o.PrepareDNSHA && serviceType != "oxidns" {
		return "", invalidInput(msgHARequiresOxidns)
	}
	if !matchesClass(o.AgentName, "_-") {
		return "", invalidInput("Invalid agent name: use letters, numbers, hyphens or underscores")
	}
	port := big.NewInt(int64(agentPort))
	if !inRange(port, 1, 65535) {
		return "", invalidInput("Invalid Agent port")
	}
	unit := o.ServiceUnit
	if unit == "" {
		unit = serviceType
	}
	if !matchesClass(unit, "_.@-") {
		return "", invalidInput("Invalid service unit")
	}
	unit = strings.TrimSuffix(unit, ".service")
	serviceManager := o.ServiceManager
	if serviceManager != "" && serviceManager != "systemd" && serviceManager != "openrc" && serviceManager != "command" {
		return "", invalidInput("Shell installs require systemd, openrc or explicit command lifecycle")
	}
	standardRestart := o.RestartCommand == "" || o.RestartCommand == "systemctl restart "+unit ||
		o.RestartCommand == "systemctl restart "+unit+".service" || o.RestartCommand == "rc-service "+unit+" restart"
	if o.InstallCore {
		if serviceType != "mihomo" && serviceType != "oxidns" {
			return "", invalidInput(msgInstallCoreService)
		}
		if (o.ConfigPath != "" && o.ConfigPath != "/etc/"+serviceType+"/config.yaml") ||
			unit != serviceType || serviceManager != "" || !standardRestart ||
			(o.ServiceBinary != "" && o.ServiceBinary != serviceType && o.ServiceBinary != "/usr/local/bin/"+serviceType) ||
			o.StopCommand != "" || o.StartCommand != "" || o.StatusCommand != "" {
			return "", invalidInput(msgInstallCoreDefaults)
		}
		dnsPort := int64(53)
		if serviceType == "mihomo" {
			dnsPort = 1053
		}
		if port.Cmp(big.NewInt(dnsPort)) == 0 {
			return "", invalidInput(msgCoreDNSPortConflict)
		}
	}
	if serviceManager == "" {
		if standardRestart {
			serviceManager = "__CONFIGFLOW_INIT__"
		} else {
			serviceManager = "command"
		}
	}
	if o.PrepareDNSHA && serviceManager == "command" {
		return "", invalidInput(msgHANativeLifecycle)
	}
	if serviceManager == "command" && (o.StopCommand == "" || o.StartCommand == "" || o.StatusCommand == "") {
		return "", invalidInput("Custom lifecycle requires stop_command, start_command and status_command; a restart command alone cannot support rollback")
	}
	forwardingSupported := serviceType == "mihomo" && serviceManager != "command"
	manageForwarding := o.ManageIPv4Forwarding == ForwardingOn
	if o.ManageIPv4Forwarding == ForwardingAuto {
		manageForwarding = forwardingSupported
	} else if manageForwarding && !forwardingSupported {
		return "", invalidInput(StandaloneForwardingUnsupported)
	}
	if !timeoutValue.IsInt64() || timeoutValue.Cmp(big.NewInt(math.MaxInt64/int64(time.Second))) > 0 || timeoutValue.Cmp(big.NewInt(math.MinInt64/int64(time.Second))) < 0 {
		return "", invalidInput("Invalid deployment health timeout")
	}
	configPath := o.ConfigPath
	if configPath == "" {
		configPath = "/etc/" + serviceType + "/config.yaml"
	}
	restart := o.RestartCommand
	if standardRestart {
		restart = "__CONFIGFLOW_RESTART__"
	}
	serviceBinary := o.ServiceBinary
	if serviceBinary == "" {
		serviceBinary = serviceType
	}
	cfg := singleAgentConfig{
		ServerURL: o.ServerURL, AgentName: o.AgentName, AgentHost: "0.0.0.0",
		AgentPort: json.Number(port.String()), AgentIP: o.AgentIP, ServiceType: serviceType,
		DeploymentMethod: "shell", ServiceName: serviceType, ConfigPath: configPath,
		RestartCommand: restart, ServiceManager: serviceManager, ServiceUnit: unit,
		ServiceBinary: serviceBinary, StopCommand: o.StopCommand, StartCommand: o.StartCommand,
		StatusCommand: o.StatusCommand, HealthURL: o.HealthURL, HealthDNSAddress: o.HealthDNSAddress,
		HealthDNSName: o.HealthDNSName, DeploymentHealthTimeout: json.Number(timeoutValue.String()),
		HeartbeatInterval: 30, AgentID: "__CONFIGFLOW_AGENT_ID__", Token: "__CONFIGFLOW_TOKEN__",
		RegistrationToken: o.RegistrationToken, RegistrationKey: o.RegistrationKey,
	}
	configJSON, err := renderConfigJSON(cfg)
	if err != nil {
		return "", err
	}
	portText := port.String()
	coreDependencies := ""
	if serviceType == "oxidns" {
		coreDependencies = " zoneinfo"
	}
	replacements := map[string]string{
		"agent_install_rollback":     agentInstallRollback,
		"service_type":               serviceType,
		"service_unit":               unit,
		"agent_name":                 o.AgentName,
		"agent_port":                 portText,
		"service_label":              serviceLabels[serviceType],
		"config_json":                string(configJSON),
		"binary_download_url_shell":  shlexQuote(strings.TrimRight(o.BinaryDownloadURL, "/")),
		"init_detection":             agentInitDetection,
		"agent_install_dependencies": AgentInstallDependencies,
		"core_dependencies":          coreDependencies,
		"installation_action":        "安装或升级 Go Agent（包含事务发布与失败回滚）",
		"core_install_phase":         "",
		"install_arguments":          "for cf_argument do\n    case \"$cf_argument\" in --verbose|-v) ;; *) cf_error \"未知参数：$cf_argument\"; exit 2 ;; esac\ndone",
		"installation_choice":        "cf_ui \"1) {installation_action}\"\ncf_ui \"2) 卸载 Agent\"\ncf_ui \"3) 取消\"\ncf_prompt '请输入选项 [1-3]: '\nread -r choice < /dev/tty",
		"dns_ha_functions":           "",
		"dns_ha_prepare":             "",
		"dns_ha_existing_config":     "",
		"agent_summary_port":         portText,
		"core_phase_state":           "",
		"core_cleanup_message":       "",
	}
	forwardingFunctions := ""
	if manageForwarding {
		helpers, err := standaloneForwardingFunctions()
		if err != nil {
			return "", err
		}
		forwardingFunctions = "fail() { echo \"$*\" >&2; exit 1; }\nSERVICE=mihomo\nSTAGE_DIR=\"\"\n" + helpers + "IPV4_FORWARDING_READY=1\n"
	}
	forwarding := map[string]string{
		"ipv4_forwarding_notice": "cf_ui \"Mihomo 单独安装将开启并持久化本机 IPv4 转发（作为主路由的代理网关时需要）；" +
			"不需要时请在面板勾选「不开启 IPv4 转发」后重新生成命令。\"\n",
		"ipv4_forwarding_dependencies":    " sysctl cmp ln",
		"ipv4_forwarding_environment":     standaloneGatewayRecordRefusal + InstallerForwardingEnvironment,
		"ipv4_forwarding_environment_end": InstallerForwardingEnvironmentEnd,
		"ipv4_forwarding_state":           "IPV4_FORWARDING_READY=0\n",
		"ipv4_forwarding_cleanup":         standaloneForwardingCleanup,
		"ipv4_forwarding_functions":       forwardingFunctions,
		"ipv4_forwarding_prepare":         standaloneForwardingPrepare,
		"ipv4_forwarding_hook":            forwardingHookJournal + "install_ipv4_forwarding_hook\n",
		"ipv4_systemd_pre":                "ExecStartPre=/bin/sh " + StandaloneForwardingHook + "\n",
		"ipv4_openrc_pre":                 "    /bin/sh " + StandaloneForwardingHook + " || return 1\n",
		"ipv4_forwarding_apply":           "persist_ipv4_forwarding\n",
		"ipv4_forwarding_summary":         "summarize_ipv4_forwarding\n",
		"ipv4_forwarding_uninstall": "        [ ! -f /etc/sysctl.d/99-configflow-aio-forward.conf ] || " +
			"cf_ui \"IPv4 转发及其 sysctl.d 持久化设置保留，避免影响仍在运行的 Mihomo。\"\n",
	}
	for key, value := range forwarding {
		if !manageForwarding {
			value = ""
		}
		replacements[key] = value
	}
	if o.InstallCore {
		coreName := "OxiDNS"
		if serviceType == "mihomo" {
			coreName = "Mihomo"
		}
		replacements["installation_action"] = "安装 " + coreName + " 核心及 Agent"
		core, err := GenerateCoreInstallScript(manageForwarding)
		if err != nil {
			return "", err
		}
		st := serviceType
		replacements["core_install_phase"] = "echo \"先安装或保留 " + st + " 核心，成功后继续安装 Agent。\"\n" +
			"(\n" +
			"set -- --core " + st + "\n" +
			"CF_OUTPUT_OWNER=false\n" +
			trimScriptTail(core) + "\n)\n" +
			"CORE_PHASE_COMPLETED=true\n" +
			"CORE_BINARY=" + shlexQuote(serviceBinary) + "\n" +
			"if [ ! -f /etc/" + st + "/config.yaml ] || [ ! -r /etc/" + st + "/config.yaml ] || ! command -v \"$CORE_BINARY\" >/dev/null 2>&1; then\n" +
			"    echo \"" + st + " 核心缺少默认配置或可执行文件。请补齐 /etc/" + st + "/config.yaml 和核心程序；已有自定义部署请取消同时安装核心，并设置实际路径。\" >&2\n" +
			"    exit 1\n" +
			"fi\n" +
			"if [ \"$INIT_SYSTEM\" = systemd ]; then\n" +
			"    core_unit_state=$(systemctl show " + st + ".service --property=LoadState --value)\n" +
			"    if [ \"$core_unit_state\" != loaded ]; then\n" +
			"        echo \"" + st + " 默认服务未正确加载，请先修复 " + st + ".service；已有自定义服务请取消同时安装核心并填写实际服务名。\" >&2\n" +
			"        exit 1\n" +
			"    fi\n" +
			"elif [ ! -x /etc/init.d/" + st + " ]; then\n" +
			"    echo \"缺少可执行的 /etc/init.d/" + st + " 服务，请先补齐；已有自定义服务请取消同时安装核心并填写实际服务名。\" >&2\n" +
			"    exit 1\n" +
			"fi\n"
		replacements["init_detection"] = coreInitDetection
		replacements["core_phase_state"] = "CORE_PHASE_COMPLETED=false\n"
		replacements["core_cleanup_message"] = "        if [ \"$CORE_PHASE_COMPLETED\" = true ]; then\n" +
			"            echo \"Agent 安装未完成；" + st + " 核心安装阶段已结束，已安装或原有的核心予以保留。修复上方 Agent 错误后可重新运行。\" >&2\n" +
			"        fi\n"
	}
	replacements["installation_choice"] = strings.ReplaceAll(replacements["installation_choice"], "{installation_action}", replacements["installation_action"])
	if o.PrepareDNSHA {
		replacements["init_detection"] = coreInitDetection
		replacements["install_arguments"] = `choice=1
for cf_argument do
    case "$cf_argument" in
        --verbose|-v) ;;
        --uninstall) choice=2 ;;
        --cancel) choice=3 ;;
        *) cf_error "未知参数：$cf_argument"; exit 2 ;;
    esac
done`
		replacements["installation_choice"] = `cf_ui "OxiDNS 高可用：自动准备环境并安装；VIP 在面板配对时设置。"`
		replacements["dns_ha_functions"] = strings.ReplaceAll(dnsHAPreparation, "{agent_port}", portText) + "\n"
		replacements["dns_ha_prepare"] = "agent_prepare_dns_ha\n"
		replacements["dns_ha_existing_config"] = `if [ -n "$DNS_HA_EXISTING_CONFIG" ]; then
    cp "$DNS_HA_EXISTING_CONFIG" "$CONFIG_FILE.new"
    chmod 600 "$CONFIG_FILE.new"
fi
`
		replacements["agent_summary_port"] = "$DNS_HA_EFFECTIVE_PORT"
		if o.InstallCore {
			replacements["core_install_phase"] = "if [ -z \"$DNS_HA_EXISTING_CONFIG\" ]; then\n" + replacements["core_install_phase"] + "fi\n"
		}
	}
	if o.InstallCore || o.PrepareDNSHA {
		replacements["install_arguments"] = "PATH=\"${PATH:-/usr/sbin:/usr/bin:/sbin:/bin}:/usr/local/sbin:/usr/local/bin\"\n" +
			"export PATH\n" + replacements["install_arguments"]
	}
	text := substitute(installGoScriptTemplate, replacements)
	if o.InstallCore {
		text = replaceOnce(text, "#!/bin/sh\n", "#!/bin/sh\n"+AgentCoreInstallInterface+"\n")
	}
	rendered := withCompensatingInstallOutput(text)
	if o.PrepareDNSHA {
		rendered = WithRootPrivileges(rendered)
		lines := textx.SplitLines(rendered, true)
		head := lines
		if len(head) > 2 {
			head = lines[:2]
		}
		rendered = strings.Join(head, "") + DNSHAPreparationInterface + "\n" + strings.Join(lines[len(head):], "")
	}
	return rendered, nil
}
