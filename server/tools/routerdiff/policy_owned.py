"""Literal managed native policy inputs; ownership tested only by public HTTP."""
import copy

from cases import Case
import policy_graph

POLICY = 'cf_device_policy'
DIRECT = 'cf_device_direct'
ROWS = [{'ip': '10.44.0.21', 'name': '历史兼容备注', 'mode': 'direct'}]


def document():
    value = policy_graph.document()
    value['plugins'][2]['args'].insert(0, {'exec': 'jump cf_device_policy'})
    value['plugins'] += [
        {'tag': POLICY, 'type': 'sequence', 'args': [
            {'matches': ['client_ip 10.44.0.21'], 'exec': 'goto cf_device_direct'}, {'exec': 'return'}]},
        {'tag': DIRECT, 'type': 'sequence', 'args': [
            {'exec': '$forward_local'}, {'matches': ['!has_resp'], 'exec': 'reject 3'}, {'exec': 'accept'}]}]
    return value


def options(value, rows):
    result = policy_graph.options(value)
    result['alpha']['oxidns']['device_policies'] = copy.deepcopy(rows)
    return result


def variants():
    changed = '设备策略执行链已被手动修改，请先在完整参数中恢复，避免覆盖自定义内容。'
    position = '设备策略的位置已被修改，请先恢复到分流规则之前。'
    conflict = '完整参数中存在同名设备策略插件，请先处理命名冲突。'
    extra = '设备策略插件存在额外引用，请先在完整参数中处理。'
    result = []
    for label, index in (('missing-policy', 4), ('missing-direct', 5)):
        value = document()
        value['plugins'].pop(index)
        result.append((label, value, ROWS, 'H0096', changed))
    for label, index in (('changed-policy', 4), ('changed-direct', 5)):
        value = document()
        value['plugins'][index]['unknown_custom_field'] = 'must not overwrite'
        result.append((label, value, ROWS, 'H0096', changed))
    value = document()
    value['plugins'][2]['args'].pop(0)
    result.append(('missing-hook', value, ROWS, 'H0096', changed))
    value = document()
    value['plugins'][2]['args'].insert(0, {'exec': 'jump cf_device_policy'})
    result.append(('duplicate-hook', value, ROWS, 'H0096', changed))
    value = document()
    value['plugins'][2]['args'][0]['matches'] = ['client_ip 10.44.0.21']
    result.append(('conditional-hook', value, ROWS, 'H0096', changed))
    value = document()
    value['plugins'][2]['args'].insert(1, {'exec': 'prefer_ipv4'})
    result.append(('hook-before-builtin', value, ROWS, 'H0097', position))
    value = document()
    value['plugins'][2]['args'] = [{'exec': 'prefer_ipv4'}, {'exec': '$forward_local'},
                                   {'exec': 'jump cf_device_policy'}, {'exec': 'goto china_dns'}]
    result.append(('hook-after-custom-step', value, ROWS, 'H0097', position))
    for label, tags, hook in (('policy-only', (POLICY,), False), ('direct-only', (DIRECT,), False),
                              ('hook-only', (), True), ('all-unrecorded', (POLICY, DIRECT), True)):
        value = policy_graph.document()
        value['plugins'] += [copy.deepcopy(item) for item in document()['plugins'] if item['tag'] in tags]
        if hook:
            value['plugins'][2]['args'].insert(0, {'exec': 'jump cf_device_policy'})
        result.append((label, value, [], 'H0098', conflict))
    for label, action in (('goto-policy-without-hook', 'goto cf_device_policy'),
                           ('goto-direct-without-owned', 'goto cf_device_direct')):
        value = policy_graph.document()
        value['plugins'][2]['args'].insert(0, {'exec': action})
        result.append((label, value, [], 'H0099', extra))
    value = document()
    value['plugins'][2]['args'].insert(1, {'exec': 'jump cf_device_direct'})
    result.append(('recorded-direct-extra-call', value, ROWS, 'H0099', extra))
    return result


def guard_cases(label, rows, helper, reason):
    mode = 'direct' if rows else ''
    row = {'ip': '10.44.0.21', 'name': '现存备注🧪', 'mode': mode}
    body = {'profile_id': 'alpha', **row, 'previous_name': row['name'], 'previous_mode': mode, 'base_url': ''}
    return (
        Case('R002/native-owned-' + label, 'R002', 'GET', '/api/client-devices', 200,
            ('R002-F12', helper, 'H0100', 'H0101'), checks={'editable': False, 'reason': reason, 'devices': [row]}),
        Case('R003/native-owned-mode-refused-' + label, 'R003', 'PUT', '/api/client-devices', 400,
            ('R003-F15', helper, 'H1261', 'H1262'), document={**body, 'name': 'must not half-save',
                                          'mode': 'smart' if rows else 'direct'}, message=reason),
        Case('R003/native-owned-note-only-' + label, 'R003', 'PUT', '/api/client-devices', 200,
            ('R003-S08',), document={**body, 'name': '仍可改名🧪'}, unchanged=False, effect='profile-write',
            retained_fields=('oxidns', 'routers', 'client_fixed_ips'),
            checks={'editable': False, 'devices': [{**row, 'name': '仍可改名🧪'}]}),
        Case('R014/native-owned-readonly-' + label, 'R014', 'GET', '/api/routers/r-ros/policy-settings', 200,
            (helper, 'H0100', 'H0101'), checks={'workspace/sources/editable': False, 'workspace/sources/reason': reason,
                'workspace/sources/policies': [{**row, 'name': '仍可改名🧪'}] if rows else []}),
    )


def worlds():
    result = []
    for label, value, rows, helper, reason in variants():
        items = guard_cases(label, rows, helper, reason)
        result.append(('native-owned-' + label, options(value, rows), (lambda items=items: items,)))
    return result


def valid():
    row = {'ip': '10.44.0.21', 'name': '现存备注🧪', 'mode': 'direct'}
    yield Case('R002/native-owned-valid-recorded', 'R002', 'GET', '/api/client-devices', 200,
        ('R002-S01', 'H0096', 'H0097', 'H0100', 'H0101'), checks={'editable': True, 'reason': '', 'devices': [row]})
    body = {'profile_id': 'alpha', 'ip': row['ip'], 'name': '保留备注🧪', 'mode': 'smart',
            'previous_name': row['name'], 'previous_mode': 'direct', 'base_url': ''}
    yield Case('R003/native-owned-direct-to-smart-withdraws-plugins', 'R003', 'PUT', '/api/client-devices', 200,
        ('R003-S02', 'H0096', 'H0097', 'H0099'), document=body, unchanged=False, effect='profile-write',
        checks={'editable': True, 'devices': [{**row, 'name': body['name'], 'mode': 'smart'}]},
        stored={'oxidns/device_policies': [{'ip': row['ip'], 'name': body['name'], 'mode': 'smart'}]},
        stored_excludes={'oxidns/native_config': (POLICY, DIRECT)})
    yield Case('R003/native-owned-smart-note-only-preserves-native', 'R003', 'PUT', '/api/client-devices', 200,
        ('R003-S08',), document={**body, 'name': '只改备注🧪', 'previous_name': body['name'], 'previous_mode': 'smart'},
        unchanged=False, effect='profile-write', retained_fields=('oxidns', 'routers', 'client_fixed_ips'),
        checks={'devices': [{**row, 'name': '只改备注🧪', 'mode': 'smart'}]})
