# Shared by the native single-Agent, All-in-One and Linux gateway installers. Call only after
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
        if [ "$agent_tool" = ha_ip ]; then
            # BusyBox ip lacks JSON output required by the HA Agent.
            if ! command -v ip >/dev/null 2>&1 || ! ip -j -4 addr show >/dev/null 2>&1; then
                AGENT_MISSING_DEPENDENCIES="${AGENT_MISSING_DEPENDENCIES:+${AGENT_MISSING_DEPENDENCIES} }iproute2(JSON)"
                case "$AGENT_PACKAGE_MANAGER" in
                    dnf|yum) agent_add_package iproute ;;
                    *) agent_add_package iproute2 ;;
                esac
            fi
            continue
        fi
        if [ "$agent_tool" = ha_service ]; then
            if [ "$INIT_SYSTEM" = openrc ]; then
                if [ ! -x /etc/init.d/keepalived ]; then
                    AGENT_MISSING_DEPENDENCIES="${AGENT_MISSING_DEPENDENCIES:+${AGENT_MISSING_DEPENDENCIES} }keepalived(OpenRC服务)"
                    if [ "$AGENT_PACKAGE_MANAGER" = apk ]; then agent_add_package keepalived-openrc; else agent_add_package keepalived; fi
                fi
            else
                agent_keepalived_unit=$(systemctl show keepalived.service --property=LoadState --value 2>/dev/null) || agent_keepalived_unit=""
                case "$agent_keepalived_unit" in
                    loaded|masked) ;;
                    *) AGENT_MISSING_DEPENDENCIES="${AGENT_MISSING_DEPENDENCIES:+${AGENT_MISSING_DEPENDENCIES} }keepalived(systemd服务)"; agent_add_package keepalived ;;
                esac
            fi
            continue
        fi
        if ! command -v "$agent_tool" >/dev/null 2>&1; then
            AGENT_MISSING_DEPENDENCIES="${AGENT_MISSING_DEPENDENCIES:+${AGENT_MISSING_DEPENDENCIES} }$agent_tool"
            case "$agent_tool" in
                awk) agent_add_package gawk ;;
                keepalived|jq) agent_add_package "$agent_tool" ;;
                grep|sed) agent_add_package "$agent_tool" ;;
                cmp) agent_add_package diffutils ;;
                # Linux gateway tools; BusyBox has no ss or nft.
                nft) agent_add_package nftables ;;
                ss)
                    case "$AGENT_PACKAGE_MANAGER" in
                        dnf|yum) agent_add_package iproute ;;
                        *) agent_add_package iproute2 ;;
                    esac
                    ;;
                tar|gzip) agent_add_package "$agent_tool" ;;
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
# A command the user can copy when automatic installation cannot finish.
agent_manual_install_command() {
    case "$AGENT_PACKAGE_MANAGER" in
        apk) printf 'apk add --no-cache %s' "$AGENT_PACKAGES" ;;
        apt-get) printf 'apt-get update && apt-get install -y --no-install-recommends %s' "$AGENT_PACKAGES" ;;
        dnf|yum) printf '%s install -y %s' "$AGENT_PACKAGE_MANAGER" "$AGENT_PACKAGES" ;;
        pacman) printf 'pacman -S --needed %s' "$AGENT_PACKAGES" ;;
        zypper) printf 'zypper install %s' "$AGENT_PACKAGES" ;;
    esac
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
            agent_dependency_fail "Agent 系统依赖安装失败，尚未下载或安装 Agent。请检查网络、软件源及包管理器锁后重试；Arch 请先运行 pacman -Syu。缺少的工具：${AGENT_MISSING_DEPENDENCIES}。可在 root 下手动执行：$(agent_manual_install_command)"
        fi
        agent_collect_dependencies "$@"
        [ -z "$AGENT_MISSING_DEPENDENCIES" ] || agent_dependency_fail "包管理器执行后仍缺少 Agent 工具：${AGENT_MISSING_DEPENDENCIES}。请检查 PATH，手动安装 ${AGENT_PACKAGES} 后重试；尚未下载或安装 Agent。"
    fi
}
