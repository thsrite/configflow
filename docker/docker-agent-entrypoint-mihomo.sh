#!/bin/sh
set -e

# ==============================================================================
# ConfigFlow Mihomo Agent Docker Entrypoint
# ------------------------------------------------------------------------------
# Responsibilities:
# 1. Generate agent config for Mihomo.
# 2. Execute supervisord to manage services.
# ==============================================================================

# 定义supervisorctl的包装函数
SUPERVISORCTL_CMD="supervisorctl -c /etc/supervisor/supervisord.conf"

AGENT_DIR="/opt/configflow-agent"
mkdir -p "$AGENT_DIR"

echo "Starting ConfigFlow Mihomo Agent..."

# --- Mihomo Agent Setup ---
echo "Generating Mihomo agent config..."
CONFIG_FILE="${AGENT_DIR}/config-mihomo.json"

# 以 root 用户身份写入文件
cat > "$CONFIG_FILE" <<EOF
{
  "server_url": "${SERVER_URL}",
  "agent_name": "${AGENT_NAME:-mihomo-agent}",
  "agent_host": "${AGENT_HOST:-0.0.0.0}",
  "agent_port": ${AGENT_PORT:-8080},
  "agent_ip": "${AGENT_IP:-}",
  "service_type": "mihomo",
  "service_name": "${SERVICE_NAME:-mihomo}",
  "config_path": "${CONFIG_PATH:-/etc/mihomo/config.yaml}",
  "restart_command": "${RESTART_COMMAND:-${SUPERVISORCTL_CMD} restart mihomo}",
  "heartbeat_interval": ${HEARTBEAT_INTERVAL:-60}
}
EOF
echo "Mihomo config generated at $CONFIG_FILE"

# 检查 mihomo 配置文件是否存在，不存在则创建默认配置
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
    echo "Default mihomo config created at $MIHOMO_CONFIG"
fi

# 确保配置目录有正确的权限（以 root 运行，简化权限设置）
chmod -R 755 /etc/mihomo /etc/supervisor

# 显示日志目录状态
echo "Log directory status:"
ls -la /var/log/supervisor/ 2>/dev/null || true

echo "Executing supervisord..."
# 执行传递给脚本的任何命令 (例如 CMD 中的 supervisord)
exec "$@"
