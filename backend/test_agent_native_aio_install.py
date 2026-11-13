"""Native cohost installer checks using isolated files and mocked services."""
import json
import os
from pathlib import Path
import subprocess
import sys

import pytest

from backend.agents.go_install_script import generate_go_agent_install_script, generate_go_aio_install_script


def generated(**kwargs):
    return generate_go_aio_install_script(
        "https://config.test", "cohost", binary_download_url="https://config.test/download", **kwargs)


def configs(script):
    return {service: json.loads(script.split(f"<<'CONFIGFLOW_{service.upper()}_JSON_EOF'\n", 1)[1]
                               .split(f"\nCONFIGFLOW_{service.upper()}_JSON_EOF", 1)[0])
            for service in ("mihomo", "oxidns")}


@pytest.mark.parametrize("kwargs", [
    {"mihomo_agent_port": 0}, {"oxidns_agent_port": 65536}, {"mihomo_agent_port": True},
    {"mihomo_agent_port": 8080.5}, {"oxidns_agent_port": 8080},
    {"mihomo_config_path": "relative/config.yaml"}, {"mihomo_config_path": "/"},
    {"mihomo_config_path": "/etc/../oxidns/config.yaml"},
    {"mihomo_config_path": "/etc/oxidns/config.yaml"},
    {"mihomo_config_path": "/etc/oxidns/other.yaml"},
    {"mihomo_service_unit": "oxidns.service"}, {"mihomo_service_unit": "configflow-agent"},
    {"mihomo_config_path": "/opt/configflow-agent/mihomo/core.yaml"},
    {"oxidns_config_path": "//opt/configflow-agent/oxidns/core.yaml"},
    {"mihomo_service_binary": "/opt/configflow-agent/mihomo/core"},
    {"oxidns_service_binary": "/usr/../opt/configflow-agent/oxidns/core"},
    {"manage_ipv4_forwarding": None}, {"manage_ipv4_forwarding": 1},
    {"manage_tun": None}, {"manage_tun": 1},
])
def test_native_aio_rejects_conflicting_or_invalid_options(kwargs):
    with pytest.raises(ValueError):
        generated(**kwargs)


def test_native_aio_config_and_quoted_values(tmp_path):
    literal = '/etc/mihomo/custom ${HOME} "quoted".yaml'
    script = generated(mihomo_config_path=literal, mihomo_service_unit="mihomo-custom.service")
    cfgs = configs(script)
    assert cfgs["mihomo"]["config_path"] == literal
    assert cfgs["mihomo"]["service_unit"] == "mihomo-custom"
    for service, port in (("mihomo", 8080), ("oxidns", 8082)):
        cfg = cfgs[service]
        assert cfg["agent_port"] == port
        assert cfg["agent_name"] == f"cohost-{service}"
        assert cfg["native_aio"] is True
        assert cfg["agent_service_unit"] == f"configflow-agent-{service}"
        assert cfg["service_manager"] == "__CONFIGFLOW_INIT__"
    path = tmp_path / "install.sh"
    path.write_text(script)
    result = subprocess.run(["sh", "-n", str(path)], text=True, capture_output=True)
    assert result.returncode == 0, result.stderr


def test_native_aio_allows_core_paths_with_similar_directory_prefix():
    cfg = configs(generated(mihomo_config_path="/opt/configflow-agent-core/mihomo/config.yaml",
                            mihomo_service_binary="/opt/configflow-agent-core/mihomo/core"))["mihomo"]
    assert cfg["config_path"] == "/opt/configflow-agent-core/mihomo/config.yaml"


FORWARD_MARKER = "# Managed by ConfigFlow Shell AIO IPv4 forwarding v1"
FORWARD_CONFIG = "etc/sysctl.d/99-configflow-aio-forward.conf"
FORWARD_HELPER = "agent/mihomo/enable-ipv4-forwarding.sh"


def harness(tmp_path, manager="systemd", existing=True, **options):
    source = generated(**options)
    if sys.platform == "darwin":
        source = source.replace("sed -i ", "sed -i '' ")
    for old, new in (
        ("/etc/", str(tmp_path / "etc") + "/"),
        ("/usr/local/bin", str(tmp_path / "local-bin")),
        ("/opt/configflow-agent", str(tmp_path / "agent")),
        ("/var/log", str(tmp_path / "logs")),
        ("/dev/tty", "/dev/stdin"),
        ("/proc/sys/net/ipv4/ip_forward", str(tmp_path / "ip_forward")),
        # The installer prefers a running systemd; never read the test host's.
        ("/run/systemd/system", str(tmp_path / "run-systemd")),
        # A supervised OpenRC Agent is confirmed through its child process.
        ("/proc/[0-9]*/status", str(tmp_path / "agent-process")),
    ):
        source = source.replace(old, new)
    (tmp_path / "agent-process").write_text("PPid:\tmissing\n")
    if manager == "systemd":
        (tmp_path / "run-systemd").mkdir()
    installer = tmp_path / "install.sh"
    installer.write_text(source)
    commands = tmp_path / "commands"
    commands.mkdir()
    (tmp_path / "ip_forward").write_text("0\n")
    events = tmp_path / "events"
    agent = tmp_path / "fake-agent"
    # These subprocess mocks use only stdlib; do not initialize site per call.
    agent.write_text(f'''#!{sys.executable} -S
import json, os, sys
args = sys.argv[1:]
if args in (['-h'], ['-version']):
    raise SystemExit(0)
cfg = json.load(open(args[args.index('-config') + 1]))
with open(os.environ['EVENTS'], 'a') as log:
    log.write('agent ' + cfg['service_type'] + ' ' + args[-1] + ' ' + args[1] + '\\n')
if '-prepare-tun' in args:
    assert cfg['service_type'] == 'mihomo', 'Only the Mihomo candidate prepares TUN'
    candidate = args[args.index('-config') + 1]
    assert '.aio-install.' in candidate and candidate.endswith('/mihomo-config.json')
    with open(os.environ['EVENTS'], 'a') as log:
        log.write('prepare-tun:' + cfg['service_type'] + ':' + candidate + '\\n')
    if os.environ.get('TUN_PREPARE_FAIL') == 'yes':
        print('TUN 自动准备失败：请检查容器设备和 CAP_NET_ADMIN 权限。', file=sys.stderr)
        raise SystemExit(51)
    raise SystemExit(0)
if '-check-environment' in args:
    if os.environ.get('VERBOSE_ENVIRONMENT') == 'yes':
        print('verbose-environment-detail-for-log-only')
    assert '-environment-peer-config' in args, 'AIO must inspect both services together'
    peer_path = args[args.index('-environment-peer-config') + 1]
    peer = json.load(open(peer_path))
    assert peer['service_type'] == 'oxidns'
    assert peer['service_manager'] in ('systemd', 'openrc')
    assert cfg['service_manager'] == peer['service_manager']
    assert cfg['agent_port'] != peer['agent_port']
    previous = open(os.environ['EVENTS']).read()
    assert 'agent mihomo -validate-service' in previous
    assert 'agent oxidns -validate-service' in previous
    with open(os.environ['EVENTS'], 'a') as log:
        log.write('environment peer oxidns ' + peer_path + '\\n')
    if os.environ.get('FAIL_ENVIRONMENT') == 'yes':
        print('IPv4 转发未开启；修复建议：确认网关用途后配置持久化转发。', file=sys.stderr)
        raise SystemExit(48)
if cfg['service_type'] == os.environ.get('FAIL_VALIDATION') and '-validate-service' in args:
    raise SystemExit(46)
if cfg['service_type'] == os.environ.get('FAIL_JOURNAL') and '-require-clean-deployments' in args:
    raise SystemExit(47)
if cfg['service_type'] == os.environ.get('FAIL_JOURNAL_AFTER_STOP') and '-require-clean-deployments' in args:
    if 'stop' in open(os.environ['EVENTS']).read():
        raise SystemExit(47)
''')
    agent.chmod(0o755)
    manager_mock = '''printf '%s %s\n' "${0##*/}" "$*" >> "$EVENTS"
case "$*" in
    *--property=LoadState*) printf 'loaded\n' ;;
esac
case "${0##*/}:$*" in
    systemctl:"start configflow-agent-"*|rc-service:configflow-agent-*" start")
        if [ "${0##*/}" = systemctl ]; then service="$2"; else service="$1"; fi
        [ "${FAIL_AGENT_START:-}" != "$service" ] || exit 49
        if [ "$service" = configflow-agent-mihomo ] && [ -f "$FORWARD_HELPER" ]; then
            /bin/sh "$FORWARD_HELPER"
        fi
        if [ "$service" = configflow-agent-oxidns ] && [ "${BLOCK_FORWARD_PERSIST:-}" = yes ]; then
            mkdir -p "$FORWARD_CONFIG"
        fi
        if [ "$service" = configflow-agent-oxidns ] && [ "${RACE_GATEWAY_RECORD:-}" = start ]; then
            printf '{"owner":"gateway"}\n' > "$GATEWAY_RECORD"
        fi
        ;;
esac
case "${0##*/}:$*" in
    systemctl:"stop configflow-agent-mihomo"|rc-service:"configflow-agent-mihomo stop")
        if [ "${RACE_GATEWAY_RECORD:-}" = stop ]; then
            printf '{"owner":"gateway"}\n' > "$GATEWAY_RECORD"
        fi
        ;;
esac
'''
    sysctl_mock = f'''#!{sys.executable} -S
import os, sys
from pathlib import Path
args = sys.argv[1:]
with open(os.environ['EVENTS'], 'a') as log:
    log.write('sysctl ' + ' '.join(args) + '\\n')
current = Path(os.environ['FORWARD_PROC'])
if args in (['-n', 'net.ipv4.ip_forward'], ['net.ipv4.ip_forward']):
    value = current.read_text().strip()
    print(value if '-n' in args else 'net.ipv4.ip_forward = ' + value)
    raise SystemExit(0)
if args not in (['-q', '-w', 'net.ipv4.ip_forward=0'], ['-q', '-w', 'net.ipv4.ip_forward=1']):
    raise SystemExit('Unexpected sysctl command in isolated installer test: ' + repr(args))
value = args[-1].rsplit('=', 1)[1]
if os.environ.get('FAIL_SYSCTL_WRITES') == 'yes' or (value == '1' and os.environ.get('FAIL_SYSCTL_ENABLE') == 'yes'):
    print('sysctl: permission denied on key net.ipv4.ip_forward', file=sys.stderr)
    raise SystemExit(50)
if os.environ.get('SYSCTL_WRITE_NO_EFFECT') != 'yes':
    current.write_text(value + '\\n')
if value == '1' and os.environ.get('DELETE_FORWARD_CONFIG_ON_ENABLE') == 'yes':
    Path(os.environ['FORWARD_CONFIG']).unlink(missing_ok=True)
'''
    mocks = {"id": "printf '0\\n'\n", "uname": "printf 'x86_64\\n'\n",
             "wget": 'cp "$FAKE_AGENT" "$3"\n', "mihomo": "exit 0\n", "oxidns": "exit 0\n",
             "sysctl": sysctl_mock,
             "modprobe": 'printf "forbidden-modprobe %s\\n" "$*" >> "$EVENTS"\nexit 97\n',
             "mknod": 'printf "forbidden-mknod %s\\n" "$*" >> "$EVENTS"\nexit 97\n'}
    if manager == "systemd":
        mocks["systemctl"] = manager_mock
        units = tmp_path / "etc/systemd/system"
    else:
        mocks["rc-service"] = manager_mock
        mocks["rc-update"] = manager_mock
        units = tmp_path / "etc/init.d"
    units.mkdir(parents=True)
    mocks["sleep"] = "exit 0\n"  # The installer waits before confirming a started Agent.
    for name, body in mocks.items():
        executable = commands / name
        executable.write_text(body if body.startswith("#!") else "#!/bin/sh\nset -eu\n" + body)
        executable.chmod(0o755)
    cfgs = configs(source)
    for service in ("mihomo", "oxidns"):
        core = Path(cfgs[service]["config_path"])
        core.parent.mkdir(parents=True)
        core.write_text("# unchanged core config\n")
        core_unit = units / (service + (".service" if manager == "systemd" else ""))
        core_unit.write_text("# original core unit\n")
        if existing:
            directory = tmp_path / "agent" / service
            directory.mkdir(parents=True)
            cfg = dict(cfgs[service], agent_id=f"id-{service}", token=f"token-{service}")
            (directory / "config.json").write_text(json.dumps(cfg))
            binary = directory / "configflow-agent"
            binary.write_bytes(agent.read_bytes())
            binary.chmod(0o755)
            (directory / "keep-state").write_text(service)
            (units / (f"configflow-agent-{service}" + (".service" if manager == "systemd" else ""))).write_text("# old Agent unit\n")
    env = dict(os.environ, PATH=str(commands) + os.pathsep + os.environ["PATH"],
               EVENTS=str(events), FAKE_AGENT=str(agent), CONFIGFLOW_INSTALL_LOG_DIR=str(tmp_path),
               FORWARD_PROC=str(tmp_path / "ip_forward"), FORWARD_CONFIG=str(tmp_path / FORWARD_CONFIG),
               FORWARD_HELPER=str(tmp_path / FORWARD_HELPER),
               GATEWAY_RECORD=str(tmp_path / "agent/mihomo/linux-gateway.json"))
    return installer, env, units


def run(installer, env, choice="1"):
    return subprocess.run(["sh", str(installer)], input=choice + "\n", env=env, text=True, capture_output=True, timeout=30)


def detail_output(result):
    path = next(line.removeprefix("完整日志：") for line in result.stdout.splitlines()
                if line.startswith("完整日志："))
    return Path(path).read_text()


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
@pytest.mark.parametrize("failure", ["FAIL_VALIDATION", "FAIL_JOURNAL"])
def test_second_core_preflight_failure_leaves_both_agents_untouched(tmp_path, manager, failure):
    installer, env, units = harness(tmp_path, manager)
    before = {path: path.read_bytes() for path in (tmp_path / "agent").rglob("*") if path.is_file()}
    unit_before = {path: path.read_bytes() for path in units.iterdir() if path.is_file()}
    env[failure] = "oxidns"
    result = run(installer, env)
    assert result.returncode in (46, 47), result.stderr
    for path, data in {**before, **unit_before}.items():
        assert path.read_bytes() == data
    events = (tmp_path / "events").read_text().splitlines()
    assert any("agent mihomo -validate-service" in line for line in events)
    assert not any("stop" in line or "start" in line or "daemon-reload" in line for line in events)
    assert not list((tmp_path / "agent").glob(".aio-install.*"))
    assert (tmp_path / "ip_forward").read_text() == "0\n"
    assert not (tmp_path / FORWARD_CONFIG).exists()
    assert not any(line.startswith("sysctl ") for line in events)
    assert not any(line.startswith("prepare-tun:") for line in events)


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
def test_native_aio_environment_failure_preserves_both_agents_and_prints_guidance(tmp_path, manager):
    installer, env, units = harness(tmp_path, manager)
    before = {path: path.read_bytes() for path in (tmp_path / "agent").rglob("*") if path.is_file()}
    unit_before = {path: path.read_bytes() for path in units.iterdir() if path.is_file()}
    env["FAIL_ENVIRONMENT"] = "yes"
    result = run(installer, env)
    assert result.returncode == 48, result.stderr
    assert "IPv4 转发" in result.stderr and "修复建议" in result.stderr
    assert "未知 -check-environment 参数" in result.stderr
    assert "1.8.19-go 或更高" in result.stderr
    for path, data in {**before, **unit_before}.items():
        assert path.read_bytes() == data
    for service in ("mihomo", "oxidns"):
        assert (tmp_path / "etc" / service / "config.yaml").read_text() == "# unchanged core config\n"
    events = (tmp_path / "events").read_text().splitlines()
    checks = [i for i, line in enumerate(events) if " -check-environment " in line]
    assert len(checks) == 1
    for service in ("mihomo", "oxidns"):
        validation = next(i for i, line in enumerate(events) if f"agent {service} -validate-service" in line)
        assert validation < checks[0]
    assert sum(line.startswith("environment peer oxidns ") for line in events) == 1
    assert not any("stop" in line or "start" in line or "daemon-reload" in line for line in events)
    assert not list((tmp_path / "agent").glob(".aio-install.*"))
    assert (tmp_path / "ip_forward").read_text() == "0\n"
    assert not (tmp_path / FORWARD_CONFIG).exists()
    assert not any(line.startswith("sysctl ") for line in events)


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
@pytest.mark.parametrize("existing", [False, True])
def test_native_aio_install_isolated_identity_and_repeatable(tmp_path, manager, existing):
    installer, env, units = harness(tmp_path, manager, existing)
    for attempt in range(2):
        (tmp_path / "events").write_text("")
        result = run(installer, env)
        assert result.returncode == 0, result.stderr
        for service in ("mihomo", "oxidns"):
            directory = tmp_path / "agent" / service
            cfg = json.loads((directory / "config.json").read_text())
            assert cfg["service_manager"] == manager
            assert cfg["agent_id"] == (f"id-{service}" if existing else "")
            assert cfg["token"] == (f"token-{service}" if existing else "")
            assert cfg["agent_service_unit"] == f"configflow-agent-{service}"
            assert (directory / "configflow-agent").is_file()
            assert Path(cfg["config_path"]).read_text() == "# unchanged core config\n"
            if existing:
                assert (directory / "keep-state").read_text() == service
        events = (tmp_path / "events").read_text().splitlines()
        # Look only at the latest attempt; both preflights precede either stop.
        end_checks = [i for i, line in enumerate(events) if "agent oxidns -require-clean-deployments" in line and ".aio-install." in line]
        stops = [i for i, line in enumerate(events) if "stop" in line and "configflow-agent" in line]
        environment_checks = [i for i, line in enumerate(events) if " -check-environment " in line]
        tun_checks = [i for i, line in enumerate(events) if line.startswith("prepare-tun:")]
        assert len(environment_checks) == 1
        assert len(tun_checks) == 1
        assert tun_checks[0] < environment_checks[0]
        assert sum(line.startswith("environment peer oxidns ") for line in events) == 1
        for service in ("mihomo", "oxidns"):
            validation = next(i for i, line in enumerate(events) if f"agent {service} -validate-service" in line)
            assert validation < tun_checks[0]
            journal_checks = [i for i, line in enumerate(events[:tun_checks[0]])
                              if f"agent {service} -require-clean-deployments" in line]
            assert len(journal_checks) == (2 if existing or attempt else 1)
        if existing or attempt:
            assert end_checks[0] < stops[0]
            assert end_checks[-1] > stops[-1]
            assert environment_checks[0] < stops[0]
        assert not any(line in {f"systemctl stop {service}", f"rc-service {service} stop"}
                       for service in ("mihomo", "oxidns") for line in events)
        assert not (tmp_path / "agent/config-mihomo.json").exists()
        assert not (tmp_path / "local-bin/configflow-agent").exists()
        assert not any(line.startswith("forbidden-") for line in events)


def test_native_aio_summary_preserves_existing_core_dns_guidance_without_verbose_output(tmp_path):
    installer, env, _ = harness(tmp_path)
    env["VERBOSE_ENVIRONMENT"] = "yes"
    result = run(installer, env)
    assert result.returncode == 0, result.stdout + result.stderr
    assert "请输入选项 [1-3]: " in result.stdout
    assert "检查安装环境与依赖" in result.stdout
    assert "检查核心接管与下载 Agent" in result.stdout
    assert "检查运行环境" in result.stdout
    assert "安装并启动 Agent 服务" in result.stdout
    assert "Mihomo Agent：端口 8080，服务已启动" in result.stdout
    assert "OxiDNS Agent：端口 8082，服务已启动" in result.stdout
    assert "已有核心配置已保留；发布也会保留自定义 DNS 端口和 dns-hijack" in result.stdout + result.stderr
    assert "先在 ConfigFlow 发布" not in result.stdout
    assert "安装完成" in result.stdout + result.stderr and "完整日志：" in result.stdout
    assert "verbose-environment-detail-for-log-only" not in result.stdout + result.stderr
    assert "verbose-environment-detail-for-log-only" in detail_output(result)


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
@pytest.mark.parametrize("initial_forwarding", ["0", "1"])
def test_native_aio_tun_preparation_failure_preserves_agents_and_forwarding(tmp_path, manager, initial_forwarding):
    installer, env, units = harness(tmp_path, manager)
    (tmp_path / "ip_forward").write_text(initial_forwarding + "\n")
    before = {path: path.read_bytes() for directory in (tmp_path / "agent", units)
              for path in directory.rglob("*") if path.is_file()}
    result = run(installer, dict(env, TUN_PREPARE_FAIL="yes"))
    assert result.returncode == 51, result.stdout + result.stderr
    assert "TUN" in result.stderr
    assert "安装完成" not in result.stdout
    assert "IPv4 转发已开启并持久化" not in result.stdout + result.stderr + detail_output(result)
    for path, content in before.items():
        assert path.read_bytes() == content
    for service in ("mihomo", "oxidns"):
        assert (tmp_path / "etc" / service / "config.yaml").read_text() == "# unchanged core config\n"
    events = (tmp_path / "events").read_text().splitlines()
    assert sum(line.startswith("prepare-tun:mihomo:") for line in events) == 1
    assert not any(" -check-environment " in line for line in events)
    assert not any("stop" in line or "start" in line or "daemon-reload" in line for line in events)
    assert not any(line.startswith(("sysctl ", "forbidden-")) for line in events)
    assert (tmp_path / "ip_forward").read_text() == initial_forwarding + "\n"
    assert not (tmp_path / FORWARD_CONFIG).exists()
    assert not (tmp_path / FORWARD_HELPER).exists()
    assert not list((tmp_path / "agent").glob(".aio-install.*"))


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
def test_native_aio_can_skip_tun_preparation_while_managing_ipv4_forwarding(tmp_path, manager):
    installer, env, _ = harness(tmp_path, manager, manage_tun=False)
    assert "-prepare-tun" not in installer.read_text()
    result = run(installer, dict(env, TUN_PREPARE_FAIL="yes"))
    assert result.returncode == 0, result.stdout + result.stderr
    events = (tmp_path / "events").read_text()
    assert "prepare-tun" not in events
    assert "forbidden-" not in events
    assert (tmp_path / "ip_forward").read_text() == "1\n"
    assert FORWARD_MARKER in (tmp_path / FORWARD_CONFIG).read_text()


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
def test_native_aio_uninstall_preserves_other_directories_and_cores(tmp_path, manager):
    installer, env, units = harness(tmp_path, manager)
    sibling = tmp_path / "agent/unrelated/keep"
    sibling.parent.mkdir()
    sibling.write_text("keep")
    result = run(installer, env, "2")
    assert result.returncode == 0, result.stderr
    assert sibling.read_text() == "keep"
    for service in ("mihomo", "oxidns"):
        assert not (tmp_path / "agent" / service).exists()
        assert (tmp_path / "etc" / service / "config.yaml").read_text() == "# unchanged core config\n"
        assert (units / (service + (".service" if manager == "systemd" else ""))).read_text() == "# original core unit\n"
    events = (tmp_path / "events").read_text()
    assert "recover-and-exit" not in events
    assert "-check-environment" not in events
    assert "prepare-tun" not in events
    assert events.index("agent oxidns -require-clean-deployments") < events.index("stop")


def test_native_aio_uninstall_blocks_pending_second_core(tmp_path):
    installer, env, _ = harness(tmp_path)
    env["FAIL_JOURNAL"] = "oxidns"
    result = run(installer, env, "2")
    assert result.returncode == 47
    assert (tmp_path / "agent/mihomo/config.json").exists()
    assert (tmp_path / "agent/oxidns/config.json").exists()
    assert "stop" not in (tmp_path / "events").read_text()


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
@pytest.mark.parametrize("choice", ["1", "2"])
def test_racing_publication_restarts_original_agents_without_replacing_files(tmp_path, manager, choice):
    installer, env, units = harness(tmp_path, manager)
    before = {path: path.read_bytes() for path in (tmp_path / "agent").rglob("*") if path.is_file()}
    unit_before = {path: path.read_bytes() for path in units.iterdir() if path.is_file()}
    env["FAIL_JOURNAL_AFTER_STOP"] = "oxidns"
    result = run(installer, env, choice)
    assert result.returncode == 47, result.stderr
    for path, data in {**before, **unit_before}.items():
        assert path.read_bytes() == data
    events = (tmp_path / "events").read_text().splitlines()
    for service in ("mihomo", "oxidns"):
        stop = f"systemctl stop configflow-agent-{service}" if manager == "systemd" else f"rc-service configflow-agent-{service} stop"
        start = f"systemctl start configflow-agent-{service}" if manager == "systemd" else f"rc-service configflow-agent-{service} start"
        assert events.index(stop) < events.index(start)
    assert (tmp_path / "ip_forward").read_text() == "0\n"
    assert not (tmp_path / FORWARD_CONFIG).exists()
    assert "sysctl -q -w net.ipv4.ip_forward=1" not in events


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
@pytest.mark.parametrize("initial_value", ["0", "1"])
def test_native_aio_enables_and_persists_forwarding_after_both_agents_start(tmp_path, manager, initial_value):
    installer, env, units = harness(tmp_path, manager)
    proc = tmp_path / "ip_forward"
    proc.write_text(initial_value + "\n")
    unrelated = tmp_path / "etc/sysctl.d/20-existing.conf"
    unrelated.parent.mkdir(parents=True)
    unrelated.write_text("# administrator-owned\nnet.ipv6.conf.all.forwarding=0\n")
    persistent = tmp_path / FORWARD_CONFIG
    for attempt in range(2):
        (tmp_path / "events").write_text("")
        result = run(installer, env)
        assert result.returncode == 0, result.stdout + result.stderr
        assert "IPv4 转发已开启并持久化" in result.stdout
        assert "IPv4 转发已开启并持久化" in detail_output(result)
        assert proc.read_text() == "1\n"
        content = persistent.read_text()
        assert content.splitlines().count(FORWARD_MARKER) == 1
        assert content.splitlines().count("net.ipv4.ip_forward=1") == 1
        assert unrelated.read_text() == "# administrator-owned\nnet.ipv6.conf.all.forwarding=0\n"
        events = (tmp_path / "events").read_text().splitlines()
        environment = next(i for i, line in enumerate(events) if " -check-environment " in line)
        writes = [i for i, line in enumerate(events) if line.startswith("sysctl -q -w ")]
        assert writes and environment < writes[0]
        if initial_value == "0" and attempt == 0:
            enable = events.index("sysctl -q -w net.ipv4.ip_forward=1")
            for service in ("mihomo", "oxidns"):
                start = (f"systemctl start configflow-agent-{service}" if manager == "systemd"
                         else f"rc-service configflow-agent-{service} start")
                assert events.index(start) < enable
        assert not list((tmp_path / "agent").glob(".aio-install.*"))
    helper = tmp_path / FORWARD_HELPER
    assert helper.is_file()
    suffix = ".service" if manager == "systemd" else ""
    mihomo_unit = (units / ("configflow-agent-mihomo" + suffix)).read_text()
    oxidns_unit = (units / ("configflow-agent-oxidns" + suffix)).read_text()
    assert str(helper) in mihomo_unit
    assert ("ExecStartPre=/bin/sh " in mihomo_unit if manager == "systemd" else "start_pre()" in mihomo_unit)
    assert "enable-ipv4-forwarding.sh" not in oxidns_unit
    # Simulate a reboot resetting the kernel knob, then run the installed hook.
    proc.write_text("0\n")
    (tmp_path / "events").write_text("")
    restarted = subprocess.run(["/bin/sh", str(helper)], env=env, text=True, capture_output=True, timeout=10)
    assert restarted.returncode == 0, restarted.stdout + restarted.stderr
    assert proc.read_text() == "1\n"
    assert "sysctl -q -w net.ipv4.ip_forward=1" in (tmp_path / "events").read_text()
    (tmp_path / "events").write_text("")
    already_enabled = subprocess.run(["/bin/sh", str(helper)], env=env, text=True, capture_output=True, timeout=10)
    assert already_enabled.returncode == 0, already_enabled.stderr
    assert " -w " not in (tmp_path / "events").read_text()


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
def test_forwarding_permission_probe_failure_leaves_services_and_forwarding_untouched(tmp_path, manager):
    installer, env, units = harness(tmp_path, manager)
    before = {path: path.read_bytes() for directory in (tmp_path / "agent", units)
              for path in directory.rglob("*") if path.is_file()}
    result = run(installer, dict(env, FAIL_SYSCTL_WRITES="yes"))
    assert result.returncode != 0
    assert "安装完成" not in result.stdout
    assert "IPv4 转发已开启并持久化" not in result.stdout + result.stderr + detail_output(result)
    assert (tmp_path / "ip_forward").read_text() == "0\n"
    assert not (tmp_path / FORWARD_CONFIG).exists()
    for path, content in before.items():
        assert path.read_bytes() == content
    events = (tmp_path / "events").read_text().splitlines()
    assert "sysctl -q -w net.ipv4.ip_forward=0" in events
    assert not any("stop" in line or "start" in line or "daemon-reload" in line for line in events)


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
def test_forwarding_prepare_rejects_blocked_persistent_directory_without_enabling(tmp_path, manager):
    installer, env, _ = harness(tmp_path, manager)
    blocked = tmp_path / "etc/sysctl.d"
    blocked.write_text("not a directory\n")
    result = run(installer, env)
    assert result.returncode != 0
    assert "安装完成" not in result.stdout
    assert "IPv4 转发已开启并持久化" not in result.stdout + result.stderr + detail_output(result)
    assert blocked.read_text() == "not a directory\n"
    assert (tmp_path / "ip_forward").read_text() == "0\n"
    events = (tmp_path / "events").read_text()
    assert "sysctl -q -w net.ipv4.ip_forward=1" not in events
    assert " stop " not in events and " start " not in events


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
def test_agent_start_failure_does_not_commit_forwarding_and_can_retry(tmp_path, manager):
    installer, env, _ = harness(tmp_path, manager)
    result = run(installer, dict(env, FAIL_AGENT_START="configflow-agent-oxidns"))
    assert result.returncode != 0
    assert "安装完成" not in result.stdout
    assert "IPv4 转发已开启并持久化" not in result.stdout + result.stderr + detail_output(result)
    assert not (tmp_path / FORWARD_CONFIG).exists()
    assert (tmp_path / "ip_forward").read_text() == "0\n"
    assert "sysctl -q -w net.ipv4.ip_forward=1" not in (tmp_path / "events").read_text()
    # A hook may already be installed, but cannot enable forwarding before commit.
    helper = tmp_path / FORWARD_HELPER
    assert helper.is_file()
    skipped = subprocess.run(["/bin/sh", str(helper)], env=env, text=True, capture_output=True, timeout=10)
    assert skipped.returncode == 0, skipped.stderr
    assert (tmp_path / "ip_forward").read_text() == "0\n"
    retried = run(installer, env)
    assert retried.returncode == 0, retried.stdout + retried.stderr
    assert (tmp_path / "ip_forward").read_text() == "1\n"
    assert FORWARD_MARKER in (tmp_path / FORWARD_CONFIG).read_text()


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
def test_persistent_write_race_does_not_report_success_or_enable_forwarding(tmp_path, manager):
    installer, env, _ = harness(tmp_path, manager)
    result = run(installer, dict(env, BLOCK_FORWARD_PERSIST="yes"))
    assert result.returncode != 0
    assert "安装完成" not in result.stdout
    assert "IPv4 转发已开启并持久化" not in result.stdout + result.stderr + detail_output(result)
    assert (tmp_path / "ip_forward").read_text() == "0\n"
    assert not (tmp_path / FORWARD_CONFIG).is_file()


@pytest.mark.parametrize("failure", ["FAIL_SYSCTL_ENABLE", "SYSCTL_WRITE_NO_EFFECT"])
def test_forwarding_final_enable_failure_or_failed_readback_cannot_report_success(tmp_path, failure):
    installer, env, _ = harness(tmp_path)
    result = run(installer, dict(env, **{failure: "yes"}))
    assert result.returncode != 0
    assert "安装完成" not in result.stdout
    assert "IPv4 转发已开启并持久化" not in result.stdout + result.stderr + detail_output(result)
    assert (tmp_path / "ip_forward").read_text() == "0\n"
    assert not (tmp_path / FORWARD_CONFIG).exists()
    assert not list((tmp_path / "etc/sysctl.d").glob(".configflow-aio-forward.*"))
    assert not list((tmp_path / "agent").glob(".aio-install.*"))
    assert "sysctl -q -w net.ipv4.ip_forward=1" in (tmp_path / "events").read_text()


def test_forwarding_config_removed_during_enable_does_not_report_success_or_disable_runtime(tmp_path):
    installer, env, _ = harness(tmp_path)
    result = run(installer, dict(env, DELETE_FORWARD_CONFIG_ON_ENABLE="yes"))
    assert result.returncode != 0
    assert "安装完成" not in result.stdout
    assert "IPv4 转发已开启并持久化" not in result.stdout + result.stderr + detail_output(result)
    assert not (tmp_path / FORWARD_CONFIG).exists()
    # Runtime was enabled before the concurrent unlink; cleanup must not toggle it off.
    assert (tmp_path / "ip_forward").read_text() == "1\n"
    writes = [line for line in (tmp_path / "events").read_text().splitlines()
              if line.startswith("sysctl -q -w ")]
    assert writes == ["sysctl -q -w net.ipv4.ip_forward=0", "sysctl -q -w net.ipv4.ip_forward=1"]
    assert not list((tmp_path / "etc/sysctl.d").glob(".configflow-aio-forward.*"))


def test_forwarding_config_removed_before_noop_helper_cannot_report_success(tmp_path):
    installer, env, _ = harness(tmp_path)
    (tmp_path / "ip_forward").write_text("1\n")
    # The commit succeeds, then another writer removes it before the hook reads it.
    # This reproduces the hook's intentional missing-file/no-op exit with global1.
    link_mock = tmp_path / "commands/ln"
    link_mock.write_text('#!/bin/sh\nset -eu\n/bin/ln "$@"\n/bin/rm -f "$FORWARD_CONFIG"\n')
    link_mock.chmod(0o755)
    result = run(installer, env)
    assert result.returncode != 0
    assert "安装完成" not in result.stdout
    assert "IPv4 转发已开启并持久化" not in result.stdout + result.stderr + detail_output(result)
    assert not (tmp_path / FORWARD_CONFIG).exists()
    assert (tmp_path / "ip_forward").read_text() == "1\n"
    assert not list((tmp_path / "etc/sysctl.d").glob(".configflow-aio-forward.*"))


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
def test_native_aio_uninstall_preserves_committed_forwarding_for_running_cores(tmp_path, manager):
    installer, env, _ = harness(tmp_path, manager)
    installed = run(installer, env)
    assert installed.returncode == 0, installed.stdout + installed.stderr
    persistent = (tmp_path / FORWARD_CONFIG).read_bytes()
    (tmp_path / "events").write_text("")
    uninstalled = run(installer, env, "2")
    assert uninstalled.returncode == 0, uninstalled.stdout + uninstalled.stderr
    assert (tmp_path / "ip_forward").read_text() == "1\n"
    assert (tmp_path / FORWARD_CONFIG).read_bytes() == persistent
    assert "sysctl " not in (tmp_path / "events").read_text()
    assert "prepare-tun" not in (tmp_path / "events").read_text()


def test_native_aio_cancel_does_not_prepare_or_enable_forwarding(tmp_path):
    installer, env, _ = harness(tmp_path)
    record = tmp_path / "agent/mihomo/linux-gateway.json"
    record.write_text('{"owner":"gateway"}\n')
    result = run(installer, env, "3")
    assert result.returncode == 0, result.stderr
    assert (tmp_path / "ip_forward").read_text() == "0\n"
    assert not (tmp_path / FORWARD_CONFIG).exists()
    assert not (tmp_path / "events").exists()
    assert record.read_text() == '{"owner":"gateway"}\n'


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
@pytest.mark.parametrize("choice", ["1", "2"])
@pytest.mark.parametrize("record_kind", ["file", "symlink"])
def test_native_aio_install_and_uninstall_refuse_existing_gateway_record(tmp_path, manager, choice, record_kind):
    installer, env, _ = harness(tmp_path, manager)
    record = tmp_path / "agent/mihomo/linux-gateway.json"
    if record_kind == "symlink":
        record.symlink_to(tmp_path / "missing-gateway-record.json")
    else:
        record.write_text('{"owner":"gateway"}\n')
    result = run(installer, env, choice)
    assert result.returncode != 0
    assert "网关" in result.stderr
    if record_kind == "symlink":
        assert record.is_symlink()
    else:
        assert record.read_text() == '{"owner":"gateway"}\n'
    for service in ("mihomo", "oxidns"):
        assert (tmp_path / "agent" / service / "config.json").is_file()
    assert (tmp_path / "ip_forward").read_text() == "0\n"
    assert not (tmp_path / FORWARD_CONFIG).exists()
    events = (tmp_path / "events").read_text().splitlines() if (tmp_path / "events").exists() else []
    assert not any("stop" in line or "start" in line or line.startswith("sysctl ") for line in events)


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
@pytest.mark.parametrize("phase", ["stop", "start"])
def test_gateway_record_created_during_install_blocks_forwarding_commit(tmp_path, manager, phase):
    installer, env, _ = harness(tmp_path, manager)
    result = run(installer, dict(env, RACE_GATEWAY_RECORD=phase))
    assert result.returncode != 0
    assert "网关" in result.stderr
    assert "安装完成" not in result.stdout
    assert "IPv4 转发已开启并持久化" not in result.stdout + result.stderr + detail_output(result)
    record = tmp_path / "agent/mihomo/linux-gateway.json"
    assert record.read_text() == '{"owner":"gateway"}\n'
    assert (tmp_path / "ip_forward").read_text() == "0\n"
    assert not (tmp_path / FORWARD_CONFIG).exists()
    assert "sysctl -q -w net.ipv4.ip_forward=1" not in (tmp_path / "events").read_text()


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
def test_native_aio_refuses_forwarding_hook_symlink_without_changing_target(tmp_path, manager):
    installer, env, _ = harness(tmp_path, manager)
    target = tmp_path / "administrator-script"
    target.write_text("#!/bin/sh\nexit 71\n")
    helper = tmp_path / FORWARD_HELPER
    helper.symlink_to(target)
    result = run(installer, env)
    assert result.returncode != 0
    assert "符号链接" in result.stderr
    assert target.read_text() == "#!/bin/sh\nexit 71\n"
    assert helper.is_symlink()
    assert (tmp_path / "ip_forward").read_text() == "0\n"
    assert not (tmp_path / FORWARD_CONFIG).exists()


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
def test_native_aio_explicitly_unmanaged_forwarding_has_no_helpers_or_effects(tmp_path, manager):
    installer, env, _ = harness(tmp_path, manager, manage_ipv4_forwarding=False)
    source = installer.read_text()
    assert "enable-ipv4-forwarding.sh" not in source
    assert "99-configflow-aio-forward.conf" not in source
    result = run(installer, env)
    assert result.returncode == 0, result.stdout + result.stderr
    assert (tmp_path / "ip_forward").read_text() == "0\n"
    assert not (tmp_path / FORWARD_CONFIG).exists()
    assert not (tmp_path / FORWARD_HELPER).exists()
    assert "sysctl " not in (tmp_path / "events").read_text()


def test_physical_core_directory_alias_is_rejected_before_download(tmp_path):
    installer, env, _ = harness(tmp_path, existing=False)
    source = installer.read_text().replace(str(tmp_path / "etc/oxidns/config.yaml"), str(tmp_path / "etc/alias/oxidns.yaml"))
    installer.write_text(source)
    (tmp_path / "etc/mihomo/oxidns.yaml").write_text("# independent file but shared journal root\n")
    (tmp_path / "etc/alias").symlink_to(tmp_path / "etc/mihomo", target_is_directory=True)
    result = run(installer, env)
    assert result.returncode == 1
    assert "实际位于同一目录" in result.stderr
    assert not (tmp_path / "agent").exists()


@pytest.mark.parametrize("kind", ["config_file", "config_directory", "binary"])
def test_core_location_inside_agent_directory_is_rejected_through_symlink(tmp_path, kind):
    installer, env, _ = harness(tmp_path, existing=False)
    protected = tmp_path / "agent/mihomo"
    protected.mkdir(parents=True)
    if kind == "binary":
        target = protected / "mihomo-core"
        target.write_text("#!/bin/sh\nexit 0\n")
        target.chmod(0o755)
        link = tmp_path / "commands/mihomo"
        link.unlink()
        link.symlink_to(target)
    else:
        target = protected / "config.yaml"
        target.write_text("# core must survive\n")
        link = tmp_path / "etc/mihomo/config.yaml"
        link.unlink()
        if kind == "config_file":
            link.symlink_to(target)
        else:
            link.parent.rmdir()
            link.parent.symlink_to(protected, target_is_directory=True)
    before = target.read_bytes()
    result = run(installer, env)
    assert result.returncode == 1
    assert "不能放在 Agent 目录内" in result.stderr
    assert target.read_bytes() == before
    assert not (tmp_path / "events").exists()


def test_native_aio_refuses_legacy_before_changes(tmp_path):
    installer, env, _ = harness(tmp_path, existing=False)
    legacy = tmp_path / "agent/config-mihomo.json"
    legacy.parent.mkdir()
    legacy.write_text('{"agent_id":"legacy"}')
    result = run(installer, env)
    assert result.returncode == 1
    assert "旧版共享 Agent" in result.stderr
    assert legacy.read_text() == '{"agent_id":"legacy"}'
    assert not (tmp_path / "events").exists()


@pytest.mark.parametrize("choice", ["1", "2"])
def test_single_installer_refuses_aio_install_and_uninstall(tmp_path, choice):
    installer, env, _ = harness(tmp_path)
    script = generate_go_agent_install_script("https://config.test", "single", "mihomo")
    script = script.replace("/opt/configflow-agent", str(tmp_path / "agent"))
    script = script.replace("/dev/tty", "/dev/stdin")
    installer.write_text(script)
    original = (tmp_path / "agent/oxidns/config.json").read_bytes()
    result = run(installer, env, choice)
    assert result.returncode == 1
    assert "同机" in result.stderr
    assert (tmp_path / "agent/oxidns/config.json").read_bytes() == original
    assert not (tmp_path / "events").exists()


GO_UNIT_TESTDATA = Path(__file__).resolve().parent / "agents/go-agent/routes/testdata"


def rendered_agent_unit(manager, hook):
    """Expand the installer's Agent service heredoc exactly as the shell does."""
    script = generated()
    start = {"systemd": 'cat > "/etc/systemd/system/$AGENT_UNIT.service" <<SERVICEEOF\n',
             "openrc": 'cat > "/etc/init.d/$AGENT_UNIT" <<SERVICEEOF\n'}[manager]
    assert script.count(start) == 1
    body = script.split(start, 1)[1].split("\nSERVICEEOF\n", 1)[0]
    # Take the hook values from install_ipv4_forwarding_hook itself.
    assignments = [line.strip() for line in script.splitlines()
                   if line.strip().startswith(('IPV4_SYSTEMD_PRE="ExecStartPre', 'IPV4_OPENRC_PRE="/bin/sh'))]
    assert len(assignments) == 2
    shell = "\n".join([
        'SERVICE=mihomo', 'AGENT_UNIT=configflow-agent-mihomo', 'AGENT_DIR=/opt/configflow-agent/mihomo',
        'CONFIG_FILE="$AGENT_DIR/config.json"', 'AGENT_BINARY="$AGENT_DIR/configflow-agent"',
        'AGENT_LOG=/var/log/configflow-agent-mihomo.log', 'IPV4_SYSTEMD_PRE=""', 'IPV4_OPENRC_PRE=""',
        *(assignments if hook else []),
        "cat <<SERVICEEOF", body, "SERVICEEOF", "",
    ])
    result = subprocess.run(["sh", "-c", shell], text=True, capture_output=True, check=True)
    return result.stdout


@pytest.mark.parametrize("manager,suffix", [("systemd", "service"), ("openrc", "initd")])
@pytest.mark.parametrize("variant", ["plain", "hook"])
def test_go_agent_unit_patch_fixtures_match_installer_templates(manager, suffix, variant):
    # The Go Agent patches an installed unit in place to register the forwarding
    # hook; its fixtures must stay identical to what this installer writes.
    expected = rendered_agent_unit(manager, variant == "hook")
    fixture = GO_UNIT_TESTDATA / f"aio-unit-{manager}-{variant}.{suffix}"
    assert fixture.read_text() == expected
