"""The router split's full TUN leaves every other path exactly as 0ef9e89 had it.

The helpers below only use code that already existed in 0ef9e89, so the
fixture in ``testdata/router_split_regression_0ef9e89.json`` was produced by
running them against that release with the same inputs.
"""
from copy import deepcopy
import hashlib
import json
from pathlib import Path
import re

import pytest
import yaml
from flask import Flask

from backend.agents.go_install_script import generate_go_agent_install_script
from backend.common import config as config_store
from backend.common.config_repository import ProfileRepository
from backend.common.gateway_onboarding import prepare_gateway_onboarding
from backend.converters.mihomo import generate_mihomo_config
from backend.routes import register_blueprints
from backend.test_agent_upgrade_auth import authenticated_app  # noqa: F401
from backend.test_gateway_onboarding import form, snapshot

BASELINE = Path(__file__).parent / 'testdata' / 'router_split_regression_0ef9e89.json'
SESSION = 'c' * 32
SECRET = 'lab-panel-pass'
NODE = {'id': 'node', 'name': 'Test node', 'type': 'ss', 'server': '192.0.2.1',
        'port': 443, 'cipher': 'aes-128-gcm', 'password': 'private'}
IP_RULE = {'id': 'chat', 'name': '聊天', 'behavior': 'classical', 'format': 'text', 'source_type': 'content',
           'content': 'DOMAIN-SUFFIX,chat.test\nIP-CIDR,198.51.100.0/24,no-resolve', 'enabled': True}
DOMAIN_RULES = {'direct': ['builtin:direct'], 'proxy': ['builtin:telegram', 'library:chat', 'builtin:proxy']}

# bbb5fd33 added these two HA-only dependency cases to the shared installer
# helper. Ordinary installers never request either tool, so compare every other
# byte with the original fixture. Freeze the exact reviewed addition rather
# than dropping the whole helper or regenerating its baseline hash.
_NATIVE_HA_DEPENDENCY_CASES = '''        if [ "$agent_tool" = ha_ip ]; then
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
'''
_NATIVE_HA_PACKAGE_CASE = '                keepalived|jq) agent_add_package "$agent_tool" ;;\n'
# The Linux gateway installer reuses the shared helper for its own tools (nft,
# ss, tar, gzip) and a failed package transaction prints a copyable command.
# Ordinary installers never request these tools; freeze the reviewed text.
_GATEWAY_DEPENDENCY_CASES = '''                # Linux gateway tools; BusyBox has no ss or nft.
                nft) agent_add_package nftables ;;
                ss)
                    case "$AGENT_PACKAGE_MANAGER" in
                        dnf|yum) agent_add_package iproute ;;
                        *) agent_add_package iproute2 ;;
                    esac
                    ;;
                tar|gzip) agent_add_package "$agent_tool" ;;
'''
_MANUAL_INSTALL_COMMAND = '''# A command the user can copy when automatic installation cannot finish.
agent_manual_install_command() {
    case "$AGENT_PACKAGE_MANAGER" in
        apk) printf 'apk add --no-cache %s' "$AGENT_PACKAGES" ;;
        apt-get) printf 'apt-get update && apt-get install -y --no-install-recommends %s' "$AGENT_PACKAGES" ;;
        dnf|yum) printf '%s install -y %s' "$AGENT_PACKAGE_MANAGER" "$AGENT_PACKAGES" ;;
        pacman) printf 'pacman -S --needed %s' "$AGENT_PACKAGES" ;;
        zypper) printf 'zypper install %s' "$AGENT_PACKAGES" ;;
    esac
}
'''
_GATEWAY_DEPENDENCY_REWORDS = (
    ('# Shared by the native single-Agent, All-in-One and Linux gateway installers. Call only after\n',
     '# Shared by the native single-Agent and All-in-One installers. Call only after\n'),
    ('缺少的工具：${AGENT_MISSING_DEPENDENCIES}。可在 root 下手动执行：$(agent_manual_install_command)"\n',
     '缺少的工具：${AGENT_MISSING_DEPENDENCIES}。"\n'),
)
_CORE_INSTALL_PATH = 'PATH="${PATH:-/usr/sbin:/usr/bin:/sbin:/bin}:/usr/local/sbin:/usr/local/bin"\nexport PATH\n'
_CORE_AGENT_FOLLOWS = 'CF_OUTPUT_OWNER=false\n'


# OpenRC core scripts let a core that is still reloading exit before KILL
# (start-stop-daemon otherwise gives up after 5 s), and gateway reruns upgrade
# exactly the earlier script in place. Require the exact locations.
_OPENRC_STOP_RETRY_DELTAS = (
    ('pidfile="/run/$service.pid"\nretry="TERM/30/KILL/5"\n', 'pidfile="/run/$service.pid"\n'),
    ('pidfile="/run/$core.pid"\nretry="TERM/30/KILL/5"\n', 'pidfile="/run/$core.pid"\n'),
    ('    if [ -e "$CORE_UNIT" ]; then\n        if ! cmp -s "$GATEWAY_STAGE/$core.service" "$CORE_UNIT"; then\n            # Earlier installs wrote the same OpenRC script without retry, so\n            # stop waited only 5 s; upgrade exactly that script in place.\n            grep -v \'^retry="TERM/30/KILL/5"$\' "$GATEWAY_STAGE/$core.service" > "$GATEWAY_STAGE/$core.service.v1"\n            if [ "$GATEWAY_INIT" = openrc ] && cmp -s "$GATEWAY_STAGE/$core.service.v1" "$CORE_UNIT"; then\n                cp "$GATEWAY_STAGE/$core.service" "$CORE_UNIT.new"\n                chmod 755 "$CORE_UNIT.new"\n                mv "$CORE_UNIT.new" "$CORE_UNIT"\n            else\n                fail "$CORE_UNIT 已被修改；请人工核对服务后重跑，不会覆盖。"\n            fi\n        fi\n    else\n',
     '    if [ -e "$CORE_UNIT" ]; then\n        cmp -s "$GATEWAY_STAGE/$core.service" "$CORE_UNIT" || fail "$CORE_UNIT 已被修改；请人工核对服务后重跑，不会覆盖。"\n    else\n'),
)


# The core and gateway installers offer to disable systemd-resolved's local
# stub when it alone holds port 53, and fix it only after the user types Y.
# Require the exact reviewed text; each appears at most once per script.
_RESOLVED_STUB_PROMPT_DELTAS = (
    ('DEPENDENCIES_ATTEMPTED=false\nRESOLVED_STUB_DISABLED=false\nCORE_SELECTION=both\n',
     'DEPENDENCIES_ATTEMPTED=false\nCORE_SELECTION=both\n'),
    ('            if [ "$DEPENDENCIES_ATTEMPTED" = true ]; then printf \'%s\\n\' \'包管理器安装的系统依赖会保留，不会随核心回滚而卸载。\' >&2; fi\n            if [ "$RESOLVED_STUB_DISABLED" = true ]; then printf \'%s\\n\' \'已按你的确认关闭 systemd-resolved 的本机 DNS 监听，此设置会保留；如需恢复：rm /etc/systemd/resolved.conf.d/configflow.conf && systemctl restart systemd-resolved\' >&2; fi\n        fi\n',
     '            if [ "$DEPENDENCIES_ATTEMPTED" = true ]; then printf \'%s\\n\' \'包管理器安装的系统依赖会保留，不会随核心回滚而卸载。\' >&2; fi\n        fi\n'),
    ('\n# Prints the listeners occupying the DNS ports this run needs, one per line.\ndns_port_conflicts() {\n    awk -v mihomo="$NEW_MIHOMO" -v oxidns="$NEW_OXIDNS" \'FNR > 1 {split($2, addr, ":"); port=toupper(addr[2]); if (((port == "0035" && oxidns == "true") || (port == "041D" && mihomo == "true")) && (FILENAME ~ /udp/ || $4 == "0A")) print toupper($2) " " FILENAME}\' $proc_files\n}\n# Debian and Ubuntu enable systemd-resolved\'s local stub on 127.0.0.53 and\n# 127.0.0.54 port 53. Offer to disable only that stub, never other DNS services.\nRESOLVED_STUB_FIX="mkdir -p /etc/systemd/resolved.conf.d && printf \'[Resolve]\\\\nDNSStubListener=no\\\\n\' > /etc/systemd/resolved.conf.d/configflow.conf && systemctl restart systemd-resolved"\nonly_resolved_stub() {\n    [ "$INIT_SYSTEM" = systemd ] && [ -n "$1" ] || return 1\n    systemctl is-active --quiet systemd-resolved 2>/dev/null || return 1\n    printf \'%s\\n\' "$1" | awk \'{if ($1 != "3500007F:0035" && $1 != "3600007F:0035") other=1} END {exit other ? 1 : 0}\'\n}\ndisable_resolved_stub() {\n    mkdir -p /etc/systemd/resolved.conf.d || return 1\n    printf \'[Resolve]\\n# ConfigFlow：让出 53 端口给 OxiDNS；删除此文件并重启 systemd-resolved 即恢复\\nDNSStubListener=no\\n\' > /etc/systemd/resolved.conf.d/configflow.conf || return 1\n    RESOLVED_STUB_DISABLED=true\n    systemctl restart systemd-resolved || return 1\n    # Older systemd keeps 127.0.0.53 in stub-resolv.conf; point at the upstreams.\n    if grep -q \'^nameserver 127\\.0\\.0\\.53\' /etc/resolv.conf 2>/dev/null && [ -f /run/systemd/resolve/resolv.conf ]; then\n        ln -sf /run/systemd/resolve/resolv.conf /etc/resolv.conf || return 1\n    fi\n}\noffer_resolved_stub_fix() {\n    resolved_message=\'端口 53 被 systemd-resolved 的本机 DNS（127.0.0.53 / 127.0.0.54）占用，OxiDNS 需要使用此端口。自动修复会关闭它的本机监听（DNSStubListener=no），本机解析改为直接使用上游 DNS，其他服务不受影响。\'\n    # A failed redirection on the special built-in \':\' would exit the whole\n    # script; probe the terminal in a subshell instead.\n    if ( : < /dev/tty && : >> /dev/tty ) 2>/dev/null; then\n        printf \'%s\\n是否自动修复？输入 Y 确认，其他任意键取消 [y/N]: \' "$resolved_message" >> /dev/tty\n        resolved_answer=""\n        read -r resolved_answer < /dev/tty || resolved_answer=""\n        case "$resolved_answer" in\n            Y|y|YES|yes|Yes)\n                disable_resolved_stub || fail "关闭 systemd-resolved 本机监听失败。可手动执行后重跑安装命令：$RESOLVED_STUB_FIX"\n                printf \'%s\\n\' \'已关闭 systemd-resolved 的本机 DNS 监听：/etc/systemd/resolved.conf.d/configflow.conf\'\n                cf_fact resolved_stub "已按确认关闭 systemd-resolved 的本机 DNS 监听；如需恢复：rm /etc/systemd/resolved.conf.d/configflow.conf && systemctl restart systemd-resolved"\n                return 0 ;;\n        esac\n        fail "已取消自动修复，安装停止。可手动执行后重跑安装命令：$RESOLVED_STUB_FIX"\n    fi\n    fail "${resolved_message}当前没有可交互的终端，安装停止。可执行后重跑安装命令：$RESOLVED_STUB_FIX"\n}\n\ncheck_ports() {\n',
     '\ncheck_ports() {\n'),
    ('    # Any IPv4/IPv6 listener on these ports can conflict with a wildcard bind.\n    conflicts=$(dns_port_conflicts)\n    [ -n "$conflicts" ] || return 0\n    printf \'%s\\n\' "$conflicts" | awk \'{print "已占用 DNS 端口：" $1 " (" $2 ")"}\'\n    if only_resolved_stub "$conflicts"; then\n        offer_resolved_stub_fix\n        conflicts=$(dns_port_conflicts)\n        [ -n "$conflicts" ] || return 0\n        printf \'%s\\n\' "$conflicts" | awk \'{print "已占用 DNS 端口：" $1 " (" $2 ")"}\'\n        fail "关闭 systemd-resolved 本机监听后，DNS 端口仍被占用。请用 ss -lntup 检查后重试。"\n    fi\n',
     '    # Any IPv4/IPv6 listener on these ports can conflict with a wildcard bind.\n    if ! awk -v mihomo="$NEW_MIHOMO" -v oxidns="$NEW_OXIDNS" \'FNR > 1 {split($2, addr, ":"); port=toupper(addr[2]); if (((port == "0035" && oxidns == "true") || (port == "041D" && mihomo == "true")) && (FILENAME ~ /udp/ || $4 == "0A")) {print "已占用 DNS 端口：" $2 " (" FILENAME ")"; conflict=1}} END {exit conflict ? 1 : 0}\' $proc_files; then\n        fail "DNS 端口被占用（本次安装需要：${DNS_PORTS}）。请用 ss -lntup 检查并调整已有 DNS 服务的监听地址/端口，再重试；本脚本不会关闭系统 DNS 服务。"\n    fi\n'),
    ('    fi\n    fail "DNS 端口被占用（本次安装需要：${DNS_PORTS}）。请用 ss -lntup 检查并调整已有 DNS 服务的监听地址/端口，再重试；本脚本不会关闭系统 DNS 服务。"\n}\n',
     '    fi\n}\n'),
    ('}\nport_busy() {\n    ss -H -lntu | awk -v wanted="$1" \'{ n=split($5,a,":"); if (a[n]==wanted) found=1 } END {exit !found}\'\n}\n# Debian and Ubuntu enable systemd-resolved\'s local DNS stub on port 53. Offer\n# to disable only that stub when it is the sole owner; never other DNS services.\nRESOLVED_STUB_FIX="mkdir -p /etc/systemd/resolved.conf.d && printf \'[Resolve]\\\\nDNSStubListener=no\\\\n\' > /etc/systemd/resolved.conf.d/configflow.conf && systemctl restart systemd-resolved"\nonly_resolved_stub() {\n    [ "$GATEWAY_INIT" = systemd ] || return 1\n    systemctl is-active --quiet systemd-resolved 2>/dev/null || return 1\n    ss -H -lntup 2>/dev/null | awk \'{ n=split($5,a,":"); if (a[n]=="53") { found=1; if (!/systemd-resolve/) other=1 } } END {exit (found && !other) ? 0 : 1}\'\n}\ndisable_resolved_stub() {\n    mkdir -p /etc/systemd/resolved.conf.d || return 1\n    printf \'[Resolve]\\n# ConfigFlow：让出 53 端口给 OxiDNS；删除此文件并重启 systemd-resolved 即恢复\\nDNSStubListener=no\\n\' > /etc/systemd/resolved.conf.d/configflow.conf || return 1\n    systemctl restart systemd-resolved || return 1\n    # Older systemd keeps 127.0.0.53 in stub-resolv.conf; point at the upstreams.\n    if grep -q \'^nameserver 127\\.0\\.0\\.53\' /etc/resolv.conf 2>/dev/null && [ -f /run/systemd/resolve/resolv.conf ]; then\n        ln -sf /run/systemd/resolve/resolv.conf /etc/resolv.conf || return 1\n    fi\n}\noffer_resolved_stub_fix() {\n    resolved_message=\'端口 53 被 systemd-resolved 的本机 DNS（127.0.0.53 / 127.0.0.54）占用，OxiDNS 需要使用此端口。自动修复会关闭它的本机监听（DNSStubListener=no），本机解析改为直接使用上游 DNS，其他服务不受影响。\'\n    # A failed redirection on the special built-in \':\' would exit the whole\n    # script; probe the terminal in a subshell instead.\n    if ( : < /dev/tty && : >> /dev/tty ) 2>/dev/null; then\n        printf \'%s\\n是否自动修复？输入 Y 确认，其他任意键取消 [y/N]: \' "$resolved_message" >> /dev/tty\n        resolved_answer=""\n        read -r resolved_answer < /dev/tty || resolved_answer=""\n        case "$resolved_answer" in\n            Y|y|YES|yes|Yes)\n                disable_resolved_stub || fail "关闭 systemd-resolved 本机监听失败。可手动执行后重跑安装命令：$RESOLVED_STUB_FIX"\n                printf \'%s\\n\' \'已关闭 systemd-resolved 的本机 DNS 监听：/etc/systemd/resolved.conf.d/configflow.conf\'\n                cf_fact resolved_stub "已按确认关闭 systemd-resolved 的本机 DNS 监听；如需恢复：rm /etc/systemd/resolved.conf.d/configflow.conf && systemctl restart systemd-resolved"\n                return 0 ;;\n        esac\n        fail "已取消自动修复，安装停止，未修改任何文件。可手动执行后重跑安装命令：$RESOLVED_STUB_FIX"\n    fi\n    fail "${resolved_message}当前没有可交互的终端，安装停止。可执行后重跑安装命令：$RESOLVED_STUB_FIX"\n}\ncheck_port_free() {\n',
     '}\ncheck_port_free() {\n'),
    ('    # by the Agent\'s paired environment preflight below.\n    port_busy "$1" || return 0\n    if [ "$1" = 53 ] && only_resolved_stub; then\n        offer_resolved_stub_fix\n        port_busy 53 || return 0\n        fail "关闭 systemd-resolved 本机监听后，端口 53 仍被占用。请用 ss -lntup 检查后重试。"\n    fi\n',
     '    # by the Agent\'s paired environment preflight below.\n    if ss -H -lntu | awk -v wanted="$1" \'{ n=split($5,a,":"); if (a[n]==wanted) found=1 } END {exit !found}\'; then\n        # Debian and Ubuntu enable this local stub by default; name the fix.\n        if [ "$1" = 53 ] && ss -H -lntup 2>/dev/null | awk \'{ n=split($5,a,":"); if (a[n]=="53" && /systemd-resolve/) found=1 } END {exit !found}\'; then\n            fail "端口 53 被 systemd-resolved 的本机 DNS（127.0.0.53）占用，OxiDNS 需要使用此端口。可关闭其本机监听后重跑安装命令：mkdir -p /etc/systemd/resolved.conf.d && printf \'[Resolve]\\nDNSStubListener=no\\n\' > /etc/systemd/resolved.conf.d/configflow.conf && ln -sf /run/systemd/resolve/resolv.conf /etc/resolv.conf && systemctl restart systemd-resolved"\n        fi\n        fail "端口 $1 已被占用。请用 ss -lntup 检查所属服务并先处理冲突；安装器不会停止现有 DNS 服务。"\n    fi\n'),
    ('    fi\n    fail "端口 $1 已被占用。请用 ss -lntup 检查所属服务并先处理冲突；安装器不会停止现有 DNS 服务。"\n}\n',
     '    fi\n}\n'),
)


# Installer hardening: stages on an executable filesystem instead of a possibly
# noexec /tmp, OpenRC restarts a crashed Agent and rotates the core log, a
# started Agent must still be running two seconds later, the previous Agent is
# started again after a failed upgrade, the All-in-One uninstall tolerates an
# Agent that was never enabled, and a running systemd wins over OpenRC tools.
_INSTALLER_HARDENING_DELTAS = (
    # install-go.sh
    ('''if [ "$INIT_SYSTEM" = "systemd" ]; then
    if systemctl is-active --quiet configflow-agent 2>/dev/null; then RESTART_PREVIOUS_AGENT=1; fi
    systemctl stop configflow-agent 2>/dev/null || true
else
    if rc-service configflow-agent status >/dev/null 2>&1; then RESTART_PREVIOUS_AGENT=1; fi
    rc-service configflow-agent stop 2>/dev/null || true
fi
''', '''if [ "$INIT_SYSTEM" = "systemd" ]; then
    systemctl stop configflow-agent 2>/dev/null || true
else
    rc-service configflow-agent stop 2>/dev/null || true
fi
'''),
    ('''    systemctl start configflow-agent
    # An Agent that exits right after starting must not count as started.
    sleep 2
    if ! systemctl is-active --quiet configflow-agent; then
        echo "Agent 启动后未保持运行，请查看 journalctl -u configflow-agent -n 50。" >&2
        exit 1
    fi
else''', '''    systemctl start configflow-agent
    systemctl is-active --quiet configflow-agent
else'''),
    ('''# Restart a crashed Agent, as Restart=always does under systemd.
supervisor="supervise-daemon"
respawn_delay=10
respawn_max=0
pidfile="/run/configflow-agent.pid"
output_log="/var/log/configflow-agent.log"
error_log="/var/log/configflow-agent.log"
directory="/opt/configflow-agent"
''', '''pidfile="/run/configflow-agent.pid"
output_log="/var/log/configflow-agent.log"
error_log="/var/log/configflow-agent.log"
directory="/opt/configflow-agent"
command_background="yes"
'''),
    ('''    # supervise-daemon keeps inherited descriptors: close the report fds, or a
    # piped or non-interactive SSH run would never return.
    rc-service configflow-agent start 3>&- 4>&-
    sleep 2
    # supervise-daemon stays "started" while it waits to respawn an Agent that
    # exited, so also require the child of the supervisor named in its pidfile.
    agent_supervisor=$(cat /run/configflow-agent.pid 2>/dev/null || true)
    if ! rc-service configflow-agent status ||
        ! grep -qs "^PPid:[[:space:]]*${agent_supervisor:-missing}\\$" /proc/[0-9]*/status; then
        echo "Agent 启动后未保持运行，请查看 /var/log/configflow-agent.log。" >&2
        exit 1
    fi
fi
RESTART_PREVIOUS_AGENT=0
''', '''    rc-service configflow-agent start
    rc-service configflow-agent status
fi
'''),
    ('''if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
    INIT_SYSTEM="systemd"
elif command -v rc-service >/dev/null 2>&1 && command -v rc-update >/dev/null 2>&1; then
    INIT_SYSTEM="openrc"
elif command -v systemctl >/dev/null 2>&1; then
    INIT_SYSTEM="systemd"
else''', '''if command -v rc-service >/dev/null 2>&1 && command -v rc-update >/dev/null 2>&1; then
    INIT_SYSTEM="openrc"
elif command -v systemctl >/dev/null 2>&1; then
    INIT_SYSTEM="systemd"
else'''),
    # install-go-aio.sh
    ('''                systemctl disable "$AGENT_UNIT" 2>/dev/null || true
''', '''                systemctl disable "$AGENT_UNIT"
'''),
    ('''                # Not being in the runlevel (an install that failed before
                # enabling it) must not leave the uninstall stuck halfway.
                rc-update del "$AGENT_UNIT" default 2>/dev/null || true
''', '''                rc-update del "$AGENT_UNIT" default
'''),
    ('''# Restart a crashed Agent, as Restart=always does under systemd.
supervisor="supervise-daemon"
respawn_delay=10
respawn_max=0
pidfile="/run/$AGENT_UNIT.pid"
output_log="$AGENT_LOG"
error_log="$AGENT_LOG"
directory="$AGENT_DIR"
''', '''pidfile="/run/$AGENT_UNIT.pid"
output_log="$AGENT_LOG"
error_log="$AGENT_LOG"
directory="$AGENT_DIR"
command_background="yes"
'''),
    ('''        systemctl enable "$AGENT_UNIT"
        systemctl start "$AGENT_UNIT"
    else
        rc-update add "$AGENT_UNIT" default
        # supervise-daemon keeps inherited descriptors: close the report fds,
        # or a piped or non-interactive SSH run would never return.
        rc-service "$AGENT_UNIT" start 3>&- 4>&-
    fi
done
# An Agent that exits right after starting must not count as started.
sleep 2
for service in mihomo oxidns; do
    select_service "$service"
    if [ "$INIT_SYSTEM" = systemd ]; then
        systemctl is-active --quiet "$AGENT_UNIT" || fail "$SERVICE Agent 启动后未保持运行，请查看 journalctl -u $AGENT_UNIT 或 $AGENT_LOG。"
    else
        # supervise-daemon stays "started" while it waits to respawn an Agent
        # that exited, so also require the child of the supervisor in its pidfile.
        agent_supervisor=$(cat "/run/$AGENT_UNIT.pid" 2>/dev/null || true)
        rc-service "$AGENT_UNIT" status &&
            grep -qs "^PPid:[[:space:]]*${agent_supervisor:-missing}\\$" /proc/[0-9]*/status ||
            fail "$SERVICE Agent 启动后未保持运行，请查看 $AGENT_LOG。"
    fi
done
''', '''        systemctl enable "$AGENT_UNIT"
        systemctl start "$AGENT_UNIT"
        systemctl is-active --quiet "$AGENT_UNIT"
    else
        rc-update add "$AGENT_UNIT" default
        rc-service "$AGENT_UNIT" start
        rc-service "$AGENT_UNIT" status
    fi
done
'''),
    ('''if [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; then
    INIT_SYSTEM=systemd
elif command -v rc-service >/dev/null 2>&1 && command -v rc-update >/dev/null 2>&1; then
    INIT_SYSTEM=openrc
elif command -v systemctl >/dev/null 2>&1; then
    INIT_SYSTEM=systemd
else''', '''if command -v rc-service >/dev/null 2>&1 && command -v rc-update >/dev/null 2>&1; then
    INIT_SYSTEM=openrc
elif command -v systemctl >/dev/null 2>&1; then
    INIT_SYSTEM=systemd
else'''),
    ('''                rc-service "configflow-agent-$stopped_service" start 3>&- 4>&- || true
''', '''                rc-service "configflow-agent-$stopped_service" start || true
'''),
    # install-cores.sh
    ('''# /tmp is often mounted noexec, and the downloaded cores validate their
# configuration from the stage. /usr/local was checked above and holds the
# installed binaries anyway.
STAGE=$(mktemp -d /usr/local/.configflow-cores.XXXXXX)
''', '''STAGE=$(mktemp -d /tmp/configflow-cores.XXXXXX)
'''),
    ('curl --fail --silent --show-error --location --connect-timeout 15', 'curl --fail --location --connect-timeout 15'),
    ('''depend() {
    need net
}
start_pre() {
    # OpenRC appends the core's output here and never rotates it. Keep one
    # previous file once it passes 10 MB; a ConfigFlow Agent also trims it.
    log_size=\\$(wc -c < "/etc/$service/$service.log" 2>/dev/null || echo 0)
    if [ "\\$log_size" -gt 10485760 ]; then
        mv -f "/etc/$service/$service.log" "/etc/$service/$service.log.1"
    fi
}
''', '''depend() {
    need net
}
'''),
    # install-gateway.sh
    ('''# /tmp is often mounted noexec, and the downloaded cores and Agent run from the
# stage before anything is installed.
mkdir -p /usr/local
GATEWAY_STAGE=$(mktemp -d /usr/local/.configflow-gateway.XXXXXX)
''', '''GATEWAY_STAGE=$(mktemp -d /tmp/configflow-gateway.XXXXXX)
'''),
    ('''# systemd as the running init wins: OpenRC tools can be installed beside it.
if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    GATEWAY_INIT=systemd
elif command -v rc-service >/dev/null 2>&1 && command -v rc-update >/dev/null 2>&1; then
    GATEWAY_INIT=openrc
else''', '''if command -v rc-service >/dev/null 2>&1 && command -v rc-update >/dev/null 2>&1; then
    GATEWAY_INIT=openrc
elif command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    GATEWAY_INIT=systemd
else'''),
)
_UNIFIED_CLEANUP = re.compile(r'BINARY_TMP=""\nRESTART_PREVIOUS_AGENT=0\n.*?\ntrap \'exit 143\' TERM\n(?=\n# Shared by the native single-Agent)', re.S)
_STAGED_AGENT = 'BINARY_TMP=$(mktemp /usr/local/bin/.configflow-agent.install.XXXXXX)\n'
_PREVIOUS_CORE_CLEANUP = '''BINARY_TMP=""
configflow_agent_cleanup() {
    agent_status=$?
    trap - 0 HUP INT TERM
    if [ -n "${BINARY_TMP:-}" ]; then rm -f "$BINARY_TMP" || true; fi
    if [ "$agent_status" -ne 0 ]; then
        echo "Agent 安装未完成；%s 核心安装阶段已结束，已安装或原有的核心予以保留。修复上方 Agent 错误后可重新运行。" >&2
    fi
    exit "$agent_status"
}
trap configflow_agent_cleanup 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
'''


def without_unified_agent_cleanup(script, *, core_path):
    """The single-service installer now installs one cleanup trap before any change.

    Earlier releases set a bare ``rm`` trap after staging the Agent, or, with a
    core phase, a cleanup function after that phase.
    """
    blocks = _UNIFIED_CLEANUP.findall(script)
    if not blocks:
        return script  # All-in-One and gateway installers keep their own cleanup.
    assert len(blocks) == 1 and script.count(_STAGED_AGENT) == 1
    script = script.replace(blocks[0], '', 1)
    if not core_path:
        assert 'CORE_PHASE_COMPLETED' not in blocks[0]
        return script.replace(_STAGED_AGENT, _STAGED_AGENT + 'trap \'rm -f "$BINARY_TMP"\' EXIT HUP INT TERM\n', 1)
    service = re.search(r'Agent 安装未完成；([a-z]+) 核心安装阶段已结束', blocks[0])[1]
    assert script.count('\n)\nCORE_PHASE_COMPLETED=true\nCORE_BINARY=') == 1
    script = script.replace('\n)\nCORE_PHASE_COMPLETED=true\nCORE_BINARY=',
                            '\n)\n' + _PREVIOUS_CORE_CLEANUP % service + 'CORE_BINARY=', 1)
    return script.replace(
        _STAGED_AGENT, _STAGED_AGENT + '# The Agent cleanup trap above preserves the completed core phase.\n', 1)


def without_reviewed_installer_deltas(script, *, core_path=False):
    count = script.count(_NATIVE_HA_DEPENDENCY_CASES)
    assert count >= 1, 'the reviewed native HA dependency addition changed'
    assert script.count(_NATIVE_HA_DEPENDENCY_CASES + '        if ! command -v "$agent_tool"') == count
    assert script.count(_NATIVE_HA_PACKAGE_CASE) == count
    script = script.replace(_NATIVE_HA_DEPENDENCY_CASES, '').replace(_NATIVE_HA_PACKAGE_CASE, '')
    assert script.count('                cmp) agent_add_package diffutils ;;\n' + _GATEWAY_DEPENDENCY_CASES) == count
    assert script.count(_MANUAL_INSTALL_COMMAND + 'agent_install_packages() {') == count
    script = script.replace(_GATEWAY_DEPENDENCY_CASES, '').replace(_MANUAL_INSTALL_COMMAND, '')
    for new, old in _GATEWAY_DEPENDENCY_REWORDS:
        assert script.count(new) == count, new
        script = script.replace(new, old)
    if core_path:
        # 707208f4 makes combined single-service installs find cores placed in
        # /usr/local/bin, even in minimal root shells. Require the exact location.
        assert script.count('set -eu\n' + _CORE_INSTALL_PATH + 'for cf_argument do\n') == 1
        script = script.replace(_CORE_INSTALL_PATH, '', 1)
        # The Agent follows the embedded core phase, so that phase no longer
        # asks for a separate Agent install. Require the exact location.
        embedded = re.findall(r'\(\nset -- --core [a-z]+\n' + _CORE_AGENT_FOLLOWS, script)
        assert len(embedded) == 1, 'the embedded core phase must not ask for a separate Agent'
        script = script.replace(embedded[0], embedded[0].removesuffix(_CORE_AGENT_FOLLOWS), 1)
    for new, old in _OPENRC_STOP_RETRY_DELTAS:
        assert script.count(new) <= 1, new
        script = script.replace(new, old)
    for new, old in _RESOLVED_STUB_PROMPT_DELTAS:
        assert script.count(new) <= 1, new
        script = script.replace(new, old)
    for new, old in _INSTALLER_HARDENING_DELTAS:
        assert script.count(new) <= 1, new
        script = script.replace(new, old)
    # The wait before confirming a started Agent made sleep a checked dependency
    # (the gateway installer embeds the dependency helper twice).
    assert script.count('readlink rm sed sleep wc "$@"; do') == count
    script = script.replace('readlink rm sed sleep wc "$@"; do', 'readlink rm sed wc "$@"; do')
    return without_unified_agent_cleanup(script, core_path=core_path)


def _app(tmp_path):
    repository = ProfileRepository(tmp_path)
    config_store.set_repository(repository)
    app = Flask(__name__)
    register_blueprints(app)
    return app.test_client(), repository


def gateway_goal_fields(tmp_path):
    """The wizard's gateway goal from creation through the installer's saved drafts."""
    previous = config_store._repository
    try:
        client, repository = _app(tmp_path)
        session = client.post('/api/setup', json={'id': SESSION, 'name': '网关配置', 'goal': 'gateway'}).get_json()
        repository.update_shared_transaction(lambda shared: shared.update(
            nodes=[deepcopy(NODE)], rule_library=[deepcopy(IP_RULE)]))
        assert client.post(f'/api/setup/{SESSION}/source', json={'node_ids': ['node']}).status_code == 200
        # The DNS query recorder switch postdates 0ef9e89, which left it off.
        response = client.post(f'/api/setup/{SESSION}/prepare', json={'controller_secret': SECRET, 'domain_rules': DOMAIN_RULES, 'query_recorder': False})
        assert response.status_code == 200, response.get_json()
        url = f"/api/profiles/{session['profile_id']}/gateway-onboarding"
        state = client.get(url).get_json()
        # The installation wizard sends back the state's settings as they are.
        body = {**state['settings'], 'expected_config_revision': state['config_revision']}
        preview = client.post(url + '/preview', json=body).get_json()
        assert preview['ready'], preview
        saved = client.post(url, json=body)
        assert saved.status_code == 200, saved.get_json()
        profile = repository.get_profile(session['profile_id'])
        return {key: profile[key] for key in ('proxy_groups', 'rule_configs', 'mihomo', 'oxidns')}
    finally:
        config_store.set_repository(previous)


def single_service_script_digests():
    """Single-service installers, which the full TUN never reaches."""
    result = {}
    for service, cores in (('mihomo', (False, True)), ('oxidns', (False, True)), ('mosdns', (False,))):
        for install_core in cores:
            script = generate_go_agent_install_script(
                'https://config.test', 'single', service, agent_ip='192.0.2.20',
                binary_download_url='https://config.test/api/agents/download', install_core=install_core)
            result[f'{service}:{install_core}'] = hashlib.sha256(without_reviewed_installer_deltas(script, core_path=install_core).encode()).hexdigest()
    return result


def unmarked_profiles():
    """Profiles without a router split marker: ordinary, selective gateway, and a tun of the user's own."""
    ordinary = snapshot()
    gateway, report = prepare_gateway_onboarding(snapshot(), form())
    assert report['ready'], report
    own_tun = snapshot()
    own_tun['mihomo']['custom_config'] = ('mixed-port: 7890\ntun:\n  enable: true\n  stack: system\n'
                                          '  auto-route: true\n  route-address-set: [proxy-ip]\n')
    return {'ordinary': ordinary, 'gateway': gateway, 'own_tun': own_tun}


def unmarked_mihomo_outputs():
    return {name: generate_mihomo_config(config, preflight_providers=False)
            for name, config in unmarked_profiles().items()}


def baseline():
    return json.loads(BASELINE.read_text(encoding='utf-8'))


# Per-rule OxiDNS DNS settings postdate 0ef9e89. Stored empty, they leave every
# generated output unchanged, so they must be empty and are otherwise ignored.
RULE_DNS_DEFAULTS = {'rule_actions': {}, 'rule_dns_follow': [], 'rule_dns_parked': {}}


_PLAIN_SETUP_NAMESERVER = '  nameserver:\n  - 223.5.5.5\n  - 119.29.29.29\n'


def with_current_setup_nameserver(fields):
    """0ef9e89's wizard wrote plain Mihomo upstreams; it now writes the gateway's DoH default."""
    from backend.common.gateway_onboarding import DEFAULT_MIHOMO_NAMESERVERS
    source = fields['mihomo']['custom_config']
    assert source.count(_PLAIN_SETUP_NAMESERVER) == 1 and source.endswith(_PLAIN_SETUP_NAMESERVER)
    current = '  nameserver:\n' + ''.join(f'  - {value}\n' for value in DEFAULT_MIHOMO_NAMESERVERS)
    return {**fields, 'mihomo': {**fields['mihomo'], 'custom_config': source.replace(_PLAIN_SETUP_NAMESERVER, current)}}


# 0ef9e89's wizard replaced the base YAML with a short one. It now keeps the
# profile's base and only sets what the plan needs; the new setup profile's base
# is empty, so it starts from the initial template (config_template.json), with
# the user's controller password, Mihomo DNS on 127.0.0.1:1053 and the
# template's TUN left to the Linux gateway. Freeze exactly this reviewed change.
_SETUP_SHORT_BASE = ("mixed-port: 7890\nallow-lan: true\nbind-address: '*'\nmode: rule\nlog-level: info\n"
                     'external-controller: 127.0.0.1:9090\ndns:\n  enable: true\n  listen: 127.0.0.1:1053\n'
                     '  enhanced-mode: fake-ip\n')


def with_current_setup_base(fields):
    """The wizard's own base for a new gateway profile: the initial template with the plan's settings."""
    from backend.common.resource import get_backend_resource
    fields = with_current_setup_nameserver(fields)
    source = fields['mihomo']['custom_config']
    assert source.startswith(_SETUP_SHORT_BASE) and 'tun:' not in source
    template = yaml.safe_load(json.loads(Path(get_backend_resource('config_template.json')).read_text(encoding='utf-8'))['mihomo']['custom_config'])
    template['secret'] = SECRET
    template['dns']['listen'] = '127.0.0.1:1053'
    template['tun']['enable'] = False
    current = yaml.safe_dump(template, allow_unicode=True, sort_keys=False)
    # New profiles keep stale cache entries for an hour instead of 300 seconds;
    # profiles saved earlier keep their stored value.
    oxidns = {**fields['oxidns'], 'cache_lazy_ttl': 3600}
    return {**fields, 'mihomo': {**fields['mihomo'], 'custom_config': current}, 'oxidns': oxidns}


def test_gateway_goal_drafts_match_the_previous_release(tmp_path):
    fields = gateway_goal_fields(tmp_path)
    oxidns = fields['oxidns']
    assert {key: oxidns.get(key) for key in RULE_DNS_DEFAULTS} == RULE_DNS_DEFAULTS
    fields['oxidns'] = {key: value for key, value in oxidns.items() if key not in RULE_DNS_DEFAULTS}
    assert fields == with_current_setup_base(baseline()['gateway_goal_fields'])


def test_single_service_scripts_match_the_previous_release():
    assert single_service_script_digests() == baseline()['single_service_script_sha256']


@pytest.mark.parametrize('name', ['ordinary', 'gateway', 'own_tun'])
def test_mihomo_without_marker_matches_the_previous_release(name):
    expected = baseline()['unmarked_mihomo'][name]
    # Text delivery compatibility postdates this baseline; only its explicit
    # local-provider marker may differ. All routing/TUN bytes stay identical.
    for provider in yaml.safe_load(expected).get('rule-providers', {}).values():
        url = provider.get('url', '')
        if '/rules/local/' in url and provider.get('format') != 'mrs':
            expected = expected.replace('url: ' + url + '\n',
                                        'url: ' + url + '?target=mihomo\n')
    if name == 'gateway':
        expected = with_current_gateway_nameserver(expected)
    assert unmarked_mihomo_outputs()[name] == expected


_PREVIOUS_GATEWAY_NAMESERVER = '  nameserver:\n    - https://223.5.5.5/dns-query\n    - https://1.1.1.1/dns-query\n'


def with_current_gateway_nameserver(expected):
    """0ef9e89 filled the gateway's missing Mihomo upstreams with Cloudflare DoH; it is now domestic."""
    from backend.common.gateway_onboarding import DEFAULT_MIHOMO_NAMESERVERS
    assert expected.count(_PREVIOUS_GATEWAY_NAMESERVER) == 1
    current = '  nameserver:\n' + ''.join(f'    - {value}\n' for value in DEFAULT_MIHOMO_NAMESERVERS)
    return expected.replace(_PREVIOUS_GATEWAY_NAMESERVER, current)


def test_router_setup_runs_end_to_end_on_a_full_tun(authenticated_app, monkeypatch):
    """Setup, installer drafts, installer script, publication and the final check, with fake Agents."""
    from backend.agents import gateway_installation as installations
    from backend.common.router_split import FULL_TUN, is_full_tun, router_split_state
    from backend.routes.agents import _prepare_transactional_content
    client, manager, _, admin, _ = authenticated_app
    repository = manager.repository
    repository.update_system_transaction(
        lambda system: system['system_config'].update(server_domain='https://config.example'))
    repository.update_shared_transaction(lambda shared: shared.update(
        nodes=[deepcopy(NODE)], rule_library=[deepcopy(IP_RULE)]))
    session = client.post('/api/setup', headers=admin, json={'id': SESSION, 'name': '分流配置', 'goal': 'router'}).get_json()
    profile_id = session['profile_id']
    assert client.post(f'/api/setup/{SESSION}/source', headers=admin, json={'node_ids': ['node']}).status_code == 200
    prepared = client.post(f'/api/setup/{SESSION}/prepare', headers=admin, json={'controller_secret': SECRET, 'domain_rules': DOMAIN_RULES})
    assert prepared.status_code == 200, prepared.get_json()
    assert prepared.get_json()['legacy_takeover'] is False

    # The installer's drafts keep the gateway off and the base YAML on the full TUN.
    url = f'/api/profiles/{profile_id}/gateway-onboarding'
    state = client.get(url, headers=admin).get_json()
    assert state['takeover'] == 'full_tun' and state['gateway_enabled'] is False
    body = {**state['settings'], 'rule_set_ids': [], 'expected_config_revision': state['config_revision']}
    assert client.post(url + '/preview', headers=admin, json=body).get_json()['ready']
    saved = client.post(url, headers=admin, json=body)
    assert saved.status_code == 200, saved.get_json()
    config, revision = repository.get_publish_snapshot(profile_id)
    assert is_full_tun(config) and config['mihomo']['linux_gateway'] == {'enabled': False, 'rule_set_ids': []}
    assert yaml.safe_load(config['mihomo']['custom_config'])['tun'] == FULL_TUN
    assert router_split_state(repository, profile_id)['mode'] == 'full_tun'

    # The installer persists forwarding and prepares the TUN itself.
    created = client.post('/api/agents/gateway-installations', headers=admin, json={
        'profile_id': profile_id, 'name': 'split-linux', 'server_url': 'https://config.example',
        'auto_network': True, 'agent_ip': '192.0.2.10', 'config_revision': revision})
    assert created.status_code == 200, created.get_json()
    assert created.get_json()['takeover'] == 'full_tun'
    script = installations._read(repository, created.get_json()['id'])['script']
    assert '-prepare-tun -assume-tun-enabled' in script and 'persist_ipv4_forwarding' in script
    assert 'CONFIGFLOW_FORWARDING_OWNER' not in script

    # Publishing generates the full TUN and refuses a shell Agent that would drop forwarding.
    agent = {'id': 'm', 'service_type': 'mihomo', 'deployment_method': 'shell', 'version': '1.8.40-go'}
    monkeypatch.setattr('backend.agents.linux_gateway.call', lambda *args: {
        'success': True, 'managed': {'active': False, 'healthy': False, 'preserves_forwarding': True}})
    main = yaml.safe_load(_prepare_transactional_content(agent, config)['config_content'])
    assert main['tun']['enable'] is True and main['tun']['auto-redirect'] is True
    assert 'x-configflow-linux-gateway' not in main
    with pytest.raises(ValueError, match='1.8.40-go'):
        _prepare_transactional_content({**agent, 'version': '1.8.39-go'}, config)

    # The final check reads forwarding and TUN, then hands the router only fake-IP.
    agents = {service: {'id': service, 'name': service, 'host': '192.0.2.10', 'status': 'online'}
              for service in ('mihomo', 'oxidns')}
    monkeypatch.setattr('backend.agents.gateway_installation.latest', lambda *_: {'session': {
        'id': 'installation', 'phase': 'published', 'target_changed': False, 'agents': agents,
        'config_changed': repository.config_revision(profile_id) != revision}})
    status = {'success': True, 'supported': True, 'enabled_in_config': False,
              'managed': {'active': False, 'healthy': False, 'preserves_forwarding': True},
              'ipv4_forwarding': {'protocol': 1, 'current': '1', 'config': 'managed', 'hook_script': 'managed',
                                  'startup_hook': 'registered', 'persisted': True, 'supported': True},
              'tun': {'configured': True, 'device': 'Meta', 'present': True, 'gateway_marker': False}}
    calls = []
    monkeypatch.setattr('backend.agents.linux_gateway.call', lambda *args: calls.append(args[2:4]) or deepcopy(status))
    status['ipv4_forwarding']['persisted'] = False
    refused = client.post(f'/api/setup/{SESSION}/check', headers=admin, json={})
    assert refused.status_code == 409 and '持久化' in refused.get_json()['message']
    status['ipv4_forwarding']['persisted'] = True
    checked = client.post(f'/api/setup/{SESSION}/check', headers=admin, json={})
    assert checked.status_code == 200, checked.get_json()
    assert set(calls) == {('status', 'GET')}
    fake_ip = checked.get_json()['route_instructions']['routes']
    assert [item['cidr'] for item in fake_ip] == ['198.18.0.0/16']
    assert fake_ip[0]['requires_source_exclusion'] is False
    finished = client.post(f'/api/setup/{SESSION}/finish', headers=admin,
                           json={'router_mode': 'manual', 'ack_routes': True, 'ack_dns': True})
    assert finished.status_code == 200, finished.get_json()
    assert finished.get_json()['completed'] and finished.get_json()['router_verification'] == 'manual'
    # Nothing on the way turned the selective gateway on.
    final = repository.get_profile(profile_id)['mihomo']
    assert final['linux_gateway']['enabled'] is False and final['router_split']['route_rule_set_ids'] == []
