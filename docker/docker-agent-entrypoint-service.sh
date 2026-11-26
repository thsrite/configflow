#!/bin/sh
set -e

case "${SERVICE_TYPE:-}" in
    oxidns-ha-host|oxidns-ha-macvlan)
        exec /usr/local/bin/configflow-agent ha-compact "$@"
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

AGENT_DIR="/opt/configflow-agent"
mkdir -p "$AGENT_DIR"
# Containers created by older images or Compose files may still pass the
# former Supervisor command; s6-overlay (/init) supervises the services now.
if [ "${1:-}" = /usr/bin/supervisord ]; then
    set -- /init
fi
# Cores enabled in this container (s6 starts each with its Agent).
CORES=""

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

# --- 生成 Agent 配置文件（保留已登记的 agent_id/token）---
generate_agent_config() {
    SERVER_URL="${SERVER_URL:-}" HEARTBEAT_INTERVAL="${HEARTBEAT_INTERVAL:-60}" \
        /usr/local/bin/configflow-agent docker-config agent -dir "$AGENT_DIR" "$1" "$2" "$3" "$4"
    echo "Agent config generated at ${AGENT_DIR}/config-$1.json"
}

# --- 启用一个核心（及其 Agent）；可选指定核心配置文件 ---
# CORES holds one entry per line, so configuration paths may contain spaces.
enable_core() {
    CORES="${CORES}${CORES:+
}$1"
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
# No journal means no keepalived: Agent recovery starts it after boot.

if [ -n "$SERVICE_TYPE" ]; then
    # ---- 单服务模式 ----
    echo "Mode: single service (${SERVICE_TYPE})"

    if [ "$SERVICE_TYPE" = "mihomo" ]; then
        generate_agent_config "mihomo" \
            "${AGENT_NAME:-mihomo-agent}" \
            "${AGENT_PORT:-8080}" \
            "${CONFIG_PATH:-/etc/mihomo/config.yaml}"
        create_default_mihomo_config "${CONFIG_PATH:-/etc/mihomo/config.yaml}"
        enable_core "mihomo=${CONFIG_PATH:-/etc/mihomo/config.yaml}"
    elif [ "$SERVICE_TYPE" = "mosdns" ]; then
        generate_agent_config "mosdns" \
            "${AGENT_NAME:-mosdns-agent}" \
            "${AGENT_PORT:-8080}" \
            "${CONFIG_PATH:-/etc/mosdns/config.yaml}"
        create_default_mosdns_config "${CONFIG_PATH:-/etc/mosdns/config.yaml}"
        enable_core "mosdns=${CONFIG_PATH:-/etc/mosdns/config.yaml}"
    elif [ "$SERVICE_TYPE" = "oxidns" ]; then
        generate_agent_config "oxidns" "${AGENT_NAME:-oxidns-agent}" "${AGENT_PORT:-8082}" "${CONFIG_PATH:-/etc/oxidns/config.yaml}"
        create_default_oxidns_config "${CONFIG_PATH:-/etc/oxidns/config.yaml}"
        enable_core "oxidns=${CONFIG_PATH:-/etc/oxidns/config.yaml}"
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
        enable_core oxidns=/etc/oxidns/config.yaml
    fi

    # Mihomo
    if [ "${ENABLE_MIHOMO}" = "true" ]; then
        echo "Mihomo is enabled."
        enable_core mihomo
    else
        echo "Mihomo is disabled."
    fi

    # MosDNS
    if [ "${ENABLE_MOSDNS}" = "true" ]; then
        echo "MosDNS is enabled."
        enable_core mosdns
    else
        echo "MosDNS is disabled."
    fi

fi

# s6 definitions for this container: the enabled cores with their Agents;
# a macvlan HA container also stops keepalived before them at shutdown.
HA_FLAG=""
if [ "${CONFIGFLOW_DNS_HA:-0}" = 1 ] && [ "${CONFIGFLOW_DNS_HA_NETWORK:-}" = macvlan ]; then
    HA_FLAG=-macvlan-ha
fi
SAVED_IFS=$IFS
IFS='
'
set -f
# shellcheck disable=SC2086 # split CORES at newlines only
/usr/local/bin/configflow-agent docker-config services $HA_FLAG $CORES
set +f
IFS=$SAVED_IFS

echo "Starting services (s6-overlay)..."
exec "$@"
