#!/bin/sh
set -e

# ==============================================================================
# ConfigFlow 3-in-1 Docker Entrypoint
# ------------------------------------------------------------------------------
# Responsibilities:
# 1. Generate agent config for Mihomo if enabled.
# 2. Generate agent config for MosDNS if enabled.
# 3. Create default config files if they don't exist.
# 4. Execute supervisord to manage services.
# ==============================================================================

# 定义supervisorctl的包装函数
SUPERVISORCTL_CMD="supervisorctl -c /etc/supervisor/supervisord.conf"

AGENT_DIR="/opt/configflow-agent"
mkdir -p "$AGENT_DIR"

echo "Starting ConfigFlow 3-in-1 Agent..."

# --- Mihomo Agent Setup ---
# 总是创建 mihomo 配置文件，即使服务被禁用
MIHOMO_CONFIG="/etc/mihomo/config.yaml"
if [ ! -f "$MIHOMO_CONFIG" ]; then
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
  listen: 0.0.0.0:1053
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
    echo "Default mihomo config created at $MIHOMO_CONFIG"
fi

if [ "${ENABLE_MIHOMO}" = "true" ]; then
    echo "Mihomo is enabled. Generating config..."
    CONFIG_FILE="${AGENT_DIR}/config-mihomo.json"

    # 以 root 用户身份写入文件
    cat > "$CONFIG_FILE" <<EOF
{
  "server_url": "${SERVER_URL}",
  "agent_name": "${AGENT_MIHOMO_NAME:-mihomo-agent}",
  "agent_host": "0.0.0.0",
  "agent_port": ${AGENT_MIHOMO_PORT:-8080},
  "agent_ip": "${AGENT_IP:-}",
  "service_type": "mihomo",
  "service_name": "mihomo",
  "config_path": "/etc/mihomo/config.yaml",
  "restart_command": "${SUPERVISORCTL_CMD} restart mihomo",
  "heartbeat_interval": ${HEARTBEAT_INTERVAL:-60}
}
EOF
    echo "Mihomo agent config generated at $CONFIG_FILE"

    # 确保 supervisor 配置中的 autostart 为 true
    if [ -f /etc/supervisor/conf.d/agent-mihomo.conf ]; then
        sed -i 's/autostart=false/autostart=true/' /etc/supervisor/conf.d/agent-mihomo.conf
    fi
    if [ -f /etc/supervisor/conf.d/mihomo.conf ]; then
        sed -i 's/autostart=false/autostart=true/' /etc/supervisor/conf.d/mihomo.conf
    fi
else
    # 如果未启用，将 supervisor 配置中的 autostart 设置为 false
    # 但不删除配置文件，这样重新启用时配置文件仍然存在
    if [ -f /etc/supervisor/conf.d/agent-mihomo.conf ]; then
        sed -i 's/autostart=true/autostart=false/' /etc/supervisor/conf.d/agent-mihomo.conf
    fi
    if [ -f /etc/supervisor/conf.d/mihomo.conf ]; then
        sed -i 's/autostart=true/autostart=false/' /etc/supervisor/conf.d/mihomo.conf
    fi
    echo "Mihomo is disabled."
fi

# --- MosDNS Agent Setup ---
# 总是创建 mosdns 配置文件，即使服务被禁用
MOSDNS_CONFIG="/etc/mosdns/config.yaml"
if [ ! -f "$MOSDNS_CONFIG" ]; then
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

if [ "${ENABLE_MOSDNS}" = "true" ]; then
    echo "MosDNS is enabled. Generating config..."
    CONFIG_FILE="${AGENT_DIR}/config-mosdns.json"

    # 以 root 用户身份写入文件
    cat > "$CONFIG_FILE" <<EOF
{
  "server_url": "${SERVER_URL}",
  "agent_name": "${AGENT_MOSDNS_NAME:-mosdns-agent}",
  "agent_host": "0.0.0.0",
  "agent_port": ${AGENT_MOSDNS_PORT:-8081},
  "agent_ip": "${AGENT_IP:-}",
  "service_type": "mosdns",
  "service_name": "mosdns",
  "config_path": "/etc/mosdns/config.yaml",
  "restart_command": "${SUPERVISORCTL_CMD} restart mosdns",
  "heartbeat_interval": ${HEARTBEAT_INTERVAL:-60}
}
EOF
    echo "MosDNS agent config generated at $CONFIG_FILE"

    # 确保 supervisor 配置中的 autostart 为 true
    if [ -f /etc/supervisor/conf.d/agent-mosdns.conf ]; then
        sed -i 's/autostart=false/autostart=true/' /etc/supervisor/conf.d/agent-mosdns.conf
    fi
    if [ -f /etc/supervisor/conf.d/mosdns.conf ]; then
        sed -i 's/autostart=false/autostart=true/' /etc/supervisor/conf.d/mosdns.conf
    fi
else
    # 如果未启用，将 supervisor 配置中的 autostart 设置为 false
    # 但不删除配置文件，这样重新启用时配置文件仍然存在
    if [ -f /etc/supervisor/conf.d/agent-mosdns.conf ]; then
        sed -i 's/autostart=true/autostart=false/' /etc/supervisor/conf.d/agent-mosdns.conf
    fi
    if [ -f /etc/supervisor/conf.d/mosdns.conf ]; then
        sed -i 's/autostart=true/autostart=false/' /etc/supervisor/conf.d/mosdns.conf
    fi
    echo "MosDNS is disabled."
fi

# 确保配置目录有正确的权限（以 root 运行，简化权限设置）
chmod -R 755 /etc/mihomo /etc/supervisor /etc/mosdns

# 显示日志目录状态
echo "Log directory status:"
ls -la /var/log/supervisor/ 2>/dev/null || true

echo "Executing supervisord..."
# 执行传递给脚本的任何命令 (例如 CMD 中的 supervisord)
exec "$@"
