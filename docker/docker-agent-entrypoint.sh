#!/bin/bash
set -e

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
RESTART_COMMAND=${RESTART_COMMAND:-"echo 'Restart not configured'"}
HEARTBEAT_INTERVAL=${HEARTBEAT_INTERVAL:-30}
AGENT_HOST=${AGENT_HOST:-"0.0.0.0"}
AGENT_PORT=${AGENT_PORT:-8080}
AGENT_IP=${AGENT_IP:-""}

# 生成 config.json
cat > /opt/configflow-agent/config.json <<EOF
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
echo "ConfigFlow Agent Docker 版本"
echo "===================================="
echo "Agent 版本: 1.0.0"
echo "服务器地址: $SERVER_URL"
echo "Agent 名称: $AGENT_NAME"
echo "服务类型: $SERVICE_TYPE"
echo "监听地址: $AGENT_HOST:$AGENT_PORT"
echo "心跳间隔: ${HEARTBEAT_INTERVAL}秒"
echo "===================================="

# 启动 agent
exec /opt/configflow-agent/agent.sh
