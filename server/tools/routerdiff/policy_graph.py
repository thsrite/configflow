"""Native DNS editor graphs seeded publicly and exercised through router HTTP."""
import copy
import json

from cases import Case
from seed import router


def document():
    return {'plugins': [
        {'tag': 'forward_local', 'type': 'forward', 'args': {'upstreams': [{'addr': 'udp://10.44.0.53:53'}]}},
        {'tag': 'china_dns', 'type': 'sequence', 'args': [{'exec': '$forward_local'}]},
        {'tag': 'sequence_main', 'type': 'sequence', 'args': [{'exec': 'goto china_dns'}]},
        {'tag': 'udp', 'type': 'udp_server', 'args': {'listen': '127.0.0.1:53', 'entry': 'sequence_main'}}]}


def options(value):
    # JSON is legal YAML; exact input bytes remain observable after note-only
    # writes. No native Python loader/editor helper is called by the fixture.
    return {'alpha': {'routers': {'items': [router()]}, 'device_notes': {'10.44.0.21': '现存备注🧪'},
                      'oxidns': {'config_mode': 'native', 'native_config': json.dumps(value, ensure_ascii=False),
                                 'device_policies': []}}}


def variants():
    suffix = '，无法安全插入设备策略；请先在 OxiDNS 完整参数中调整。'
    result = []
    value = document()
    value['include'] = ['synthetic-fragment.yaml']
    result.append(('include', value, '配置用 include 引入了其他文件，看不到完整的处理链' + suffix))
    value = document()
    value['plugins'].pop(2)
    result.append(('missing-main', value, '找不到主处理链 sequence_main' + suffix))
    value = document()
    value['plugins'].append({'type': 'cache'})
    result.append(('missing-tag', value, '第 5 个插件没有 tag' + suffix))
    value = document()
    value['plugins'].append(copy.deepcopy(value['plugins'][0]))
    result.append(('duplicate-tag', value, '插件 tag「forward_local」重复' + suffix))
    for label, patch, reason in (
        ('main-type', {'type': 'fallback'}, '主处理链 sequence_main 的类型是 fallback，不是 sequence'),
        ('main-args-object', {'args': {}}, '主处理链 sequence_main 没有处理步骤'),
        ('main-args-empty', {'args': []}, '主处理链 sequence_main 没有处理步骤'),
        ('main-step-scalar', {'args': ['invalid']}, '主处理链 sequence_main 第 1 步没有 exec 动作'),
        ('main-exec-missing', {'args': [{}]}, '主处理链 sequence_main 第 1 步没有 exec 动作'),
        ('main-exec-number', {'args': [{'exec': 1}]}, '主处理链 sequence_main 第 1 步没有 exec 动作'),
        ('main-last-wrong', {'args': [{'exec': 'return'}]},
         '主处理链 sequence_main 的最后一步是「return」，设备策略要求最后一步是不带条件的 goto china_dns 或 goto proxy_dns_seq'),
        ('main-last-conditional', {'args': [{'exec': 'goto china_dns', 'matches': ['qname example.com']}]},
         '主处理链 sequence_main 的最后一步是「goto china_dns（带匹配条件）」，设备策略要求最后一步是不带条件的 goto china_dns 或 goto proxy_dns_seq'),
        ('main-last-extra', {'args': [{'exec': 'goto china_dns', 'unknown': True}]},
         '主处理链 sequence_main 的最后一步是「goto china_dns（带其他字段）」，设备策略要求最后一步是不带条件的 goto china_dns 或 goto proxy_dns_seq')):
        value = document()
        value['plugins'][2].update(patch)
        result.append((label, value, reason + suffix))
    value = document()
    value['plugins'].pop(0)
    result.append(('missing-local', value, '找不到本地上游 forward_local，直连设备没有可用的上游' + suffix))
    for label, patch, reason in (
        ('local-type', {'type': 'cache'}, '本地上游 forward_local 的类型是 cache，不是 forward'),
        ('local-args-array', {'args': []}, '本地上游 forward_local 没有配置上游地址'),
        ('local-upstreams-missing', {'args': {}}, '本地上游 forward_local 没有配置上游地址'),
        ('local-upstreams-empty', {'args': {'upstreams': []}}, '本地上游 forward_local 没有配置上游地址')):
        value = document()
        value['plugins'][0].update(patch)
        result.append((label, value, reason + suffix))
    return result


def guard_cases(label, reason):
    row = {'ip': '10.44.0.21', 'name': '现存备注🧪', 'mode': ''}
    body = {'profile_id': 'alpha', **row, 'previous_name': row['name'], 'previous_mode': '', 'base_url': ''}
    return (
        Case('R002/native-policy-' + label, 'R002', 'GET', '/api/client-devices', 200,
            ('R002-F12', 'H0086', 'H0100', 'H0101'),
            checks={'editable': False, 'reason': reason, 'devices': [row], 'side_entries': []}),
        Case('R003/native-policy-mode-refused-' + label, 'R003', 'PUT', '/api/client-devices', 400,
            ('R003-F15', 'H0086'), document={**body, 'name': 'must not half-save', 'mode': 'direct'}, message=reason),
        Case('R003/native-policy-name-only-' + label, 'R003', 'PUT', '/api/client-devices', 200,
            ('R003-S08',), document={**body, 'name': '仍可改名🧪'}, unchanged=False, effect='profile-write',
            retained_fields=('oxidns', 'routers', 'client_fixed_ips'),
            checks={'editable': False, 'reason': reason, 'devices': [{**row, 'name': '仍可改名🧪'}]}),
        Case('R014/native-policy-readonly-' + label, 'R014', 'GET', '/api/routers/r-ros/policy-settings', 200,
            ('H0100', 'H0101'), checks={'workspace/sources/editable': False,
                'workspace/sources/reason': reason, 'workspace/sources/policies': []}),
    )


def worlds():
    result = []
    for label, value, reason in variants():
        items = guard_cases(label, reason)
        result.append(('native-policy-' + label, options(value), (lambda items=items: items,)))
    return result


def valid():
    row = {'ip': '10.44.0.21', 'name': '现存备注🧪', 'mode': ''}
    yield Case('R002/native-policy-recognized-main', 'R002', 'GET', '/api/client-devices', 200,
        ('R002-S01',), checks={'editable': True, 'reason': '', 'devices': [row], 'side_entries': []})
    body = {'profile_id': 'alpha', 'ip': row['ip'], 'name': '新直连设备🧪', 'mode': 'direct',
            'previous_name': row['name'], 'previous_mode': '', 'base_url': ''}
    yield Case('R003/native-policy-valid-direct', 'R003', 'PUT', '/api/client-devices', 200,
        ('R003-S03',), document=body, unchanged=False, effect='profile-write',
        checks={'editable': True, 'devices': [{**row, 'name': body['name'], 'mode': 'direct'}]},
        stored={'oxidns/device_policies': [{'ip': row['ip'], 'name': body['name'], 'mode': 'direct'}]})
    yield Case('R003/native-policy-valid-rename', 'R003', 'PUT', '/api/client-devices', 200,
        ('R003-S08',), document={**body, 'name': '模式不变改名🧪', 'previous_name': body['name'], 'previous_mode': 'direct'},
        unchanged=False, effect='profile-write', retained_fields=('oxidns', 'routers', 'client_fixed_ips'),
        checks={'devices': [{**row, 'name': '模式不变改名🧪', 'mode': 'direct'}]})
