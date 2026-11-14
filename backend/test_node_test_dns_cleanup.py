"""Upgrading past the removed "OxiDNS 托管" mode leaves nothing behind and contacts no device."""
from copy import deepcopy

import pytest
import requests
import yaml

from backend import notifications
from backend.agents import node_test_dns_cleanup as cleanup
from backend.agents.manager import AgentManager
from backend.common import config as config_store
from backend.common.config_repository import ProfileRepository
from backend.common.dns_watchdog import WATCHDOG_RULE
from backend.utils import node_dns

ENDPOINT = '192.0.2.2:15353'
DOH = list(node_dns.DIRECT_DOH)


def listener(endpoint_id='0123456789abcdef', endpoint=ENDPOINT):
    prefix = 'cf_node_test_' + endpoint_id
    return [
        {'tag': prefix + '_forward', 'type': 'forward', 'args': {'upstreams': [{'addr': 'udp://192.0.2.53'}]}},
        {'tag': prefix + '_sequence', 'type': 'sequence', 'args': [
            {'exec': '$' + prefix + '_forward'}, {'matches': ['!has_resp'], 'exec': 'reject 2'}, {'exec': 'accept'}]},
        *[{'tag': prefix + '_' + protocol, 'type': protocol + '_server',
           'args': {'listen': endpoint, 'entry': prefix + '_sequence'}} for protocol in ('udp', 'tcp')],
    ]


MAIN = [
    {'tag': 'forward_local', 'type': 'forward', 'args': {'upstreams': [{'addr': '192.0.2.53'}]}},
    {'tag': 'main', 'type': 'sequence', 'args': [{'exec': '$forward_local'}, {'exec': 'accept'}]},
    {'tag': 'server_udp', 'type': 'udp_server', 'args': {'listen': '0.0.0.0:53', 'entry': 'main'}},
]


@pytest.fixture
def env(tmp_path, monkeypatch):
    previous = config_store._repository
    repository = ProfileRepository(tmp_path)
    config_store.set_repository(repository)
    manager = AgentManager(repository)
    agent = manager.register_agent({'name': 'DNS', 'host': '192.0.2.2', 'port': 8081, 'service_type': 'oxidns'})
    monkeypatch.setattr(requests.sessions.Session, 'request', lambda *_a, **_k: pytest.fail('cleanup must not contact a device'))

    def seed(plugins, records, dns_config=None, legacy=None):
        repository.save_profile('default', {'oxidns': {
            'config_mode': 'native', 'native_config': yaml.safe_dump({'plugins': MAIN + plugins}, sort_keys=False)}})

        def update(system):
            target = next(item for item in system['agents'] if item['id'] == agent['id'])
            target[cleanup.FIELD] = deepcopy(records)
            if dns_config is not None:
                system['system_config'][node_dns.CONFIG_KEY] = dns_config
            if legacy is not None:
                system['system_config']['node_test_dns'] = legacy
        repository.update_system_transaction(update)

    def tags():
        document = yaml.safe_load(repository.get_profile('default')['oxidns']['native_config'])
        return [plugin['tag'] for plugin in document['plugins']]

    yield type('Env', (), {'repository': repository, 'agent_id': agent['id'], 'seed': staticmethod(seed), 'tags': staticmethod(tags),
                           'agent': staticmethod(lambda: manager.get_agent_by_id(agent['id']))})
    config_store.set_repository(previous)


def record(plugins, status='ready', installed=True, endpoint_id='0123456789abcdef', endpoint=ENDPOINT):
    return {'endpoint_id': endpoint_id, 'endpoint': endpoint, 'profile_id': 'default', 'status': status,
            'installed': installed, 'operation': 'create', 'plugins': deepcopy(plugins)}


def managed(endpoint_id='0123456789abcdef'):
    return {'mode': 'managed', 'servers': [], 'bootstrap': [], 'managed': {'agent_id': 'x', 'endpoint_id': endpoint_id}}


def notices():
    return [item for item in notifications.snapshot() if item['source'] == notifications.NOTICE_SOURCE]


def test_listener_setting_and_records_are_removed_and_the_user_is_told(env):
    plugins = listener()
    published = deepcopy(plugins)
    published[1]['args'].insert(0, deepcopy(WATCHDOG_RULE))       # as a publication leaves it
    failed = record(listener('ffffffffffffffff'), status='failed', installed=False, endpoint_id='ffffffffffffffff')
    env.seed(published, {'0123456789abcdef': record(plugins), 'ffffffffffffffff': failed}, managed(), [ENDPOINT])
    before = env.repository.get_profile('default')['_revision']
    assert cleanup.migrate(env.repository) is True
    assert env.tags() == [plugin['tag'] for plugin in MAIN]         # only our plugins went
    assert env.repository.get_profile('default')['_revision'] > before   # shows as unpublished on the Agent page
    assert cleanup.FIELD not in env.agent()
    system = env.repository.get_system()['system_config']
    assert system[node_dns.CONFIG_KEY] == {'mode': 'custom', 'servers': DOH, 'bootstrap': []}
    assert system['node_test_dns'] == DOH
    assert node_dns.load_config() == {'mode': 'custom', 'servers': DOH, 'bootstrap': []}
    notice, = notices()
    assert '托管' in notice['message'] and ENDPOINT in notice['message'] and 'DNS' in notice['message']
    assert DOH[0] in notice['message'] and '下一次发布' in notice['message']
    assert notice['target']['path'] == '/nodes'
    # A second start (or another worker) changes nothing more.
    snapshot = (env.repository.get_system(), env.repository.get_profile('default'))
    assert cleanup.migrate(env.repository) is False
    assert (env.repository.get_system(), env.repository.get_profile('default')) == snapshot
    assert len(notices()) == 1


@pytest.mark.parametrize('variant', ['edited', 'referenced'])
def test_listener_the_user_changed_is_left_alone_and_keeps_serving_node_tests(env, variant):
    plugins = listener()
    deployed = deepcopy(plugins)
    extra = []
    if variant == 'edited':
        deployed[0]['args']['upstreams'][0]['addr'] = 'udp://192.0.2.99'
    else:
        extra = [{'tag': 'other', 'type': 'sequence', 'args': [{'exec': '$' + plugins[0]['tag']}]}]
    env.seed(deployed + extra, {'0123456789abcdef': record(plugins)}, managed(), [ENDPOINT])
    cleanup.migrate(env.repository)
    assert set(plugin['tag'] for plugin in plugins) <= set(env.tags())
    assert cleanup.FIELD not in env.agent()
    system = env.repository.get_system()['system_config']
    assert system[node_dns.CONFIG_KEY] == {'mode': 'custom', 'servers': [ENDPOINT], 'bootstrap': []}
    notice, = notices()
    assert ENDPOINT in notice['message'] and '下一次发布' not in notice['message']


def test_other_node_test_dns_choices_are_kept(env):
    plugins = listener()
    custom = {'mode': 'custom', 'servers': ['https://192.0.2.53/dns-query', ENDPOINT], 'bootstrap': []}
    env.seed(plugins, {'0123456789abcdef': record(plugins)}, custom, custom['servers'])
    cleanup.migrate(env.repository)
    assert env.tags() == [plugin['tag'] for plugin in MAIN]
    # Only the address that no longer exists is dropped.
    assert env.repository.get_system()['system_config'][node_dns.CONFIG_KEY]['servers'] == ['https://192.0.2.53/dns-query']
    # System DNS is not touched at all, and without the removed mode nothing is announced as changed.
    env.seed(plugins, {'0123456789abcdef': record(plugins)}, {'mode': 'system', 'servers': [], 'bootstrap': []}, [])
    cleanup.migrate(env.repository)
    assert env.repository.get_system()['system_config'][node_dns.CONFIG_KEY]['mode'] == 'system'


def test_installations_that_never_used_the_mode_are_not_written(env):
    before = (env.repository.get_system(), env.repository.get_profile('default'))
    assert cleanup.migrate(env.repository) is False
    assert (env.repository.get_system(), env.repository.get_profile('default')) == before
    assert notices() == []


def test_managed_setting_whose_record_is_gone_moves_to_doh(env):
    env.seed([], {}, managed('missing'), [])
    cleanup.migrate(env.repository)
    assert env.repository.get_system()['system_config'][node_dns.CONFIG_KEY]['servers'] == DOH
