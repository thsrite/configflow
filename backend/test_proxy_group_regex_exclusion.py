"""Exclusion previews and persisted group filters agree without changing sources."""
from copy import deepcopy

import pytest
from flask import Flask

from backend.common import config
from backend.common.config_repository import ProfileRepository
from backend.routes import register_blueprints, proxy_groups


@pytest.fixture
def regex_env(tmp_path, monkeypatch):
    repository = ProfileRepository(tmp_path)
    repository.save_shared({
        'subscriptions': [
            {'id': 'feed', 'name': 'Feed', 'url': 'https://example.test/sub', 'regex_filter': 'HK|JP|广告'},
            {'id': 'disabled', 'name': 'Disabled', 'enabled': False},
        ],
        'nodes': [
            {'id': 'manual', 'name': 'HK manual', 'type': 'http', 'server': 'example.test', 'port': 80},
        ],
        'subscription_aggregations': [
            {'id': 'agg', 'name': 'Aggregate', 'subscriptions': ['feed'], 'nodes': ['manual']},
        ],
    })
    repository.save_profile('default', {
        'proxy_groups': [
            {'id': 'group', 'name': 'Group', 'type': 'select', 'subscriptions': ['feed'], 'regex': 'HK'},
        ],
    })
    repository.create_profile({'id': 'other', 'name': 'Other'}, clone_from='default')
    cache = {'nodes': [
        {'name': 'HK 01', 'type': 'ss'},
        {'name': 'HK 02', 'type': 'ss'},
        {'name': 'JP 01', 'type': 'ss'},
        {'name': '广告', 'type': 'ss'},
        {'name': 'US hidden by subscription', 'type': 'ss'},
        {'name': 'HK 01', 'type': 'ss'},
    ]}
    monkeypatch.setattr(config, '_repository', repository)
    monkeypatch.setattr('backend.common.auth.is_auth_enabled', lambda: False)
    monkeypatch.setattr(proxy_groups, 'load_subscription_cache', lambda _: deepcopy(cache))
    config.reset_config_context()
    app = Flask(__name__)
    register_blueprints(app)
    yield app.test_client(), repository
    config.reset_config_context()


@pytest.mark.parametrize('source', ['subscription', 'aggregation'])
@pytest.mark.parametrize('include,exclude,expected', [
    ('', '广告|02|manual', ['HK 01', 'JP 01']),
    ('HK', '02|manual', ['HK 01']),
    ('(?i)^hk', '(?i)02|manual', ['HK 01']),
    ('', '.*', []),
    ('HK', '   ', ['HK 01', 'HK 02', 'HK manual']),
])
def test_preview_keeps_only_final_candidates(regex_env, source, include, exclude, expected):
    client, repository = regex_env
    before = repository.export_all()
    response = client.post('/api/proxy-groups/preview-regex', json={
        'source': source, 'subscriptions': ['feed', 'disabled'], 'aggregations': ['agg'],
        'regex': include, 'exclude_regex': exclude,
    })
    assert response.status_code == 200, response.json
    if source == 'subscription':
        expected = [name for name in expected if name != 'HK manual']
    assert [node['name'] for node in response.json['nodes']] == expected
    assert response.json['count'] == len(expected)
    assert response.json['total_candidates'] == (6 if source == 'aggregation' else 5)
    assert repository.export_all() == before


@pytest.mark.parametrize('patch,message', [
    ({'exclude_regex': '('}, '排除正则无效'),
    ({'exclude_regex': 123}, '排除正则必须是字符串'),
    ({'exclude_regex': []}, '排除正则必须是字符串'),
    ({'regex': '('}, '正则表达式无效'),
    ({'regex': False}, '正则表达式必须是字符串'),
])
def test_preview_rejects_invalid_filters(regex_env, patch, message):
    client, _ = regex_env
    response = client.post('/api/proxy-groups/preview-regex', json={
        'source': 'subscription', 'subscriptions': ['feed'], 'regex': '.*', **patch,
    })
    assert response.status_code == 400
    assert message in response.json['message']


@pytest.mark.parametrize('include,exclude,expected', [
    # Mihomo splits a group filter on backticks: parts are ordered buckets, each node once.
    ('JP`HK 02`HK', '', ['JP 01', 'HK 02', 'HK 01', 'HK manual']),
    ('(?i)^jp`(?i)^hk', 'manual', ['JP 01', 'HK 01', 'HK 02']),
    ('HK 02`', '', ['HK 02', 'HK 01', 'JP 01', '广告', 'HK manual']),
    # Exclusions render backticks as name characters, so they stay one pattern.
    ('JP`HK', 'HK`02', ['JP 01', 'HK 01', 'HK 02', 'HK manual']),
])
def test_preview_orders_backtick_filter_buckets_like_mihomo(regex_env, include, exclude, expected):
    client, _ = regex_env
    response = client.post('/api/proxy-groups/preview-regex', json={
        'source': 'aggregation', 'aggregations': ['agg'], 'regex': include, 'exclude_regex': exclude,
    })
    assert response.status_code == 200, response.json
    assert [node['name'] for node in response.json['nodes']] == expected
    assert response.json['count'] == len(expected)


def test_preview_reports_invalid_backtick_filter_part(regex_env):
    client, _ = regex_env
    response = client.post('/api/proxy-groups/preview-regex', json={
        'source': 'subscription', 'subscriptions': ['feed'], 'regex': 'HK`(',
    })
    assert response.status_code == 400
    assert '正则表达式无效' in response.json['message']


def test_aggregation_preview_applies_source_filter_before_group_exclusion(regex_env):
    client, repository = regex_env
    shared = repository.get_shared()
    shared['subscription_aggregations'][0]['regex_filter'] = '^HK'
    repository.save_shared(shared)
    response = client.post('/api/proxy-groups/preview-regex', json={
        'source': 'aggregation', 'aggregations': ['agg'], 'exclude_regex': '02|manual',
    })
    assert response.status_code == 200, response.json
    assert [node['name'] for node in response.json['nodes']] == ['HK 01']


def test_saved_exclusions_roundtrip_in_selected_profile_and_can_be_cleared(regex_env):
    client, repository = regex_env
    default_before = repository.get_profile('default')
    shared_before = repository.get_shared()
    headers = {'X-ConfigFlow-Profile': 'other'}
    payload = {
        'regex': '', 'exclude_regex': '广告|02',
        'aggregation_regex': 'HK', 'aggregation_exclude_regex': 'manual',
    }
    response = client.put('/api/proxy-groups/group', json=payload, headers=headers)
    assert response.status_code == 200, response.json
    saved = client.get('/api/proxy-groups', headers=headers).json[0]
    assert {key: saved[key] for key in payload} == payload
    assert saved['subscriptions'] == ['feed']
    assert repository.get_profile('default') == default_before
    assert repository.get_shared() == shared_before

    response = client.put('/api/proxy-groups/group', headers=headers,
                          json={'exclude_regex': '', 'aggregation_exclude_regex': ''})
    assert response.status_code == 200
    saved = client.get('/api/proxy-groups', headers=headers).json[0]
    assert saved['exclude_regex'] == saved['aggregation_exclude_regex'] == ''


@pytest.mark.parametrize('field', ['exclude_regex', 'aggregation_exclude_regex'])
@pytest.mark.parametrize('value', [123, [], {}, False, '('])
def test_invalid_exclusion_is_not_persisted(regex_env, field, value):
    client, repository = regex_env
    before = repository.export_all()
    response = client.put('/api/proxy-groups/group', json={field: value})
    assert response.status_code == 400, response.json
    assert repository.export_all() == before


@pytest.mark.parametrize('format', ['surge', 'loon'])
@pytest.mark.parametrize('source', ['subscription', 'aggregation'])
def test_provider_refresh_filters_response_without_removing_source_cache(regex_env, monkeypatch, source, format):
    import yaml
    from backend.routes import subscriptions, aggregations
    from backend.utils.subscription_cache import load_subscription_cache
    from backend.utils import scheduled_refresh, sub_store_client

    client, repository = regex_env
    names = ['HK 01', 'HK 02', 'JP 01', '广告']
    fetch_calls = []

    def fetch(*args, **kwargs):
        fetch_calls.append(args)
        return yaml.safe_dump({'proxies': [
            {'name': name, 'type': 'http', 'server': 'example.test', 'port': 80}
            for name in names
        ]}), 'rendered_yaml'

    for module in (subscriptions, aggregations):
        monkeypatch.setattr(module, 'get_subscription_proxies_yaml', fetch)
        monkeypatch.setattr(module, 'validate_token_or_jwt', lambda _: {'valid': True})
    endpoint = '/api/subscriptions/feed/proxies' if source == 'subscription' else '/api/aggregations/agg/provider'
    query = {'format': format, 'regex': 'HK', 'exclude_regex': '02|manual'}
    before = repository.export_all()
    response = client.get(endpoint, query_string=query)
    assert response.status_code == 200, response.get_data(as_text=True)
    body = response.get_data(as_text=True)
    assert 'HK 01' in body
    assert all(name not in body for name in ['HK 02', 'HK manual', 'JP 01', '广告'])
    assert {node['name'] for node in load_subscription_cache('feed')['nodes']} == set(names)

    # A later refresh uses the same predicates on new source content.
    names.append('HK 03')
    cached_response = client.get(endpoint, query_string=query)
    assert cached_response.status_code == 200
    assert 'HK 03' not in cached_response.get_data(as_text=True)
    assert len(fetch_calls) == 1
    # Once the interval has passed, the scheduled task refreshes the source;
    # provider pulls keep reading the stored nodes.
    cached_nodes = repository.read_shared_json('subscribes/feed.json')
    cached_nodes['updated_at'] = '2000-01-01T00:00:00+00:00'
    repository.write_shared_json('subscribes/feed.json', cached_nodes)
    monkeypatch.setattr(sub_store_client, 'get_subscription_proxies_yaml', fetch)
    assert scheduled_refresh.run_round() == 1
    response = client.get(endpoint, query_string=query)
    assert response.status_code == 200, response.get_data(as_text=True)
    assert 'HK 03' in response.get_data(as_text=True)
    assert 'HK 02' not in response.get_data(as_text=True)
    assert len(fetch_calls) == 2
    assert repository.export_all() == before
    if source == 'aggregation':
        artifact = yaml.safe_load(repository.profile_path('default', 'providers/agg.yaml').read_text())
        assert 'HK 02' in {node['name'] for node in artifact['proxies']}


@pytest.mark.parametrize('source', ['subscription', 'aggregation'])
def test_invalid_provider_exclusion_rejected_before_fetch(regex_env, monkeypatch, source):
    from unittest.mock import Mock
    from backend.routes import subscriptions, aggregations

    client, _ = regex_env
    fetch = Mock(side_effect=AssertionError('must validate before fetching'))
    for module in (subscriptions, aggregations):
        monkeypatch.setattr(module, 'get_subscription_proxies_yaml', fetch)
        monkeypatch.setattr(module, 'validate_token_or_jwt', lambda _: {'valid': True})
    endpoint = '/api/subscriptions/feed/proxies' if source == 'subscription' else '/api/aggregations/agg/provider'
    response = client.get(endpoint, query_string={'format': 'surge', 'exclude_regex': '('})
    assert response.status_code == 400
    fetch.assert_not_called()
