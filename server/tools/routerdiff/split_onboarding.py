"""Real profile-scoped wizard preview reaches split base preparation guards."""
import json

from cases import Case
import split


def body():
    return {'rule_set_ids': [], 'direct_rule_set_ids': [], 'proxy_rule_set_ids': [],
            'local_dns': 'https://10.44.0.55/dns-query'}


def document():
    value = split.base()
    value['dns']['nameserver'] = ['https://10.44.0.55/dns-query']
    return value


def variants():
    result = []
    for label, tun in (('bool', False), ('number', 1), ('string', 'tun'), ('array', [])):
        result.append(('tun-' + label, json.dumps({**document(), 'tun': tun}),
                       ('H0102',), 'Mihomo tun 必须是对象'))
    filters = ('include-interface', 'exclude-interface', 'include-uid', 'include-uid-range',
               'exclude-uid', 'exclude-uid-range', 'include-mac-address', 'exclude-mac-address',
               'include-android-user', 'include-package', 'exclude-package')
    for key in filters:
        result.append(('filter-' + key, json.dumps({**document(), 'tun': {key: ['synthetic-filter']}}),
            ('H0103',), '主路由分流的完整 TUN 与现有 tun.' + key
            + ' 过滤冲突：未进入 TUN 的连接会在主路由与 Linux 之间回环。请先在 Mihomo 基础 YAML 中移除该过滤'))
    for label, value in (('null', None), ('bool', False), ('number', 1), ('array', []), ('object', {})):
        result.append(('base-type-' + label, value, ('H0104',), 'Mihomo 基础配置必须为 YAML 文本'))
    for label, value in (('syntax', 'a: ['), ('recursive-depth', '[' * 1500 + '0' + ']' * 1500),
                         ('unsafe-tag', '!!python/object:synthetic {}')):
        result.append(('base-yaml-' + label, value, ('H0105', 'H0106'), 'Mihomo 基础 YAML 无法解析，请先修正基础配置'))
    for label, value in (('null', 'null'), ('bool', 'true'), ('number', '42'), ('array', '[]'), ('string', 'scalar')):
        result.append(('base-root-' + label, value, ('H0107',), 'Mihomo 基础配置必须是 YAML 对象'))
    return result


def worlds():
    result = []
    for label, text, helpers, message in variants():
        options = split.options(split.marker())
        options['alpha']['mihomo']['custom_config'] = text
        items = (Case('G23/onboarding-' + label, '', 'POST',
            '/api/profiles/alpha/gateway-onboarding/preview', 200, ('G23', *helpers),
            boundary=('POST', '/api/profiles/<profile_id>/gateway-onboarding/preview'),
            owner='R02（router_split）及网关安装向导 HTTP', document=body(),
            checks={'/success': True, '/ready': False, '/blockers': [message]},
            header_checks={'Cache-Control': 'no-store'}),)
        result.append(('split-onboarding-' + label, options, (lambda items=items: items,)))
    no_proxy = ('当前配置没有被启用规则引用的可用代理出口；请先添加节点或订阅、配置策略组，'
                '并为需要代理的规则选择该策略')
    for label, text in (('base-empty', ''), ('tun-null', json.dumps({**document(), 'tun': None})),
                         ('tun-custom', json.dumps({**document(), 'tun': {'stack': 'gvisor', 'mtu': 1400}}))):
        options = split.options(split.marker())
        options['alpha']['mihomo']['custom_config'] = text
        items = (Case('G23/onboarding-accepted-' + label, '', 'POST',
            '/api/profiles/alpha/gateway-onboarding/preview', 200, ('G23', 'H0102', 'H0104', 'H0107'),
            boundary=('POST', '/api/profiles/<profile_id>/gateway-onboarding/preview'),
            owner='R02（router_split）及网关安装向导 HTTP', document=body(),
            checks={'/success': True, '/ready': False, '/blockers': [no_proxy],
                    '/validation/mihomo/valid': True, '/validation/oxidns/valid': True},
            header_checks={'Cache-Control': 'no-store'}),)
        result.append(('split-onboarding-accepted-' + label, options, (lambda items=items: items,)))
    return result
