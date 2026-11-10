"""Domain-to-domain subscription hosts for node connections: proxy-server-nameserver-policy and Fake-IP filter."""
from copy import deepcopy
from unittest.mock import Mock

import pytest
import yaml

from backend.test_subscription_hosts import generate, profile, repo, store, sub  # noqa: F401
from backend.utils.proxy_server_dns import (CONNECTIVITY_FILTER, MIN_CORE_TEXT, POLICY_KEY, compress, core_too_old,
                                            final_target, listener_address, parse_core_version, reported_core_version)

FEED = """\
proxies:
  - {name: HK 01, type: socks5, server: hk1.example.net, port: 1080}
  - {name: HK 02, type: socks5, server: hk1.example.net, port: 1081}
  - {name: JP 01, type: socks5, server: jp1.example.net, port: 1080}
  - {name: SG 01, type: socks5, server: 203.0.113.9, port: 1080}
  - {name: DE 01, type: socks5, server: de1.example.net, port: 1080}
hosts:
  hk1.example.net: hk1.example.org
  jp1.example.net: 198.51.100.12
"""
THREE = """\
proxies:
  - {name: HK, type: socks5, server: jp-node.example.net, port: 1080}
  - {name: JP, type: socks5, server: hk-node.example.net, port: 1080}
  - {name: DE, type: socks5, server: de-node.example.net, port: 1080}
hosts:
  jp-node.example.net: jp-entry.example.org
  hk-node.example.net: hk-entry.example.org
  de-node.example.net: de-entry.example.org
"""
OTHER = 'proxies: [{name: US 01, type: socks5, server: us1.other-air.net, port: 1080}]\n'
FAKE_IP = """\
mode: rule
dns:
  enable: true
  listen: 0.0.0.0:1053
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  nameserver: [223.5.5.5]
  proxy-server-nameserver: [https://223.5.5.5/dns-query]
"""
LISTENER = 'udp://127.0.0.1:1053'


def config_with(custom=FAKE_IP, *, feeds=None, **kwargs):
    feeds = feeds if feeds is not None else [(sub(), FEED)]
    for feed, body in feeds:
        store(feed, body)
    config = profile([feed for feed, _ in feeds], custom=custom, **kwargs)
    config['system_config']['config_token'] = 'tok'
    return config


# ------------------------------------------------------------------ helpers

@pytest.mark.parametrize('listen,expected', [
    ('0.0.0.0:1053', ('udp://127.0.0.1:1053', None)),
    ('[::]:53', ('udp://127.0.0.1:53', None)),
    (':7874', ('udp://127.0.0.1:7874', None)),
    ('127.0.0.1:7874', ('udp://127.0.0.1:7874', None)),
    ('192.168.1.2:53', ('udp://192.168.1.2:53', None)),
    ('[::1]:1053', ('udp://[::1]:1053', None)),
    ('localhost:53', (None, 'listen_unparsed')),
    ('0.0.0.0', (None, 'listen_unparsed')),
    ('0.0.0.0:0', (None, 'listen_unparsed')),
    ('', (None, 'no_listen')),
    (None, (None, 'no_listen')),
])
def test_listener_address_is_derived_from_dns_listen(listen, expected):
    assert listener_address(listen) == expected


@pytest.mark.parametrize('domains,expected', [
    ({'a.example.org', 'b.example.org'}, ['+.example.org']),
    ({'hk1.node.example.org', 'jp2.node.example.org'}, ['+.node.example.org']),
    ({'example.org', 'x.example.org'}, ['+.example.org']),
    ({'only.example.org'}, ['only.example.org']),
    # Public suffixes, including private ones, never become a wildcard.
    ({'a.github.io', 'b.github.io'}, ['a.github.io', 'b.github.io']),
    ({'example.co.uk', 'other.co.uk'}, ['example.co.uk', 'other.co.uk']),
    ({'a.example.co.uk', 'b.example.co.uk'}, ['+.example.co.uk']),
    # A suffix the list does not know stays exact.
    ({'edge-a.shared.lab.invalid', 'edge-b.shared.lab.invalid'},
     ['edge-a.shared.lab.invalid', 'edge-b.shared.lab.invalid']),
])
def test_compress_merges_only_within_one_registrable_domain(domains, expected):
    assert compress(domains) == expected


def test_final_target_follows_hosts_like_mihomo():
    hosts = {'a.test': 'b.test', 'b.test': 'c.test', 'x.test': 'y.test', 'y.test': '198.51.100.1',
             '+.wild.test': ['198.51.100.2'], 'w.test': 'n.wild.test', 'ip.test': ['198.51.100.3']}
    assert final_target(hosts, 'a.test') == 'c.test'
    assert final_target(hosts, 'x.test') is None          # ends in an IP: Mihomo dials it directly
    assert final_target(hosts, 'w.test') is None          # a wildcard key resolves the target
    assert final_target(hosts, 'ip.test') is None
    assert final_target(hosts, 'missing.test') is None


@pytest.mark.parametrize('text,parsed,old', [
    ('v1.19.33', (1, 19, 33), False),
    ('v1.19.20', (1, 19, 20), False),
    ('v1.19.19', (1, 19, 19), True),
    ('Mihomo Meta v1.18.10 linux amd64', (1, 18, 10), True),
    ('1.20.0', (1, 20, 0), False),
    ('alpha-1a2b3c4', None, False),       # unknown builds are not judged old
    ('', None, False),
    (None, None, False),
])
def test_core_versions(text, parsed, old):
    assert parse_core_version(text) == parsed
    assert core_too_old(text) is old


# ------------------------------------------------------------- generation

def test_without_a_domain_mapping_the_output_is_unchanged(repo, monkeypatch):
    from backend.converters.mihomo import generate_mihomo_config
    for body in (FEED.replace('hk1.example.org', '198.51.100.11'), OTHER):
        config = config_with(feeds=[(sub(), body)])
        text = generate_mihomo_config(config, preflight_providers=False)
        with monkeypatch.context() as patch:
            patch.setattr('backend.utils.proxy_server_dns.apply_node_dns', lambda *_a, **_k: None)
            assert generate_mihomo_config(config, preflight_providers=False) == text
        main = yaml.safe_load(text)
        assert POLICY_KEY not in main['dns'] and 'fake-ip-filter' not in main['dns']
        assert main['dns']['proxy-server-nameserver'] == ['https://223.5.5.5/dns-query']


def test_fake_ip_mapping_adds_a_policy_and_filters_only_the_target(repo):
    other = sub('other', '其他')
    config = config_with(feeds=[(sub(), FEED), (other, OTHER)])
    config['proxy_groups'][0]['subscriptions'] = ['air', 'other']
    main, report = generate(config)
    dns = main['dns']
    # Only the mapped node domain goes to the listener; the user's nameserver stays as written.
    assert dns[POLICY_KEY] == {'hk1.example.net': LISTENER}
    assert dns['proxy-server-nameserver'] == ['https://223.5.5.5/dns-query']
    # Absent key: keep Mihomo's own defaults, then only the mapping target.
    assert dns['fake-ip-filter'] == [*CONNECTIVITY_FILTER, 'hk1.example.org']
    # Windows' connectivity check as Windows spells it, not Mihomo's "msftnsci".
    assert dns['fake-ip-filter'][:2] == ['dns.msftncsi.com', 'www.msftncsi.com']
    assert 'rule-providers' not in main or all('node' not in name for name in main['rule-providers'])
    dns_report = report['dns']
    assert dns_report['applied'] is True and dns_report['listener'] == LISTENER and dns_report['fake_ip'] == 'blacklist'
    assert dns_report['policy'] == [{'domain': 'hk1.example.net', 'target': 'hk1.example.org', 'source': 'configflow'}]
    assert dns_report['filter'] == ['hk1.example.org'] and dns_report['targets_filtered'] == 1
    assert dns_report['mapped'] == 1 and dns_report['warnings'] == [] and dns_report['skipped'] == []
    assert dns_report['min_version'] == MIN_CORE_TEXT
    assert any(MIN_CORE_TEXT in note for note in dns_report['notes'])  # no device version known here


def test_several_targets_share_one_filter_entry_per_registrable_domain(repo):
    custom = FAKE_IP + "  fake-ip-filter: ['*.lan', '+.pool.ntp.org']\n"
    main, report = generate(config_with(custom, feeds=[(sub(), THREE)]))
    dns = main['dns']
    assert dns[POLICY_KEY] == {'jp-node.example.net': LISTENER, 'hk-node.example.net': LISTENER,
                               'de-node.example.net': LISTENER}
    assert dns['fake-ip-filter'] == ['*.lan', '+.pool.ntp.org', '+.example.org']
    assert report['dns']['filter'] == ['+.example.org'] and report['dns']['targets_filtered'] == 3
    assert report['dns']['mapped'] == 3 and len(report['dns']['policy']) == 3


def test_targets_already_in_the_filter_are_not_repeated(repo):
    custom = FAKE_IP + "  fake-ip-filter: ['*.lan', '+.example.org', 'geosite:private']\n"
    main, report = generate(config_with(custom))
    assert main['dns']['fake-ip-filter'] == ['*.lan', '+.example.org', 'geosite:private']
    assert main['dns'][POLICY_KEY] == {'hk1.example.net': LISTENER}
    assert report['dns']['filter'] == [] and report['dns']['targets_covered'] == 1


def test_rule_filter_mode_gets_real_ip_rules_in_front(repo):
    custom = FAKE_IP + "  fake-ip-filter-mode: rule\n  fake-ip-filter: ['MATCH,fake-ip']\n"
    main, report = generate(config_with(custom))
    assert main['dns']['fake-ip-filter'] == ['DOMAIN,hk1.example.org,real-ip', 'MATCH,fake-ip']
    assert report['dns']['fake_ip'] == 'rule'
    main, _ = generate(config_with(FAKE_IP + '  fake-ip-filter-mode: rule\n', feeds=[(sub(), THREE)]))
    assert main['dns']['fake-ip-filter'] == ['DOMAIN-SUFFIX,example.org,real-ip']


def test_whitelist_mode_keeps_its_list_and_skips_only_covered_targets(repo):
    custom = FAKE_IP + "  fake-ip-filter-mode: whitelist\n  fake-ip-filter: ['+.google.com']\n"
    main, report = generate(config_with(custom))
    assert main['dns']['fake-ip-filter'] == ['+.google.com']
    assert main['dns'][POLICY_KEY] == {'hk1.example.net': LISTENER}
    assert report['dns']['fake_ip'] == 'whitelist' and report['dns']['applied'] is True
    # A whitelist covering one target would hand that node a fake address: that mapping stays out.
    covering = FAKE_IP + "  fake-ip-filter-mode: whitelist\n  fake-ip-filter: ['jp-entry.example.org']\n"
    main, report = generate(config_with(covering, feeds=[(sub(), THREE)]))
    assert main['dns']['fake-ip-filter'] == ['jp-entry.example.org']
    assert set(main['dns'][POLICY_KEY]) == {'hk-node.example.net', 'de-node.example.net'}
    assert report['dns']['skipped'] == [{'domain': 'jp-node.example.net', 'target': 'jp-entry.example.org',
                                         'reason': 'whitelist_covers'}]
    assert 'whitelist' in report['dns']['warnings'][0]


def test_redir_host_adds_only_the_policy(repo):
    main, report = generate(config_with(FAKE_IP.replace('fake-ip', 'redir-host', 1)))
    assert main['dns'][POLICY_KEY] == {'hk1.example.net': LISTENER}
    assert 'fake-ip-filter' not in main['dns']
    assert report['dns']['fake_ip'] == 'not_needed' and report['dns']['applied'] is True and report['dns']['filter'] == []


def test_without_a_proxy_server_nameserver_nothing_is_added(repo):
    """Mihomo refuses a policy without proxy-server-nameserver; ConfigFlow does not invent one."""
    custom = FAKE_IP.replace('  proxy-server-nameserver: [https://223.5.5.5/dns-query]\n', '')
    main, report = generate(config_with(custom))
    assert main['dns'] == yaml.safe_load(custom)['dns']
    assert report['dns']['applied'] is False and 'proxy-server-nameserver' in report['dns']['warnings'][0]
    main, report = generate(config_with(custom + '  proxy-server-nameserver: []\n'))
    assert POLICY_KEY not in main['dns'] and report['dns']['applied'] is False


@pytest.mark.parametrize('custom,reason', [
    ('mode: rule\ndns: {enable: false, listen: 0.0.0.0:1053, proxy-server-nameserver: [1.1.1.1]}\n', 'DNS 没有开启'),
    ('mode: rule\n', 'DNS 没有开启'),
    ('mode: rule\ndns: {enable: true, enhanced-mode: fake-ip, proxy-server-nameserver: [1.1.1.1]}\n', '没有配置 dns.listen'),
    ('mode: rule\ndns: {enable: true, listen: "localhost:53", proxy-server-nameserver: [1.1.1.1]}\n', '无法推导'),
    ('mode: rule\ndns: {enable: true, listen: 0.0.0.0:1053, use-hosts: false, proxy-server-nameserver: [1.1.1.1]}\n',
     'use-hosts'),
    ('mode: rule\ndns: {enable: true, listen: 0.0.0.0:1053, enhanced-mode: fake-ip, fake-ip-filter: oops, '
     'proxy-server-nameserver: [1.1.1.1]}\n', '不是列表'),
    ('mode: rule\ndns: {enable: true, listen: 0.0.0.0:1053, proxy-server-nameserver: [1.1.1.1], '
     'proxy-server-nameserver-policy: [oops]}\n', '不是「域名: DNS」'),
])
def test_without_a_usable_listener_nothing_changes(repo, custom, reason):
    main, report = generate(config_with(custom))
    assert main.get('dns') == yaml.safe_load(custom).get('dns')
    assert report['dns']['applied'] is False and reason in report['dns']['warnings'][0]


def test_a_user_policy_for_the_same_domain_wins(repo):
    custom = FAKE_IP + ("  proxy-server-nameserver-policy:\n"
                        "    'geosite:cn': 223.5.5.5\n"
                        "    'hk-node.example.net': https://1.1.1.1/dns-query\n")
    main, report = generate(config_with(custom, feeds=[(sub(), THREE)]))
    policy = main['dns'][POLICY_KEY]
    # The user's entries first and unchanged, then ConfigFlow's for the other two.
    assert list(policy.items()) == [('geosite:cn', '223.5.5.5'), ('hk-node.example.net', 'https://1.1.1.1/dns-query'),
                                    ('jp-node.example.net', LISTENER), ('de-node.example.net', LISTENER)]
    dns_report = report['dns']
    assert dns_report['skipped'] == [{'domain': 'hk-node.example.net', 'target': 'hk-entry.example.org',
                                      'reason': 'policy_conflict'}]
    assert dns_report['warnings'][0] == ('你已为 hk-node.example.net 写了 proxy-server-nameserver-policy，'
                                         '保留你的设置；这条映射对该节点不生效')
    assert any('geosite' in note for note in dns_report['notes'])
    # Two targets left: they still collapse to one filter entry.
    assert main['dns']['fake-ip-filter'][-1] == '+.example.org' and dns_report['targets_filtered'] == 2


def test_a_user_wildcard_policy_counts_and_one_pointing_at_the_listener_is_kept(repo):
    wildcard = FAKE_IP + "  proxy-server-nameserver-policy: {'+.example.net': 'https://1.1.1.1/dns-query'}\n"
    main, report = generate(config_with(wildcard))
    assert main['dns'][POLICY_KEY] == {'+.example.net': 'https://1.1.1.1/dns-query'}
    assert 'fake-ip-filter' not in main['dns'] and report['dns']['applied'] is False
    assert report['dns']['warnings'][0].startswith('你写的 proxy-server-nameserver-policy 条目 +.example.net 覆盖了 hk1.example.net')
    own = FAKE_IP + "  proxy-server-nameserver-policy: {'hk1.example.net,x.test': '127.0.0.1:1053'}\n"
    main, report = generate(config_with(own))
    assert main['dns'][POLICY_KEY] == {'hk1.example.net,x.test': '127.0.0.1:1053'}
    assert main['dns']['fake-ip-filter'][-1] == 'hk1.example.org'
    assert report['dns']['policy'][0]['source'] == 'user' and report['dns']['warnings'] == []


def test_a_chain_ending_in_an_ip_needs_nothing(repo):
    custom = FAKE_IP + 'hosts:\n  "+.example.org": 198.51.100.40\n'
    main, report = generate(config_with(custom))
    assert main['hosts']['hk1.example.net'] == 'hk1.example.org'
    assert POLICY_KEY not in main['dns'] and 'dns' not in report


@pytest.mark.parametrize('version,applied', [('v1.19.19', False), ('v1.19.20', True), ('alpha-1a2b3c4', True)])
def test_a_known_old_core_gets_nothing(repo, version, applied):
    main, report = generate(config_with(), mihomo_core_version=version)
    assert (POLICY_KEY in main['dns']) is applied and ('fake-ip-filter' in main['dns']) is applied
    assert report['dns']['applied'] is applied
    if not applied:
        assert version in report['dns']['warnings'][0] and MIN_CORE_TEXT in report['dns']['warnings'][0]
    else:
        # A known version needs no reminder; an unknown one does.
        assert any(MIN_CORE_TEXT in note for note in report['dns']['notes']) is (version.startswith('alpha'))


def test_mapped_nodes_of_an_aggregation_and_inline_nodes_count(repo):
    feed = sub()
    store(feed, THREE)
    node = {'id': 'hk', 'name': 'HK', 'enabled': True, 'type': 'socks5', 'server': 'jp-node.example.net',
            'port': 1080, 'subscription_id': 'air'}
    aggregation = {'id': 'agg', 'name': 'Asia', 'enabled': True, 'subscriptions': ['air'], 'regex_filter': 'JP'}
    config = profile([feed], custom=FAKE_IP, aggregations=[aggregation], nodes=[node], groups=[
        {'id': 'g', 'name': 'Proxy', 'type': 'select', 'aggregations': ['agg'], 'manual_nodes': ['hk']}])
    main, _report = generate(config)
    # DE is filtered out of the aggregation: no hosts entry, no policy.
    assert set(main['dns'][POLICY_KEY]) == {'jp-node.example.net', 'hk-node.example.net'}


# ------------------------------------------------------------- devices

def _discovery(agent_id, version):
    from backend.agents.traffic_store import get_traffic_store
    get_traffic_store().add_report(agent_id, {'status': 'running', 'mihomo_version': version, 'items': []})


def test_reported_core_version_comes_from_domain_discovery_only(repo):
    agent = {'id': 'a1', 'service_type': 'mihomo', 'domain_discovery_enabled': True}
    assert reported_core_version(agent) is None
    _discovery('a1', 'v1.19.10')
    assert reported_core_version(agent) == 'v1.19.10'
    # A report left from before discovery was turned off may be stale.
    assert reported_core_version({**agent, 'domain_discovery_enabled': False}) is None
    assert reported_core_version({**agent, 'service_type': 'mosdns'}) is None


def test_publication_follows_the_device_core_version(content_env):
    from backend.routes import agents
    from backend.test_agent_content_preparation import BASE_URL, files
    config, remote, _fetches, _repository = content_env
    remote['provider'] = THREE
    store(config['subscriptions'][0], THREE)
    config['mihomo']['custom_config'] = FAKE_IP

    def deployed(agent):
        prepared = agents._prepare_transactional_content(agent, deepcopy(config), base_url=BASE_URL,
                                                         deployment_id='policy-check')
        return yaml.safe_load(files(prepared)['config.yaml'])

    agent = {'id': 'mihomo-agent', 'profile_id': 'default', 'service_type': 'mihomo'}
    assert len(deployed(agent)['dns'][POLICY_KEY]) == 3          # version unknown: added
    agent = {**agent, 'domain_discovery_enabled': True}
    _discovery('mihomo-agent', 'v1.19.12')
    document = deployed(agent)
    assert POLICY_KEY not in document['dns'] and 'fake-ip-filter' not in document['dns']
    assert document['hosts']['jp-node.example.net'] == 'jp-entry.example.org'
    _discovery('mihomo-agent', 'v1.19.33')
    assert len(deployed(agent)['dns'][POLICY_KEY]) == 3


def test_preview_names_agents_with_an_old_reported_core(repo, monkeypatch):
    from flask import Flask
    from backend.agents.manager import AgentManager
    from backend.routes import register_blueprints
    feed = sub()
    config = config_with(feeds=[(feed, FEED)])
    repo.update_shared_transaction(lambda shared: shared['subscriptions'].append(feed))
    saved = repo.get_profile('default')
    saved.update(proxy_groups=config['proxy_groups'], rule_configs=config['rule_configs'])
    saved.setdefault('mihomo', {})['custom_config'] = FAKE_IP
    repo.save_profile('default', saved)
    manager = AgentManager(repo)
    monkeypatch.setattr('backend.common.agent_manager.get_agent_manager', lambda: manager)
    old = manager.register_agent({'name': '旧内核', 'host': 'old.test', 'service_type': 'mihomo'})['id']
    new = manager.register_agent({'name': '新内核', 'host': 'new.test', 'service_type': 'mihomo'})['id']
    manager._update_agents(lambda rows: [row.update(domain_discovery_enabled=True) for row in rows])
    _discovery(old, 'v1.19.0')
    _discovery(new, 'v1.19.33')
    app = Flask(__name__)
    register_blueprints(app)
    body = app.test_client().post('/api/generate/mihomo/preview', json={}).get_json()
    assert yaml.safe_load(body['content'])['dns'][POLICY_KEY] == {'hk1.example.net': LISTENER}
    assert body['subscription_hosts']['dns']['outdated_agents'] == [{'name': '旧内核', 'version': 'v1.19.0'}]


# ------------------------------------------------------------- after refresh

def test_only_hosts_changes_invalidate_agent_content_checks(repo, monkeypatch):
    invalidate = Mock()
    monkeypatch.setattr('backend.agents.content_sync.invalidate_content_sync', invalidate)
    feed = sub()
    store(feed, OTHER)
    store(feed, OTHER.replace('us1.other-air.net', 'us2.other-air.net'))
    assert invalidate.call_count == 0  # node domains alone: providers tell the device
    store(feed, FEED)
    assert invalidate.call_count == 1
    store(feed, FEED.replace('  - {name: DE 01,', '  - {name: HK 03, type: socks5, server: hk1.example.net, port: 2}\n'
                                                  '  - {name: DE 01,'))
    assert invalidate.call_count == 1  # a new node on a mapped domain works without a publication


from backend.test_agent_content_preparation import content_env  # noqa: E402,F401
