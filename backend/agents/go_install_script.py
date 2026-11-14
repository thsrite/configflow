"""Generate the canonical Shell installer for the Go Agent."""
import json
import posixpath
import re
import shlex
from pathlib import Path, PurePosixPath
from typing import Optional
from backend.agents.install_output import with_install_output, with_root_privileges


INSTALL_GO_SCRIPT_TEMPLATE = (Path(__file__).parent / "scripts" / "install-go.sh").read_text(encoding="utf-8")
INSTALL_GO_AIO_SCRIPT_TEMPLATE = (Path(__file__).parent / "scripts" / "install-go-aio.sh").read_text(encoding="utf-8")
INSTALL_GO_AIO_FORWARDING = (Path(__file__).parent / "scripts" / "aio-ipv4-forwarding.sh").read_text(encoding="utf-8")
AGENT_INSTALL_DEPENDENCIES = (Path(__file__).parent / "scripts" / "agent-install-dependencies.sh").read_text(encoding="utf-8")
AGENT_CORE_INSTALL_INTERFACE = "# ConfigFlow Agent + core installer interface: 1"
DNS_HA_PREPARATION_INTERFACE = "# ConfigFlow DNS HA preparation interface: 1"
DNS_HA_PREPARATION = (Path(__file__).parent / "scripts" / "prepare-dns-ha.sh").read_text(encoding="utf-8")
# systemd as the running init wins: OpenRC tools can be installed beside it.
_AGENT_INIT_DETECTION = '''if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
    INIT_SYSTEM="systemd"
elif command -v rc-service >/dev/null 2>&1 && command -v rc-update >/dev/null 2>&1; then
    INIT_SYSTEM="openrc"
elif command -v systemctl >/dev/null 2>&1; then
    INIT_SYSTEM="systemd"
else
    echo "需要 systemd 或 OpenRC；自定义运行环境请显式配置停止、启动、状态命令。"
    exit 1
fi'''
_CORE_INIT_DETECTION = '''if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
    INIT_SYSTEM="systemd"
elif [ -x /sbin/openrc-run ] && command -v rc-service >/dev/null 2>&1 && command -v rc-update >/dev/null 2>&1; then
    INIT_SYSTEM="openrc"
else
    echo "需要正在使用的 systemd 或 OpenRC；容器请先配置 init 服务管理器。" >&2
    exit 1
fi'''
# The single-service Mihomo installer reuses the AIO forwarding helpers, rooted
# at its own Agent directory; the config file and marker stay identical, so the
# Agent recognises (and the gateway removal preserves) either layout. Only the
# hook's comment names the one Agent it waits for; the Go Agent writes and
# recognises the same per-layout text (aioStandaloneHookComment).
_AIO_FORWARDING_HEAD = "# Included only in the ordinary Shell cohost installer, never the gateway wizard.\n"
_AIO_FORWARDING_ROOT = "$AGENT_ROOT/mihomo/"
AIO_FORWARDING_HOOK_COMMENT = "# The installer commits this file only after both Agents start successfully.\n"
STANDALONE_FORWARDING_HOOK_COMMENT = "# The installer commits this file only after the Agent starts successfully.\n"
STANDALONE_FORWARDING_HOOK = "/opt/configflow-agent/enable-ipv4-forwarding.sh"
STANDALONE_FORWARDING_UNSUPPORTED = "仅使用 systemd 或 OpenRC 的 Mihomo 单独安装可开启并持久化 IPv4 转发"
# Exported for the environment check only and unset right after it, so no
# service inherits it: the installer enables and persists forwarding itself, so
# the check reports that instead of asking for a manual sysctl repair. Older
# Agents ignore it. Plain shell keeps it free of an extra tool such as env.
INSTALLER_FORWARDING_ENVIRONMENT = ("CONFIGFLOW_INSTALLER_FORWARDING=persist\n"
                                    "export CONFIGFLOW_INSTALLER_FORWARDING\n")
INSTALLER_FORWARDING_ENVIRONMENT_END = "unset CONFIGFLOW_INSTALLER_FORWARDING\n"


def _standalone_forwarding_functions() -> str:
    if (not INSTALL_GO_AIO_FORWARDING.startswith(_AIO_FORWARDING_HEAD)
            or INSTALL_GO_AIO_FORWARDING.count(_AIO_FORWARDING_ROOT) != 6
            or INSTALL_GO_AIO_FORWARDING.count(AIO_FORWARDING_HOOK_COMMENT) != 1):
        raise RuntimeError("aio-ipv4-forwarding.sh changed; review the single-service forwarding helpers")
    text = ("# Single-service Mihomo installer: the AIO forwarding helpers, rooted at $AGENT_DIR.\n"
            + INSTALL_GO_AIO_FORWARDING[len(_AIO_FORWARDING_HEAD):].replace(_AIO_FORWARDING_ROOT, "$AGENT_DIR/")
            .replace(AIO_FORWARDING_HOOK_COMMENT, STANDALONE_FORWARDING_HOOK_COMMENT))
    if "$AGENT_ROOT" in text:
        raise RuntimeError("single-service forwarding helpers still reference the AIO root")
    return text


# Refused before the environment check, which would otherwise first describe
# the forwarding this installer is about to decline to manage.
_STANDALONE_GATEWAY_RECORD_REFUSAL = '''if [ -e "$AGENT_DIR/linux-gateway.json" ] || [ -L "$AGENT_DIR/linux-gateway.json" ]; then
    fail "检测到 Linux 网关记录，单独安装不会接管网关管理的 IPv4 转发；本次未改动 Agent。请在面板勾选「不开启 IPv4 转发」后重新生成安装命令；如这台设备是路由器的代理网关，可在面板 Agent 页这台设备的卡片上开启并持久化 IPv4 转发。"
fi
'''
# Runs before the previous Agent is stopped: a failure leaves the host unchanged.
_STANDALONE_FORWARDING_PREPARE = '''cf_step "检查 IPv4 转发持久化"
STAGE_DIR=$(mktemp -d "$AGENT_DIR/.ipv4-forward.XXXXXX")
prepare_ipv4_forwarding
cf_step_done "IPv4 转发持久化检查通过"
'''
# The cleanup trap is installed before the helpers are defined.
_STANDALONE_FORWARDING_CLEANUP = '''    if [ "$IPV4_FORWARDING_READY" = 1 ]; then
        cleanup_ipv4_forwarding "$agent_status"
        if [ -n "$STAGE_DIR" ]; then rm -rf "$STAGE_DIR" || true; fi
    fi
'''


def generate_go_agent_install_script(
    server_url: str,
    agent_name: str,
    service_type: str,
    agent_port: int = 8080,
    agent_ip: str = "",
    config_path: str = None,
    restart_command: str = None,
    binary_download_url: str = "",
    service_manager: str = "",
    service_unit: str = "",
    service_binary: str = "",
    stop_command: str = "",
    start_command: str = "",
    status_command: str = "",
    health_url: str = "",
    health_dns_address: str = "",
    health_dns_name: str = "",
    deployment_health_timeout: int = 30,
    install_core: bool = False,
    manage_ipv4_forwarding: Optional[bool] = False,
    prepare_dns_ha: bool = False,
) -> str:
    """Generate an installer with explicit service lifecycle and deployment checks.

    Standard native installs detect systemd/OpenRC on the target. A custom
    restart command is not parsed to invent stop/start/status operations.

    ``manage_ipv4_forwarding`` enables and persists IPv4 forwarding like the
    Shell All-in-One installer: ``False`` leaves forwarding alone, ``None``
    enables it for Mihomo under systemd/OpenRC, and ``True`` requires that.
    """
    if service_type not in ("mihomo", "mosdns", "oxidns"):
        raise ValueError("Go Agent transactional deployment supports mihomo, mosdns and oxidns")
    if not isinstance(install_core, bool):
        raise ValueError("install_core 必须为 true 或 false")
    if not isinstance(prepare_dns_ha, bool):
        raise ValueError("prepare_dns_ha 必须为 true 或 false")
    if prepare_dns_ha and service_type != "oxidns":
        raise ValueError("仅 OxiDNS 单服务 Go Agent 支持准备 DNS 高可用环境")
    if not re.fullmatch(r"[A-Za-z0-9_-]+", agent_name):
        raise ValueError("Invalid agent name: use letters, numbers, hyphens or underscores")
    if not 1 <= int(agent_port) <= 65535:
        raise ValueError("Invalid Agent port")
    unit = service_unit or service_type
    if not re.fullmatch(r"[A-Za-z0-9_.@-]+", unit):
        raise ValueError("Invalid service unit")
    if unit.endswith(".service"):
        unit = unit[:-8]
    if service_manager not in ("", "systemd", "openrc", "command"):
        raise ValueError("Shell installs require systemd, openrc or explicit command lifecycle")
    standard_restart = not restart_command or restart_command in (
        f"systemctl restart {unit}", f"systemctl restart {unit}.service", f"rc-service {unit} restart",
    )
    if install_core:
        if service_type not in ("mihomo", "oxidns"):
            raise ValueError("仅单服务 Mihomo 或 OxiDNS Go Agent 支持同时安装核心")
        if (config_path not in (None, "", f"/etc/{service_type}/config.yaml") or
                unit != service_type or service_manager or not standard_restart or
                service_binary not in ("", service_type, f"/usr/local/bin/{service_type}") or
                any((stop_command, start_command, status_command))):
            raise ValueError("同时安装核心需要默认配置路径、服务名、核心路径和自动识别服务管理器，不能使用自定义启停命令")
        if isinstance(agent_port, bool) or not re.fullmatch(r"[0-9]+", str(agent_port)):
            raise ValueError("Invalid Agent port")
        if int(agent_port) == (1053 if service_type == "mihomo" else 53):
            raise ValueError("Agent 端口不能与所安装核心的 DNS 监听端口相同")
    if not service_manager:
        service_manager = "__CONFIGFLOW_INIT__" if standard_restart else "command"
    if prepare_dns_ha and service_manager == "command":
        raise ValueError("DNS 高可用安装需要原生 systemd/OpenRC，不能使用自定义启停命令")
    if service_manager == "command" and not all((stop_command, start_command, status_command)):
        raise ValueError("Custom lifecycle requires stop_command, start_command and status_command; a restart command alone cannot support rollback")
    if manage_ipv4_forwarding is not None and not isinstance(manage_ipv4_forwarding, bool):
        raise ValueError("manage_ipv4_forwarding 必须为 true 或 false")
    # The Agent recognises the persisted forwarding only under systemd/OpenRC.
    forwarding_supported = service_type == "mihomo" and service_manager != "command"
    if manage_ipv4_forwarding is None:
        manage_ipv4_forwarding = forwarding_supported
    elif manage_ipv4_forwarding and not forwarding_supported:
        raise ValueError(STANDALONE_FORWARDING_UNSUPPORTED)
    cfg = {
        "server_url": server_url,
        "agent_name": agent_name,
        "agent_host": "0.0.0.0",
        "agent_port": int(agent_port),
        "agent_ip": agent_ip,
        "service_type": service_type,
        "deployment_method": "shell",
        "service_name": service_type,
        "config_path": config_path or f"/etc/{service_type}/config.yaml",
        "restart_command": "__CONFIGFLOW_RESTART__" if standard_restart else restart_command,
        "service_manager": service_manager,
        "service_unit": unit,
        "service_binary": service_binary or service_type,
        "stop_command": stop_command,
        "start_command": start_command,
        "status_command": status_command,
        "health_url": health_url,
        "health_dns_address": health_dns_address,
        "health_dns_name": health_dns_name,
        "deployment_health_timeout": int(deployment_health_timeout),
        "heartbeat_interval": 30,
        "agent_id": "__CONFIGFLOW_AGENT_ID__",
        "token": "__CONFIGFLOW_TOKEN__",
    }
    replacements = {
        "service_type": service_type,
        "service_unit": unit,
        "agent_name": agent_name,
        "agent_port": str(cfg["agent_port"]),
        "service_label": {"mihomo": "Mihomo", "oxidns": "OxiDNS", "mosdns": "MosDNS"}[service_type],
        "config_json": json.dumps(cfg, ensure_ascii=True, indent=2),
        "binary_download_url_shell": shlex.quote(binary_download_url.rstrip("/")),
        "init_detection": _AGENT_INIT_DETECTION,
        "agent_install_dependencies": AGENT_INSTALL_DEPENDENCIES,
        # OxiDNS resolves cron timezones from the system tzdata.
        "core_dependencies": " zoneinfo" if service_type == "oxidns" else "",
        "installation_action": "安装或升级 Go Agent（包含事务发布与失败回滚）",
        "core_install_phase": "",
        "install_arguments": 'for cf_argument do\n    case "$cf_argument" in --verbose|-v) ;; *) cf_error "未知参数：$cf_argument"; exit 2 ;; esac\ndone',
        "installation_choice": 'cf_ui "1) {installation_action}"\ncf_ui "2) 卸载 Agent"\ncf_ui "3) 取消"\ncf_prompt \'请输入选项 [1-3]: \'\nread -r choice < /dev/tty',
        "dns_ha_functions": "",
        "dns_ha_prepare": "",
        "dns_ha_existing_config": "",
        "agent_summary_port": str(cfg["agent_port"]),
        "core_phase_state": "",
        "core_cleanup_message": "",
    }
    # Every placeholder starts a line and carries its own newline, so the
    # opt-out output is byte-identical to the installer before this option.
    forwarding = {
        "ipv4_forwarding_notice": 'cf_ui "Mihomo 单独安装将开启并持久化本机 IPv4 转发（作为主路由的代理网关时需要）；'
                                  '不需要时请在面板勾选「不开启 IPv4 转发」后重新生成命令。"\n',
        "ipv4_forwarding_dependencies": " sysctl cmp ln",
        "ipv4_forwarding_environment": _STANDALONE_GATEWAY_RECORD_REFUSAL + INSTALLER_FORWARDING_ENVIRONMENT,
        "ipv4_forwarding_environment_end": INSTALLER_FORWARDING_ENVIRONMENT_END,
        "ipv4_forwarding_state": "IPV4_FORWARDING_READY=0\n",
        "ipv4_forwarding_cleanup": _STANDALONE_FORWARDING_CLEANUP,
        "ipv4_forwarding_functions": ('fail() { echo "$*" >&2; exit 1; }\nSERVICE=mihomo\nSTAGE_DIR=""\n'
                                      + _standalone_forwarding_functions() + "IPV4_FORWARDING_READY=1\n"),
        "ipv4_forwarding_prepare": _STANDALONE_FORWARDING_PREPARE,
        "ipv4_forwarding_hook": "install_ipv4_forwarding_hook\n",
        "ipv4_systemd_pre": f"ExecStartPre=/bin/sh {STANDALONE_FORWARDING_HOOK}\n",
        "ipv4_openrc_pre": f"    /bin/sh {STANDALONE_FORWARDING_HOOK} || return 1\n",
        "ipv4_forwarding_apply": "persist_ipv4_forwarding\n",
        "ipv4_forwarding_summary": "summarize_ipv4_forwarding\n",
        "ipv4_forwarding_uninstall": '        [ ! -f /etc/sysctl.d/99-configflow-aio-forward.conf ] || '
                                     'cf_ui "IPv4 转发及其 sysctl.d 持久化设置保留，避免影响仍在运行的 Mihomo。"\n',
    }
    replacements.update({key: value if manage_ipv4_forwarding else "" for key, value in forwarding.items()})
    if install_core:
        from backend.agents.core_install_script import generate_core_install_script

        core_name = "Mihomo" if service_type == "mihomo" else "OxiDNS"
        replacements["installation_action"] = f"安装 {core_name} 核心及 Agent"
        # Keep the core installer's variables, umask, traps and exit statements
        # inside a subshell. In particular, do not place this in an if/&&/||
        # condition: that would disable its set -e failure handling.
        replacements["core_install_phase"] = (
            f'echo "先安装或保留 {service_type} 核心，成功后继续安装 Agent。"\n'
            '(\n'
            f'set -- --core {service_type}\n'
            'CF_OUTPUT_OWNER=false\n'
            + generate_core_install_script(installer_forwarding=manage_ipv4_forwarding).rstrip() + '\n)\n'
            'CORE_PHASE_COMPLETED=true\n'
            f'CORE_BINARY={shlex.quote(service_binary or service_type)}\n'
            f'if [ ! -f /etc/{service_type}/config.yaml ] || [ ! -r /etc/{service_type}/config.yaml ] || ! command -v "$CORE_BINARY" >/dev/null 2>&1; then\n'
            f'    echo "{service_type} 核心缺少默认配置或可执行文件。请补齐 /etc/{service_type}/config.yaml 和核心程序；已有自定义部署请取消同时安装核心，并设置实际路径。" >&2\n'
            '    exit 1\n'
            'fi\n'
            'if [ "$INIT_SYSTEM" = systemd ]; then\n'
            f'    core_unit_state=$(systemctl show {service_type}.service --property=LoadState --value)\n'
            '    if [ "$core_unit_state" != loaded ]; then\n'
            f'        echo "{service_type} 默认服务未正确加载，请先修复 {service_type}.service；已有自定义服务请取消同时安装核心并填写实际服务名。" >&2\n'
            '        exit 1\n'
            '    fi\n'
            f'elif [ ! -x /etc/init.d/{service_type} ]; then\n'
            f'    echo "缺少可执行的 /etc/init.d/{service_type} 服务，请先补齐；已有自定义服务请取消同时安装核心并填写实际服务名。" >&2\n'
            '    exit 1\n'
            'fi\n'
        )
        replacements["init_detection"] = _CORE_INIT_DETECTION
        replacements["core_phase_state"] = "CORE_PHASE_COMPLETED=false\n"
        replacements["core_cleanup_message"] = (
            '        if [ "$CORE_PHASE_COMPLETED" = true ]; then\n'
            f'            echo "Agent 安装未完成；{service_type} 核心安装阶段已结束，已安装或原有的核心予以保留。修复上方 Agent 错误后可重新运行。" >&2\n'
            '        fi\n')
    replacements["installation_choice"] = replacements["installation_choice"].replace("{installation_action}", replacements["installation_action"])
    if prepare_dns_ha:
        replacements["init_detection"] = _CORE_INIT_DETECTION
        replacements["install_arguments"] = '''choice=1
for cf_argument do
    case "$cf_argument" in
        --verbose|-v) ;;
        --uninstall) choice=2 ;;
        --cancel) choice=3 ;;
        *) cf_error "未知参数：$cf_argument"; exit 2 ;;
    esac
done'''
        replacements["installation_choice"] = 'cf_ui "OxiDNS 高可用：自动准备环境并安装；VIP 在面板配对时设置。"'
        replacements["dns_ha_functions"] = DNS_HA_PREPARATION.replace("{agent_port}", str(cfg["agent_port"])) + "\n"
        replacements["dns_ha_prepare"] = 'agent_prepare_dns_ha\n'
        replacements["dns_ha_existing_config"] = '''if [ -n "$DNS_HA_EXISTING_CONFIG" ]; then
    cp "$DNS_HA_EXISTING_CONFIG" "$CONFIG_FILE.new"
    chmod 600 "$CONFIG_FILE.new"
fi
'''
        replacements["agent_summary_port"] = "$DNS_HA_EFFECTIVE_PORT"
        if install_core:
            replacements["core_install_phase"] = ('if [ -z "$DNS_HA_EXISTING_CONFIG" ]; then\n'
                                                  + replacements["core_install_phase"] + 'fi\n')
    if install_core or prepare_dns_ha:
        # Minimal root shells (notably Alpine pct/SSH sessions) can omit the
        # location where the canonical core installer places its binaries.
        replacements["install_arguments"] = (
            'PATH="${PATH:-/usr/sbin:/usr/bin:/sbin:/bin}:/usr/local/sbin:/usr/local/bin"\n'
            'export PATH\n' + replacements["install_arguments"]
        )
    # Single pass: values containing braces must remain literal, especially JSON
    # commands and URLs. The config heredoc is quoted to disable shell expansion.
    script = re.sub(r"\{(" + "|".join(replacements) + r")\}", lambda match: replacements[match.group(1)], INSTALL_GO_SCRIPT_TEMPLATE)
    if install_core:
        script = script.replace("#!/bin/sh\n", f"#!/bin/sh\n{AGENT_CORE_INSTALL_INTERFACE}\n", 1)
    rendered = with_install_output(script)
    if prepare_dns_ha:
        rendered = with_root_privileges(rendered)
        lines = rendered.splitlines(keepends=True)
        rendered = ''.join(lines[:2]) + DNS_HA_PREPARATION_INTERFACE + '\n' + ''.join(lines[2:])
    return rendered


def generate_go_aio_install_script(
    server_url: str,
    agent_name: str,
    agent_ip: str = "",
    binary_download_url: str = "",
    mihomo_agent_port: int = 8080,
    oxidns_agent_port: int = 8082,
    mihomo_config_path: str = "/etc/mihomo/config.yaml",
    oxidns_config_path: str = "/etc/oxidns/config.yaml",
    mihomo_service_unit: str = "mihomo",
    oxidns_service_unit: str = "oxidns",
    mihomo_service_binary: str = "mihomo",
    oxidns_service_binary: str = "oxidns",
    install_core: bool = False,
    non_interactive: bool = False,
    verified_agent_path: str = "",
    installation_id: str = "",
    installation_token: str = "",
    manage_ipv4_forwarding: bool = True,
    manage_tun: bool = True,
    installation_description: str = "此安装器只接管已安装的 Mihomo 和 OxiDNS，不安装核心、不改核心配置、不重启核心。",
    tun_prepare_assume_enabled: bool = False,
) -> str:
    """Manage two isolated native Agents, optionally installing missing cores.

    Core installation is opt-in and uses the canonical installer in an isolated
    phase. Agent failures retain completed cores and restart previous Agents.
    Ordinary AIO prepares enabled TUN and persists IPv4 forwarding; the selective
    gateway installer opts out because it manages its own network lifecycle.
    The router split's gateway installer prepares TUN although its bootstrap
    config keeps TUN off (``tun_prepare_assume_enabled``).
    """
    if not isinstance(install_core, bool):
        raise ValueError("install_core 必须为 true 或 false")
    if not isinstance(manage_ipv4_forwarding, bool):
        raise ValueError("manage_ipv4_forwarding must be a boolean")
    if not isinstance(manage_tun, bool):
        raise ValueError("manage_tun must be a boolean")
    if not isinstance(non_interactive, bool):
        raise ValueError("non_interactive must be a boolean")
    if not isinstance(tun_prepare_assume_enabled, bool):
        raise ValueError("tun_prepare_assume_enabled must be a boolean")
    if tun_prepare_assume_enabled and not (manage_tun and verified_agent_path):
        raise ValueError("assume-tun-enabled is only supported inside the gateway installer")
    if verified_agent_path and verified_agent_path != "$GATEWAY_STAGE/configflow-agent":
        raise ValueError("Preverified Agent binary is only supported inside the gateway installer")
    if verified_agent_path and not non_interactive:
        raise ValueError("Preverified Agent requires non-interactive installation")
    if bool(installation_id) != bool(installation_token):
        raise ValueError("Installation id and token must be supplied together")
    if installation_id and not re.fullmatch(r"[A-Za-z0-9_-]{16,80}", installation_id):
        raise ValueError("Invalid installation id")
    if installation_token and not re.fullmatch(r"[A-Za-z0-9_.-]{16,512}", installation_token):
        raise ValueError("Invalid installation token")
    if not isinstance(agent_name, str) or not re.fullmatch(r"[A-Za-z0-9_-]+", agent_name):
        raise ValueError("Invalid agent name: use letters, numbers, hyphens or underscores")
    ports = []
    for port in (mihomo_agent_port, oxidns_agent_port):
        if isinstance(port, bool) or not re.fullmatch(r"[0-9]+", str(port)) or not 1 <= int(port) <= 65535:
            raise ValueError("Invalid Agent port")
        ports.append(int(port))
    if ports[0] == ports[1]:
        raise ValueError("Mihomo and OxiDNS Agent ports must be different")
    if install_core and any(port in (53, 1053) for port in ports):
        raise ValueError("两个 Agent 端口均不能使用核心 DNS 监听端口 53 或 1053")
    def inside_agent_directory(value):
        # Collapse Linux absolute-path aliases before comparing components.
        normalized = PurePosixPath(posixpath.normpath("/" + value.lstrip("/")))
        agent_root = PurePosixPath("/opt/configflow-agent")
        return normalized == agent_root or agent_root in normalized.parents

    paths = []
    for value in (mihomo_config_path, oxidns_config_path):
        if (not isinstance(value, str) or not value.startswith("/") or
                any(ord(c) < 32 or ord(c) == 127 for c in value) or
                ".." in PurePosixPath(value).parts or str(PurePosixPath(value)) == "/" or value.endswith("/")):
            raise ValueError("Core configuration paths must be absolute file paths without parent traversal")
        if inside_agent_directory(value):
            raise ValueError("Core configuration paths must be outside the Agent directory")
        paths.append(str(PurePosixPath("/" + value.lstrip("/"))))
    if paths[0] == paths[1]:
        raise ValueError("Mihomo and OxiDNS configuration paths must be different")
    if PurePosixPath(paths[0]).parent == PurePosixPath(paths[1]).parent:
        raise ValueError("Core configurations require separate directories for deployment journals")
    replacements = {"binary_download_url_shell": shlex.quote(binary_download_url.rstrip("/")),
                    "mihomo_agent_port": str(ports[0]), "oxidns_agent_port": str(ports[1])}
    tun_flags = "-prepare-tun -assume-tun-enabled" if tun_prepare_assume_enabled else "-prepare-tun"
    replacements.update({
        "tun_prepare_notice": 'cf_ui "Mihomo 已启用 TUN 时，自动检查运行环境；本机加载 TUN 驱动并设置开机加载，容器缺少设备时给出宿主机处理提示。"' if manage_tun else "",
        "tun_prepare": '''cf_step "准备 TUN 驱动与开机加载"
if "$BINARY_TMP" -config "$STAGE_DIR/mihomo-config.json" ''' + tun_flags + '''; then
    :
else
    tun_status=$?
    echo "TUN 准备未完成。若提示未知 -prepare-tun 参数，请使用包含 TUN 自动准备功能的新 Agent 构建；否则按上方原因处理后重跑。现有 Agent 尚未停止。" >&2
    exit "$tun_status"
fi
cf_step_done "TUN 准备完成（具体处理结果见日志）"''' if manage_tun else "",
        "ipv4_forwarding_functions": INSTALL_GO_AIO_FORWARDING if manage_ipv4_forwarding else "",
        "ipv4_forwarding_dependencies": " sysctl cmp ln" if manage_ipv4_forwarding else "",
        "ipv4_forwarding_environment": INSTALLER_FORWARDING_ENVIRONMENT if manage_ipv4_forwarding else "",
        "ipv4_forwarding_environment_end": INSTALLER_FORWARDING_ENVIRONMENT_END if manage_ipv4_forwarding else "",
        "ipv4_forwarding_notice": 'cf_ui "普通 Shell 同机安装将自动开启并持久化当前 Linux 的 IPv4 转发。"' if manage_ipv4_forwarding else "",
        "ipv4_forwarding_prepare": "prepare_ipv4_forwarding" if manage_ipv4_forwarding else "",
        "ipv4_forwarding_ownership": "check_ipv4_forwarding_ownership" if manage_ipv4_forwarding else "",
        "ipv4_forwarding_hook": "install_ipv4_forwarding_hook" if manage_ipv4_forwarding else "",
        "ipv4_forwarding_apply": "persist_ipv4_forwarding" if manage_ipv4_forwarding else "",
        "ipv4_forwarding_cleanup": 'cleanup_ipv4_forwarding "$cleanup_status"' if manage_ipv4_forwarding else "",
        "ipv4_forwarding_summary": "summarize_ipv4_forwarding" if manage_ipv4_forwarding else "",
        "ipv4_forwarding_uninstall": 'cf_ui "IPv4 转发及其 sysctl.d 持久化设置保留，避免影响仍在运行的核心。"' if manage_ipv4_forwarding else "",
    })
    for service, port, path, unit, binary in zip(
        ("mihomo", "oxidns"), ports, paths,
        (mihomo_service_unit, oxidns_service_unit),
        (mihomo_service_binary, oxidns_service_binary),
    ):
        if not isinstance(unit, str) or not re.fullmatch(r"[A-Za-z0-9_][A-Za-z0-9_.@-]*", unit):
            raise ValueError("Invalid service unit")
        unit = unit.removesuffix(".service")
        if not unit or unit.startswith("configflow-"):
            raise ValueError("Core service unit must not be a ConfigFlow Agent unit")
        if not isinstance(binary, str) or not binary or any(ord(c) < 32 or ord(c) == 127 for c in binary):
            raise ValueError("Invalid core service binary")
        if binary.startswith("/") and inside_agent_directory(binary):
            raise ValueError("Core service binaries must be outside the Agent directory")
        if install_core and (path != f"/etc/{service}/config.yaml" or unit != service or
                             binary not in (service, f"/usr/local/bin/{service}")):
            raise ValueError("同时安装两个核心需要默认配置路径、服务名和核心路径；自定义部署请取消同时安装核心")
        cfg = {
            "server_url": server_url, "agent_name": f"{agent_name}-{service}",
            "agent_host": "0.0.0.0", "agent_port": port, "agent_ip": agent_ip,
            "service_type": service, "service_name": service, "deployment_method": "shell",
            "native_aio": True, "agent_service_unit": f"configflow-agent-{service}",
            "config_path": path, "service_manager": "__CONFIGFLOW_INIT__", "service_unit": unit,
            "service_binary": binary, "restart_command": "__CONFIGFLOW_RESTART__",
            "heartbeat_interval": 30, "deployment_health_timeout": 30,
            "agent_id": "__CONFIGFLOW_AGENT_ID__", "token": "__CONFIGFLOW_TOKEN__",
        }
        if installation_id:
            cfg["installation_id"] = installation_id
            cfg["installation_token"] = installation_token
        replacements[f"{service}_config_json"] = json.dumps(cfg, ensure_ascii=True, indent=2)
        replacements[f"{service}_config_path_shell"] = shlex.quote(path)
        replacements[f"{service}_service_unit_shell"] = shlex.quote(unit)
        replacements[f"{service}_service_binary_shell"] = shlex.quote(binary)
    if mihomo_service_unit.removesuffix(".service") == oxidns_service_unit.removesuffix(".service"):
        raise ValueError("Mihomo and OxiDNS service units must be different")
    agent_arch_detection = '''case "$(uname -m)" in
    x86_64|amd64) AGENT_ARCH=linux-amd64 ;;
    aarch64|arm64) AGENT_ARCH=linux-arm64 ;;
    armv7l|armhf) AGENT_ARCH=linux-armv7 ;;
    *) fail "不支持的 CPU 架构" ;;
esac'''
    replacements.update({
        "init_detection": '''if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
    INIT_SYSTEM=systemd
elif command -v rc-service >/dev/null 2>&1 && command -v rc-update >/dev/null 2>&1; then
    INIT_SYSTEM=openrc
elif command -v systemctl >/dev/null 2>&1; then
    INIT_SYSTEM=systemd
else
    fail "需要 systemd 或 OpenRC 管理已安装的 Mihomo 和 OxiDNS。"
fi''',
        "installation_description": "cf_ui " + shlex.quote(installation_description),
        "installation_action": "安装或升级两个独立 Agent",
        "agent_arch_detection": "\n".join("        " + line for line in agent_arch_detection.splitlines()),
        "agent_install_dependencies": AGENT_INSTALL_DEPENDENCIES,
        "core_phase_state": "",
        "core_cleanup_message": "",
        "core_install_phase": "",
    })
    if install_core:
        from backend.agents.core_install_script import generate_core_install_script

        replacements.update({
            "init_detection": _CORE_INIT_DETECTION,
            "installation_description": 'cf_ui "安装 Mihomo 与 OxiDNS，并自动安装管理 Agent；已有核心与配置保留。"',
            "installation_action": "安装 Mihomo 与 OxiDNS 以及对应 Agent",
            "core_phase_state": "CORE_PHASE_COMPLETED=false\n",
            "core_cleanup_message": '''        if [ "$CORE_PHASE_COMPLETED" = true ]; then
            echo "Agent 安装未完成；两个核心的安装阶段已结束，已安装或原有的核心予以保留。修复上方 Agent 错误后可重新运行。" >&2
        fi
''',
            # An unconditional subshell preserves set -e, unlike an if/&&/||
            # context, and isolates core variables, exit statements and traps.
            "core_install_phase": 'echo "先安装或保留两个核心，成功后继续安装两个 Agent。"\n'
                + '(\nset -- --core both\nCF_OUTPUT_OWNER=false\n'
                + generate_core_install_script(installer_forwarding=manage_ipv4_forwarding).rstrip() + '\n)\n'
                + 'CORE_PHASE_COMPLETED=true\n'
                + '''# Minimal systems may not yet have /opt, needed for physical path checks.
mkdir -p "$(dirname "$AGENT_ROOT")"
''',
        })
    template = INSTALL_GO_AIO_SCRIPT_TEMPLATE
    if non_interactive:
        template = template.replace('read -r choice < /dev/tty', 'choice=1 # Gateway setup already selected install in the web wizard')
    if verified_agent_path:
        start = template.index('if command -v wget >/dev/null 2>&1; then\n    wget -q -O "$BINARY_TMP"')
        end = template.index('\nchmod 755 "$BINARY_TMP"', start)
        template = template[:start] + 'cp "$GATEWAY_STAGE/configflow-agent" "$BINARY_TMP"' + template[end:]
    script = re.sub(r"\{(" + "|".join(replacements) + r")\}",
                    lambda match: replacements[match.group(1)], template)
    if install_core:
        script = script.replace("#!/bin/sh\n", f"#!/bin/sh\n{AGENT_CORE_INSTALL_INTERFACE}\n", 1)
    return with_install_output(script)
