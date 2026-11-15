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
