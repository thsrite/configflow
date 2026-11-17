"""Router-linked Agent forwarding guards, using public registration storage/HB."""
import copy
from dataclasses import replace

from cases import Case
from fixtures import agent_options, agent_peers, heartbeat
from seed import router

UPGRADE = '请先将 Mihomo Agent 更新到 1.8.41-go 或更新版本，才能在此开启并持久化 IPv4 转发。'
STANDALONE = '单独安装的 Mihomo Agent 需更新到 1.8.41-go 或更新版本才能开启并持久化 IPv4 转发。'


def options(patch=None, missing=False):
    agent = copy.deepcopy(agent_options()['system']['agents'][0])
    agent.update(patch or {})
    return {'system': {'agents': [] if missing else [agent]},
            'alpha': {'routers': {'items': [router(proxy_gateway_agent_id='m-a')]}}}


def refusal(label, helper, state, status, code, message, online=False):
    prepare = (heartbeat('m-a'),) if online else ()
    return (Case('R025/forwarding-guard-' + label, 'R025', 'GET', '/api/routers/gateway-forwarding', 200,
        ('R025-F10',) + (('H0001',) if helper == 'H0001' else ()), prepare=prepare,
        checks={'/gateways/0/state': state, '/gateways/0/can_persist': False,
                '/gateways/0/message': 'Agent 离线。' if state == 'offline' else message},
        expected_requests=({'match': {'peer': 'agent'}, 'count': 0},)),
        Case('R026/forwarding-guard-' + label, 'R026', 'POST', '/api/routers/gateway-forwarding', status,
        ('R026-F12', helper), document={'agent_id': 'm-a'}, prepare=prepare, message=message,
        checks={'/success': False, '/code': code}, expected_requests=({'match': {'peer': 'agent'}, 'count': 0},)))


def worlds():
    result = []
    definitions = (
        ('missing', {}, True, 'H0018', 'missing', 404, 'AGENT_NOT_FOUND', '关联的 Agent 已删除，请在路由器设置中重新选择代理网关。'),
        ('wrong-service', {'service_type': 'oxidns'}, False, 'H0019', 'not_mihomo', 400, 'WRONG_SERVICE',
         '代理网关不是 Mihomo Agent。'),
        ('docker', {'deployment_method': 'docker'}, False, 'H0020', 'docker', 409, 'UNSUPPORTED_ENVIRONMENT',
         'Docker 部署的 Agent 不管理宿主机网络，请在宿主机上开启并持久化 IPv4 转发。'),
        ('offline', {}, False, 'H0021', 'offline', 409, 'AGENT_OFFLINE', 'Agent 离线，无法开启转发。'),
    )
    for label, patch, missing, helper, state, status, code, message in definitions:
        items = refusal(label, helper, state, status, code, message)
        result.append(('forwarding-guard-' + label, options(patch, missing), (lambda items=items: items,)))
    invalid_versions = (('null', None), ('false', False), ('number', 1), ('array', []), ('object', {}),
        ('empty', ''), ('text', 'version'), ('no-patch', '1.8'), ('four-parts', '1.8.40.0'),
        ('prefix', 'v1.8.40'), ('suffix', '1.8.40-GO'), ('extra-suffix', '1.8.40-go-extra'),
        ('leading-space', ' 1.8.40-go'), ('trailing-space', '1.8.40-go '),
        ('newline', '1.8.40-go\n'), ('fullwidth', '１.８.４０-go'), ('negative', '-1.8.40'))
    for label, value in (*invalid_versions, ('old', '1.8.39-go'), ('old-major', '0.99.99-go'), ('old-minor', '1.7.99-go')):
        malformed = not label.startswith('old')
        items = refusal('version-' + label, 'H0001' if malformed else 'H0022',
                        'upgrade_required', 409, 'UPGRADE_REQUIRED', UPGRADE, online=True)
        if malformed:
            # GET has its own precheck; only POST reaches persist's H0022.
            items = tuple(replace(case, entries=(*case.entries, 'H0022')) if case.method == 'POST' else case
                          for case in items)
        result.append(('forwarding-version-' + label, options({'version': value}), (lambda items=items: items,)))
    for label, missing in (('missing-status', True), ('unsupported-layout', False)):
        peers = agent_peers()
        status = peers['10.44.0.2:18083']['endpoints']['/api/linux-gateway/status']
        if missing:
            status.pop('ipv4_forwarding')
        else:
            status['ipv4_forwarding']['supported'] = False
        items = (Case('R026/forwarding-layout-' + label, 'R026', 'POST', '/api/routers/gateway-forwarding', 409,
            ('H0004', 'R026-F12'), document={'agent_id': 'm-a'},
            prepare=({'action': 'seed', 'agent': peers}, heartbeat('m-a')), unchanged=False, effect='device-read',
            message=STANDALONE, checks={'/code': 'UPGRADE_REQUIRED'},
            expected_requests=({'match': {'peer': 'agent', 'path': '/api/linux-gateway/status'}, 'count': 1},
                               {'match': {'peer': 'agent', 'path': '/api/linux-gateway/forwarding'}, 'count': 0})),)
        result.append(('forwarding-layout-' + label, options({'version': '1.8.40-go'}), (lambda items=items: items,)))
    for label, version, reads in (('aio-1.8.40', '1.8.40-go', 1), ('standalone-1.8.41', '1.8.41-go', 0),
                                  ('suffixless', '1.8.41', 0), ('leading-zeroes', '01.08.041-go', 0),
                                  ('new-major', '2.0.0-go', 0), ('new-minor', '1.9.0-go', 0)):
        peers = agent_peers()
        peers['10.44.0.2:18083']['endpoints']['/api/linux-gateway/status']['ipv4_forwarding']['layout'] = (
            'aio' if reads else 'standalone')
        items = (Case('R026/forwarding-version-accepted-' + label, 'R026', 'POST',
            '/api/routers/gateway-forwarding', 200, ('H0001', 'H0004', 'H0022'), document={'agent_id': 'm-a'},
            prepare=({'action': 'seed', 'agent': peers}, heartbeat('m-a')), unchanged=False, effect='device-only',
            checks={'/success': True, '/gateway/state': 'persisted', '/gateway/current': '1', '/gateway/persisted': True},
            expected_requests=({'match': {'peer': 'agent', 'path': '/api/linux-gateway/status'}, 'count': reads},
                               {'match': {'peer': 'agent', 'path': '/api/linux-gateway/forwarding'}, 'count': 1})),)
        result.append(('forwarding-version-accepted-' + label, options({'version': version}), (lambda items=items: items,)))
    return result
