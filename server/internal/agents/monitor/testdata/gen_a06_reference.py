"""CPython reference corpus for A06 (oxidns_monitor, oxidns_metrics,
oxidns_dashboard_state, the pure part of oxidns_publication_stats).

Synthetic values only. Writes a06_reference.json.gz next to this file:

    rt -- bash -lc 'cd server/internal/agents/monitor && PYTHONPATH=../../../.. ~/cf-test/.venv/bin/python testdata/gen_a06_reference.py'

Every result is json.dumps(value, sort_keys=True) text so the Go test
compares bytes (int vs float, float repr).
"""
import copy
import datetime
import gzip
import json
import os
import random
import secrets

from werkzeug.datastructures import MultiDict

from backend.agents import oxidns_metrics as metrics
from backend.agents import oxidns_monitor as monitor
from backend.agents import oxidns_dashboard_state as storage
from backend.agents import oxidns_publication_stats as publication

random.seed(6)
OUT = {}


def dump(value):
    return json.dumps(value, sort_keys=True)


def error_of(exc):
    if isinstance(exc, monitor.MonitorError):
        return {'error': exc.code, 'status': exc.status, 'message': exc.message}
    return {'error': type(exc).__name__}


# --- metrics -------------------------------------------------------------------

def server(tag, protocol, requests, failed, elapsed, count, inflight=0):
    return '\n'.join(f'server_{f}{{plugin_tag="{tag}",protocol="{protocol}"}} {v}' for f, v in (
        ('request_total', requests), ('failed_total', failed), ('latency_sum_ms', elapsed),
        ('latency_count', count), ('inflight', inflight)))


def cache(tag, lookups, fresh, stale, misses=0, expired=0, entries=0):
    return '\n'.join([
        f'cache_lookup_total{{plugin_tag="{tag}"}} {lookups}',
        f'cache_hit_total{{plugin_tag="{tag}",kind="fresh"}} {fresh}',
        f'cache_hit_total{{plugin_tag="{tag}",kind="stale"}} {stale}',
        f'cache_miss_total{{plugin_tag="{tag}"}} {misses}',
        f'cache_expired_total{{plugin_tag="{tag}"}} {expired}',
        f'cache_entry_count{{plugin_tag="{tag}"}} {entries}'])


def upstream(tag, name, requests, failed, elapsed, count):
    return '\n'.join(f'forward_upstream_{f}{{plugin_tag="{tag}",upstream="{name}"}} {v}' for f, v in (
        ('query_total', requests), ('error_total', failed), ('latency_sum_ms', elapsed), ('latency_count', count)))


values = ['5', '+5', '-0', '-0.0', '0.0', '1.', '.5', '1e2', '1E+2', '1e-400', '1e400', '1.5e308', '2e308',
          '１２', '٣', '1٣', '٣.٥', '١e٢', '𝟘𝟙', '0' * 4299 + '1', '0' * 4300 + '1', '9' * 400, '1_0', '0x10',
          'inf', 'nan', '1e', '.', '+', '-1', '−1', '18446744073709551615', '9007199254740993', '1.0000000000000002',
          '0.1', '123456789012345678901234567890', '5\u00a0', '1e-5', '00012', '1.50']
texts = []
for value in values:
    texts.append(f'cache_entry_count{{plugin_tag="c"}} {value}')
    texts.append(server('s', 'udp', value, 0, value, 4))
texts += [
    'cache_entry_count{plugin_tag="c"} 7 ١٢٣',
    'cache_entry_count{plugin_tag="c"} 7 -12',
    'cache_entry_count{plugin_tag="c"} 7 12x',
    'cache_entry_count{plugin_tag="c"} 7 1.5',
    'cache_entry_count٣{plugin_tag="c"} 7',
    'cache_entry_count{plugin_tag="c"}\u00a07',
    '\u3000cache_entry_count{plugin_tag="c"} 7\u3000',
    'cache_entry_count{plugin_tag="c"} 7\x0bcache_lookup_total{plugin_tag="c"} 9',
    'cache_entry_count{plugin_tag="c"} 7\x85cache_lookup_total{plugin_tag="c"} 9',
    'cache_entry_count{plugin_tag="c"} 7\x1ccache_lookup_total{plugin_tag="c"} 9',
    'cache_entry_count{plugin_tag="c"} 7\u2028cache_lookup_total{plugin_tag="c"} 9',
    'cache_entry_count{plugin_tag="c"} 7\r\ncache_lookup_total{plugin_tag="c"} 9\rcache_miss_total{plugin_tag="c"} 1',
    '   # comment {plugin_tag="c"} 1\ncache_entry_count{plugin_tag="c"} 2',
    'oxidns_oxidns_cache_entry_count{plugin_tag="c"} 7',
    'cache_entry_count{plugin_tag="c",} 7',
    'cache_entry_count{ plugin_tag = "c" , kind="x" } 7',
    'cache_entry_count{plugin_tag="c" kind="x"} 7',
    'cache_entry_count{plugin_tag=""} 7',
    'cache_entry_count{plugin_tag="c"}} 7',
    'cache_entry_count{plugin_tag="c"} 5} 7',
    'cache_entry_count{plugin_tag="a}b"} 7',
    'cache_entry_count{plugin_tag="c\\\\"} 7',
    'cache_entry_count{plugin_tag="c\\n"} 7',
    'cache_entry_count{plugin_tag="Σ İ"} 7',
    'cache_entry_count{' + ','.join(f'l{i}="v"' for i in range(31)) + ',plugin_tag="c"} 7',
    'cache_entry_count{' + ','.join(f'l{i}="v"' for i in range(32)) + ',plugin_tag="c"} 7',
    'cache_entry_count{plugin_tag="c"} 5\ncache_entry_count{plugin_tag="c"} 5.0',
    'cache_entry_count{plugin_tag="c"} 5.0\ncache_entry_count{plugin_tag="c"} 5',
    'cache_entry_count{plugin_tag="c"} 5\ncache_entry_count{plugin_tag="c"} NaN\ncache_entry_count{plugin_tag="c"} 5',
    'cache_hit_total{plugin_tag="c",kind="fresh"} 1\ncache_hit_total{plugin_tag="c",kind="Fresh"} 1',
    server('s', 'udp', 18446744073709551615, 0, '1.5', 3) + '\n' + server('t', 'tcp', '2.5', 1, 7, 2),
    server('s', 'udp', 10 ** 20, 1, 10 ** 20, 3),
    server('s', 'udp', 10 ** 300, 1, 10 ** 300, 3) + '\n' + server('t', 'udp', 10 ** 300, 1, 10 ** 300, 3),
    server('s', 'udp', '1e308', 1, '1e308', 3) + '\n' + server('t', 'udp', 10 ** 308, 1, 1, 3),
    cache('a', '10.0', 3, '2.5', 1, 1, '7.0') + '\n' + cache('b', 5, 5, 0) + '\n' + cache('c', 7, 1, 1),
    cache('a', 0.0, 0, 0) + '\n' + cache('b', 3, 1, 1),
    '\n'.join([upstream('b', 'x', 5, 0, 1, 1), upstream('a', 'y', 5, 0, 1, 1), upstream('a', 'x', '5.0', 0, 1, 1),
               upstream('c', 'z', 'NaN', 0, 1, 1), upstream('a', 'w', '7.5', 0, 1, 0), upstream('d', 'v', 0, 0, 0, 0)]),
    upstream('Ä', 'é', 3, 1, 9, 3) + '\n' + upstream('A', 'e', 3, 1, 9, 3) + '\n' + upstream('a', 'E', 3, 1, 9, 3),
    server('z', 'udp', 1, 0, 1, 1) + '\n' + server('a', 'udp', 1, 0, 1, 1) + '\n' + server('a', 'tcp', 1, 0, 1, 1),
    'cache_entry_count{plugin_tag="c"} 7 1 2',
    'cache_entry_count{plugin_tag="c"}  7  ',
    'cache_entry_count{plugin_tag="c"}\t7\t1792100000000\t',
]
OUT['metrics'] = []
for text in texts:
    try:
        result = {'parsed': metrics.parse_metrics(text), 'summary': metrics.summarize_metrics(text)}
    except ValueError as exc:
        result = {'error': str(exc)}
    OUT['metrics'].append({'text': text, 'result': dump(result)})

# Baseline arithmetic: (current, baseline, carried) observation texts.
triples = [
    (server('s', 'udp', 110, 3, '1300.5', 110, 4), server('s', 'udp', 100, 1, 1000, 100, 2), ''),
    (server('s', 'udp', '110.0', 3, 1300, 110), server('s', 'udp', 100, 1, '1000.25', 100), server('s', 'udp', 5, 1, 50, 5)),
    (server('s', 'udp', 10 ** 20 + 7, 3, 1300, 110), server('s', 'udp', 10 ** 20, 1, 1000, 100), server('old', 'tcp', 9, 1, 90, 9, 3)),
    (server('s', 'udp', 5, 3, 1300, 110), server('s', 'udp', 100, 1, 1000, 100), ''),
    (server('s', 'udp', 'NaN', 3, 1300, 110), server('s', 'udp', 100, 1, 1000, 100), server('s', 'udp', 'NaN', 1, 1, 1)),
    (cache('c', 120, 65, 12, 32, 11, 105) + '\n' + cache('d', 1, 1, 0), cache('c', 100, 50, 10, 30, 10, 99), cache('e', 3, 1, 1, 0, 0, 9)),
    (upstream('f', 'dns', 54, 6, '5800.5', 54), upstream('f', 'dns', 50, 5, 5000, 50) + '\n' + upstream('g', 'x', 1, 1, 1, 1),
     upstream('f', 'dns', '1e308', 1, '1.5', 1)),
]
OUT['baseline'] = []
for current_text, baseline_text, carried_text in triples:
    current, baseline = metrics.parse_metrics(current_text), metrics.parse_metrics(baseline_text)
    baseline = json.loads(json.dumps(baseline))
    carried = metrics.parse_metrics(carried_text)
    adjusted, partial = metrics.apply_metrics_baseline(current, baseline)
    OUT['baseline'].append({'current': current_text, 'baseline': baseline_text, 'carried': carried_text, 'result': dump({
        'restarted': metrics.metrics_counters_restarted(current, baseline),
        'adjusted': adjusted, 'partial': partial, 'zero': metrics.zero_baseline(current),
        'added': metrics.add_observations(adjusted, carried),
        'summary': metrics.summarize_observations(metrics.add_observations(adjusted, carried))})})

# --- strings, addresses, upstream labels ----------------------------------------

labels = ['https://User:Pass@Host.Example:443/path?q#f', 'HTTPS://DNS.TEST', 'tls://[2001:DB8::1]:853',
          'tls://[fe80::1%25eth0]:853', 'tls://[fe80::1%eth0]:853', 'udp://1.2.3.4:99999', 'udp://h:abc', 'udp://:53',
          'quic://[::1', 'quic://x]', 'h3://İSTANBUL.example/dns', 'https://ΣΑΣ.test/', 'https://a\u2100b/',
          ' https://lead.space/', 'tcp://host:+53', 'tcp://host:٥٣', 'plain@host:53?x', 'a@b@c', '1x://host/p',
          'dns#1', 'https://host:0/', 'https://[v1.fe]/', 'https://[1.2.3.4]/', '\x7fbad', 'x' * 3000,
          'https://' + 'h' * 3000, 'https://host:/p', 'udp://[::ffff:1.2.3.4]:53', 'https://h\u3000x/',
          'HTTPS://H/P/../Q', 'svc://name', 'https://a@[::1]:5/', 'https://[::1]x/', 'https://x[::1]/',
          '//host/path', 'http:///path', 'https://host?a', 'https://host#a', 'é://host', 'a+b.c-d://Host',
          123, None, '', 'udp://host:65535', 'udp://host:065535', 'udp://host:65536', '\u0080x', '\u009fx', '\u00a0x']
OUT['upstream'] = [[label, monitor._display_upstream(label)] for label in labels]
OUT['public_upstreams'] = dump(monitor._public_upstreams(
    [{'tag': 'r', 'upstream': 'é', 'requests': 1}, {'tag': 'ü', 'upstream': 'https://u:p@x/'}, {'tag': None, 'upstream': 7}],
    [{'plugin_tag': 'r', 'upstream': 'é', 'address': 'https://a:b@addr.test:8443/q?x'}]))

ips = ['1.2.3.4', '01.2.3.4', '1.2.3', '256.1.1.1', '::ffff:1.2.3.4', '::1.2.3.4', 'fe80::1%eth0', 'fe80::1%',
       'fe80::1%a%b', '1.2.3.4%eth0', '1::2:3:4:5:6:7', '1:2:3:4:5:6:7:8:9', '1:2:3:4:5:6:1.2.3.4',
       '1:2:3:4:5:6:7:1.2.3.4', ':1:2:3:4:5:6:7', '1:2:3:4:5:6:7:', '::', ':::', '1::2::3', '2001:0DB8::',
       '2001:db8:0:0:1:0:0:1', '00001::', 'g::', '１.2.3.4', ' 1.2.3.4', '1.2.3.4 ', '::ffff:1.2.3.04',
       'fe80::1%eth 0', '::%x', '0.0.0.0', '127.000.0.1', '1:2:3:4:5:6:7:8', '1::', '::1:2:3:4:5:6:7',
       '1:2:3:4::5:6:7:8', '::ffff:0:1.2.3.4', '1.2.3.4.5', '', '[::1]', '0::0:0', 'FE80::ABCD', '::1.2.3',
       '1:2:3:4:5:6::1.2.3.4', 'fe80::1%25', 'a:b:c:d:e:f:0:0', '0:0:a:b:0:0:0:c', '1.2.3.255', '1.2.3.-0']
OUT['ip'] = []
for ip in ips:
    try:
        OUT['ip'].append([ip, str(__import__('ipaddress').ip_address(ip))])
    except ValueError:
        OUT['ip'].append([ip, None])

origins = [('127.0.0.1', 8082), ('[::1]', 80), ('::1', 80), ('[127.0.0.1]', 1), ('[::ffff:1.2.3.4]', 9), ('fe80::1%eth0', 5),
           ('Host.Example', 65535), ('-host', 5), ('host-', 5), ('a' * 253, 5), ('a' * 254, 5), ('ho_st', 5), ('h', 0),
           ('h', 65536), ('h', True), ('h', 80.0), ('h', '80'), ('', 80), (None, 80), ('h st', 80), ('h\\x', 80),
           ('[h]', 80), ('[::1', 80), ('é.test', 80), ('1.2.3', 80), ('01.2.3.4', 80), ('h', 10 ** 30), ('h.', 80)]
OUT['origin'] = []
for host, port in origins:
    agent = {'host': host, 'port': port}
    try:
        OUT['origin'].append({'agent': agent, 'result': monitor._agent_origin(agent)})
    except monitor.MonitorError as exc:
        OUT['origin'].append({'agent': agent, 'error': exc.code})
OUT['origin'].append({'agent': {'port': 80}, 'error': 'AGENT_UNREACHABLE'})


def params_case(name, function, args, *extra):
    try:
        result = function(MultiDict(args), *extra)
        if hasattr(result, 'items'):
            result = list(result.items())
        return {'args': args, 'extra': list(extra), 'result': result}
    except monitor.MonitorError as exc:
        return {'args': args, 'extra': list(extra), 'error': exc.code}


query_args = [
    [('qname', '\u3000example\u3000')], [('limit', ' 42 ')], [('limit', '０5')], [('limit', '1' * 17)],
    [('since_ms', '9007199254740992')], [('since_ms', '9007199254740991')], [('until_ms', '0000000000000001')],
    [('cursor', '0:1')], [('cursor', '1:0')], [('cursor', '9007199254740991:9007199254740991')],
    [('cursor', '1:9007199254740992')], [('qtype', 'aaaa')], [('qtype', 'ı')], [('rcode', 'nxdomain')], [('rcode', ' servfail ')],
    [('recorder', 'x' * 256)], [('recorder', 'x' * 257)], [('recorder', 'é' * 256)], [('matcher_tag', 'a/b')],
    [('matcher_tag', 'a b')], [('client_ip', '2001:0DB8::2')], [('client_ip', ' 192.0.2.1 ')], [('client_ip', '::ffff:1.2.3.4')],
    [('qname', 'a\x7fb')], [('qname', '測' * 341)], [('qname', '測' * 342)], [('qname', '')], [('qname', '   ')],
    [('qname', 'a'), ('limit', '7'), ('recorder', 'r')], [('status', 'error')], [('status', ' all ')],
    [('since_ms', '5'), ('until_ms', '5')], [('since_ms', '6'), ('until_ms', ' 5')], [('limit', '100')], [('limit', '')],
    [('qname', '\x1f')], [('qname', '\x85x')], [('rcode', 'No\u00a0Error')], [('recorder', 'a?b')],
]
OUT['query_params'] = [params_case('q', monitor.query_params, a, False) for a in query_args]
OUT['query_params'] += [params_case('q', monitor.query_params, a, True) for a in
                        ([('recorder', ' r ')], [('limit', '5')], [], [('recorder', '')])]
client_args = [
    [('search', '\u0085')], [('search', 'İ' * 32)], [('search', 'İ' * 33)], [('search', '  ')], [('search', ' a ')],
    [('recorder', 'a.b')], [('recorder', 'a_b')], [('recorder', '-a')], [('recorder', 'a-')], [('recorder', 'a' * 64)],
    [('recorder', 'a' * 65)], [('since_ms', '0')], [('until_ms', '9007199254740991')], [('until_ms', '9007199254740992')],
    [('since_ms', ' 1')], [('since_ms', '１')], [('search', '\x00')], [('search', '\u00a0x\u00a0')],
]
OUT['client_params'] = [params_case('c', monitor.client_params, a) for a in client_args]
dashboard_args = [[('recorder', 'records')], [('recorder', 'a..b')], [('range', '24h')], [('range', '1h')],
                  [('range', '7d'), ('recorder', 'r')], [('range', '7d'), ('range', '7d')], [('unknown', '1')], []]
OUT['dashboard_params'] = [params_case('d', monitor._dashboard_params, a) for a in dashboard_args]
OUT['recorder_tags'] = [[tag, monitor._valid_recorder_tag(tag)] for tag in
                        ['a', 'a.b', 'a..b', '.a', 'a.', 'a_b-c.d', 'A1', '_a', 'a' * 64, 'a' * 65, 'é', '', 'a.b_', 'a-.b']]

# --- dashboard state validation -----------------------------------------------------

obs = json.loads(json.dumps(metrics.parse_metrics(server('s', 'udp', 1, 0, 2, 1, 0) + '\n' + cache('c', 1, 1, 0))))
base1 = {'schema': 1, 'generation': 'a' * 32, 'started_at_ms': 1000, 'reason': 'initial', 'target': 'b' * 64,
         'instance_id': 'core', 'source_revision': 'c' * 64, 'runtime_revision': 'd' * 64, 'baseline': obs}
base2 = {**base1, 'schema': 2, 'carried': obs, 'carried_partial': False, 'publication_count': 0,
         'pending': {'deployment_id': 'dep-1', 'expected_revision': 'e' * 64, 'collected_at_ms': 1000, 'totals': obs,
                     'partial': False}}
state_texts = [json.dumps(base1), json.dumps(base2)]
for key, value in [('schema', 1.0), ('schema', True), ('schema', 3), ('generation', 'A' * 32), ('generation', 'a' * 31),
                   ('started_at_ms', -1), ('started_at_ms', 2 ** 53), ('started_at_ms', 2 ** 53 - 1), ('started_at_ms', 1.0),
                   ('started_at_ms', True), ('reason', 'other'), ('reason', 'publication'), ('instance_id', ''),
                   ('instance_id', 'x' * 129), ('instance_id', '測' * 128), ('target', 'b' * 63), ('instance_id', 5)]:
    state_texts.append(json.dumps({**base1, key: value}))
state_texts.append(json.dumps({**base1, 'reason': ['x']}))
state_texts.append(json.dumps(base1).replace('"instance_id": "core"', '"instance_id": "core", "extra": 1'))
for row_change in [{'extra': 1}, {'tag': ''}, {'tag': 'x' * 16385}, {'tag': 'x' * 16384}, {'requests': -1}, {'requests': True},
                   {'requests': '1'}, {'requests': 1e308}, {'requests': -0.0}, {'requests': None}, {'protocol': 5}]:
    state = copy.deepcopy(base1)
    state['baseline']['servers'][0].update(row_change)
    state_texts.append(json.dumps(state))
state = copy.deepcopy(base1)
del state['baseline']['servers'][0]['requests']
state_texts.append(json.dumps(state))
state = copy.deepcopy(base1)
state['baseline']['servers'].append(dict(state['baseline']['servers'][0]))
state_texts.append(json.dumps(state))
state_texts.append(json.dumps(base1).replace('"requests": 1', '"requests": NaN'))
state_texts.append(json.dumps(base1).replace('"requests": 1', '"requests": Infinity'))
state_texts.append(json.dumps(base1).replace('"requests": 1', '"requests": 1' + '0' * 400))
state_texts.append(json.dumps(base1).replace('"requests": 1', '"requests": 1e400'))
state = copy.deepcopy(base1)
state['baseline']['servers'] = [{'tag': f't{i}', 'protocol': 'udp'} for i in range(10001)]
state_texts.append(json.dumps(state))
state['baseline']['servers'] = [{'tag': f't{i}', 'protocol': 'udp'} for i in range(9999)]
state['baseline']['upstreams'] = [{'tag': 'u', 'upstream': 'x'}, {'tag': 'u', 'upstream': 'y'}]
state_texts.append(json.dumps(state))
for key, value in [('carried_partial', 0), ('publication_count', 2 ** 53), ('publication_count', -1), ('pending', None),
                   ('pending', {}), ('carried', {})]:
    state_texts.append(json.dumps({**base2, key: value}))
for key, value in [('deployment_id', 'a.b'), ('deployment_id', 'x' * 129), ('deployment_id', 'x' * 128),
                   ('collected_at_ms', 999), ('collected_at_ms', 253402300799999), ('collected_at_ms', 253402300800000),
                   ('collected_at_ms', 1000.0), ('partial', None), ('expected_revision', 'E' * 64), ('totals', [])]:
    state_texts.append(json.dumps({**base2, 'pending': {**base2['pending'], key: value}}))
state_texts.append(json.dumps({**base2, 'schema': 1}))
state_texts.append(json.dumps({**base1, 'schema': 2}))
state_texts.append('{"schema": 1, "schema": 1}')
OUT['validate_state'] = []
for text in state_texts:
    try:
        valid = storage.validate_state(json.loads(text))
    except TypeError:
        valid = 'TypeError'
    OUT['validate_state'].append([text, valid])

# --- rankings, records, timestamps ------------------------------------------------------

sections = []
for kind in ('clients', 'domains'):
    for value in [
            {'sample_size': 3, 'rows': [{'key': '192.0.2.1', 'count': 2}, {'key': '192.0.2.2', 'count': 1}], 'at_limit': False},
            {'sample_size': 3, 'rows': [{'key': 'b.test', 'count': 2}, {'key': 'a.test', 'count': 2}], 'at_limit': False},
            {'sample_size': 7, 'rows': [{'key': 'x.test', 'count': 1}], 'at_limit': False},
            {'sample_size': 10000, 'rows': [], 'at_limit': True}, {'sample_size': 10000, 'rows': [], 'at_limit': False},
            {'sample_size': 0, 'rows': [], 'at_limit': False}, {'sample_size': 0, 'rows': [{'key': 'a', 'count': 1}], 'at_limit': False},
            {'sample_size': 3.0, 'rows': [], 'at_limit': False}, {'sample_size': 3, 'rows': [], 'at_limit': 0},
            {'sample_size': 1, 'rows': [{'key': 'fe80::1%eth0', 'count': 1}], 'at_limit': False},
            {'sample_size': 1, 'rows': [{'key': '\x85', 'count': 1}], 'at_limit': False},
            {'sample_size': 1, 'rows': [{'key': 'x' * 1025, 'count': 1}], 'at_limit': False},
            {'sample_size': 2, 'rows': [{'key': '2001:DB8::1', 'count': 1}, {'key': '2001:db8::1', 'count': 1}], 'at_limit': False},
            {'sample_size': 1, 'rows': [{'key': 'many.test', 'count': 65535}], 'at_limit': False},
            {'sample_size': 1, 'rows': [{'key': 'many.test', 'count': 65536}], 'at_limit': False},
            {'sample_size': 1, 'rows': [{'key': 'a.test', 'count': 65535}, {'key': 'b.test', 'count': 1}], 'at_limit': False},
            {'sample_size': 3, 'rows': [{'key': 'a', 'count': 1.0}], 'at_limit': False},
            {'sample_size': 3, 'rows': [{'key': 'a', 'count': True}], 'at_limit': False},
            {'sample_size': 3, 'rows': [{'key': 1, 'count': 1}], 'at_limit': False},
            {'sample_size': 9, 'rows': [{'key': f'{i}.test', 'count': 1} for i in range(10)], 'at_limit': False}]:
        try:
            result = monitor._ranking_section(value, kind)
        except monitor.MonitorError as exc:
            result = {'error': exc.code}
        sections.append({'kind': kind, 'value': value, 'result': dump(result)})
OUT['ranking_section'] = sections

row = {'id': 42, 'created_at_ms': 1792100000123, 'elapsed_ms': 0, 'questions_json': [], 'answers_json': []}
records = []
for change, detail in [({}, False), ({'steps': []}, True), ({}, True), ({'id': 0}, False), ({'id': 2 ** 53 - 1}, False),
                       ({'id': 2 ** 53}, False), ({'id': 42.0}, False), ({'id': True}, False), ({'elapsed_ms': 1.5}, False),
                       ({'elapsed_ms': -0.0}, False), ({'elapsed_ms': -1}, False), ({'created_at_ms': None}, False),
                       ({'questions_json': {}}, False), ({'extra': 1, 'rcode': 'NOERROR', 'resp_cd': False}, False),
                       ({'resolution_source': 'cache'}, True)]:
    value = {**row, **change}
    try:
        result = monitor._record(value, detail)
    except monitor.MonitorError as exc:
        result = {'error': exc.code}
    records.append({'value': value, 'detail': detail, 'result': dump(result)})
records.append({'value': {k: v for k, v in row.items() if k != 'answers_json'}, 'detail': False,
                'result': dump({'error': 'INVALID_RESPONSE'})})
OUT['record'] = records

stamps = [0, 1, 999, 1000, 1001, 1792100000123, 253402300799999, 253402300799998, 253402300799000, 4102444800000,
          1700000000001, 1700000000005, 1999999999999]
stamps += [random.randint(0, 253402300799999) for _ in range(300)]
OUT['timestamps'] = [[ms, datetime.datetime.fromtimestamp(ms / 1000, datetime.timezone.utc).isoformat()] for ms in stamps]

# --- _request: answers of a fake Agent -----------------------------------------------------


class Response:
    def __init__(self, status, body):
        self.status_code, self.body = status, body
    def __enter__(self): return self
    def __exit__(self, *args): pass
    def iter_content(self, size):
        for start in range(0, len(self.body), size):
            yield self.body[start:start + size]


class Session:
    def __init__(self, response):
        self.response, self.trust_env = response, True
        self.seen = None
    def __enter__(self): return self
    def __exit__(self, *args): pass
    def get(self, url, **kwargs):
        Session.last = (url, kwargs)
        return self.response


answers = [
    (200, b'\xef\xbb\xbf{"ok": true}'), (200, '{"ok": true, "x": "\u00e9"}'.encode('utf-16')),
    (200, '{"ok": true}'.encode('utf-16-le')), (200, '{"ok": true}'.encode('utf-32')), (200, b'{"ok": true, "x": "\xff"}'),
    (200, b'{"ok": true, "x": NaN, "y": -Infinity, "z": 1e400}'), (200, b'{"x": ' + b'1' * 4301 + b'}'),
    (200, b'{"x": ' + b'1' * 4300 + b'}'), (502, b'{"success": false, "code": ["x"]}'), (502, b'{"success": false, "code": 5}'),
    (200, b'{"success": false, "code": "INVALID_QUERY"}'), (200, b'{"ok": false}'), (200, b'{"ok": 0}'),
    (200, b'{"success": 0, "ok": null}'), (404, b'{"message": "x"}'), (404, b'<html>'), (404, b''),
    (500, b'{"code": "INVALID_QUERY"}'), (409, b'{"code": "SNAPSHOT_CHANGED"}'), (500, b'{"code": "UPGRADE_REQUIRED"}'),
    (418, b'{"code": "RECORD_NOT_FOUND"}'), (400, b'{"code": "DASHBOARD_STATE_ERROR"}'), (300, b'{}'), (399, b'{}'),
    (400, b'not json'), (200, b'"text"'), (200, b'null'), (200, b'{"a": 1, "a": 2}'), (299, b'{"ok": true}'),
    (200, b' {"ok": true} '), (200, b'{"ok": true}\n\x00'), (204, b''), (200, b'{"code": "RECORD_NOT_FOUND"}'),
]
OUT['responses'] = []
agent = {'id': 'oxi', 'host': '127.0.0.1', 'port': 8082, 'token': 'tok', 'service_type': 'oxidns', 'version': '1.8.14-go'}
original_session = monitor.requests.Session
try:
    for status, body in answers:
        monitor.requests.Session = lambda: Session(Response(status, body))
        try:
            result = {'data': monitor._request(agent, 'clients', {'search': 'a b'}, min_version=monitor.MIN_CLIENTS_AGENT_VERSION)}
        except Exception as exc:  # noqa: BLE001
            result = error_of(exc)
        OUT['responses'].append({'status': status, 'body': body.hex(), 'result': dump(result)})
finally:
    monitor.requests.Session = original_session

# --- advance / totals ---------------------------------------------------------------------

secrets_token_hex = secrets.token_hex
publication.secrets.token_hex = lambda n=16: 'f' * 32
observed = json.loads(json.dumps(metrics.parse_metrics(server('s', 'udp', 20, 0, 40, 20, 1) + '\n' + cache('c', 4, 2, 1))))
lower = json.loads(json.dumps(metrics.parse_metrics(server('s', 'udp', 2, 0, 4, 2, 1) + '\n' + cache('c', 1, 1, 0))))
snapshot = {'collected_at_ms': 5000, 'source_revision': 'e' * 64, 'runtime_revision': 'd' * 64}
core = {'instance_id': 'core'}
pending_state = {**base2, 'baseline': obs, 'carried': lower, 'publication_count': 2,
                 'pending': {**base2['pending'], 'collected_at_ms': 3000, 'totals': observed}}


def deploy(status, **extra):
    return {'deployment_id': 'dep-1', 'deployments': {'dep-1': {'status': status, 'service_type': 'oxidns', 'profile_id': 'default', **extra}},
            'latest_deployment': {'deployment_id': 'dep-1', 'status': status}, 'profile_id': 'default'}


agents_variants = [None, {}, deploy('ready'), deploy('uploading'), deploy('succeeded'), deploy('succeeded', manual_rollback=True),
                   deploy('rolled_back'), deploy('failed'), deploy('rollback_failed'), deploy(['x']), deploy(''), deploy(5),
                   {**deploy('succeeded'), 'latest_deployment': {'deployment_id': 'dep-2', 'status': 'preparing'}},
                   {**deploy('succeeded'), 'latest_deployment': {'deployment_id': 'dep-2', 'status': 'failed'}},
                   {**deploy('succeeded'), 'profile_id': None}, {**deploy('succeeded'), 'profile_id': 'other'},
                   {**deploy('succeeded'), 'deployments': ['x']}, {**deploy('succeeded'), 'latest_deployment': 'x'},
                   {**deploy('succeeded'), 'deployments': {'dep-1': 'x'}}, {**deploy('succeeded'), 'deployments': {'dep-1': {}}},
                   {**deploy('succeeded'), 'deployments': []}]
cases = []
for previous_name, previous in [('none', None), ('schema1', base1), ('pending', pending_state)]:
    for snap_change in [{}, {'source_revision': 'c' * 64}, {'runtime_revision': 'a' * 64}, {'collected_at_ms': 2000}]:
        for core_value in [core, {'instance_id': 'other'}]:
            for observations in [observed, lower]:
                for target in ['b' * 64, '9' * 64]:
                    for manual in [False, True]:
                        for agent_value in (agents_variants if previous_name == 'pending' else [None]):
                            snap = {**snapshot, **snap_change}
                            try:
                                state = publication.advance(copy.deepcopy(previous), snap, core_value, observations, target,
                                                            copy.deepcopy(agent_value), manual=manual)
                                adjusted, partial = publication.totals(state, observations)
                                result = {'state': state, 'same': state == previous, 'totals': adjusted, 'partial': partial}
                            except publication.PublicationPending:
                                result = {'error': 'PublicationPending'}
                            except Exception as exc:  # noqa: BLE001
                                result = {'error': type(exc).__name__}
                            cases.append({'previous': previous, 'snapshot': snap, 'core': core_value, 'observations': observations,
                                          'target': target, 'manual': manual, 'agent': agent_value, 'result': dump(result)})
publication.secrets.token_hex = secrets_token_hex
OUT['advance'] = cases

path = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'a06_reference.json.gz')
with gzip.GzipFile(path, 'wb', mtime=0) as handle:
    handle.write(json.dumps(OUT, ensure_ascii=True, sort_keys=True).encode())
print(path, {key: len(value) if isinstance(value, list) else 1 for key, value in OUT.items()})
