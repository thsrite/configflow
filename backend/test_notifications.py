"""Notification lifecycle and precise, profile-bound destinations, with no device I/O."""
from datetime import datetime, timedelta, timezone
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


def test_receipts_persist_without_an_open_page_and_read_does_not_resolve(setup):
    client, repo = setup
    fail()
    saved = json.loads((repo.data_dir / 'notifications.json').read_text())['items']
    key = 'task:subscription:feed'
    assert saved[key]['state'] == 'pending'
    first = items(client)[key]
    assert first['severity'] == 'warning' and first['impact'] == '仍使用上一次有效缓存'
    assert first['target'] == notifications.target('/subscriptions', '检查订阅设置', subscription='feed', section='settings')
    assert client.post('/api/notifications/read', json={'versions': {key: first['version']}}).status_code == 200
    second = items(client)[key]
    assert second['read'] and second['state'] == 'pending' and second['occurrences'] == 1
    assert items(client)[key]['occurrences'] == 1
    fail()
    third = items(client)[key]
    assert third['read'] and third['occurrences'] == 2 and third['failure_count'] == 2


def test_recovery_and_recurrence_without_browser_and_stale_read_is_ignored(setup):
    client, repo = setup
    fail()
    key = 'task:subscription:feed'
    original = items(client)[key]
    task_status.record('subscription:feed', ok=True)
    saved = json.loads((repo.data_dir / 'notifications.json').read_text())['items'][key]
    assert saved['state'] == 'resolved' and saved['resolved_at']
    fail()
    reopened = items(client)[key]
    assert reopened['state'] == 'pending' and not reopened['read']
    assert reopened['version'] != original['version']
    client.post('/api/notifications/read', json={'versions': {key: original['version']}})
    assert not items(client)[key]['read']


def test_removed_and_disabled_sources_retire_instead_of_claiming_recovery(setup):
    client, repo = setup
    fail()
    shared = repo.get_shared()
    shared['subscriptions'][0]['enabled'] = False
    repo.save_shared(shared)
    row = items(client)['task:subscription:feed']
    assert row['state'] == 'resolved' and row['resolution'] == '已不再适用'
    shared = repo.get_shared()
    shared['subscriptions'] = []
    repo.save_shared(shared)
    assert items(client)['task:subscription:feed']['state'] == 'resolved'


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
    assert json.loads(path.read_text())['items']['task:subscription:feed']['state'] == 'resolved'


def test_corrupt_store_is_not_replaced_by_empty_success(setup):
    client, repo = setup
    path = repo.data_dir / 'notifications.json'
    path.write_text('broken')
    assert client.get('/api/notifications').status_code == 503
    assert path.read_text() == 'broken'


@pytest.mark.parametrize('body', [None, {}, {'versions': []}, {'versions': {'a': 3}}])
def test_read_payload_validation(setup, body):
    assert setup[0].post('/api/notifications/read', json=body).status_code == 400


def test_resolved_history_expires_but_pending_never_expires(setup):
    client, repo = setup
    fail()
    key = 'task:subscription:feed'
    stamp = (datetime.now(timezone.utc) - timedelta(days=31)).isoformat()
    with notification_store.transaction(repo.data_dir) as data:
        data[key]['first_seen_at'] = stamp
        data['old'] = {**data[key], 'id': 'old', 'state': 'resolved', 'resolved_at': stamp}
    assert key in items(client) and 'old' not in items(client)


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
    assert items(client)['deployment:a']['state'] == 'resolved'


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
    assert items(client)[key]['state'] == 'resolved'


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
    assert saved[key]['state'] == 'resolved'
    assert items(client)[key]['state'] == 'resolved'


def test_notifications_require_authentication(authenticated_app):
    client, *_ = authenticated_app
    assert client.get('/api/notifications').status_code == 401
    assert client.post('/api/notifications/read', json={'versions': {}}).status_code == 401


from backend.test_agent_upgrade_auth import authenticated_app  # noqa: E402,F401
