#!/usr/bin/env python3
"""Reference results of config_repository.py part A (C03 review) for reference_test.go.

Runs the real Python functions on edge inputs and writes repo_reference.json:

    CONFIGFLOW_REPO=<repo root> python repo_reference.py OUT

Sections: three_way, deep_merge, lock_timeout, validate (base document + patches),
collect_refs, prune_rule_dns, resource_usage, router_guard, convert_import.
Python exceptions are recorded as {"class", "message", "usages"}; classes outside
the ProfileRepositoryError family are Python crashes (500): Go only has to fail.
"""

import contextlib
import copy
import json
import os
import sys
import tempfile
import uuid
from pathlib import Path

sys.path.insert(0, os.environ['CONFIGFLOW_REPO'])

from backend.common import config as config_module  # noqa: E402
from backend.common import config_repository as cr  # noqa: E402
from backend.common.config_repository import ProfileRepository  # noqa: E402

NOW = '2026-01-02T03:04:05.000006Z'
cr._now = lambda: NOW

_counter = [0]


def fake_uuid4():
    _counter[0] += 1
    return uuid.UUID(hex=(f'{_counter[0]:012x}' * 3)[:32])


cr.uuid.uuid4 = fake_uuid4


def outcome(function):
    try:
        return {'result': function()}
    except Exception as error:  # class and message are compared
        return {'error': {'class': type(error).__name__, 'message': str(error),
                          'usages': getattr(error, 'usages', None)}}


def apply_patch(value, patch):
    for step in patch or []:
        target = value
        *parents, last = step['path']
        for key in parents:
            target = target[key]
        if 'set' in step:
            target[last] = copy.deepcopy(step['set'])
        elif 'append' in step:
            target[last].append(copy.deepcopy(step['append']))
        elif step.get('delete'):
            del target[last]
    return value


def S(path, value):
    return {'path': path, 'set': value}


def D(path):
    return {'path': path, 'delete': True}


def A(path, value):
    return {'path': path, 'append': value}


# --- _three_way_merge / _deep_merge ---------------------------------------------------------

THREE_WAY = [
    [{'a': 1, 'b': 2}, {'a': 1, 'b': 2}, {'a': 1}],
    [{'a': 5}, {'a': 1}, {}],
    [{'a': 1}, {'a': 1}, {'a': 1, 'b': {'x': 1}}],
    [{'b': {'y': 2}}, {}, {'b': {'x': 1}}],
    [{'b': 3}, {}, {'b': {'x': 1}}],
    [{'b': {'y': 2}}, {}, {'b': 7}],
    [{'a': 1}, {'a': 1}, {'a': True}],
    [{'a': 1}, {'a': 1}, {'a': 1.0}],
    [{'a': 9}, {'a': 1}, {'a': 2}],
    [{'k': 'cur'}, {'k': 'base'}, {'k': 'base'}],
    [[1, 2, 3], [1, 2], [2, 1]],
    [['a', 'a', 'b'], ['a', 'b'], ['b']],
    [['b', 'a', 'c'], ['a', 'b'], ['b', 'a']],
    [['x', 'b', 'a'], ['a', 'b'], ['b', 'a', 'n']],
    [[{'id': 'a', 'v': 1}, {'id': 'b', 'v': 1}], [{'id': 'a', 'v': 1}, {'id': 'b', 'v': 1}],
     [{'id': 'b', 'v': 2}, {'id': 'a', 'v': 1}]],
    [[{'id': 'a', 'v': 9}, {'id': 'c'}], [{'id': 'a', 'v': 1}, {'id': 'b'}], [{'id': 'a', 'v': 2}]],
    [[{'id': 1, 'v': 'x'}], [{'id': True, 'v': 'x'}], [{'id': 1, 'v': 'y'}]],
    [[{'id': 1.0, 'v': 'x'}], [{'id': 1.0, 'v': 'x'}], [{'id': 1.0, 'v': 'z'}]],
    [[{'id': None, 'v': 1}], [{'id': None, 'v': 1}], [{'id': None, 'v': 2}]],
    [[{'id': 'a'}, {'id': 'a', 'dup': 1}], [{'id': 'a'}], [{'id': 'a', 'new': 1}]],
    [[{'id': 'a', 'n': {'x': 1, 'y': 1}}], [{'id': 'a', 'n': {'x': 1, 'y': 1}}],
     [{'id': 'a', 'n': {'x': 2, 'y': 1}}]],
    [[[1], [2]], [[1], [2]], [[2], [1], [3]]],
    [{'x': [1]}, {'x': {'a': 1}}, {'x': {'a': 2}}],
    [None, {'a': 1}, {'a': 2}],
    [{'中文': '值', 'emoji🙂': [1]}, {'中文': '值'}, {'中文': '新', 'emoji🙂': [2]}],
    [{'a': {'b': {'c': [1, {'id': 'z', 'q': 1}]}}}, {'a': {'b': {'c': [1, {'id': 'z', 'q': 1}]}}},
     {'a': {'b': {'c': [{'id': 'z', 'q': 2}, 1]}}}],
    [{'a': 1, 'b': 2}, {'b': 2, 'a': 1}, {'a': 1, 'b': 2}],
    [[1, 2], [1, 2], [1, 2, 1]],
    [[3, 1, 2], [1, 2, 3], [3, 2, 1]],
    [['k', {'id': 'k'}], ['k'], [{'id': 'k'}, 'k']],
]

DEEP_MERGE = [
    [{'a': {'b': 1, 'c': 2}}, {'a': {'b': 3}}],
    [{'a': {'b': 1}}, {'a': [1]}],
    [{'a': [1]}, {'a': {'b': 1}}],
    [{}, {'x': {'y': {'z': None}}}],
    [{'k': 1, 'm': 2}, {'m': {'n': 1}, 'k': None}],
]

LOCK_TIMEOUT = [
    None, '1', ' 2.5 ', '1_0', '1__0', '_1', '1_', '1_0.5_0', '１０', '١٢', '١.٥', '0x10', '0x1p3',
    '1e2', '1E2', '1e-400', '1e400', 'inf', '-inf', 'Infinity', 'nan', '+nan', '', '  ', 'abc', '5\n',
    '\t3', ' 3', ' 3', '+3', '-3', '.5', '5.', '.', '0.05', '0', '3600.5', '1,5',
    '1e', 'e1', '1.2.3', '٣e٢', '3_600', '0_1', '00012', '1_e2', '1e_2', '1e2_0',
]


def lock_timeouts():
    results = []
    for raw in LOCK_TIMEOUT:
        if raw is None:
            os.environ.pop(ProfileRepository.LOCK_TIMEOUT_ENV, None)
        else:
            os.environ[ProfileRepository.LOCK_TIMEOUT_ENV] = raw
        results.append([raw, ProfileRepository._lock_timeout_seconds()])
    os.environ.pop(ProfileRepository.LOCK_TIMEOUT_ENV, None)
    return results


# --- documents ----------------------------------------------------------------------------

def base_document(repository):
    profile = repository._empty_profile('default', '默认配置')
    profile['_revision'] = 4
    profile['proxy_groups'] = [
        {'id': 'pg1', 'name': 'Proxy', 'type': 'select', 'manual_nodes': ['n1', 'DIRECT'],
         'subscriptions': ['s1'], 'aggregations': ['g1'],
         'proxies_order': [{'type': 'node', 'id': 'n1'}, {'type': 'strategy', 'id': 'pg2'},
                           {'type': 'node', 'id': 'DIRECT'}, {'type': 'aggregation', 'id': 'g1'},
                           {'type': 'subscription', 'id': 's1'}]},
        {'id': 'pg2', 'name': 'Auto', 'type': 'url-test', 'include_groups': ['pg1'], 'interval': 60},
        {'id': 'pg3', 'name': 'Chain', 'type': 'chain',
         'chain': {'entry': {'type': 'node', 'id': 'n1'}, 'exit': {'type': 'group', 'id': 'pg4'}}},
        {'id': 'pg4', 'name': 'Exit', 'type': 'select', 'subscriptions': ['s1']},
    ]
    profile['rule_configs'] = [
        {'id': 'r1', 'itemType': 'rule', 'rule_type': 'DOMAIN', 'value': 'example.com', 'policy': 'Proxy'},
        {'id': 'r2', 'itemType': 'ruleset', 'library_rule_id': 'l1', 'policy': 'Proxy'},
    ]
    profile['oxidns']['direct_rulesets'] = ['r2']
    profile['oxidns']['rule_dns_follow'] = ['r1']
    profile['oxidns']['rule_actions'] = {'r1': None}
    profile['mosdns']['proxy_rules'] = ['r1']
    profile['surge']['smart_groups'] = [{'group_id': 'pg2'}]
    profile['routers'] = {'items': [{'id': 'rt1', 'name': 'R', 'type': 'routeros', 'endpoint': 'https://192.0.2.1/'}]}
    return {
        'schema_version': 5,
        'system': {'system_config': {'server_domain': '', 'rule_proxy_token': 'tok'}, 'backup': {},
                   'agents': [{'id': 'a1', 'name': 'Agent 1', 'profile_id': 'default'}], '_revision': 3},
        'shared': {
            'subscriptions': [{'id': 's1', 'name': 'Sub 1', 'url': 'https://example.invalid/s', 'interval': 3600,
                               'health_check': {'enable': True, 'interval': 300}}],
            'nodes': [{'id': 'n1', 'name': 'Node 1', 'params': {}},
                      {'id': 'n2', 'name': 'Node 2', 'subscription_id': 's1', 'params': {'dialer-proxy': 'Node 1'}}],
            'subscription_aggregations': [{'id': 'g1', 'name': 'Agg 1', 'subscriptions': ['s1'], 'nodes': ['n1']}],
            'rule_library': [{'id': 'l1', 'name': 'Lib 1', 'url': 'https://example.invalid/r', 'behavior': 'domain',
                              'source_type': 'url'}],
            '_revision': 2},
        'profiles': {'default': profile},
    }


P = ['profiles', 'default']
G = P + ['proxy_groups']
SH = ['shared']
SYS = ['system']


def profile_copy(base, profile_id, **changes):
    value = copy.deepcopy(base['profiles']['default'])
    value['id'] = profile_id
    value.update(changes)
    return value


def validate_cases(base):
    cases = {
        'base': [],
        'schema 4': [S(['schema_version'], 4)], 'schema str': [S(['schema_version'], '5')],
        'schema float': [S(['schema_version'], 5.0)], 'schema true': [S(['schema_version'], True)],
        'schema missing': [D(['schema_version'])],
        'system missing': [D(SYS)], 'shared none': [S(SH, None)], 'profiles list': [S(['profiles'], [])],
        'system_config missing': [D(SYS + ['system_config'])], 'system_config list': [S(SYS + ['system_config'], [])],
        'default id int': [S(SYS + ['system_config', 'default_profile_id'], 5)],
        'default id empty': [S(SYS + ['system_config', 'default_profile_id'], '')],
        'default id missing profile': [S(SYS + ['system_config', 'default_profile_id'], 'p2')],
        'default id none': [S(SYS + ['system_config', 'default_profile_id'], None)],
        'default id false': [S(SYS + ['system_config', 'default_profile_id'], False)],
        'default id list': [S(SYS + ['system_config', 'default_profile_id'], [])],
        'system extra': [S(SYS + ['x'], 1)], 'backup missing': [D(SYS + ['backup'])], 'backup none': [S(SYS + ['backup'], None)],
        'agents missing': [D(SYS + ['agents'])], 'agents dict': [S(SYS + ['agents'], {})],
        'agent empty id': [S(SYS + ['agents'], [{'id': ''}])], 'agent dup': [A(SYS + ['agents'], {'id': 'a1', 'profile_id': 'default'})],
        'agent str': [S(SYS + ['agents'], ['a'])], 'agent int id': [S(SYS + ['agents'], [{'id': 5}])],
        'agent missing profile': [S(SYS + ['agents', 0, 'profile_id'], 'gone')],
        'agent no profile': [D(SYS + ['agents', 0, 'profile_id'])],
        'agent profile list': [S(SYS + ['agents', 0, 'profile_id'], ['default'])],
        'agent name int': [S(SYS + ['agents', 0, 'profile_id'], 'gone'), S(SYS + ['agents', 0, 'name'], 5)],
        'agent name none': [S(SYS + ['agents', 0, 'profile_id'], 'gone'), S(SYS + ['agents', 0, 'name'], None)],
        'agent name dict': [S(SYS + ['agents', 0, 'profile_id'], 'gone'), S(SYS + ['agents', 0, 'name'], {'a': [1, 'x']})],
        'agent name float': [S(SYS + ['agents', 0, 'profile_id'], 'gone'), S(SYS + ['agents', 0, 'name'], 1.0)],
        'agent name bool': [S(SYS + ['agents', 0, 'profile_id'], 'gone'), S(SYS + ['agents', 0, 'name'], True)],
        'agent name emoji': [S(SYS + ['agents', 0, 'profile_id'], 'gone'), S(SYS + ['agents', 0, 'name'], '名字🙂')],
        'agent no name': [S(SYS + ['agents', 0, 'profile_id'], 'gone'), D(SYS + ['agents', 0, 'name'])],
        'subscriptions none': [S(SH + ['subscriptions'], None)],
        'subscription no id': [D(SH + ['subscriptions', 0, 'id'])],
        'subscription dup': [A(SH + ['subscriptions'], {'id': 's1'})],
        'nodes dict': [S(SH + ['nodes'], {})], 'library none': [S(SH + ['rule_library'], None)],
        'aggregations str': [S(SH + ['subscription_aggregations'], 'x')],
        'interval 0': [S(SH + ['subscriptions', 0, 'interval'], 0)],
        'interval 59': [S(SH + ['subscriptions', 0, 'interval'], 59)],
        'interval 60': [S(SH + ['subscriptions', 0, 'interval'], 60)],
        'interval max': [S(SH + ['subscriptions', 0, 'interval'], 604800)],
        'interval over': [S(SH + ['subscriptions', 0, 'interval'], 604801)],
        'interval neg': [S(SH + ['subscriptions', 0, 'interval'], -1)],
        'interval str': [S(SH + ['subscriptions', 0, 'interval'], '3600')],
        'interval true': [S(SH + ['subscriptions', 0, 'interval'], True)],
        'interval float': [S(SH + ['subscriptions', 0, 'interval'], 3600.0)],
        'interval none': [S(SH + ['subscriptions', 0, 'interval'], None)],
        'interval huge': [S(SH + ['subscriptions', 0, 'interval'], 10 ** 30)],
        'health str': [S(SH + ['subscriptions', 0, 'health_check'], 'x')],
        'health unknown': [S(SH + ['subscriptions', 0, 'health_check'], {'tolerance': 5})],
        'health enable int': [S(SH + ['subscriptions', 0, 'health_check'], {'enable': 1})],
        'health interval 0': [S(SH + ['subscriptions', 0, 'health_check'], {'interval': 0})],
        'health interval max': [S(SH + ['subscriptions', 0, 'health_check'], {'interval': 86400})],
        'health interval over': [S(SH + ['subscriptions', 0, 'health_check'], {'interval': 86401})],
        'health timeout bool': [S(SH + ['subscriptions', 0, 'health_check'], {'timeout': True})],
        'health url ftp': [S(SH + ['subscriptions', 0, 'health_check'], {'url': 'ftp://h/'})],
        'health url creds': [S(SH + ['subscriptions', 0, 'health_check'], {'url': 'https://u:p@h/'})],
        'health url user': [S(SH + ['subscriptions', 0, 'health_check'], {'url': 'https://u@h/'})],
        'health url frag': [S(SH + ['subscriptions', 0, 'health_check'], {'url': 'https://h/#f'})],
        'health url empty frag': [S(SH + ['subscriptions', 0, 'health_check'], {'url': 'https://h/#'})],
        'health url port 0': [S(SH + ['subscriptions', 0, 'health_check'], {'url': 'http://h:0/'})],
        'health url port big': [S(SH + ['subscriptions', 0, 'health_check'], {'url': 'http://h:65536/'})],
        'health url port max': [S(SH + ['subscriptions', 0, 'health_check'], {'url': 'http://h:65535/'})],
        'health url port abc': [S(SH + ['subscriptions', 0, 'health_check'], {'url': 'http://h:abc/'})],
        'health url no host': [S(SH + ['subscriptions', 0, 'health_check'], {'url': 'http:///x'})],
        'health url space': [S(SH + ['subscriptions', 0, 'health_check'], {'url': 'http://h/ x'})],
        'health url ideographic space': [S(SH + ['subscriptions', 0, 'health_check'], {'url': 'http://h/　'})],
        'health url ipv6': [S(SH + ['subscriptions', 0, 'health_check'], {'url': 'http://[::1]:8080/x'})],
        'health url bad ipv6': [S(SH + ['subscriptions', 0, 'health_check'], {'url': 'http://[::1/x'})],
        'health url upper scheme': [S(SH + ['subscriptions', 0, 'health_check'], {'url': 'HTTP://h/'})],
        'health url int': [S(SH + ['subscriptions', 0, 'health_check'], {'url': 5})],
        'health status ok': [S(SH + ['subscriptions', 0, 'health_check'], {'expected-status': '200-299/304'})],
        'health status bad': [S(SH + ['subscriptions', 0, 'health_check'], {'expected-status': '20'})],
        'health status range': [S(SH + ['subscriptions', 0, 'health_check'], {'expected-status': '299-200'})],
        'health status 600': [S(SH + ['subscriptions', 0, 'health_check'], {'expected-status': '600'})],
        'health status fullwidth': [S(SH + ['subscriptions', 0, 'health_check'], {'expected-status': '２０４'})],
        'health status newline': [S(SH + ['subscriptions', 0, 'health_check'], {'expected-status': '204\n'})],
        'aggregation health': [S(SH + ['subscription_aggregations', 0, 'health_check'], {'lazy': 'no'})],
        'node params none': [S(SH + ['nodes', 0, 'params'], None)],
        'node name empty': [S(SH + ['nodes', 0, 'name'], '')], 'node name int': [S(SH + ['nodes', 0, 'name'], 5)],
        'node name blank': [S(SH + ['nodes', 0, 'name'], '　')],
        'node proxy_string int': [S(SH + ['nodes', 0, 'proxy_string'], 5)],
        'node dialer_ref': [S(SH + ['nodes', 0, 'dialer_ref'], None)],
        'node missing subscription': [S(SH + ['nodes', 0, 'subscription_id'], 'gone')],
        'node subscription list': [S(SH + ['nodes', 0, 'subscription_id'], ['s1'])],
        'node subscription zero': [S(SH + ['nodes', 0, 'subscription_id'], 0)],
        'node subscription int': [S(SH + ['nodes', 0, 'subscription_id'], 7)],
        'node dialer missing': [S(SH + ['nodes', 1, 'params', 'dialer-proxy'], 'Nobody')],
        'node dialer blank': [S(SH + ['nodes', 1, 'params', 'dialer-proxy'], ' ')],
        'node dialer int': [S(SH + ['nodes', 1, 'params', 'dialer-proxy'], 5)],
        'node dialer group': [S(SH + ['nodes', 1, 'params', 'dialer-proxy'], 'Proxy')],
        'node dialer builtin': [S(SH + ['nodes', 1, 'params', 'dialer-proxy'], 'DIRECT')],
        'node dialer disabled': [S(SH + ['nodes', 1, 'params', 'dialer-proxy'], 'Nobody'), S(SH + ['nodes', 1, 'enabled'], False)],
        'node dialer self': [S(SH + ['nodes', 1, 'params', 'dialer-proxy'], 'Node 2')],
        'node dialer dup name': [S(SH + ['nodes', 1, 'name'], 'Node 1'), S(SH + ['nodes', 1, 'params', 'dialer-proxy'], 'Node 1')],
        'library policy': [S(SH + ['rule_library', 0, 'policy'], 'X')],
        'library target': [S(SH + ['rule_library', 0, 'target'], None)],
        'library name empty': [S(SH + ['rule_library', 0, 'name'], '')],
        'library name int': [S(SH + ['rule_library', 0, 'name'], 5)],
        'library name missing': [D(SH + ['rule_library', 0, 'name'])],
        'library name dup': [A(SH + ['rule_library'], {'id': 'l2', 'name': 'Lib 1'})],
        'aggregation subs str': [S(SH + ['subscription_aggregations', 0, 'subscriptions'], 's1')],
        'aggregation nodes int': [S(SH + ['subscription_aggregations', 0, 'nodes'], [1])],
        'aggregation dup': [S(SH + ['subscription_aggregations', 0, 'nodes'], ['n1', 'n1'])],
        'aggregation missing': [S(SH + ['subscription_aggregations', 0, 'nodes'], ['gone'])],
        'aggregation reject': [S(SH + ['subscription_aggregations', 0, 'nodes'], ['REJECT'])],
        'aggregation subs direct': [S(SH + ['subscription_aggregations', 0, 'subscriptions'], ['DIRECT'])],
        'aggregation name none': [S(SH + ['subscription_aggregations', 0, 'name'], None),
                                  S(SH + ['subscription_aggregations', 0, 'nodes'], ['gone'])],
        'aggregation name list': [S(SH + ['subscription_aggregations', 0, 'name'], ['a', 1]),
                                  S(SH + ['subscription_aggregations', 0, 'nodes'], 'x')],
        'profile id space': [S(['profiles', 'bad id'], profile_copy(base, 'bad id'))],
        'profile id 64': [S(['profiles', 'a' * 64], profile_copy(base, 'a' * 64))],
        'profile id 65': [S(['profiles', 'a' * 65], profile_copy(base, 'a' * 65))],
        'profile id underscore first': [S(['profiles', '_x'], profile_copy(base, '_x'))],
        'profile id dash': [S(['profiles', 'A-b_9'], profile_copy(base, 'A-b_9'))],
        'profile id fullwidth': [S(['profiles', 'Ａ１'], profile_copy(base, 'Ａ１'))],
        'profile id fullwidth digit': [S(['profiles', 'a１'], profile_copy(base, 'a１'))],
        'profile id accent': [S(['profiles', 'é'], profile_copy(base, 'é'))],
        'profile id newline': [S(['profiles', 'a\n'], profile_copy(base, 'a\n'))],
        'profile id empty': [S(['profiles', ''], profile_copy(base, ''))],
        'profile id cjk': [S(['profiles', '配置'], profile_copy(base, '配置'))],
        'profile not dict': [S(['profiles', 'p2'], [])],
        'profile id mismatch': [S(['profiles', 'p2'], profile_copy(base, 'p3'))],
        'profile id int': [S(['profiles', 'p2'], profile_copy(base, 2))],
        'profile shared copy': [S(P + ['nodes'], [])],
        'profile name empty': [S(P + ['name'], '')], 'profile name blank': [S(P + ['name'], ' \t')],
        'profile name ideographic': [S(P + ['name'], '　')], 'profile name int': [S(P + ['name'], 5)],
        'profile name missing': [D(P + ['name'])],
        'profile resource_refs': [S(P + ['resource_refs'], {})], 'profile node_dialers': [S(P + ['node_dialers'], {})],
        'groups none': [S(G, None)], 'groups missing': [D(G)], 'groups dict': [S(G, {})], 'group int': [S(G, [5])],
        'group no id': [S(G, [{'name': 'X'}])], 'group dup id': [A(G, {'id': 'pg1', 'name': 'Other'})],
        'group dup name': [A(G, {'id': 'pg9', 'name': 'Proxy'})], 'group name direct': [A(G, {'id': 'pg9', 'name': 'DIRECT'})],
        'group name empty': [A(G, {'id': 'pg9', 'name': ''})], 'group name int': [A(G, {'id': 'pg9', 'name': 5})],
        'group manual str': [S(G + [0, 'manual_nodes'], 'n1')], 'group manual int': [S(G + [0, 'manual_nodes'], [5])],
        'group manual missing': [S(G + [0, 'manual_nodes'], ['gone'])], 'group manual dup': [S(G + [0, 'manual_nodes'], ['n1', 'n1'])],
        'group manual reject': [S(G + [0, 'manual_nodes'], ['REJECT'])], 'group manual blank': [S(G + [0, 'manual_nodes'], [' '])],
        'group manual none': [S(G + [0, 'manual_nodes'], None)],
        'group subs missing': [S(G + [0, 'subscriptions'], ['gone'])],
        'group aggs missing': [S(G + [0, 'aggregations'], ['gone'])],
        'group subs direct': [S(G + [0, 'subscriptions'], ['DIRECT'])],
        'group source int': [S(G + [0, 'source'], 5)],
        'group source node': [S(G + [0, 'source'], 'node'), S(G + [0, 'proxies'], ['n1', 'DIRECT'])],
        'group source node missing': [S(G + [0, 'source'], 'node'), S(G + [0, 'proxies'], ['gone'])],
        'group source subscription direct': [S(G + [0, 'source'], 'subscription'), S(G + [0, 'proxies'], ['DIRECT'])],
        'group source other': [S(G + [0, 'source'], 'other'), S(G + [0, 'proxies'], ['gone'])],
        'group source aggregation dup': [S(G + [0, 'source'], 'aggregation'), S(G + [0, 'proxies'], ['g1', 'g1'])],
        'order str': [S(G + [0, 'proxies_order'], 'x')], 'order int': [S(G + [0, 'proxies_order'], [5])],
        'order node missing': [S(G + [0, 'proxies_order'], [{'type': 'node', 'id': 'gone'}])],
        'order self': [S(G + [0, 'proxies_order'], [{'type': 'strategy', 'id': 'pg1'}])],
        'order bogus type': [S(G + [0, 'proxies_order'], [{'type': 'bogus', 'id': 'n1'}])],
        'order sub missing': [S(G + [0, 'proxies_order'], [{'type': 'subscription', 'id': 'gone'}])],
        'order strategy missing': [S(G + [0, 'proxies_order'], [{'type': 'strategy', 'id': 'gone'}])],
        'order no type': [S(G + [0, 'proxies_order'], [{'id': 'n1'}])],
        'order id int': [S(G + [0, 'proxies_order'], [{'type': 'node', 'id': 5}])],
        'order node reject': [S(G + [0, 'proxies_order'], [{'type': 'node', 'id': 'REJECT'}])],
        'include missing': [S(G + [1, 'include_groups'], ['gone'])], 'include self': [S(G + [1, 'include_groups'], ['pg2'])],
        'include str': [S(G + [1, 'include_groups'], 'pg1')], 'include chain': [S(G + [1, 'include_groups'], ['pg3'])],
        'follow missing': [S(G + [1, 'follow_group'], 'gone')], 'follow chain': [S(G + [1, 'follow_group'], 'pg3')],
        'follow self': [S(G + [1, 'follow_group'], 'pg2')], 'follow ok': [S(G + [1, 'follow_group'], 'pg1')],
        'follow int': [S(G + [1, 'follow_group'], 5)], 'follow empty': [S(G + [1, 'follow_group'], '')],
        'group health bad': [S(G + [1, 'interval'], 0)], 'group tolerance': [S(G + [1, 'tolerance'], -1)],
        'group health unknown ok': [S(G + [1, 'bogus'], 1)],
        'chain missing': [D(G + [2, 'chain'])], 'chain extra': [S(G + [2, 'chain', 'x'], 1)],
        'chain entry type': [S(G + [2, 'chain', 'entry', 'type'], 'x')],
        'chain entry id empty': [S(G + [2, 'chain', 'entry', 'id'], ' ')],
        'chain entry extra': [S(G + [2, 'chain', 'entry', 'name'], 'x')],
        'chain exit missing group': [S(G + [2, 'chain', 'exit', 'id'], 'gone')],
        'chain entry direct': [S(G + [2, 'chain', 'entry', 'id'], 'DIRECT')],
        'chain entry missing node': [S(G + [2, 'chain', 'entry', 'id'], 'gone')],
        'chain self': [S(G + [2, 'chain', 'exit'], {'type': 'group', 'id': 'pg3'})],
        'chain both groups': [S(G + [2, 'chain', 'entry'], {'type': 'group', 'id': 'pg2'})],
        'rules none': [S(P + ['rule_configs'], None)], 'rule no id': [D(P + ['rule_configs', 0, 'id'])],
        'rule type x': [S(P + ['rule_configs', 0, 'itemType'], 'x')],
        'ruleset missing library': [S(P + ['rule_configs', 1, 'library_rule_id'], 'gone')],
        'ruleset library none': [S(P + ['rule_configs', 1, 'library_rule_id'], None)],
        'ruleset library list': [S(P + ['rule_configs', 1, 'library_rule_id'], ['l1'])],
        'mihomo none': [S(P + ['mihomo'], None)], 'surge list': [S(P + ['surge'], [])],
        'mosdns str': [S(P + ['mosdns'], 'x')], 'mosdns missing': [D(P + ['mosdns'])], 'mosdns none': [S(P + ['mosdns'], None)],
        'mosdns list with bad dns': [S(P + ['mosdns'], [1]), S(P + ['oxidns', 'direct_rules'], ['gone'])],
        'loon none': [S(P + ['loon'], None)], 'loon missing': [D(P + ['loon'])],
        'discovery list': [S(P + ['domain_discovery'], [])], 'discovery missing': [D(P + ['domain_discovery'])],
        'notes str': [S(P + ['device_notes'], 'x')], 'notes missing': [D(P + ['device_notes'])],
        'notes bad key': [S(P + ['device_notes'], {'not-a-mac': 'x'})],
        'notes ok': [S(P + ['device_notes'], {'aa:bb:cc:dd:ee:ff': 'Phone'})],
        'notes value int': [S(P + ['device_notes'], {'aa:bb:cc:dd:ee:ff': 5})],
        'fixed str': [S(P + ['client_fixed_ips'], 'x')], 'fixed missing': [D(P + ['client_fixed_ips'])],
        'fixed bad': [S(P + ['client_fixed_ips'], {'x': 'y'})],
        'routers list': [S(P + ['routers'], [])], 'routers items str': [S(P + ['routers'], {'items': 'x'})],
        'routers missing': [D(P + ['routers'])], 'routers none': [S(P + ['routers'], None)],
        'routers empty': [S(P + ['routers'], {})], 'router no id': [S(P + ['routers', 'items'], [{'name': 'x'}])],
        'oxidns list': [S(P + ['oxidns'], [])], 'oxidns missing': [D(P + ['oxidns'])],
        'mosdns wrong type': [S(P + ['mosdns', 'direct_rulesets'], ['r1'])],
        'mosdns missing rule': [S(P + ['mosdns', 'proxy_rules'], ['gone'])],
        'mosdns str ids': [S(P + ['mosdns', 'direct_rulesets'], 'r2')],
        'mosdns dup ids': [S(P + ['mosdns', 'proxy_rules'], ['r1', 'r1'])],
        'oxidns wrong type': [S(P + ['oxidns', 'proxy_rules'], ['r2'])],
        'oxidns key none': [S(P + ['oxidns', 'direct_rules'], None)],
        'rule_actions list': [S(P + ['oxidns', 'rule_actions'], ['r1'])],
        'rule_actions str': [S(P + ['oxidns', 'rule_actions'], 'r1')],
        'rule_actions none': [S(P + ['oxidns', 'rule_actions'], None)],
        'rule_actions missing rule': [S(P + ['oxidns', 'rule_actions'], {'gone': None})],
        'rule_follow int': [S(P + ['oxidns', 'rule_dns_follow'], [5])],
        'rule_follow dict': [S(P + ['oxidns', 'rule_dns_follow'], {'r1': {'bogus': 1}})],
        'rule_actions bogus': [S(P + ['oxidns', 'rule_actions'], {'r1': {'bogus': 1}})],
        'rule_actions str action': [S(P + ['oxidns', 'rule_actions'], {'r1': 'x'})],
        'rule_actions empty action': [S(P + ['oxidns', 'rule_actions'], {'r1': {}})],
        'rule_parked bogus': [S(P + ['oxidns', 'rule_dns_parked'], {'r1': {'bogus': 1}})],
        'rule_parked list': [S(P + ['oxidns', 'rule_dns_parked'], ['r1'])],
        'smart missing': [S(P + ['surge', 'smart_groups'], [{'group_id': 'gone'}])],
        'smart str': [S(P + ['surge', 'smart_groups'], 'x')], 'smart int': [S(P + ['surge', 'smart_groups'], [5])],
        'smart none': [S(P + ['surge', 'smart_groups'], None)], 'smart dict': [S(P + ['surge', 'smart_groups'], {})],
        'revision neg': [S(SYS + ['_revision'], -1)], 'revision str': [S(SYS + ['_revision'], '1')],
        'revision float': [S(SH + ['_revision'], 1.0)], 'revision true': [S(SH + ['_revision'], True)],
        'revision missing': [D(P + ['_revision'])], 'revision huge': [S(P + ['_revision'], 10 ** 25)],
        'revision none': [S(P + ['_revision'], None)],
        'two profiles in use': [S(['profiles', 'p2'], profile_copy(base, 'p2', name='第二')),
                                S(['profiles', 'p2', 'proxy_groups', 0, 'manual_nodes'], ['gone'])],
    }
    return cases


def validate(repository, base):
    results = []
    for label, patch in validate_cases(base).items():
        document = apply_patch(copy.deepcopy(base), patch)
        results.append({'label': label, 'patch': patch,
                        **outcome(lambda: repository._validate_document(document))})
    return results


# --- _collect_resource_refs, _prune_rule_dns, _resource_usage ----------------------------------

def collect_cases(base):
    catalogs_patch = []
    cases = [
        ('default', [], None), ('roots all nodes', [], {'nodes': None}),
        ('roots listed', [], {'subscriptions': ['s1', 'gone'], 'nodes': []}),
        ('roots bad kind', [], {'rule_library': None}),
        ('aggregation closure', [S(G, [{'id': 'x', 'name': 'X', 'aggregations': ['g1']}])], None),
        ('dialer closure', [S(G, [{'id': 'x', 'name': 'X', 'manual_nodes': ['n2']}])], None),
        ('disabled dialer', [S(G, [{'id': 'x', 'name': 'X', 'manual_nodes': ['n2']}]),
                             S(SH + ['nodes', 1, 'enabled'], 0)], None),
        ('order selects', [S(G, [{'id': 'x', 'name': 'X', 'proxies_order': [{'type': 'subscription', 'id': 's1'},
                                                                             {'type': 'node', 'id': 'REJECT'}]}])], None),
        ('order id none', [S(G, [{'id': 'x', 'name': 'X', 'proxies_order': [{'type': 'node', 'id': None}]}])], None),
        ('order id blank', [S(G, [{'id': 'x', 'name': 'X', 'proxies_order': [{'type': 'node', 'id': ' '}]}])], None),
        ('legacy proxies', [S(G, [{'id': 'x', 'name': 'X', 'source': 'subscription', 'proxies': ['s1']}])], None),
        ('blank catalog id', [S(SH + ['nodes', 0, 'id'], ' '), S(G, [])], {'nodes': None}),
        ('group dialer target', [S(SH + ['nodes', 1, 'params', 'dialer-proxy'], 'X'),
                                 S(G, [{'id': 'x', 'name': 'X', 'manual_nodes': ['n2']}])], None),
        ('ambiguous dialer', [S(SH + ['nodes', 1, 'params', 'dialer-proxy'], 'Node 1'),
                              A(SH + ['nodes'], {'id': 'n3', 'name': 'Node 1', 'params': {}}),
                              S(G, [{'id': 'x', 'name': 'X', 'manual_nodes': ['n2']}])], None),
        ('chain group missing', [S(G, [{'id': 'x', 'name': 'X', 'type': 'chain',
                                        'chain': {'entry': {'type': 'node', 'id': 'n1'}, 'exit': {'type': 'group', 'id': 'y'}}}])], None),
        ('chain order ignored', [S(G, [{'id': 'x', 'name': 'X', 'type': 'chain', 'proxies_order': 'junk',
                                        'chain': {'entry': {'type': 'node', 'id': 'n2'}, 'exit': {'type': 'node', 'id': 'n1'}}}])], None),
    ]
    del catalogs_patch
    return cases


def collect(base):
    results = []
    for label, patch, roots in collect_cases(base):
        document = apply_patch(copy.deepcopy(base), patch)

        def run():
            catalogs = {field: cr._items_by_id(document['shared'][field], field) for field in cr.RESOURCE_FIELDS}
            selected = cr._collect_resource_refs(document['profiles']['default'], catalogs, roots)
            return {key: sorted(value) for key, value in selected.items()}
        results.append({'label': label, 'patch': patch, 'roots': roots, **outcome(run)})
    return results


PRUNE = [
    {'rule_configs': [{'id': 'a'}, 'x', {'id': 5}, {}], 'oxidns': {
        'rule_actions': {'a': 1, 'b': 2, '5': 3}, 'rule_dns_follow': ['b', 'a', 5, None],
        'rule_dns_parked': 'keep', 'other': {'b': 1}}},
    {'rule_configs': {}, 'oxidns': {'rule_actions': {'x': 1}}},
    {'rule_configs': [], 'oxidns': None},
    {'rule_configs': [{'id': None}], 'oxidns': {'rule_dns_follow': [None, 'z'], 'rule_actions': {}}},
]


def prune():
    results = []
    for profile in PRUNE:
        value = copy.deepcopy(profile)
        cr._prune_rule_dns(value)
        results.append([profile, value])
    return results


def resource_usage(base):
    cases = [('subscriptions', 's1', []), ('nodes', 'n1', []), ('nodes', 'n2', []),
             ('subscription_aggregations', 'g1', []), ('rule_library', 'l1', []), ('rule_library', 'gone', []),
             ('nodes', 'n1', [S(SH + ['subscription_aggregations', 0, 'name'], None), D(SH + ['nodes', 0, 'name'])]),
             ('subscriptions', 's1', [S(['profiles', 'p2'], profile_copy(base, 'p2', name='第二'))]),
             ('nodes', 'n1', [S(G + [0, 'manual_nodes'], ['gone'])])]
    results = []
    for kind, resource_id, patch in cases:
        document = apply_patch(copy.deepcopy(base), patch)
        results.append({'kind': kind, 'id': resource_id, 'patch': patch,
                        **outcome(lambda: ProfileRepository._resource_usage(document, kind, resource_id))})
    return results


# --- _router_commit_guard ------------------------------------------------------------------

def router(**fields):
    value = {'id': 'rt1', 'name': 'R', 'type': 'routeros', 'endpoint': 'https://192.0.2.1/'}
    value.update(fields)
    return value


def routers_doc(base, items, extra=None):
    document = copy.deepcopy(base)
    document['profiles']['default']['routers'] = {'items': items}
    if extra is not None:
        document['profiles']['p2'] = profile_copy(base, 'p2', name='第二', routers={'items': extra})
    return document


ROUTER_CASES = [
    ('unchanged', [router()], [router()], [], None, []),
    ('password change locks', [router()], [router(password='x')], [], None, []),
    ('removed locks', [router()], [], [], None, []),
    ('refused lock', [router()], [router(password='x')], [], None, [['default', 'rt1']]),
    ('locked router skipped', [router()], [router(password='x')], [['default', 'rt1']], None, [['default', 'rt1']]),
    ('manifest identity change', [router(ros_policy_state={'a': 1})], [router(ros_policy_state={'a': 1}, endpoint='x')], [], None, []),
    ('manifest dropped', [router(dns_guard_state=[1])], [router()], [], None, []),
    ('manifest dropped policy router', [router(dns_guard_state=[1])], [router()], [], ['default', 'rt1'], []),
    ('manifest removed router', [router(ipv6_pd_guard_state='x')], [], [], ['default', 'rt1'], []),
    ('manifest falsy', [router(ipv6_routes_state={})], [router()], [], None, []),
    ('manifest kept credentials change', [router(ipv6_fakeip_state=1)], [router(ipv6_fakeip_state=1, username='u')], [], None, []),
    ('manifest name missing', [{'id': 'rt1', 'type': 'routeros', 'ros_policy_state': 1}], [], [], None, []),
    ('same device once', [router(), router(id='rt2', endpoint='https://192.0.2.1')], [], [], None, []),
    ('different devices', [router(), router(id='rt2', endpoint='https://192.0.2.2')], [], [], None, [['default', 'rt2']]),
    ('endpoint none', [router(endpoint=None), router(id='rt2', endpoint='')], [], [], None, []),
    ('endpoint int', [router(endpoint=5), router(id='rt2', endpoint='5/')], [], [], None, []),
    ('new router no lock', [], [router()], [], None, []),
    ('verify_tls change', [router(verify_tls=True)], [router(verify_tls=1)], [], None, []),
    ('verify_tls real change', [router(verify_tls=True)], [router(verify_tls=False)], [], None, []),
]


def router_guard(base, repository):
    from backend.routers import rule_sync_store
    original = rule_sync_store.job_lock
    results = []
    try:
        for label, old, new, locked, policy, refuse in ROUTER_CASES:
            calls = []

            @contextlib.contextmanager
            def fake(profile_id, router_id, connection=None, refuse=refuse, calls=calls):
                calls.append([profile_id, router_id])
                yield [profile_id, router_id] not in refuse
            rule_sync_store.job_lock = fake
            previous = routers_doc(base, old)
            proposed = routers_doc(base, new)

            def run():
                with repository._router_commit_guard(
                        previous, proposed, {tuple(key) for key in locked}, tuple(policy) if policy else None):
                    pass
                return None
            record = outcome(run)
            results.append({'label': label, 'old': old, 'new': new, 'locked': locked, 'policy': policy,
                            'refuse': refuse, 'calls': calls, **record})
    finally:
        rule_sync_store.job_lock = original
    return results


# --- _convert_import ------------------------------------------------------------------------

def convert_cases():
    node = lambda i, name, **kw: {'id': i, 'name': name, 'params': kw.pop('params', {}), **kw}  # noqa: E731
    v2_index = lambda *ids: {'schema_version': 2, 'profiles': [{'id': i, 'name': i.upper()} for i in ids]}  # noqa: E731
    return [
        ('empty', {}),
        ('not dict', []),
        ('schema 5 invalid', {'schema_version': 5}),
        ('schema 2.0 missing', {'schema_version': 2.0}),
        ('schema true', {'schema_version': True}),
        ('schema 3', {'schema_version': 3}),
        ('intermediate', {'shared': {}}),
        ('resource_refs', {'resource_refs': {}}),
        ('system_config list', {'system_config': []}),
        ('github proxy int', {'system_config': {'github_proxy_domain': 5, 'server_domain': None}}),
        ('agents', {'agents': [{'id': 'a', 'profile_id': 'x'}, {'id': 'b'}]}),
        ('agents dict', {'agents': {'a': 1}}),
        ('revision', {'_revision': 7, 'backup': {'auto_backup': True}}),
        ('resources', {
            'subscriptions': [{'id': 's', 'name': 'S'}, {'name': 'No id'}],
            'nodes': [node('n', 'N', subscription_id='s'), node('m', 'M')],
            'subscription_aggregations': [{'id': 'g', 'name': 'S', 'subscriptions': ['s'], 'nodes': ['n', 'DIRECT']}],
            'rule_library': [{'id': 'l', 'name': 'L/1', 'url': 'u'}],
            'proxy_groups': [{'id': 'pg', 'name': 'P', 'manual_nodes': ['m', 'REJECT'], 'subscriptions': ['s'],
                              'aggregations': ['g'], 'source': 'node', 'proxies': ['n'],
                              'proxies_order': [{'type': 'node', 'id': 'n'}, {'type': 'node', 'id': 'gone'},
                                                {'type': 'strategy', 'id': 'pg'}, {'type': 'strategy', 'id': 'gone'},
                                                'junk', {'type': 'x', 'id': 'n'}, {'type': 'node', 'id': 'DIRECT'},
                                                {'type': 'aggregation', 'id': 'g'}]}],
            'rules': [{'rule_type': 'RULE-SET', 'value': 'L/1', 'policy': 'P'}, {'rule_type': 'RULE-SET', 'policy': 'P'},
                      {'rule_type': 'DOMAIN', 'value': 'x', 'policy': 'P'}],
            'rule_sets': [{'name': 'Inline', 'content': 'DOMAIN,a', 'policy': 'P'},
                          {'library_rule_id': 'l', 'name': 'Renamed', 'policy': 'P'},
                          {'library_rule_id': 'l', 'policy': 'P'}, {'url': 'https://u', 'policy': 'P'}],
            'mosdns': {'direct_rulesets': ['gone', 5], 'proxy_rules': []},
            'surge': {'smart_groups': [{'group_id': 'pg'}, {'group_id': 'gone'}, 'x']},
        }),
        ('dangling node subscription', {'nodes': [node('n', 'N', subscription_id='gone')]}),
        ('subscription id list', {'nodes': [node('n', 'N', subscription_id=['s'])]}),
        ('aggregation unknown node', {'subscription_aggregations': [{'id': 'g', 'nodes': ['gone']}]}),
        ('aggregation nodes str', {'subscription_aggregations': [{'id': 'g', 'nodes': 'n'}]}),
        ('resources not list', {'nodes': {}}),
        ('resource not dict', {'nodes': [1]}),
        ('resource dup id', {'nodes': [node('n', 'N'), node('n', 'M')]}),
        ('group unknown ref', {'proxy_groups': [{'id': 'pg', 'name': 'P', 'manual_nodes': ['gone']}]}),
        ('group ref int', {'proxy_groups': [{'id': 'pg', 'name': 'P', 'manual_nodes': [5]}]}),
        ('rule library missing ref', {'rule_configs': [{'id': 'r', 'itemType': 'ruleset', 'library_rule_id': 'gone'}]}),
        ('rule configs kept', {'rule_configs': [{'itemType': 'rule', 'rule_type': 'RULE-SET'}], 'rules': [{'x': 1}]}),
        ('rule id int', {'rule_configs': [{'id': 5, 'itemType': 'ruleset', 'name': 'N', 'url': 'u'}]}),
        ('rule name int', {'rule_configs': [{'id': 'a', 'itemType': 'ruleset', 'name': 7, 'url': 'u'},
                                            {'id': 'b', 'itemType': 'rule', 'rule_type': 'RULE-SET', 'value': 7}]}),
        ('library name int', {'rule_library': [{'id': 'l', 'name': 7}]}),
        ('library name missing', {'rule_library': [{'id': 'l'}]}),
        ('library name none', {'rule_library': [{'id': 'l', 'name': None}]}),
        ('subscription name dict', {'subscriptions': [{'id': 's', 'name': {'a': 1}}]}),
        ('subscription names collide', {'subscriptions': [{'id': 's', 'name': 'X'}],
                                        'subscription_aggregations': [{'id': 'g', 'name': 'X'}]}),
        ('library names collide by sanitizing', {'rule_library': [{'id': 'a', 'name': 'a/b'}, {'id': 'b', 'name': 'a:b'}]}),
        ('rule value list', {'rule_configs': [{'id': 'b', 'itemType': 'rule', 'rule_type': 'RULE-SET', 'value': [1]}]}),
        ('rules not list', {'rules': 5}),
        ('rule not dict', {'rules': ['x']}),
        ('groups not list', {'proxy_groups': 5}),
        ('dns not list', {'mosdns': {'direct_rules': 'x'}, 'oxidns': []}),
        ('v2 not dicts', {'schema_version': 2, 'system': [], 'profiles': {}}),
        ('v2 bad index version', {'schema_version': 2, 'system': {'schema_version': True, 'profiles': []}, 'profiles': {}}),
        ('v2 no default', {'schema_version': 2, 'system': v2_index('a'), 'profiles': {'a': {}}}),
        ('v2 bad id', {'schema_version': 2, 'system': v2_index('default', 'a b'), 'profiles': {'default': {}, 'a b': {}}}),
        ('v2 mismatch', {'schema_version': 2, 'system': v2_index('default'), 'profiles': {'default': {}, 'x': {}}}),
        ('v2 profile not dict', {'schema_version': 2, 'system': v2_index('default'), 'profiles': {'default': 5}}),
        ('v2 intermediate', {'schema_version': 2, 'system': v2_index('default'), 'profiles': {'default': {'shared': 1}}}),
        ('v2 merge', {'schema_version': 2.0, 'system': {**v2_index('default', 'b'), 'agents': [{'id': 'a'}, {'id': 'c', 'profile_id': 'b'}],
                                                         '_revision': 2},
                      'profiles': {
                          'default': {'subscriptions': [{'id': 's', 'name': 'S'}], 'nodes': [node('n', 'Same'), node('d', 'Dep', params={'dialer-proxy': 'Same'})],
                                      'rule_library': [{'id': 'l', 'name': 'L'}],
                                      'proxy_groups': [{'id': 'pg', 'name': 'G', 'manual_nodes': ['d']}],
                                      'rule_configs': [{'id': 'r', 'itemType': 'rule', 'policy': 'Same'}]},
                          'b': {'subscriptions': [{'id': 's', 'name': 'S', 'url': 'other'}], 'nodes': [node('n', 'Same', params={'x': 1}), node('d', 'Dep', params={'dialer-proxy': 'Same'})],
                                'rule_library': [{'id': 'l', 'name': 'L'}, {'id': 'l2', 'name': 'L', 'url': 'x'}],
                                'proxy_groups': [{'id': 'pg', 'name': 'Same2'}],
                                'rule_configs': [{'id': 'r', 'itemType': 'rule', 'policy': 'Same'}, {'id': 'q', 'itemType': 'ruleset', 'library_rule_id': 'l2', 'name': 'X'}]},
                      }}),
        ('v2 proxy string dialer', {'schema_version': 2, 'system': v2_index('default', 'b'), 'profiles': {
            'default': {'nodes': [node('n', 'T', params={'a': 1}), {'id': 'p', 'name': 'P', 'proxy_string': '{"name": "P", "type": "ss", "dialer-proxy": "T"}'}]},
            'b': {'nodes': [node('n', 'T', params={'a': 2}), {'id': 'p', 'name': 'P', 'proxy_string': '{"name": "P", "type": "ss", "dialer-proxy": "T"}'}]}}}),
        ('v2 dialer ambiguous', {'schema_version': 2, 'system': v2_index('default'), 'profiles': {
            'default': {'nodes': [node('n', 'T'), node('m', 'T'), node('d', 'D', params={'dialer-proxy': 'T'})]}}}),
        ('v2 dialer blank', {'schema_version': 2, 'system': v2_index('default'), 'profiles': {
            'default': {'nodes': [node('d', 'D', params={'dialer-proxy': ' '})]}}}),
        ('v2 dialer to group', {'schema_version': 2, 'system': v2_index('default', 'b'), 'profiles': {
            'default': {'nodes': [node('d', 'D', params={'dialer-proxy': 'G'})], 'proxy_groups': [{'id': 'g', 'name': 'G'}]},
            'b': {'nodes': [node('d', 'D', params={'dialer-proxy': 'G'})], 'proxy_groups': [{'id': 'g', 'name': 'G', 'x': 1}]}}}),
        ('v2 node bad subscription', {'schema_version': 2, 'system': v2_index('default'), 'profiles': {
            'default': {'nodes': [node('d', 'D', subscription_id='gone')]}}}),
        ('v2 shapes', {'schema_version': 2, 'system': v2_index('default'), 'profiles': {
            'default': {'nodes': [{'name': ''}]}}}),
        ('v2 name collisions', {'schema_version': 2, 'system': v2_index('default', 'b', 'c'), 'profiles': {
            'default': {'subscriptions': [{'id': 's', 'name': 'N' * 200}]},
            'b': {'subscriptions': [{'id': 's', 'name': 'N' * 200, 'x': 1}]},
            'c': {'subscriptions': [{'id': 's', 'name': 'N' * 200, 'x': 2}, {'id': 't', 'name': 'N' * 200 + ' (c)'}]}}}),
        ('v2 long ids', {'schema_version': 2, 'system': v2_index('default', 'b'), 'profiles': {
            'default': {'nodes': [node('节' * 50, 'A')]}, 'b': {'nodes': [node('节' * 50, 'B')]}}}),
    ]


def convert(repository):
    results = []
    for label, source in convert_cases():
        _counter[0] = 0
        results.append({'label': label, 'source': source,
                        **outcome(lambda: repository._convert_import(copy.deepcopy(source)))})
    _counter[0] = 0
    results.append({'label': 'initial config', 'source': None,
                    **outcome(lambda: repository._convert_import(repository._initial_config_factory()))})
    return results


def main():
    output = sys.argv[1]
    with tempfile.TemporaryDirectory() as directory:
        plain = ProfileRepository(Path(directory) / 'plain')
        factories = ProfileRepository(Path(directory) / 'factories',
                                      default_config_factory=config_module.get_default_config,
                                      initial_config_factory=config_module._get_initial_config)
        base = base_document(plain)
        plain._validate_document(base)
        result = {
            'three_way': [[*case, cr._three_way_merge(*copy.deepcopy(case))] for case in THREE_WAY],
            'deep_merge': [[*case, cr._deep_merge(*copy.deepcopy(case))] for case in DEEP_MERGE],
            'lock_timeout': lock_timeouts(),
            'legacy_defaults': {'plain': plain._legacy_defaults(), 'factories': factories._legacy_defaults()},
            'base': base,
            'validate': validate(plain, base),
            'collect_refs': collect(base),
            'prune_rule_dns': prune(),
            'resource_usage': resource_usage(base),
            'router_guard': router_guard(base, plain),
            'convert_import': convert(factories),
        }
    Path(output).write_text(json.dumps(result, ensure_ascii=False, indent=1) + '\n', encoding='utf-8')


if __name__ == '__main__':
    main()
