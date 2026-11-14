"""Public settings contract and independent worker checks, with no production DNS."""
import json
import subprocess
from types import SimpleNamespace

import pytest

from backend.common import config
from backend.common.config_repository import ProfileRepository
from backend.test_flow_ui_apis import make_client
from backend.utils import node_dns as dns, node_latency as latency


@pytest.fixture
def repo(tmp_path, monkeypatch):
    repository = ProfileRepository(tmp_path)
    config.set_repository(repository)
    monkeypatch.setattr(latency, '_probe_binary', lambda: '/test/node-probe')
    return repository


def test_legacy_migration_is_read_only_and_does_not_adopt_manual_listener(repo):
    repo.update_system_transaction(lambda system: system['system_config'].update(
        node_test_dns=['192.168.0.2:15353']))
    before = repo.get_system()
    assert dns.load_config() == {'mode': 'custom', 'servers': ['192.168.0.2:15353'], 'bootstrap': []}
    assert repo.get_system() == before


@pytest.mark.parametrize(('raw', 'expected'), [
    ('https://DNS.EXAMPLE', 'https://dns.example/dns-query'),
    ('https://192.0.2.1:443/dns-query', 'https://192.0.2.1/dns-query'),
    ('tls://DNS.EXAMPLE:853', 'tls://dns.example'),
    ('tls://192.0.2.1:8853', 'tls://192.0.2.1:8853'),
    ('192.0.2.53:053', '192.0.2.53'),
])
def test_canonical_dns_addresses(raw, expected):
    assert dns.normalize_dns_servers([raw, raw]) == [expected]


@pytest.mark.parametrize('raw', [
    'https://user:password@dns.example/dns-query', 'https://dns.example/dns-query?',
    'https://dns.example/dns-query#', 'https://dns.example/dns-query?token=secret',
    'https://dns.example:0', 'https://dns.example:', 'https://dns.example:65536',
    'https://dns.example/a\nb', 'https://dns.example/dns%zz', 'https://dns.example/dns\x7f', 'https://dns.example\\evil', 'tls://dns.example/',
    'http://192.0.2.53', 'udp://192.0.2.53', 'https://[::1]/dns-query',
    'https://0.0.0.0/dns-query', 'https://224.0.0.1/dns-query', 'tls://bad_host', 'tls://dns.example..',
    '192.0.2.53:0', '192.0.2.53:65536', '192.0.2.53:', 'dns.example',
])
def test_invalid_upstreams_are_rejected_without_echoing_secrets(raw):
    with pytest.raises(ValueError) as error:
        dns.normalize_dns_servers([raw])
    assert 'password' not in str(error.value) and 'secret' not in str(error.value)


def test_custom_encrypted_hostname_requires_explicit_literal_bootstrap():
    with pytest.raises(ValueError, match='引导'):
        dns.normalize_config({'mode': 'custom', 'servers': ['tls://dns.example']})
    with pytest.raises(ValueError, match='引导'):
        dns.normalize_config({'mode': 'custom', 'servers': ['tls://dns.example'], 'bootstrap': ['tls://dns.example']})
    assert dns.normalize_config({'mode': 'custom', 'servers': ['tls://192.0.2.1']})['bootstrap'] == []


def test_custom_save_is_global_and_does_not_publish_or_change_profile(repo, monkeypatch):
    from backend.common import agent_manager
    # Load lazy route imports before replacing a function they may import by name.
    client = make_client(repo)
    monkeypatch.setattr(agent_manager, 'get_agent_manager', lambda: pytest.fail('Custom DNS must not provision an Agent'))
    shared, profile = repo.get_shared(), repo.get_profile('default')
    value = {'mode': 'custom', 'servers': ['https://DNS.EXAMPLE'], 'bootstrap': ['192.0.2.53:53']}
    response = client.post('/api/nodes/latency/settings', json={'dns': value})
    assert response.status_code == 200
    body = response.get_json()
    assert body['dns']['servers'] == ['https://dns.example/dns-query']
    assert body['dns_bootstrap'] == ['192.0.2.53']
    assert repo.get_shared() == shared
    current = repo.get_profile('default')
    for key in profile.keys() - {'system_config', '_revision'}:
        assert current[key] == profile[key]
    assert dns.load_config() == body['dns']
    assert latency.settings()['dns_bootstrap'] == ['192.0.2.53']
    # System mode ignores stale hidden form fields, including its previous binding.
    system = client.post('/api/nodes/latency/settings', json={'dns': {
        'mode': 'system', 'servers': ['invalid'], 'bootstrap': ['invalid'], 'managed': {'agent_id': 'old'}}})
    assert system.get_json()['dns_servers'] == []
    assert dns.load_config() == {'mode': 'system', 'servers': [], 'bootstrap': []}


def test_removed_managed_mode_keeps_querying_its_listener_as_custom_dns(repo):
    # "OxiDNS 托管" was removed. Its listener stays on the device and its address
    # was always mirrored into node_test_dns, so tests keep working unchanged.
    value = {'mode': 'managed', 'servers': [], 'bootstrap': [], 'managed': {'agent_id': 'dns', 'endpoint_id': 'e'}}
    repo.update_system_transaction(lambda system: system['system_config'].update(
        node_test_dns_config=value, node_test_dns=['192.0.2.2:15353']))
    before = repo.get_system()
    expected = {'mode': 'custom', 'servers': ['192.0.2.2:15353'], 'bootstrap': []}
    assert dns.load_config() == expected
    assert dns.effective_settings() == {'dns_servers': ['192.0.2.2:15353'], 'dns_bootstrap': []}
    client = make_client(repo)
    shown = client.get('/api/nodes/latency/settings').get_json()
    assert shown['dns'] == expected and shown['dns_servers'] == ['192.0.2.2:15353'] and 'managed_status' not in shown
    assert repo.get_system() == before                       # reading migrates nothing on disk
    # Saving what is shown stores the ordinary custom form.
    assert client.post('/api/nodes/latency/settings', json={'dns': shown['dns']}).status_code == 200
    assert repo.get_system()['system_config']['node_test_dns_config'] == expected


def test_removed_managed_mode_without_a_recorded_address_falls_back_to_system_dns(repo):
    value = {'mode': 'managed', 'servers': [], 'bootstrap': [], 'managed': {'agent_id': 'missing', 'endpoint_id': 'gone'}}
    repo.update_system_transaction(lambda system: system['system_config'].update(node_test_dns_config=value))
    assert dns.load_config() == {'mode': 'system', 'servers': [], 'bootstrap': []}


def test_managed_mode_can_no_longer_be_saved_or_reached(repo):
    client = make_client(repo)
    before = repo.get_system()
    response = client.post('/api/nodes/latency/settings', json={'dns': {
        'mode': 'managed', 'managed': {'agent_id': 'a', 'endpoint_id': 'e'}}})
    assert response.status_code == 400 and '系统 DNS 或自定义 DNS' in response.get_json()['message']
    assert repo.get_system() == before
    assert client.get('/api/nodes/latency/dns/managed').status_code == 404
    assert client.post('/api/nodes/latency/dns/managed', json={}).status_code in (404, 405)


def test_dns_check_uses_only_unsaved_dns_config_and_preserves_history(repo, monkeypatch):
    calls = []
    repo.write_shared_json(latency.LATENCY_FILE, {'unchanged': {'history': [12]}})
    before = repo.get_system()
    def run(command, **kwargs):
        calls.append(json.loads(kwargs['input']))
        return SimpleNamespace(returncode=0, stdout=json.dumps({'version': 4, 'dns_check': {
            'success': True, 'addresses': ['192.0.2.1', '2001:db8::1'], 'latency_ms': 12}}))
    monkeypatch.setattr(dns.subprocess, 'run', run)
    response = make_client(repo).post('/api/nodes/latency/dns/check', json={'dns': {
        'mode': 'custom', 'servers': ['tls://dns.example'], 'bootstrap': ['192.0.2.53']}})
    assert response.status_code == 200 and response.get_json()['success'] is True
    assert calls == [{'operation': 'dns_check', 'timeout_ms': 5000,
                      'dns_servers': ['tls://dns.example'], 'dns_bootstrap': ['192.0.2.53']}]
    assert repo.get_system() == before
    assert repo.read_shared_json(latency.LATENCY_FILE) == {'unchanged': {'history': [12]}}


@pytest.mark.parametrize('result', [
    {'success': False, 'addresses': [], 'error_code': 'dns_fake_ip', 'error': 'private raw exception'},
    {'success': False, 'addresses': [], 'error_code': 'unknown', 'error': 'private raw exception'},
])
def test_check_failure_is_safe_and_not_a_save(repo, monkeypatch, result):
    monkeypatch.setattr(dns.subprocess, 'run', lambda *args, **kwargs: SimpleNamespace(
        returncode=0, stdout=json.dumps({'version': 4, 'dns_check': result})))
    response = make_client(repo).post('/api/nodes/latency/dns/check', json={'dns': {'mode': 'system'}})
    assert response.status_code == 200
    assert not response.get_json()['success']
    assert 'private raw exception' not in response.get_data(as_text=True)


@pytest.mark.parametrize('result', [
    {'success': True, 'addresses': []}, {'success': True, 'addresses': ['198.19.0.1']},
    {'success': True, 'addresses': ['invalid']}, {'success': True, 'addresses': [True]},
    {'success': True, 'addresses': ['192.0.2.1'], 'latency_ms': True},
])
def test_malformed_check_does_not_claim_success(repo, monkeypatch, result):
    monkeypatch.setattr(dns.subprocess, 'run', lambda *args, **kwargs: SimpleNamespace(
        returncode=0, stdout=json.dumps({'version': 4, 'dns_check': result})))
    response = make_client(repo).post('/api/nodes/latency/dns/check', json={'dns': {'mode': 'system'}})
    assert response.status_code == 503


def test_dns_check_timeout_is_bounded(repo, monkeypatch):
    def run(*args, **kwargs):
        assert kwargs['timeout'] == 10
        raise subprocess.TimeoutExpired('node-probe', 10, stderr='secret')
    monkeypatch.setattr(dns.subprocess, 'run', run)
    response = make_client(repo).post('/api/nodes/latency/dns/check', json={'dns': {'mode': 'system'}})
    assert response.get_json()['error_code'] == 'timeout'
    assert 'secret' not in response.get_data(as_text=True)


def test_changing_only_bootstrap_resets_latency_history(repo, monkeypatch):
    repo.save_shared({'nodes': [{'id': 'a', 'name': 'A', 'type': 'http', 'server': 'proxy.invalid', 'port': 8080}]})
    monkeypatch.setattr(latency, '_run_probe', lambda proxies, options: {
        proxy['name']: {'latency': 12} for proxy in proxies})
    value = {'mode': 'custom', 'servers': ['tls://dns.example'], 'bootstrap': ['192.0.2.53']}
    dns.save_config(value)
    latency.run_tests(['A'])
    assert latency.run_tests(['A'])['results']['A']['history'] == [12, 12]
    value['bootstrap'] = ['192.0.2.54']
    dns.save_config(value)
    result = latency.run_tests(['A'])['results']['A']
    assert result['history'] == [12]
    assert result['dns_bootstrap'] == ['192.0.2.54']
