"""Persistent short installer links use the same validated script as the preview."""
import os
import re
import sqlite3
import subprocess
import sys

from flask import Flask
import pytest

from backend.agents import install_command_store as store
from backend.agents.core_install_script import generate_core_install_script
from backend.common import auth, config
from backend.routes.agents import bp
# These modules add routes to bp; import them before it is first registered,
# or a later register_blueprints() in the same process cannot extend it.
from backend.routes import client_tools, gateway_installations, linux_gateway_agents  # noqa: F401
from backend.routes.auth import setup_before_request


def app_client():
    app = Flask(__name__)
    app.register_blueprint(bp)
    setup_before_request(app)
    return app.test_client()


@pytest.fixture
def client(tmp_path, monkeypatch):
    monkeypatch.setattr(config, 'DATA_DIR', str(tmp_path))
    monkeypatch.setattr(auth, 'ADMIN_USERNAME', 'admin')
    monkeypatch.setattr(auth, 'ADMIN_PASSWORD', 'install-test')
    return app_client()


@pytest.fixture
def admin():
    return {'Authorization': 'Bearer ' + auth.generate_token('admin')}


def make(client, admin, **params):
    response = client.post('/api/agents/install-command', headers=admin, json={
        'name': 'gateway', 'type': 'mihomo', 'server_url': 'https://config.test/panel', **params,
    })
    assert response.status_code == 200, response.get_data(as_text=True)
    return response.get_json()


def snapshot_count():
    if not store._database_path().exists():
        return 0
    with sqlite3.connect(store._database_path()) as connection:
        return connection.execute('SELECT COUNT(*) FROM snapshots').fetchone()[0]


@pytest.mark.parametrize('path,payload', [
    ('/api/agents/install-command', {'name': 'gateway', 'type': 'mihomo'}),
    ('/api/agents/core-install-command', {'target': 'both'}),
])
def test_creation_requires_panel_auth(client, path, payload):
    assert client.post(path, json=payload).status_code == 401
    assert client.post(path, json=payload, headers={'Authorization': 'Bearer invalid'}).status_code == 401
    assert snapshot_count() == 0


@pytest.mark.parametrize('params', [
    {'type': 'mihomo', 'port': 18080},
    {'type': 'oxidns', 'port': 18082, 'install_core': True},
    {'type': 'oxidns', 'port': 18082, 'install_core': True, 'prepare_dns_ha': True},
    {'type': 'mihomo', 'port': 18080, 'install_core': True},
    {'type': 'aio', 'mihomo_agent_port': 18080, 'oxidns_agent_port': 18082},
    {'type': 'aio', 'mihomo_agent_port': 18080, 'oxidns_agent_port': 18082, 'install_core': True},
    {'type': 'mosdns', 'config_path': '/srv/mosdns/config.yaml', 'service_unit': 'custom-mosdns',
     'restart_command': 'systemctl restart custom-mosdns'},
])
def test_snapshot_matches_preview_and_legacy_validated_generator(client, admin, params):
    payload = {'name': 'gateway', 'type': 'mihomo', 'server_url': 'https://config.test/panel',
               'agent_ip': '192.0.2.3', **params}
    result = make(client, admin, **payload)
    assert re.fullmatch(r'/api/agents/i/[A-Za-z0-9_-]{22}', result['download_path'])
    query = {key: str(value).lower() if isinstance(value, bool) else value for key, value in payload.items()}
    legacy = client.get('/api/agents/install-script', query_string=query)
    assert legacy.status_code == 200
    assert result['script'] == legacy.get_data(as_text=True)
    downloaded = client.get(result['download_path'])
    assert downloaded.status_code == 200
    assert downloaded.data == result['script'].encode('utf-8')
    assert downloaded.mimetype == 'text/plain'
    assert downloaded.headers['Cache-Control'] == 'no-store'
    assert downloaded.headers['X-Content-Type-Options'] == 'nosniff'
    assert 'https://config.test/panel/api/agents/download' in result['script']


@pytest.mark.parametrize('target', ['both', 'mihomo', 'oxidns'])
def test_core_selection_is_embedded_without_moving_interface_marker(client, admin, tmp_path, target):
    response = client.post('/api/agents/core-install-command', headers=admin, json={'target': target})
    assert response.status_code == 200
    data = response.get_json()
    lines = data['script'].splitlines(keepends=True)
    assert lines[1] == '# ConfigFlow core installer interface: 1\n'
    assert lines[2] == f'set -- --core {target}\n'
    assert ''.join(lines[:2] + lines[3:]) == generate_core_install_script()
    assert client.get(data['download_path']).data == data['script'].encode()
    script = tmp_path / 'selected-core.sh'
    script.write_text(data['script'])
    assert subprocess.run(['sh', '-n', str(script)], capture_output=True).returncode == 0


def test_snapshots_survive_new_app_and_separate_process(client, admin, tmp_path):
    result = make(client, admin)
    assert app_client().get(result['download_path']).data == result['script'].encode()
    token = result['download_path'].rsplit('/', 1)[1]
    script = subprocess.check_output([
        sys.executable, '-c',
        'from backend.agents.install_command_store import load_install_script; '
        'import sys; sys.stdout.write(load_install_script(sys.argv[1]))', token,
    ], env={**os.environ, 'DATA_DIR': str(tmp_path)})
    assert script == result['script'].encode()
    assert store._database_path().stat().st_mode & 0o777 == 0o600
    assert store._database_path().parent.stat().st_mode & 0o777 == 0o700


def test_link_expires_at_24_hours_and_is_pruned(client, admin, monkeypatch):
    now = store.time.time()
    monkeypatch.setattr(store.time, 'time', lambda: now)
    result = make(client, admin)
    assert result['expires_at'] == int(now) + 86400
    monkeypatch.setattr(store.time, 'time', lambda: result['expires_at'] - 1)
    assert client.get(result['download_path']).status_code == 200
    monkeypatch.setattr(store.time, 'time', lambda: result['expires_at'])
    expired = client.get(result['download_path'])
    assert expired.status_code == 404
    assert expired.headers['Cache-Control'] == 'no-store'
    assert snapshot_count() == 0


def test_scoped_links_can_expire_earlier_without_extending_installation_capabilities(client, monkeypatch):
    now = int(store.time.time())
    monkeypatch.setattr(store.time, 'time', lambda: now)
    token, expires = store.save_install_script('#!/bin/sh\nexit 0\n', expires_at=now + 60)
    assert expires == now + 60
    monkeypatch.setattr(store.time, 'time', lambda: now + 59)
    assert store.load_install_script(token)
    monkeypatch.setattr(store.time, 'time', lambda: now + 60)
    assert store.load_install_script(token) is None


@pytest.mark.parametrize('deadline', [0, -1, True, 'later', 86401])
def test_invalid_scoped_link_lifetimes_do_not_write_snapshots(client, monkeypatch, deadline):
    monkeypatch.setattr(store.time, 'time', lambda: 100)
    with pytest.raises(ValueError):
        store.save_install_script('#!/bin/sh\nexit 0\n', expires_at=deadline + 100 if type(deadline) is int else deadline)
    assert snapshot_count() == 0


def test_capacity_preserves_existing_links_and_recovers_after_expiry(client, admin, monkeypatch):
    monkeypatch.setattr(store, 'MAX_SNAPSHOTS', 2)
    now = store.time.time()
    monkeypatch.setattr(store.time, 'time', lambda: now)
    first, second = make(client, admin), make(client, admin)
    assert first['download_path'] != second['download_path']
    full = client.post('/api/agents/install-command', headers=admin, json={'name': 'next'})
    assert full.status_code == 503
    assert snapshot_count() == 2
    assert client.get(first['download_path']).status_code == 200
    monkeypatch.setattr(store.time, 'time', lambda: first['expires_at'])
    make(client, admin)
    assert snapshot_count() == 1


@pytest.mark.parametrize('payload', [
    [], {'script': '#!/bin/sh\necho untrusted'}, {'port': 'oops'}, {'port': True}, {'port': 65536},
    {'port': 12.5}, {'name': {}}, {'install_core': 'true'}, {'name': 'bad name'},
    {'name': 'x' * 4097}, {'type': 'unsupported'}, {'agent_type': 'unknown'},
    {'type': 'mosdns', 'install_core': True},
    {'type': 'mihomo', 'install_core': True, 'port': 1053},
    {'type': 'aio', 'mihomo_agent_port': 8082, 'oxidns_agent_port': 8082},
])
def test_invalid_parameters_never_create_snapshot(client, admin, payload):
    assert client.post('/api/agents/install-command', headers=admin, json=payload).status_code == 400
    assert snapshot_count() == 0


@pytest.mark.parametrize('payload', [{}, {'target': 'bad'}, {'target': 'both', 'script': 'echo unsafe'}, []])
def test_invalid_core_parameters_never_create_snapshot(client, admin, payload):
    assert client.post('/api/agents/core-install-command', headers=admin, json=payload).status_code == 400
    assert snapshot_count() == 0


def test_request_and_script_size_are_bounded(client, admin, monkeypatch):
    assert client.post('/api/agents/install-command', headers=admin,
                       json={'name': 'x' * 33000}).status_code == 413
    monkeypatch.setattr(store, 'MAX_SCRIPT_BYTES', 10)
    assert client.post('/api/agents/install-command', headers=admin, json={}).status_code == 500
    assert snapshot_count() == 0


def test_only_exact_download_endpoint_is_public(client, admin):
    result = make(client, admin)
    for path in ['/api/agents/i/not-valid', '/api/agents/i/' + 'A' * 22]:
        assert client.get(path).status_code == 404
    assert client.get(result['download_path'] + '/extra').status_code == 401
    assert client.post(result['download_path']).status_code == 401
    assert store.load_install_script('../../config.json') is None
    assert store.load_install_script('A' * 10000) is None


def test_storage_failures_have_actionable_errors(client, admin, monkeypatch):
    def unavailable(*args):
        raise sqlite3.OperationalError('database is locked')

    monkeypatch.setattr(store, 'save_install_script', unavailable)
    response = client.post('/api/agents/install-command', headers=admin, json={})
    assert response.status_code == 500
    assert '检查数据目录' in response.get_json()['message']
    monkeypatch.setattr(store, 'load_install_script', unavailable)
    response = client.get('/api/agents/i/' + 'A' * 22)
    assert response.status_code == 503
    assert '请稍后重试' in response.get_data(as_text=True)
    assert response.headers['Cache-Control'] == 'no-store'


@pytest.mark.parametrize('value', ['true', 1, None, [], {}])
def test_ha_preparation_requires_a_json_boolean(client, admin, value):
    response = client.post('/api/agents/install-command', headers=admin, json={
        'name': 'ha-node', 'type': 'oxidns', 'prepare_dns_ha': value,
    })
    assert response.status_code == 400
    assert snapshot_count() == 0


@pytest.mark.parametrize('service', ['mihomo', 'mosdns'])
def test_legacy_shell_agent_is_no_longer_generated(client, service):
    # The script-only Agent was removed; the public endpoint must say how to move on.
    response = client.get('/api/agents/install-script', query_string={
        'name': 'old', 'type': service, 'agent_type': 'shell', 'restart_command': 'x "$(id)"'})
    assert response.status_code == 400
    message = response.get_json()['message']
    assert '旧版 Shell Agent' in message and 'Go Agent' in message
    assert '$(id)' not in response.get_data(as_text=True)
