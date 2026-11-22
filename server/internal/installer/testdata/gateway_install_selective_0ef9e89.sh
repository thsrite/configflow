#!/bin/sh
# ConfigFlow Linux gateway installation. No forwarding or TUN interception here.
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
umask 077
GATEWAY_STAGE=""
GATEWAY_LOCK=""
GATEWAY_CHANGED=0
fail() { printf '%s\n' "$*" >&2; exit 1; }
cleanup_gateway() {
    gateway_exit=$?
    if [ "$gateway_exit" -ne 0 ] && [ "$GATEWAY_CHANGED" -eq 1 ]; then
        echo '安装未完成。已安装的核心和原有配置均保留；修复上方错误后重新运行同一命令可继续。尚未应用网关转发，请不要修改客户端网关。' >&2
    fi
    [ -z "$GATEWAY_STAGE" ] || rm -rf "$GATEWAY_STAGE"
    [ -z "$GATEWAY_LOCK" ] || rmdir "$GATEWAY_LOCK"
    return "$gateway_exit"
}
trap cleanup_gateway EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
need() { command -v "$1" >/dev/null 2>&1 || fail "缺少命令 $1。Debian/Ubuntu: apt-get update && apt-get install -y ca-certificates curl iproute2 nftables tar gzip coreutils；Alpine: apk add ca-certificates curl iproute2 nftables tar gzip coreutils。安装依赖后重跑。"; }
download() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL --connect-timeout 15 --max-time 300 --retry 2 -o "$2" "$1"
    else
        wget -q -T 60 -O "$2" "$1"
    fi
}
verified_download() {
    download "$1" "$3" || fail "下载失败；请检查访问 GitHub/ConfigFlow 的网络后重跑。"
    printf '%s  %s\n' "$2" "$3" | sha256sum -c - >/dev/null || fail '下载文件 SHA256 不匹配；本次未执行下载文件，请重新生成安装命令。'
}
select_core() {
    CORE=$1
    CORE_DIR="/etc/$CORE"
    CORE_CONFIG="$CORE_DIR/config.yaml"
    CORE_BINARY="/usr/local/bin/$CORE"
    CORE_MARKER="$CORE_DIR/.configflow-gateway-core-v1"
    if [ "$GATEWAY_INIT" = systemd ]; then
        CORE_UNIT="/etc/systemd/system/$CORE.service"
    else
        CORE_UNIT="/etc/init.d/$CORE"
    fi
}
core_running() {
    if [ "$GATEWAY_INIT" = systemd ]; then systemctl is-active --quiet "$CORE"; else rc-service "$CORE" status >/dev/null 2>&1; fi
}
check_ordinary_layout() {
    # Existing cores are only attached when their exact binary/config paths can
    # be proved. No edits or restarts are performed for external services.
    [ -f "$CORE_CONFIG" ] && [ -x "$CORE_BINARY" ] && [ -f "$CORE_UNIT" ] || fail "$CORE 已有不完整或非标准安装。请使用普通同机 Agent 接入并填写现有路径；网关安装不会覆盖已有文件。"
    if [ "$GATEWAY_INIT" = systemd ]; then
        core_exec=$(systemctl show "$CORE" --property=ExecStart --value)
        printf '%s' "$core_exec" | grep -F "path=$CORE_BINARY ;" >/dev/null || fail "$CORE 服务使用其他二进制；不会覆盖，请通过普通同机安装接入。"
        printf '%s' "$core_exec" | grep -F "$CORE_CONFIG" >/dev/null || fail "$CORE 服务配置路径无法核对；不会覆盖，请通过普通同机安装接入。"
    else
        grep -Fx "command=\"$CORE_BINARY\"" "$CORE_UNIT" >/dev/null || fail "$CORE OpenRC 服务不符合标准布局；请通过普通同机安装接入。"
        grep '^command_args=' "$CORE_UNIT" | grep -F "$CORE_CONFIG" >/dev/null || fail "$CORE 服务配置路径无法核对；请通过普通同机安装接入。"
    fi
    core_running || fail "$CORE 现有服务未运行；请先检查并启动原服务，再运行安装命令。"
}
check_port_free() {
    # Only called for a new core; existing services have their listeners checked
    # by the Agent's paired environment preflight below.
    if ss -H -lntu | awk -v wanted="$1" '{ n=split($5,a,":"); if (a[n]==wanted) found=1 } END {exit !found}'; then
        # Debian and Ubuntu enable this local stub by default; name the fix.
        if [ "$1" = 53 ] && ss -H -lntup 2>/dev/null | awk '{ n=split($5,a,":"); if (a[n]=="53" && /systemd-resolve/) found=1 } END {exit !found}'; then
            fail "端口 53 被 systemd-resolved 的本机 DNS（127.0.0.53）占用，OxiDNS 需要使用此端口。可关闭其本机监听后重跑安装命令：mkdir -p /etc/systemd/resolved.conf.d && printf '[Resolve]\nDNSStubListener=no\n' > /etc/systemd/resolved.conf.d/configflow.conf && ln -sf /run/systemd/resolve/resolv.conf /etc/resolv.conf && systemctl restart systemd-resolved"
        fi
        fail "端口 $1 已被占用。请用 ss -lntup 检查所属服务并先处理冲突；安装器不会停止现有 DNS 服务。"
    fi
}

[ "$(id -u)" -eq 0 ] || fail '请在目标 Linux 上使用 root 运行命令（或 sudo sh）。'
[ "$(uname -s)" = Linux ] || fail 'Linux 网关安装只支持 Linux 系统。'
case "$(uname -m)" in
    x86_64|amd64)
        GATEWAY_ARCH=linux-amd64
        MIHOMO_URL=https://github.com/MetaCubeX/mihomo/releases/download/v1.19.32/mihomo-linux-amd64-compatible-v1.19.32.gz
        MIHOMO_SHA=ba3ce607747a07f948fc35780e108a4a7c7f552a38b9bd4d115f313ebcb89c20
        OXIDNS_URL=https://github.com/svenshi/oxidns/releases/download/v1.6.1/oxidns-x86_64-unknown-linux-musl.tar.gz
        OXIDNS_SHA=2ac8e103e03407a73c29da848f6ce2d524e751ce1da8a744aa9168fc1384e3b0
        AGENT_SHA=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
        ;;
    aarch64|arm64)
        GATEWAY_ARCH=linux-arm64
        MIHOMO_URL=https://github.com/MetaCubeX/mihomo/releases/download/v1.19.32/mihomo-linux-arm64-v1.19.32.gz
        MIHOMO_SHA=9dd862e28b46ff7d775f169cceebc28deccaa0a9e804237d421cd2571e0caba0
        OXIDNS_URL=https://github.com/svenshi/oxidns/releases/download/v1.6.1/oxidns-aarch64-unknown-linux-musl.tar.gz
        OXIDNS_SHA=d879d4b9199c5c911d296af4df8d9e974523fb98a6ee2418ae7274d152365e42
        AGENT_SHA=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
        ;;
    *) fail '目前网关一体安装支持 x86_64 / arm64；不支持当前 CPU 架构。' ;;
esac
cf_step "检查网关安装环境"
[ -n "$AGENT_SHA" ] || fail "ConfigFlow 服务器未提供 $GATEWAY_ARCH 的 Agent 构建，请更新服务器的 Agent 下载文件后重新生成命令。"
if command -v rc-service >/dev/null 2>&1 && command -v rc-update >/dev/null 2>&1; then
    GATEWAY_INIT=openrc
elif command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    GATEWAY_INIT=systemd
else
    fail '需要正在运行的 systemd 或 OpenRC。Docker/普通无 init 容器请使用其他安装方式。'
fi
for dependency in ip nft ss tar gzip sha256sum awk sed grep mktemp readlink head cmp; do need "$dependency"; done
command -v curl >/dev/null 2>&1 || need wget
# OxiDNS cron plugins resolve IANA timezones from the system database; minimal
# Alpine images ship without it. The package stays installed like other deps.
if [ ! -f "${TZDIR:-/usr/share/zoneinfo}/Asia/Shanghai" ]; then
    printf '%s\n' '缺少时区数据，正在安装 tzdata；系统依赖会保留。'
    if command -v apk >/dev/null 2>&1; then
        apk add --no-cache tzdata || fail '安装 tzdata 失败；请检查网络和软件源后运行 apk add tzdata，再重跑。'
    elif command -v apt-get >/dev/null 2>&1; then
        { apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends tzdata; } || fail '安装 tzdata 失败；请检查网络和软件源后运行 apt-get install -y tzdata，再重跑。'
    fi
    [ -f "${TZDIR:-/usr/share/zoneinfo}/Asia/Shanghai" ] || fail '缺少时区数据，OxiDNS 的定时任务无法使用时区。请手动安装 tzdata 后重跑。'
fi
[ -c /dev/net/tun ] || fail '缺少 /dev/net/tun。请先在宿主机/LXC 配置中开放 TUN，再重跑；安装器不会修改宿主机权限。'
nft -j list ruleset >/dev/null || fail '无法读取 nftables，缺少 CAP_NET_ADMIN 或内核 nftables 支持；请检查宿主机/LXC 权限。'
[ -r /proc/sys/net/ipv4/ip_forward ] || fail '无法读取 IPv4 转发设置，请检查内核网络命名空间。'
for directory in /etc /usr/local /usr/local/bin /opt /opt/configflow-agent /run; do
    [ ! -L "$directory" ] || fail "安装目录不能是符号链接：$directory"
done
for legacy in /opt/configflow-agent/config*.json /etc/systemd/system/configflow-agent.service /etc/init.d/configflow-agent /usr/local/bin/configflow-agent; do
    [ ! -e "$legacy" ] && [ ! -L "$legacy" ] || fail '检测到旧版共享 Agent，请先通过原卸载流程移除 Agent 并保留核心配置。'
done
MIHOMO_DOWNLOAD=0
OXIDNS_DOWNLOAD=0
for core in mihomo oxidns; do
    select_core "$core"
    for path in "$CORE_DIR" "$CORE_CONFIG" "$CORE_BINARY" "$CORE_MARKER" "$CORE_UNIT"; do
        [ ! -L "$path" ] || fail "已有核心路径是符号链接，不能自动接管：$path"
    done
    if [ -f "$CORE_MARKER" ]; then
        [ "$(cat "$CORE_MARKER")" = 'ConfigFlow gateway core v1' ] || fail "核心所有权标识无效：$CORE_MARKER"
    elif [ -e "$CORE_DIR" ] || [ -e "$CORE_BINARY" ] || [ -e "$CORE_UNIT" ]; then
        check_ordinary_layout
    else
        if [ "$core" = mihomo ]; then check_port_free 1053; check_port_free 9090; else check_port_free 53; check_port_free 9199; fi
        if [ "$GATEWAY_INIT" = systemd ] && [ "$(systemctl show "$core" --property=LoadState --value)" != not-found ]; then
            fail "$core 已存在其他路径的系统服务，请先通过普通同机 Agent 安装接入。"
        fi
    fi
    if [ -e "$CORE_BINARY" ]; then
        [ -f "$CORE_BINARY" ] && [ -x "$CORE_BINARY" ] || fail "现有 $core 二进制不是可执行普通文件；请人工修复，安装器不会覆盖。"
    elif [ "$core" = mihomo ]; then
        MIHOMO_DOWNLOAD=1
    else
        OXIDNS_DOWNLOAD=1
    fi
    [ ! -e "$CORE_CONFIG" ] || [ -f "$CORE_CONFIG" ] || fail "现有 $core 配置不是普通文件；请人工修复，安装器不会覆盖。"
done
mkdir /run/configflow-gateway-install.lock 2>/dev/null || fail '另一个网关安装正在运行；若之前异常断电，请确认无安装进程后删除 /run/configflow-gateway-install.lock 再重跑。'
GATEWAY_LOCK=/run/configflow-gateway-install.lock
GATEWAY_STAGE=$(mktemp -d /tmp/configflow-gateway.XXXXXX)
export GATEWAY_STAGE
BINARY_BASE=https://cf.test/api/agents/download
cf_step "校验核心并下载所需程序"
if [ "$MIHOMO_DOWNLOAD" -eq 1 ]; then
    verified_download "$MIHOMO_URL" "$MIHOMO_SHA" "$GATEWAY_STAGE/mihomo.gz"
fi
if [ "$OXIDNS_DOWNLOAD" -eq 1 ]; then
    verified_download "$OXIDNS_URL" "$OXIDNS_SHA" "$GATEWAY_STAGE/oxidns.tar.gz"
fi
verified_download "$BINARY_BASE/configflow-agent-$GATEWAY_ARCH" "$AGENT_SHA" "$GATEWAY_STAGE/configflow-agent"
MIHOMO_VERSION=v1.19.32
OXIDNS_VERSION=v1.6.1
if [ "$MIHOMO_DOWNLOAD" -eq 1 ]; then
    gzip -dc "$GATEWAY_STAGE/mihomo.gz" > "$GATEWAY_STAGE/mihomo"
    chmod 755 "$GATEWAY_STAGE/mihomo"
    "$GATEWAY_STAGE/mihomo" -v | grep -F "$MIHOMO_VERSION" >/dev/null || fail 'Mihomo 下载版本与固定版本不一致。'
fi
if [ "$OXIDNS_DOWNLOAD" -eq 1 ]; then
    # Only extract the explicitly named executable from the authenticated archive.
    # Never unpack archive-controlled directories or service/config files.
    tar -xzOf "$GATEWAY_STAGE/oxidns.tar.gz" oxidns > "$GATEWAY_STAGE/oxidns"
    chmod 755 "$GATEWAY_STAGE/oxidns"
    "$GATEWAY_STAGE/oxidns" --version | grep -F "${OXIDNS_VERSION#v}" >/dev/null || fail 'OxiDNS 下载版本与固定版本不一致。'
fi
chmod 755 "$GATEWAY_STAGE/configflow-agent"
"$GATEWAY_STAGE/configflow-agent" -version >/dev/null || fail 'Agent 无法在当前系统运行；请重新构建匹配版本。'
agent_version=$("$GATEWAY_STAGE/configflow-agent" -version)
printf '%s\n' "$agent_version" | awk -F '[-.]' '/^[0-9]+\.[0-9]+\.[0-9]+(-go)?$/ && ($1>1 || ($1==1 && ($2>8 || ($2==8 && $3>=25)))) {ok=1} END {exit !ok}' || fail '网关向导需要 Agent 1.8.25-go 或更新，请更新服务器的 Agent 构建并重新生成命令。'
cat > "$GATEWAY_STAGE/mihomo.yaml" <<'CONFIGFLOW_GATEWAY_MIHOMO_BOOTSTRAP'
# ConfigFlow bootstrap only: final gateway configuration is published in the UI.
mode: rule
log-level: info
allow-lan: false
ipv6: false
external-controller: 127.0.0.1:9090
tun:
  enable: false
dns:
  enable: true
  listen: 127.0.0.1:1053
  ipv6: false
  enhanced-mode: redir-host
  nameserver:
    - https://223.5.5.5/dns-query
rules:
  - MATCH,DIRECT
CONFIGFLOW_GATEWAY_MIHOMO_BOOTSTRAP
cat > "$GATEWAY_STAGE/oxidns.yaml" <<'CONFIGFLOW_GATEWAY_OXIDNS_BOOTSTRAP'
# ConfigFlow bootstrap: local DNS only, final LAN split routing comes from UI.
log:
  level: info
api:
  http:
    listen: 127.0.0.1:9199
plugins:
  - tag: bootstrap_upstream
    type: forward
    args:
      upstreams:
        - addr: https://223.5.5.5/dns-query
  - tag: bootstrap_main
    type: sequence
    args:
      - exec: $bootstrap_upstream
      - exec: accept
  - tag: bootstrap_udp
    type: udp_server
    args:
      entry: bootstrap_main
      listen: 127.0.0.1:53
  - tag: bootstrap_tcp
    type: tcp_server
    args:
      entry: bootstrap_main
      listen: 127.0.0.1:53
CONFIGFLOW_GATEWAY_OXIDNS_BOOTSTRAP
# Validate the actual binary/config pair that each service will use before
# installing anything. Complete existing cores never need a release download.
for core in mihomo oxidns; do
    select_core "$core"
    CHECK_BINARY="$CORE_BINARY"
    [ -x "$CHECK_BINARY" ] || CHECK_BINARY="$GATEWAY_STAGE/$core"
    if [ -f "$CORE_CONFIG" ]; then
        CHECK_CONFIG="$CORE_CONFIG"
        CHECK_DIR="$CORE_DIR"
    else
        CHECK_CONFIG="$GATEWAY_STAGE/$core.yaml"
        CHECK_DIR="$GATEWAY_STAGE"
    fi
    if [ "$core" = mihomo ]; then
        if [ "$CHECK_BINARY" = "$CORE_BINARY" ]; then
            # Match the same stable version token accepted by Agent runtime;
            # alpha/custom suffixes cannot be silently treated as a release.
            "$CORE_BINARY" -v | awk 'tolower($1)=="mihomo" {i=(tolower($2)=="meta" ? 3 : 2); v=$i; if (v ~ /^v[0-9]+\.[0-9]+\.[0-9]+$/) {sub(/^v/,"",v); split(v,a,"."); if (a[1]>1 || (a[1]==1 && a[2]>=19)) ok=1}} END {exit !ok}' || fail '现有 Mihomo 需要稳定版 1.19.0 或更新；请手动升级后重跑，安装器不会覆盖已有核心。'
        fi
        "$CHECK_BINARY" -t -d "$CHECK_DIR" -f "$CHECK_CONFIG" || fail 'Mihomo 现有或引导配置校验失败；尚未安装或覆盖核心。'
    else
        "$CHECK_BINARY" check -c "$CHECK_CONFIG" -d "$CHECK_DIR" || fail 'OxiDNS 现有或引导配置校验失败；尚未安装或覆盖核心。'
    fi
done
cf_success "下载与核心配置校验通过"
cf_step "安装或保留核心服务"
for core in mihomo oxidns; do
    select_core "$core"
    if [ ! -f "$CORE_MARKER" ] && [ -f "$CORE_CONFIG" ]; then
        cf_fact "service_$core" "保留并接入已有 ${core} 核心、配置和服务。"
        continue
    fi
    GATEWAY_CHANGED=1
    mkdir -p "$CORE_DIR" /usr/local/bin
    if [ ! -f "$CORE_MARKER" ]; then printf '%s\n' 'ConfigFlow gateway core v1' > "$CORE_MARKER"; fi
    if [ ! -e "$CORE_BINARY" ]; then
        cp "$GATEWAY_STAGE/$core" "$CORE_BINARY.new"
        chmod 755 "$CORE_BINARY.new"
        mv "$CORE_BINARY.new" "$CORE_BINARY"
    fi
    if [ ! -e "$CORE_CONFIG" ]; then
        cp "$GATEWAY_STAGE/$core.yaml" "$CORE_CONFIG.new"
        chmod 600 "$CORE_CONFIG.new"
        mv "$CORE_CONFIG.new" "$CORE_CONFIG"
        # Read by the Agent installer: no existing configuration was kept.
        cf_fact "core_${core}_new" yes
    fi
    if [ "$core" = mihomo ]; then
        CORE_ARGS="-d $CORE_DIR -f $CORE_CONFIG"
    else
        CORE_ARGS="start -c $CORE_CONFIG -d $CORE_DIR"
    fi
    if [ "$GATEWAY_INIT" = systemd ]; then
        mkdir -p /etc/systemd/system
        cat > "$GATEWAY_STAGE/$core.service" <<UNIT
# ConfigFlow gateway core v1
[Unit]
Description=ConfigFlow managed $core core
After=network-online.target
Wants=network-online.target
[Service]
Type=simple
User=root
WorkingDirectory=$CORE_DIR
ExecStart=$CORE_BINARY $CORE_ARGS
Restart=on-failure
RestartSec=5
LimitNOFILE=1048576
[Install]
WantedBy=multi-user.target
UNIT
    else
        mkdir -p /etc/init.d
        cat > "$GATEWAY_STAGE/$core.service" <<UNIT
#!/sbin/openrc-run
# ConfigFlow gateway core v1
name="ConfigFlow managed $core core"
command="$CORE_BINARY"
command_args="$CORE_ARGS"
command_background="yes"
pidfile="/run/$core.pid"
directory="$CORE_DIR"
output_log="/var/log/$core.log"
error_log="/var/log/$core.log"
depend() {
    need net
    after firewall
}
UNIT
    fi
    if [ -e "$CORE_UNIT" ]; then
        cmp -s "$GATEWAY_STAGE/$core.service" "$CORE_UNIT" || fail "$CORE_UNIT 已被修改；请人工核对服务后重跑，不会覆盖。"
    else
        cp "$GATEWAY_STAGE/$core.service" "$CORE_UNIT"
        if [ "$GATEWAY_INIT" = openrc ]; then chmod 755 "$CORE_UNIT"; else chmod 644 "$CORE_UNIT"; fi
    fi
done
if [ "$GATEWAY_INIT" = systemd ]; then systemctl daemon-reload; else rc-update -u; fi
for core in mihomo oxidns; do
    select_core "$core"
    [ -f "$CORE_MARKER" ] || continue
    if [ "$GATEWAY_INIT" = systemd ]; then
        systemctl enable "$core"
        systemctl start "$core"
    else
        rc-update add "$core" default
        rc-service "$core" start
    fi
    sleep 1
    core_running || fail "$core 服务启动失败；请用 journalctl -u $core 或 /var/log/$core.log 查看错误后重跑。"
done
cat > "$GATEWAY_STAGE/install-agents.sh" <<'CONFIGFLOW_GATEWAY_AIO_SCRIPT'
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
MIHOMO_SERVICE_BINARY=/usr/local/bin/mihomo
OXIDNS_SERVICE_BINARY=/usr/local/bin/oxidns
STAGE_DIR=""
RESTART_ON_FAILURE=""
cleanup() {
    cleanup_status=$?
    
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

cf_ui '为上一步准备好的 Mihomo 和 OxiDNS 安装 Agent，不改核心配置、不重启核心。'


if [ "${_CF_NESTED:-0}" -eq 0 ]; then
    cf_ui "1) 安装或升级两个独立 Agent"
    cf_ui "2) 卸载两个 Agent（保留两个核心、核心配置及发布记录）"
    cf_ui "3) 取消"
    cf_prompt '请输入选项 [1-3]: '
fi
choice=1 # Gateway setup already selected install in the web wizard
case "$choice" in
    1)
        cf_step "检查安装环境与依赖"
        case "$(uname -m)" in
            x86_64|amd64) AGENT_ARCH=linux-amd64 ;;
            aarch64|arm64) AGENT_ARCH=linux-arm64 ;;
            armv7l|armhf) AGENT_ARCH=linux-armv7 ;;
            *) fail "不支持的 CPU 架构" ;;
        esac
        agent_ensure_dependencies zoneinfo
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
        
        exit 0
        ;;
    *) cf_complete "已取消"; exit 0 ;;
esac


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
cp "$GATEWAY_STAGE/configflow-agent" "$BINARY_TMP"
chmod 755 "$BINARY_TMP"
"$BINARY_TMP" -version >/dev/null
cat > "$STAGE_DIR/mihomo-config.json" <<'CONFIGFLOW_MIHOMO_JSON_EOF'
{
  "server_url": "https://cf.test",
  "agent_name": "gateway-mihomo",
  "agent_host": "0.0.0.0",
  "agent_port": 8080,
  "agent_ip": "192.168.8.2",
  "service_type": "mihomo",
  "service_name": "mihomo",
  "deployment_method": "shell",
  "native_aio": true,
  "agent_service_unit": "configflow-agent-mihomo",
  "config_path": "/etc/mihomo/config.yaml",
  "service_manager": "__CONFIGFLOW_INIT__",
  "service_unit": "mihomo",
  "service_binary": "/usr/local/bin/mihomo",
  "restart_command": "__CONFIGFLOW_RESTART__",
  "heartbeat_interval": 30,
  "deployment_health_timeout": 30,
  "agent_id": "__CONFIGFLOW_AGENT_ID__",
  "token": "__CONFIGFLOW_TOKEN__",
  "installation_id": "install-session-0123456789",
  "installation_token": "session-token-01234567890123456789"
}
CONFIGFLOW_MIHOMO_JSON_EOF
cat > "$STAGE_DIR/oxidns-config.json" <<'CONFIGFLOW_OXIDNS_JSON_EOF'
{
  "server_url": "https://cf.test",
  "agent_name": "gateway-oxidns",
  "agent_host": "0.0.0.0",
  "agent_port": 8082,
  "agent_ip": "192.168.8.2",
  "service_type": "oxidns",
  "service_name": "oxidns",
  "deployment_method": "shell",
  "native_aio": true,
  "agent_service_unit": "configflow-agent-oxidns",
  "config_path": "/etc/oxidns/config.yaml",
  "service_manager": "__CONFIGFLOW_INIT__",
  "service_unit": "oxidns",
  "service_binary": "/usr/local/bin/oxidns",
  "restart_command": "__CONFIGFLOW_RESTART__",
  "heartbeat_interval": 30,
  "deployment_health_timeout": 30,
  "agent_id": "__CONFIGFLOW_AGENT_ID__",
  "token": "__CONFIGFLOW_TOKEN__",
  "installation_id": "install-session-0123456789",
  "installation_token": "session-token-01234567890123456789"
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

RESTART_ON_FAILURE=""
cf_step_done "两个 Agent 服务已启动"

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

CONFIGFLOW_GATEWAY_AIO_SCRIPT
cf_step "安装两个 Agent"
# Forwarding belongs to the explicit gateway apply, which saves and restores it;
# the environment check must not tell the user to persist it by hand.
CONFIGFLOW_FORWARDING_OWNER=gateway sh "$GATEWAY_STAGE/install-agents.sh"

ENROLLMENT_URL=https://cf.test/api/enroll
ENROLLMENT_TOKEN=session-token-01234567890123456789
if [ -n "$ENROLLMENT_URL" ]; then
    identity_value() {
        value=$(sed -n 's/.*"'"$2"'"[[:space:]]*:[[:space:]]*"\([^"\\]*\)".*/\1/p' "$1" | head -n 1)
        case "$value" in ''|*[!A-Za-z0-9_.-]*) return 1 ;; esac
        printf '%s' "$value"
    }
    cf_step '等待 Agent 注册并绑定安装向导'
    ready=0
    attempt=0
    while [ "$attempt" -lt 60 ]; do
        if MIHOMO_ID=$(identity_value /opt/configflow-agent/mihomo/config.json agent_id) &&
           MIHOMO_TOKEN=$(identity_value /opt/configflow-agent/mihomo/config.json token) &&
           OXIDNS_ID=$(identity_value /opt/configflow-agent/oxidns/config.json agent_id) &&
           OXIDNS_TOKEN=$(identity_value /opt/configflow-agent/oxidns/config.json token); then
            ready=1
            break
        fi
        attempt=$((attempt + 1))
        sleep 2
    done
    [ "$ready" -eq 1 ] || fail 'Agent 尚未完成注册。请检查 ConfigFlow 地址、Agent 日志和服务器网络，然后重跑同一命令；已装核心和配置会保留。'
    printf '{"agents":{"mihomo":{"id":"%s","token":"%s"},"oxidns":{"id":"%s","token":"%s"}}}\n' "$MIHOMO_ID" "$MIHOMO_TOKEN" "$OXIDNS_ID" "$OXIDNS_TOKEN" > "$GATEWAY_STAGE/enrollment.json"
    # Keep credentials out of process command lines and terminal output.
    printf 'Authorization: Bearer %s\nContent-Type: application/json\n' "$ENROLLMENT_TOKEN" > "$GATEWAY_STAGE/enrollment.headers"
    if command -v curl >/dev/null 2>&1; then
        curl -fsS --connect-timeout 15 --max-time 45 -H "@$GATEWAY_STAGE/enrollment.headers" --data-binary "@$GATEWAY_STAGE/enrollment.json" -o "$GATEWAY_STAGE/enrollment-result.json" "$ENROLLMENT_URL" || fail '安装完成，但向导绑定失败或会话已过期。请在网页检查会话后重新生成命令；不会重写现有核心。'
    else
        # BusyBox wget lacks @header-file. Session headers are not echoed, and
        # the restricted install token expires independently of Agent tokens.
        wget -q -T 45 --header="Authorization: Bearer $ENROLLMENT_TOKEN" --header='Content-Type: application/json' --post-file="$GATEWAY_STAGE/enrollment.json" -O "$GATEWAY_STAGE/enrollment-result.json" "$ENROLLMENT_URL" || fail '安装完成，但向导绑定失败或会话已过期。请回到网页检查安装会话。'
    fi
    grep -Eq '"success"[[:space:]]*:[[:space:]]*true' "$GATEWAY_STAGE/enrollment-result.json" || fail '服务器未确认安装绑定，请回到网页刷新安装状态。'
fi
cf_fact next_steps '请回到 ConfigFlow 安装向导，继续发布 Mihomo、发布 OxiDNS，再检查并应用网关转发。'
cf_note gateway '安装脚本未写入 TUN/转发配置，原有配置已保留；完成网页验证前不要修改客户端网关/DNS。'
cf_complete '核心及两个 Agent 已就绪'
)
cf_installer_exit_status=$?
cf_finish "$cf_installer_exit_status"
exit "$cf_installer_exit_status"
