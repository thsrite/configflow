#!/bin/sh
# ConfigFlow native Mihomo + OxiDNS: manage existing cores with isolated Agents.
set +e
# Shared POSIX-shell reports. Diagnostics stay in a private, append-only session
# log; fd 3/4 retain the terminal across nested installers and Agent commands.
# Logging must never prevent a minimal host from repairing its dependencies.
cf_verbose_options() {
    for cf_option do case "$cf_option" in --verbose|-v) CONFIGFLOW_VERBOSE=1 ;; esac; done
    CONFIGFLOW_VERBOSE=${CONFIGFLOW_VERBOSE:-0}
    export CONFIGFLOW_VERBOSE
}

cf_read_file() {
    while IFS= read -r cf_file_line || [ -n "$cf_file_line" ]; do
        printf '%s\n' "$cf_file_line" || return 1
    done < "$1"
}

cf_output_is_active() {
    [ "${CF_OUTPUT_ACTIVE:-false}" = true ] || return 1
    if [ -f "${CONFIGFLOW_LOG_STATE_DIR:-}/disabled" ]; then
        # A child can only communicate through shared state, not shell vars.
        # Restore this process too before its next raw command writes output.
        CF_OUTPUT_ACTIVE=false CONFIGFLOW_CLI_ACTIVE=0
        export CF_OUTPUT_ACTIVE CONFIGFLOW_CLI_ACTIVE
        exec 1>&3 2>&4
        return 1
    fi
    return 0
}

cf_output_degrade() {
    CF_OUTPUT_ACTIVE=false
    CONFIGFLOW_CLI_ACTIVE=0
    export CF_OUTPUT_ACTIVE CONFIGFLOW_CLI_ACTIVE
    # This stable path also informs the parent when a subshell loses metadata.
    if [ -d "${CONFIGFLOW_LOG_STATE_DIR:-}" ]; then
        { printf 'disabled\n' > "$CONFIGFLOW_LOG_STATE_DIR/disabled"; } 2>/dev/null || :
    fi
    exec 1>&3 2>&4
    printf '[提醒] 日志记录不可用，继续显示原始输出；后续输出未完整保存。\n' >&4 || :
    if [ -f "${CONFIGFLOW_LOG_FILE:-}" ]; then
        printf '已有日志（可能不完整）：%s\n' "$CONFIGFLOW_LOG_FILE" >&4 || :
    fi
    return 0
}

cf_cursor() {
    cf_line_count=0
    while IFS= read -r cf_cursor_line || [ -n "$cf_cursor_line" ]; do
        cf_line_count=$((cf_line_count + 1))
    done < "$CONFIGFLOW_LOG_FILE"
    printf '%s\n' "$cf_line_count"
}

cf_mark_read() {
    cf_output_is_active || return 0
    if ! cf_cursor > "$CONFIGFLOW_LOG_CURSOR"; then cf_output_degrade; fi
    return 0
}

cf_redact() {
    if command -v sed >/dev/null 2>&1; then
        LC_ALL=C sed -E 's/(Authorization:[[:space:]]*(Bearer|Basic)[[:space:]]+)[^[:space:]]+/\1[REDACTED]/g; s/("(token|installation_token|secret|password)"[[:space:]]*:[[:space:]]*")[^"]*/\1[REDACTED]/g; s/((token|installation_token|password)=)[^&[:space:]]+/\1[REDACTED]/g'
    else
        # Callers never log config bodies, enrollment headers or credentials.
        while IFS= read -r cf_redact_line || [ -n "$cf_redact_line" ]; do printf '%s\n' "$cf_redact_line"; done
    fi
}

cf_pending_output() {
    cf_last_line=0
    IFS= read -r cf_last_line < "$CONFIGFLOW_LOG_CURSOR" || cf_last_line=0
    case "$cf_last_line" in ''|*[!0-9]*) cf_last_line=0 ;; esac
    cf_output_line=0
    while IFS= read -r cf_pending_line || [ -n "$cf_pending_line" ]; do
        cf_output_line=$((cf_output_line + 1))
        if [ "$cf_output_line" -gt "$cf_last_line" ]; then printf '%s\n' "$cf_pending_line"; fi
    done < "$CONFIGFLOW_LOG_FILE"
}

cf_flush() {
    cf_output_is_active && [ "${CONFIGFLOW_VERBOSE:-0}" = 1 ] || return 0
    cf_pending_output | cf_redact >&3
    cf_mark_read
}

cf_failure_details() {
    cf_output_is_active || return 0
    if [ "${CONFIGFLOW_VERBOSE:-0}" = 1 ]; then cf_flush; return 0; fi
    if command -v tail >/dev/null 2>&1; then
        cf_pending_output | tail -n 20 | cf_redact >&4
    else
        cf_pending_output | cf_redact >&4
    fi
    cf_mark_read
}

cf_emit() {
    cf_kind=$1
    shift
    cf_flush
    cf_line=$*
    cf_color=""
    case "$cf_kind" in
        step) cf_line="[进行] $cf_line"; cf_color=${_CF_BLUE:-} ;;
        success) cf_line="[通过] $cf_line"; cf_color=${_CF_GREEN:-} ;;
        warn) cf_line="[提醒] $cf_line"; cf_color=${_CF_YELLOW:-} ;;
        error) cf_line="[失败] $cf_line"; cf_color=${_CF_RED:-} ;;
    esac
    if cf_output_is_active; then
        printf '%s\n' "$cf_line" >> "$CONFIGFLOW_LOG_FILE" || cf_output_degrade
    fi
    case "$cf_kind" in
        warn|error) printf '%s%s%s\n' "$cf_color" "$cf_line" "${_CF_RESET:-}" >&4 || : ;;
        *) printf '%s%s%s\n' "$cf_color" "$cf_line" "${_CF_RESET:-}" >&3 || : ;;
    esac
    cf_mark_read
}

cf_step() {
    if cf_output_is_active; then
        printf '%s\n' "$*" > "$CF_OUTPUT_DIR/current-step" || cf_output_degrade
    fi
    cf_notice ""
    cf_emit step "$*"
}
cf_success() { cf_emit success "$*"; }
cf_warn() { cf_emit warn "$*"; }
cf_error() { cf_failure_details; cf_emit error "$*"; }
cf_notice() { cf_emit notice "$*"; }
cf_detail() {
    if cf_output_is_active; then printf '%s\n' "$*" >> "$CONFIGFLOW_LOG_FILE" || cf_output_degrade;
    else printf '%s\n' "$*" >&3 || :; fi
    return 0
}
cf_result() {
    _CF_RESULT=$*
    if cf_output_is_active; then
        printf '%s\n' "$*" > "$CF_OUTPUT_DIR/result" && return 0
        cf_output_degrade
    fi
    cf_notice "$*"
}
cf_prompt() {
    cf_flush
    if cf_output_is_active; then printf '%s\n' "$*" >> "$CONFIGFLOW_LOG_FILE" || cf_output_degrade; fi
    printf '%s' "$*" >&3 || :
    cf_mark_read
}

# External commands only. Wrapping shell lifecycle functions in an if condition
# can disable their set -e semantics; installers keep their unconditional body.
cf_run() {
    cf_label=$1
    shift
    cf_step "$cf_label"
    if "$@"; then cf_success "$cf_label"; return 0;
    else
        cf_run_status=$?
        cf_error "${cf_label}（退出码 ${cf_run_status}）"
        return "$cf_run_status"
    fi
}

cf_finish() {
    cf_exit_status=$1
    [ "${_CF_FINISHED:-0}" = 0 ] || return 0
    _CF_FINISHED=1
    [ "${_CF_NESTED:-0}" = 0 ] || return 0
    if ! cf_output_is_active; then
        exec 1>&3 2>&4
        if [ "$cf_exit_status" -ne 0 ]; then cf_emit error "操作未完成（退出码 ${cf_exit_status}）"; fi
        return 0
    fi
    if [ "$cf_exit_status" -ne 0 ]; then
        cf_failure_details
        cf_failed_step='操作未完成'
        if [ -f "$CF_OUTPUT_DIR/current-step" ]; then IFS= read -r cf_failed_step < "$CF_OUTPUT_DIR/current-step" || :; fi
        cf_notice ""
        cf_emit error "${cf_failed_step}（退出码 ${cf_exit_status}）"
    else
        cf_flush
        if command -v cf_install_summary >/dev/null 2>&1; then cf_install_summary; fi
        cf_warning_count=0
        while IFS= read -r cf_warning_line || [ -n "$cf_warning_line" ]; do
            case "$cf_warning_line" in '[提醒]'*) cf_warning_count=$((cf_warning_count + 1)) ;; esac
        done < "$CONFIGFLOW_LOG_FILE"
        cf_final_result=${_CF_RESULT:-操作完成}
        if [ -f "$CF_OUTPUT_DIR/result" ]; then cf_final_result=$(cf_read_file "$CF_OUTPUT_DIR/result"); fi
        cf_notice ""
        if [ "$cf_warning_count" -gt 0 ]; then cf_emit warn "完成 · 有提醒：${cf_final_result}";
        else cf_success "$cf_final_result"; fi
    fi
    cf_notice "完整日志：$CONFIGFLOW_LOG_FILE"
    return 0
}

cf_init() {
    _CF_FINISHED=0 _CF_RESULT="" _CF_NESTED=0
    if [ "${CONFIGFLOW_CLI_ACTIVE:-0}" = 1 ] && [ -f "${CONFIGFLOW_LOG_FILE:-}" ] &&
        [ -d "${CONFIGFLOW_LOG_STATE_DIR:-}" ] && ( : >&3 ) 2>/dev/null; then
        _CF_NESTED=1
        CF_OUTPUT_DIR=$CONFIGFLOW_LOG_STATE_DIR
        CF_OUTPUT_LOG=$CONFIGFLOW_LOG_FILE
        CF_OUTPUT_ACTIVE=true
    else
        exec 3>&1 4>&2
        CF_OUTPUT_ACTIVE=false CF_OUTPUT_DIR="" CF_OUTPUT_LOG=""
        CONFIGFLOW_LOG_FILE="" CONFIGFLOW_LOG_STATE_DIR="" CONFIGFLOW_LOG_CURSOR=""
        cf_primary_log_root=${CONFIGFLOW_LOG_DIR:-${CONFIGFLOW_INSTALL_LOG_DIR:-/var/log/configflow}}
        cf_selected_log_root=""
        for cf_log_root in "$cf_primary_log_root" "${TMPDIR:-/tmp}" /tmp; do
            if [ ! -d "$cf_log_root" ]; then mkdir -p "$cf_log_root" 2>/dev/null || continue; fi
            [ -w "$cf_log_root" ] || continue
            CF_OUTPUT_DIR=$(umask 077; mktemp -d "$cf_log_root/configflow-install.XXXXXX" 2>/dev/null) || continue
            CF_OUTPUT_LOG="$CF_OUTPUT_DIR/install.log"
            if { (umask 077; printf '' > "$CF_OUTPUT_LOG" && printf '0\n' > "$CF_OUTPUT_DIR/cursor"); } 2>/dev/null; then
                CONFIGFLOW_LOG_FILE=$CF_OUTPUT_LOG
                CONFIGFLOW_LOG_STATE_DIR=$CF_OUTPUT_DIR
                CONFIGFLOW_LOG_CURSOR=$CF_OUTPUT_DIR/cursor
                CF_OUTPUT_ACTIVE=true
                cf_selected_log_root=$cf_log_root
                break
            fi
        done
    fi
    CONFIGFLOW_VERBOSE=${CONFIGFLOW_VERBOSE:-0}
    CONFIGFLOW_CLI_ACTIVE=0
    CONFIGFLOW_REPORT_FD=3 CONFIGFLOW_REPORT_NESTED=1
    if cf_output_is_active; then CONFIGFLOW_CLI_ACTIVE=1; fi
    export CONFIGFLOW_VERBOSE CONFIGFLOW_CLI_ACTIVE CONFIGFLOW_REPORT_FD CONFIGFLOW_REPORT_NESTED
    export CONFIGFLOW_LOG_FILE CONFIGFLOW_LOG_STATE_DIR CONFIGFLOW_LOG_CURSOR
    export CF_OUTPUT_ACTIVE CF_OUTPUT_DIR CF_OUTPUT_LOG
    _CF_BLUE="" _CF_GREEN="" _CF_YELLOW="" _CF_RED="" _CF_RESET=""
    if [ -t 3 ] && [ -z "${NO_COLOR+x}" ] && [ "${TERM:-dumb}" != dumb ]; then
        _CF_BLUE=$(printf '\033[36m') _CF_GREEN=$(printf '\033[32m')
        _CF_YELLOW=$(printf '\033[33m') _CF_RED=$(printf '\033[31m') _CF_RESET=$(printf '\033[0m')
    fi
    if cf_output_is_active; then
        # Redirection on a compound command keeps a failed special builtin from
        # terminating POSIX shells before their dependency repair can run.
        if { exec 5>&1; } >> "$CONFIGFLOW_LOG_FILE"; then exec 1>&5 2>&1 5>&-;
        else cf_output_degrade; fi
    fi
    trap 'cf_finish $?' 0
    if [ "$_CF_NESTED" -eq 0 ]; then
        cf_notice "ConfigFlow · $*"
        if ! cf_output_is_active; then
            printf '[提醒] 无法创建详细日志，将直接显示原始输出；本次输出未保存。\n' >&4 || :
        elif [ "$cf_selected_log_root" != "$cf_primary_log_root" ]; then
            cf_warn "日志目录不可写，已改用：$CONFIGFLOW_LOG_FILE"
        fi
    fi
    return 0
}

# Compatibility adapter for generated installers. State is stored in the shared
# private directory because installer bodies and core phases run in subshells.
cf_ui() { cf_notice "$*"; }
cf_step_done() { cf_success "$*"; }
cf_complete() { cf_result "$*"; }

cf_fact() {
    case "$1" in ''|*[!a-zA-Z0-9_-]*) return 0 ;; esac
    if cf_output_is_active; then
        if printf '%s\n' "$2" > "$CF_OUTPUT_DIR/fact.$1"; then return 0; fi
        cf_output_degrade
    fi
    case "$1" in tun_verified|core_mihomo_new|core_oxidns_new) return 0 ;; esac
    cf_notice "$2"
}
cf_fact_read() {
    cf_output_is_active || return 0
    case "$1" in ''|*[!a-zA-Z0-9_-]*) return 0 ;; esac
    if [ -f "$CF_OUTPUT_DIR/fact.$1" ]; then cf_read_file "$CF_OUTPUT_DIR/fact.$1" || :; fi
    return 0
}
cf_note() {
    case "$1" in ''|*[!a-zA-Z0-9_-]*) return 0 ;; esac
    if cf_output_is_active; then
        if printf '%s\n' "$2" > "$CF_OUTPUT_DIR/note.$1"; then return 0; fi
        cf_output_degrade
    fi
    cf_warn "$2"
}

cf_environment_check() {
    if ! cf_output_is_active; then "$@"; return $?; fi
    if ! cf_environment_report=$(mktemp "$CF_OUTPUT_DIR/environment.XXXXXX"); then
        cf_output_degrade
        "$@"
        return $?
    fi
    # New Agents report compact results directly through fd 3 and append details
    # to CONFIGFLOW_LOG_FILE. Older Agents are captured here for compatibility.
    cf_environment_log_start=$(cf_cursor)
    if "$@" > "$cf_environment_report" 2>&1; then cf_environment_status=0;
    else cf_environment_status=$?; fi
    cf_read_file "$cf_environment_report" || cf_output_degrade
    [ "$cf_environment_status" -eq 0 ] || return "$cf_environment_status"
    cf_warning_number=0
    while IFS= read -r cf_environment_line || [ -n "$cf_environment_line" ]; do
        case "$cf_environment_line" in
            '[提醒] IPv4 转发持久化：'*)
                if [ ! -f "$CF_OUTPUT_DIR/note.forwarding" ]; then
                    cf_note forwarding "${cf_environment_line#'[提醒] '}"
                fi ;;
            '[提醒] '*)
                cf_warning_number=$((cf_warning_number + 1))
                cf_note "environment_$cf_warning_number" "${cf_environment_line#'[提醒] '}" ;;
        esac
    done < "$cf_environment_report"
    # Read only this command's appended evidence, including captured old stdout.
    # The final summary needs the verified PID/interface, not the full probe text.
    cf_environment_log_line=0
    while IFS= read -r cf_environment_line || [ -n "$cf_environment_line" ]; do
        cf_environment_log_line=$((cf_environment_log_line + 1))
        [ "$cf_environment_log_line" -gt "$cf_environment_log_start" ] || continue
        case "$cf_environment_line" in
            '[通过] TUN 实际启动：Mihomo 核心 PID '*' 正持有内核 TUN/TAP 接口 '*)
                cf_tun_pid=${cf_environment_line#*Mihomo 核心 PID }
                cf_tun_pid=${cf_tun_pid%% *}
                cf_tun_interfaces=${cf_environment_line#* 正持有内核 TUN/TAP 接口 }
                cf_tun_interfaces=${cf_tun_interfaces%%，*}
                case "$cf_tun_pid" in ''|*[!0-9]*) continue ;; esac
                cf_fact tun_runtime "TUN：${cf_tun_interfaces} · Mihomo PID ${cf_tun_pid} 已持有" ;;
        esac
    done < "$CONFIGFLOW_LOG_FILE"
    return 0
}

cf_deployment_check() {
    if ! cf_output_is_active; then "$@"; return $?; fi
    if ! cf_deployment_report=$(mktemp "$CF_OUTPUT_DIR/deployment.XXXXXX"); then
        cf_output_degrade
        "$@"
        return $?
    fi
    if "$@" > "$cf_deployment_report" 2>&1; then cf_deployment_status=0;
    else cf_deployment_status=$?; fi
    cf_read_file "$cf_deployment_report" || cf_output_degrade
    while IFS= read -r cf_deployment_line || [ -n "$cf_deployment_line" ]; do
        case "$cf_deployment_line" in
            *'Deployment check requires attention:'*)
                cf_note deployment "发布记录需要处理：${cf_deployment_line#*Deployment check requires attention: }"
                break ;;
        esac
    done < "$cf_deployment_report"
    return "$cf_deployment_status"
}

cf_install_summary() {
    cf_output_is_active || return 0
    for cf_summary_file in "$CF_OUTPUT_DIR"/fact.*; do
        [ -f "$cf_summary_file" ] || continue
        case "$cf_summary_file" in */fact.tun_verified|*/fact.core_mihomo_new|*/fact.core_oxidns_new) continue ;; esac
        cf_notice "  $(cf_read_file "$cf_summary_file")"
    done
    for cf_summary_file in "$CF_OUTPUT_DIR"/note.*; do
        [ -f "$cf_summary_file" ] || continue
        cf_warn "$(cf_read_file "$cf_summary_file")"
    done
}

cf_verbose_options "$@"
cf_init "安装与维护"
(
set -eu
for cf_argument do
    case "$cf_argument" in --verbose|-v) ;; *) cf_error "未知参数：$cf_argument"; exit 2 ;; esac
done

AGENT_ROOT="/opt/configflow-agent"
MIHOMO_CONFIG_PATH=/etc/mihomo/config.yaml
OXIDNS_CONFIG_PATH=/etc/oxidns/config.yaml
MIHOMO_SERVICE_UNIT=mihomo
OXIDNS_SERVICE_UNIT=oxidns
MIHOMO_SERVICE_BINARY=mihomo
OXIDNS_SERVICE_BINARY=oxidns
STAGE_DIR=""
RESTART_ON_FAILURE=""
cleanup() {
    cleanup_status=$?
    cleanup_ipv4_forwarding "$cleanup_status"
    if [ "$cleanup_status" -ne 0 ]; then
        for stopped_service in $RESTART_ON_FAILURE; do
            if [ "$INIT_SYSTEM" = systemd ]; then
                systemctl start "configflow-agent-$stopped_service" || true
            else
                rc-service "configflow-agent-$stopped_service" start || true
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
# Included only in the ordinary Shell cohost installer, never the gateway wizard.
IPV4_CONFIG="/etc/sysctl.d/99-configflow-aio-forward.conf"
IPV4_MARKER="# Managed by ConfigFlow Shell AIO IPv4 forwarding v1"
IPV4_CONFIG_CREATED=0
IPV4_TEMP=""

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
    cf_note forwarding "IPv4 转发已设置持久化，尚未实际重启验证；其他配置或服务覆盖风险及原检查结果见详细日志。"
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
if command -v rc-service >/dev/null 2>&1 && command -v rc-update >/dev/null 2>&1; then
    INIT_SYSTEM=openrc
elif command -v systemctl >/dev/null 2>&1; then
    INIT_SYSTEM=systemd
else
    fail "需要 systemd 或 OpenRC 管理已安装的 Mihomo 和 OxiDNS。"
fi

# Shared by the native single-Agent and All-in-One installers. Call only after
# the user chooses installation and the architecture/init checks have passed.
agent_dependency_fail() { printf '%s\n' "$*" >&2; exit 1; }
agent_add_package() {
    case " ${AGENT_PACKAGES} " in *" $1 "*) ;; *) AGENT_PACKAGES="${AGENT_PACKAGES:+${AGENT_PACKAGES} }$1" ;; esac
}
agent_detect_package_manager() {
    if command -v apk >/dev/null 2>&1; then AGENT_PACKAGE_MANAGER=apk
    elif command -v apt-get >/dev/null 2>&1; then AGENT_PACKAGE_MANAGER=apt-get
    elif command -v dnf >/dev/null 2>&1; then AGENT_PACKAGE_MANAGER=dnf
    elif command -v yum >/dev/null 2>&1; then AGENT_PACKAGE_MANAGER=yum
    elif command -v pacman >/dev/null 2>&1; then AGENT_PACKAGE_MANAGER=pacman
    elif command -v zypper >/dev/null 2>&1; then AGENT_PACKAGE_MANAGER=zypper
    else AGENT_PACKAGE_MANAGER=""
    fi
}
agent_collect_dependencies() {
    AGENT_MISSING_DEPENDENCIES=""
    AGENT_PACKAGES=""
    # Extra commands are supplied only by installers that actually need them.
    # Existing BusyBox applets count as available; do not replace working tools.
    for agent_tool in awk basename cat chmod cp dirname grep head mkdir mktemp mv readlink rm sed wc "$@"; do
        if [ "$agent_tool" = zoneinfo ]; then
            # OxiDNS cron plugins resolve IANA timezones from the system
            # database; minimal Alpine images ship without it.
            if [ ! -f "${TZDIR:-/usr/share/zoneinfo}/Asia/Shanghai" ]; then
                AGENT_MISSING_DEPENDENCIES="${AGENT_MISSING_DEPENDENCIES:+${AGENT_MISSING_DEPENDENCIES} }时区数据"
                if [ "$AGENT_PACKAGE_MANAGER" = zypper ]; then agent_add_package timezone; else agent_add_package tzdata; fi
            fi
            continue
        fi
        if ! command -v "$agent_tool" >/dev/null 2>&1; then
            AGENT_MISSING_DEPENDENCIES="${AGENT_MISSING_DEPENDENCIES:+${AGENT_MISSING_DEPENDENCIES} }$agent_tool"
            case "$agent_tool" in
                awk) agent_add_package gawk ;;
                grep|sed) agent_add_package "$agent_tool" ;;
                cmp) agent_add_package diffutils ;;
                sysctl)
                    case "$AGENT_PACKAGE_MANAGER" in
                        pacman|dnf|yum) agent_add_package procps-ng ;;
                        *) agent_add_package procps ;;
                    esac
                    ;;
                *) agent_add_package coreutils ;;
            esac
        fi
    done
    if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
        AGENT_MISSING_DEPENDENCIES="${AGENT_MISSING_DEPENDENCIES:+${AGENT_MISSING_DEPENDENCIES} }curl/wget"
        agent_add_package curl
        agent_add_package ca-certificates
    fi
}
agent_install_packages() {
    if [ -z "$AGENT_PACKAGE_MANAGER" ]; then
        agent_dependency_fail "Agent 安装缺少工具：${AGENT_MISSING_DEPENDENCIES}。未找到支持的包管理器（apk、apt-get、dnf、yum、pacman、zypper）；请手动安装 ${AGENT_PACKAGES} 后重新运行。"
    fi
    printf '%s\n' "Agent 安装缺少工具：${AGENT_MISSING_DEPENDENCIES}；使用 ${AGENT_PACKAGE_MANAGER} 安装 ${AGENT_PACKAGES}。系统依赖会保留，不随 Agent 卸载。"
    case "$AGENT_PACKAGE_MANAGER" in
        apk) apk add --no-cache "$@" ;;
        apt-get) apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "$@" ;;
        dnf|yum) "$AGENT_PACKAGE_MANAGER" install -y "$@" ;;
        # Do not refresh only indexes on Arch; a full upgrade is user-managed.
        pacman) pacman -S --needed --noconfirm "$@" ;;
        zypper) zypper --non-interactive install --no-recommends "$@" ;;
    esac
}
agent_ensure_dependencies() {
    agent_detect_package_manager
    agent_collect_dependencies "$@"
    if [ -n "$AGENT_MISSING_DEPENDENCIES" ]; then
        # Package names are selected from the fixed list above.
        if ! agent_install_packages $AGENT_PACKAGES; then
            agent_dependency_fail "Agent 系统依赖安装失败，尚未下载或安装 Agent。请检查网络、软件源及包管理器锁后重试；Arch 请先运行 pacman -Syu。缺少的工具：${AGENT_MISSING_DEPENDENCIES}。"
        fi
        agent_collect_dependencies "$@"
        [ -z "$AGENT_MISSING_DEPENDENCIES" ] || agent_dependency_fail "包管理器执行后仍缺少 Agent 工具：${AGENT_MISSING_DEPENDENCIES}。请检查 PATH，手动安装 ${AGENT_PACKAGES} 后重试；尚未下载或安装 Agent。"
    fi
}

cf_ui '此安装器只接管已安装的 Mihomo 和 OxiDNS，不安装核心、不改核心配置、不重启核心。'
cf_ui "Mihomo 已启用 TUN 时，自动检查运行环境；本机加载 TUN 驱动并设置开机加载，容器缺少设备时给出宿主机处理提示。"
cf_ui "普通 Shell 同机安装将自动开启并持久化当前 Linux 的 IPv4 转发。"
if [ "${_CF_NESTED:-0}" -eq 0 ]; then
    cf_ui "1) 安装或升级两个独立 Agent"
    cf_ui "2) 卸载两个 Agent（保留两个核心、核心配置及发布记录）"
    cf_ui "3) 取消"
    cf_prompt '请输入选项 [1-3]: '
fi
read -r choice < /dev/tty
case "$choice" in
    1)
        cf_step "检查安装环境与依赖"
        case "$(uname -m)" in
            x86_64|amd64) AGENT_ARCH=linux-amd64 ;;
            aarch64|arm64) AGENT_ARCH=linux-arm64 ;;
            armv7l|armhf) AGENT_ARCH=linux-armv7 ;;
            *) fail "不支持的 CPU 架构" ;;
        esac
        agent_ensure_dependencies zoneinfo sysctl cmp ln
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
        check_ipv4_forwarding_ownership
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
        check_ipv4_forwarding_ownership
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
                systemctl disable "$AGENT_UNIT"
                rm -f "/etc/systemd/system/$AGENT_UNIT.service"
            else
                rc-update del "$AGENT_UNIT" default
                rm -f "/etc/init.d/$AGENT_UNIT"
            fi
            rm -rf "$AGENT_DIR"
            rm -f "$AGENT_LOG" "$AGENT_LOG".* "/etc/logrotate.d/$AGENT_UNIT"
        done
        if [ "$INIT_SYSTEM" = systemd ]; then systemctl daemon-reload; else rc-update -u; fi
        cf_complete "两个 Agent 已卸载；核心服务、配置及发布记录未改动。"
        cf_ui "IPv4 转发及其 sysctl.d 持久化设置保留，避免影响仍在运行的核心。"
        exit 0
        ;;
    *) cf_complete "已取消"; exit 0 ;;
esac

check_ipv4_forwarding_ownership
# Read-only core checks precede even the Agent download. The existing service
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
BINARY_BASE=https://cf.test/api/agents/download
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
{
  "server_url": "https://cf.test",
  "agent_name": "ordinary-mihomo",
  "agent_host": "0.0.0.0",
  "agent_port": 8080,
  "agent_ip": "192.168.8.3",
  "service_type": "mihomo",
  "service_name": "mihomo",
  "deployment_method": "shell",
  "native_aio": true,
  "agent_service_unit": "configflow-agent-mihomo",
  "config_path": "/etc/mihomo/config.yaml",
  "service_manager": "__CONFIGFLOW_INIT__",
  "service_unit": "mihomo",
  "service_binary": "mihomo",
  "restart_command": "__CONFIGFLOW_RESTART__",
  "heartbeat_interval": 30,
  "deployment_health_timeout": 30,
  "agent_id": "__CONFIGFLOW_AGENT_ID__",
  "token": "__CONFIGFLOW_TOKEN__"
}
CONFIGFLOW_MIHOMO_JSON_EOF
cat > "$STAGE_DIR/oxidns-config.json" <<'CONFIGFLOW_OXIDNS_JSON_EOF'
{
  "server_url": "https://cf.test",
  "agent_name": "ordinary-oxidns",
  "agent_host": "0.0.0.0",
  "agent_port": 8082,
  "agent_ip": "192.168.8.3",
  "service_type": "oxidns",
  "service_name": "oxidns",
  "deployment_method": "shell",
  "native_aio": true,
  "agent_service_unit": "configflow-agent-oxidns",
  "config_path": "/etc/oxidns/config.yaml",
  "service_manager": "__CONFIGFLOW_INIT__",
  "service_unit": "oxidns",
  "service_binary": "oxidns",
  "restart_command": "__CONFIGFLOW_RESTART__",
  "heartbeat_interval": 30,
  "deployment_health_timeout": 30,
  "agent_id": "__CONFIGFLOW_AGENT_ID__",
  "token": "__CONFIGFLOW_TOKEN__"
}
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
cf_step "准备 TUN 驱动与开机加载"
if "$BINARY_TMP" -config "$STAGE_DIR/mihomo-config.json" -prepare-tun; then
    :
else
    tun_status=$?
    echo "TUN 准备未完成。若提示未知 -prepare-tun 参数，请使用包含 TUN 自动准备功能的新 Agent 构建；否则按上方原因处理后重跑。现有 Agent 尚未停止。" >&2
    exit "$tun_status"
fi
cf_step_done "TUN 准备完成（具体处理结果见日志）"

# Inspect the pair together so the environment check can distinguish their
# listeners from unrelated port owners. Diagnostics include repair guidance;
# the check never changes capabilities, forwarding or either core config.
cf_step "检查运行环境"
if cf_environment_check "$BINARY_TMP" -config "$STAGE_DIR/mihomo-config.json" \
    -environment-peer-config "$STAGE_DIR/oxidns-config.json" -check-environment; then
    :
else
    environment_status=$?
    echo "若提示未知 -check-environment 参数，请更新 ConfigFlow 并重新构建/提供 1.8.19-go 或更高的 Agent 下载文件；否则按上方环境问题修复后重跑。" >&2
    exit "$environment_status"
fi
cf_step_done "运行环境检查通过"

prepare_ipv4_forwarding

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
check_ipv4_forwarding_ownership
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
    install_ipv4_forwarding_hook
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
pidfile="/run/$AGENT_UNIT.pid"
output_log="$AGENT_LOG"
error_log="$AGENT_LOG"
directory="$AGENT_DIR"
command_background="yes"
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
        systemctl is-active --quiet "$AGENT_UNIT"
    else
        rc-update add "$AGENT_UNIT" default
        rc-service "$AGENT_UNIT" start
        rc-service "$AGENT_UNIT" status
    fi
done
persist_ipv4_forwarding
RESTART_ON_FAILURE=""
cf_step_done "两个 Agent 服务已启动"
summarize_ipv4_forwarding
cf_fact agent_mihomo "Mihomo Agent：端口 8080，服务已启动"
cf_fact agent_oxidns "OxiDNS Agent：端口 8082，服务已启动"
if [ "$(cf_fact_read core_mihomo_new)" != yes ] || [ "$(cf_fact_read core_oxidns_new)" != yes ]; then
    cf_note dns_existing "已有核心配置已保留；发布也会保留自定义 DNS 端口和 dns-hijack，请将 OxiDNS 上游与 Mihomo 实际 DNS 监听地址及端口对应。"
fi
cf_complete "安装完成"
)
cf_installer_exit_status=$?
cf_finish "$cf_installer_exit_status"
exit "$cf_installer_exit_status"
