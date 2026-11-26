# Included only in the opt-in native OxiDNS HA installer.
agent_ha_fail() { agent_dependency_fail "DNS 高可用准备失败：$*"; }
agent_ha_preserve_existing() {
    DNS_HA_EXISTING_CONFIG=""
    DNS_HA_EFFECTIVE_PORT="{agent_port}"
    for agent_ha_config in "$CONFIG_FILE" "$AGENT_DIR/config.json"; do
        [ -e "$agent_ha_config" ] || [ -L "$agent_ha_config" ] || continue
        [ -f "$agent_ha_config" ] && [ -r "$agent_ha_config" ] || agent_ha_fail "原 Agent 配置不可读取，已停止：$agent_ha_config"
        if ! jq -e 'type == "object" and .service_type == "oxidns" and
            (.service_manager == "systemd" or .service_manager == "openrc") and
            (.config_path | type == "string" and startswith("/")) and
            (.agent_port | type == "number" and . >= 1 and . <= 65535 and . == floor) and
            (.service_unit | type == "string" and test("^[A-Za-z0-9_.@-]+$"))' "$agent_ha_config" >/dev/null; then
            agent_ha_fail "原 Agent 配置无效、不是 OxiDNS，或使用自定义启停命令；已保留原配置并停止，不能自动接管。"
        fi
        agent_ha_manager=$(jq -r '.service_manager' "$agent_ha_config")
        [ "$agent_ha_manager" = "$INIT_SYSTEM" ] || agent_ha_fail "原 Agent 服务管理器与本机不符，已保留原配置并停止。"
        DNS_HA_EXISTING_CONFIG="$agent_ha_config"
        DNS_HA_EFFECTIVE_PORT=$(jq -r '.agent_port' "$agent_ha_config")
        SERVICE_UNIT=$(jq -r '.service_unit' "$agent_ha_config")
        SERVICE_UNIT=${SERVICE_UNIT%.service}
        cf_ui "检测到现有 OxiDNS Agent：保留完整配置、身份、端口和核心路径，仅补齐高可用依赖并更新 Agent。"
        break
    done
}
agent_ha_install_dependencies() (
    # A subshell confines cleanup traps to package preparation. Never call this
    # function in an if/&&/|| condition: keep errexit active inside it.
    set -eu
    agent_ha_mask_owned=0
    agent_ha_fresh_config=0
    agent_ha_mask=/run/systemd/system/keepalived.service
    agent_ha_empty_config='# ConfigFlow: HA dependencies prepared; configure VIP through pairing.'
    agent_ha_preserve_package_default() {
        [ "$agent_ha_fresh_config" = 1 ] && [ -f /etc/keepalived/keepalived.conf ] || return 0
        if [ "$(cat /etc/keepalived/keepalived.conf)" != "$agent_ha_empty_config" ]; then
            agent_ha_sample=$(mktemp /etc/keepalived/keepalived.conf.package-default.XXXXXX) || return 1
            cp -p /etc/keepalived/keepalived.conf "$agent_ha_sample" || return 1
            printf '%s\n' "$agent_ha_empty_config" > /etc/keepalived/keepalived.conf || return 1
            cf_ui "已备份新安装包的 keepalived 示例配置：$agent_ha_sample；未配置 VIP。"
        fi
        agent_ha_fresh_config=0
    }
    agent_ha_cleanup() {
        agent_ha_status=$?
        trap - 0 HUP INT TERM
        agent_ha_preserve_package_default || agent_ha_status=1
        if [ "$agent_ha_mask_owned" = 1 ]; then
            # New installs stay disabled until pairing, including failed package transactions.
            systemctl disable keepalived.service >/dev/null 2>&1 || agent_ha_status=1
            if [ -L "$agent_ha_mask" ] && [ "$(readlink "$agent_ha_mask")" = /dev/null ]; then
                rm -f "$agent_ha_mask" || agent_ha_status=1
                systemctl daemon-reload || agent_ha_status=1
            else
                printf '%s\n' '高可用准备期间 keepalived 的临时服务屏蔽被外部修改，未删除，请核对。' >&2
                agent_ha_status=1
            fi
        fi
        exit "$agent_ha_status"
    }
    trap agent_ha_cleanup 0
    trap 'exit 129' HUP
    trap 'exit 130' INT
    trap 'exit 143' TERM
    agent_detect_package_manager
    agent_collect_dependencies keepalived ha_ip ha_service jq
    agent_ha_installs_keepalived=0
    case " $AGENT_PACKAGES " in *' keepalived '*) agent_ha_installs_keepalived=1 ;; esac
    if [ "$agent_ha_installs_keepalived" = 1 ]; then
        # A package install must not revive an unknown old VRRP configuration.
        if [ -L /etc/keepalived/keepalived.conf ] || { [ -e /etc/keepalived/keepalived.conf ] && [ "$(cat /etc/keepalived/keepalived.conf)" != "$agent_ha_empty_config" ]; }; then
            agent_ha_fail "需要补装 keepalived，但本机已有 keepalived 配置。为避免意外启用旧 VIP，本次未安装高可用依赖；请先恢复原 keepalived 安装后重试。"
        fi
        agent_ha_fresh_config=1
        if [ "$INIT_SYSTEM" = systemd ]; then
            agent_ha_enabled=$(systemctl is-enabled keepalived.service 2>/dev/null) || agent_ha_enabled="${agent_ha_enabled:-not-found}"
            case "$agent_ha_enabled" in enabled|enabled-runtime) agent_ha_fail "keepalived 已设置开机启动但安装不完整，已保留原设置并停止。" ;; esac
            if systemctl is-active --quiet keepalived.service; then
                agent_ha_fail "keepalived 服务仍在运行但安装不完整，已停止；请先修复原服务。"
            fi
            # /etc has higher priority than /run. Do not override an existing
            # administrator unit, drop-in or persistent mask to install packages.
            if [ -e /etc/systemd/system/keepalived.service ] || [ -L /etc/systemd/system/keepalived.service ] || [ -d /etc/systemd/system/keepalived.service.d ]; then
                agent_ha_fail "本机已有自定义或屏蔽的 keepalived 服务，不能自动覆盖。"
            fi
            if [ -e "$agent_ha_mask" ] || [ -L "$agent_ha_mask" ]; then
                agent_ha_fail "本机已有 keepalived 临时服务设置，不能自动覆盖。"
            fi
            # Block package post-install scripts from starting new VRRP before
            # pairing. This runtime-only mask disappears on reboot and is removed
            # on both successful and failed package transactions.
            agent_ha_mask_status=0
            systemctl mask --runtime keepalived.service || agent_ha_mask_status=$?
            if [ -L "$agent_ha_mask" ] && [ "$(readlink "$agent_ha_mask")" = /dev/null ]; then
                agent_ha_mask_owned=1
            fi
            [ "$agent_ha_mask_status" = 0 ] && [ "$agent_ha_mask_owned" = 1 ] || agent_ha_fail "无法临时阻止 keepalived 在安装包时自动启动。"
        elif [ "$AGENT_PACKAGE_MANAGER" = apt-get ]; then
            # Alpine apk does not start new services. For Debian with OpenRC,
            # respect its existing policy instead of replacing policy-rc.d.
            agent_ha_policy_status=0
            if [ -x /usr/sbin/policy-rc.d ]; then
                /usr/sbin/policy-rc.d keepalived start >/dev/null 2>&1 || agent_ha_policy_status=$?
            fi
            [ "$agent_ha_policy_status" = 101 ] || agent_ha_fail "Debian/Ubuntu 的 OpenRC 环境未禁止软件包自动启动服务，无法安全自动准备；已保留系统策略并停止。"
        fi
    fi
    # Install the original missing set. Recollecting while our runtime mask is
    # present would mistake an absent systemd unit for an installed one.
    if [ -n "$AGENT_MISSING_DEPENDENCIES" ]; then
        if ! agent_install_packages $AGENT_PACKAGES; then
            agent_ha_fail "系统依赖安装失败，尚未安装核心或 Agent。请检查网络、软件源及包管理器锁后重试。"
        fi
    fi
    agent_collect_dependencies keepalived ha_ip ha_service jq
    [ -z "$AGENT_MISSING_DEPENDENCIES" ] || agent_ha_fail "包安装后仍缺少工具：${AGENT_MISSING_DEPENDENCIES}，尚未安装核心或 Agent。"
    agent_ha_preserve_package_default
    keepalived --version >/dev/null 2>&1 || agent_ha_fail "keepalived 无法运行，请检查包安装结果。"
    ip -j -4 addr show >/dev/null 2>&1 || agent_ha_fail "ip 命令不支持所需 JSON 输出，请检查 iproute2 安装结果。"
)
agent_prepare_dns_ha() {
    cf_step "自动准备 DNS 高可用环境"
    [ "$(uname -s)" = Linux ] || agent_ha_fail "仅支持 Linux。"
    agent_ha_install_dependencies
    if [ "$INIT_SYSTEM" = systemd ]; then
        agent_ha_unit=$(systemctl show keepalived.service --property=LoadState --value)
        [ "$agent_ha_unit" = loaded ] || agent_ha_fail "keepalived systemd 服务未就绪（$agent_ha_unit），未启用 VIP。"
    else
        [ -x /etc/init.d/keepalived ] || agent_ha_fail "缺少 keepalived OpenRC 服务。"
    fi
    agent_ha_preserve_existing
    cf_fact dns_ha "高可用依赖：keepalived、iproute2 与服务管理已就绪；VIP 尚未配置，请在面板验证与配对。"
    cf_step_done "DNS 高可用依赖已准备，原有 keepalived 配置与运行状态保留"
}
