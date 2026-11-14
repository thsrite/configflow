"""Managed node-test DNS never publishes unrelated drafts or contacts a real host."""
from copy import deepcopy
import json
from types import SimpleNamespace

import pytest
import requests
import yaml

from backend.agents import node_test_dns as dns
from backend.agents import rule_sync_store as store
from backend.agents.deployment_bundle import build_deployment_bundle
from backend.agents.manager import AgentManager
from backend.common import config as config_store
from backend.common.config_repository import ProfileRepository


@pytest.fixture
def env(tmp_path, monkeypatch):
    previous = config_store._repository
    repository = ProfileRepository(tmp_path)
    config_store.set_repository(repository)
    manager = AgentManager(repository)
    agent = manager.register_agent({'name': 'DNS', 'host': '192.0.2.2', 'port': 8081,
                                    'service_type': 'oxidns'})
    agent = manager.get_agent_by_id(agent['id'])
    document = {'plugins': [
        {'tag': 'forward_local', 'type': 'forward', 'args': {'upstreams': [{'addr': '192.0.2.53'}]}},
        {'tag': 'main', 'type': 'sequence', 'args': [{'exec': '$forward_local'}, {'exec': 'accept'}]},
        {'tag': 'server_udp', 'type': 'udp_server', 'args': {'listen': '0.0.0.0:53', 'entry': 'main'}},
        {'tag': 'kept', 'type': 'domain_set', 'args': {'files': ['rules/kept.txt']}},
    ]}
    repository.save_profile('default', {'oxidns': {'config_mode': 'native',
                            'native_config': yaml.safe_dump(document, sort_keys=False)}})
    source, _ = repository.get_publish_snapshot('default')
    bundle = build_deployment_bundle(agent, source, source['oxidns']['native_config'],
        pinned_files=[{'path': 'rules/kept.txt', 'role': 'rule', 'content': b'full:old.test\n'}],
        deployment_id='baseline')
    agent_id = agent['id']

    def enroll(value, snapshot):
        ident = value.manifest['deployment_id']
        manager.begin_deployment(agent_id, ident)
        manager._remember_deployment_content(agent_id, value, '', source=snapshot)
        store.capture(repository, agent_id, value, snapshot, '')
        manager.record_deployment(agent_id, ident, {'status': 'succeeded', 'success': True,
                                                   'config_version': value.config_version})
        manager.finish_deployment_preparation(agent_id, ident)

    enroll(bundle, source)
    result = SimpleNamespace(repository=repository, manager=manager, agent_id=agent_id,
        source=source, baseline=bundle, live=bundle, jobs=[], uploads=[], remote_status='succeeded',
        verify_calls=[], published_sources=[], enroll=enroll)
    monkeypatch.setattr(requests.sessions.Session, 'request',
                        lambda *_a, **_k: pytest.fail('unexpected network'))
    monkeypatch.setattr(dns, '_enqueue', lambda key, callback: result.jobs.append(callback))
    monkeypatch.setattr(dns, 'fetch_snapshot', lambda *_args: result.live)

    def publish(identity, candidate, **kwargs):
        assert identity == agent_id
        result.uploads.append(candidate)
        result.published_sources.append(deepcopy(kwargs['source_snapshot']))
        if result.remote_status == 'succeeded':
            result.live = candidate
            enroll(candidate, kwargs['source_snapshot'])
        else:
            manager.record_deployment(agent_id, candidate.manifest['deployment_id'], {
                'status': result.remote_status, 'success': result.remote_status != 'failed',
                'submitted': result.remote_status != 'failed'})
        return {'status': result.remote_status, 'success': result.remote_status != 'failed'}

    monkeypatch.setattr(manager, 'publish_deployment', publish)
    monkeypatch.setattr(manager, 'get_deployment', lambda identity, ident:
                        deepcopy(manager.get_agent_by_id(identity)['deployments'][ident]))

    def verify(endpoint):
        result.verify_calls.append(endpoint)
        return {'success': True, 'message': '真实 DNS 可用'}

    result.verify = verify
    result.payload = {'agent_id': agent_id, 'listen_address': '192.0.2.2',
                      'port': 15353, 'upstream': '192.0.2.53'}
    result.create = lambda **extra: dns.create(manager, {**result.payload, **extra}, verify=verify)
    result.state = lambda eid: dns.status(manager, agent_id, eid, verify=verify)

    def drain():
        while result.jobs:
            result.jobs.pop(0)()
    result.drain = drain
    yield result
    config_store.set_repository(previous)


def test_create_is_explicit_async_pins_rules_and_preserves_pending_draft(env):
    env.repository.update_profile_transaction('default', lambda p: p['oxidns'].update(
        native_config=p['oxidns']['native_config'] + '\nfuture_setting: keep-draft\n'))
    before_system = deepcopy(env.repository.get_system()['system_config'])
    created = env.create()
    assert created['status'] == 'preparing'
    assert not env.uploads
    env.drain()
    ready = env.state(created['endpoint_id'])
    assert ready['status'] == 'ready'
    assert dns.resolve(env.manager, {'agent_id': env.agent_id, 'endpoint_id': created['endpoint_id']}) == '192.0.2.2:15353'
    assert env.repository.get_system()['system_config'] == before_system
    assert len(env.uploads) == 1
    files = store.bundle_files(env.uploads[0])
    assert files['rules/kept.txt'] == (b'full:old.test\n', 'rule')
    deployed = yaml.safe_load(files['config.yaml'][0])
    assert 'future_setting' not in deployed
    draft = yaml.safe_load(env.repository.get_profile('default')['oxidns']['native_config'])
    assert draft['future_setting'] == 'keep-draft'
    assert len(draft['plugins']) == 8
    assert env.verify_calls == ['192.0.2.2:15353']


def test_repeated_create_reuses_ready_endpoint_without_publication(env):
    created = env.create(); env.drain()
    again = env.create()
    assert again['endpoint_id'] == created['endpoint_id']
    assert again['status'] == 'ready'
    assert len(env.uploads) == 1


def test_unknown_publication_never_becomes_ready_or_modifies_draft(env):
    before = env.repository.get_profile('default')['oxidns']
    env.remote_status = 'unknown'
    created = env.create(); env.drain()
    assert env.state(created['endpoint_id'])['status'] == 'publishing'
    env.drain()
    assert not env.verify_calls
    assert env.repository.get_profile('default')['oxidns'] == before
    with pytest.raises(ValueError):
        dns.resolve(env.manager, {'agent_id': env.agent_id, 'endpoint_id': created['endpoint_id']})


def test_unknown_publication_converges_without_duplicate_upload(env):
    env.remote_status = 'unknown'
    created = env.create(); env.drain()
    env.enroll(env.uploads[0], env.published_sources[0])
    assert env.state(created['endpoint_id'])['status'] == 'publishing'
    env.drain()
    assert env.state(created['endpoint_id'])['status'] == 'ready'
    assert len(env.uploads) == 1


def test_failed_publication_does_not_touch_draft_or_existing_dns_settings(env):
    env.repository.save_system({'system_config': {'node_test_dns_config': {'mode': 'custom', 'servers': ['192.0.2.9']}}})
    before = deepcopy(env.repository.get_system()['system_config'])
    draft = env.repository.get_profile('default')['oxidns']
    env.remote_status = 'failed'
    created = env.create(); env.drain()
    assert env.state(created['endpoint_id'])['status'] == 'failed'
    assert env.repository.get_profile('default')['oxidns'] == draft
    assert env.repository.get_system()['system_config'] == before


def test_verification_failure_keeps_binding_unavailable(env):
    created = dns.create(env.manager, env.payload, verify=lambda _: {'success': False, 'message': 'Fake-IP'})
    env.drain()
    assert env.state(created['endpoint_id'])['status'] != 'ready'
    with pytest.raises(ValueError):
        dns.resolve(env.manager, {'agent_id': env.agent_id, 'endpoint_id': created['endpoint_id']})


def test_live_port_collision_rejects_without_publishing(env):
    created = env.create(port=53)
    env.drain()
    assert env.state(created['endpoint_id'])['status'] == 'failed'
    assert not env.uploads


def test_unsubmitted_failure_can_be_removed_without_contacting_offline_agent(env, monkeypatch):
    created = env.create(port=53); env.drain()
    assert env.state(created['endpoint_id'])['status'] == 'failed'
    monkeypatch.setattr(dns, 'fetch_snapshot', lambda *_a: pytest.fail('unsubmitted failure needs no network'))
    assert dns.remove(env.manager, env.agent_id, created['endpoint_id'])['status'] == 'removed'
    assert env.manager.delete_agent(env.agent_id)


def test_missing_published_baseline_and_ha_are_rejected(env):
    env.manager._update_agents(lambda agents: agents[0].update(applied_content={}))
    with pytest.raises(ValueError, match='先.*发布'):
        env.create()
    env.manager._update_agents(lambda agents: agents[0].update(oxidns_ha={'enabled': True}))
    with pytest.raises(ValueError, match='高可用'):
        env.create()


def test_remove_refuses_global_reference_then_removes_only_owned_plugins(env):
    created = env.create(); env.drain()
    binding = {'agent_id': env.agent_id, 'endpoint_id': created['endpoint_id']}
    env.repository.save_system({'system_config': {'node_test_dns_config': {'mode': 'managed', 'managed': binding}}})
    with pytest.raises(ValueError, match='引用'):
        dns.remove(env.manager, **binding)
    env.repository.save_system({'system_config': {'node_test_dns_config': {'mode': 'system'}}})
    assert dns.remove(env.manager, **binding)['status'] == 'removing'
    env.drain()
    assert env.state(created['endpoint_id'])['status'] == 'removed'
    draft = yaml.safe_load(env.repository.get_profile('default')['oxidns']['native_config'])
    assert len(draft['plugins']) == 4
    assert store.bundle_files(env.uploads[-1])['rules/kept.txt'][0] == b'full:old.test\n'


def test_manual_owned_plugin_change_prevents_remove(env):
    created = env.create(); env.drain()
    def edit(profile):
        d = yaml.safe_load(profile['oxidns']['native_config'])
        d['plugins'][-1]['args']['listen'] = '192.0.2.2:15444'
        profile['oxidns']['native_config'] = yaml.safe_dump(d)
    with pytest.raises(ValueError, match='手工修改'):
        env.repository.update_profile_transaction('default', edit)
    assert len(env.uploads) == 1


def test_agent_binding_deletion_and_profile_mode_are_protected(env):
    env.create(); env.drain()
    with pytest.raises(ValueError, match='先删除'):
        env.manager.delete_agent(env.agent_id)
    with pytest.raises(ValueError, match='先删除'):
        env.manager.update_agent(env.agent_id, {'host': '192.0.2.4'})
    with pytest.raises(ValueError, match='原生配置'):
        env.repository.save_profile('default', {'oxidns': {'config_mode': 'template'}})
    with pytest.raises(ValueError, match='独立配置空间'):
        env.manager.register_agent({'name': 'Other DNS', 'host': '192.0.2.4', 'service_type': 'oxidns'})


def test_shared_profile_rejects_creation_and_options_explain(env):
    env.manager.register_agent({'name': 'Other DNS', 'host': '192.0.2.4', 'service_type': 'oxidns'})
    with pytest.raises(ValueError, match='独立配置空间'):
        env.create()
    assert dns.options(env.manager)['agents'][0]['eligible'] is False


def test_save_binding_transaction_cannot_race_endpoint_removal(env):
    created = env.create(); env.drain()
    binding = {'agent_id': env.agent_id, 'endpoint_id': created['endpoint_id']}
    assert dns.resolve(env.manager, binding)
    dns.remove(env.manager, **binding)
    def save(system):
        dns.validate_binding(system, binding)
        system['system_config']['node_test_dns_config'] = {'mode': 'managed', 'managed': binding}
    with pytest.raises(ValueError, match='尚未就绪'):
        env.repository.update_system_transaction(save)


def test_reregistration_keeps_managed_ownership(env):
    created = env.create(); env.drain()
    agent = env.manager.get_agent_by_id(env.agent_id)
    env.manager.register_agent({'name': agent['name'], 'host': agent['host'], 'port': agent['port'],
        'service_type': 'oxidns'}, existing_token=agent['token'])
    assert env.state(created['endpoint_id'])['status'] == 'ready'


def test_options_never_exposes_tokens_and_records_scope(env):
    result = dns.options(env.manager)
    agent = result['agents'][0]
    assert agent['profile_id'] == 'default'
    assert agent['address'] == '192.0.2.2'
    assert env.manager.get_agent_by_id(env.agent_id)['token'] not in str(result)


def test_pins_latest_active_rule_bytes_instead_of_old_receipt(env):
    agent = env.manager.get_agent_by_id(env.agent_id)
    env.live = build_deployment_bundle(agent, env.source, env.source['oxidns']['native_config'],
        pinned_files=[{'path': 'rules/kept.txt', 'role': 'rule', 'content': b'full:refreshed.test\n'}],
        deployment_id='baseline')
    created = env.create(); env.drain()
    assert env.state(created['endpoint_id'])['status'] == 'ready'
    assert store.bundle_files(env.uploads[0])['rules/kept.txt'][0] == b'full:refreshed.test\n'


def test_normal_generation_keeps_owned_listener_and_validates_native_schema(env):
    from backend.converters.oxidns import generate_oxidns_config
    created = env.create(); env.drain()
    source, _ = env.repository.get_publish_snapshot('default')
    rendered = yaml.safe_load(generate_oxidns_config(source))
    record = env.manager.get_agent_by_id(env.agent_id)[dns.FIELD][created['endpoint_id']]
    assert dns._owned(rendered, record)
    assert all(p.get('type') != 'cache' for p in record['plugins'])


@pytest.mark.parametrize('value', ['tls://dns.example/', 'https://@dns.example/dns-query',
    'https://dns.example/dns-query?', 'https://dns.example/#'])
def test_managed_upstreams_share_strict_custom_dns_validation(env, value):
    with pytest.raises(ValueError):
        env.create(upstream=value, bootstrap=['192.0.2.53'])
    assert not env.jobs


def test_managed_upstream_can_use_device_loopback(env):
    created = env.create(upstream='127.0.0.1:5353')
    assert created['upstream'] == 'udp://127.0.0.1:5353'


def test_routes_require_separate_save_and_protect_selected_endpoint(env, monkeypatch):
    from backend.common import agent_manager
    from backend.routes import nodes
    from backend.test_flow_ui_apis import make_client
    monkeypatch.setattr(agent_manager, 'get_agent_manager', lambda: env.manager)
    monkeypatch.setattr(nodes, '_verify_managed_dns', env.verify)
    client = make_client(env.repository)
    before = deepcopy(env.repository.get_system()['system_config'])
    offered = client.get('/api/nodes/latency/dns/managed').get_json()['agents'][0]
    assert offered['eligible'] and offered['upstreams'][0]['validation'] == 'unverified'
    response = client.post('/api/nodes/latency/dns/managed', json=selected_payload(env, offered['upstreams'][0]))
    assert response.status_code == 200
    created = response.get_json()
    binding = {'agent_id': env.agent_id, 'endpoint_id': created['endpoint_id']}
    path = '/api/nodes/latency/dns/managed/' + env.agent_id + '/' + created['endpoint_id']
    assert client.post('/api/nodes/latency/settings', json={'dns': {'mode': 'managed', 'managed': binding}}).status_code == 400
    env.drain()
    assert client.get(path).get_json()['status'] == 'ready'
    assert env.repository.get_system()['system_config'] == before
    assert client.post('/api/nodes/latency/settings', json={'dns': {'mode': 'managed', 'managed': binding}}).status_code == 200
    assert client.delete(path).status_code == 400
    assert client.post('/api/nodes/latency/settings', json={'dns': {'mode': 'system'}}).status_code == 200
    assert client.delete(path).get_json()['status'] == 'removing'
    env.drain()
    assert client.get(path).get_json()['status'] == 'removed'


def test_pending_managed_operation_reserves_agent_against_normal_publication(env):
    env.create()
    result = env.manager.begin_deployment(env.agent_id, 'unrelated-publication')
    assert result['http_status'] == 409
    assert not result['success']
    env.drain()
    result = env.manager.begin_deployment(env.agent_id, 'unrelated-publication')
    assert result['success']
    env.manager.finish_deployment_preparation(env.agent_id, 'unrelated-publication')


def test_profile_clone_does_not_copy_device_specific_listener(env):
    created = env.create(); env.drain()
    env.repository.clone_profile('default', {'id': 'copied', 'name': 'Copied'})
    copied = yaml.safe_load(env.repository.get_profile('copied')['oxidns']['native_config'])
    assert len(copied['plugins']) == 4
    assert env.state(created['endpoint_id'])['status'] == 'ready'


def test_guard_routes_return_json_conflict_instead_of_server_error(env, monkeypatch):
    from backend.routes import agents
    from backend.test_flow_ui_apis import make_client
    env.create(); env.drain()
    monkeypatch.setattr(agents, 'get_agent_manager', lambda: env.manager)
    client = make_client(env.repository)
    responses = [
        client.delete('/api/agents/' + env.agent_id),
        client.put('/api/agents/' + env.agent_id, json={'host': '192.0.2.4'}),
        client.post('/api/agents', json={'name': 'Second DNS', 'host': '192.0.2.4', 'service_type': 'oxidns'}),
        client.post('/api/oxidns/settings', json={'config_mode': 'template'}),
    ]
    for response in responses:
        assert response.status_code == 409
        assert response.is_json and response.get_json()['success'] is False


def test_settings_reads_do_not_change_system_revision(env):
    created = env.create(); env.drain()
    binding = {'agent_id': env.agent_id, 'endpoint_id': created['endpoint_id']}
    before = env.repository.get_system()
    assert dns.resolve(env.manager, binding)
    assert dns.resolve(env.manager, binding)
    assert dns.status(env.manager, **binding)['status'] == 'ready'
    assert env.repository.get_system() == before


def test_normal_republication_requires_fresh_receipt_check_then_recovers(env):
    created = env.create(); env.drain()
    binding = {'agent_id': env.agent_id, 'endpoint_id': created['endpoint_id']}
    assert dns.resolve(env.manager, binding)
    source = env.published_sources[-1]
    candidate = build_deployment_bundle(env.manager.get_agent_by_id(env.agent_id), source,
        source['oxidns']['native_config'], deployment_id='normal-publish',
        pinned_files=[{'path': 'rules/kept.txt', 'role': 'rule', 'content': b'full:old.test\n'}])
    env.enroll(candidate, source)
    with pytest.raises(ValueError, match='配置已变化'):
        dns.validate_binding(env.repository.get_system(), binding)
    assert dns.resolve(env.manager, binding) == '192.0.2.2:15353'
    assert dns.validate_binding(env.repository.get_system(), binding) == '192.0.2.2:15353'


def publish_forward_fixture(env, upstreams, *, network=None, **forward_options):
    source = deepcopy(env.source)
    document = yaml.safe_load(source['oxidns']['native_config'])
    document['plugins'][0]['args'] = {'upstreams': upstreams, **forward_options}
    if network is not None:
        document['network'] = network
    source['oxidns']['native_config'] = yaml.safe_dump(document, sort_keys=False)
    env.repository.save_profile('default', {'oxidns': source['oxidns']})
    candidate = build_deployment_bundle(env.manager.get_agent_by_id(env.agent_id), source,
        source['oxidns']['native_config'], deployment_id='fixture-' + str(len(upstreams)) + '-' + str(env.repository.config_revision('default')),
        pinned_files=[{'path': 'rules/kept.txt', 'role': 'rule', 'content': b'full:old.test\n'}])
    env.enroll(candidate, source)
    env.live = candidate
    return source


def selected_payload(env, candidate=None):
    candidate = candidate or dns.options(env.manager)['agents'][0]['upstreams'][0]
    return {key: value for key, value in env.payload.items() if key != 'upstream'} | {'upstream_id': candidate['id']}


def test_candidates_only_use_confirmed_upstreams_and_are_unverified(env):
    before = dns.options(env.manager)['agents'][0]['upstreams']
    assert len(before) == 1 and before[0]['eligible'] and before[0]['validation'] == 'unverified'
    assert before[0]['address'] == 'udp://192.0.2.53'
    assert 'forward_local' in before[0]['label']
    assert before[0]['label'].startswith('本地上游（forward_local）')
    def change(profile):
        document = yaml.safe_load(profile['oxidns']['native_config'])
        document['plugins'][0]['args']['upstreams'] = [{'addr': '192.0.2.99'}]
        profile['oxidns']['native_config'] = yaml.safe_dump(document)
    env.repository.update_profile_transaction('default', change)
    assert dns.options(env.manager)['agents'][0]['upstreams'] == before
    created = dns.create(env.manager, selected_payload(env), verify=env.verify); env.drain()
    assert env.state(created['endpoint_id'])['status'] == 'ready'
    document = yaml.safe_load(store.bundle_files(env.uploads[0])['config.yaml'][0])
    assert all('192.0.2.99' not in str(p) for p in document['plugins'])
    assert '192.0.2.99' in env.repository.get_profile('default')['oxidns']['native_config']
    assert len(dns.options(env.manager)['agents'][0]['upstreams']) == 1  # owned forward excluded


def test_selected_upstream_copies_all_supported_parameters(env):
    upstream = {'addr': 'https://dns.example/dns-query', 'bootstrap': '192.0.2.53:5353',
        'dial_addr': '192.0.2.54', 'bootstrap_version': 4, 'insecure_skip_verify': False,
        'timeout': '3s', 'idle_timeout': '7s', 'max_conns': 8, 'min_conns': 1, 'tag': 'original',
        'enable_http3': False, 'enable_pipeline': False, 'so_mark': 42, 'bind_to_device': 'eth0'}
    publish_forward_fixture(env, [upstream], concurrent=1, response_selection='prefer_positive', short_circuit=False)
    created = dns.create(env.manager, selected_payload(env), verify=env.verify); env.drain()
    assert env.state(created['endpoint_id'])['status'] == 'ready'
    record = env.manager.get_agent_by_id(env.agent_id)[dns.FIELD][created['endpoint_id']]
    assert record['plugins'][0]['args'] == {'upstreams': [upstream], 'concurrent': 1,
        'response_selection': 'prefer_positive', 'short_circuit': False}
    again = dns.create(env.manager, selected_payload(env), verify=env.verify)
    assert again['endpoint_id'] == created['endpoint_id'] and len(env.uploads) == 1


def test_reordered_or_changed_published_candidates_reject_stale_id(env):
    publish_forward_fixture(env, [{'addr': '192.0.2.53'}, {'addr': '192.0.2.54'}])
    stale = selected_payload(env)
    publish_forward_fixture(env, [{'addr': '192.0.2.54'}, {'addr': '192.0.2.53'}])
    with pytest.raises(ValueError, match='已变更'):
        dns.create(env.manager, stale, verify=env.verify)
    assert not env.jobs and not env.uploads


@pytest.mark.parametrize('profile', [
    {'proxy': {'socks5': 'user:private-secret@127.0.0.1:1080'}},
    {'proxy': 'direct', 'resolver': 'system'},
])
def test_global_default_outbound_is_not_guessed_from_profile_name(env, profile):
    publish_forward_fixture(env, [{'addr': '192.0.2.53'}],
        network={'outbound': {'default': 'direct', 'profiles': {'direct': profile}}})
    candidate = dns.options(env.manager)['agents'][0]['upstreams'][0]
    assert not candidate['eligible'] and '全局默认出站' in candidate['reason']
    assert 'private-secret' not in json.dumps(candidate)
    with pytest.raises(ValueError, match='全局默认出站'):
        dns.create(env.manager, selected_payload(env, candidate), verify=env.verify)
    assert not env.jobs and not env.uploads


def test_global_outbound_change_invalidates_previously_selected_candidate(env):
    stale = selected_payload(env)
    publish_forward_fixture(env, [{'addr': '192.0.2.53'}], network={'outbound': {
        'default': 'remote', 'profiles': {'remote': {'proxy': {'socks5': '127.0.0.1:1080'}}}}})
    with pytest.raises(ValueError, match='已变更'):
        dns.create(env.manager, stale, verify=env.verify)
    assert not env.jobs and not env.uploads


def test_candidate_rechecked_after_async_preparation_wait(env, monkeypatch):
    created = dns.create(env.manager, selected_payload(env), verify=env.verify)
    baseline = dns._baseline(env.manager, env.manager.get_agent_by_id(env.agent_id))
    changed = deepcopy(baseline)
    document = yaml.safe_load(changed['source']['oxidns']['native_config'])
    document['plugins'][0]['args']['upstreams'] = [{'addr': '192.0.2.99'}]
    changed['source']['oxidns']['native_config'] = yaml.safe_dump(document)
    monkeypatch.setattr(dns, '_baseline', lambda *_args: changed)
    env.drain()
    assert env.state(created['endpoint_id'])['status'] == 'failed'
    assert not env.uploads


def test_candidate_is_id_only_and_manual_compatibility_remains(env):
    payload = selected_payload(env)
    with pytest.raises(ValueError, match='只提交'):
        dns.create(env.manager, {**payload, 'upstream': '192.0.2.99'}, verify=env.verify)
    with pytest.raises(ValueError, match='只提交'):
        dns.create(env.manager, {**payload, 'bootstrap': []}, verify=env.verify)
    created = env.create(); env.drain()
    assert env.state(created['endpoint_id'])['status'] == 'ready'


def test_unsupported_or_secret_upstream_never_leaks_credentials(env):
    # Store a receipt directly: an upstream may be valid in a newer OxiDNS but
    # not safely reusable by this version's narrowly scoped provisioner.
    publish_forward_fixture(env, [{'addr': 'https://user:private-password@dns.example/dns-query?token=private-token'},
        {'addr': '192.0.2.53', 'future_secret': 'private-extension'},
        {'addr': '192.0.2.53', 'socks5': 'user:private-socks@127.0.0.1:1080'}])
    candidates = dns.options(env.manager)['agents'][0]['upstreams']
    assert len(candidates) == 3 and all(not c['eligible'] and c['reason'] for c in candidates)
    assert all(value not in json.dumps(candidates) for value in
               ('private-password', 'private-token', 'private-extension', 'private-socks'))
    for candidate in candidates:
        with pytest.raises(ValueError):
            dns.create(env.manager, selected_payload(env, candidate), verify=env.verify)
    assert not env.jobs


def test_eligible_existing_candidate_still_requires_real_dns_verification(env):
    created = dns.create(env.manager, selected_payload(env), verify=lambda _: {'success': False})
    env.drain()
    assert env.state(created['endpoint_id'])['status'] == 'failed'
    with pytest.raises(ValueError):
        dns.resolve(env.manager, {'agent_id': env.agent_id, 'endpoint_id': created['endpoint_id']})


def test_failed_attempt_that_installed_nothing_does_not_keep_a_second_agent_out(env):
    from backend.agents.manager import AgentDeploymentConflict
    failed = env.create(port=53); env.drain()
    assert env.state(failed['endpoint_id'])['status'] == 'failed'
    system = env.repository.get_system()
    assert not dns.holds_listener(env.manager.get_agent_by_id(env.agent_id))
    assert dns.registration_blocker(system, 'default') is None
    second = env.manager.register_agent({'name': 'Second-DNS', 'host': '192.0.2.3', 'port': 8081, 'service_type': 'oxidns'})
    assert env.manager.delete_agent(second['id'])
    # A published listener still excludes it, and says what to do about it.
    created = env.create(); env.drain()
    assert env.state(created['endpoint_id'])['status'] == 'ready'
    with pytest.raises(AgentDeploymentConflict, match='独立配置空间'):
        env.manager.register_agent({'name': 'Second-DNS', 'host': '192.0.2.3', 'port': 8081, 'service_type': 'oxidns'})
    system = env.repository.get_system()
    assert '测速设置' in dns.registration_blocker(system, 'default')
    assert dns.registration_blocker(system, 'default', ('DNS',)) is None     # reinstalling that same Agent
    assert dns.registration_blocker(system, 'another-profile') is None


def test_next_attempt_replaces_a_failure_that_installed_nothing(env):
    failed = env.create(port=53); env.drain()
    assert env.state(failed['endpoint_id'])['status'] == 'failed'
    # The same request no longer needs a manual delete first.
    retried = env.create(port=53); env.drain()
    assert retried['endpoint_id'] != failed['endpoint_id']
    records = env.manager.get_agent_by_id(env.agent_id)[dns.FIELD]
    assert records[failed['endpoint_id']]['status'] == 'removed'
    created = env.create(); env.drain()
    assert env.state(created['endpoint_id'])['status'] == 'ready'
    assert [e['endpoint_id'] for e in dns.options(env.manager)['agents'][0]['endpoints']] == [created['endpoint_id']]


def test_refused_registration_is_logged_notified_and_expires(env, monkeypatch, caplog):
    from backend import notifications
    from backend.routes import agents
    from backend.test_flow_ui_apis import make_client
    env.create(); env.drain()
    monkeypatch.setattr(agents, 'get_agent_manager', lambda: env.manager)
    client = make_client(env.repository)
    payload = {'name': 'Second-DNS', 'host': '192.0.2.3', 'port': 8081, 'service_type': 'oxidns'}
    with caplog.at_level('WARNING'):
        for _ in range(3):
            response = client.post('/api/agents/register', json=payload)
            assert response.status_code == 409 and '独立配置空间' in response.get_json()['message']
    assert sum('Agent 注册被拒绝' in record.getMessage() for record in caplog.records) == 3
    listed = [item for item in notifications.snapshot() if item['source'] == notifications.REGISTRATION_SOURCE]
    assert len(listed) == 1
    notice = listed[0]
    assert 'Second-DNS' in notice['title'] and '192.0.2.3' in notice['title']
    assert '测速设置' in notice['message'] and notice['occurrences'] == 1     # one per minute, not per retry
    assert notice['target']['path'] == '/agents'
    # The device stopped retrying (it registered or was uninstalled): the notice goes away.
    monkeypatch.setattr(notifications, 'REGISTRATION_FRESH_SECONDS', 0)
    assert not [item for item in notifications.snapshot() if item['source'] == notifications.REGISTRATION_SOURCE]
    # A reinstalled device without its old credentials is told what to do as well.
    known = env.manager.get_agent_by_id(env.agent_id)
    monkeypatch.setattr(notifications, 'REGISTRATION_FRESH_SECONDS', 300)
    response = client.post('/api/agents/register', json={'name': known['name'], 'host': known['host'],
                                                         'port': 8081, 'service_type': 'oxidns'})
    assert response.status_code == 401
    listed = [item for item in notifications.snapshot() if item['source'] == notifications.REGISTRATION_SOURCE]
    assert len(listed) == 1 and '删除旧记录' in listed[0]['message']


def test_install_command_says_a_second_oxidns_would_be_refused(env, monkeypatch):
    from backend.routes import agents
    from backend.test_flow_ui_apis import make_client
    env.create(); env.drain()
    monkeypatch.setattr(agents, 'get_agent_manager', lambda: env.manager)
    client = make_client(env.repository)
    query = {'server_url': 'https://config.test', 'type': 'oxidns', 'name': 'Second-DNS'}
    refused = client.get('/api/agents/install-script', query_string=query)
    assert refused.status_code == 400 and '测速设置' in refused.get_json()['message']
    assert client.get('/api/agents/install-script', query_string={**query, 'type': 'aio'}).status_code == 400
    # Reinstalling the Agent that owns the listener, and other services, are unaffected.
    assert client.get('/api/agents/install-script', query_string={**query, 'name': 'DNS'}).status_code == 200
    assert client.get('/api/agents/install-script', query_string={**query, 'type': 'mihomo'}).status_code == 200
