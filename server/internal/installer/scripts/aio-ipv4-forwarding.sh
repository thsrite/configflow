# Included only in the ordinary Shell cohost installer, never the gateway wizard.
IPV4_CONFIG="/etc/sysctl.d/99-configflow-aio-forward.conf"
IPV4_MARKER="# Managed by ConfigFlow Shell AIO IPv4 forwarding v1"
IPV4_CONFIG_CREATED=0
IPV4_TEMP=""
IPV4_BOOT_RESULT=""

ipv4_failure() {
    fail "$*。请在运行 Mihomo 的 Linux/虚拟机/容器内检查 sysctl、CAP_NET_ADMIN 和 /proc/sys 的写权限；LXC 权限受限时需由宿主机管理员调整容器权限。本脚本不会修改 PVE 宿主机。"
}

check_ipv4_config() {
    [ ! -L "$IPV4_CONFIG" ] || fail "IPv4 转发持久化路径不能是符号链接：$IPV4_CONFIG"
    if [ -e "$IPV4_CONFIG" ]; then
        [ -f "$IPV4_CONFIG" ] && cmp -s "$IPV4_CONFIG" "$STAGE_DIR/ipv4-forward.conf" ||
            fail "IPv4 转发持久化路径已有非本安装器配置，请先核对：$IPV4_CONFIG"
    fi
}

check_ipv4_forwarding_ownership() {
    # A gateway journal owns its own original values and recovery. Do not add a
    # competing boot hook when an existing gateway is reinstalled as ordinary AIO.
    if [ -e "$AGENT_ROOT/mihomo/linux-gateway.json" ] || [ -L "$AGENT_ROOT/mihomo/linux-gateway.json" ]; then
        fail "检测到 Linux 网关的网络记录，网关恢复记录未清理。请使用原网关入口管理；如需卸载，请先恢复客户端的网关和 DNS，再撤销网关转发。普通同机安装不会接管其转发设置。"
    fi
    [ ! -L "$AGENT_ROOT/mihomo/enable-ipv4-forwarding.sh" ] || fail "IPv4 转发启动脚本不能是符号链接。"
    [ ! -e "$AGENT_ROOT/mihomo/enable-ipv4-forwarding.sh" ] || [ -f "$AGENT_ROOT/mihomo/enable-ipv4-forwarding.sh" ] || fail "IPv4 转发启动脚本路径不是普通文件。"
}

prepare_ipv4_forwarding() {
    check_ipv4_forwarding_ownership
    for ipv4_tool in sysctl cmp ln; do
        command -v "$ipv4_tool" >/dev/null 2>&1 || ipv4_failure "缺少 $ipv4_tool 命令，请安装该工具后重试"
    done
    ipv4_current=$(cat /proc/sys/net/ipv4/ip_forward) || ipv4_failure "无法读取 IPv4 转发状态"
    case "$ipv4_current" in 0|1) ;; *) ipv4_failure "IPv4 转发状态不是 0 或 1" ;; esac
    printf '%s\nnet.ipv4.ip_forward=1\n' "$IPV4_MARKER" > "$STAGE_DIR/ipv4-forward.conf"
    [ ! -L /etc/sysctl.d ] || fail "sysctl.d 目录不能是符号链接：/etc/sysctl.d"
    [ ! -e /etc/sysctl.d ] || [ -d /etc/sysctl.d ] || fail "sysctl.d 路径不是目录：/etc/sysctl.d"
    check_ipv4_config
    # Writing the current value tests the real kernel permission without toggling
    # forwarding. A root uid or writable-looking proc file is not sufficient in LXC.
    sysctl -q -w "net.ipv4.ip_forward=$ipv4_current" || ipv4_failure "无法写入 IPv4 转发参数"
    [ "$(cat /proc/sys/net/ipv4/ip_forward)" = "$ipv4_current" ] || ipv4_failure "IPv4 转发权限检查后读回值不一致"
    mkdir -p /etc/sysctl.d || ipv4_failure "无法创建 IPv4 转发持久化目录"
    IPV4_TEMP=$(mktemp /etc/sysctl.d/.configflow-aio-forward.XXXXXX) || ipv4_failure "IPv4 转发持久化目录不可写"
    cat "$STAGE_DIR/ipv4-forward.conf" > "$IPV4_TEMP" || ipv4_failure "无法写入 IPv4 转发持久化文件"
    chmod 644 "$IPV4_TEMP"

    cat > "$STAGE_DIR/enable-ipv4-forwarding.sh" <<'CONFIGFLOW_AIO_FORWARDING_EOF'
#!/bin/sh
set -eu
config=/etc/sysctl.d/99-configflow-aio-forward.conf
# The installer commits this file only after both Agents start successfully.
[ -e "$config" ] || exit 0
expected='# Managed by ConfigFlow Shell AIO IPv4 forwarding v1
net.ipv4.ip_forward=1'
if [ -L "$config" ] || [ ! -f "$config" ] || [ "$(cat "$config")" != "$expected" ]; then
    echo "ConfigFlow IPv4 转发持久化配置已被修改，拒绝覆盖：$config" >&2
    exit 1
fi
current=$(cat /proc/sys/net/ipv4/ip_forward)
case "$current" in
    1) exit 0 ;;
    0) ;;
    *) echo "无法确认当前网络命名空间的 IPv4 转发状态" >&2; exit 1 ;;
esac
# Apply only this key, not unrelated sysctl files. Changing 0 -> 1 also has the
# normal Linux host/router side effects; do not toggle it on every Agent restart.
if ! sysctl -q -w net.ipv4.ip_forward=1; then
    echo "无法开启 IPv4 转发，请检查当前 Linux/容器的 CAP_NET_ADMIN 和 /proc/sys 写权限" >&2
    exit 1
fi
if [ "$(cat /proc/sys/net/ipv4/ip_forward)" != 1 ]; then
    echo "IPv4 转发写入后读回不是 1，未能确认开启" >&2
    exit 1
fi
CONFIGFLOW_AIO_FORWARDING_EOF
}

install_ipv4_forwarding_hook() {
    [ "$SERVICE" = mihomo ] || return 0
    cp "$STAGE_DIR/enable-ipv4-forwarding.sh" "$AGENT_DIR/enable-ipv4-forwarding.sh"
    chmod 755 "$AGENT_DIR/enable-ipv4-forwarding.sh"
    IPV4_SYSTEMD_PRE="ExecStartPre=/bin/sh $AGENT_DIR/enable-ipv4-forwarding.sh"
    IPV4_OPENRC_PRE="/bin/sh $AGENT_DIR/enable-ipv4-forwarding.sh || return 1"
}

persist_ipv4_forwarding() {
    check_ipv4_forwarding_ownership
    check_ipv4_config
    if [ ! -e "$IPV4_CONFIG" ]; then
        # Hard-link commit is atomic and refuses a file created by another writer.
        ln "$IPV4_TEMP" "$IPV4_CONFIG" || ipv4_failure "无法提交 IPv4 转发持久化配置"
        IPV4_CONFIG_CREATED=1
    fi
    /bin/sh "$AGENT_ROOT/mihomo/enable-ipv4-forwarding.sh" || ipv4_failure "IPv4 转发未能成功开启并验证"
    # A missing file is deliberately a no-op during the first Agent start, but
    # must never be accepted as a successful installer commit.
    [ -f "$IPV4_CONFIG" ] && [ ! -L "$IPV4_CONFIG" ] && cmp -s "$IPV4_CONFIG" "$STAGE_DIR/ipv4-forward.conf" ||
        ipv4_failure "IPv4 转发持久化配置在应用期间发生变化"
    [ "$(cat /proc/sys/net/ipv4/ip_forward)" = 1 ] || ipv4_failure "最终 IPv4 转发状态不是 1"
    ensure_ipv4_sysctl_boot
}

ipv4_sysctl_boot_state() {
    # Matches the Agent's SysctlBoot: only OpenRC can leave sysctl.d unapplied.
    [ "$INIT_SYSTEM" = openrc ] || { echo na; return 0; }
    [ -f /etc/init.d/sysctl ] || { echo unavailable; return 0; }
    for ipv4_level in sysinit boot default; do
        [ -e "/etc/runlevels/$ipv4_level/sysctl" ] && { echo enabled; return 0; }
    done
    echo disabled
}

ensure_ipv4_sysctl_boot() {
    # PVE's Alpine LXC template has the OpenRC sysctl service in no runlevel, so
    # sysctl.d would never apply at boot and forwarding would rely on the Agent
    # alone. Forwarding is already on, so a failure only asks for a manual step.
    case "$(ipv4_sysctl_boot_state)" in
        disabled)
            if rc-update add sysctl boot >/dev/null 2>&1 && [ "$(ipv4_sysctl_boot_state)" = enabled ]; then
                IPV4_BOOT_RESULT=added
            else
                IPV4_BOOT_RESULT=failed
            fi
            ;;
        unavailable) IPV4_BOOT_RESULT=unavailable ;;
    esac
}

summarize_ipv4_forwarding() {
    # Earlier core/environment checks describe the state before this installer
    # enabled forwarding. Remove only that exact resolved runtime warning;
    # unrelated warnings and the original detailed log remain available.
    if cf_output_is_active; then
        for ipv4_note_file in "$CF_OUTPUT_DIR"/note.environment_*; do
            [ -f "$ipv4_note_file" ] || continue
            if ipv4_note_text=$(cf_read_file "$ipv4_note_file"); then
                case "$ipv4_note_text" in
                    'IPv4 转发：当前 net.ipv4.ip_forward=0；'*)
                        rm -f "$ipv4_note_file" || echo "无法更新已解决的 IPv4 转发提醒：$ipv4_note_file" >&2
                        ;;
                esac
            fi
        done
    fi
    cf_note forwarding "IPv4 转发已设置持久化，尚未实际重启验证；其他配置或服务仍可能在重启后把它改回 0，重启后可执行 sysctl net.ipv4.ip_forward 确认仍为 1。"
    case "${IPV4_BOOT_RESULT:-}" in
        added) cf_fact ipv4_forwarding_boot "OpenRC 的 sysctl 服务原本不在开机运行级别，已执行 rc-update add sysctl boot，开机时会应用 $IPV4_CONFIG。" ;;
        failed) cf_note forwarding_boot "无法自动把 OpenRC 的 sysctl 服务加入开机运行级别，开机时不会应用 $IPV4_CONFIG（目前只靠 Mihomo Agent 启动前检查补设）。请执行：rc-update add sysctl boot" ;;
        unavailable) cf_note forwarding_boot "系统没有 OpenRC 的 sysctl 服务，开机时不会应用 $IPV4_CONFIG（目前只靠 Mihomo Agent 启动前检查补设）。请执行：mkdir -p /etc/local.d && printf '#!/bin/sh\\nsysctl -w net.ipv4.ip_forward=1\\n' > /etc/local.d/configflow-ip-forward.start && chmod 755 /etc/local.d/configflow-ip-forward.start && rc-update add local default" ;;
    esac
    # The environment check only warns about these; repeat them in the final
    # summary because neither stops the install.
    if [ -n "${CONFIGFLOW_LOG_FILE:-}" ] && [ -f "$CONFIGFLOW_LOG_FILE" ]; then
        if grep -q '^\[提醒\] IPv4 转发冲突：' "$CONFIGFLOW_LOG_FILE" 2>/dev/null; then
            cf_note forwarding_conflict "IPv4 转发冲突：其他 sysctl 设置会在重启后把 net.ipv4.ip_forward 改回 0；处理方法见上方「IPv4 转发冲突」或完整日志。"
        fi
        if grep -q '^\[提醒\] 转发过滤：' "$CONFIGFLOW_LOG_FILE" 2>/dev/null; then
            cf_note forward_filter "转发过滤：这台 Linux 的防火墙可能拦截经过 Mihomo TUN 的流量，安装不受影响；影响范围和放行命令见上方「转发过滤」或完整日志。"
        fi
    fi
    cf_fact ipv4_forwarding "IPv4 转发已开启并持久化；Mihomo Agent 开机启动时会再次检查并恢复。"
}

cleanup_ipv4_forwarding() {
    if [ "$1" -ne 0 ] && [ "$IPV4_CONFIG_CREATED" = 1 ]; then
        if [ ! -L "$IPV4_CONFIG" ] && cmp -s "$IPV4_CONFIG" "$STAGE_DIR/ipv4-forward.conf"; then
            rm -f "$IPV4_CONFIG" || echo "无法清理本次创建的持久化文件，请检查：$IPV4_CONFIG" >&2
        fi
        echo "安装失败，未确认完成 IPv4 转发持久化。请检查 sysctl net.ipv4.ip_forward 当前值；不会擅自关闭可能已在使用的转发。" >&2
    fi
    if [ -n "$IPV4_TEMP" ]; then rm -f "$IPV4_TEMP" || echo "无法清理临时文件：$IPV4_TEMP" >&2; fi
    return 0
}
