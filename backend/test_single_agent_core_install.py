"""Single Agent + core opt-in validation and isolated phase sequencing."""
import json
import os
from pathlib import Path
import subprocess
import sys

from flask import Flask
import pytest

from backend.agents.core_install_script import generate_core_install_script
from backend.agents.go_install_script import AGENT_CORE_INSTALL_INTERFACE, generate_go_agent_install_script
from backend.routes.agents import get_install_script


def generated(service="mihomo", **options):
    return generate_go_agent_install_script(
        "https://config.test", "single", service,
        binary_download_url="https://config.test/api/agents/download", **options)


@pytest.fixture
def client():
    app = Flask(__name__)
    app.add_url_rule("/api/agents/install-script", view_func=get_install_script)
    return app.test_client()


@pytest.mark.parametrize("service", ["mihomo", "oxidns"])
def test_combined_api_reuses_selected_canonical_installer_and_has_valid_shell(client, tmp_path, service):
    response = client.get("/api/agents/install-script", query_string={
        "type": service, "name": "single", "install_core": "true"})
    assert response.status_code == 200
    source = response.get_data(as_text=True)
    assert source.splitlines()[1] == AGENT_CORE_INSTALL_INTERFACE
    # Mihomo persists IPv4 forwarding by default, so its core phase says so.
    assert generate_core_install_script(installer_forwarding=service == "mihomo").rstrip() in source
    assert ("旁路网关修复：" in source) is (service == "oxidns")
    assert f"(\nset -- --core {service}\n" in source
    assert source.index('case "$choice"') < source.index('case "$(uname -m)"') < source.index(f"set -- --core {service}")
    assert source.index(f"set -- --core {service}") < source.index('cf_step "下载并校验 Agent"')
    script = tmp_path / "install.sh"
    script.write_text(source)
    result = subprocess.run(["sh", "-n", str(script)], capture_output=True, text=True)
    assert result.returncode == 0, result.stderr


@pytest.mark.parametrize("service", ["mihomo", "oxidns", "mosdns", "aio"])
def test_omitted_and_false_preserve_agent_only_api(client, service):
    base = {"type": service, "name": "single"}
    omitted = client.get("/api/agents/install-script", query_string=base)
    explicit = client.get("/api/agents/install-script", query_string={**base, "install_core": "false"})
    assert omitted.status_code == explicit.status_code == 200
    assert omitted.data == explicit.data
    assert AGENT_CORE_INSTALL_INTERFACE.encode() not in explicit.data
    assert b"set -- --core" not in explicit.data


@pytest.mark.parametrize("options", [
    {"install_core": "1"}, {"install_core": "yes"}, {"install_core": "TRUE"}, {"install_core": ""},
    {"type": "mosdns"}, {"agent_type": "shell"}, {"agent_type": "unknown"},
    {"config_path": "/srv/mihomo/config.yaml"}, {"service_unit": "custom"},
    {"service_binary": "/usr/bin/custom"}, {"service_manager": "systemd"},
    {"restart_command": "/custom/restart"}, {"stop_command": "/custom/stop"},
    {"start_command": "/custom/start"}, {"status_command": "/custom/status"},
    {"port": "1053"}, {"type": "oxidns", "port": "53"}, {"port": "wrong"},
    {"port": "8080.1"}, {"port": "0"}, {"port": "65536"},
])
def test_api_rejects_unsupported_combined_settings_before_loading_core(client, monkeypatch, options):
    def unexpected(**_):
        pytest.fail("Invalid settings must be rejected before loading the core installer")
    monkeypatch.setattr("backend.agents.core_install_script.generate_core_install_script", unexpected)
    response = client.get("/api/agents/install-script", query_string={
        "type": "mihomo", "name": "single", "install_core": "true", **options})
    assert response.status_code == 400, response.get_data(as_text=True)
    assert response.json["success"] is False


@pytest.mark.parametrize("service", ["mihomo", "oxidns"])
def test_default_layout_aliases_are_accepted(service):
    source = generated(service, install_core=True, config_path=f"/etc/{service}/config.yaml",
                       service_unit=f"{service}.service", service_binary=f"/usr/local/bin/{service}",
                       restart_command=f"rc-service {service} restart")
    assert source.splitlines()[1] == AGENT_CORE_INSTALL_INTERFACE
    with pytest.raises(ValueError, match="true 或 false"):
        generated(service, install_core="false")


FAKE_CORE = '''#!/bin/sh
set -eu
printf 'core %s\\n' "$*" >> "$EVENTS"
[ "$1" = --core ]
[ "$2" = "$TEST_CORE" ]
if [ "${FAIL_CORE:-}" = yes ]; then false; fi
printf 'complete\\n' > "$CORE_COMPLETED"
# The Agent must not inherit core variables, traps or an explicit exit.
AGENT_DIR=/must-not-leak
INIT_SYSTEM=must-not-leak
trap 'printf "core cleanup\\n" >> "$EVENTS"' 0
exit 0
'''


def harness(tmp_path, monkeypatch, service="mihomo", manager="systemd", real_core=False, install_core=True, **settings):
    if not real_core:
        monkeypatch.setattr("backend.agents.core_install_script.generate_core_install_script", lambda **_: FAKE_CORE)
    source = generated(service, install_core=install_core)
    root = tmp_path / "root"
    for relative in ("etc/systemd/system", "etc/init.d", "usr/local/bin", "sbin", "run", "opt", "var/log"):
        (root / relative).mkdir(parents=True)
    if manager == "systemd":
        (root / "run/systemd/system").mkdir(parents=True)
    else:
        init = root / "sbin/openrc-run"
        init.write_text("#!/bin/sh\n")
        init.chmod(0o755)
    for old, new in (
        ("/usr/local/", str(root / "usr/local") + "/"),
        ("/usr/lib/", str(root / "usr/lib") + "/"),
        ("/lib/systemd/", str(root / "lib/systemd") + "/"),
        ("/etc/", str(root / "etc") + "/"),
        ("/run/", str(root / "run") + "/"),
        ("/sbin/openrc-run", str(root / "sbin/openrc-run")),
        ("/opt/configflow-agent", str(root / "opt/configflow-agent")),
        ("/var/log", str(root / "var/log")),
        ("/dev/tty", "/dev/stdin"),
        # A supervised OpenRC Agent is confirmed through its child process.
        ("/proc/[0-9]*/status", str(tmp_path / "agent-process")),
    ):
        source = source.replace(old, new)
    (tmp_path / "agent-process").write_text("PPid:\tmissing\n")
    # The core's directory checks also contain standalone /run and /etc names.
    source = source.replace("in /run /etc /usr/local ", f"in {root}/run {root}/etc {root}/usr/local ")
    if sys.platform == "darwin":
        source = source.replace("sed -i ", "sed -i '' ")
    script = tmp_path / "install.sh"
    script.write_text(source)
    config = root / f"etc/{service}/config.yaml"
    config.parent.mkdir()
    config.write_text("# existing core config\n")
    binary = root / f"usr/local/bin/{service}"
    binary.write_text("#!/bin/sh\nexit 0\n")
    binary.chmod(0o755)
    unit = root / (f"etc/systemd/system/{service}.service" if manager == "systemd" else f"etc/init.d/{service}")
    unit.write_text("# existing core service\n")
    unit.chmod(0o755)
    commands = tmp_path / "commands"
    commands.mkdir()
    fake_agent = tmp_path / "fake-agent"
    fake_agent.write_text(f'''#!{sys.executable}
import json, os, sys
args = sys.argv[1:]
with open(os.environ['EVENTS'], 'a') as log:
    log.write('agent ' + ' '.join(args) + '\\n')
if args in (['-h'], ['-version']):
    raise SystemExit(0)
cfg = json.load(open(args[args.index('-config') + 1]))
assert cfg['service_manager'] == os.environ['TEST_MANAGER']
if '-check-environment' in args and os.environ.get('VERBOSE_ENVIRONMENT') == 'yes':
    print('verbose-environment-detail-for-log-only')
if '-check-deployments' in args and os.environ.get('PENDING_DEPLOYMENT') == 'yes':
    print('2026/10/18 12:00:00 Deployment check requires attention: pending publication; inspect its journal', file=sys.stderr)
''')
    fake_agent.chmod(0o755)
    manager_body = '''printf '%s %s\\n' "$(basename "$0")" "$*" >> "$EVENTS"
case "$*" in *--property=LoadState*) printf '%s\\n' "${CORE_LOAD_STATE:-loaded}" ;; esac
'''
    mocks = {
        "id": "printf '0\\n'\n",
        "uname": 'case "$1" in -s) echo Linux ;; *) echo "${TEST_ARCH:-x86_64}" ;; esac\n',
        "wget": 'echo agent-download >> "$EVENTS"\n[ "${FAIL_AGENT_DOWNLOAD:-}" != yes ] || exit 47\ncp "$FAKE_AGENT" "$3"\n',
        "systemctl": manager_body, "rc-service": manager_body, "rc-update": manager_body,
    }
    if not install_core and manager == "systemd":
        # The legacy Agent-only detector chooses OpenRC when both are present.
        # Model the selected init instead of the combined-mode priority fixture.
        del mocks["rc-service"], mocks["rc-update"]
    for name, body in mocks.items():
        command = commands / name
        command.write_text("#!/bin/sh\nset -eu\n" + body)
        command.chmod(0o755)
    events = tmp_path / "events"
    events.touch()
    env = dict(os.environ, PATH=f"{commands}:{root}/usr/local/bin:" + os.environ["PATH"],
               EVENTS=str(events), FAKE_AGENT=str(fake_agent), TEST_CORE=service,
               TEST_MANAGER=manager, CORE_COMPLETED=str(tmp_path / "core-completed"),
               CONFIGFLOW_INSTALL_LOG_DIR=str(tmp_path), **settings)
    return script, env, root, events


def run(script, env, choice="1"):
    return subprocess.run(["/bin/sh", str(script)], input=choice + "\n", env=env,
                          text=True, capture_output=True)


def detail_output(result):
    path = next(line.removeprefix("完整日志：") for line in result.stdout.splitlines()
                if line.startswith("完整日志："))
    return Path(path).read_text()


@pytest.mark.parametrize("service", ["mihomo", "oxidns"])
@pytest.mark.parametrize("manager", ["systemd", "openrc"])
def test_core_completes_before_agent_with_isolated_state(tmp_path, monkeypatch, service, manager):
    script, env, root, events = harness(tmp_path, monkeypatch, service, manager)
    result = run(script, env)
    assert result.returncode == 0, result.stdout + result.stderr
    log = events.read_text()
    assert log.index(f"core --core {service}") < log.index("core cleanup") < log.index("agent-download")
    assert (root / f"opt/configflow-agent/config-{service}.json").is_file()
    assert not list((root / "usr/local/bin").glob(".configflow-agent.install.*"))
    # Both manager commands exist; a running systemd must win in combined mode.
    unexpected_manager = "rc-service" if manager == "systemd" else "systemctl"
    assert unexpected_manager not in log


@pytest.mark.parametrize("service,label", [("mihomo", "Mihomo"), ("oxidns", "OxiDNS"), ("mosdns", "MosDNS")])
def test_single_agent_only_prints_progress_and_service_summary(tmp_path, monkeypatch, service, label):
    script, env, root, events = harness(tmp_path, monkeypatch, service, install_core=False,
                                      VERBOSE_ENVIRONMENT="yes")
    result = run(script, env)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "请输入选项 [1-3]: " in result.stdout
    assert "检查安装环境与依赖" in result.stdout
    assert "下载并校验 Agent" in result.stdout
    assert "检查核心接管与运行环境" in result.stdout
    assert "安装并启动 Agent 服务" in result.stdout
    assert f"{label} Agent：端口 8080，服务已启动" in result.stdout
    assert "安装完成" in result.stdout + result.stderr and "完整日志：" in result.stdout
    assert "verbose-environment-detail-for-log-only" not in result.stdout + result.stderr
    assert "verbose-environment-detail-for-log-only" in detail_output(result)
    assert "core --core" not in events.read_text()


@pytest.mark.parametrize("existing", [False, True])
def test_single_agent_pending_publication_remains_visible_in_success_summary(tmp_path, monkeypatch, existing):
    script, env, _, events = harness(tmp_path, monkeypatch, install_core=False)
    if existing:
        installed = run(script, env)
        assert installed.returncode == 0, installed.stdout + installed.stderr
        events.write_text("")
    env["PENDING_DEPLOYMENT"] = "yes"
    result = run(script, env)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "安装完成" in result.stdout + result.stderr
    assert result.stderr.count("发布记录需要处理：pending publication; inspect its journal") == 1
    assert "Deployment check requires attention:" not in result.stdout + result.stderr
    details = detail_output(result)
    expected_checks = 2 if existing else 1
    assert details.count("Deployment check requires attention:") == expected_checks
    assert "inspect its journal" in details
    assert events.read_text().count("-check-deployments") == expected_checks


@pytest.mark.parametrize("choice", ["2", "3"])
def test_uninstall_or_cancel_never_runs_core(tmp_path, monkeypatch, choice):
    script, env, root, events = harness(tmp_path, monkeypatch)
    original = (root / "etc/mihomo/config.yaml").read_bytes()
    result = run(script, env, choice)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "core --core" not in events.read_text()
    assert "agent-download" not in events.read_text()
    assert (root / "etc/mihomo/config.yaml").read_bytes() == original


def test_core_failure_stops_before_agent_phase(tmp_path, monkeypatch):
    script, env, root, events = harness(tmp_path, monkeypatch, FAIL_CORE="yes")
    result = run(script, env)
    assert result.returncode != 0
    assert "core --core mihomo" in events.read_text()
    assert "agent-download" not in events.read_text()
    assert not Path(env["CORE_COMPLETED"]).exists()
    assert not (root / "opt/configflow-agent").exists()


def test_later_agent_failure_preserves_completed_core_and_reports_it(tmp_path, monkeypatch):
    script, env, root, events = harness(tmp_path, monkeypatch, FAIL_AGENT_DOWNLOAD="yes")
    result = run(script, env)
    assert result.returncode != 0
    assert Path(env["CORE_COMPLETED"]).read_text() == "complete\n"
    assert "核心予以保留" in result.stderr
    assert "Agent 二进制下载失败" in result.stderr
    assert not list((root / "usr/local/bin").glob(".configflow-agent.install.*"))
    assert "stop mihomo" not in events.read_text()


def test_unsupported_architecture_never_runs_core(tmp_path, monkeypatch):
    script, env, root, events = harness(tmp_path, monkeypatch, TEST_ARCH="unknown")
    result = run(script, env)
    assert result.returncode != 0
    assert events.read_text() == ""
    assert not (root / "opt/configflow-agent").exists()


@pytest.mark.parametrize("missing", ["config", "binary", "service"])
def test_incomplete_existing_core_stops_before_agent_files(tmp_path, monkeypatch, missing):
    script, env, root, events = harness(tmp_path, monkeypatch)
    if missing == "config":
        (root / "etc/mihomo/config.yaml").unlink()
    elif missing == "binary":
        (root / "usr/local/bin/mihomo").unlink()
    else:
        env["CORE_LOAD_STATE"] = "not-found"
    result = run(script, env)
    assert result.returncode != 0
    assert "取消同时安装核心" in result.stderr
    assert "agent-download" not in events.read_text()
    assert not (root / "opt/configflow-agent").exists()


def test_missing_agent_tool_has_repair_hint_and_cannot_delete_inherited_temp(tmp_path, monkeypatch):
    script, env, root, events = harness(tmp_path, monkeypatch)
    inherited = tmp_path / "unrelated-existing-file"
    inherited.write_text("keep me")
    env["BINARY_TMP"] = str(inherited)
    # The fake core and initial menu use only builtins plus the fixture's id,
    # uname and manager; awk is deliberately absent from this isolated PATH.
    env["PATH"] = f"{tmp_path}/commands:{root}/usr/local/bin"
    result = run(script, env)
    assert result.returncode != 0
    assert "Agent 安装缺少工具：awk" in result.stderr
    assert "未找到支持的包管理器" in result.stderr
    assert "请手动安装 gawk coreutils grep sed 后重新运行" in result.stderr
    assert "agent-download" not in events.read_text()
    assert inherited.read_text() == "keep me"


def test_existing_core_reuses_real_skip_logic_without_modification(tmp_path, monkeypatch):
    script, env, root, events = harness(tmp_path, monkeypatch, real_core=True)
    originals = {path: path.read_bytes() for path in (
        root / "etc/mihomo/config.yaml", root / "usr/local/bin/mihomo",
        root / "etc/systemd/system/mihomo.service")}
    result = run(script, env)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "Mihomo：已有部署，已保留" in result.stdout
    assert "跳过已有 mihomo" in detail_output(result)
    for path, content in originals.items():
        assert path.read_bytes() == content
    assert "agent-download" in events.read_text()
    assert not any(line.endswith(" mihomo") for line in events.read_text().splitlines())
