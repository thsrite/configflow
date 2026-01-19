#!/bin/sh
set -e

# ==============================================================================
# ConfigFlow MosDNS Agent Docker Entrypoint
# ------------------------------------------------------------------------------
# Responsibilities:
# 1. Generate agent config for MosDNS.
# 2. Execute supervisord to manage services.
# ==============================================================================

# 定义supervisorctl的包装函数
SUPERVISORCTL_CMD="supervisorctl -c /etc/supervisor/supervisord.conf"

AGENT_DIR="/opt/configflow-agent"
mkdir -p "$AGENT_DIR"

echo "Starting ConfigFlow MosDNS Agent..."

# --- MosDNS Agent Setup ---
echo "Generating MosDNS agent config..."
CONFIG_FILE="${AGENT_DIR}/config-mosdns.json"

# 以 root 用户身份写入文件
cat > "$CONFIG_FILE" <<EOF
{
  "server_url": "${SERVER_URL}",
  "agent_name": "${AGENT_NAME:-mosdns-agent}",
  "agent_host": "${AGENT_HOST:-0.0.0.0}",
  "agent_port": ${AGENT_PORT:-8080},
  "agent_ip": "${AGENT_IP:-}",
  "service_type": "mosdns",
  "service_name": "${SERVICE_NAME:-mosdns}",
  "config_path": "${CONFIG_PATH:-/etc/mosdns/config.yaml}",
  "restart_command": "${RESTART_COMMAND:-${SUPERVISORCTL_CMD} restart mosdns}",
  "heartbeat_interval": ${HEARTBEAT_INTERVAL:-60}
}
EOF
echo "MosDNS config generated at $CONFIG_FILE"

# 检查 mosdns 配置文件是否存在，不存在则创建默认配置
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

# 确保配置目录有正确的权限（以 root 运行，简化权限设置）
chmod -R 755 /etc/mosdns /etc/supervisor

# 显示日志目录状态
echo "Log directory status:"
ls -la /var/log/supervisor/ 2>/dev/null || true

echo "Executing supervisord..."
# 执行传递给脚本的任何命令 (例如 CMD 中的 supervisord)
exec "$@"
