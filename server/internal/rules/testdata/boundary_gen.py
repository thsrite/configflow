#!/usr/bin/env python3
"""Boundary corpus for internal/rules: realistic rule lines, filters, domains,
IPs and node names, and what the production Python modules make of them.

Run from the repository root on the test server (synthetic values only):

    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/rules/testdata/boundary_gen.py \
        server/internal/rules/testdata/boundary.jsonl.gz

One JSON object per line. Kinds:

Go-native word boundaries are exercised in regex_review_test.go. This corpus
keeps Python expectations for filters without those boundary constructs.

    search   re.search(pattern, name[, IGNORECASE]) for node filters, DOMAIN-REGEX
             values and the patterns other packages compile (compiled? + hits)
    sub      pattern.sub(lambda m: '<' + m.group(0).upper() + '>', text)
    filter   group_candidates.filter_candidates(nodes, include, exclude)
    rve      rule_syntax.rule_value_error(kind, value) (Chinese message or null)
    line     rule_matcher.parse_rule_line(line)
    query    rule_matcher.match_query(query, type, value)
    domain   domain_value_error + str.encode('idna') of realistic host names
"""
import gzip
import json
import re
import sys

from backend.utils import group_candidates as gc
from backend.utils import rule_matcher, rule_syntax

assert sys.version_info[:2] == (3, 11), sys.version
OUT = gzip.open(sys.argv[1], 'wt', encoding='utf-8', compresslevel=9)


def emit(**record):
    OUT.write(json.dumps(record, ensure_ascii=False) + '\n')


NAMES = [
    '🇭🇰 香港 01', '🇭🇰 香港 02 | IPLC', '🇯🇵 日本 东京 01', '🇯🇵 Japan Tokyo 02', '🇺🇸 美国 洛杉矶',
    '🇸🇬 新加坡 01 [0.5x]', '🇹🇼 台湾 01', '🇰🇷 韩国 首尔', 'HK-01', 'hk 02', 'HKG|BGP|x1.5', 'US-Seattle-03',
    'Singapore 04', 'sg5', 'JP_Osaka_06', '剩余流量：120.5 GB', '套餐到期：2026-12-31', '过期时间 2026-12-31',
    '官网 https://example.test', '距离下次重置剩余：12 天', '香港ＩＰＬＣ０１', 'ＨＫ　０２', '日本０３（中转）',
    '🇩🇪 德国 法兰克福', 'Germany 01 ✈', 'UK London', '🇬🇧 英国 伦敦 02', 'Россия 01', 'Türkiye 01',
    'Ελλάδα 01', 'العربية 01', '香港 01 - 游戏', '香港 Netflix 解锁', 'HK 01 x2', 'HK 01 ×0.1', 'ChatGPT 美国',
    'DIRECT', 'REJECT', 'Auto 自动选择', '♻️ 自动选择', '🚀 节点选择', 'node.example.test:443', '',
    '新加坡⁰¹', '台湾 ① 号', 'HK01-备用', '香港 01 (v6)', 'IPv6 香港', 'Emoji 😀 only', 'tab\tname',
    'Hong Kong 2x', '香港 ０１x', '日本东京 01 x2', '🇸🇬 新加坡 03 | IPLC', 'a\u0301', '\u0301a', 'a\u203f',
]

PATTERNS = [
    '香港|HK|Hong Kong', '(?i)hk|hong ?kong|香港', '(?i)^(?!.*(过期|剩余|到期|官网|重置)).*$', '^(?!.*(过期|剩余|流量)).*$',
    '日本|JP|Japan|东京|大阪', '(?i)(sg|singapore|新加坡)', r'\d+', r'0\d', r'\d{2}', r'[0-9]{2}', r'\w+\s\d+', r'^\W',
    '美国|US|United States', r'(?i)x(\d+(\.\d+)?)', r'[一-龥]+', r'^[一-鿿]',
    r'(?P<region>香港|日本)\s*(?P<num>\d+)', r'(香港|日本)\s*\1', r'(?<=香港 )\d+', r'(?<!IPLC)\s01$', r'\|',
    r'\[0\.5x\]', r'(?i)netflix|nf|奈飞', r'(?i)gpt|openai', '^🇭🇰', '🇯🇵|🇺🇸', r'[ＡＢＣＨＫ]', r'(?i)ｈｋ', '０',
    r'\s{2,}', r'^.{0,5}$', r'.*', '', r'(?x) 香港 | 日本  # comment', r'(?s).+', r'(?m)^HK', r'\Ahk', r'01\Z', r'01$',
    r'(?i)[a-z]+', r'[^\x00-\x7f]', r'\p{Han}', r'[\d-z]', r'(?<=\d+)x', r'a{2,1}', r'(', r'[a-', r'**', r'(?P<n>a)(?P=n)',
    r'(?>HK)\d', r'HK\d++', r'\x48K', r'香港', r'\N{DIGIT ONE}', r'[[:digit:]]', r'(?a)\w+', r'(?a)\d',
    r'(?u)\d', r'\D+', r'\S+\s\S+', r'(?i)TÜRKIYE', r'(?i)россия', r'ελλάδα', r'(?:x|×)\d', r'[①-⑩]', r'⁰¹',
    '^(?!.*(过期|剩余|官网)).*$', r'\d+x', r'(?P<name>香港|日本)\s*(?P<num>\d+)',
    '香港|日本|新加坡|台湾',
]

for pattern in PATTERNS:
    for flags in (0, re.IGNORECASE):
        try:
            compiled = re.compile(pattern, flags)
        except re.error as error:
            emit(kind='search', pattern=pattern, ignore=bool(flags), error=str(error))
            continue
        except Exception as error:  # noqa: BLE001
            emit(kind='search', pattern=pattern, ignore=bool(flags), error=type(error).__name__)
            continue
        emit(kind='search', pattern=pattern, ignore=bool(flags), error=None,
             hits=[bool(compiled.search(name)) for name in NAMES])
        if not flags:
            emit(kind='sub', pattern=pattern, outputs=[
                compiled.sub(lambda m: '<' + m.group(0).upper() + '>', name) for name in NAMES])

INCLUDES = [None, '', '香港|HK', '(?i)hk', r'\d', r'\d+x', '香港|日本|新加坡|台湾',
            '(?i)^(?!.*(过期|剩余|官网)).*$', '(', 5]
EXCLUDES = [None, '', '过期|剩余|到期|官网', r'(?i)x\d', '[', '0\\d']
nodes = [{'name': name} for name in NAMES]
for include in INCLUDES:
    for exclude in EXCLUDES:
        try:
            kept = [NAMES.index(item['name']) for item in gc.filter_candidates(nodes, include, exclude)]
            emit(kind='filter', include=include, exclude=exclude, kept=kept)
        except gc.CandidateValidationError as error:
            emit(kind='filter', include=include, exclude=exclude, error=str(error))

VALUES = {
    'DOMAIN': ['example.com', 'www.Example.COM', '例子.测试', 'bücher.example', 'ｅｘａｍｐｌｅ．ｃｏｍ', 'straße.de',
               'xn--fiqs8s.example', 'a_b.example', '-bad.example', 'bad-.example', 'exa mple.com', '1.2.3.4',
               'example.123', '中国.cn', 'πού.gr', 'пример.рф', '😀.example', 'a..b', '.example.com', 'example.com.',
               'x' * 64 + '.com', 'b' * 250 + '.com', 'mail.google.com', 'localhost'],
    'DOMAIN-SUFFIX': ['google.com', 'apple.com.cn', '香港.example', 'co.uk', 'example.', ''],
    'DOMAIN-KEYWORD': ['google', 'netflix', '谷歌', 'a,b'],
    'DOMAIN-REGEX': [r'^(.*\.)?google\.com$', r'.*\.cn$', r'^ad[sx]?\d*\.', r'(?i)^api\.', r'[a-z', r'(abc',
                     r'abc)', r'a{3,2}', r'*.example', r'a**', r'\.(com|net)$', r'^(?!www\.).*\.example$', r'\p{Han}',
                     r'(?<=a)b', r'\d+\.example', r'[z-a]', r'(?x) a # c', r'\Qa.b\E', '^.*$'],
    'IP-CIDR': ['192.168.0.0/16', '10.0.0.1/8', '192.168.1.1', '192.168.1.0/33', '192.168.1.0/024',
                '2001:db8::/32', '::/0', '0.0.0.0/0', '192.168.1.0/255.255.255.0', 'abc', '172.16.0.0/12'],
    'IP-CIDR6': ['2001:db8::/32', 'fe80::/10', '2001:db8::1', '2001:db8::/129', '::ffff:1.2.3.4/128'],
    'DST-PORT': ['443', '80/443', '8000-9000', '0-65535', '65536', '443-80', 'abc', '/'.join(['1'] * 29)],
    'SRC-PORT': ['1024-65535'], 'IN-PORT': ['7890'], 'DSCP': ['46', '64', '*', '0-63'],
    'UID': ['1000', '4294967295', '4294967296'], 'IP-ASN': ['13335', 'AS13335'],
    'NETWORK': ['tcp', 'UDP', 'quic'], 'GEOIP': ['CN', 'private'], 'GEOSITE': ['cn', 'geolocation-!cn'],
    'PROCESS-NAME': ['Telegram.exe', 'a,b'], 'PROCESS-NAME-REGEX': [r'(?i)telegram.*', r'[a'],
    'AND': ['((DOMAIN,example.com),(NETWORK,tcp))', '((DOMAIN,example.com)', '(DOMAIN,example.com)',
            '((DOMAIN-SUFFIX,google.com),(DST-PORT,443))', '((MATCH,))'],
    'OR': ['((DOMAIN-KEYWORD,ads),(DOMAIN-REGEX,^ad\\d))'], 'NOT': ['((GEOIP,CN))', '((A,1),(B,2))'],
    'MATCH': ['', 'x'], 'RULE-SET': ['Netflix'], 'SUB-RULE': ['(NETWORK,tcp)'],
    'DOMAIN-SUFIX': ['example.com'], 'IPCIDR': ['1.2.3.4/32'], 'domain': ['example.com'], 'FOO': ['x'],
}
for kind, values in VALUES.items():
    for value in values:
        emit(kind='rve', rule_kind=kind, value=value, error=rule_syntax.rule_value_error(kind, value))

LINES = [
    'DOMAIN-SUFFIX,google.com', 'DOMAIN,www.example.com,DIRECT', 'IP-CIDR,192.168.0.0/16,no-resolve',
    'IP-CIDR6,2001:db8::/32,no-resolve', "  - 'DOMAIN-SUFFIX,apple.com'", '- +.example.com', '+.example.com',
    '*.example.com', '.example.com', 'example.com', '192.168.1.1', '10.0.0.0/8', 'domain:google.com',
    'full:www.google.com', 'keyword:ads', 'regexp:^ad\\d+\\.', 'ip:1.2.3.4', '# comment', '// comment', '! adblock',
    'payload:', '', '   ', 'DOMAIN-KEYWORD,谷歌 # 注释', '"例子.测试"', 'domain-suffix,Example.COM', '2001:db8::1',
    'DOMAIN-SUFFIX,example.com,NO-RESOLVE', 'GEOIP,CN', 'fe80::1%eth0',
]
for line in LINES:
    emit(kind='line', line=line, result=rule_matcher.parse_rule_line(line))

QUERIES = ['www.google.com', 'google.com', 'GOOGLE.COM', 'mail.google.com.', '192.168.1.10', '2001:db8::1', '1.2.3.4',
           'example.cn', 'ads1.example.com', 'a_b.example.com', '例子.测试', 'not a domain']
RULES = [('DOMAIN', 'google.com'), ('DOMAIN-SUFFIX', 'google.com'), ('DOMAIN-KEYWORD', 'ads'),
         ('DOMAIN-REGEX', r'^ad[sx]?\d*\.'), ('DOMAIN-REGEX', r'(?i)GOOGLE'), ('REGEX', r'\.cn$'),
         ('SUBDOMAIN-ONLY', 'google.com'), ('DIRECT-SUBDOMAIN', 'google.com'), ('IP-CIDR', '192.168.0.0/16'),
         ('IP-CIDR6', '2001:db8::/32'), ('IP', '1.2.3.4'), ('IP-CIDR', '1.2.3.4/255.255.255.0')]
for query in QUERIES:
    for rule_type, value in RULES:
        emit(kind='query', query=query, rule_type=rule_type, value=value,
             result=rule_matcher.match_query(query, rule_type, value))

HOSTS = ['example.com', 'Example.COM', '例子.测试', 'bücher.example', 'ｅｘａｍｐｌｅ．ｃｏｍ', 'straße.de',
         '中国.cn', 'пример.рф', 'πού.gr', '😀.example', 'xn--fiqs8s.cn', 'example.com.', '香港。example',
         'München.de', 'café.fr', 'ÀÉ.test', 'a' * 63 + '.com', 'a' * 64 + '.com', '日本語.jp', '한국.kr']
for host in HOSTS:
    try:
        encoded = host.encode('idna').decode('ascii')
    except UnicodeError:
        encoded = None
    emit(kind='domain', host=host, error=rule_syntax.domain_value_error(host), idna=encoded)

OUT.close()
