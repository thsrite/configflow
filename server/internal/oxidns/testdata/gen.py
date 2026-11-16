#!/usr/bin/env python3
"""Generate corpus.jsonl.gz: random OxiDNS settings, profiles, native YAML,
attachments, documents and the other inputs of C05/C22 (and of the rule_dns,
device policy and validation functions ported with them), with what Python
makes of them.

Run from the repository root on the test server (CPython 3.11, the backend venv):

    PYTHONPATH=.:server/internal/oxidns/testdata ~/cf-test/.venv/bin/python \\
        server/internal/oxidns/testdata/gen.py [scale]

Every value is synthetic (example.test names, documentation/private
addresses). Rows have the format of c22_record.py (fn, args, out | err,
issues?, references?, unholdable?).
"""

import base64
import gzip
import json
import random
import sys
from pathlib import Path

import c22_record as rec
from backend.common import oxidns_settings as st
from backend.common import profile_context as pc
from backend.converters import oxidns as cv
from backend.common import oxidns_native as nv
from backend.common import oxidns_references as rf
from backend.common import rule_dns as rd
from backend.common import oxidns_device_policy as dp
from backend.common import oxidns_validation as vl

SCALE = float(sys.argv[1]) if __name__ == '__main__' and len(sys.argv) > 1 else 1.0
R = random.Random(20261116)
OUT = []


def n(count):
    return max(1, int(count * SCALE))


def row(fn, args, call, out=lambda v: v, unholdable=False):
    entry = {'fn': fn}
    if unholdable:
        entry['unholdable'] = True
    try:
        entry['args'] = rec.conv(args)
    except (TypeError, RecursionError):
        return
    try:
        result = call()
    except Exception as exc:  # noqa: BLE001 - every Python exception is part of the contract
        entry['err'] = [type(exc).__name__, str(exc)]
        try:
            if hasattr(exc, 'issues'):
                entry['issues'] = rec.conv(exc.issues)
            if hasattr(exc, 'references'):
                entry['references'] = rec.conv(exc.references)
        except (TypeError, RecursionError):
            return
    else:
        try:
            entry['out'] = rec.conv(out(result))
        except (TypeError, RecursionError):
            return
    OUT.append(entry)


def pick(*values):
    return R.choice(values)


def maybe(p=0.5):
    return R.random() < p


# ---- value pools ------------------------------------------------------------

WEIRD = [None, True, False, 0, 1, -1, 1.5, '', ' ', 'x', [], {}, ['a'], {'a': 1}, 2**31, 2**64, '１']
INTS = [0, 1, 2, 7, 64, 255, 256, 257, 65535, 65536, 2**31 - 1, 2**31, -1, True, False, 1.0, '1', None, 10**20]
BOOLS = [True, False, 0, 1, None, 'true']
PATHS = ['a.txt', './a.txt', 'dir/a.txt', 'state/x.db', './state/x.db', '../x', '/abs', 'a//b', 'a/./b', '.',
         './', '', 'a\\b', 'a\x01b', 'rules/r.txt', '.configflow/x', 'config.yaml', 'manifest.json', 'state',
         'dir', 'dir/sub/a.txt', '././a', 'a/', 'ü/文件.txt', 5, None]
DOMAINS = ['example.test', 'a.example.test', 'sub.example.test', 'x-y.test', '*.example.test', '+.example.test',
           'ex ample.test', '例子.test', 'EXAMPLE.TEST', '']
UPSTREAM_TEXTS = [
    '', '   ', 'udp://192.0.2.1', 'udp://192.0.2.1\ntls://192.0.2.2 192.0.2.53', 'https://dns.example.test/dns-query bootstrap=192.0.2.53',
    '- addr: udp://192.0.2.1\n  enable_pipeline: true', '- udp://192.0.2.1\n- addr: tcp://192.0.2.2',
    'addr: udp://192.0.2.9', '[udp://192.0.2.1, udp://192.0.2.2]', '{addr: udp://192.0.2.3}', '- {}', '- addr: ""',
    '- addr: [', 'null', '~', '[]', '- 5', '- addr: 5', '- {1: x, addr: udp://192.0.2.1}', '- &a {addr: udp://192.0.2.1}\n- *a',
    '  - addr: udp://192.0.2.1', '# comment only', 'addr:', '- addr: udp://192.0.2.1\n  bootstrap: 192.0.2.53',
    '192.0.2.1:5353', '- !!set {a}', 'tls://[2001:db8::1]:853', '- addr: " "',
]
HOST_LINES = [
    'example.test 192.0.2.1', 'example.test 192.0.2.1 2001:db8::1', '192.0.2.1 example.test', '192.0.2.1 a.test b.test',
    '192.0.2.1 a.test # comment', '# only comment', '', '   ', 'full:a.test 192.0.2.2', 'domain:a.test 192.0.2.2',
    '192.0.2.1 full:x.test', '192.0.2.1 1.2.3', '192.0.2.1 ١٢٣', '192.0.2.1 fe80::1', 'a.test fe80::1%eth0',
    '192.0.2.1', 'a.test', '192.0.2.1 a.test 192.0.2.2', 'keyword: 192.0.2.1', '192.0.2.1 regexp:', '192.0.2.1 _a.test.',
    '192.0.2.01 a.test', '2001:db8::1 v6.test', 'a.test\t192.0.2.1\t# tab', '192.0.2.1 bad_name!', 'x.test 999.1.1.1',
]
ENDINGS = ['\n', '\r\n', '\r', '', '\x0b', ' ', '\x1c', '\x85']


def hosts_text():
    return ''.join(pick(*HOST_LINES) + pick(*ENDINGS) for _ in range(R.randint(0, 5)))


NATIVE_SNIPPETS = [
    'plugins: []', 'plugins:\n- tag: a\n  type: forward\n  args: {upstreams: [{addr: udp://192.0.2.1}]}',
    'log: {level: info}\nplugins: []', 'a: yes\nb: on\nc: 0x1F\nd: 0o17\ne: 0b101\nf: 1_000\ng: 2026-01-01\nh: .inf',
    'a: .nan', 'a: -.inf', 'a: 1e3\nb: 1.5e-3\nc: .5\nd: 3.', '__proto__: 1', 'constructor: 1', 'a: 1\na: 2',
    '1: a', 'true: a', '~: a', '[a]: 1', '{a: 1}: 2', '- a\n- b', 'just text', '', '# c', 'a: &x {b: 1}\nc: *x',
    'a: &x [1, *x]', 'a: !!set {x}', 'a: !!binary aGVsbG8=', 'a: !!timestamp 2026-01-01', 'a: !!int "0x1F"',
    'a: !!bool "maybe"', 'a: [', 'a: b: c', 'a: !!omap [{x: 1}]', 'a: !!float "x"', 'a: +12', 'a: -0x10',
    'a: 0x_1F', 'a: 012', 'a: 1__0', 'a: True\nb: FALSE\nc: tRuE', '<<: {a: 1}', 'a: {<<: {b: 1}}',
    'include: [x.yaml]\nplugins: []', 'include: x.yaml', 'include: [5]', 'plugins: {}', 'plugins: 5',
    'api: {http: {listen: 127.0.0.1:9199}}',
]


def deep_yaml(depth):
    return 'a: ' + '[' * depth + ']' * depth


def native_text():
    k = R.random()
    if k < 0.1:
        return deep_yaml(R.choice([30, 31, 32, 33, 40, 200]))
    if k < 0.15:
        return 'a: [' + ', '.join(str(i) for i in range(R.choice([10, 49999, 50001]))) + ']'
    if k < 0.2:
        return 'x: &a [1, 2, 3]\n' + '\n'.join(f'k{i}: [*a, *a, *a, *a]' for i in range(R.choice([10, 3000, 4500])))
    parts = R.sample(NATIVE_SNIPPETS, R.randint(1, 3))
    return '\n'.join(parts) if maybe(0.7) else R.choice(parts)


def plugin():
    kind = pick('forward', 'sequence', 'cache', 'hosts', 'domain_set', 'ip_set', 'query_recorder',
                'dynamic_domain_set', 'udp_server', 'download', 'reload_provider', 'unknown', 5, None)
    tag = pick('a', 'b.c', 'sequence_main', 'rules_download', 'qs.exec.x', 'bad tag', '', 'x' * 65, 'x' * 64,
               '${TAG}', 5, None, 'cf_dns_blackhole', 'lazy_cache')
    p = {}
    if maybe(0.95):
        p['tag'] = tag
    if maybe(0.95):
        p['type'] = kind
    a = R.random()
    if a < 0.15:
        p['args'] = pick('$a', ['$a', 'b'], None, 5, [], {})
    elif a < 0.85:
        args = {}
        for key in R.sample(['files', 'file', 'cert', 'key', 'persistent', 'tls', 'dump_file', 'path', 'exec', 'upstreams'],
                            R.randint(0, 4)):
            if key == 'files':
                args[key] = pick(['rules/a.txt'], ['./rules/a.txt', '/etc/x'], 'rules/a.txt', [''], [5], ['${HOME}/x'], [])
            elif key in ('persistent', 'tls'):
                args[key] = pick({'files': ['rules/b.txt']}, {'ca': 'ca.pem'}, {'ca': ''}, 'x', None, {'files': 'x'})
            elif key == 'upstreams':
                args[key] = [{'addr': 'udp://192.0.2.1'}]
            elif key == 'exec':
                args[key] = pick('$a', 'goto b', '${X}', 'jump sequence_main')
            else:
                args[key] = pick('a.pem', './x/y.db', '/abs/x', '${X}/y', '', 5, None, '../up', 'state/q.db')
        p['args'] = args
    return p


def document():
    doc = {}
    if maybe(0.9):
        doc['plugins'] = [plugin() for _ in range(R.randint(0, 5))] if maybe(0.95) else pick({}, 5, None)
    if maybe(0.3):
        doc['api'] = pick(
            {'http': {'listen': '127.0.0.1:9199', 'ssl': {'cert': 'c.pem', 'key': 'k.pem', 'client_ca': pick('ca.pem', '', None)},
                      'webui': {'root': pick('ui', './ui/', '/srv/ui', '', 5, 'a//b/.'), 'index': pick('index.html', '', '/x.html', None, 5)}}},
            {'http': None}, {'http': 5}, None, 5, {'http': {'webui': {'root': 'ui'}}}, {'http': {'ssl': 'x'}})
    if maybe(0.2):
        doc['log'] = {'level': 'info'}
    return doc


def additional_files():
    files = []
    for _ in range(R.randint(0, 4)):
        entry = {}
        if maybe(0.95):
            entry['path'] = pick(*PATHS)
        enc = pick('text', 'base64', None, 'hex', 5)
        if maybe(0.9):
            entry['encoding'] = enc
        if enc == 'base64':
            raw = R.choice([b'', b'a', b'ab', b'abc', b'plugins: []\n', '中文'.encode(), b'\xff\xfe', b'x' * 10])
            text = base64.b64encode(raw).decode()
            text = pick(text, text.rstrip('='), text + '=', text + '==', text[:-1], ' ' + text, text + '\n', 'QQ', 'QUI',
                        'QUJD=', '=', 'Q', 'QQ=', 'A===', '中文', '')
            entry['content'] = text
        else:
            entry['content'] = pick('plugins: []\n', 'x', '', 5, None, '中文\n', 'plugins:\n- {tag: inc, type: sequence, args: []}\n')
        files.append(entry)
    if maybe(0.05):
        return pick(None, {}, 'x', [5], [{}] * 257)
    return files


def include_settings():
    names = ['inc.yaml', 'dir/inc.yaml', 'dir/sub.yaml', 'other.yaml', 'state/x.yaml', 'loop.yaml']
    files = []
    for name in R.sample(names, R.randint(0, len(names))):
        includes = R.sample(['inc.yaml', 'dir/inc.yaml', 'sub.yaml', '../other.yaml', 'loop.yaml', 'missing.yaml',
                             '/abs.yaml', '${X}.yaml', '', 'a\\b.yaml', '../../x.yaml', './dir/sub.yaml'], R.randint(0, 2))
        body = {'plugins': [{'tag': f'p_{len(files)}', 'type': 'sequence', 'args': []}]}
        if includes:
            body['include'] = includes
        if maybe(0.3):
            body['log'] = {'level': 'debug'}
        text = rec_yaml(body)
        if maybe(0.1):
            text = pick('not: [valid', 'a: 1\na: 2', '- x', '')
        entry = {'path': name, 'content': text}
        if maybe(0.1):
            entry = {'path': name, 'content': base64.b64encode(b'\xff\xfeplugins: []').decode(), 'encoding': 'base64'}
        files.append(entry)
    main = {'include': R.sample(['inc.yaml', 'dir/inc.yaml', 'other.yaml', 'missing.yaml', 'state/x.yaml', 'config.yaml'],
                                R.randint(0, 3)), 'plugins': [{'tag': 'm', 'type': 'sequence', 'args': []}]}
    if maybe(0.1):
        main['include'] = pick('inc.yaml', [5], None)
    if maybe(0.1):
        main['plugins'] = pick({}, None)
    return {'native_config': rec_yaml(main) if maybe(0.95) else native_text(), 'additional_files': files}


def rec_yaml(value):
    import yaml
    return yaml.safe_dump(value, allow_unicode=True, sort_keys=False)


# ---- settings ---------------------------------------------------------------

def section(name):
    defaults = st.get_default_oxidns_settings()[name]
    if maybe(0.1):
        return pick(None, 5, 'x', [])
    out = {}
    for key in R.sample(list(defaults), R.randint(0, len(defaults))):
        value = defaults[key]
        if key == 'enabled':
            out[key] = pick(True, True, False, None, 1)
        elif key == 'args' and isinstance(value, dict):
            args = {}
            for sub in R.sample(list(value), R.randint(0, len(value))):
                args[sub] = mutate(sub, value[sub])
            if maybe(0.1) and name in ('ros_address_list', 'ros_route'):
                args['tls'] = pick({'ca': 'ca.pem'}, {'insecure': True}, {'ca': 'x', 'insecure': True}, 'x', {})
            out[key] = args if maybe(0.95) else pick(None, 'x')
        else:
            out[key] = mutate(key, value)
    return out


def mutate(key, value):
    if maybe(0.55):
        return value
    if key in ('address', 'username', 'password'):
        return pick('192.0.2.1', 'admin', 'secret', '', ' ', None, 5)
    if key in ('path', 'cache_dump_file'):
        return pick(*PATHS)
    if key == 'schedule':
        return pick('30 3 * * *', '* * * *', '0 0 * * * *', '　*　*　*　*　*', 5, '')
    if key in ('routing_table', 'gateway4', 'gateway6'):
        return pick('', 'main', '192.0.2.254', None)
    if key in ('timeout', 'outbound', 'socks5'):
        return pick('60s', '5 MS', '10ſ', '1m', '-1s', '1.5s', 'abc', 30, -1, 2**64, '', None, 'user:pw@192.0.2.1:1080',
                    '192.0.2.1:1080', '[2001:db8::1]:1080', 'host:99999', 'proxy-a', ' ', '${PROXY}', True, 1.5)
    if isinstance(value, bool):
        return pick(*BOOLS)
    if isinstance(value, int):
        return pick(*INTS)
    if isinstance(value, str):
        return pick(value, '', 'x', 5, None, 'info', 'trace', 'never', 'hourly', 'direct', 'proxy', 'first', 'all',
                    'domain', 'stop', 'nodata', 'skip', 'refused', 'best_within_budget', 'before', 'https://lists.example.test/a.txt')
    if isinstance(value, list):
        return pick(value, [], ['x'], [5], 'x', None, ['tcp:443'], ['https://lists.example.test/a.txt', 'ftp://x.test'])
    if isinstance(value, dict):
        return pick(value, {}, None, {'cert': 'c.pem'}, {'cert': 'c.pem', 'key': 'k.pem'})
    return pick(*WEIRD)


def settings_value(native=None):
    s = {}
    if native is None:
        native = maybe(0.15)
    if native:
        s['config_mode'] = 'native'
        if maybe(0.5):
            s.update(include_settings())
        else:
            s['native_config'] = native_text() if maybe(0.8) else pick(5, None)
    elif maybe(0.05):
        s['config_mode'] = pick('other', None, 5)
    top = ['worker_threads', 'cache_size', 'cache_dump_interval', 'probe_cache_size', 'local_concurrent', 'remote_concurrent',
           'fallback_concurrent', 'fallback_threshold', 'cache_lazy_ttl', 'remote_ttl']
    for key in R.sample(top, R.randint(0, 3)):
        s[key] = pick(*INTS)
    flags = ['tcp_enabled', 'prefer_ipv4', 'metrics_enabled', 'aggregate_rules', 'always_standby', 'cache_enabled',
             'cache_dump_enabled', 'probe_cache_enabled']
    for key in R.sample(flags, R.randint(0, 3)):
        s[key] = pick(True, False, True, None, 1)
    for key in R.sample(['direct_rulesets', 'proxy_rulesets', 'direct_rules', 'proxy_rules'], R.randint(0, 2)):
        s[key] = pick(['r1', 'r2'], ['r1', 'r1'], [], ['s1'], ['r3'], [5], 'r1', None)
    for key in R.sample(['local_dns', 'remote_dns', 'fallback_dns'], R.randint(0, 2)):
        s[key] = pick(*UPSTREAM_TEXTS) if maybe(0.95) else 5
    if maybe(0.2):
        s['listen'] = pick('0.0.0.0:53', '', ' ', 5, '[::]:53')
    if maybe(0.2):
        s['default_forward'] = pick('forward_local', 'forward_remote', 'x', None)
    if maybe(0.2):
        s['custom_match_position'] = pick('head', 'tail', 'middle')
    if maybe(0.3):
        s['custom_matches'] = pick(
            [{'matches': ['qname $x'], 'exec': 'accept'}], [{'enabled': False}], [{'matches': 'qtype 1\n\nqtype 28', 'exec': 'drop_resp'}],
            [{'matches': [], 'exec': 'accept'}], [{'matches': ['  '], 'exec': 'x'}], [{'matches': ['a', 5], 'exec': 'x'}],
            [{'matches': ['a'], 'exec': ' '}], [{'matches': ['a']}], [5], 'x', [{'matches': 5, 'exec': 'x'}], [])
    if maybe(0.2):
        s['block_qtypes'] = pick([65, 255], [], [0], [65536], [True], [1.0], ['65'], 65, [1, 65535])
    if maybe(0.25):
        s['custom_hosts'] = hosts_text() if maybe(0.9) else 5
    if maybe(0.25):
        s['custom_config'] = custom_config()
    for name in ('api', 'log', 'query_recorder', 'ip_selector', 'dynamic_domain_set', 'adguard', 'ros_address_list',
                 'ros_route', 'rule_update'):
        if maybe(0.2):
            s[name] = section(name)
    if maybe(0.2):
        s['device_policies'] = pick(
            [{'ip': '192.0.2.10', 'name': 'TV', 'mode': 'direct'}], [{'ip': '192.0.2.0/24', 'mode': 'smart'}],
            [{'ip': 'x', 'mode': 'smart'}], [{'ip': '192.0.2.1', 'mode': 'smart', 'extra': 1}], 'x', [],
            [{'ip': '::ffff:192.0.2.1', 'mode': 'direct', 'name': 'a\x7fb '}], [{'ip': '192.0.2.1', 'mode': 'smart'}] * 2)
    if maybe(0.15):
        s['additional_files'] = additional_files()
    if maybe(0.25):
        s['rule_actions'] = pick(
            {'r1': {'action': 'blackhole'}}, {'r1': {'action': 'refused'}, 's1': {'action': 'nxdomain'}},
            {'r1': {'action': 'upstream', 'source': 'local', 'upstream': {'addr': 'udp://192.0.2.53'}}},
            {'r2': {'action': 'upstream', 'source': 'remote', 'upstream': {'addr': 'tls://192.0.2.54'}}},
            {'r1': None}, {'r1': {'action': 'direct'}}, {'r1': {'action': 'nodata', 'x': 1}}, 'x', {'r1': 5},
            {'r1': {'action': 'upstream', 'source': 'x', 'upstream': {'addr': 'a'}}},
            {'r1': {'action': 'upstream', 'source': 'local', 'upstream': {'addr': ' '}}})
    if maybe(0.1):
        s['rule_dns_parked'] = pick({'r1': {'action': 'direct'}}, {'r2': {'action': 'proxy'}}, {'r1': {'action': 'x'}}, [])
    if maybe(0.1):
        s['rule_dns_follow'] = pick(['r1'], ['r2', 'r2'], 'r1', [5], [])
    return s


VALID_HOSTS = ['example.test 192.0.2.1', 'example.test 192.0.2.1 2001:db8::1', '192.0.2.1 a.test b.test',
               '192.0.2.1 a.test # comment', '# only comment', '', 'full:a.test 192.0.2.2', '2001:db8::1 v6.test']
VALID_UPSTREAMS = ['', 'udp://192.0.2.1', 'udp://192.0.2.1\ntls://192.0.2.2 192.0.2.53',
                   'https://dns.example.test/dns-query bootstrap=192.0.2.53', '- addr: udp://192.0.2.1\n  enable_pipeline: true',
                   '- udp://192.0.2.1\n- addr: tcp://192.0.2.2', 'addr: udp://192.0.2.9', '- &a {addr: udp://192.0.2.1}\n- *a',
                   '192.0.2.1:5353', 'tls://[2001:db8::1]:853']


def valid_settings(ids):
    """Settings that normalize: every feature toggled at random."""
    s = {}
    for key, values in (('worker_threads', [1, 2, 256]), ('cache_size', [1, 10240]), ('cache_lazy_ttl', [0, 3600]),
                        ('remote_ttl', [0, 1, 300]), ('local_concurrent', [1, 3]), ('fallback_threshold', [1, 500])):
        if maybe(0.3):
            s[key] = pick(*values)
    for key in ('tcp_enabled', 'prefer_ipv4', 'metrics_enabled', 'aggregate_rules', 'always_standby', 'cache_enabled',
                'cache_dump_enabled', 'probe_cache_enabled'):
        if maybe(0.3):
            s[key] = maybe()
    if s.get('cache_dump_enabled'):
        s['cache_dump_file'] = pick('./state/cache.dump', 'state/c.dump', 'cache.dump')
    shuffled = R.sample(ids, len(ids))
    half = len(shuffled) // 2
    s['direct_rules'] = shuffled[:half] if maybe(0.7) else []
    s['proxy_rules'] = shuffled[half:] if maybe(0.7) else []
    s['direct_rulesets'] = shuffled[:half] if maybe(0.7) else []
    s['proxy_rulesets'] = [i for i in shuffled[half:] if maybe(0.8)]
    s['direct_rules'] = [i for i in s['direct_rules'] if i not in s['proxy_rules']]
    s['direct_rulesets'] = [i for i in s['direct_rulesets'] if i not in s['proxy_rulesets']]
    for key in ('local_dns', 'remote_dns', 'fallback_dns'):
        if maybe(0.35):
            s[key] = pick(*VALID_UPSTREAMS, 'null', '~', '[]') if key == 'fallback_dns' else pick(*VALID_UPSTREAMS)
    s['default_forward'] = pick('forward_local', 'forward_remote')
    s['custom_match_position'] = pick('head', 'tail')
    if maybe(0.3):
        s['custom_matches'] = [{'matches': pick(['qname $x'], 'qtype 1\nqtype 28'), 'exec': pick('accept', 'drop_resp'),
                                'enabled': pick(True, False, True)} for _ in range(R.randint(1, 2))]
    if maybe(0.3):
        s['block_qtypes'] = pick([65, 255], [], [1], [28, 65])
    if maybe(0.3):
        s['custom_hosts'] = '\n'.join(R.sample(VALID_HOSTS, R.randint(1, 4))) + pick('', '\n', '\r\n')
    if maybe(0.3):
        s['custom_config'] = pick('', 'runtime: {worker_threads: 9, x: 1}', 'runtime: null', 'log: {level: debug}\nother: [1, 2]',
                                  'plugins:\n- {tag: extra, type: sequence, args: []}', 'a: &x {b: 1}\nc: *x',
                                  'plugins:\n- {tag: dl, type: download, args: {}}', 'when: 2026-01-01\nbin: !!binary aGk=',
                                  'plugins:\n- &p {tag: p1, type: s, args: [1]}\n- {tag: p2, type: s, args: *p}', 'runtime: {}',
                                  'api: {http: {listen: x}}', 'plugins:\n- {tag: lazy_cache, type: cache}')
    if maybe(0.3):
        s['api'] = {'enabled': maybe(0.7), 'listen': '127.0.0.1:9199', 'username': pick('', 'admin'), 'webui_root': pick('', 'ui')}
        s['api']['password'] = 'pw' if s['api']['username'] else ''
        if maybe(0.3):
            s['api']['ssl'] = pick({}, {'cert': 'c.pem', 'key': 'k.pem'})
    if maybe(0.3):
        s['log'] = {'level': pick('info', 'debug', 'off'), 'file': pick('', 'oxidns.log'), 'rotation': pick('daily', 'never'),
                    'max_files': pick(0, 7)}
    if maybe(0.2):
        s['query_recorder'] = {'enabled': True, 'args': {'path': pick('./state/q.db', 'state/x.db')}}
    if maybe(0.2):
        s['ip_selector'] = {'enabled': True, 'args': {'selection_mode': pick('first_success', 'background'), 'top_n': pick(0, 2)}}
    if maybe(0.2):
        s['dynamic_domain_set'] = {'enabled': True, 'route': pick('direct', 'proxy'), 'learn': maybe(),
                                   'args': {'bootstrap_rules': pick([], ['domain:a.test'])}}
    if maybe(0.2):
        s['adguard'] = {'enabled': True, 'mode': pick('nxdomain', 'null'), 'content': pick('', '||ads.example.test^'),
                        'urls': pick(['https://lists.example.test/ad.txt'], [], ['https://lists.example.test/a.txt', 'http://b.test/x'])}
        if not s['adguard']['content'] and not s['adguard']['urls']:
            s['adguard']['content'] = '||x.test^'
    for name in ('ros_address_list', 'ros_route'):
        if maybe(0.15):
            args = {'address': '192.0.2.1', 'username': 'admin', 'password': 'pw', 'min_ttl': pick(0, 60), 'max_ttl': pick(60, 3600)}
            if name == 'ros_route':
                args.update(routing_table='main', gateway4=pick('192.0.2.254', ''), gateway6=pick('', 'fe80::1'))
                if not args['gateway4'] and not args['gateway6']:
                    args['gateway4'] = '192.0.2.254'
            s[name] = {'enabled': True, 'args': args}
    if maybe(0.25):
        s['rule_update'] = {'enabled': maybe(0.6), 'args': {'schedule': '30 3 * * *', 'timezone': 'Asia/Shanghai',
                            'timeout': pick('60s', '5m', 30, '', None, 'bad', '1.5s'), 'outbound': pick('', 'proxy-a', ' '),
                            'socks5': pick('', '192.0.2.1:1080', 'user:pw@[2001:db8::1]:1080', 'nohost')}}
    if maybe(0.3) and ids:
        target = R.choice(ids)
        s['rule_actions'] = {target: pick({'action': 'blackhole'}, {'action': 'refused'}, {'action': 'nodata'},
                                          {'action': 'upstream', 'source': pick('local', 'remote', 'fallback'),
                                           'upstream': {'addr': 'udp://192.0.2.53'}})}
    return s


def valid_profile():
    rules = []
    for i in range(R.randint(0, 6)):
        if maybe():
            rules.append({'id': f'r{i}', 'itemType': 'rule', 'rule_type': pick('DOMAIN-SUFFIX', 'DOMAIN', 'DOMAIN-KEYWORD',
                          'DOMAIN-REGEX', 'IP-CIDR', 'IP-CIDR6'), 'value': pick('example.test', 'a.example.test', '192.0.2.0/24',
                          '2001:db8::/32', '^a.*\\.test$', '*.example.test'), 'enabled': pick(True, True, False)})
        else:
            rules.append({'id': f's{i}', 'itemType': 'ruleset', 'behavior': pick('domain', 'ipcidr', 'classical'),
                          'url': pick('https://lists.example.test/a.txt', '/api/rule-library/content/x'), 'name': pick('Set', '')})
    config = {'rule_configs': rules, 'system_config': {'rule_proxy_token': 'tok-1'}}
    if maybe(0.4):
        config['system_config']['server_domain'] = pick('https://cf.example.test/', 'http://192.0.2.10:8080')
    if maybe(0.3):
        config['profile_id'] = 'home'
    config['oxidns'] = valid_settings([r['id'] for r in rules])
    return config


def custom_config():
    return pick(
        '', 'runtime: {worker_threads: 9, x: 1}', 'runtime: 5', 'runtime: null', 'plugins: 5', 'include: [x]',
        'plugins:\n- {tag: extra, type: sequence, args: []}', 'plugins:\n- {tag: sequence_main, type: sequence}',
        'plugins:\n- 5', 'plugins:\n- {tag: "bad tag", type: x}', 'plugins:\n- {tag: dl, type: download, args: {}}',
        'plugins:\n- {tag: rules_reload, type: x}', 'log: {level: debug}\napi: {x: 1}\nother: [1, 2]',
        '- a', 'a: [', 'a: &x {b: 1}\nc: *x\nplugins:\n- &p {tag: p1, type: s}\n', '1: x', 'a: {1: x}', 'a: !!set {x}',
        'a: &x [*x]', 'when: 2026-01-01\nbin: !!binary aGk=', 'a: 1\n# c', '~', 'a: .inf', 'runtime: {}',
        'plugins: []\nruntime: {worker_threads: 1}\nlog: {}', 'a: 1\na: 2')


def rule_item(i):
    item = {'id': pick(f'r{i}', 'r1', 'r2', 'r3', 's1', 5, None) if maybe(0.95) else f'r{i}'}
    if maybe(0.5):
        item['itemType'] = 'ruleset'
        item['behavior'] = pick('domain', 'ipcidr', 'classical', 'other', None)
        if maybe(0.1):
            item.pop('behavior')
        item['url'] = pick('https://lists.example.test/a.txt', '/api/rule-library/content/x', '', None, 5, 'relative.txt')
        item['name'] = pick('Set A', '', None, '规则集甲')
    else:
        item['itemType'] = pick('rule', 'rule', None)
        item['rule_type'] = pick('DOMAIN-SUFFIX', 'DOMAIN', 'DOMAIN-KEYWORD', 'DOMAIN-REGEX', 'IP-CIDR', 'IP-CIDR6', 'GEOIP',
                                 'DST-PORT', None, 5)
        item['value'] = pick(*DOMAINS, '192.0.2.0/24', '2001:db8::/32', '^a.*\\.test$', None, 5, '(', '#x')
    if maybe(0.1):
        item['enabled'] = pick(False, True, 0)
    if maybe(0.05):
        item['library_enabled'] = pick(False, True)
    return item


def profile():
    config = {'rule_configs': [rule_item(i) for i in range(R.randint(0, 6))]}
    if maybe(0.05):
        config['rule_configs'] = pick([], 'x', {'a': 1})
    if maybe(0.1):
        config.pop('rule_configs')
        config['rules'] = [{'id': 'r1', 'rule_type': 'DOMAIN-SUFFIX', 'value': 'example.test', 'enabled': True}]
        config['rule_sets'] = [{'id': 's1', 'url': 'https://lists.example.test/s.txt', 'behavior': 'domain', 'enabled': True}]
    system = {}
    if maybe(0.85):
        system['rule_proxy_token'] = pick('tok-123', '', 5)
    if maybe(0.4):
        system['server_domain'] = pick('https://cf.example.test/', 'http://192.0.2.10:8080', '', None, 5)
    if maybe(0.95):
        config['system_config'] = system
    else:
        config['system_config'] = pick(None, 5)
    if maybe(0.3):
        config['profile_id'] = pick('home', 'p-1', '')
    if maybe(0.97):
        config['oxidns'] = settings_value()
        # Route most rules somewhere.
        if maybe(0.6) and isinstance(config['oxidns'], dict):
            rules = config.get('rule_configs')
            ids = [r.get('id') for r in rules if isinstance(r, dict)] if isinstance(rules, list) else []
            ids = [i for i in ids if isinstance(i, str)]
            if ids:
                config['oxidns'].setdefault('direct_rules', R.sample(ids, R.randint(0, len(ids))))
                rest = [i for i in ids if i not in config['oxidns']['direct_rules']] if isinstance(config['oxidns']['direct_rules'], list) else []
                config['oxidns'].setdefault('proxy_rulesets', R.sample(rest, R.randint(0, len(rest))))
                config['oxidns'].setdefault('direct_rulesets', [])
                config['oxidns'].setdefault('proxy_rules', [])
    else:
        config['oxidns'] = pick(None, 5, [])
    return config


# ---- generators -------------------------------------------------------------

def gen_settings():
    for index in range(n(6000)):
        if index % 3 == 0:
            value = valid_settings(['r1', 'r2', 's1'])
        else:
            value = settings_value() if maybe(0.97) else pick(None, 5, 'x', [])
        validate = maybe(0.6)
        row('normalize', [value, validate], lambda: st.normalize_oxidns_settings(value, validate_native_arguments=validate))
    for _ in range(n(800)):
        value, label = pick(*PATHS, 'a' * 300, 'a/b/../c'), pick('文件路径', '缓存文件', '', 'X')
        row('safe_relative_path', [value, label], lambda: st.safe_relative_path(value, label))
    for _ in range(n(600)):
        defaults = st.get_default_oxidns_settings()
        value = settings_value(False)
        row('merge', [defaults, value], lambda: st._merge(defaults, value))
    for _ in range(n(800)):
        url = pick('https://cf.example.test/api/x', 'https://cf.example.test/api/x?a=1&b=', 'http://[::1]:8080/p?x=%20y#f',
                   'http://[bad/x', '/relative?q=1;r=2', 'https://h.test/p?=v&k', 'https://h.test/?a=1&&b=2+3', '')
        params = R.choice([[('token', pick('t 1', 'ü', '&=?', '', '/'))], [('url', 'https://a.test/x?y=1'), ('part', 'domain')],
                           [('k', ['a', 'b'])], [('k', 5)], [('k', None)], [('k', {'a': 1})], [(5, 'v')], []])
        row('append_url_query', [url, params], lambda: pc.append_url_query(url, params))


def gen_profiles():
    for index in range(n(6000)):
        config = valid_profile() if index % 2 else profile()
        base = pick('', 'https://cf.example.test', 'http://192.0.2.10:8080/', '/')
        hold = rec.settings_holdable(config)
        row('generate', [config, base], lambda: cv.generate_oxidns_config(config, base), unholdable=not hold)
        row('downloads', [config, base], lambda: cv.get_oxidns_ruleset_downloads(config, base), unholdable=not hold)
        row('custom_files', [config], lambda: cv.get_oxidns_custom_files(config), unholdable=not hold)
        if maybe(0.3):
            row('items', [config], lambda: cv._items(config))
            row('base', [config, base], lambda: cv._base(config, base))
        if maybe(0.3) and isinstance(config.get('oxidns'), dict):
            try:
                settings = st.normalize_oxidns_settings(config['oxidns'])
            except Exception:  # noqa: BLE001
                settings = None
            if settings is not None:
                legacy = pick(False, True, False)
                rules = config.get('rule_configs')
                items = None if maybe(0.7) or not isinstance(rules, list) else [i for i in rules if isinstance(i, dict)][:2]
                row('routing_groups', [config, settings, legacy, items],
                    lambda: cv._routing_groups(config, settings, legacy=legacy, items=items))
        if maybe(0.2):
            paths = R.sample([f'rules/{cv._tag("rule", i)}_{p}.txt' for i in ('r1', 'r2', 's1', 'gone', 5) for p in ('domain', 'ip')]
                             + ['rules/custom_hosts.txt', 'x.txt'], R.randint(0, 5))
            row('unavailable_rulesets', [config, paths], lambda: cv.unavailable_rulesets(config, paths))


def gen_text():
    for _ in range(n(1500)):
        text = hosts_text()
        row('hosts', [text], lambda: cv._hosts_file_content(text))
    for _ in range(n(1500)):
        text = pick(*UPSTREAM_TEXTS) + pick('', '\n', '\n- addr: udp://192.0.2.7')
        defaults = pick(['udp://192.0.2.1'], [], ['a', 'b'])
        hold = rec.text_holdable(text)
        row('forward', [text, defaults], lambda: cv._forward(text, defaults), unholdable=not hold)
        row('fallback', [text], lambda: cv._fallback_upstreams(text), unholdable=not hold)


def gen_native():
    for _ in range(n(2500)):
        text = native_text() if maybe(0.97) else pick(5, None, 'a' * (2 * 1024 * 1024 + 1))
        label = pick('完整配置', 'include 文件', '')
        row('load_native_yaml', [text, label], lambda: nv.load_native_yaml(text, label))
    for _ in range(n(1500)):
        value = additional_files()
        row('managed_files', [value], lambda: nv.managed_files(value))
    alphabet = 'QUJDYWJj+/=xA= \n'
    for _ in range(n(2000)):
        text = ''.join(R.choice(alphabet) for _ in range(R.randint(0, 9)))
        value = [{'path': 'a.bin', 'encoding': 'base64', 'content': text}]
        row('managed_files', [value], lambda: nv.managed_files(value))
    for _ in range(n(2000)):
        settings = include_settings() if maybe(0.8) else settings_value(True)
        if maybe(0.1):
            settings['additional_files'] = additional_files()
        with_sources = maybe(0.5)

        def call(settings=settings, with_sources=with_sources):
            sources = {} if with_sources else None
            document = nv.native_document(settings, validate_arguments=False, sources=sources)
            return {'document': document, 'sources': sources}
        row('native_document', [settings, False, with_sources], call)
    for _ in range(n(1500)):
        doc = document()
        row('resource_references', [doc], lambda: nv.resource_references(doc))
        paths = R.sample(['rules/a.txt', 'a.pem', 'c.pem', 'k.pem', 'ca.pem', 'x/y.db', 'state/q.db', 'ui/index.html',
                          './rules/b.txt', '../x', 5], R.randint(0, 6))
        row('validate_resource_files', [doc, paths], lambda: nv.validate_resource_files(doc, paths))
    for _ in range(n(500)):
        value = pick(*PATHS, '/etc/x', '${HOME}/x', 'a${X}', None)
        row('relative_resource', [value], lambda: nv.relative_resource(value))


def gen_references():
    for _ in range(n(1500)):
        doc = document()
        if maybe(0.3):
            doc['plugins'] = doc.get('plugins') if isinstance(doc.get('plugins'), list) else []
            doc['plugins'].append({'tag': pick('${T}', '$${T}', 'x'), 'type': 'sequence',
                                   'args': [{'exec': pick('$a', '${E}', 'goto b.c', 'jump a-b', '$$ {x}', 'a.b', 'xa', 'a')},
                                            {'matches': ['qname $a', '!has_resp'], 'exec': 'accept'}]})
        tags = R.sample(['a', 'b.c', 'a-b', 'sequence_main', 'x', '例'], R.randint(0, 3))
        dynamic = maybe(0.7)
        row('reference_locations', [doc, tags, dynamic], lambda: rf.reference_locations(doc, tags, dynamic=dynamic))
    for _ in range(n(500)):
        refs = [{'path': R.choice([['plugins', 0, 'args'], [], ['a', 'b'], ['plugins', 3], [0]]),
                 'tag': pick('a', None), 'reason': pick('仍有插件引用', '未展开的环境变量引用')} for _ in range(R.randint(0, 6))]
        prefix = pick('功能仍被自定义执行链引用，请在完整参数中调整', '', '前缀')
        row('reference_reason', [refs, prefix], lambda: rf.reference_reason(refs, prefix))
        message = pick('不能删除', '')
        row('reference_error', [message, refs], lambda: str(rf.OxidnsReferenceError(message, refs)))


def gen_parts():
    for _ in range(n(800)):
        value = pick({'action': 'blackhole'}, {'action': 'refused', 'x': 1}, {'action': 'direct'}, {'action': 'proxy'},
                     {'action': 'upstream', 'source': 'fallback', 'upstream': {'addr': 'udp://192.0.2.5', 'x': [1]}},
                     {'action': 'upstream', 'source': 'local', 'upstream': 'x'}, {'action': 'upstream'}, 5, None, {})
        parked = maybe()
        row('normalize_action', [value, parked], lambda: rd.normalize_action(value, parked=parked))
    for _ in range(n(800)):
        route = pick('direct', 'proxy', 'refused', 'nxdomain', 'nodata', 'blackhole', 'upstream:0123456789ab', 'upstream:xyz',
                     'upstream:0123456789AB', 'other', 5, None)
        part = pick('domain', 'ip')
        row('route_exec', [route, part], lambda: rd.route_exec(route, part))
    for _ in range(n(600)):
        actions = pick({}, None, {'r1': {'action': 'upstream', 'source': 'local', 'upstream': {'addr': 'udp://192.0.2.53'}}},
                       {'r1': {'action': 'upstream', 'source': 'remote', 'upstream': {'addr': 'tls://192.0.2.54', 'b': 'x'}},
                        'r2': {'action': 'blackhole'}})
        keys = [rd.route_key(a) for a in (actions or {}).values()]
        routes = R.sample(keys + ['blackhole', 'blackhole', 'direct', 'upstream:000000000000'], R.randint(0, 4))
        settings = {'cache_enabled': pick(True, False), 'cache_lazy_ttl': pick(0, 3600, True), 'remote_ttl': pick(0, 1, 5),
                    'ip_selector': {'enabled': pick(True, False, 1)}, 'ros_route': pick({'enabled': True}, 5)}
        row('helper_plugins', [routes, actions, settings], lambda: rd.helper_plugins(routes, actions, settings))
        if actions:
            for action in actions.values():
                row('entry_key', [action.get('upstream'), action.get('source')],
                    lambda: rd.entry_key(action.get('upstream'), action.get('source')))
    for _ in range(n(1500)):
        raw = pick('192.0.2.1', ' 192.0.2.0/24 ', '192.0.2.1/33', '192.0.2.01', '::', '::/0', '0.0.0.0', '0.0.0.0/0',
                   '::ffff:192.0.2.1', '::ffff:192.0.2.0/120', '::ffff:0:0/95', '224.0.0.1', '224.0.0.0/4', '224.0.0.0/3',
                   'ff02::1', '2001:db8::1/64', '2001:DB8::/32', 'fe80::1%eth0', '1.2.3.4/0x8', '1.2.3.4/ 8', '１.2.3.4',
                   '192.0.2.1/', '/24', '192.0.2.1/24/1', '::1.2.3.4', '64:ff9b::192.0.2.1/128', 5, None, '')
        row('normalize_client_range', [raw], lambda: dp.normalize_client_range(raw))
    for _ in range(n(400)):
        value = settings_value(False).get('device_policies', pick([], [{'ip': '192.0.2.9', 'mode': 'smart', 'name': 'x' * 81}]))
        row('normalize_policies', [value], lambda: dp.normalize_policies(value))
    for _ in range(n(500)):
        prefix = pick('rule', 'adguard')
        identity = pick('r1', 5, None, 1.5, True, 'https://lists.example.test/a.txt', ['x'], {'a': 1}, '中文')
        row('tag', [prefix, identity], lambda: cv._tag(prefix, identity))
        item = pick({'id': 'r1'}, {'id': 5}, {}, {'id': None})
        part = pick('domain', 'ip')
        row('rule_path', [item, part], lambda: cv._rule_path(item, part))


def gen_validation():
    fields = vl.schemas()[1]['download']['properties']
    for _ in range(n(2000)):
        key = pick('timeout', 'outbound', 'socks5', 'insecure_skip_verify', 'downloads')
        value = mutate('timeout', None) if key != 'downloads' else pick(
            [{'url': 'https://a.test/x', 'dir': 'rules'}], [], [{'url': 'ftp://a', 'dir': ''}], [{'dir': 'x', 'extra': 1}], 5)
        path = ['plugins', R.randint(0, 9), 'args', key]
        schema = fields[key]

        def call(value=value, schema=schema, path=path):
            return vl.validate_schema(value, schema, path)
        entry_args = [value, {'$schema': ['plugins', 'download', 'properties', key]}, path]
        before = len(OUT)
        row('validate_schema', [value, schema, path], call)
        if len(OUT) > before:
            OUT[-1]['args'] = rec.conv(entry_args)
    formats = ['duration', 'non-empty-string', 'nonempty-string', 'socks5', 'socket-address', 'ip', 'ip-address', 'ip-network',
               'upstream-address', 'http-url', 'http-method', 'plugin-tag', 'dns-qtype', 'dns-base-rcode', 'dns-rcode', 'dns-class',
               'unknown-format']
    values = ['60s', '5 ms', '5 MS', '10ſ', '1h', '1d', '1w', '', ' ', '-1', '18446744073709551615', '18446744073709551616s',
              0, 5, -1, 2**64, True, 1.5, None, [], 'x', '${X}', '$${X}', '192.0.2.1:53', ':53', '[::1]:53', '::1:53',
              '[fe80::1%eth0]:53', 'a:b@host:1080', 'nouser@host:1080', 'user:pw@[2001:db8::1]:1080', '[host:1', 'host:+80',
              '192.0.2.1', '192.0.2.0/24', '192.0.2.1/33', 'fe80::1%x', 'udp://192.0.2.1:53', 'tls://dns.example.test:853',
              'https://dns.example.test/dns-query', 'http://[::1]:99999/', 'ftp://x.test', 'udp://', 'quic://a.test', 'a b',
              'GET', 'G T', 'tag.ok', 'qs.exec.x', 'x' * 65, 'A', 'AAAA', 'aaaa', '28', '+28', '65536', 'NXDOMAIN', 'IN', 'CH',
              '16', '15', 'REFUSED', 'BADSIG']
    for _ in range(n(3000)):
        name, value = pick(*formats), pick(*values)
        row('format_error', [name, value], lambda: vl.format_error(name, value))


def main():
    for generate in (gen_settings, gen_profiles, gen_text, gen_native, gen_references, gen_parts, gen_validation):
        generate()
    path = Path(__file__).with_name('corpus.jsonl.gz')
    data = '\n'.join(json.dumps(e, ensure_ascii=False) for e in OUT) + '\n'
    with open(path, 'wb') as raw:
        with gzip.GzipFile(fileobj=raw, mode='wb', mtime=0) as fh:
            fh.write(data.encode())
    counts = {}
    for e in OUT:
        counts[e['fn']] = counts.get(e['fn'], 0) + 1
    print(f'{len(OUT)} rows {counts} -> {path}')


if __name__ == '__main__':
    main()
