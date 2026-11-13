#!/bin/sh
# ConfigFlow Linux gateway installation. {header_note}
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
        echo '安装未完成。已安装的核心和原有配置均保留；修复上方错误后重新运行同一命令可继续。{failure_note}' >&2
    fi
    [ -z "$GATEWAY_STAGE" ] || rm -rf "$GATEWAY_STAGE"
    [ -z "$GATEWAY_LOCK" ] || rmdir "$GATEWAY_LOCK"
    return "$gateway_exit"
}
trap cleanup_gateway EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
{agent_install_dependencies}
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
        MIHOMO_URL={mihomo_amd64_url_shell}
        MIHOMO_SHA={mihomo_amd64_sha_shell}
        OXIDNS_URL={oxidns_amd64_url_shell}
        OXIDNS_SHA={oxidns_amd64_sha_shell}
        AGENT_SHA={agent_amd64_sha_shell}
        ;;
    aarch64|arm64)
        GATEWAY_ARCH=linux-arm64
        MIHOMO_URL={mihomo_arm64_url_shell}
        MIHOMO_SHA={mihomo_arm64_sha_shell}
        OXIDNS_URL={oxidns_arm64_url_shell}
        OXIDNS_SHA={oxidns_arm64_sha_shell}
        AGENT_SHA={agent_arm64_sha_shell}
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
# Install missing tools with the system package manager, as ordinary native
# installs do (Alpine lacks nft and ss). The gateway Agent reads `ip -j`, which
# BusyBox ip lacks; OxiDNS cron plugins need the timezone database.
agent_ensure_dependencies ha_ip nft ss tar gzip sha256sum cmp zoneinfo
[ -c /dev/net/tun ] || fail '缺少 /dev/net/tun。请先在宿主机/LXC 配置中开放 TUN，再重跑；安装器不会修改宿主机权限。'
nft -j list ruleset >/dev/null || fail '无法读取 nftables，缺少 CAP_NET_ADMIN 或内核 nftables 支持；请检查宿主机/LXC 权限。'
[ -r /proc/sys/net/ipv4/ip_forward ] || fail '无法读取 IPv4 转发设置，请检查内核网络命名空间。'
for directory in /etc /usr/local /usr/local/bin /opt /opt/configflow-agent /run; do
    [ ! -L "$directory" ] || fail "安装目录不能是符号链接：$directory"
done
for legacy in /opt/configflow-agent/config*.json /etc/systemd/system/configflow-agent.service /etc/init.d/configflow-agent /usr/local/bin/configflow-agent; do
    [ ! -e "$legacy" ] && [ ! -L "$legacy" ] || fail '检测到旧版共享 Agent，请先通过原卸载流程移除 Agent 并保留核心配置。'
done{journal_precheck}
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
BINARY_BASE={binary_download_url_shell}
cf_step "校验核心并下载所需程序"
if [ "$MIHOMO_DOWNLOAD" -eq 1 ]; then
    verified_download "$MIHOMO_URL" "$MIHOMO_SHA" "$GATEWAY_STAGE/mihomo.gz"
fi
if [ "$OXIDNS_DOWNLOAD" -eq 1 ]; then
    verified_download "$OXIDNS_URL" "$OXIDNS_SHA" "$GATEWAY_STAGE/oxidns.tar.gz"
fi
verified_download "$BINARY_BASE/configflow-agent-$GATEWAY_ARCH" "$AGENT_SHA" "$GATEWAY_STAGE/configflow-agent"
MIHOMO_VERSION={mihomo_version_shell}
OXIDNS_VERSION={oxidns_version_shell}
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
printf '%s\n' "$agent_version" | awk -F '[-.]' '/^[0-9]+\.[0-9]+\.[0-9]+(-go)?$/ && ($1>1 || ($1==1 && ($2>8 || ($2==8 && $3>={agent_min_patch})))) {ok=1} END {exit !ok}' || fail '网关向导需要 Agent {agent_min_version} 或更新，请更新服务器的 Agent 构建并重新生成命令。'
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
retry="TERM/30/KILL/5"
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
        if ! cmp -s "$GATEWAY_STAGE/$core.service" "$CORE_UNIT"; then
            # Earlier installs wrote the same OpenRC script without retry, so
            # stop waited only 5 s; upgrade exactly that script in place.
            grep -v '^retry="TERM/30/KILL/5"$' "$GATEWAY_STAGE/$core.service" > "$GATEWAY_STAGE/$core.service.v1"
            if [ "$GATEWAY_INIT" = openrc ] && cmp -s "$GATEWAY_STAGE/$core.service.v1" "$CORE_UNIT"; then
                cp "$GATEWAY_STAGE/$core.service" "$CORE_UNIT.new"
                chmod 755 "$CORE_UNIT.new"
                mv "$CORE_UNIT.new" "$CORE_UNIT"
            else
                fail "$CORE_UNIT 已被修改；请人工核对服务后重跑，不会覆盖。"
            fi
        fi
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
{aio_script}
CONFIGFLOW_GATEWAY_AIO_SCRIPT
cf_step "安装两个 Agent"
{forwarding_comment}
{forwarding_owner_env}sh "$GATEWAY_STAGE/install-agents.sh"

ENROLLMENT_URL={enrollment_url_shell}
ENROLLMENT_TOKEN={enrollment_token_shell}
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
cf_fact next_steps '{next_steps}'
cf_note gateway '{gateway_note}'
cf_complete '核心及两个 Agent 已就绪'
