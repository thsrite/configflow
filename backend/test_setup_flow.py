from copy import deepcopy
from types import SimpleNamespace

import pytest
import yaml
from flask import Flask

from backend.common import config as config_store
from backend.common.config_repository import ProfileRepository
from backend.common.setup_presets import builtin_rule_catalog, recommended_domain_rules
from backend.routes import register_blueprints
from backend.utils.subscription_cache import save_subscription_nodes

SESSION = 'a' * 32
NODE = {'id': 'node', 'name': 'Test node', 'type': 'ss', 'server': '192.0.2.1',
        'port': 443, 'cipher': 'aes-128-gcm', 'password': 'private'}
DOMAIN_RULE = {'id': 'work', 'name': '工作域名', 'behavior': 'domain', 'format': 'yaml',
               'source_type': 'content', 'content': 'payload:\n  - +.work.test', 'enabled': True}


@pytest.fixture
def setup_api(tmp_path):
    repository = ProfileRepository(tmp_path)
    previous = config_store._repository
    config_store.set_repository(repository)
    app = Flask(__name__)
    register_blueprints(app)
    yield app.test_client(), repository
    config_store.set_repository(previous)


def start(client, goal='router'):
    response = client.post('/api/setup', json={'id': SESSION, 'name': '新手配置', 'goal': goal})
    assert response.status_code == 200, response.json
    return response.json


def test_suggestion_is_read_only_and_creation_resumes_the_same_profile(setup_api):
    client, repository = setup_api
    before = repository.export_all()
    assert client.get('/api/setup').json['suggested']
    assert repository.export_all() == before
    first = start(client)
    token = repository.get_system()['system_config']['config_token']
    assert start(client)['profile_id'] == first['profile_id']
    assert repository.get_system()['system_config']['config_token'] == token
    assert len(repository.list_profiles()) == 2
    assert client.get('/api/setup').json['sessions'][0]['id'] == SESSION
    assert repository.get_profile('default') == before['profiles']['default']


def test_first_start_template_still_suggests_setup_until_a_device_exists(tmp_path):
    from backend.common.config import _get_initial_config, get_default_config
    repository = ProfileRepository(tmp_path, default_config_factory=get_default_config,
                                   initial_config_factory=_get_initial_config)
    assert repository.get_profile('default')['proxy_groups']
    previous = config_store._repository
    config_store.set_repository(repository)
    try:
        app = Flask(__name__)
        register_blueprints(app)
        client = app.test_client()
        assert client.get('/api/setup').json['suggested']
        repository.update_system_transaction(lambda system: system.update(agents=[
            {'id': 'mihomo', 'name': 'mihomo', 'host': '192.0.2.10', 'service_type': 'mihomo', 'profile_id': 'default'}]))
        assert not client.get('/api/setup').json['suggested']
    finally:
        config_store.set_repository(previous)


def test_subscriptions_without_a_link_are_not_offered_as_sources(tmp_path):
    from backend.common.config import _get_initial_config, get_default_config
    repository = ProfileRepository(tmp_path, default_config_factory=get_default_config,
                                   initial_config_factory=_get_initial_config)
    placeholder = repository.get_shared()['subscriptions'][0]['id']
    repository.update_shared_transaction(lambda shared: shared['subscriptions'].append(
        {'id': 'multi', 'name': 'Multi', 'url': 'https://a.test/sub\n\nhttps://b.test/sub', 'enabled': True}))
    repository.update_shared_transaction(lambda shared: shared['subscriptions'].append(
        {'id': 'noted', 'name': 'Noted', 'url': '订阅链接\nhttps://a.test/sub', 'enabled': True}))
    previous = config_store._repository
    config_store.set_repository(repository)
    try:
        app = Flask(__name__)
        register_blueprints(app)
        client = app.test_client()
        assert [item['id'] for item in client.get('/api/setup').json['subscriptions']] == ['multi']
        start(client)
        response = client.post(f'/api/setup/{SESSION}/source', json={'subscription_ids': [placeholder]})
        assert response.status_code == 400 and '订阅链接' in response.json['message']
        response = client.post(f'/api/setup/{SESSION}/source', json={'subscription_ids': ['multi']})
        assert response.status_code == 200, response.json
    finally:
        config_store.set_repository(previous)


def test_sources_are_idempotent_and_bad_subscription_cannot_complete(setup_api):
    client, repository = setup_api
    start(client)
    body = {'url': 'https://subscription.test/list?token=private'}
    for _ in range(2):
        response = client.post(f'/api/setup/{SESSION}/source', json=body)
        assert response.status_code == 200, response.json
    assert len(repository.get_shared()['subscriptions']) == 1
    response = client.post(f'/api/setup/{SESSION}/prepare', json={})
    assert response.status_code == 400
    assert '可用节点' in response.json['message']
    assert client.post(f'/api/setup/{SESSION}/finish', json={}).status_code == 409


@pytest.mark.parametrize('goal', ['router', 'gateway'])
def test_prepare_is_resumable_scoped_and_has_a_real_proxy_exit(setup_api, goal):
    client, repository = setup_api
    old = repository.get_profile('default')
    session = start(client, goal)
    repository.update_shared_transaction(lambda shared: shared['nodes'].append(deepcopy(NODE)))
    response = client.post(f'/api/setup/{SESSION}/source', json={'node_ids': ['node']})
    assert response.status_code == 200
    for _ in range(2):
        response = client.post(f'/api/setup/{SESSION}/prepare', json={'template': 'basic'})
        assert response.status_code == 200, response.json
    profile = repository.get_profile(session['profile_id'])
    assert profile['_revision'] == 1
    groups = {group['name']: group for group in profile['proxy_groups']}
    # 'Test node' names no region, so only the all-node group carries it.
    assert groups['ALL']['manual_nodes'] == ['node'] and '🇭🇰 香港节点' not in groups
    assert profile['rule_configs'][-1]['policy'] == '🐟 漏网之鱼'
    mihomo_dns = yaml.safe_load(profile['mihomo']['custom_config'])['dns']
    assert mihomo_dns['listen'] == '127.0.0.1:1053'
    # Same IP-address DoH upstreams the gateway installer fills in by default.
    from backend.common.gateway_onboarding import DEFAULT_MIHOMO_NAMESERVERS
    assert mihomo_dns['nameserver'] == ['https://223.5.5.5/dns-query', 'https://1.12.12.12/dns-query']
    assert mihomo_dns['nameserver'] == list(DEFAULT_MIHOMO_NAMESERVERS)
    assert repository.get_profile('default') == old
    response = client.post(f'/api/setup/{SESSION}/finish', json={})
    assert response.status_code == 409
    assert profile['oxidns']['remote_dns'] == '127.0.0.1:1053'
    assert profile['oxidns']['listen'] == '0.0.0.0:53'
    assert profile['oxidns']['local_dns'] == 'https://223.5.5.5/dns-query'
    assert client.get(f'/api/setup/{SESSION}').json['local_dns'] == profile['oxidns']['local_dns']


def _recorder(repository, profile_id):
    return repository.get_profile(profile_id)['oxidns']['query_recorder']


def test_dns_query_recorder_is_on_by_default_and_follows_the_switch(setup_api):
    from backend.common.oxidns_settings import get_default_oxidns_settings
    client, repository = setup_api
    session = start(client)
    profile_id = session['profile_id']
    repository.update_shared_transaction(lambda shared: shared['nodes'].append(deepcopy(NODE)))
    client.post(f'/api/setup/{SESSION}/source', json={'node_ids': ['node']})
    response = client.post(f'/api/setup/{SESSION}/prepare', json={})
    assert response.status_code == 200, response.json
    assert response.json['query_recorder'] is True
    default_args = get_default_oxidns_settings()['query_recorder']['args']
    assert _recorder(repository, profile_id) == {'enabled': True, 'args': default_args}
    # The embedded installer starts from the profile, so it keeps the choice.
    assert client.get(f'/api/profiles/{profile_id}/gateway-onboarding').json['settings']['query_recorder'] is True
    response = client.post(f'/api/setup/{SESSION}/prepare', json={'query_recorder': False})
    assert response.status_code == 200, response.json
    assert _recorder(repository, profile_id) == {'enabled': False, 'args': default_args}
    # A later prepare without the field keeps the saved choice.
    assert client.post(f'/api/setup/{SESSION}/prepare', json={}).json['query_recorder'] is False
    assert _recorder(repository, profile_id)['enabled'] is False
    assert client.get(f'/api/profiles/{profile_id}/gateway-onboarding').json['settings']['query_recorder'] is False


def test_setup_prepared_before_the_recorder_switch_keeps_it_off(setup_api):
    client, repository = setup_api
    session = start(client)
    repository.update_shared_transaction(lambda shared: shared['nodes'].append(deepcopy(NODE)))
    client.post(f'/api/setup/{SESSION}/source', json={'node_ids': ['node']})
    assert client.post(f'/api/setup/{SESSION}/prepare', json={'query_recorder': False}).status_code == 200
    from backend.common import setup_flow
    stored = setup_flow.read(repository, SESSION)
    del stored['query_recorder']
    setup_flow.write(repository, stored)
    response = client.post(f'/api/setup/{SESSION}/prepare', json={})
    assert response.status_code == 200, response.json
    assert _recorder(repository, session['profile_id'])['enabled'] is False


@pytest.mark.parametrize('value', ['yes', 1, None])
def test_invalid_recorder_switch_does_not_change_the_profile(setup_api, value):
    client, repository = setup_api
    start(client)
    repository.update_shared_transaction(lambda shared: shared['nodes'].append(deepcopy(NODE)))
    client.post(f'/api/setup/{SESSION}/source', json={'node_ids': ['node']})
    before = repository.export_all()
    response = client.post(f'/api/setup/{SESSION}/prepare', json={'query_recorder': value})
    assert response.status_code == 400
    assert repository.export_all() == before


def test_subscription_cache_and_manual_profile_changes_are_checked(setup_api):
    client, repository = setup_api
    session = start(client)
    client.post(f'/api/setup/{SESSION}/source', json={'url': 'https://subscription.test/list'})
    save_subscription_nodes('setup_sub_' + SESSION, [NODE])
    assert client.post(f'/api/setup/{SESSION}/prepare', json={}).status_code == 200
    repository.save_profile(session['profile_id'], {'mihomo': {'custom_config': 'mixed-port: 9999'}})
    before = repository.export_all()
    assert client.post(f'/api/setup/{SESSION}/prepare', json={}).status_code == 409
    assert repository.export_all() == before
    assert client.post(f'/api/setup/{SESSION}/finish', json={}).status_code == 409


def test_embedded_installation_and_router_writes_do_not_block_new_choices(setup_api):
    client, repository = setup_api
    session = start(client)
    profile_id = session['profile_id']
    repository.update_shared_transaction(lambda shared: shared['nodes'].append(deepcopy(NODE)))
    client.post(f'/api/setup/{SESSION}/source', json={'node_ids': ['node']})
    assert client.post(f'/api/setup/{SESSION}/prepare', json={}).status_code == 200
    # Step 4 saves the installation settings before generating the command.
    state = client.get(f'/api/profiles/{profile_id}/gateway-onboarding').json
    response = client.post(f'/api/profiles/{profile_id}/gateway-onboarding',
                           json={**state['settings'], 'expected_config_revision': state['config_revision']})
    assert response.status_code == 200, response.json
    response = client.post('/api/routers', json=ROUTER_DRAFT, headers={'X-ConfigFlow-Profile': profile_id})
    assert response.status_code == 200, response.json
    response = client.post(f'/api/setup/{SESSION}/prepare', json={'traffic_fallback': 'direct'})
    assert response.status_code == 200, response.json
    assert repository.get_profile(profile_id)['rule_configs'][-1]['policy'] == 'DIRECT'
    repository.save_profile(profile_id, {'mihomo': {'custom_config': 'mixed-port: 9999'}})
    response = client.post(f'/api/setup/{SESSION}/prepare', json={'traffic_fallback': 'proxy'})
    assert response.status_code == 409
    assert '其他页面' in response.json['message']


def test_invalid_requests_cannot_skip_steps_or_target_other_profiles(setup_api):
    client, repository = setup_api
    start(client)
    before = repository.export_all()
    assert client.patch(f'/api/setup/{SESSION}', json={'step': 5}).status_code == 400
    assert client.patch(f'/api/setup/{SESSION}', json={'profile_id': 'default'}).status_code == 400
    assert client.patch(f'/api/setup/{SESSION}', json={'router_id': 'unrelated'}).status_code == 409
    assert client.post(f'/api/setup/{SESSION}/source', json={'url': 'file:///etc/passwd'}).status_code == 400
    assert client.post('/api/setup', json=[]).status_code == 400
    assert repository.export_all() == before


def test_setup_apis_require_administrator_authentication(setup_api, monkeypatch):
    client, _ = setup_api
    monkeypatch.setattr('backend.common.auth.is_auth_enabled', lambda: True)
    assert client.get('/api/setup').status_code == 401
    assert client.post('/api/setup', json={'id': SESSION, 'name': 'test', 'goal': 'router'}).status_code == 401


# A router split Agent reporting persisted forwarding and a running full TUN.
FULL_TUN_STATUS = {'success': True, 'enabled_in_config': False, 'managed': {},
                   'ipv4_forwarding': {'protocol': 1, 'current': '1', 'persisted': True},
                   'tun': {'configured': True, 'device': 'Meta', 'present': True, 'gateway_marker': False}}


@pytest.fixture
def installed(setup_api, monkeypatch):
    client, repository = setup_api
    session = start(client)
    repository.update_shared_transaction(lambda shared: shared['nodes'].append(deepcopy(NODE)))
    client.post(f'/api/setup/{SESSION}/source', json={'node_ids': ['node']})
    client.post(f'/api/setup/{SESSION}/prepare', json={})
    profile_id = session['profile_id']
    revision = repository.config_revision(profile_id)
    agents = {service: {'id': service, 'name': service, 'host': '192.0.2.10', 'status': 'online',
                       'profile_id': profile_id, 'service_type': service} for service in ('mihomo', 'oxidns')}
    repository.update_system_transaction(lambda system: system.update(agents=list(agents.values())))
    gateway = {'id': 'installation', 'phase': 'published', 'target_changed': False, 'agents': agents}
    def latest(*_):
        return {'session': {**gateway, 'config_changed': repository.config_revision(profile_id) != revision}}
    monkeypatch.setattr('backend.common.agent_manager.get_agent_manager', lambda: object())
    monkeypatch.setattr('backend.agents.gateway_installation.latest', latest)
    status = deepcopy(FULL_TUN_STATUS)
    monkeypatch.setattr('backend.agents.linux_gateway.call', lambda *_: status)
    return client, repository, profile_id, gateway, status


ROUTER_DRAFT = {'name': 'Main', 'type': 'ikuai', 'endpoint': 'http://192.0.2.1',
                'username': 'admin', 'password': 'draft-secret'}


def test_early_router_settings_are_private_and_do_not_invalidate_installation(installed):
    client, repository, profile_id, _, _ = installed
    before = repository.export_all()
    response = client.post(f'/api/setup/{SESSION}/router-draft', json={'router': ROUTER_DRAFT})
    assert response.status_code == 200, response.json
    draft = response.json['router_draft']
    assert draft['has_password'] and 'password' not in draft
    assert response.json['step'] == response.json['max_step'] == 3
    assert repository.export_all() == before
    path = repository.data_dir / 'setup-sessions' / f'{SESSION}.json'
    assert path.stat().st_mode & 0o777 == 0o600
    assert 'draft-secret' in path.read_text()
    for response in (client.get(f'/api/setup/{SESSION}'), client.get('/api/setup'),
                     client.post('/api/setup', json={'id': SESSION, 'name': '新手配置', 'goal': 'router'}),
                     client.patch(f'/api/setup/{SESSION}', json={'step': 3})):
        assert response.status_code == 200
        assert 'draft-secret' not in response.get_data(as_text=True)
    response = client.post(f'/api/setup/{SESSION}/router-draft', json={'router': {**ROUTER_DRAFT, 'name': 'Renamed', 'password': ''}})
    assert response.json['router_draft']['id'] == draft['id']
    assert response.json['router_draft']['has_password']
    assert repository.export_all() == before
    assert client.post(f'/api/setup/{SESSION}/router-draft', json={'router': None}).status_code == 200
    assert 'router_draft' not in client.get(f'/api/setup/{SESSION}').json
    assert 'draft-secret' not in path.read_text()


def test_router_draft_attaches_only_after_healthy_installation_and_is_idempotent(installed):
    client, repository, profile_id, _, status = installed
    response = client.post(f'/api/setup/{SESSION}/router-draft', json={'router': ROUTER_DRAFT})
    router_id = response.json['router_draft']['id']
    status['tun']['present'] = False
    assert client.post(f'/api/setup/{SESSION}/check', json={}).status_code == 409
    assert repository.get_profile(profile_id)['routers']['items'] == []
    assert 'router_draft' in client.get(f'/api/setup/{SESSION}').json
    status['tun']['present'] = True
    for _ in range(2):
        response = client.post(f'/api/setup/{SESSION}/check', json={})
        assert response.status_code == 200, response.json
        assert response.json['step'] == 4
        assert response.json['router_id'] == router_id
        assert 'router_draft' not in response.json
    routers = repository.get_profile(profile_id)['routers']['items']
    assert len(routers) == 1
    assert routers[0]['proxy_gateway_agent_id'] == 'mihomo'
    assert routers[0]['dns_agent_id'] == 'oxidns'
    assert routers[0]['password'] == 'draft-secret'
    assert client.post(f'/api/setup/{SESSION}/router-draft', json={'router': ROUTER_DRAFT}).status_code == 409


def test_router_draft_retries_after_interruption_between_profile_and_session_commits(installed, monkeypatch):
    client, repository, profile_id, _, _ = installed
    client.post(f'/api/setup/{SESSION}/router-draft', json={'router': ROUTER_DRAFT})
    from backend.common import setup_flow
    write = setup_flow.write
    def interrupted_write(repo, session):
        if session['step'] == 4:
            raise OSError('simulated interrupted session commit')
        return write(repo, session)
    monkeypatch.setattr(setup_flow, 'write', interrupted_write)
    assert client.post(f'/api/setup/{SESSION}/check', json={}).status_code == 500
    assert len(repository.get_profile(profile_id)['routers']['items']) == 1
    monkeypatch.setattr(setup_flow, 'write', write)
    response = client.post(f'/api/setup/{SESSION}/check', json={})
    assert response.status_code == 200, response.json
    assert 'router_draft' not in response.json
    assert len(repository.get_profile(profile_id)['routers']['items']) == 1


@pytest.mark.parametrize('change', [{'proxy_gateway_agent_id': 'other'}, {'dns_agent_id': 'other'},
                                   {'auto_rule_sync': True}, {'unknown': True}, {'endpoint': 'file:///etc/passwd'},
                                   {'type': 'routeros', 'ros_policy_rule_sets': ['setup_lan_0']}])
def test_router_draft_rejects_invalid_or_premature_links(installed, change):
    client, repository, _, _, _ = installed
    before = repository.export_all()
    assert client.post(f'/api/setup/{SESSION}/router-draft', json={'router': {**ROUTER_DRAFT, **change}}).status_code == 400
    assert repository.export_all() == before
    assert 'router_draft' not in client.get(f'/api/setup/{SESSION}').json


def test_router_draft_accepts_routeros_without_policy_rule_sets(installed):
    client, _, _, _, _ = installed
    response = client.post(f'/api/setup/{SESSION}/router-draft',
                           json={'router': {**ROUTER_DRAFT, 'type': 'routeros', 'ros_policy_rule_sets': []}})
    assert response.status_code == 200, response.json
    assert response.json['router_draft']['ros_policy_rule_sets'] == []


def test_router_draft_checks_policy_rule_sets_after_merging_the_saved_draft(installed):
    """A draft saved before policy routing was refused keeps its sets unless the payload clears them."""
    client, repository, _, _, _ = installed
    from backend.common import setup_flow
    routeros = {**ROUTER_DRAFT, 'type': 'routeros'}
    assert client.post(f'/api/setup/{SESSION}/router-draft', json={'router': routeros}).status_code == 200
    stored = setup_flow.read(repository, SESSION)
    stored['router_draft']['ros_policy_rule_sets'] = ['setup_lan_0']
    setup_flow.write(repository, stored)
    response = client.post(f'/api/setup/{SESSION}/router-draft', json={'router': {'name': 'Renamed'}})
    assert response.status_code == 400
    assert '客户端设备' in response.json['message']
    assert setup_flow.read(repository, SESSION)['router_draft']['name'] == 'Main'
    response = client.post(f'/api/setup/{SESSION}/router-draft', json={'router': {'name': 'Renamed', 'ros_policy_rule_sets': []}})
    assert response.status_code == 200, response.json
    assert response.json['router_draft']['ros_policy_rule_sets'] == []


def test_installation_check_requires_both_services_and_healthy_forwarding(installed):
    client, _, _, gateway, status = installed
    status['ipv4_forwarding']['current'] = '0'
    assert client.post(f'/api/setup/{SESSION}/check', json={}).status_code == 409
    status['ipv4_forwarding']['current'] = '1'
    gateway['target_changed'] = True
    assert client.post(f'/api/setup/{SESSION}/check', json={}).status_code == 409
    gateway['target_changed'] = False
    response = client.post(f'/api/setup/{SESSION}/check', json={})
    assert response.status_code == 200, response.json
    assert response.json['step'] == 4
    assert response.json['route_instructions']['routes'][0]['cidr'] == '198.18.0.0/16'
    assert response.json['route_instructions']['dns'] == '192.0.2.10'


def test_manual_router_completion_requires_explicit_acknowledgements(installed):
    client, _, _, _, _ = installed
    assert client.post(f'/api/setup/{SESSION}/check', json={}).status_code == 200
    assert client.post(f'/api/setup/{SESSION}/finish', json={}).status_code == 409
    response = client.post(f'/api/setup/{SESSION}/finish', json={'router_mode': 'manual', 'ack_routes': True, 'ack_dns': True})
    assert response.status_code == 200, response.json
    assert response.json['router_verification'] == 'manual'
    assert response.json['completed']


def test_router_receipts_do_not_invalidate_core_checks_but_core_edits_do(installed, monkeypatch):
    client, repository, profile_id, _, _ = installed
    client.post(f'/api/setup/{SESSION}/check', json={})
    from backend.routers.store import normalize
    router = normalize({'name': 'Main', 'type': 'ikuai', 'endpoint': 'http://192.0.2.1',
                        'username': 'admin', 'password': 'private',
                        'proxy_gateway_agent_id': 'mihomo', 'dns_agent_id': 'oxidns'})
    repository.save_profile(profile_id, {'routers': {'items': [router]}})
    closed = []
    monkeypatch.setattr('backend.routes.routers._driver', lambda _: SimpleNamespace(close=lambda: closed.append(True)))
    monkeypatch.setattr('backend.routes.routers._inspect', lambda *_: (None, {}, {}, {'return_routing': {}}))
    plan = {'empty': False, 'blockers': []}
    monkeypatch.setattr('backend.routers.plan.build_plan', lambda *_: plan)
    body = {'router_mode': 'managed', 'router_id': router['id']}
    assert client.post(f'/api/setup/{SESSION}/finish', json=body).status_code == 409
    plan['empty'] = True
    response = client.post(f'/api/setup/{SESSION}/finish', json=body)
    assert response.status_code == 200, response.json
    assert response.json['router_verification'] == 'verified'
    assert len(closed) == 2
    repository.save_profile(profile_id, {'mihomo': {'custom_config': 'mixed-port: 9999'}})
    assert client.post(f'/api/setup/{SESSION}/finish', json=body).status_code == 409


@pytest.mark.parametrize('dns_fallback', ['mihomo', 'direct'])
@pytest.mark.parametrize('traffic_fallback', ['proxy', 'direct'])
def test_dns_and_connection_fallbacks_are_independent_through_installation(setup_api, dns_fallback, traffic_fallback):
    from backend.common.gateway_onboarding import gateway_onboarding_state, prepare_gateway_onboarding, assert_gateway_onboarding_ready
    from backend.converters.oxidns import generate_oxidns_config
    from backend.converters.mihomo import generate_mihomo_config
    client, repository = setup_api
    session = start(client)
    repository.update_shared_transaction(lambda shared: shared['nodes'].append(deepcopy(NODE)))
    client.post(f'/api/setup/{SESSION}/source', json={'node_ids': ['node']})
    local_dns = 'https://223.6.6.6/dns-query\ntls://223.5.5.5:853'
    choices = {'dns_fallback': dns_fallback, 'traffic_fallback': traffic_fallback, 'local_dns': f'  {local_dns}\n',
               'domain_rules': {'direct': ['builtin:direct'], 'proxy': ['builtin:proxy']}}
    for _ in range(2):
        response = client.post(f'/api/setup/{SESSION}/prepare', json=choices)
        assert response.status_code == 200, response.json
        assert response.json['dns_fallback'] == dns_fallback
        assert response.json['traffic_fallback'] == traffic_fallback
        assert response.json['local_dns'] == local_dns
    assert client.get(f'/api/setup/{SESSION}').json['local_dns'] == local_dns
    config, revision = repository.get_publish_snapshot(session['profile_id'])
    assert repository.get_profile(session['profile_id'])['_revision'] == 1
    assert len(repository.get_shared()['rule_library']) == len(builtin_rule_catalog())
    expected_forward = 'forward_remote' if dns_fallback == 'mihomo' else 'forward_local'
    expected_policy = '🐟 漏网之鱼' if traffic_fallback == 'proxy' else 'DIRECT'
    assert config['oxidns']['default_forward'] == expected_forward
    assert config['oxidns']['local_dns'] == local_dns
    assert config['oxidns']['direct_rulesets'] == ['setup_builtin_direct']
    assert config['oxidns']['proxy_rulesets'] == ['setup_builtin_proxy']
    assert config['rule_configs'][-1]['policy'] == expected_policy
    state = gateway_onboarding_state(config, revision, takeover='full_tun')
    assert state['settings']['default_forward'] == expected_forward
    assert state['settings']['local_dns'] == local_dns
    prepared, report = prepare_gateway_onboarding(config, state['settings'], base_url='https://config.test', takeover='full_tun')
    assert report['ready'], report
    assert assert_gateway_onboarding_ready(prepared, base_url='https://config.test')['ready']
    assert prepared['oxidns']['default_forward'] == expected_forward
    assert prepared['oxidns']['local_dns'] == local_dns
    assert prepared['rule_configs'] == config['rule_configs']
    assert not any('MATCH' in warning for warning in report['warnings'])
    document = yaml.safe_load(generate_mihomo_config(prepared))
    assert document['rules'][-1] == 'MATCH,' + expected_policy
    assert 'RULE-SET,Proxy,✈️ 节点选择' in document['rules']
    dns_document = yaml.safe_load(generate_oxidns_config(prepared, 'https://config.test'))
    forward = next(plugin for plugin in dns_document['plugins'] if plugin['tag'] == 'forward_local')
    assert [upstream['addr'] for upstream in forward['args']['upstreams']] == local_dns.splitlines()
    main = next(plugin for plugin in dns_document['plugins'] if plugin['tag'] == 'sequence_main')
    assert main['args'][-1]['exec'] == ('goto proxy_dns_seq' if dns_fallback == 'mihomo' else 'goto china_dns')


def test_removed_proxy_mode_and_invalid_fallbacks_do_not_mutate_configuration(setup_api):
    client, repository = setup_api
    start(client)
    before = repository.export_all()
    for body in ({'template': 'proxy'}, {'dns_fallback': 'proxy'}, {'traffic_fallback': 'mihomo'}, {'dns_fallback': []}):
        response = client.post(f'/api/setup/{SESSION}/prepare', json=body)
        assert response.status_code == 400, response.json
    assert repository.export_all() == before


@pytest.mark.parametrize('local_dns', ['', '  ', None, [], '223.5.5.5',
                                     'https://127.0.0.1/dns-query', 'tls://223.5.5.5:53',
                                     'https://dns.example/dns-query'])
def test_invalid_direct_dns_does_not_change_profile_presets_or_session(setup_api, local_dns):
    client, repository = setup_api
    start(client)
    repository.update_shared_transaction(lambda shared: shared['nodes'].append(deepcopy(NODE)))
    client.post(f'/api/setup/{SESSION}/source', json={'node_ids': ['node']})
    before = repository.export_all()
    session = client.get(f'/api/setup/{SESSION}').json
    response = client.post(f'/api/setup/{SESSION}/prepare', json={'local_dns': local_dns})
    assert response.status_code == 400, response.json
    assert '直连 DNS' in response.json['message']
    assert repository.export_all() == before
    assert client.get(f'/api/setup/{SESSION}').json == session


def test_domain_choices_are_read_only_and_only_offer_enabled_supported_domain_rules(setup_api):
    client, repository = setup_api
    entries = [DOMAIN_RULE,
               {**DOMAIN_RULE, 'id': 'disabled', 'name': '停用', 'enabled': False},
               {**DOMAIN_RULE, 'id': 'ips', 'name': 'IP', 'behavior': 'ipcidr'},
               {**DOMAIN_RULE, 'id': 'mrs', 'name': 'MRS', 'format': 'mrs'},
               {'id': 'setup_dns_cn', 'name': '旧预设', 'url': 'https://example.test/old-cn', 'behavior': 'domain'},
               {'id': 'existing_direct', 'name': '已有直连规则', 'url': builtin_rule_catalog()['builtin:direct']['url'], 'behavior': 'classical'}]
    repository.update_shared_transaction(lambda shared: shared['rule_library'].extend(deepcopy(entries)))
    before = repository.export_all()
    options = client.get('/api/setup').json['domain_rules']
    assert [item['id'] for item in options if item['source'] != 'builtin'] == ['library:work']
    assert any(item['id'] == 'builtin:telegram' and item['behavior'] == 'classical' for item in options)
    assert not any(item['id'] in ('builtin:fakeip_filter', 'builtin:private_ip') for item in options)
    assert repository.export_all() == before


def test_old_two_preset_choices_use_template_sources_without_extra_rule_library(setup_api):
    client, repository = setup_api
    start(client)
    repository.update_shared_transaction(lambda shared: shared['nodes'].append(deepcopy(NODE)))
    client.post(f'/api/setup/{SESSION}/source', json={'node_ids': ['node']})
    response = client.post(f'/api/setup/{SESSION}/prepare', json={'domain_rules': {
        'direct': ['preset:direct'], 'proxy': ['preset:proxy']}})
    assert response.status_code == 200, response.json
    assert response.json['domain_rules'] == {'direct': ['builtin:chinamax'], 'proxy': ['builtin:proxy']}
    assert len(repository.get_shared()['rule_library']) == len(builtin_rule_catalog())


@pytest.mark.parametrize('selections', [
    {'direct': ['library:work'], 'proxy': ['builtin:proxy']},
    {'direct': [], 'proxy': ['library:work', 'builtin:direct']},
    {'direct': [], 'proxy': []},
])
def test_selected_domain_rules_reach_both_core_configs_and_installer(setup_api, selections):
    from backend.common.gateway_onboarding import gateway_onboarding_state, prepare_gateway_onboarding
    from backend.converters.oxidns import generate_oxidns_config
    from backend.converters.mihomo import generate_mihomo_config
    client, repository = setup_api
    session = start(client)
    repository.update_shared_transaction(lambda shared: shared.update(
        nodes=[deepcopy(NODE)], rule_library=[deepcopy(DOMAIN_RULE)]))
    client.post(f'/api/setup/{SESSION}/source', json={'node_ids': ['node']})
    before_rule = deepcopy(repository.get_shared()['rule_library'][0])
    for _ in range(2):
        response = client.post(f'/api/setup/{SESSION}/prepare', json={'domain_rules': selections})
        assert response.status_code == 200, response.json
        assert response.json['domain_rules'] == selections
    assert client.get(f'/api/setup/{SESSION}').json['domain_rules'] == selections
    snapshot, revision = repository.get_publish_snapshot(session['profile_id'])
    profile = repository.get_profile(session['profile_id'])
    assert profile['_revision'] == 1
    assert repository.get_shared()['rule_library'][0] == before_rule
    assert profile['oxidns']['rule_update']['enabled'] == bool(selections['direct'] or selections['proxy'])
    assert len(repository.get_shared()['rule_library']) == 1 + len(builtin_rule_catalog())
    rules = {item['id']: item for item in snapshot['rule_configs'] if item['itemType'] == 'ruleset'}
    for route in ('direct', 'proxy'):
        ids = snapshot['oxidns'][route + '_rulesets']
        assert len(ids) == len(selections[route])
        assert all(rules[identity]['policy'] == ('🎯 全球直连' if route == 'direct' else '✈️ 节点选择') for identity in ids)
    # A router setup is a full TUN; generation refuses the selective gateway there.
    state = gateway_onboarding_state(snapshot, revision, takeover='full_tun')
    candidate, report = prepare_gateway_onboarding(snapshot, state['settings'], base_url='https://config.test',
                                                   takeover='full_tun')
    assert report['ready'], report
    assert candidate['oxidns']['direct_rulesets'] == snapshot['oxidns']['direct_rulesets']
    assert candidate['oxidns']['proxy_rulesets'] == snapshot['oxidns']['proxy_rulesets']
    mihomo = yaml.safe_load(generate_mihomo_config(candidate, base_url='https://config.test'))
    assert len([rule for rule in mihomo['rules'] if rule.startswith('RULE-SET,')]) == len(rules)
    dns = yaml.safe_load(generate_oxidns_config(candidate, 'https://config.test'))
    assert any(plugin['tag'] == 'forward_local' for plugin in dns['plugins'])


@pytest.mark.parametrize('selections', [None, [], {}, {'direct': [], 'proxy': 'builtin:proxy'},
                                      {'direct': ['builtin:direct'], 'proxy': ['builtin:direct']},
                                      {'direct': ['library:missing'], 'proxy': []},
                                      {'direct': ['library:disabled'], 'proxy': []}])
def test_invalid_domain_selections_do_not_create_presets_or_change_drafts(setup_api, selections):
    client, repository = setup_api
    start(client)
    repository.update_shared_transaction(lambda shared: shared.update(
        nodes=[deepcopy(NODE)], rule_library=[{**DOMAIN_RULE, 'id': 'disabled', 'enabled': False}]))
    client.post(f'/api/setup/{SESSION}/source', json={'node_ids': ['node']})
    before = repository.export_all()
    session = client.get(f'/api/setup/{SESSION}').json
    response = client.post(f'/api/setup/{SESSION}/prepare', json={'domain_rules': selections})
    assert response.status_code in (400, 409), response.json
    assert repository.export_all() == before
    assert client.get(f'/api/setup/{SESSION}').json == session


def test_builtin_library_comes_only_from_the_original_template(setup_api):
    import json
    from backend.common.resource import get_backend_resource
    catalog = builtin_rule_catalog()
    with open(get_backend_resource('config_template.json'), encoding='utf-8') as handle:
        template = json.load(handle)
    by_name = {item['name']: item for item in catalog.values()}
    assert len(catalog) == len(template['rule_library']) == 25
    assert by_name.keys() == {item['name'] for item in template['rule_library']}
    for item in template['rule_library']:
        field = 'content' if item.get('source_type') == 'content' else 'url'
        assert by_name[item['name']][field] == item[field]
    assert not {'OpenAI', 'Gemini', 'Claude', 'Copilot', 'SteamCN', 'Custom_Direct', 'Custom_Proxy', 'Custom_IP_Proxy'} & by_name.keys()
    template_library = {item['id']: item for item in template['rule_library']}
    assert list(by_name) == [template_library[rule['library_rule_id']]['name']
                             for rule in template['rule_configs'] if rule.get('itemType') == 'ruleset']
    client, repository = setup_api
    before = repository.export_all()
    index = client.get('/api/setup').json
    assert index['recommended_domain_rules'] == recommended_domain_rules()
    assert [len(index['recommended_domain_rules'][route]) for route in ('direct', 'proxy')] == [7, 18]
    assert repository.export_all() == before


def test_recommended_classical_library_is_ready_and_existing_sources_are_not_duplicated(setup_api):
    from backend.common.gateway_onboarding import gateway_onboarding_state, prepare_gateway_onboarding, assert_gateway_onboarding_ready
    from backend.converters.oxidns import get_oxidns_ruleset_downloads
    client, repository = setup_api
    existing = {'id': 'existing_telegram', 'name': 'Telegram', 'behavior': 'classical',
                'url': builtin_rule_catalog()['builtin:telegram']['url'].replace('/refs/heads/', '/')}
    custom = {**DOMAIN_RULE, 'id': 'personal', 'name': '我的自定义'}
    repository.update_shared_transaction(lambda shared: shared.update(nodes=[deepcopy(NODE)], rule_library=[existing, custom]))
    session = start(client)
    client.post(f'/api/setup/{SESSION}/source', json={'node_ids': ['node']})
    for _ in range(2):
        response = client.post(f'/api/setup/{SESSION}/prepare', json={})
        assert response.status_code == 200, response.json
        assert response.json['domain_rules'] == recommended_domain_rules()
    shared = repository.get_shared()['rule_library']
    assert shared[:2] == [existing, custom]
    assert len(shared) == len(builtin_rule_catalog()) + 1
    profile = repository.get_profile(session['profile_id'])
    assert profile['_revision'] == 1
    assert any(rule.get('library_rule_id') == existing['id'] for rule in profile['rule_configs'])
    assert not any(rule.get('library_rule_id') == custom['id'] for rule in profile['rule_configs'])
    snapshot, revision = repository.get_publish_snapshot(session['profile_id'])
    assert [rule['name'] for rule in snapshot['rule_configs'] if rule['itemType'] == 'ruleset'] == [item['name'] for item in builtin_rule_catalog().values()]
    state = gateway_onboarding_state(snapshot, revision, takeover='full_tun')
    candidate, report = prepare_gateway_onboarding(snapshot, state['settings'], base_url='https://config.test', takeover='full_tun')
    assert report['ready'], report
    assert assert_gateway_onboarding_ready(candidate, base_url='https://config.test')['ready']
    downloads = get_oxidns_ruleset_downloads(candidate, 'https://config.test')
    assert any(item['local_path'].endswith('_domain.txt') for item in downloads)
    assert not any(item['local_path'].endswith('_ip.txt') for item in downloads)
    assert len(downloads) == 25  # One domain file per selected ruleset; no IP files.
    index = client.get('/api/setup').json
    telegram = next(item for item in index['domain_rules'] if item['id'] == 'builtin:telegram')
    assert 'library:existing_telegram' in telegram['aliases']
    assert not any(item['id'] == 'library:existing_telegram' for item in index['domain_rules'])
    # An older saved library identity is still accepted and resolves to the same source.
    result = client.post(f'/api/setup/{SESSION}/prepare', json={'domain_rules': {'direct': [], 'proxy': ['library:existing_telegram']}})
    assert result.status_code == 200, result.json
    assert result.json['domain_rules'] == {'direct': [], 'proxy': ['builtin:telegram']}


def test_disabled_existing_builtin_source_is_not_cloned_or_reenabled(setup_api):
    client, repository = setup_api
    telegram = {key: value for key, value in builtin_rule_catalog()['builtin:telegram'].items() if key not in ('default_route', '_template_order')}
    telegram.update(id='disabled_telegram', enabled=False)
    repository.update_shared_transaction(lambda shared: shared.update(nodes=[deepcopy(NODE)], rule_library=[deepcopy(telegram)]))
    session = start(client)
    client.post(f'/api/setup/{SESSION}/source', json={'node_ids': ['node']})
    index = client.get('/api/setup').json
    assert 'builtin:telegram' not in index['recommended_domain_rules']['proxy']
    assert not any(item['id'] == 'builtin:telegram' for item in index['domain_rules'])
    response = client.post(f'/api/setup/{SESSION}/prepare', json={})
    assert response.status_code == 200, response.json
    shared = repository.get_shared()['rule_library']
    assert len(shared) == len(builtin_rule_catalog())
    assert shared[0] == telegram
    assert not any(rule.get('library_rule_id') == telegram['id'] for rule in repository.get_profile(session['profile_id'])['rule_configs'])


def test_original_first_start_library_is_reused_without_a_second_copy(tmp_path):
    from backend.common.setup_presets import ensure_builtin_library, domain_rule_options
    repository = ProfileRepository(tmp_path / 'first-start', default_config_factory=config_store.get_default_config,
                                   initial_config_factory=config_store._get_initial_config)
    before = repository.export_all()
    resolved = ensure_builtin_library(repository)
    assert len(resolved) == 25
    assert len(repository.get_shared()['rule_library']) == 25
    assert repository.export_all() == before
    assert all(item['source'] == 'builtin' for item in domain_rule_options(repository))


@pytest.mark.parametrize('first, second, same', [
    (('DOMAIN-SUFFIX,x.ai\nDOMAIN-SUFFIX,grok.com', 'classical', 'text'),
     ('# AI\n+.GROK.com\n\n+.x.ai\n+.x.ai', 'domain', 'text'), True),
    (('DOMAIN-SUFFIX,x.ai', 'classical', 'text'), ('payload:\n  - "+.x.ai"', 'domain', 'yaml'), True),
    (('10.0.0.1/8', 'classical', 'text'), ('IP-CIDR,10.0.0.0/8,no-resolve', 'classical', 'text'), True),
    # Mihomo matches parameters exactly: an upper-case NO-RESOLVE does nothing.
    (('IP-CIDR,10.0.0.0/8,no-resolve', 'classical', 'text'), ('IP-CIDR,10.0.0.0/8,NO-RESOLVE', 'classical', 'text'), False),
    # A bare Classical name is an exact DOMAIN, not a suffix.
    (('x.ai', 'classical', 'text'), ('DOMAIN-SUFFIX,x.ai', 'classical', 'text'), False),
    (('.x.ai', 'domain', 'text'), ('DOMAIN-SUFFIX,x.ai', 'classical', 'text'), False),
    (('DOMAIN-SUFFIX,x.ai,extra', 'classical', 'text'), ('DOMAIN-SUFFIX,x.ai', 'classical', 'text'), False),
])
def test_hand_written_rules_compare_by_what_they_match(first, second, same):
    from backend.common.setup_presets import _parsed_rules
    assert (_parsed_rules(*first) == _parsed_rules(*second)) is same


@pytest.mark.parametrize('content, behavior, rule_format', [
    ('', 'classical', 'text'), ('# only a comment', 'classical', 'text'), ('not a rule!', 'classical', 'text'),
    ('payload: 1', 'classical', 'yaml'),
    # Mihomo reads YAML payload line by line, so these lose rules there that a YAML parser keeps.
    ('payload:\n  - DOMAIN-SUFFIX,x.ai\n  - "DOMAIN-SUFFIX,\n    grok.com"', 'classical', 'yaml'),
    ('x: &a DOMAIN-SUFFIX,x.ai\npayload:\n  - *a\n  - DOMAIN-SUFFIX,grok.com', 'classical', 'yaml'),
    ('payload:\n  - >-\n    DOMAIN-SUFFIX,x.ai\n  - DOMAIN-SUFFIX,grok.com', 'classical', 'yaml'),
    ("payload:\n  - ' +.x.ai'\n  - '+.grok.com'", 'domain', 'yaml'),
])
def test_empty_or_ambiguous_rules_never_compare_equal(content, behavior, rule_format):
    from backend.common.setup_presets import _parsed_rules
    assert _parsed_rules(content, behavior, rule_format) is None


def test_equivalent_hand_written_set_is_reused_instead_of_copied(setup_api):
    client, repository = setup_api
    mine = {'id': 'my_grok', 'name': 'Grok', 'behavior': 'domain', 'format': 'text', 'source_type': 'content',
            'content': '+.grok.com\n+.x.ai', 'enabled': True}
    repository.update_shared_transaction(lambda shared: shared.update(nodes=[deepcopy(NODE)], rule_library=[deepcopy(mine)]))
    session = start(client)
    client.post(f'/api/setup/{SESSION}/source', json={'node_ids': ['node']})
    grok = next(item for item in client.get('/api/setup').json['domain_rules'] if item['id'] == 'builtin:grok')
    assert grok['aliases'] == ['library:my_grok'] and 'conflict' not in grok
    response = client.post(f'/api/setup/{SESSION}/prepare', json={'domain_rules': {'direct': [], 'proxy': ['library:my_grok']}})
    assert response.status_code == 200, response.json
    assert response.json['domain_rules'] == {'direct': [], 'proxy': ['builtin:grok']}
    shared = repository.get_shared()['rule_library']
    assert shared[0] == mine and [item['name'] for item in shared].count('Grok') == 1
    assert not any('（内置' in item['name'] for item in shared)
    assert any(rule.get('library_rule_id') == 'my_grok' for rule in repository.get_profile(session['profile_id'])['rule_configs'])


def test_namesake_with_different_rules_is_offered_and_copied_only_when_chosen(setup_api):
    client, repository = setup_api
    mine = {'id': 'my_grok', 'name': 'Grok', 'behavior': 'classical', 'format': 'text', 'source_type': 'content',
            'content': 'x.ai\ngrok.com', 'enabled': True}
    repository.update_shared_transaction(lambda shared: shared.update(nodes=[deepcopy(NODE)], rule_library=[deepcopy(mine)]))
    session = start(client)
    client.post(f'/api/setup/{SESSION}/source', json={'node_ids': ['node']})
    options = client.get('/api/setup').json['domain_rules']
    grok = next(item for item in options if item['id'] == 'builtin:grok')
    assert grok['conflict'] == {'id': 'library:my_grok', 'name': 'Grok'}
    assert any(item['id'] == 'library:my_grok' for item in options)
    # Choosing the user's set leaves no copy behind, even for template sets not chosen at all.
    response = client.post(f'/api/setup/{SESSION}/prepare', json={'domain_rules': {'direct': [], 'proxy': ['library:my_grok']}})
    assert response.status_code == 200, response.json
    shared = repository.get_shared()['rule_library']
    assert [item['name'] for item in shared].count('Grok') == 1
    assert not any(item['name'].startswith('Grok（') for item in shared)
    assert len(shared) == len(builtin_rule_catalog())
    # Choosing the template's set adds it under a name that says where it came from.
    response = client.post(f'/api/setup/{SESSION}/prepare', json={'domain_rules': {'direct': [], 'proxy': ['builtin:grok']}})
    assert response.status_code == 200, response.json
    copy = next(item for item in repository.get_shared()['rule_library'] if item['name'].startswith('Grok（'))
    assert copy['name'] == 'Grok（内置）' and copy['id'] == 'builtin_grok'
    assert any(rule.get('library_rule_id') == 'builtin_grok' for rule in repository.get_profile(session['profile_id'])['rule_configs'])
    # Once added it is the template's set: no conflict and no further copy.
    grok = next(item for item in client.get('/api/setup').json['domain_rules'] if item['id'] == 'builtin:grok')
    assert 'conflict' not in grok and grok['aliases'] == ['library:builtin_grok']


@pytest.mark.parametrize('namesake', [{'enabled': False}, {'behavior': 'ipcidr', 'content': '192.0.2.0/24'}])
def test_namesake_the_wizard_cannot_offer_still_warns_before_copying(setup_api, namesake):
    client, repository = setup_api
    mine = {'id': 'my_grok', 'name': 'Grok', 'behavior': 'classical', 'format': 'text', 'source_type': 'content',
            'content': 'DOMAIN,foo.test', 'enabled': True, **namesake}
    repository.update_shared_transaction(lambda shared: shared.update(nodes=[deepcopy(NODE)], rule_library=[deepcopy(mine)]))
    start(client)
    options = client.get('/api/setup').json['domain_rules']
    assert next(item for item in options if item['id'] == 'builtin:grok')['conflict'] == {'name': 'Grok'}
    assert not any(item['id'] == 'library:my_grok' for item in options)


def test_new_choices_after_installation_check_clear_stale_later_steps(installed):
    client, repository, profile_id, _, _ = installed
    response = client.post(f'/api/setup/{SESSION}/check', json={})
    assert response.status_code == 200, response.json
    assert response.json['max_step'] == 4 and response.json['route_instructions']
    retry = client.post(f'/api/setup/{SESSION}/prepare', json={})
    assert retry.status_code == 200 and retry.json['max_step'] == 4
    assert 'route_instructions' in retry.json
    changed = client.post(f'/api/setup/{SESSION}/prepare', json={'traffic_fallback': 'direct'})
    assert changed.status_code == 200, changed.json
    assert changed.json['max_step'] == 3 and changed.json['step'] == 3
    assert not {'installation', 'route_instructions', 'router_verification'} & set(changed.json)
    assert client.patch(f'/api/setup/{SESSION}', json={'step': 4}).status_code == 400


IP_RULE = {'id': 'chat', 'name': '聊天', 'behavior': 'classical', 'format': 'text', 'source_type': 'content',
           'content': 'DOMAIN-SUFFIX,chat.test\nIP-CIDR,198.51.100.0/24,no-resolve', 'enabled': True}
DIRECT_IP_RULE = {**IP_RULE, 'id': 'lan-chat', 'name': '内网聊天', 'content': 'IP-CIDR,203.0.113.0/24'}


def test_gateway_takes_over_only_ip_app_rule_sets(setup_api):
    from backend.common.gateway_onboarding import gateway_onboarding_state, prepare_gateway_onboarding
    from backend.converters.mihomo import generate_mihomo_config
    client, repository = setup_api
    session = start(client, 'gateway')
    repository.update_shared_transaction(lambda shared: shared.update(
        nodes=[deepcopy(NODE)], rule_library=[deepcopy(DOMAIN_RULE), deepcopy(IP_RULE), deepcopy(DIRECT_IP_RULE)]))
    client.post(f'/api/setup/{SESSION}/source', json={'node_ids': ['node']})
    # Broad proxy lists (Apple, a library set with IPs) and direct sets stay
    # out: their ranges overlap direct sites that would then be proxied.
    selections = {'direct': ['library:lan-chat', 'builtin:direct'],
                  'proxy': ['builtin:telegram', 'builtin:apple', 'builtin:googlefcm', 'library:chat', 'library:work']}
    response = client.post(f'/api/setup/{SESSION}/prepare', json={'domain_rules': selections})
    assert response.status_code == 200, response.json
    snapshot, revision = repository.get_publish_snapshot(session['profile_id'])
    names = {item['id']: item['name'] for item in snapshot['rule_configs'] if item['itemType'] == 'ruleset'}
    selected = snapshot['mihomo']['linux_gateway']['rule_set_ids']
    assert sorted(names[identity] for identity in selected) == ['GoogleFCM', 'Telegram']
    assert snapshot['mihomo']['linux_gateway']['enabled'] is False
    # The embedded installer carries the selection into the enabled gateway.
    state = gateway_onboarding_state(snapshot, revision)
    assert state['settings']['rule_set_ids'] == selected
    candidate, report = prepare_gateway_onboarding(snapshot, state['settings'], base_url='https://config.test')
    assert report['ready'], report
    mihomo = yaml.safe_load(generate_mihomo_config(candidate, base_url='https://config.test'))
    assert len(mihomo['tun']['route-address-set']) == 3
    # Without either app rule set among the proxy rules nothing is taken over.
    selections = {'direct': [], 'proxy': ['builtin:apple', 'library:chat']}
    response = client.post(f'/api/setup/{SESSION}/prepare', json={'domain_rules': selections})
    assert response.status_code == 200, response.json
    assert repository.get_profile(session['profile_id'])['mihomo']['linux_gateway']['rule_set_ids'] == []


def test_router_setup_leaves_ip_takeover_to_the_router_settings(setup_api):
    client, repository = setup_api
    session = start(client)
    repository.update_shared_transaction(lambda shared: shared.update(nodes=[deepcopy(NODE)], rule_library=[deepcopy(IP_RULE)]))
    client.post(f'/api/setup/{SESSION}/source', json={'node_ids': ['node']})
    selections = {'direct': [], 'proxy': ['builtin:telegram', 'library:chat']}
    response = client.post(f'/api/setup/{SESSION}/prepare', json={'domain_rules': selections})
    assert response.status_code == 200, response.json
    mihomo = repository.get_profile(session['profile_id'])['mihomo']
    assert mihomo['linux_gateway'] == {'enabled': False, 'rule_set_ids': []}
    assert mihomo['router_split']['route_rule_set_ids'] == []


def test_template_groups_follow_wizard_choices_and_drop_empty_regions():
    from backend.common.setup_presets import template_strategy
    rules = [('builtin:apple', 'proxy'), ('builtin:telegram', 'direct'), ('builtin:epic', 'proxy'),
             ('builtin:steam', 'direct'), ('builtin:bilibili', 'direct'), ('builtin:chinamax', 'proxy'),
             ('builtin:lan', 'direct'), ('library:work', 'proxy'), ('library:other', 'direct')]
    nodes = [{'name': '香港 01'}, {'name': '日本 01'}]
    groups, policies, final = template_strategy(rules, 'proxy', ['sub'], ['manual'], nodes,
                                                [{'id': 'manual', 'name': '新加坡 01'}])
    names = {group['name']: group for group in groups}
    ids = {group['id']: group['name'] for group in groups}
    assert policies == [
        '🍎 苹果服务',       # template direct-first; chosen proxy, so it now follows 优先代理
        '🎯 全球直连',       # template proxy-first; chosen direct
        '🎮 游戏平台',       # shared with Steam: the proxy rule keeps the group
        '🎯 全球直连',       # ...and the direct one leaves it
        '📺 哔哩哔哩',       # template direct-first and chosen direct
        '✈️ 节点选择',       # template routes ChinaMax direct; chosen proxy
        'DIRECT',           # template policy is DIRECT itself
        '✈️ 节点选择', '🎯 全球直连',  # sets without a template group
    ]
    assert final == '🐟 漏网之鱼'
    assert ids[names['🍎 苹果服务']['follow_group']] == '优先代理'
    assert ids[names['🐟 漏网之鱼']['follow_group']] == '优先代理'
    assert ids[names['📺 哔哩哔哩']['follow_group']] == '优先直连'
    assert ids[names['🎮 游戏平台']['follow_group']] == '香港优先策略'
    # Regions without matching nodes are gone, including from every group list.
    assert {'🇭🇰 香港节点', '🇯🇵 日本节点', '🇸🇬 新加坡节点'} <= set(names)
    assert not {'🇹🇼 台湾节点', '🇺🇸 美国节点', '🇰🇷 韩国节点', '🇺🇸 美国负载均衡'} & set(names)
    assert all(item in ids for group in groups for item in group.get('include_groups') or [])
    assert names['🇭🇰 香港节点']['subscriptions'] == ['sub'] and names['🇭🇰 香港节点']['manual_nodes'] == []
    assert names['🇸🇬 新加坡节点']['subscriptions'] == [] and names['🇸🇬 新加坡节点']['manual_nodes'] == ['manual']
    assert names['ALL']['subscriptions'] == ['sub'] and names['ALL']['manual_nodes'] == ['manual']
    _, _, final = template_strategy(rules, 'direct', ['sub'], [], nodes, [])
    assert final == 'DIRECT'


def test_setup_profile_uses_template_groups_with_regions_from_the_nodes(setup_api):
    from backend.converters.mihomo import generate_mihomo_config
    client, repository = setup_api
    session = start(client, 'gateway')
    hong_kong = {**deepcopy(NODE), 'id': 'hk', 'name': '香港 01'}
    japan = {**deepcopy(NODE), 'id': 'jp', 'name': '日本 01', 'server': '192.0.2.2'}
    repository.update_shared_transaction(lambda shared: shared['nodes'].extend([hong_kong, japan]))
    client.post(f'/api/setup/{SESSION}/source', json={'node_ids': ['hk', 'jp']})
    response = client.post(f'/api/setup/{SESSION}/prepare', json={})
    assert response.status_code == 200, response.json
    snapshot, _ = repository.get_publish_snapshot(session['profile_id'])
    document = yaml.safe_load(generate_mihomo_config(snapshot, base_url='https://config.test', preflight_providers=False))
    emitted = {group['name']: group for group in document['proxy-groups']}
    assert emitted['🇭🇰 香港节点']['proxies'] == ['香港 01']
    assert emitted['🇯🇵 日本节点']['proxies'] == ['日本 01']
    assert '🇺🇸 美国节点' not in emitted and '🇺🇸 美国节点' not in emitted['✈️ 节点选择']['proxies']
    assert emitted['📲 Telegram']['proxies'][0] == '✈️ 节点选择'
    assert emitted['🍎 苹果服务']['proxies'][0] == '✈️ 节点选择'
    assert document['rules'][-1] == 'MATCH,🐟 漏网之鱼'
    assert emitted['🐟 漏网之鱼']['proxies'][0] == '✈️ 节点选择'


def test_finishing_can_make_the_setup_profile_the_default_and_names_leftovers(installed):
    client, repository, profile_id, _, _ = installed
    response = client.get(f'/api/setup/{SESSION}')
    assert response.json['default_profile'] == {'is_default': False, 'suggested': True,
                                                'current': {'id': 'default', 'name': '默认配置'}}
    assert client.post(f'/api/setup/{SESSION}/check', json={}).status_code == 200
    choices = {'router_mode': 'manual', 'ack_routes': True, 'ack_dns': True}
    assert client.post(f'/api/setup/{SESSION}/finish', json={**choices, 'make_default': 'yes'}).status_code == 400
    response = client.post(f'/api/setup/{SESSION}/finish', json={**choices, 'make_default': True})
    assert response.status_code == 200, response.json
    assert repository.default_profile_id() == profile_id
    summary = response.json['default_profile']
    assert summary['is_default'] and not summary['suggested']
    assert summary['leftovers'] == {'profile': {'id': 'default', 'name': '默认配置'}, 'subscriptions': []}
    # The template's sample subscription counts as a leftover only while
    # nothing but the old default uses it.
    sample = 'sub_1760688109177'
    repository.update_shared_transaction(lambda shared: shared['subscriptions'].append(
        {'id': sample, 'name': 'subscribe', 'url': 'https://example.invalid/sub', 'enabled': True}))
    repository.save_profile('default', {'proxy_groups': [{'id': 'g', 'name': 'G', 'type': 'select', 'subscriptions': [sample]}]})
    leftovers = client.get(f'/api/setup/{SESSION}').json['default_profile']['leftovers']
    assert leftovers['subscriptions'] == [{'id': sample, 'name': 'subscribe'}]
    repository.update_profile_transaction(profile_id, lambda profile: profile['proxy_groups'].append(
        {'id': 'uses-sample', 'name': 'Uses sample', 'type': 'select', 'subscriptions': [sample]}))
    assert client.get(f'/api/setup/{SESSION}').json['default_profile']['leftovers']['subscriptions'] == []


def test_default_is_not_suggested_while_devices_use_it(installed):
    client, repository, _, _, _ = installed
    repository.update_system_transaction(lambda system: system['agents'].append({'id': 'old', 'name': 'Old', 'profile_id': 'default'}))
    assert client.get(f'/api/setup/{SESSION}').json['default_profile']['suggested'] is False
    assert client.post(f'/api/setup/{SESSION}/check', json={}).status_code == 200
    response = client.post(f'/api/setup/{SESSION}/finish', json={'router_mode': 'manual', 'ack_routes': True, 'ack_dns': True})
    assert response.status_code == 200, response.json
    assert repository.default_profile_id() == 'default' and 'leftovers' not in response.json['default_profile']


def _abandon(client, dry_run=False):
    return client.post(f'/api/setup/{SESSION}/abandon', json={'dry_run': dry_run} if dry_run else {})


def test_abandon_deletes_an_unused_setup_profile_and_its_subscription(setup_api):
    client, repository = setup_api
    profile_id = start(client)['profile_id']
    client.post(f'/api/setup/{SESSION}/source', json={'url': 'https://subscription.test/list'})
    save_subscription_nodes('setup_sub_' + SESSION, [NODE])
    before = {p['id'] for p in repository.list_profiles()}
    plan = _abandon(client, dry_run=True).json
    assert plan['delete_profile'] and plan['kept_reason'] is None
    assert plan['subscription']['id'] == 'setup_sub_' + SESSION
    assert {p['id'] for p in repository.list_profiles()} == before  # A dry run changes nothing.
    result = _abandon(client).json
    assert result['delete_profile']
    assert profile_id not in {p['id'] for p in repository.list_profiles()}
    assert 'default' in {p['id'] for p in repository.list_profiles()}
    assert not any(s['id'] == 'setup_sub_' + SESSION for s in repository.get_shared()['subscriptions'])
    from backend.utils.subscription_cache import load_subscription_cache
    assert load_subscription_cache('setup_sub_' + SESSION) is None
    index = client.get('/api/setup').json
    assert index['sessions'] == [] and index['suggested'] is False
    assert client.get(f'/api/setup/{SESSION}').status_code == 404


def test_abandon_keeps_a_subscription_another_profile_uses(setup_api):
    client, repository = setup_api
    start(client)
    client.post(f'/api/setup/{SESSION}/source', json={'url': 'https://subscription.test/list'})
    subscription_id = 'setup_sub_' + SESSION
    repository.update_profile_transaction('default', lambda profile: {**profile, 'proxy_groups': [
        {'id': 'g', 'name': 'G', 'type': 'select', 'subscriptions': [subscription_id]}]})
    result = _abandon(client).json
    assert result['delete_profile'] and result.get('subscription_kept')
    assert any(s['id'] == subscription_id for s in repository.get_shared()['subscriptions'])


def test_abandon_keeps_a_profile_with_bound_devices_and_hides_the_banner(setup_api):
    client, repository = setup_api
    profile_id = start(client)['profile_id']
    repository.update_system_transaction(lambda system: system.update(agents=[
        {'id': 'mihomo', 'name': '网关 Mihomo', 'host': '192.0.2.10', 'service_type': 'mihomo', 'profile_id': profile_id}]))
    plan = _abandon(client, dry_run=True).json
    assert not plan['delete_profile'] and '网关 Mihomo' in plan['kept_reason'] and plan['guidance']
    result = _abandon(client).json
    assert not result['delete_profile']
    assert profile_id in {p['id'] for p in repository.list_profiles()}
    session = client.get('/api/setup').json['sessions'][0]
    assert session['abandoned'] and not session['completed']
    # Continuing the wizard reopens it.
    assert client.patch(f'/api/setup/{SESSION}', json={'step': 1}).status_code == 200
    assert not client.get(f'/api/setup/{SESSION}').json.get('abandoned')


def test_abandon_keeps_profiles_with_routers_installations_or_default_role(setup_api, monkeypatch):
    client, repository = setup_api
    profile_id = start(client)['profile_id']
    repository.update_profile_transaction(profile_id, lambda profile: {**profile, 'routers': {'items': [
        {**ROUTER_DRAFT, 'id': 'r1'}]}})
    assert '主路由' in _abandon(client, dry_run=True).json['kept_reason']
    repository.update_profile_transaction(profile_id, lambda profile: {**profile, 'routers': {'items': []}})
    from backend.agents import gateway_installation
    monkeypatch.setattr(gateway_installation, 'has_installation', lambda repo, pid: pid == profile_id)
    assert '安装' in _abandon(client, dry_run=True).json['kept_reason']
    monkeypatch.setattr(gateway_installation, 'has_installation', lambda repo, pid: False)
    repository.set_default_profile(profile_id)
    assert '默认' in _abandon(client, dry_run=True).json['kept_reason']


def test_abandon_rejects_completed_setups_and_bad_bodies(setup_api):
    client, repository = setup_api
    start(client)
    assert client.post(f'/api/setup/{SESSION}/abandon', json={'dry_run': 'yes'}).status_code == 400
    assert client.post(f'/api/setup/{SESSION}/abandon', json={'delete': True}).status_code == 400
    path = repository.data_dir / 'setup-sessions' / f'{SESSION}.json'
    import json
    path.write_text(json.dumps({**json.loads(path.read_text()), 'completed': True}))
    assert _abandon(client).status_code == 409


def test_listing_skips_a_session_abandoned_during_the_listing(setup_api, monkeypatch):
    client, repository = setup_api
    start(client)
    from backend.common import setup_flow
    original = setup_flow.read
    def vanish(repo, session_id):
        (repo.data_dir / 'setup-sessions' / f'{session_id}.json').unlink(missing_ok=True)
        return original(repo, session_id)
    monkeypatch.setattr(setup_flow, 'read', vanish)
    response = client.get('/api/setup')
    assert response.status_code == 200 and response.json['sessions'] == []


def test_an_automatic_check_does_not_reopen_an_abandoned_setup(setup_api, monkeypatch):
    client, repository = setup_api
    profile_id = start(client)['profile_id']
    repository.update_system_transaction(lambda system: system.update(agents=[
        {'id': 'mihomo', 'name': 'Mihomo', 'host': '192.0.2.10', 'service_type': 'mihomo', 'profile_id': profile_id}]))
    assert not _abandon(client).json['delete_profile']
    client.post(f'/api/setup/{SESSION}/check', json={})
    assert client.get(f'/api/setup/{SESSION}').json['abandoned']


def test_router_setup_writes_full_tun_and_marker_instead_of_the_gateway(setup_api):
    from backend.common.router_split import FULL_TUN
    from backend.converters.mihomo import generate_mihomo_config
    client, repository = setup_api
    session = start(client)
    repository.update_shared_transaction(lambda shared: shared['nodes'].append(deepcopy(NODE)))
    client.post(f'/api/setup/{SESSION}/source', json={'node_ids': ['node']})
    for _ in range(2):
        response = client.post(f'/api/setup/{SESSION}/prepare', json={})
        assert response.status_code == 200, response.json
        assert response.json['legacy_takeover'] is False
    profile = repository.get_profile(session['profile_id'])
    assert profile['_revision'] == 1
    base = yaml.safe_load(profile['mihomo']['custom_config'])
    assert base['tun'] == FULL_TUN
    # The new profile's base is empty, so the built-in template is kept around the full TUN.
    assert list(base) == ['mixed-port', 'allow-lan', 'bind-address', 'mode', 'log-level', 'external-controller', 'dns', 'tun']
    assert profile['mihomo']['router_split'] == {'version': 1, 'takeover': 'full_tun', 'origin': 'setup',
                                                 'route_rule_set_ids': []}
    assert profile['mihomo']['linux_gateway'] == {'enabled': False, 'rule_set_ids': []}
    snapshot, _ = repository.get_publish_snapshot(session['profile_id'])
    generated = yaml.safe_load(generate_mihomo_config(snapshot, preflight_providers=False))
    assert 'x-configflow-linux-gateway' not in generated
    assert generated['tun']['auto-redirect'] is True and 'route-address-set' not in generated['tun']


def test_router_reprepare_keeps_forwarded_rule_sets_that_still_exist(setup_api):
    client, repository = setup_api
    session = start(client)
    repository.update_shared_transaction(lambda shared: shared.update(nodes=[deepcopy(NODE)], rule_library=[deepcopy(IP_RULE)]))
    client.post(f'/api/setup/{SESSION}/source', json={'node_ids': ['node']})
    selections = {'direct': [], 'proxy': ['library:chat']}
    assert client.post(f'/api/setup/{SESSION}/prepare', json={'domain_rules': selections}).status_code == 200
    profile_id = session['profile_id']
    before = repository.get_profile(profile_id)
    chat = next(rule['id'] for rule in before['rule_configs'] if rule.get('library_rule_id') == 'chat')
    mihomo = deepcopy(before['mihomo'])
    mihomo['router_split']['route_rule_set_ids'] = [chat, 'deleted']
    repository.save_profile(profile_id, {'mihomo': mihomo})
    from backend.common.setup_flow import adopt_owned_write
    adopt_owned_write(repository, profile_id, before, repository.get_profile(profile_id))
    assert client.post(f'/api/setup/{SESSION}/prepare', json={'domain_rules': selections}).status_code == 200
    assert repository.get_profile(profile_id)['mihomo']['router_split']['route_rule_set_ids'] == [chat]


def test_gateway_setup_fields_match_the_previous_release(setup_api):
    import json
    from pathlib import Path
    client, repository = setup_api
    session = start(client, 'gateway')
    repository.update_shared_transaction(lambda shared: shared.update(nodes=[deepcopy(NODE)], rule_library=[deepcopy(IP_RULE)]))
    assert client.post(f'/api/setup/{SESSION}/source', json={'node_ids': ['node']}).status_code == 200
    selections = {'direct': ['builtin:direct'], 'proxy': ['builtin:telegram', 'library:chat', 'builtin:proxy']}
    # The DNS query recorder switch postdates 0ef9e89, which left it off.
    response = client.post(f'/api/setup/{SESSION}/prepare', json={'domain_rules': selections, 'query_recorder': False})
    assert response.status_code == 200, response.json
    assert 'legacy_takeover' not in response.json
    profile = repository.get_profile(session['profile_id'])
    # Captured from 0ef9e89 with the same inputs.
    expected = json.loads((Path(__file__).parent / 'testdata' / 'setup_gateway_fields_0ef9e89.json').read_text(encoding='utf-8'))
    from backend.test_router_split_regression import with_current_setup_base
    expected = with_current_setup_base(expected)
    # Per-rule OxiDNS DNS settings postdate 0ef9e89; they must be stored empty.
    rule_dns = {'rule_actions': {}, 'rule_dns_follow': [], 'rule_dns_parked': {}}
    assert {key: profile['oxidns'].get(key) for key in rule_dns} == rule_dns
    profile['oxidns'] = {key: value for key, value in profile['oxidns'].items() if key not in rule_dns}
    for key, value in expected.items():
        assert profile[key] == value, key


def _make_legacy(repository, profile_id):
    """Turn a prepared router setup into one saved by the earlier gateway installer."""
    from backend.common.setup_flow import adopt_owned_write
    before = repository.get_profile(profile_id)
    mihomo = {key: value for key, value in before['mihomo'].items() if key != 'router_split'}
    mihomo.update(custom_config='mixed-port: 7890\ndns:\n  enable: true\n  listen: 127.0.0.1:1053\n',
                  linux_gateway={'enabled': True, 'rule_set_ids': []})
    repository.save_profile(profile_id, {'mihomo': mihomo})
    adopt_owned_write(repository, profile_id, before, repository.get_profile(profile_id))


def test_router_session_prepared_before_the_takeover_field_reports_legacy(setup_api):
    """Sessions prepared by an earlier server have no legacy_takeover; derive it like prepare."""
    from backend.common import setup_flow
    client, repository = setup_api
    session = start(client)
    repository.update_shared_transaction(lambda shared: shared['nodes'].append(deepcopy(NODE)))
    client.post(f'/api/setup/{SESSION}/source', json={'node_ids': ['node']})
    client.post(f'/api/setup/{SESSION}/prepare', json={})
    stored = setup_flow.read(repository, SESSION)
    stored.pop('legacy_takeover')
    setup_flow.write(repository, stored)
    assert client.get(f'/api/setup/{SESSION}').json['legacy_takeover'] is False
    _make_legacy(repository, session['profile_id'])
    assert client.get(f'/api/setup/{SESSION}').json['legacy_takeover'] is True
    assert 'legacy_takeover' not in setup_flow.read(repository, SESSION)


def test_legacy_router_setup_keeps_the_gateway_when_prepared_again(setup_api):
    client, repository = setup_api
    session = start(client)
    repository.update_shared_transaction(lambda shared: shared['nodes'].append(deepcopy(NODE)))
    client.post(f'/api/setup/{SESSION}/source', json={'node_ids': ['node']})
    client.post(f'/api/setup/{SESSION}/prepare', json={})
    _make_legacy(repository, session['profile_id'])
    response = client.post(f'/api/setup/{SESSION}/prepare', json={'traffic_fallback': 'direct'})
    assert response.status_code == 200, response.json
    assert response.json['legacy_takeover'] is True
    mihomo = repository.get_profile(session['profile_id'])['mihomo']
    assert 'router_split' not in mihomo
    assert 'tun' not in yaml.safe_load(mihomo['custom_config'])
    assert mihomo['linux_gateway'] == {'enabled': True, 'rule_set_ids': []}


def test_router_draft_attaches_with_the_forwarded_rule_sets(installed, monkeypatch):
    client, repository, profile_id, gateway, _ = installed
    mihomo = repository.get_profile(profile_id)['mihomo']
    mihomo['router_split']['route_rule_set_ids'] = ['setup_builtin_telegram']
    repository.save_profile(profile_id, {'mihomo': mihomo})
    revision = repository.config_revision(profile_id)
    monkeypatch.setattr('backend.agents.gateway_installation.latest', lambda *_: {'session': {
        **gateway, 'config_changed': repository.config_revision(profile_id) != revision}})
    client.post(f'/api/setup/{SESSION}/router-draft', json={'router': ROUTER_DRAFT})
    response = client.post(f'/api/setup/{SESSION}/check', json={})
    assert response.status_code == 200, response.json
    router = repository.get_profile(profile_id)['routers']['items'][0]
    assert router['ip_rule_sets'] == ['setup_builtin_telegram']


@pytest.mark.parametrize('change, message', [
    (lambda status, agents: status.pop('ipv4_forwarding'), '1.8.40-go'),
    (lambda status, agents: status.pop('tun'), '1.8.40-go'),
    (lambda status, agents: status['ipv4_forwarding'].update(current='0'), 'IPv4 转发未开启'),
    (lambda status, agents: status['ipv4_forwarding'].update(current='', error='读取失败'), 'IPv4 转发未开启'),
    (lambda status, agents: status['ipv4_forwarding'].update(persisted=False), '尚未持久化'),
    (lambda status, agents: status['managed'].update(active=True), '仍有 Linux 网关的转发记录'),
    (lambda status, agents: status['managed'].update(pending=True), '仍有 Linux 网关的转发记录'),
    (lambda status, agents: status['managed'].update(error='nft 表残留'), '仍有 Linux 网关的转发记录'),
    (lambda status, agents: status.update(enabled_in_config=True), '仍是 Linux 网关配置'),
    (lambda status, agents: status['tun'].update(gateway_marker=True), '仍是 Linux 网关配置'),
    (lambda status, agents: status['tun'].update(configured=False), '未启用完整 TUN'),
    (lambda status, agents: status['tun'].update(present=False), 'Mihomo TUN（Meta）未运行'),
    (lambda status, agents: status['tun'].update(present=False, device='utun'), 'Mihomo TUN（utun）未运行'),
    (lambda status, agents: agents['oxidns'].update(status='offline'), '安装设备离线'),
    (lambda status, agents: agents['mihomo'].update(enabled=False), '安装设备离线或已禁用'),
])
def test_full_tun_installation_check_covers_forwarding_and_tun(installed, change, message):
    client, _, _, gateway, status = installed
    change(status, gateway['agents'])
    response = client.post(f'/api/setup/{SESSION}/check', json={})
    assert response.status_code == 409
    assert message in response.json['message']


def test_full_tun_route_instructions_only_use_the_marker_list(installed, monkeypatch):
    client, repository, profile_id, gateway, _ = installed
    mihomo = repository.get_profile(profile_id)['mihomo']
    mihomo['router_split']['route_rule_set_ids'] = ['forwarded']
    mihomo['linux_gateway']['rule_set_ids'] = ['gateway-only']
    repository.save_profile(profile_id, {'mihomo': mihomo})
    revision = repository.config_revision(profile_id)
    monkeypatch.setattr('backend.agents.gateway_installation.latest', lambda *_: {'session': {
        **gateway, 'config_changed': repository.config_revision(profile_id) != revision}})
    calls = []
    def desired(config, host, selected, content_loader=None):
        calls.append(selected)
        return {'proxy_cidrs': [{'cidr': '198.18.0.0/16', 'sources': ['fake-ip']}], 'fake_ip_range': '198.18.0.0/16', 'warnings': []}
    monkeypatch.setattr('backend.routers.desired.compute_desired', desired)
    response = client.post(f'/api/setup/{SESSION}/check', json={})
    assert response.status_code == 200, response.json
    assert calls == [['forwarded']]
    assert response.json['route_instructions']['routes'] == [
        {'cidr': '198.18.0.0/16', 'sources': ['fake-ip'], 'requires_source_exclusion': False}]


TELEGRAM_LIBRARY = {'id': 'lib-tg', 'name': 'TG Lab', 'url': 'https://rules.test/telegram.list', 'source_type': 'url',
                    'behavior': 'ipcidr', 'format': 'text', 'enabled': True}
TELEGRAM = {'id': 'tg', 'itemType': 'ruleset', 'library_rule_id': 'lib-tg', 'policy': 'Proxy', 'enabled': True}


def forward_telegram(repository, profile_id, monkeypatch, gateway, *, marker=True):
    repository.update_shared_transaction(lambda shared: shared['rule_library'].append(deepcopy(TELEGRAM_LIBRARY)))
    profile = repository.get_profile(profile_id)
    mihomo = profile['mihomo']
    if marker:
        mihomo['router_split']['route_rule_set_ids'] = ['tg']
    else:
        mihomo['linux_gateway']['rule_set_ids'] = ['tg']
    repository.save_profile(profile_id, {'mihomo': mihomo, 'rule_configs': profile['rule_configs'] + [deepcopy(TELEGRAM)]})
    revision = repository.config_revision(profile_id)
    monkeypatch.setattr('backend.agents.gateway_installation.latest', lambda *_: {'session': {
        **gateway, 'config_changed': repository.config_revision(profile_id) != revision}})


def test_full_tun_route_instructions_download_the_forwarded_rule_sets(installed, monkeypatch):
    """Lab v2-E: on a fresh server nothing had cached Telegram, so its IP ranges were missing."""
    import requests
    client, repository, profile_id, gateway, _ = installed
    forward_telegram(repository, profile_id, monkeypatch, gateway)
    downloads = []
    reply = {'value': requests.ConnectionError('offline')}
    def request_rule(url, **kwargs):
        downloads.append((url, kwargs.get('timeout')))
        if isinstance(reply['value'], Exception):
            raise reply['value']
        return SimpleNamespace(status_code=200, content=reply['value'].encode(), raise_for_status=lambda: None)
    monkeypatch.setattr('backend.routes.rules.request_rule', request_rule)
    response = client.post(f'/api/setup/{SESSION}/check', json={})
    assert response.status_code == 200, response.json
    instructions = response.json['route_instructions']
    assert instructions['incomplete'] is True and instructions['missing_rule_sets'] == ['TG Lab']
    assert '规则集「TG Lab」没有本地缓存，未计入其中的 IP 段' in instructions['warnings']
    # The library URL with the check's timeout; the rule's own is the same, so it is not asked again.
    assert downloads == [('https://rules.test/telegram.list', (5, 20))]
    # The manual confirmation re-checks, so a download that still fails blocks it.
    manual = {'router_mode': 'manual', 'ack_routes': True, 'ack_dns': True}
    response = client.post(f'/api/setup/{SESSION}/finish', json=manual)
    assert response.status_code == 409 and '「TG Lab」的 IP 段仍未下载' in response.json['message']
    assert not client.get(f'/api/setup/{SESSION}').json['completed']
    reply['value'] = '91.108.4.0/22\n149.154.160.0/20\n'
    # The download works now, but the table the user confirmed lacked these ranges.
    response = client.post(f'/api/setup/{SESSION}/finish', json=manual)
    assert response.status_code == 409 and '「TG Lab」的 IP 段已下载' in response.json['message']
    assert not client.get(f'/api/setup/{SESSION}').json['completed']
    response = client.post(f'/api/setup/{SESSION}/check', json={})
    instructions = response.json['route_instructions']
    assert instructions['incomplete'] is False and 'missing_rule_sets' not in instructions
    assert [route['cidr'] for route in instructions['routes']] == ['91.108.4.0/22', '149.154.160.0/20', '198.18.0.0/16']
    assert instructions['warnings'] == []
    # Cached now: a later check needs no network.
    reply['value'] = requests.ConnectionError('offline')
    downloads.clear()
    response = client.post(f'/api/setup/{SESSION}/finish', json=manual)
    assert response.status_code == 200, response.json
    assert response.json['completed'] and downloads == []


def test_route_rule_downloads_stop_starting_after_the_check_budget(installed, monkeypatch):
    client, repository, profile_id, gateway, _ = installed
    forward_telegram(repository, profile_id, monkeypatch, gateway)
    monkeypatch.setattr('backend.common.setup_completion.ROUTE_RULE_BUDGET', -1)
    monkeypatch.setattr('backend.routes.rules.request_rule', lambda *_a, **_k: pytest.fail('downloaded'))
    response = client.post(f'/api/setup/{SESSION}/check', json={})
    assert response.status_code == 200 and response.json['route_instructions']['incomplete'] is True


def test_route_rule_budget_bounds_a_stalled_download(installed, monkeypatch):
    """Review: a download started near the budget's end ran on past the wizard's 90 s timeout."""
    import threading
    import time as clock
    client, repository, profile_id, gateway, _ = installed
    forward_telegram(repository, profile_id, monkeypatch, gateway)
    monkeypatch.setattr('backend.common.setup_completion.ROUTE_RULE_BUDGET', 0.5)
    release, timeouts = threading.Event(), []
    def stalled(url, **kwargs):
        timeouts.append(kwargs.get('timeout'))
        release.wait(10)
        return SimpleNamespace(status_code=200, content=b'91.108.4.0/22\n', raise_for_status=lambda: None)
    monkeypatch.setattr('backend.routes.rules.request_rule', stalled)
    started = clock.monotonic()
    try:
        response = client.post(f'/api/setup/{SESSION}/check', json={})
        elapsed = clock.monotonic() - started
    finally:
        release.set()
    assert response.status_code == 200, response.json
    assert response.json['route_instructions']['incomplete'] is True and elapsed < 3
    # Each request's timeouts shrink to what is left of the budget.
    assert len(timeouts) == 1 and all(0 < value <= 0.5 for value in timeouts[0])


def test_rule_item_url_is_asked_only_when_it_differs_from_the_library_url(monkeypatch):
    import requests
    from backend.routes.rules import get_ruleset_content
    asked = []
    def offline(url, **kwargs):
        asked.append(url)
        raise requests.ConnectionError('offline')
    monkeypatch.setattr('backend.routes.rules.request_rule', offline)
    import uuid
    library = {**TELEGRAM_LIBRARY, 'id': 'lib-' + uuid.uuid4().hex, 'name': 'Uncached Lab'}
    config = {'rule_library': [], 'system_config': {}}
    assert get_ruleset_content({'name': 'x', 'url': library['url']}, library, config_data=config) == ''
    assert asked == [library['url']]
    asked.clear()
    get_ruleset_content({'name': 'x', 'url': 'https://mirror.test/telegram.list'}, library, config_data=config)
    assert asked == [library['url'], 'https://mirror.test/telegram.list']


def test_selective_route_instructions_still_read_only_the_local_cache(installed, monkeypatch):
    client, repository, profile_id, gateway, status = installed
    from backend.common import setup_flow
    session = setup_flow.read(repository, SESSION)
    session['legacy_takeover'] = True
    setup_flow.write(repository, session)
    status.clear()
    status['managed'] = {'active': True, 'healthy': True}
    forward_telegram(repository, profile_id, monkeypatch, gateway, marker=False)
    monkeypatch.setattr('backend.routes.rules.request_rule', lambda *_a, **_k: pytest.fail('downloaded'))
    response = client.post(f'/api/setup/{SESSION}/check', json={})
    assert response.status_code == 200, response.json
    instructions = response.json['route_instructions']
    assert instructions['incomplete'] is False and '规则集「TG Lab」没有本地缓存，未计入其中的 IP 段' in instructions['warnings']


def test_legacy_and_gateway_installation_checks_still_use_gateway_forwarding(installed):
    client, repository, profile_id, gateway, status = installed
    from backend.common import setup_flow
    session = setup_flow.read(repository, SESSION)
    session['legacy_takeover'] = True
    setup_flow.write(repository, session)
    status.clear()
    status['managed'] = {'active': True, 'healthy': False}
    assert client.post(f'/api/setup/{SESSION}/check', json={}).status_code == 409
    status['managed']['healthy'] = True
    assert client.post(f'/api/setup/{SESSION}/check', json={}).status_code == 200
    session = setup_flow.read(repository, SESSION)
    session.update(goal='gateway', legacy_takeover=False)
    setup_flow.write(repository, session)
    assert client.post(f'/api/setup/{SESSION}/check', json={}).status_code == 200
    status['managed']['active'] = False
    assert client.post(f'/api/setup/{SESSION}/check', json={}).status_code == 409


# The wizard keeps the profile's Mihomo base YAML and only sets what its plan needs.
SETUP_RESERVED = {1053: ' Mihomo DNS ', 53: ' OxiDNS ', 9199: ' OxiDNS 管理 API '}


def _template_base():
    import json
    from pathlib import Path
    return json.loads((Path(__file__).parent / 'config_template.json').read_text(encoding='utf-8'))['mihomo']['custom_config']


def _base(text, full_tun=True):
    from backend.common.setup_flow import setup_mihomo_base
    result, changes = setup_mihomo_base(text, full_tun=full_tun, reserved=SETUP_RESERVED)
    return yaml.safe_load(result), changes, result


def test_setup_base_keeps_the_initial_template_around_the_full_tun():
    from backend.common.router_split import FULL_TUN, is_full_tun_document
    original = yaml.safe_load(_template_base())
    document, changes, text = _base(_template_base())
    assert changes == ['dns.listen 由 0.0.0.0:1053 改为 127.0.0.1:1053']
    assert document['tun'] == FULL_TUN and is_full_tun_document(document)
    assert document['dns'] == {**original['dns'], 'listen': '127.0.0.1:1053'}
    for key in ('sniffer', 'profile', 'external-controller', 'secret', 'external-ui', 'external-ui-url',
                'port', 'socks-port', 'mixed-port', 'redir-port', 'tproxy-port', 'find-process-mode'):
        assert document[key] == original[key], key
    assert {key: value for key, value in document.items() if key not in ('tun', 'dns')} == \
        {key: value for key, value in original.items() if key not in ('tun', 'dns')}
    for key in ('proxy-server-nameserver', 'direct-nameserver', 'default-nameserver', 'nameserver-policy',
                'fake-ip-filter', 'respect-rules', 'nameserver'):
        assert document['dns'][key] == original['dns'][key], key
    # Preparing again changes nothing.
    assert _base(text)[1:] == ([], text)


def test_setup_base_keeps_user_settings_and_existing_nameservers():
    source = ('mixed-port: 17890\nexternal-controller: 0.0.0.0:19090\nsecret: s\nsniffer: {enable: true}\n'
              'dns:\n  enable: false\n  listen: 0.0.0.0:53\n  enhanced-mode: redir-host\n  nameserver: [tls://192.0.2.53]\n'
              '  proxy-server-nameserver: [https://192.0.2.1/dns-query]\ntun:\n  enable: true\n  stack: gvisor\n  mtu: 1400\n'
              '  dns-hijack: [udp://1.2.3.4:53]\n  route-address-set: [proxy-ip]\n  route-exclude-address: [192.0.2.9/32]\n'
              "mode: global\nrules: ['MATCH,DIRECT']\n")
    document, changes, _ = _base(source)
    assert document['dns'] == {'enable': True, 'listen': '127.0.0.1:1053', 'enhanced-mode': 'fake-ip',
                               'nameserver': ['tls://192.0.2.53'],
                               'proxy-server-nameserver': ['https://192.0.2.1/dns-query']}
    # Full TUN keys win; the user's stack, MTU and the OxiDNS upstream exclusions stay.
    assert document['tun'] == {'enable': True, 'stack': 'gvisor', 'mtu': 1400, 'dns-hijack': ['any:53', 'tcp://any:53'],
                               'route-exclude-address': ['192.0.2.9/32'], 'auto-route': True, 'auto-redirect': True,
                               'auto-detect-interface': True}
    assert document['mode'] == 'rule'
    assert {key: document[key] for key in ('mixed-port', 'external-controller', 'secret', 'sniffer', 'rules')} == {
        'mixed-port': 17890, 'external-controller': '0.0.0.0:19090', 'secret': 's', 'sniffer': {'enable': True},
        'rules': ['MATCH,DIRECT']}
    assert changes == ['dns.enable 由 false 改为 true', 'dns.listen 由 0.0.0.0:53 改为 127.0.0.1:1053',
                       'dns.enhanced-mode 由 redir-host 改为 fake-ip', 'mode 由 global 改为 rule',
                       'tun 设为完整 TUN（enable、auto-route、auto-redirect、auto-detect-interface，dns-hijack 为 any:53），'
                       '移除 tun.route-address-set']


def test_setup_base_fills_missing_nameservers_only():
    from backend.common.gateway_onboarding import DEFAULT_MIHOMO_NAMESERVERS
    document, changes, _ = _base('log-level: debug\ndns:\n  fallback: [https://192.0.2.2/dns-query]\n')
    assert document['dns']['nameserver'] == list(DEFAULT_MIHOMO_NAMESERVERS)
    assert document['dns']['fallback'] == ['https://192.0.2.2/dns-query'] and document['log-level'] == 'debug'
    assert changes[0] == 'dns.nameserver 为空，已补入 ' + '、'.join(DEFAULT_MIHOMO_NAMESERVERS)
    assert _base('dns: {nameserver: [https://192.0.2.3/dns-query]}\n')[0]['dns']['nameserver'] == ['https://192.0.2.3/dns-query']


def test_setup_base_starts_an_empty_profile_from_the_built_in_template():
    from backend.common.gateway_onboarding import DEFAULT_MIHOMO_NAMESERVERS
    from backend.common.router_split import FULL_TUN
    from backend.converters.mihomo import _default_mihomo_template
    template = _default_mihomo_template()
    for text in ('', '  \n'):
        document, changes, _ = _base(text)
        assert changes == ['基础配置为空，已使用内置 Mihomo 模板']
        assert document == {**template, 'tun': FULL_TUN,
                            'dns': {**template['dns'], 'listen': '127.0.0.1:1053', 'nameserver': list(DEFAULT_MIHOMO_NAMESERVERS)}}
    assert 'tun' not in _base('', full_tun=False)[0]


@pytest.mark.parametrize('text, message', [
    ('dns: [broken', '无法解析'), ('- a\n', 'YAML 对象'), ('dns: 1\n', 'dns 必须是 YAML 对象'), ('tun: [x]\n', 'tun 必须是对象'),
    ('dns: {nameserver: [127.0.0.1:53]}\n', '解析循环'), ('dns: {proxy-server-nameserver: [system]}\n', '解析循环'),
    ('mixed-port: 53\n', 'mixed-port 使用端口 53'), ('external-controller: 127.0.0.1:9199\n', 'external-controller 使用端口 9199'),
    ('port: 1053\n', 'port 使用端口 1053'), ('listeners: [{name: x, type: mixed, port: 53}]\n', 'listeners 使用端口 53'),
    ('tun: {enable: true, include-interface: [eth1]}\n', 'tun.include-interface'),
])
def test_setup_base_reports_what_it_cannot_change_safely(text, message):
    from backend.common.setup_flow import SetupError
    for full_tun in (True, False):
        with pytest.raises(SetupError, match=message.replace('.', r'\.')):
            _base(text, full_tun)


def test_setup_base_without_the_full_tun_leaves_the_tun_to_the_gateway():
    document, changes, text = _base(_template_base(), full_tun=False)
    tun = yaml.safe_load(_template_base())['tun']
    assert document['tun'] == {**tun, 'enable': False} and 'sniffer' in document
    assert changes == ['dns.listen 由 0.0.0.0:1053 改为 127.0.0.1:1053', 'tun 已停用，由 Linux 网关启用时接管']
    assert _base(text, full_tun=False)[1:] == ([], text)
    assert 'tun' not in _base('mixed-port: 7890\n', full_tun=False)[0]


def _prepare_with_base(client, repository, text, goal='router'):
    """Prepare a wizard whose new profile already holds ``text`` as its base."""
    from backend.common import setup_flow
    session = start(client, goal)
    repository.update_shared_transaction(lambda shared: shared['nodes'].append(deepcopy(NODE)))
    client.post(f'/api/setup/{SESSION}/source', json={'node_ids': ['node']})
    repository.save_profile(session['profile_id'], {'mihomo': {'custom_config': text}})
    stored = setup_flow.read(repository, SESSION)
    stored['profile_revision'] = repository.get_profile(session['profile_id'])['_revision']
    setup_flow.write(repository, stored)
    return session


def test_router_prepare_keeps_the_base_and_publishes_it(setup_api):
    from backend.converters.mihomo import generate_mihomo_config
    client, repository = setup_api
    session = _prepare_with_base(client, repository, _template_base())
    revision = repository.get_profile(session['profile_id'])['_revision']
    for _ in range(2):
        response = client.post(f'/api/setup/{SESSION}/prepare', json={})
        assert response.status_code == 200, response.json
        assert response.json['mihomo_changes'] == ['dns.listen 由 0.0.0.0:1053 改为 127.0.0.1:1053']
    profile = repository.get_profile(session['profile_id'])
    assert profile['_revision'] == revision + 1
    snapshot, _ = repository.get_publish_snapshot(session['profile_id'])
    generated = yaml.safe_load(generate_mihomo_config(snapshot, preflight_providers=False))
    assert generated['sniffer']['enable'] is True and generated['secret'] == '123456'
    assert generated['dns']['listen'] == '127.0.0.1:1053' and generated['dns']['proxy-server-nameserver']
    assert generated['tun']['dns-hijack'] == ['any:53', 'tcp://any:53'] and 'route-address-set' not in generated['tun']


def test_prepare_again_keeps_base_edits_made_after_it(setup_api):
    client, repository = setup_api
    session = start(client)
    repository.update_shared_transaction(lambda shared: shared['nodes'].append(deepcopy(NODE)))
    client.post(f'/api/setup/{SESSION}/source', json={'node_ids': ['node']})
    assert client.post(f'/api/setup/{SESSION}/prepare', json={}).status_code == 200
    profile_id = session['profile_id']
    mihomo = repository.get_profile(profile_id)['mihomo']
    edited = mihomo['custom_config'] + 'sniffer:\n  enable: true\n'
    repository.save_profile(profile_id, {'mihomo': {**mihomo, 'custom_config': edited}})
    # The same choices leave the edited base as it is instead of reporting a conflict.
    response = client.post(f'/api/setup/{SESSION}/prepare', json={})
    assert response.status_code == 200, response.json
    assert repository.get_profile(profile_id)['mihomo']['custom_config'] == edited
    # New choices keep it too; only what the plan needs is set again.
    document = yaml.safe_load(edited)
    document['dns']['listen'] = '0.0.0.0:53'
    repository.save_profile(profile_id, {'mihomo': {**mihomo, 'custom_config': yaml.safe_dump(document, sort_keys=False)}})
    response = client.post(f'/api/setup/{SESSION}/prepare', json={'traffic_fallback': 'direct'})
    assert response.status_code == 200, response.json
    assert response.json['mihomo_changes'] == ['dns.listen 由 0.0.0.0:53 改为 127.0.0.1:1053']
    profile = repository.get_profile(profile_id)
    assert yaml.safe_load(profile['mihomo']['custom_config']) == yaml.safe_load(edited)
    assert profile['rule_configs'][-1]['policy'] == 'DIRECT'
    # Edits to anything else the wizard writes still stop it.
    repository.save_profile(profile_id, {'rule_configs': profile['rule_configs'][:-1]})
    response = client.post(f'/api/setup/{SESSION}/prepare', json={'traffic_fallback': 'proxy'})
    assert response.status_code == 409 and '其他页面' in response.json['message']


def test_prepare_reports_an_unreadable_base_without_writing(setup_api):
    client, repository = setup_api
    session = _prepare_with_base(client, repository, 'dns: [broken')
    before = repository.get_profile(session['profile_id'])
    response = client.post(f'/api/setup/{SESSION}/prepare', json={})
    assert response.status_code == 400 and '无法解析' in response.json['message']
    assert repository.get_profile(session['profile_id']) == before


def test_gateway_prepare_keeps_the_base_and_disables_its_tun(setup_api):
    client, repository = setup_api
    session = _prepare_with_base(client, repository, _template_base(), goal='gateway')
    response = client.post(f'/api/setup/{SESSION}/prepare', json={})
    assert response.status_code == 200, response.json
    base = yaml.safe_load(repository.get_profile(session['profile_id'])['mihomo']['custom_config'])
    assert base['tun']['enable'] is False and base['sniffer']['enable'] is True
    assert 'router_split' not in repository.get_profile(session['profile_id'])['mihomo']
