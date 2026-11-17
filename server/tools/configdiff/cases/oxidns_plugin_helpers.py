"""H555–H639 through the public native-draft validator, never plugin execution."""
import copy
from collections import Counter

import yaml

from harness import Pair, json_body
from sources import Source
from cases.pull_routes import SourceStable
from cases.subscriptions import require

ROUTE = 'POST /api/oxidns/validate'


def vectors(origin):
    rows = []
    def add(number, kind, args, path, text, severity='error'):
        rows.append(('H' + str(number), kind, args, path, text, severity))
    for number, raw, text in [(556, r'regexp:(a)\1', 'OxiDNS 的正则不支持反向引用或前后查找'),
                              (557, 'regexp:[', '正则表达式语法无效'), (558, 'regexp:', '正则表达式不能为空'),
                              (559, 'domain:.', '域名规则不能为空')]:
        add(number, 'qname', [raw], [], text)
    schedules = [(560, '0 0 * *', '必须使用 5 段 Cron 表达式（分 时 日 月 星期）'),
                 (561, '0,,1 * * * *', 'Cron 字段中有空条目'),
                 (562, '0 0 0W * *', 'Cron 月日字段超出 1–31'),
                 (563, '*/0 * * * *', 'Cron 步长必须是字段范围内的正整数'),
                 (564, '? * * * *', 'Cron 的 ? 只能用于日或星期字段'),
                 (565, '60 * * * *', 'Cron 字段超出允许范围'),
                 (566, '2-1 * * * *', 'Cron 范围起点不能大于终点')]
    def job(**changes):
        return {'name': 'synthetic-job', 'interval': '1m', 'executors': ['accept'], **changes}
    for number, schedule, text in schedules:
        value = job(schedule=schedule)
        value.pop('interval')
        add(number, 'cron', {'jobs': [value]}, ['jobs', 0, 'schedule'], text)
    add(567, 'cache', {'dump_file': './synthetic.dump', 'dump_interval': 0}, ['dump_interval'], '配置持久化文件时，保存间隔必须大于 0')
    add(568, 'qname', ['full:..'], [], '域名规则不能为空')
    add(569, 'client_ip', ['not-a-cidr'], [], '需要有效的 IP 或 CIDR')
    add(570, 'cache', {'min_positive_ttl': 2, 'max_positive_ttl': 1}, ['min_positive_ttl'], 'min_positive_ttl 不能大于 max_positive_ttl')
    add(571, 'http_request', {'url': origin + '/${}'}, ['url'], '占位符名称不能为空')
    add(572, 'http_request', {'url': origin + '/${qname'}, ['url'], '占位符缺少右花括号')
    add(573, 'qtype', [], [], '至少配置一个匹配值')
    add(574, 'question', ['not-a-provider-reference'], [], '这里只接受 $tag 形式的 Provider 引用')
    add(575, 'qtype', ['65536'], [], 'DNS 类型、类别或响应码无效')
    add(576, 'mark', ['4294967296'], [], '标记必须是 0–4294967295 的整数')
    add(577, 'random', ['NaN'], [], '概率必须且只能填写一个 0–1 的数字字符串')
    add(578, 'env', [], [], '至少配置一个环境变量条件')
    add(579, 'env', ['=synthetic'], [0], '环境变量名称不能为空')
    add(580, 'any_match', [], [], '至少配置一个匹配表达式')
    add(581, 'string_exp', 'invalid eq synthetic', [], '字符串表达式的来源字段无效')
    add(582, 'string_exp', 'qname invalid synthetic', [], '字符串表达式的操作无效')
    add(583, 'string_exp', 'qname zl synthetic', [], 'zl 不接受参数；其他操作至少需要一个参数')
    add(584, 'string_exp', 'qname regexp (?=a)', [], 'OxiDNS 的正则不支持反向引用或前后查找')
    add(585, 'ttl', '1-2-3', [], 'TTL 简写需为非负整数或 min-max，数值不得大于 4294967295')
    add(586, 'ttl', {}, [], 'TTL 至少需要 fix、min 或 max 中的一个非空值')
    add(587, 'forward_edns0opt', '65536', [], 'EDNS0 代码必须为 0–65535 的整数')
    add(588, 'black_hole', {'mode': None}, ['mode'], 'mode 为空时 OxiDNS 会按 nxdomain 处理；黑洞请写 "null"（带引号）', 'warning')
    add(589, 'black_hole', {'mode': 'invalid'}, ['mode'], '拦截模式无效')
    add(590, 'black_hole', {'mode': 'custom', 'ips': []}, ['ips'], 'custom 模式至少需要一个 IP')
    add(591, 'black_hole', {'mode': 'refused', 'ips': ['192.0.2.1']}, ['ips'], '只有 custom 模式可以配置 ips')
    add(592, 'black_hole', 'nxdomain short_circuit=maybe', [], 'short_circuit 必须为 true 或 false')
    add(593, 'black_hole', 'custom', [], 'custom 模式至少需要一个 IP')
    add(594, 'black_hole', 'nxdomain 192.0.2.1', [], '只有 custom 模式可以附加 IP')
    add(595, 'cache', {'dump_file': './synthetic.dump', 'dump_interval': 0}, ['dump_interval'], '配置持久化文件时，保存间隔必须大于 0')
    add(596, 'prefer_ipv4', {'cache': True, 'cache_ttl': 0}, ['cache_ttl'], '启用探测缓存时 TTL 必须大于 0')
    add(597, 'ip_selector', {'probe_methods': []}, ['probe_methods'], '至少配置一种探测方法')
    add(598, 'ip_selector', {'probe_methods': ['tcp:0']}, ['probe_methods'], '探测方法应为 ping、none 或 tcp:1–65535')
    add(599, 'ip_selector', {'probe_methods': ['none', 'ping']}, ['probe_methods'], 'none 不能与其他探测方法同时使用')
    add(600, 'ip_selector', {'cache': {'enabled': True, 'size': 0}}, ['cache', 'size'], '启用缓存时此值必须大于 0')
    add(601, 'hosts', {'entries': ['example.com']}, ['entries', 0], '需填写域名规则及至少一个 IP')
    add(602, 'hosts', {'entries': ['regexp:(?=a) 192.0.2.1']}, ['entries', 0], 'OxiDNS 的正则不支持反向引用或前后查找')
    add(603, 'redirect', {'rules': ['example.com ' + 'a' * 64 + '.example']}, ['rules', 0], '重定向的目标域名无效')
    add(604, 'arbitrary', {'rules': ['example.com. IN A not-an-ip']}, ['rules', 0], 'DNS 记录的名称、TTL、类型或 RDATA 格式无效')
    runtime_zone = '外部 zone 文件和生成记录需要目标核心在初始化时检查'
    add(605, 'arbitrary', {'rules': ['$INCLUDE /synthetic-static-only.zone']}, ['rules', 0], runtime_zone, 'warning')
    add(606, 'response', {'answers': ['$INCLUDE /synthetic-static-only.zone']}, ['answers', 0], runtime_zone, 'warning')
    add(607, 'adguard_rule', {'rules': ['example.com##.advert']}, ['rules', 0], '此为非 DNS 过滤规则，OxiDNS 会跳过', 'warning')
    add(608, 'geosite', {'files': ['./synthetic-geosite.db'], 'selectors': ['@cn']}, ['selectors', 0], '分类名称和 @ 后的属性名不能为空')
    nft = {'table_family4': 'ip', 'table_name4': 'synthetic-table', 'set_name4': 'synthetic-set'}
    add(609, 'nftset', {**nft, 'table_family4': 'bad'}, ['table_family4'], '地址族必须为 ip、ip6 或 inet')
    add(610, 'nftset', {**nft, 'table_name4': ' '}, ['table_name4'], '名称不能为空')
    add(611, 'nftset', {**nft, 'mask4': 33}, ['mask4'], '前缀长度不能超过 32')
    ros = {'address': '127.0.0.1:' + origin.rsplit(':', 1)[-1], 'username': 'synthetic', 'password': 'synthetic', 'routing_table': 'synthetic-table'}
    add(612, 'ros_route', ros, ['gateway4'], '至少配置 gateway4 或 gateway6')
    ros = {**ros, 'gateway4': '192.0.2.2'}
    add(613, 'ros_route', {**ros, 'comment_prefix': 'bad;prefix'}, ['comment_prefix'], '注释前缀不能包含 ; 或 =')
    add(614, 'ros_route', {**ros, 'tls': {'insecure': True, 'ca': './synthetic-ca.pem'}}, ['tls', 'ca'], '跳过 TLS 校验时不能同时指定 CA')
    add(615, 'ros_route', {**ros, 'wait_timeout': '0s'}, ['wait_timeout'], '等待超时必须大于 0')
    add(616, 'ros_route', {**ros, 'persistent': {'ips': ['not-an-ip']}}, ['persistent', 'ips', 0], '常驻地址需为有效 IP 或 CIDR')
    add(617, 'ros_route', {**ros, 'persistent': {'ips': ['2001:db8::/32']}}, ['persistent', 'ips', 0], '未设置该地址族的目标，OxiDNS 会忽略此常驻地址', 'warning')
    add(618, 'ros_route', {**ros, 'persistent': {'ips': ['0.0.0.0/0']}}, ['persistent', 'ips', 0], 'OxiDNS 会忽略 /0 默认路由', 'warning')
    http = {'url': origin + '/static-only'}
    add(619, 'http_request', {**http, 'timeout': '-1s'}, ['timeout'], '超时需为非负整数秒或带 ms/s/m/h/d 单位的时长')
    add(620, 'http_request', {**http, 'body': 'x', 'json': {}}, ['body'], 'body、json 和非空 form 不能同时配置')
    add(621, 'http_request', {**http, 'json': {}, 'content_type': 'application/json'}, ['content_type'], 'content_type 仅用于原始 body')
    add(622, 'http_request', {**http, 'content_type': 'application/json\nX: synthetic'}, ['content_type'], 'HTTP 头值不能包含换行')
    add(623, 'http_request', {'url': 'file:///synthetic-static-only'}, ['url'], '需要有效的 HTTP / HTTPS URL')
    add(624, 'http_request', {**http, 'headers': {'': 'synthetic'}}, ['headers', ''], '名称不能为空')
    add(625, 'http_request', {**http, 'headers': {'bad header': 'synthetic'}}, ['headers', 'bad header'], 'HTTP 请求头名称无效')
    add(626, 'http_request', {**http, 'headers': {'X-Synthetic': ' '}}, ['headers', 'X-Synthetic'], '请求模板值不能为空')
    add(627, 'script', {'command': './synthetic-never-executed', 'env': {'': 'synthetic'}}, ['env', ''], '环境变量名称不能为空')
    add(628, 'download', {'downloads': [{'url': origin + '/', 'dir': './synthetic-downloads'}]}, ['downloads', 0, 'filename'], 'URL 无法推导文件名，请填写 filename')
    add(629, 'download', {'downloads': [{'url': origin + '/a', 'filename': 'same', 'dir': './synthetic-downloads'},
                                      {'url': origin + '/b', 'filename': 'same', 'dir': './synthetic-downloads'}]}, ['downloads', 1], '下载目标文件重复')
    add(630, 'cron', {'timezone': 'Synthetic/Unknown', 'jobs': [job()]}, ['timezone'], '需要有效的 IANA 时区，例如 Asia/Shanghai')
    add(631, 'time', {'periods': [{'start': '12:00'}]}, ['periods', 0], 'start 和 end 必须同时填写或同时省略')
    add(632, 'time', {'periods': [{'start': '25:00', 'end': '01:00'}]}, ['periods', 0, 'start'], '时间需为 00:00–23:59 的 HH:MM')
    add(633, 'time', {'periods': [{'start': '12:00', 'end': '12:00'}]}, ['periods', 0, 'end'], '开始和结束时间不能相同')
    add(634, 'time', {'periods': [{}]}, ['periods', 0], '省略时间段时至少设置星期或月日')
    add(635, 'time', {'periods': [{'weekdays': ['wrong']}]}, ['periods', 0, 'weekdays', 0], '星期为 mon–sun 或数字 1–7；数字字符串不支持')
    add(636, 'cron', {'jobs': [job(), job()]}, ['jobs', 1, 'name'], '任务名称重复')
    add(637, 'cron', {'jobs': [job(schedule='0 * * * *')]}, ['jobs', 0], 'schedule 和 interval 必须且只能填写一个')
    invalid_schedule = job(schedule='0 0 * *')
    invalid_schedule.pop('interval')
    add(638, 'cron', {'jobs': [invalid_schedule]}, ['jobs', 0, 'schedule'], '必须使用 5 段 Cron 表达式（分 时 日 月 星期）')
    add(639, 'cron', {'jobs': [job(interval='59s')]}, ['jobs', 0, 'interval'], '定时间隔至少为 1 分钟')
    # Exercise each field/family in the helper's loops, and both sides of
    # conditional alternatives. IDs remain the original branch recipes.
    for raw in (r'regexp:(?=a)', r'regexp:(?!a)', r'regexp:(?<=a)b', r'regexp:(?<!a)b'):
        add(556, 'qname', [raw], [], 'OxiDNS 的正则不支持反向引用或前后查找')
    for kind in ('cname',):
        add(559, kind, ['domain:.'], [], '域名规则不能为空')
    for schedule in ('0 0 32W * *', '0 0 L-32 * *'):
        value = job(schedule=schedule); value.pop('interval')
        add(562, 'cron', {'jobs': [value]}, ['jobs', 0, 'schedule'], 'Cron 月日字段超出 1–31')
    for schedule in ('*/1/2 * * * *', '*/bad * * * *'):
        value = job(schedule=schedule); value.pop('interval')
        add(563, 'cron', {'jobs': [value]}, ['jobs', 0, 'schedule'], 'Cron 步长必须是字段范围内的正整数')
    for schedule in ('1-2-3 * * * *', '0 0 0 * *', 'bad * * * *'):
        value = job(schedule=schedule); value.pop('interval')
        add(565, 'cron', {'jobs': [value]}, ['jobs', 0, 'schedule'], 'Cron 字段超出允许范围')
    for kind in ('ptr_ip', 'resp_ip'):
        add(569, kind, ['not-a-cidr'], [], '需要有效的 IP 或 CIDR')
    for kind in ('ros_route', 'ros_address_list'):
        add(570, kind, {**ros, 'min_ttl': 3601, 'max_ttl': 3600}, ['min_ttl'], 'min_ttl 不能大于 max_ttl')
    for number, bad, text in [(571, '${}', '占位符名称不能为空'), (572, '${qname', '占位符缺少右花括号')]:
        for key in ('body', 'content_type'):
            add(number, 'http_request', {**http, key: bad}, [key], text)
        for key in ('headers', 'query_params', 'form'):
            add(number, 'http_request', {**http, key: {'X-Synthetic': bad}}, [key, 'X-Synthetic'], text)
        add(number, 'script', {'command': './synthetic-never-executed', 'args': [bad]}, ['args', 0], text)
        add(number, 'script', {'command': './synthetic-never-executed', 'env': {'SYNTHETIC': bad}}, ['env', 'SYNTHETIC'], text)
    add(573, 'reload_provider', [], [], '至少引用一个 Provider')
    add(574, 'reload_provider', ['not-a-provider-reference'], [], '这里只接受 $tag 形式的 Provider 引用')
    for kind in ('qclass', 'rcode'):
        add(575, kind, ['65536'], [], 'DNS 类型、类别或响应码无效')
    for args in (['2'], ['0.5', '0.6']):
        add(577, 'random', args, [], '概率必须且只能填写一个 0–1 的数字字符串')
    add(579, 'env', ' =synthetic', [], '环境变量名称不能为空')
    add(580, 'any_match', '\n \t', [], '至少配置一个匹配表达式')
    add(583, 'string_exp', 'qname eq', [], 'zl 不接受参数；其他操作至少需要一个参数')
    add(585, 'ttl', '4294967296', [], 'TTL 简写需为非负整数或 min-max，数值不得大于 4294967295')
    add(586, 'ttl', {'fix': None, 'min': None, 'max': None}, [], 'TTL 至少需要 fix、min 或 max 中的一个非空值')
    add(591, 'black_hole', {'mode': 'null', 'ips': ['2001:db8::1']}, ['ips'], '只有 custom 模式可以配置 ips')
    add(594, 'black_hole', 'refused 2001:db8::1', [], '只有 custom 模式可以附加 IP')
    add(596, 'prefer_ipv6', {'cache_ttl': 0}, ['cache_ttl'], '启用探测缓存时 TTL 必须大于 0')
    for number, methods, text in [(597, ' , ', '至少配置一种探测方法'),
                                  (598, 'tcp:65536', '探测方法应为 ping、none 或 tcp:1–65535'),
                                  (598, 'tcp:abc', '探测方法应为 ping、none 或 tcp:1–65535'),
                                  (599, 'none,ping', 'none 不能与其他探测方法同时使用')]:
        add(number, 'ip_selector', {'probe_methods': methods}, ['probe_methods'], text)
    for key in ('ttl', 'failure_ttl'):
        add(600, 'ip_selector', {'cache': {key: 0}}, ['cache', key], '启用缓存时此值必须大于 0')
    add(601, 'redirect', {'rules': ['example.com']}, ['rules', 0], '重定向规则需恰好填写域名规则和目标域名')
    add(602, 'redirect', {'rules': ['regexp:(?=a) example.com']}, ['rules', 0], 'OxiDNS 的正则不支持反向引用或前后查找')
    add(603, 'redirect', {'rules': ['example.com a..example']}, ['rules', 0], '重定向的目标域名无效')
    for key in ('authorities', 'additionals'):
        add(606, 'response', {key: ['$INCLUDE /synthetic-static-only.zone']}, [key, 0], runtime_zone, 'warning')
    add(608, 'geosite', {'files': ['./synthetic-geosite.db'], 'selectors': ['cn@']}, ['selectors', 0], '分类名称和 @ 后的属性名不能为空')
    nft6 = {'table_family6': 'ip6', 'table_name6': 'synthetic-table', 'set_name6': 'synthetic-set'}
    add(609, 'nftset', {**nft6, 'table_family6': 'bad'}, ['table_family6'], '地址族必须为 ip、ip6 或 inet')
    for suffix, base in [('4', nft), ('6', nft6)]:
        for key in ('table_name', 'set_name'):
            add(610, 'nftset', {**base, key + suffix: ' '}, [key + suffix], '名称不能为空')
    add(611, 'nftset', {**nft6, 'mask6': 129}, ['mask6'], '前缀长度不能超过 128')
    address_list = {key: value for key, value in ros.items() if key not in ('gateway4', 'routing_table')}
    add(612, 'ros_address_list', address_list, ['address_list4'], '至少配置 address_list4 或 address_list6')
    for kind, base in [('ros_route', ros), ('ros_address_list', {**address_list, 'address_list4': 'synthetic-v4'})]:
        add(613, kind, {**base, 'comment_prefix': 'bad=prefix'}, ['comment_prefix'], '注释前缀不能包含 ; 或 =')
        add(614, kind, {**base, 'tls': {'insecure': True, 'ca': './synthetic-ca.pem'}}, ['tls', 'ca'], '跳过 TLS 校验时不能同时指定 CA')
        add(615, kind, {**base, 'wait_timeout': 0}, ['wait_timeout'], '等待超时必须大于 0')
        add(616, kind, {**base, 'persistent': {'ips': ['# comment\nnot-a-cidr']}}, ['persistent', 'ips', 0], '常驻地址需为有效 IP 或 CIDR')
    ros6 = {**{k: v for k, v in ros.items() if k != 'gateway4'}, 'gateway6': '2001:db8::2'}
    add(617, 'ros_route', {**ros6, 'persistent': {'ips': ['192.0.2.0/24']}}, ['persistent', 'ips', 0], '未设置该地址族的目标，OxiDNS 会忽略此常驻地址', 'warning')
    add(618, 'ros_route', {**ros6, 'persistent': {'ips': ['::/0']}}, ['persistent', 'ips', 0], 'OxiDNS 会忽略 /0 默认路由', 'warning')
    for kind in ('learn_domain', 'upgrade'):
        add(619, kind, {**http, 'timeout': 'bad'}, ['timeout'], '超时需为非负整数秒或带 ms/s/m/h/d 单位的时长')
    for fields in ({'body': 'x', 'form': {'x': 'y'}}, {'json': {}, 'form': {'x': 'y'}}):
        add(620, 'http_request', {**http, **fields}, ['body'], 'body、json 和非空 form 不能同时配置')
    add(621, 'http_request', {**http, 'form': {'x': 'y'}, 'content_type': 'application/json'}, ['content_type'], 'content_type 仅用于原始 body')
    add(622, 'http_request', {**http, 'content_type': 'text/plain\rsynthetic'}, ['content_type'], 'HTTP 头值不能包含换行')
    add(624, 'http_request', {**http, 'form': {' ': 'synthetic'}}, ['form', ' '], '名称不能为空')
    for key in ('form', 'query_params'):
        add(626, 'http_request', {**http, key: {'X-Synthetic': ' '}}, [key, 'X-Synthetic'], '请求模板值不能为空')
    add(627, 'script', {'command': './synthetic-never-executed', 'env': {' ': 'synthetic'}}, ['env', ' '], '环境变量名称不能为空')
    add(631, 'time', {'periods': [{'end': '12:00'}]}, ['periods', 0], 'start 和 end 必须同时填写或同时省略')
    add(632, 'time', {'periods': [{'start': '01:00', 'end': '24:00'}]}, ['periods', 0, 'end'], '时间需为 00:00–23:59 的 HH:MM')
    add(635, 'time', {'periods': [{'weekdays': ['1']}]}, ['periods', 0, 'weekdays', 0], '星期为 mon–sun 或数字 1–7；数字字符串不支持')
    add(637, 'cron', {'jobs': [{'name': 'synthetic-job', 'executors': ['accept']}]}, ['jobs', 0], 'schedule 和 interval 必须且只能填写一个')
    for interval in ('bad', '0s'):
        add(639, 'cron', {'jobs': [job(interval=interval)]}, ['jobs', 0, 'interval'], '定时间隔至少为 1 分钟')
    return rows


def check_issue(reply, path, text, severity, *, source='config.yaml'):
    value = reply.json()
    require(value['schema_version'] == '1.6.1' and value['core_check_required'] is True,
            'static diagnostics retain the pinned schema and explicit target-core validation requirement')
    matching = [item for item in value['issues'] if item.get('path') == path and item.get('message') == text
                and item.get('severity') == severity and item.get('source') == source]
    require(len(matching) == 1 and isinstance(matching[0].get('code'), str) and bool(matching[0]['code']),
            'named helper branch produces exactly its expected path/message/severity and a code: ' + repr((path, text, value)))
    require(value['valid'] is (not any(item['severity'] == 'error' for item in value['issues'])),
            'valid is computed from all issues; a warning alone is never silently upgraded to an error')


def constraints(binary, recorder):
    with Source(b'synthetic static validator must not connect') as source, Pair(binary, recorder, options={'isolated_cwd': True}) as pair:
        rows = [('H555', {'log': {'level': 'synthetic-invalid'}, 'plugins': []}, ['log', 'level'],
                 '日志级别必须为 off、trace、debug、info、warn 或 error', 'error')]
        rows += [(case, {'plugins': [{'tag': 'subject', 'type': kind, 'args': copy.deepcopy(args)}]},
                  ['plugins', 0, 'args', *path], text, severity) for case, kind, args, path, text, severity in vectors(source.url)]
        require(len({item[0] for item in rows}) == 85, 'every H555–H639 recipe has independent public requests')
        counts = Counter()
        for case, document, path, text, severity in rows:
            settings = {'config_mode': 'native', 'native_config': yaml.safe_dump(document, allow_unicode=True, sort_keys=False)}
            pair.request(case, ROUTE, body=json_body({'settings': settings, 'base_url': 'http://panel.example'}), expect=200, writes=True,
                check=lambda r: check_issue(r, path, text, severity), interaction=SourceStable(pair, source),
                variant='native-plugin-constraint/' + str(counts[case]))
            counts[case] += 1


GROUPS = {'oxidns-plugin-helper-constraints': constraints}
