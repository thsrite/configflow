#!/bin/sh
# ConfigFlow native Mihomo + OxiDNS: manage existing cores with isolated Agents.
set -eu
for cf_argument do
    case "$cf_argument" in --verbose|-v) ;; *) cf_error "未知参数：$cf_argument"; exit 2 ;; esac
done

AGENT_ROOT="/opt/configflow-agent"
MIHOMO_CONFIG_PATH={mihomo_config_path_shell}
OXIDNS_CONFIG_PATH={oxidns_config_path_shell}
MIHOMO_SERVICE_UNIT={mihomo_service_unit_shell}
OXIDNS_SERVICE_UNIT={oxidns_service_unit_shell}
MIHOMO_SERVICE_BINARY={mihomo_service_binary_shell}
OXIDNS_SERVICE_BINARY={oxidns_service_binary_shell}
STAGE_DIR=""
RESTART_ON_FAILURE=""
{core_phase_state}cleanup() {
    cleanup_status=$?
    {ipv4_forwarding_cleanup}
    if [ "$cleanup_status" -ne 0 ]; then
{core_cleanup_message}        for stopped_service in $RESTART_ON_FAILURE; do
            if [ "$INIT_SYSTEM" = systemd ]; then
                systemctl start "configflow-agent-$stopped_service" || true
            else
                rc-service "configflow-agent-$stopped_service" start 3>&- 4>&- || true
            fi
        done
    fi
    if [ -n "$STAGE_DIR" ]; then rm -rf "$STAGE_DIR"; fi
    return "$cleanup_status"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

fail() { echo "$*" >&2; exit 1; }
{ipv4_forwarding_functions}
json_string() {
    sed -n 's/.*"'"$2"'"[[:space:]]*:[[:space:]]*"\([^"\\]*\)".*/\1/p' "$1" | head -n 1
}
identity_string() {
    identity_value=$(json_string "$1" "$2")
    case "$identity_value" in *[!A-Za-z0-9_.-]*) fail "已有 Agent 身份格式无法安全保留：$1" ;; esac
    if [ -z "$identity_value" ] && grep -q '"'"$2"'"[[:space:]]*:' "$1" &&
        ! grep -Eq '"'"$2"'"[[:space:]]*:[[:space:]]*""' "$1"; then
        fail "已有 Agent 身份格式无法安全保留：$1"
    fi
    printf '%s' "$identity_value"
}
physical_file_directory() {
    resolved_file=$1
    link_count=0
    while [ -L "$resolved_file" ]; do
        link_count=$((link_count + 1))
        [ "$link_count" -le 40 ] || fail "核心路径包含过多符号链接：$1"
        link_target=$(readlink "$resolved_file") || fail "无法解析核心路径：$1"
        case "$link_target" in
            /*) resolved_file=$link_target ;;
            *) resolved_file="$(dirname "$resolved_file")/$link_target" ;;
        esac
    done
    CDPATH= cd "$(dirname "$resolved_file")" && pwd -P
}
reject_agent_directory() {
    case "$1" in
        "$PHYSICAL_AGENT_ROOT"|"$PHYSICAL_AGENT_ROOT"/*)
            fail "核心配置和核心程序不能放在 Agent 目录内，卸载 Agent 会删除该目录：$2"
            ;;
    esac
}
select_service() {
    SERVICE=$1
    AGENT_UNIT="configflow-agent-$SERVICE"
    AGENT_DIR="$AGENT_ROOT/$SERVICE"
    CONFIG_FILE="$AGENT_DIR/config.json"
    AGENT_BINARY="$AGENT_DIR/configflow-agent"
    AGENT_LOG="/var/log/configflow-agent-$SERVICE.log"
    if [ "$SERVICE" = mihomo ]; then
        CORE_CONFIG=$MIHOMO_CONFIG_PATH
        CORE_UNIT=$MIHOMO_SERVICE_UNIT
        CORE_BINARY=$MIHOMO_SERVICE_BINARY
    else
        CORE_CONFIG=$OXIDNS_CONFIG_PATH
        CORE_UNIT=$OXIDNS_SERVICE_UNIT
        CORE_BINARY=$OXIDNS_SERVICE_BINARY
    fi
}
stop_existing_agent() {
    [ -f "$CONFIG_FILE" ] || return 0
    if [ "$INIT_SYSTEM" = systemd ]; then
        if systemctl is-active --quiet "$AGENT_UNIT"; then RESTART_ON_FAILURE="$RESTART_ON_FAILURE $SERVICE"; fi
        systemctl stop "$AGENT_UNIT"
    else
        if rc-service "$AGENT_UNIT" status >/dev/null 2>&1; then RESTART_ON_FAILURE="$RESTART_ON_FAILURE $SERVICE"; fi
        rc-service "$AGENT_UNIT" stop
    fi
}

[ "$(id -u)" -eq 0 ] || fail "请使用 root 用户运行此脚本"
{init_detection}

{agent_install_dependencies}
{installation_description}
{tun_prepare_notice}
{ipv4_forwarding_notice}
if [ "${_CF_NESTED:-0}" -eq 0 ]; then
    cf_ui "1) {installation_action}"
    cf_ui "2) 卸载两个 Agent（保留两个核心、核心配置及发布记录）"
    cf_ui "3) 取消"
    cf_prompt '请输入选项 [1-3]: '
fi
read -r choice < /dev/tty
case "$choice" in
    1)
        cf_step "检查安装环境与依赖"
{agent_arch_detection}
        agent_ensure_dependencies zoneinfo{ipv4_forwarding_dependencies}
        cf_step_done "安装环境与依赖已就绪"
        ;;
    2) cf_step "卸载 Agent" ;;
    *) cf_complete "已取消"; exit 0 ;;
esac

# A legacy Agent owns a shared binary and directory. Do not guess how to split
# its identity or let either installer delete another instance's state.
[ ! -L "$AGENT_ROOT" ] || fail "Agent 根目录不能是符号链接：$AGENT_ROOT"
for legacy in "$AGENT_ROOT"/config*.json /etc/systemd/system/configflow-agent.service /etc/init.d/configflow-agent /usr/local/bin/configflow-agent; do
    if [ -e "$legacy" ] || [ -L "$legacy" ]; then
        fail "检测到旧版共享 Agent 部署：${legacy}。请先卸载旧 Agent 并保留核心配置，再运行同机安装；本次未修改服务。"
    fi
done
for service in mihomo oxidns; do
    select_service "$service"
    [ ! -L "$AGENT_DIR" ] && [ ! -L "$CONFIG_FILE" ] && [ ! -L "$AGENT_BINARY" ] || fail "独立 Agent 路径不能是符号链接：$AGENT_DIR"
    [ ! -L "/etc/systemd/system/$AGENT_UNIT.service" ] && [ ! -L "/etc/init.d/$AGENT_UNIT" ] || fail "独立 Agent 服务文件不能是符号链接：$AGENT_UNIT"
    if [ -e "$AGENT_DIR" ] && [ ! -d "$AGENT_DIR" ]; then fail "Agent 路径不是目录：$AGENT_DIR"; fi
    if [ -f "$CONFIG_FILE" ]; then
        [ "$(json_string "$CONFIG_FILE" service_type)" = "$SERVICE" ] || fail "已有 Agent 类型不匹配：$CONFIG_FILE"
        [ "$(json_string "$CONFIG_FILE" agent_service_unit)" = "$AGENT_UNIT" ] || fail "已有 Agent 服务标识不匹配：$CONFIG_FILE"
        grep -Eq '"native_aio"[[:space:]]*:[[:space:]]*true([[:space:],}]|$)' "$CONFIG_FILE" || fail "此目录不属于原生同机安装：$AGENT_DIR"
    elif [ -e "$AGENT_BINARY" ] || [ -e "/etc/systemd/system/$AGENT_UNIT.service" ] || [ -e "/etc/init.d/$AGENT_UNIT" ]; then
        fail "已有独立 Agent 缺少可核对的配置：${AGENT_DIR}；请先检查旧部署。"
    fi
done

case "$choice" in
    1) ;;
    2)
        {ipv4_forwarding_ownership}
        # Check BOTH journals before stopping either Agent. Do not auto-recover
        # a core merely because its management Agent is being removed.
        for service in mihomo oxidns; do
            select_service "$service"
            if [ -e "$AGENT_DIR/linux-gateway.json" ] || [ -L "$AGENT_DIR/linux-gateway.json" ]; then
                fail "请先恢复客户端的网关和 DNS，再到 Linux 网关设置中撤销网关转发；网关恢复记录未清理，不能卸载 Agent。"
            fi
            if [ -f "$CONFIG_FILE" ]; then
                [ -x "$AGENT_BINARY" ] || fail "缺少检查发布记录的 Agent：$AGENT_BINARY"
                "$AGENT_BINARY" -config "$CONFIG_FILE" -require-clean-deployments
            fi
        done
        for service in mihomo oxidns; do
            select_service "$service"
            stop_existing_agent
        done
        {ipv4_forwarding_ownership}
        for service in mihomo oxidns; do
            select_service "$service"
            if [ -e "$AGENT_DIR/linux-gateway.json" ] || [ -L "$AGENT_DIR/linux-gateway.json" ]; then
                fail "请先恢复客户端的网关和 DNS，再到 Linux 网关设置中撤销网关转发；网关恢复记录未清理，不能卸载 Agent。"
            fi
            if [ -f "$CONFIG_FILE" ]; then
                "$AGENT_BINARY" -config "$CONFIG_FILE" -require-clean-deployments
            fi
        done
        for service in mihomo oxidns; do
            select_service "$service"
            [ -f "$CONFIG_FILE" ] || continue
            if [ "$INIT_SYSTEM" = systemd ]; then
                systemctl disable "$AGENT_UNIT" 2>/dev/null || true
                rm -f "/etc/systemd/system/$AGENT_UNIT.service"
            else
                # Not being in the runlevel (an install that failed before
                # enabling it) must not leave the uninstall stuck halfway.
                rc-update del "$AGENT_UNIT" default 2>/dev/null || true
                rm -f "/etc/init.d/$AGENT_UNIT"
            fi
            rm -rf "$AGENT_DIR"
            rm -f "$AGENT_LOG" "$AGENT_LOG".* "/etc/logrotate.d/$AGENT_UNIT"
        done
        if [ "$INIT_SYSTEM" = systemd ]; then systemctl daemon-reload; else rc-update -u; fi
        cf_complete "两个 Agent 已卸载；核心服务、配置及发布记录未改动。"
        {ipv4_forwarding_uninstall}
        exit 0
        ;;
    *) cf_complete "已取消"; exit 0 ;;
esac

{ipv4_forwarding_ownership}
{core_install_phase}# Read-only core checks precede even the Agent download. The existing service
# manager remains the only owner of each core process.
cf_step "检查核心接管与下载 Agent"
PHYSICAL_AGENT_ROOT="$(CDPATH= cd "$(dirname "$AGENT_ROOT")" && pwd -P)/$(basename "$AGENT_ROOT")"
for service in mihomo oxidns; do
    select_service "$service"
    [ -f "$CORE_CONFIG" ] || fail "$SERVICE 核心配置不存在：${CORE_CONFIG}；请先安装核心。"
    CORE_DIRECTORY=$(physical_file_directory "$CORE_CONFIG")
    reject_agent_directory "$CORE_DIRECTORY" "$CORE_CONFIG"
    if [ "$SERVICE" = mihomo ]; then
        MIHOMO_DIRECTORY=$CORE_DIRECTORY
    elif [ "$CORE_DIRECTORY" = "$MIHOMO_DIRECTORY" ]; then
        fail "两个核心配置实际位于同一目录，会共用发布记录；请使用独立目录。"
    fi
    CORE_EXECUTABLE=$(command -v "$CORE_BINARY") || fail "$SERVICE 核心程序不存在；请先安装核心或填写正确路径。"
    [ -f "$CORE_EXECUTABLE" ] || fail "$SERVICE 核心程序必须是可执行文件。"
    CORE_BINARY_DIRECTORY=$(physical_file_directory "$CORE_EXECUTABLE")
    reject_agent_directory "$CORE_BINARY_DIRECTORY" "$CORE_EXECUTABLE"
    if [ "$INIT_SYSTEM" = systemd ]; then
        [ "$(systemctl show "$CORE_UNIT" --property=LoadState --value)" = loaded ] || fail "$SERVICE 核心服务未安装：$CORE_UNIT"
    else
        [ -f "/etc/init.d/$CORE_UNIT" ] || fail "$SERVICE 核心服务未安装：$CORE_UNIT"
    fi
done

umask 077
mkdir -p "$AGENT_ROOT"
STAGE_DIR=$(mktemp -d "$AGENT_ROOT/.aio-install.XXXXXX")
BINARY_TMP="$STAGE_DIR/configflow-agent"
BINARY_BASE={binary_download_url_shell}
BINARY_URL="$BINARY_BASE/configflow-agent-$AGENT_ARCH"
if command -v wget >/dev/null 2>&1; then
    wget -q -O "$BINARY_TMP" "$BINARY_URL" || fail "Agent 二进制下载失败"
elif command -v curl >/dev/null 2>&1; then
    curl -fsL -o "$BINARY_TMP" "$BINARY_URL" || fail "Agent 二进制下载失败"
else
    fail "安装阶段需要 wget 或 curl 下载二进制"
fi
chmod 755 "$BINARY_TMP"
"$BINARY_TMP" -version >/dev/null
cat > "$STAGE_DIR/mihomo-config.json" <<'CONFIGFLOW_MIHOMO_JSON_EOF'
{mihomo_config_json}
CONFIGFLOW_MIHOMO_JSON_EOF
cat > "$STAGE_DIR/oxidns-config.json" <<'CONFIGFLOW_OXIDNS_JSON_EOF'
{oxidns_config_json}
CONFIGFLOW_OXIDNS_JSON_EOF

for service in mihomo oxidns; do
    select_service "$service"
    EXISTING_ID=""
    EXISTING_TOKEN=""
    if [ -f "$CONFIG_FILE" ]; then
        EXISTING_ID=$(identity_string "$CONFIG_FILE" agent_id)
        EXISTING_TOKEN=$(identity_string "$CONFIG_FILE" token)
    fi
    if [ "$INIT_SYSTEM" = systemd ]; then RESTART="systemctl restart $CORE_UNIT"; else RESTART="rc-service $CORE_UNIT restart"; fi
    CANDIDATE="$STAGE_DIR/$SERVICE-config.json"
    sed -i "s/__CONFIGFLOW_INIT__/$INIT_SYSTEM/g; s/__CONFIGFLOW_RESTART__/$RESTART/g; s/__CONFIGFLOW_AGENT_ID__/$EXISTING_ID/g; s/__CONFIGFLOW_TOKEN__/$EXISTING_TOKEN/g" "$CANDIDATE"
    "$BINARY_TMP" -config "$CANDIDATE" -validate-service
    if [ -f "$CONFIG_FILE" ]; then
        "$BINARY_TMP" -config "$CONFIG_FILE" -require-clean-deployments
    fi
    "$BINARY_TMP" -config "$CANDIDATE" -require-clean-deployments
done
cf_step_done "核心接管检查与 Agent 下载校验通过"

# Only explicitly enabled TUN is prepared. Keep a loaded module if a later
# check fails: unloading it could disrupt an existing core using the driver.
{tun_prepare}

# Inspect the pair together so the environment check can distinguish their
# listeners from unrelated port owners. Diagnostics include repair guidance;
# the check never changes capabilities, forwarding or either core config.
cf_step "检查运行环境"
{ipv4_forwarding_environment}if cf_environment_check "$BINARY_TMP" -config "$STAGE_DIR/mihomo-config.json" \
    -environment-peer-config "$STAGE_DIR/oxidns-config.json" -check-environment; then
    :
else
    environment_status=$?
    echo "若提示未知 -check-environment 参数，请更新 ConfigFlow 并重新构建/提供 1.8.19-go 或更高的 Agent 下载文件；否则按上方环境问题修复后重跑。" >&2
    exit "$environment_status"
fi
{ipv4_forwarding_environment_end}cf_step_done "运行环境检查通过"

{ipv4_forwarding_prepare}

# Both candidates, existing roots and environment passed. No Agent is stopped earlier.
cf_step "安装并启动 Agent 服务"
for service in mihomo oxidns; do
    select_service "$service"
    stop_existing_agent
done
# Recheck after stopping: the first checks are snapshots, and stopping an Agent
# can interrupt a publication begun in the meantime. This is not a publication
# lock. On detection, restart the previously running Agents without replacing
# candidate files or automatically recovering the core.
for service in mihomo oxidns; do
    select_service "$service"
    if [ -f "$CONFIG_FILE" ]; then
        "$BINARY_TMP" -config "$CONFIG_FILE" -require-clean-deployments
    fi
    "$BINARY_TMP" -config "$STAGE_DIR/$SERVICE-config.json" -require-clean-deployments
done
{ipv4_forwarding_ownership}
for service in mihomo oxidns; do
    select_service "$service"
    mkdir -p "$AGENT_DIR"
    chmod 700 "$AGENT_DIR"
    cp "$BINARY_TMP" "$AGENT_DIR/.configflow-agent.new"
    chmod 755 "$AGENT_DIR/.configflow-agent.new"
    mv "$AGENT_DIR/.configflow-agent.new" "$AGENT_BINARY"
    mv "$STAGE_DIR/$SERVICE-config.json" "$CONFIG_FILE"
    chmod 600 "$CONFIG_FILE"
    IPV4_SYSTEMD_PRE=""
    IPV4_OPENRC_PRE=""
    {ipv4_forwarding_hook}
    if command -v logrotate >/dev/null 2>&1; then
        mkdir -p /etc/logrotate.d
        cat > "/etc/logrotate.d/$AGENT_UNIT" <<LOGEOF
$AGENT_LOG {
    rotate 3
    compress
    delaycompress
    missingok
    notifempty
    size 10M
    copytruncate
}
LOGEOF
    fi
    if [ "$INIT_SYSTEM" = systemd ]; then
        cat > "/etc/systemd/system/$AGENT_UNIT.service" <<SERVICEEOF
[Unit]
Description=ConfigFlow Agent ($SERVICE)
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=$AGENT_DIR
$IPV4_SYSTEMD_PRE
ExecStart=$AGENT_BINARY -config $CONFIG_FILE
StandardOutput=append:$AGENT_LOG
StandardError=append:$AGENT_LOG
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
SERVICEEOF
    else
        cat > "/etc/init.d/$AGENT_UNIT" <<SERVICEEOF
#!/sbin/openrc-run
name="ConfigFlow Agent ($SERVICE)"
command="$AGENT_BINARY"
command_args="-config $CONFIG_FILE"
# Restart a crashed Agent, as Restart=always does under systemd.
supervisor="supervise-daemon"
respawn_delay=10
respawn_max=0
pidfile="/run/$AGENT_UNIT.pid"
output_log="$AGENT_LOG"
error_log="$AGENT_LOG"
directory="$AGENT_DIR"
depend() {
    need net
    after firewall
}
start_pre() {
    $IPV4_OPENRC_PRE
    checkpath --directory --mode 0755 /var/log
    checkpath --file --mode 0644 --owner root:root "$AGENT_LOG"
}
SERVICEEOF
        chmod 755 "/etc/init.d/$AGENT_UNIT"
    fi
done
if [ "$INIT_SYSTEM" = systemd ]; then systemctl daemon-reload; else rc-update -u; fi
for service in mihomo oxidns; do
    select_service "$service"
    if [ "$INIT_SYSTEM" = systemd ]; then
        systemctl enable "$AGENT_UNIT"
        systemctl start "$AGENT_UNIT"
    else
        rc-update add "$AGENT_UNIT" default
        # supervise-daemon keeps inherited descriptors: close the report fds,
        # or a piped or non-interactive SSH run would never return.
        rc-service "$AGENT_UNIT" start 3>&- 4>&-
    fi
done
# An Agent that exits right after starting must not count as started.
sleep 2
for service in mihomo oxidns; do
    select_service "$service"
    if [ "$INIT_SYSTEM" = systemd ]; then
        systemctl is-active --quiet "$AGENT_UNIT" || fail "$SERVICE Agent 启动后未保持运行，请查看 journalctl -u $AGENT_UNIT 或 $AGENT_LOG。"
    else
        # supervise-daemon stays "started" while it waits to respawn an Agent
        # that exited, so also require the child of the supervisor in its pidfile.
        agent_supervisor=$(cat "/run/$AGENT_UNIT.pid" 2>/dev/null || true)
        rc-service "$AGENT_UNIT" status &&
            grep -qs "^PPid:[[:space:]]*${agent_supervisor:-missing}\$" /proc/[0-9]*/status ||
            fail "$SERVICE Agent 启动后未保持运行，请查看 $AGENT_LOG。"
    fi
done
{ipv4_forwarding_apply}
RESTART_ON_FAILURE=""
cf_step_done "两个 Agent 服务已启动"
{ipv4_forwarding_summary}
cf_fact agent_mihomo "Mihomo Agent：端口 {mihomo_agent_port}，服务已启动"
cf_fact agent_oxidns "OxiDNS Agent：端口 {oxidns_agent_port}，服务已启动"
if [ "$(cf_fact_read core_mihomo_new)" != yes ] || [ "$(cf_fact_read core_oxidns_new)" != yes ]; then
    cf_note dns_existing "已有核心配置已保留；发布也会保留自定义 DNS 端口和 dns-hijack，请将 OxiDNS 上游与 Mihomo 实际 DNS 监听地址及端口对应。"
fi
cf_complete "安装完成"
