"""Reachable K46 error branches through the ordinary public rule writer."""
import json

from compare import database
from harness import Pair
from cases.config_backup import message
from cases.subscriptions import require


def guards(binary, recorder):
    logical_kind, logical = 'DOMAIN', 'example.com'
    for _ in range(34):
        logical = '((' + logical_kind + ',' + logical + '))'
        logical_kind = 'NOT'
    rows = [
        ('H918', 'DOMAIN', '192.0.2.1', '不是有效域名；IP 地址或网段请使用 IP-CIDR。', ''),
        ('H919', 'DOMAIN', '\ud800.example', '不是有效域名。', 'escaped-lone-surrogate'),
        ('H920', 'DOMAIN', '-bad.example', '不是有效域名。', ''),
        ('H922', 'IP-CIDR', 'not-cidr', '不是有效 IP 网段；IPCIDR 只接受带前缀长度的 CIDR。', ''),
        ('H923', 'IP-CIDR', '192.0.2.1', '单个 IP 也需要前缀长度，请填写 192.0.2.1/32。', ''),
        ('H923', 'IP-CIDR6', '2001:db8::1', '单个 IP 也需要前缀长度，请填写 2001:db8::1/128。', 'ipv6'),
        ('H924', 'IP-CIDR', '192.0.2.1/024', '不是有效 IP 网段；请使用数字前缀长度。', 'leading-zero-prefix'),
        ('H924', 'IP-CIDR6', 'fe80::1%eth0/64', '不是有效 IP 网段；请使用数字前缀长度。', 'zone'),
        ('H925', 'IP-CIDR', '192.0.2.1/33', '不是有效 IP 网段；请检查地址与前缀长度。', ''),
        ('H926', 'SRC-PORT', '/'.join(['1'] * 29), '端口最多支持 28 组范围。', ''),
        ('H927', 'SRC-PORT', '80-abc', '端口请填写数字或范围，以 / 分隔，例如 80/443/8000-9000。', ''),
        ('H928', 'SRC-PORT', '100000', '端口范围必须在 0–65535 内。', ''),
        ('H929', 'SRC-PORT', '65536', '端口范围必须在 0–65535 内，且起点不大于终点。', 'above-max'),
        ('H929', 'SRC-PORT', '100-1', '端口范围必须在 0–65535 内，且起点不大于终点。', 'descending'),
        ('H930', 'DOMAIN-REGEX', ')', '正则表达式的括号不匹配。', ''),
        ('H931', 'DOMAIN-REGEX', '[unclosed', '正则表达式含未闭合括号、字符组或末尾转义。', 'class'),
        ('H931', 'DOMAIN-REGEX', 'trailing\\', '正则表达式含未闭合括号、字符组或末尾转义。', 'escape'),
        ('H931', 'DOMAIN-REGEX', '(unclosed', '正则表达式含未闭合括号、字符组或末尾转义。', 'parenthesis'),
        ('H932', 'DOMAIN-REGEX', '(' * 1000 + 'a' + ')' * 1000, '正则表达式嵌套过深，请简化表达式。', ''),
        ('H933', logical_kind, logical, '逻辑规则嵌套过深，请拆分规则。', ''),
        ('H934', 'AND', 'DOMAIN,example.com', '逻辑规则应使用括号包裹条件，例如 ((DOMAIN,example.com),(NETWORK,tcp))。', ''),
        ('H935', 'AND', '())()', '逻辑规则的括号不匹配。', 'negative-depth'),
        ('H936', 'AND', '(()', '逻辑规则的括号不匹配。', 'positive-depth'),
        ('H937', 'AND', '()', '逻辑规则需要有效的子条件；NOT 必须恰好包含一个条件。', 'empty'),
        ('H937', 'NOT', '((DOMAIN,a.example),(DOMAIN,b.example))', '逻辑规则需要有效的子条件；NOT 必须恰好包含一个条件。', 'two-children'),
        ('H938', 'AND', '((MATCH,))', '逻辑规则不能包含 MATCH。', 'match'),
        ('H938', 'AND', '((SUB-RULE,(NETWORK,tcp)))', '逻辑规则不能包含 SUB-RULE。', 'sub-rule'),
        ('H939', 'DOMAIN', 1, '规则值必须是单行文本。', 'non-text'),
        ('H939', 'DOMAIN', 'a.example\nb.example', '规则值必须是单行文本。', 'newline'),
        ('H939', 'DOMAIN', 'a.example\rb.example', '规则值必须是单行文本。', 'carriage-return'),
        ('H940', 'DOMAIN', '  ', '规则值为空。', ''),
        ('H941', 'DOMAIN', 'example.com,DIRECT', '此规则值不能包含逗号；端口列表请使用 / 分隔，策略与参数应单独填写。', ''),
        ('H942', 'IP-ASN', '-1', 'ASN 应为非负整数。', ''),
        ('H943', 'NETWORK', 'sctp', 'NETWORK 只接受 tcp 或 udp。', ''),
    ]
    with Pair(binary, recorder) as pair:
        before = database(pair.dirs[0])
        for case, kind, value, expected, variant in rows:
            def check(reply):
                message(expected)(reply)
                require(database(pair.dirs[0]) == before, 'invalid syntax leaves all fields and revisions unchanged')
            # ensure_ascii keeps the deliberate surrogate valid JSON on the wire.
            body = json.dumps({'id': 'invalid-rule', 'itemType': 'rule', 'rule_type': kind, 'value': value, 'policy': 'DIRECT'})
            pair.request(case, 'POST /api/rules', body=body, expect=400, writes=True, check=check, variant=variant)


GROUPS = {'rule-syntax-guards': guards}
