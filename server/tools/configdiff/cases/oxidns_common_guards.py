"""Public OxiDNS common draft type guards and client policy validation."""
import copy

from harness import Pair, json_body
from cases.config_backup import message
from cases.oxidns_routes import SCOPES, OPTIONS, BASE, prepared
from cases.setup import Stable
from cases.subscriptions import require


def common_types(binary, recorder):
    rows = [('H356', {'ros_address_list.args.tls': 'invalid'}, 'ros_address_list.args.tls 必须是 TLS 参数对象或 null'),
            ('H357', {'cache_enabled': 1}, 'cache_enabled 必须为布尔值'),
            ('H357', {'cache_enabled': 'true'}, 'cache_enabled 必须为布尔值'),
            ('H358', {'worker_threads': True}, 'worker_threads 必须为整数'),
            ('H358', {'worker_threads': 2.0}, 'worker_threads 必须为整数'),
            ('H359', {'block_qtypes': {}}, 'block_qtypes 必须为数组'),
            ('H360', {'block_qtypes': [True]}, '屏蔽查询类型必须为整数数组'),
            ('H360', {'block_qtypes': ['28']}, '屏蔽查询类型必须为整数数组'),
            ('H361', {'listen': []}, 'listen 必须为文本'),
            ('H362', {'dynamic_domain_set.route': 'unknown'}, '动态域名路由必须为 direct 或 proxy'),
            ('H366', {'adguard.urls': ['ftp://rules.example/source']}, 'AdGuard 订阅必须为 HTTP(S) URL'),
            ('H367', {'adguard.mode': 'unknown'}, 'AdGuard 响应模式无效'),
            ('H370', {'unknown_z': True, 'unknown_a': 1}, '不支持的常用参数：unknown_a, unknown_z'),
            ('H353', {'fallback_dns': '[\n'}, 'DNS 上游 YAML 语法无效'),
            ('H354', {'fallback_dns': '{}'}, 'DNS 上游必须为参数对象数组'),
            ('H355', {'fallback_dns': '- {}\n'}, '启用代理回退前请先填写备用 DNS 地址'),
            ('H372', {'local_dns': '[\n'}, 'DNS 上游 YAML 语法无效'),
            ('H373', {'local_dns': '{}'}, 'DNS 上游必须为参数对象数组')]
    for template, prefix in SCOPES:
        with Pair(binary, recorder, options=OPTIONS) as pair:
            start = prepared(pair, template, prefix, 'common-primitive-guards-precondition')
            for i, (case, changes, expected) in enumerate(rows):
                pair.request(case, 'POST ' + template + '/common-settings/apply', prefix + '/common-settings/apply',
                    body=json_body({'settings': start, 'changes': changes, 'base_url': BASE}), expect=400, writes=True,
                    interaction=Stable(pair), check=message(expected), variant='common-type/' + str(i))


def client_policies(binary, recorder):
    valid = {'ip': '192.0.2.8', 'name': '合成客户端🧪', 'mode': 'direct'}
    address_error = '请输入有效的客户端 IPv4、IPv6 或 CIDR 网段'
    rows = [('H416', [{**valid, 'ip': 1}], address_error),
            ('H417', [{**valid, 'ip': '192.0.2.8/24/24'}], address_error),
            ('H418', [{**valid, 'ip': '192.0.2.8/ab'}], address_error),
            ('H419', [{**valid, 'ip': '2001:db8::1/129'}], address_error),
            ('H420', [{**valid, 'ip': '::ffff:192.0.2.8/95'}], address_error),
            ('H421', [{**valid, 'ip': '224.0.0.1'}], address_error),
            ('H422', [{**valid, 'ip': '0.0.0.0'}], address_error),
            ('H424', {}, '设备策略必须为列表，最多 512 条策略。'),
            ('H424', [valid] * 513, '设备策略必须为列表，最多 512 条策略。'),
            ('H425', ['invalid'], '设备策略仅支持 IP / 网段、备注和策略。'),
            ('H425', [{**valid, 'unexpected': True}], '设备策略仅支持 IP / 网段、备注和策略。'),
            ('H426', [valid, {**valid, 'ip': '::ffff:192.0.2.8'}], '客户端 IP / 网段重复：192.0.2.8'),
            ('H427', [{**valid, 'name': 'x' * 81}], '设备备注最多 80 个字符，不能包含换行或控制字符。'),
            ('H427', [{**valid, 'name': '中文\n🧪'}], '设备备注最多 80 个字符，不能包含换行或控制字符。'),
            ('H428', [{**valid, 'mode': 'proxy'}], '设备策略必须为智能分流或直连。')]
    for template, prefix in SCOPES:
        with Pair(binary, recorder) as pair:
            for i, (case, policies, expected) in enumerate(rows):
                pair.request(case, 'POST ' + template + '/editor', prefix + '/editor',
                    body=json_body({'settings': {'worker_threads': 2, 'device_policies': copy.deepcopy(policies)}, 'base_url': BASE}),
                    expect=400, writes=True, interaction=Stable(pair),
                    check=(lambda reply: require(
                        reply.json().get('message', '').startswith(address_error), 'client range guard keeps its complete Chinese public diagnostic'))
                        if expected == address_error else message(expected), variant='client-policy/' + str(i))


GROUPS = {'oxidns-common-type-guards': common_types, 'oxidns-client-policy-guards': client_policies}
