"""Independent OxiDNS strategy and compatibility regression tests."""
from copy import deepcopy
from hashlib import sha256
import re
import pytest
import yaml

from backend.common.oxidns_settings import (
    get_default_oxidns_settings, normalize_oxidns_settings, OxidnsSettingsError,
)
from backend.converters.oxidns import (
    generate_oxidns_config, get_oxidns_custom_files, get_oxidns_ruleset_downloads,
)
from backend.converters.mosdns import generate_mosdns_config
from backend.common.oxidns_validation import validate_native_config
from backend.common.rule_dns import route_key


def document(settings=None, rules=None):
    return yaml.safe_load(generate_oxidns_config({'oxidns': settings or {}, 'rule_configs': rules or []}))


def plugins(settings=None, rules=None):
    return {p['tag']: p for p in document(settings, rules)['plugins']}


def rule(identity, value, kind='DOMAIN-SUFFIX'):
    return {'id': identity, 'itemType': 'rule', 'rule_type': kind, 'value': value, 'enabled': True}


def test_verified_defaults_independent_from_mosdns():
    original = {'mosdns': {'cache_lazy_ttl': 21600}, 'oxidns': {}}
    before = deepcopy(original)
    oxi = yaml.safe_load(generate_oxidns_config(original))
    assert original == before
    oxicache = next(p for p in oxi['plugins'] if p['tag'] == 'lazy_cache')['args']
    assert oxicache == {'size': 10240, 'lazy_cache_ttl': 3600, 'short_circuit': True}
    assert oxi['runtime'] == {'worker_threads': 2}
    assert oxi['api'] == {'http': {'listen': '127.0.0.1:9199'}} and 'file' not in oxi['log']
    assert not any('query_summary' in str(p) for p in oxi['plugins'])
    mos = yaml.safe_load(generate_mosdns_config(original))
    assert next(p for p in mos['plugins'] if p['tag'] == 'lazy_cache')['args']['lazy_cache_ttl'] == 21600


def test_legacy_probe_helper_is_not_called_by_managed_routing():
    # Keep the helper for custom native pipelines, but never invoke it automatically.
    p = plugins({'direct_rules': ['ip']}, [rule('ip', '203.0.113.0/24', 'IP-CIDR')])
    assert p['local_probe_cache']['args'] == {'size': 2048, 'short_circuit': False}
    assert p['local_probe']['args'] == [
        {'exec': '$local_probe_cache'}, {'matches': ['!has_resp'], 'exec': '$forward_local'}, {'exec': 'return'}]
    main = p['sequence_main']['args']
    assert {'exec': 'jump local_probe'} not in main
    assert not any('resp_ip' in str(step) for step in main)
    assert main[-1] == {'exec': 'goto proxy_dns_seq'}
    assert p['ip_requery_proxy']['args'][0] == {'exec': 'drop_resp'}
    assert not any('$lazy_cache' in str(entry) for entry in p['proxy_dns_seq']['args'])


def test_aggregation_preserves_alternating_rule_priority_and_regex_commas():
    rules = [rule('a', 'example.test'), rule('b', 'x.example.test', 'DOMAIN'),
             rule('c', 'exception.example.test'), rule('d', r'^n[0-9]{1,3}\.test$', 'DOMAIN-REGEX'),
             rule('e', '203.0.113.0/24', 'IP-CIDR'), rule('f', '203.0.113.1/32', 'IP-CIDR')]
    p = plugins({'direct_rules': ['a', 'b', 'd', 'e'], 'proxy_rules': ['c', 'f']}, rules)
    assert p['rules_domain_1']['args']['exps'] == ['domain:example.test', 'full:x.example.test']
    assert p['rules_domain_2']['args']['exps'] == ['domain:exception.example.test']
    assert p['rules_domain_3']['args']['exps'] == [r'regexp:^n[0-9]{1,3}\.test$']
    domains = [entry['exec'] for entry in p['sequence_main']['args'] if str(entry.get('matches', [''])[0]).startswith('qname $rules_')]
    assert domains == ['goto china_dns', 'goto proxy_dns_seq', 'goto china_dns']
    assert not any(tag.startswith('rules_ip_') for tag in p)


def test_aggregation_can_be_disabled():
    p = plugins({'direct_rules': ['a', 'b'], 'aggregate_rules': False}, [rule('a', 'a.test'), rule('b', 'b.test')])
    assert 'rules_domain_2' in p


def test_download_paths_are_stable_ascii_and_names_cannot_escape():
    config = {'profile_id': 'home', 'system_config': {'rule_proxy_token': 'fixture-token'},
        'oxidns': {'direct_rulesets': ['a', 'b']},
        'rule_configs': [dict(id=i, name='../同名规则', itemType='ruleset', behavior='classical', url='https://rules.test/source') for i in ('a', 'b')]}
    downloads = get_oxidns_ruleset_downloads(config, base_url='https://config.test')
    assert len({d['local_path'] for d in downloads}) == 2
    assert all(re.fullmatch(r'./rules/rule_[a-f0-9]{16}_domain.txt', d['local_path']) for d in downloads)
    assert all('/profiles/home/mosdns/rule-proxy?' in d['url'] for d in downloads)
    assert [d['local_path'] for d in downloads] == [d['local_path'] for d in get_oxidns_ruleset_downloads(config)]
    p = yaml.safe_load(generate_oxidns_config(config))['plugins']
    assert all(len(item['tag']) <= 64 and item['tag'].isascii() for item in p)


def test_advanced_features_preserve_real_response_boundaries_and_runtime_state():
    s = get_default_oxidns_settings()
    for name in ('query_recorder', 'ip_selector', 'dynamic_domain_set'):
        s[name]['enabled'] = True
    s['dynamic_domain_set']['learn'] = True
    s['proxy_rules'] = ['ip']
    s['default_forward'] = 'forward_local'
    p = plugins(s, [rule('ip', '203.0.113.0/24', 'IP-CIDR')])
    assert p['china_dns']['args'][:2] == [{'exec': '$ip_selector'}, {'exec': '$lazy_cache'}]
    assert '$ip_selector' not in str(p['proxy_dns_seq']) + str(p['local_probe'])
    assert {'exec': '$learn_classified_domain'} in p['direct_ip']['args']
    assert {'exec': '$learn_classified_domain'} not in p['default_direct_ip']['args']
    assert p['sequence_main']['args'][-1] == {'exec': 'goto china_dns'}
    assert {'exec': 'jump local_probe'} not in p['sequence_main']['args']
    assert '$learn_classified_domain' not in str(p['china_dns']) + str(p['proxy_dns_seq'])
    assert get_oxidns_custom_files({'oxidns': s}) == []
    assert p['dynamic_domains']['args']['path'].startswith('./state/')
    assert p['query_recorder']['args']['retention_days'] == 7


def test_adguard_uses_question_for_dnstype_before_cache_and_keeps_raw_rules():
    content = '||ads.test^$dnstype=A\n@@||good.ads.test^'
    s = {'adguard': {'enabled': True, 'content': content}}
    p = plugins(s)
    assert {'matches': ['question $adguard_rules'], 'exec': 'goto adguard_reject'} in p['sequence_main']['args']
    assert p['adguard_block']['args']['mode'] == 'nxdomain'
    assert get_oxidns_custom_files({'oxidns': s}) == [{'path': './rules/adguard-custom.txt', 'content': content}]


def test_api_auth_log_rotation_and_custom_plugin_extension():
    s = {'api': {'enabled': True, 'listen': '0.0.0.0:9199', 'username': 'test', 'password': 'fixture', 'webui_root': './webui'},
         'log': {'file': './oxidns.log'}, 'custom_config': 'plugins:\n- tag: extra\n  type: sequence\n  args: []\n'}
    doc = document(s)
    assert doc['api']['http']['auth'] == {'type': 'basic', 'username': 'test', 'password': 'fixture'}
    assert doc['api']['http']['webui']['root'] == './webui'
    assert doc['log']['rotation'] == {'type': 'daily', 'max_files': 7}
    assert doc['plugins'][-1]['tag'] == 'extra'


@pytest.mark.parametrize('value', [
    {'worker_threads': 0}, {'worker_threads': True}, {'cache_size': -1},
    {'direct_rules': ['a'], 'proxy_rules': ['a']},
    {'api': {'enabled': True, 'username': 'alone'}},
    {'dynamic_domain_set': {'enabled': True, 'args': {'path': './rules/overwrite.txt'}}},
    {'query_recorder': {'enabled': True, 'args': {'retention_days': 0}}},
    {'adguard': {'enabled': True}},
    {'cache_dump_enabled': True, 'cache_dump_file': '../escape'},
    {'ros_route': {'enabled': True}},
    {'rule_update': {'enabled': True, 'args': {'schedule': '* *'}}},
])
def test_invalid_settings_rejected(value):
    with pytest.raises(OxidnsSettingsError):
        normalize_oxidns_settings(value)


@pytest.mark.parametrize('custom', [
    'include: [external.yaml]', 'plugins: {}', 'plugins: [{tag: 国内, type: sequence}]',
    'plugins: [{tag: sequence_main, type: sequence}]', 'plugins: [{tag: qs.exec.reserved, type: sequence}]',
    'plugins: [{tag: invalid..tag, type: sequence}]', 'plugins: [',
])
def test_invalid_or_nonportable_custom_config_rejected(custom):
    with pytest.raises(OxidnsSettingsError):
        document({'custom_config': custom})


@pytest.mark.parametrize('scheduled', [False, True])
def test_native_rule_update_lists_converted_downloads_and_targeted_providers(scheduled):
    config = {'system_config': {'rule_proxy_token': 'fixture-token'},
        'oxidns': {'rule_update': {'enabled': scheduled}, 'direct_rulesets': ['a']},
        'rule_configs': [{'id': 'a', 'name': 'test', 'itemType': 'ruleset', 'behavior': 'classical', 'url': 'https://rules.test/list'}]}
    doc = yaml.safe_load(generate_oxidns_config(config, 'https://config.test'))
    p = {item['tag']: item for item in doc['plugins']}
    assert len(p['rules_download']['args']['downloads']) == 1
    assert p['rules_reload']['args'] == ['$rules_domain_1']
    assert p['rules_refresh']['args'] == [{'exec': '$rules_download'}, {'exec': '$rules_reload'}]
    if scheduled:
        assert p['rules_cron']['args']['jobs'][0]['executors'] == ['$rules_refresh']
        assert p['rules_cron']['args']['jobs'][0]['schedule'] == '30 3 * * *'
    else:
        assert 'rules_cron' not in p


def test_default_manual_update_chain_supports_adguard_subscriptions_without_cron():
    p = plugins({'adguard': {'enabled': True, 'urls': ['https://rules.test/adguard.txt']}})
    assert p['rules_download']['args']['downloads'][0]['url'] == 'https://rules.test/adguard.txt'
    assert p['rules_reload']['args'] == ['$adguard_rules']
    assert p['rules_refresh']['args'] == [{'exec': '$rules_download'}, {'exec': '$rules_reload'}]
    assert 'rules_cron' not in p


@pytest.mark.parametrize('settings,rules', [
    ({}, []),
    ({'direct_rules': ['a']}, [rule('a', 'example.test')]),
    ({'adguard': {'enabled': True, 'content': '||ads.test^'}}, []),
    ({}, [{'id': 'unused', 'itemType': 'ruleset', 'url': 'https://rules.test/unused'}]),
])
def test_no_downloads_produce_no_empty_update_chain(settings, rules):
    p = plugins(settings, rules)
    assert not {'rules_download', 'rules_reload', 'rules_refresh', 'rules_cron'} & p.keys()
    with pytest.raises(OxidnsSettingsError, match='定时更新需要至少一个规则订阅'):
        plugins({**settings, 'rule_update': {'enabled': True}}, rules)


@pytest.mark.parametrize('args', [
    {'timeout': 'not-a-duration'}, {'timeout': False},
    {'socks5': 'missing-port'}, {'socks5': False},
    {'outbound': '   '}, {'outbound': []},
])
def test_manual_download_options_are_validated_with_scheduling_disabled(args):
    with pytest.raises(OxidnsSettingsError):
        plugins({'adguard': {'enabled': True, 'urls': ['https://rules.test/adguard.txt']},
                 'rule_update': {'enabled': False, 'args': args}})


def test_manual_download_keeps_zero_timeout_and_ignores_inactive_schedule_fields():
    p = plugins({'adguard': {'enabled': True, 'urls': ['https://rules.test/adguard.txt']},
                 'rule_update': {'enabled': False, 'args': {
                     'timeout': 0, 'socks5': '127.0.0.1:1080', 'outbound': '',
                     'schedule': 'unfinished', 'timezone': 'not-a-timezone'}}})
    assert p['rules_download']['args']['timeout'] == 0
    assert p['rules_download']['args']['socks5'] == '127.0.0.1:1080'
    assert 'outbound' not in p['rules_download']['args']
    assert 'rules_cron' not in p


@pytest.mark.parametrize('prefix', ['rules', 'custom'])
def test_custom_update_chain_is_preserved_without_adding_another_or_enabling_cron(prefix):
    settings = {'adguard': {'enabled': True, 'urls': ['https://rules.test/adguard.txt']}}
    path = get_oxidns_ruleset_downloads({'oxidns': settings})[0]['local_path']
    custom = [
        {'tag': f'{prefix}_download', 'type': 'download', 'args': {'timeout': '19s', 'downloads': [
            {'url': 'https://rules.test/adguard.txt', 'dir': './rules', 'filename': path.rsplit('/', 1)[-1]}]}},
        {'tag': f'{prefix}_reload', 'type': 'reload_provider', 'args': ['$adguard_rules']},
        {'tag': f'{prefix}_refresh', 'type': 'sequence', 'args': [
            {'exec': f'${prefix}_download'}, {'exec': f'${prefix}_reload'}]},
    ]
    settings['custom_config'] = yaml.safe_dump({'plugins': custom})
    p = plugins(settings)
    assert [item for item in p.values() if item['type'] in ('download', 'reload_provider')] == custom[:2]
    assert p[f'{prefix}_refresh'] == custom[2]
    assert 'rules_cron' not in p
    if prefix != 'rules':
        assert not {'rules_download', 'rules_reload', 'rules_refresh'} & p.keys()
    with pytest.raises(OxidnsSettingsError, match='已有自定义下载或重载插件'):
        plugins({**settings, 'rule_update': {'enabled': True}})


@pytest.mark.parametrize('custom', [
    {'tag': 'custom_download', 'type': 'download', 'args': {'downloads': [
        {'url': 'https://rules.test/custom.txt', 'dir': './rules', 'filename': 'custom.txt'}]}},
    {'tag': 'custom_reload', 'type': 'reload_provider', 'args': ['$adguard_rules']},
    {'tag': 'rules_refresh', 'type': 'sequence', 'args': [{'exec': 'accept'}]},
])
def test_custom_maintenance_executors_do_not_receive_an_implicit_standard_chain(custom):
    settings = {'adguard': {'enabled': True, 'urls': ['https://rules.test/adguard.txt']},
                'custom_config': yaml.safe_dump({'plugins': [custom]})}
    p = plugins(settings)
    assert p[custom['tag']] == custom
    assert not ({'rules_download', 'rules_reload', 'rules_refresh', 'rules_cron'} - {custom['tag']}) & p.keys()
    with pytest.raises(OxidnsSettingsError, match='已有自定义下载或重载插件'):
        plugins({**settings, 'rule_update': {'enabled': True}})


def native_rule_fixture():
    config = {'system_config': {'server_domain': 'https://config.test', 'rule_proxy_token': 'fixture-token'},
              'rule_configs': [{'id': 'a', 'itemType': 'ruleset', 'behavior': 'domain', 'url': 'https://rules.test/list'}],
              'oxidns': {'direct_rulesets': ['a'], 'rule_update': {'enabled': False}}}
    path = get_oxidns_ruleset_downloads(config)[0]['local_path']
    native = {'runtime': {'worker_threads': 3}, 'future': {'keep': [False, None, 0]}, 'plugins': [
        {'tag': 'domains', 'type': 'domain_set', 'args': {'files': [path]}}]}
    config['oxidns'].update(config_mode='native', native_config='# saved native draft\n' + yaml.safe_dump(native))
    return config, native


def include_native_plugins(config, native):
    config['oxidns']['additional_files'] = [{'path': 'plugins.yaml', 'encoding': 'text',
                                            'content': yaml.safe_dump({'plugins': native['plugins']})}]
    config['oxidns']['native_config'] = yaml.safe_dump({**native, 'include': ['plugins.yaml'], 'plugins': []})


@pytest.mark.parametrize('included', [False, True])
def test_native_generation_adds_manual_capability_without_changing_saved_draft(included):
    config, native = native_rule_fixture()
    if included:
        include_native_plugins(config, native)
    before = deepcopy(config)
    rendered = generate_oxidns_config(config)
    generated = yaml.safe_load(rendered)
    p = {item['tag']: item for item in generated['plugins']}
    assert set(p) == {'domains', 'rules_download', 'rules_reload', 'rules_refresh'}
    assert p['domains'] == native['plugins'][0]
    assert p['rules_reload']['args'] == ['$domains']
    assert p['rules_refresh']['args'] == [{'exec': '$rules_download'}, {'exec': '$rules_reload'}]
    assert len(p['rules_download']['args']['downloads']) == 1
    assert generated['runtime'] == native['runtime'] and generated['future'] == native['future']
    assert config == before
    assert generate_oxidns_config(config) == rendered
    deployed = deepcopy(config)
    deployed['oxidns']['native_config'] = rendered
    assert generate_oxidns_config(deployed) == rendered


@pytest.mark.parametrize('timeout', ['17s', 'unfinished'])
def test_native_generation_restores_and_validates_parked_download_options(timeout):
    config, _ = native_rule_fixture()
    config['oxidns']['_common_disabled'] = {
        'rules_download': {'tag': 'rules_download', 'type': 'download', 'args': {'timeout': timeout, 'downloads': []}},
        'rules_reload': {'tag': 'rules_reload', 'type': 'reload_provider', 'args': ['$withdrawn', '$domains']},
        'rules_refresh': {'tag': 'rules_refresh', 'type': 'sequence', 'args': []},
        'rules_cron': {'tag': 'rules_cron', 'type': 'cron', 'args': {'jobs': []}},
    }
    before = deepcopy(config)
    if timeout == 'unfinished':
        with pytest.raises(OxidnsSettingsError, match='timeout'):
            generate_oxidns_config(config)
    else:
        p = {item['tag']: item for item in yaml.safe_load(generate_oxidns_config(config))['plugins']}
        assert p['rules_download']['args']['timeout'] == timeout
        assert len(p['rules_download']['args']['downloads']) == 1
        assert p['rules_reload']['args'] == ['$domains']
        assert 'rules_cron' not in p
    assert config == before


@pytest.mark.parametrize('included', [False, True])
@pytest.mark.parametrize('prefix', ['rules', 'custom'])
def test_native_generation_keeps_existing_update_chain_without_adding_another(prefix, included):
    config, native = native_rule_fixture()
    native['plugins'].extend([
        {'tag': f'{prefix}_download', 'type': 'download', 'args': {'downloads': [
            {'url': 'https://rules.test/list', 'dir': 'rules', 'filename': 'custom.txt'}]}},
        {'tag': f'{prefix}_reload', 'type': 'reload_provider', 'args': ['$domains']},
        {'tag': f'{prefix}_refresh', 'type': 'sequence', 'args': [
            {'exec': f'${prefix}_download'}, {'exec': f'${prefix}_reload'}]},
    ])
    config['oxidns']['native_config'] = yaml.safe_dump(native)
    if included:
        include_native_plugins(config, native)
    before = deepcopy(config)
    assert yaml.safe_load(generate_oxidns_config(config)) == native
    assert config == before


@pytest.mark.parametrize('tag', ['rules_download', 'rules_reload', 'rules_refresh'])
def test_native_generation_does_not_claim_existing_standard_tags(tag):
    config, native = native_rule_fixture()
    native['plugins'].append({'tag': tag, 'type': 'sequence', 'args': [{'exec': 'return'}]})
    include_native_plugins(config, native)
    before = deepcopy(config)
    assert yaml.safe_load(generate_oxidns_config(config)) == native
    assert config == before


@pytest.mark.parametrize('unmanaged_file', [False, True])
def test_native_generation_without_referenced_downloads_does_not_add_a_chain(unmanaged_file):
    config, native = native_rule_fixture()
    native['plugins'][0]['args'] = {'files': ['rules/private.txt']} if unmanaged_file else {'exps': ['domain:example.test']}
    config['oxidns']['native_config'] = yaml.safe_dump(native)
    if unmanaged_file:
        config['oxidns']['additional_files'] = [{'path': 'rules/private.txt', 'encoding': 'text', 'content': 'domain:example.test\n'}]
    before = deepcopy(config)
    assert yaml.safe_load(generate_oxidns_config(config)) == native
    assert config == before


def test_explicit_domain_rule_wins_over_old_learned_classification():
    p = plugins({'direct_rules': ['explicit'], 'dynamic_domain_set': {'enabled': True, 'route': 'proxy'}},
                [rule('explicit', 'business.test')])
    main = p['sequence_main']['args']
    explicit = {'matches': ['qname $rules_domain_1'], 'exec': 'goto china_dns'}
    learned = {'matches': ['qname $dynamic_domains'], 'exec': 'goto proxy_dns_seq'}
    assert main.index(explicit) < main.index(learned)


@pytest.mark.parametrize('upstream', ['- addr: [', '- wrong: 1', '[false]'])
def test_invalid_upstream_yaml_is_not_silently_dropped(upstream):
    with pytest.raises(OxidnsSettingsError):
        document({'local_dns': upstream})


def test_empty_fallback_disables_backup_without_inheriting_local_upstreams():
    local = '- addr: udp://192.0.2.10:5353\n- addr: tls://dns.test\n  dial_addr: 192.0.2.11\n'
    p = plugins({'local_dns': local})
    assert not {'forward_fallback', 'fallback_dns', 'fallback'} & p.keys()
    assert p['proxy_dns_seq']['args'] == [{'exec': 'goto remote_dns'}]
    assert p['forward_local']['args']['upstreams'][1]['dial_addr'] == '192.0.2.11'
    override = plugins({'local_dns': local, 'fallback_dns': 'udp://192.0.2.12'})
    assert override['forward_fallback']['args']['upstreams'] == [{'addr': 'udp://192.0.2.12'}]


def test_default_cache_dump_lives_in_unmanaged_state_directory():
    p = plugins({'cache_dump_enabled': True})
    assert p['lazy_cache']['args']['dump_file'] == './state/cache.dump'
    assert get_oxidns_custom_files({'oxidns': {'cache_dump_enabled': True}}) == []


@pytest.mark.parametrize('lazy_ttl', [0, 1, 300])
def test_lazy_cache_ttl_zero_disables_lazy_without_disabling_cache(lazy_ttl):
    p = plugins({'cache_enabled': True, 'cache_lazy_ttl': lazy_ttl,
                 'cache_size': 512, 'cache_dump_enabled': True})
    cache = p['lazy_cache']['args']
    assert cache['size'] == 512
    assert cache['short_circuit'] is True
    assert cache['dump_file'] == './state/cache.dump'
    assert cache['dump_interval'] == get_default_oxidns_settings()['cache_dump_interval']
    assert {'exec': '$lazy_cache'} in p['china_dns']['args']
    if lazy_ttl == 0:
        assert 'lazy_cache_ttl' not in cache
    else:
        assert cache['lazy_cache_ttl'] == lazy_ttl


@pytest.mark.parametrize('content', [
    'router.test 192.0.2.10',
    'router.test 2001:db8::10',
    'full:router.test 192.0.2.10 192.0.2.11 2001:db8::10',
    'domain:service.test 2001:db8::1 192.0.2.12 2001:db8::2',
    'keyword:router 192.0.2.1 192.0.2.1',
    r'regexp:^api[0-9]{1,3}\.test$ 192.0.2.10 2001:db8::10',
    '# comment\n\n  full:router.test\t192.0.2.10\t2001:DB8::10 # dual stack\n',
    'full:router.test 192.0.2.1\r\nfull:router.test 192.0.2.2\r\n',
    '# temporarily no hosts\n',
])
def test_native_hosts_preserves_ipv6_all_addresses_rule_order_and_comments(content):
    config = {'oxidns': {'custom_hosts': content}}
    before = deepcopy(config)
    files = get_oxidns_custom_files(config)
    assert files == [{'path': './rules/custom_hosts.txt', 'content': content}]
    p = plugins(config['oxidns'])
    assert p['custom_hosts']['args']['files'] == [files[0]['path']]
    assert p['sequence_main']['args'].index({'exec': '$custom_hosts'}) < p['sequence_main']['args'].index({'exec': 'goto proxy_dns_seq'})
    assert config == before


@pytest.mark.parametrize(('content', 'expected'), [
    ('192.0.2.10 router.test', 'router.test 192.0.2.10'),
    ('2001:db8::10 router.test', 'router.test 2001:db8::10'),
    ('192.0.2.10 router.test alias.test # aliases\n',
     'router.test 192.0.2.10 # aliases\nalias.test 192.0.2.10\n'),
    ('2001:db8::10 router.test alias.test\r\n',
     'router.test 2001:db8::10\r\nalias.test 2001:db8::10\r\n'),
    ('192.0.2.10 domain:router.test', 'domain:router.test 192.0.2.10'),
    ('192.0.2.10 router.test alias.test', 'router.test 192.0.2.10\nalias.test 192.0.2.10'),
    ('router.test 192.0.2.1 2001:db8::1\n2001:db8::2 legacy.test',
     'router.test 192.0.2.1 2001:db8::1\nlegacy.test 2001:db8::2'),
])
def test_legacy_hosts_ip_first_rows_and_aliases_convert_without_truncation(content, expected):
    config = {'oxidns': {'custom_hosts': content}}
    assert get_oxidns_custom_files(config) == [{'path': './rules/custom_hosts.txt', 'content': expected}]
    document(config['oxidns'])


@pytest.mark.parametrize('invalid', [
    'router.test',
    'router.test not-an-address',
    'router.test 192.0.2.1 bad-address',
    'router.test 999.999.999.999',
    'router.test [2001:db8::1]',
    'router.test fe80::1%eth0',
    'router.test 192.0.2.1,2001:db8::1',
    '192.0.2.1 router.test 2001:db8::1',
    '192.0.2.1 999.999.999.999',
    '2001:db8::1 https://router.test',
    '192.0.2.1 full:',
])
def test_invalid_or_ambiguous_hosts_fail_in_preview_and_download_instead_of_dropping_data(invalid):
    config = {'oxidns': {'custom_hosts': '# first line\n' + invalid}}
    for render in (generate_oxidns_config, get_oxidns_custom_files):
        with pytest.raises(OxidnsSettingsError, match='Hosts 第 2 行'):
            render(config)


# Golden output for managed domain-only routing; empty action metadata must not
# alter the generated document. Legacy IP choices remain inert.
def golden_rules():
    return [
        rule('d1', 'example.test'), rule('d2', 'x.example.test', 'DOMAIN'), rule('d3', 'ads', 'DOMAIN-KEYWORD'),
        rule('i1', '203.0.113.0/24', 'IP-CIDR'), rule('i2', '2001:db8::/32', 'IP-CIDR6'),
        rule('g1', 'CN', 'GEOIP'), dict(rule('off', 'off.test'), enabled=False),
        {'id': 's1', 'name': 'cn', 'itemType': 'ruleset', 'behavior': 'domain', 'url': 'https://rules.test/cn'},
        {'id': 's2', 'name': 'mixed', 'itemType': 'ruleset', 'behavior': 'classical', 'url': '/api/rule-library/content/s2'},
        {'id': 's3', 'name': 'ips', 'itemType': 'ruleset', 'behavior': 'ipcidr', 'url': 'https://rules.test/ip'},
    ]


GOLDEN_ROUTING = {'direct_rules': ['d1', 'd3', 'i1', 'off'], 'proxy_rules': ['d2', 'i2'],
                  'direct_rulesets': ['s1'], 'proxy_rulesets': ['s2', 's3'],
                  'custom_matches': [{'matches': 'qname domain:corp.test', 'exec': 'goto china_dns'}],
                  'custom_match_position': 'head', 'custom_hosts': 'full:nas.test 192.168.1.2', 'block_qtypes': [65]}
GOLDEN_CASES = {
    'defaults': ({}, '64d578c5b847e15c1b8cb6da114a16eac3855b3f139fbd42bdcb0ab047581b71'),
    'routing': (GOLDEN_ROUTING, 'd9f02ffdac1c906fbafc55a22891ddab538421bbd189f5c31c0334b8902bbb2d'),
    'features': ({**GOLDEN_ROUTING, 'aggregate_rules': False, 'default_forward': 'forward_local', 'cache_lazy_ttl': 0,
                  'custom_match_position': 'tail', 'fallback_dns': 'udp://9.9.9.9', 'remote_ttl': 60,
                  'local_dns': 'udp://10.0.0.1\nudp://10.0.0.2',
                  'remote_dns': '- addr: https://1.1.1.1/dns-query\n  bootstrap: 223.5.5.5\n',
                  'ip_selector': {'enabled': True},
                  'ros_address_list': {'enabled': True, 'args': {'address': '192.168.88.1', 'username': 'u', 'password': 'p'}},
                  'dynamic_domain_set': {'enabled': True, 'learn': True, 'route': 'proxy'},
                  'query_recorder': {'enabled': True}, 'adguard': {'enabled': True, 'urls': ['https://rules.test/adguard.txt']},
                  'api': {'enabled': True}, 'log': {'file': './oxidns.log'}, 'rule_update': {'enabled': True},
                  'device_policies': [{'ip': '192.168.1.10', 'name': 'pc', 'mode': 'direct'}]},
                 '3e7c61ee57c8b316a4db474a0cd4816925f711cea7a847d4c73a0ef4cec1f6d3'),
    'minimal': ({**GOLDEN_ROUTING, 'cache_enabled': False, 'probe_cache_enabled': False, 'prefer_ipv4': False,
                 'metrics_enabled': False, 'tcp_enabled': False, 'remote_ttl': 0, 'block_qtypes': [],
                 'ros_route': {'enabled': True, 'args': {'address': '192.168.88.1', 'username': 'u', 'password': 'p',
                                                         'routing_table': 'proxy', 'gateway4': '10.0.0.1'}}},
                'af157b644aa945bfa93cdf935b787c8d6ffdc227051a6dfad420ef8d11e94cfc'),
}


def golden_config(settings):
    return {'system_config': {'rule_proxy_token': 'fixture-token', 'server_domain': 'https://config.test'},
            'oxidns': deepcopy(settings), 'rule_configs': golden_rules()}


@pytest.mark.parametrize('name', GOLDEN_CASES)
@pytest.mark.parametrize('extra', [{}, {'rule_actions': {}, 'rule_dns_follow': [], 'rule_dns_parked': {}},
                                   {'rule_actions': {'gone': None}, 'rule_dns_follow': ['d1', 's2']}])
def test_output_byte_identical_without_rule_dns_fields(name, extra):
    settings, digest = GOLDEN_CASES[name]
    output = generate_oxidns_config(golden_config({**settings, **extra}), 'https://fallback.test')
    assert sha256(output.encode()).hexdigest() == digest


PINNED = {'action': 'upstream', 'source': 'local', 'upstream': {'addr': 'udp://10.0.0.53:53'}}
PINNED_KEY = route_key(PINNED)[9:]
ACTION_EXEC = {'refused': 'reject', 'nxdomain': 'reject 3', 'nodata': 'reject 0', 'blackhole': '$cf_dns_blackhole'}


def main_execs(p, kind):
    return [(entry['matches'][0].split('$')[1], entry['exec']) for entry in p['sequence_main']['args']
            if str((entry.get('matches') or [''])[0]).startswith(kind + ' $rules_')]


@pytest.mark.parametrize(('action', 'expected'), [*ACTION_EXEC.items(), ('upstream', f'goto cf_dns_up_{PINNED_KEY}')])
@pytest.mark.parametrize(('kind', 'value', 'match'), [('DOMAIN-SUFFIX', 'a.test', 'qname'),
                                                      ('IP-CIDR', '203.0.113.0/24', 'resp_ip'),
                                                      ('IP-CIDR6', '2001:db8::/32', 'resp_ip')])
def test_template_emits_exec_for_each_action_domain_and_ip(action, expected, kind, value, match):
    stored = PINNED if action == 'upstream' else {'action': action}
    p = plugins({'rule_actions': {'a': stored}}, [rule('a', value, kind)])
    part = 'domain' if match == 'qname' else 'ip'
    assert main_execs(p, match) == ([(f'rules_{part}_1', expected)] if part == 'domain' else [])
    assert {'exec': 'jump local_probe'} not in p['sequence_main']['args']


def test_rule_actions_take_precedence_over_lists():
    rules = [rule('a', 'a.test'), rule('b', 'b.test'), rule('c', '203.0.113.0/24', 'IP-CIDR')]
    p = plugins({'direct_rules': ['a', 'c'], 'proxy_rules': ['b'],
                 'rule_actions': {'a': {'action': 'nxdomain'}, 'c': {'action': 'refused'}}}, rules)
    assert main_execs(p, 'qname') == [('rules_domain_1', 'reject 3'), ('rules_domain_2', 'goto proxy_dns_seq')]
    assert main_execs(p, 'resp_ip') == []
    assert p['rules_domain_1']['args'] == {'exps': ['domain:a.test']}


def test_union_catalog_lists_with_actions_render_in_template():
    # Native writes keep a union catalog in the lists; it flows into templates.
    rules = [rule('a', 'a.test'), rule('b', 'b.test'), rule('c', 'c.test')]
    settings = {'direct_rules': ['a', 'b', 'c'], 'proxy_rules': [],
                'rule_actions': {'b': {'action': 'blackhole'}}, 'rule_dns_follow': ['a']}
    p = plugins(settings, rules)
    assert main_execs(p, 'qname') == [('rules_domain_1', 'goto china_dns'), ('rules_domain_2', '$cf_dns_blackhole'),
                                      ('rules_domain_3', 'goto china_dns')]


def test_blackhole_helper_mode_is_quoted_null_string():
    text = generate_oxidns_config({'oxidns': {'rule_actions': {'a': {'action': 'blackhole'}}}, 'rule_configs': [rule('a', 'a.test')]})
    assert "- tag: cf_dns_blackhole\n  type: black_hole\n  args:\n    mode: 'null'\n    short_circuit: true\n" in text
    helper = next(item for item in yaml.safe_load(text)['plugins'] if item['tag'] == 'cf_dns_blackhole')
    assert helper['args'] == {'mode': 'null', 'short_circuit': True}


def test_upstream_helper_chain_copies_entry_and_follows_features():
    remote = {'action': 'upstream', 'source': 'remote', 'upstream': {'addr': 'tls://dns.test', 'socks5': '127.0.0.1:1080'}}
    rules = [rule('a', 'a.test'), rule('b', 'b.test')]
    settings = {'rule_actions': {'a': PINNED, 'b': remote}, 'remote_ttl': 30, 'cache_lazy_ttl': 0,
                'ros_address_list': {'enabled': True, 'args': {'address': '192.0.2.1', 'username': 'u', 'password': 'p'}}}
    p = plugins(settings, rules)
    local_key, remote_key = PINNED_KEY, route_key(remote)[9:]
    assert p[f'cf_dns_fwd_{remote_key}']['args'] == {'upstreams': [remote['upstream']]}
    assert p[f'cf_dns_cache_{local_key}']['args'] == {'size': 1024, 'short_circuit': True}
    assert p[f'cf_dns_up_{local_key}']['args'][:3] == [
        {'exec': 'drop_resp'}, {'exec': '$ros_address_list'}, {'exec': f'$cf_dns_cache_{local_key}'}]
    assert p['china_dns']['args'][0] == {'exec': '$ros_address_list'}
    assert p[f'cf_dns_up_{remote_key}']['args'][-2:] == [{'exec': 'ttl 30'}, {'exec': 'accept'}]
    assert '$ros_address_list' not in str(p[f'cf_dns_up_{remote_key}'])
    no_cache = plugins({**settings, 'cache_enabled': False}, rules)
    assert f'cf_dns_cache_{local_key}' not in no_cache and f'$cf_dns_cache_{local_key}' not in str(no_cache)


def test_helpers_emitted_only_when_used_before_sequence_main():
    rules = [rule('a', 'a.test'), rule('b', 'b.test'), dict(rule('c', 'c.test'), enabled=False)]
    unused = plugins({'rule_actions': {'a': {'action': 'nodata'}, 'c': {'action': 'blackhole'}}}, rules)
    assert not any(tag.startswith('cf_dns_') for tag in unused)
    order = list(plugins({'rule_actions': {'a': PINNED, 'b': {'action': 'blackhole'}}}, rules))
    helpers = [tag for tag in order if tag.startswith('cf_dns_')]
    assert helpers == [f'cf_dns_fwd_{PINNED_KEY}', f'cf_dns_cache_{PINNED_KEY}', f'cf_dns_up_{PINNED_KEY}', 'cf_dns_blackhole']
    assert order.index('ip_requery_proxy') < order.index(helpers[0])
    assert order.index(helpers[-1]) + 1 == order.index('sequence_main')


def test_user_plugin_named_like_a_helper_is_a_duplicate_tag():
    custom = 'plugins:\n- tag: cf_dns_blackhole\n  type: black_hole\n  args: {mode: nxdomain}\n'
    with pytest.raises(OxidnsSettingsError, match='tag 重复：cf_dns_blackhole'):
        document({'custom_config': custom, 'rule_actions': {'a': {'action': 'blackhole'}}}, [rule('a', 'a.test')])


def test_adjacent_same_action_merges_only_with_aggregate_rules():
    other = {**PINNED, 'upstream': {'addr': 'udp://10.0.0.54'}}
    rules = [rule(name, f'{name}.test') for name in 'abcdef']
    actions = {'a': {'action': 'refused'}, 'b': {'action': 'refused'}, 'c': PINNED, 'd': PINNED, 'e': other,
               'f': {'action': 'nxdomain'}}
    p = plugins({'rule_actions': actions}, rules)
    assert main_execs(p, 'qname') == [('rules_domain_1', 'reject'), ('rules_domain_2', f'goto cf_dns_up_{PINNED_KEY}'),
                                      ('rules_domain_3', f'goto cf_dns_up_{route_key(other)[9:]}'),
                                      ('rules_domain_4', 'reject 3')]
    assert p['rules_domain_2']['args'] == {'exps': ['domain:c.test', 'domain:d.test']}
    separate = plugins({'rule_actions': actions, 'aggregate_rules': False}, rules)
    assert len(main_execs(separate, 'qname')) == 6


def test_actions_keep_rule_order_and_section_positions():
    rules = [rule('a', 'a.test'), rule('b', '203.0.113.0/24', 'IP-CIDR'), rule('c', 'c.test'), rule('d', 'd.test'),
             rule('e', '198.51.100.0/24', 'IP-CIDR')]
    settings = {'direct_rules': ['c'], 'proxy_rules': ['e'], 'custom_match_position': 'tail',
                'custom_matches': [{'matches': 'qname domain:corp.test', 'exec': 'reject 3'}],
                'dynamic_domain_set': {'enabled': True},
                'rule_actions': {'a': {'action': 'blackhole'}, 'b': {'action': 'nxdomain'}, 'd': {'action': 'refused'}}}
    main = plugins(settings, rules)['sequence_main']['args']
    execs = [entry['exec'] for entry in main[main.index({'exec': 'prefer_ipv4'}) + 2:]]
    assert execs == ['$cf_dns_blackhole', 'goto china_dns', 'reject', 'reject 3', 'goto china_dns',
                     'goto proxy_dns_seq']
    assert main[-2]['matches'] == ['qname $dynamic_domains']


def test_action_on_unconvertible_rule_is_rejected():
    with pytest.raises(OxidnsSettingsError, match='GEOIP'):
        document({'rule_actions': {'g': {'action': 'refused'}}}, [rule('g', 'CN', 'GEOIP')])


def test_action_only_ruleset_is_downloaded_and_reloaded():
    config = {'system_config': {'rule_proxy_token': 'fixture-token'},
              'oxidns': {'rule_actions': {'rs1': {'action': 'blackhole'}}, 'rule_update': {'enabled': True}},
              'rule_configs': [{'id': 'rs1', 'name': 'ads', 'itemType': 'ruleset', 'behavior': 'domain',
                                'url': 'https://rules.test/ads.txt'}]}
    downloads = get_oxidns_ruleset_downloads(config, 'https://config.test')
    assert [item['local_path'] for item in downloads] == ['./rules/rule_8c4bba9e7c828a1b_domain.txt']
    p = {item['tag']: item for item in yaml.safe_load(generate_oxidns_config(config, 'https://config.test'))['plugins']}
    assert p['rules_reload']['args'] == ['$rules_domain_1']
    assert p['rules_domain_1']['args'] == {'files': ['./rules/rule_8c4bba9e7c828a1b_domain.txt']}
    assert len(p['rules_download']['args']['downloads']) == 1


def test_generated_actions_pass_configflow_validator():
    remote = {'action': 'upstream', 'source': 'remote', 'upstream': {'addr': 'udp://127.0.0.1:15403'}}
    rules = [rule('a', 'a.test'), rule('b', 'b.test'), rule('c', 'c.test'), rule('d', 'd.test'), rule('e', 'e.test'),
             rule('f', 'f.test'), rule('g', '203.0.113.0/24', 'IP-CIDR'), rule('h', '198.51.100.0/24', 'IP-CIDR'),
             {'id': 'mixed', 'name': 'mixed', 'itemType': 'ruleset', 'behavior': 'classical', 'url': 'https://rules.test/m'}]
    settings = {'direct_rules': ['a'], 'ip_selector': {'enabled': True}, 'rule_update': {'enabled': True},
                'rule_actions': {'b': {'action': 'refused'}, 'c': {'action': 'nxdomain'}, 'd': {'action': 'nodata'},
                                 'e': {'action': 'blackhole'}, 'f': PINNED, 'g': PINNED, 'h': remote,
                                 'mixed': {'action': 'blackhole'}}}
    result = generate_oxidns_config({'system_config': {'rule_proxy_token': 'fixture-token'},
                                     'oxidns': settings, 'rule_configs': rules}, 'https://config.test')
    assert validate_native_config(yaml.safe_load(result)) == []
