#!/bin/sh
set -e

case "${SERVICE_TYPE:-}" in
    oxidns-ha-host|oxidns-ha-macvlan)
        exec python3 /usr/local/lib/configflow/docker-compose-compact.py "$@"
        ;;
    ha-companion-compact)
        export CONFIGFLOW_DNS_HA=1 CONFIGFLOW_DNS_HA_NETWORK=host CONFIGFLOW_DNS_HA_IDENTITY=installation
        exec /ha-companion-entrypoint.sh
        ;;
esac

if [ "${SERVICE_TYPE:-}" = "ha-companion" ]; then
    exec /ha-companion-entrypoint.sh
fi

# ==============================================================================
# ConfigFlow Agent Docker Entrypoint (统一服务入口)
# ------------------------------------------------------------------------------
# 支持两种模式：
#   1. 单服务模式：设置 SERVICE_TYPE=mihomo|mosdns|oxidns（独立核心镜像使用）
#   2. AIO 模式：设置 ENABLE_MIHOMO/ENABLE_MOSDNS/ENABLE_OXIDNS（aio 镜像使用）
# ==============================================================================

SUPERVISORCTL_CMD="supervisorctl -c /etc/supervisor/supervisord.conf"
AGENT_DIR="/opt/configflow-agent"
mkdir -p "$AGENT_DIR"

# --- 创建默认 Mihomo 配置 ---
create_default_mihomo_config() {
    MIHOMO_CONFIG="${1:-/etc/mihomo/config.yaml}"
    mkdir -p "$(dirname "$MIHOMO_CONFIG")"
    if [ ! -f "$MIHOMO_CONFIG" ]; then
        if [ -e "$(dirname "$MIHOMO_CONFIG")/.configflow-deployments" ]; then
            echo "Mihomo config missing in a managed deployment directory; preserving files for manual recovery."
            return 0
        fi
        echo "Mihomo config not found, creating default config..."
        cat > "$MIHOMO_CONFIG" <<'EOFCONFIG'
# Mihomo 默认配置
# 该配置会被 ConfigFlow 自动更新

mixed-port: 7890
allow-lan: true
mode: rule
log-level: info
external-controller: 0.0.0.0:9090

dns:
  enable: true
  listen: 0.0.0.0:53
  enhanced-mode: fake-ip
  nameserver:
    - 223.5.5.5
    - 119.29.29.29

proxies: []

proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - DIRECT

rules:
  - MATCH,PROXY
EOFCONFIG
        if [ "${ENABLE_MIHOMO:-false}" = "true" ] && { [ "${ENABLE_MOSDNS:-false}" = "true" ] || [ "${ENABLE_OXIDNS:-false}" = "true" ]; }; then
            sed -i 's/listen: 0.0.0.0:53/listen: 0.0.0.0:1053/' "$MIHOMO_CONFIG"
        fi
        echo "Default mihomo config created at $MIHOMO_CONFIG"
    fi
}

# --- 创建默认 MosDNS 配置 ---
create_default_mosdns_config() {
    MOSDNS_CONFIG="${1:-/etc/mosdns/config.yaml}"
    mkdir -p "$(dirname "$MOSDNS_CONFIG")"
    if [ ! -f "$MOSDNS_CONFIG" ]; then
        if [ -e "$(dirname "$MOSDNS_CONFIG")/.configflow-deployments" ]; then
            echo "MosDNS config missing in a managed deployment directory; preserving files for manual recovery."
            return 0
        fi
        echo "MosDNS config not found, creating default config..."
        cat > "$MOSDNS_CONFIG" <<'EOFCONFIG'
# MosDNS 默认配置
# 该配置会被 ConfigFlow 自动更新

log:
  level: info
  file: ""

plugins:
  # 上游服务器
  - tag: forward_local
    type: forward
    args:
      concurrent: 2
      upstreams:
        - addr: 223.5.5.5
        - addr: 119.29.29.29

  # 执行序列
  - tag: main_sequence
    type: sequence
    args:
      - exec: $forward_local

  # UDP 服务器
  - tag: udp_server
    type: udp_server
    args:
      entry: main_sequence
      listen: ":53"

  # TCP 服务器
  - tag: tcp_server
    type: tcp_server
    args:
      entry: main_sequence
      listen: ":53"
EOFCONFIG
        echo "Default mosdns config created at $MOSDNS_CONFIG"
    fi
}

# The OxiDNS core owns state/, including learned rules and the recorder DB.
create_default_oxidns_config() {
    OXIDNS_CONFIG="${1:-/etc/oxidns/config.yaml}"
    if [ ! -f "$OXIDNS_CONFIG" ] && [ -e "$(dirname "$OXIDNS_CONFIG")/.configflow-deployments" ]; then
        echo "OxiDNS config missing in a managed deployment directory; preserving files for manual recovery."
        return 0
    fi
    mkdir -p "$(dirname "$OXIDNS_CONFIG")/state"
    if [ ! -f "$OXIDNS_CONFIG" ]; then
        cat > "$OXIDNS_CONFIG" <<'EOFOXIDNS'
runtime:
  worker_threads: 2
log:
  level: info
plugins:
  - tag: forward_local
    type: forward
    args:
      concurrent: 2
      upstreams:
        - addr: udp://223.5.5.5:53
        - addr: udp://119.29.29.29:53
  - tag: main_sequence
    type: sequence
    args:
      - exec: $forward_local
  - tag: server_udp
    type: udp_server
    args: {entry: main_sequence, listen: ':53'}
  - tag: server_tcp
    type: tcp_server
    args: {entry: main_sequence, listen: ':53'}
EOFOXIDNS
    fi
}

# --- 生成 Agent 配置文件 ---
generate_agent_config() {
    _service_type=$1
    _agent_name=$2
    _agent_port=$3
    _config_path=$4

    _config_file="${AGENT_DIR}/config-${_service_type}.json"
    # Python already ships in this Supervisor image. JSON serialization keeps
    # quotes/newlines in environment values from corrupting credentials/config.
    SERVER_URL="${SERVER_URL:-}" HEARTBEAT_INTERVAL="${HEARTBEAT_INTERVAL:-60}" python3 - "$_config_file" "$_service_type" "$_agent_name" "$_agent_port" "$_config_path" <<'PYCONFIG'
import json
import os
import sys
from pathlib import Path

path, service, name, port, config_path = sys.argv[1:]
path = Path(path)
existing = json.loads(path.read_text()) if path.exists() else {}
prefix = service.upper()
cfg = {
    "server_url": os.environ.get("SERVER_URL", ""),
    "agent_name": name,
    "agent_host": os.environ.get("AGENT_HOST", "0.0.0.0"),
    "agent_port": int(port),
    "agent_ip": os.environ.get("AGENT_IP", ""),
    "service_type": service,
    "deployment_method": "docker",
    "service_name": service,
    "config_path": config_path,
    "restart_command": "supervisorctl -c /etc/supervisor/supervisord.conf restart " + service,
    "service_manager": "supervisor",
    "service_unit": service,
    "service_binary": "/usr/local/bin/" + service,
    "health_url": os.environ.get(prefix + "_HEALTH_URL", ""),
    "health_dns_address": os.environ.get(prefix + "_HEALTH_DNS_ADDRESS", ""),
    "health_dns_name": os.environ.get(prefix + "_HEALTH_DNS_NAME", ""),
    "deployment_health_timeout": int(os.environ.get("DEPLOYMENT_HEALTH_TIMEOUT", "30")),
    "heartbeat_interval": int(os.environ.get("HEARTBEAT_INTERVAL", "60")),
}
if not existing.get("token"):
    cfg["registration_token"] = os.environ.get("AGENT_" + prefix + "_REGISTRATION_TOKEN", "")
    cfg["registration_key"] = os.environ.get("AGENT_REGISTRATION_KEY", "")
for key in ("agent_id", "token"):
    if existing.get(key):
        cfg[key] = existing[key]
tmp = path.with_name(path.name + ".tmp")
with tmp.open("w") as output:
    os.chmod(tmp, 0o600)
    json.dump(cfg, output, indent=2)
    output.flush()
    os.fsync(output.fileno())
os.replace(tmp, path)
parent = os.open(path.parent, os.O_RDONLY)
try:
    os.fsync(parent)
finally:
    os.close(parent)
PYCONFIG
    echo "Agent config generated at $_config_file"
}

configure_supervisor_core() {
    python3 - "$1" "$2" <<'PYSUPERVISOR'
import shlex
import sys
from pathlib import Path
service, config = sys.argv[1:]
path = Path("/etc/supervisor/conf.d") / (service + ".conf")
if path.exists():
    args = ["/usr/local/bin/" + service]
    args += (["-d", str(Path(config).parent), "-f", config] if service == "mihomo"
             else ["start", "-c", config, "-d", str(Path(config).parent)])
    command = "command=" + " ".join(shlex.quote(arg) for arg in args)
    # Supervisor interpolates percent signs in config values.
    command = command.replace("%", "%%")
    lines = path.read_text().splitlines()
    path.write_text("\n".join(command if line.startswith("command=") else line for line in lines) + "\n")
PYSUPERVISOR
}

# --- 设置 supervisor autostart ---
set_supervisor_autostart() {
    _conf_file=$1
    _enabled=$2
    if [ -f "$_conf_file" ]; then
        if [ "$_enabled" = "true" ]; then
            sed -i 's/autostart=false/autostart=true/' "$_conf_file"
        else
            sed -i 's/autostart=true/autostart=false/' "$_conf_file"
        fi
    fi
}

# ==============================================================================
# 主逻辑
# ==============================================================================

echo "Starting ConfigFlow Agent..."

case "${CONFIGFLOW_DNS_HA:-0}" in
    0) ;;
    1)
        if [ "${SERVICE_TYPE:-}" != "oxidns" ] && { [ -n "${SERVICE_TYPE:-}" ] || [ "${ENABLE_OXIDNS:-false}" != "true" ] || [ "${ENABLE_MIHOMO:-false}" = "true" ] || [ "${ENABLE_MOSDNS:-false}" = "true" ]; }; then
            echo "CONFIGFLOW_DNS_HA=1 requires a dedicated OxiDNS container; Mihomo, MosDNS and AIO are unsupported." >&2
            exit 1
        fi
        case "${CONFIGFLOW_DNS_HA_NETWORK:-}" in
            host|macvlan) ;;
            *) echo "CONFIGFLOW_DNS_HA_NETWORK must be host or macvlan." >&2; exit 1 ;;
        esac
        mkdir -p /var/lib/configflow-ha
        chmod 700 /var/lib/configflow-ha
        ;;
    *) echo "CONFIGFLOW_DNS_HA must be 0 or 1." >&2; exit 1 ;;
esac
# No journal means no keepalived: Agent recovery owns startup after Supervisor.
set_supervisor_autostart /etc/supervisor/conf.d/configflow-keepalived.conf false

if [ -n "$SERVICE_TYPE" ]; then
    # ---- 单服务模式 ----
    echo "Mode: single service (${SERVICE_TYPE})"

    if [ "$SERVICE_TYPE" = "mihomo" ]; then
        generate_agent_config "mihomo" \
            "${AGENT_NAME:-mihomo-agent}" \
            "${AGENT_PORT:-8080}" \
            "${CONFIG_PATH:-/etc/mihomo/config.yaml}"
        create_default_mihomo_config "${CONFIG_PATH:-/etc/mihomo/config.yaml}"
        configure_supervisor_core mihomo "${CONFIG_PATH:-/etc/mihomo/config.yaml}"
        set_supervisor_autostart /etc/supervisor/conf.d/agent-mihomo.conf true
        set_supervisor_autostart /etc/supervisor/conf.d/mihomo.conf true
    elif [ "$SERVICE_TYPE" = "mosdns" ]; then
        generate_agent_config "mosdns" \
            "${AGENT_NAME:-mosdns-agent}" \
            "${AGENT_PORT:-8080}" \
            "${CONFIG_PATH:-/etc/mosdns/config.yaml}"
        create_default_mosdns_config "${CONFIG_PATH:-/etc/mosdns/config.yaml}"
        configure_supervisor_core mosdns "${CONFIG_PATH:-/etc/mosdns/config.yaml}"
        set_supervisor_autostart /etc/supervisor/conf.d/agent-mosdns.conf true
        set_supervisor_autostart /etc/supervisor/conf.d/mosdns.conf true
    elif [ "$SERVICE_TYPE" = "oxidns" ]; then
        generate_agent_config "oxidns" "${AGENT_NAME:-oxidns-agent}" "${AGENT_PORT:-8082}" "${CONFIG_PATH:-/etc/oxidns/config.yaml}"
        create_default_oxidns_config "${CONFIG_PATH:-/etc/oxidns/config.yaml}"
        configure_supervisor_core oxidns "${CONFIG_PATH:-/etc/oxidns/config.yaml}"
        set_supervisor_autostart /etc/supervisor/conf.d/agent-oxidns.conf true
        set_supervisor_autostart /etc/supervisor/conf.d/oxidns.conf true
    else
        echo "Unsupported SERVICE_TYPE: $SERVICE_TYPE" >&2
        exit 1
    fi
else
    # ---- AIO 模式 (aio 镜像) ----
    echo "Mode: AIO"

    # Agents report interrupted deployments independently after Supervisor starts.
    generate_agent_config "mihomo" "${AGENT_MIHOMO_NAME:-mihomo-agent}" "${AGENT_MIHOMO_PORT:-8080}" "/etc/mihomo/config.yaml"
    generate_agent_config "mosdns" "${AGENT_MOSDNS_NAME:-mosdns-agent}" "${AGENT_MOSDNS_PORT:-8081}" "/etc/mosdns/config.yaml"
    if [ "${ENABLE_OXIDNS:-false}" = "true" ] && [ "${ENABLE_MOSDNS:-false}" = "true" ]; then
        echo "MosDNS and OxiDNS both default to DNS port 53; enable one per container." >&2
        exit 1
    fi
    if [ "${ENABLE_OXIDNS:-false}" = "true" ]; then
        generate_agent_config "oxidns" "${AGENT_OXIDNS_NAME:-oxidns-agent}" "${AGENT_OXIDNS_PORT:-8082}" "/etc/oxidns/config.yaml"
    fi
    create_default_mihomo_config
    create_default_mosdns_config
    if [ "${ENABLE_OXIDNS:-false}" = "true" ]; then
        create_default_oxidns_config
        configure_supervisor_core oxidns /etc/oxidns/config.yaml
    fi
    set_supervisor_autostart /etc/supervisor/conf.d/agent-oxidns.conf "${ENABLE_OXIDNS:-false}"
    set_supervisor_autostart /etc/supervisor/conf.d/oxidns.conf "${ENABLE_OXIDNS:-false}"

    # Mihomo
    if [ "${ENABLE_MIHOMO}" = "true" ]; then
        echo "Mihomo is enabled."
        set_supervisor_autostart /etc/supervisor/conf.d/agent-mihomo.conf true
        set_supervisor_autostart /etc/supervisor/conf.d/mihomo.conf true
    else
        echo "Mihomo is disabled."
        set_supervisor_autostart /etc/supervisor/conf.d/agent-mihomo.conf false
        set_supervisor_autostart /etc/supervisor/conf.d/mihomo.conf false
    fi

    # MosDNS
    if [ "${ENABLE_MOSDNS}" = "true" ]; then
        echo "MosDNS is enabled."
        set_supervisor_autostart /etc/supervisor/conf.d/agent-mosdns.conf true
        set_supervisor_autostart /etc/supervisor/conf.d/mosdns.conf true
    else
        echo "MosDNS is disabled."
        set_supervisor_autostart /etc/supervisor/conf.d/agent-mosdns.conf false
        set_supervisor_autostart /etc/supervisor/conf.d/mosdns.conf false
    fi

fi

chmod -R 755 /etc/supervisor

echo "Log directory status:"
ls -la /var/log/supervisor/ 2>/dev/null || true

echo "Executing supervisord..."
exec "$@"
