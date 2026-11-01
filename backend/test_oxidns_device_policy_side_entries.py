"""Device policies with side DNS listeners: node-test entries and other isolated chains.

Only listeners on a client DNS port must enter sequence_main. Others may keep
their own chain; they are reported, except ConfigFlow's node-test listeners,
and the policy hook never touches them.
"""
from copy import deepcopy

import pytest
import yaml

from backend.agents.node_test_dns import _plugins as managed_plugins
from backend.common.agent_manager import get_agent_manager
from backend.common.oxidns_common_routing import _plugins as plugin_map
from backend.common.oxidns_device_policy import (DIRECT_TAG, HOOK, POLICY_TAG, apply_policies,
    inspect_entries, patch_document, read_policies)
from backend.common.oxidns_editor import prepare_editor
from backend.common.oxidns_settings import OxidnsSettingsError
from backend.test_oxidns_integration import oxidns_app  # noqa: F401

DIRECT = [{'ip': '198.51.100.21', 'name': '电视', 'mode': 'direct'}]
CHANGED = [{'ip': '198.51.100.21', 'name': '电视', 'mode': 'direct'},
           {'ip': '198.51.100.0/24', 'name': '客厅', 'mode': 'direct'},
           {'ip': '198.51.100.22', 'name': '电脑', 'mode': 'smart'}]
HOST = '198.51.100.2'


def legacy(listen=f'{HOST}:15353', prefix='node_test_real'):
    """The hand-made shape found in production: its own servers, sequence and forward."""
    return [
        {'tag': prefix + '_forward', 'type': 'forward', 'args': {'upstreams': [{'addr': 'udp://198.51.100.53:53'}]}},
        {'tag': prefix + '_sequence', 'type': 'sequence', 'args': [
            {'exec': '$query_recorder'}, {'exec': '$' + prefix + '_forward'},
            {'matches': ['!has_resp'], 'exec': 'reject 2'}, {'exec': 'accept'}]},
        {'tag': prefix + '_udp', 'type': 'udp_server', 'args': {'listen': listen, 'entry': prefix + '_sequence'}},
        {'tag': prefix + '_tcp', 'type': 'tcp_server', 'args': {'listen': listen, 'entry': prefix + '_sequence'}},
    ]


def managed(endpoint_id='0123456789abcdef', port=15353):
    spec = {'listen_address': HOST, 'port': port, 'endpoint': f'{HOST}:{port}',
            'upstream': 'udp://198.51.100.53', 'bootstrap': []}
    return spec, managed_plugins(endpoint_id, spec)


def context(node_test_dns=(), records=(), profile='default', host=HOST, mode=None):
    system = {'node_test_dns': list(node_test_dns)}
    if mode:
        system['node_test_dns_config'] = {'mode': mode, 'servers': list(node_test_dns), 'bootstrap': [], 'managed': {}}
    endpoints = {}
    for index, (spec, plugins, owner) in enumerate(records):
        endpoints[f'e{index}'] = {**spec, 'endpoint_id': f'e{index}', 'profile_id': owner,
                                  'agent_host': host, 'status': 'ready', 'plugins': deepcopy(plugins)}
    return {'oxidns': {'query_recorder': {'enabled': True}}, 'profile_id': profile, 'system_config': system,
            'agents': [{'id': 'dns', 'service_type': 'oxidns', 'profile_id': profile, 'host': host,
                        'node_test_dns_endpoints': endpoints}]}


def native(config, extra=(), mutate=None):
    settings = prepare_editor(config['oxidns'], config)
    document = yaml.safe_load(settings['native_config'])
    document['plugins'].extend(deepcopy(list(extra)))
    if mutate:
        mutate(document)
    settings['native_config'] = yaml.safe_dump(document, allow_unicode=True, sort_keys=False)
    return settings


def doc(settings):
    return yaml.safe_load(settings['native_config'])


def side_plugins(settings, tags):
    return [p for p in doc(settings)['plugins'] if p['tag'] in tags]


def report(settings, config):
    document = doc(settings)
    return inspect_entries(document, plugin_map(document), config)


def lifecycle(settings, config, extra_tags):
    """Insert, change and revoke; the side chain is identical at every step."""
    before = doc(settings)
    side = side_plugins(settings, extra_tags)
    inserted = apply_policies(settings, config, DIRECT)
    main = next(p for p in doc(inserted)['plugins'] if p['tag'] == 'sequence_main')['args']
    assert main.count(HOOK) == 1
    assert side_plugins(inserted, extra_tags) == side
    assert read_policies(inserted, config)['editable']
    changed = apply_policies(inserted, config, CHANGED)
    policy = next(p for p in doc(changed)['plugins'] if p['tag'] == POLICY_TAG)
    assert {'matches': ['client_ip 198.51.100.22'], 'exec': 'return'} in policy['args']
    assert side_plugins(changed, extra_tags) == side
    revoked = apply_policies(changed, config, [])
    assert doc(revoked) == before
    assert all(p['tag'] not in (POLICY_TAG, DIRECT_TAG) for p in doc(revoked)['plugins'])
    return inserted


def test_legacy_node_test_entry_used_by_node_tests_is_silent_and_untouched():
    config = context([f'{HOST}:15353'])
    settings = native(config, legacy())
    state = read_policies(settings, config)
    assert state['editable'], state['reason']
    assert state['side_entries'] == []
    assert report(settings, config)['node_test_entries'] == [{
        'tags': ['node_test_real_udp', 'node_test_real_tcp'], 'listen': f'{HOST}:15353',
        'entry': 'node_test_real_sequence', 'kind': 'legacy'}]
    lifecycle(settings, config, {p['tag'] for p in legacy()})


def test_legacy_entry_is_recognized_from_the_saved_custom_dns_and_a_wildcard_listen():
    config = context([f'{HOST}:15353'], mode='custom')
    config['system_config'].pop('node_test_dns')
    settings = native(config, legacy(listen=':15353'))
    assert read_policies(settings, config)['side_entries'] == []
    assert [item['kind'] for item in report(settings, config)['node_test_entries']] == ['legacy']


@pytest.mark.parametrize(('node_test_dns', 'mutate'), [
    ([], None),                                   # node tests no longer use it
    ([f'{HOST}:15354'], None),                    # node tests use another listener
    (['198.51.100.9:15353'], None),               # another host on the same port
    ([f'{HOST}:15353'], lambda d: d['plugins'].append(  # the forward is shared
        {'tag': 'reuse', 'type': 'sequence', 'args': [{'exec': '$node_test_real_forward'}]})),
    ([f'{HOST}:15353'], lambda d: next(p for p in d['plugins'] if p['tag'] == 'node_test_real_sequence')
        ['args'].insert(0, {'exec': 'goto china_dns'})),  # runs a main-chain sequence
])
def test_unconfirmed_legacy_entries_are_reported_but_still_allowed(node_test_dns, mutate):
    config = context(node_test_dns)
    settings = native(config, legacy(), mutate)
    state = read_policies(settings, config)
    assert state['editable'], state['reason']
    assert state['side_entries'] == [{'tags': ['node_test_real_udp', 'node_test_real_tcp'],
                                      'listen': f'{HOST}:15353', 'entry': 'node_test_real_sequence'}]
    assert report(settings, config)['node_test_entries'] == []


def test_current_managed_entry_confirmed_by_its_record_is_silent_and_untouched():
    spec, plugins = managed()
    config = context([spec['endpoint']], records=[(spec, plugins, 'default')], mode='managed')
    settings = native(config, plugins)
    state = read_policies(settings, config)
    assert state['editable'], state['reason']
    assert state['side_entries'] == []
    assert [item['kind'] for item in report(settings, config)['node_test_entries']] == ['managed']
    lifecycle(settings, config, {p['tag'] for p in plugins})


def test_managed_and_legacy_entries_together_and_one_unknown_side_entry():
    spec, plugins = managed(port=15354)
    unknown = legacy(listen='198.51.100.3:5300', prefix='debug_dns')
    config = context([f'{HOST}:15353'], records=[(spec, plugins, 'default')])
    settings = native(config, legacy() + plugins + unknown)
    state = read_policies(settings, config)
    assert state['editable'], state['reason']
    assert state['side_entries'] == [{'tags': ['debug_dns_udp', 'debug_dns_tcp'],
                                      'listen': '198.51.100.3:5300', 'entry': 'debug_dns_sequence'}]
    assert sorted(item['kind'] for item in report(settings, config)['node_test_entries']) == ['legacy', 'managed']
    lifecycle(settings, config, {p['tag'] for p in legacy() + plugins + unknown})


@pytest.mark.parametrize('variant', ['edited', 'other_profile', 'removed', 'no_record'])
def test_managed_tags_without_an_intact_record_are_only_a_reported_side_entry(variant):
    spec, plugins = managed()
    deployed = deepcopy(plugins)
    records = [(spec, plugins, 'other' if variant == 'other_profile' else 'default')]
    if variant == 'edited':
        deployed[0]['args']['upstreams'][0]['addr'] = 'udp://198.51.100.99'
    config = context(records=[] if variant == 'no_record' else records)
    if variant == 'removed':
        config['agents'][0]['node_test_dns_endpoints']['e0']['status'] = 'removed'
    settings = native(config, deployed)
    state = read_policies(settings, config)
    assert state['editable'], state['reason']
    assert [item['tags'] for item in state['side_entries']] == [[plugins[2]['tag'], plugins[3]['tag']]]


def test_managed_entry_on_port_53_is_trusted_only_with_its_record():
    spec, plugins = managed(port=53)
    config = context(records=[(spec, plugins, 'default')])
    settings = native(config, plugins)
    assert read_policies(settings, config)['editable']
    lifecycle(settings, config, {p['tag'] for p in plugins})
    config['agents'][0]['node_test_dns_endpoints'].clear()
    state = read_policies(settings, config)
    assert not state['editable']
    assert f'{HOST}:53' in state['reason'] and '没有进入主处理链 sequence_main' in state['reason']


def test_unknown_side_entry_on_another_port_is_allowed_reported_and_untouched():
    extra = [
        {'tag': 'lab_forward', 'type': 'forward', 'args': {'upstreams': [{'addr': 'udp://198.51.100.54:53'}]}},
        {'tag': 'lab_chain', 'type': 'sequence', 'args': [{'exec': '$lab_forward'}, {'exec': 'accept'}]},
        {'tag': 'lab_doh', 'type': 'http_server', 'args': {'listen': '[::]:8443',
                                                           'entries': [{'path': '/dns-query', 'exec': 'lab_chain'}]}},
        {'tag': 'lab_v6', 'type': 'udp_server', 'args': {'listen': '[2001:db8::2]:5353', 'entry': 'lab_chain'}},
    ]
    config = context()
    settings = native(config, extra)
    state = read_policies(settings, config)
    assert state['editable'], state['reason']
    assert state['side_entries'] == [
        {'tags': ['lab_doh'], 'listen': '[::]:8443', 'entry': 'lab_chain'},
        {'tags': ['lab_v6'], 'listen': '[2001:db8::2]:5353', 'entry': 'lab_chain'},
    ]
    lifecycle(settings, config, {p['tag'] for p in extra})


def test_template_generation_allows_side_listeners_without_profile_context():
    from backend.converters.oxidns import generate_oxidns_config
    config = {'oxidns': {'device_policies': DIRECT, 'query_recorder': {'enabled': True},
                         'custom_config': yaml.safe_dump({'plugins': legacy(listen='198.51.100.2:15353')})}}
    document = yaml.safe_load(generate_oxidns_config(config))
    assert HOOK in next(p for p in document['plugins'] if p['tag'] == 'sequence_main')['args']
    assert [p for p in document['plugins'] if p['tag'].startswith('node_test_real')] == legacy()


@pytest.mark.parametrize(('listen', 'expected'), [
    ('0.0.0.0:53', '监听入口 side_udp（0.0.0.0:53）监听客户端 DNS 端口 53，但 entry 是 side_seq'),
    ('[::]:53', '监听入口 side_udp（[::]:53）监听客户端 DNS 端口 53'),
    ('[fd00::53]:53', '（[fd00::53]:53）监听客户端 DNS 端口 53'),
    ('198.51.100.53:53', '（198.51.100.53:53）监听客户端 DNS 端口 53'),   # an HA VIP
    ('${SIDE_LISTEN}', '（${SIDE_LISTEN}）无法确认监听端口'),
    (None, '监听入口 side_udp（未填写监听地址）无法确认监听端口'),
])
def test_client_port_listener_outside_the_main_chain_is_refused_with_details(listen, expected):
    server = {'entry': 'side_seq'} if listen is None else {'listen': listen, 'entry': 'side_seq'}
    extra = [{'tag': 'side_seq', 'type': 'sequence', 'args': [{'exec': '$forward_local'}, {'exec': 'accept'}]},
             {'tag': 'side_udp', 'type': 'udp_server', 'args': server}]
    config = context()
    settings = native(config, extra)
    state = read_policies(settings, config)
    assert not state['editable'] and state['side_entries'] == []
    assert expected in state['reason']
    assert '没有进入主处理链 sequence_main' in state['reason']
    assert '把它的 entry 改为 sequence_main，或让它改用其他端口' in state['reason']
    assert '自定义处理链' not in state['reason']
    with pytest.raises(OxidnsSettingsError, match='side_udp'):
        apply_policies(settings, config, DIRECT)


def test_a_legacy_shaped_entry_on_port_53_is_still_refused():
    config = context([f'{HOST}:53'])
    settings = native(config, legacy(listen=f'{HOST}:53'))
    state = read_policies(settings, config)
    assert not state['editable']
    assert 'node_test_real_udp、node_test_real_tcp' in state['reason'] and '监听客户端 DNS 端口 53' in state['reason']


def test_the_main_chains_own_port_is_a_client_port_too():
    def move(document):
        for plugin in document['plugins']:
            if plugin['tag'] in ('server_udp', 'server_tcp'):
                plugin['args']['listen'] = '0.0.0.0:5353'
    extra = [{'tag': 'v6_seq', 'type': 'sequence', 'args': [{'exec': '$forward_local'}]},
             {'tag': 'server_v6', 'type': 'udp_server', 'args': {'listen': '[::]:5353', 'entry': 'v6_seq'}}]
    config = context()
    state = read_policies(native(config, extra, move), config)
    assert not state['editable']
    assert '监听入口 server_v6（[::]:5353）监听客户端 DNS 端口 5353' in state['reason']


@pytest.mark.parametrize(('steps', 'entry', 'expected'), [
    ([{'exec': 'jump sequence_main'}], 'side_seq', '的处理链 side_seq 会间接调用主处理链 sequence_main'),
    ([{'exec': '$inner'}], 'side_seq', '的处理链 side_seq 会间接调用主处理链 sequence_main'),
    ([{'exec': '$forward_local'}], 'missing_seq', '的处理链用到的插件 missing_seq 不存在'),
    ([{'exec': '${SIDE_EXEC}'}], 'side_seq', '的处理链含未展开的环境变量'),
])
def test_side_chain_that_is_not_independent_is_refused(steps, entry, expected):
    extra = [{'tag': 'inner', 'type': 'sequence', 'args': [{'exec': 'goto sequence_main'}]},
             {'tag': 'side_seq', 'type': 'sequence', 'args': steps},
             {'tag': 'side_udp', 'type': 'udp_server', 'args': {'listen': '198.51.100.2:5300', 'entry': entry}}]
    config = context()
    state = read_policies(native(config, extra), config)
    assert not state['editable']
    assert '监听入口 side_udp（198.51.100.2:5300）' in state['reason'] and expected in state['reason']


def _set_main(document, args):
    next(p for p in document['plugins'] if p['tag'] == 'sequence_main')['args'] = args


def _drop(tag):
    return lambda document: document.update(plugins=[p for p in document['plugins'] if p['tag'] != tag])


def _edit(tag, **changes):
    return lambda document: next(p for p in document['plugins'] if p['tag'] == tag).update(changes)


def _all_servers_to(entry):
    def mutate(document):
        document['plugins'].append({'tag': 'other_main', 'type': 'sequence', 'args': [{'exec': '$forward_local'}]})
        for plugin in document['plugins']:
            if plugin['type'].endswith('_server'):
                plugin['args']['entry'] = entry
    return mutate


@pytest.mark.parametrize(('mutate', 'expected'), [
    (lambda d: d.update(include=['extra.yaml']), '配置用 include 引入了其他文件'),
    (_drop('sequence_main'), '找不到主处理链 sequence_main'),
    (_edit('sequence_main', type='fallback'), '主处理链 sequence_main 的类型是 fallback，不是 sequence'),
    (lambda d: _set_main(d, []), '主处理链 sequence_main 没有处理步骤'),
    (lambda d: _set_main(d, [{'matches': ['qtype 65']}, {'exec': 'goto china_dns'}]),
     '主处理链 sequence_main 第 1 步没有 exec 动作'),
    (lambda d: _set_main(d, [{'exec': '$forward_local'}, {'exec': 'accept'}]),
     '主处理链 sequence_main 的最后一步是「accept」'),
    (lambda d: _set_main(d, [{'matches': ['qname $x'], 'exec': 'goto china_dns'}]),
     '最后一步是「goto china_dns（带匹配条件）」'),
    (lambda d: _set_main(d, [{'exec': 'goto proxy_dns_seq', 'note': 'manual'}]),
     '最后一步是「goto proxy_dns_seq（带其他字段）」'),
    (_edit('forward_local', type='sequence', args=[]), '本地上游 forward_local 的类型是 sequence，不是 forward'),
    (_edit('forward_local', args={'upstreams': []}), '本地上游 forward_local 没有配置上游地址'),
    (lambda d: d['plugins'].append({'tag': 'sequence_main', 'type': 'sequence', 'args': []}),
     '插件 tag「sequence_main」重复'),
    (_all_servers_to('other_main'), '没有任何监听入口进入主处理链 sequence_main'),
])
def test_existing_refusals_remain_and_say_what_is_wrong(mutate, expected):
    config = context()
    settings = native(config, (), mutate)
    state = read_policies(settings, config)
    assert not state['editable']
    assert expected in state['reason'], state['reason']
    assert state['reason'].endswith('无法安全插入设备策略；请先在 OxiDNS 完整参数中调整。')
    with pytest.raises(OxidnsSettingsError):
        apply_policies(settings, config, DIRECT)


def test_forward_local_missing_is_named():
    config = context()
    settings = native(config, (), lambda d: (_drop('forward_local')(d),
                                             d['plugins'].append({'tag': 'forward_local_x', 'type': 'forward', 'args': {}})))
    assert '找不到本地上游 forward_local' in read_policies(settings, config)['reason']


def test_patch_document_still_accepts_calls_without_profile_context():
    config = context()
    document = doc(native(config, legacy()))
    patched = patch_document(document, [], DIRECT)
    assert HOOK in next(p for p in patched['plugins'] if p['tag'] == 'sequence_main')['args']
    assert [p for p in patched['plugins'] if p['tag'].startswith('node_test_real')] == legacy()


def test_client_devices_api_lists_side_entries_and_sets_policies_with_a_legacy_entry(oxidns_app):
    app, repo = oxidns_app
    get_agent_manager().register_agent({'name': 'dns', 'host': HOST, 'port': 18000,
                                        'service_type': 'oxidns', 'profile_id': 'office'})
    repo.update_system_transaction(lambda system: system['system_config'].update(
        node_test_dns=[f'{HOST}:15353'], node_test_dns_config={
            'mode': 'custom', 'servers': [f'{HOST}:15353'], 'bootstrap': [], 'managed': {}}))
    unknown = legacy(listen='198.51.100.3:5300', prefix='debug_dns')

    def seed(profile):
        profile['oxidns']['query_recorder'] = {'enabled': True}
        profile['oxidns'] = native({'oxidns': profile['oxidns']}, legacy() + unknown)
    repo.update_profile_transaction('office', seed)
    client = app.test_client()
    state = client.get('/api/client-devices?profile=office').get_json()
    assert state['editable'], state['reason']
    assert state['side_entries'] == [{'tags': ['debug_dns_udp', 'debug_dns_tcp'],
                                      'listen': '198.51.100.3:5300', 'entry': 'debug_dns_sequence'}]
    saved = client.put('/api/client-devices?profile=office', json={
        'profile_id': 'office', 'ip': '198.51.100.21', 'name': '电视', 'mode': 'direct',
        'previous_name': '', 'previous_mode': '', 'base_url': ''})
    assert saved.status_code == 200, saved.get_json()
    document = yaml.safe_load(repo.get_profile('office')['oxidns']['native_config'])
    assert HOOK in next(p for p in document['plugins'] if p['tag'] == 'sequence_main')['args']
    assert [p for p in document['plugins'] if p['tag'].startswith(('node_test_real', 'debug_dns'))] == legacy() + unknown
    assert saved.get_json()['side_entries'] == state['side_entries']
