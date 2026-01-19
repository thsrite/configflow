#!/bin/sh
set -e

# ==============================================================================
# ConfigFlow Go Agent Docker Entrypoint
# ------------------------------------------------------------------------------
# Responsibilities:
# 1. Generate agent config based on environment variables.
# 2. Start the Go Agent binary.
# ==============================================================================

# 检查必需的环境变量
if [ -z "$SERVER_URL" ]; then
    echo "错误: SERVER_URL 环境变量未设置"
    echo "示例: SERVER_URL=http://your-server:80"
    exit 1
fi

if [ -z "$AGENT_NAME" ]; then
    echo "错误: AGENT_NAME 环境变量未设置"
    echo "示例: AGENT_NAME=my-agent"
    exit 1
fi

if [ -z "$SERVICE_TYPE" ]; then
    echo "错误: SERVICE_TYPE 环境变量未设置"
    echo "示例: SERVICE_TYPE=mihomo 或 SERVICE_TYPE=mosdns"
    exit 1
fi

# 设置默认值
SERVICE_NAME=${SERVICE_NAME:-$SERVICE_TYPE}
CONFIG_PATH=${CONFIG_PATH:-"/etc/${SERVICE_TYPE}/config.yaml"}
RESTART_COMMAND=${RESTART_COMMAND:-"supervisorctl -c /etc/supervisor/supervisord.conf restart ${SERVICE_TYPE}"}
HEARTBEAT_INTERVAL=${HEARTBEAT_INTERVAL:-30}
AGENT_HOST=${AGENT_HOST:-"0.0.0.0"}
AGENT_PORT=${AGENT_PORT:-8080}
AGENT_IP=${AGENT_IP:-""}

# 生成配置文件
CONFIG_FILE="/opt/configflow-agent/config-${SERVICE_TYPE}.json"
mkdir -p /opt/configflow-agent

cat > $CONFIG_FILE <<EOF
{
  "server_url": "$SERVER_URL",
  "agent_name": "$AGENT_NAME",
  "agent_host": "$AGENT_HOST",
  "agent_port": $AGENT_PORT,
  "agent_ip": "$AGENT_IP",
  "service_type": "$SERVICE_TYPE",
  "service_name": "$SERVICE_NAME",
  "config_path": "$CONFIG_PATH",
  "restart_command": "$RESTART_COMMAND",
  "heartbeat_interval": $HEARTBEAT_INTERVAL
}
EOF

echo "===================================="
echo "ConfigFlow Agent Docker (Go Version)"
echo "===================================="
echo "Agent 版本: 2.0.0 (Go)"
echo "服务器地址: $SERVER_URL"
echo "Agent 名称: $AGENT_NAME"
echo "服务类型: $SERVICE_TYPE"
echo "监听地址: $AGENT_HOST:$AGENT_PORT"
echo "心跳间隔: ${HEARTBEAT_INTERVAL}秒"
echo "配置文件: $CONFIG_FILE"
echo "===================================="

# 启动 Go Agent
exec /usr/local/bin/configflow-agent -config $CONFIG_FILE
