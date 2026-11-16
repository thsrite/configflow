#!/usr/bin/env python3
"""Generate corpus.jsonl.gz: inputs plus what the Python rule engine makes of them.

The Go tests in this package assert equality with every recorded output, so
the corpus must come from the interpreter production runs (CPython 3.11) with
the production modules (backend/utils/*). Run from the repository root:

    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/rules/testdata/gen.py \
        2>/dev/null | gzip -9n > server/internal/rules/testdata/corpus.jsonl.gz

Deterministic (fixed seed); one JSON object per line, ASCII only. Kinds:

    match    rule_configs + library + contents + queries and the full result of
             RuleConfigMatcher.match for each query (dict or null)
    line     parse_rule_line(line)
    direct   match_domain / match_ip / match_query (rule type, value, query)
    syntax   rule_value_error(kind, value)
    regex    re.compile error text (or null) and re.search results; native Go
             boundary behavior is covered in regex_review_test.go
    ip       ipaddress.ip_address / ip_network(strict=False) of a string
    idna     str.encode('idna')
    cache    rule_cache_filename / sanitize_rule_name
    filters  group_candidates string helpers and filter_candidates
    group    managed_members / render_members / validate_candidates
    lower    lower_group_provider_filters on a synthetic Mihomo document
"""
import copy
import ipaddress
import json
import random
import re
import sys

from backend.utils import group_candidates as gc
from backend.utils import rule_matcher, rule_syntax, rule_utils

assert sys.version_info[:2] == (3, 11), sys.version
R = random.Random(20261114)


def emit(**record):
    print(json.dumps(record, ensure_ascii=True, sort_keys=False))


def python_error(exc):
    return f'{type(exc).__name__}: {exc}'


# --------------------------------------------------------------------------
# Rule lines and matching
# --------------------------------------------------------------------------

LINES = [
    'DOMAIN,example.com', 'DOMAIN-SUFFIX,Example.COM', 'DOMAIN-SUFFIX,example.com', 'DOMAIN-KEYWORD,ample',
    'DOMAIN-KEYWORD,', 'DOMAIN-REGEX,^a.*\\.test$', 'DOMAIN-REGEX,^EXAMPLE\\.com$', 'DOMAIN-REGEX,[abc',
    'DOMAIN-REGEX,(?i)^Case', 'DOMAIN-REGEX,^(?!bad)', 'DOMAIN-REGEX,^node[0-9]{1,3}\\.org$',
    'DOMAIN-REGEX,^\\D+\\.org$', 'DOMAIN-REGEX,(', 'DOMAIN-REGEX,a{3,2}', 'DOMAIN-REGEX,.*',
    'IP-CIDR,192.0.2.0/24,no-resolve', 'IP-CIDR,192.0.2.9/24', 'IP-CIDR,192.0.2.1', 'IP-CIDR,192.0.2.0/33',
    'IP-CIDR,192.0.2.0/255.255.255.0', 'IP-CIDR,10.0.0.0/8,no-resolve', 'IP-CIDR6,2001:db8::/32',
    'IP-CIDR6,192.0.2.0/24', 'IP-CIDR,::/0', 'IP-CIDR6,fe80::/16', 'IP-CIDR,127.0.0.1/008', 'IP-CIDR6,::ffff:192.0.2.0/120',
    'IP,192.0.2.5', 'IP,2001:db8::1', 'IP,fe80::1%eth0', 'IP,bad', 'IP-CIDR,bad',
    'domain-suffix,lower.org', 'Domain-Suffix,Mixed.Org', ' DOMAIN-SUFFIX , spaced.test ', '"DOMAIN,quoted.test"',
    "'DOMAIN,single.test'", '- DOMAIN,dash.test', '-DOMAIN,nodash.test', '  - plain-dash.test  ',
    '# comment', '// comment', '! bang', '', '   ', 'DOMAIN,a.test # trailing comment', 'a.test # c',
    '+.wild.test', '*.star.test', '.dot.test', '..bad.test', '+.', '*.', '.', '*', '+.*.x.test', 'api.*.test',
    '.api.*.test', 'full:full.test', 'domain:mos.test', 'keyword:kw', 'regexp:^re[0-9]+\\.test$', 'regex:[',
    'regexp:^\\D+$', 'ip:192.0.2.1', 'ip:2001:db8::1', 'payload:', 'payload: x', 'FULL:Upper.test',
    'plain.test', 'PLAIN.TEST', 'trailing.dot.', 'a_b.test', '-lead.test', 'xn--fsqu00a.xn--0zwm56d',
    '例子.测试', '192.0.2.1', '192.0.2.77', '2001:db8::2', 'fe80::1%eth0', '::1', '::ffff:192.0.2.1',
    '192.0.2.0/24', '192.0.2.9/24', '2001:db8::/48', '192.0.2.0/255.0.0.0', '192.0.2.0/0.0.0.255', '999.999.999.999',
    '1.2.3', '3com.org', '123.example', 'DOMAIN,', 'DOMAIN-SUFFIX,', 'MATCH', 'DOMAIN', 'MATCH,DIRECT',
    'AND,((DOMAIN,a.test),(NETWORK,tcp))', 'GEOIP,CN', 'RULE-SET,x', 'DST-PORT,80', 'IP-CIDR,2001:db8::/32,no-resolve,extra',
    'DOMAIN-SUFFIX,a.test,DIRECT', 'UNKNOWN,x.test', 'DOMAIN-SHUFFIX,tmdb.org', 'tmdb.org', '+.tmdb.org',
    'DOMAIN-SUFFIX,Ǆ.test', 'DOMAIN,İ.test', 'DOMAIN,ΑΣ.test', 'DOMAIN-SUFFIX,ï.test',
]

QUERIES = [
    'example.com', 'www.example.com', 'a.b.example.com', 'EXAMPLE.COM', 'example.com.', 'Www.Example.Com',
    'sub.wild.test', 'wild.test', 'x.star.test', 'y.x.star.test', 'star.test', 'a.dot.test', 'dot.test',
    'full.test', 'upper.test', 'mos.test', 'kw.test', 'keyword.example', 're123.test', 'plain.test', 'PLAIN.TEST',
    'trailing.dot', 'api.mid.test', 'sub.api.mid.test', 'api.test', 'a_b.test', '-lead.test', 'a.test',
    'quoted.test', 'single.test', 'dash.test', 'nodash.test', 'plain-dash.test', 'node12.org', 'text.org',
    'xn--fsqu00a.xn--0zwm56d', '例子.测试', 'localhost', 'x', '', 'bad host', 'case.test', 'bad.test',
    'lower.org', 'mixed.org', 'sub.mixed.org', 'spaced.test', 'tmdb.org', 'api.tmdb.org', 'x.x.test', 'sub.x.x.test',
    '3com.org', '123.example', 'a.b', 'ï.test', 'sub.ï.test', 'i̇.test', 'as.test', 'ας.test',
    '192.0.2.1', '192.0.2.5', '192.0.2.77', '192.0.2.250', '192.0.3.1', '10.0.0.1', '10.9.9.9', '127.0.0.1',
    '2001:db8::1', '2001:db8::2', '2001:db8:1::1', '2001:db9::1', 'fe80::1%eth0', 'fe80::1%eth1', 'fe80::1',
    '::1', '::', '::ffff:192.0.2.1', '::ffff:10.0.0.1', '999.1.1.1', '1.2.3', '1.2.3.4.', '192.0.2.1.', 'a.b.c.d.',
]

POLICIES = ['DIRECT', 'PROXY', 'REJECT', 'Proxy']


def yaml_body(lines):
    style = R.choice(['block', 'flow', 'quoted', 'comment', 'mixed'])
    texts = [line for line in lines if '\n' not in line]
    if style == 'flow':
        return 'payload: [' + ', '.join(json.dumps(text) for text in texts) + ']\n'
    if style == 'quoted':
        return 'payload:\n' + ''.join(f'  - {json.dumps(text)}\n' for text in texts)
    if style == 'comment':
        return '# header\npayload:\n' + ''.join(f"  - '{text.replace(chr(39), chr(39) * 2)}' # note\n" for text in texts)
    if style == 'mixed':
        return 'payload:\n' + ''.join(f'  - {text}\n' for text in texts if not re.search(r'[#:\'"\[\]{},&*!|>%@`]|^[-?]', text) and text.strip())
    return 'payload:\n' + ''.join(f'- {json.dumps(text)}\n' for text in texts)


def make_content(lines, fmt):
    if fmt == 'yaml' or (fmt is None and R.random() < 0.25):
        return yaml_body(lines)
    sep = R.choice(['\n', '\n', '\n', '\r\n', '\r', '\n\n', '\n', '\r\n'])
    prefix = '﻿' if R.random() < 0.05 else ''
    return prefix + sep.join(lines) + R.choice(['', '\n'])


def match_case(index):
    rules, library, contents = [], [], {}
    for n in range(R.randint(1, 3)):
        if R.random() < 0.25:
            kind = R.choice(['DOMAIN', 'DOMAIN-SUFFIX', 'DOMAIN-KEYWORD', 'DOMAIN-REGEX', 'REGEX', 'REGEXP', 'IP', 'IP-CIDR', 'IP-CIDR6', 'SUBDOMAIN-ONLY', 'DIRECT-SUBDOMAIN'])
            value = R.choice(['example.com', 'Example.COM', 'ample', '^a.*', 'a{2', '192.0.2.0/24', '192.0.2.5', '2001:db8::/32', 'fe80::1%eth0', 'bad', '', '.', 'test'])
            rules.append({'id': f'r{n}', 'itemType': 'rule', 'rule_type': kind, 'value': value,
                          'policy': R.choice(POLICIES), **({'enabled': False} if R.random() < 0.1 else {})})
            continue
        behavior = R.choice(['domain', 'ipcidr', 'classical', 'classical', None, 'weird'])
        fmt = R.choice(['text', 'text', 'yaml', None])
        lines = R.sample(LINES, R.randint(1, 14))
        rid = f's{n}'
        item = {'id': rid, 'itemType': 'ruleset', 'name': f'Set {n}', 'policy': R.choice(POLICIES)}
        if behavior:
            item['behavior'] = behavior
        if fmt:
            item['format'] = fmt
        if R.random() < 0.1:
            item['enabled'] = False
        if R.random() < 0.1:
            item['library_enabled'] = False
        if R.random() < 0.3:
            item['library_rule_id'] = f'lib{n}'
            entry = {'id': f'lib{n}'}
            if R.random() < 0.5 and behavior is None:
                entry['behavior'] = R.choice(['domain', 'ipcidr', 'classical'])
            if R.random() < 0.3:
                entry['format'] = R.choice(['text', 'yaml'])
            library.append(entry)
        rules.append(item)
        contents[rid] = make_content(lines, fmt)
    queries = R.sample(QUERIES, 6)
    # Queries taken from the lines themselves exercise the hit paths.
    for text in R.sample(LINES, 3):
        for token in re.split(r'[,:]', text)[-1:]:
            queries.append(token.strip().strip('+*.').strip() or 'x')
    results = []
    loader = lambda item, _: contents.get(item['id'], '')
    matcher = rule_matcher.RuleConfigMatcher(copy.deepcopy(rules), copy.deepcopy(library), loader)
    for query in queries:
        try:
            results.append(matcher.match(query))
        except Exception as exc:  # recorded, so Go can be compared on it too
            results.append({'__error__': python_error(exc)})
    emit(kind='match', configs=rules, library=library, contents=contents, queries=queries, results=results)


# --------------------------------------------------------------------------
# Tests of single functions
# --------------------------------------------------------------------------

def line_cases():
    pool = list(LINES)
    glue = ['', ' ', '\t', '#x', ',no-resolve', ', no-resolve ', ',NO-RESOLVE', '"', "'", '- ', '-', '+.', '*.', '.', 'ip:', 'domain:', 'DOMAIN-SUFFIX,', ':', ',', '##']
    for _ in range(900):
        pool.append(R.choice(glue) + R.choice(LINES) + R.choice(glue))
    for text in pool:
        parsed = rule_matcher.parse_rule_line(text)
        emit(kind='line', line=text, result=list(parsed) if parsed else None)


def direct_cases():
    types = ['DOMAIN', 'DOMAIN-SUFFIX', 'DOMAIN-KEYWORD', 'DOMAIN-REGEX', 'REGEX', 'REGEXP', 'SUBDOMAIN-ONLY',
             'DIRECT-SUBDOMAIN', 'IP', 'IP-CIDR', 'IP-CIDR6', 'OTHER']
    values = ['example.com', 'Example.COM', 'ample', '^a.*', '^A.*', '^\\D+$', '^\\d+$', '[', 'a{2', '(?i)EXAMPLE', 'test', '',
              '192.0.2.0/24', '192.0.2.5', '192.0.2.5/32', '2001:db8::/32', 'fe80::1%eth0', 'fe80::1%eth1', 'bad', '0.0.0.0/0', '::/0',
              '192.0.2.0/255.255.255.0', '::ffff:192.0.2.0/120']
    for _ in range(2500):
        rule_type, value, query = R.choice(types), R.choice(values), R.choice(QUERIES)
        domain_result = rule_matcher.match_domain(query, rule_type, value) if query and '\n' not in query else None
        emit(kind='direct', rule_type=rule_type, value=value, query=query,
             match_domain=domain_result, match_ip=rule_matcher.match_ip(query, rule_type, value),
             match_query=rule_matcher.match_query(query, rule_type, value),
             valid_domain=rule_matcher.is_valid_domain(query), valid_dns=rule_matcher.is_valid_domain(query, allow_dns_labels=True),
             valid_ip=rule_matcher.is_valid_ip(query), normalized=rule_matcher.normalize_match_query(query))


IPS = ['192.0.2.1', '0.0.0.0', '255.255.255.255', '01.2.3.4', '1.2.3', '1.2.3.4.5', '256.1.1.1', '1.2.3.4%x', ' 1.2.3.4', '1.2.3.4 ', '١.2.3.4',
       '::', '::1', '1::', '2001:db8::1', '2001:0db8:0000:0000:0000:0000:0000:0001', '2001:db8::1%eth0', '2001:db8::1%', '2001:db8::1%a%b', 'fe80::1%eth0',
       '::ffff:192.0.2.1', '::ffff:c000:201', '::192.0.2.1', '1:2:3:4:5:6:7:8', '1:2:3:4:5:6:7:8:9', '12345::', ':::', '1::2::3', 'g::1', '[::1]',
       '1:2:3:4:5:6:1.2.3.4', '::1.2.3', '::01.2.3.4', 'fe80::1%eth0/64', '', '/', 'a', '192.0.2.0/24', '192.0.2.1/32', '192.0.2.1/33', '192.0.2.1/0',
       '192.0.2.1/00', '192.0.2.1/024', '192.0.2.1/-1', '192.0.2.1/+8', '192.0.2.1/٨', '192.0.2.1/', '192.0.2.1//8', '192.0.2.0/255.255.255.0',
       '192.0.2.0/0.0.0.255', '192.0.2.0/255.0.255.0', '192.0.2.0/0.255.0.255', '192.0.2.0/255.255.255.255', '192.0.2.0/0.0.0.0', '192.0.2.0/255.255.255.256',
       '2001:db8::/32', '2001:db8::1/128', '2001:db8::1/129', '2001:db8::/ffff::', '2001:db8::1%eth0/64', 'fe80::1%eth0/10', '::/0', '::ffff:192.0.2.0/120',
       '::ffff:192.0.2.1/128', '1.2.3.4/8', '1.2.3.4/1e1', '1.2.3.4/ 8', ' 1.2.3.4/8', '0/0', '10/8', '1.2.3.4/255.255.0.0/8']


def ip_cases():
    for text in IPS:
        try:
            ip = ipaddress.ip_address(text)
            address = [ip.version, str(int(ip)), getattr(ip, 'scope_id', None)]
        except ValueError:
            address = None
        try:
            net = ipaddress.ip_network(text, strict=False)
            network = [net.version, str(int(net.network_address)), net.prefixlen, str(net)]
        except ValueError:
            network = None
        emit(kind='ip', text=text, address=address, network=network)


IDNAS = ['', 'example.com', 'Example.COM', 'a.b.c', 'a..b', '.a', 'a.', 'a..', '..', '.', 'x' * 63 + '.com', 'x' * 64 + '.com', 'a.' + 'x' * 64,
         'a' * 250 + '.com', '-bad.com', 'bad-.com', 'a_b.com', 'xn--fsqu00a.xn--0zwm56d', '例子.测试', '例子.测试.', 'bücher.de',
         'BÜCHER.de', 'ß.de', 'faß.de', 'Ａ.com', 'ǅ.com', 'a­.com', '­.com', 'ab。cd', 'a．b', 'a｡b',
         'مثال.إختبار', 'مثالa.com', 'aمثال.com', '1مثال1',
         '​.com', 'a‍b.com', ' .com', 'a b.com', ' .com', '\u0000.com', 'a\x00.com', '﻿.com', '.com', '﷐.com',
         'é.com', 'é.com', 'xn--e1afmkfd.xn--p1ai', 'пример.рф', 'ドメイン.テスト',
         '한글.한국', '가.com', 'Ⅰ.com', 'ﬁ.com', 'Ω.com', 'K.com', 'İ.com', 'ı.com', 'xn--例.com',
         'XN--abc.例', '例' * 20 + '.com', '例' * 40 + '.com', 'a' * 60 + '例.com', 'é' * 30 + '.com', 'ไทย.com',
         'あ゙.com', 'が.com', '\U0001d400.com', '\U0001f600.com', '\U000e0001.com', '�.com', ' .com', 'a.́.com']


def idna_cases():
    for text in IDNAS:
        try:
            result = text.encode('idna').decode('ascii')
        except UnicodeError:
            result = None
        emit(kind='idna', text=text, result=result)
    blocks = [(0x00a1, 0x024f), (0x0370, 0x03ff), (0x0400, 0x04ff), (0x0590, 0x06ff), (0x0900, 0x097f), (0x0e00, 0x0e7f), (0x1100, 0x11ff),
              (0x1e00, 0x1eff), (0x2000, 0x206f), (0x2100, 0x218f), (0x3040, 0x30ff), (0x4e00, 0x4f00), (0xac00, 0xac40), (0xfb00, 0xfb4f),
              (0xfe00, 0xfe6f), (0xff00, 0xffef), (0x1d400, 0x1d4ff), (0x1f300, 0x1f3ff)]
    for _ in range(600):
        label = ''.join(chr(R.randint(*R.choice(blocks))) for _ in range(R.randint(1, 6)))
        text = R.choice(['', 'a', '1', '-']) + label + R.choice(['', 'z', '-']) + R.choice(['.com', '', '.', '.测试'])
        try:
            result = text.encode('idna').decode('ascii')
        except UnicodeError:
            result = None
        emit(kind='idna', text=text, result=result)


REGEXES = [
    '', 'a', 'abc', 'a|b', '(a|b)+c', 'a*', 'a+?', 'a??', 'a{2}', 'a{2,}', 'a{,3}', 'a{2,3}?', 'a{3,2}', 'a{', 'a{}', 'a{1', 'a{1,', 'a{x}', 'a{,}', 'a{1,2,3}',
    '[abc]', '[^abc]', '[a-c]', '[c-a]', '[abc', '[', '[]', '[]a]', '[^]a]', '[a-]', '[-a]', '[a-\\d]', '[\\d-z]', '[\\w.]', '[\\W]', '[\\Wa]', '[[:alpha:]]',
    '[[]', '[a\\]b]', '[\\x41-\\x5a]', '[\\u0041]', '[\\q]', '(', ')', '(()', '())', '(?', '(?:', '(?:a)', '(?P<n>a)', '(?P<n>a)(?P=n)', '(?P<n', '(?P<1n>a)',
    '(?P<n>a)(?P<n>b)', '(?P=x)', '(?P>a)', '(?<n>a)', '(?<=a)b', '(?<!a)b', '(?=a)', '(?!a)', '(?#comment)a', '(?#', '(?i)a', 'a(?i)', '(?i:a)', '(?-i:a)',
    '(?i-s:a)', '(?x) a b # c', '(?s).', '(?m)^a$', '(?a)\\w', '(?u)\\w', '(?L)a', '(?z)', '(?i', '(?i-', '(?-', '(?i-i:a)', '(?>a)', '(?(1)a|b)', '(a)(?(1)b|c)',
    '(a)(?(2)b|c)', '(?(x)a)', '(a)\\1', '\\1', '(a)\\2', '(?:a)\\1', '\\0', '\\08', '\\12', '\\123', '\\400', '\\x', '\\x4', '\\x41', '\\u00', '\\u0041', '\\U0041',
    '\\U00000041', '\\U00110000', '\\N{DIGIT ZERO}', '\\N', '\\N{', '\\q', '\\', 'a\\', '\\d+', '\\D+', '\\w+', '\\W+', '\\s+', '\\S+', '\\A', '\\Z',
    '\\z', '\\p{Han}', '\\Q.\\E', '\\cA', '^', '$', '^*', '$+', '*', '+', '?', '{1}', 'a**', 'a*+', 'a++', 'a?+', 'a*?', 'a{2}+', 'a{2}*', 'a{2}{3}', '(?:)*',
    '(?:^)*', '(a*)*', '(a|)+', '|', 'a||b', '(|a)', '.*', '.+\\.test$', '^[a-z0-9-]+\\.test$', '^(?:[^.]+\\.)*example\\.com$', '^node[0-9]{1,3}\\.org$',
    '^(?!bad)', 'a{1001}', 'a{4294967295}', 'a{4294967294}', 'a{99999999999999999999}', '(?i)^EXAMPLE\\.COM$', '\\.', '\\-', '\\_', '\\ ', '\\\n', 'a\nb',
    'a#b', '(?x) a # unterminated (', '例子', '[例-子]', '(?P<例>a)', '(?P<a-b>x)', 'é', '\\u00e9', '[^\\n]', '.', '(?s:.)', '(?m:^a)',
    '(a)(b)(c)(d)(e)(f)(g)(h)(i)(j)\\10', '(a)\\10', '\\1' + '(a)', '(?:(?:a))', '((a))', '(?:a|b|c)+', 'x*y+z?', '[a-z]{2,4}', '[0-9a-fA-F:.]+',
]

WORDS = ['\u8fc7\u671f', '\u5269\u4f59|\u6d41\u91cf', 'HK', 'US|JP', 'test', '\U0001f1ed\U0001f1f0', 'x']
LOOK = []
for w in WORDS:
    LOOK += [f'^(?!.*({w})).*$', f'^(?=.*({w}))', f'(?<!{w.split("|")[0]})\\d+', f'(?<=\\U0001f1ed\\U0001f1f0 )\\w+', f'({w})\\1', f'(?>{w})x', f'(?:{w})*+x', f'(?:{w})++$',
             f'(?i)^(?!.*({w}))', f'(?s)(?!{w}).', f'((?:{w}))(?(1)\\d|x)', f'(?P<n>{w})(?P=n)?', f'^(?!(?:{w})$)[^\\n]*', f'{w}(?=\\d)', f'{w}(?!\\d)',
             f'(?<=({w.split("|")[0]}))x', f'(?=({w}))\\1', f'^.{{0,40}}(?<!{w.split("|")[0]})$', f'(?m)^(?!{w})']
LOOK += [
    '^(?!.*(\u8fc7\u671f|\u5269\u4f59|\u6d41\u91cf)).*$', '^(?!.*\u8fc7\u671f)(?!.*\u6d41\u91cf).*', '(?<![A-Z])US(?![A-Z])', '(?<=-)\\d{2}$',
    '(?a)\\d+', 'a$', 'a$\\n', '(?m)a$', '(?m)^a$', '$', '^$', '$|a', '(?:a$)*', 'a$b', '(a$)', '(a$)|b', 'a\\Z', 'a\\Z|b', '(?:a|b$)c', 'b$(?=\\n)',
    'a{1001}', 'a{2000,}', '(?:ab){1500}', 'a{4294967294}', 'x{0,1001}y', '(?:a{500}){3}', '(a)|b(?(1)c)', '(a)?(?(1)b|c)', '(a)?(?(1)b)', '(?P<g>a)?(?(g)b|c)', '(?(1)a)', '(a)(?(1)(?(1)b|c)|d)',
    '(?>a+)b', '(?>a|ab)c', 'a++b', 'a*+a', 'a?+a', '(?:ab)++c', 'a{2,3}+a', '(\\w)\\1', '(a)(b)\\2\\1', '(?i)(a)\\1', '(?:(a)|b)\\1', '(a*)\\1', '(?P<q>[\"\'])x(?P=q)', '(.)(?=.*\\1)', '^(.+)\\1$',
    '(?<=a|b)c', '(?<=a+)c', '(?<=ab|c)d', '(?<!ab|c)d', '(?<=(a))b', '(?<=(?:ab){2})c', '(?<=a{2})b', '(?<=a{1,2})b', '(?<=a*)b', '(?<=(a)\\1)b', '(?<=a)(?<=b)',
    '(?=a)*', '(?!a)+', '(?=a){2}', '(?:(?=a)a)+', '(?i:(?!A)a)', '(?-i:A)(?i:(?=a))', '(?=\\n)', '(?!\\Z)', '(?<=^)a', '(?<=\\A)a', '(?=.$)',
    '(?x)(?!a) # c', '(?x)(?<=a) b', '(?s:.)(?<!\\n)', '[^\\W\\d](?!\\d)', '(?![\\W])\\S', '[\\w-](?<![-])', '(?:(?!a).)+', '(?:(?!ab)\\w)+$',
]
REGEXES += LOOK
TEXTS = ['', 'a', 'abc', 'aab', 'aaa', 'b', 'abcabc', 'example.com', 'EXAMPLE.COM', 'a.example.com', 'node12.org', 'text.org', 'x-y.test', '1.2.3.4', '_x.test',
         'a b', '例子', 'case.test', 'bad.test', 'good.test', 'Aé', 'aA', 'ab\ncd', 'AB']


def regex_cases():
    for pattern in REGEXES:
        for ignore in (False, True):
            try:
                compiled = re.compile(pattern, re.IGNORECASE if ignore else 0)
                error = None
            except re.error as exc:
                compiled, error = None, str(exc)
            except RecursionError:
                compiled, error = None, 'RecursionError'
            except OverflowError as exc:
                compiled, error = None, 'OverflowError: ' + str(exc)
            searches = None if compiled is None else [bool(compiled.search(text)) for text in TEXTS]
            emit(kind='regex', pattern=pattern, ignore=ignore, error=error, texts=TEXTS, searches=searches)
    for depth in (10, 400, 480, 600, 2000):
        pattern = '(' * depth + 'x' + ')' * depth
        try:
            re.compile(pattern)
            error = None
        except RecursionError:
            error = 'RecursionError'
        emit(kind='regex', pattern=pattern, ignore=False, error=error, texts=[], searches=None)


# --------------------------------------------------------------------------
# rule_value_error
# --------------------------------------------------------------------------

KINDS = sorted(rule_syntax.RULE_TYPES) + ['DOMAIN-SHUFFIX', 'FUTURE-TYPE', '', 'domain', 'Domain-Suffix', 'IP-CIDR7', 'PROCESS-NAME-REGEXP', 'GEOIPP', 'SRC-IP', None, 5, ['x'], 'PROCEß-NAME']
VALUES = (
    ['', ' ', 'example.com', 'api.example.test', 'EXAMPLE.COM', 'bad..test', '-bad.test', 'bad-.test', 'a_b.test', '999.999.999.999', '192.0.2.1', '192.0.2.0/24',
     '2001:db8::/32', '2001:db8::1', '192.0.2.0/33', '192.0.2.0/024', '192.0.2.0/255.255.255.0', 'fe80::1%eth0/64', '999.2.3.4/8', 'x' * 64 + '.com', 'x.' * 130 + 'com',
     '例子.测试', 'bücher.de', 'xn--fsqu00a.xn--0zwm56d', 'a.1', 'a.123', '1.a', 'a,b', 'a\nb', 'a\rb', '80', '80/443', '80,443', '8000-9000', '0', '65535',
     '65536', '-1', '443-80', '80/abc', '0/80/443/8000-9000/65535', '/'.join(['80'] * 28), '/'.join(['80'] * 29), '0000080', '00065535', '000065536', '9' * 5000, '63', '64', '*',
     '0/12-18/63', '4294967295', '4294967296', '0-4294967295', 'abc', '65535', 'tcp', 'UDP', 'quic', 'cn@ads', 'CN', 'app*', '/usr/bin/app', '*.example.?om',
     '((DOMAIN,example.test),(NETWORK,tcp))', '(DOMAIN,example.test)', '((DOMAIN,example.test)', '((DOMAIN-SHUFFIX,example.test))', '((MATCH,))', '((SUB-RULE,x))', '()', '(())',
     '((DOMAIN,one.test),(DOMAIN,two.test))', '((AND,((DOMAIN,a.test),(OR,((NETWORK,tcp),(DST-PORT,80/443))))))', '((NOT,((DOMAIN,a.test))))', '((NETWORK,quic))', '(' * 40 + '(DOMAIN,a.test)' + ')' * 40,
     '((DOMAIN-REGEX,^node{1,3}$),(DOMAIN,example.test))', '((DOMAIN-REGEX,[abc))', '((DST-PORT,80,443))', '(DOMAIN,a)(NETWORK,tcp)']
    + [pattern for pattern in REGEXES if '\n' not in pattern]
    + ['(?x)^example # (', '(?x)^example # [', '(?x)^example # \\', '(?#literal comment [)^example', '(?ix:^ example )', '(?-i:x)(?x)^example # [',
       '\\Q[(literal)\\E', '\\Q[(literal', '^node[[:alpha:](]$', '^node[]a,b]$', '^node[^]a,b]$', '^node{foo$', '^node{1$', '(?<host>\\p{Han}+)\\.test$', '/apps/(a,b)',
       '\\cA[', '\\c', '[\\cA]', 'a\\Q(\\E)', '\\Q\\E(', '[[:alpha:]', '[[:^alpha:]]', '[[:alpha:]]]', '(' * 2000 + 'x' + ')' * 2000, '(' * 300 + 'x' + ')' * 300]
)


def syntax_cases():
    emitted = set()
    pairs = [(R.choice(KINDS), R.choice(VALUES)) for _ in range(6000)]
    pairs += [('DOMAIN-REGEX', pattern) for pattern in VALUES if isinstance(pattern, str)]
    pairs += [(kind, value) for kind in ('AND', 'OR', 'NOT') for value in VALUES if isinstance(value, str)]
    pairs += [(kind, value) for kind in ('DOMAIN', 'DOMAIN-SUFFIX', 'IP-CIDR', 'IP-CIDR6', 'DST-PORT', 'DSCP', 'UID') for value in VALUES if isinstance(value, str)]
    pairs += [(kind, value) for kind in ('DOMAIN', 'DOMAIN-SUFFIX') for value in IDNAS]
    pairs += [('DOMAIN', 5), ('DOMAIN', None), ('DST-PORT', ['80']), ('MATCH', None), ('NETWORK', 'TCP'), ('NETWORK', 'Tcp ')]
    for kind, value in pairs:
        try:
            error = rule_syntax.rule_value_error(kind, value)
        except Exception as exc:
            error = '__exception__ ' + type(exc).__name__
        key = (repr(kind), repr(value))
        if key in emitted:
            continue
        emitted.add(key)
        emit(kind='syntax', rule_kind=kind, value=value, error=error)
    for text in ['DOMAIN,example.test,DIRECT,no-resolve', 'DOMAIN-REGEX,^a{1,3}$,DIRECT', 'PROCESS-PATH-REGEX,/apps/(a,b)', 'and,((a,b))', ' domain , x ', '', ',', 'MATCH', 'DOMAIN,a,,b',
                 'NOT,((DOMAIN,a))', 'sub-rule,(x,y),z', 'DOMAIN-REGEX', 'PROCEß-NAME,x']:
        kind, payload, params = rule_syntax.split_rule_payload(text)
        emit(kind='split', text=text, result=[kind, payload, params])
    for value in ['192.0.2.1', '2001:db8::1', 'x', '', '192.0.2.0/24', '1.2.3.4/', '::1/', 'fe80::1%eth0', 'fe80::1%eth0/64', '1.2.3.4/08', '::/00', '1.2.3.4/0']:
        emit(kind='cidr', value=value, error=rule_syntax.cidr_error(value))


# --------------------------------------------------------------------------
# rule_utils names
# --------------------------------------------------------------------------

def cache_cases():
    names = ['', 'A', 'Rule', 'a/b', 'a\\b', 'a:b*c?d"e<f>g|h', '. leading', 'trailing. ', '  ', '...', 'x' * 250, '例子', '例' * 250, 'café', 'a b', 'a.b.c', None, 5]
    for name in names:
        try:
            result = rule_utils.sanitize_rule_name(name)
        except Exception as exc:
            result = '__exception__ ' + type(exc).__name__
        emit(kind='cache', what='sanitize', name=name, result=result)
    ids = [None, '', 'id1', 'lib1', 0, 5, '例']
    for _ in range(600):
        rule = {}
        for key, choices in (('library_rule_id', ids), ('id', ids), ('name', ['', 'N', '例', None, 'a/b']),
                             ('source_type', ['url', 'content', None, 'other']), ('url', ['', 'https://x.test/a.yaml', 'https://x.test/例', None, 5]),
                             ('behavior', ['domain', 'ipcidr', 'classical', None, 3]), ('format', ['yaml', 'text', 'mrs', '', None])):
            if R.random() < 0.7:
                rule[key] = R.choice(choices)
        try:
            result = rule_utils.rule_cache_filename(rule)
        except Exception as exc:
            result = '__exception__ ' + type(exc).__name__
        emit(kind='cache', what='filename', rule=rule, result=result)


# --------------------------------------------------------------------------
# group_candidates
# --------------------------------------------------------------------------

FILTER_PATTERNS = ['^(?!.*(\u8fc7\u671f|\u5269\u4f59|\u6d41\u91cf)).*$', '^(?!.*\u8fc7\u671f)', '(?<![A-Z])US', '\\bHK\\b', '(?i)^(?!.*us)', '(HK).*\\1', '(?>US)-', '\\d++$',
                   '(?<=-)\\d+$', '(?<=a|bc)x', None, '', ' ', 'US', '^US', 'expired', ' (?i)expired ', '(?i)^us-expired(?:-new)?$', '^US-expired(?:-new)?$', 'expired|US`old', 'expired|US\\`old', 'a``b',
                   '\\\\`', '\\\\\\`', '[', '(', '(?=x)', '(?!x)', '(?<=x)y', '(?P<n>a)(?P=n)', '\\d+', '\\w+', '\\bUS\\b', '^HK', '^(?!US)', '(?i)(?x)a', '(?s)(?i)US', '(?i)', '(?)',
                   '(?aiLmsux)x', '(?ai)x', '(?i)(?s)x', 5, True, ['x'], '.*', '香港|HK', '^[一-鿿]+', '(?i)HK', '[0-9]+x', 'a{1001}', '\\1', '(a)\\1']
NODE_NAMES = ['\u9999\u6e2f 01 \u8fc7\u671f', '\u9999\u6e2f 02', '\U0001f1ed\U0001f1f0 HK-03', '\u5269\u4f59\u6d41\u91cf 100G', 'US-us', '\u65e5\u672c|JP 05', 'US-new', 'US-expired', 'HK-new', 'HK-expired', 'US`old', 'EU 01', '香港-01', 'sg', '', 'US-expired-new', 'hk-1', '日本 02', 'Aé', 'x_1', '123']


def filter_cases():
    for pattern in FILTER_PATTERNS:
        if isinstance(pattern, str):
            emit(kind='filters', what='backticks', pattern=pattern, result=gc._literal_backticks(pattern))
            emit(kind='filters', what='embedded', pattern=pattern, result=gc._embedded(pattern))
            for include in ('', '^US', '(?i)us', '^US-`^HK-'):
                emit(kind='filters', what='exclusion', include=include, exclude=pattern, result=gc.exclusion_filter(include, pattern))
    emit(kind='filters', what='names', names=NODE_NAMES)
    names = [{'name': name, 'extra': i} for i, name in enumerate(NODE_NAMES)]
    for _ in range(500):
        include, exclude = R.choice(FILTER_PATTERNS), R.choice(FILTER_PATTERNS)
        try:
            kept = [item['extra'] for item in gc.filter_candidates(names, include, exclude)]
            result = {'kept': kept}
        except gc.CandidateValidationError as exc:
            result = {'error': str(exc)}
        except Exception as exc:
            result = {'exception': type(exc).__name__}
        emit(kind='filters', what='filter', include=include, exclude=exclude, result=result)
    for _ in range(400):
        group = {}
        for field in gc.FILTER_FIELDS:
            if R.random() < 0.5:
                group[field] = R.choice(FILTER_PATTERNS)
        original = copy.deepcopy(group) if R.random() < 0.4 else None
        if original is not None and R.random() < 0.5:
            original[R.choice(gc.FILTER_FIELDS)] = R.choice(FILTER_PATTERNS)
        try:
            gc.validate_filters(group, original)
            error = None
        except gc.CandidateValidationError as exc:
            error = str(exc)
        except Exception as exc:
            error = '__exception__ ' + type(exc).__name__
        emit(kind='filters', what='validate', group=group, original=original, result=error)


def catalog():
    nodes = [{'id': f'n{i}', 'name': f'Node {i}', **({'enabled': False} if i == 4 else {})} for i in range(1, 5)]
    subs = [{'id': f's{i}', 'name': f'Sub{i}', **({'enabled': False} if i == 3 else {})} for i in range(1, 4)]
    aggs = [{'id': 'a1', 'name': 'Agg1', 'subscriptions': ['s1']}, {'id': 'a2', 'name': 'Agg2', 'subscriptions': ['s2', 's3']},
            {'id': 'a3', 'name': 'Agg3', 'subscriptions': [], 'enabled': False}]
    groups = [{'id': 'g1', 'name': 'G1', 'type': 'select', 'subscriptions': ['s1'], 'manual_nodes': ['n1']},
              {'id': 'g2', 'name': 'G2', 'type': 'select', 'aggregations': ['a2']},
              {'id': 'g3', 'name': 'G3', 'type': 'chain'},
              {'id': 'g4', 'name': 'G4', 'type': 'select', 'follow_group': 'g1'},
              {'id': 'g5', 'name': 'G5', 'type': 'select', 'enabled': False, 'subscriptions': ['s1']}]
    return {'nodes': nodes, 'subscriptions': subs, 'subscription_aggregations': aggs}, {'proxy_groups': groups}


def sample(pool, low=0, high=3):
    return R.sample(pool, R.randint(low, min(high, len(pool))))


def random_group():
    group = {'id': 'new', 'name': 'New', 'type': R.choice(['select', 'url-test', 'select', 'chain'] if R.random() < 0.1 else ['select', 'url-test'])}
    if R.random() < 0.7:
        group['manual_nodes'] = sample(['n1', 'n2', 'n3', 'n4', 'DIRECT', 'REJECT', 'bogus'])
    if R.random() < 0.6:
        group['subscriptions'] = sample(['s1', 's2', 's3', 'bogus'])
    if R.random() < 0.5:
        group['aggregations'] = sample(['a1', 'a2', 'a3', 'bogus'])
    if R.random() < 0.4:
        group['include_groups'] = sample(['g1', 'g2', 'g3', 'g4', 'g5', 'bogus'])
    if R.random() < 0.15:
        group['source'] = R.choice(['node', 'strategy', 'other'])
        group['proxies'] = sample(['n1', 'g1', 'DIRECT', 'bogus'])
    if R.random() < 0.3:
        order = [{'type': 'node', 'id': i} for i in group.get('manual_nodes', [])] + [{'type': 'strategy', 'id': i} for i in group.get('include_groups', [])]
        order += [{'type': 'aggregation', 'id': i} for i in group.get('aggregations', [])]
        R.shuffle(order)
        if R.random() < 0.3 and order:
            order.pop(R.randrange(len(order)))
        if R.random() < 0.2:
            order.append(R.choice([{'type': 'node', 'id': 'n1'}, {'type': 'x', 'id': 'y'}, {'type': 'node'}, 'str', {'type': 'node', 'id': 'n1', 'extra': 1}]))
        group['proxies_order'] = order
    if R.random() < 0.2:
        group['proxy_order'] = 'strategies_first'
    if R.random() < 0.2:
        group['follow_group'] = R.choice(['g1', 'g2', 'g3', 'g4', 'g5', 'bogus', 5])
    for field in gc.FILTER_FIELDS:
        if R.random() < 0.25:
            group[field] = R.choice(['US', '^HK', 'expired', ' x ', '[', None, 5, ''])
    for field in ('manual_nodes', 'subscriptions', 'aggregations', 'include_groups', 'proxies', 'proxies_order'):
        if R.random() < 0.03:
            group[field] = R.choice(['text', 5, {'a': 1}, None])
    return group


def group_cases():
    for _ in range(1500):
        shared, profile = catalog()
        group = random_group()
        original = None
        if R.random() < 0.4:
            original = copy.deepcopy(group)
            for _ in range(R.randint(0, 2)):
                original[R.choice(gc.SOURCE_FIELDS + ('regex',))] = R.choice([[], ['n1'], 'x', None])
        record = {'kind': 'group', 'group': group, 'original': original, 'shared': shared, 'profile': profile}
        try:
            members = gc.managed_members(group)
            record['members'] = [list(item) for item in members['members']]
            record['selection'] = {key: members[key] for key in ('nodes', 'subscriptions', 'aggregations', 'groups', 'order')}
        except Exception as exc:
            record['members'] = '__exception__ ' + type(exc).__name__
        full = {**shared, **profile}
        try:
            record['render'] = gc.render_members(group, full)
        except Exception as exc:
            record['render'] = '__exception__ ' + type(exc).__name__
        try:
            record['providers'] = [[item['id'], include, exclude] for item, include, exclude in gc.managed_providers(group, full)]
        except Exception as exc:
            record['providers'] = '__exception__ ' + type(exc).__name__
        try:
            gc.validate_candidates(copy.deepcopy(group), profile, shared, original=copy.deepcopy(original))
            record['validate'] = None
        except gc.CandidateValidationError as exc:
            record['validate'] = str(exc)
        except Exception as exc:
            record['validate'] = '__exception__ ' + type(exc).__name__
        emit(**record)
    for _ in range(300):
        original = random_group()
        patch = {key: value for key, value in random_group().items() if R.random() < 0.4}
        if R.random() < 0.7:
            patch['delete_fields'] = R.choice([['url'], ['interval', 'timeout'], ['url', 'url'], ['bogus'], 'url', [5], [], ['lazy', 'tolerance']])
            if R.random() < 0.3:
                patch['url'] = 'x'
        try:
            result = {'merged': gc.merge_group_update(copy.deepcopy(original), copy.deepcopy(patch))}
        except gc.CandidateValidationError as exc:
            result = {'error': str(exc)}
        emit(kind='group', what='merge', original=original, patch=patch, result=result)


def lower_cases():
    for _ in range(600):
        shared, profile = catalog()
        groups = [random_group() for _ in range(R.randint(1, 3))]
        for index, group in enumerate(groups):
            group['id'], group['name'] = f'x{index}', f'X{index}'
            group['type'] = 'select'
            group.pop('proxies_order', None)
            group.pop('proxies', None)
            group.pop('source', None)
            for field in ('manual_nodes', 'subscriptions', 'aggregations', 'include_groups'):
                if not isinstance(group.get(field, []), list):
                    group.pop(field)
            for field in gc.FILTER_FIELDS:
                if not isinstance(group.get(field), (str, type(None))):
                    group.pop(field)
        config = {**shared, 'proxy_groups': profile['proxy_groups'] + groups}
        main = {'proxies': [{'name': item['name']} for item in shared['nodes']], 'proxy-providers': {}, 'proxy-groups': []}
        for item in shared['subscriptions'] + shared['subscription_aggregations']:
            main['proxy-providers'][item['name']] = {'type': 'http', 'url': f"https://p.test/{item['id']}", 'path': f"./providers/{item['id']}.yaml",
                                                      'health-check': {'enable': True, 'url': 'https://t.test'}}
        for group in config['proxy_groups']:
            try:
                rendered = gc.render_members(group, config)
            except Exception:
                continue
            entry = {'name': group['name'], 'type': 'select', **rendered}
            if R.random() < 0.2:
                entry['filter'] = '^old'
            main['proxy-groups'].append(entry)
        result = copy.deepcopy(main)
        try:
            gc.lower_group_provider_filters(copy.deepcopy(config), result)
            outcome = result
        except Exception as exc:
            outcome = '__exception__ ' + type(exc).__name__
        emit(kind='lower', config=config, main=main, result=outcome)


for index in range(3000):
    match_case(index)
line_cases()
direct_cases()
ip_cases()
idna_cases()
regex_cases()
syntax_cases()
cache_cases()
filter_cases()
group_cases()
lower_cases()
