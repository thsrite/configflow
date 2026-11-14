"""Installation/bootstrap regression tests for transactional Go Agent deployment."""
import json
import os
import subprocess
import sys
from pathlib import Path

import pytest
import yaml

from backend.agents.go_install_script import generate_go_agent_install_script
from backend.agents.install_script import generate_docker_agent_compose, generate_docker_agent_run

ROOT = Path(__file__).parents[1]


def generated(**kwargs):
    return generate_go_agent_install_script(
        server_url="https://config.test", agent_name="native-test", service_type=kwargs.pop("service_type", "mihomo"),
        binary_download_url="https://config.test/api/agents/download", **kwargs,
    )


def generated_config(script):
    return json.loads(script.split("<<'CONFIGFLOW_JSON_EOF'\n", 1)[1].split("\nCONFIGFLOW_JSON_EOF", 1)[0])


@pytest.mark.parametrize("service", ["mihomo", "mosdns", "oxidns"])
def test_native_install_has_explicit_lifecycle_without_boot_gate(tmp_path, service):
    script = generated(service_type=service)
    cfg = generated_config(script)
    assert cfg["service_manager"] == "__CONFIGFLOW_INIT__"
    assert cfg["service_unit"] == service
    assert cfg["service_binary"] == service
    assert cfg["deployment_method"] == "shell"
    assert "RemainAfterExit=yes" not in script
    assert f"Requires=configflow-recover-{service}.service" not in script
    assert 'rc_need="${rc_need:-} configflow-recover-' not in script
    assert "ExecStartPre=" not in script
    assert '-config "$CONFIG_FILE.new" -check-deployments' in script
    assert '-config "$CONFIG_FILE" -check-deployments' in script
    assert '"$AGENT_DIR/config.json"' in script  # legacy Shell identity migration
    assert 'mktemp /usr/local/bin/.configflow-agent.install.XXXXXX' in script
    assert 'mv "$BINARY_TMP" /usr/local/bin/configflow-agent' in script
    assert '/tmp/configflow-agent' not in script  # common noexec mount
    assert 'trap' in script
    path = tmp_path / "install.sh"
    path.write_text(script)
    result = subprocess.run(["sh", "-n", str(path)], text=True, capture_output=True)
    assert result.returncode == 0, result.stderr


def test_custom_restart_requires_explicit_lifecycle():
    with pytest.raises(ValueError, match="stop_command"):
        generated(restart_command="/custom/restart")
    cfg = generated_config(generated(
        restart_command="/custom/restart", stop_command="/custom/stop", start_command="/custom/start", status_command="/custom/status",
    ))
    assert cfg["service_manager"] == "command"
    assert cfg["stop_command"] == "/custom/stop"
    assert cfg["start_command"] == "/custom/start"


def test_installer_preserves_literal_json_and_shell_characters(tmp_path):
    # Quotes, braces, dollar signs and newlines must be data in the quoted
    # heredoc, never evaluated during installation.
    value = '/tmp/"config${HOME}{service_type}\\file\n.yaml'
    cfg = generated_config(generated(config_path=value))
    assert cfg["config_path"] == value


def native_install_harness(tmp_path, manager, service):
    source = generated(service_type=service, service_unit="new-core")
    if sys.platform == "darwin":
        source = source.replace("sed -i ", "sed -i '' ")
    for original, replacement in (
        ("/etc/", str(tmp_path / "etc") + "/"),
        ("/usr/local/bin", str(tmp_path / "local-bin")),
        ("/opt/configflow-agent", str(tmp_path / "agent")),
        ("/var/log", str(tmp_path / "logs")),
        ("/dev/tty", "/dev/stdin"),
        # The installer prefers a running systemd; never read the test host's.
        ("/run/systemd/system", str(tmp_path / "run-systemd")),
        # A supervised OpenRC Agent is confirmed through its child process.
        ("/proc/[0-9]*/status", str(tmp_path / "agent-process")),
    ):
        source = source.replace(original, replacement)
    (tmp_path / "agent-process").write_text("PPid:\tmissing\n")
    if manager == "systemd":
        (tmp_path / "run-systemd").mkdir()
    script = tmp_path / "install.sh"
    script.write_text(source)
    commands = tmp_path / "commands"
    commands.mkdir()
    agent = tmp_path / "fake-agent"
    agent.write_text('''#!/bin/sh
printf 'agent %s\n' "$*" >> "$EVENTS"
case "$*" in
    *-recover-*) exit 42 ;;
    *-validate-service*) if [ "${FAIL_VALIDATION:-}" = "yes" ]; then exit 46; fi ;;
    *-check-environment*)
        if [ "${FAIL_ENVIRONMENT:-}" = "yes" ]; then
            echo "端口已被其他进程占用；修复建议：选择空闲端口或调整冲突服务。" >&2
            exit 48
        fi
        ;;
esac
''')
    agent.chmod(0o755)
    mocks = {
        "id": "printf '0\\n'\n",
        "uname": "printf 'x86_64\\n'\n",
        "wget": 'cp "$FAKE_AGENT" "$3"\n',
    }
    manager_mock = '''printf '%s %s\n' "$(basename "$0")" "$*" >> "$EVENTS"
case "$*" in *stop*configflow-recover*|*configflow-recover*stop*|*stop*old-core*|*old-core*stop*) exit 44 ;; esac
if [ "$1" = daemon-reload ] || [ "$1" = -u ]; then
    if [ -f "$GATE_FILE" ]; then
        echo reload-gate-present >> "$EVENTS"
        for unit in "$OLD_CORE_FILE" "$OLD_DEPENDENCY_FILE" "$OLD_AGENT_FILE"; do
            if [ -f "$unit" ] && grep -q "configflow-recover-$SERVICE" "$unit"; then
                echo "dependency still present: $unit" >&2
                exit 45
            fi
        done
    else
        echo reload-gate-absent >> "$EVENTS"
    fi
fi
'''
    if manager == "systemd":
        mocks["systemctl"] = manager_mock
        units = tmp_path / "etc/systemd/system"
        units.mkdir(parents=True)
        gate = units / f"configflow-recover-{service}.service"
        core = units / "old-core.service"
        core.write_text(f"[Unit]\nRequires=network.target configflow-recover-{service}.service\n[Service]\nExecStart=/usr/bin/core\n")
        dropin = units / "old-core.service.d/configflow-recovery.conf"
        dropin.parent.mkdir()
        dropin.write_text(f"[Unit]\nAfter=configflow-recover-{service}.service \\\n local-fs.target\n[Service]\nEnvironment=KEEP=1\n")
        agent_unit = units / "configflow-agent.service"
        agent_unit.write_text(f"[Unit]\nRequires=configflow-recover-{service}.service\nAfter=network.target configflow-recover-{service}.service\n")
        agent_dropin = units / "configflow-agent.service.d/custom.conf"
        agent_dropin.parent.mkdir()
        agent_dropin.write_text(f"[Unit]\nRequires=keep.service configflow-recover-other.service configflow-recover-{service}.service\n[Service]\nEnvironment=EXTRA=1\n")
        new_core_dropin = units / "new-core.service.d/configflow-recovery.conf"
        new_core_dropin.parent.mkdir()
        new_core_dropin.write_text(f"[Unit]\nRequires=configflow-recover-{service}.service\nAfter=configflow-recover-{service}.service\n")
    else:
        mocks["rc-service"] = manager_mock
        mocks["rc-update"] = manager_mock
        units = tmp_path / "etc/init.d"
        units.mkdir(parents=True)
        conf = tmp_path / "etc/conf.d"
        conf.mkdir()
        gate = units / f"configflow-recover-{service}"
        core = units / "old-core"
        core.write_text(f'#!/sbin/openrc-run\ndepend() {{\n need localmount configflow-recover-{service}\n}}\ncommand="/usr/bin/core"\n')
        dropin = conf / "old-core"
        dropin.write_text(f'CUSTOM=yes\n# BEGIN CONFIGFLOW RECOVERY {service}\nrc_need="${{rc_need:-}} configflow-recover-{service}"\n# END CONFIGFLOW RECOVERY {service}\n')
        agent_unit = units / "configflow-agent"
        agent_unit.write_text(f'#!/sbin/openrc-run\ndepend() {{\n need net configflow-recover-{service}\n}}\n')
        agent_dropin = conf / "configflow-agent"
        agent_dropin.write_text(f'rc_need="keep configflow-recover-other configflow-recover-{service}"\nCUSTOM=yes\n')
    gate.write_text("old recovery service\n")
    mocks["sleep"] = "exit 0\n"  # The installer waits before confirming a started Agent.
    for name, body in mocks.items():
        executable = commands / name
        executable.write_text("#!/bin/sh\nset -eu\n" + body)
        executable.chmod(0o755)
    existing_config = tmp_path / "agent" / f"config-{service}.json"
    existing_config.parent.mkdir()
    existing_config.write_text(json.dumps({"service_unit": "old-core.service", "service_type": service, "agent_name": "native-test", "agent_id": "existing-id", "token": "existing-token"}))
    env = dict(os.environ, PATH=str(commands) + os.pathsep + os.environ["PATH"], EVENTS=str(tmp_path / "events"), FAKE_AGENT=str(agent), GATE_FILE=str(gate), OLD_CORE_FILE=str(core), OLD_DEPENDENCY_FILE=str(dropin), OLD_AGENT_FILE=str(agent_unit), SERVICE=service)
    return script, env, gate, core, dropin, agent_dropin, existing_config


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
@pytest.mark.parametrize("service", ["mihomo", "mosdns", "oxidns"])
def test_native_reinstall_safely_detaches_old_gate_and_is_repeatable(tmp_path, manager, service):
    script, env, gate, core, dropin, agent_dropin, config = native_install_harness(tmp_path, manager, service)
    for _ in range(2):
        result = subprocess.run(["sh", str(script)], input="1\n", env=env, text=True, capture_output=True)
        assert result.returncode == 0, result.stderr
        assert not gate.exists()
        for file in (core, dropin, agent_dropin):
            assert f"configflow-recover-{service}" not in file.read_text()
        assert "/usr/bin/core" in core.read_text()
        assert "localmount" in core.read_text() if manager == "openrc" else "network.target" in core.read_text()
        assert "CUSTOM=yes" in dropin.read_text() if manager == "openrc" else "Environment=KEEP=1" in dropin.read_text()
        assert "keep" in agent_dropin.read_text()
        assert "configflow-recover-other" in agent_dropin.read_text()
        if manager == "systemd":
            assert not (gate.parent / "new-core.service.d/configflow-recovery.conf").exists()
        cfg = json.loads(config.read_text())
        assert cfg["agent_id"] == "existing-id"
        assert cfg["token"] == "existing-token"
    events = (tmp_path / "events").read_text().splitlines()
    assert events.index("reload-gate-present") < events.index("reload-gate-absent")
    assert not any("stop" in event and f"configflow-recover-{service}" in event for event in events)
    assert not any("stop" in event and "old-core" in event for event in events)
    assert any("-check-deployments" in event for event in events)
    validation = next(i for i, event in enumerate(events) if "-validate-service" in event)
    environment = next(i for i, event in enumerate(events) if "-check-environment" in event)
    stop = next(i for i, event in enumerate(events) if "stop" in event and "configflow-agent" in event)
    assert validation < environment < stop
    assert sum("-check-environment" in event for event in events) == 2
    assert not any(" -recover-" in event for event in events)


def test_docker_generated_volumes_persist_identity_and_transaction_roots():
    compose = yaml.safe_load(generate_docker_agent_compose("https://config.test", enable_mosdns=True))
    volumes = compose["services"]["agent"]["volumes"]
    assert "./agent_data/agent:/opt/configflow-agent" in volumes
    assert "./agent_data/mihomo:/etc/mihomo" in volumes
    assert "./agent_data/mosdns:/etc/mosdns" in volumes
    command = generate_docker_agent_run("https://config.test", enable_mosdns=True)
    assert "./agent_data/agent:/opt/configflow-agent" in command
    assert "./agent_data/mihomo:/etc/mihomo" in command
    assert "/root/.config/mihomo" not in command


def entrypoint_harness(tmp_path):
    source = (ROOT / "docker/docker-agent-entrypoint-service.sh").read_text()
    if sys.platform == "darwin":
        source = source.replace("sed -i ", "sed -i '' ")
    source = source.replace("/etc/", str(tmp_path / "etc") + "/")
    source = source.replace("/opt/configflow-agent", str(tmp_path / "agent"))
    source = source.replace("/usr/local/bin/configflow-agent", str(tmp_path / "agent-binary"))
    source = source.replace("/var/log/supervisor/", str(tmp_path / "logs") + "/")
    for service in ("mihomo", "mosdns", "oxidns"):
        directory = tmp_path / "etc" / service
        directory.mkdir(parents=True)
    supervisor = tmp_path / "etc/supervisor/conf.d"
    supervisor.mkdir(parents=True)
    for name in ("mihomo", "mosdns", "oxidns", "agent-mihomo", "agent-mosdns", "agent-oxidns"):
        (supervisor / (name + ".conf")).write_text("command=old\nautostart=false\n")
    binary = tmp_path / "agent-binary"
    # An unavailable Agent must not block Supervisor or either managed core.
    binary.write_text('#!/bin/sh\nprintf "unexpected agent call: %s\\n" "$*" >> "$EVENTS"\nexit 42\n')
    binary.chmod(0o755)
    entrypoint = tmp_path / "entrypoint.sh"
    entrypoint.write_text(source)
    env = dict(os.environ, SERVER_URL='https://config.test/"literal', ENABLE_MIHOMO="true", ENABLE_MOSDNS="true", ENABLE_OXIDNS="false", EVENTS=str(tmp_path / "events"), CONFIG_ROOT=str(tmp_path / "etc"))
    env.pop("CONFIG_PATH", None)
    env.pop("SERVICE_TYPE", None)
    return entrypoint, env


def test_docker_boot_starts_supervisor_without_recovery_gate(tmp_path):
    entrypoint, env = entrypoint_harness(tmp_path)
    result = subprocess.run(["sh", str(entrypoint), "sh", "-c", 'echo supervisor >> "$EVENTS"'], env=env, text=True, capture_output=True)
    assert result.returncode == 0, result.stderr
    events = (tmp_path / "events").read_text().splitlines()
    assert events == ["supervisor"]
    mihomo = (tmp_path / "etc/mihomo/config.yaml").read_text()
    assert "listen: 0.0.0.0:1053" in mihomo
    cfg = json.loads((tmp_path / "agent/config-mihomo.json").read_text())
    assert cfg["server_url"] == env["SERVER_URL"]
    assert cfg["service_manager"] == "supervisor"
    assert cfg["deployment_method"] == "docker"


@pytest.mark.parametrize("service", ["mihomo", "mosdns", "oxidns"])
def test_docker_existing_core_config_survives_unavailable_agent(tmp_path, service):
    entrypoint, env = entrypoint_harness(tmp_path)
    env["SERVICE_TYPE"] = service
    config = tmp_path / "etc" / service / "config.yaml"
    config.write_text("# existing service configuration\n")
    result = subprocess.run(["sh", str(entrypoint), "sh", "-c", 'echo supervisor >> "$EVENTS"'], env=env, text=True, capture_output=True)
    assert result.returncode == 0, result.stderr
    assert config.read_text() == "# existing service configuration\n"
    assert (tmp_path / "events").read_text().splitlines() == ["supervisor"]


@pytest.mark.parametrize("service", ["mihomo", "mosdns", "oxidns"])
@pytest.mark.parametrize("mode", ["single", "aio"])
def test_docker_pending_deployment_preserves_missing_config_without_blocking_other_core(tmp_path, service, mode):
    entrypoint, env = entrypoint_harness(tmp_path)
    if mode == "single":
        env["SERVICE_TYPE"] = service
    elif service == "oxidns":
        env.update(ENABLE_OXIDNS="true", ENABLE_MOSDNS="false")
    root = tmp_path / "etc" / service
    transaction = root / ".configflow-deployments" / "interrupted"
    transaction.mkdir(parents=True)
    journal = transaction / "state.json"
    journal.write_text('{"phase":"applying"}\n')
    runtime = root / "state"
    runtime.mkdir()
    learned = runtime / "learned.txt"
    recorder = runtime / "queries.db"
    learned.write_bytes(b"learned rule\n")
    recorder.write_bytes(b"existing recorder database")
    other_service = "mosdns" if service == "mihomo" else "mihomo"
    result = subprocess.run(["sh", str(entrypoint), "sh", "-c", 'echo supervisor >> "$EVENTS"'], env=env, text=True, capture_output=True)
    assert result.returncode == 0, result.stderr
    assert "preserving files for manual recovery" in result.stdout
    assert not (root / "config.yaml").exists()
    assert journal.read_text() == '{"phase":"applying"}\n'
    assert learned.read_bytes() == b"learned rule\n"
    assert recorder.read_bytes() == b"existing recorder database"
    supervisor = tmp_path / "etc/supervisor/conf.d"
    assert "autostart=true" in (supervisor / f"{service}.conf").read_text()
    if mode == "aio":
        assert (tmp_path / "etc" / other_service / "config.yaml").exists()
        assert "autostart=true" in (supervisor / f"{other_service}.conf").read_text()
    assert (tmp_path / "events").read_text().splitlines() == ["supervisor"]


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
def test_oxidns_invalid_working_directory_leaves_existing_agent_running(tmp_path, manager):
    script, env, gate, core, dropin, agent_dropin, config = native_install_harness(tmp_path, manager, "oxidns")
    original_config = config.read_bytes()
    original_core = core.read_bytes()
    env["FAIL_VALIDATION"] = "yes"
    result = subprocess.run(["sh", str(script)], input="1\n", env=env, text=True, capture_output=True)
    assert result.returncode == 46, result.stderr
    assert gate.exists()
    assert config.read_bytes() == original_config
    assert core.read_bytes() == original_core
    events = (tmp_path / "events").read_text().splitlines()
    assert any("-validate-service" in event for event in events)
    assert not any("stop" in event or "reload" in event for event in events)


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
@pytest.mark.parametrize("service", ["mihomo", "mosdns", "oxidns"])
def test_native_environment_failure_keeps_existing_agent_and_core(tmp_path, manager, service):
    script, env, gate, core, dropin, agent_dropin, config = native_install_harness(tmp_path, manager, service)
    before = {path: path.read_bytes() for path in (config, core, dropin, agent_dropin, gate)}
    env["FAIL_ENVIRONMENT"] = "yes"
    result = subprocess.run(["sh", str(script)], input="1\n", env=env, text=True, capture_output=True)
    assert result.returncode == 48, result.stderr
    assert "端口" in result.stderr and "修复建议" in result.stderr
    assert "未知 -check-environment 参数" in result.stderr
    assert "1.8.19-go 或更高" in result.stderr
    for path, data in before.items():
        assert path.read_bytes() == data
    events = (tmp_path / "events").read_text().splitlines()
    validation = next(i for i, event in enumerate(events) if "-validate-service" in event)
    checks = [i for i, event in enumerate(events) if "-check-environment" in event]
    assert len(checks) == 1 and validation < checks[0]
    assert not any("stop" in event or "start" in event or "reload" in event for event in events)
    assert not list((tmp_path / "local-bin").glob(".configflow-agent.install.*"))


@pytest.mark.parametrize("mode", ["single", "aio"])
def test_oxidns_interrupted_boot_does_not_create_runtime_state_directory(tmp_path, mode):
    entrypoint, env = entrypoint_harness(tmp_path)
    root = tmp_path / "etc/oxidns"
    if mode == "single":
        root = root / "custom-root"
        env.update(SERVICE_TYPE="oxidns", CONFIG_PATH=str(root / "config.yaml"))
    else:
        env.update(ENABLE_OXIDNS="true", ENABLE_MOSDNS="false")
    transaction = root / ".configflow-deployments/interrupted"
    transaction.mkdir(parents=True)
    journal = transaction / "state.json"
    journal.write_text('{"phase":"applying"}\n')
    result = subprocess.run(["sh", str(entrypoint), "sh", "-c", 'echo supervisor >> "$EVENTS"'], env=env, text=True, capture_output=True)
    assert result.returncode == 0, result.stderr
    assert not (root / "config.yaml").exists()
    assert not (root / "state").exists()
    assert journal.read_text() == '{"phase":"applying"}\n'
    supervisor = tmp_path / "etc/supervisor/conf.d/oxidns.conf"
    assert "autostart=true" in supervisor.read_text()
    assert f"-d {root}" in supervisor.read_text()
    assert (tmp_path / "events").read_text().splitlines() == ["supervisor"]


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
def test_native_reinstall_preserves_unaffected_vendor_unit_symlink(tmp_path, manager):
    script, env, gate, core, dropin, agent_dropin, config = native_install_harness(tmp_path, manager, "oxidns")
    vendor = tmp_path / "vendor-unit"
    # No final newline: an untouched file must stay byte-for-byte unchanged.
    original = b"[Unit]\nAfter=network.target\n[Service]\nExecStart=/usr/bin/core" if manager == "systemd" else b'#!/sbin/openrc-run\ncommand="/usr/bin/core"'
    vendor.write_bytes(original)
    core.unlink()
    core.symlink_to(vendor)
    inode = core.lstat().st_ino
    for _ in range(2):
        result = subprocess.run(["sh", str(script)], input="1\n", env=env, text=True, capture_output=True)
        assert result.returncode == 0, result.stderr
        assert core.is_symlink()
        assert core.lstat().st_ino == inode
        assert core.readlink() == vendor
        assert vendor.read_bytes() == original
        assert not gate.exists()


@pytest.mark.parametrize("manager", ["systemd", "openrc"])
def test_native_reinstall_refuses_to_rewrite_vendor_dependency_symlink(tmp_path, manager):
    script, env, gate, core, dropin, agent_dropin, config = native_install_harness(tmp_path, manager, "oxidns")
    vendor = tmp_path / "vendor-unit"
    original = core.read_bytes()
    vendor.write_bytes(original)
    core.unlink()
    core.symlink_to(vendor)
    result = subprocess.run(["sh", str(script)], input="1\n", env=env, text=True, capture_output=True)
    assert result.returncode != 0
    assert "恢复依赖位于符号链接服务文件" in result.stderr
    assert core.is_symlink()
    assert core.readlink() == vendor
    assert vendor.read_bytes() == original
    assert gate.exists()
