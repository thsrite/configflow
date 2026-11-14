"""Standalone core installation: real archives and checksum checks, isolated init/files."""
import gzip
import hashlib
import io
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tarfile

import pytest
import yaml

from backend.agents.core_install_script import generate_core_install_script


def harness(tmp_path, manager="systemd", **options):
    root = tmp_path / "root"
    commands = tmp_path / "commands"
    commands.mkdir()
    for directory in ("run", "etc/systemd/system", "etc/init.d", "usr/local/bin", "sbin", "proc/net", "proc/self", "proc/sys/net/ipv4", "dev/net", "sys/class/net"):
        (root / directory).mkdir(parents=True, exist_ok=True)
    if manager == "systemd":
        (root / "run/systemd/system").mkdir(parents=True)
    elif manager == "openrc":
        (root / "sbin/openrc-run").write_text("#!/bin/sh\n")
        (root / "sbin/openrc-run").chmod(0o755)
    for network in ("tcp", "udp", "tcp6", "udp6"):
        (root / f"proc/net/{network}").write_text("sl local_address rem_address st\n")
    tun_device = options.pop("TUN_DEVICE", "ready")
    if tun_device == "regular":
        (root / "dev/net/tun").write_text("not a device")
    elif tun_device != "missing":
        (root / "dev/net/tun").symlink_to("/dev/tty" if tun_device == "denied" else "/dev/null")
    cap_status = options.pop("CAP_STATUS", "CapEff:\t0000000000001000\nCapBnd:\t0000000000001000\n")
    if cap_status is not None:
        (root / "proc/self/status").write_text(cap_status)
    forwarding = options.pop("FORWARDING", "1")
    if forwarding is not None:
        (root / "proc/sys/net/ipv4/ip_forward").write_text(forwarding + "\n")
    if options.pop("EXISTING_TUN_INTERFACE", False):
        (root / "sys/class/net/configflow-tun").mkdir()
    if options.pop("TZDATA", True):
        (root / "usr/share/zoneinfo/Asia").mkdir(parents=True)
        (root / "usr/share/zoneinfo/Asia/Shanghai").write_text("TZif")
    events = tmp_path / "events"
    events.touch()
    fake_core = f'''#!{sys.executable} -S
import os, sys
from pathlib import Path
name = 'mihomo' if '-t' in sys.argv else 'oxidns'
with open(os.environ['EVENTS'], 'a') as log: log.write('validate ' + name + '\\n')
config = Path(sys.argv[sys.argv.index('-f' if name == 'mihomo' else '-c') + 1])
assert config.is_file()
if os.environ.get('FAIL_VALIDATE') == name: raise SystemExit(29)
'''.encode()
    mihomo = gzip.compress(fake_core)
    archive = io.BytesIO()
    with tarfile.open(fileobj=archive, mode="w:gz") as bundle:
        member = tarfile.TarInfo("oxidns")
        member.size = len(fake_core)
        bundle.addfile(member, io.BytesIO(fake_core))
    oxidns = archive.getvalue()
    (tmp_path / "mihomo.gz").write_bytes(mihomo)
    (tmp_path / "oxidns.tar.gz").write_bytes(oxidns)
    source = generate_core_install_script()
    # Use real checksums against locally generated test archives.
    source = re.sub(r"MIHOMO_SHA=[0-9a-f]{64}", "MIHOMO_SHA=" + hashlib.sha256(mihomo).hexdigest(), source)
    source = re.sub(r"OXIDNS_SHA=[0-9a-f]{64}", "OXIDNS_SHA=" + hashlib.sha256(oxidns).hexdigest(), source)
    source = re.sub(r"(?<![A-Za-z0-9_])/(etc|run|usr|lib|sbin|proc|dev|sys)/", lambda m: str(root / m[1]) + "/", source)
    source = source.replace("in /run /etc ", f"in {root}/run {root}/etc ")
    installer = tmp_path / "install.sh"
    installer.write_text(source)
    command_bodies = {
        "id": """#!/bin/sh
printf '%s\\n' "${FAKE_UID-0}"
""",
        "uname": """#!/bin/sh
set -eu
if [ "$1" = -s ]; then printf '%s\\n' "${OS_NAME-Linux}"; else printf '%s\\n' "${ARCH-x86_64}"; fi
""",
        "sleep": """#!/bin/sh
set -eu
if [ -n "${LATE_TUN_READY-}" ] && [ "$1" = 1 ]; then
    counter="$ROOT/run/tun-wait-count"
    count=0
    if [ -f "$counter" ]; then IFS= read -r count < "$counter" || [ -n "$count" ]; fi
    count=$((count + 1))
    printf '%s' "$count" > "$counter"
    if [ "$count" -ge "$LATE_TUN_READY" ]; then
        interface="$ROOT/sys/class/net/configflow-tun"
        fdinfo="$ROOT/proc/12345/fdinfo"
        mkdir -p "$interface" "$fdinfo"
        printf '0x1001' > "$interface/tun_flags"
        printf '0x1001' > "$interface/flags"
        printf 'iff: configflow-tun' > "$fdinfo/8"
    fi
fi
""",
        "stat": """#!/bin/sh
printf '%s\\n' "${DEVICE_NUMBER-a:c8}"
""",
        "curl": """import shutil
with open(os.environ['EVENTS'], 'a') as log: log.write('download ' + sys.argv[-1] + '\\n')
if os.environ.get('FAIL_DOWNLOAD'): raise SystemExit(22)
source = 'mihomo.gz' if 'MetaCubeX' in sys.argv[-1] else 'oxidns.tar.gz'
shutil.copyfile(os.path.join(os.environ['FIXTURE'], source), sys.argv[sys.argv.index('--output') + 1])
if os.environ.get('BAD_CHECKSUM') == source:
    with open(sys.argv[sys.argv.index('--output') + 1], 'ab') as file: file.write(b'bad')
""",
        "sha256sum": """import hashlib
if sys.argv[1] == '-c':
    for line in open(sys.argv[2]):
        digest, file = line.rstrip().split('  ', 1)
        if hashlib.sha256(open(file, 'rb').read()).hexdigest() != digest:
            print(file + ': FAILED'); raise SystemExit(1)
        print(file + ': OK')
else:
    for file in sys.argv[1:]: print(hashlib.sha256(open(file, 'rb').read()).hexdigest() + '  ' + file)
""",
    }
    service_body = """name = os.path.basename(sys.argv[0])
args = sys.argv[1:]
with open(os.environ['EVENTS'], 'a') as log: log.write(name + ' ' + ' '.join(args) + '\\n')
if args[0] == 'show':
    print('12345' if '--property=MainPID' in args else ('loaded' if os.environ.get('EXISTING_SERVICE') == args[1] else 'not-found'))
    raise SystemExit(0)
action = args[0] if name != 'rc-service' else args[1]
service = args[-1] if name == 'systemctl' else (args[0] if name == 'rc-service' else args[1])
if name == 'systemctl' and service == 'systemd-resolved':
    if action == 'is-active':
        raise SystemExit(0 if os.environ.get('RESOLVED_ACTIVE') else 3)
    if action == 'restart':
        # The stub listener is gone once the drop-in disables it.
        root = os.environ['ROOT']
        if os.path.exists(os.path.join(root, 'etc/systemd/resolved.conf.d/configflow.conf')):
            for network in ('tcp', 'udp', 'tcp6', 'udp6'):
                path = os.path.join(root, 'proc/net', network)
                lines = open(path).read().splitlines(True)
                open(path, 'w').write(''.join(line for line in lines if '3500007F:0035' not in line and '3600007F:0035' not in line))
    raise SystemExit(0)
if os.environ.get('FAIL_SERVICE') == action + ':' + service or os.environ.get('FAIL_ROLLBACK') == action + ':' + service:
    if name == 'rc-service' and action == 'start':
        with open(os.path.join(os.environ['ROOT'], 'etc', service, service + '.log'), 'w') as log:
            log.write('core startup error: fixture failure\\n')
    raise SystemExit(38)
if name != 'rc-update' and service == 'mihomo':
    from pathlib import Path
    import shutil
    root = Path(os.environ['ROOT'])
    interface = root / 'sys/class/net/configflow-tun'
    fdinfo = root / 'proc/12345/fdinfo'
    if action == 'start':
        counter = root / 'run/mihomo-start-count'
        count = int(counter.read_text()) + 1 if counter.exists() else 1
        counter.write_text(str(count))
        if count > 1 and os.environ.get('FAIL_FALLBACK_START'): raise SystemExit(38)
        (root / 'run/mihomo.pid').write_text('12345')
        enabled = 'tun:\\n  enable: true\\n' in (root / 'etc/mihomo/config.yaml').read_text()
        if enabled and not os.environ.get('NO_TUN_INTERFACE') and not os.environ.get('LATE_TUN_READY'):
            interface.mkdir(parents=True, exist_ok=True)
            (interface / 'tun_flags').write_text(os.environ.get('TUN_FLAGS', '0x1001') + '\\n')
            (interface / 'flags').write_text(os.environ.get('TUN_LINK_FLAGS', '0x1001') + '\\n')
            fdinfo.mkdir(parents=True, exist_ok=True)
            (fdinfo / '8').write_text('iff:\\t' + os.environ.get('TUN_OWNER_DEVICE', 'configflow-tun') + '\\n')
    elif action == 'stop':
        shutil.rmtree(interface, ignore_errors=True)
        shutil.rmtree(fdinfo, ignore_errors=True)
"""
    for name in ("systemctl",) if manager == "systemd" else ("rc-service", "rc-update"):
        command_bodies[name] = service_body
    command_bodies["dig"] = """with open(os.environ['EVENTS'], 'a') as log: log.write('dig ' + ' '.join(sys.argv[1:]) + '\\n')
if os.environ.get('FAIL_DIG') == 'exit': raise SystemExit(9)
if os.environ.get('FAIL_DIG') == 'servfail':
    print(';; ->>HEADER<<- opcode: QUERY, status: SERVFAIL, id: 17')
    print(';; flags: qr rd ra; QUERY: 1, ANSWER: 0, AUTHORITY: 0, ADDITIONAL: 0')
else:
    print(';; ->>HEADER<<- opcode: QUERY, status: NOERROR, id: 17')
    print(';; flags: qr rd ra; QUERY: 1, ANSWER: 1, AUTHORITY: 0, ADDITIONAL: 0')
"""
    if options.pop("NO_STAT", False):
        del command_bodies["stat"]
    if options.pop("NO_DIG", False):
        del command_bodies["dig"]
    if options.pop("DOWNLOAD_TOOL", "curl") == "wget":
        command_bodies["wget"] = command_bodies.pop("curl").replace("'--output'", "'-O'")
    package_manager = options.pop("PACKAGE_MANAGER", None)
    missing_commands = options.pop("MISSING_COMMANDS", ())
    if package_manager:
        command_bodies[package_manager] = """from pathlib import Path
import shutil
name = os.path.basename(sys.argv[0])
with open(os.environ['EVENTS'], 'a') as log:
    log.write('package ' + name + ' ' + ' '.join(sys.argv[1:]) + ' DEBIAN_FRONTEND=' + os.environ.get('DEBIAN_FRONTEND', '') + '\\n')
updating = sys.argv[1:] == ['update']
if os.environ.get('FAIL_PACKAGE') == ('update' if updating else 'install'): raise SystemExit(41)
if updating or os.environ.get('PACKAGE_NO_COMMANDS'): raise SystemExit(0)
commands = Path(os.environ['PATH'])
if 'tzdata' in sys.argv or 'timezone' in sys.argv:
    zone = Path(os.environ['ROOT']) / 'usr/share/zoneinfo/Asia/Shanghai'
    zone.parent.mkdir(parents=True, exist_ok=True)
    zone.write_text('TZif')
for executable in (Path(os.environ['FIXTURE']) / 'unavailable-commands').iterdir():
    # Restore the real fixture command only after a successful fake transaction.
    shutil.move(str(executable), commands / executable.name)
"""
    for name in ("sysctl", "ip", "iptables", "nft", "modprobe", "pct"):
        command_bodies[name] = "with open(os.environ['EVENTS'], 'a') as log: log.write('FORBIDDEN ' + os.path.basename(sys.argv[0]) + '\\n')\nraise SystemExit(99)"
    for name, body in command_bodies.items():
        executable = commands / name
        # Fixed replies and the simulated clock need no Python process. The
        # remaining fake commands use only stdlib, without site initialization.
        executable.write_text(body if body.startswith("#!") else
                              f"#!{sys.executable} -S\nimport os, sys\n" + body + "\n")
        executable.chmod(0o755)
    for name in ("awk", "cat", "chmod", "cp", "gzip", "mkdir", "mktemp", "mv", "rm", "rmdir", "sed", "sleep", "tail", "tar", "sh"):
        if not (commands / name).exists():
            (commands / name).symlink_to(shutil.which(name))
    unavailable = tmp_path / "unavailable-commands"
    unavailable.mkdir()
    for name in missing_commands:
        (commands / name).rename(unavailable / name)
    env = dict(os.environ)
    # CI may export this globally; tests must observe what the installer sets.
    env.pop("DEBIAN_FRONTEND", None)
    env.pop("TZDIR", None)
    env.update(PATH=str(commands), EVENTS=str(events), FIXTURE=str(tmp_path), ROOT=str(root), CONFIGFLOW_INSTALL_LOG_DIR=str(tmp_path), **options)
    return installer, env, root, events


def run(installer, env, *args):
    result = subprocess.run(["sh", str(installer), *args], env=env, text=True, errors="replace", capture_output=True, timeout=30, start_new_session=True)
    log_match = re.search(r"完整日志[：:]\s*([^\r\n]+)", result.stdout + result.stderr)
    result.log_path = Path(log_match[1].strip()) if log_match else None
    # Minimal systems without mktemp deliberately fall back to raw output until
    # dependencies are installed; all ordinary fixtures inspect the saved log.
    result.detail_output = result.log_path.read_text(errors="replace") if result.log_path and result.log_path.is_file() else result.stdout + result.stderr
    return result


def assert_uninstalled(root):
    for service in ("mihomo", "oxidns"):
        assert not (root / f"usr/local/bin/{service}").exists()
        assert not (root / f"etc/{service}").exists()
        assert not (root / f"etc/systemd/system/{service}.service").exists()
        assert not (root / f"etc/init.d/{service}").exists()
    assert not (root / "run/configflow-core-install.lock").exists()


def seed_existing_core(root, service, manager="systemd"):
    """Represent an externally managed install, without ConfigFlow receipts."""
    unit = f"etc/systemd/system/{service}.service" if manager == "systemd" else f"etc/init.d/{service}"
    contents = {
        f"usr/local/bin/{service}": b"existing core executable\n",
        f"etc/{service}/config.yaml": b"# existing user configuration\n",
        f"etc/{service}/state.db": b"existing runtime state\x00\x01",
        unit: b"# existing service definition\n",
    }
    for relative, data in contents.items():
        path = root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
    (root / f"usr/local/bin/{service}").chmod(0o755)
    return contents


def assert_existing_core_untouched(root, service, contents, events):
    for relative, data in contents.items():
        assert (root / relative).read_bytes() == data
    assert not (root / f"etc/{service}/.configflow-core-install").exists()
    for line in events.read_text().splitlines():
        assert not re.match(rf"systemctl (?:start|stop|restart|enable|disable) {service}(?:\.service)?$", line)
        assert not re.match(rf"rc-service {service} (?:start|stop|restart)$", line)
        assert not re.match(rf"rc-update (?:add|del) {service}(?: |$)", line)


def operational_events(events):
    """Ignore read-only systemd discovery while checking absence of changes."""
    return [line for line in events.read_text().splitlines() if not line.startswith("systemctl show ")]


def test_generated_script_syntax_and_bootstrap_config(tmp_path):
    source = generate_core_install_script()
    assert source.startswith("#!/bin/sh\n# ConfigFlow core installer interface: 1\n")
    script = tmp_path / "script.sh"
    script.write_text(source)
    assert subprocess.run(["sh", "-n", str(script)], capture_output=True).returncode == 0
    mihomo = yaml.safe_load(source.split("<<MIHOMO_CONFIG\n", 1)[1].split("\nMIHOMO_CONFIG", 1)[0])
    assert mihomo["tun"]["enable"] == "$TUN_ENABLED"
    assert mihomo["tun"]["dns-hijack"] == ["any:1053", "tcp://any:1053"]
    assert mihomo["tun"]["auto-route"] is True
    assert mihomo["tun"]["auto-redirect"] is False
    assert mihomo["dns"]["listen"] == "0.0.0.0:1053"
    assert mihomo["dns"]["nameserver"] == ["223.5.5.5", "119.29.29.29"]
    assert all(mihomo[key] == 0 for key in ("port", "socks-port", "mixed-port", "redir-port", "tproxy-port"))
    assert "/opt/configflow-agent" not in source
    assert "iptables" not in source
    assert "sysctl -w net.ipv4.ip_forward=1" in source  # Printed repair instructions only.


@pytest.mark.parametrize("package_manager,install_prefix", [
    ("apk", "apk add --no-cache"),
    ("apt-get", "apt-get install -y --no-install-recommends"),
    ("dnf", "dnf install -y"),
    ("yum", "yum install -y"),
    ("pacman", "pacman -S --needed --noconfirm"),
    ("zypper", "zypper --non-interactive install --no-recommends"),
])
def test_missing_dependencies_installed_before_core_download(tmp_path, package_manager, install_prefix):
    installer, env, root, events = harness(
        tmp_path, "openrc" if package_manager == "apk" else "systemd",
        PACKAGE_MANAGER=package_manager,
        MISSING_COMMANDS=("awk", "cat", "chmod", "cp", "gzip", "mkdir", "mktemp", "mv", "rm", "rmdir", "sed", "sha256sum", "sleep", "tail", "tar", "curl"),
    )
    result = run(installer, env)
    assert result.returncode == 0, result.stdout + result.stderr
    log = events.read_text()
    package_events = [line for line in log.splitlines() if line.startswith("package ")]
    transaction = next(line for line in package_events if line.startswith("package " + install_prefix))
    assert transaction.split(" DEBIAN_FRONTEND=", 1)[0] == (
        "package " + install_prefix + " gawk coreutils gzip sed tar curl ca-certificates"
    )
    assert log.index(transaction) < log.index("download ") < log.index("validate mihomo")
    if package_manager == "apt-get":
        assert package_events[0].startswith("package apt-get update ")
        assert transaction.endswith("DEBIAN_FRONTEND=noninteractive")
    else:
        assert len(package_events) == 1
    assert "-Sy" not in log and " upgrade " not in log
    assert (root / "etc/mihomo/.configflow-core-install").is_file()
    # A second invocation must not rerun the package manager or restart cores.
    assert run(installer, env).returncode == 0
    assert events.read_text() == log


@pytest.mark.parametrize("package_manager,package", [("apk", "tzdata"), ("apt-get", "tzdata"), ("zypper", "timezone")])
def test_missing_timezone_data_installed_for_new_oxidns(tmp_path, package_manager, package):
    installer, env, root, events = harness(
        tmp_path, "openrc" if package_manager == "apk" else "systemd",
        PACKAGE_MANAGER=package_manager, TZDATA=False)
    result = run(installer, env, "--core", "oxidns")
    assert result.returncode == 0, result.stdout + result.stderr
    installs = [line for line in events.read_text().splitlines()
                if line.startswith("package ") and " update " not in line]
    assert len(installs) == 1 and installs[0].split(" DEBIAN_FRONTEND=", 1)[0].endswith(" " + package)
    assert (root / "usr/share/zoneinfo/Asia/Shanghai").is_file()


def test_mihomo_only_install_does_not_require_timezone_data(tmp_path):
    installer, env, root, events = harness(tmp_path, PACKAGE_MANAGER="apk", TZDATA=False)
    result = run(installer, env, "--core", "mihomo")
    assert result.returncode == 0, result.stdout + result.stderr
    assert "package " not in events.read_text()


@pytest.mark.parametrize("missing", [(), ("tar",)])
def test_existing_wget_and_custom_trust_are_preserved(tmp_path, missing):
    installer, env, root, events = harness(
        tmp_path, PACKAGE_MANAGER="apk", DOWNLOAD_TOOL="wget", MISSING_COMMANDS=missing,
        SSL_CERT_FILE="/custom/company-ca.pem", CURL_CA_BUNDLE="/custom/curl-ca.pem",
    )
    result = run(installer, env)
    assert result.returncode == 0, result.stdout + result.stderr
    package_events = [line for line in events.read_text().splitlines() if line.startswith("package ")]
    assert package_events == (["package apk add --no-cache tar DEBIAN_FRONTEND="] if missing else [])
    assert not (Path(env["PATH"]) / "curl").exists()
    assert env["SSL_CERT_FILE"] == "/custom/company-ca.pem"
    assert env["CURL_CA_BUNDLE"] == "/custom/curl-ca.pem"


def test_missing_dependencies_without_supported_package_manager_stop_before_core_changes(tmp_path):
    installer, env, root, events = harness(tmp_path, MISSING_COMMANDS=("tar", "curl"))
    result = run(installer, env)
    assert result.returncode != 0
    assert "未找到支持的包管理器" in result.stderr
    assert "tar curl ca-certificates" in result.stderr
    assert operational_events(events) == []
    assert_uninstalled(root)


@pytest.mark.parametrize("package_manager", ["apk", "apt-get", "dnf", "yum", "pacman", "zypper"])
def test_dependency_install_failure_never_downloads_or_installs_cores(tmp_path, package_manager):
    installer, env, root, events = harness(
        tmp_path, PACKAGE_MANAGER=package_manager, MISSING_COMMANDS=("tar",), FAIL_PACKAGE="install",
    )
    result = run(installer, env)
    assert result.returncode != 0
    assert "系统依赖安装失败" in result.stderr
    assert "软件源及包管理器锁" in result.stderr
    assert "系统依赖会保留" in result.stderr
    assert all(line.startswith("package ") for line in operational_events(events))
    assert_uninstalled(root)


def test_failed_apt_update_does_not_install_packages_or_cores(tmp_path):
    installer, env, root, events = harness(
        tmp_path, PACKAGE_MANAGER="apt-get", MISSING_COMMANDS=("tar",), FAIL_PACKAGE="update",
    )
    result = run(installer, env)
    assert result.returncode != 0
    assert operational_events(events) == ["package apt-get update DEBIAN_FRONTEND="]
    assert_uninstalled(root)


def test_nominal_dependency_install_rechecks_actual_commands(tmp_path):
    installer, env, root, events = harness(
        tmp_path, PACKAGE_MANAGER="apk", MISSING_COMMANDS=("tar", "curl"), PACKAGE_NO_COMMANDS="1",
    )
    result = run(installer, env)
    assert result.returncode != 0
    assert "包管理器执行后仍缺少依赖：tar curl/wget" in result.stderr
    assert "尚未下载或安装核心" in result.stderr
    assert "download " not in events.read_text()
    assert_uninstalled(root)


@pytest.mark.parametrize("manager,options", [
    ("systemd", {"FAKE_UID": "1000"}),
    ("systemd", {"OS_NAME": "Darwin"}),
    ("systemd", {"ARCH": "riscv64"}),
    ("none", {}),
])
def test_unsupported_environment_never_installs_dependencies(tmp_path, manager, options):
    installer, env, root, events = harness(
        tmp_path, manager, PACKAGE_MANAGER="apk", MISSING_COMMANDS=("tar", "curl"), **options,
    )
    result = run(installer, env)
    assert result.returncode != 0
    assert events.read_text() == ""
    assert_uninstalled(root)


def test_later_core_failure_keeps_installed_dependencies(tmp_path):
    installer, env, root, events = harness(
        tmp_path, PACKAGE_MANAGER="apk", MISSING_COMMANDS=("tar",), FAIL_DOWNLOAD="1",
    )
    result = run(installer, env)
    assert result.returncode != 0
    assert "系统依赖会保留" in result.stderr
    assert "ca-certificates / 自定义 CA 信任" in result.stderr
    assert (Path(env["PATH"]) / "tar").exists()
    assert "package apk add --no-cache tar" in events.read_text()
    assert "package apk del" not in events.read_text()
    assert_uninstalled(root)


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
def test_install_two_cores_and_preserve_configuration_on_second_run(tmp_path, manager):
    installer, env, root, events = harness(tmp_path, manager)
    result = run(installer, env)
    assert result.returncode == 0, result.stdout + result.stderr
    initial_events = events.read_text()
    assert initial_events.index("validate oxidns") < initial_events.index("enable mihomo" if manager == "systemd" else "add mihomo")
    assert initial_events.count("dig ") == 4
    for service in ("mihomo", "oxidns"):
        assert os.access(root / f"usr/local/bin/{service}", os.X_OK)
        assert (root / f"etc/{service}/.configflow-core-install").is_file()
    config = root / "etc/mihomo/config.yaml"
    config.write_text("# user's published configuration\n")
    result = run(installer, env)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "跳过" in result.detail_output
    assert events.read_text() == initial_events
    assert config.read_text() == "# user's published configuration\n"
    assert not (root / "run/configflow-core-install.lock").exists()


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
@pytest.mark.parametrize("failure", ["start:oxidns", "enable:oxidns"])
def test_service_failure_rolls_back_both_cores(tmp_path, manager, failure):
    if manager == "openrc" and failure.startswith("enable:"):
        failure = failure.replace("enable:", "add:")
    installer, env, root, events = harness(tmp_path, manager, FAIL_SERVICE=failure)
    result = run(installer, env)
    assert result.returncode != 0
    assert_uninstalled(root)
    log = events.read_text()
    assert ("systemctl stop mihomo" if manager == "systemd" else "rc-service mihomo stop") in log
    assert ("systemctl disable oxidns" if manager == "systemd" else "rc-update del oxidns default") in log


@pytest.mark.parametrize("options", [{"BAD_CHECKSUM": "mihomo.gz"}, {"BAD_CHECKSUM": "oxidns.tar.gz"}, {"FAIL_VALIDATE": "mihomo"}, {"FAIL_VALIDATE": "oxidns"}, {"FAIL_DOWNLOAD": "1"}])
def test_failed_download_checksum_or_validation_writes_no_cores(tmp_path, options):
    installer, env, root, events = harness(tmp_path, **options)
    result = run(installer, env)
    assert result.returncode != 0
    assert_uninstalled(root)
    assert " enable " not in events.read_text()
    assert " start " not in events.read_text()
    assert not list(tmp_path.glob("stage.*"))


@pytest.mark.parametrize("failure", ["exit", "servfail"])
def test_dns_failure_even_zero_exit_servfail_rolls_back(tmp_path, failure):
    installer, env, root, events = harness(tmp_path, FAIL_DIG=failure)
    result = run(installer, env)
    assert result.returncode != 0
    assert "DNS 查询" in result.stderr
    assert "systemctl stop oxidns" in events.read_text()
    assert_uninstalled(root)


def test_existing_partial_configuration_is_skipped_while_other_core_installs(tmp_path):
    installer, env, root, events = harness(tmp_path)
    path = root / "etc/oxidns/config.yaml"
    path.parent.mkdir()
    path.write_text("original config")
    result = run(installer, env)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "跳过" in result.detail_output
    assert path.read_text() == "original config"
    assert "svenshi/oxidns" not in events.read_text()
    assert (root / "usr/local/bin/mihomo").exists()
    assert not (root / "usr/local/bin/oxidns").exists()
    assert "oxidns" not in "\n".join(line for line in operational_events(events) if not line.startswith("download "))


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
@pytest.mark.parametrize("selected,port,asset,other", [
    ("mihomo", 1053, "MetaCubeX/mihomo", "oxidns"),
    ("oxidns", 53, "svenshi/oxidns", "mihomo"),
])
def test_single_core_installs_only_selected_binary_service_and_dns(tmp_path, manager, selected, port, asset, other):
    installer, env, root, events = harness(tmp_path, manager)
    result = run(installer, env, "--core", selected)
    assert result.returncode == 0, result.stdout + result.stderr
    log = events.read_text()
    downloads = [line for line in log.splitlines() if line.startswith("download ")]
    assert len(downloads) == 1 and asset in downloads[0]
    assert f"validate {selected}" in log
    assert f"validate {other}" not in log
    assert (root / f"usr/local/bin/{selected}").is_file()
    assert (root / f"etc/{selected}/.configflow-core-install").is_file()
    assert not (root / f"usr/local/bin/{other}").exists()
    assert not (root / f"etc/{other}").exists()
    queries = [line for line in log.splitlines() if line.startswith("dig ")]
    assert len(queries) == 2
    assert all(f" -p {port} " in line for line in queries)
    assert sum("+tcp" in line for line in queries) == 1
    assert other not in "\n".join(operational_events(events))
    if selected == "oxidns":
        config = yaml.safe_load((root / "etc/oxidns/config.yaml").read_text())
        upstreams = config["plugins"][0]["args"]["upstreams"]
        assert upstreams == [{"addr": "udp://223.5.5.5"}, {"addr": "udp://119.29.29.29"}]
        assert "公共" in result.detail_output
        assert "IPv4 转发" not in result.detail_output
        assert "tun.enable" not in result.detail_output


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
@pytest.mark.parametrize("existing,new,occupied_port", [("mihomo", "oxidns", "041D"), ("oxidns", "mihomo", "0035")])
def test_both_installs_missing_core_without_touching_existing_peer(tmp_path, manager, existing, new, occupied_port):
    installer, env, root, events = harness(tmp_path, manager)
    contents = seed_existing_core(root, existing, manager)
    with (root / "proc/net/udp").open("a") as file:
        file.write(f"0: 00000000:{occupied_port} 00000000:0000 07\n")
    result = run(installer, env, "--core", "both")
    assert result.returncode == 0, result.stdout + result.stderr
    assert "跳过" in result.detail_output
    assert (root / f"etc/{new}/.configflow-core-install").is_file()
    assert_existing_core_untouched(root, existing, contents, events)
    log = events.read_text()
    assert len([line for line in log.splitlines() if line.startswith("download ")]) == 1
    assert f"validate {existing}" not in log
    assert f"validate {new}" in log
    queries = [line for line in log.splitlines() if line.startswith("dig ")]
    assert len(queries) == 2
    new_port = 53 if new == "oxidns" else 1053
    assert all(f" -p {new_port} " in line for line in queries)
    if new == "oxidns":
        config = yaml.safe_load((root / "etc/oxidns/config.yaml").read_text())
        assert config["plugins"][0]["args"]["upstreams"] == [{"addr": "udp://127.0.0.1:1053"}]
        assert "127.0.0.1:1053" in result.detail_output
        assert "tun.enable" not in result.detail_output
    else:
        assert "已有 OxiDNS 的上游保持原样" in result.detail_output
        assert "127.0.0.1:1053" in result.detail_output


@pytest.mark.parametrize("selected", ["both", "mihomo", "oxidns"])
def test_existing_selected_cores_skip_without_package_or_service_operations(tmp_path, selected):
    installer, env, root, events = harness(
        tmp_path, PACKAGE_MANAGER="apk", MISSING_COMMANDS=("awk", "tar", "curl", "sha256sum", "cat"),
    )
    contents = {service: seed_existing_core(root, service) for service in ("mihomo", "oxidns")}
    result = run(installer, env, "--core", selected)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "跳过" in result.detail_output
    assert operational_events(events) == []
    for service, files in contents.items():
        assert_existing_core_untouched(root, service, files, events)
    assert not (root / "run/configflow-core-install.lock").exists()


@pytest.mark.parametrize("evidence", ["binary", "service", "path", "loaded-service"])
def test_external_mihomo_installation_is_recognized_without_default_config(tmp_path, evidence):
    installer, env, root, events = harness(tmp_path, EXISTING_SERVICE="mihomo.service" if evidence == "loaded-service" else "")
    path = None
    if evidence == "binary":
        path = root / "usr/local/bin/mihomo"
    elif evidence == "service":
        path = root / "etc/systemd/system/mihomo.service"
    elif evidence == "path":
        path = Path(env["PATH"]) / "mihomo"
    if path is not None:
        path.write_text("#!/bin/sh\nexit 97\n")
        path.chmod(0o755)
    result = run(installer, env)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "跳过" in result.detail_output
    assert not (root / "etc/mihomo").exists()
    assert (root / "etc/oxidns/.configflow-core-install").is_file()
    assert "MetaCubeX/mihomo" not in events.read_text()
    assert not any("mihomo" in line for line in operational_events(events))
    if path is not None:
        assert path.read_text() == "#!/bin/sh\nexit 97\n"


@pytest.mark.parametrize("args", [("--core", "invalid"), ("--core",), ("--bad-option",), ("mihomo",)])
def test_invalid_core_arguments_reject_before_dependency_or_service_actions(tmp_path, args):
    installer, env, root, events = harness(tmp_path, PACKAGE_MANAGER="apk", MISSING_COMMANDS=("tar", "curl"))
    result = run(installer, env, *args)
    assert result.returncode != 0
    assert events.read_text() == ""
    assert_uninstalled(root)


@pytest.mark.parametrize("selected,other_port", [("oxidns", "041D"), ("mihomo", "0035")])
@pytest.mark.parametrize("network,state", [("tcp", "0A"), ("udp", "07"), ("tcp6", "0A"), ("udp6", "07")])
def test_single_core_ignores_unrelated_dns_port(tmp_path, selected, other_port, network, state):
    installer, env, root, events = harness(tmp_path)
    with (root / f"proc/net/{network}").open("a") as file:
        file.write(f"0: 00000000:{other_port} 00000000:0000 {state}\n")
    result = run(installer, env, "--core", selected)
    assert result.returncode == 0, result.stdout + result.stderr
    assert (root / f"etc/{selected}/.configflow-core-install").is_file()


@pytest.mark.parametrize("selected,port", [("oxidns", "0035"), ("mihomo", "041D")])
def test_single_core_checks_its_own_dns_port_before_download(tmp_path, selected, port):
    installer, env, root, events = harness(tmp_path)
    with (root / "proc/net/udp").open("a") as file:
        file.write(f"0: 00000000:{port} 00000000:0000 07\n")
    result = run(installer, env, "--core", selected)
    assert result.returncode != 0
    assert "端口被占用" in result.stderr
    assert "download " not in events.read_text()
    assert_uninstalled(root)


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
@pytest.mark.parametrize("failure", ["service", "dns", "package"])
def test_install_missing_core_failure_preserves_existing_mihomo(tmp_path, manager, failure):
    options = {"FAIL_SERVICE": "start:oxidns"} if failure == "service" else {"FAIL_DIG": "servfail"}
    if failure == "package":
        options = {"PACKAGE_MANAGER": "apk", "MISSING_COMMANDS": ("tar",), "FAIL_PACKAGE": "install"}
    installer, env, root, events = harness(tmp_path, manager, **options)
    contents = seed_existing_core(root, "mihomo", manager)
    result = run(installer, env, "--core", "both")
    assert result.returncode != 0
    assert_existing_core_untouched(root, "mihomo", contents, events)
    assert not (root / "usr/local/bin/oxidns").exists()
    assert not (root / "etc/oxidns").exists()
    assert not (root / "run/configflow-core-install.lock").exists()
    if failure == "dns":
        assert "127.0.0.1:1053" in result.detail_output + result.stderr
        assert "公共" not in result.detail_output
    if failure == "package":
        assert "download " not in events.read_text()


@pytest.mark.parametrize("network,port,state", [("udp", "0035", "07"), ("tcp", "041D", "0A"), ("udp6", "041D", "07"), ("tcp6", "0035", "0A")])
def test_port_conflict_prevents_download_and_mutation(tmp_path, network, port, state):
    installer, env, root, events = harness(tmp_path)
    with (root / f"proc/net/{network}").open("a") as file:
        file.write(f"0: 00000000:{port} 00000000:0000 {state}\n")
    result = run(installer, env)
    assert result.returncode != 0
    assert "端口被占用" in result.stderr
    assert "download " not in events.read_text()
    assert_uninstalled(root)


@pytest.mark.parametrize("options", [{"FAKE_UID": "1000"}, {"ARCH": "riscv64"}])
def test_unsupported_environment_writes_nothing(tmp_path, options):
    installer, env, root, events = harness(tmp_path, **options)
    result = run(installer, env)
    assert result.returncode != 0
    assert operational_events(events) == []
    assert_uninstalled(root)


def test_concurrent_install_keeps_other_process_lock(tmp_path):
    installer, env, root, events = harness(tmp_path)
    lock = root / "run/configflow-core-install.lock"
    lock.mkdir()
    (lock / "pid").write_text("123456")
    result = run(installer, env)
    assert result.returncode != 0
    assert "另一个核心安装" in result.stderr
    assert (lock / "pid").read_text() == "123456"
    assert operational_events(events) == []


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
def test_failed_service_stop_preserves_manageable_core_files(tmp_path, manager):
    installer, env, root, events = harness(tmp_path, manager, FAIL_SERVICE="start:oxidns", FAIL_ROLLBACK="stop:mihomo")
    result = run(installer, env)
    assert result.returncode != 0
    assert "回滚未完成" in result.stderr
    assert (root / "usr/local/bin/mihomo").exists()
    assert (root / "etc/mihomo/config.yaml").exists()
    unit = "etc/systemd/system/mihomo.service" if manager == "systemd" else "etc/init.d/mihomo"
    assert (root / unit).exists()
    assert not (root / "etc/mihomo/.configflow-core-install").exists()
    assert not (root / "run/configflow-core-install.lock").exists()


def test_no_dig_reports_unverified_dns_without_rejecting_install(tmp_path):
    installer, env, root, events = harness(tmp_path, NO_DIG=True)
    result = run(installer, env)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "实际 DNS 解析尚未验证" in result.detail_output
    assert "UDP/TCP DNS 查询均已通过" not in result.detail_output
    assert "未验证（缺少 dig）" in result.stdout
    assert "实际解析未验证" in result.stdout
    assert "UDP/TCP 查询通过" not in result.stdout
    assert (root / "etc/mihomo/.configflow-core-install").exists()


@pytest.mark.parametrize("selected", ["both", "mihomo", "oxidns"])
def test_default_output_summarizes_every_core_selection_and_keeps_verbose_log(tmp_path, selected):
    installer, env, root, events = harness(tmp_path)
    result = run(installer, env, "--core", selected)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "检查核心安装环境与依赖" in result.stdout
    assert "下载安装" in result.stdout
    assert "验证新装核心的 DNS" in result.stdout
    assert "UDP/TCP 查询均通过" in result.stdout
    assert "核心安装完成" in result.stdout + result.stderr
    assert "完整日志：" in result.stdout
    assert "HEADER" not in result.stdout
    assert "初始上游为公共 DNS" not in result.stdout
    assert "首次从面板发布前" not in result.stdout
    assert result.log_path and result.log_path.is_file()
    assert result.log_path.stat().st_mode & 0o077 == 0
    assert "status: NOERROR" in result.detail_output
    if selected in ("both", "mihomo"):
        assert "Mihomo：新装" in result.stdout
        assert "TUN：已启用" in result.stdout
        assert "DIRECT 直连" in result.stdout + result.stderr
        assert "重启后是否保持尚未确认" in result.stdout + result.stderr
    if selected in ("both", "oxidns"):
        assert "OxiDNS：新装" in result.stdout
    if selected == "oxidns":
        assert "TUN：" not in result.stdout
        assert "IPv4 转发" not in result.stdout
        assert "DIRECT 直连" not in result.stdout
    assert "如需面板管理，请继续安装对应 Agent" in result.stdout + result.stderr


@pytest.mark.parametrize("installer_kind,selected", [("single", "mihomo"), ("single", "oxidns"), ("aio", "both")])
def test_embedded_core_phase_does_not_ask_for_a_separate_agent(tmp_path, installer_kind, selected):
    from backend.agents.go_install_script import generate_go_agent_install_script, generate_go_aio_install_script

    if installer_kind == "single":
        combined = generate_go_agent_install_script("https://config.test", "single", selected, install_core=True)
    else:
        combined = generate_go_aio_install_script("https://config.test", "cohost", install_core=True)
    # Run the real core installer behind the exact preamble the combined
    # installer puts in front of it; the Agent is installed right after.
    start = combined.index(f"(\nset -- --core {selected}\n")
    preamble = combined[start:combined.index("#!/bin/sh", start)]
    installer, env, root, events = harness(tmp_path)
    installer.write_text(preamble + installer.read_text().rstrip() + "\n)\n")
    result = run(installer, env)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "核心安装完成" in result.stdout + result.stderr
    assert "继续安装对应 Agent" not in result.stdout + result.stderr
    assert "独立的 Agent 接入安装" not in result.detail_output


def test_existing_core_summary_does_not_claim_dns_or_tun_success(tmp_path):
    installer, env, root, events = harness(tmp_path)
    for service in ("mihomo", "oxidns"):
        seed_existing_core(root, service)
    result = run(installer, env)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "全部跳过" in result.stdout
    assert "运行状态未验证" in result.stdout
    assert "DNS：未验证" in result.stdout
    assert "UDP/TCP 查询通过" not in result.stdout
    assert "TUN：已启用" not in result.stdout
    assert operational_events(events) == []


def test_tun_runtime_fallback_summary_is_clear_and_preserves_detailed_repair_help(tmp_path):
    installer, env, root, events = harness(tmp_path, NO_TUN_INTERFACE="1")
    result = run(installer, env, "--core", "mihomo")
    assert result.returncode == 0, result.stdout + result.stderr
    assert "TUN：已关闭" in result.stdout
    assert "TUN 未启用" in result.stdout + result.stderr
    assert "修复步骤见详细日志" in result.stdout + result.stderr
    assert "TUN：已启用" not in result.stdout
    assert "UDP/TCP 查询通过" in result.stdout
    assert "pct set <CTID> --devN" not in result.stdout
    assert "pct set <CTID> --devN" in result.detail_output


@pytest.mark.parametrize("target", ["usr/local/bin/mihomo", "etc/systemd/system/oxidns.service"])
def test_changed_binary_or_service_is_preserved_and_not_reinstalled(tmp_path, target):
    installer, env, root, events = harness(tmp_path)
    assert run(installer, env).returncode == 0
    path = root / target
    path.write_text("custom replacement")
    initial_events = events.read_text()
    result = run(installer, env)
    assert result.returncode == 0, result.stdout + result.stderr
    assert path.read_text() == "custom replacement"
    assert events.read_text() == initial_events


@pytest.mark.parametrize("arch,mihomo_asset,oxidns_asset", [
    ("aarch64", "mihomo-linux-arm64-v1.19.32.gz", "oxidns-aarch64-unknown-linux-musl.tar.gz"),
    ("armv7l", "mihomo-linux-armv7-v1.19.32.gz", "oxidns-armv7-unknown-linux-musleabihf.tar.gz"),
])
def test_architecture_selects_matching_official_assets(tmp_path, arch, mihomo_asset, oxidns_asset):
    installer, env, root, events = harness(tmp_path, ARCH=arch)
    result = run(installer, env)
    assert result.returncode == 0, result.stdout + result.stderr
    assert mihomo_asset in events.read_text()
    assert oxidns_asset in events.read_text()


def test_openrc_start_failure_shows_core_log_before_cleanup(tmp_path):
    installer, env, root, events = harness(tmp_path, "openrc", FAIL_SERVICE="start:oxidns")
    result = run(installer, env)
    assert result.returncode != 0
    assert "core startup error: fixture failure" in result.stderr
    assert_uninstalled(root)


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
@pytest.mark.parametrize("forwarding", ["1", "0", None])
def test_tun_ready_auto_enables_without_changing_forwarding(tmp_path, manager, forwarding):
    installer, env, root, events = harness(tmp_path, manager, FORWARDING=forwarding)
    result = run(installer, env)
    assert result.returncode == 0, result.stdout + result.stderr
    config = yaml.safe_load((root / "etc/mihomo/config.yaml").read_text())
    assert config["tun"] == {
        "enable": True, "device": "configflow-tun", "stack": "mixed",
        "auto-route": True, "auto-detect-interface": True, "auto-redirect": False,
        "dns-hijack": ["any:1053", "tcp://any:1053"],
    }
    assert config["dns"]["enable"] is True
    assert config["rules"] == ["MATCH,DIRECT"]
    assert config["proxies"] == []
    assert "已确认 Mihomo 创建并持有" in result.detail_output
    assert "安装结果：tun.enable=true" in result.detail_output
    assert "自动调整本机路由" in result.detail_output
    assert "FORBIDDEN" not in events.read_text()
    forward_file = root / "proc/sys/net/ipv4/ip_forward"
    if forwarding is None:
        assert not forward_file.exists()
        assert "IPv4 转发：状态无法读取" in result.detail_output
    else:
        assert forward_file.read_text() == forwarding + "\n"
        assert "当前为 " + forwarding in result.detail_output
    if forwarding != "1":
        assert "sysctl -w net.ipv4.ip_forward=1" in result.detail_output
        assert "99-configflow-forward.conf" in result.detail_output
        assert "仅做二层网桥" in result.detail_output
    assert not (root / "etc/sysctl.d").exists()


@pytest.mark.parametrize("options,reason", [
    ({"TUN_DEVICE": "missing"}, "未提供"),
    ({"TUN_DEVICE": "regular"}, "不是字符设备"),
    ({"TUN_DEVICE": "denied"}, "无法读写打开"),
    ({"DEVICE_NUMBER": "a:3"}, "无法确认"),
    ({"DEVICE_NUMBER": ""}, "无法确认"),
    ({"NO_STAT": True}, "缺少 stat"),
    ({"CAP_STATUS": None}, "无法读取或解析"),
    ({"CAP_STATUS": "CapEff:\t1000\n"}, "无法读取或解析"),
    ({"CAP_STATUS": "CapEff:\tXXXX\nCapBnd:\t1000\n"}, "无法读取或解析"),
    ({"CAP_STATUS": "CapEff:\t10000000000000000\nCapBnd:\t1000\n"}, "无法读取或解析"),
    ({"CAP_STATUS": "CapEff:\t0\nCapBnd:\t1000\n"}, "缺少有效 CAP_NET_ADMIN"),
    ({"CAP_STATUS": "CapEff:\t1000\nCapBnd:\t0\n"}, "能力上限 CapBnd"),
    ({"EXISTING_TUN_INTERFACE": True}, "接口 configflow-tun 已存在"),
])
def test_unavailable_tun_installs_dns_and_prints_repair_steps(tmp_path, options, reason):
    installer, env, root, events = harness(tmp_path, **options)
    result = run(installer, env)
    assert result.returncode == 0, result.stdout + result.stderr
    config = yaml.safe_load((root / "etc/mihomo/config.yaml").read_text())
    assert config["tun"]["enable"] is False
    assert config["dns"]["enable"] is True
    assert reason in result.detail_output
    assert "PVE 宿主机" in result.detail_output
    assert "pct set <CTID> --devN" in result.detail_output
    assert "面板的 Mihomo 基础配置中设置 tun.enable: true" in result.detail_output
    assert "首次从面板发布前" in result.detail_output
    assert "发布以面板配置为准" in result.detail_output
    assert "安装结果：tun.enable=false" in result.detail_output
    assert events.read_text().count("dig ") == 4
    assert "FORBIDDEN" not in events.read_text()
    assert "已确认 Mihomo 创建并持有" not in result.detail_output
    assert (root / "etc/oxidns/.configflow-core-install").exists()


@pytest.mark.parametrize("cap_status", [
    "CapEff:\t00000000FFFFFFFF\nCapBnd:\t00000000FFFFFFFF\n",
    "CapEff:\t1000\nCapBnd:\t1000\n",
    "CapEff:\t000000000000B000\nCapBnd:\t000000000000D000\n",
])
def test_capability_probe_supports_uppercase_and_32bit_masks(tmp_path, cap_status):
    installer, env, root, events = harness(tmp_path, CAP_STATUS=cap_status)
    result = run(installer, env)
    assert result.returncode == 0, result.stdout + result.stderr
    assert yaml.safe_load((root / "etc/mihomo/config.yaml").read_text())["tun"]["enable"] is True


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
@pytest.mark.parametrize("options", [
    {"NO_TUN_INTERFACE": "1"},
    {"TUN_FLAGS": "0x1002"},  # TAP is not TUN.
    {"TUN_LINK_FLAGS": "0x1000"},  # Existing TUN never came UP.
    {"TUN_OWNER_DEVICE": "another-tun"},  # Device exists but belongs elsewhere.
])
def test_runtime_tun_failure_falls_back_to_dns_with_guidance(tmp_path, manager, options):
    installer, env, root, events = harness(tmp_path, manager, **options)
    result = run(installer, env)
    assert result.returncode == 0, result.stdout + result.stderr
    config = yaml.safe_load((root / "etc/mihomo/config.yaml").read_text())
    assert config["tun"]["enable"] is False
    assert config["dns"]["enable"] is True
    assert config["dns"]["listen"] == "0.0.0.0:1053"
    assert "将关闭本次初始配置的 TUN，保留 DNS 服务安装" in result.detail_output
    assert "服务管理器权限或容器设备策略" in result.detail_output
    assert "安装结果：tun.enable=false" in result.detail_output
    log = events.read_text()
    stop = "systemctl stop mihomo" if manager == "systemd" else "rc-service mihomo stop"
    start = "systemctl start mihomo" if manager == "systemd" else "rc-service mihomo start"
    assert log.count(start) == 2
    assert log.count(stop) == 1
    assert log.index(start) < log.index(stop) < log.rindex(start) < log.index("dig ")
    assert log.count("dig ") == 4
    assert "FORBIDDEN" not in log
    for service in ("mihomo", "oxidns"):
        assert (root / f"etc/{service}/.configflow-core-install").exists()


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
def test_runtime_fallback_stop_failure_preserves_existing_running_config(tmp_path, manager):
    installer, env, root, events = harness(tmp_path, manager, NO_TUN_INTERFACE="1", FAIL_ROLLBACK="stop:mihomo")
    result = run(installer, env)
    assert result.returncode != 0
    assert "关闭 TUN 前停止 Mihomo 失败" in result.stderr
    assert "回滚未完成" in result.stderr
    assert yaml.safe_load((root / "etc/mihomo/config.yaml").read_text())["tun"]["enable"] is True
    assert (root / "usr/local/bin/mihomo").exists()
    assert not (root / "etc/mihomo/.configflow-core-install").exists()
    assert "dig " not in events.read_text()


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
def test_runtime_fallback_start_failure_rolls_back_installation(tmp_path, manager):
    installer, env, root, events = harness(tmp_path, manager, NO_TUN_INTERFACE="1", FAIL_FALLBACK_START="1")
    result = run(installer, env)
    assert result.returncode != 0
    assert "关闭 TUN 后 Mihomo 启动失败" in result.stderr
    assert_uninstalled(root)


def test_reinstall_does_not_enable_tun_after_device_is_fixed(tmp_path):
    installer, env, root, events = harness(tmp_path, TUN_DEVICE="missing")
    assert run(installer, env).returncode == 0
    config = root / "etc/mihomo/config.yaml"
    old_config = config.read_text()
    old_events = events.read_text()
    (root / "dev/net/tun").symlink_to("/dev/null")
    result = run(installer, env)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "跳过" in result.detail_output
    assert config.read_text() == old_config
    assert yaml.safe_load(old_config)["tun"]["enable"] is False
    assert events.read_text() == old_events


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
def test_slow_tun_start_gets_time_to_initialize(tmp_path, manager):
    installer, env, root, events = harness(tmp_path, manager, LATE_TUN_READY="3")
    result = run(installer, env)
    assert result.returncode == 0, result.stdout + result.stderr
    assert yaml.safe_load((root / "etc/mihomo/config.yaml").read_text())["tun"]["enable"] is True
    assert (root / "run/tun-wait-count").read_text() == "3"
    assert "已确认 Mihomo 创建并持有" in result.detail_output
    assert "保留 DNS 服务安装" not in result.detail_output


def test_tun_wait_is_bounded_before_dns_fallback(tmp_path):
    installer, env, root, events = harness(tmp_path, LATE_TUN_READY="99")
    result = run(installer, env)
    assert result.returncode == 0, result.stdout + result.stderr
    assert yaml.safe_load((root / "etc/mihomo/config.yaml").read_text())["tun"]["enable"] is False
    assert (root / "run/tun-wait-count").read_text() == "10"


def resolved_stub(root, answer=None):
    with (root / "proc/net/udp").open("a") as file:
        file.write("0: 3500007F:0035 00000000:0000 07\n1: 3600007F:0035 00000000:0000 07\n")
    with (root / "proc/net/tcp").open("a") as file:
        file.write("0: 3500007F:0035 00000000:0000 0A\n1: 3600007F:0035 00000000:0000 0A\n")
    if answer is not None:
        (root / "dev/tty").write_text(answer + "\n")


def test_resolved_stub_without_terminal_stops_with_the_fix(tmp_path):
    installer, env, root, events = harness(tmp_path, RESOLVED_ACTIVE="1")
    resolved_stub(root)
    result = run(installer, env, "--core", "oxidns")
    assert result.returncode != 0
    assert "没有可交互的终端" in result.stderr and "DNSStubListener=no" in result.stderr
    assert "download " not in events.read_text() and "restart systemd-resolved" not in events.read_text()
    assert not (root / "etc/systemd/resolved.conf.d").exists()
    assert_uninstalled(root)


def test_resolved_stub_disabled_after_user_confirms_then_installs(tmp_path):
    installer, env, root, events = harness(tmp_path, RESOLVED_ACTIVE="1")
    resolved_stub(root, "y")
    result = run(installer, env, "--core", "oxidns")
    assert result.returncode == 0, result.stdout + result.stderr
    assert "[y/N]" in (root / "dev/tty").read_text()
    assert (root / "etc/systemd/resolved.conf.d/configflow.conf").read_text().endswith("DNSStubListener=no\n")
    assert "systemctl restart systemd-resolved" in events.read_text()
    assert (root / "usr/local/bin/oxidns").exists()


@pytest.mark.parametrize("answer", ["", "n", "no"])
def test_resolved_stub_left_alone_unless_user_types_y(tmp_path, answer):
    installer, env, root, events = harness(tmp_path, RESOLVED_ACTIVE="1")
    resolved_stub(root, answer)
    result = run(installer, env, "--core", "oxidns")
    assert result.returncode != 0 and "已取消自动修复" in result.stderr
    assert not (root / "etc/systemd/resolved.conf.d").exists()
    assert_uninstalled(root)


@pytest.mark.parametrize("variant", ["other-listener", "resolved-inactive", "openrc"])
def test_resolved_fix_only_offered_for_a_lone_running_stub(tmp_path, variant):
    options = {} if variant == "resolved-inactive" else {"RESOLVED_ACTIVE": "1"}
    installer, env, root, events = harness(tmp_path, "openrc" if variant == "openrc" else "systemd", **options)
    resolved_stub(root, "y")
    if variant == "other-listener":
        with (root / "proc/net/udp").open("a") as file:
            file.write("2: 00000000:0035 00000000:0000 07\n")
    result = run(installer, env, "--core", "oxidns")
    assert result.returncode != 0 and "端口被占用" in result.stderr
    assert "[y/N]" not in (root / "dev/tty").read_text()
    assert not (root / "etc/systemd/resolved.conf.d").exists()
    assert_uninstalled(root)
