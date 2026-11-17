"""OxiDNS public settings/editor wrappers and K20 per-field error recipes."""
import copy
import json
from concurrent.futures import ThreadPoolExecutor
import threading
import time
import re

from harness import Pair, DesignError, json_body, send
from compare import Difference, database_diffs, file_diffs, response_diffs
from inventory import routes
from cases.helpers import profile_policy
from cases.setup import Stable
from oxidns import check as etag_check, capture as etag_capture

MATRIX = routes()
SCOPES = [('/api/oxidns', '/api/oxidns'), ('/api/profiles/<profile_id>/oxidns', '/api/profiles/default/oxidns')]


def require_message(expected):
    def check(reply):
        if reply.json().get('message') != expected:
            raise DesignError('OxiDNS fixture hit wrong guard: ' + repr(reply.json()))
    return check


def settings(binary, recorder):
    for template, path in SCOPES:
        get_route, post_route = 'GET ' + template + '/settings', 'POST ' + template + '/settings'
        get_id, post_id = MATRIX[get_route], MATRIX[post_route]
        with Pair(binary, recorder) as pair:
            pair.policy = profile_policy(pair)
            replies = pair.request(get_id + '-S01', get_route, path + '/settings', expect=200,
                                   writes=True, check=etag_check(pair), capture=etag_capture(pair))
            base, original = replies[0].json(), replies[0].headers['etag']
            modified = {**base, 'cache_size': base['cache_size'] + 1}
            result = pair.request(post_id + '-S05', post_route, path + '/settings', body=json_body(modified),
                                  headers={'If-Match': original}, expect=200, writes=True,
                                  check=etag_check(pair), capture=etag_capture(pair))
            previous_guards = [r.json().get('publish_guard') if r.status == 200 else None for r in result]
            pair.request(post_id + '-F09', post_route, path + '/settings', body=json_body(base),
                         headers={'If-Match': original}, expect=409, writes=True)
            result = pair.request(post_id + '-S06', post_route, path + '/settings', body=json_body(base),
                                  headers={'If-Match': 'W/' + result[0].headers['etag']}, expect=200, writes=True,
                                  check=etag_check(pair), capture=etag_capture(pair))
            next_guards = [r.json().get('publish_guard') if r.status == 200 else None for r in result]
            if next_guards[0]['expected_config_revision'] != previous_guards[0]['expected_config_revision'] + 1:
                raise DesignError('Python successful second save did not expire the old publish guard')
            available = pair.route_available(post_route)
            diffs = []
            if available and (not previous_guards[1] or not next_guards[1] or
                              next_guards[1]['expected_config_revision'] != previous_guards[1]['expected_config_revision'] + 1):
                diffs = [Difference('publish_guard.chain', 'old-guard-not-expired', '<revision + 1>', next_guards[1])]
            recorder.record('T30', template + '/committed-guard-chain', post_route, 'T30',
                            'go-unimplemented' if not available else 'different' if diffs else 'compared', diffs)
            for case, body, headers in [('S01', base, {}), ('S03', base, {}), ('S04', base, {'If-Match': '*'}),
                                         ('B01', {}, {}), ('B02', {**base, 'custom_hosts': '合成.example: 192.0.2.1 # 🧪'}, {}),
                                         ('B03', base, {})]:
                result = pair.request(post_id + '-' + case, post_route, path + '/settings', body=json_body(body),
                                      headers=headers, expect=200, writes=True, check=etag_check(pair), capture=etag_capture(pair))
            pair.request(post_id + '-B91', post_route, path + '/settings', body=json_body(base),
                         headers={'If-Match': '"a", "b"'}, expect=409, writes=True, variant='list-is-one-string')
            pair.request(post_id + '-B91', post_route, path + '/settings', body=json_body(base),
                         headers={'If-Match': '  W/' + result[0].headers['etag'] + '  '}, expect=200, writes=True,
                         check=etag_check(pair), capture=etag_capture(pair), variant='trim-weak')
            for case, body in [('F08', []), ('F10', {**base, 'cache_size': True})]:
                pair.request(post_id + '-' + case, post_route, path + '/settings', body=json_body(body), expect=400, writes=True)
        fixture = {'profile': {'oxidns': {'config_mode': 'native', 'native_config': 'plugins: []\n',
                   'synthetic_extra': {'浮点': [1.25, 1e20, 1e-7, -0.0], '大整数': 9223372036854775809, '文本': '🧪 <>&'}}}}
        with Pair(binary, recorder, options=fixture) as pair:
            pair.policy = profile_policy(pair)
            result = pair.request(get_id + '-B90', get_route, path + '/settings', expect=200,
                                  check=etag_check(pair), capture=etag_capture(pair), writes=True)
            pair.request(post_id + '-B90', post_route, path + '/settings', body=json_body(result[0].json()),
                         headers={'If-Match': result[0].headers['etag']}, expect=200, writes=True,
                         check=etag_check(pair), capture=etag_capture(pair))
            pair.request(get_id + '-S02', get_route, path + '/settings', expect=200, check=etag_check(pair), capture=etag_capture(pair))
            pair.request(post_id + '-S02', post_route, path + '/settings', body=json_body({'config_mode': 'native', 'native_config': 'plugins: []\n'}),
                         expect=200, writes=True, check=etag_check(pair), capture=etag_capture(pair))
        for case, changed in [('F08', {'config_mode': 'invalid'}), ('B01', {'config_mode': 'native', 'native_config': ''})]:
            with Pair(binary, recorder, options={'profile': {'oxidns': changed}}) as pair:
                pair.request(get_id + '-' + case, get_route, path + '/settings', expect=400, writes=True)


def wrappers(binary, recorder):
    suffixes = ('render', 'editor', 'validate', 'template-preview', 'common-settings', 'common-settings/apply', 'native-settings/apply')
    for template, path in SCOPES:
        with Pair(binary, recorder, options={'x01_native': True}) as pair:
            get_route = 'GET ' + template + '/settings'
            reply = pair.request(MATRIX[get_route] + '-S02', get_route, path + '/settings', expect=200, variant=template + '/editor-fixture')
            base = reply[0].json()
            for suffix in suffixes:
                route = 'POST ' + template + '/' + suffix
                rid = MATRIX[route]
                for label, body in [('missing', {}), ('array-settings', {'settings': []}), ('null-settings', {'settings': None})]:
                    pair.request(rid + '-F08', route, path + '/' + suffix, body=json_body(body),
                                 expect=400, check=require_message('请提供 settings 配置对象'), writes=True, variant=label)
                pair.request(rid + '-F09', route, path + '/' + suffix,
                             body=json_body({'settings': base, 'base_url': 1}), expect=400,
                             check=require_message('base_url 必须为文本'), writes=True)
            common = 'POST ' + template + '/common-settings/apply'
            for value in ({}, [], None):
                pair.request('H369', common, path + '/common-settings/apply',
                             body=json_body({'settings': base, 'changes': value, 'base_url': 'http://panel.example'}),
                             expect=400, check=require_message('changes 必须为非空参数对象'), writes=True,
                             variant=template + '/' + repr(value))


def put(settings, dotted, value):
    target = settings
    keys = dotted.split('.')
    for key in keys[:-1]:
        target = target[key]
    target[keys[-1]] = value


def recipes():
    """Each vector names its Python guard and exact intended message."""
    out = []
    def add(case, path, value, message, enable=None, label=None):
        out.append((case, path, value, message, enable or {}, label or repr(value)))
    integer_paths = [(key, 1, 256 if key == 'worker_threads' else 2**31-1, {}) for key in
        ('worker_threads', 'cache_size', 'cache_dump_interval', 'probe_cache_size', 'local_concurrent',
         'remote_concurrent', 'fallback_concurrent', 'fallback_threshold')]
    integer_paths += [(key, 0, 2**31-1, {}) for key in ('cache_lazy_ttl', 'remote_ttl')]
    integer_paths += [('log.max_files', 0, 2**31-1, {})]
    integer_paths += [('query_recorder.args.' + key, 0 if key == 'memory_tail' else 1, 2**31-1,
                      {'query_recorder.enabled': True}) for key in ('queue_size', 'batch_size', 'flush_interval_ms',
                       'retention_days', 'cleanup_interval_hours', 'reader_concurrency', 'memory_tail')]
    integer_paths += [('ip_selector.args.' + key, 0 if key in ('top_n', 'probe_stagger') else 1, 2**31-1,
                      {'ip_selector.enabled': True}) for key in ('top_n', 'probe_stagger', 'probe_timeout', 'max_wait', 'max_parallel_probes')]
    integer_paths += [('dynamic_domain_set.args.' + key, 1, 2**31-1, {'dynamic_domain_set.enabled': True})
                     for key in ('queue_size', 'batch_size', 'flush_interval_ms')]
    ros = {'address': '192.0.2.1:8728', 'username': 'synthetic-user', 'password': 'synthetic-pass'}
    for section in ('ros_address_list', 'ros_route'):
        enable = {section + '.enabled': True, **{section + '.args.' + k: v for k, v in ros.items()}}
        integer_paths += [(section + '.args.' + key, 0, 2**31-1, enable) for key in ('min_ttl', 'max_ttl')]
    for path, minimum, maximum, enable in integer_paths:
        message = f'{path.split(".")[-1]} 必须为 {minimum}–{maximum} 的整数'
        for value in (minimum-1, maximum+1, True, False, 60.0, '60', None, float('nan'), 'CONFIGDIFF_RAW_EXPONENT'):
            add('H511', path, value, message, enable)
    for key in ('tcp_enabled', 'prefer_ipv4', 'metrics_enabled', 'aggregate_rules', 'always_standby', 'cache_enabled', 'cache_dump_enabled', 'probe_cache_enabled'):
        for value in (1, 'true', None):
            add('H512', key, value, key + ' 必须为布尔值')
    for key in ('direct_rulesets', 'proxy_rulesets', 'direct_rules', 'proxy_rules'):
        for value in (None, 'id', [1]):
            add('H513', key, value, key + ' 必须为规则 ID 数组')
    for suffix in ('rules', 'rulesets'):
        add('H514', 'direct_' + suffix, ['r'], '同一规则不能同时分配给直连和代理', {'proxy_' + suffix: ['r']})
    for key in ('local_dns', 'remote_dns', 'fallback_dns', 'listen', 'custom_hosts', 'custom_config'):
        add('H515', key, None, key + ' 必须为文本')
    for case, path, value, message in [
        ('H509', 'config_mode', 'invalid', 'OxiDNS 配置模式必须为 template 或 native'),
        ('H510', 'native_config', None, 'OxiDNS 完整配置必须为 YAML 文本'),
        ('H516', 'listen', ' ', 'DNS 监听地址不能为空'), ('H517', 'default_forward', 'invalid', '默认转发目标无效'),
        ('H518', 'custom_match_position', 'invalid', '自定义匹配执行顺序无效'),
        ('H519', 'custom_matches', {}, '自定义匹配必须为数组'),
        ('H520', 'custom_matches', [1], '自定义匹配条目必须为对象'),
        ('H521', 'custom_matches', [{'matches': []}], '启用的自定义匹配需要至少一个条件'),
        ('H522', 'custom_matches', [{'matches': ['qname example.com', 1], 'exec': 'accept'}], '自定义匹配条件和执行动作必须为非空文本'),
        ('H522', 'custom_matches', [{'matches': ['qname example.com'], 'exec': ''}], '自定义匹配条件和执行动作必须为非空文本')]:
        add(case, path, value, message)
    for value in (None, {}, [True], [0], [65536], [1.0], ['1']):
        add('H523', 'block_qtypes', value, '屏蔽查询类型必须为 1–65535 的整数数组')
    sections = ('api', 'log', 'query_recorder', 'ip_selector', 'dynamic_domain_set', 'adguard', 'ros_address_list', 'ros_route', 'rule_update')
    for name in sections:
        add('H524', name, [], name + ' 必须为对象')
        if name != 'log':
            add('H525', name + '.enabled', 1, name + '.enabled 必须为布尔值')
        add('H526', name + '.args', [], name + '.args 必须为对象')
    for key in ('listen', 'username', 'password', 'webui_root'):
        add('H527', 'api.' + key, None, 'api.' + key + ' 必须为文本')
    for case, path, value, message, enable in [
        ('H528', 'api.listen', '', 'API 监听地址不能为空', {}),
        ('H529', 'api.username', 'u', 'API 用户名和密码必须同时设置', {}),
        ('H529', 'api.password', 'p', 'API 用户名和密码必须同时设置', {}),
        ('H530', 'api.ssl', [], 'API TLS 设置必须为对象', {}),
        ('H531', 'api.ssl', {'cert': 'synthetic.pem'}, 'API TLS 证书和私钥必须同时设置', {}),
        ('H532', 'log.file', None, '日志文件路径必须为文本', {}),
        ('H533', 'log.level', 'INVALID', '日志级别无效', {}),
        ('H534', 'log.rotation', 'INVALID', '日志轮转方式无效', {}),
        ('H535', 'query_recorder.args.path', 'queries.db', '查询记录请放在 state/ 下，避免部署覆盖运行数据', {'query_recorder.enabled': True}),
        ('H536', 'ip_selector.args.selection_mode', 'invalid', 'IP 优选模式无效', {'ip_selector.enabled': True}),
        ('H537', 'ip_selector.args.dnssec_policy', 'invalid', 'IP 优选 DNSSEC 策略无效', {'ip_selector.enabled': True}),
        ('H539', 'dynamic_domain_set.route', 'invalid', '动态域名集路由或学习设置无效', {'dynamic_domain_set.enabled': True}),
        ('H539', 'dynamic_domain_set.learn', 1, '动态域名集路由或学习设置无效', {'dynamic_domain_set.enabled': True}),
        ('H540', 'dynamic_domain_set.args.path', 'learned.txt', '动态规则请放在 state/ 下，避免部署覆盖运行数据', {'dynamic_domain_set.enabled': True}),
        ('H541', 'dynamic_domain_set.learn_args', [], '学习参数必须为对象', {'dynamic_domain_set.enabled': True}),
        ('H542', 'dynamic_domain_set.args.bootstrap_rules', [1], '动态域名初始规则必须为文本数组', {'dynamic_domain_set.enabled': True}),
        ('H543', 'dynamic_domain_set.learn_args.phase', 'invalid', '动态学习阶段或问题范围无效', {'dynamic_domain_set.enabled': True}),
        ('H543', 'dynamic_domain_set.learn_args.questions', 'invalid', '动态学习阶段或问题范围无效', {'dynamic_domain_set.enabled': True}),
        ('H544', 'dynamic_domain_set.learn_args.rule_kind', 'invalid', '动态学习规则类型或错误行为无效', {'dynamic_domain_set.enabled': True}),
        ('H544', 'dynamic_domain_set.learn_args.error_mode', 'invalid', '动态学习规则类型或错误行为无效', {'dynamic_domain_set.enabled': True}),
        ('H545', 'adguard.mode', 'invalid', 'AdGuard 拦截响应模式无效', {'adguard.enabled': True}),
        ('H546', 'adguard.content', [], 'AdGuard 规则必须为文本和订阅 URL 数组', {'adguard.enabled': True}),
        ('H546', 'adguard.urls', {}, 'AdGuard 规则必须为文本和订阅 URL 数组', {'adguard.enabled': True, 'adguard.content': '||ads.example^'}),
        ('H547', 'adguard.content', '', '启用 AdGuard 时请填写规则或订阅 URL', {'adguard.enabled': True}),
        ('H548', 'adguard.urls', ['ftp://ads.example/rules'], 'AdGuard 订阅必须为 HTTP(S) URL', {'adguard.enabled': True}),
        ('H554', 'rule_update.args.schedule', '0 * * *', '规则更新需要五段 cron 表达式', {'rule_update.enabled': True})]:
        add(case, path, value, message, enable)
    for value in ([], 'tcp:443', [1]):
        add('H538', 'ip_selector.args.probe_methods', value, 'IP 探测方式必须为非空文本数组', {'ip_selector.enabled': True})
    for section in ('ros_address_list', 'ros_route'):
        enable = {section + '.enabled': True, **{section + '.args.' + k: v for k, v in ros.items()}}
        for key in ros:
            add('H549', section + '.args.' + key, '', section + ' 需要 RouterOS 地址、用户名和密码', enable)
        add('H550', section + '.args.tls', [], 'RouterOS TLS 必须为对象，关闭时删除 tls 字段', enable)
        add('H551', section + '.args.tls', {'ca': 'synthetic.pem', 'insecure': True}, 'RouterOS TLS 不能同时设置 CA 和跳过验证', enable)
        add('H552', section + '.args.min_ttl', 100, 'RouterOS 最小 TTL 不能大于最大 TTL', {**enable, section + '.args.max_ttl': 99})
    for path, value in [('ros_route.args.routing_table', ''), ('ros_route.args.gateway4', '')]:
        enable = {'ros_route.enabled': True, 'ros_route.args.routing_table': 'synthetic-table', 'ros_route.args.gateway4': '192.0.2.2',
                  **{'ros_route.args.' + k: v for k, v in ros.items()}}
        add('H553', path, value, 'RouterOS 路由需要已存在的路由表和至少一个网关', enable)
    for path, enable, label in [('cache_dump_file', {'cache_dump_enabled': True}, '缓存文件'),
                                ('query_recorder.args.path', {'query_recorder.enabled': True}, '文件路径'),
                                ('dynamic_domain_set.args.path', {'dynamic_domain_set.enabled': True}, '文件路径')]:
        for value in ('', None, 'bad\\path', 'bad\npath'):
            add('H502', path, value, label + '无效', enable)
        for value in ('/absolute', '../escape', 'a//b', '././file'):
            add('H503', path, value, label + '必须为安全的相对路径', enable)
    for path in ('rule_actions', 'rule_dns_parked'):
        for value in (None, []):
            add('H504', path, value, path + ' 必须为以规则 ID 为键的对象')
    for value in (None, 'r', [1]):
        add('H505', 'rule_dns_follow', value, 'rule_dns_follow 必须为规则 ID 数组')
    add('H506', 'rule_dns_follow', ['r'], '规则 r 已设置 DNS 处理「拒绝」，不能同时跟随策略', {'rule_actions': {'r': {'action': 'refused'}}})
    add('H507', 'rule_dns_follow', ['r'], '规则 r 已暂存停用前的 DNS 处理，不能同时跟随策略', {'rule_dns_parked': {'r': {'action': 'direct'}}})
    return out


def normalization_guards(binary, recorder):
    for template, path in SCOPES:
        with Pair(binary, recorder) as pair:
            get_route = 'GET ' + template + '/settings'
            reply = pair.request(MATRIX[get_route] + '-S01', get_route, path + '/settings', expect=200,
                                 variant=template + '/normalizer-base')
            base = reply[0].json()
            malformed = pair.request(MATRIX['POST ' + template + '/render'] + '-F08',
                'POST ' + template + '/render', path + '/render',
                body='{', expect=400, writes=True, interaction=Stable(pair),
                check=require_message('请提供 settings 配置对象'),
                variant=template + '/nonfinite-malformed-control')[0]
            for case, field, value, message, enable, label in recipes():
                altered = copy.deepcopy(base)
                for key, item in enable.items():
                    put(altered, key, item)
                put(altered, field, value)
                body = json_body({'settings': altered, 'base_url': 'http://panel.example'})
                if value == 'CONFIGDIFF_RAW_EXPONENT':
                    if body.count('"CONFIGDIFF_RAW_EXPONENT"') != 1:
                        raise DesignError('exponent JSON fixture is ambiguous')
                    body = body.replace('"CONFIGDIFF_RAW_EXPONENT"', '6e1')
                pair.request(case, 'POST ' + template + '/render', path + '/render', body=body,
                             expect=400, check=require_message(message), writes=True,
                             interaction=Stable(pair) if label == 'nan' else None,
                             capture=(lambda replies, policy: response_diffs(malformed, replies[1], policy)) if label == 'nan' else None,
                             variant=template + '/' + field + '/' + label)


def concurrent_etag(binary, recorder):
    for template, path in SCOPES:
        with Pair(binary, recorder) as pair:
            pair.policy = profile_policy(pair)
            get_route, route = 'GET ' + template + '/settings', 'POST ' + template + '/settings'
            initial = pair.request(MATRIX[get_route] + '-S01', get_route, path + '/settings', expect=200,
                                   variant='concurrent-prepare', check=etag_check(pair), capture=etag_capture(pair))
            body = initial[0].json()
            body['cache_size'] += 1
            old = initial[0].headers['etag']
            results = []
            available = pair.route_available(route)
            for index, base in enumerate(pair.bases):
                if index and not available:
                    break
                barrier = threading.Barrier(2)
                def request(_):
                    barrier.wait(timeout=5)
                    return send(base, 'POST', path + '/settings', json_body(body),
                                {'Authorization': 'Bearer ' + pair.tokens[index], 'Content-Type': 'application/json',
                                 'If-Match': old, 'X-ConfigFlow-Profile': 'other', 'Host': 'panel.example'})
                # Non-scoped route should target default, scoped must override
                # the conflicting header; set the former explicitly to default.
                if '<profile_id>' not in template:
                    def request(_):
                        barrier.wait(timeout=5)
                        return send(base, 'POST', path + '/settings', json_body(body),
                                    {'Authorization': 'Bearer ' + pair.tokens[index], 'Content-Type': 'application/json',
                                     'If-Match': old, 'X-ConfigFlow-Profile': 'default', 'Host': 'panel.example'})
                with ThreadPoolExecutor(max_workers=2) as pool:
                    replies = list(pool.map(request, range(2)))
                results.append(sorted(replies, key=lambda r: r.status))
            if [r.status for r in results[0]] != [200, 409]:
                raise DesignError('Python same-ETag race did not yield exactly one success and one conflict')
            pair.policy.end = time.time()
            diffs = []
            if available:
                for left, right in zip(*results):
                    diffs += response_diffs(left, right, pair.policy)
                diffs += database_diffs(*pair.dirs, pair.policy) + file_diffs(*pair.dirs, pair.policy)
            recorder.record(MATRIX[route] + '-F11', 'concurrent-identical-new-body', route, 'F',
                            'go-unimplemented' if not available else 'different' if diffs else 'compared', diffs,
                            python_statuses=[r.status for r in results[0]], go_statuses=[r.status for r in results[1]] if available else None)


def integer_boundaries(binary, recorder):
    fields = {}
    for case, path, value, message, enable, label in recipes():
        if case == 'H511' and path not in fields:
            limits = re.search(r'必须为 ([0-9]+)–([0-9]+) 的整数', message)
            fields[path] = (int(limits[1]), int(limits[2]), enable)
    vectors = [(template, path, field, bound, value, enable) for template, path in SCOPES
               for field, (minimum, maximum, enable) in fields.items()
               for bound, value in [('minimum', minimum), ('maximum', maximum)]]
    def execute(vector):
        template, path, field, bound, value, enable = vector
        with Pair(binary, recorder) as pair:
            pair.policy = profile_policy(pair)
            # Start with the same public defaults. Mutate only the selected field
            # plus prerequisites required to reach that active plugin's guard.
            initial = send(pair.bases[0], 'GET', path + '/settings', headers={
                'Authorization': 'Bearer ' + pair.tokens[0], 'X-ConfigFlow-Profile': 'default'})
            if initial.status != 200:
                raise DesignError('Python integer boundary settings fixture unavailable')
            body = initial.json()
            for key, item in enable.items():
                put(body, key, item)
            if field.startswith('ros_route.'):
                put(body, 'ros_route.args.routing_table', 'synthetic-table')
                put(body, 'ros_route.args.gateway4', '192.0.2.2')
            if field.endswith('.min_ttl'):
                put(body, field.replace('min_ttl', 'max_ttl'), max(value, 3600))
            if field.endswith('.max_ttl'):
                put(body, field.replace('max_ttl', 'min_ttl'), min(value, 60))
            put(body, field, value)
            def check(reply):
                etag_check(pair)(reply)
                item = reply.json()['settings']
                for key in field.split('.'):
                    item = item[key]
                if type(item) is not int or item != value:
                    raise DesignError('Python did not persist the exact integer boundary')
            route = 'POST ' + template + '/settings'
            pair.request(MATRIX[route] + '-S01', route, path + '/settings', body=json_body(body),
                         expect=200, writes=True, check=check, capture=etag_capture(pair),
                         variant='integer-boundary/' + field + '/' + bound)
    # Independent DATA_DIRs/ports/processes; concurrency is remote only. No agent
    # delegation or shared application state. Each input still gets a fresh seed.
    with ThreadPoolExecutor(max_workers=4) as pool:
        list(pool.map(execute, vectors))


GROUPS = {'oxidns-settings': settings, 'oxidns-wrappers': wrappers,
          'oxidns-normalization-guards': normalization_guards, 'oxidns-concurrent-etag': concurrent_etag}
GROUPS['oxidns-integer-boundaries'] = integer_boundaries
