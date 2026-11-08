"""Network setup regressions, with all installer effects confined to temp files."""

import os
from pathlib import Path
import re
import subprocess

import pytest

from backend.agents.install_output import with_install_output
from backend.test_agent_native_aio_install import (
    FORWARD_CONFIG,
    detail_output,
    harness,
    run,
)
from backend.test_agent_install_dependencies import isolate_dependencies


SCRIPTS = Path(__file__).parent / "agents" / "scripts"


def shell_function(filename, name):
    """Extract a top-level function without executing the surrounding installer."""
    source = (SCRIPTS / filename).read_text()
    match = re.search(rf"^{re.escape(name)}\(\) \{{\n.*?^\}}$", source, re.M | re.S)
    assert match, f"Missing shell function {name}"
    return match.group(0)


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
@pytest.mark.parametrize("missing_tool", ["sysctl", "cmp", "ln"])
def test_missing_forwarding_prerequisite_fails_before_stopping_agents(tmp_path, manager, missing_tool):
    installer, env, units = harness(tmp_path, manager)
    # All commands initially exist, so dependency installation completes. The
    # later environment check simulates a command disappearing before use.
    isolate_dependencies(tmp_path, env, package_manager=None)
    env["MISSING_TOOL_READY"] = str(tmp_path / "missing-tool-ready")
    agent = Path(env["FAKE_AGENT"])
    marker = "if '-check-environment' in args:\n"
    assert marker in agent.read_text()
    agent.write_text(agent.read_text().replace(
        marker, marker + "    open(os.environ['MISSING_TOOL_READY'], 'w').close()\n", 1))
    before = {
        path: path.read_bytes()
        for directory in (tmp_path / "agent", units)
        for path in directory.rglob("*")
        if path.is_file()
    }
    # Intercept discovery, not execution. All real kernel writes are already
    # redirected to the harness's proc file and strict sysctl mock.
    source = installer.read_text()
    discovery_override = '''command() {
    if [ -f "$MISSING_TOOL_READY" ] && [ "$#" -eq 2 ] && [ "$1" = -v ] && [ "$2" = "$MISSING_TOOL" ]; then
        return 127
    fi
    /bin/sh -c 'command "$@"' sh "$@"
}
'''
    installer.write_text(source.replace("#!/bin/sh\n", "#!/bin/sh\n" + discovery_override, 1))
    result = run(installer, dict(env, MISSING_TOOL=missing_tool))

    assert result.returncode != 0
    assert f"缺少 {missing_tool} 命令" in detail_output(result)
    assert "安装完成" not in result.stdout
    events = (tmp_path / "events").read_text().splitlines()
    assert any(" -check-environment " in event for event in events)
    assert not any(" stop " in f" {event} " or " start " in f" {event} "
                   or "daemon-reload" in event or event.startswith("sysctl ") for event in events)
    for path, content in before.items():
        assert path.read_bytes() == content
    assert (tmp_path / "ip_forward").read_text() == "0\n"
    assert not (tmp_path / FORWARD_CONFIG).exists()
    assert not list((tmp_path / "agent").glob(".aio-install.*"))


def test_forwarding_summary_only_replaces_resolved_warning_and_preserves_log(tmp_path):
    resolved = "IPv4 转发：当前 net.ipv4.ip_forward=0；作为网关时需开启。\n"
    warnings = {
        "note.environment_1": resolved,
        "note.environment_2": "DNS 监听：端口已被其他服务占用。\n",
        "note.environment_3": "IPv4 转发：持久化文件被其他服务覆盖的风险仍存在。\n",
        "note.environment_4": "IPv4 转发：当前 net.ipv4.ip_forward=1；请核对启动顺序。\n",
        "note.environment_5": "转发检查历史：" + resolved,
        # A similarly worded note outside the environment namespace is not ours.
        "note.administrator": resolved,
        "note.forwarding": "IPv4 转发：当前为 0，持久化未验证。\n",
    }
    for name, content in warnings.items():
        (tmp_path / name).write_text(content)
    original_log = "检查时的原始状态\n" + "".join(warnings.values())
    (tmp_path / "install.log").write_text(original_log)
    source = with_install_output("#!/bin/sh\n" + "\n".join([
        "set -eu",
        shell_function("aio-ipv4-forwarding.sh", "summarize_ipv4_forwarding"),
        'for note in "$1"/note.*; do cp "$note" "$CF_OUTPUT_DIR/"; done',
        'cat "$1/install.log"',
        'cp "$CONFIGFLOW_LOG_FILE" "$1/log-before-summary"',
        "summarize_ipv4_forwarding",
        'cp "$CONFIGFLOW_LOG_FILE" "$1/log-after-summary"',
    ]))
    env = dict(os.environ, CONFIGFLOW_LOG_DIR=str(tmp_path / "logs"), CONFIGFLOW_VERBOSE="0")
    for key in ("CONFIGFLOW_CLI_ACTIVE", "CONFIGFLOW_LOG_FILE", "CONFIGFLOW_LOG_STATE_DIR",
                "CONFIGFLOW_LOG_CURSOR", "CONFIGFLOW_REPORT_FD", "CONFIGFLOW_REPORT_NESTED"):
        env.pop(key, None)
    result = subprocess.run(["/bin/sh", "-c", source, "summary-test", str(tmp_path)],
                            env=env, text=True, capture_output=True, timeout=10)

    assert result.returncode == 0, result.stdout + result.stderr
    logs = list((tmp_path / "logs").glob("configflow-install.*/install.log"))
    assert len(logs) == 1
    report_dir = logs[0].parent
    assert not (report_dir / "note.environment_1").exists()
    for name, content in warnings.items():
        if name not in ("note.environment_1", "note.forwarding"):
            assert (report_dir / name).read_text() == content
    summary = (report_dir / "note.forwarding").read_text()
    assert "已设置持久化" in summary
    assert "尚未实际重启验证" in summary
    assert "其他配置或服务仍可能在重启后把它改回 0" in summary
    assert "sysctl net.ipv4.ip_forward 确认仍为 1" in summary
    # The environment check no longer asks for a manual repair to look up.
    assert "原检查结果" not in summary
    assert "当前为 0" not in summary
    assert "已开启并持久化" in (report_dir / "fact.ipv4_forwarding").read_text()
    assert (tmp_path / "log-before-summary").read_bytes() == (tmp_path / "log-after-summary").read_bytes()
    assert original_log in logs[0].read_text()


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
def test_successful_aio_install_does_not_repeat_resolved_forwarding_warning(tmp_path, manager):
    installer, env, _ = harness(tmp_path, manager)
    agent = Path(env["FAKE_AGENT"])
    resolved = "[提醒] IPv4 转发：当前 net.ipv4.ip_forward=0；作为网关时需开启。"
    unrelated = "[提醒] DNS 检查：仍需核对客户端入口。"
    persistent_risk = "[提醒] IPv4 转发持久化：其他配置或服务可能覆盖。"
    source = agent.read_text()
    marker = "if '-check-environment' in args:\n"
    assert marker in source
    agent.write_text(source.replace(marker, marker + "".join(
        f"    print({message!r})\n" for message in (resolved, unrelated, persistent_risk)), 1))
    result = run(installer, env)

    assert result.returncode == 0, result.stdout + result.stderr
    output = result.stdout + result.stderr
    assert (tmp_path / "ip_forward").read_text() == "1\n"
    assert "IPv4 转发已开启并持久化" in output
    assert "尚未实际重启验证" in output
    assert "其他配置或服务仍可能在重启后把它改回 0" in output
    assert "当前 net.ipv4.ip_forward=0" not in output
    assert "DNS 检查：仍需核对客户端入口。" in output
    log = detail_output(result)
    for message in (resolved, unrelated, persistent_risk):
        assert message in log


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
def test_core_tun_permissions_guidance_matches_actual_service_manager(manager):
    source = "\n".join([
        "set -eu",
        'cf_fact() { printf "fact:%s:%s\\n" "$1" "$2"; }',
        'cf_note() { printf "note:%s:%s\\n" "$1" "$2"; }',
        shell_function("install-cores.sh", "show_tun_disabled_guidance"),
        'INIT_SYSTEM="$1"; TUN_REASON="/dev/net/tun 未提供。"',
        "show_tun_disabled_guidance",
    ])
    result = subprocess.run(["/bin/sh", "-c", source, "guidance-test", manager],
                            text=True, capture_output=True, timeout=10)

    assert result.returncode == 0, result.stderr
    assert "modprobe tun" in result.stdout
    assert "PVE 宿主机" in result.stdout
    assert "CAP_NET_ADMIN" in result.stdout and "CapEff" in result.stdout
    assert "fact:tun:TUN：已关闭" in result.stdout
    assert "note:tun:TUN 未启用：/dev/net/tun 未提供。" in result.stdout
    if manager == "openrc":
        assert "权限（OpenRC）" in result.stdout
        assert "/etc/init.d/mihomo" in result.stdout
        assert "/etc/conf.d/mihomo" in result.stdout
        assert "command_user" in result.stdout and "capabilities" in result.stdout
        assert "root 身份不能证明具备该能力" in result.stdout
        for option in ("systemd", "CapabilityBoundingSet", "DeviceAllow", "AmbientCapabilities"):
            assert option not in result.stdout
    else:
        assert "权限（systemd）" in result.stdout
        for option in ("CapabilityBoundingSet", "DeviceAllow=/dev/net/tun rw", "AmbientCapabilities"):
            assert option in result.stdout
        assert "OpenRC" not in result.stdout
        assert "command_user" not in result.stdout


@pytest.mark.parametrize("case", ["added", "failed", "unavailable", "enabled", "systemd"])
def test_forwarding_persistence_makes_openrc_apply_sysctl_at_boot(tmp_path, case):
    """PVE's Alpine LXC template has the OpenRC sysctl service in no runlevel, so
    sysctl.d is never applied at boot; the installer adds it to the boot runlevel,
    or notes the exact manual step when it cannot."""
    installer, env, units = harness(tmp_path, "systemd" if case == "systemd" else "openrc")
    runlevels = tmp_path / "etc/runlevels"
    script = tmp_path / "etc/init.d/sysctl"
    if case != "unavailable":
        script.parent.mkdir(parents=True, exist_ok=True)
        script.write_text("#!/sbin/openrc-run\n")
    if case == "enabled":
        (runlevels / "boot").mkdir(parents=True)
        (runlevels / "boot/sysctl").symlink_to(script)
    rc_update = tmp_path / "commands/rc-update"
    if rc_update.exists():
        rc_update.write_text(
            '#!/bin/sh\nprintf \'%s %s\\n\' "${0##*/}" "$*" >> "$EVENTS"\n'
            'if [ "$*" = "add sysctl boot" ]; then\n'
            '    [ "${FAIL_RC_UPDATE:-}" != yes ] || exit 1\n'
            f'    mkdir -p "{runlevels}/boot" && ln -s "{script}" "{runlevels}/boot/sysctl"\n'
            'fi\n')
    if case == "failed":
        env["FAIL_RC_UPDATE"] = "yes"
    result = run(installer, env)

    # Forwarding itself is persisted either way; only the boot step differs.
    assert result.returncode == 0, result.stdout + result.stderr
    assert (tmp_path / "ip_forward").read_text() == "1\n"
    events = (tmp_path / "events").read_text().splitlines()
    added = [line for line in events if line == "rc-update add sysctl boot"]
    output = result.stdout + detail_output(result)
    assert len(added) == (1 if case in ("added", "failed") else 0), events
    if case == "added":
        assert (runlevels / "boot/sysctl").is_symlink()
        assert "已执行 rc-update add sysctl boot" in output
    elif case == "failed":
        assert "无法自动把 OpenRC 的 sysctl 服务加入开机运行级别" in output
        assert "请执行：rc-update add sysctl boot" in output
    elif case == "unavailable":
        assert "系统没有 OpenRC 的 sysctl 服务" in output
        assert "rc-update add local default" in output
        assert "/etc/local.d/configflow-ip-forward.start" in output
    else:
        assert "forwarding_boot" not in output and "OpenRC 的 sysctl 服务" not in output
