# Installation-local rollback journal. Only paths saved before a write are
# restored; core configuration/publication data is never part of this journal.
AGENT_ROLLBACK_DIR=""
AGENT_ROLLBACK_FILES=0
AGENT_ROLLBACK_SERVICES=0
AGENT_ROLLBACK_ARMED=0
AGENT_ROLLBACK_MUTATED=0
AGENT_ROLLBACK_KEEP=0
AGENT_ROLLBACK_RUNLEVELS=""

agent_rollback_begin() {
    AGENT_ROLLBACK_DIR=$(mktemp -d "$1/.agent-install-backup.XXXXXX")
    chmod 700 "$AGENT_ROLLBACK_DIR"
    AGENT_ROLLBACK_RUNLEVELS="$(dirname /etc/init.d)/runlevels"
}

agent_rollback_save() {
    [ -n "$AGENT_ROLLBACK_DIR" ] || return 0
    rollback_slot=1
    while [ "$rollback_slot" -le "$AGENT_ROLLBACK_FILES" ]; do
        rollback_path=$(cat "$AGENT_ROLLBACK_DIR/path.$rollback_slot")
        [ "$rollback_path" != "$1" ] || return 0
        rollback_slot=$((rollback_slot + 1))
    done
    rollback_slot=$((AGENT_ROLLBACK_FILES + 1))
    if [ -e "$1" ] || [ -L "$1" ]; then
        cp -pP "$1" "$AGENT_ROLLBACK_DIR/file.$rollback_slot"
        : > "$AGENT_ROLLBACK_DIR/present.$rollback_slot"
    fi
    printf '%s\n' "$1" > "$AGENT_ROLLBACK_DIR/path.$rollback_slot"
    AGENT_ROLLBACK_FILES=$rollback_slot
}

agent_rollback_service() {
    rollback_service_slot=$((AGENT_ROLLBACK_SERVICES + 1))
    printf '%s\n' "$1" > "$AGENT_ROLLBACK_DIR/service.$rollback_service_slot"
    rollback_active=0
    rollback_enabled=disabled
    if [ "$INIT_SYSTEM" = systemd ]; then
        if systemctl is-active --quiet "$1" 2>/dev/null; then rollback_active=1; fi
        rollback_enabled=$(systemctl is-enabled "$1" 2>/dev/null || true)
        agent_rollback_save "/etc/systemd/system/$1.service"
    else
        if rc-service "$1" status >/dev/null 2>&1; then rollback_active=1; fi
        if [ -L "$AGENT_ROLLBACK_RUNLEVELS/default/$1" ] || [ -e "$AGENT_ROLLBACK_RUNLEVELS/default/$1" ]; then rollback_enabled=enabled; fi
        agent_rollback_save "/etc/init.d/$1"
    fi
    printf '%s\n' "$rollback_active" > "$AGENT_ROLLBACK_DIR/active.$rollback_service_slot"
    printf '%s\n' "$rollback_enabled" > "$AGENT_ROLLBACK_DIR/enabled.$rollback_service_slot"
    AGENT_ROLLBACK_SERVICES=$rollback_service_slot
}

agent_rollback_restore() {
    rollback_failed=0
    rollback_stop_failed=0
    if [ "$AGENT_ROLLBACK_MUTATED" = 1 ]; then
        # Stop every attempted new service before restoring any file, including
        # the first AIO service when starting the second service fails.
        rollback_service_slot=1
        while [ "$rollback_service_slot" -le "$AGENT_ROLLBACK_SERVICES" ]; do
            if ! rollback_unit=$(cat "$AGENT_ROLLBACK_DIR/service.$rollback_service_slot") || [ -z "$rollback_unit" ]; then
                rollback_stop_failed=1
                rollback_service_slot=$((rollback_service_slot + 1))
                continue
            fi
            if [ "$INIT_SYSTEM" = systemd ]; then
                if ! systemctl stop "$rollback_unit"; then
                    if systemctl is-active --quiet "$rollback_unit"; then rollback_stop_failed=1; fi
                fi
                if [ -e "/etc/systemd/system/$rollback_unit.service" ] || [ -L "/etc/systemd/system/$rollback_unit.service" ]; then
                    systemctl disable "$rollback_unit" 2>/dev/null || rollback_failed=1
                fi
            else
                if ! rc-service "$rollback_unit" stop 3>&- 4>&-; then
                    if rc-service "$rollback_unit" status >/dev/null 2>&1; then rollback_stop_failed=1; fi
                fi
                if [ -e "$AGENT_ROLLBACK_RUNLEVELS/default/$rollback_unit" ] || [ -L "$AGENT_ROLLBACK_RUNLEVELS/default/$rollback_unit" ]; then
                    rc-update del "$rollback_unit" default 2>/dev/null || rollback_failed=1
                fi
            fi
            rollback_service_slot=$((rollback_service_slot + 1))
        done
        if [ "$rollback_stop_failed" -ne 0 ]; then
            echo "无法停止安装失败的 Agent；未覆盖恢复文件，请检查服务并使用保留的备份恢复。" >&2
            AGENT_ROLLBACK_KEEP=1
            return 1
        fi
        rollback_slot=1
        while [ "$rollback_slot" -le "$AGENT_ROLLBACK_FILES" ]; do
            if ! rollback_path=$(cat "$AGENT_ROLLBACK_DIR/path.$rollback_slot") || [ -z "$rollback_path" ]; then
                rollback_failed=1
                rollback_slot=$((rollback_slot + 1))
                continue
            fi
            if [ -f "$AGENT_ROLLBACK_DIR/present.$rollback_slot" ]; then
                # Stage alongside the destination: the backup may be on another
                # filesystem, and replacing a running executable must be a rename.
                if rollback_temp=$(mktemp "${rollback_path}.agent-restore.XXXXXX"); then
                    if cp -pP "$AGENT_ROLLBACK_DIR/file.$rollback_slot" "$rollback_temp" && mv -f "$rollback_temp" "$rollback_path"; then
                        :
                    else
                        rm -f "$rollback_temp" || true
                        rollback_failed=1
                    fi
                else
                    rollback_failed=1
                fi
            elif ! rm -f "$rollback_path"; then
                rollback_failed=1
            fi
            rollback_slot=$((rollback_slot + 1))
        done
        if [ "$INIT_SYSTEM" = systemd ]; then
            systemctl daemon-reload || rollback_failed=1
        else
            rc-update -u || rollback_failed=1
        fi
        if [ "$rollback_failed" -ne 0 ]; then
            echo "Agent 文件或服务配置未能全部恢复；服务保持停止，请使用保留的备份恢复。" >&2
            AGENT_ROLLBACK_KEEP=1
            return 1
        fi
    fi
    rollback_service_slot=1
    while [ "$rollback_service_slot" -le "$AGENT_ROLLBACK_SERVICES" ]; do
        if ! rollback_unit=$(cat "$AGENT_ROLLBACK_DIR/service.$rollback_service_slot") ||
            ! rollback_enabled=$(cat "$AGENT_ROLLBACK_DIR/enabled.$rollback_service_slot") || [ -z "$rollback_unit" ]; then
            rollback_failed=1
            rollback_service_slot=$((rollback_service_slot + 1))
            continue
        fi
        if [ "$INIT_SYSTEM" = systemd ]; then
            case "$rollback_enabled" in
                enabled) systemctl enable "$rollback_unit" || rollback_failed=1 ;;
                enabled-runtime) systemctl enable --runtime "$rollback_unit" || rollback_failed=1 ;;
            esac
        elif [ "$rollback_enabled" = enabled ]; then
            rc-update add "$rollback_unit" default || rollback_failed=1
        fi
        rollback_service_slot=$((rollback_service_slot + 1))
    done
    rollback_restarted=0
    if [ "$rollback_failed" -eq 0 ]; then
        rollback_service_slot=1
        while [ "$rollback_service_slot" -le "$AGENT_ROLLBACK_SERVICES" ]; do
            if ! rollback_unit=$(cat "$AGENT_ROLLBACK_DIR/service.$rollback_service_slot") ||
                ! rollback_active=$(cat "$AGENT_ROLLBACK_DIR/active.$rollback_service_slot") || [ -z "$rollback_unit" ]; then
                rollback_failed=1
                rollback_service_slot=$((rollback_service_slot + 1))
                continue
            fi
            if [ "$rollback_active" = 1 ]; then
                rollback_restarted=1
                if [ "$INIT_SYSTEM" = systemd ]; then
                    systemctl start "$rollback_unit" || rollback_failed=1
                else
                    rc-service "$rollback_unit" start 3>&- 4>&- || rollback_failed=1
                fi
            fi
            rollback_service_slot=$((rollback_service_slot + 1))
        done
    fi
    if [ "$rollback_failed" -eq 0 ] && [ "$rollback_restarted" = 1 ]; then
        sleep 2
        rollback_service_slot=1
        while [ "$rollback_service_slot" -le "$AGENT_ROLLBACK_SERVICES" ]; do
            if ! rollback_unit=$(cat "$AGENT_ROLLBACK_DIR/service.$rollback_service_slot") ||
                ! rollback_active=$(cat "$AGENT_ROLLBACK_DIR/active.$rollback_service_slot") || [ -z "$rollback_unit" ]; then
                rollback_failed=1
                rollback_service_slot=$((rollback_service_slot + 1))
                continue
            fi
            if [ "$rollback_active" = 1 ]; then
                if [ "$INIT_SYSTEM" = systemd ]; then
                    systemctl is-active --quiet "$rollback_unit" || rollback_failed=1
                else
                    rc-service "$rollback_unit" status >/dev/null 2>&1 || rollback_failed=1
                    if grep -Eq '^[[:space:]]*supervisor=.*supervise-daemon' "/etc/init.d/$rollback_unit"; then
                        rollback_supervisor=$(cat "/run/$rollback_unit.pid" 2>/dev/null || true)
                        case "$rollback_supervisor" in
                            ''|*[!0-9]*) rollback_failed=1 ;;
                            *) grep -qs "^PPid:[[:space:]]*${rollback_supervisor}\$" /proc/[0-9]*/status || rollback_failed=1 ;;
                        esac
                    fi
                fi
            fi
            rollback_service_slot=$((rollback_service_slot + 1))
        done
    fi
    if [ "$rollback_failed" -ne 0 ]; then
        echo "旧 Agent 文件已恢复，但服务状态未能全部恢复；请检查服务和保留的备份。" >&2
        AGENT_ROLLBACK_KEEP=1
        return 1
    fi
    echo "安装失败，已恢复安装前的 Agent 文件、身份和服务状态。" >&2
}

agent_rollback_discard() {
    [ -n "$AGENT_ROLLBACK_DIR" ] || return 0
    if [ "$AGENT_ROLLBACK_KEEP" = 1 ]; then
        printf 'Agent 安装备份已保留：%s\n' "$AGENT_ROLLBACK_DIR" >&2
    elif ! rm -rf "$AGENT_ROLLBACK_DIR"; then
        printf 'Agent 安装备份未能清理：%s\n' "$AGENT_ROLLBACK_DIR" >&2
    fi
}
