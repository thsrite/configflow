"""OxiDNS profile settings shared by HTTP, MCP and configuration generation."""
from copy import deepcopy
from pathlib import PurePosixPath
import re


class OxidnsSettingsError(ValueError):
    pass


# The SOA lets clients cache the empty answer; ``reject 0`` carries none, so
# every lookup of a blocked type returned to OxiDNS. Earlier drafts keep it.
BLOCK_QTYPES_EXEC = 'black_hole nodata short_circuit=true'
BLOCK_QTYPES_EXECS = (BLOCK_QTYPES_EXEC, 'reject 0')


def get_default_oxidns_settings():
    return {
        'config_mode': 'template', 'native_config': '', 'additional_files': [],
        'direct_rulesets': [], 'proxy_rulesets': [], 'direct_rules': [], 'proxy_rules': [],
        'local_dns': '', 'remote_dns': '', 'fallback_dns': '',
        'default_forward': 'forward_remote', 'custom_hosts': '', 'custom_config': '',
        'custom_matches': [], 'custom_match_position': 'tail', 'device_policies': [],
        'worker_threads': 2, 'listen': '0.0.0.0:53', 'tcp_enabled': True,
        'prefer_ipv4': True, 'block_qtypes': [65, 255], 'metrics_enabled': True,
        'aggregate_rules': True, 'local_concurrent': 2, 'remote_concurrent': 1,
        'fallback_concurrent': 2, 'fallback_threshold': 500, 'always_standby': False,
        'remote_ttl': 1, 'cache_enabled': True, 'cache_size': 10240,
        'cache_lazy_ttl': 3600, 'cache_dump_enabled': False,
        'cache_dump_file': './state/cache.dump', 'cache_dump_interval': 300,
        'probe_cache_enabled': True, 'probe_cache_size': 2048,
        'api': {'enabled': True, 'listen': '127.0.0.1:9199', 'username': '',
                'password': '', 'webui_root': '', 'ssl': {}},
        'log': {'level': 'info', 'file': '', 'rotation': 'daily', 'max_files': 7},
        'query_recorder': {'enabled': False, 'args': {
            'path': './state/queries.db', 'queue_size': 8192, 'batch_size': 256,
            'flush_interval_ms': 200, 'memory_tail': 1024, 'retention_days': 7,
            'cleanup_interval_hours': 1, 'reader_concurrency': 2}},
        'ip_selector': {'enabled': False, 'args': {
            'selection_mode': 'background', 'probe_methods': ['tcp:443', 'tcp:80'],
            'probe_stagger': 200, 'probe_timeout': 600, 'max_wait': 1000, 'top_n': 0,
            'dnssec_policy': 'reorder_only', 'max_parallel_probes': 64,
            'cache': {'enabled': True, 'size': 4096, 'ttl': 3600, 'failure_ttl': 60}}},
        'dynamic_domain_set': {'enabled': False, 'route': 'direct', 'learn': False,
            'args': {'path': './state/learned-domains.txt', 'bootstrap_rules': [],
                     'queue_size': 1024, 'batch_size': 256, 'flush_interval_ms': 200},
            'learn_args': {'phase': 'after', 'questions': 'first', 'qtypes': ['A', 'AAAA'],
                           'success_only': True, 'answer_required': True, 'rule_kind': 'full',
                           'async': True, 'error_mode': 'continue', 'timeout': '1s'}},
        'adguard': {'enabled': False, 'content': '', 'urls': [], 'mode': 'nxdomain'},
        'ros_address_list': {'enabled': False, 'args': {
            'address': '', 'username': '', 'password': '', 'async': True,
            'address_list4': 'oxidns', 'address_list6': 'oxidns',
            'min_ttl': 60, 'max_ttl': 3600, 'queue_capacity': 16384,
            'comment_prefix': 'configflow', 'cleanup_on_shutdown': True}},
        'ros_route': {'enabled': False, 'args': {
            'address': '', 'username': '', 'password': '', 'async': True,
            'routing_table': '', 'gateway4': '', 'gateway6': '', 'distance': 100,
            'min_ttl': 60, 'max_ttl': 3600, 'queue_capacity': 16384,
            'comment_prefix': 'configflow', 'cleanup_on_shutdown': True}},
        'rule_update': {'enabled': False, 'args': {
            'schedule': '30 3 * * *', 'timezone': 'Asia/Shanghai', 'timeout': '60s',
            'outbound': '', 'socks5': ''}},
        # Per-rule DNS handling: explicit actions (generation reads these),
        # follow-the-policy intent, and native-mode choices of disabled rules.
        'rule_actions': {}, 'rule_dns_follow': [], 'rule_dns_parked': {},
    }


def _merge(defaults, value):
    result = deepcopy(defaults)
    for key, item in value.items():
        if isinstance(result.get(key), dict) and isinstance(item, dict):
            result[key] = _merge(result[key], item)
        else:
            result[key] = deepcopy(item)
    return result


def safe_relative_path(value, label='文件路径'):
    if not isinstance(value, str) or not value or '\\' in value or any(ord(c) < 32 for c in value):
        raise OxidnsSettingsError(f'{label}无效')
    normalized = value.removeprefix('./')
    path = PurePosixPath(normalized)
    if path.is_absolute() or any(part in ('', '.', '..') for part in normalized.split('/')):
        raise OxidnsSettingsError(f'{label}必须为安全的相对路径')
    return path.as_posix()


def _normalize_rule_dns(settings):
    """Shape and disjointness of the per-rule DNS fields, in both modes.

    A null value deletes its key; that is the only way a recursive merge (MCP)
    can remove one. rule_actions wins over the selector lists by definition, so
    overlap with them is allowed.
    """
    from backend.common.rule_dns import ROUTE_LABELS, normalize_action
    for key, parked in (('rule_actions', False), ('rule_dns_parked', True)):
        value = settings[key]
        if not isinstance(value, dict) or any(not isinstance(identity, str) for identity in value):
            raise OxidnsSettingsError(f'{key} 必须为以规则 ID 为键的对象')
        settings[key] = {identity: normalize_action(action, parked=parked)
                         for identity, action in value.items() if action is not None}
    follow = settings['rule_dns_follow']
    if not isinstance(follow, list) or any(not isinstance(identity, str) for identity in follow):
        raise OxidnsSettingsError('rule_dns_follow 必须为规则 ID 数组')
    settings['rule_dns_follow'] = list(dict.fromkeys(follow))
    for identity in settings['rule_dns_follow']:
        if identity in settings['rule_actions']:
            label = ROUTE_LABELS[settings['rule_actions'][identity]['action']]
            raise OxidnsSettingsError(f'规则 {identity} 已设置 DNS 处理「{label}」，不能同时跟随策略')
        if identity in settings['rule_dns_parked']:
            raise OxidnsSettingsError(f'规则 {identity} 已暂存停用前的 DNS 处理，不能同时跟随策略')


def normalize_oxidns_settings(value=None, *, validate_native_arguments=True):
    if value is None:
        value = {}
    if not isinstance(value, dict):
        raise OxidnsSettingsError('OxiDNS 设置必须是对象')
    settings = _merge(get_default_oxidns_settings(), value)
    from backend.common.oxidns_device_policy import normalize_policies
    settings['device_policies'] = normalize_policies(settings['device_policies'])
    if settings['config_mode'] not in ('template', 'native'):
        raise OxidnsSettingsError('OxiDNS 配置模式必须为 template 或 native')
    if not isinstance(settings['native_config'], str):
        raise OxidnsSettingsError('OxiDNS 完整配置必须为 YAML 文本')
    from backend.common.oxidns_native import managed_files, native_document
    managed_files(settings['additional_files'])
    _normalize_rule_dns(settings)
    if settings['config_mode'] == 'native':
        if validate_native_arguments:
            native_document(settings)
        else:
            # A stored draft may be incomplete; only enforce safe YAML and
            # bounded, explicitly managed attachment data at this boundary.
            from backend.common.oxidns_native import load_native_yaml
            load_native_yaml(settings['native_config'])
        # Retain the template draft without validating inactive template fields.
        return settings

    def integer(obj, key, minimum=1, maximum=2**31-1):
        item = obj.get(key)
        if isinstance(item, bool) or not isinstance(item, int) or not minimum <= item <= maximum:
            raise OxidnsSettingsError(f'{key} 必须为 {minimum}–{maximum} 的整数')

    for key in ('worker_threads', 'cache_size', 'cache_dump_interval', 'probe_cache_size',
                'local_concurrent', 'remote_concurrent', 'fallback_concurrent', 'fallback_threshold'):
        integer(settings, key, maximum=256 if key == 'worker_threads' else 2**31-1)
    for key in ('cache_lazy_ttl', 'remote_ttl'):
        integer(settings, key, minimum=0)
    for key in ('tcp_enabled', 'prefer_ipv4', 'metrics_enabled', 'aggregate_rules', 'always_standby',
                'cache_enabled', 'cache_dump_enabled', 'probe_cache_enabled'):
        if not isinstance(settings[key], bool):
            raise OxidnsSettingsError(f'{key} 必须为布尔值')
    for key in ('direct_rulesets', 'proxy_rulesets', 'direct_rules', 'proxy_rules'):
        if not isinstance(settings[key], list) or any(not isinstance(v, str) for v in settings[key]):
            raise OxidnsSettingsError(f'{key} 必须为规则 ID 数组')
        settings[key] = list(dict.fromkeys(settings[key]))
    for suffix in ('rules', 'rulesets'):
        if set(settings[f'direct_{suffix}']) & set(settings[f'proxy_{suffix}']):
            raise OxidnsSettingsError('同一规则不能同时分配给直连和代理')
    for key in ('local_dns', 'remote_dns', 'fallback_dns', 'listen', 'custom_hosts', 'custom_config'):
        if not isinstance(settings[key], str):
            raise OxidnsSettingsError(f'{key} 必须为文本')
    if not settings['listen'].strip():
        raise OxidnsSettingsError('DNS 监听地址不能为空')
    if settings['default_forward'] not in ('forward_local', 'forward_remote'):
        raise OxidnsSettingsError('默认转发目标无效')
    if settings['custom_match_position'] not in ('head', 'tail'):
        raise OxidnsSettingsError('自定义匹配执行顺序无效')
    if not isinstance(settings['custom_matches'], list):
        raise OxidnsSettingsError('自定义匹配必须为数组')
    for entry in settings['custom_matches']:
        if not isinstance(entry, dict):
            raise OxidnsSettingsError('自定义匹配条目必须为对象')
        if not entry.get('enabled', True):
            continue
        matches = entry.get('matches', [])
        if isinstance(matches, str):
            matches = matches.splitlines()
        if not isinstance(matches, list) or not any(isinstance(m, str) and m.strip() for m in matches):
            raise OxidnsSettingsError('启用的自定义匹配需要至少一个条件')
        if any(not isinstance(m, str) for m in matches) or not isinstance(entry.get('exec'), str) or not entry['exec'].strip():
            raise OxidnsSettingsError('自定义匹配条件和执行动作必须为非空文本')
    if not isinstance(settings['block_qtypes'], list) or any(type(v) is not int or not 1 <= v <= 65535 for v in settings['block_qtypes']):
        raise OxidnsSettingsError('屏蔽查询类型必须为 1–65535 的整数数组')
    if settings['cache_dump_enabled']:
        safe_relative_path(settings['cache_dump_file'], '缓存文件')
    for name in ('api', 'log', 'query_recorder', 'ip_selector', 'dynamic_domain_set', 'adguard',
                 'ros_address_list', 'ros_route', 'rule_update'):
        section = settings[name]
        if not isinstance(section, dict):
            raise OxidnsSettingsError(f'{name} 必须为对象')
        if name != 'log' and not isinstance(section.get('enabled'), bool):
            raise OxidnsSettingsError(f'{name}.enabled 必须为布尔值')
        if 'args' in section and not isinstance(section['args'], dict):
            raise OxidnsSettingsError(f'{name}.args 必须为对象')
    api = settings['api']
    for key in ('listen', 'username', 'password', 'webui_root'):
        if not isinstance(api.get(key), str):
            raise OxidnsSettingsError(f'api.{key} 必须为文本')
    if api['enabled']:
        if not isinstance(api['listen'], str) or not api['listen'].strip():
            raise OxidnsSettingsError('API 监听地址不能为空')
        if bool(api['username']) != bool(api['password']):
            raise OxidnsSettingsError('API 用户名和密码必须同时设置')
        if not isinstance(api['ssl'], dict):
            raise OxidnsSettingsError('API TLS 设置必须为对象')
        if bool(api['ssl'].get('cert')) != bool(api['ssl'].get('key')):
            raise OxidnsSettingsError('API TLS 证书和私钥必须同时设置')
    log = settings['log']
    if not isinstance(log['file'], str):
        raise OxidnsSettingsError('日志文件路径必须为文本')
    if log['level'] not in ('off', 'trace', 'debug', 'info', 'warn', 'error'):
        raise OxidnsSettingsError('日志级别无效')
    if log['rotation'] not in ('never', 'minutely', 'hourly', 'daily', 'weekly'):
        raise OxidnsSettingsError('日志轮转方式无效')
    integer(log, 'max_files', minimum=0)
    recorder = settings['query_recorder']
    if recorder['enabled']:
        for key in ('queue_size', 'batch_size', 'flush_interval_ms', 'retention_days', 'cleanup_interval_hours', 'reader_concurrency'):
            integer(recorder['args'], key)
        integer(recorder['args'], 'memory_tail', minimum=0)
        if not safe_relative_path(recorder['args']['path']).startswith('state/'):
            raise OxidnsSettingsError('查询记录请放在 state/ 下，避免部署覆盖运行数据')
    selector = settings['ip_selector']
    if selector['enabled']:
        if selector['args'].get('selection_mode') not in ('first_success', 'best_within_budget', 'background'):
            raise OxidnsSettingsError('IP 优选模式无效')
        integer(selector['args'], 'top_n', minimum=0)
        for key in ('probe_stagger', 'probe_timeout', 'max_wait', 'max_parallel_probes'):
            integer(selector['args'], key, minimum=0 if key == 'probe_stagger' else 1)
        if selector['args'].get('dnssec_policy') not in ('reorder_only', 'skip'):
            raise OxidnsSettingsError('IP 优选 DNSSEC 策略无效')
        methods = selector['args'].get('probe_methods')
        if not isinstance(methods, list) or not methods or any(not isinstance(method, str) for method in methods):
            raise OxidnsSettingsError('IP 探测方式必须为非空文本数组')
    dynamic = settings['dynamic_domain_set']
    if dynamic['enabled']:
        if dynamic['route'] not in ('direct', 'proxy') or type(dynamic['learn']) is not bool:
            raise OxidnsSettingsError('动态域名集路由或学习设置无效')
        if not safe_relative_path(dynamic['args']['path']).startswith('state/'):
            raise OxidnsSettingsError('动态规则请放在 state/ 下，避免部署覆盖运行数据')
        if not isinstance(dynamic['learn_args'], dict):
            raise OxidnsSettingsError('学习参数必须为对象')
        for key in ('queue_size', 'batch_size', 'flush_interval_ms'):
            integer(dynamic['args'], key)
        bootstrap = dynamic['args'].get('bootstrap_rules')
        if not isinstance(bootstrap, list) or any(not isinstance(item, str) for item in bootstrap):
            raise OxidnsSettingsError('动态域名初始规则必须为文本数组')
        learn = dynamic['learn_args']
        if learn.get('phase') not in ('before', 'after') or learn.get('questions') not in ('first', 'all'):
            raise OxidnsSettingsError('动态学习阶段或问题范围无效')
        if learn.get('rule_kind') not in ('full', 'domain') or learn.get('error_mode') not in ('continue', 'stop', 'fail'):
            raise OxidnsSettingsError('动态学习规则类型或错误行为无效')
    adguard = settings['adguard']
    if adguard['enabled']:
        if adguard['mode'] not in ('nxdomain', 'nodata', 'null', 'refused'):
            raise OxidnsSettingsError('AdGuard 拦截响应模式无效')
        if not isinstance(adguard['content'], str) or not isinstance(adguard['urls'], list):
            raise OxidnsSettingsError('AdGuard 规则必须为文本和订阅 URL 数组')
        if not adguard['content'].strip() and not adguard['urls']:
            raise OxidnsSettingsError('启用 AdGuard 时请填写规则或订阅 URL')
        if any(not isinstance(url, str) or not re.match(r'^https?://', url) for url in adguard['urls']):
            raise OxidnsSettingsError('AdGuard 订阅必须为 HTTP(S) URL')
    for name in ('ros_address_list', 'ros_route'):
        section = settings[name]
        if section['enabled']:
            args = section['args']
            if any(not isinstance(args.get(key), str) or not args[key].strip() for key in ('address', 'username', 'password')):
                raise OxidnsSettingsError(f'{name} 需要 RouterOS 地址、用户名和密码')
            if 'tls' in args:
                if not isinstance(args['tls'], dict):
                    raise OxidnsSettingsError('RouterOS TLS 必须为对象，关闭时删除 tls 字段')
                if args['tls'].get('ca') and args['tls'].get('insecure'):
                    raise OxidnsSettingsError('RouterOS TLS 不能同时设置 CA 和跳过验证')
            integer(args, 'min_ttl', minimum=0)
            integer(args, 'max_ttl', minimum=0)
            if args['min_ttl'] > args['max_ttl']:
                raise OxidnsSettingsError('RouterOS 最小 TTL 不能大于最大 TTL')
            if name == 'ros_route' and (not args.get('routing_table') or not (args.get('gateway4') or args.get('gateway6'))):
                raise OxidnsSettingsError('RouterOS 路由需要已存在的路由表和至少一个网关')
    update = settings['rule_update']
    if update['enabled']:
        schedule = update['args'].get('schedule', '')
        if not isinstance(schedule, str) or len(schedule.split()) != 5:
            raise OxidnsSettingsError('规则更新需要五段 cron 表达式')
    return settings
