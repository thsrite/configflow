"""Public zone/RDATA diagnostics; includes/generate are never executed here."""
from collections import Counter

import yaml

from harness import Pair, json_body
from sources import Source
from cases.pull_routes import SourceStable
from cases.oxidns_plugin_helpers import check_issue
from cases.subscriptions import require

ROUTE = 'POST /api/oxidns/validate'
UNOBSERVABLE = {
    'H664': 'validate_plugin_format的name由固定schema目录选择；HTTP草稿不能定义schema格式名，未知插件只返回warning，不能驱动Unsupported plugin validation format分支。',
}
INVALID_RECORD = 'DNS 记录的名称、TTL、类型或 RDATA 格式无效'


def zone_vectors():
    rows = [
        (640, '{qname} IN TXT "{qname}"', '每条响应记录中的 {qname} 和 {qclass} 各只能出现一次'),
        (640, '{qname} {qclass} TXT "{qclass}"', '每条响应记录中的 {qname} 和 {qclass} 各只能出现一次'),
        (641, '$ORIGIN relative', '$ORIGIN 必须能解析为绝对域名'),
        # The defensive >U32 checks follow dnspython's bounded TTL parser;
        # these public inputs are already rejected by that parser (H662).
        (642, '$TTL 4294967296', INVALID_RECORD),
        (643, '$INCLUDE', '$INCLUDE 需要文件路径和可选 origin'),
        (643, '$INCLUDE /synthetic.zone example.com. extra', '$INCLUDE 需要文件路径和可选 origin'),
        (644, '$INCLUDE relative.zone', '内联 zone 中的 $INCLUDE 必须使用绝对路径'),
        (645, '$GENERATE 1-2 host$.example. A', '$GENERATE 需要范围、名称、类型和 RDATA'),
        (646, '$GENERATE bad host$.example. A 192.0.2.$', '$GENERATE 范围或步长无效'),
        (646, '$GENERATE 2-1 host$.example. A 192.0.2.$', '$GENERATE 范围或步长无效'),
        (646, '$GENERATE 1-2/0 host$.example. A 192.0.2.$', '$GENERATE 范围或步长无效'),
        (646, '$GENERATE 0-9223372036854775808 host$.example. A 192.0.2.$', '$GENERATE 范围或步长无效'),
        (647, '$GENERATE 1-2 host$.example. A 192.0.2.$', '响应记录条目不能包含多条生成记录'),
        (648, '$UNKNOWN synthetic', '不支持的 zone 指令或指令参数不完整'),
        (648, '$ORIGIN', '不支持的 zone 指令或指令参数不完整'),
        (649, '"example.com." IN A 192.0.2.1', 'DNS 记录的域名不能放在引号内'),
        (650, '  IN A 192.0.2.1', '省略记录名称前必须先定义名称'),
        (651, 'relative IN A 192.0.2.1', '相对域名需要 $ORIGIN；否则名称必须以 . 结尾'),
        (652, 'example.com. 4294967296 IN A 192.0.2.1', INVALID_RECORD),
        (653, 'example.com. IN', 'DNS 记录缺少类型和 RDATA'),
        (654, 'example.com. IN CNAME {qname}', '{qname} 只能作为未加引号的记录名称'),
        (655, '{qname} IN TXT "{qclass}"', '{qclass} 只能作为类型前的未加引号的记录类别'),
        (656, 'example.com. IN A', 'DNS 记录缺少 RDATA'),
        (657, r'example.com. IN TYPE65280 \#', 'RFC3597 记录需要长度和十六进制数据'),
        (657, r'example.com. IN TYPE65280 \# bad AABB', 'RFC3597 记录需要长度和十六进制数据'),
        (658, r'example.com. IN TYPE65280 \# 65536 AABB', 'RFC3597 数据长度或十六进制格式无效'),
        (658, r'example.com. IN TYPE65280 \# 2 AA', 'RFC3597 数据长度或十六进制格式无效'),
        (658, r'example.com. IN TYPE65280 \# 2 AAGG', 'RFC3597 数据长度或十六进制格式无效'),
        (659, 'example.com. IN TYPE65280 AABB', '该记录类型在 OxiDNS 中需要 RFC3597 格式：\\# 长度 十六进制数据'),
        (660, 'example.com. IN MX 10', 'DNS 记录的 RDATA 参数不足'),
        (660, 'example.com. IN SRV 1 2 3', 'DNS 记录的 RDATA 参数不足'),
        (661, 'example.com. IN CNAME relative', 'RDATA 中的相对域名需要 $ORIGIN；否则必须以 . 结尾'),
        (662, 'example.com. IN A not-an-ip', INVALID_RECORD),
        (662, 'example.com. IN TXT "unterminated', INVALID_RECORD),
        (663, '', '每个响应记录条目必须恰好包含一条 DNS 记录'),
        # A comment-only string is tokenized as a relative owner by this
        # public wrapper; it does not reach the final empty-record guard.
        (651, '# comment only', '相对域名需要 $ORIGIN；否则名称必须以 . 结尾'),
        (663, 'example.com. IN A 192.0.2.1\nexample.org. IN A 192.0.2.2', '每个响应记录条目必须恰好包含一条 DNS 记录'),
    ]
    return rows


def constraints(binary, recorder):
    with Source(b'static zone validation must not connect') as source, Pair(binary, recorder, options={'isolated_cwd': True}) as pair:
        counters = Counter()
        for number, raw, text in zone_vectors():
            document = {'plugins': [{'tag': 'subject', 'type': 'response', 'args': {'answers': [raw]}}]}
            settings = {'config_mode': 'native', 'native_config': yaml.safe_dump(document, allow_unicode=True, sort_keys=False)}
            case = 'H' + str(number)
            pair.request(case, ROUTE, body=json_body({'settings': settings}), expect=200, writes=True,
                check=lambda r: check_issue(r, ['plugins', 0, 'args', 'answers', 0], text, 'error'),
                interaction=SourceStable(pair, source), variant='zone-record/' + str(counters[case]))
            counters[case] += 1
        for variant, raw in [('placeholders', '{qname} {qclass} A 192.0.2.1'),
                             ('maximum-TTL', 'example.com. 4294967295 IN A 192.0.2.1'),
                             ('RFC3597', r'example.com. IN TYPE65280 \# 2 AABB')]:
            document = {'plugins': [{'tag': 'subject', 'type': 'response', 'args': {'answers': [raw]}}]}
            pair.request('G15', ROUTE, body=json_body({'settings': {'config_mode': 'native', 'native_config': yaml.safe_dump(document)}}),
                expect=200, writes=True, interaction=SourceStable(pair, source), variant='zone-valid/' + variant,
                check=lambda r: require(r.json() == {'valid': True, 'issues': [], 'schema_version': '1.6.1', 'core_check_required': True},
                                         'valid maximum/placeholder/RFC3597 record is accepted without guessing target-core availability'))


def adguard(binary, recorder):
    vectors = [
        ('cosmetic', 'example.com##.advert', '此为非 DNS 过滤规则，OxiDNS 会跳过', 'warning'),
        ('hosts', '192.0.2.1 example.com', 'Hosts 格式规则会被 AdGuard Provider 跳过', 'warning'),
        ('URL', '/path/not-dns', '路径或 URL 过滤规则不用于 DNS，OxiDNS 会跳过', 'warning'),
        ('empty-pattern', '$important', 'AdGuard 规则的匹配模式不能为空', 'error'),
        ('only-unsupported-modifier', '$third-party', '规则仅含不支持的修饰符，OxiDNS 会跳过', 'warning'),
        ('URL-pattern', 'https://example.com/ad', '路径或 URL 过滤规则不用于 DNS，OxiDNS 会跳过', 'warning'),
        ('unsupported-modifier', '||example.com^$third-party', '规则含 OxiDNS 不支持的修饰符，整条规则会跳过', 'warning'),
        ('missing-modifier-value', '||example.com^$denyallow', 'denyallow/dnstype 修饰符需要等号和值', 'error'),
        ('invalid-denyallow', '||example.com^$denyallow=bad!example', 'denyallow 中需要合法域名', 'error'),
        ('numeric-dnstype', '||example.com^$dnstype=1', 'dnstype 中需要有效的 DNS 类型名称', 'error'),
        ('unknown-dnstype', '||example.com^$dnstype=UNKNOWN', 'dnstype 中需要有效的 DNS 类型名称', 'error'),
        ('empty-regex', '//', 'AdGuard 正则不能为空', 'error'),
        ('lookahead', '/(?=a)/', 'OxiDNS 的正则不支持反向引用或前后查找', 'error'),
        ('bad-regex', '/[/', '正则表达式语法无效', 'error'),
        ('empty-domain', '...', 'AdGuard 匹配域名不能为空', 'error'),
        ('internal-pipe', '||exam|ple.com^', 'AdGuard 模式内部不能包含 |', 'error'),
    ]
    with Source(b'static AdGuard validation must not connect') as source, Pair(binary, recorder, options={'isolated_cwd': True}) as pair:
        for variant, raw, text, severity in vectors:
            document = {'plugins': [{'tag': 'subject', 'type': 'adguard_rule', 'args': {'rules': [raw]}}]}
            pair.request('H607', ROUTE, body=json_body({'settings': {'config_mode': 'native', 'native_config': yaml.safe_dump(document)}}),
                expect=200, writes=True, interaction=SourceStable(pair, source), variant='adguard/' + variant,
                check=lambda r: check_issue(r, ['plugins', 0, 'args', 'rules', 0], text, severity))


GROUPS = {'oxidns-zone-helper-records': constraints, 'oxidns-adguard-helper-rules': adguard}
