"""Subscription nodes whose dialer-proxy names one of the airport's own groups."""
import pytest
import yaml

from backend.common import config as config_store
from backend.common.config_repository import ProfileRepository
from backend.converters.mihomo import generate_mihomo_config
from backend.utils.dialer_references import DialerReferenceError
from backend.utils.provider_delivery import DeliverySnapshot, prepare_subscription_provider
from backend.utils.sub_store_client import proxies_to_nodes
from backend.utils.subscription_cache import save_subscription_nodes
from backend.utils.subscription_dialers import adapt_subscription_proxies, extract_subscription_dialers

RELAY_KR = {'name': 'Relay-KR', 'type': 'gost-relay', 'server': '198.51.100.1', 'port': 2083, 'udp': True, 'tls': True}
RELAY_HK = {'name': 'Relay-HK-1', 'type': 'gost-relay', 'server': '198.51.100.2', 'port': 2083, 'tls': True}
PLAIN = {'name': '🇯🇵JP 日本 01', 'type': 'ss', 'server': 'jp.example.test', 'port': 443, 'cipher': 'aes-128-gcm', 'password': 'pw'}
KR = {'name': '🇰🇷 韩国 01', 'type': 'ss', 'server': 'kr.example.test', 'port': 18901, 'cipher': 'aes-128-gcm',
      'password': 'pw', 'dialer-proxy': 'Relay'}
HK = {'name': '🇭🇰 香港 02', 'type': 'ss', 'server': 'hk.example.test', 'port': 18902, 'cipher': 'aes-128-gcm',
      'password': 'pw', 'dialer-proxy': 'Relay-HK'}
AIRPORT = {
    'mixed-port': 7890,
    'proxies': [RELAY_KR, RELAY_HK, PLAIN, KR, HK],
    'proxy-groups': [
        {'name': 'Relay', 'type': 'select', 'proxies': ['Relay-KR'], 'hidden': True},
        {'name': 'Relay-HK', 'type': 'url-test', 'proxies': ['Relay-HK-1'], 'url': 'https://cp.example.test/204',
         'interval': 300, 'hidden': True},
        {'name': '节点选择', 'type': 'select', 'proxies': ['🇯🇵JP 日本 01', '🇰🇷 韩国 01']},
    ],
}


def test_extract_records_only_what_chained_nodes_need():
    record = extract_subscription_dialers(yaml.safe_dump(AIRPORT, allow_unicode=True))
    assert record == {'targets': {
        'Relay': {'type': 'select', 'members': ['Relay-KR']},
        'Relay-HK': {'type': 'url-test', 'members': ['Relay-HK-1'], 'url': 'https://cp.example.test/204', 'interval': 300},
    }, 'unresolved': []}
    assert extract_subscription_dialers(yaml.safe_dump({'proxies': [PLAIN]})) is None


def test_extract_expands_nested_groups_and_reports_dynamic_ones():
    document = {
        'proxies': [RELAY_KR, RELAY_HK, {**KR, 'dialer-proxy': 'Outer'}, {**HK, 'dialer-proxy': 'Dynamic'},
                    {**PLAIN, 'dialer-proxy': 'Relay-KR'}],
        'proxy-groups': [
            {'name': 'Outer', 'type': 'fallback', 'proxies': ['Inner', 'DIRECT', 'Relay-HK-1']},
            {'name': 'Inner', 'type': 'select', 'proxies': ['Relay-KR', 'Outer']},  # a cycle stays finite
            {'name': 'Dynamic', 'type': 'select', 'include-all': True},
        ],
    }
    record = extract_subscription_dialers(yaml.safe_dump(document, allow_unicode=True))
    # A proxy name is not the airport's group: the regular validation decides about it.
    assert record == {'targets': {'Outer': {'type': 'fallback', 'members': ['Relay-KR', 'Relay-HK-1']}},
                      'unresolved': ['Dynamic']}


@pytest.fixture
def profile(tmp_path):
    repository = ProfileRepository(tmp_path)
    repository.save_shared({'subscriptions': [{'id': 'air', 'name': 'Airport', 'url': 'https://air.example.test/sub',
                                               'enabled': True}],
                            'subscription_aggregations': [{'id': 'mix', 'name': 'Mix', 'subscriptions': ['air'],
                                                           'enabled': True}],
                            'nodes': [], 'rule_library': []})
    repository.save_profile('default', {
        'proxy_groups': [{'id': 'pick', 'name': 'Proxy', 'type': 'select', 'subscriptions': ['air']}],
        'rule_configs': [{'id': 'final', 'itemType': 'rule', 'rule_type': 'MATCH', 'value': '', 'policy': 'Proxy',
                          'enabled': True}],
    })
    config_store.set_repository(repository)
    yield repository
    config_store.reset_config_context()


def _cache(document=AIRPORT, *, record=True):
    nodes = proxies_to_nodes(document['proxies'])
    for index, node in enumerate(nodes):
        node.update(id=f'n{index}', subscription_id='air', subscription_name='Airport')
    metadata = {'url': 'https://air.example.test/sub', 'subscription_name': 'Airport'}
    if record:
        metadata['dialers'] = extract_subscription_dialers(yaml.safe_dump(document, allow_unicode=True))
    save_subscription_nodes('air', nodes, metadata)


def _generate(repository, **profile_changes):
    if profile_changes:
        repository.update_profile_transaction('default', lambda profile: profile.update(profile_changes))
    config = repository.get_compat_config('default')
    report = {}
    main = yaml.safe_load(generate_mihomo_config(config, preflight_providers=False, report=report))
    return config, main, report.get('subscription_dialers')


def _groups(main):
    return {group['name']: group for group in main['proxy-groups']}


def test_generation_selects_relays_from_the_provider_by_exact_name(profile):
    _cache()
    _config, main, report = _generate(profile)
    groups = _groups(main)
    assert groups['Airport/Relay'] == {'name': 'Airport/Relay', 'type': 'select', 'hidden': True,
                                      'use': ['Airport'], 'filter': r'^(?:Relay\-KR)$'}
    assert groups['Airport/Relay-HK']['type'] == 'url-test'
    assert groups['Airport/Relay-HK']['url'] == 'https://cp.example.test/204'
    assert not any(proxy['name'].startswith('Airport/') for proxy in main['proxies'])
    assert [item['group'] for item in report['groups']] == ['Airport/Relay', 'Airport/Relay-HK']
    assert report['skipped'] == []


def test_relays_removed_by_the_subscription_filter_are_copied(profile):
    _cache()
    shared = profile.get_shared()
    shared['subscriptions'][0]['regex_filter'] = '^(?!Relay)'
    profile.save_shared(shared)
    _config, main, report = _generate(profile)
    assert _groups(main)['Airport/Relay']['proxies'] == ['Airport/Relay-KR']
    copy = next(proxy for proxy in main['proxies'] if proxy['name'] == 'Airport/Relay-KR')
    assert copy['type'] == 'gost-relay' and copy['server'] == '198.51.100.1'
    assert all(item['copied'] for item in report['groups'])


def test_aggregation_only_delivery_copies_the_relays(profile):
    _cache()
    _config, main, _report = _generate(profile, proxy_groups=[
        {'id': 'pick', 'name': 'Proxy', 'type': 'select', 'aggregations': ['mix']}])
    assert 'Airport' not in main['proxy-providers']
    assert _groups(main)['Airport/Relay']['proxies'] == ['Airport/Relay-KR']


def test_delivery_points_chained_nodes_at_the_generated_groups(profile):
    _cache()
    config, main, _report = _generate(profile)
    snapshot = DeliverySnapshot.capture('default', main, profile=config)
    sub = config['subscriptions'][0]
    content = prepare_subscription_provider({**sub, 'url': ''}, config, snapshot)['content']
    delivered = {proxy['name']: proxy for proxy in yaml.safe_load(content)['proxies']}
    assert delivered['🇰🇷 韩国 01']['dialer-proxy'] == 'Airport/Relay'
    assert delivered['🇭🇰 香港 02']['dialer-proxy'] == 'Airport/Relay-HK'
    assert 'dialer-proxy' not in delivered['Relay-KR']


def test_a_cache_from_before_the_upgrade_uses_the_last_downloaded_body(profile):
    from backend.utils.subscription_refresh import _download_cache_path
    _cache(record=False)
    profile.write_shared_json(_download_cache_path('https://air.example.test/sub'),
                              {'text': yaml.safe_dump(AIRPORT, allow_unicode=True), 'fetched_at': '2026-11-03T00:00:00+00:00'})
    _config, main, report = _generate(profile)
    assert {'Airport/Relay', 'Airport/Relay-HK'} <= set(_groups(main))
    assert report['skipped'] == []


def test_without_any_record_publishing_says_what_to_refresh(profile):
    _cache(record=False)
    config, main, report = _generate(profile)
    assert report['skipped'] == [
        {'subscription': 'Airport', 'node': '🇰🇷 韩国 01', 'target': 'Relay', 'reason': 'refresh'},
        {'subscription': 'Airport', 'node': '🇭🇰 香港 02', 'target': 'Relay-HK', 'reason': 'refresh'}]
    snapshot = DeliverySnapshot.capture('default', main, profile=config)
    with pytest.raises(DialerReferenceError, match='「🇰🇷 韩国 01」的 dialer-proxy「Relay」.*刷新该订阅'):
        prepare_subscription_provider({**config['subscriptions'][0], 'url': ''}, config, snapshot)


def test_an_airport_group_that_cannot_be_provided_skips_only_its_nodes(profile):
    dynamic = {**AIRPORT, 'proxy-groups': [AIRPORT['proxy-groups'][0],
                                           {'name': 'Relay-HK', 'type': 'select', 'include-all': True}]}
    _cache(dynamic)
    config, main, report = _generate(profile)
    assert 'Airport/Relay-HK' not in _groups(main)
    assert report['skipped'] == [{'subscription': 'Airport', 'node': '🇭🇰 香港 02', 'target': 'Relay-HK',
                                  'reason': 'missing'}]
    snapshot = DeliverySnapshot.capture('default', main, profile=config)
    content = prepare_subscription_provider({**config['subscriptions'][0], 'url': ''}, config, snapshot)['content']
    delivered = {proxy['name']: proxy for proxy in yaml.safe_load(content)['proxies']}
    assert '🇭🇰 香港 02' not in delivered
    assert delivered['🇰🇷 韩国 01']['dialer-proxy'] == 'Airport/Relay'


def test_a_group_the_user_made_by_hand_keeps_working(profile):
    _cache()
    groups = [{'id': 'pick', 'name': 'Proxy', 'type': 'select', 'subscriptions': ['air']},
              {'id': 'relay', 'name': 'Relay', 'type': 'select', 'subscriptions': ['air'], 'regex': '^Relay-KR$'}]
    _config, main, report = _generate(profile, proxy_groups=groups)
    assert 'Airport/Relay' not in _groups(main) and 'Airport/Relay-HK' in _groups(main)
    record = extract_subscription_dialers(yaml.safe_dump(AIRPORT, allow_unicode=True))
    delivered = adapt_subscription_proxies('Airport', [KR, HK], main, record)
    assert [proxy['dialer-proxy'] for proxy in delivered] == ['Relay', 'Airport/Relay-HK']


def test_a_name_conflict_is_reported_instead_of_reusing_the_users_object(profile):
    _cache()
    groups = [{'id': 'pick', 'name': 'Proxy', 'type': 'select', 'subscriptions': ['air']},
              {'id': 'clash', 'name': 'Airport/Relay', 'type': 'select', 'subscriptions': ['air']}]
    with pytest.raises(DialerReferenceError, match='Airport/Relay'):
        _generate(profile, proxy_groups=groups)


def test_adapting_touches_only_airport_groups_and_is_stable():
    record = extract_subscription_dialers(yaml.safe_dump(AIRPORT, allow_unicode=True))
    main = {'proxies': [], 'proxy-groups': [{'name': 'Airport/Relay'}]}
    other = {**PLAIN, 'name': 'Cross', 'dialer-proxy': 'OtherFeedRelay'}
    once = adapt_subscription_proxies('Airport', [PLAIN, KR, HK, other], main, record)
    assert [proxy.get('dialer-proxy') for proxy in once] == [None, 'Airport/Relay', 'OtherFeedRelay']
    assert adapt_subscription_proxies('Airport', once, main, record) == once
    assert adapt_subscription_proxies('Airport', [PLAIN, KR, other], main, record, chains=False) == [PLAIN, other]
    assert adapt_subscription_proxies('Airport', [KR], main, None) == [KR]
