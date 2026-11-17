"""Agent return-routing safety negotiation and ordered public publication."""
import copy
from dataclasses import replace

from cases import Case, Ref
from fixtures import heartbeat
from seed import AGENT_TOKEN
import releases


BASE = '/api/routers/r-ros'
STATE = 'agent/10.44.0.2:18083/endpoints/~1api~1return-routing'
ALLOCATION = {'version': 1, 'table': 104, 'priority': 105, 'mark': '0x400000', 'reused': False}
FOREIGN_TABLE = {'family': 'inet', 'name': 'synthetic_foreign', 'marks': ['0x1']}
FOREIGN_RULE = {'priority': '55', 'mark': '0x1/0x1', 'table': '55'}


def options():
    value = releases.options()
    value['alpha']['routers']['items'][0]['manage_return_routing'] = True
    return value


def status(protocol=2):
    return {'success': True, 'managed': None, 'return_tables': [copy.deepcopy(FOREIGN_TABLE)],
            'fwmark_rules': [copy.deepcopy(FOREIGN_RULE)],
            'safety': {'version': 1, 'ok': True, 'blockers': [],
                       'allocation': copy.deepcopy(ALLOCATION), 'protocols': [1, 2] if protocol == 2 else [1]}}


def peer(value):
    return {'action': 'seed', 'agent': {'10.44.0.2:18083': {'token': AGENT_TOKEN + '-gateway',
                'endpoints': {'/api/return-routing': copy.deepcopy(value)}}}}


def flow(protocol):
    label = 'protocol' + str(protocol)
    preview = 'R012/return-' + label
    spec = {'router_ip': '10.44.0.1', 'table': 104, 'priority': 105, 'mark': '0x400000', 'safety_version': protocol}
    if protocol == 1:
        spec['proxy_dst'] = ['198.18.0.0/16']
    result = [Case(preview, 'R012', 'POST', BASE + '/plan', 200, ('G03', 'D27'),
        prepare=(peer(status(protocol)), heartbeat('gateway')), unchanged=False, effect='device-read',
        checks={'plan/blockers': [], 'plan/stages/0/key': 'return_routing', 'plan/stages/0/enabled': True,
                'plan/stages/0/ops/0/spec': spec}, formats={'plan/fingerprint': r'[0-9a-f]{16}'})]
    result.append(Case('R011/return-' + label, 'R011', 'POST', BASE + '/apply', 200,
        ('R011-S03' if protocol == 1 else 'R011-S04', 'G03', 'D27'), requires=('R012',),
        document={'fingerprint': Ref(preview, '/body/plan/fingerprint')}, unchanged=False, effect='profile-write-device',
        checks={'success': True, 'result/ok': True, 'result/log/0/stage': 'return_routing'},
        fake_checks={STATE + '/managed/spec': {'proxy_dst': None, **spec}, STATE + '/managed/active': True,
                     STATE + '/return_tables/0': FOREIGN_TABLE, STATE + '/fwmark_rules/0': FOREIGN_RULE},
        expected_requests=({'match': {'peer': 'agent', 'method': 'PUT', 'path': '/api/return-routing'}, 'count': 1},),
        request_order=({'peer': 'agent', 'method': 'PUT', 'path': '/api/return-routing'},
                       {'peer': 'ros', 'method': 'PUT', 'path': '/rest/ip/route'})))
    result.append(Case('R012/return-noop-' + label, 'R012', 'POST', BASE + '/plan', 200, ('G03',),
        unchanged=False, effect='device-read', checks={'plan/blockers': [], 'plan/empty': True,
            'plan/stages/0/enabled': True, 'plan/stages/0/ops': []}))
    result.append(Case('R010/return-disable-' + label, 'R010', 'PUT', BASE, 200, ('G03',),
        document={'manage_return_routing': False}, unchanged=False, effect='profile-write',
        checks={'router/manage_return_routing': False}))
    cleanup = 'R012/return-cleanup-' + label
    result.append(Case(cleanup, 'R012', 'POST', BASE + '/plan', 200, ('G03', 'D27'),
        unchanged=False, effect='device-read', checks={'plan/blockers': [], 'plan/empty': False,
            'plan/stages/0/enabled': False, 'plan/stages/0/ops': [],
            'plan/stages/4/key': 'return_cleanup', 'plan/stages/4/ops/0/kind': 'return_routing_remove'}))
    result.append(Case('R011/return-cleanup-' + label, 'R011', 'POST', BASE + '/apply', 200,
        ('R011-S05', 'G03', 'D27'), requires=('R012', 'R010'),
        document={'fingerprint': Ref(cleanup, '/body/plan/fingerprint')}, unchanged=False, effect='profile-write-device',
        checks={'success': True, 'result/log/-1/stage': 'return_cleanup'},
        fake_checks={STATE + '/managed/active': False, STATE + '/managed/table_present': False,
                     STATE + '/return_tables': [FOREIGN_TABLE], STATE + '/fwmark_rules': [FOREIGN_RULE]},
        expected_requests=({'match': {'peer': 'agent', 'method': 'DELETE', 'path': '/api/return-routing'}, 'count': 1},)))
    result.append(Case('R012/return-cleaned-noop-' + label, 'R012', 'POST', BASE + '/plan', 200, ('G03',),
        unchanged=False, effect='device-read', checks={'plan/blockers': [], 'plan/empty': True}))
    return result


def protocol1():
    yield from flow(1)


def protocol2():
    yield from flow(2)


def invalid_safety():
    yield Case('R012/return-offline-before-heartbeat', 'R012', 'POST', BASE + '/plan', 200,
        ('G03', 'D27', 'H0610'), unchanged=False, effect='device-read',
        checks={'plan/blockers': ['无法读取 mihomo Agent 的回流规则状态：Agent 合成代理网关🧪 离线'], 'plan/stages': []},
        expected_requests=({'match': {'peer': 'agent'}, 'count': 0},))
    definitions = []
    unsupported = 'mihomo Agent 不支持回流占用安全检查，请先升级 Agent，或关闭自动配置回流后继续发布'
    incomplete = 'mihomo Agent 的回流安全检查结果不完整，无法确认规则归属'
    invalid_allocation = 'mihomo Agent 未提供有效的回流资源自动分配结果，请先升级 Agent，或关闭自动配置回流后继续发布'
    for value in (None, [], {}, {'version': True}, {'version': 0}, {'version': 2}, {'version': '1'}):
        definitions.append(('safety-version-' + str(len(definitions)), value, unsupported, 'H0611'))
    for field, values in (('ok', (None, 1, 'true', [], {})),
                          ('blockers', (None, 'blocked', {}, [None], [''], ['  '], [False]))):
        for index, value in enumerate(values):
            safety = status()['safety']
            safety[field] = value
            definitions.append(('safety-' + field + '-' + str(index), safety, incomplete, 'H0611'))
    for ok, blockers in ((False, []), (False, ['synthetic occupied table']), (True, ['synthetic foreign mark'])):
        safety = status()['safety']
        safety.update(ok=ok, blockers=blockers)
        definitions.append(('safety-refusal-' + str(len(definitions)), safety,
                            blockers[0] if blockers else 'mihomo Agent 未确认回流配置可安全使用，停止发布', 'H0611'))
    for field, values in (('version', (None, True, 0, 2, '1')),
                          ('reused', (None, 0, 'false')),
                          ('table', (None, False, 0, 253, '104', 1.5)),
                          ('priority', (None, True, 0, 32766, '105', 1.5)),
                          ('mark', (None, 1, '', '400000', '0x', '0x100000000', '0xg', '0x0', '0x3'))):
        for index, value in enumerate(values):
            safety = status()['safety']
            safety['allocation'][field] = value
            helper = 'H0600' if field != 'mark' else 'H0602' if value in ('0x0', '0x3') else 'H0601'
            definitions.append(('allocation-' + field + '-' + str(index), safety, invalid_allocation, helper))
    for value in (None, [], {}, 'unsupported'):
        safety = status()['safety']
        safety['allocation'] = value
        definitions.append(('allocation-shape-' + str(len(definitions)), safety, invalid_allocation, 'H0600'))
    for label, safety, message, helper in definitions:
        value = status()
        value['safety'] = safety
        yield Case('R012/return-' + label, 'R012', 'POST', BASE + '/plan', 200,
            ('G03', 'D27', helper), prepare=(peer(value), heartbeat('gateway')),
            unchanged=False, effect='device-read', checks={'plan/blockers': [message], 'plan/stages': [],
                                                         'plan/fingerprint': '', 'plan/empty': True},
            expected_requests=({'match': {'peer': 'agent', 'method': 'PUT'}, 'count': 0},
                               {'match': {'peer': 'agent', 'method': 'DELETE'}, 'count': 0}))


def valid_allocation_boundaries():
    for label, changes, normalized in (
        ('table-low', {'table': 1}, {}), ('table-high', {'table': 252}, {}),
        ('priority-low', {'priority': 1}, {}), ('priority-high', {'priority': 32765}, {}),
        ('mark-low', {'mark': '0x1'}, {}), ('mark-high', {'mark': '0x80000000'}, {}),
        ('mark-padded-uppercase', {'mark': '0X00008000'}, {'mark': '0x8000'}),
        ('allocation-reused', {'reused': True}, {}),
    ):
        value = status()
        value['safety']['allocation'].update(changes, unknown_native_allocation='must-not-leak-into-spec')
        allocated = {**ALLOCATION, **changes, **normalized}
        expected = {'router_ip': '10.44.0.1', 'table': allocated['table'], 'priority': allocated['priority'],
                    'mark': allocated['mark'], 'safety_version': 2}
        yield Case('R012/return-boundary-' + label, 'R012', 'POST', BASE + '/plan', 200,
            ('G03', 'D27'), prepare=(peer(value), heartbeat('gateway')), unchanged=False, effect='device-read',
            checks={'plan/blockers': [], 'plan/stages/0/ops/0/spec': expected},
            absent=('plan/stages/0/ops/0/spec/proxy_dst',))


def route_failure_rolls_back_return():
    preview = 'R012/return-before-route-failure'
    yield Case(preview, 'R012', 'POST', BASE + '/plan', 200, ('D27', 'T09'),
        prepare=(peer(status()), heartbeat('gateway')), unchanged=False, effect='device-read',
        checks={'plan/blockers': [], 'plan/stages/0/ops/0/kind': 'return_routing_apply'})
    yield Case('R011/route-failure-removes-new-return', 'R011', 'POST', BASE + '/apply', 502,
        ('R011-F17', 'D27', 'T09', 'T13'), requires=('R012',),
        document={'fingerprint': Ref(preview, '/body/plan/fingerprint')},
        prepare=({'action': 'fault', 'match': {'peer': 'ros', 'method': 'PUT', 'path': '/rest/ip/route'},
                  'status': 400, 'body': {'message': 'synthetic route write refusal after return'}},),
        unchanged=False, effect='profile-write-device',
        checks={'success': False, 'result/failed_stage': 'routes', 'result/rollback/0/stage': 'return_routing',
                'result/rollback/0/status': 'ok'}, stored={'routers/items/0/last_release/ok': False},
        fake_checks={STATE + '/managed/active': False, STATE + '/managed/table_present': False,
                     STATE + '/return_tables': [FOREIGN_TABLE], STATE + '/fwmark_rules': [FOREIGN_RULE]},
        fake_lengths={'ros/~1ip~1route': 0, 'ros/~1ip~1firewall~1nat': 0},
        expected_requests=({'match': {'peer': 'agent', 'method': 'PUT'}, 'count': 1},
                           {'match': {'peer': 'agent', 'method': 'DELETE'}, 'count': 1}),
        request_order=({'peer': 'agent', 'method': 'PUT'}, {'peer': 'ros', 'method': 'PUT', 'path': '/rest/ip/route'},
                       {'peer': 'agent', 'method': 'DELETE'}))


def uncertain_route_failure():
    preview = 'R012/return-before-uncertain-route-failure'
    for case in route_failure_rolls_back_return():
        if case.route == 'R012':
            yield replace(case, ident=preview)
        else:
            controls = tuple({**control, 'status': 500} for control in case.prepare)
            checks = {**case.checks, 'result/rollback/0/stage': 'routes', 'result/rollback/0/status': 'failed',
                      'result/rollback/1/stage': 'return_routing', 'result/rollback/1/status': 'ok'}
            yield replace(case, ident='R011/uncertain-route-failure-cleans-known-return', prepare=controls,
                document={'fingerprint': Ref(preview, '/body/plan/fingerprint')}, checks=checks,
                entries=('R011-F18', 'R011-F19', 'D27', 'T09', 'T13'),
                message='阶段「routes」失败，回滚未完成，请检查发布记录')
