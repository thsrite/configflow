"""Router-split guards at the real configuration and Agent generation APIs."""
import copy
import json

from cases import Case
from seed import AGENT_TOKEN

INVALID = '主路由分流标记 mihomo.router_split 无效，已停止生成和网关操作。请从备份恢复，或重新运行首次使用向导。'
CONFLICT = ('此配置空间使用 Mihomo + OxiDNS 分流（完整 TUN），主路由转来的连接已全部交给 Mihomo，'
            '不能再开启 Linux 网关的选择性接管。')
MISSING = ('主路由分流需要完整 TUN，但生成的 Mihomo 配置未启用（基础 YAML 缺少 tun 或被修改）。'
           '请在基础 YAML 中恢复 tun 后再发布，否则主路由转来的连接会回环。')


def marker():
    return {'version': 1, 'takeover': 'full_tun', 'origin': 'setup', 'route_rule_set_ids': []}


def base():
    return {'ipv6': False, 'dns': {'enable': True, 'enhanced-mode': 'fake-ip', 'fake-ip-range': '198.18.0.1/16'}}


def full_tun():
    return {'enable': True, 'stack': 'mixed', 'auto-route': True, 'auto-redirect': True,
            'auto-detect-interface': True, 'dns-hijack': ['any:53', 'tcp://any:53']}


def options(value, document=None, gateway=False):
    return {'alpha': {'mihomo': {'router_split': copy.deepcopy(value),
                'custom_config': json.dumps(document if document is not None else base()),
                'linux_gateway': {'enabled': gateway}}},
            'system': {'agents': [{'id': 'split-agent', 'name': '合成分流 Agent', 'service_type': 'mihomo',
                'profile_id': 'alpha', 'host': '10.44.0.2', 'port': 18083, 'token': AGENT_TOKEN,
                'enabled': False, 'status': 'offline', 'version': '1.99.0-go', 'deployment_method': 'docker'}]}}


def config_case(label, helper, message):
    return (Case('G23/config-' + label, '', 'POST', '/api/generate/mihomo/preview', 500, ('G23', helper),
        boundary=('POST', '/api/generate/mihomo/preview'), owner='R02（router_split）及配置生成 HTTP',
        document={'base_url': 'http://panel.example'}, message=message),)


def agent_case(label, helper, message):
    return (Case('G23/agent-' + label, '', 'POST', '/api/agents/split-agent/generate-config', 400, ('G23', helper),
        boundary=('POST', '/api/agents/<agent_id>/generate-config'), owner='R02（router_split）及 Agent 最终配置生成',
        document={'base_url': 'http://panel.example', 'expected_profile_id': 'alpha', 'expected_service_type': 'mihomo'},
        message=message, expected_requests=({'match': {'peer': 'agent'}, 'count': 0},)),)


def worlds():
    result = []
    for label, value in (('marker-null', None), ('marker-bool', False), ('marker-array', []),
                         ('marker-empty', {}), ('marker-extra', {**marker(), 'extra': True}),
                         ('marker-version-bool', {**marker(), 'version': True}),
                         ('marker-version-float', {**marker(), 'version': 1.0}),
                         ('marker-version-string', {**marker(), 'version': '1'}),
                         ('marker-takeover', {**marker(), 'takeover': 'legacy'}),
                         ('marker-origin', {**marker(), 'origin': 'custom'}),
                         ('marker-version-zero', {**marker(), 'version': 0}),
                         ('marker-missing-key', {key: value for key, value in marker().items() if key != 'origin'}),
                         *[('marker-ids-' + label, {**marker(), 'route_rule_set_ids': value})
                           for label, value in (('null', None), ('bool', False), ('object', {}), ('string', ''),
                                                ('1025', ['ip-set'] * 1025), ('empty', ['']), ('null-row', [None]),
                                                ('bool-row', [True]), ('number-row', [1]), ('array-row', [[]]),
                                                ('object-row', [{}]), ('257', ['行' * 257]))],
                         *[('marker-control-' + str(number), {**marker(), 'route_rule_set_ids': ['ip' + chr(number)]})
                           for number in (*range(32), 127)]):
        items = config_case(label, 'H0110', INVALID)
        result.append(('split-' + label, options(value), (lambda items=items: items,)))
    items = config_case('full-tun-gateway-conflict', 'H0111', CONFLICT)
    result.append(('split-gateway-conflict', options(marker(), gateway=True), (lambda items=items: items,)))
    missing = [('missing-tun', None)]
    missing += [('tun-' + label, value) for label, value in
                (('null', None), ('bool', False), ('array', []), ('number', 1), ('string', 'tun'))]
    missing += [(key + '-' + label, {**full_tun(), key: value})
                for key in ('enable', 'auto-route', 'auto-redirect')
                for label, value in (('missing', None), ('false', False), ('one', 1), ('string', 'true'))]
    missing += [('hijack-' + label, {**full_tun(), 'dns-hijack': value})
                for label, value in (('null', None), ('string', 'any:53'), ('object', {}),
                                     ('empty', []), ('tcp-only', ['tcp://any:53']), ('wrong-case', ['ANY:53']))]
    missing += [('old-route-' + key, {**full_tun(), key: []}) for key in
                ('route-address', 'inet4-route-address', 'inet6-route-address',
                 'route-address-set', 'route-exclude-address-set')]
    for label, tun in missing:
        document = base()
        if label != 'missing-tun':
            document['tun'] = tun
        if label.endswith('-missing'):
            document['tun'].pop(label.removesuffix('-missing'))
        items = agent_case('full-tun-' + label, 'H0112', MISSING)
        result.append(('split-agent-' + label, options(marker(), document), (lambda items=items: items,)))
    document = {**base(), 'tun': full_tun(), 'x-configflow-linux-gateway': None}
    items = agent_case('full-tun-gateway-marker', 'H0112', MISSING)
    result.append(('split-agent-gateway-marker', options(marker(), document), (lambda items=items: items,)))
    filters = ('include-interface', 'exclude-interface', 'include-uid', 'include-uid-range',
               'exclude-uid', 'exclude-uid-range', 'include-mac-address', 'exclude-mac-address',
               'include-android-user', 'include-package', 'exclude-package')
    for keys in (*[(key,) for key in filters], filters):
        label = keys[0] if len(keys) == 1 else 'all'
        document = {**base(), 'tun': {**full_tun(), **{key: ['synthetic-filter'] for key in keys}}}
        items = agent_case('full-tun-filtered-' + label, 'H0113',
            '主路由分流的完整 TUN 不能带 tun.' + '、tun.'.join(keys)
            + ' 过滤：未进入 TUN 的连接会在主路由与 Linux 之间回环。请在 Mihomo 基础 YAML 中移除后再发布。')
        result.append(('split-agent-filter-' + label, options(marker(), document), (lambda items=items: items,)))
    # Public preview is the positive control for marker acceptance. Agent final
    # generation writes a preview record, so its success is a separate workflow.
    for label, value in (('empty', []), ('1024-duplicate', ['ip-set'] * 1024), ('unicode256', ['行' * 256])):
        document = {**base(), 'tun': full_tun()}
        items = (Case('G23/config-valid-' + label, '', 'POST', '/api/generate/mihomo/preview', 200,
            ('G23', 'H0110', 'H0111'), boundary=('POST', '/api/generate/mihomo/preview'),
            owner='R02（router_split）及配置生成 HTTP', document={'base_url': 'http://panel.example'},
            absent=('success',), formats={'/content': r'(?s).+tun:.+'}),)
        result.append(('split-valid-' + label, options({**marker(), 'route_rule_set_ids': value}, document),
                       (lambda items=items: items,)))
    return result
