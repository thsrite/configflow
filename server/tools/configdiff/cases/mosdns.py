"""MosDNS setting route file: normalization, errors, selectors and write effects."""
import copy

from compare import Policy
from harness import Pair, json_body, DesignError
from cases.helpers import profile_policy, exception_policy, deny_writes


EDITORS = [
    (77, 78, 'api-settings', {'api_enabled': False, 'api_address': '127.0.0.1:18080'}, 'api_address'),
    (79, 80, 'cache-settings', {'cache_enabled': False, 'cache_size': 123, 'cache_lazy_ttl': 456,
        'cache_dump_enabled': False, 'cache_dump_file': './合成🧪.dump', 'cache_dump_interval': 12}, 'cache_dump_file'),
    (81, 82, 'custom-matches', {'custom_matches': ['qname &^example.com$'], 'position': 'head'}, 'position'),
    (83, 84, 'dns-servers', {'local_dns': '192.0.2.53', 'remote_dns': 'https://192.0.2.54/dns-query',
        'fallback_dns': '192.0.2.55', 'default_forward': 'forward_local', 'custom_hosts': 'example.com: 192.0.2.1'}, 'custom_hosts'),
    (85, 86, 'log-settings', {'log_enabled': False, 'log_level': 'warn', 'log_file': './合成🧪.log'}, 'log_file'),
    (88, 89, 'rulesets', {'direct_rulesets': [], 'proxy_rulesets': [], 'direct_rules': ['规则🧪'],
                         'proxy_rules': []}, 'direct_rules')]


def settings(binary, recorder):
    for get_id, post_id, suffix, good, text_field in EDITORS:
        fixture = {'profile': {'rule_configs': [{'id': '规则🧪', 'itemType': 'rule', 'rule_type': 'DOMAIN',
                   'value': 'example.com', 'policy': 'DIRECT', 'enabled': True}]}} if suffix == 'rulesets' else {}
        if fixture:
            fixture['other_profile'] = copy.deepcopy(fixture['profile'])
        with Pair(binary, recorder, options=fixture) as pair:
            route = '/api/mosdns/' + suffix
            policy = profile_policy(pair)
            def call(rid, case, method, body=None, *, path=route, **kwargs):
                return pair.request(f'R{rid:03}-{case}', method + ' ' + route, path,
                                    body=None if body is None else json_body(body), writes=True, policy=policy, **kwargs)
            call(get_id, 'S01', 'GET', expect=200)
            call(post_id, 'S01', 'POST', good, expect=200)
            call(get_id, 'S02', 'GET', expect=200)
            call(post_id, 'S02', 'POST', {}, expect=200)
            array_status = 200 if suffix == 'cache-settings' else 500
            for case in ('F08', 'B02'):
                error_policy = exception_policy(pair)
                error_policy.times.update(policy.times)
                pair.request(f'R{post_id:03}-{case}', 'POST ' + route, body='[]', expect=array_status,
                             writes=True, policy=error_policy if array_status == 500 else policy)
            for case, body, ct in [('F09', '{"bad":', 'application/json'), ('F10', '{}', 'text/plain')]:
                error_policy = exception_policy(pair)
                error_policy.times.update(policy.times)
                pair.request(f'R{post_id:03}-{case}', 'POST ' + route, body=body,
                             headers={'Content-Type': ct}, expect=500, writes=True, policy=error_policy)
            with deny_writes(pair):
                error_policy = exception_policy(pair)
                error_policy.times.update(policy.times)
                pair.request(f'R{post_id:03}-F11', 'POST ' + route, body=json_body(good), expect=500,
                             writes=True, policy=error_policy)
            unicode = good if suffix == 'rulesets' else {**good, text_field: ['中文🧪'] if isinstance(good[text_field], list) else '中文🧪'}
            call(post_id, 'B01', 'POST', unicode, expect=200)
            # Legacy endpoints deliberately permit field values that full editor
            # normalization would reject/convert; GET must expose the stored shape.
            wrong = {text_field: None}
            call(post_id, 'B03', 'POST', wrong, expect=500 if suffix == 'rulesets' else 200)
            call(get_id, 'B01', 'GET', expect=200)
            for rid, method, first in [(get_id, 'GET', 8), (post_id, 'POST', 12)]:
                for n, (value, status) in enumerate([('missing-synthetic', 404), ('bad!id', 400)]):
                    call(rid, f'F{first+n:02}', method, good if method == 'POST' else None,
                         path=route + '?profile=' + value, expect=status)
            for rid, method, first in [(get_id, 'GET', 2), (post_id, 'POST', 4)]:
                # These six legacy setting routes have no explicit-profile URL
                # alias in routes.tsv. P3 is inapplicable; do not invent one.
                selectors = [('?profile=default&profile_id=other', 'other'), ('', 'other'), ('', None)]
                for n, (query, header) in enumerate(selectors):
                    call(rid, f'B{first+n+1:02}', method, good if method == 'POST' else None,
                         path=route + query, headers={'X-ConfigFlow-Profile': header}, expect=200,
                         variant='selector-' + str(n + 4))
    with Pair(binary, recorder, options={'profile': {'mosdns': {'synthetic_extra': '保留🧪', 'api_addr': ':18081'}}}) as pair:
        policy = profile_policy(pair)
        for case, data in [('S01', {'local_dns': '192.0.2.53', 'remote_dns': '192.0.2.54'}),
                           ('S02', {}), ('S03', {'cache_size': ' 12 '}), ('B01', {}),
                           ('B02', {'api_addr': ':1234'}), ('B03', {'cache_dump_interval': -1})]:
            pair.request('R090-' + case, 'POST /api/mosdns/settings', body=json_body(data),
                         expect=200, writes=True, policy=policy)
        pair.request('R090-F08', 'POST /api/mosdns/settings', body='[]', expect=400, writes=True, policy=policy)
        for case, name, status in [('F11', 'missing-synthetic', 404), ('F12', 'bad!id', 400)]:
            pair.request('R090-' + case, 'POST /api/mosdns/settings', '/api/mosdns/settings?profile=' + name,
                         body='{}', expect=status, writes=True, policy=policy)


def numeric_cache(binary, recorder):
    # Literal JSON spellings matter: int, float and exponent are distinct inputs.
    values = [('true', 1), ('3.9', 3), ('" 12 "', 12), ('"1_000"', 1000),
              ('1e400', 10240), ('"inf"', 10240), ('null', 10240)]
    for rid, route in [(80, '/api/mosdns/cache-settings'), (90, '/api/mosdns/settings')]:
        with Pair(binary, recorder) as pair:
            policy = profile_policy(pair)
            for literal, expected in values:
                pair.request(f'R{rid:03}-B90', 'POST ' + route,
                             body='{"cache_size":' + literal + ',"cache_dump_interval":0}',
                             variant=literal, expect=200, writes=True, policy=policy)
                # Assert reference normalization through public GET, never a Go helper.
                from harness import send
                response = send(pair.bases[0], 'GET', '/api/mosdns/cache-settings',
                                headers={'Authorization': 'Bearer ' + pair.tokens[0]})
                if response.json()['cache_size'] != expected or response.json()['cache_dump_interval'] != 300:
                    raise DesignError('Python MosDNS numeric fixture did not reach normalization')


GROUPS = {'mosdns-settings': settings, 'mosdns-numeric': numeric_cache}
