#!/bin/sh
# ConfigFlow Go Agent: native Shell installation, systemd / OpenRC.
# Service: {service_type}; Agent: {agent_name}
set -eu
{install_arguments}

AGENT_DIR="/opt/configflow-agent"
CONFIG_FILE="$AGENT_DIR/config-{service_type}.json"
RECOVERY_SERVICE="configflow-recover-{service_type}"
SERVICE_UNIT="{service_unit}"

if [ "$(id -u)" -ne 0 ]; then
    echo "请使用 root 用户运行此脚本"
    exit 1
fi
{init_detection}
BINARY_TMP=""
RESTART_PREVIOUS_AGENT=0
{core_phase_state}{ipv4_forwarding_state}# One cleanup for every exit: a signal must stop the installer instead of
# letting it continue without its staged files, and an Agent that was running
# before a failed upgrade is started again.
configflow_agent_cleanup() {
    agent_status=$?
    trap - 0 HUP INT TERM
    if [ -n "$BINARY_TMP" ]; then rm -f "$BINARY_TMP" || true; fi
{ipv4_forwarding_cleanup}    if [ "$agent_status" -ne 0 ]; then
        if [ "$RESTART_PREVIOUS_AGENT" = 1 ]; then
            echo "安装未完成，正在重新启动原先运行的 Agent。" >&2
            if [ "$INIT_SYSTEM" = "systemd" ]; then
                systemctl start configflow-agent || true
            else
                rc-service configflow-agent start 3>&- 4>&- || true
            fi
        fi
{core_cleanup_message}    fi
    exit "$agent_status"
}
trap configflow_agent_cleanup 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

{agent_install_dependencies}
{dns_ha_functions}{ipv4_forwarding_notice}{installation_choice}
case "$choice" in
    1)
        cf_step "检查安装环境与依赖"
        case "$(uname -m)" in
            x86_64|amd64) AGENT_ARCH="linux-amd64" ;;
            aarch64|arm64) AGENT_ARCH="linux-arm64" ;;
            armv7l|armhf) AGENT_ARCH="linux-armv7" ;;
            *) echo "不支持的 CPU 架构"; exit 1 ;;
        esac
        agent_ensure_dependencies{core_dependencies}{ipv4_forwarding_dependencies}
        cf_step_done "安装环境与依赖已就绪"
        ;;
    2) cf_step "卸载 Agent" ;;
    *) cf_complete "已取消"; exit 0 ;;
esac

# Remove only this installation's recovery dependency, retaining unrelated unit
# settings and custom drop-ins. Joining continuations also handles edited units.
remove_systemd_recovery_dependency() {
    dependency_file=$1
    [ -f "$dependency_file" ] || return 0
    dependency_tmp=$(mktemp "${dependency_file}.configflow-cleanup.XXXXXX")
    cp -p "$dependency_file" "$dependency_tmp"
    dependency_status=0
    awk -v gate="${RECOVERY_SERVICE}.service" '
        function emit(    line, key, value, count, fields, output, changed, i) {
            line = logical
            if (line ~ /^[ \t]*\[/) {
                section = line
                sub(/^[ \t]*/, "", section)
                sub(/[ \t]*$/, "", section)
            }
            if (section == "[Unit]" && line ~ /^[ \t]*(Requires|Requisite|Wants|BindsTo|PartOf|Upholds|After|Before)[ \t]*=/) {
                key = line
                sub(/=.*/, "", key)
                value = substr(line, index(line, "=") + 1)
                count = split(value, fields, /[ \t]+/)
                output = ""
                changed = 0
                for (i = 1; i <= count; i++) {
                    if (fields[i] == gate) changed = 1
                    else if (fields[i] != "") output = output (output == "" ? "" : " ") fields[i]
                }
                if (changed) {
                    modified = 1
                    if (output != "") print key "=" output
                    return
                }
            }
            printf "%s", original
        }
        {
            original = original $0 "\n"
            fragment = $0
            if (sub(/\\$/, "", fragment)) {
                logical = logical fragment " "
                next
            }
            logical = logical fragment
            emit()
            logical = original = ""
        }
        END {
            if (original != "") emit()
            if (!modified) exit 3
        }
    ' "$dependency_file" > "$dependency_tmp" || dependency_status=$?
    if [ "$dependency_status" -ne 0 ]; then
        rm -f "$dependency_tmp"
        [ "$dependency_status" -eq 3 ] && return 0
        return "$dependency_status"
    fi
    if [ -L "$dependency_file" ]; then
        rm -f "$dependency_tmp"
        echo "恢复依赖位于符号链接服务文件，请在源文件中清理：$dependency_file" >&2
        return 1
    fi
    mv "$dependency_tmp" "$dependency_file"
    case "$dependency_file" in
        */configflow-recovery.conf)
            # Drop the old generated file only when it has no other settings.
            if awk '/^[ \t]*($|[#;]|\[)/ { next } { found = 1 } END { exit found }' "$dependency_file"; then
                rm -f "$dependency_file"
            fi
            ;;
    esac
}

remove_openrc_recovery_dependency() {
    dependency_file=$1
    [ -f "$dependency_file" ] || return 0
    dependency_tmp=$(mktemp "${dependency_file}.configflow-cleanup.XXXXXX")
    cp -p "$dependency_file" "$dependency_tmp"
    dependency_status=0
    awk -v gate="$RECOVERY_SERVICE" '
        $0 == "# BEGIN CONFIGFLOW RECOVERY {service_type}" || $0 == "# END CONFIGFLOW RECOVERY {service_type}" { modified = 1; next }
        /^[ \t]*(need[ \t]|rc_need=)/ {
            line = $0
            offset = 1
            while ((at = index(substr(line, offset), gate)) != 0) {
                at += offset - 1
                before = at == 1 ? "" : substr(line, at - 1, 1)
                after = substr(line, at + length(gate), 1)
                if ((before == "" || before ~ /[ \t"\047]/) && (after == "" || after ~ /[ \t"\047;]/)) {
                    line = substr(line, 1, at - 1) substr(line, at + length(gate))
                    modified = 1
                    offset = at
                } else offset = at + length(gate)
            }
            if (line ~ /^[ \t]*need[ \t]*$/ || line ~ /^[ \t]*rc_need="\$\{rc_need:-\}[ \t]*"[ \t]*$/) next
            print line
            next
        }
        { print }
        END { if (!modified) exit 3 }
    ' "$dependency_file" > "$dependency_tmp" || dependency_status=$?
    if [ "$dependency_status" -ne 0 ]; then
        rm -f "$dependency_tmp"
        [ "$dependency_status" -eq 3 ] && return 0
        return "$dependency_status"
    fi
    if [ -L "$dependency_file" ]; then
        rm -f "$dependency_tmp"
        echo "恢复依赖位于符号链接服务文件，请在源文件中清理：$dependency_file" >&2
        return 1
    fi
    mv "$dependency_tmp" "$dependency_file"
}

remove_recovery_gate() {
    previous_unit="$SERVICE_UNIT"
    if [ -f "$CONFIG_FILE" ]; then
        configured_unit=$(sed -n 's/.*"service_unit"[[:space:]]*:[[:space:]]*"\([A-Za-z0-9_.@-]*\)".*/\1/p' "$CONFIG_FILE" | head -n 1)
        if [ -n "$configured_unit" ]; then
            previous_unit=${configured_unit%.service}
        fi
    fi
    if [ "$INIT_SYSTEM" = "systemd" ]; then
        for cleanup_unit in "$SERVICE_UNIT" "$previous_unit" configflow-agent; do
            remove_systemd_recovery_dependency "/etc/systemd/system/${cleanup_unit}.service"
            for dependency_file in /etc/systemd/system/"${cleanup_unit}.service.d/"*.conf; do
                remove_systemd_recovery_dependency "$dependency_file"
            done
        done
        # Reload the detached units BEFORE deleting the gate. Never stop it:
        # stopping a still-required gate can propagate a stop to a running core.
        systemctl daemon-reload
        rm -f "/etc/systemd/system/${RECOVERY_SERVICE}.service"
        systemctl daemon-reload
    else
        for cleanup_unit in "$SERVICE_UNIT" "$previous_unit" configflow-agent; do
            remove_openrc_recovery_dependency "/etc/conf.d/$cleanup_unit"
            remove_openrc_recovery_dependency "/etc/init.d/$cleanup_unit"
        done
        rc-update -u
        rc-update del "$RECOVERY_SERVICE" boot 2>/dev/null || true
        rm -f "/etc/init.d/$RECOVERY_SERVICE"
        rc-update -u
    fi
}

{ipv4_forwarding_functions}# One native installer owns the legacy configflow-agent unit and shared binary.
# Refuse a cross-core overwrite; Docker supports separate core/Agent instances.
for isolated_dir in "$AGENT_DIR/mihomo" "$AGENT_DIR/oxidns"; do
    if [ -e "$isolated_dir" ] || [ -L "$isolated_dir" ]; then
        echo "检测到原生同机双 Agent：${isolated_dir}。请使用同机安装器管理；单服务安装/卸载不会覆盖或删除它。" >&2
        exit 1
    fi
done
for other_config in "$AGENT_DIR"/config*.json; do
    [ -f "$other_config" ] || continue
    other_type=$(sed -n 's/.*"service_type"[[:space:]]*:[[:space:]]*"\([A-Za-z0-9_-]*\)".*/\1/p' "$other_config" | head -n 1)
    if [ -n "$other_type" ] && [ "$other_type" != "{service_type}" ]; then
        echo "本机已有 $other_type Agent。请先卸载原 Agent，或使用独立 Docker 实例，避免覆盖其服务与凭据。" >&2
        exit 1
    fi
done

case "$choice" in
    1) ;;
    2)
        # Preserve the only ownership/recovery record while gateway rules exist.
        if [ -e "$AGENT_DIR/linux-gateway.json" ] || [ -L "$AGENT_DIR/linux-gateway.json" ]; then
            echo "请先恢复客户端的网关和 DNS，再到 Linux 网关设置中撤销网关转发；网关恢复记录未清理，不能卸载 Agent。" >&2
            exit 1
        fi
        # Restore before removing the only binary able to recover the journal.
        if [ -f "$CONFIG_FILE" ] && [ -x /usr/local/bin/configflow-agent ]; then
            /usr/local/bin/configflow-agent -config "$CONFIG_FILE" -recover-and-exit
        fi
        remove_recovery_gate
        if [ "$INIT_SYSTEM" = "systemd" ]; then
            systemctl stop configflow-agent 2>/dev/null || true
            if [ -e "$AGENT_DIR/linux-gateway.json" ] || [ -L "$AGENT_DIR/linux-gateway.json" ]; then
                systemctl start configflow-agent 2>/dev/null || true
                echo "卸载期间出现网关恢复记录；已保留 Agent，请先恢复客户端和所选设备网络。" >&2
                exit 1
            fi
            systemctl disable configflow-agent 2>/dev/null || true
            rm -f /etc/systemd/system/configflow-agent.service
            systemctl daemon-reload
        else
            rc-service configflow-agent stop 2>/dev/null || true
            if [ -e "$AGENT_DIR/linux-gateway.json" ] || [ -L "$AGENT_DIR/linux-gateway.json" ]; then
                rc-service configflow-agent start 2>/dev/null || true
                echo "卸载期间出现网关恢复记录；已保留 Agent，请先恢复客户端和所选设备网络。" >&2
                exit 1
            fi
            rc-update del configflow-agent default 2>/dev/null || true
            rm -f /etc/init.d/configflow-agent
            rc-update -u
        fi
        rm -f /usr/local/bin/configflow-agent
        rm -rf "$AGENT_DIR"
        rm -f /var/log/configflow-agent.log /var/log/configflow-agent.log.* /etc/logrotate.d/configflow-agent
{ipv4_forwarding_uninstall}        cf_complete "Agent 已卸载，服务配置与发布备份已保留。"
        exit 0
        ;;
    *) cf_complete "已取消"; exit 0 ;;
esac

{dns_ha_prepare}{core_install_phase}BINARY_BASE={binary_download_url_shell}
BINARY_URL="$BINARY_BASE/configflow-agent-$AGENT_ARCH"
# /tmp is often mounted noexec. Stage beside the final executable so validation
# works and replacement is an atomic rename on the same filesystem.
mkdir -p /usr/local/bin
BINARY_TMP=$(mktemp /usr/local/bin/.configflow-agent.install.XXXXXX)
cf_step "下载并校验 Agent"
# The download URL may carry credentials; do not print it.
if command -v wget >/dev/null 2>&1; then
    if ! wget -q -O "$BINARY_TMP" "$BINARY_URL"; then
        echo "Agent 二进制下载失败" >&2; exit 1
    fi
elif command -v curl >/dev/null 2>&1; then
    if ! curl -fsL -o "$BINARY_TMP" "$BINARY_URL"; then
        echo "Agent 二进制下载失败" >&2; exit 1
    fi
else
    echo "安装阶段需要 wget 或 curl 下载二进制"; exit 1
fi
chmod 755 "$BINARY_TMP"
"$BINARY_TMP" -version >/dev/null
cf_step_done "Agent 下载与校验通过"


mkdir -p "$AGENT_DIR"
chmod 700 "$AGENT_DIR"
EXISTING_ID=""
EXISTING_TOKEN=""
for old_config in "$CONFIG_FILE" "$AGENT_DIR/config.json"; do
    if [ -f "$old_config" ]; then
        old_name=$(sed -n 's/.*"agent_name"[[:space:]]*:[[:space:]]*"\([A-Za-z0-9_-]*\)".*/\1/p' "$old_config" | head -n 1)
        old_type=$(sed -n 's/.*"service_type"[[:space:]]*:[[:space:]]*"\([A-Za-z0-9_-]*\)".*/\1/p' "$old_config" | head -n 1)
        if [ "$old_name" = "{agent_name}" ] && [ "$old_type" = "{service_type}" ]; then
            EXISTING_ID=$(sed -n 's/.*"agent_id"[[:space:]]*:[[:space:]]*"\([A-Za-z0-9_.-]*\)".*/\1/p' "$old_config" | head -n 1)
            EXISTING_TOKEN=$(sed -n 's/.*"token"[[:space:]]*:[[:space:]]*"\([A-Za-z0-9_-]*\)".*/\1/p' "$old_config" | head -n 1)
            break
        fi
    fi
done
umask 077
cat > "$CONFIG_FILE.new" <<'CONFIGFLOW_JSON_EOF'
{config_json}
CONFIGFLOW_JSON_EOF
if [ "$INIT_SYSTEM" = "systemd" ]; then
    RESTART="systemctl restart $SERVICE_UNIT"
else
    RESTART="rc-service $SERVICE_UNIT restart"
fi
sed -i "s/__CONFIGFLOW_INIT__/$INIT_SYSTEM/g; s/__CONFIGFLOW_RESTART__/$RESTART/g; s/__CONFIGFLOW_AGENT_ID__/$EXISTING_ID/g; s/__CONFIGFLOW_TOKEN__/$EXISTING_TOKEN/g" "$CONFIG_FILE.new"
{dns_ha_existing_config}# Validate OxiDNS's working directory before stopping an existing Agent.
cf_step "检查核心接管与运行环境"
"$BINARY_TMP" -config "$CONFIG_FILE.new" -validate-service
# Report TUN, permissions, forwarding and listener conflicts with repair steps.
# This check is read-only; an error leaves the previous Agent running.
{ipv4_forwarding_environment}if cf_environment_check "$BINARY_TMP" -config "$CONFIG_FILE.new" -check-environment; then
    :
else
    environment_status=$?
    echo "若提示未知 -check-environment 参数，请更新 ConfigFlow 并重新构建/提供 1.8.19-go 或更高的 Agent 下载文件；否则按上方环境问题修复后重跑。" >&2
    exit "$environment_status"
fi
{ipv4_forwarding_environment_end}cf_step_done "核心接管与运行环境检查通过"
{ipv4_forwarding_prepare}cf_step "安装并启动 Agent 服务"
# Stop the previous Go or legacy Shell Agent before replacing its binary/config.
if [ "$INIT_SYSTEM" = "systemd" ]; then
    if systemctl is-active --quiet configflow-agent 2>/dev/null; then RESTART_PREVIOUS_AGENT=1; fi
    systemctl stop configflow-agent 2>/dev/null || true
else
    if rc-service configflow-agent status >/dev/null 2>&1; then RESTART_PREVIOUS_AGENT=1; fi
    rc-service configflow-agent stop 2>/dev/null || true
fi
# Existing journals belong to the old config root, which may differ from the
# newly requested config_path. Report pending work before losing its reference.
if [ -f "$CONFIG_FILE" ]; then
    cf_deployment_check "$BINARY_TMP" -config "$CONFIG_FILE" -check-deployments
fi
# Report interrupted deployments without changing core files or services.
cf_deployment_check "$BINARY_TMP" -config "$CONFIG_FILE.new" -check-deployments
remove_recovery_gate
mv "$BINARY_TMP" /usr/local/bin/configflow-agent
mv "$CONFIG_FILE.new" "$CONFIG_FILE"
chmod 600 "$CONFIG_FILE"

{ipv4_forwarding_hook}# Reuse an existing logrotate installation; publishing requires no new package.
if command -v logrotate >/dev/null 2>&1; then
    mkdir -p /etc/logrotate.d
    cat > /etc/logrotate.d/configflow-agent <<'LOGEOF'
/var/log/configflow-agent.log {
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

if [ "$INIT_SYSTEM" = "systemd" ]; then
    cat > /etc/systemd/system/configflow-agent.service <<'SERVICEEOF'
[Unit]
Description=ConfigFlow Agent (Go)
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/configflow-agent
{ipv4_systemd_pre}ExecStart=/usr/local/bin/configflow-agent -config /opt/configflow-agent/config-{service_type}.json
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
SERVICEEOF
    systemctl daemon-reload
    systemctl enable configflow-agent
    systemctl start configflow-agent
    # An Agent that exits right after starting must not count as started.
    sleep 2
    if ! systemctl is-active --quiet configflow-agent; then
        echo "Agent 启动后未保持运行，请查看 journalctl -u configflow-agent -n 50。" >&2
        exit 1
    fi
else
    cat > /etc/init.d/configflow-agent <<'SERVICEEOF'
#!/sbin/openrc-run
name="ConfigFlow Agent (Go)"
command="/usr/local/bin/configflow-agent"
command_args="-config /opt/configflow-agent/config-{service_type}.json"
# Restart a crashed Agent, as Restart=always does under systemd.
supervisor="supervise-daemon"
respawn_delay=10
respawn_max=0
pidfile="/run/configflow-agent.pid"
output_log="/var/log/configflow-agent.log"
error_log="/var/log/configflow-agent.log"
directory="/opt/configflow-agent"
depend() {
    need net
    after firewall
}
start_pre() {
{ipv4_openrc_pre}    checkpath --directory --mode 0755 /var/log
    checkpath --file --mode 0644 --owner root:root /var/log/configflow-agent.log
    if [ -f /var/log/configflow-agent.log ]; then
        log_size=$(wc -c < /var/log/configflow-agent.log 2>/dev/null || echo 0)
        if [ "$log_size" -gt 10485760 ]; then
            mv -f /var/log/configflow-agent.log /var/log/configflow-agent.log.1
            : > /var/log/configflow-agent.log
            chmod 0644 /var/log/configflow-agent.log
        fi
    fi
}
SERVICEEOF
    chmod 755 /etc/init.d/configflow-agent
    rc-update add configflow-agent default
    rc-update -u
    # supervise-daemon keeps inherited descriptors: close the report fds, or a
    # piped or non-interactive SSH run would never return.
    rc-service configflow-agent start 3>&- 4>&-
    sleep 2
    # supervise-daemon stays "started" while it waits to respawn an Agent that
    # exited, so also require the child of the supervisor named in its pidfile.
    agent_supervisor=$(cat /run/configflow-agent.pid 2>/dev/null || true)
    if ! rc-service configflow-agent status ||
        ! grep -qs "^PPid:[[:space:]]*${agent_supervisor:-missing}\$" /proc/[0-9]*/status; then
        echo "Agent 启动后未保持运行，请查看 /var/log/configflow-agent.log。" >&2
        exit 1
    fi
fi
RESTART_PREVIOUS_AGENT=0
{ipv4_forwarding_apply}cf_step_done "Agent 服务已启动"
{ipv4_forwarding_summary}cf_fact agent_{service_type} "{service_label} Agent：端口 {agent_summary_port}，服务已启动"
cf_complete "安装完成"
