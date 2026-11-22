"""Allocator defaults apply before app startup without changing other services."""
import configparser
import json
import os
from pathlib import Path
import re
import subprocess
import sys
from unittest.mock import Mock

import pytest

from backend import startup


def test_production_default_does_not_mutate_parent_environment(monkeypatch):
    monkeypatch.setattr(startup, 'uses_glibc', lambda: True)
    original = {'DATA_DIR': '/data', 'PYTHONUNBUFFERED': '1'}
    child = startup.application_environment(original)
    assert child == {**original, 'MALLOC_ARENA_MAX': '4',
                     'MALLOC_TRIM_THRESHOLD_': '1048576'}
    assert original == {'DATA_DIR': '/data', 'PYTHONUNBUFFERED': '1'}


@pytest.mark.parametrize('settings', [
    {'MALLOC_ARENA_MAX': '2'},
    {'MALLOC_ARENA_MAX': '0'},
    {'MALLOC_ARENA_MAX': ''},
    {'GLIBC_TUNABLES': 'glibc.malloc.arena_max=2'},
    {'GLIBC_TUNABLES': 'glibc.malloc.arena_max='},
    {'GLIBC_TUNABLES': 'glibc.malloc.arena_max'},
    {'GLIBC_TUNABLES': 'glibc.malloc.arena_max=2:glibc.malloc.arena_max=3'},
    {'GLIBC_TUNABLES': 'glibc.malloc.arena_max=3', 'MALLOC_ARENA_MAX': '2'},
])
def test_explicit_arena_settings_are_preserved_verbatim(monkeypatch, settings):
    monkeypatch.setattr(startup, 'uses_glibc', lambda: True)
    assert startup.application_environment(settings) == {
        **settings, 'MALLOC_TRIM_THRESHOLD_': '1048576',
    }


@pytest.mark.parametrize('settings', [
    {'MALLOC_TRIM_THRESHOLD_': '2097152'},
    {'MALLOC_TRIM_THRESHOLD_': '0'},
    {'MALLOC_TRIM_THRESHOLD_': ''},
    {'GLIBC_TUNABLES': 'glibc.malloc.trim_threshold=2097152'},
    {'GLIBC_TUNABLES': 'glibc.malloc.trim_threshold='},
    {'GLIBC_TUNABLES': 'glibc.malloc.trim_threshold'},
    {'GLIBC_TUNABLES': 'glibc.malloc.trim_threshold=2097152:glibc.malloc.trim_threshold=524288'},
    {'GLIBC_TUNABLES': 'glibc.malloc.trim_threshold=2097152', 'MALLOC_TRIM_THRESHOLD_': '524288'},
])
def test_explicit_trim_settings_are_preserved_verbatim(monkeypatch, settings):
    monkeypatch.setattr(startup, 'uses_glibc', lambda: True)
    assert startup.application_environment(settings) == {
        **settings, 'MALLOC_ARENA_MAX': '4',
    }


@pytest.mark.parametrize('settings', [
    {'MALLOC_ARENA_MAX': '2', 'MALLOC_TRIM_THRESHOLD_': '2097152'},
    {'MALLOC_ARENA_MAX': '', 'MALLOC_TRIM_THRESHOLD_': ''},
    {'GLIBC_TUNABLES': 'glibc.malloc.trim_threshold=2097152:glibc.malloc.arena_max=2'},
    {'MALLOC_ARENA_MAX': '2', 'GLIBC_TUNABLES': 'glibc.malloc.trim_threshold=2097152'},
    {'MALLOC_TRIM_THRESHOLD_': '2097152', 'GLIBC_TUNABLES': 'glibc.malloc.arena_max=2'},
])
def test_both_settings_can_be_overridden_independently(monkeypatch, settings):
    monkeypatch.setattr(startup, 'uses_glibc', lambda: True)
    assert startup.application_environment(settings) == settings


def test_other_allocator_settings_remain_unchanged(monkeypatch):
    monkeypatch.setattr(startup, 'uses_glibc', lambda: True)
    original = {
        'GLIBC_TUNABLES': 'glibc.malloc.trim_threshold=1048576:glibc.malloc.tcache_count=4',
        'MALLOC_TRIM_THRESHOLD_': '524288',
        'MALLOC_MMAP_THRESHOLD_': '262144',
        'MALLOC_TOP_PAD_': '65536',
        'PYTHONMALLOC': 'malloc',
    }
    assert startup.application_environment(original) == {
        **original, 'MALLOC_ARENA_MAX': '4',
    }


@pytest.mark.parametrize('settings', [
    {'CONFIGFLOW_MEMORY_TUNING': '0'},
    {'LOG_LEVEL': 'DEBUG'},
    {'LOG_LEVEL': 'debug'},
    {'LOG_LEVEL': 'Debug', 'MALLOC_ARENA_MAX': '2'},
    {'LOG_LEVEL': 'DEBUG', 'MALLOC_TRIM_THRESHOLD_': '2097152'},
    {'CONFIGFLOW_MEMORY_TUNING': '0', 'MALLOC_ARENA_MAX': '2'},
    {'CONFIGFLOW_MEMORY_TUNING': '0', 'MALLOC_TRIM_THRESHOLD_': ''},
])
def test_opt_out_and_debug_preserve_existing_environment(monkeypatch, settings):
    monkeypatch.setattr(startup, 'uses_glibc', lambda: True)
    assert startup.application_environment(settings) == settings


def test_non_glibc_does_not_get_defaults(monkeypatch):
    monkeypatch.setattr(startup, 'uses_glibc', lambda: False)
    assert startup.application_environment({'DATA_DIR': '/data'}) == {'DATA_DIR': '/data'}


@pytest.mark.parametrize('platform,version,expected', [
    ('linux', 'glibc 2.41', True),
    ('linux', 'musl 1.2.5', False),
    ('linux', None, False),
    ('darwin', 'glibc 2.41', False),
    ('win32', 'glibc 2.41', False),
])
def test_libc_detection(monkeypatch, platform, version, expected):
    monkeypatch.setattr(startup.sys, 'platform', platform)
    monkeypatch.setattr(startup.os, 'confstr', lambda key: version, raising=False)
    assert startup.uses_glibc() is expected


@pytest.mark.parametrize('error', [AttributeError(), OSError(), ValueError()])
def test_unavailable_libc_detection_is_safe(monkeypatch, error):
    monkeypatch.setattr(startup.sys, 'platform', 'linux')
    monkeypatch.setattr(startup.os, 'confstr', Mock(side_effect=error), raising=False)
    assert startup.uses_glibc() is False


def test_exec_keeps_interpreter_arguments_and_environment(monkeypatch):
    monkeypatch.setattr(startup, 'uses_glibc', lambda: True)
    monkeypatch.setattr(startup.sys, 'executable', '/opt/python/bin/python3')
    monkeypatch.setattr(startup.sys, 'argv', ['startup.py', '--example', 'literal value'])
    monkeypatch.setattr(startup.os, 'environ', {'DATA_DIR': '/data'})
    execute = Mock()
    monkeypatch.setattr(startup.os, 'execve', execute)
    startup.main()
    execute.assert_called_once_with(
        '/opt/python/bin/python3',
        ['/opt/python/bin/python3', '-m', 'backend.app', '--example', 'literal value'],
        {'DATA_DIR': '/data', 'MALLOC_ARENA_MAX': '4',
         'MALLOC_TRIM_THRESHOLD_': '1048576'},
    )


@pytest.mark.skipif(os.name != 'posix', reason='The Docker launcher uses POSIX exec')
@pytest.mark.parametrize('installed_launcher', [False, True])
def test_real_exec_preserves_pid_and_exit_status_without_double_app_start(tmp_path, installed_launcher):
    package = tmp_path / 'backend'
    package.mkdir()
    (package / '__init__.py').write_text('')
    if installed_launcher:
        # Auto-update replaces backend/, including with older cached sources
        # that have app.py but no startup.py. The image's launcher must survive.
        bin_dir = tmp_path / 'bin'
        bin_dir.mkdir()
        launcher = bin_dir / 'configflow-python-start.py'
        launcher.write_text(Path(startup.__file__).read_text())
        command = [sys.executable, str(launcher), 'literal value']
        assert not (package / 'startup.py').exists()
    else:
        (package / 'startup.py').write_text(Path(startup.__file__).read_text())
        command = [sys.executable, '-m', 'backend.startup', 'literal value']
    (package / 'app.py').write_text(
        'import json, os, sys\n'
        'print(json.dumps({"pid": os.getpid(), "args": sys.argv[1:], '
        '"arena": os.environ["MALLOC_ARENA_MAX"], '
        '"trim": os.environ["MALLOC_TRIM_THRESHOLD_"], '
        '"executable": sys.executable}))\n'
        'sys.exit(23)\n'
    )
    process = subprocess.Popen(
        command,
        cwd=tmp_path,
        env={**os.environ, 'MALLOC_ARENA_MAX': '7', 'MALLOC_TRIM_THRESHOLD_': '2097152'},
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    try:
        stdout, stderr = process.communicate(timeout=10)
    finally:
        if process.poll() is None:
            process.kill()
            process.communicate()
    assert process.returncode == 23, stderr
    assert json.loads(stdout) == {
        'pid': process.pid,
        'args': ['literal value'],
        'arena': '7',
        'trim': '2097152',
        'executable': sys.executable,
    }



def test_image_installs_launcher_outside_the_replaceable_sources():
    dockerfile = (Path(__file__).resolve().parents[1] / 'docker' / 'Dockerfile').read_text()
    # The launcher may be staged under /rootfs and copied to / as one layer.
    assert re.search(r'^COPY backend/startup\.py (?:/rootfs)?/usr/local/bin/configflow-python-start\.py$', dockerfile, re.M)
    assert 'COPY --link --from=app /rootfs/ /' in dockerfile
