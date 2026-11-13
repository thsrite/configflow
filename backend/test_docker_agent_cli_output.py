"""Sandboxed CLI reporting checks; no host services or containers are changed."""

import os
import subprocess
from pathlib import Path

import pytest

from backend.agents.install_script import generate_docker_agent_run


def _write_command(directory, name, body):
    path = directory / name
    path.write_text("#!/bin/sh\n" + body, encoding="utf-8")
    path.chmod(0o755)


def _run(script, env, choice="1", verbose=False):
    args = ["sh", str(script)] + (["--verbose"] if verbose else [])
    return subprocess.run(args, input=choice + "\n", env=env, text=True, capture_output=True, timeout=15)


def _log(tmp_path):
    logs = list((tmp_path / "logs").glob("configflow-install.*/install.log"))
    assert len(logs) == 1
    assert logs[0].parent.stat().st_mode & 0o777 == 0o700
    assert logs[0].stat().st_mode & 0o777 == 0o600
    return logs[0], logs[0].read_text(encoding="utf-8")


@pytest.mark.parametrize("verbose,exit_code", [(False, 0), (True, 0), (False, 37)])
def test_docker_run_uses_same_reporting_and_preserves_docker_exit(tmp_path, verbose, exit_code):
    script = tmp_path / "docker-agent.sh"
    script.write_text(generate_docker_agent_run("https://config.example", "docker-test"))
    commands = tmp_path / "bin"
    commands.mkdir()
    _write_command(commands, "docker", f"printf 'DOCKER_PROGRESS container-id\\n'\nexit {exit_code}\n")
    env = os.environ.copy()
    env.update(PATH=f"{commands}{os.pathsep}{env['PATH']}", CONFIGFLOW_LOG_DIR=str(tmp_path / "logs"))
    for name in ("CONFIGFLOW_CLI_ACTIVE", "CONFIGFLOW_LOG_CURSOR", "CONFIGFLOW_LOG_FILE", "CONFIGFLOW_VERBOSE"):
        env.pop(name, None)
    syntax = subprocess.run(["sh", "-n", str(script)], text=True, capture_output=True)
    assert syntax.returncode == 0, syntax.stderr
    result = _run(script, env, verbose=verbose)
    assert result.returncode == exit_code
    screen = result.stdout + result.stderr
    assert ("DOCKER_PROGRESS" in screen) is (verbose or exit_code != 0)
    log_path, logs = _log(tmp_path)
    assert "DOCKER_PROGRESS" in logs
    assert str(log_path) in screen
