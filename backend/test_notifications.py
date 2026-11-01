"""Notification lifecycle and precise, profile-bound destinations, with no device I/O."""
from unittest.mock import Mock
import json

import pytest

from backend import notifications
from backend.common.config import get_config
from backend.test_qa_integrity import make_app
from backend.utils import notification_store, task_status


@pytest.fixture
def setup(tmp_path, monkeypatch):
    app, repo = make_app(tmp_path)
    repo.save_shared({'subscriptions': [{'id': 'feed', 'name': '常用订阅', 'url': 'https://test.invalid/?token=private',
                                       'enabled': True, 'interval': 3600}],
                      'rule_library': [], 'nodes': [], 'subscription_aggregations': []})
    monkeypatch.setattr('requests.sessions.Session.request', Mock(side_effect=AssertionError('notification must not contact devices')))
    return app.test_client(), repo


def items(client):
    response = client.get('/api/notifications')
    assert response.status_code == 200, response.get_json()
    assert response.headers['Cache-Control'] == 'no-store'
    return {item['id']: item for item in response.get_json()['items']}


def fail():
    task_status.record('subscription:feed', ok=False, error='连接上游超时', cache_retained=True)


def test_read_removes_body_persists_dedupe_and_a_new_failure_notifies_again(setup):
    client, repo = setup
    fail()
    saved = json.loads((repo.data_dir / 'notifications.json').read_text())['items']
    key = 'task:subscription:feed'
    assert saved[key]['state'] == 'pending'
    first = items(client)[key]
    assert first['severity'] == 'warning' and first['impact'] == '仍使用上一次有效缓存'
    assert first['target'] == notifications.target('/subscriptions', '检查订阅设置', subscription='feed', section='settings')
    assert client.post('/api/notifications/read', json={'versions': {key: first['version']}}).status_code == 200
    assert key not in items(client)
    saved = json.loads((repo.data_dir / 'notifications.json').read_text())['items'][key]
    assert set(saved) == {'state', 'fingerprint', 'source'}
    assert len(saved['fingerprint']) == 64 and saved['state'] == 'dismissed'
    assert key not in items(client)  # another read reloads the persisted acknowledgement
    assert task_status.get('subscription:feed')['last_status'] == 'failed'
    fail()
    third = items(client)[key]
    assert not third['read'] and third['failure_count'] == 2 and third['version'] != first['version']
    client.post('/api/notifications/read', json={'versions': {key: first['version']}})
    assert key in items(client)
    client.post('/api/notifications/read', json={'versions': {key: third['version']}})
    task_status.record('subscription:feed', ok=True)
    assert key not in json.loads((repo.data_dir / 'notifications.json').read_text())['items']


def test_recovery_and_recurrence_without_browser_and_stale_read_is_ignored(setup):
    client, repo = setup
    fail()
    key = 'task:subscription:feed'
    original = items(client)[key]
    task_status.record('subscription:feed', ok=True)
    saved = json.loads((repo.data_dir / 'notifications.json').read_text())['items']
    assert key not in saved
    fail()
    reopened = items(client)[key]
    assert reopened['state'] == 'pending' and not reopened['read']
    assert reopened['version'] != original['version']
    client.post('/api/notifications/read', json={'versions': {key: original['version']}})
    assert not items(client)[key]['read']


def test_removed_and_disabled_sources_clear_notifications_and_dedupe_markers(setup):
    client, repo = setup
    fail()
    shared = repo.get_shared()
    shared['subscriptions'][0]['enabled'] = False
    repo.save_shared(shared)
    key = 'task:subscription:feed'
    assert key not in items(client)
    shared = repo.get_shared()
    shared['subscriptions'][0]['enabled'] = True
    repo.save_shared(shared)
    row = items(client)[key]
    client.post('/api/notifications/read', json={'versions': {key: row['version']}})
    shared = repo.get_shared()
    shared['subscriptions'] = []
    repo.save_shared(shared)
    assert key not in items(client)
    assert key not in json.loads((repo.data_dir / 'notifications.json').read_text())['items']


def test_task_running_or_source_read_failure_never_clears_pending(setup, monkeypatch):
    client, _ = setup
    fail()
    monkeypatch.setattr('backend.utils.scheduled_refresh.is_running', lambda key: True)
    assert items(client)['task:subscription:feed']['state'] == 'pending'
    monkeypatch.setattr('backend.routes.tasks.list_tasks', Mock(side_effect=OSError('unavailable')))
    assert client.get('/api/notifications').status_code == 503


def test_completed_receipt_is_persisted_before_worker_releases_running_flag(setup, monkeypatch):
    _, repo = setup
    monkeypatch.setattr('backend.utils.scheduled_refresh.is_running', lambda key: True)
    path = repo.data_dir / 'notifications.json'
    fail()
    assert json.loads(path.read_text())['items']['task:subscription:feed']['state'] == 'pending'
    task_status.record('subscription:feed', ok=True)
    assert 'task:subscription:feed' not in json.loads(path.read_text())['items']


def test_manual_backup_failure_is_actionable_even_without_automatic_backup(setup):
    client, repo = setup
    repo.update_system_transaction(lambda system: system.update(backup={
        'auto_backup': False, 'webdav_url': 'https://backup.invalid', 'webdav_username': 'admin', 'webdav_password': 'private',
    }))
    task_status.record('webdav_backup', ok=False, error='HTTP 401', trigger='manual')
    row = items(client)['task:webdav_backup']
    assert row['state'] == 'pending' and row['title'] == 'WebDAV 手动备份'
    assert row['target']['query'] == {'section': 'backup'}
    task_status.record('webdav_backup', ok=True, trigger='manual')
    assert 'task:webdav_backup' not in items(client)


def test_corrupt_store_is_not_replaced_by_empty_success(setup):
    client, repo = setup
    path = repo.data_dir / 'notifications.json'
    path.write_text('broken')
    assert client.get('/api/notifications').status_code == 503
    assert path.read_text() == 'broken'


@pytest.mark.parametrize('body', [None, {}, {'versions': []}, {'versions': {'a': 3}}])
def test_read_payload_validation(setup, body):
    assert setup[0].post('/api/notifications/read', json=body).status_code == 400


def test_existing_history_and_read_bodies_are_removed_on_upgrade(setup):
    client, repo = setup
    fail()
    key = 'task:subscription:feed'
    path = repo.data_dir / 'notifications.json'
    data = json.loads(path.read_text())
    data['items']['recently-recovered'] = {**data['items'][key], 'state': 'resolved', 'resolved_at': notification_store.now()}
    data['items'][key]['read'] = True
    path.write_text(json.dumps(data))
    assert not items(client)
    saved = json.loads(path.read_text())['items']
    assert 'recently-recovered' not in saved
    assert set(saved[key]) == {'state', 'fingerprint', 'source'}


def test_old_unread_problem_is_retained_and_stale_read_cannot_clear_a_new_receipt(setup):
    client, repo = setup
    fail()
    key = 'task:subscription:feed'
    first = items(client)[key]
    with notification_store.transaction(repo.data_dir) as data:
        data[key]['first_seen_at'] = '2000-01-01T00:00:00+00:00'
    assert key in items(client)
    fail()
    assert items(client)[key]['version'] != first['version']
    client.post('/api/notifications/read', json={'versions': {key: first['version']}})
    assert key in items(client)


def test_latest_deployment_status_uses_exact_record_and_keeps_failures_while_retrying(setup):
    client, repo = setup
    from backend.common.agent_manager import get_agent_manager
    manager = get_agent_manager()
    agent = {'id': 'a', 'name': '网关', 'service_type': 'mihomo', 'profile_id': 'default', 'enabled': True,
             'token': 'very-private-token', 'latest_deployment': {
                 'deployment_id': 'd1', 'profile_id': 'default', 'status': 'rollback_failed',
                 'rollback_error': '恢复失败 very-private-token https://device.test/?secret=hidden'}}
    repo.update_system_transaction(lambda system: system.update(agents=[agent]))
    notifications.record_agent(agent, repo)
    row = items(client)['deployment:a']
    assert row['severity'] == 'error'
    assert row['target']['query'] == {'agent': 'a', 'profile': 'default', 'section': 'deployment', 'deployment': 'd1'}
    assert 'very-private-token' not in str(row) and 'secret=hidden' not in str(row)
    agent['latest_deployment'] = {'deployment_id': 'd2', 'profile_id': 'default', 'status': 'preparing'}
    repo.update_system_transaction(lambda system: system.update(agents=[agent]))
    assert items(client)['deployment:a']['state'] == 'pending'
    agent['latest_deployment']['status'] = 'succeeded'
    repo.update_system_transaction(lambda system: system.update(agents=[agent]))
    notifications.record_agent(manager.get_agent_by_id('a'), repo)
    assert 'deployment:a' not in items(client)


def seed_router(repo, profile='default'):
    router = {'id': 'r', 'name': '主路由', 'type': 'routeros', 'endpoint': 'http://router.test',
              'username': 'user', 'password': 'private', 'ip_rule_sets': []}
    repo.update_profile_transaction(profile, lambda config: config.update(routers={'items': [router]}))
    return router


def test_router_notices_keep_profile_section_freshness_and_unknown_is_not_recovery(setup):
    client, repo = setup
    router = seed_router(repo)
    report = {'in_sync': False, 'routes': {'forward_hint': {'status': 'drop', 'warning': '同口转发被阻止', 'fix_command': 'command'}}}
    plan = {'stages': [{'key': 'routes', 'title': '代理路由', 'ops': [{'kind': 'route_add'}]}]}
    notifications.record_router('default', get_config('default'), router, report, plan)
    key = 'router:default:r:firewall'
    row = items(client)[key]
    assert row['target']['query'] == {'router': 'r', 'profile': 'default', 'section': 'ipv4-forward-hint'}
    assert row['checked_at'] and not row['stale']
    assert items(client)['router:default:r:changes']['severity'] == 'neutral'
    report['routes']['forward_hint'] = {'status': 'unknown'}
    notifications.record_router('default', get_config('default'), router, report)
    assert items(client)[key]['state'] == 'pending'
    notifications.record_router('default', get_config('default'), router, {})
    assert items(client)[key]['state'] == 'pending'
    repo.update_profile_transaction('default', lambda config: config.update(custom_notification_test='changed'))
    assert items(client)[key]['stale']
    report['routes']['forward_hint'] = {'status': 'ok'}
    report['in_sync'] = True
    notifications.record_router('default', get_config('default'), router, report)
    assert key not in items(client)


def test_same_router_id_in_different_profiles_does_not_merge(setup):
    client, repo = setup
    repo.create_profile({'id': 'other', 'name': '其他'})
    for profile in ('default', 'other'):
        router = seed_router(repo, profile)
        notifications.record_router(profile, get_config(profile), router,
            {'ipv6_dns': {'status': 'blocked', 'blockers': ['需要指定 LAN']}})
    rows = items(client)
    assert rows['router:default:r:ipv6_dns']['profile_id'] == 'default'
    assert rows['router:other:r:ipv6_dns']['profile_id'] == 'other'
    notifications.record_router('other', get_config('other'), router, {'desired': {}, 'routes': {}})
    assert 'router:other:r:ipv6_dns' not in items(client)
    assert items(client)['router:default:r:ipv6_dns']['state'] == 'pending'


def test_confirmed_empty_router_release_clears_previous_failure(setup, monkeypatch):
    client, repo = setup
    router = seed_router(repo)
    repo.save_router_publication('default', router, last_release={'ok': False, 'at': notification_store.now(), 'message': '回读失败'})
    key = 'router:default:r:release'
    assert items(client)[key]['state'] == 'pending'
    from backend.routes import routers
    monkeypatch.setattr(routers, '_driver', lambda _: Mock())
    monkeypatch.setattr(routers, '_inspect', lambda *args: (None, {}, {}, {'return_routing': {}}))
    monkeypatch.setattr(routers, 'build_plan', lambda *args: {'empty': True, 'blockers': [], 'fingerprint': 'confirmed'})
    monkeypatch.setattr(routers, '_capture_sync', Mock())
    monkeypatch.setattr(routers, 'execute', Mock(side_effect=AssertionError('empty release must not write to devices')))
    result = client.post('/api/routers/r/apply', json={'fingerprint': 'confirmed'})
    assert result.status_code == 200, result.get_json()
    saved = json.loads((repo.data_dir / 'notifications.json').read_text())['items']
    assert key not in saved
    assert key not in items(client)


def test_router_read_is_not_replayed_by_repeated_checks_and_marker_clears_on_removal(setup):
    client, repo = setup
    router = seed_router(repo)
    config = get_config('default')
    report = {'in_sync': False, 'routes': {'forward_hint': {'status': 'drop', 'warning': '同口转发被阻止', 'fix_command': 'command'}}}
    plan = {'stages': [{'key': 'routes', 'title': '代理路由', 'ops': [{'kind': 'route_add'}]}]}
    notifications.record_router('default', config, router, report, plan)
    rows = items(client)
    assert len(rows) == 2
    client.post('/api/notifications/read', json={'versions': {key: row['version'] for key, row in rows.items()}})
    notifications.record_router('default', config, router, report)
    notifications.record_router('default', config, router, report, plan)
    assert not items(client)
    report['routes']['forward_hint'] = {'status': 'unknown'}
    notifications.record_router('default', config, router, report)
    assert not items(client)
    report['routes']['forward_hint'] = {'status': 'ok'}
    report['in_sync'] = True
    notifications.record_router('default', config, router, report)
    assert not json.loads((repo.data_dir / 'notifications.json').read_text())['items']
    report['routes']['forward_hint'] = {'status': 'drop', 'warning': '同口转发被阻止', 'fix_command': 'command'}
    report['in_sync'] = False
    notifications.record_router('default', config, router, report, plan)
    rows = items(client)
    assert len(rows) == 2
    client.post('/api/notifications/read', json={'versions': {key: row['version'] for key, row in rows.items()}})
    repo.update_profile_transaction('default', lambda config: config.update(routers={'items': []}))
    assert not items(client)
    assert not json.loads((repo.data_dir / 'notifications.json').read_text())['items']


def test_notifications_require_authentication(authenticated_app):
    client, *_ = authenticated_app
    assert client.get('/api/notifications').status_code == 401
    assert client.post('/api/notifications/read', json={'versions': {}}).status_code == 401


from backend.test_agent_upgrade_auth import authenticated_app  # noqa: E402,F401
