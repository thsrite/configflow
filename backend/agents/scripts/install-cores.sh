#!/bin/sh
# ConfigFlow core installer interface: 1
# Standalone core installer. Never installs or registers a ConfigFlow Agent.
set -eu
umask 022

MIHOMO_VERSION=v1.19.32
OXIDNS_VERSION=v1.6.1
LOCK=/run/configflow-core-install.lock
STAGE=""
LOCKED=false
COMMITTED=false
CREATED_FILES=""
CREATED_DIRS=""
ENABLE_ATTEMPTED=""
START_ATTEMPTED=""
INIT_SYSTEM=""
TUN_ENABLED=false
TUN_REASON=""
DEPENDENCIES_ATTEMPTED=false
CORE_SELECTION=both
NEW_CORES=""
NEW_MIHOMO=false
NEW_OXIDNS=false
DNS_PORTS=""

fail() { printf '%s\n' "$*" >&2; exit 1; }
exists() { [ -e "$1" ] || [ -L "$1" ]; }
cleanup() {
    cleanup_status=$?
    trap - EXIT HUP INT TERM
    set +e
    if [ "$COMMITTED" != true ]; then
        rollback_safe=true
        for service in $START_ATTEMPTED; do
            if [ "$INIT_SYSTEM" = systemd ]; then systemctl stop "$service" || rollback_safe=false
            else rc-service "$service" stop || rollback_safe=false; fi
        done
        for service in $ENABLE_ATTEMPTED; do
            if [ "$INIT_SYSTEM" = systemd ]; then systemctl disable "$service" || rollback_safe=false
            else rc-update del "$service" default || rollback_safe=false; fi
        done
        if [ "$rollback_safe" = true ]; then
            for path in $CREATED_FILES; do rm -f "$path"; done
            # These directories were absent before this invocation. Cores may
            # create their own cache/runtime files after starting.
            for path in $CREATED_DIRS; do rm -rf "$path"; done
            if [ -n "$CREATED_FILES" ] && [ "$INIT_SYSTEM" = systemd ]; then systemctl daemon-reload; fi
        else
            printf '%s\n' "回滚未完成：核心停止或取消开机启动失败。为避免删除仍运行服务的文件，已保留本次核心、配置及服务文件。请先手动停止本次新装服务（${NEW_CORES}）并取消其开机启动，再检查这些文件；已有的其他核心不在本次回滚范围。本脚本不会自动覆盖残留安装。" >&2
        fi
        if [ "$cleanup_status" -ne 0 ]; then
            if [ -n "$CREATED_FILES$CREATED_DIRS$START_ATTEMPTED$ENABLE_ATTEMPTED" ]; then
                printf '%s\n' '安装失败；已尝试撤销本次创建和启动的核心，请同时检查上方回滚结果。' >&2
            else
                printf '%s\n' '安装已停止；尚未修改核心、配置或服务。' >&2
            fi
            if [ "$DEPENDENCIES_ATTEMPTED" = true ]; then printf '%s\n' '包管理器安装的系统依赖会保留，不会随核心回滚而卸载。' >&2; fi
        fi
    fi
    [ -z "$STAGE" ] || rm -rf "$STAGE"
    if [ "$LOCKED" = true ]; then rm -f "$LOCK/pid"; rmdir "$LOCK"; fi
    exit "$cleanup_status"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

while [ "$#" -gt 0 ]; do
    case "$1" in
        --verbose|-v) shift ;;
        --core)
            [ "$#" -ge 2 ] || fail '缺少 --core 参数：请选择 both、mihomo 或 oxidns。'
            CORE_SELECTION=$2; shift 2 ;;
        --core=*) CORE_SELECTION=${1#--core=}; shift ;;
        *) fail "不支持的参数：$1。用法：sh install-cores.sh [--core both|mihomo|oxidns]。" ;;
    esac
done
case "$CORE_SELECTION" in both) REQUESTED_CORES='mihomo oxidns' ;; mihomo|oxidns) REQUESTED_CORES=$CORE_SELECTION ;; *) fail '无效的 --core 参数：请选择 both、mihomo 或 oxidns。' ;; esac

cf_step '检查核心安装环境与依赖'
[ "$(id -u)" -eq 0 ] || fail '请使用 root 运行，例如 sudo sh install-cores.sh。'
[ "$(uname -s)" = Linux ] || fail '仅支持 Linux（systemd 或 OpenRC）。'
case "$(uname -m)" in
    x86_64|amd64)
        MIHOMO_ASSET=mihomo-linux-amd64-v1-v1.19.32.gz
        MIHOMO_SHA=306f81e723e60ce6b828899a6fe83e1d00e9ecefb2dc8d4d849312a5bc00efdc
        OXIDNS_ASSET=oxidns-x86_64-unknown-linux-musl.tar.gz
        OXIDNS_SHA=2ac8e103e03407a73c29da848f6ce2d524e751ce1da8a744aa9168fc1384e3b0 ;;
    aarch64|arm64)
        MIHOMO_ASSET=mihomo-linux-arm64-v1.19.32.gz
        MIHOMO_SHA=9dd862e28b46ff7d775f169cceebc28deccaa0a9e804237d421cd2571e0caba0
        OXIDNS_ASSET=oxidns-aarch64-unknown-linux-musl.tar.gz
        OXIDNS_SHA=d879d4b9199c5c911d296af4df8d9e974523fb98a6ee2418ae7274d152365e42 ;;
    armv7l|armv7)
        MIHOMO_ASSET=mihomo-linux-armv7-v1.19.32.gz
        MIHOMO_SHA=f622cbe903ba1a11d30e9071d1c5373929c675b854856ff3e7f977255a25bb66
        OXIDNS_ASSET=oxidns-armv7-unknown-linux-musleabihf.tar.gz
        OXIDNS_SHA=369739e626e2664131d91b7f2b9b4cf396475672ed3086d07358675f390e9786 ;;
    *) fail "不支持的架构 $(uname -m)，仅支持 amd64、arm64、armv7。" ;;
esac
if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
    INIT_SYSTEM=systemd
    UNIT_DIR=/etc/systemd/system
elif [ -x /sbin/openrc-run ] && command -v rc-service >/dev/null 2>&1 && command -v rc-update >/dev/null 2>&1; then
    INIT_SYSTEM=openrc
    UNIT_DIR=/etc/init.d
else fail '需要正在使用的 systemd 或 OpenRC；容器请先配置 init 服务管理器。'; fi
for directory in /run /etc /usr/local /usr/local/bin "$UNIT_DIR"; do
    [ -d "$directory" ] && [ ! -L "$directory" ] || fail "安装目录缺失或为符号链接：${directory}。请先准备正常目录后重试。"
done
unit_path() {
    if [ "$INIT_SYSTEM" = systemd ]; then printf '%s/%s.service' "$UNIT_DIR" "$1"; else printf '%s/%s' "$UNIT_DIR" "$1"; fi
}
receipt="configflow-standalone-cores-v1:$INIT_SYSTEM"
for service in $REQUESTED_CORES; do
    existing_core=""
    for path in "/usr/local/bin/$service" "/etc/$service" "/etc/systemd/system/$service.service" "/etc/systemd/system/$service.service.d" "/run/systemd/system/$service.service" "/usr/lib/systemd/system/$service.service" "/lib/systemd/system/$service.service" "/etc/init.d/$service" "/run/$service.pid"; do
        if exists "$path"; then existing_core=$path; break; fi
    done
    if [ -z "${existing_core}" ] && command -v "$service" >/dev/null 2>&1; then existing_core="PATH 中的 $service"; fi
    if [ -z "${existing_core}" ] && [ "$INIT_SYSTEM" = systemd ]; then
        load_state=$(systemctl show "$service.service" --property=LoadState --value) || fail "无法检查已有 $service 服务，请检查 systemd 是否正常运行。"
        if [ "$load_state" != not-found ]; then existing_core="systemd $service 服务（${load_state}）"; fi
    fi
    if [ -n "${existing_core}" ]; then
        printf '%s\n' "跳过已有 ${service}：${existing_core}。保留程序、配置和服务，不升级或重启；尚未验证其完整性与运行状态，残留文件也会跳过。需要管理时请使用 Agent 接入。"
        if [ "$service" = mihomo ]; then core_label=Mihomo; else core_label=OxiDNS; fi
        cf_fact "service_$service" "${core_label}：已有部署，已保留（运行状态未验证）"
        cf_note "existing_$service" "${core_label} 的已有程序、配置与服务均跳过；DNS 和运行状态未单独验证。"
    else
        NEW_CORES="${NEW_CORES:+${NEW_CORES} }$service"
        if [ "$service" = mihomo ]; then NEW_MIHOMO=true; DNS_PORTS="${DNS_PORTS:+$DNS_PORTS }1053"
        else NEW_OXIDNS=true; DNS_PORTS="${DNS_PORTS:+$DNS_PORTS }53"; fi
    fi
done
if [ -z "${NEW_CORES}" ]; then
    printf '%s\n' '所选核心均已有程序、配置或服务，本次全部跳过；未下载、升级或重启核心。'
    cf_step_done '所选核心已有部署，全部跳过'
    cf_fact dns 'DNS：未验证（本次没有新装核心）'
    cf_complete '核心检查完成'
    COMMITTED=true
    exit 0
fi

# Resolve only missing tools, after rejecting unsupported systems. Package
# transactions remain managed by the distribution and are never rolled back
# together with the two cores.
add_package() {
    case " ${PACKAGES} " in *" $1 "*) ;; *) PACKAGES="${PACKAGES:+${PACKAGES} }$1" ;; esac
}
detect_package_manager() {
    if command -v apk >/dev/null 2>&1; then
        package_manager=apk
    elif command -v apt-get >/dev/null 2>&1; then
        package_manager=apt-get
    elif command -v dnf >/dev/null 2>&1; then
        package_manager=dnf
    elif command -v yum >/dev/null 2>&1; then
        package_manager=yum
    elif command -v pacman >/dev/null 2>&1; then
        package_manager=pacman
    elif command -v zypper >/dev/null 2>&1; then
        package_manager=zypper
    else
        package_manager=""
    fi
}
collect_missing_dependencies() {
    MISSING_DEPENDENCIES=""
    PACKAGES=""
    for dependency in awk cat chmod cp gzip mkdir mktemp mv rm rmdir sed sha256sum sleep tail tar; do
        if ! command -v "$dependency" >/dev/null 2>&1; then
            MISSING_DEPENDENCIES="${MISSING_DEPENDENCIES:+${MISSING_DEPENDENCIES} }$dependency"
            case "$dependency" in
                awk) add_package gawk ;;
                gzip|sed|tar) add_package "$dependency" ;;
                *) add_package coreutils ;;
            esac
        fi
    done
    if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
        MISSING_DEPENDENCIES="${MISSING_DEPENDENCIES:+${MISSING_DEPENDENCIES} }curl/wget"
        add_package curl
        add_package ca-certificates
    fi
    # OxiDNS cron plugins resolve IANA timezones from the system database;
    # minimal Alpine images ship without it.
    if [ "$NEW_OXIDNS" = true ] && [ ! -f "${TZDIR:-/usr/share/zoneinfo}/Asia/Shanghai" ]; then
        MISSING_DEPENDENCIES="${MISSING_DEPENDENCIES:+${MISSING_DEPENDENCIES} }时区数据"
        if [ "$package_manager" = zypper ]; then add_package timezone; else add_package tzdata; fi
    fi
}
install_dependencies() {
    if [ -z "$package_manager" ]; then
        fail "缺少依赖：${MISSING_DEPENDENCIES}。未找到支持的包管理器（apk、apt-get、dnf、yum、pacman、zypper）；请手动安装 ${PACKAGES} 后重新运行。"
    fi
    printf '%s\n' "缺少依赖：${MISSING_DEPENDENCIES}；使用 ${package_manager} 安装 ${PACKAGES}。"
    DEPENDENCIES_ATTEMPTED=true
    case "${package_manager}" in
        apk) apk add --no-cache "$@" ;;
        apt-get) apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "$@" ;;
        dnf|yum) "${package_manager}" install -y "$@" ;;
        # Never refresh only package indexes (-Sy): Arch requires a complete
        # system upgrade when synchronizing an outdated installation.
        pacman) pacman -S --needed --noconfirm "$@" ;;
        zypper) zypper --non-interactive install --no-recommends "$@" ;;
    esac
}
detect_package_manager
collect_missing_dependencies
if [ -n "${MISSING_DEPENDENCIES}" ]; then
    # PACKAGES contains only the fixed package names listed above.
    if ! install_dependencies ${PACKAGES}; then
        fail "系统依赖安装失败，尚未下载或安装核心。请检查网络、软件源及包管理器锁，完成系统更新后重新运行；Arch 请先运行 pacman -Syu。缺少的依赖：${MISSING_DEPENDENCIES}。"
    fi
    collect_missing_dependencies
    [ -z "${MISSING_DEPENDENCIES}" ] || fail "包管理器执行后仍缺少依赖：${MISSING_DEPENDENCIES}。请检查 PATH，手动安装 ${PACKAGES} 后重试；尚未下载或安装核心。"
fi
if command -v curl >/dev/null 2>&1; then DOWNLOAD=curl; else DOWNLOAD=wget; fi

if ! mkdir "$LOCK" 2>/dev/null; then
    fail "另一个核心安装可能正在运行：${LOCK}。请等待完成；若为中断残留，确认其中 pid 对应进程已退出后删除此锁目录再重试。"
fi
LOCKED=true
printf '%s\n' "$$" > "$LOCK/pid"

check_ports() {
    proc_files=""
    for network in /proc/net/tcp /proc/net/udp /proc/net/tcp6 /proc/net/udp6; do
        if [ -r "$network" ]; then proc_files="$proc_files $network"; fi
    done
    [ -r /proc/net/tcp ] && [ -r /proc/net/udp ] || fail '无法读取 /proc/net/tcp 或 udp，不能检查 DNS 端口，请挂载容器 /proc 后重试。'
    # Any IPv4/IPv6 listener on these ports can conflict with a wildcard bind.
    if ! awk -v mihomo="$NEW_MIHOMO" -v oxidns="$NEW_OXIDNS" 'FNR > 1 {split($2, addr, ":"); port=toupper(addr[2]); if (((port == "0035" && oxidns == "true") || (port == "041D" && mihomo == "true")) && (FILENAME ~ /udp/ || $4 == "0A")) {print "已占用 DNS 端口：" $2 " (" FILENAME ")"; conflict=1}} END {exit conflict ? 1 : 0}' $proc_files; then
        fail "DNS 端口被占用（本次安装需要：${DNS_PORTS}）。请用 ss -lntup 检查并调整已有 DNS 服务的监听地址/端口，再重试；本脚本不会关闭系统 DNS 服务。"
    fi
}
check_ports

show_tun_disabled_guidance() {
    printf '%s\n' "TUN 初始配置保持关闭：$TUN_REASON" \
        '普通 Linux/虚拟机：先检查内核 TUN 驱动（modprobe tun），确认 /dev/net/tun 为 10:200 字符设备且可读写。' \
        'PVE LXC：在 PVE 宿主机执行 modprobe tun，检查 pct config <CTID> 的空闲 devN 槽，再用 pct set <CTID> --devN /dev/net/tun 添加设备（例如空闲的 dev0）；完整停止并启动容器后，在容器内复查。不要改为特权容器或放宽整个设备目录权限。'
    if [ "$INIT_SYSTEM" = openrc ]; then
        printf '%s\n' '权限（OpenRC）：核对 /etc/init.d/mihomo 与 /etc/conf.d/mihomo 的 command_user、capabilities 和启动命令，以实际核心进程 /proc/<PID>/status 的 CapEff 确认 CAP_NET_ADMIN，并检查该进程对 /dev/net/tun 的访问权限；root 身份不能证明具备该能力。若运行在容器内，还需核对容器保留的能力和设备映射。'
    else
        printf '%s\n' '权限（systemd）：保留容器的 CAP_NET_ADMIN；核对 Mihomo 服务的 CapabilityBoundingSet、DeviceAllow=/dev/net/tun rw 及设备隔离策略，并以实际核心进程的 CapEff 确认能力；非 root 服务还需核对 AmbientCapabilities 或程序文件能力。'
    fi
    printf '%s\n' '若接口重名，确认已有接口的归属后更改 Mihomo 的 tun.device。' \
        '修复后：在面板的 Mihomo 基础配置中设置 tun.enable: true，核对 DNS 监听、dns-hijack 及同机 DNS 上下游关系后保存并发布；未接入 Agent 时修改 /etc/mihomo/config.yaml 并重启 mihomo 服务。发布保留自定义端口；重复运行本安装脚本会保留已有配置，不会自动开启 TUN。'
    cf_fact tun 'TUN：已关闭'
    cf_note tun "TUN 未启用：${TUN_REASON}修复步骤见详细日志。"
}

# Probe without creating an interface, changing routes, or writing sysctls.
# The new service runs as root; these checks describe the installer context.
# Service-manager policy may differ, so actual interface creation is checked
# again after startup instead of treating an active DNS process as TUN success.
check_tun_environment() {
    if ! exists /dev/net/tun; then
        TUN_REASON='/dev/net/tun 未提供。'
    elif [ ! -c /dev/net/tun ]; then
        TUN_REASON='/dev/net/tun 不是字符设备。'
    elif ! command -v stat >/dev/null 2>&1; then
        TUN_REASON='缺少 stat，无法确认 TUN 设备号；请安装 coreutils 后再检查。'
    elif [ "$(stat -L -c '%t:%T' /dev/net/tun 2>/dev/null || true)" != a:c8 ]; then
        TUN_REASON='无法确认 /dev/net/tun 是 Linux TUN 设备（主/次设备号 10:200）。'
    elif ! (exec 3<>/dev/net/tun) 2>/dev/null; then
        TUN_REASON='TUN 设备存在，但无法读写打开；设备映射、cgroup 或安全策略可能限制访问。'
    elif [ -e /sys/class/net/configflow-tun ]; then
        TUN_REASON='网络接口 configflow-tun 已存在；为避免接管其他接口，保留 TUN 关闭。'
    else
        # CAP_NET_ADMIN is bit 12. Inspect its hex digit without relying on
        # awk extensions or shell integer width (also supports 32-bit armv7).
        tun_capability=$(awk '
            $1 == "CapEff:" || $1 == "CapBnd:" {
                if (NF != 2 || $2 !~ /^[0-9a-fA-F]+$/ || length($2) > 16) {invalid=1; next}
                mask=tolower($2); digit=length(mask) >= 4 ? substr(mask, length(mask)-3, 1) : "0"
                seen[$1]++; allowed[$1]=(digit ~ /^[13579bdf]$/)
            }
            END {
                if (invalid || seen["CapEff:"] != 1 || seen["CapBnd:"] != 1) print "unknown"
                else if (!allowed["CapEff:"]) print "effective"
                else if (!allowed["CapBnd:"]) print "bounding"
                else print "available"
            }' /proc/self/status 2>/dev/null) || tun_capability=unknown
        case "$tun_capability" in
            available) TUN_ENABLED=true ;;
            effective) TUN_REASON='当前进程缺少有效 CAP_NET_ADMIN，无法确认可创建 TUN 和调整路由。' ;;
            bounding) TUN_REASON='当前进程的能力上限 CapBnd 不包含 CAP_NET_ADMIN。' ;;
            *) TUN_REASON='无法读取或解析当前进程的 CAP_NET_ADMIN，不能确认 TUN 权限。' ;;
        esac
    fi
    if [ "$TUN_ENABLED" = true ]; then
        printf '%s\n' 'TUN 环境检测通过，初始配置将启用 TUN（mixed、auto-route）。启动时会创建 configflow-tun 并自动调整本机路由；当前没有代理节点，流量按 DIRECT 直连。'
    else
        show_tun_disabled_guidance
    fi
    forwarding=$(cat /proc/sys/net/ipv4/ip_forward 2>/dev/null || true)
    if [ "$forwarding" = 1 ]; then
        printf '%s\n' 'IPv4 转发：当前为 1；重启后的持久化状态尚未验证。'
        cf_note forwarding 'IPv4 转发：当前已开启；重启后是否保持尚未确认。'
    else
        if [ "$forwarding" = 0 ]; then forwarding_status='当前为 0'; else forwarding_status='状态无法读取'; fi
        printf '%s\n' "IPv4 转发：${forwarding_status}。这不影响本机 TUN 的选择；仅当此 Linux/LXC 承担旁路网关或路由转发时需要开启。" \
            '旁路网关修复：在该 Linux/LXC 内执行 sysctl -w net.ipv4.ip_forward=1，并把 net.ipv4.ip_forward=1 写入 /etc/sysctl.d/99-configflow-forward.conf；检查其他 sysctl 文件是否将其改回 0，重启后用 sysctl net.ipv4.ip_forward 验证。PVE 仅做二层网桥时不因此需要开启宿主机转发。'
        cf_note forwarding "IPv4 转发：${forwarding_status}，持久化未验证；作为旁路网关时需开启，修复步骤见详细日志。"
    fi
}
if [ "$NEW_MIHOMO" = true ]; then check_tun_environment; fi
cf_step_done "完成（${INIT_SYSTEM}）"
case "$NEW_CORES" in
    'mihomo oxidns') cf_step '下载安装 Mihomo 与 OxiDNS 核心' ;;
    mihomo) cf_step '下载安装 Mihomo 核心' ;;
    oxidns) cf_step '下载安装 OxiDNS 核心' ;;
esac
STAGE=$(mktemp -d /tmp/configflow-cores.XXXXXX)
chmod 700 "$STAGE"
for service in ${NEW_CORES}; do mkdir "$STAGE/$service"; done
download() {
    if [ "$DOWNLOAD" = curl ]; then
        curl --fail --location --connect-timeout 15 --max-time 300 --retry 2 --output "$2" "$1" || fail "下载失败：$1。请检查到 GitHub 的网络和系统时间；若证书验证失败，请安装或修复 ca-certificates / 自定义 CA 信任后重试，不要关闭证书校验。"
    else
        wget -T 60 -O "$2" "$1" || fail "下载失败：$1。请检查到 GitHub 的网络和系统时间；若证书验证失败，请安装或修复 ca-certificates / 自定义 CA 信任后重试，不要关闭证书校验。"
    fi
}
printf '%s\n' "本次下载并安装：${NEW_CORES}。使用官方 Mihomo $MIHOMO_VERSION / OxiDNS $OXIDNS_VERSION full 发布文件，并校验固定 SHA256。"
: > "$STAGE/download.sha256"
if [ "$NEW_MIHOMO" = true ]; then
    download "https://github.com/MetaCubeX/mihomo/releases/download/$MIHOMO_VERSION/$MIHOMO_ASSET" "$STAGE/mihomo.gz"
    printf '%s  %s\n' "$MIHOMO_SHA" "$STAGE/mihomo.gz" >> "$STAGE/download.sha256"
fi
if [ "$NEW_OXIDNS" = true ]; then
    download "https://github.com/svenshi/oxidns/releases/download/$OXIDNS_VERSION/$OXIDNS_ASSET" "$STAGE/oxidns.tar.gz"
    printf '%s  %s\n' "$OXIDNS_SHA" "$STAGE/oxidns.tar.gz" >> "$STAGE/download.sha256"
fi
sha256sum -c "$STAGE/download.sha256" || fail '下载文件的 SHA256 不匹配，已停止安装；请重新下载，不要跳过校验。'
if [ "$NEW_MIHOMO" = true ]; then
    gzip -dc "$STAGE/mihomo.gz" > "$STAGE/mihomo-bin" || fail 'Mihomo 压缩包解压失败。'
    chmod 755 "$STAGE/mihomo-bin"
fi
if [ "$NEW_OXIDNS" = true ]; then
    # Only extract the verified core executable, never upstream sample configs/UI.
    tar -xzf "$STAGE/oxidns.tar.gz" -C "$STAGE/oxidns" oxidns || fail 'OxiDNS 压缩包解压失败。'
    [ -f "$STAGE/oxidns/oxidns" ] && [ ! -L "$STAGE/oxidns/oxidns" ] || fail 'OxiDNS 包内核心文件无效。'
    chmod 755 "$STAGE/oxidns/oxidns"
fi
write_mihomo_bootstrap() {
cat > "$STAGE/mihomo/config.yaml" <<MIHOMO_CONFIG
# Direct bootstrap; TUN follows the installation environment probe.
port: 0
socks-port: 0
mixed-port: 0
redir-port: 0
tproxy-port: 0
mode: rule
log-level: info
ipv6: false
tun:
  enable: $TUN_ENABLED
  device: configflow-tun
  stack: mixed
  auto-route: true
  auto-detect-interface: true
  auto-redirect: false
  dns-hijack:
    - any:1053
    - tcp://any:1053
dns:
  enable: true
  listen: 0.0.0.0:1053
  ipv6: false
  enhanced-mode: normal
  nameserver:
    - 223.5.5.5
    - 119.29.29.29
proxies: []
proxy-groups: []
rules:
  - MATCH,DIRECT
MIHOMO_CONFIG
}
if [ "$NEW_MIHOMO" = true ]; then
    write_mihomo_bootstrap
    "$STAGE/mihomo-bin" -t -d "$STAGE/mihomo" -f "$STAGE/mihomo/config.yaml" || fail 'Mihomo 初始配置校验失败，未安装核心。'
fi
if [ "$NEW_OXIDNS" = true ]; then
cat > "$STAGE/oxidns/config.yaml" <<'OXIDNS_CONFIG'
log:
  level: info
plugins:
  - tag: forward
    type: forward
    args:
      upstreams:
        - addr: udp://127.0.0.1:1053
  - tag: main_sequence
    type: sequence
    args:
      - exec: $forward
      - exec: accept
  - tag: udp_server
    type: udp_server
    args:
      listen: 0.0.0.0:53
      entry: main_sequence
  - tag: tcp_server
    type: tcp_server
    args:
      listen: 0.0.0.0:53
      entry: main_sequence
OXIDNS_CONFIG
    if [ "$CORE_SELECTION" = oxidns ]; then
        awk '{if ($0 == "        - addr: udp://127.0.0.1:1053") {print "        - addr: udp://223.5.5.5"; print "        - addr: udp://119.29.29.29"} else print}' "$STAGE/oxidns/config.yaml" > "$STAGE/oxidns/standalone.yaml"
        mv "$STAGE/oxidns/standalone.yaml" "$STAGE/oxidns/config.yaml"
        printf '%s\n' '仅安装 OxiDNS：初始上游为公共 DNS 223.5.5.5 / 119.29.29.29。若要搭配已有 Mihomo 形成 AIO，请选择 --core both；该模式保留已有 Mihomo 并将新 OxiDNS 上游设为 127.0.0.1:1053。'
    else
        printf '%s\n' 'AIO DNS 分工：新 OxiDNS 监听 53，上游为 127.0.0.1:1053；已有 Mihomo 不会被修改，请确认它已在 1053 提供 DNS 服务。'
    fi
    "$STAGE/oxidns/oxidns" check -c "$STAGE/oxidns/config.yaml" -d "$STAGE/oxidns" || fail 'OxiDNS 初始配置校验失败，未安装核心。'
fi
# Recheck after downloads/validation, before touching persistent core resources.
check_ports

write_new_file() {
    new_source=$1
    new_target=$2
    (set -C; : > "$new_target") || fail "目标已存在或无法创建：${new_target}；不覆盖现有文件。"
    CREATED_FILES="$new_target $CREATED_FILES"
    cp "$new_source" "$new_target" || fail "写入失败：$new_target"
}
for service in ${NEW_CORES}; do
    mkdir "/etc/$service" || fail "无法新建 /etc/${service}；请检查权限或并行操作。"
    CREATED_DIRS="/etc/$service $CREATED_DIRS"
    write_new_file "$STAGE/$service/config.yaml" "/etc/$service/config.yaml"
    chmod 600 "/etc/$service/config.yaml"
    if [ "$service" = mihomo ]; then binary="$STAGE/mihomo-bin"; else binary="$STAGE/oxidns/oxidns"; fi
    write_new_file "$binary" "/usr/local/bin/$service"
    chmod 755 "/usr/local/bin/$service"
    if [ "$service" = mihomo ]; then core_args='-d /etc/mihomo -f /etc/mihomo/config.yaml'; else core_args='start -c /etc/oxidns/config.yaml -d /etc/oxidns'; fi
    if [ "$INIT_SYSTEM" = systemd ]; then
        cat > "$STAGE/$service.unit" <<UNIT
[Unit]
Description=$service DNS core
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
WorkingDirectory=/etc/$service
ExecStart=/usr/local/bin/$service $core_args
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
UNIT
    else
        cat > "$STAGE/$service.unit" <<UNIT
#!/sbin/openrc-run
name="$service"
description="$service DNS core"
command="/usr/local/bin/$service"
command_args="$core_args"
command_background=true
pidfile="/run/$service.pid"
retry="TERM/30/KILL/5"
directory="/etc/$service"
output_log="/etc/$service/$service.log"
error_log="/etc/$service/$service.log"
depend() {
    need net
}
UNIT
    fi
    write_new_file "$STAGE/$service.unit" "$(unit_path "$service")"
    if [ "$INIT_SYSTEM" = openrc ]; then chmod 755 "$(unit_path "$service")"; fi
done
show_service_logs() {
    log_service=$1
    if [ "$INIT_SYSTEM" = systemd ]; then
        if command -v journalctl >/dev/null 2>&1; then journalctl -u "$log_service" -n 40 --no-pager >&2 || true; fi
    elif [ -f "/etc/$log_service/$log_service.log" ]; then
        tail -n 40 "/etc/$log_service/$log_service.log" >&2 || true
    fi
}
if [ "$INIT_SYSTEM" = systemd ]; then systemctl daemon-reload || fail 'systemd 加载新服务失败。'; fi
for service in ${NEW_CORES}; do
    ENABLE_ATTEMPTED="$service $ENABLE_ATTEMPTED"
    if [ "$INIT_SYSTEM" = systemd ]; then systemctl enable "$service" || fail "$service 设置开机启动失败。"
    else rc-update add "$service" default || fail "$service 设置开机启动失败。"; fi
    START_ATTEMPTED="$service $START_ATTEMPTED"
    if [ "$INIT_SYSTEM" = systemd ]; then systemctl start "$service" || { show_service_logs "$service"; fail "$service 启动失败，请查看上方错误。"; }
    else rc-service "$service" start || { show_service_logs "$service"; fail "$service 启动失败，请查看上方错误。"; }; fi
done
sleep 2
for service in ${NEW_CORES}; do
    if [ "$INIT_SYSTEM" = systemd ]; then systemctl is-active --quiet "$service" || { show_service_logs "$service"; fail "$service 启动后未保持运行，请查看服务日志。"; }
    else rc-service "$service" status || { show_service_logs "$service"; fail "$service 启动后未保持运行，请查看服务日志。"; }; fi
done
tun_interface_ready() {
    for tun_flag_file in /sys/class/net/configflow-tun/tun_flags /sys/class/net/configflow-tun/flags; do
        tun_flags=$(cat "$tun_flag_file" 2>/dev/null || true)
        # Bit 0 is IFF_TUN in tun_flags and IFF_UP in interface flags.
        printf '%s\n' "$tun_flags" | awk '/^0x[0-9a-fA-F]+$/ {digit=substr(tolower($0), length($0), 1); if (digit ~ /^[13579bdf]$/) ok=1} END {exit ok ? 0 : 1}' || return 1
    done
    if [ "$INIT_SYSTEM" = systemd ]; then
        tun_pid=$(systemctl show mihomo.service --property=MainPID --value 2>/dev/null) || return 1
    else
        tun_pid=$(cat /run/mihomo.pid 2>/dev/null) || return 1
    fi
    case "$tun_pid" in ''|0|*[!0-9]*) return 1 ;; esac
    # An existing interface alone is insufficient: require the new core's
    # open TUN descriptor to identify exactly our explicitly named device.
    for tun_fdinfo in /proc/"$tun_pid"/fdinfo/*; do
        [ -r "$tun_fdinfo" ] || continue
        if awk '$1 == "iff:" && $2 == "configflow-tun" && NF == 2 {found=1} END {exit found ? 0 : 1}' "$tun_fdinfo"; then return 0; fi
    done
    return 1
}
wait_for_tun_interface() {
    tun_wait=0
    until tun_interface_ready; do
        [ "$tun_wait" -lt 10 ] || return 1
        sleep 1
        tun_wait=$((tun_wait + 1))
    done
    return 0
}
if [ "$TUN_ENABLED" = true ]; then
    # Mihomo can remain active with DNS working after its TUN listener failed.
    # Allow slower machines to finish listener creation before falling back.
    if wait_for_tun_interface; then
        printf '%s\n' '已确认 Mihomo 创建并持有 configflow-tun TUN 接口；此检查不代表全部透明代理路由或代理节点已配置。'
        cf_fact tun_verified yes
        cf_fact tun 'TUN：已启用，configflow-tun 已创建并由 Mihomo 持有'
    else
        show_service_logs mihomo
        printf '%s\n' '未能确认 Mihomo 实际创建并持有 TUN 接口；将关闭本次初始配置的 TUN，保留 DNS 服务安装。'
        if [ "$INIT_SYSTEM" = systemd ]; then systemctl stop mihomo || fail '关闭 TUN 前停止 Mihomo 失败，未改写运行配置；请手动检查服务。'
        else rc-service mihomo stop || fail '关闭 TUN 前停止 Mihomo 失败，未改写运行配置；请手动检查服务。'; fi
        TUN_ENABLED=false
        TUN_REASON='安装进程预检通过，但未能确认核心服务实际创建 TUN；服务管理器权限或容器设备策略可能与安装进程不同。'
        write_mihomo_bootstrap
        cp "$STAGE/mihomo/config.yaml" /etc/mihomo/config.yaml || fail '写入关闭 TUN 的初始配置失败。'
        if [ "$INIT_SYSTEM" = systemd ]; then systemctl start mihomo || { show_service_logs mihomo; fail '关闭 TUN 后 Mihomo 启动失败。'; }
        else rc-service mihomo start || { show_service_logs mihomo; fail '关闭 TUN 后 Mihomo 启动失败。'; }; fi
        sleep 2
        if [ "$INIT_SYSTEM" = systemd ]; then systemctl is-active --quiet mihomo || { show_service_logs mihomo; fail '关闭 TUN 后 Mihomo 未保持运行。'; }
        else rc-service mihomo status || { show_service_logs mihomo; fail '关闭 TUN 后 Mihomo 未保持运行。'; }; fi
        show_tun_disabled_guidance
    fi
fi
cf_step_done '核心服务已启动'
cf_step '验证新装核心的 DNS'
if command -v dig >/dev/null 2>&1; then
    for port in $DNS_PORTS; do
        if [ "$port" = 53 ] && [ "$CORE_SELECTION" = both ]; then
            dns_failure_guidance='请确认已有或本次安装的 Mihomo 在 127.0.0.1:1053 提供 DNS，且其上游可达。已有核心保持原样。'
        else
            dns_failure_guidance='请检查公共 DNS 上游 223.5.5.5 / 119.29.29.29 是否可达。已有核心保持原样。'
        fi
        for transport in +notcp +tcp; do
            result="$STAGE/dns-$port-${transport#+}.txt"
            dig @127.0.0.1 -p "$port" example.com A "$transport" +time=2 +tries=1 > "$result" 2>&1 || { cat "$result" >&2; fail "DNS 查询失败：$port ${transport}，已停止安装。${dns_failure_guidance}"; }
            cat "$result"
            if ! awk '/status: NOERROR,/ {status=1} /ANSWER: [1-9][0-9]*,/ {answer=1} END {exit (status && answer) ? 0 : 1}' "$result"; then
                fail "DNS 查询未获得有效答案：$port ${transport}。${dns_failure_guidance}"
            fi
        done
    done
    printf '%s\n' "本次新装核心（${NEW_CORES}）的 UDP/TCP DNS 查询均已通过。"
    cf_fact dns 'DNS 验证：UDP/TCP 查询通过（仅本次新装核心）'
    cf_step_done 'UDP/TCP 查询均通过'
else
    printf '%s\n' '服务已启动，实际 DNS 解析尚未验证（未安装 dig）。可安装 dnsutils（Debian/Ubuntu）或 bind-tools（Alpine）后运行：'
    for port in $DNS_PORTS; do printf '%s\n' "dig @127.0.0.1 -p $port example.com; dig @127.0.0.1 -p $port example.com +tcp"; done
    cf_step_done '未验证（缺少 dig）'
    cf_fact dns 'DNS：服务已启动，实际解析未验证'
    cf_note dns_validation '缺少 dig，未验证 DNS 解析；安装 dnsutils 或 bind-tools 后按详细日志中的命令检查。'
fi
for service in ${NEW_CORES}; do
    printf '%s\n' "$receipt" > "$STAGE/receipt"
    sha256sum "/usr/local/bin/$service" "$(unit_path "$service")" >> "$STAGE/receipt" || fail '无法保存安装记录。'
    write_new_file "$STAGE/receipt" "/etc/$service/.configflow-core-install"
done
COMMITTED=true
for service in ${NEW_CORES}; do
    printf '%s\n' "${service} 已安装并设置开机启动，配置：/etc/$service/config.yaml。"
    if [ "$service" = mihomo ]; then core_label=Mihomo; else core_label=OxiDNS; fi
    cf_fact "core_${service}_new" yes
    cf_fact "service_$service" "${core_label}：新装，服务运行中，已设开机启动"
done
if [ "$NEW_MIHOMO" = true ] && [ "$NEW_OXIDNS" = true ]; then
    cf_fact core_dns_ports 'DNS：OxiDNS :53 → Mihomo :1053 → 上游 DNS'
elif [ "$NEW_OXIDNS" = true ]; then
    if [ "$CORE_SELECTION" = both ]; then cf_fact core_dns_ports 'OxiDNS DNS：:53 → 127.0.0.1:1053（已有 Mihomo）'
    else cf_fact core_dns_ports 'OxiDNS DNS：:53 → 公共 DNS'; fi
elif [ "$NEW_MIHOMO" = true ]; then
    cf_fact core_dns_ports 'Mihomo DNS：:1053 → 公共 DNS'
fi
if [ "$NEW_OXIDNS" = true ]; then
    if [ "$CORE_SELECTION" = both ]; then printf '%s\n' 'DNS 分工：OxiDNS 53 → Mihomo 1053 → 上游 DNS；已有核心的配置保持原样。'
    else printf '%s\n' '独立 OxiDNS：53 → 公共 DNS 223.5.5.5 / 119.29.29.29。'; fi
elif [ "$CORE_SELECTION" = both ]; then
    printf '%s\n' '已有 OxiDNS 的上游保持原样。要使用 AIO DNS 链路，请将远端上游对应到 Mihomo 实际的 DNS 监听地址和端口（新装默认 127.0.0.1:1053），再通过 Agent 发布或自行重启 OxiDNS。'
    cf_note oxidns_upstream '已有 OxiDNS 的上游保持原样；使用同机 DNS 链路时，请将远端上游对应到 Mihomo 实际的 DNS 监听（新装默认 127.0.0.1:1053）。'
fi
if [ "$NEW_MIHOMO" = true ]; then
    printf '%s\n' '新 Mihomo 在 1053 提供 DNS，当前没有代理节点，流量按 DIRECT 直连。'
    printf '%s\n' "安装结果：tun.enable=${TUN_ENABLED}。首次从面板发布前，请检查 Mihomo 基础配置的 tun.enable 与本次检测结果一致；发布以面板配置为准。"
    cf_note direct '当前没有代理节点，Mihomo 流量使用 DIRECT 直连。'
    cf_note panel_tun '后续发布配置时，TUN 设置以面板配置为准。'
fi
if [ "${CF_OUTPUT_OWNER:-true}" = true ]; then
    printf '%s\n' '本脚本只安装所选的缺失核心；如需面板管理，请继续使用独立的 Agent 接入安装。'
    cf_note agent '如需面板管理，请继续安装对应 Agent。'
fi
cf_complete '核心安装完成'
