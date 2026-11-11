"""Subscription top-level hosts: extraction, scoped merge, priority and refresh."""
from copy import deepcopy
from unittest.mock import Mock

import pytest
import yaml

from backend.common import config as config_module
from backend.common.config_repository import ProfileRepository
from backend.converters.mihomo import generate_mihomo_config
from backend.utils.sub_store_client import SubscriptionResult, proxies_to_nodes
from backend.utils.subscription_cache import load_subscription_cache, save_subscription_nodes
from backend.utils.subscription_hosts import MAX_ENTRIES, extract_subscription_hosts
from backend.utils.subscription_refresh import source_cache_metadata

AIRPORT = """\
mixed-port: 7890
proxies:
  - {name: HK 01, type: socks5, server: jp-node.example.net, port: 1080}
  - name: JP 01
    type: socks5
    server: HK-NODE.example.net.
    port: 1080
    # an indented key named hosts must not be read as the top-level section
    hosts: ignored.example
  - {name: SG 01, type: socks5, server: 203.0.113.9, port: 1080}
hosts:
  jp-node.example.net: jp-entry.example.org
  hk-node.example.net: [198.51.100.12, '2001:db8::12']
  google.com: 203.0.113.66
proxy-groups:
  - {name: PROXY, type: select, proxies: [HK 01, JP 01, SG 01]}
"""


@pytest.fixture
def repo(tmp_path, monkeypatch):
    repository = ProfileRepository(tmp_path, default_config_factory=config_module.get_default_config,
                                   initial_config_factory=config_module.get_default_config)
    monkeypatch.setattr(config_module, '_repository', repository)
    config_module.reset_config_context()
    yield repository
    config_module.reset_config_context()


def store(sub, text, source='direct_yaml'):
    """Save a subscription cache the way a refresh stores a downloaded body."""
    result = SubscriptionResult(text, source)
    data = yaml.safe_load(text)
    nodes = proxies_to_nodes(data.get('proxies') or []) if isinstance(data, dict) else []
    for index, node in enumerate(nodes):
        node.update(id=f"{sub['id']}-{index}", subscription_id=sub['id'], subscription_name=sub['name'])
    return save_subscription_nodes(sub['id'], nodes, source_cache_metadata(sub, result))


def sub(id='air', name='机场', url=None, **extra):
    return {'id': id, 'name': name, 'url': url or f'https://sub.test/{id}', 'enabled': True, **extra}


def profile(subscriptions, *, groups=None, aggregations=(), nodes=(), custom='mode: rule\ndns: {enable: true}\n',
            mosdns_hosts=''):
    config = config_module.get_default_config()
    config.update(profile_id='default')
    config['system_config']['server_domain'] = 'https://panel.test'
    config['mihomo']['custom_config'] = custom
    config['mosdns']['custom_hosts'] = mosdns_hosts
    config['subscriptions'] = list(subscriptions)
    config['subscription_aggregations'] = list(aggregations)
    config['nodes'] = list(nodes)
    config['proxy_groups'] = groups if groups is not None else [
        {'id': 'g', 'name': 'Proxy', 'type': 'select', 'subscriptions': [s['id'] for s in subscriptions]}]
    config['rule_configs'] = [{'id': 'm', 'itemType': 'rule', 'rule_type': 'MATCH', 'policy': 'Proxy', 'enabled': True}]
    return config


def generate(config, **kwargs):
    report = {}
    main = yaml.safe_load(generate_mihomo_config(config, preflight_providers=False, report=report, **kwargs))
    return main, report['subscription_hosts']


def reasons(report):
    return {item['domain']: item.get('reason') for item in report['items'] if item['status'] == 'ignored'}


# ------------------------------------------------------------------ extraction

def test_extract_reads_only_the_top_level_section_and_normalizes():
    record = extract_subscription_hosts(AIRPORT)
    assert record['entries'] == {
        'jp-node.example.net': 'jp-entry.example.org',
        'hk-node.example.net': ['198.51.100.12', '2001:db8::12'],
        'google.com': ['203.0.113.66'],
    }
    assert record['total'] == 3 and record['skipped_count'] == 0


@pytest.mark.parametrize('key,value,reason', [
    ('*.example.net', '198.51.100.1', 'wildcard'),
    ('+.example.net', '198.51.100.1', 'wildcard'),
    ('.example.net', '198.51.100.1', 'wildcard'),
    ('node.test', 'lan', 'invalid_value'),
    ('node.test', '198.51.100.1,198.51.100.2', 'invalid_value'),
    ('node.test', [], 'invalid_value'),
    ('node.test', ['198.51.100.1', 'other.test'], 'invalid_value'),
    ('node.test', {'a': 1}, 'invalid_value'),
    ('node.test', 'node.test', 'invalid_value'),
    ('node.test', '0.0.0.0', 'invalid_value'),
    ('node.test', '224.0.0.1', 'invalid_value'),
    ('node.test', [f'198.51.100.{i}' for i in range(9)], 'too_many_addresses'),
    ('198.51.100.1', 'node.test', 'invalid_domain'),
    ('localhost', '127.0.0.1', 'invalid_domain'),
    ('bad_label-.test..', '198.51.100.1', 'invalid_domain'),
])
def test_extract_drops_unsafe_or_malformed_entries_with_a_reason(key, value, reason):
    text = yaml.safe_dump({'proxies': [], 'hosts': {key: value, 'ok.test': '198.51.100.7'}})
    record = extract_subscription_hosts(text)
    assert record['entries'] == {'ok.test': ['198.51.100.7']}
    assert record['skipped'] == [{'domain': str(key).lower(), 'reason': reason}]
    assert record['total'] == 2 and record['skipped_count'] == 1


def test_extract_bounds_entries_and_stored_samples():
    hosts = {f'n{i}.test': '198.51.100.1' for i in range(MAX_ENTRIES + 30)}
    record = extract_subscription_hosts(yaml.safe_dump({'proxies': [], 'hosts': hosts}))
    assert len(record['entries']) == MAX_ENTRIES
    assert record['skipped_count'] == 30 and len(record['skipped']) == 20
    assert {item['reason'] for item in record['skipped']} == {'over_limit'}


@pytest.mark.parametrize('text,expected', [
    ('c3M6Ly9ZV1Z6TFRJMU5pMW5ZMjA2Y0dGemMzZHZjbVE9QDE5OC41MS4xMDAuMTo4Mzg4I2hvc3Rz', None),
    ('proxies: []\n', None),
    ('proxies:\n  - {name: a, type: http, server: a.test, port: 80}\n    # hosts: here\n', None),
    ('proxies: []\nhosts: {node.test: 198.51.100.4}\n', {'node.test': ['198.51.100.4']}),
    ('hosts:\n  node.test:\n  - 198.51.100.4\nproxies: []\n', {'node.test': ['198.51.100.4']}),
])
def test_extract_handles_bodies_without_or_with_flow_hosts(text, expected):
    record = extract_subscription_hosts(text)
    assert (record and record['entries']) == expected


@pytest.mark.parametrize('text,reason', [
    ('proxies: []\nhosts: [198.51.100.1]\n', 'not_mapping'),
    ('proxies: []\nhosts:\n  a.test: [unclosed\n', 'parse_error'),
])
def test_extract_reports_a_section_it_cannot_use(text, reason):
    assert extract_subscription_hosts(text) == {'entries': {}, 'total': 0, 'skipped_count': 1,
                                                'skipped': [{'domain': '', 'reason': reason}]}


def test_only_downloaded_bodies_carry_hosts_into_the_cache_metadata():
    feed = sub()
    assert source_cache_metadata(feed, SubscriptionResult(AIRPORT, 'direct_yaml'))['hosts']['total'] == 3
    assert 'hosts' not in source_cache_metadata(feed, SubscriptionResult(AIRPORT, 'cache'))
    conversion = yaml.safe_dump({'proxies': yaml.safe_load(AIRPORT)['proxies']})
    assert 'hosts' not in source_cache_metadata(feed, SubscriptionResult(conversion, 'sub_store'))


def test_scheduled_download_stores_hosts_next_to_nodes(repo, monkeypatch):
    from backend.utils.subscription_refresh import download_subscription_nodes
    feed = sub()
    monkeypatch.setattr('backend.utils.sub_store_client.get_subscription_proxies_yaml',
                        lambda *_a, **_k: SubscriptionResult(AIRPORT, 'direct_yaml'))
    download_subscription_nodes(feed)
    cache = load_subscription_cache(feed['id'])
    assert cache['count'] == 3
    assert cache['metadata']['hosts']['entries']['google.com'] == ['203.0.113.66']


def test_provider_endpoint_stores_hosts_but_serves_only_proxies(repo, monkeypatch):
    from flask import Flask
    from backend.routes import register_blueprints
    feed = sub(interval=60)
    repo.update_shared_transaction(lambda shared: shared['subscriptions'].append(feed))
    monkeypatch.setattr('backend.routes.subscriptions.get_subscription_proxies_yaml',
                        lambda *_a, **_k: SubscriptionResult(AIRPORT, 'direct_yaml'))
    app = Flask(__name__)
    register_blueprints(app)
    response = app.test_client().get(f"/api/subscriptions/{feed['id']}/proxies")
    assert response.status_code == 200, response.get_data(as_text=True)
    served = yaml.safe_load(response.get_data(as_text=True))
    assert set(served) == {'proxies'}
    assert load_subscription_cache(feed['id'])['metadata']['hosts']['total'] == 3


# ----------------------------------------------------------------------- merge

def test_merges_only_domains_of_nodes_the_configuration_delivers(repo):
    feed = sub()
    store(feed, AIRPORT)
    main, report = generate(profile([feed]))
    assert main['hosts'] == {'jp-node.example.net': 'jp-entry.example.org',
                             'hk-node.example.net': ['198.51.100.12', '2001:db8::12']}
    assert report['merged'] == 2 and report['ignored'] == 1 and report['domain_mapped'] == 1
    assert reasons(report) == {'google.com': 'not_node_domain'}
    assert report['subscriptions'] == [{'id': 'air', 'name': '机场', 'total': 3, 'merged': 2, 'ignored': 1}]
    # Providers still deliver nodes; hosts never leak into a provider definition.
    assert 'hosts' not in main['proxy-providers']['机场']


def test_extraction_skips_are_counted_with_their_reasons(repo):
    feed = sub()
    store(feed, AIRPORT.replace('  google.com: 203.0.113.66\n', '  "+.example.net": 198.51.100.1\n  bad.test: lan\n'))
    _main, report = generate(profile([feed]))
    assert report['merged'] == 2 and report['ignored'] == 2
    assert reasons(report) == {'+.example.net': 'wildcard', 'bad.test': 'invalid_value'}


def test_unreferenced_or_disabled_subscriptions_contribute_nothing(repo):
    feed = sub()
    store(feed, AIRPORT)
    main, report = generate(profile([feed], groups=[{'id': 'g', 'name': 'Proxy', 'type': 'select', 'manual_nodes': ['DIRECT']}]))
    assert 'hosts' not in main and report == {'merged': 0, 'ignored': 0, 'domain_mapped': 0, 'subscriptions': [], 'items': []}
    disabled = profile([{**feed, 'enabled': False}])
    assert 'hosts' not in generate(disabled)[0]


def test_subscription_filter_removes_the_domains_of_filtered_nodes(repo):
    feed = sub(regex_filter='HK')
    store(feed, AIRPORT)
    main, report = generate(profile([feed]))
    assert main['hosts'] == {'jp-node.example.net': 'jp-entry.example.org'}
    assert reasons(report)['hk-node.example.net'] == 'not_node_domain'


def test_cache_of_a_previous_url_is_not_used(repo):
    feed = sub()
    store(feed, AIRPORT)
    main, report = generate(profile([{**feed, 'url': 'https://sub.test/rotated'}]))
    assert 'hosts' not in main and report['merged'] == 0


def test_hand_written_hosts_win_then_mosdns_then_subscriptions(repo):
    feed = sub()
    store(feed, AIRPORT.replace('  google.com: 203.0.113.66\n', '  wild.example.net: 198.51.100.40\n')
          .replace('  - {name: SG 01,', '  - {name: W, type: socks5, server: wild.example.net, port: 1}\n  - {name: SG 01,'))
    custom = ('mode: rule\nhosts:\n  jp-node.example.net: 192.0.2.10\n  "+.wild.example.net": 192.0.2.11\n'
              'dns: {enable: true}\n')
    config = profile([feed], custom=custom, mosdns_hosts='hk-node.example.net 10.0.0.2\n')
    main, report = generate(config, sync_lan_hosts=True)
    assert main['hosts'] == {'jp-node.example.net': '192.0.2.10', '+.wild.example.net': '192.0.2.11',
                             'hk-node.example.net': '10.0.0.2'}
    assert reasons(report) == {'jp-node.example.net': 'custom_hosts', 'wild.example.net': 'custom_hosts',
                               'hk-node.example.net': 'mosdns_hosts'}
    # Download configurations never carry LAN hosts, so the subscription value applies there.
    main, report = generate(config)
    assert main['hosts']['hk-node.example.net'] == ['198.51.100.12', '2001:db8::12']


def test_conflicting_subscriptions_follow_the_subscription_list_order(repo):
    first, second = sub('a', '甲'), sub('b', '乙')
    body = 'proxies: [{name: N, type: socks5, server: shared.test, port: 1}]\nhosts: {shared.test: %s}\n'
    store(first, body % '198.51.100.1')
    store(second, body % '198.51.100.2')
    main, report = generate(profile([first, second]))
    assert main['hosts'] == {'shared.test': '198.51.100.1'}
    ignored = next(item for item in report['items'] if item['status'] == 'ignored')
    assert ignored['subscription'] == '乙' and ignored['reason'] == 'conflict' and '「甲」' in ignored['detail']
    main, _ = generate(profile([second, first]))
    assert main['hosts'] == {'shared.test': '198.51.100.2'}
    store(second, body % '198.51.100.1')
    main, report = generate(profile([first, second]))
    assert main['hosts'] == {'shared.test': '198.51.100.1'} and report['ignored'] == 0


def test_aggregation_uses_its_filter_and_member_nodes(repo):
    feed = sub()
    store(feed, AIRPORT)
    other = sub('other', '其他')
    store(other, 'proxies: [{name: X, type: socks5, server: x.other.test, port: 1}]\nhosts: {x.other.test: 198.51.100.50}\n')
    imported = {'id': 'imported', 'name': 'Imported', 'enabled': True, 'type': 'socks5', 'server': 'x.other.test',
                'port': 1, 'subscription_id': 'other'}
    aggregation = {'id': 'agg', 'name': 'Asia', 'enabled': True, 'subscriptions': ['air'], 'nodes': ['imported'],
                   'regex_filter': 'JP|Imported'}
    config = profile([feed, other], aggregations=[aggregation], nodes=[imported],
                     groups=[{'id': 'g', 'name': 'Proxy', 'type': 'select', 'aggregations': ['agg']}])
    main, report = generate(config)
    assert main['hosts'] == {'hk-node.example.net': ['198.51.100.12', '2001:db8::12'],
                             'x.other.test': '198.51.100.50'}
    assert reasons(report) == {'jp-node.example.net': 'not_node_domain', 'google.com': 'not_node_domain'}


def test_imported_inline_nodes_use_their_own_subscription_hosts(repo):
    feed = sub()
    store(feed, AIRPORT)
    node = {'id': 'hk', 'name': 'HK 01', 'enabled': True, 'type': 'socks5', 'server': 'jp-node.example.net',
            'port': 1080, 'subscription_id': 'air'}
    stranger = {'id': 'own', 'name': 'Own', 'enabled': True, 'type': 'socks5', 'server': 'google.com', 'port': 1}
    config = profile([feed], nodes=[node, stranger],
                     groups=[{'id': 'g', 'name': 'Proxy', 'type': 'select', 'manual_nodes': ['hk', 'own']}])
    main, report = generate(config)
    assert main['hosts'] == {'jp-node.example.net': 'jp-entry.example.org'}
    # A hand-made node on google.com does not let the subscription remap google.com.
    assert reasons(report)['google.com'] == 'not_node_domain'


def test_a_mapping_loop_is_never_emitted(repo):
    feed = sub()
    store(feed, AIRPORT)
    custom = 'mode: rule\nhosts:\n  "+.example.org": jp-node.example.net\n'
    main, report = generate(profile([feed], custom=custom))
    assert main['hosts']['+.example.org'] == 'jp-node.example.net'
    assert 'jp-node.example.net' not in main['hosts']
    assert reasons(report)['jp-node.example.net'] == 'cycle'


def test_preview_route_returns_the_merge_report(repo, monkeypatch):
    from flask import Flask
    from backend.routes import register_blueprints
    feed = sub()
    config = profile([feed])
    repo.update_shared_transaction(lambda shared: shared['subscriptions'].append(feed))
    saved = repo.get_profile('default')
    saved.update(proxy_groups=config['proxy_groups'], rule_configs=config['rule_configs'])
    saved.setdefault('mihomo', {})['custom_config'] = config['mihomo']['custom_config']
    repo.save_profile('default', saved)
    store(feed, AIRPORT)
    app = Flask(__name__)
    register_blueprints(app)
    body = app.test_client().post('/api/generate/mihomo/preview', json={}).get_json()
    assert yaml.safe_load(body['content'])['hosts']['jp-node.example.net'] == 'jp-entry.example.org'
    assert {key: body['subscription_hosts'][key] for key in ('merged', 'ignored', 'domain_mapped')} == \
        {'merged': 2, 'ignored': 1, 'domain_mapped': 1}


# --------------------------------------------------------------- after refresh

def test_hosts_change_invalidates_agent_content_checks_once(repo, monkeypatch):
    invalidate = Mock()
    monkeypatch.setattr('backend.agents.content_sync.invalidate_content_sync', invalidate)
    feed = sub()
    store(feed, AIRPORT)
    assert invalidate.call_count == 1  # none -> hosts
    store(feed, AIRPORT)
    assert invalidate.call_count == 1  # same entries, only nodes refreshed
    store(feed, AIRPORT.replace('203.0.113.66', '203.0.113.67'))
    assert invalidate.call_count == 2


def test_content_checker_invalidation_drops_finished_and_in_flight_results(monkeypatch):
    from types import SimpleNamespace
    from backend.agents.content_sync import ContentSyncChecker
    from backend.test_agent_content_sync import DeferredExecutor
    executor = DeferredExecutor()
    checker = ContentSyncChecker(executor=executor, clock=lambda: 0.0)
    fingerprints = {'value': 'v1:' + '0' * 64}
    agent = {'id': 'a', 'profile_id': 'default', 'service_type': 'mihomo', 'config_version': 'x',
             'applied_content': {'fingerprint': fingerprints['value'], 'profile_id': 'default', 'service_type': 'mihomo',
                                 'config_version': 'x', 'deployment_id': 'd', 'config_path': 'config.yaml'}}
    # The worker prepares a bundle and fingerprints its manifest; here the manifest is the fingerprint.
    checker._prepare_bundle = lambda *_args: SimpleNamespace(manifest=fingerprints['value'])
    monkeypatch.setattr('backend.agents.deployment_bundle.deployment_content_fingerprint', lambda manifest: manifest)
    from backend.agents import content_sync as module
    query = lambda: checker.get_content_sync(agent, {'profile_id': 'default'}, '', None,
                                             repository=module._current_repository())
    assert query() == {'status': 'checking'}
    executor.run()
    assert query()['status'] == 'synced'
    fingerprints['value'] = 'v1:' + '1' * 64
    assert query()['status'] == 'synced'  # within the success TTL
    checker.invalidate()
    assert query() == {'status': 'checking'}
    checker.invalidate()  # while that check runs: its answer may predate the change
    executor.run()
    assert query() == {'status': 'checking'}
    executor.run()
    assert query()['status'] == 'changed'


def test_agent_shows_pending_after_subscription_hosts_change(api_env_hosts):
    client, ids, executor, feed, listing, push = api_env_hosts
    agent_id = ids['mihomo']
    assert push(client, agent_id).status_code == 200
    listing(client)
    executor.run()
    assert listing(client)['mihomo']['content_sync']['status'] == 'synced'
    # The same body fetched again keeps the cached comparison.
    store(feed, ENTRY_FEED)
    assert listing(client)['mihomo']['content_sync']['status'] == 'synced' and not executor.jobs
    # Only the hosts change: nodes are identical, providers would not tell.
    store(feed, ENTRY_FEED.replace('198.51.100.12', '198.51.100.13'))
    # Invalidated immediately instead of after the fifteen-minute reuse.
    assert listing(client)['mihomo']['content_sync']['status'] == 'checking'
    executor.run()
    assert listing(client)['mihomo']['content_sync']['status'] == 'changed'
    assert push(client, agent_id, 'second').status_code == 200
    listing(client)
    executor.run()
    assert listing(client)['mihomo']['content_sync']['status'] == 'synced'


ENTRY_FEED = """\
hosts:
  left.entry.test: 198.51.100.12
proxies:
  - {name: A, type: socks5, server: left.entry.test, port: 1080}
"""


@pytest.fixture
def api_env_hosts(content_env, monkeypatch):
    """The real push -> baseline -> list flow with a subscription cache refreshed in between."""
    from flask import Flask
    from backend.agents.content_sync import ContentSyncChecker
    from backend.agents.manager import AgentManager
    from backend.routes import agents, register_blueprints
    from backend.test_agent_content_api import DeferredExecutor, listing, push
    config, _remote, _fetches, repository = content_env
    feed = config['subscriptions'][0]
    store(feed, ENTRY_FEED)
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
        return {'success': True, 'status': 'succeeded', 'config_version': 'agent-hash'}

    monkeypatch.setattr(manager, '_deployment_request', transport)
    app = Flask(__name__)
    register_blueprints(app)
    ids = {'mihomo': manager.register_agent({'name': 'mihomo', 'host': 'mihomo.test', 'service_type': 'mihomo'})['id']}
    return app.test_client(), ids, executor, feed, listing, push


from backend.test_agent_content_preparation import content_env  # noqa: E402,F401
