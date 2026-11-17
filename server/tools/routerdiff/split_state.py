"""Split diagnostic fallback observed through the real Linux gateway editor."""
import json

from cases import Case
import split


def variants():
    result = []
    for label, value in (('null', None), ('bool', False), ('number', 42), ('array', []), ('string', 'scalar')):
        result.append(('root-' + label, json.dumps(value), ('H0108',), ['tun_missing']))
    result.append(('old-gateway-marker', json.dumps({**split.base(), 'tun': split.full_tun(),
                   'x-configflow-linux-gateway': None}), ('H0108',), ['tun_missing']))
    result.append(('missing-tun', json.dumps(split.base()), ('H0109',), ['tun_missing']))
    for label, tun in (('null', None), ('bool', False), ('number', 1), ('array', []), ('string', 'tun')):
        result.append(('tun-' + label, json.dumps({**split.base(), 'tun': tun}), ('H0109',), ['tun_missing']))
    for label, value in (('syntax', 'a: ['), ('recursive-depth', '[' * 1500 + '0' + ']' * 1500),
                         ('unsafe-tag', '!!python/object:synthetic {}')):
        result.append(('yaml-' + label, value, ('H0108', 'H0114', 'H0115'), ['tun_missing']))
    result.append(('valid-full-tun', json.dumps({**split.base(), 'tun': split.full_tun()}),
                   ('H0108', 'H0109', 'H0114', 'H0115'), []))
    for key in ('include-interface', 'exclude-interface', 'include-uid', 'include-uid-range',
                'exclude-uid', 'exclude-uid-range', 'include-mac-address', 'exclude-mac-address',
                'include-android-user', 'include-package', 'exclude-package'):
        result.append(('filter-' + key,
                       json.dumps({**split.base(), 'tun': {**split.full_tun(), key: ['synthetic-filter']}}),
                       (), ['tun_filtered']))
    return result


def worlds():
    result = []
    for label, text, helpers, issues in variants():
        options = split.options(split.marker())
        options['alpha']['mihomo']['custom_config'] = text
        items = (Case('G23/editor-state-' + label, '', 'GET',
            '/api/profiles/alpha/custom-config/linux-gateway', 200, ('G23', *helpers),
            boundary=('GET', '/api/profiles/<profile_id>/custom-config/linux-gateway'),
            owner='R02（router_split）及 Linux 网关编辑器 HTTP',
            checks={'/router_split': {'mode': 'full_tun', 'source': 'marker', 'issues': issues,
                                     'route_rule_set_ids': []}},
            header_checks={'Cache-Control': 'no-store'}),)
        result.append(('split-state-' + label, options, (lambda items=items: items,)))
    return result
