"""Push -> saved bundle -> /push-diff end to end, sharing the list's background comparison."""
from copy import deepcopy
import json

import pytest
from flask import Flask

from backend.agents.content_sync import ContentSyncChecker
from backend.agents.manager import AgentManager
from backend.agents.push_diff import unpack, without_secrets
from backend.routes import agents, register_blueprints
from backend.test_agent_content_preparation import content_env  # noqa: F401

CONTROLLER_SECRET = 'controller-secret-value-1234'
OLD_PASSWORD = 'manual-node-password-old'
NEW_PASSWORD = 'manual-node-password-new'
NODE_PASSWORD = 'subscription-node-password'
CONFIG_TOKEN = 'fixture-config-token-before'
NEW_CONFIG_TOKEN = 'fixture-config-token-after'


class DeferredExecutor:
    def __init__(self):
        self.jobs = []

    def submit(self, callback, *args):
        self.jobs.append((callback, args))

    def run_all(self):
        while self.jobs:
            callback, args = self.jobs.pop(0)
            callback(*args)


@pytest.fixture
def diff_env(content_env, monkeypatch):
    config, remote, _fetches, repository = content_env
    # ConfigFlow's own pull token: Mihomo fetches the subscription provider with ?token=...
    config['system_config']['config_token'] = CONFIG_TOKEN
    config['nodes'] = [{'id': 'manual', 'name': 'Manual SS', 'type': 'ss', 'server': 'manual.example.com', 'port': 8388,
                        'enabled': True, 'params': {'cipher': 'aes-128-gcm', 'password': OLD_PASSWORD}}]
    config['proxy_groups'][0]['manual_nodes'] = ['manual']
    manager = AgentManager(repository)
    monkeypatch.setattr(agents, 'get_agent_manager', lambda: manager)
    monkeypatch.setattr(agents, 'get_config', lambda *_: deepcopy(config))
    monkeypatch.setattr(repository, 'get_compat_config', lambda *_: deepcopy(config))
    monkeypatch.setattr('backend.utils.provider_delivery.commit_provider_bundle', lambda *_: None)
    executor = DeferredExecutor()
    monkeypatch.setattr('backend.agents.content_sync._checker', ContentSyncChecker(executor=executor))

    def transport(agent, method, path, **kwargs):
        if path == 'capabilities':
            return {'deployment_protocols': [1], 'service_type': agent['service_type'], 'config_path': 'config.yaml'}
        assert method == 'post' and path == 'deployments'
        # A real Agent reports the installed main file's short digest.
        return {'success': True, 'status': 'succeeded', 'config_version': '0a1b2c3d'}

    monkeypatch.setattr(manager, '_deployment_request', transport)
    app = Flask(__name__)
    register_blueprints(app)
    ids = {service: manager.register_agent({'name': service, 'host': service + '.test', 'service_type': service})['id']
           for service in ('mihomo', 'mosdns')}
    return app.test_client(), manager, config, remote, repository, ids, executor


def push(client, agent_id, deployment_id='first'):
    response = client.post(f'/api/agents/{agent_id}/push-config', json={
        'deployment_id': deployment_id, 'base_url': 'https://config.test'})
    assert response.status_code == 200, response.json
    return response


def diff(client, agent_id):
    response = client.get(f'/api/agents/{agent_id}/push-diff')
    assert response.status_code == 200
    assert response.headers['Cache-Control'] == 'no-store'
    return response.json


def settle(client, executor, agent_id):
    first = diff(client, agent_id)
    assert first['status'] == 'checking'
    executor.run_all()
    return diff(client, agent_id)


def refresh_subscription(config):
    """The push saved the downloaded nodes; new upstream nodes arrive with the
    scheduled task (定时任务) or a manual update, which store them."""
    from backend.utils.subscription_refresh import download_subscription_nodes
    download_subscription_nodes(config['subscriptions'][0])


def test_unpushed_and_legacy_agents_explain_the_missing_baseline(diff_env):
    client, manager, _config, _remote, _repository, ids, executor = diff_env
    assert diff(client, ids['mihomo']) == {'status': 'never-pushed', 'service_type': 'mihomo'}
    manager.repository.update_system_transaction(lambda system: next(
        item for item in system['agents'] if item['id'] == ids['mosdns']).update(config_version='abcd1234'))
    assert diff(client, ids['mosdns']) == {'status': 'unknown', 'reason': 'baseline_missing', 'service_type': 'mosdns'}
    assert not executor.jobs
    assert client.get('/api/agents/missing/push-diff').status_code == 404


def test_synced_after_push_then_real_changes_are_listed_with_credentials_marked(diff_env):
    client, manager, config, remote, _repository, ids, executor = diff_env
    agent_id = ids['mihomo']
    push(client, agent_id)
    synced = settle(client, executor, agent_id)
    assert synced['status'] == 'synced' and synced['baseline']['pushed_at']
    # The list's status reuses the same finished comparison.
    listed = {item['id']: item for item in client.get('/api/agents').json}
    assert listed[agent_id]['content_sync']['status'] == 'synced' and not executor.jobs

    config['mihomo']['custom_config'] = ('mode: rule\nlog-level: info\ndns: {enable: false}\n'
                                         f'secret: {CONTROLLER_SECRET}\nhosts: {{printer.lan: 192.168.1.30}}\n')
    config['rule_library'][0]['content'] = '+.first.test\n+.second.test\n+.third.test'
    config['nodes'][0]['params']['password'] = NEW_PASSWORD
    remote['provider'] = ('proxies: [{name: Remote, type: http, server: first.test, port: 8090, '
                          f'password: {NODE_PASSWORD}}}, {{name: Added, type: http, server: added.test, port: 8081}}]')
    refresh_subscription(config)
    result = settle(client, executor, agent_id)
    assert result['status'] == 'changed' and result['content'] == 'full' and result['checked_at']
    files = {item['path']: item for item in result['files']}
    feed = files['providers/Feed.yaml']
    assert feed['summary'] == {'kind': 'nodes', 'before': 1, 'after': 2, 'added': 1, 'removed': 0, 'modified': 1}
    # The subscription's row expands to the nodes: the new one with type and address, field changes of the other.
    groups = {group['change']: group for group in feed['details']['groups']}
    assert groups['added']['entries'] == [{'change': 'added', 'text': 'Added', 'info': 'http · added.test:8081'}]
    remote_node = groups['modified']['entries'][0]
    assert remote_node['text'] == 'Remote'
    assert {field['key']: field for field in remote_node['fields']} == {
        'port': {'key': 'port', 'change': 'modified', 'sensitive': False, 'old': '8080', 'new': '8090'},
        'password': {'key': 'password', 'change': 'added', 'sensitive': True, 'old': None,
                     'new': [{'secret': NODE_PASSWORD}]}}
    rule = next(item for item in result['files'] if item['role'] == 'rule')
    assert rule['name'] == 'Selected' and rule['summary'] == {'kind': 'rules', 'before': 1, 'after': 3,
                                                              'added': 2, 'removed': 0}
    assert [entry['text'] for entry in rule['details']['groups'][0]['entries']] == ['+.second.test', '+.third.test']
    items = {item['key']: item for item in result['config']['items']}
    assert items['hosts.printer.lan']['new'] == '192.168.1.30'
    # Values come as they are; credentials are marked so the dialog can hide them.
    assert items['secret'] == {'key': 'secret', 'change': 'added', 'sensitive': True, 'old': None,
                               'new': [{'secret': CONTROLLER_SECRET}]}
    node = next(entry for entry in items['proxies']['list']['entries'] if entry['text'] == 'Manual SS')
    assert node['fields'] == [{'key': 'password', 'change': 'modified', 'sensitive': True,
                               'old': [{'secret': OLD_PASSWORD}], 'new': [{'secret': NEW_PASSWORD}]}]
    body = json.dumps(result, ensure_ascii=False)
    hidden = json.dumps(without_secrets(result), ensure_ascii=False)
    for secret in (CONTROLLER_SECRET, OLD_PASSWORD, NEW_PASSWORD, NODE_PASSWORD):
        assert secret in body and secret not in hidden
    assert 'fixture-rule-token' not in body
    assert '[REDACTED]' not in body  # no stored token, and nothing needed the response hook's fallback
    raw = result['config']['raw']['lines']
    assert '+  printer.lan: 192.168.1.30' in raw and ['+secret: ', {'secret': CONTROLLER_SECRET}] in raw
    # The change list is cached with the status: polling again does not rebuild it.
    assert diff(client, agent_id) == result and not executor.jobs
    listed = {item['id']: item for item in client.get('/api/agents').json}
    assert listed[agent_id]['content_sync']['status'] == 'changed' and not executor.jobs

    push(client, agent_id, 'second')
    assert settle(client, executor, agent_id)['status'] == 'synced'


def test_list_check_and_change_list_share_one_worker(diff_env):
    client, _manager, config, _remote, _repository, ids, executor = diff_env
    agent_id = ids['mihomo']
    push(client, agent_id)
    config['rule_library'][0]['content'] = '+.first.test\n+.other.test'
    assert client.get('/api/agents').json  # the list starts a worker
    assert len(executor.jobs) == 1
    # Asking for the change list while it waits joins that worker instead of queueing another.
    assert diff(client, agent_id)['status'] == 'checking' and len(executor.jobs) == 1
    executor.run_all()
    assert diff(client, agent_id)['counts']['modified'] == 1 and not executor.jobs
    listed = {item['id']: item for item in client.get('/api/agents').json}
    assert listed[agent_id]['content_sync']['status'] == 'changed' and not executor.jobs


def test_mosdns_without_saved_bundle_lists_files_by_digest(diff_env):
    client, _manager, config, _remote, _repository, ids, executor = diff_env
    agent_id = ids['mosdns']
    push(client, agent_id)
    config['mosdns']['remote_dns'] = 'udp://192.0.2.200:53'
    result = settle(client, executor, agent_id)
    assert result['status'] == 'changed'
    assert result['content'] == 'digests' and result['content_reason'] == 'service_without_bundle'
    assert [item['path'] for item in result['files']] == ['config.yaml']
    assert result['config']['status'] == 'unavailable'


def test_pending_deployment_and_failed_preparation_are_reported(diff_env, monkeypatch):
    client, manager, config, _remote, _repository, ids, executor = diff_env
    agent_id = ids['mihomo']
    push(client, agent_id)
    config['mihomo']['custom_config'] = 'mode: rule\nlog-level: debug\n'

    def broken(*_args, **_kwargs):
        raise ValueError(f'secret {CONTROLLER_SECRET} in an error')

    monkeypatch.setattr(agents, '_prepare_content_bundle', broken)
    failed = settle(client, executor, agent_id)
    assert failed == {'status': 'unknown', 'reason': 'check_failed', 'service_type': 'mihomo',
                      'baseline': failed['baseline']}
    manager.begin_deployment(agent_id, 'running')
    try:
        assert diff(client, agent_id)['reason'] == 'deployment_pending'
    finally:
        manager.finish_deployment_preparation(agent_id, 'running')


def test_pull_token_is_shown_and_marked_and_stored_tokens_never_leave(diff_env):
    client, _manager, config, _remote, repository, ids, executor = diff_env
    repository.update_system_transaction(lambda system: system['system_config'].update(
        rule_proxy_token='fixture-rule-token'))
    agent_id = ids['mihomo']
    push(client, agent_id)
    assert settle(client, executor, agent_id)['status'] == 'synced'
    # The pull token changes (系统设置 → 访问令牌), and a stored rule-proxy token ends up in the YAML.
    config['system_config']['config_token'] = NEW_CONFIG_TOKEN
    config['mihomo']['custom_config'] += 'external-ui-name: ui-fixture-rule-token\n'
    result = settle(client, executor, agent_id)
    items = {item['key']: item for item in result['config']['items']}
    url = next(item for key, item in items.items() if key.startswith('proxy-providers.') and key.endswith('.url'))
    assert url['sensitive'] is False
    assert url['old'][-1] == {'secret': CONFIG_TOKEN} and url['new'][-1] == {'secret': NEW_CONFIG_TOKEN}
    assert unpack(url['new'])[0].endswith('?token=' + NEW_CONFIG_TOKEN)
    # ConfigFlow's stored credentials are replaced on the server, as their own hidden piece.
    assert items['external-ui-name']['new'] == ['ui-', {'secret': '[REDACTED]'}]
    raw = result['config']['raw']['lines']
    assert ['+external-ui-name: ui-', {'secret': '[REDACTED]'}] in raw
    body = json.dumps(result, ensure_ascii=False)
    hidden = json.dumps(without_secrets(result), ensure_ascii=False)
    assert NEW_CONFIG_TOKEN in body and NEW_CONFIG_TOKEN not in hidden and CONFIG_TOKEN not in hidden
    assert 'fixture-rule-token' not in body and '[REDACTED]' not in hidden


def entries(client, agent_id, **params):
    response = client.get(f'/api/agents/{agent_id}/push-diff/entries', query_string=params)
    if response.status_code == 200:
        assert response.headers['Cache-Control'] == 'no-store'
    return response


def test_every_node_and_rule_is_paged_and_searched_through_the_api(diff_env):
    client, _manager, config, remote, repository, ids, executor = diff_env
    repository.update_system_transaction(lambda system: system['system_config'].update(
        rule_proxy_token='fixture-rule-token'))
    agent_id = ids['mihomo']
    push(client, agent_id)
    assert settle(client, executor, agent_id)['status'] == 'synced'
    rules = [f'+.r{index:03d}.test' for index in range(300)]
    config['rule_library'][0]['content'] = '\n'.join(['+.first.test', *rules, '+.fixture-rule-token.test'])
    remote['provider'] = 'proxies: [{name: Remote, type: http, server: first.test, port: 8080, password: remote-pw-new}, ' + ', '.join(
        f'{{name: N{index:02d}, type: http, server: n{index}.test, port: {8000 + index}, password: pw-{index}-x}}'
        for index in range(60)) + ']'
    refresh_subscription(config)
    result = settle(client, executor, agent_id)
    version = result['version']
    rule = next(item for item in result['files'] if item['role'] == 'rule')
    feed = next(item for item in result['files'] if item['path'] == 'providers/Feed.yaml')
    added = rule['details']['groups'][0]
    assert rule['details']['paging'] is True
    assert (added['total'], len(added['entries']), added['available']) == (301, 20, 301)

    # 加载更多: from where the first page ends, 50 at a time, up to the end.
    page = entries(client, agent_id, version=version, file=rule['path'], change='added', offset=20).json
    assert (page['status'], page['total'], page['offset'], page['next_offset']) == ('ok', 301, 20, 70)
    assert [entry['text'] for entry in page['entries']][:2] == ['+.r020.test', '+.r021.test']
    listed, offset = list(added['entries']), 20
    while offset is not None:
        page = entries(client, agent_id, version=version, file=rule['path'], change='added', offset=offset,
                       limit=200).json
        listed += page['entries']
        offset = page['next_offset']
    assert len(listed) == 301 and listed[-2]['text'] == '+.r299.test'
    # ConfigFlow's stored token never leaves, also in a later page, and search does not find it.
    assert listed[-1]['text'] == ['+.', {'secret': '[REDACTED]'}, '.test']
    assert 'fixture-rule-token' not in json.dumps(listed)
    for query in ('fixture', '-TOKEN.test'):
        found = entries(client, agent_id, version=version, file=rule['path'], change='added', q=query).json
        assert found['total'] == 0, query
    found = entries(client, agent_id, version=version, file=rule['path'], change='added', q='R29').json
    assert found['total'] == 10 and [entry['text'] for entry in found['entries']][0] == '+.r290.test'

    nodes = {group['change']: group for group in feed['details']['groups']}
    assert (nodes['added']['total'], nodes['added']['available']) == (60, 60)
    found = entries(client, agent_id, version=version, file=feed['path'], change='added', q='n7.test:8007').json
    assert [entry['text'] for entry in found['entries']] == ['N07']
    assert entries(client, agent_id, version=version, file=feed['path'], change='added', q='pw-7').json['total'] == 0
    modified = entries(client, agent_id, version=version, file=feed['path'], change='modified').json
    password = next(field for field in modified['entries'][0]['fields'] if field['key'] == 'password')
    assert password['new'] == [{'secret': 'remote-pw-new'}]  # marked, so 隐藏敏感信息 hides it in loaded entries
    assert 'remote-pw-new' not in json.dumps(without_secrets(modified), ensure_ascii=False)
    assert entries(client, agent_id, version=version, file=feed['path'], change='modified',
                   q='remote-pw').json['total'] == 0

    # Requests that cannot be a page of this list.
    for params in ({'change': 'bad'}, {'limit': 201}, {'limit': 0}, {'offset': -1}, {'offset': 'x'},
                   {'version': ''}, {'file': ''}, {'q': 'x' * 201}):
        response = entries(client, agent_id, **{'version': version, 'file': rule['path'], 'change': 'added', **params})
        assert response.status_code == 400, params
    assert entries(client, agent_id, version=version, file='ruleset/missing.list', change='added').status_code == 404
    assert entries(client, agent_id, version=version, file=rule['path'], change='modified').status_code == 404
    assert entries(client, 'missing', version=version, file=rule['path'], change='added').status_code == 404
    assert entries(client, agent_id, version='older', file=rule['path'], change='added').json == {
        'status': 'stale', 'reason': 'replaced'}
    # The configuration changes while the dialog is open: reload instead of mixing two comparisons.
    config['mihomo']['custom_config'] += 'tcp-concurrent: true\n'
    assert entries(client, agent_id, version=version, file=rule['path'], change='added', offset=20).json == {
        'status': 'stale', 'reason': 'changed'}
    fresh = settle(client, executor, agent_id)
    assert fresh['version'] != version
    assert entries(client, agent_id, version=version, file=rule['path'], change='added').json['reason'] == 'replaced'
    assert entries(client, agent_id, version=fresh['version'], file=rule['path'], change='added').json['status'] == 'ok'
