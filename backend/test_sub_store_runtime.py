"""Lifecycle regressions: no idle shutdown during work, polling or updates."""
from concurrent.futures import ThreadPoolExecutor
from contextlib import contextmanager
import multiprocessing
from threading import Event
import time
from types import SimpleNamespace

import pytest
from flask import Flask

from backend.common import config
from backend.common.config_repository import ProfileRepository
from backend.utils import dependencies, sub_store_client as client, sub_store_runtime as lifecycle


@pytest.fixture
def service(tmp_path, monkeypatch):
    manager = lifecycle.SubStoreRuntime(tmp_path / 'runtime', idle_seconds=0)
    mode = {'value': 'on_demand'}
    process = {'running': False, 'starts': 0, 'stops': 0}
    monkeypatch.setattr(dependencies, 'is_builtin_available', lambda: True)
    monkeypatch.setattr(lifecycle, 'get_mode', lambda: mode['value'])
    monkeypatch.setattr(lifecycle, '_save_mode', lambda value: mode.update(value=value))
    monkeypatch.setattr(manager, '_ready', lambda: process['running'])
    def command(action):
        if action == 'start':
            process.update(running=True, starts=process['starts'] + 1)
        elif action == 'stop':
            process.update(running=False, stops=process['stops'] + 1)
        return 'RUNNING' if process['running'] else 'STOPPED'
    monkeypatch.setattr(manager, '_command', command)
    return manager, process, mode


def test_concurrent_conversions_share_start_and_cannot_be_stopped(service):
    manager, process, _ = service
    ready, release = Event(), Event()
    def convert():
        with manager.use():
            ready.set()
            assert release.wait(3)
    with ThreadPoolExecutor(2) as pool:
        first = pool.submit(convert)
        assert ready.wait(3)
        second = pool.submit(convert)
        time.sleep(.1)
        manager.tick()
        assert process == {'running': True, 'starts': 1, 'stops': 0}
        release.set()
        first.result(); second.result()
    manager.tick()
    assert process['stops'] == 1
    with manager.use():
        assert process['starts'] == 2


def test_polling_never_starts_or_extends_idle_time(service):
    manager, process, _ = service
    assert manager.status(False)['state'] == 'sleeping'
    assert process['starts'] == 0
    with manager.use():
        pass
    completed = manager._read()['last_used']
    for _ in range(5):
        assert manager.status(True)['state'] == 'running'
    assert manager._read()['last_used'] == completed
    manager.tick()
    assert not process['running']
    assert manager.status(False)['state'] == 'sleeping'


def test_idle_time_begins_after_failed_task_finishes(service):
    manager, process, _ = service
    manager.idle_seconds = 300
    with pytest.raises(ValueError), manager.use():
        with manager._lock('control'):
            manager._write(last_used=time.monotonic() - 400)
        manager.tick()
        assert process['running']
        raise ValueError('conversion failed')
    manager.tick()
    assert process['running']
    assert time.monotonic() - manager._read()['last_used'] < 1


def test_update_waits_for_conversion_and_blocks_new_work_and_stop(service):
    manager, process, _ = service
    updating, finish, converted = Event(), Event(), Event()
    def update():
        with manager.maintenance():
            updating.set()
            assert finish.wait(3)
    def convert():
        with manager.use():
            converted.set()
    with ThreadPoolExecutor(2) as pool:
        with manager.use():
            task = pool.submit(update)
            assert not updating.wait(.15)
        assert updating.wait(3)
        assert manager.status(True)['state'] == 'updating'
        new_work = pool.submit(convert)
        assert not converted.wait(.15)
        manager.tick()
        assert process['stops'] == 0
        finish.set()
        task.result(); new_work.result()
    assert converted.is_set()


def test_start_failure_can_retry_and_is_not_shown_as_sleeping(service, monkeypatch):
    manager, process, _ = service
    original = manager._command
    def fail(_):
        raise lifecycle.RuntimeUnavailable('启动失败')
    monkeypatch.setattr(manager, '_command', fail)
    with pytest.raises(lifecycle.RuntimeUnavailable), manager.use():
        pytest.fail('work must not run before readiness')
    assert manager.status(False)['state'] == 'failed'
    monkeypatch.setattr(manager, '_command', original)
    with manager.use():
        assert process['running']
    assert manager.status(True)['error'] == ''


def test_mode_switch_and_boot_respect_always_on(service):
    manager, process, mode = service
    manager.tick()
    assert process['starts'] == 0
    manager.set_mode('always')
    manager.tick()
    assert process['running'] and mode['value'] == 'always'
    process['running'] = False
    manager.tick()  # backend restart / process exit in always-on mode
    assert process['starts'] == 2
    manager.set_mode('on_demand')
    manager.tick()
    assert not process['running']


def test_external_server_is_never_managed(service, monkeypatch):
    manager, process, _ = service
    monkeypatch.setattr(dependencies, 'is_builtin_available', lambda: False)
    with manager.use():
        pass
    manager.tick()
    assert process['starts'] == process['stops'] == 0
    assert manager.status(True)['mode'] == 'external'
    with pytest.raises(lifecycle.RuntimeUnavailable):
        manager.set_mode('always')


def test_lock_protects_work_in_another_process(service):
    manager, process, _ = service
    ctx = multiprocessing.get_context('fork')
    entered, finish = ctx.Event(), ctx.Event()
    def child():
        with manager.use():
            entered.set()
            finish.wait(3)
    task = ctx.Process(target=child)
    task.start()
    try:
        assert entered.wait(3)
        manager.tick()
        assert process['stops'] == 0
        with pytest.raises(lifecycle.RuntimeUnavailable):
            with manager._lock('work', timeout=0):
                pytest.fail('exclusive lock acquired while child works')
    finally:
        finish.set()
        task.join(3)
        if task.is_alive():
            task.terminate(); task.join()
    assert task.exitcode == 0


@pytest.mark.parametrize('entry', ['node', 'subscription'])
def test_client_holds_lease_until_cleanup_even_on_failure(monkeypatch, entry):
    events = []
    @contextmanager
    def lease():
        events.append('enter')
        try:
            yield
        finally:
            events.append('leave')
    monkeypatch.setattr(client.runtime, 'use', lease)
    monkeypatch.setattr(client, '_get_base_url', lambda: 'http://sub-store.test')
    monkeypatch.setattr(client, '_create_subscription', lambda *a, **k: events.append('create'))
    monkeypatch.setattr(client, '_delete_subscription', lambda *a: events.append('delete'))
    def fail(*a, **k):
        events.append('download')
        raise ValueError('bad conversion')
    monkeypatch.setattr(client.requests, 'get', fail)
    if entry == 'node':
        assert client.convert_proxy_string('ss://test') is None
    else:
        monkeypatch.setattr(client, '_fetch_direct_subscription_yaml', lambda _: 'proxies: []')
        client._get_subscription_proxies_yaml('id', 'http://example.test/#options')
    assert events == ['enter', 'create', 'download', 'delete', 'leave']


def test_direct_yaml_does_not_take_lease(monkeypatch):
    monkeypatch.setattr(client.runtime, 'use', lambda: pytest.fail('unnecessary wakeup'))
    monkeypatch.setattr(client.requests, 'get', lambda *a, **k: SimpleNamespace(
        text='proxies: []', headers={}, raise_for_status=lambda: None))
    assert client._get_subscription_proxies_yaml('id', 'http://example.test/sub')[1] == 'direct_yaml'


def test_mode_route_validation_and_atomic_persistence(tmp_path, monkeypatch):
    from backend.routes.settings import settings_bp
    repo = ProfileRepository(tmp_path / 'config')
    monkeypatch.setattr(config, '_repository', repo)
    monkeypatch.setattr('backend.common.auth.is_auth_enabled', lambda: False)
    manager = lifecycle.SubStoreRuntime(tmp_path / 'locks')
    monkeypatch.setattr(lifecycle, 'runtime', manager)
    monkeypatch.setattr(dependencies, 'is_builtin_available', lambda: True)
    monkeypatch.setattr(manager, '_ensure_running', lambda: None)
    app = Flask(__name__); app.register_blueprint(settings_bp)
    http = app.test_client()
    path = '/api/settings/dependencies/sub-store/mode'
    for body in (None, [], {}, {'mode': 'stop'}, {'mode': ['always']}):
        assert http.post(path, json=body).status_code == 400
    before = repo.get_system()
    assert http.post(path, json={'mode': 'always'}).status_code == 200
    after = repo.get_system()
    assert after['system_config']['sub_store_mode'] == 'always'
    assert after['agents'] == before['agents'] and after['backup'] == before['backup']
    assert all(after['system_config'][k] == v for k, v in before['system_config'].items())
    monkeypatch.setattr('backend.common.auth.is_auth_enabled', lambda: True)
    assert http.post(path, json={'mode': 'on_demand'}).status_code == 401


def test_mode_is_read_from_the_settings_alone(tmp_path, monkeypatch):
    repo = ProfileRepository(tmp_path / 'config')
    monkeypatch.setattr(config, '_repository', repo)
    repo.update_system_transaction(lambda system: system['agents'].append(
        {'id': 'agent', 'name': 'Agent', 'token': 'agent-token', 'profile_id': 'default',
         'deployments': {'d': {'status': 'succeeded'}}}))
    monkeypatch.setattr(repo, 'get_system', lambda: pytest.fail('whole system section copied'))
    monkeypatch.setattr(repo, 'get_system_with_source', lambda: pytest.fail('whole system section copied'))
    assert lifecycle.get_mode() == 'on_demand'
    repo.update_system_transaction(lambda system: system['system_config'].update(sub_store_mode='always'))
    assert lifecycle.get_mode() == 'always'
    repo.update_system_transaction(lambda system: system['system_config'].update(sub_store_mode='bogus'))
    assert lifecycle.get_mode() == 'on_demand'
    # The settings are a copy: editing them does not reach the stored document.
    settings = repo.get_system_settings()
    settings['sub_store_mode'] = 'always'
    assert repo.get_system_settings()['sub_store_mode'] == 'bogus'
    assert set(settings) == set(repo.update_system_transaction(lambda system: None)['system_config'])
