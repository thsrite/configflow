"""Public release preview/confirmation sequences, with actual readback."""
from cases import Case, Ref
from seed import router, AGENT_TOKEN


def options():
    return {'system': {'agents': [{'id': 'gateway', 'name': '合成代理网关🧪', 'service_type': 'mihomo',
                'profile_id': 'alpha', 'host': '10.44.0.2', 'port': 18083, 'status': 'offline',
                'version': '1.99.0-go', 'enabled': True, 'token': AGENT_TOKEN + '-gateway'}]},
            'alpha': {'routers': {'items': [router(proxy_gateway_agent_id='gateway'),
                                          router('r-ik', 'ikuai', proxy_gateway_agent_id='gateway')]}},
            'beta': {'routers': {'items': [router()]}}}


def _basic(vendor, ident, preparation=()):
    path = '/api/routers/' + ident
    plan = 'R012/fake-ip-preview-' + vendor
    result = [Case(plan, 'R012', 'POST', path + '/plan', 200,
               ('R012-S01',) if vendor == 'routeros' else ('R012-S02',),
               unchanged=False, effect='device-read', prepare=preparation,
               checks={'success': True, 'plan/blockers': [], 'plan/empty': False},
               formats={'plan/fingerprint': r'[0-9a-f]{16}'})]
    for label, body in (('missing', {}), ('wrong', {'fingerprint': 'wrong'})):
        result.append(Case('R011/fingerprint-' + label + '-' + vendor, 'R011', 'POST', path + '/apply', 409,
                   ('H1309',), document=body, unchanged=False, effect='device-read',
                   message='路由器、Agent 或期望状态在预览后发生了变化，请重新预览'))
    result.append(Case('R011/fake-ip-publish-' + vendor, 'R011', 'POST', path + '/apply', 200,
               ('R011-S01',) if vendor == 'routeros' else ('R011-S02',),
               document={'fingerprint': Ref(plan, '/body/plan/fingerprint')}, unchanged=False,
               effect='profile-write-device', checks={'success': True, 'result/ok': True}, requires=('R012',),
               stored={'routers/items/' + ('0' if vendor == 'routeros' else '1') + '/last_release/ok': True}))
    noop = 'R012/noop-preview-' + vendor
    result.append(Case(noop, 'R012', 'POST', path + '/plan', 200, ('R012-B90', 'R012-S11'), unchanged=False, effect='device-read',
               checks={'success': True, 'plan/blockers': [], 'plan/empty': True},
               formats={'plan/fingerprint': r'[0-9a-f]{16}'}))
    result.append(Case('R011/noop-publish-' + vendor, 'R011', 'POST', path + '/apply', 200,
               ('R011-S16',), document={'fingerprint': Ref(noop, '/body/plan/fingerprint')}, unchanged=False,
               effect='profile-write', checks={'success': True, 'result/ok': True, 'result/log': [], 'result/rollback': []},
               requires=('R012',)))
    return result


def routeros_basic():
    yield from _basic('routeros', 'r-ros')


def ikuai_plain():
    # A separate, explicitly named counterfactual variant. The default fake
    # retains the real display suffix; never relax it globally for a green run.
    yield from _basic('ikuai', 'r-ik', ({'action': 'seed', 'presentation': {'static_mask_suffix': False}},))


def ikuai_suffix_bug():
    plan = 'R012/iKuai-real-suffix-preview'
    yield Case(plan, 'R012', 'POST', '/api/routers/r-ik/plan', 200, ('R012-S02',),
               unchanged=False, effect='device-read', checks={'success': True, 'plan/blockers': []},
               formats={'plan/fingerprint': r'[0-9a-f]{16}'},
               fake_checks={'presentation/static_mask_suffix': True})
    yield Case('R011-S02/real-suffix-python-bug', 'R011', 'POST', '/api/routers/r-ik/apply', 502,
               ('R011-S02',), document={'fingerprint': Ref(plan, '/body/plan/fingerprint')}, unchanged=False,
               effect='profile-write-device', message='阶段「routes」失败，回滚未完成，请检查发布记录',
               checks={'success': False, 'result/ok': False, 'result/failed_stage': 'routes',
                       'result/rollback/0/status': 'failed'},
               stored={'routers/items/1/last_release/ok': False, 'routers/items/1/last_release/failed_stage': 'routes'},
               fake_lengths={'ikuai/static_rt': 1},
               fake_checks={'ikuai/static_rt/0/dst_addr': '198.18.0.0', 'ikuai/static_rt/0/gateway': '10.44.0.2'},
               python_bug='IKUAI_NETMASK_SUFFIX_ROLLBACK', requires=('R012',))
