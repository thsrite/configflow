"""Viewing aggregation members must never download or create refresh receipts."""
from datetime import datetime, timedelta, timezone

import pytest

from backend.test_qa_integrity import make_app
from backend.utils import subscription_health, task_status
from backend.utils.subscription_cache import load_subscription_cache, save_subscription_nodes


@pytest.fixture
def setup(tmp_path, monkeypatch):
    app, repo = make_app(tmp_path)
    repo.save_shared({
        'subscriptions': [{'id': 's', 'name': 'Feed', 'url': 'https://feed.test/new',
                           'interval': 0, 'regex_filter': '^HK'}],
        'nodes': [{'id': 'imported', 'name': 'HK imported', 'subscription_id': 's'}],
        'subscription_aggregations': [], 'rule_library': [],
    })

    def unexpected_fetch(*args, **kwargs):
        pytest.fail('a cache preview must not access the network or refresh a source')

    monkeypatch.setattr('requests.sessions.Session.request', unexpected_fetch)
    monkeypatch.setattr('backend.routes.subscriptions.refresh_subscription', unexpected_fetch)
    subscription_health.record_fetch('s', ok=False, from_cache=True, count=2)
    task_status.record('subscription:s', ok=False, trigger='manual', error='上游返回 HTTP 403')
    return app.test_client(), repo


def snapshot(repo):
    return (repo.get_shared(), load_subscription_cache('s'),
            subscription_health.load_health(), task_status.load())


@pytest.mark.parametrize('interval', [0, 60, 86400])
@pytest.mark.parametrize('legacy', [False, True])
def test_cache_preview_filters_saved_nodes_without_refreshing_or_mutating(setup, interval, legacy):
    client, repo = setup
    repo.update_shared_transaction(lambda shared: shared['subscriptions'][0].update(interval=interval))
    old = (datetime.now(timezone.utc) - timedelta(days=8)).isoformat()
    metadata = {'fetched_at': old, **({} if legacy else {'url': 'https://feed.test/new'})}
    save_subscription_nodes('s', [{'id': 'hk', 'name': 'HK cached', 'type': 'ss'},
                                  {'id': 'jp', 'name': 'JP cached', 'type': 'ss'}], metadata)
    before = snapshot(repo)
    for _ in range(2):
        response = client.get('/api/subscriptions/s/nodes?source=cache')
        assert response.status_code == 200
        assert response.json == {'success': True, 'cache_available': True, 'count': 1,
                                 'nodes': [{'id': 'hk', 'name': 'HK cached', 'type': 'ss'}],
                                 'cached_updated_at': old}
    assert snapshot(repo) == before


@pytest.mark.parametrize('state', ['missing', 'different_url', 'invalid_nodes', 'empty', 'filtered'])
def test_unavailable_or_empty_cache_does_not_fall_back_to_imports_or_upstream(setup, state):
    client, repo = setup
    if state != 'missing':
        nodes = [] if state == 'empty' else [{'id': 'jp', 'name': 'JP cached'}]
        if state == 'invalid_nodes':
            nodes = None
        save_subscription_nodes('s', nodes, {'url': 'https://feed.test/old' if state == 'different_url' else 'https://feed.test/new'})
    before = snapshot(repo)
    response = client.get('/api/subscriptions/s/nodes?source=cache')
    assert response.status_code == 200
    assert response.json['nodes'] == []
    assert response.json['count'] == 0
    assert response.json['cache_available'] is (state in ('empty', 'filtered'))
    assert snapshot(repo) == before


def test_existing_nodes_call_keeps_imported_node_contract(setup):
    client, repo = setup
    save_subscription_nodes('s', [{'id': 'cached', 'name': 'HK cached'}])
    assert client.get('/api/subscriptions/s/nodes').json == repo.get_shared()['nodes']


def test_cache_preview_requires_existing_subscription_and_auth(setup, monkeypatch):
    client, repo = setup
    before = snapshot(repo)
    assert client.get('/api/subscriptions/missing/nodes?source=cache').status_code == 404
    monkeypatch.setattr('backend.common.auth.is_auth_enabled', lambda: True)
    assert client.get('/api/subscriptions/s/nodes?source=cache').status_code == 401
    assert snapshot(repo) == before
