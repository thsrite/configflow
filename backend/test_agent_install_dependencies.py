"""Dependency setup runs inside native installers before touching deployments."""
import json
from itertools import combinations
import os
from pathlib import Path
import shutil
import shlex
import subprocess
import sys

import pytest

from backend.agents.go_install_script import AGENT_INSTALL_DEPENDENCIES
from backend import test_agent_native_aio_install as aio
from backend import test_aio_agent_core_install as aio_core
from backend import test_single_agent_core_install as single
from backend import test_gateway_core_install as gateway


TOOLS = "awk basename cat chmod cmp cp dirname grep head ln mkdir mktemp mv readlink rm sed wc".split()
FORWARDING_TOOLS = ("sysctl", "cmp", "ln")
FORWARDING_MANAGERS = [
    ("apt-get", ["install", "-y", "--no-install-recommends"], "procps"),
    ("apk", ["add", "--no-cache"], "procps"),
    ("pacman", ["-S", "--needed", "--noconfirm"], "procps-ng"),
]


def isolate_dependencies(tmp_path, env, missing=(), package_manager="apk", tzdata=True):
    """No host package manager can be reached, including after a mock failure."""
    commands = tmp_path / "commands"
    commands.mkdir(exist_ok=True)
    unavailable = tmp_path / "unavailable"
    unavailable.mkdir()
    for tool in [*TOOLS, "sh"]:
        if not (commands / tool).exists():
            (commands / tool).symlink_to(shutil.which(tool))
    for tool in ("wget", "sleep"):
        if not (commands / tool).exists():
            (commands / tool).write_text("#!/bin/sh\nexit 0\n")
            (commands / tool).chmod(0o755)
    if not (commands / "sysctl").exists():
        # Dependency-only tests may discover sysctl, but must never call the host's.
        (commands / "sysctl").write_text("#!/bin/sh\nprintf 'unexpected sysctl execution\\n' >&2\nexit 97\n")
        (commands / "sysctl").chmod(0o755)
    for tool in missing:
        (commands / tool).rename(unavailable / tool)
    if package_manager:
        manager = commands / package_manager
        # This mock needs only stdlib, even when the test runner uses a venv.
        manager.write_text(f'''#!{sys.executable} -S
import json, os, sys
from pathlib import Path
with open(os.environ['PACKAGE_EVENTS'], 'a') as log:
    log.write(json.dumps([Path(sys.argv[0]).name, sys.argv[1:], os.environ.get('DEBIAN_FRONTEND')]) + '\\n')
updating = sys.argv[1:] == ['update']
if os.environ.get('EVENTS'):
    with open(os.environ['EVENTS'], 'a') as log:
        log.write(('package-update' if updating else 'package-install') + '\\n')
if os.environ.get('FAIL_PACKAGE') == ('update' if updating else 'install'): raise SystemExit(41)
if updating or os.environ.get('PACKAGE_NO_COMMANDS'): raise SystemExit(0)
if 'tzdata' in sys.argv or 'timezone' in sys.argv:
    zone = Path(os.environ['TZDIR']) / 'Asia/Shanghai'
    zone.parent.mkdir(parents=True, exist_ok=True)
    zone.write_text('TZif')
for executable in Path(os.environ['TEST_UNAVAILABLE']).iterdir():
    packages = {{'awk': ['gawk'], 'grep': ['grep'], 'sed': ['sed'], 'wget': ['curl'],
                'sysctl': ['procps-ng' if Path(sys.argv[0]).name in ('pacman', 'dnf', 'yum') else 'procps'],
                'cmp': ['diffutils'], 'nft': ['nftables'], 'ss': ['iproute2', 'iproute'],
                'tar': ['tar'], 'gzip': ['gzip']}}.get(executable.name, ['coreutils'])
    if any(package in sys.argv[1:] for package in packages):
        executable.rename(Path(os.environ['TEST_COMMANDS']) / executable.name)
''')
        manager.chmod(0o755)
    env.pop("DEBIAN_FRONTEND", None)
    events = tmp_path / "packages"
    events.touch()
    # Timezone data is looked up through TZDIR, never the host's zoneinfo.
    zoneinfo = tmp_path / "zoneinfo"
    if tzdata:
        (zoneinfo / "Asia").mkdir(parents=True, exist_ok=True)
        (zoneinfo / "Asia/Shanghai").write_text("TZif")
    # Existing mock core binaries are in the second PATH entry, never host bins.
    core_bins = tmp_path / "root/usr/local/bin"
    env.update(PATH=f"{commands}:{core_bins}", TEST_COMMANDS=str(commands),
               TEST_UNAVAILABLE=str(unavailable), PACKAGE_EVENTS=str(events), TZDIR=str(zoneinfo))
    return events


def dependency_harness(tmp_path, missing=(), manager="apk", required=(), tzdata=True, **options):
    script = tmp_path / "dependencies.sh"
    script.write_text("set -eu\n" + AGENT_INSTALL_DEPENDENCIES +
                      "\nagent_ensure_dependencies " + shlex.join(required) + "\nprintf 'ready\\n'\n")
    env = dict(os.environ, **options)
    events = isolate_dependencies(tmp_path, env, missing, manager, tzdata)
    return script, env, events


def run(script, env, choice="1"):
    return subprocess.run(["/bin/sh", str(script)], env=env, input=choice + "\n",
                          capture_output=True, text=True, timeout=30)


@pytest.mark.parametrize("manager,prefix", [
    ("apk", ["add", "--no-cache"]),
    ("apt-get", ["install", "-y", "--no-install-recommends"]),
    ("dnf", ["install", "-y"]),
    ("yum", ["install", "-y"]),
    ("pacman", ["-S", "--needed", "--noconfirm"]),
    ("zypper", ["--non-interactive", "install", "--no-recommends"]),
])
def test_missing_runtime_tools_installed_and_rechecked(tmp_path, manager, prefix):
    script, env, events = dependency_harness(
        tmp_path, ("awk", "basename", "grep", "sed", "wget"), manager)
    result = run(script, env)
    assert result.returncode == 0, result.stderr
    assert result.stdout.endswith("ready\n")
    transactions = [json.loads(line) for line in events.read_text().splitlines()]
    expected = [manager, prefix + ["gawk", "coreutils", "grep", "sed", "curl", "ca-certificates"],
                "noninteractive" if manager == "apt-get" else None]
    assert transactions == ([["apt-get", ["update"], None], expected] if manager == "apt-get" else [expected])
    # Repeated installs with tools already available make no package transaction.
    assert run(script, env).returncode == 0
    assert len(events.read_text().splitlines()) == len(transactions)


def test_existing_wget_only_installs_missing_runtime_package(tmp_path):
    script, env, events = dependency_harness(tmp_path, ("sed",))
    result = run(script, env)
    assert result.returncode == 0, result.stderr
    assert json.loads(events.read_text()) == ["apk", ["add", "--no-cache", "sed"], None]


def test_no_package_manager_gives_manual_repair_without_continuing(tmp_path):
    script, env, events = dependency_harness(tmp_path, ("awk",), None)
    result = run(script, env)
    assert result.returncode != 0
    assert "未找到支持的包管理器" in result.stderr
    assert "请手动安装 gawk 后重新运行" in result.stderr
    assert "ready" not in result.stdout
    assert events.read_text() == ""


@pytest.mark.parametrize("manager, failure", [("apk", "install"), ("apt-get", "update"), ("apt-get", "install")])
def test_failed_package_transaction_does_not_continue(tmp_path, manager, failure):
    script, env, events = dependency_harness(tmp_path, ("grep",), manager, FAIL_PACKAGE=failure)
    result = run(script, env)
    assert result.returncode != 0
    assert "Agent 系统依赖安装失败" in result.stderr
    assert "ready" not in result.stdout
    if failure == "update":
        assert len(events.read_text().splitlines()) == 1


def test_nominal_package_success_must_provide_actual_tools(tmp_path):
    script, env, events = dependency_harness(tmp_path, ("grep",), PACKAGE_NO_COMMANDS="yes")
    result = run(script, env)
    assert result.returncode != 0
    assert "包管理器执行后仍缺少 Agent 工具：grep" in result.stderr
    assert "ready" not in result.stdout


GATEWAY_TOOLS = ("nft", "ss", "tar", "gzip")


@pytest.mark.parametrize("manager,prefix", [
    ("apk", ["add", "--no-cache"]),
    ("apt-get", ["install", "-y", "--no-install-recommends"]),
    ("dnf", ["install", "-y"]),
    ("pacman", ["-S", "--needed", "--noconfirm"]),
    ("zypper", ["--non-interactive", "install", "--no-recommends"]),
])
def test_gateway_tools_install_distribution_packages_once(tmp_path, manager, prefix):
    for tool in GATEWAY_TOOLS:
        (tmp_path / "commands").mkdir(exist_ok=True)
        (tmp_path / "commands" / tool).write_text("#!/bin/sh\nexit 0\n")
        (tmp_path / "commands" / tool).chmod(0o755)
    script, env, events = dependency_harness(tmp_path, GATEWAY_TOOLS, manager, required=GATEWAY_TOOLS)
    result = run(script, env)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "缺少工具：nft ss tar gzip" in result.stdout
    iproute = "iproute" if manager == "dnf" else "iproute2"
    installs = [json.loads(line) for line in events.read_text().splitlines() if json.loads(line)[1] != ["update"]]
    assert installs == [[manager, prefix + ["nftables", iproute, "tar", "gzip"],
                         "noninteractive" if manager == "apt-get" else None]]
    count = len(events.read_text().splitlines())
    assert run(script, env).returncode == 0
    assert len(events.read_text().splitlines()) == count


@pytest.mark.parametrize("manager,command", [
    ("apk", "apk add --no-cache nftables iproute2"),
    ("apt-get", "apt-get update && apt-get install -y --no-install-recommends nftables iproute2"),
    ("dnf", "dnf install -y nftables iproute"),
    ("pacman", "pacman -S --needed nftables iproute2"),
    ("zypper", "zypper install nftables iproute2"),
])
def test_failed_install_prints_a_copyable_manual_command(tmp_path, manager, command):
    for tool in ("nft", "ss"):
        (tmp_path / "commands").mkdir(exist_ok=True)
        (tmp_path / "commands" / tool).write_text("#!/bin/sh\nexit 0\n")
        (tmp_path / "commands" / tool).chmod(0o755)
    script, env, events = dependency_harness(tmp_path, ("nft", "ss"), manager, required=("nft", "ss"),
                                             FAIL_PACKAGE="install")
    result = run(script, env)
    assert result.returncode != 0
    assert result.stderr.strip().endswith("可在 root 下手动执行：" + command)
    assert "ready" not in result.stdout


@pytest.mark.parametrize("manager,prefix,sysctl_package", FORWARDING_MANAGERS)
@pytest.mark.parametrize("missing", [
    missing for count in (1, 2, 3) for missing in combinations(FORWARDING_TOOLS, count)
])
def test_forwarding_tools_install_only_needed_packages_and_skip_second_run(
        tmp_path, manager, prefix, sysctl_package, missing):
    script, env, events = dependency_harness(
        tmp_path, missing, manager, required=FORWARDING_TOOLS)
    result = run(script, env)
    assert result.returncode == 0, result.stdout + result.stderr
    assert result.stdout.endswith("ready\n")
    package_for = {"sysctl": sysctl_package, "cmp": "diffutils", "ln": "coreutils"}
    expected = [manager, prefix + [package_for[tool] for tool in missing],
                "noninteractive" if manager == "apt-get" else None]
    transactions = [json.loads(line) for line in events.read_text().splitlines()]
    assert transactions == ([["apt-get", ["update"], None], expected]
                            if manager == "apt-get" else [expected])
    assert "缺少工具：" + " ".join(missing) in result.stdout
    assert all((tmp_path / "commands" / tool).exists() for tool in FORWARDING_TOOLS)
    assert run(script, env).returncode == 0
    assert events.read_text().splitlines() == [json.dumps(event) for event in transactions]


@pytest.mark.parametrize("manager", ["apt-get", "apk", "pacman", None])
def test_existing_forwarding_commands_do_not_install_any_packages(tmp_path, manager):
    script, env, events = dependency_harness(tmp_path, manager=manager, required=FORWARDING_TOOLS)
    assert run(script, env).returncode == 0
    assert events.read_text() == ""


@pytest.mark.parametrize("manager,prefix,sysctl_package", FORWARDING_MANAGERS)
def test_shared_coreutils_is_installed_once_for_runtime_and_forwarding_tools(
        tmp_path, manager, prefix, sysctl_package):
    script, env, events = dependency_harness(
        tmp_path, ("head", "ln"), manager, required=FORWARDING_TOOLS)
    result = run(script, env)
    assert result.returncode == 0, result.stdout + result.stderr
    transactions = [json.loads(line) for line in events.read_text().splitlines()]
    assert transactions[-1][1] == prefix + ["coreutils"]


@pytest.mark.parametrize("manager,package", [("apk", "tzdata"), ("apt-get", "tzdata"), ("zypper", "timezone")])
def test_missing_timezone_data_installed_for_oxidns(tmp_path, manager, package):
    script, env, events = dependency_harness(tmp_path, manager=manager, required=("zoneinfo",), tzdata=False)
    result = run(script, env)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "时区数据" in result.stdout
    installs = [json.loads(line) for line in events.read_text().splitlines() if json.loads(line)[1] != ["update"]]
    assert len(installs) == 1 and installs[0][1][-1] == package
    assert (tmp_path / "zoneinfo/Asia/Shanghai").is_file()
    # The second run finds the data and makes no package transaction.
    count = len(events.read_text().splitlines())
    assert run(script, env).returncode == 0
    assert len(events.read_text().splitlines()) == count


def test_timezone_data_not_required_without_oxidns(tmp_path):
    script, env, events = dependency_harness(tmp_path, tzdata=False)
    assert run(script, env).returncode == 0
    assert events.read_text() == ""


def test_unrequested_forwarding_commands_are_not_installed(tmp_path):
    script, env, events = dependency_harness(tmp_path, FORWARDING_TOOLS)
    assert run(script, env).returncode == 0
    assert events.read_text() == ""
    assert all(not (tmp_path / "commands" / tool).exists() for tool in FORWARDING_TOOLS)


def test_install_entrypoints_request_forwarding_tools_only_when_enabled():
    ordinary = aio.generated()
    optout = aio.generated(manage_ipv4_forwarding=False, manage_tun=False)
    for source, expected in ((ordinary, "agent_ensure_dependencies zoneinfo sysctl cmp ln"),
                             (optout, "agent_ensure_dependencies zoneinfo"),
                             (single.generated(), "agent_ensure_dependencies"),
                             (single.generated("oxidns"), "agent_ensure_dependencies zoneinfo"),
                             # The gateway checks its own tools first, then the inner AIO step.
                             (gateway.generate(), ["agent_ensure_dependencies ha_ip nft ss tar gzip sha256sum cmp zoneinfo",
                                                   "agent_ensure_dependencies zoneinfo"])):
        calls = [line.strip() for line in source.splitlines()
                 if line.strip().startswith("agent_ensure_dependencies") and not line.endswith("{")]
        assert calls == (expected if isinstance(expected, list) else [expected])


def installer_harness(tmp_path, monkeypatch, mode, combined):
    if mode == "aio":
        if combined:
            script, env, _ = aio_core.harness(tmp_path, monkeypatch, manager="openrc")
        else:
            script, env, _ = aio.harness(tmp_path, manager="openrc")
    else:
        original = single.generated
        monkeypatch.setattr(single, "generated", lambda service="mihomo", **options:
                            original(service, **{**options, "install_core": combined}))
        script, env, root, _ = single.harness(tmp_path, monkeypatch, manager="openrc")
        # Existing identity needs sed/head before any replacement is safe.
        agent_dir = root / "opt/configflow-agent"
        agent_dir.mkdir()
        (agent_dir / "config-mihomo.json").write_text(json.dumps({
            "service_type": "mihomo", "service_unit": "mihomo", "service_manager": "openrc",
            "agent_name": "single", "agent_id": "old-id", "token": "old-token"}))
    return script, env


@pytest.mark.parametrize("mode", ["single", "aio"])
@pytest.mark.parametrize("combined", [False, True])
def test_install_repairs_tools_before_inspecting_existing_identity(tmp_path, monkeypatch, mode, combined):
    script, env = installer_harness(tmp_path, monkeypatch, mode, combined)
    events = isolate_dependencies(tmp_path, env, ("grep", "head", "sed"))
    result = run(script, env)
    assert result.returncode == 0, result.stdout + result.stderr
    assert len(events.read_text().splitlines()) == 1
    config = (tmp_path / "agent/mihomo/config.json" if mode == "aio"
              else tmp_path / "root/opt/configflow-agent/config-mihomo.json")
    assert json.loads(config.read_text())["agent_id"] == ("id-mihomo" if mode == "aio" else "old-id")


@pytest.mark.parametrize("mode", ["single", "aio"])
@pytest.mark.parametrize("choice", ["2", "3"])
def test_cancel_and_uninstall_never_install_system_dependencies(tmp_path, monkeypatch, mode, choice):
    script, env = installer_harness(tmp_path, monkeypatch, mode, combined=True)
    # wc is required by the installed OpenRC service, not by its removal.
    events = isolate_dependencies(tmp_path, env, ("wc",))
    result = run(script, env, choice)
    assert result.returncode == 0, result.stdout + result.stderr
    assert events.read_text() == ""
    assert not (tmp_path / "core-completed").exists()


@pytest.mark.parametrize("mode", ["single", "aio"])
def test_unsupported_architecture_does_not_install_missing_dependencies(tmp_path, monkeypatch, mode):
    script, env = installer_harness(tmp_path, monkeypatch, mode, combined=True)
    events = isolate_dependencies(tmp_path, env, ("awk",))
    env["TEST_ARCH"] = "unsupported"
    result = run(script, env)
    assert result.returncode != 0
    assert "不支持的 CPU 架构" in result.stdout + result.stderr
    assert events.read_text() == ""
    assert not (tmp_path / "core-completed").exists()


@pytest.mark.parametrize("mode", ["single", "aio"])
def test_dependency_failure_preserves_existing_agents_before_core_phase(tmp_path, monkeypatch, mode):
    script, env = installer_harness(tmp_path, monkeypatch, mode, combined=True)
    agent_dir = tmp_path / ("agent" if mode == "aio" else "root/opt/configflow-agent")
    before = {path: path.read_bytes() for path in agent_dir.rglob("*") if path.is_file()}
    isolate_dependencies(tmp_path, env, ("grep",))
    env["FAIL_PACKAGE"] = "install"
    result = run(script, env)
    assert result.returncode != 0
    assert "Agent 系统依赖安装失败" in result.stderr
    assert not (tmp_path / "core-completed").exists()
    assert "agent-download" not in (tmp_path / "events").read_text()
    assert {path: path.read_bytes() for path in agent_dir.rglob("*") if path.is_file()} == before


@pytest.mark.parametrize("manager,prefix,sysctl_package", FORWARDING_MANAGERS)
@pytest.mark.parametrize("combined", [False, True])
def test_aio_repairs_forwarding_tools_before_core_and_agent_changes(
        tmp_path, monkeypatch, manager, prefix, sysctl_package, combined):
    script, env = installer_harness(tmp_path, monkeypatch, "aio", combined)
    events = isolate_dependencies(tmp_path, env, FORWARDING_TOOLS, manager)
    result = run(script, env)
    assert result.returncode == 0, result.stdout + result.stderr
    transactions = [json.loads(line) for line in events.read_text().splitlines()]
    assert transactions[-1][1] == prefix + [sysctl_package, "diffutils", "coreutils"]
    assert len(transactions) == (2 if manager == "apt-get" else 1)
    log = (tmp_path / "events").read_text()
    assert log.index("package-install") < log.index("agent mihomo") < log.index("sysctl ")
    if combined:
        assert log.index("package-install") < log.index("core --core both") < log.index("agent-download")
        assert (tmp_path / "core-completed").exists()
    else:
        assert "core --core" not in log
    assert (tmp_path / "ip_forward").read_text() == "1\n"
    assert "net.ipv4.ip_forward=1" in (tmp_path / aio.FORWARD_CONFIG).read_text()
    for service in ("mihomo", "oxidns"):
        config = json.loads((tmp_path / f"agent/{service}/config.json").read_text())
        assert config["agent_id"] == f"id-{service}"


@pytest.mark.parametrize("manager", ["apt-get", "apk", "pacman"])
@pytest.mark.parametrize("missing_tool", FORWARDING_TOOLS)
@pytest.mark.parametrize("failure", ["install", "no-commands"])
def test_forwarding_dependency_failure_keeps_existing_services_and_configs(
        tmp_path, monkeypatch, manager, missing_tool, failure):
    script, env = installer_harness(tmp_path, monkeypatch, "aio", combined=True)
    before = {path: path.read_bytes()
              for directory in (tmp_path / "agent", tmp_path / "etc")
              for path in directory.rglob("*") if path.is_file()}
    isolate_dependencies(tmp_path, env, (missing_tool,), manager)
    env["FAIL_PACKAGE" if failure == "install" else "PACKAGE_NO_COMMANDS"] = "install" if failure == "install" else "yes"
    result = run(script, env)
    assert result.returncode != 0
    output = result.stdout + result.stderr + aio.detail_output(result)
    assert missing_tool in output
    assert ("Agent 系统依赖安装失败" if failure == "install" else "包管理器执行后仍缺少 Agent 工具") in output
    assert "安装完成" not in result.stdout
    assert not (tmp_path / "core-completed").exists()
    log = (tmp_path / "events").read_text()
    assert all(line in ("package-update", "package-install") for line in log.splitlines())
    assert (tmp_path / "ip_forward").read_text() == "0\n"
    assert not (tmp_path / aio.FORWARD_CONFIG).exists()
    for path, original in before.items():
        assert path.read_bytes() == original


@pytest.mark.parametrize("mode", ["single", "aio-optout"])
def test_other_installers_do_not_add_forwarding_packages(tmp_path, monkeypatch, mode):
    if mode == "single":
        script, env = installer_harness(tmp_path, monkeypatch, "single", combined=False)
    else:
        script, env, _ = aio.harness(tmp_path, "openrc", manage_ipv4_forwarding=False, manage_tun=False)
    events = isolate_dependencies(tmp_path, env, FORWARDING_TOOLS)
    result = run(script, env)
    assert result.returncode == 0, result.stdout + result.stderr
    assert events.read_text() == ""
    assert all(not (tmp_path / "commands" / tool).exists() for tool in FORWARDING_TOOLS)
    assert not (tmp_path / aio.FORWARD_CONFIG).exists()
