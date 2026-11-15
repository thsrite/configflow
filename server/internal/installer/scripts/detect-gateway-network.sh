#!/bin/sh
# ConfigFlow gateway network probe v1. Read-only; no root or JSON tools required.
set -eu
set -f
LC_ALL=C
export LC_ALL

fail() {
    printf '%s\n' "$1" >&2
    exit 1
}

[ "$#" -eq 0 ] || fail '网络检测不接受参数。'
command -v ip >/dev/null 2>&1 || fail '缺少 ip 命令，请先安装 iproute2。'
command -v awk >/dev/null 2>&1 || fail '缺少 awk 命令，无法检测网络。'

probe_routes=$(ip -4 -o route show default 2>/dev/null) || fail '无法读取 IPv4 默认路由。'
probe_route=$(printf '%s\n' "$probe_routes" | awk '
function fail(message) {
    print message > "/dev/stderr"
    failed = 1
    exit 1
}
NF {
    count++
    if ($1 != "default") fail("默认路由格式无效，无法自动确定网络。")
    for (i = 2; i <= NF; i++) {
        if ($i == "nexthop" || $i == "nhid") fail("存在多路径默认路由，无法自动确定出口。")
        if ($i == "linkdown") fail("默认出口网卡未连接，请先检查网络。")
        if ($i == "dev") { interfaces++; interface = $(i + 1) }
        if ($i == "via") { gateways++; gateway = $(i + 1) }
    }
}
END {
    if (failed) exit 1
    if (count == 0) fail("未找到 IPv4 默认路由，请先连接网络。")
    if (count != 1) fail("存在多个 IPv4 默认路由，无法自动确定出口。")
    if (interfaces != 1 || gateways != 1) fail("默认路由缺少唯一网卡或主路由地址。")
    if (interface !~ /^[A-Za-z0-9_][A-Za-z0-9_.-]*$/ || length(interface) > 15 || interface == "lo")
        fail("默认出口网卡名不受支持，不能使用回环网卡。")
    print interface, gateway
}') || exit 1

set -- $probe_route
probe_interface=$1
probe_gateway=$2
probe_addresses=$(ip -4 -o addr show dev "$probe_interface" 2>/dev/null) || fail '无法读取默认出口网卡的 IPv4 地址。'

printf '%s\n' "$probe_addresses" | awk -v interface="$probe_interface" -v gateway="$probe_gateway" '
function fail(message) {
    print message > "/dev/stderr"
    failed = 1
    exit 1
}
function ipv4(value, parts, count, i, number) {
    count = split(value, parts, ".")
    if (count != 4) return -1
    number = 0
    for (i = 1; i <= 4; i++) {
        if (parts[i] !~ /^[0-9]+$/ || length(parts[i]) > 3 || parts[i] + 0 > 255 ||
            (length(parts[i]) > 1 && substr(parts[i], 1, 1) == "0")) return -1
        number = number * 256 + parts[i]
    }
    return number
}
function host(number, first, second) {
    first = int(number / 16777216)
    second = int(number / 65536) % 256
    return first > 0 && first < 224 && first != 127 &&
        !(first == 169 && second == 254)
}
function dotted(number) {
    return sprintf("%d.%d.%d.%d", int(number / 16777216), int(number / 65536) % 256,
                   int(number / 256) % 256, number % 256)
}
NF {
    found = 0
    for (i = 1; i <= NF; i++) {
        if ($i == "tentative" || $i == "dadfailed" || $i == "deprecated")
            fail("默认出口网卡的 IPv4 地址尚未可用，请先检查网络。")
        if ($i == "inet") { count++; found++; cidr = $(i + 1) }
    }
    if (found != 1) fail("网卡 IPv4 地址格式无效。")
}
END {
    if (failed) exit 1
    if (count == 0) fail("默认出口网卡没有可用的 IPv4 地址。")
    if (count != 1) fail("默认出口网卡有多个 IPv4 地址，无法自动确定。")
    if (split(cidr, parts, "/") != 2 || parts[2] !~ /^[0-9]+$/ || parts[2] + 0 < 8 || parts[2] + 0 > 30)
        fail("LAN 网段掩码必须在 /8 到 /30 之间。")
    address = ipv4(parts[1])
    upstream = ipv4(gateway)
    if (address < 0 || upstream < 0 || !host(address) || !host(upstream))
        fail("Linux 和主路由必须使用有效的 IPv4 主机地址。")
    prefix = parts[2] + 0
    size = 2 ^ (32 - prefix)
    network = int(address / size) * size
    broadcast = network + size - 1
    if (address <= network || address >= broadcast || upstream <= network || upstream >= broadcast)
        fail("Linux 和主路由必须位于同一 LAN 网段，且不能使用网络或广播地址。")
    if (address == upstream) fail("Linux IP 与主路由 IP 不能相同。")
    printf "{\"version\":1,\"interface\":\"%s\",\"address\":\"%s\",\"lan_cidr\":\"%s/%d\",\"upstream_gateway\":\"%s\"}\n", \
        interface, dotted(address), dotted(network), prefix, dotted(upstream)
}'
