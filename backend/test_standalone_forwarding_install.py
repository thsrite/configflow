"""Single-service Mihomo installs enable and persist IPv4 forwarding like the Shell AIO installer.

The script runs against isolated files with mocked services, sysctl and ip_forward.
"""
import hashlib
import json
import os
import re
from pathlib import Path
import subprocess
import sys

import pytest

from backend.agents.go_install_script import (
    AIO_FORWARDING_HOOK_COMMENT, INSTALL_GO_AIO_FORWARDING, STANDALONE_FORWARDING_HOOK,
    STANDALONE_FORWARDING_HOOK_COMMENT, generate_go_agent_install_script,
)
from backend.test_agent_install_commands import admin, client, make  # noqa: F401
from backend.test_router_split_regression import baseline, single_service_script_digests, without_reviewed_installer_deltas
from backend.test_single_agent_core_install import FAKE_CORE

GO_UNIT_TESTDATA = Path(__file__).resolve().parent / "agents/go-agent/routes/testdata"
FORWARD_CONTENT = "# Managed by ConfigFlow Shell AIO IPv4 forwarding v1\nnet.ipv4.ip_forward=1\n"
AIO_HOOK_SCRIPT = INSTALL_GO_AIO_FORWARDING.split("<<'CONFIGFLOW_AIO_FORWARDING_EOF'\n", 1)[1].split(
    "\nCONFIGFLOW_AIO_FORWARDING_EOF\n", 1)[0] + "\n"
# The single-service hook is the AIO hook whose comment names its one Agent.
HOOK_SCRIPT = AIO_HOOK_SCRIPT.replace(AIO_FORWARDING_HOOK_COMMENT, STANDALONE_FORWARDING_HOOK_COMMENT)
GO_FORWARDING = Path(__file__).resolve().parent / "agents/go-agent/routes/aio_forwarding.go"


def generated(service="mihomo", **options):
    return generate_go_agent_install_script(
        "https://config.test", "single", service, agent_ip="192.0.2.20",
        binary_download_url="https://config.test/api/agents/download", **options)


# -- Generator options -------------------------------------------------------

def test_opt_out_is_byte_identical_to_the_previous_release():
    expected = baseline()["single_service_script_sha256"]
    for install_core in (False, True):
        script = generate_go_agent_install_script(
            'https://config.test', 'single', 'mihomo', agent_ip='192.0.2.20',
            binary_download_url='https://config.test/api/agents/download', install_core=install_core,
            manage_ipv4_forwarding=False)
        assert hashlib.sha256(without_reviewed_installer_deltas(script, core_path=install_core).encode()).hexdigest() == expected[f"mihomo:{install_core}"]
    # The default stays the opt-out; the panel's API asks for the automatic choice.
    assert single_service_script_digests() == expected


@pytest.mark.parametrize("service,cores", [("oxidns", (False, True)), ("mosdns", (False,))])
def test_dns_services_never_take_forwarding(service, cores):
    expected = baseline()["single_service_script_sha256"]
    for install_core in cores:
        for value in (None, False):
            script = generate_go_agent_install_script(
                'https://config.test', 'single', service, agent_ip='192.0.2.20',
                binary_download_url='https://config.test/api/agents/download', install_core=install_core,
                manage_ipv4_forwarding=value)
            assert hashlib.sha256(without_reviewed_installer_deltas(script, core_path=install_core).encode()).hexdigest() == expected[f"{service}:{install_core}"]


@pytest.mark.parametrize("install_core", [False, True])
def test_automatic_choice_enables_forwarding_for_mihomo(install_core):
    for value in (None, True):
        script = generated(install_core=install_core, manage_ipv4_forwarding=value)
        assert "persist_ipv4_forwarding\n" in script
        assert hashlib.sha256(without_reviewed_installer_deltas(script, core_path=install_core).encode()).hexdigest() != \
            baseline()["single_service_script_sha256"][f"mihomo:{install_core}"]


@pytest.mark.parametrize("value", [1, 0, "true", "false", "yes"])
def test_forwarding_option_must_be_a_boolean(value):
    with pytest.raises(ValueError, match="manage_ipv4_forwarding"):
        generated(manage_ipv4_forwarding=value)


CUSTOM_LIFECYCLE = {"restart_command": "/custom/restart", "stop_command": "/custom/stop",
                    "start_command": "/custom/start", "status_command": "/custom/status"}


@pytest.mark.parametrize("service,options", [
    ("oxidns", {}), ("mosdns", {}), ("mihomo", CUSTOM_LIFECYCLE),
    ("mihomo", {"service_manager": "command", "stop_command": "/s", "start_command": "/t", "status_command": "/u"}),
])
def test_forwarding_needs_mihomo_under_systemd_or_openrc(service, options):
    with pytest.raises(ValueError, match="仅使用 systemd 或 OpenRC 的 Mihomo 单独安装"):
        generated(service, manage_ipv4_forwarding=True, **options)
    assert "persist_ipv4_forwarding" not in generated(service, manage_ipv4_forwarding=None, **options)


@pytest.mark.parametrize("options", [{"service_manager": "systemd"}, {"service_manager": "openrc"},
                                     {"restart_command": "rc-service mihomo restart"}])
def test_explicit_standard_managers_keep_forwarding(options):
    assert "persist_ipv4_forwarding\n" in generated(manage_ipv4_forwarding=None, **options)


# -- Script structure -------------------------------------------------------

def service_heredoc(script, manager):
    start = {"systemd": "cat > /etc/systemd/system/configflow-agent.service <<'SERVICEEOF'\n",
             "openrc": "cat > /etc/init.d/configflow-agent <<'SERVICEEOF'\n"}[manager]
    assert script.count(start) == 1
    return script.split(start, 1)[1].split("\nSERVICEEOF\n", 1)[0] + "\n"


@pytest.mark.parametrize("install_core", [False, True])
def test_hook_is_registered_where_the_agent_looks_for_it(tmp_path, install_core):
    script = generated(install_core=install_core, manage_ipv4_forwarding=None)
    systemd = service_heredoc(script, "systemd").splitlines()
    pre = [index for index, line in enumerate(systemd) if line.startswith("ExecStartPre=")]
    assert len(pre) == 1 and systemd[pre[0]] == f"ExecStartPre=/bin/sh {STANDALONE_FORWARDING_HOOK}"
    assert systemd[pre[0] + 1].startswith("ExecStart=") and systemd[pre[0] - 1] == "WorkingDirectory=/opt/configflow-agent"
    openrc = service_heredoc(script, "openrc").splitlines()
    first = openrc.index("start_pre() {") + 1
    assert openrc[first] == f"    /bin/sh {STANDALONE_FORWARDING_HOOK} || return 1"
    assert sum("enable-ipv4-forwarding.sh" in line for line in openrc) == 1
    assert "$AGENT_ROOT" not in script
    # Before stopping the previous Agent; committed only after the new one started.
    assert script.index("prepare_ipv4_forwarding\n") < script.index("systemctl stop configflow-agent 2>/dev/null || true\n", script.index('cf_step "安装并启动 Agent 服务"'))
    assert script.index("install_ipv4_forwarding_hook\n") < script.index("cat > /etc/systemd/system/configflow-agent.service")
    assert script.index("rc-service configflow-agent status\n") < script.index("persist_ipv4_forwarding\n")
    assert script.count('cleanup_ipv4_forwarding "$agent_status"') == 1
    path = tmp_path / "install.sh"
    path.write_text(script)
    result = subprocess.run(["sh", "-n", str(path)], text=True, capture_output=True)
    assert result.returncode == 0, result.stderr


@pytest.mark.parametrize("manager,suffix", [("systemd", "service"), ("openrc", "initd")])
@pytest.mark.parametrize("variant", ["plain", "hook"])
def test_go_agent_unit_fixtures_match_the_single_service_installer(manager, suffix, variant):
    # The Go Agent registers the hook in an installed unit; its fixtures must
    # stay identical to what this installer writes, with and without the hook.
    script = generated(manage_ipv4_forwarding=None if variant == "hook" else False)
    fixture = GO_UNIT_TESTDATA / f"standalone-unit-{manager}-{variant}.{suffix}"
    assert fixture.read_text() == service_heredoc(script, manager)


def test_uses_the_aio_hook_with_a_single_agent_comment():
    script = generated(manage_ipv4_forwarding=None)
    assert HOOK_SCRIPT != AIO_HOOK_SCRIPT and AIO_HOOK_SCRIPT.count(AIO_FORWARDING_HOOK_COMMENT) == 1
    assert script.count(HOOK_SCRIPT) == 1
    assert "both Agents" not in script and AIO_FORWARDING_HOOK_COMMENT not in script
    assert 'cp "$STAGE_DIR/enable-ipv4-forwarding.sh" "$AGENT_DIR/enable-ipv4-forwarding.sh"' in script
    assert '/bin/sh "$AGENT_DIR/enable-ipv4-forwarding.sh" || ipv4_failure' in script


def test_go_agent_writes_and_recognises_the_same_hook_comments():
    # The Agent rewrites the hook when forwarding is persisted from the panel and
    # reports any other text as modified, so both comments must match exactly.
    go = GO_FORWARDING.read_text(encoding="utf-8")
    for name, comment in (("aioHookComment", AIO_FORWARDING_HOOK_COMMENT),
                          ("aioStandaloneHookComment", STANDALONE_FORWARDING_HOOK_COMMENT)):
        assert re.search(rf'^\s*{name}\s*= {re.escape(json.dumps(comment, ensure_ascii=False))}$', go, re.M), name


# -- Running the installer --------------------------------------------------

def harness(tmp_path, monkeypatch, manager="systemd", install_core=False, **options):
    if install_core:
        monkeypatch.setattr("backend.agents.core_install_script.generate_core_install_script", lambda **_: FAKE_CORE)
    options.setdefault("manage_ipv4_forwarding", None)
    source = generated(install_core=install_core, **options)
    root = tmp_path / "root"
    for relative in ("etc/systemd/system", "etc/init.d", "usr/local/bin", "sbin", "run", "var/log"):
        (root / relative).mkdir(parents=True)
    if manager == "systemd":
        (root / "run/systemd/system").mkdir(parents=True)
    else:
        (root / "sbin/openrc-run").write_text("#!/bin/sh\n")
        (root / "sbin/openrc-run").chmod(0o755)
    for old, new in (
        ("/proc/sys/net/ipv4/ip_forward", str(tmp_path / "ip_forward")),
        ("/usr/local/", str(root / "usr/local") + "/"),
        ("/etc/", str(root / "etc") + "/"),
        ("/run/", str(root / "run") + "/"),
        ("/sbin/openrc-run", str(root / "sbin/openrc-run")),
        ("/opt/configflow-agent", str(root / "opt/configflow-agent")),
        ("/var/log", str(root / "var/log")),
        ("/dev/tty", "/dev/stdin"),
    ):
        source = source.replace(old, new)
    source = source.replace("in /run /etc /usr/local ", f"in {root}/run {root}/etc {root}/usr/local ")
    if sys.platform == "darwin":
        source = source.replace("sed -i ", "sed -i '' ")
    script = tmp_path / "install.sh"
    script.write_text(source)
    (tmp_path / "ip_forward").write_text("0\n")
    core_config = root / "etc/mihomo/config.yaml"
    core_config.parent.mkdir()
    core_config.write_text("# existing core config\n")
    (root / "usr/local/bin/mihomo").write_text("#!/bin/sh\nexit 0\n")
    (root / "usr/local/bin/mihomo").chmod(0o755)
    core_unit = root / ("etc/systemd/system/mihomo.service" if manager == "systemd" else "etc/init.d/mihomo")
    core_unit.write_text("# existing core service\n")
    core_unit.chmod(0o755)
    unit = root / ("etc/systemd/system/configflow-agent.service" if manager == "systemd" else "etc/init.d/configflow-agent")
    commands = tmp_path / "commands"
    commands.mkdir()
    agent = tmp_path / "fake-agent"
    agent.write_text('#!/bin/sh\nprintf \'agent %s\\n\' "$*" >> "$EVENTS"\nexit 0\n')
    agent.chmod(0o755)
    # Starting the Agent runs its registered start hook, as systemd/OpenRC would.
    manager_mock = '''printf '%s %s\\n' "${0##*/}" "$*" >> "$EVENTS"
case "$*" in *--property=LoadState*) printf 'loaded\\n' ;; esac
case "${0##*/}:$*" in
    "systemctl:start configflow-agent"|"rc-service:configflow-agent start")
        [ "${FAIL_AGENT_START:-}" != yes ] || exit 49
        hook=$(sed -n 's#^.*/bin/sh \\([^ ]*enable-ipv4-forwarding\\.sh\\).*$#\\1#p' "$AGENT_UNIT")
        if [ -n "$hook" ]; then /bin/sh "$hook"; fi
        ;;
esac
'''
    sysctl_mock = f'''#!{sys.executable} -S
import os, sys
from pathlib import Path
args = sys.argv[1:]
with open(os.environ['EVENTS'], 'a') as log:
    log.write('sysctl ' + ' '.join(args) + '\\n')
if args not in (['-q', '-w', 'net.ipv4.ip_forward=0'], ['-q', '-w', 'net.ipv4.ip_forward=1']):
    raise SystemExit('Unexpected sysctl command in isolated installer test: ' + repr(args))
if os.environ.get('FAIL_SYSCTL_WRITES') == 'yes':
    print('sysctl: permission denied on key net.ipv4.ip_forward', file=sys.stderr)
    raise SystemExit(50)
Path(os.environ['FORWARD_PROC']).write_text(args[-1].rsplit('=', 1)[1] + '\\n')
'''
    mocks = {"id": "printf '0\\n'\n", "uname": 'case "${1:-}" in -s) echo Linux ;; *) echo x86_64 ;; esac\n',
             "wget": 'cp "$FAKE_AGENT" "$3"\n', "sysctl": sysctl_mock}
    if manager == "systemd":
        mocks["systemctl"] = manager_mock
    else:
        mocks["rc-service"] = manager_mock
        mocks["rc-update"] = manager_mock
    for name, body in mocks.items():
        command = commands / name
        command.write_text(body if body.startswith("#!") else "#!/bin/sh\nset -eu\n" + body)
        command.chmod(0o755)
    events = tmp_path / "events"
    events.touch()
    env = dict(os.environ, PATH=f"{commands}:{root}/usr/local/bin:" + os.environ["PATH"], EVENTS=str(events),
               FAKE_AGENT=str(agent), FORWARD_PROC=str(tmp_path / "ip_forward"), AGENT_UNIT=str(unit),
               TEST_CORE="mihomo", CORE_COMPLETED=str(tmp_path / "core-completed"),
               CONFIGFLOW_INSTALL_LOG_DIR=str(tmp_path))
    paths = {"root": root, "conf": root / "etc/sysctl.d/99-configflow-aio-forward.conf",
             "hook": root / "opt/configflow-agent/enable-ipv4-forwarding.sh", "unit": unit,
             "agent_dir": root / "opt/configflow-agent", "proc": tmp_path / "ip_forward", "events": events}
    return script, env, paths


def run(script, env, choice="1"):
    return subprocess.run(["/bin/sh", str(script)], input=choice + "\n", env=env, text=True,
                          capture_output=True, timeout=60)


def sysctl_writes(paths):
    return [line for line in paths["events"].read_text().splitlines() if line.startswith("sysctl ")]


def expected_hook(paths, tmp_path):
    return (HOOK_SCRIPT.replace("/proc/sys/net/ipv4/ip_forward", str(tmp_path / "ip_forward"))
            .replace("/etc/", str(paths["root"] / "etc") + "/"))


def leftovers(paths):
    sysctl = paths["root"] / "etc/sysctl.d"
    return (list(paths["agent_dir"].glob(".ipv4-forward.*"))
            + (list(sysctl.glob(".configflow-aio-forward.*")) if sysctl.exists() else []))


def stopped(paths):
    return any(" stop" in line and "configflow-agent" in line for line in paths["events"].read_text().splitlines())


def assert_persisted(paths, tmp_path, manager):
    assert paths["conf"].read_text() == FORWARD_CONTENT
    assert paths["hook"].stat().st_mode & 0o777 == 0o755
    assert paths["hook"].read_text() == expected_hook(paths, tmp_path)
    lines = paths["unit"].read_text().splitlines()
    hook_line = f"/bin/sh {paths['hook']}"
    if manager == "systemd":
        index = lines.index("ExecStartPre=" + hook_line)
        assert lines[index + 1].startswith("ExecStart=")
        assert sum(line.startswith("ExecStartPre=") for line in lines) == 1
    else:
        assert lines[lines.index("start_pre() {") + 1] == f"    {hook_line} || return 1"
    assert paths["proc"].read_text() == "1\n"
    assert not leftovers(paths)


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
@pytest.mark.parametrize("initial", ["0", "1"])
@pytest.mark.parametrize("install_core", [False, True])
def test_install_persists_forwarding_and_is_repeatable(tmp_path, monkeypatch, manager, initial, install_core):
    script, env, paths = harness(tmp_path, monkeypatch, manager, install_core)
    paths["proc"].write_text(initial + "\n")
    for attempt in range(2):
        paths["events"].write_text("")
        result = run(script, env)
        assert result.returncode == 0, result.stdout + result.stderr
        assert_persisted(paths, tmp_path, manager)
        writes = sysctl_writes(paths)
        # The permission probe rewrites the value read; only 0 -> 1 changes it.
        assert all(line in ("sysctl -q -w net.ipv4.ip_forward=0", "sysctl -q -w net.ipv4.ip_forward=1")
                   for line in writes)
        if initial == "1" or attempt:
            assert "sysctl -q -w net.ipv4.ip_forward=0" not in writes
        assert "IPv4 转发已开启并持久化" in result.stdout


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
def test_opt_out_leaves_forwarding_alone(tmp_path, monkeypatch, manager):
    script, env, paths = harness(tmp_path, monkeypatch, manager, manage_ipv4_forwarding=False)
    result = run(script, env)
    assert result.returncode == 0, result.stdout + result.stderr
    assert not paths["conf"].exists() and not paths["hook"].exists()
    assert "enable-ipv4-forwarding" not in paths["unit"].read_text()
    assert paths["proc"].read_text() == "0\n"
    assert sysctl_writes(paths) == []


def install_once(tmp_path, monkeypatch, manager):
    script, env, paths = harness(tmp_path, monkeypatch, manager)
    result = run(script, env)
    assert result.returncode == 0, result.stdout + result.stderr
    return script, env, paths


def snapshot(paths):
    # The candidate config-*.json.new is staged before these checks, as before.
    return {path: path.read_bytes() for path in paths["root"].rglob("*")
            if path.is_file() and not path.is_symlink() and not path.name.endswith(".json.new")}


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
def test_gateway_record_refuses_before_the_agent_is_stopped(tmp_path, monkeypatch, manager):
    script, env, paths = harness(tmp_path, monkeypatch, manager)
    # A previous single-service Agent that applied a selective Linux gateway.
    paths["agent_dir"].mkdir(parents=True)
    (paths["agent_dir"] / "config-mihomo.json").write_text(json.dumps(
        {"agent_name": "single", "service_type": "mihomo", "agent_id": "id", "token": "token"}))
    (paths["agent_dir"] / "linux-gateway.json").write_text('{"owner":"gateway"}\n')
    paths["unit"].write_text("# previous Agent unit\n")
    before = snapshot(paths)
    result = run(script, env)
    assert result.returncode != 0
    assert "不开启 IPv4 转发" in result.stderr and "Agent 页这台设备的卡片上" in result.stderr
    assert not stopped(paths)
    assert snapshot(paths) == before
    assert not paths["conf"].exists() and paths["proc"].read_text() == "0\n"
    assert sysctl_writes(paths) == []
    assert not leftovers(paths)
    # Opting out installs as before and leaves the gateway's forwarding to it.
    script, env, paths = harness(tmp_path / "opt-out", monkeypatch, manager, manage_ipv4_forwarding=False)
    paths["agent_dir"].mkdir(parents=True)
    (paths["agent_dir"] / "linux-gateway.json").write_text('{"owner":"gateway"}\n')
    result = run(script, env)
    assert result.returncode == 0, result.stdout + result.stderr
    assert not paths["conf"].exists() and sysctl_writes(paths) == []


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
@pytest.mark.parametrize("problem", ["foreign_config", "symlink_config", "symlink_hook", "sysctl_denied"])
def test_unsafe_host_state_fails_before_the_agent_is_stopped(tmp_path, monkeypatch, manager, problem):
    script, env, paths = harness(tmp_path, monkeypatch, manager)
    sysctl = paths["root"] / "etc/sysctl.d"
    sysctl.mkdir(parents=True)
    paths["agent_dir"].mkdir(parents=True)
    if problem == "foreign_config":
        paths["conf"].write_text("net.ipv4.ip_forward=1\n")
    elif problem == "symlink_config":
        (tmp_path / "elsewhere.conf").write_text(FORWARD_CONTENT)
        paths["conf"].symlink_to(tmp_path / "elsewhere.conf")
    elif problem == "symlink_hook":
        (tmp_path / "elsewhere.sh").write_text("exit 0\n")
        paths["hook"].symlink_to(tmp_path / "elsewhere.sh")
    else:
        env["FAIL_SYSCTL_WRITES"] = "yes"
    before = snapshot(paths)
    links = {path: os.readlink(path) for path in paths["root"].rglob("*") if path.is_symlink()}
    result = run(script, env)
    assert result.returncode != 0, result.stdout
    assert not stopped(paths)
    assert snapshot(paths) == before
    assert {path: os.readlink(path) for path in paths["root"].rglob("*") if path.is_symlink()} == links
    assert paths["proc"].read_text() == "0\n"
    assert "sysctl -q -w net.ipv4.ip_forward=1" not in sysctl_writes(paths)
    assert not leftovers(paths)
    if problem == "sysctl_denied":
        assert "CAP_NET_ADMIN" in result.stderr


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
def test_failed_agent_start_commits_nothing_and_a_rerun_converges(tmp_path, monkeypatch, manager):
    script, env, paths = harness(tmp_path, monkeypatch, manager)
    result = run(script, {**env, "FAIL_AGENT_START": "yes"})
    assert result.returncode != 0
    assert not paths["conf"].exists()
    assert paths["proc"].read_text() == "0\n"
    assert not leftovers(paths)
    result = run(script, env)
    assert result.returncode == 0, result.stdout + result.stderr
    assert_persisted(paths, tmp_path, manager)


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
def test_uninstall_keeps_the_persisted_forwarding(tmp_path, monkeypatch, manager):
    script, env, paths = install_once(tmp_path, monkeypatch, manager)
    result = run(script, env, "2")
    assert result.returncode == 0, result.stdout + result.stderr
    assert "IPv4 转发及其 sysctl.d 持久化设置保留" in result.stdout
    assert paths["conf"].read_text() == FORWARD_CONTENT
    assert not paths["hook"].exists() and not paths["unit"].exists()
    assert paths["proc"].read_text() == "1\n"


# -- Install-command API ----------------------------------------------------

def test_api_enables_forwarding_for_mihomo_by_default(client, admin):
    assert "persist_ipv4_forwarding\n" in make(client, admin)["script"]
    assert "persist_ipv4_forwarding" not in make(client, admin, ipv4_forwarding=False)["script"]
    assert "persist_ipv4_forwarding\n" in make(client, admin, ipv4_forwarding=True)["script"]
    assert "persist_ipv4_forwarding" not in make(client, admin, type="oxidns")["script"]


def test_api_opt_out_matches_the_generator_opt_out(client, admin):
    script = make(client, admin, ipv4_forwarding=False, agent_ip="192.0.2.20")["script"]
    assert script == generate_go_agent_install_script(
        "https://config.test/panel", "gateway", "mihomo", agent_ip="192.0.2.20",
        config_path="/etc/mihomo/config.yaml", restart_command="systemctl restart mihomo",
        binary_download_url="https://config.test/panel/api/agents/download")


def test_api_custom_lifecycle_without_the_field_skips_forwarding(client, admin):
    script = make(client, admin, restart_command="/custom/restart", stop_command="/custom/stop",
                  start_command="/custom/start", status_command="/custom/status")["script"]
    assert "persist_ipv4_forwarding" not in script and "ExecStartPre=" not in script


@pytest.mark.parametrize("params,message", [
    ({"ipv4_forwarding": "false"}, "ipv4_forwarding 必须为 true 或 false"),
    ({"ipv4_forwarding": 1}, "ipv4_forwarding 必须为 true 或 false"),
    ({"ipv4_forwarding": False, "type": "oxidns"}, "仅 Mihomo 单独安装"),
    ({"ipv4_forwarding": False, "type": "aio"}, "仅 Mihomo 单独安装"),
    ({"ipv4_forwarding": False, "agent_type": "shell"}, "旧版 Shell Agent"),
    ({"ipv4_forwarding": True, "restart_command": "/custom/restart", "stop_command": "/s",
      "start_command": "/t", "status_command": "/u"}, "仅使用 systemd 或 OpenRC"),
])
def test_api_rejects_invalid_forwarding_requests(client, admin, params, message):
    response = client.post('/api/agents/install-command', headers=admin, json={
        'name': 'gateway', 'type': 'mihomo', 'server_url': 'https://config.test/panel', **params})
    assert response.status_code == 400
    assert message in response.get_json()["message"]


def test_install_script_query_takes_the_opt_out(client):
    query = {"type": "mihomo", "name": "single", "server_url": "https://config.test"}
    enabled = client.get("/api/agents/install-script", query_string=query).get_data(as_text=True)
    disabled = client.get("/api/agents/install-script", query_string={**query, "ipv4_forwarding": "false"})
    assert "persist_ipv4_forwarding\n" in enabled
    assert disabled.status_code == 200 and "persist_ipv4_forwarding" not in disabled.get_data(as_text=True)
    assert client.get("/api/agents/install-script", query_string={**query, "ipv4_forwarding": "no"}).status_code == 400
