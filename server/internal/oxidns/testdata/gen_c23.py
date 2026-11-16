"""Random corpus for module C23 (OxiDNS validation and constraints), run with
real Python. Synthetic values only.

    rt --back server/internal/oxidns/testdata/c23_corpus.jsonl.gz -- bash -lc \\
        'PYTHONPATH=.:server/internal/oxidns/testdata ~/cf-test/.venv/bin/python \\
         server/internal/oxidns/testdata/gen_c23.py'

Rows (same format as gen.py): validate_native_config / require_valid_document
/ validate_expanded_document on random documents (every catalog plugin type,
schema-driven arguments with mutations, constraint-targeted arguments, global
sections, references, expressions, cycles), collect_plugin_issues,
collect_global_issues, collect_reference_issues (with graph), format_error
for the C23 formats, validate_plugin_format, and the private helpers
(_zone_records_error, _adguard_error, _cron_error, _domain_error,
_regex_error, _duration, ZoneInfo keys, encodings.idna.ToASCII).
"""
import encodings.idna
import gzip
import json
import os
import random
import sys
import zoneinfo
from pathlib import Path

import c22_record as rec
from backend.common import oxidns_validation as vl
from backend.common import oxidns_plugin_constraints as pc
from backend.common import oxidns_global_constraints as gc
from backend.common import oxidns_reference_validation as rv

R = random.Random(2311)
SCALE = float(sys.argv[1]) if len(sys.argv) > 1 else 1.0
OUT = []
GLOBAL, CATALOG = vl.schemas()
KINDS = sorted(CATALOG)


def n(count):
    return max(1, int(count * SCALE))


def row(fn, args, call, out=lambda v: v, schemas=False):
    entry = {'fn': fn}
    try:
        entry['args'] = rec.conv(args, schemas)
    except (TypeError, RecursionError):
        return
    try:
        result = call()
    except Exception as exc:  # noqa: BLE001 - every Python exception is part of the contract
        entry['err'] = [type(exc).__name__, str(exc)]
        try:
            if hasattr(exc, 'issues'):
                entry['issues'] = rec.conv(exc.issues)
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


UNICODE = ['ü', 'é', '中文', 'ﬀ', 'ß', 'İ', 'ı', 'ſ', '٣', '²', '①', 'Ⅻ', '　', ' ', '​', '́',
           'א', 'ب', '😀', 'Å', 'K', ' ', '\x85', '\x1c', '\t', '\r', '\n', '\x00', '\\', '"', "'"]
WORDS = ['a', 'b', 'x1', 'abc', 'Test', 'cache', 'seq', 'main', 'forward', 'set', 'p', 'qs.exec.x', 'tag.one', '_x', '-x']


def junk():
    return ''.join(R.choice(UNICODE + WORDS + [' ', ',', ':', ';', '.', '/', '$', '${A}', '$${B}', '${', '}', '#', '@', '|', '-', '='])
                   for _ in range(R.randint(0, 5)))


def big_int():
    return pick(0, 1, -1, 59, 60, 65535, 65536, 4294967295, 4294967296, 18446744073709551615,
                18446744073709551616, 10 ** 30, -10 ** 30, 255, 256, 32, 33, 128, 129, 64, 3600)


def scalar():
    return pick(None, True, False, '', ' ', junk(), big_int(), 0.0, 1.5, -0.0, 1e300, 0.001, [], {}, 'null', '0')


TIMEZONES = ['Asia/Shanghai', 'UTC', 'Etc/GMT+8', 'America/New_York', 'Europe/London', 'posix/Asia/Tokyo',
             'right/UTC', 'asia/shanghai', 'Asia/Nowhere', 'America', 'zone.tab', 'tzdata.zi', 'posixrules',
             'Factory', 'localtime', '/etc/localtime', '../etc/passwd', 'Asia/../UTC', 'Asia//Shanghai', './UTC',
             'UTC/', '.', '', ' ', 'Asia/Shanghai ', ' UTC', 'UTC\x00', 'Asia/上海', 'GMT+0', 'GMT-14', 'EST5EDT',
             '${TZ}', 'Asia/Shanghai\n', 'Etc', 'leapseconds', 'iso3166.tab', 'SECURITY', 'right', 'Europe/Kyiv']

DURATIONS = ['5', '5s', '5S', '5ms', '5MS', '5Ms', '5mS', '5 m', '5　h', '5d', '5ſ', '5mſ', '5x', '٣s', '5٣', '',
             ' ', '-5', '+5', '0', '0ms', '59', '60', '59999ms', '60000ms', '1m', '18446744073709551615',
             '18446744073709551616', '18446744073709551615d', '99999999999999999999999ſ', '5 s s', '1e3', '00060',
             '\t60\t', '5sec', '${D}', '1h30m']


def duration_value():
    return pick(*DURATIONS, *[big_int() for _ in range(3)], True, None, 1.0, [])


IPS = ['1.2.3.4', '01.2.3.4', '1.2.3.256', '::1', 'fe80::1%eth0', '1.2.3.4/24', '1.2.3.4/33', '10.0.0.0/8', '::/0',
       '0.0.0.0/0', '1.2.3.4/0.0.0.0', '1.2.3.4/255.255.255.0', '1.2.3.4/0.0.0.255', '::ffff:1.2.3.4', ' 1.2.3.4',
       '1.2.3.4 ', 'abc', '', '1.2.3.4/00', '2001:db8::/32', '2001:db8::/129', '1.2.3.4%x', '١.٢.٣.٤', '${IP}']

REGEXES = ['^a.*b$', '(', '(?=x)', '(?<!y)z', r'a\1', r'\\1', r'\p{L}', 'a&&b', 'a--b', '[a-', 'a{1,2}', 'a{99999999999}',
           '(?P<n>x)(?P=n)', r'\d+\w', '', ' ', '(?i)abc', '[[:alpha:]]', '*', 'a**', r'\N{LATIN SMALL LETTER A}',
           '(' * 50 + ')' * 50, 'ü+', '(?<n>x)']

DOMAINS = ['example.com', 'full:example.com', 'domain:', 'domain: .', 'keyword:x', 'regexp:^a', 'regexp:(', 'regexp: ',
           '#comment', '', ' ', 'full:', '.', '..', 'bücher.de', '${D}', 'regexp:(?=a)', 'example.com.', 'a b']


def zone_name():
    return pick('example.com.', 'example.com', '@', '.', 'a..b.', 'a.b\\.c.', r'\065bc.', r'\300x.', r'\06', '\\',
                'bücher.de.', 'BÜCHER.de.', 'xn--bcher-kva.de.', 'ﬀ.example.', '。', 'a。b。', 'a' * 63 + '.',
                'a' * 64 + '.', ('a' * 63 + '.') * 4, 'ü' * 30 + '.', 'א.', 'אb.', 'a​.', '́a.', '*.example.',
                '"quoted".', 'xn--ü.', 'XN--ü.', r'a\٣00.', r'a\²00.', '{qname}', 'configflow-placeholder.invalid.')


RDATA = {
    'A': ['1.2.3.4', '1.2.3.256', '01.2.3.4', '1.2.3', 'x', '1.2.3.4 5', r'1.2.3.\052', '"1.2.3.4"'],
    'AAAA': ['::1', '::', '1::2::3', '::ffff:1.2.3.4', '1:2:3:4:5:6:7:8', '1:2:3:4:5:6:7:8:9', 'g::1', r'::1.2.3.4\010',
             ':1', '1:', 'fe80::1%eth0', '12345::'],
    'MX': ['10 mail.example.com.', '10 mail', '65536 a.', 'x a.', '10', '٣ a.', '² a.', '10 a. extra'],
    'CNAME': ['target.example.', 'target', 'a..b.', '"t."', 'ü.example.'],
    'TXT': ['"hello world"', 'plain', '"' + 'x' * 256 + '"', r'"\300"', r'"a\"b"', '"a" "b"', '', '( "a"\n "b" )', '"unterminated'],
    'SOA': ['ns. host. 1 2 3 4 5', 'ns. host. 1 2h 3d 4w 5', 'ns. host. 4294967296 2 3 4 5', 'ns. host. 1 2 3 4', 'ns host 1 2 3 4 5'],
    'SRV': ['1 2 80 t.', '1 2 65536 t.', '1 2 3', '1 2 3 t. x'],
    'NAPTR': ['1 2 "u" "E2U+sip" "!^.*$!sip:x@y!" .', '1 2 u s r t.', '1 2 "' + 'x' * 256 + '" s r .'],
    'CAA': ['0 issue "ca.example"', '0 is-sue "x"', '256 issue "x"', '0 "" "x"', '0 issue'],
    'HINFO': ['"cpu" "os"', 'cpu os', '"' + 'é' * 200 + '" os', 'one'],
    'RP': ['a. b.', 'a. b', 'a.'],
    'MINFO': ['a. b.', r'\# 0'],
    'PTR': ['p.example.'], 'NS': ['ns.example.', 'ns'], 'DNAME': ['d.example.'], 'SPF': ['"v=spf1 -all"'],
    'AFSDB': ['1 a.'], 'RT': ['1 a.'], 'ANAME': ['a.'], 'MD': ['a.'], 'NSAP-PTR': ['a.'], 'AVC': ['"x"'], 'RESINFO': ['"x"'],
}
TYPES = list(RDATA) + ['TYPE65', 'TYPE65536', 'TYPE٣', 'ANY', 'OPT', '+5', '5', 'type1', 'mx', 'Txt', 'BOGUS', 'HTTPS', 'SVCB']


def zone_line(single):
    owner = '' if maybe(0.15) else zone_name()
    rtype = pick(*TYPES)
    head = [x for x in (pick('', '3600', '1h', '1W2d', 'IN', 'CH', 'in', 'NONE', '4294967296', '٣', '5x', '+1', '1h5')
                        if maybe(0.5) else '',) if x]
    if single and maybe(0.3):
        head.insert(0, '{qclass}')
    data = R.choice(RDATA.get(rtype, [r'\# 2 abcd', r'\# 1 ab', '# 2 0a0b', r'\# 2 zz', r'\# x 00', r'\# 70000 00', 'x']))
    if maybe(0.1):
        data = pick(r'\# 2 abcd', r'\# 0', r'\# 3 abcdef', '#')
    prefix = ('\t' if not owner else owner + ' ')
    line = prefix + ' '.join(head + [rtype, data])
    if maybe(0.1):
        line += ' ; comment'
    return line


def zone_text(single):
    lines = []
    for _ in range(R.randint(0, 3)):
        if maybe(0.25):
            lines.append(pick('$ORIGIN example.com.', '$ORIGIN rel', '$ORIGIN', '$TTL 300', '$TTL 1w', '$TTL x',
                              '$INCLUDE /etc/zone', '$INCLUDE rel', '$INCLUDE', '$INCLUDE /a b c', '$GENERATE 1-5 a$ A 1.2.3.4',
                              '$GENERATE 5-1 a A 1.2.3.4', '$GENERATE 1-10/0 a A 1.2.3.4', '$GENERATE 1-٣ a A 1.2.3.4',
                              '$GENERATE 1-2', '$generate 1-1 x A 1.2.3.4', '$ınclude /x', '$BOGUS x', '$ORIGIN \\300.',
                              '  $TTL 5', '"$ORIGIN" a.', '$GENERATE 0-9223372036854775808 a A 1.2.3.4'))
        else:
            lines.append(zone_line(single))
    text = pick('\n', '\n', '\r\n', '\n\n')
    out = text.join(lines)
    if maybe(0.1):
        out = '(' + out
    if maybe(0.05):
        out += ')'
    if maybe(0.1):
        out = out.replace(' ', ' ( ', 1) + ' )'
    if single and maybe(0.4):
        out = pick('{qname} 60 IN A 1.2.3.4', '{qname} {qclass} TXT "x"', 'a. {qclass} A 1.2.3.4', '{qname} {qname} A 1.2.3.4',
                   '"{qname}" A 1.2.3.4', '{qname} A 1.2.3.4\n{qname} A 1.2.3.5', '{qname} TXT "{qclass}"', 'a. A {qname}')
    return out


ADGUARD = ['||example.com^', '@@||example.com^', '/ads?/', '/a/$dnstype=A', '/$important', 'example.com$denyallow=a.com|b',
           'x$dnstype=A|~AAAA|1', 'x$dnstype', '$important', '$third-party', '||a.com^$third-party', 'a.com/path',
           '1.2.3.4 a.com', '! comment', '# c', 'a##b', '|a|b|', '||', '.', '/(/', r'/a\1/', 'x$denyallow=ü.com',
           '@@', '  ', 'a$', 'a$,', 'A.COM.', 'x$DNSTYPE=a', '/ü/', 'a b', 'a:b', '${A}', '//', '/']

CRONS = ['* * * * *', '0 0 * * *', '*/5 * * * *', '0 0 L * *', '0 0 L-3 * *', '0 0 15W * *', '0 0 LW * *', '0 0 32W * *',
         '0 0 * * 5L', '0 0 * * 1#3', '0 0 * * 1#6', '0 0 * JAN-MAR MON-FRI', '0 0 * jan sun', '0 0 * JANFEB *',
         '0 0 ? * ?', '0 ? * * *', '60 * * * *', '5-1 * * * *', '*/0 * * * *', '1,,2 * * * *', '* * * *', '* * * * * *',
         '0 0 L-٣ * *', '٣ * * * *', '0 0 * * 7', '0 0 * * 8', '0 0 1-2-3 * *', '0 0 * DEC *', '0 0 * * SAT', 'ﬀ * * * *',
         '0 0 * MAR-JAN *', '0/5 * * * *', '${CRON}', '0 0 * * SUN#2', '0 0 * * TUESDAY']

TEMPLATES = ['${qname}', '${ qname }', '${}', '$${x}', '${a', 'x${y}z${', '${${a}}', 'plain', '', ' ', '$$${a}', '${\n}']

EXPRESSIONS = ['accept', 'return', 'jump seq', 'goto $seq', 'goto', 'jump missing', 'reject', 'reject 3', 'reject NXDOMAIN',
               'reject 16', 'reject 3 4', 'reject badvers', 'mark 1,2', 'mark', 'mark 4294967296', 'set_mark +5', '$cache1',
               '$missing', '$ ', '$', '', '  ', '!qtype A', '! $set1', '!', 'qtype', 'qtype A AAAA', 'qname $dset', 'qname $ipset1',
               'client_ip $ipset1', 'client_ip $dset', 'question $x', 'forward 1.1.1.1 short_circuit=maybe', 'forward short_circuit',
               'forward tls://dns.example:853 short_circuit=true', 'black_hole custom 1.2.3.4', 'black_hole nxdomain 1.2.3.4',
               'black_hole custom', 'black_hole 1.2.3.4,5.6.7.8 short_circuit', 'sleep 5', 'sleep', 'sleep 5x', 'sleep ${T}',
               'rate_limiter 10 20 24 56', 'rate_limiter x', 'rate_limiter inf', 'rate_limiter 1e400 1', 'rate_limiter 1 -1',
               'ipset s,inet,24 t,inet6,64', 'ipset bad', 'nftset inet,t,s,ipv4_addr,24', 'nftset inet,t,s,mac,24', 'nftset x',
               'download http://example.com/f.txt /dir', 'download http://x', 'upgrade force bundle=ABC github_token=t',
               'upgrade force=maybe', 'upgrade bogus', 'upgrade no_restart=true', 'time 08:00-09:00', 'time 8', 'time 25:00-01:00',
               'ttl 5-10', 'ttl 5-x', 'env HOME', 'env', 'string_exp qname contains x', 'string_exp qnamezl', 'string_exp bad',
               'random 0.5', 'random 2', 'cache short_circuit', 'cache bogus', 'ip_selector background tcp:80',
               'ip_selector tcp:0 none', 'client_ip_from_ecs 1.2.3.0/24', 'forward_edns0opt 8 70000', 'has_resp', 'has_resp x',
               '_true', 'drop_resp', 'query_summary x', 'unknown_thing', 'sequence', 'cron', 'domain_set', 'rcode 3', 'qclass IN',
               'reload_provider $dset', 'mark 5', 'ecs_handler', 'prefer_ipv4', 'debug_print', 'qname ${X}', '${X}', 'goto seq']


def pick_expression():
    e = pick(*EXPRESSIONS)
    if maybe(0.1):
        e = '  ' + e
    return e


FORMAT_POOLS = {
    'socket-address': [':53', '0.0.0.0:53', '[::]:53', '::1:53', '1.2.3.4:65536', 'x:53', '[fe80::1%eth0]:53'],
    'socks5': ['1.2.3.4:1080', 'user:pw@h:1080', 'user@h:1', '[::1]:1080', '[h:1', 'h:x'],
    'upstream-address': ['1.1.1.1', 'udp://1.1.1.1:53', 'tls://dns.example:853', 'https://dns.example/dns-query', 'ftp://x', 'h:99999', 'a b'],
    'http-url': ['http://example.com', 'https://[::1]/x', 'ftp://x', 'http://', 'http://h:99999'],
    'http-method': ['GET', 'POST', 'BAD METHOD', ''],
    'duration': DURATIONS,
    'ip': IPS, 'ip-address': IPS, 'ip-network': IPS,
    'timezone': TIMEZONES,
    'plugin-tag': ['seq', 'qs.exec.x', 'a' * 65, 'a.b', '-x', 'x-'],
    'dns-record': None, 'domain-expression': DOMAINS,
    'non-empty-string': ['', ' ', 'x'], 'nonempty-string': ['', 'x'],
}


def format_value(name):
    if name == 'dns-record':
        return zone_text(True)
    pool = FORMAT_POOLS.get(name)
    if name.startswith('dns-'):
        pool = ['A', 'AAAA', 'IN', 'NXDOMAIN', 'noerror', '3', '+3', '16', '65535', '65536', 'BOGUS', 'ü']
    if not pool:
        return junk()
    return pick(*pool)


def from_schema(schema, depth=0):
    if maybe(0.04) or depth > 5:
        return scalar()
    if not isinstance(schema, dict):
        return scalar()
    if 'anyOf' in schema:
        return from_schema(R.choice(schema['anyOf']), depth + 1)
    if 'enum' in schema and maybe(0.85):
        return R.choice(schema['enum'])
    kinds = schema.get('type')
    kind = R.choice(kinds) if isinstance(kinds, list) else kinds
    if schema.get('format') and kind in (None, 'string', 'integer') and maybe(0.85):
        return format_value(schema['format'])
    if kind == 'object' or 'properties' in schema:
        out = {}
        for key, field in schema.get('properties', {}).items():
            if key in schema.get('required', []) or maybe(0.45):
                out[key] = from_schema(field, depth + 1)
        if maybe(0.1):
            out[pick('unknown', 'extra_field')] = scalar()
        if isinstance(schema.get('additionalProperties'), dict):
            for _ in range(R.randint(0, 2)):
                out[pick('k1', 'K', ' ', '', 'x-y')] = from_schema(schema['additionalProperties'], depth + 1)
        return out
    if kind == 'array':
        return [from_schema(schema.get('items', {}), depth + 1) for _ in range(R.randint(0, 3))]
    if kind == 'integer':
        return pick(big_int(), schema.get('minimum', 0), schema.get('maximum', 10), 0, 1)
    if kind == 'number':
        return pick(0.5, 1, -1, 1e308, schema.get('minimum', 0))
    if kind == 'boolean':
        return pick(True, False)
    if kind == 'null':
        return None
    if kind == 'string':
        return pick(junk(), 'x', '', '${ENV}', 'seq')
    return scalar()


TAGS = ['seq', 'seq2', 'cache1', 'fwd', 'dset', 'ipset1', 'dyn', 'srv', 'main', 'loop1', 'loop2', 'x', 'qs.exec.y', 'bad tag']


def targeted_args(kind):
    """Arguments aimed at the cross-field rules of collect_plugin_issues."""
    t = {
        'qname': lambda: pick('example.com regexp:( $dset &x ${A}', ['full:', 'domain:a', 5, True, 'keyword:'], '', [], 'ü.com,regexp:(?=x)'),
        'question': lambda: pick('$x $', ['x', '$y'], '$ '),
        'qtype': lambda: pick('A 1 65536 BOGUS +3', [1, 'MX', True], 'ﬀ'),
        'rcode': lambda: pick('3 NXDOMAIN 16 badsig', ['servfail']),
        'mark': lambda: pick('1 4294967296 x', [0, -1], '+1'),
        'random': lambda: pick('0.5', '2', 'nan', 'inf', '0.5 0.6', '1_0', ' 1 ', '${R}', [0.5], ['1']),
        'env': lambda: pick(['HOME', '=x', ' ', ':v'], 'A=B', '', [], [5]),
        'any_match': lambda: pick(['', ' '], 'a\n\nb', '', ['qtype A']),
        'string_exp': lambda: pick('qname contains x', 'qnamecontains', 'qnamezl', 'qname zl x', 'mark regexp (', '$x eq', 'bad op v',
                                   ['qname', 'regexp', '(?=a)'], 'eq', '${S}', 'url_pathprefix /a'),
        'client_ip_from_ecs': lambda: pick(['1.2.3.0/24', ' 1.2.3.4 ', 'x', 5, ''], 'x'),
        'ttl': lambda: pick('5', '5-10', '5-x', '1-2-3', ' 4294967296', '${T}', {'fix': None}, {'min': 5}, {}, ['x']),
        'forward_edns0opt': lambda: pick('8 70000', '8,9', 'x', '${E}', ''),
        'black_hole': lambda: pick({'mode': None}, {'mode': 'custom'}, {'mode': 'custom', 'ips': [' ', None]}, {'mode': 'NXDOMAIN ', 'ips': ['1.2.3.4']},
                                   {'mode': 'bogus'}, {'mode': 5, 'ips': ['x']}, {'ips': ['1.2.3.4', ' 1.2.3.4']}, 'custom', 'nxdomain 1.2.3.4',
                                   'custom 1.2.3.4 short_circuit=yes', ['custom', '1.2.3.4'], [5, 'x'], '${M}'),
        'cache': lambda: {'dump_file': pick(None, 'f', ''), 'dump_interval': pick(0, False, 0.0, 1, None), 'min_positive_ttl': pick(5, 10, None, 2.5, True),
                          'max_positive_ttl': pick(1, 5, None, 1e309 if False else 3.0)},
        'prefer_ipv4': lambda: {'cache': pick(True, None, False, 'x'), 'cache_ttl': pick(0, False, 5), 'probe_executor': pick(*TAGS)},
        'ip_selector': lambda: {'probe_methods': pick('ping,tcp:80', ['none', 'ping'], 'tcp:0', 'tcp:٣', ' , ', ['tcp:65536', 5], 'none,none', '${P}', 7),
                                'cache': pick({'size': 0}, {'enabled': False, 'ttl': 0}, {'enabled': None, 'failure_ttl': False}, None)},
        'ecs_handler': lambda: {'preset': pick('1.2.3.4', ' 1.2.3.4', 'x', '', None, 5)},
        'hosts': lambda: {'entries': pick(['a.com 1.2.3.4', 'a.com', 'regexp:( 1.2.3.4', 'a.com x', '# c', ' ', 5, '${H} x'], None, 'x')},
        'redirect': lambda: {'rules': [pick('a.com b.com', 'a.com', 'a.com ' + 'b' * 64 + '.com', 'a.com ..', 'regexp:(?=x) b.com', 'a.com ü.com.')]},
        'domain_set': lambda: {'exps': [pick('a.com\nregexp:(', 'full:', 5)], 'sets': [pick(*TAGS), '$x', 5]},
        'dynamic_domain_set': lambda: {'bootstrap_rules': [pick('', 'a.com', '#x', 'domain: .', 'regexp:', None)]},
        'arbitrary': lambda: {'rules': [zone_text(False) for _ in range(R.randint(1, 2))] + ([5] if maybe(0.1) else [])},
        'response': lambda: {'answers': [pick('$INCLUDE /x', ' \n $generate 1-2 a A 1.2.3.4', 'a. A 1.2.3.4', '$INCLUDEx', '　$İNCLUDE x')]},
        'adguard_rule': lambda: {'rules': ['\n'.join(R.choice(ADGUARD) for _ in range(R.randint(1, 3)))]},
        'geosite': lambda: {'selectors': [pick('cn@ads', '@ads', 'cn@', 'cn', ' @ ', '${G}@x', 5)]},
        'ip_set': lambda: {'ips': [pick('1.2.3.4\n# c\n10.0.0.0/8 # x', 'bad', 5)], 'sets': [pick(*TAGS)]},
        'nftset': lambda: {'table_family4': pick('ip', 'bad', 5), 'table_name4': pick('t', ' ', 5), 'set_name4': pick('s', ''), 'mask4': pick(24, 33, 32.5, True),
                           'ipv4': pick(None, None, {}), 'table_family6': 'inet', 'table_name6': 't', 'set_name6': pick('s', None), 'mask6': 129},
        'ros_route': lambda: {'min_ttl': pick(None, 5000, 10), 'max_ttl': pick(None, 100, 3600.5), 'gateway4': pick('', ' ', 'gw', None),
                              'gateway6': pick(None, 'gw6'), 'comment_prefix': pick('a;b', 'ok', 5), 'tls': pick({'insecure': True, 'ca': 'c'}, {'insecure': 1, 'ca': 'c'}, None),
                              'wait_timeout': duration_value(), 'persistent': {'ips': [pick('1.2.3.4\n::/0\n0.0.0.0/0\nbad\n# c', '10.0.0.0/0.0.0.0', '${P}', 5)]}},
        'ros_address_list': lambda: {'address_list4': pick('', 'l', None), 'address_list6': pick(None, 'l6'), 'min_ttl': pick(1, 5000),
                                     'wait_timeout': duration_value(), 'persistent': {'ips': ['::1\n1.2.3.4/0']}},
        'http_request': lambda: {'url': pick('http://example.com/${qname}', 'ftp://x', 'http://', 'http://[::1', '${U}', 5, 'https://h/${'),
                                 'body': pick(None, 'b${}', 'x'), 'json': pick(None, {}), 'form': pick(None, {}, {'': '', 'k': '${v}'}),
                                 'content_type': pick(None, 'a\nb', 'text/plain'), 'timeout': duration_value(),
                                 'headers': pick({'X-A': '${qname}', 'bad header': 'v', ' ': ' '}, None), 'query_params': {'': 'v', 'q': '${'}},
        'learn_domain': lambda: {'timeout': duration_value(), 'provider': pick(*TAGS)},
        'upgrade': lambda: {'timeout': duration_value(), 'outbound': pick('', ' ', 'p1', ' p1', 'missing', None)},
        'script': lambda: {'args': ['${}', 'x', 5], 'env': {'': '${a', 'K': 'v'}},
        'download': lambda: {'downloads': [{'url': pick('http://example.com/a.txt', 'http://example.com/', 'http://[x', '${U}'), 'dir': pick('/d', 'd/', '', '${D}'),
                                            'filename': pick(None, '', 'f')} for _ in range(R.randint(1, 3))] + ([5] if maybe(0.1) else [])},
        'cron': lambda: {'timezone': pick(*TIMEZONES, None, 5), 'jobs': [{'name': pick('j', '', 'j', None), 'schedule': pick(*CRONS, None, ''),
                                                                          'interval': pick(None, '', *DURATIONS), 'executors': [pick_expression(), pick(*TAGS)]}
                                                                         for _ in range(R.randint(0, 3))]},
        'time': lambda: {'periods': [{'start': pick('08:00', '8:00', '24:00', '1٣:00', None, ' 08:00 ', '${S}', 5), 'end': pick('08:00', '09:00', None, '23:59'),
                                      'weekdays': pick(None, [], ['mon', 'MON ', 'funday', '1', 1]), 'monthdays': pick(None, [1])} for _ in range(R.randint(1, 2))] + [5]},
    }
    t['cname'] = t['qname']
    for k in ('client_ip', 'ptr_ip', 'resp_ip'):
        t[k] = lambda: pick('1.2.3.0/24 $ipset1 &x bad', ['::1', 5], '')
    t['reload_provider'] = t['question']
    t['qclass'] = t['qtype']
    t['prefer_ipv6'] = t['prefer_ipv4']
    return t[kind]() if kind in t else None


def plugin_entry(i):
    kind = pick(*KINDS) if maybe(0.85) else pick('unknown_kind', 5, None, '')
    entry = {}
    if maybe(0.95):
        entry['tag'] = TAGS[i % len(TAGS)] if maybe(0.8) else pick(*TAGS, 5, '', 'a' * 70)
    if maybe(0.97):
        entry['type'] = kind
    if maybe(0.93):
        args = targeted_args(kind) if isinstance(kind, str) and maybe(0.5) else None
        if args is None:
            schema = CATALOG.get(kind) if isinstance(kind, str) else None
            args = from_schema(schema) if schema else scalar()
        if kind == 'sequence' or (maybe(0.1) and kind == 'any_match'):
            args = [{'exec': pick_expression(), **({'matches': pick(pick_expression(), [pick_expression(), pick_expression()], 5)} if maybe(0.6) else {})}
                    for _ in range(R.randint(0, 4))]
        if kind in ('udp_server', 'tcp_server', 'quic_server') and isinstance(args, dict) and maybe(0.7):
            args['entry'] = pick(*TAGS, '', '${E}', '$seq')
        if kind == 'fallback' and isinstance(args, dict):
            args.update({'primary': pick(*TAGS), 'secondary': pick(*TAGS)})
        entry['args'] = args
    if maybe(0.03):
        return pick('not a plugin', 5, None, [])
    return entry


def global_part(doc):
    if maybe(0.5):
        doc['log'] = {'level': pick('info', 'INFO', 'verbose', '${L}', 5, None)} if maybe(0.9) else 'x'
    if maybe(0.5):
        ssl = {k: pick(None, '', ' ', 'f', '${C}') for k in ('cert', 'key', 'client_ca') if maybe(0.6)}
        if maybe(0.4):
            ssl['require_client_cert'] = pick(True, False, 1)
        doc['api'] = {'http': {'listen': pick('0.0.0.0:8080', ':80', 'x'), 'ssl': ssl}}
    if maybe(0.5):
        profiles = {}
        for name in R.sample(['p1', 'p2', ' p3', '', 'p4'], R.randint(0, 3)):
            profile = {}
            if maybe(0.6):
                profile['resolver'] = pick('system', 'SYSTEM ', 'dns', '${R}', {'proxy': pick('profile', 'none', None),
                                                                              'nameservers': [{'addr': pick('1.1.1.1', 'dns.example', 'tls://dns.example', 'quic://[::1]:853',
                                                                                                            'h3://x', 'udp://[x', '${A}', 5), 'dial_addr': pick(None, '1.1.1.1')}
                                                                                              for _ in range(R.randint(0, 2))]})
            if maybe(0.6):
                profile['proxy'] = pick('none', 'Direct', 'socks', {'socks5': pick('h:1', None, 5)}, '${P}')
            profiles[name] = profile
        doc['network'] = {'outbound': {'default': pick('p1', ' p1', '', 'missing', None, '${D}'), 'profiles': profiles}}
    return doc


def document():
    doc = {}
    if maybe(0.95):
        doc['plugins'] = [plugin_entry(i) for i in range(R.randint(0, 8))]
    for p in doc.get('plugins', []):
        if isinstance(p, dict) and p.get('type') == 'forward' and isinstance(p.get('args'), dict) and maybe(0.7):
            p['args']['upstreams'] = [{'addr': pick('dns.example', '1.1.1.1', 'tls://dns.example:853', '[::1]:53'), 'bootstrap': pick(None, '', '1.1.1.1:53', '[::1]:53', '[1.2.3.4]:53',
                                                                                                                                 '1.1.1.1', '1.1.1.1:٣', '1.1.1.1:99999', '${B}', 5),
                                       'dial_addr': pick(None, None, 'x'), 'min_conns': pick(None, 70, 5, True), 'max_conns': pick(None, 10, 100.0),
                                       'outbound': pick(None, 'p1', '', ' x')} for _ in range(R.randint(0, 2))]
        if isinstance(p, dict) and p.get('type') == 'http_server' and isinstance(p.get('args'), dict) and maybe(0.7):
            p['args']['entries'] = [{'path': pick('/dns', 'dns', '${P}', 5), 'exec': pick(*TAGS)}]
            p['args']['enable_http3'] = pick(True, False)
    doc = global_part(doc)
    if maybe(0.05):
        return pick([], 'x', None, 5)
    if maybe(0.05):
        doc['plugins'] = pick('x', {}, None)
    return doc


def main():
    for _ in range(n(2500)):
        doc = document()
        row('validate_native_config', [doc], lambda: vl.validate_native_config(doc))
    for _ in range(n(500)):
        doc = document()
        row('require_valid_document', [doc], lambda: vl.require_valid_document(doc))
    for _ in range(n(600)):
        doc = document()
        plugins = doc.get('plugins') if isinstance(doc, dict) and isinstance(doc.get('plugins'), list) else []
        sources = {'plugins': [{'source': pick('config.yaml', 'inc/a.yaml'), 'index': R.randint(0, 3)} for _ in plugins]}
        if maybe(0.1):
            sources['plugins'] = sources['plugins'][:-1]
        if maybe(0.5):
            sources['includes'] = [(pick('inc/a.yaml', 'b.yaml'), global_part({}) if maybe(0.8) else {'log': 5}) for _ in range(R.randint(0, 2))]
        row('validate_expanded_document', [doc, sources], lambda: vl.validate_expanded_document(doc, sources))
    for i in range(n(6000)):
        plugin = plugin_entry(i)
        index = R.randint(0, 9)
        row('collect_plugin_issues', [plugin, index, {}], lambda: pc.collect_plugin_issues(plugin, index, {}))
    for _ in range(n(1500)):
        doc = document()
        row('collect_global_issues', [doc], lambda: gc.collect_global_issues(doc))
    for _ in range(n(1500)):
        doc = document()
        if maybe(0.5):
            graph = {}
            row('collect_reference_issues', [doc, CATALOG, True], lambda: rv.collect_reference_issues(doc, CATALOG, dependency_graph=graph),
                out=lambda issues: {'issues': issues, 'graph': graph}, schemas=True)
        else:
            row('collect_reference_issues', [doc, CATALOG, False], lambda: rv.collect_reference_issues(doc, CATALOG), schemas=True)
    for _ in range(n(1500)):
        name = pick('timezone', 'dns-record', 'domain-expression', 'duration')
        value = format_value(name) if maybe(0.9) else scalar()
        row('format_error', [name, value], lambda: vl.format_error(name, value))
        fmt = pick('dns-record', 'domain-expression', 'dns-record', 'bogus', 5)
        row('validate_plugin_format', [fmt, value], lambda: pc.validate_plugin_format(fmt, value))
    for _ in range(n(4000)):
        single = maybe(0.5)
        text = zone_text(single) if maybe(0.97) else scalar()
        row('zone_records_error', [text, single], lambda: pc._zone_records_error(text, single))
    for _ in range(n(2000)):
        line = '\n'.join(R.choice(ADGUARD) for _ in range(1)) if maybe(0.8) else junk() + R.choice(ADGUARD)
        row('adguard_error', [line], lambda: list(pc._adguard_error(line)))
    for _ in range(n(1500)):
        value = pick(*CRONS) if maybe(0.85) else (junk() if maybe(0.7) else scalar())
        row('cron_error', [value], lambda: pc._cron_error(value))
    for _ in range(n(800)):
        value = pick(*DOMAINS) if maybe(0.85) else scalar()
        dynamic = maybe(0.5)
        row('domain_error', [value, dynamic], lambda: pc._domain_error(value, dynamic))
    for _ in range(n(800)):
        value = pick(*REGEXES) if maybe(0.8) else junk()
        row('regex_error', [value], lambda: pc._regex_error(value))
    for _ in range(n(800)):
        value = duration_value() if maybe(0.9) else junk()
        row('duration', [value], lambda: pc._duration(value), out=lambda v: None if v is None else repr(float(v)))
    zones = []
    root = Path('/usr/share/zoneinfo')
    for path in root.rglob('*'):
        zones.append(str(path.relative_to(root)))
    zones.sort()
    for key in TIMEZONES + R.sample(zones, min(len(zones), n(400))):
        def check(key=key):
            try:
                zoneinfo.ZoneInfo(key)
                return True
            except (zoneinfo.ZoneInfoNotFoundError, ValueError):
                return False
        row('zoneinfo', [key], check)
    labels = ['bücher', 'BÜCHER', 'ﬀ', 'ß', 'İ', 'ı', 'xn--ü', 'XN--ü', 'א', 'אb', 'א1א', '​', '­', 'á', '́',
              'ü' * 60, 'ü' * 20, '中文', '😀', '　', 'Å', 'K', 'ﬁ', '①', 'ǅ', 'İ̇', '가', '가', '각ᆨ',
              'ạ̈', 'ạ̈', 'ཱི', 'Ω', '\U0001d400', '', '￿', '̀', 'क़', 'ﬁ' * 30]
    pool = [chr(c) for c in list(range(0xa0, 0x250)) + list(range(0x300, 0x370)) + list(range(0x590, 0x700)) +
            list(range(0x1100, 0x1200)) + list(range(0x3000, 0x3100)) + list(range(0xac00, 0xac40)) + list(range(0xfb00, 0xfb50)) +
            list(range(0xff00, 0xff70)) + [0x200b, 0x200c, 0x200d, 0xfeff, 0x1d400, 0x1f600, 0xe0001, 0x2ff0]]
    for _ in range(n(3000)):
        labels.append(''.join(R.choice(pool + list('abcXYZ-019')) for _ in range(R.randint(1, 25))))
    friendly = [chr(c) for c in list(range(0xc0, 0x250)) + list(range(0x370, 0x3d0)) + list(range(0x400, 0x460)) +
                list(range(0x4e00, 0x4e40)) + list(range(0x1100, 0x1176)) + list(range(0x11a8, 0x11c3)) + list(range(0xac00, 0xac20)) +
                list(range(0xff01, 0xff5f)) + list(range(0x2160, 0x2180)) + list(range(0x300, 0x370)) + list(range(0x2460, 0x2480)) +
                list(range(0x3300, 0x3320)) + list(range(0x1e00, 0x1f00)) + [0xad, 0x34f, 0x1806, 0x180b, 0xfe00, 0x2060]]
    for _ in range(n(3000)):
        labels.append(''.join(R.choice(friendly + list('abcXYZ-019')) for _ in range(R.randint(1, 12))))
    for label in labels:
        def encode(label=label):
            try:
                return encodings.idna.ToASCII(label).decode()
            except UnicodeError:
                return None
        row('idna_to_ascii', [label], encode)
    path = Path(__file__).with_name('c23_corpus.jsonl.gz')
    data = '\n'.join(json.dumps(e, ensure_ascii=False) for e in OUT) + '\n'
    with open(path, 'wb') as raw:
        with gzip.GzipFile(fileobj=raw, mode='wb', mtime=0) as fh:
            fh.write(data.encode())
    counts = {}
    for e in OUT:
        counts[e['fn']] = counts.get(e['fn'], 0) + 1
    print(len(OUT), 'rows', counts, '->', path)


if __name__ == '__main__':
    main()
