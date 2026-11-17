"""Gateway forwarding state, Agent failure mapping and untrusted display data."""
import base64
from copy import deepcopy
import json

from cases import Case
from fixtures import agent_peers, heartbeat

UPGRADE = '请先将 Mihomo Agent 更新到 1.8.41-go 或更新版本，才能在此开启并持久化 IPv4 转发。'


def preparation():
    return ({'action': 'seed', 'agent': agent_peers()}, heartbeat('m-a'), heartbeat('m-b'))


def fault(operation, **values):
    return {'action': 'fault', 'match': {'peer': 'agent', '/headers/host': '10.44.0.2:18083',
            'path': '/api/linux-gateway/' + operation}, **deepcopy(values)}


def body():
    return deepcopy(agent_peers()['10.44.0.2:18083']['endpoints']['/api/linux-gateway/status'])


def reads():
    definitions = []
    for status in (400, 409, 422, 500, 502, 503):
        definitions.append(('http-' + str(status), {'status': status, 'body': {'message': '合成 Agent 拒绝🧪'}},
                            'error', '合成 Agent 拒绝🧪'))
    for status in (401, 403):
        definitions.append(('auth-' + str(status), {'status': status}, 'error', 'Agent 认证失败，请检查设备注册状态。'))
    definitions.append(('missing-api', {'status': 404}, 'upgrade_required', UPGRADE))
    for index, value in enumerate(([], False, 42, 'text', None)):
        definitions.append(('not-object-' + str(index), {'status': 200, 'body': value}, 'error', 'Agent 返回的网关数据格式异常。'))
    definitions.extend([
        ('invalid-json', {'status': 200, 'raw': base64.b64encode(b'<html>synthetic</html>').decode()},
         'error', 'Agent 返回的网关数据无法解析，请刷新设备状态。'),
        ('lost-response', {'disconnect': True}, 'error', '暂时无法连接 Agent，请检查设备连接。'),
        ('unicode-error-limit', {'status': 500, 'body': {'message': '错误🧪' * 400}}, 'error', ('错误🧪' * 400)[:1000])])
    for label, values, state, message in definitions:
        yield Case('R025/agent-' + label, 'R025', 'GET', '/api/routers/gateway-forwarding', 200,
                   ('R025-S04', 'R025-F11', 'H0017'), prepare=(*preparation(), fault('status', **values)),
                   unchanged=False, effect='device-read', checks={'success': True, 'gateways/0/state': state,
                    'gateways/0/message': message, 'gateways/0/can_persist': False, 'gateways/1/state': 'needs_persist'},
                   header_checks={'Cache-Control': 'no-store'})
    for index, value in enumerate((None, [], False, 'bad')):
        data = body()
        data['ipv4_forwarding'] = value
        yield Case('R025/forwarding-shape-' + str(index), 'R025', 'GET', '/api/routers/gateway-forwarding', 200,
                   ('H0017',), prepare=(*preparation(), fault('status', status=200, body=data)),
                   unchanged=False, effect='device-read', checks={'gateways/0/state': 'upgrade_required',
                    'gateways/0/message': UPGRADE, 'gateways/1/state': 'needs_persist'})
    for label, patch, state, extra in [
        ('unsupported', {'supported': False, 'unsupported_reason': '合成平台不支持🧪'}, 'unsupported', {'message': '合成平台不支持🧪'}),
        ('foreign', {'config': 'foreign'}, 'blocked', {}), ('symlink', {'config': 'symlink'}, 'blocked', {}),
        ('modified-hook', {'hook_script': 'modified'}, 'blocked', {}),
        ('persisted', {'current': '1', 'persisted': True}, 'persisted', {'can_persist': False}),
        ('bool-current', {'current': True, 'persisted': True}, 'needs_persist', {'can_persist': True}),
        ('integer-persisted', {'current': '1', 'persisted': 1}, 'needs_persist', {'can_persist': True}),
        ('system-persisted', {'current': '1', 'system_persisted': True, 'system_source': '/etc/sysctl.d/synthetic.conf:2'},
         'system_persisted', {'can_persist': True, 'system_source': '/etc/sysctl.d/synthetic.conf:2'}),
        ('bad-system-source', {'current': '1', 'system_persisted': True, 'system_source': 'bad\nsource'},
         'system_persisted', {'system_source': None}),
        ('notice', {'notice': '启动服务尚未启用🧪'}, 'needs_persist', {'message': '启动服务尚未启用🧪'})]:
        data = body()
        data['ipv4_forwarding'].update(patch)
        yield Case('R025/state-' + label, 'R025', 'GET', '/api/routers/gateway-forwarding', 200,
                   ('R025-S01',), prepare=(*preparation(), fault('status', status=200, body=data)),
                   unchanged=False, effect='device-read', checks={'gateways/0/state': state,
                    **{'gateways/0/' + key: value for key, value in extra.items()}})


def writes():
    definitions = []
    for status in (400, 409, 422, 500, 503):
        definitions.append(('http-' + str(status), {'status': status, 'body': {'message': '合成持久化失败🧪'}},
                            status if status in (400, 409, 422) else 502, 'GATEWAY_ERROR', '合成持久化失败🧪'))
    for status in (401, 403):
        definitions.append(('auth-' + str(status), {'status': status}, 502, 'AGENT_AUTH_FAILED', 'Agent 认证失败，请检查设备注册状态。'))
    definitions.append(('missing-api', {'status': 404}, 409, 'UPGRADE_REQUIRED',
                        '设备尚未提供网关接口，请更新到包含本功能的 1.8.40-go 或更新版本。'))
    for index, value in enumerate(([], None, False, 'bad')):
        definitions.append(('shape-' + str(index), {'status': 200, 'body': value}, 502,
                            'INVALID_RESPONSE', 'Agent 返回的网关数据格式异常。'))
    definitions.append(('invalid-json', {'status': 200, 'raw': base64.b64encode(b'bad JSON').decode()},
                        502, 'INVALID_RESPONSE', 'Agent 返回的网关数据无法解析，请刷新设备状态。'))
    for label, values, status, code, message in definitions:
        yield Case('R026/agent-' + label, 'R026', 'POST', '/api/routers/gateway-forwarding', status,
                   ('R026-F13',), document={'agent_id': 'm-a'}, prepare=(*preparation(), fault('forwarding', **values)),
                   unchanged=False, effect='device-read', message=message, checks={'success': False, 'code': code},
                   expected_requests=({'match': {'peer': 'agent', 'path': '/api/linux-gateway/forwarding'}, 'count': 1},),
                   header_checks={'Cache-Control': 'no-store'})


def response_limits():
    for length in (512 * 1024, 512 * 1024 + 1):
        data = body()
        data['padding'] = ''
        fixed = json.dumps(data, ensure_ascii=False, separators=(',', ':')).encode()
        data['padding'] = 'x' * (length - len(fixed))
        raw = json.dumps(data, ensure_ascii=False, separators=(',', ':')).encode()
        if len(raw) != length:
            raise ValueError('invalid response-size fixture')
        oversized = length > 512 * 1024
        yield Case('R025/response-byte-limit-' + str(length), 'R025', 'GET', '/api/routers/gateway-forwarding', 200,
                   ('H0005',) if oversized else (),
                   prepare=(*preparation(), fault('status', status=200, raw=base64.b64encode(raw).decode())),
                   unchanged=False, effect='device-read', checks={'gateways/0/state': 'error' if oversized else 'needs_persist',
                    'gateways/0/message': 'Agent 返回的网关数据过大。' if oversized else ''}, absent=('gateways/0/padding',))


def malformed_filter_bug():
    for label, value in [('state-array', {'state': []}), ('state-object', {'state': {}}),
                          ('source-kind-array', {'state': 'may_drop', 'sources': [{'kind': []}]}),
                          ('remedy-source-array', {'state': 'may_drop', 'remedies': [{'source': []}]})]:
        data = body()
        data['forward_filter'] = value
        yield Case('R025/filter-bug-' + label, 'R025', 'GET', '/api/routers/gateway-forwarding', 500,
                   ('H0016', 'R025-F11'), prepare=(*preparation(), fault('status', status=200, body=data)),
                   unchanged=False, effect='device-read', html_error=True,
                   python_bug='AGENT_FORWARD_FILTER_UNHASHABLE',
                   console_patterns=(r"TypeError: unhashable type: 'dict'" if label == 'state-object' else
                                     r"TypeError: unhashable type: 'list'",))


def display_filter():
    for index, value in enumerate((None, False, [], 'wrong', {}, {'state': 'wrong'}, {'state': 0})):
        data = body()
        data['forward_filter'] = value
        yield Case('R025/filter-dropped-' + str(index), 'R025', 'GET', '/api/routers/gateway-forwarding', 200,
                   ('H0016',), prepare=(*preparation(), fault('status', status=200, body=data)),
                   unchanged=False, effect='device-read', checks={'gateways/0/forward_filter': None,
                                                                'gateways/0/state': 'needs_persist'})
    commands = ['iptables -I FORWARD -i tun0 -j ACCEPT', 'nft add rule inet filter forward iifname "tun0" accept',
                'iptables -I FORWARD -i tun0 -j ACCEPT && nft add rule inet filter forward accept']
    source = {'kind': 'nftables', 'backend': 'nft', 'hook': 'forward', 'family': 'inet',
              'table': 'filter', 'chain': 'forward', 'policy': 'drop', 'detail': '\x00 来源🧪\u200b\n'}
    remedy = {'source': 'iptables', 'label': '\x00 允许转发🧪\u200b', 'device': 'tun0',
              'commands': commands + ['curl https://example.com', 'iptables -I FORWARD -i $(touch synthetic) -j ACCEPT',
                  'iptables -I FORWARD -i tun0 -j ACCEPT; echo synthetic', 'iptables -I FORWARD -j ACCEPT > synthetic'],
              'persist_note': '提示🧪' * 200}
    data = body()
    data['forward_filter'] = {'protocol': 1, 'state': 'may_drop', 'reason': 'forward_chain_drop', 'device': 'tun0',
        'auto_redirect': True, 'policy': 'drop', 'input_policy': 'accept', 'flows': ['tun_tcp', 'forward', 'dns', 'forward', 'bad'],
        'forward_protocols': ['udp', 'TCP', 'tcp', 'icmp', 'ssh'], 'sources': [None, {'kind': 'wrong'}] + [source] * 10,
        'remedies': [None, {'source': 'wrong'}] + [remedy] * 8, 'hint': '\x00 提示🧪\u200b\n', 'verify': 7,
        'checked_at': '2026-01-02T03:04:05Z', 'cached': True, 'unknown': 'must not appear'}
    expected_source = {**source, 'detail': '来源🧪'}
    expected_remedy = {**remedy, 'label': '允许转发🧪', 'commands': commands, 'persist_note': ('提示🧪' * 200)[:300]}
    expected = {'protocol': 1, 'state': 'may_drop', 'reason': 'forward_chain_drop', 'device': 'tun0',
        'auto_redirect': True, 'policy': 'drop', 'input_policy': 'accept', 'flows': ['forward', 'dns', 'tun_tcp'],
        'forward_protocols': ['tcp', 'udp', 'icmp'], 'sources': [expected_source] * 8, 'remedies': [expected_remedy] * 6,
        'hint': '提示🧪', 'verify': '', 'checked_at': '2026-01-02T03:04:05Z', 'cached': True}
    yield Case('R025/filter-safe-display-shape', 'R025', 'GET', '/api/routers/gateway-forwarding', 200, ('H0016',),
               prepare=(*preparation(), fault('status', status=200, body=data)), unchanged=False, effect='device-read',
               checks={'gateways/0/forward_filter': expected})
    data['forward_filter'].update(protocol=True, auto_redirect=1, cached=1, reason='Bad Reason', device='bad;device',
                                  flows=['dns'], policy='invalid', input_policy=False, sources=[], remedies=[])
    yield Case('R025/filter-exact-types', 'R025', 'GET', '/api/routers/gateway-forwarding', 200, ('H0016',),
               prepare=(*preparation(), fault('status', status=200, body=data)), unchanged=False, effect='device-read',
               checks={'gateways/0/forward_filter': {**expected, 'protocol': 0, 'auto_redirect': False, 'cached': False,
                    'reason': '', 'device': '', 'flows': ['dns'], 'forward_protocols': [], 'policy': '', 'input_policy': '',
                    'sources': [], 'remedies': []}})


def uncertain():
    for applied in (False, True):
        label = 'applied' if applied else 'not-applied'
        yield Case('R026/lost-reply-' + label, 'R026', 'POST', '/api/routers/gateway-forwarding', 502,
                   ('R026-B03', 'T20'), document={'agent_id': 'm-a'},
                   prepare=(*preparation(), fault('forwarding', apply=applied, disconnect=True)),
                   unchanged=False, effect='device-only' if applied else 'device-read',
                   message='未能确认转发持久化结果，请先刷新设备状态，勿重复操作。',
                   checks={'code': 'AGENT_UNREACHABLE'},
                   after_poll=({'path': '/api/routers/gateway-forwarding',
                                'expect': {'gateways/0/state': 'persisted' if applied else 'needs_persist'}},),
                   expected_requests=({'match': {'peer': 'agent', 'path': '/api/linux-gateway/forwarding'}, 'count': 1},))


def timeouts():
    for operation in ('status', 'forwarding'):
        writing = operation == 'forwarding'
        gate = 'Agent-' + operation + '-real-timeout'
        yield Case('R025-26/real-timeout-' + operation, 'R026' if writing else 'R025',
                   'POST' if writing else 'GET', '/api/routers/gateway-forwarding', 502 if writing else 200,
                   ('R026-F13', 'T20') if writing else ('R025-F11', 'T20'),
                   document={'agent_id': 'm-a'} if writing else None,
                   prepare=(*preparation(), fault(operation, gate=gate, apply=writing, passthrough=writing,
                                                  status=200, body=body())),
                   finalize=({'action': 'release', 'gate': gate},), elapsed_min=44,
                   unchanged=False, effect='device-only' if writing else 'device-read',
                   message='未能确认转发持久化结果，请先刷新设备状态，勿重复操作。' if writing else None,
                   checks={'code': 'AGENT_UNREACHABLE'} if writing else
                          {'gateways/0/state': 'error', 'gateways/0/message': '暂时无法连接 Agent，请检查设备连接。',
                           'gateways/1/state': 'needs_persist'},
                   after_poll=({'path': '/api/routers/gateway-forwarding', 'expect': {'gateways/0/state': 'persisted'}},)
                               if writing else ())
