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
