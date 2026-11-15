"""Adversarial review corpus for A06 (randomised, synthetic values only).

Runs the Python functions on generated edge inputs and writes
a06_review.json.gz next to this file; a06_review_test.go asserts the Go port
gives the same results.

    rt -- bash -lc 'cd server/internal/agents/monitor && PYTHONPATH=../../../.. ~/cf-test/.venv/bin/python testdata/gen_a06_review.py'

Every input that is a JSON value is stored as JSON text and loaded again
before Python sees it, so both sides start from the same parsed value.
"""
import copy
import gzip
import ipaddress
import json
import os
import random

from werkzeug.datastructures import MultiDict

from backend.agents import oxidns_metrics as metrics
from backend.agents import oxidns_monitor as monitor
from backend.agents import oxidns_dashboard_state as storage

random.seed(20261115)
OUT = {}


def dump(value):
    return json.dumps(value, sort_keys=True)


def rt(value):
    """JSON text and the value Python reads back from it."""
    text = json.dumps(value)
    return text, json.loads(text)


def code_of(exc):
    if isinstance(exc, monitor.MonitorError):
        return {'error': exc.code, 'status': exc.status, 'message': exc.message}
    return {'error': type(exc).__name__}


# --- metrics text fuzz -------------------------------------------------------------

FAMILIES = ['server_request_total', 'server_failed_total', 'server_inflight', 'server_latency_sum_ms',
            'server_latency_count', 'cache_lookup_total', 'cache_miss_total', 'cache_expired_total',
            'cache_entry_count', 'cache_hit_total', 'forward_upstream_query_total', 'forward_upstream_error_total',
            'forward_upstream_latency_sum_ms', 'forward_upstream_latency_count']
NAMES = FAMILIES + ['oxidns_' + f for f in FAMILIES] + ['other_total', 'oxidns_', 'Server_request_total', ':x',
                                                        'cache_hit_total_x', '9bad', 'oxidns_oxidns_cache_entry_count']
LABEL_VALUES = ['a', 'b', 'udp', 'tcp', 'fresh', 'stale', 'Fresh', '', ' ', 'x y', '\\"q\\"', 'back\\\\slash', 'nl\\n',
                'bad\\t', 'bad\\x', '測試', 'é', 'a,b', 'a=b', 'a}b', '{', 'İ', 'ı', '　', 'tls://1.2.3.4:853',
                'https://u:p@h/q?x', '\\\\n', '\\', 'trail\\']
LABEL_NAMES = ['plugin_tag', 'protocol', 'upstream', 'kind', 'other', 'plugin_tag', '_x', '9x', 'Plugin_tag']
NUMBERS = ['0', '1', '2', '5', '10', '+3', '-0', '-0.0', '0.0', '1.', '.5', '1.5', '1e2', '1E+2', '1e-2', '1e400',
           '1e308', '1.7976931348623157e308', '1.8e308', '9007199254740993', '9007199254740992.0', '-1', '-1e-400',
           '١٢', '٣.٥', '١e٢', '𝟗', '１２', '꧐', '0x10', 'inf', '+Inf', 'NaN', 'nan', '1_0', '1e', '.', '+', '', '٫5',
           '5٫', '12345678901234567890', '1' * 309, '1' * 310, '0' * 4300 + '7', '0' * 4301 + '7', '2.5e-324',
           '4.9e-324', '1e-324', '00', '007', '7.000', '3.14159', '100', '99', '1e1']
SPACES = [' ', '\t', '  ', ' \t', ' ', '　', '\x1f']
BREAKS = ['\n', '\n', '\n', '\r\n', '\r', '\x0b', '\x0c', '\x1c', '\x1d', '\x1e', '\x85', ' ', ' ', '\n\n']
EDGES = ['', ' ', '\t', '　', '\x1f', ' ', ' ', '﻿']


def label_text():
    count = random.choice([0, 1, 1, 2, 2, 2, 3, 3, 4])
    parts = []
    for _ in range(count):
        name = random.choice(LABEL_NAMES)
        if random.random() < 0.6:
            name = random.choice(['plugin_tag', 'protocol', 'upstream', 'kind'])
        value = random.choice(LABEL_VALUES)
        eq = random.choice(['=', '=', '=', ' = ', '\t=', '= '])
        parts.append(f'{name}{eq}"{value}"')
    sep = random.choice([',', ',', ', ', ' ,', ',,', ' '])
    text = sep.join(parts)
    if random.random() < 0.1:
        text += random.choice([',', ' ', ', ', '"', 'x'])
    if random.random() < 0.1:
        text = random.choice([' ', '\t']) + text
    return text


def sample_line():
    name = random.choice(NAMES)
    labels = label_text()
    body = name
    if random.random() < 0.95:
        body += '{' + labels + '}'
    body += random.choice(SPACES[:3]) if random.random() < 0.95 else random.choice(SPACES)
    body += random.choice(NUMBERS)
    if random.random() < 0.15:
        body += random.choice([' ', '\t']) + random.choice(['1792100000000', '-5', '+7', '١٢', 'x', '1.5', '1 2'])
    if random.random() < 0.2:
        body += random.choice(SPACES)
    return random.choice(EDGES) + body + random.choice(EDGES)


def metrics_text():
    lines = []
    for _ in range(random.randint(1, 10)):
        r = random.random()
        if r < 0.08:
            lines.append(random.choice(['# HELP x', '  # comment', '#', '', ' ', '　']))
        elif r < 0.25 and lines:
            # Repeat an earlier series with another value (alias or duplicate).
            previous = random.choice(lines)
            for number in NUMBERS:
                if previous.rstrip().endswith(' ' + number):
                    previous = previous.rstrip()[:-len(number)] + random.choice(NUMBERS)
                    break
            lines.append(previous)
        else:
            lines.append(sample_line())
    text = ''
    for line in lines:
        text += line + random.choice(BREAKS)
    if random.random() < 0.5:
        text = text.rstrip('\n')
    return text


def parse_or_error(text):
    try:
        return {'parsed': metrics.parse_metrics(text), 'summary': metrics.summarize_metrics(text)}
    except ValueError as exc:
        return {'error': str(exc)}


OUT['metrics'] = []
for _ in range(2500):
    text = metrics_text()
    OUT['metrics'].append({'text': text, 'result': dump(parse_or_error(text))})


# Structured observations for baseline arithmetic: every series of every
# family with random values (ints, floats, huge ints, None).

def obs_value():
    return random.choice([None, 0, 1, 2, 3, 5, 10, 100, 1.5, 2.0, 0.0, -0.0, 10 ** 20, 10 ** 20 + 1, 10 ** 308,
                          1.7e308, 9007199254740993, 9007199254740992.0, 1e-300, 7, 7.0])


def observations():
    result = {'servers': [], 'caches': [], 'upstreams': []}
    for tag in random.sample(['a', 'b', 'c'], random.randint(0, 3)):
        for protocol in random.sample(['udp', 'tcp'], random.randint(1, 2)):
            row = {'tag': tag, 'protocol': protocol}
            for field in ('requests', 'failed', 'inflight', 'latency_sum_ms', 'latency_count'):
                if random.random() < 0.95:
                    row[field] = obs_value()
            result['servers'].append(row)
        row = {'tag': tag}
        for field in ('lookups', 'fresh_hits', 'stale_hits', 'misses', 'expired', 'entries'):
            if random.random() < 0.95:
                row[field] = obs_value()
        result['caches'].append(row)
        for upstream in random.sample(['x', 'y', 'é'], random.randint(0, 2)):
            row = {'tag': tag, 'upstream': upstream}
            for field in ('requests', 'failed', 'latency_sum_ms', 'latency_count'):
                if random.random() < 0.95:
                    row[field] = obs_value()
            result['upstreams'].append(row)
    for kind in result:
        result[kind].sort(key=lambda r: (r['tag'], r.get('protocol', r.get('upstream', ''))))
    return result


def bump(observed):
    """A later snapshot: mostly larger counters, sometimes resets."""
    later = copy.deepcopy(observed)
    for kind in later:
        for row in later[kind]:
            for key, value in list(row.items()):
                if key in ('tag', 'protocol', 'upstream') or value is None:
                    continue
                r = random.random()
                if r < 0.6:
                    row[key] = value + random.choice([0, 1, 5, 0.5, 10 ** 20])
                elif r < 0.7:
                    row[key] = random.choice([0, 1, None])
    if random.random() < 0.2 and later['servers']:
        later['servers'].pop()
    if random.random() < 0.2:
        later['upstreams'].append({'tag': 'z', 'upstream': 'new', 'requests': 1, 'failed': 0,
                                   'latency_sum_ms': 2, 'latency_count': 1})
    return later


OUT['baseline'] = []
for _ in range(700):
    base_text, baseline = rt(observations())
    current_text, current = rt(bump(baseline))
    carried_text, carried = rt(observations())
    try:
        adjusted, partial = metrics.apply_metrics_baseline(current, baseline)
        added = metrics.add_observations(adjusted, carried)
        result = {'restarted': metrics.metrics_counters_restarted(current, baseline), 'adjusted': adjusted,
                  'partial': partial, 'zero': metrics.zero_baseline(current), 'added': added,
                  'summary': metrics.summarize_observations(added), 'raw_summary': metrics.summarize_observations(current)}
    except Exception as exc:  # noqa: BLE001
        result = {'error': type(exc).__name__}
    OUT['baseline'].append({'current': current_text, 'baseline': base_text, 'carried': carried_text, 'result': dump(result)})


# --- ip addresses ----------------------------------------------------------------

def random_ipv6():
    groups = [format(random.randint(0, 0xffff), random.choice(['x', 'X', '04x'])) for _ in range(8)]
    for i in range(8):
        if random.random() < 0.3:
            groups[i] = random.choice(['0', '00', '0000', '000'])
    if random.random() < 0.3:
        v4 = '.'.join(str(random.randint(0, 255)) for _ in range(4))
        groups = groups[:6] + [v4]
    if random.random() < 0.6:
        start = random.randint(0, len(groups) - 1)
        end = random.randint(start, len(groups))
        text = ':'.join(groups[:start]) + '::' + ':'.join(groups[end:])
    else:
        text = ':'.join(groups)
    if random.random() < 0.1:
        text = random.choice(['::ffff:', '::', '64:ff9b::', '::ffff:0:']) + '.'.join(
            str(random.randint(0, 255)) for _ in range(4))
    if random.random() < 0.1:
        text += '%' + random.choice(['eth0', '1', 'x%y', '', ' ', '25', 'é'])
    return text


def mutate(text):
    chars = list(text)
    for _ in range(random.randint(1, 3)):
        op = random.random()
        position = random.randint(0, len(chars))
        if op < 0.4 and chars:
            del chars[min(position, len(chars) - 1)]
        elif op < 0.8:
            chars.insert(position, random.choice('0123456789abcdefABCDEF:.%g١ -'))
        elif chars:
            chars[min(position, len(chars) - 1)] = random.choice(':.0f')
    return ''.join(chars)


ips = []
for _ in range(2500):
    r = random.random()
    if r < 0.35:
        ips.append(random_ipv6())
    elif r < 0.55:
        ips.append('.'.join(random.choice(['0', '1', '01', '255', '256', '00', '127', '1000', '٣']) for _ in range(
            random.choice([3, 4, 4, 4, 5]))))
    else:
        ips.append(mutate(random_ipv6() if random.random() < 0.6 else '192.168.1.10'))
OUT['ip'] = []
for ip in ips:
    try:
        OUT['ip'].append([ip, str(ipaddress.ip_address(ip))])
    except ValueError:
        OUT['ip'].append([ip, None])


# --- upstream labels --------------------------------------------------------------

def random_url():
    scheme = random.choice(['https', 'HTTPS', 'tls', 'udp', 'quic', 'h3', '1x', 'a+b.c-d', 'é', '', 'sdns', 'tcp'])
    user = random.choice(['', '', 'u@', 'u:p@', 'a@b@', '@', 'ü:p@'])
    host = random.choice(['host', 'Host.Example', '1.2.3.4', '[::1]', '[2001:DB8::1]', '[fe80::1%25eth0]',
                          '[fe80::1%eth0]', '[v1.fe]', '[1.2.3.4]', '', '[::1', '::1]', 'İSTANBUL', 'ΣΑΣ', 'a℀b',
                          'h　x', 'xn--abc', '[::ffff:1.2.3.4]', 'host:', 'a b', 'ho%st', '[]', 'ＨＯＳＴ'])
    port = random.choice(['', '', ':53', ':0', ':65535', ':65536', ':', ':abc', ':٥٣', ':+53', ': 53', ':-1',
                          ':053', ':99999999999999999999'])
    path = random.choice(['', '/', '/dns-query', '/a/../b', '/p?q=1', '/p#f', '?x', '#y', '/é', '//x'])
    sep = random.choice(['://', '://', '://', ':/', ':///', '://'])
    text = scheme + sep + user + host + port + path
    if random.random() < 0.1:
        text = random.choice([' ', '　', '\x7f', '\x85', ' ']) + text
    if random.random() < 0.15:
        text = random.choice(['plain', 'u@h:53', 'a@b@c?x', 'dns#1', 'h?x@y', '']) + random.choice(['', '?q', '@z'])
    return text


labels = [random_url() for _ in range(2000)] + [None, 5, 1.5, True, ['x'], {'a': 1}]
OUT['upstream'] = [[label, monitor._display_upstream(label)] for label in labels]


# --- request parameters -------------------------------------------------------------

PARAM_KEYS = ['recorder', 'limit', 'cursor', 'qname', 'qtype', 'client_ip', 'rcode', 'status', 'since_ms', 'until_ms',
              'matcher_tag', 'search', 'range', 'other', 'Limit', '']
PARAM_VALUES = ['', ' ', 'r', 'records', 'a.b', 'a..b', '-a', 'a' * 64, 'a' * 65, 'x' * 257, '5', ' 5 ', '0', '100',
                '101', '０', '١', '9007199254740991', '9007199254740992', '1' * 17, '0:1', '1:0', '12:34', ' 1:2 ',
                'aaaa', 'AAAA', 'ı', 'nxdomain', 'No Error', 'servfail', 'error', 'all', 'has_response', 'no_response',
                '192.0.2.1', '2001:DB8::1', '::ffff:1.2.3.4', 'fe80::1%eth0', '01.2.3.4', '24h', '7d', '30d', 'round',
                '1h', 'a/b', 'a?b', 'a#b', 'a\\b', '\x1f', '\x7f', '\x85', ' x ', '測' * 22, '測' * 21 + 'ab',
                'İ' * 32, 'İ' * 33, '　r　', '\x00', '\t5', '5\t', '\x1c5', '5 ']


def random_args():
    return [(random.choice(PARAM_KEYS[:13]) if random.random() < 0.9 else random.choice(PARAM_KEYS),
             random.choice(PARAM_VALUES)) for _ in range(random.choice([0, 1, 1, 2, 2, 3, 4]))]


def params_case(function, args, *extra):
    try:
        result = function(MultiDict(args), *extra)
        return {'args': args, 'extra': list(extra), 'result': list(result.items())}
    except monitor.MonitorError as exc:
        return {'args': args, 'extra': list(extra), 'error': exc.code}


OUT['query_params'] = [params_case(monitor.query_params, random_args(), random.random() < 0.2) for _ in range(2500)]
OUT['client_params'] = [params_case(monitor.client_params, random_args()) for _ in range(1500)]
OUT['dashboard_params'] = [params_case(monitor._dashboard_params, random_args()) for _ in range(800)]


# --- Agent answers after _request -----------------------------------------------------

ODD = [None, True, False, 0, 1, -1, 1.0, 1.5, -0.0, 10000, 10000.0, 9999, 10001, 2 ** 53 - 1, 2 ** 53, 10 ** 30,
       1e308, float('inf'), float('nan'), '1', '', 'x', [], {}, ['x'], {'a': 1}]


def odd():
    return random.choice(ODD)


def maybe(value, p=0.1):
    return odd() if random.random() < p else value


IPS = ['192.0.2.1', '192.0.2.2', '2001:db8::1', '2001:DB8::1', '::ffff:1.2.3.4', 'fe80::1%eth0', '1.2.3', 'x',
       '01.2.3.4', '::1', '10.0.0.1', '\u0085', '']
DOMAINS = ['a.test', 'b.test', 'é.test', 'x' * 1025, 'x' * 1024, '\x7f', 'A.test', 'a.test.', '測試.test', ' ', '']


def rows(kind, sample, count):
    keys = IPS if kind == 'clients' else DOMAINS
    out = []
    for _ in range(count):
        out.append(maybe({'key': maybe(random.choice(keys), 0.05),
                          'count': maybe(random.choice([1, 1, 2, max(1, sample), sample, sample + 1, 65535,
                                                        sample * 65535]), 0.05)}, 0.03))
    return out


def section(kind):
    sample = random.choice([0, 1, 2, 3, 10, 9999, 10000, 10000, 10001])
    count = random.choice([0, 1, 2, 3, 10, 11])
    if kind == 'clients' and random.random() < 0.5:
        # A consistent client sample: counts adding up to the sample.
        keys = random.sample(IPS[:5] + ['10.0.0.%d' % i for i in range(20)], min(count, 10))
        counts = [1] * len(keys)
        if keys:
            counts[0] += max(0, sample - len(keys))
        value_rows = [{'key': k, 'count': c} for k, c in zip(keys, counts)]
        if len(keys) > sample:
            value_rows = value_rows[:sample]
    else:
        value_rows = rows(kind, sample, count)
    return maybe({'sample_size': maybe(sample, 0.05), 'rows': maybe(value_rows, 0.03),
                  'at_limit': maybe(sample == 10000, 0.05)}, 0.03)


OUT['ranking_section'] = []
for _ in range(1500):
    kind = random.choice(['clients', 'domains'])
    text, value = rt(section(kind))
    try:
        result = monitor._ranking_section(value, kind)
    except monitor.MonitorError as exc:
        result = {'error': exc.code}
    OUT['ranking_section'].append({'kind': kind, 'value': text, 'result': dump(result)})


class FakeManager:
    def __init__(self, agent, data_dir='/nonexistent-a06'):
        self.agent = agent
        self.repository = type('Repo', (), {'data_dir': data_dir})()

    def get_agent_by_id(self, agent_id, history=True):
        return copy.deepcopy(self.agent) if self.agent is not None and agent_id == 'oxi' else None


AGENT = {'id': 'oxi', 'name': 'DNS', 'host': '127.0.0.1', 'port': 8082, 'profile_id': 'default',
         'service_type': 'oxidns', 'version': '1.8.20-go', 'status': 'online', 'token': 'tok'}


def with_answer(data_text, function):
    """Run function with _request answering json.loads(data_text)."""
    original = monitor._request
    seen = []

    def fake(agent, endpoint, params=None, *, min_version=monitor.MIN_AGENT_VERSION):
        seen.append({'endpoint': endpoint, 'params': list((params or {}).items()), 'min_version': min_version})
        return json.loads(data_text)
    monitor._request = fake
    try:
        result = function()
    except Exception as exc:  # noqa: BLE001
        result = code_of(exc)
    finally:
        monitor._request = original
    return {'result': result, 'seen': seen}


def client_answer():
    body = section('clients')
    data = {'ok': maybe(True, 0.05), 'recorder': maybe(random.choice(['r', 'records', 'a..b', 'r']), 0.05),
            **(body if isinstance(body, dict) else {'rows': body})}
    if random.random() < 0.05:
        data.pop(random.choice(list(data)))
    return data


OUT['clients'] = []
for _ in range(1200):
    args = random.choice([[], [], [('recorder', 'r')], [('search', 'x')], [('recorder', 'records'), ('since_ms', '5')]])
    if random.random() < 0.2:
        args = random_args()
    data_text, _ = rt(client_answer())
    agent = dict(AGENT, version=random.choice(['1.8.20-go', '1.7.1-go', '1.7.0-go', '2.0', 'x', '1.7.1']))
    answer = with_answer(data_text, lambda: monitor.clients(FakeManager(agent), 'oxi', MultiDict(args)))
    OUT['clients'].append({'args': args, 'data': data_text, 'version': agent['version'], 'result': dump(answer)})


def record_value():
    row = {'id': maybe(random.choice([1, 42, 2 ** 53 - 1]), 0.1), 'created_at_ms': maybe(1792100000123, 0.1),
           'elapsed_ms': maybe(random.choice([0, 1.5, 3]), 0.1), 'questions_json': maybe([], 0.05),
           'answers_json': maybe([{'name': 'a.'}], 0.05), 'client_ip': '192.0.2.1', 'rcode': 'NOERROR',
           'extra': 'dropped', 'steps': maybe([{'kind': 'x'}], 0.2),
           'resolution_source': random.choice(['cache', 'remote', 'pinned', 'bogus', 5, None, 'ipv4_preferred'])}
    for key in list(row):
        if random.random() < 0.04:
            del row[key]
    return maybe(row, 0.03)


OUT['queries'] = []
for _ in range(800):
    data = {'ok': maybe(True, 0.05), 'records': maybe([record_value() for _ in range(random.choice([0, 1, 2, 3]))], 0.05),
            'next_cursor': random.choice([None, None, '0:1', '1:0', '12:34', 5, '9' * 17 + ':1', ''])}
    if random.random() < 0.05:
        data['records'] = [record_value()] * 101
    data_text, _ = rt(data)
    args = random.choice([[], [('limit', '5')], [('client_ip', '192.0.2.1')], [('recorder', 'r'), ('qname', 'a')]])
    agent = dict(AGENT, version=random.choice(['1.8.20-go', '1.7.0-go']))
    answer = with_answer(data_text, lambda: monitor.queries(FakeManager(agent), 'oxi', MultiDict(args)))
    OUT['queries'].append({'args': args, 'data': data_text, 'version': agent['version'], 'result': dump(answer)})

OUT['query'] = []
for _ in range(500):
    data_text, _ = rt({'ok': maybe(True, 0.1), 'record': record_value()})
    record_id = random.choice(['42', '1', '0', '01', '9007199254740991', '9007199254740992', '1' * 17, 'x', '-1', '٤'])
    args = random.choice([[], [('recorder', 'r')], [('limit', '5')]])
    answer = with_answer(data_text, lambda: monitor.query(FakeManager(AGENT), 'oxi', record_id, MultiDict(args)))
    OUT['query'].append({'args': args, 'record_id': record_id, 'data': data_text, 'result': dump(answer)})


# --- overview snapshot and rankings ------------------------------------------------------

def overview_answer(dashboard):
    data = {'success': True,
            'health': maybe({'version': maybe('1.6.1', 0.1), 'status': maybe('ok', 0.1),
                             'uptime_ms': odd() if random.random() < 0.5 else 1234,
                             'instance_id': maybe(random.choice(['core', 'a' * 128, 'a' * 129, 'a b', 'c:1.x-y_z']), 0.1)},
                            0.03),
            'capabilities': maybe({'query_recorders': maybe(random.choice([[], ['r'], ['r', 'records'], ['a..b'], ['r'] * 65,
                                                                           ['r'] * 64]), 0.05)}, 0.03),
            'metrics': maybe(metrics_text(), 0.03),
            'upstream_labels': maybe([maybe({'plugin_tag': maybe(random.choice(['a', 'b']), 0.03),
                                             'upstream': maybe(random.choice(['x', 'y', 'é']), 0.03),
                                             'address': maybe(random_url(), 0.03)}, 0.02)
                                      for _ in range(random.choice([0, 1, 2, 3]))], 0.03),
            'collected_at_ms': maybe(random.choice([0, 1792100000123, 253402300799999, 253402300800000]), 0.1),
            'source_revision': maybe('e' * 64, 0.05), 'runtime_revision': maybe('d' * 64, 0.05)}
    if random.random() < 0.05:
        data.pop(random.choice(list(data)))
    return data


OUT['overview_snapshot'] = []
for _ in range(1200):
    dashboard = random.random() < 0.5
    data_text, _ = rt(overview_answer(dashboard))

    def run():
        snapshot, core, recorders, observations, labels = monitor._overview_snapshot(AGENT, dashboard)
        summary = metrics.summarize_observations(observations)
        summary['upstreams'] = monitor._public_upstreams(summary.get('upstreams', []), labels)
        return {'core': core, 'recorders': recorders, 'summary': summary}
    answer = with_answer(data_text, run)
    OUT['overview_snapshot'].append({'dashboard': dashboard, 'data': data_text, 'result': dump(answer)})


def ranking_answer(state, snapshot):
    blocked_available = random.random() < 0.6
    blocked = {'available': maybe(blocked_available, 0.05)}
    if blocked_available:
        body = section('domains')
        blocked.update(body if isinstance(body, dict) and random.random() < 0.9 else {})
        if random.random() < 0.7:
            blocked['reason'] = maybe(random.choice(['standard_adguard', 'rule_dns', 'standard_adguard_rule_dns',
                                                     'other_reason', 'Bad', 'a' * 65]), 0.05)
        if random.random() < 0.3:
            blocked['matcher_tag'] = maybe(random.choice(['block', 'a..b']), 0.1)
        if random.random() < 0.3:
            blocked['matcher_tags'] = maybe(random.choice([['a', 'b'], [], ['x'] * 33, ['x'] * 32, ['a..b']]), 0.1)
    else:
        if random.random() < 0.8:
            blocked['reason'] = maybe(random.choice(['too_many_block_rules', 'unsupported_configuration', 'x_y',
                                                     'Upper', '']), 0.1)
    data = {'success': maybe(True, 0.05), 'recorder': maybe(random.choice(['r', 'records']), 0.05),
            'collected_at_ms': maybe(snapshot['collected_at_ms'] + random.choice([0, 0, 1, -1]), 0.05),
            'sample_limit': maybe(10000, 0.1), 'instance_id': maybe(state['instance_id'], 0.05),
            'source_revision': maybe(state['source_revision'], 0.03),
            'runtime_revision': maybe(state['runtime_revision'], 0.03),
            'clients': section('clients'), 'domains': section('domains'), 'blocked': maybe(blocked, 0.03)}
    if random.random() < 0.5:
        data['retention_days'] = maybe(random.choice([None, 7, 0, 36500, 36501, -1]), 0.1)
    return data


OUT['dashboard_rankings'] = []
for _ in range(1500):
    state = {'started_at_ms': random.choice([0, 1000, 1792100000000, 1792100000123]), 'instance_id': 'core',
             'source_revision': 'e' * 64, 'runtime_revision': 'd' * 64, 'generation': 'f' * 32}
    snapshot = {'collected_at_ms': random.choice([1792100000123, 86400000, 5000])}
    recorders = random.choice([[], ['r'], ['r', 'records'], ['records']])
    params = random.choice([{}, {}, {'recorder': 'r'}, {'recorder': 'records'}, {'recorder': 'gone'}])
    data_text, _ = rt(ranking_answer(state, snapshot))
    answer = with_answer(data_text, lambda: monitor._dashboard_rankings(AGENT, dict(params), snapshot, state, recorders))
    OUT['dashboard_rankings'].append({'state': state, 'snapshot': snapshot, 'recorders': recorders,
                                      'params': list(params.items()), 'data': data_text, 'result': dump(answer)})


# --- dashboard state ------------------------------------------------------------------------

def valid_observations():
    obs = observations()
    for kind in obs:
        for row in obs[kind]:
            for key, value in list(row.items()):
                if isinstance(value, (int, float)) and not isinstance(value, bool) and value < 0:
                    row[key] = 0
    return obs


def state_value():
    state = {'schema': 1, 'generation': 'a' * 32, 'started_at_ms': 1000, 'reason': 'initial', 'target': 'b' * 64,
             'instance_id': 'core', 'source_revision': 'c' * 64, 'runtime_revision': 'd' * 64,
             'baseline': valid_observations()}
    if random.random() < 0.5:
        state.update({'schema': 2, 'carried': valid_observations(), 'carried_partial': False, 'publication_count': 1,
                      'pending': random.choice([None, {'deployment_id': 'dep_1-x', 'expected_revision': 'e' * 64,
                                                       'collected_at_ms': 1000, 'totals': valid_observations(),
                                                       'partial': True}])})
    for _ in range(random.choice([0, 0, 1, 1, 2])):
        r = random.random()
        target = state
        if r < 0.3:
            observations_key = random.choice(['baseline', 'carried', 'pending'])
            target = state.get(observations_key)
            if isinstance(target, dict) and observations_key == 'pending':
                target = target.get('totals') if random.random() < 0.5 else target
            if isinstance(target, dict) and random.random() < 0.7:
                family = random.choice(list(target)) if target else None
                rows_ = target.get(family) if family else None
                if isinstance(rows_, list) and rows_:
                    target = random.choice(rows_)
        if not isinstance(target, dict):
            continue
        op = random.random()
        if op < 0.2 and target:
            del target[random.choice(list(target))]
        elif op < 0.3:
            target[random.choice(['extra', 'tag', 'requests', 'schema'])] = odd()
        elif target:
            target[random.choice(list(target))] = odd() if random.random() < 0.7 else random.choice(
                ['a' * 32, 'b' * 64, 'manual', 'publication', 'x' * 16385, 1.0, 2.0, 2, 0, 2 ** 53, -1, 'A' * 64])
    return state


OUT['validate_state'] = []
for _ in range(2500):
    text, value = rt(state_value())
    try:
        valid = storage.validate_state(value)
    except TypeError:
        valid = 'TypeError'
    OUT['validate_state'].append([text, valid])

# Raw state file bytes through DashboardStateStore._read.
import tempfile  # noqa: E402
from pathlib import Path  # noqa: E402

base_state = {'schema': 1, 'generation': 'a' * 32, 'started_at_ms': 1000, 'reason': 'initial', 'target': 'b' * 64,
              'instance_id': 'core', 'source_revision': 'c' * 64, 'runtime_revision': 'd' * 64,
              'baseline': {'servers': [{'tag': 'é', 'protocol': 'udp', 'requests': 1}], 'caches': [], 'upstreams': []}}
plain = json.dumps(base_state, ensure_ascii=False, separators=(',', ':'))
raw_files = [plain.encode(), b'\xef\xbb\xbf' + plain.encode(), plain.encode('utf-16'), plain.encode('utf-16-le'),
             plain.encode('utf-16-be'), plain.encode('utf-32'), plain.encode('utf-32-le'), b'', b' ', b'null',
             plain.encode() + b'\n', b'\n' + plain.encode(), plain.encode().replace(b'"core"', b'"core","schema":1'),
             plain.encode().replace(b'"core"', b'"co\\u0000re"'), plain.encode().replace(b'"core"', b'"\xff"'),
             plain.encode().replace(b'"core"', b'"\\ud800"'), plain.encode().replace(b'"core"', b'"\xed\xa0\x80"'),
             plain.encode().replace(b'"requests":1', b'"requests":1' + b'0' * 4300),
             plain.encode().replace(b'"requests":1', b'"requests":1' + b'0' * 4299),
             plain.encode().replace(b'"requests":1', b'"requests":1.0E+2'),
             plain.encode().replace(b'"requests":1', b'"requests":-0'),
             plain.encode().replace(b'"requests":1', b'"requests":1,"requests":2'),
             plain.encode().replace(b'"requests":1', b'"requests":-1,"requests":2'),
             plain.encode().replace(b'"started_at_ms":1000', b'"started_at_ms":1000,"started_at_ms":1e3'),
             plain.encode().replace(b'"servers"', b'"\\u0073ervers"'), b'[' * 2000 + b']' * 2000,
             plain.encode().replace(b'"instance_id":"core"', b'"instance_id":"' + 'é'.encode() * 128 + b'"'),
             plain.encode().replace(b'"instance_id":"core"', b'"instance_id":"' + '𝟘'.encode() * 128 + b'"'),
             plain.encode().replace(b'"instance_id":"core"', b'"instance_id":"' + '𝟘'.encode() * 129 + b'"')]
OUT['state_files'] = []
with tempfile.TemporaryDirectory() as directory:
    store = storage.DashboardStateStore(directory)
    path = Path(directory) / 'state.json'
    for raw in raw_files:
        path.write_bytes(raw)
        try:
            result = {'state': store._read(path)}
        except storage.DashboardStateError as exc:
            result = {'error': str(exc)}
        except Exception as exc:  # noqa: BLE001
            result = {'error': type(exc).__name__}
        OUT['state_files'].append({'raw': raw.hex(), 'result': json.dumps(result, sort_keys=True)})


# --- target digests -------------------------------------------------------------------------

OUT['target'] = []
for _ in range(300):
    agent = {'id': maybe('oxi', 0.2), 'host': maybe(random.choice(['127.0.0.1', '測試', 'h ']), 0.2),
             'port': maybe(8082, 0.3), 'token': maybe(random.choice(['tok', 'tök', '😀']), 0.2),
             'service_type': maybe('oxidns', 0.2)}
    for key in list(agent):
        if random.random() < 0.05:
            del agent[key]
    text, value = rt(agent)
    OUT['target'].append([text, monitor._target(value)])


path = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'a06_review.json.gz')
with gzip.GzipFile(path, 'wb', mtime=0) as handle:
    handle.write(json.dumps(OUT, ensure_ascii=True, sort_keys=True).encode())
print(path, {key: len(value) for key, value in OUT.items()})
