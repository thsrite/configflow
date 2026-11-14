import json
import subprocess
from types import SimpleNamespace

import pytest

from backend.common import config
from backend.common.config_repository import ProfileRepository
from backend.test_flow_ui_apis import make_client
from backend.utils import node_latency as latency

NODE_A = '["node","a"]'


@pytest.fixture
def repository(tmp_path, monkeypatch):
    repo = ProfileRepository(tmp_path)
    config.set_repository(repo)
    repo.save_shared({'nodes': [{'id': 'a', 'name': 'A', 'type': 'trojan', 'server': 'example.invalid',
                                 'port': 443, 'password': 'secret', 'tls': True}]})
    monkeypatch.setattr(latency, '_probe_binary', lambda: '/test/probe')
    return repo


def test_raw_and_cached_node_credentials_are_preserved(repository, monkeypatch):
    raw = latency._proxy_of(latency.collect_candidates()[NODE_A])
    assert raw['password'] == 'secret' and raw['tls'] is True
    node = {'name': 'nested', 'type': 'vless', 'server': 'example.invalid', 'port': 443,
            'params': {'uuid': 'uuid', 'network': 'ws', 'ws-opts': {'path': '/path'},
                       'reality-opts': {'public-key': 'public'}}}
    proxy = latency._proxy_of(node)
    assert proxy['uuid'] == 'uuid'
    assert proxy['ws-opts'] == {'path': '/path'}
    assert proxy['reality-opts']['public-key'] == 'public'
    assert 'short-id' not in node['params']['reality-opts']  # no mutation of the cache


@pytest.mark.parametrize('method', [None, 'proxy_http_v1'])
def test_previous_probe_results_are_hidden_and_not_mixed(repository, monkeypatch, method):
    repository.write_shared_json(latency.LATENCY_FILE, {
        NODE_A: {'latency': None, 'history': [1, None], 'method': method,
              'url': latency.settings()['url'], 'unified_delay': True},
    })
    assert latency.load_results() == {}
    monkeypatch.setattr(latency, '_run_probe', lambda proxies, options: {NODE_A: {'latency': 150}})
    result = latency.run_tests(['A', 'A'])['results']['A']
    assert result['history'] == [150]
    assert result['method'] == latency.METHOD
    monkeypatch.setenv('CONFIGFLOW_NODE_TEST_URL', 'https://test.example/generate_204')
    assert latency.run_tests(['A'])['results']['A']['history'] == [150]


def test_worker_gets_stdin_json_and_no_credentials_in_command(repository, monkeypatch):
    calls = []
    def run(command, **kwargs):
        calls.append(command)
        payload = json.loads(kwargs['input'])
        assert payload['nodes'][0]['password'] == 'secret'
        assert payload['nodes'][0]['name'] == NODE_A
        assert kwargs['timeout'] == 10
        return SimpleNamespace(returncode=0, stdout=json.dumps({'version': 4, 'results': [
            {'name': NODE_A, 'latency': 99, 'first_latency': 250}]}))
    monkeypatch.setattr(latency.subprocess, 'run', run)
    result = latency.run_tests(['A'])['results']['A']
    assert calls == [['/test/probe']]
    assert result['latency'] == 99 and result['first_latency'] == 250


def test_subscription_cache_keeps_protocol_options_and_distinct_sources(repository, monkeypatch):
    repository.save_shared({'subscriptions': [{'id': 'sub', 'name': 'test', 'enabled': True}]})
    monkeypatch.setattr(latency, 'load_subscription_cache', lambda _: {'nodes': [
        {'name': 'A', 'type': 'http', 'server': 'other.invalid', 'port': 443},
        {'name': 'cached', 'type': 'trojan', 'server': 'proxy.invalid', 'port': 443,
         'params': {'password': 'cached-secret', 'sni': 'tls.invalid'}},
    ]})
    candidates = latency.collect_candidates()
    assert candidates[NODE_A]['password'] == 'secret'
    assert candidates['["subscription","sub","A"]']['server'] == 'other.invalid'
    proxy = latency._proxy_of(candidates['["subscription","sub","cached"]'])
    assert proxy['password'] == 'cached-secret' and proxy['sni'] == 'tls.invalid'


@pytest.mark.parametrize('output', ['not JSON', '{}', '{"version":4,"results":[]}',
    json.dumps({'version': 4, 'results': [{'name': NODE_A, 'latency': True}]}),
    json.dumps({'version': 4, 'results': [{'name': NODE_A, 'latency': None}]})])
def test_invalid_worker_output_does_not_replace_history(repository, monkeypatch, output):
    monkeypatch.setattr(latency.subprocess, 'run', lambda *args, **kwargs: SimpleNamespace(returncode=0, stdout=output))
    with pytest.raises(latency.ProbeUnavailable):
        latency.run_tests(['A'])
    assert latency.load_results() == {}


def test_worker_timeout_releases_lock_and_preserves_results(repository, monkeypatch):
    def timeout(*args, **kwargs):
        raise subprocess.TimeoutExpired('/test/probe', 10, stderr='secret')
    monkeypatch.setattr(latency.subprocess, 'run', timeout)
    client = make_client(repository)
    for _ in range(2):
        response = client.post('/api/nodes/latency', json={'names': ['A']})
        assert response.status_code == 503
        assert b'secret' not in response.data
    assert latency.load_results() == {}


def test_invalid_worker_dns_is_explained_without_leaking_settings(repository, monkeypatch):
    previous = {NODE_A: {'latency': 100, 'history': [100], 'method': latency.METHOD}}
    repository.write_shared_json(latency.LATENCY_FILE, previous)
    monkeypatch.setattr(latency.subprocess, 'run', lambda *args, **kwargs: SimpleNamespace(
        returncode=3, stdout='', stderr='https://user:secret@invalid-dns.example'))
    response = make_client(repository).post('/api/nodes/latency', json={'names': ['A']})
    assert response.status_code == 503
    assert '节点测速 DNS 设置无效' in response.get_json()['message']
    assert 'secret' not in response.get_data(as_text=True)
    assert 'invalid-dns.example' not in response.get_data(as_text=True)
    assert latency.load_results() == previous


@pytest.mark.parametrize('old_version', [1, 2, 3])
def test_old_worker_is_rejected_without_replacing_current_history(repository, monkeypatch, old_version):
    previous = {NODE_A: {'latency': 100, 'history': [100], 'method': latency.METHOD}}
    repository.write_shared_json(latency.LATENCY_FILE, previous)
    monkeypatch.setattr(latency.subprocess, 'run', lambda *args, **kwargs: SimpleNamespace(
        returncode=0, stdout=json.dumps({'version': old_version, 'results': [{'name': 'A', 'latency': 1}]})))
    response = make_client(repository).post('/api/nodes/latency', json={'names': ['A']})
    assert response.status_code == 503
    assert '版本不匹配' in response.get_json()['message']
    assert latency.load_results() == previous


def test_full_batch_uses_one_probe_timeout_and_preserves_every_result(repository, monkeypatch):
    nodes = [{**config.get_shared_config()['nodes'][0], 'id': f'id-{i}', 'name': f'node-{i}'} for i in range(20)]
    repository.save_shared({'nodes': nodes})
    calls = []

    def run(command, **kwargs):
        payload = json.loads(kwargs['input'])
        calls.append(payload)
        assert kwargs['timeout'] == payload['timeout_ms'] / 1000 + 5
        return SimpleNamespace(returncode=0, stdout=json.dumps({'version': 4, 'results': [
            {'name': node['name'], 'latency': None, 'error_code': 'timeout', 'error': '连接或请求超时'}
            for node in payload['nodes']]}))

    monkeypatch.setattr(latency.subprocess, 'run', run)
    response = make_client(repository).post('/api/nodes/latency', json={'names': [node['name'] for node in nodes]})
    assert response.status_code == 200
    assert len(calls) == 1 and len(calls[0]['nodes']) == 20
    assert set(response.get_json()['results']) == {node['name'] for node in nodes}
    assert all(entry['error_code'] == 'timeout' for entry in latency.load_results().values())


def test_more_than_five_remote_conversions_are_rejected_before_conversion_or_probe(repository, monkeypatch):
    repository.save_shared({'nodes': [
        {'id': f'uri-{i}', 'name': f'URI {i}', 'proxy_string': 'ss://example'} for i in range(6)
    ]})
    def unexpected_call(*args):
        pytest.fail('Oversized conversion batches must fail before conversion or probing')
    monkeypatch.setattr(latency, '_proxy_of', unexpected_call)
    monkeypatch.setattr(latency, '_probe_binary', unexpected_call)
    monkeypatch.setattr(latency, '_run_probe', unexpected_call)
    response = make_client(repository).post('/api/nodes/latency', json={})
    assert response.status_code == 400
    assert '5 个需要 Sub-Store 转换' in response.get_json()['message']
    assert latency.load_results() == {}


def test_five_uri_and_fifteen_local_nodes_share_one_probe_batch(repository, monkeypatch):
    from backend.utils import sub_store_client

    local = config.get_shared_config()['nodes'][0]
    repository.save_shared({'nodes': [
        {'id': f'uri-{i}', 'name': f'URI {i}', 'proxy_string': f'ss://example-{i}'} for i in range(5)
    ] + [{**local, 'id': f'local-{i}', 'name': f'Local {i}'} for i in range(15)]})
    conversions = []
    batches = []
    def convert(raw):
        conversions.append(raw)
        return {'type': 'http', 'server': 'converted.invalid', 'port': 8080}
    def probe(proxies, options):
        batches.append(proxies)
        return {proxy['name']: {'latency': 10} for proxy in proxies}
    monkeypatch.setattr(sub_store_client, 'convert_proxy_string', convert)
    monkeypatch.setattr(latency, '_run_probe', probe)
    response = make_client(repository).post('/api/nodes/latency', json={})
    assert response.status_code == 200
    assert len(conversions) == 5
    assert len(batches) == 1 and len(batches[0]) == 20
    assert len(response.get_json()['results']) == 20


def test_twenty_json_and_yaml_nodes_need_no_remote_conversion(repository, monkeypatch):
    from backend.utils import sub_store_client

    raw_json = '{"type":"trojan","server":"local.invalid","port":443,"password":"test-only"}'
    raw_yaml = 'type: trojan\nserver: local.invalid\nport: 443\npassword: test-only\n'
    repository.save_shared({'nodes': [
        {'id': f'local-{i}', 'name': f'Local {i}', 'proxy_string': raw_json if i % 2 else raw_yaml}
        for i in range(20)
    ]})
    def unexpected_conversion(*args):
        pytest.fail('JSON/YAML proxy strings must be recognized locally')
    monkeypatch.setattr(sub_store_client, 'convert_proxy_string', unexpected_conversion)
    monkeypatch.setattr(latency, '_run_probe', lambda proxies, options: {
        proxy['name']: {'latency': 10} for proxy in proxies})
    response = make_client(repository).post('/api/nodes/latency', json={})
    assert response.status_code == 200
    assert len(response.get_json()['results']) == 20
    assert all(entry['latency'] == 10 for entry in response.get_json()['results'].values())


def test_parse_failure_is_a_result_not_missing(repository, monkeypatch):
    monkeypatch.setattr(latency, '_proxy_of', lambda node: None)
    result = latency.run_tests(['A', 'deleted'])
    assert result['results']['A']['error_code'] == 'config'
    assert result['missing'] == ['deleted']


def test_busy_and_batch_limits(repository):
    client = make_client(repository)
    with latency._test_lock:
        assert client.post('/api/nodes/latency', json={'names': ['A']}).status_code == 409
    assert client.post('/api/nodes/latency', json={'names': [str(i) for i in range(21)]}).status_code == 400
    assert client.post('/api/nodes/latency', json={'keys': [str(i) for i in range(21)]}).status_code == 400
    for body in [[], 'text', {'names': [None]}, {'names': 'A'}, {'keys': [None]}, {'keys': NODE_A},
                 {'names': ['A'], 'keys': [NODE_A]}, {'names': [], 'keys': []}]:
        assert client.post('/api/nodes/latency', json=body).status_code == 400


def test_same_name_sources_have_independent_measurements_and_history(repository, monkeypatch):
    repository.save_shared({'subscriptions': [{'id': 'first'}, {'id': 'second'}]})
    monkeypatch.setattr(latency, 'load_subscription_cache', lambda sub_id: {'nodes': [
        {'name': 'A', 'type': 'http', 'server': f'{sub_id}.invalid', 'port': 8080},
    ]})
    first = '["subscription","first","A"]'
    second = '["subscription","second","A"]'
    calls = []

    def probe(proxies, options):
        calls.append([(proxy['name'], proxy['server']) for proxy in proxies])
        return {proxy['name']: {'latency': {'example.invalid': 10, 'first.invalid': 20,
                                          'second.invalid': 30}[proxy['server']]} for proxy in proxies}

    monkeypatch.setattr(latency, '_run_probe', probe)
    client = make_client(repository)
    response = client.post('/api/nodes/latency', json={'keys': [NODE_A, first, second]})
    assert response.status_code == 200
    assert {key: entry['latency'] for key, entry in response.get_json()['results'].items()} == {
        NODE_A: 10, first: 20, second: 30,
    }
    client.post('/api/nodes/latency', json={'keys': [second, second]})
    assert calls[-1] == [(second, 'second.invalid')]
    stored = client.get('/api/nodes/latency').get_json()['results']
    assert stored[NODE_A]['history'] == [10]
    assert stored[first]['history'] == [20]
    assert stored[second]['history'] == [30, 30]
    assert set(stored) == {NODE_A, first, second}


def test_keys_report_deleted_disabled_and_filtered_nodes_as_missing(repository, monkeypatch):
    repository.save_shared({
        'nodes': [config.get_shared_config()['nodes'][0], {'id': 'disabled', 'name': 'Disabled', 'enabled': False}],
        'subscriptions': [{'id': 'off', 'enabled': False}, {'id': 'filtered', 'regex_filter': '^Keep$'}],
    })
    monkeypatch.setattr(latency, 'load_subscription_cache', lambda _: {'nodes': [
        {'name': 'Excluded', 'type': 'http', 'server': 'unused.invalid', 'port': 8080},
    ]})
    monkeypatch.setattr(latency, '_run_probe', lambda proxies, options: {NODE_A: {'latency': 10}})
    missing = ['["node","deleted"]', '["node","disabled"]', '["subscription","off","Excluded"]',
               '["subscription","filtered","Excluded"]', 'A']
    response = make_client(repository).post('/api/nodes/latency', json={'keys': [NODE_A, *missing]})
    assert response.status_code == 200
    assert set(response.get_json()['results']) == {NODE_A}
    assert response.get_json()['missing'] == missing


def test_imported_node_uses_library_id_even_with_a_subscription_source(repository, monkeypatch):
    repository.save_shared({
        'nodes': [{**config.get_shared_config()['nodes'][0], 'subscription_id': 'sub'}],
        'subscriptions': [{'id': 'sub'}],
    })
    monkeypatch.setattr(latency, 'load_subscription_cache', lambda _: {'nodes': [
        {'name': 'A', 'type': 'http', 'server': 'cached.invalid', 'port': 8080},
    ]})
    def probe(proxies, options):
        assert len(proxies) == 1
        assert proxies[0]['name'] == NODE_A
        assert proxies[0]['server'] == 'example.invalid'
        return {NODE_A: {'latency': 10}}
    monkeypatch.setattr(latency, '_run_probe', probe)
    response = make_client(repository).post('/api/nodes/latency', json={'keys': [NODE_A]})
    assert response.status_code == 200
    assert set(response.get_json()['results']) == {NODE_A}


def test_legacy_names_are_rejected_when_the_source_is_ambiguous(repository, monkeypatch):
    repository.save_shared({'subscriptions': [{'id': 'sub'}]})
    monkeypatch.setattr(latency, 'load_subscription_cache', lambda _: {'nodes': [
        {'name': 'A', 'type': 'http', 'server': 'other.invalid', 'port': 8080},
    ]})
    def unexpected_probe(*args):
        pytest.fail('Ambiguous names must be rejected before probing')
    monkeypatch.setattr(latency, '_run_probe', unexpected_probe)
    response = make_client(repository).post('/api/nodes/latency', json={'names': ['A']})
    assert response.status_code == 400
    assert '同名节点' in response.get_json()['message']
    assert latency.load_results() == {}


def test_legacy_names_response_uses_names_but_persists_source_keys(repository, monkeypatch):
    monkeypatch.setattr(latency, '_run_probe', lambda proxies, options: {NODE_A: {'latency': 10}})
    client = make_client(repository)
    response = client.post('/api/nodes/latency', json={'names': ['A', 'missing']})
    assert response.status_code == 200
    assert set(response.get_json()['results']) == {'A'}
    assert response.get_json()['missing'] == ['missing']
    assert set(client.get('/api/nodes/latency').get_json()['results']) == {NODE_A}


def test_source_key_encoding_and_default_request_include_all_candidates(repository, monkeypatch):
    repository.save_shared({'subscriptions': [{'id': '机场'}]})
    monkeypatch.setattr(latency, 'load_subscription_cache', lambda _: {'nodes': [
        {'name': '香港 "01"', 'type': 'http', 'server': 'other.invalid', 'port': 8080},
    ]})
    monkeypatch.setattr(latency, '_run_probe', lambda proxies, options: {
        proxy['name']: {'latency': 10} for proxy in proxies})
    response = make_client(repository).post('/api/nodes/latency', json={})
    assert response.status_code == 200
    assert set(response.get_json()['results']) == {NODE_A, '["subscription","机场","香港 \\"01\\""]'}


def test_missing_worker_is_explicit_and_never_falls_back_to_tcp(tmp_path, monkeypatch):
    monkeypatch.setenv('CONFIGFLOW_NODE_PROBE_BIN', str(tmp_path / 'missing'))
    with pytest.raises(latency.ProbeUnavailable, match='未安装'):
        latency._probe_binary()


@pytest.mark.parametrize('url', ['file:///etc/passwd', 'https://user:secret@example.com', 'not-a-url'])
def test_invalid_url_is_rejected(monkeypatch, url):
    monkeypatch.setenv('CONFIGFLOW_NODE_TEST_URL', url)
    with pytest.raises(latency.ProbeUnavailable):
        latency.settings()


def test_dns_settings_persist_without_changing_nodes_or_other_settings(repository):
    system = config.get_system_config()
    system.setdefault('system_config', {})['rule_fetch_proxy'] = 'http://127.0.0.1:7890'
    config.save_system_config(system)
    original_system = config.get_system_config()
    original_shared = repository.get_shared()
    client = make_client(repository)
    assert client.get('/api/nodes/latency/settings').get_json()['dns_servers'] == []
    response = client.post('/api/nodes/latency/settings', json={
        'dns_servers': [' 192.0.2.53 ', '192.0.2.53:53', '127.0.0.1:5353']})
    assert response.status_code == 200
    assert response.get_json()['dns_servers'] == ['192.0.2.53', '127.0.0.1:5353']
    expected = original_system
    expected.setdefault('system_config', {})['node_test_dns'] = ['192.0.2.53', '127.0.0.1:5353']
    expected['system_config']['node_test_dns_config'] = {
        'mode': 'custom', 'servers': expected['system_config']['node_test_dns'],
        'bootstrap': []}
    persisted = repository.get_system()
    assert {k: v for k, v in persisted.items() if k != '_revision'} == {
        k: v for k, v in expected.items() if k != '_revision'}
    assert repository.get_shared() == original_shared
    # A new repository/client reloads the setting from disk, without process env.
    fresh = ProfileRepository(repository.data_dir)
    assert make_client(fresh).get('/api/nodes/latency/settings').get_json()['dns_servers'] == expected['system_config']['node_test_dns']


@pytest.mark.parametrize('value', [None, '192.0.2.53', [None], [''], ['dns.example'],
    ['https://user:secret@dns.example/query'], ['192.0.2.53:0'], ['192.0.2.53:65536'],
    ['192.0.2.53:abc'], ['::1'], ['0.0.0.0'], ['224.0.0.1'], ['192.0.2.53'] * 5])
def test_invalid_dns_settings_preserve_saved_value(repository, value):
    client = make_client(repository)
    assert client.post('/api/nodes/latency/settings', json={'dns_servers': ['127.0.0.1:5353']}).status_code == 200
    response = client.post('/api/nodes/latency/settings', json={'dns_servers': value})
    assert response.status_code == 400
    assert 'secret' not in response.get_data(as_text=True)
    assert client.get('/api/nodes/latency/settings').get_json()['dns_servers'] == ['127.0.0.1:5353']


def test_saved_dns_reaches_worker_and_changing_it_starts_new_history(repository, monkeypatch):
    client = make_client(repository)
    payloads = []
    def probe(command, **kwargs):
        payloads.append(json.loads(kwargs['input']))
        return SimpleNamespace(returncode=0, stdout=json.dumps({'version': 4, 'results': [
            {'name': NODE_A, 'latency': 10}]}))
    monkeypatch.setattr(latency.subprocess, 'run', probe)
    assert client.post('/api/nodes/latency', json={'names': ['A']}).status_code == 200
    assert payloads[-1]['dns_servers'] == []
    assert client.post('/api/nodes/latency/settings', json={'dns_servers': ['192.0.2.53']}).status_code == 200
    result = client.post('/api/nodes/latency', json={'names': ['A']}).get_json()['results']['A']
    assert payloads[-1]['dns_servers'] == ['192.0.2.53']
    assert payloads[-1]['nodes'][0]['server'] == 'example.invalid'
    assert result['dns_servers'] == ['192.0.2.53']
    assert result['history'] == [10]
    result = client.post('/api/nodes/latency', json={'names': ['A']}).get_json()['results']['A']
    assert result['history'] == [10, 10]
    assert client.post('/api/nodes/latency/settings', json={'dns_servers': []}).status_code == 200
    result = client.post('/api/nodes/latency', json={'names': ['A']}).get_json()['results']['A']
    assert payloads[-1]['dns_servers'] == []
    assert result['history'] == [10]
