"""H665–H698 public plugin references, quick syntax and dependency cycles."""
from collections import Counter

import yaml

from harness import Pair, json_body
from sources import Source
from cases.oxidns_plugin_helpers import check_issue
from cases.pull_routes import SourceStable
from cases.subscriptions import require

ROUTE = 'POST /api/oxidns/validate'


def sequence(expression, *, matcher=False, extras=()):
    rule = {'exec': 'accept', 'matches': [expression]} if matcher else {'exec': expression}
    document = {'plugins': [{'tag': 'subject', 'type': 'sequence', 'args': [rule]}, *extras]}
    return document, ['plugins', 0, 'args', 0, 'matches', 0] if matcher else ['plugins', 0, 'args', 0, 'exec']


def reference_vectors(origin):
    rows = []
    def add(number, document, path, text, severity='error'):
        rows.append(('H' + str(number), document, path, text, severity))
    def quick(number, expression, text, *, matcher=False, severity='error', extras=()):
        document, path = sequence(expression, matcher=matcher, extras=extras)
        add(number, document, path, text, severity)
    def server(number, target, text, *, target_type=None, severity='error'):
        plugins = [{'tag': 'subject', 'type': 'udp_server', 'args': {'listen': '127.0.0.1:0', 'entry': target}}]
        if target_type:
            plugins.append({'tag': 'target', 'type': target_type, 'args': {}})
        add(number, {'plugins': plugins}, ['plugins', 0, 'args', 'entry'], text, severity)
    server(665, 'missing', '引用的插件 tag 不存在')
    add(666, {'plugins': [{'tag': 'duplicate', 'type': 'cache', 'args': {}}, {'tag': 'duplicate', 'type': 'cache', 'args': {}}]},
        ['plugins', 1, 'tag'], '插件 tag 重复，必须全局唯一')
    server(667, ' ', '插件引用不能为空')
    server(668, 'missing', '引用的插件 tag 不存在')
    server(669, 'target', '目标是尚未识别的插件类型，由目标核心确认其能力', target_type='synthetic_unknown', severity='warning')
    server(670, 'target', '引用目标必须为 executor 类型插件', target_type='domain_set')
    quick(671, 'jump target', '引用目标必须为 sequence 插件', extras=[{'tag': 'target', 'type': 'cache', 'args': {}}])
    for number, source, target, text in [(672, 'qname', 'ip_set', '引用的 Provider 必须支持域名匹配'),
                                          (673, 'client_ip', 'domain_set', '引用的 Provider 必须支持 IP 匹配')]:
        add(number, {'plugins': [{'tag': 'subject', 'type': source, 'args': ['$target']},
                                {'tag': 'target', 'type': target, 'args': {}}]}, ['plugins', 0, 'args'], text)
    quick(674, 'rate_limiter -1', '必须大于 0', matcher=True)
    quick(675, 'qname regexp:[', '正则表达式语法无效', matcher=True)
    quick(676, 'forward udp://192.0.2.53 short_circuit=maybe', 'short_circuit 只能使用 true 或 false')
    quick(677, 'synthetic_unknown argument', '尚未识别的快捷表达式，请由目标核心确认', severity='warning')
    quick(678, 'qname example.com', '此处需要 executor 表达式')
    quick(679, 'response', '该插件不支持快捷表达式，请定义插件后使用 $tag 引用')
    quick(680, '_true argument', '此匹配器不接受参数', matcher=True)
    quick(681, 'qname', '快捷表达式缺少必填参数', matcher=True)
    quick(682, 'cache size=10', 'cache 快捷参数仅支持 short_circuit、short_circuit=true/false')
    # format_error('', duration) already returns its nonempty diagnostic, so
    # even absent sleep arguments use that text before the fallback literal.
    quick(683, 'sleep', '时长须为非负整数秒或整数加 ms、s、m、h、d 单位')
    quick(684, 'forward_edns0opt 65536', 'EDNS0 代码必须为 0–65535 的整数')
    quick(685, 'time 12:00', 'time 快捷参数必须是单个 HH:MM-HH:MM 时间段', matcher=True)
    quick(686, 'rate_limiter 1 bad 24 64', 'rate_limiter 快捷参数需为 qps burst mask4 mask6 数字', matcher=True)
    quick(687, 'rate_limiter invalid 1 24 64', 'rate_limiter 快捷参数需为 qps burst mask4 mask6 数字', matcher=True)
    quick(688, 'ipset synthetic,bad,24', 'ipset 快捷格式为 set,inet|inet6,mask')
    quick(689, 'nftset bad,table,set,ipv4_addr,24', 'nftset 快捷格式为 family,table,set,type,mask')
    quick(690, 'nftset ip,synthetic,synthetic,unknown,24', '未知 nftset 地址类型，核心会忽略此条目', severity='warning')
    quick(691, 'download ' + origin + '/file', 'download 快捷参数必须包含 URL 和目录')
    quick(692, 'upgrade unsupported=1', 'upgrade 包含不支持的快捷参数')
    quick(693, 'upgrade force=maybe', 'upgrade 开关必须为 true 或 false')
    quick(694, '', '表达式不能为空')
    quick(695, 'jump $target', 'jump/goto 必须引用不带 $ 的 sequence tag')
    quick(696, 'reject 16', 'reject 只接受一个基础响应码 0–15 或对应名称')
    quick(697, 'mark 4294967296', 'mark/set_mark 至少需要一个 0–4294967295 整数')
    quick(698, '$subject', '插件依赖形成循环，无法初始化')
    # Traverse the distinct callable fields and shorthand alternatives.
    for kind, args, suffix in [
        ('http_server', {'listen': '127.0.0.1:0', 'entries': [{'path': '/', 'exec': 'missing'}]}, ['entries', 0, 'exec']),
        ('fallback', {'primary': 'missing', 'secondary': 'available'}, ['primary']),
        ('prefer_ipv4', {'probe_executor': 'missing'}, ['probe_executor']),
        ('prefer_ipv6', {'probe_executor': 'missing'}, ['probe_executor'])]:
        add(668, {'plugins': [{'tag': 'subject', 'type': kind, 'args': args}, {'tag': 'available', 'type': 'cache', 'args': {}}]},
            ['plugins', 0, 'args', *suffix], '引用的插件 tag 不存在')
    add(671, {'plugins': [{'tag': 'subject', 'type': 'learn_domain', 'args': {'provider': 'target'}},
                         {'tag': 'target', 'type': 'domain_set', 'args': {}}]},
        ['plugins', 0, 'args', 'provider'], '引用目标必须为 dynamic_domain_set 插件')
    for kind in ('cname',):
        add(672, {'plugins': [{'tag': 'subject', 'type': kind, 'args': ['$target']}, {'tag': 'target', 'type': 'ip_set', 'args': {}}]},
            ['plugins', 0, 'args'], '引用的 Provider 必须支持域名匹配')
    for kind in ('resp_ip', 'ptr_ip'):
        add(673, {'plugins': [{'tag': 'subject', 'type': kind, 'args': ['$target']}, {'tag': 'target', 'type': 'domain_set', 'args': {}}]},
            ['plugins', 0, 'args'], '引用的 Provider 必须支持 IP 匹配')
    quick(674, 'rate_limiter NaN', '参数类型无效，需要 number / null', matcher=True)
    quick(675, 'time 12:00-12:00', '开始和结束时间不能相同', matcher=True)
    quick(676, 'black_hole nxdomain short_circuit=maybe', 'short_circuit 只能使用 true 或 false')
    quick(678, 'cache', '此处需要 matcher 表达式', matcher=True)
    for kind in ('_false', 'has_resp', 'has_wanted_ans'):
        quick(680, kind + ' argument', '此匹配器不接受参数', matcher=True)
    for kind, matcher in [('qtype', True), ('qclass', True), ('rcode', True), ('mark', True), ('random', True),
                          ('string_exp', True), ('ttl', False)]:
        quick(681, kind, '快捷表达式缺少必填参数', matcher=matcher)
    quick(683, 'sleep bad', '时长须为非负整数秒或整数加 ms、s、m、h、d 单位')
    quick(684, 'forward_edns0opt -1', 'EDNS0 代码必须为 0–65535 的整数')
    quick(685, 'time 12:00 - 13:00', 'time 快捷参数必须是单个 HH:MM-HH:MM 时间段', matcher=True)
    for value in ('rate_limiter 1 1 -1 64', 'rate_limiter 1 1 24 4294967296'):
        quick(686, value, 'rate_limiter 快捷参数需为 qps burst mask4 mask6 数字', matcher=True)
    for value in ('ipset synthetic', 'ipset synthetic,inet,256'):
        quick(688, value, 'ipset 快捷格式为 set,inet|inet6,mask')
    for value in ('nftset ip,table', 'nftset ip,,set,ipv4_addr,24', 'nftset ip,table,,ipv4_addr,24', 'nftset ip,table,set,ipv4_addr,256'):
        quick(689, value, 'nftset 快捷格式为 family,table,set,type,mask')
    quick(692, 'upgrade unsupported', 'upgrade 包含不支持的快捷参数')
    quick(693, 'upgrade no_restart=maybe', 'upgrade 开关必须为 true 或 false')
    quick(694, '! ', '表达式不能为空', matcher=True)
    for value in ('jump', 'goto', 'goto $target'):
        quick(695, value, 'jump/goto 必须引用不带 $ 的 sequence tag')
    for value in ('reject NOERROR NXDOMAIN', 'reject INVALID'):
        quick(696, value, 'reject 只接受一个基础响应码 0–15 或对应名称')
    for value in ('mark', 'set_mark', 'set_mark 1,-1'):
        quick(697, value, 'mark/set_mark 至少需要一个 0–4294967295 整数')
    document, _ = sequence('$other', extras=[{'tag': 'other', 'type': 'sequence', 'args': [{'exec': '$subject'}]}])
    add(698, document, ['plugins', 1, 'args', 0, 'exec'], '插件依赖形成循环，无法初始化')
    return rows


def constraints(binary, recorder):
    with Source(b'reference validation must not execute or connect') as source, Pair(binary, recorder, options={'isolated_cwd': True}) as pair:
        rows = reference_vectors(source.url)
        require(len({item[0] for item in rows}) == 34, 'every H665–H698 has an independent public diagnostic recipe')
        counters = Counter()
        for case, document, path, text, severity in rows:
            settings = {'config_mode': 'native', 'native_config': yaml.safe_dump(document, allow_unicode=True, sort_keys=False)}
            pair.request(case, ROUTE, body=json_body({'settings': settings}), expect=200, writes=True,
                check=lambda r: check_issue(r, path, text, severity), interaction=SourceStable(pair, source),
                variant='reference-quick/' + str(counters[case]))
            counters[case] += 1
        for variant, expression, matcher in [('sleep-zero', 'sleep 0s', False),
            ('EDNS0-bounds', 'forward_edns0opt 0,65535', False), ('reject-name', 'reject NXDOMAIN', False),
            ('mark-bounds', 'set_mark 0,4294967295', False), ('time-period', 'time 12:00-13:00', True)]:
            document, _ = sequence(expression, matcher=matcher)
            pair.request('G15', ROUTE, body=json_body({'settings': {'config_mode': 'native', 'native_config': yaml.safe_dump(document)}}),
                expect=200, writes=True, interaction=SourceStable(pair, source), variant='quick-valid/' + variant,
                check=lambda r: require(r.json() == {'valid': True, 'issues': [], 'schema_version': '1.6.1', 'core_check_required': True},
                                         'supported zero/bounds/named-code/time shorthand is statically valid without executing the plugin'))


GROUPS = {'oxidns-reference-helper-constraints': constraints}
