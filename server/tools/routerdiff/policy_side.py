"""Side and legacy node-test listeners through public native editor requests."""
import copy

from cases import Case
from seed import AGENT_TOKEN
import policy_graph


def document():
    value = policy_graph.document()
    # Keep the common udp tag/udp:// collision as a separate reference-bug
    # world; neutral tags let the other legacy-listener guards be reached.
    value['plugins'][3]['tag'] = 'client_dns_listener'
    value['plugins'] += [
        {'tag': 'side_forward', 'type': 'forward', 'args': {
            'upstreams': [{'addr': 'udp://10.44.0.54:53'}], 'opaque_side_setting': 'side_custom_keep🧪'}},
        {'tag': 'side', 'type': 'sequence', 'args': [{'exec': '$side_forward'}]},
        {'tag': 'side_udp', 'type': 'udp_server', 'args': {'listen': '127.0.0.1:5353', 'entry': 'side'}}]
    return value


def options(value, node_dns=None, extra_system=None):
    result = policy_graph.options(value)
    result['system'] = {'system_config': {'node_test_dns': ['127.0.0.1:5353'] if node_dns is None else node_dns}}
    if extra_system:
        for key, patch in extra_system.items():
            if isinstance(patch, dict):
                result['system'].setdefault(key, {}).update(copy.deepcopy(patch))
            else:
                result['system'][key] = copy.deepcopy(patch)
    return result


def variants():
    result = [('isolated-side', document(), [], None, ('H0091',), False, ['side_udp'], '127.0.0.1:5353', 'side'),
              ('legacy-direct-host', document(), None, None, (), True, ['side_udp'], '127.0.0.1:5353', 'side')]
    for label, node_dns in (('different-host', ['127.0.0.2:5353']), ('different-port', ['127.0.0.1:5354']), ('default-node-port-is53', ['127.0.0.1'])):
        result.append((label, document(), node_dns, None, ('H0091',), False, ['side_udp'], '127.0.0.1:5353', 'side'))
    for label, listen in (('wildcard-no-host', '0.0.0.0:5353'), ('ipv6-wildcard-no-host', '[::]:5353')):
        value = document()
        value['plugins'][6]['args']['listen'] = listen
        result.append((label, value, None, None, ('H0091',), False, ['side_udp'], listen, 'side'))
    value = document()
    value['plugins'][6]['args']['listen'] = '0.0.0.0:5353'
    system = {'agents': [{'id': 'side-dns', 'name': '合成 DNS', 'service_type': 'oxidns', 'profile_id': 'alpha',
                         'host': '10.44.0.53', 'port': 18083, 'token': AGENT_TOKEN, 'enabled': False,
                         'status': 'offline', 'version': '1.99.0-go', 'deployment_method': 'shell'}]}
    result.append(('legacy-wildcard-agent-host', value, ['10.44.0.53:5353'], system, (), True,
                   ['side_udp'], '0.0.0.0:5353', 'side'))
    for label, listen in (('legacy-ipv6-wildcard-agent-host', '[::]:5353'), ('legacy-empty-wildcard-agent-host', ':5353')):
        value = document()
        value['plugins'][6]['args']['listen'] = listen
        result.append((label, value, ['10.44.0.53:5353'], system, (), True, ['side_udp'], listen, 'side'))
    invalid = [None, False, 1, {}, 'https://example.com/dns-query', 'hostname.example:5353', 'bad-ip', '[::1]:5353', '127.0.0.1:5353']
    result.append(('invalid-dns-context-members', document(), invalid, None, ('H0090',), True,
                   ['side_udp'], '127.0.0.1:5353', 'side'))
    result.append(('custom-dns-context-members', document(), [], {'system_config': {
        'node_test_dns_config': {'mode': 'custom', 'servers': invalid}}}, ('H0090',), True,
        ['side_udp'], '127.0.0.1:5353', 'side'))
    value = document()
    value['plugins'][6] = {'tag': 'side_udp', 'type': 'http_server',
                          'args': {'listen': '127.0.0.1:5353', 'entries': [{'exec': 'side'}]}}
    result.append(('http-is-not-legacy', value, None, None, ('H0092',), False,
                   ['side_udp'], '127.0.0.1:5353', 'side'))
    value = document()
    value['plugins'][6]['type'] = 'tls_server'
    result.append(('tls-is-not-legacy', value, None, None, ('H0092',), False,
                   ['side_udp'], '127.0.0.1:5353', 'side'))
    value = document()
    value['plugins'] += [{'tag': 'other', 'type': 'sequence', 'args': [{'exec': '$side_forward'}]}]
    value['plugins'][6] = {'tag': 'side_udp', 'type': 'http_server', 'args': {'listen': '127.0.0.1:5353',
                          'entries': [{'exec': 'side'}, {'exec': 'other'}]}}
    result.append(('multiple-http-entries', value, None, None, ('H0092',), False,
                   ['side_udp'], '127.0.0.1:5353', 'side、other'))
    value = document()
    value['plugins'][5] = {'tag': 'side', 'type': 'forward', 'args': {'upstreams': [{'addr': 'udp://10.44.0.54:53'}]}}
    result.append(('entry-not-sequence', value, None, None, ('H0093',), False,
                   ['side_udp'], '127.0.0.1:5353', 'side'))
    for kind in ('sequence', 'fallback'):
        value = document()
        value['plugins'][5]['args'] = [{'exec': 'jump nested'}]
        args = [{'exec': '$side_forward'}] if kind == 'sequence' else {'primary': 'side_forward', 'secondary': 'side_forward'}
        value['plugins'].append({'tag': 'nested', 'type': kind, 'args': args})
        result.append(('nested-' + kind, value, None, None, ('H0094',), False,
                       ['side_udp'], '127.0.0.1:5353', 'side'))
    for label, external in (('outside-plugin-reference', {'root_entry': 'side'}), ('foreign-plugin-reference', None)):
        value = document()
        if external:
            value.update(external)
        else:
            value['plugins'].append({'tag': 'foreign', 'type': 'sequence', 'args': [{'exec': 'jump side'}]})
        result.append((label, value, None, None, ('H0095',), False,
                       ['side_udp'], '127.0.0.1:5353', 'side'))
    value = document()
    value['plugins'][5]['args'] = [{'exec': 'return'}]
    result.append(('no-forward', value, None, None, (), False, ['side_udp'], '127.0.0.1:5353', 'side'))
    value = document()
    value['plugins'][1]['args'] = [{'exec': '$side_forward'}]
    result.append(('forward-shared-with-main', value, None, None, (), False, ['side_udp'], '127.0.0.1:5353', 'side'))
    value = document()
    value['plugins'].append({'tag': 'side_tcp', 'type': 'tcp_server',
                             'args': {'listen': '127.0.0.1:5353', 'entry': 'side'}})
    result.append(('udp-tcp-group-legacy', value, None, None, (), True,
                   ['side_udp', 'side_tcp'], '127.0.0.1:5353', 'side'))
    return result


def requests(label, helpers, legacy, tags, listen, entry):
    if legacy:
        helpers = tuple(dict.fromkeys((*helpers, 'H0091', 'H0092', 'H0093', 'H0094', 'H0095')))
    entries = [] if legacy else [{'tags': tags, 'listen': listen, 'entry': entry}]
    row = {'ip': '10.44.0.21', 'name': '现存备注🧪', 'mode': ''}
    body = {'profile_id': 'alpha', **row, 'previous_name': row['name'], 'previous_mode': '', 'base_url': ''}
    return (
        Case('R002/native-side-' + label, 'R002', 'GET', '/api/client-devices', 200, helpers,
            checks={'editable': True, 'reason': '', 'side_entries': entries, 'devices': [row]}),
        Case('R003/native-side-direct-' + label, 'R003', 'PUT', '/api/client-devices', 200, helpers,
            document={**body, 'mode': 'direct'}, unchanged=False, effect='profile-write',
            checks={'editable': True, 'side_entries': entries, 'devices': [{**row, 'mode': 'direct'}]},
            stored_contains={'oxidns/native_config': ('side_forward', 'opaque_side_setting', 'side_custom_keep🧪',
                                                       'cf_device_policy', 'cf_device_direct')}),
        Case('R003/native-side-note-' + label, 'R003', 'PUT', '/api/client-devices', 200, ('R003-S08',),
            document={**body, 'mode': 'direct', 'previous_mode': 'direct', 'name': '只改备注🧪'},
            unchanged=False, effect='profile-write', retained_fields=('oxidns', 'routers', 'client_fixed_ips'),
            checks={'editable': True, 'side_entries': entries}),
    )


def worlds():
    result = []
    for label, value, node_dns, system, helpers, legacy, tags, listen, entry in variants():
        items = requests(label, helpers, legacy, tags, listen, entry)
        result.append(('native-side-' + label, options(value, node_dns, system), (lambda items=items: items,)))
    return result


def collision_options():
    value = document()
    value['plugins'][3]['tag'] = 'udp'
    return options(value, [])


def protocol_collision():
    reason = ('监听入口 side_udp（127.0.0.1:5353） 的处理链 side 会间接调用主处理链 sequence_main，看不出设备策略是否生效'
              '，无法安全插入设备策略；请在 OxiDNS 完整参数中把它的 entry 直接改为 sequence_main，或让它不再调用主处理链。')
    yield Case('R002/native-side-udp-protocol-collision', 'R002', 'GET', '/api/client-devices', 200,
        ('R002-F12',), python_bug='NATIVE_SIDE_REFERENCE_PROTOCOL_COLLISION',
        checks={'editable': False, 'reason': reason, 'side_entries': []})
    yield Case('R003/native-side-udp-protocol-mode-refused', 'R003', 'PUT', '/api/client-devices', 400,
        ('R003-F15',), python_bug='NATIVE_SIDE_REFERENCE_PROTOCOL_COLLISION',
        document={'profile_id': 'alpha', 'ip': '10.44.0.21', 'name': '现存备注🧪', 'mode': 'direct',
                  'previous_name': '现存备注🧪', 'previous_mode': '', 'base_url': ''}, message=reason)
