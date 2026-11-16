#!/usr/bin/env python3
"""Generate corpus.jsonl.gz: random MosDNS settings, rule contents and
upstream definitions and what Python's MosDNS converter (C21,
backend/converters/mosdns.py) makes of them.

Run from the repository root on the test server (CPython 3.11, the backend venv):

    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/mosdns/testdata/gen.py [count]

Every value is synthetic. One JSON object per line:
    {"fn": <name>, "args": [...], "out" | "err": [class, message], "unholdable": true?}

Functions (args are positional; a missing trailing part means None):
    generate [config, base_url]          -> YAML text
    downloads [config, base_url]         -> list of dicts
    custom_files [config]                -> list of dicts
    convert [content] / [content, part]  -> text
    domain_pattern [pattern]             -> text or None
    parse_clash [line]                   -> [kind, value]
    upstreams [value]                    -> {"dump": yaml.dump(result, Dumper=IndentDumper, ...)}
    proxy_url [config, base, url, part, source_id] -> text
    ruleset_files [config, base, rule_set, file_stem] -> list of dicts
    split [config]                       -> [rules, rule_sets]
    custom_matches [value]               -> list
    ruleset_parts [behavior]             -> list
"unholdable" marks rows whose YAML input loads to values the Go jsonx model
cannot hold (non-string keys, !!set/!!omap/!!pairs, recursive aliases).
"""

import gzip
import json
import random
import sys
from copy import deepcopy
from pathlib import Path

import yaml

from backend.converters import mosdns as m

COUNT = int(sys.argv[1]) if len(sys.argv) > 1 else 6000
R = random.Random(20261116)
OUT = []


def dump(value):
    return yaml.dump(value, Dumper=m.IndentDumper, allow_unicode=True, default_flow_style=False,
                     sort_keys=False, indent=2)


def holdable(value, seen=None):
    seen = set() if seen is None else seen
    if isinstance(value, (dict, list)):
        if id(value) in seen:
            return False
        seen = seen | {id(value)}
    if isinstance(value, dict):
        return all(isinstance(k, str) and holdable(v, seen) for k, v in value.items())
    if isinstance(value, list):
        return all(holdable(v, seen) for v in value)
    if isinstance(value, (set, frozenset, tuple)):
        return False
    return True


def text_holdable(text):
    if not isinstance(text, str):
        return True
    for candidate in (text, text.strip()):
        try:
            if not holdable(yaml.safe_load(candidate)):
                return False
        except Exception:  # noqa: BLE001
            pass
    return True


def record(fn, args, call, convert_out=lambda v: v, unholdable=False):
    entry = {'fn': fn, 'args': deepcopy(args)}
    if unholdable:
        entry['unholdable'] = True
    try:
        result = call()
    except Exception as exc:  # noqa: BLE001 - the corpus records every exception
        entry['err'] = [type(exc).__name__, str(exc)]
    else:
        entry['out'] = convert_out(result)
    try:
        json.dumps(entry, allow_nan=False)
    except (TypeError, ValueError):
        return
    OUT.append(entry)


WILD = [True]


def pick(seq):
    return R.choice(seq)


def pk(seq):
    """pick, limited to well-formed strings unless the profile is a wild one."""
    if not WILD[0]:
        clean = [v for v in seq if isinstance(v, str) and v.strip() and '[bad' not in v]
        if clean:
            return R.choice(clean)
    return R.choice(seq)


def maybe(p=0.5):
    return R.random() < p


# ------------------------------------------------------------------ values

WEIRD = [None, 5, 0, 1.5, True, False, '', 'x', [], [1], ['a', 'b'], {}, {'a': 1}, ' ', -3, 2.0]

DOMAINS = ['example.test', 'a.example.test', 'xn--fsqu00a.xn--0zwm56d', '_srv._tcp.example.test', 'host-.example',
           'UPPER.Example.TEST', '例子.测试', 'ＦＵＬＬ.test', 'İstanbul.test', 'straße.test', 'a', 'localhost']
PATTERNS = ['+.example.test', '.example.test', '*.example.test', '*', '+.*', '.*', '*.*', 'www.*', 'a.*.b.*',
            '+.a.*.com', '.a.*.com', '*.*.example.com', 'exact.test', 'foo..test', 'invalid/host', '*a.test',
            'a*.test', '+.', '.', '', '+.UPPER.Test', 'a_b.test', '-x.test', 'x-.test', '例子.测试', 'a b.test',
            'a.test.', '..', '*.', '+.*.*', 'a+b.test', 'a$b.test', 'tab\t.test', 'ß.test', 'K.test']
IPS = ['1.2.3.0/24', '1.2.3.4', '10.0.0.0/8', '2001:db8::/32', '2001:db8::1', '::/0', '0.0.0.0/0',
       '1.2.3.4/255.255.255.0', '1.2.3.4/0.0.0.255', '1.2.3.4/33', '300.1.1.1', '1.2.3.4/', '1.2.3.4/24/1',
       'fe80::1%eth0', 'fe80::%eth0/64', '::ffff:1.2.3.4', '1.2.3.04', '１.２.３.４', '1.2.3', '01.2.3.4/24',
       '2001:db8::/129', '2001:DB8::/32', ' 1.2.3.4 ', '1.2.3.4/024', '1.2.3.4/+24', '1.2.3.4/ 24']
REGEXES = [r'^node[0-9]{1,3}\.example\.test$', r'^a{1,3}b{2,}\.test$', r'^[a,b]+\.test$', r'^a\,b\.test$', '(', 'x,y,z']
CLASH_TYPES = ['DOMAIN-SUFFIX', 'DOMAIN', 'DOMAIN-KEYWORD', 'DOMAIN-REGEX', 'IP-CIDR', 'IP-CIDR6', 'GEOIP',
               'PROCESS-NAME', 'domain-suffix', ' Domain ', 'DOMAIN-WILDCARD', 'IP-ASN', 'MATCH', 'ıp-cidr', 'domaın']


def clash_line():
    kind = pick(CLASH_TYPES)
    if 'REGEX' in kind.upper():
        value = pick(REGEXES)
    elif 'IP' in kind.upper() and 'GEO' not in kind:
        value = pick(IPS)
    else:
        value = pick(DOMAINS + ['', 'CN', 'keyword'])
    line = f'{kind},{value}'
    if maybe(0.3):
        line += pick([',DIRECT', ',no-resolve', ',Proxy,no-resolve', ',', ' , x'])
    if maybe(0.2):
        line = pick([' ', '\t', '  ']) + line + pick([' ', '', '\t'])
    return line


def native_line():
    prefix = pick(['domain', 'full', 'keyword', 'regexp', 'ip', 'DOMAIN', 'Full', 'IP', 'geoip', 'domaın'])
    value = pick(IPS) if prefix.lower() == 'ip' else pick(DOMAINS + REGEXES + ['', ' spaced'])
    return f'{prefix}:{value}'


def rule_line():
    r = R.random()
    if r < 0.35:
        return clash_line()
    if r < 0.6:
        return pick(PATTERNS + DOMAINS)
    if r < 0.75:
        return pick(IPS)
    if r < 0.85:
        return native_line()
    return pick(['# comment', '// comment', '', '   ', '#', 'payload:', '- x', 'a: b', 'DOMAIN', ',', ':',
                 'a,b', 'ip:', 'domain:', '﻿example.test', 'x\x85y.test'])


def rule_content():
    r = R.random()
    lines = [rule_line() for _ in range(R.randint(0, 25))]
    if r < 0.5:
        sep = pick(['\n', '\r\n', '\n', '\r', '\x0b', ' '])
        text = sep.join(lines)
        if maybe(0.3):
            text += sep
    elif r < 0.75:
        items = [yaml.safe_dump(line, allow_unicode=True).strip().removesuffix('\n...') if maybe(0.5) else
                 json.dumps(line, ensure_ascii=False) for line in lines]
        text = 'payload:\n' + ''.join(f'  - {item}\n' for item in items)
        if maybe(0.2):
            text = '# head\n' + text
    elif r < 0.85:
        text = 'payload: ' + json.dumps(lines, ensure_ascii=False)
    else:
        text = pick([
            'payload:', 'payload: null', 'payload: 5', 'payload: true', 'payload: 1.5', 'payload: abc',
            'payload: {a.test: 1, DOMAIN-SUFFIX,b.test: 2}', 'payload: [1, 2, a.test]', 'payload: [[a.test]]',
            'payload: !!binary aGVsbG8=', 'payload: 2001-12-14', 'payload: [a.test',
            'other: 1\npayload: [x.test]', 'payload: []\npayload: [y.test]', 'a: &r [DOMAIN,x.test]\npayload: *r',
            'payload: [&x DOMAIN,x.test, *x]', '{1: x, payload: [a.test]}', 'payload: {1: a, b.test: c}',
            'payload: !!set {a.test, b.test}', 'payload: !!omap [{a.test: 1}]', 'payload: &a [*a, x.test]',
            '- payload', 'payload: [{DOMAIN: x}]', 'payload:\n  - "IP-CIDR,1.2.3.0/24"\n  - +.x.test\n',
            '﻿payload: [x.test]', 'payload: [x.test]\n---\npayload: [y.test]', 'payload: !!python/none x',
            'payload: ["DOMAIN,a.test", "IP-CIDR6,2001:db8::/32,no-resolve", "domain:b.test", "ip:1.1.1.1"]',
        ])
    if maybe(0.05):
        text = '﻿' + text
    return text


def upstream_text():
    r = R.random()
    if r < 0.15:
        return pick(WEIRD + ['   ', '\n'])
    addrs = ['223.5.5.5', '119.29.29.29:53', 'https://1.1.1.1/dns-query', 'tls://8.8.8.8', 'udp://192.168.0.2:1053',
             'quic://dns.example.test', 'h3://dns.example.test/dns-query', '[2001:db8::1]:53', '例子.测试']
    if r < 0.45:
        lines = []
        for _ in range(R.randint(1, 4)):
            parts = [pick(addrs)]
            for _ in range(R.randint(0, 3)):
                parts.append(pick(['bootstrap=223.5.5.5', '223.5.5.5', 'enable_pipeline=true', 'enable_pipeline=No',
                                   'enable_pipeline=maybe', 'enable_pipeline=TRUE', 'enable_pipeline=İ', 'x=y', '=',
                                   'bootstrap=', 'bootstrap=a=b', 'idle_timeout=10']))
            line = pick([' ', '\t', '　']).join(parts)
            lines.append(pick(['', '', '', ' ', '\t', '- ', '# ', '　']) + line)
        return pick(['\n', '\r\n']).join(lines + ([''] if maybe(0.2) else []))
    if r < 0.8:
        items = []
        for _ in range(R.randint(0, 4)):
            kind = R.random()
            if kind < 0.5:
                item = {'addr': pick(addrs)}
                if maybe():
                    item['bootstrap'] = '223.5.5.5'
                if maybe():
                    item['enable_pipeline'] = pick([True, False, 'yes'])
                if maybe(0.2):
                    item['dial_addr'] = '1.1.1.1'
                items.append(yaml.safe_dump(item, default_flow_style=pick([True, False])).strip())
            elif kind < 0.7:
                items.append(pick(addrs))
            else:
                items.append(pick(['{bootstrap: 1.1.1.1}', '5', 'null', '[a]', '{addr: x, 1: y}', '2001-01-01',
                                   '!!set {a}', '{addr: [1, 2]}', '{addr: x, when: 2001-01-01 10:00:00}']))
        text = ''.join('- ' + item.replace('\n', '\n  ') + '\n' for item in items)
        if maybe(0.2):
            text = '- &u {addr: 9.9.9.9}\n- *u\n'
        return text
    return pick(['addr: 1.1.1.1', 'addr: 1.1.1.1\nbootstrap: 223.5.5.5', '{addr: x, 1: y}', 'bootstrap: x',
                 'addr: [a, b]', '[unclosed', ': x', 'addr: 1.1.1.1\n  bad: indent', '  223.5.5.5\n119.29.29.29',
                 '- a\n223.5.5.5', 'a: b: c', '"quoted"', "'223.5.5.5'", 'tls://8.8.8.8 # comment',
                 '!!binary aGVsbG8=', '1.1.1.1 2.2.2.2 bootstrap=3.3.3.3'])


def custom_config_text():
    r = R.random()
    if r < 0.3:
        return pick(WEIRD + ['   ', '\n'])
    if r < 0.8:
        doc = {}
        for key in R.sample(['log', 'api', 'plugins', 'servers', 'data_providers', 'extra', 'include', '中文', 'z'],
                            R.randint(0, 5)):
            doc[key] = pick([{'level': 'debug'}, {'http': '127.0.0.1:9091'}, [{'tag': 'old'}], 'value', 5, None,
                             [], {}, {'nested': {'deep': [1, 'two', None]}}, '多字节', 'line\nbreak', True])
        return yaml.safe_dump(doc, allow_unicode=maybe(), sort_keys=maybe())
    return pick([
        '[a, b]', 'just text', 'key: [unclosed', 'a: &x {k: 1}\nb: *x', 'a: 2001-12-14\nb: 2001-12-14 21:59:43.10-05:00',
        'bin: !!binary aGVsbG8=', 'big: 123456789012345678901234567890', 'f: 1e3\ng: .inf\nh: -.nan',
        '{1: x, extra: y}', 'plugins: {1: x}\nextra: 1', 'extra: {1: x}', 's: !!set {a}', 'plugins: !!set {a}',
        'r: &r [*r]', 'servers: [x]\ndata_providers: [y]\nkeep: z', 'log: {level: x}\napi: null', '---\na: 1\n---\nb: 2',
        'oct: 0o17\nhex: 0x1F\nyes: yes\nsexa: 1:30', 'empty: {}\nlist: []', "'q': 'é'", 'x: !!python/tuple [1]',
    ])


def custom_matches_value():
    if maybe(0.15):
        return pick(WEIRD)
    out = []
    for _ in range(R.randint(0, 4)):
        if maybe(0.1):
            out.append(pick(WEIRD))
            continue
        entry = {}
        if maybe(0.8):
            entry['exec'] = pick(['goto china_dns', ' accept ', '', None, 5, 'reject 3', '$forward_local'])
        if maybe(0.85):
            entry['matches'] = pick(['qname $x\n  \nqtype 65', 'qname a\r\nqname b', '', [' qname $a ', '', 5, None, 'b'],
                                     [], 'single', 5, None, {'a': 1}, ['x　']])
        if maybe(0.3):
            entry['enabled'] = pick([True, False, 0, 1, None, ''])
        out.append(entry)
    return out


RS_IDS = ['rs-cn', 'rs-tg', 'ruleset_ads', 'ruleset_', 'rs-ip', 'rs dup', 'rs-x', 7, True, 1.0]
RULE_IDS = ['r1', 'r2', 'r3', 'r4', 'r5', 'r6', 3, 'r-ip']
NAMES = ['CN', 'Telegram', 'Telegram', 'lazy_cache', 'direct_rules', 'direct_ip_rules', 'proxy_rules', ' ', '',
         None, 'CN_ip', 'Ads', '中文规则', 'a/b', 'name with space', 5, 'remote_dns', 'Telegram_2']
URLS = ['https://r.example.test/cn.txt', '/api/rules/local/CN', '', None, 'http://[::1]:8080/x', 'http://[bad/x',
        'https://r.example.test/q?x=1&y=&z', 'ftp://r.example.test/a b', '/relative path?q=a b', 5, ['x'],
        'https://r.example.test/%E4%B8%AD', 'https://user:pw@r.example.test/x#frag']


def rand_ruleset(i):
    item = {'id': pk(RS_IDS), 'itemType': 'ruleset'}
    if maybe(0.9):
        item['name'] = pk(NAMES)
    if maybe(0.9):
        item['behavior'] = pick(['domain', 'ipcidr', 'classical', 'classical', 'other', None, 'Domain'])
    if maybe(0.9):
        item['url'] = pk(URLS)
    if maybe(0.3):
        item['library_rule_id'] = pick(['lib-1', 'lib-2', '', None, 9])
    if maybe(0.15):
        item['enabled'] = pick([False, True, 0, None])
    if maybe(0.1):
        item['library_enabled'] = pick([False, True, 0])
    return item


def rand_rule(i):
    item = {'id': pk(RULE_IDS), 'itemType': pick(['rule'] * 8 + ['RULE', None, 'other'])}
    if maybe(0.95):
        item['rule_type'] = pick(['DOMAIN-SUFFIX', 'DOMAIN', 'DOMAIN-KEYWORD', 'DOMAIN-REGEX', 'IP-CIDR', 'IP-CIDR6',
                                  'GEOIP', 'MATCH', 'domain', None, 5])
    if maybe(0.95):
        item['value'] = pick(DOMAINS + IPS + REGEXES + ['', None, 5, 1.5, True])
    if maybe(0.1):
        item['enabled'] = pick([False, True, 0])
    if maybe(0.05):
        item['library_enabled'] = False
    return item


def id_list(pool):
    if maybe(0.05):
        return pick([None, 'rs-cn', 5, {}, {'rs-cn': 1}])
    return R.sample(pool + ['unknown'], R.randint(0, min(len(pool) + 1, 5)))


def rand_config():
    WILD[0] = maybe(0.3)
    config = {}
    items = []
    for i in range(R.randint(0, 10)):
        r = R.random()
        if r < 0.45:
            items.append(rand_ruleset(i))
        elif r < 0.9:
            items.append(rand_rule(i))
        else:
            items.append({'id': pick(RS_IDS + RULE_IDS), 'itemType': pick(['folder', '', None])})
    if maybe(0.85):
        config['rule_configs'] = items
    else:
        config['rules'] = [it for it in items if it.get('itemType') == 'rule']
        config['rule_sets'] = [it for it in items if it.get('itemType') == 'ruleset']
        if maybe(0.3):
            config['rule_configs'] = []
    mos = {}
    if maybe(0.85):
        mos['direct_rulesets'] = id_list(RS_IDS)
    if maybe(0.85):
        mos['proxy_rulesets'] = id_list(RS_IDS)
    if maybe(0.7):
        mos['direct_rules'] = id_list(RULE_IDS)
    if maybe(0.7):
        mos['proxy_rules'] = id_list(RULE_IDS)
    optional = {
        'custom_matches': custom_matches_value,
        'custom_match_position': lambda: pick(['head', 'tail', 'middle', None, 5]),
        'custom_config': custom_config_text,
        'log_enabled': lambda: pick([True, False, 0, 1, '', 'no', None]),
        'log_level': lambda: pick(['info', 'debug', 'warn', '', None, 5, {'x': 1}]),
        'log_file': lambda: pick(['./mosdns.log', '/var/log/mosdns.log', '', None, '日志.log']),
        'api_enabled': lambda: pick([True, False, 0, None]),
        'api_address': lambda: pick(['0.0.0.0:8338', '127.0.0.1:9091', '', None, ':8338', ['a']]),
        'cache_enabled': lambda: pick([True, False, 0, None, 'yes']),
        'cache_size': lambda: pick([10240, 0, -5, '2048', ' 4096 ', '1_000', 'abc', '1.5', 1.9, -1.9, None, True,
                                    False, [], {}, '٣٠', 1e20, 12345678901234567890, '']),
        'cache_lazy_ttl': lambda: pick([21600, 0, '3600', 'x', None, 2.5, True, [1]]),
        'cache_dump_enabled': lambda: pick([True, False, 0, None]),
        'cache_dump_file': lambda: pick(['./cache.dump', ' /tmp/c.dump ', '', None, 5, 0, ['a'], '　x　']),
        'cache_dump_interval': lambda: pick([300, 0, -10, '600', 'abc', None, 1.5, -0.5, [], {}, True, '1e3', 7.9]),
        'local_dns': upstream_text,
        'remote_dns': upstream_text,
        'fallback_dns': upstream_text,
        'custom_hosts': lambda: pick(['', None, 'a.test 1.2.3.4\n1.2.3.5 b.test\n# c\nc.test ::1  # comment\nbad line',
                                      'x.test 2001:db8::1', 'only-one-field', 5, ['a'], 'a b', '\n\n',
                                      'fe80::1%eth0 z.test', 'a.test　1.1.1.1']),
        'default_forward': lambda: pick(['forward_remote', 'forward_local', 'other', None]),
        'listen_address': lambda: pick([':53', '0.0.0.0:5353', '', None, 0, {'a': 1}, ['x', 'y']]),
    }
    for key, make in optional.items():
        if maybe(0.4):
            mos[key] = make()
    config['mosdns'] = mos
    system = {}
    if maybe(0.8):
        system['server_domain'] = pk(['https://cf.example.test', '', '  http://10.0.0.1:8080  ', 'http://[::1]:5000',
                                        'http://cf.example.test/sub/', 'https://[bad', '　https://x.test　'])
    if maybe(0.85) or not WILD[0]:
        system['rule_proxy_token'] = pk(['tok', 'a b&c=d/é', '', None, 5])
    config['system_config'] = system
    if maybe(0.3):
        config['profile_id'] = pick(['office', 'p 1', '', None, 5, 'default'])
    if WILD[0] and maybe(0.25):
        malform(config)
    return config


def malform(config):
    target = R.random()
    if target < 0.2:
        config['mosdns'] = pick(WEIRD)
    elif target < 0.35:
        config['system_config'] = pick(WEIRD)
    elif target < 0.5:
        config['rule_configs'] = pick(WEIRD)
    elif target < 0.6:
        config.setdefault('rule_configs', []).append(pick(WEIRD))
    elif target < 0.7:
        config['system_config'] = {'server_domain': pick(WEIRD)}
    elif target < 0.8:
        config.pop('rule_configs', None)
        config['rules'] = pick(WEIRD)
        config['rule_sets'] = pick(WEIRD + [[{'id': 'x', 'name': 'X', 'url': 'https://r.example.test/x'}]])
    elif target < 0.9:
        for it in config.get('rule_configs') or []:
            if isinstance(it, dict) and maybe():
                it['id'] = pick([['list'], {'d': 1}, None, 0])
    else:
        config['mosdns'] = dict(config.get('mosdns') or {}, direct_rulesets=pick(WEIRD), proxy_rulesets=pick(WEIRD))


FIXED_CONFIGS = [
    {},
    {'mosdns': {}},
    {'mosdns': {'cache_enabled': True, 'cache_dump_enabled': False}},
    {'mosdns': {'cache_enabled': False}},
    {
        'system_config': {'server_domain': 'https://cf.test', 'rule_proxy_token': 'tok'},
        'mosdns': {'direct_rulesets': ['rs-cn', 'rs-cnip'], 'proxy_rulesets': ['rs-mixed'], 'direct_rules': ['r-ip'],
                   'proxy_rules': [], 'default_forward': 'forward_local',
                   'custom_matches': [{'matches': 'qname $x', 'exec': 'accept'}], 'custom_match_position': 'head',
                   'custom_hosts': 'a.test 1.2.3.4'},
        'rule_configs': [
            {'id': 'rs-mixed', 'name': 'Telegram', 'itemType': 'ruleset', 'behavior': 'classical',
             'url': 'https://r.test/tg.yaml'},
            {'id': 'rs-cn', 'name': 'CN', 'itemType': 'ruleset', 'behavior': 'domain', 'url': 'https://r.test/cn.txt'},
            {'id': 'rs-cnip', 'name': 'CNIP', 'itemType': 'ruleset', 'behavior': 'ipcidr',
             'url': 'https://r.test/cnip.txt'},
            {'id': 'r-ip', 'itemType': 'rule', 'rule_type': 'IP-CIDR', 'value': '192.0.2.0/24'},
        ],
    },
]


def run_config(config):
    base = pk(['', 'http://panel.example.test:8080', 'https://[bad', 'http://p.example.test/'])
    mos = config.get('mosdns') if isinstance(config.get('mosdns'), dict) else {}
    unholdable = not all(text_holdable(mos.get(k)) for k in ('custom_config', 'local_dns', 'remote_dns', 'fallback_dns'))
    record('generate', [config, base], lambda: m.generate_mosdns_config(deepcopy(config), base), unholdable=unholdable)
    record('downloads', [config, base], lambda: m.get_mosdns_ruleset_downloads(deepcopy(config), base))
    record('custom_files', [config], lambda: m.get_mosdns_custom_files(deepcopy(config)))
    record('split', [config], lambda: list(m.split_rules_and_rulesets(deepcopy(config))))
    sets = config.get('rule_configs') if isinstance(config.get('rule_configs'), list) else []
    for rs in sets[:2]:
        if isinstance(rs, dict) and rs.get('itemType') == 'ruleset':
            stem = pick([None, 'stem', ''])
            record('ruleset_files', [config, base, rs, stem],
                   lambda: m._ruleset_files(deepcopy(config), base, deepcopy(rs), stem))
            url = pick(URLS[:3] + ['https://a.test/x?y=1', '/api/rules/x'])
            part = pick([None, 'domain', 'ip', ''])
            source = pick([None, 'lib', 5, ['a', 'b'], {'k': 1}, True, '', 1.5])
            record('proxy_url', [config, base, url, part, source],
                   lambda: m._build_rule_proxy_url(deepcopy(config), base, url, part=part, source_id=source))


def main():
    WILD[0] = True
    for config in FIXED_CONFIGS:
        run_config(config)
    for _ in range(COUNT):
        run_config(rand_config())
    WILD[0] = True
    for _ in range(COUNT):
        content = rule_content()
        args = [content] + ([] if maybe(0.3) else [pick(['domain', 'ip', 'domain', 'ip', '', 'other'])])
        record('convert', args, lambda: m.convert_rule_content_for_mosdns(*args), unholdable=not text_holdable(content))
    for pattern in PATTERNS + DOMAINS + IPS:
        record('domain_pattern', [pattern], lambda: m.domain_pattern_for_mosdns(pattern))
    for _ in range(COUNT // 4):
        line = pick([clash_line(), native_line(), rule_line()])
        record('parse_clash', [line], lambda: list(m.parse_clash_rule_value(line)))
    for _ in range(COUNT):
        value = upstream_text()
        record('upstreams', [value], lambda: {'dump': dump(m.parse_dns_upstreams(value))},
               unholdable=not text_holdable(value))
    for _ in range(COUNT // 4):
        value = custom_matches_value()
        record('custom_matches', [value], lambda: m._parse_custom_matches(value))
    for behavior in ['domain', 'ipcidr', 'classical', 'other', None, 5, '']:
        record('ruleset_parts', [behavior], lambda: m._ruleset_parts(behavior))
    out = Path(__file__).with_name('corpus.jsonl.gz')
    data = '\n'.join(json.dumps(e, ensure_ascii=False, allow_nan=False) for e in OUT) + '\n'
    with open(out, 'wb') as raw:
        with gzip.GzipFile(fileobj=raw, mode='wb', mtime=0) as fh:
            fh.write(data.encode())
    counts = {}
    for e in OUT:
        counts[e['fn']] = counts.get(e['fn'], 0) + 1
    errors = sum(1 for e in OUT if 'err' in e)
    print(f'{len(OUT)} rows ({errors} errors) {counts} -> {out}')


if __name__ == '__main__':
    main()
