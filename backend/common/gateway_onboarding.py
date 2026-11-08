"""Prepare the two service drafts for the explicit Linux gateway installer.

This module never touches an Agent or host networking. Both drafts are validated
before their caller commits them in one profile transaction.
"""
from copy import deepcopy
import ipaddress
import re
from urllib.parse import urlsplit

import yaml

from backend.common.linux_gateway import (
    LinuxGatewayError, direct_overlap_warnings, gateway_editor_state, gateway_settings,
    validate_gateway_selection, validate_rule_set_ids,
)
from backend.common.router_split import (
    MARKER_KEY, MESSAGES, full_tun_base_yaml, is_full_tun, is_full_tun_document, read_marker,
    route_rule_set_ids,
)
from backend.common.oxidns_common import apply_common_settings, read_common_settings
from backend.common.oxidns_settings import normalize_oxidns_settings, OxidnsSettingsError
from backend.common.oxidns_native import native_document, load_native_yaml
from backend.common.oxidns_validation import validate_settings
from backend.converters.oxidns import _forward, generate_oxidns_config
from backend.converters.mihomo import generate_mihomo_config, _default_mihomo_template

DEFAULT_LOCAL_DNS = 'https://223.5.5.5/dns-query'
# Domestic IP-address DoH (AliDNS, DNSPod): Mihomo resolves direct and proxy
# server names with it, needs no bootstrap resolver and is not exposed to plain :53
# hijacking; the first-run wizard writes the same default.
DEFAULT_MIHOMO_NAMESERVERS = ('https://223.5.5.5/dns-query', 'https://1.12.12.12/dns-query')
DNS_TARGET = '127.0.0.1:1053'
INPUT_FIELDS = {'rule_set_ids', 'direct_rule_set_ids', 'proxy_rule_set_ids', 'local_dns',
                'query_recorder', 'confirm_template_conversion', 'expected_config_revision', 'default_forward'}
GATEWAY_CHANGES = [
    '启用当前配置空间的 Linux 网关；fake-IP 网段始终接管，真实 IP 只接管勾选的规则集。',
    'Mihomo 使用 rule / fake-IP 模式，DNS 监听 127.0.0.1:1053；保留现有规则顺序、策略组和节点。',
    'OxiDNS 在 0.0.0.0:53 同时监听 UDP / TCP，优先 IPv4；直连使用所填 IP 地址 DoH / DoT。',
    'OxiDNS 代理和备用上游均为 127.0.0.1:1053；未匹配域名使用已选择的默认 DNS。',
    'OxiDNS 管理 API 未启用时自动开启（仅本机 127.0.0.1:9199，不设账号），供 DNS 面板、规则更新和热重载使用；已启用的监听与认证设置不变。',
    '只保存两份配置草稿；后续分别发布并检查核心，再显式应用主机转发。',
]
ROUTER_SPLIT_CHANGES = [
    'Mihomo 使用完整 TUN 接管主路由转来的连接，不启用 Linux 网关的选择性接管。',
    '安装时开启并持久化 IPv4 转发、准备 TUN，与普通同机安装一致。',
    'Mihomo 使用 rule / fake-IP 模式，DNS 监听 127.0.0.1:1053；保留现有规则顺序、策略组和节点。',
    'OxiDNS 在 0.0.0.0:53 同时监听 UDP / TCP，优先 IPv4；直连使用所填 IP 地址 DoH / DoT。',
    'OxiDNS 代理和备用上游均为 127.0.0.1:1053；未匹配域名使用已选择的默认 DNS。',
    'OxiDNS 管理 API 未启用时自动开启（仅本机 127.0.0.1:9199，不设账号），供 DNS 面板、规则更新和热重载使用；已启用的监听与认证设置不变。',
    '主路由只需转发 fake-IP 网段和所选「主路由转发」规则集；只保存两份配置草稿，之后依次发布并检查转发与 TUN。',
]
LEGACY_TAKEOVER_WARNING = '此配置空间使用旧版 Linux 网关接管，保持不变。'


class GatewayOnboardingError(ValueError):
    pass


class GatewayOnboardingBlocked(GatewayOnboardingError):
    def __init__(self, result):
        self.result = result
        super().__init__(result['blockers'][0])


def _library(config, item):
    return next((entry for entry in config.get('rule_library', [])
                 if entry.get('id') == item.get('library_rule_id')), None)


def _domain_candidates(config, selections):
    candidates = []
    selected = set(selections)
    seen = set()
    for item in config.get('rule_configs', []):
        if item.get('itemType') != 'ruleset':
            continue
        source = _library(config, item) or item
        behavior = source.get('behavior', item.get('behavior', 'domain'))
        if behavior not in ('domain', 'classical') and item.get('id') not in selected:
            continue
        identity = item.get('id')
        seen.add(identity)
        reason = ''
        if item.get('library_rule_id') and not _library(config, item):
            reason = '共享规则库条目已删除'
        elif behavior not in ('domain', 'classical'):
            reason = '现有 IP 分流选择会保留；安装向导只能新增域名或 Classical 规则集'
        elif source.get('format') == 'mrs' or urlsplit(source.get('url', '')).path.lower().endswith('.mrs'):
            reason = 'OxiDNS 转换不支持 MRS，请改用 text / YAML 规则集'
        elif not item.get('enabled', True) or not item.get('library_enabled', True) or not source.get('enabled', True):
            reason = '规则集已停用，不会出现在新配置中'
        candidates.append({'id': identity, 'name': source.get('name', item.get('name', identity)),
                           'behavior': behavior, 'policy': item.get('policy', ''),
                           'enabled': bool(item.get('enabled', True) and item.get('library_enabled', True) and source.get('enabled', True)),
                           'available': not reason, 'reason': reason,
                           'description': '混合规则中的域名和 IP 条件都会参与 OxiDNS 分流' if behavior == 'classical' else '按域名选择直连或代理 DNS'})
    for identity in selections:
        if identity not in seen:
            candidates.append({'id': identity, 'name': identity, 'behavior': '', 'policy': '',
                               'enabled': False, 'available': False, 'reason': '规则集已删除或不属于当前配置空间'})
    return candidates


def _oxidns_values(config, base_url=''):
    settings = normalize_oxidns_settings(config.get('oxidns', {}), validate_native_arguments=False)
    if settings['config_mode'] == 'native':
        return settings, read_common_settings(settings, config, base_url)['values']
    return settings, settings


def _check_native_dns_path(settings, *, allow_disabled_fallback=False):
    """Only patch canonical forward nodes when listeners actually reach them."""
    document = native_document(settings, validate_arguments=False)
    plugins = {item.get('tag'): item for item in document.get('plugins', [])}
    def args(item):
        return item.get('args') if isinstance(item.get('args'), dict) else {}
    for tag in ('server_udp', 'server_tcp'):
        item = plugins.get(tag)
        if item and args(item).get('entry') != 'sequence_main':
            raise OxidnsSettingsError('原生 DNS 监听入口使用自定义处理链，无法确认网关分流路径')
    proxy = plugins.get('proxy_dns_seq', {})
    fallback_enabled = proxy.get('args') == [{'exec': '$fallback'}, {'exec': 'accept'}]
    fallback_disabled = proxy.get('args') in ([{'exec': 'goto remote_dns'}],
                                            [{'exec': '$remote_dns'}, {'exec': 'accept'}])
    if (proxy.get('type') != 'sequence'
            or not (fallback_enabled or allow_disabled_fallback and fallback_disabled)):
        raise OxidnsSettingsError('原生 proxy_dns_seq 使用自定义处理链，无法确认 Mihomo 上游')
    # Newer native templates omit disabled fallback nodes entirely. Preparation
    # may enable that recognized path through the common editor's guarded graph
    # projection; already-saved gateway configurations must have it active.
    if fallback_enabled:
        fallback = plugins.get('fallback', {})
        if (fallback.get('type') != 'fallback' or args(fallback).get('primary') != 'remote_dns'
                or args(fallback).get('secondary') != 'fallback_dns'):
            raise OxidnsSettingsError('原生 fallback 使用自定义处理链，无法确认 Mihomo 代理与备用上游')
        secondary = plugins.get('fallback_dns', {})
        if (secondary.get('type') != 'sequence' or secondary.get('args') != [
                {'exec': '$forward_fallback'}, {'matches': ['!has_resp'], 'exec': 'reject 3'}]):
            raise OxidnsSettingsError('原生 fallback_dns 使用自定义处理链，无法确认 Mihomo 上游')
    remote = plugins.get('remote_dns', {})
    steps = remote.get('args')
    if (remote.get('type') != 'sequence' or not isinstance(steps, list) or steps[:2] != [
            {'exec': '$forward_remote'}, {'matches': ['!has_resp'], 'exec': 'reject 3'}]
            or any(not isinstance(item, dict) or set(item) != {'exec'}
                   or not isinstance(item['exec'], str) or not item['exec'].startswith('ttl ') for item in steps[2:])):
        raise OxidnsSettingsError('原生 remote_dns 使用自定义处理链，无法确认 Mihomo 上游')


def _prepare_native_gateway_fallback(settings):
    """Restore a standard disabled backup only within explicit gateway setup.

    Ordinary OxiDNS generation and both editors retain their existing behavior.
    A user-supplied native document may already have its backup disabled, so the
    gateway projection must reconnect that known graph before editing upstreams.
    """
    from backend.common.oxidns_common_routing import _references

    document = load_native_yaml(settings['native_config'])
    if document.get('include'):
        raise OxidnsSettingsError('原生备用 DNS 含 include，无法安全准备网关上游')
    plugins = {item['tag']: item for item in document.get('plugins', [])}
    tags = ('forward_fallback', 'fallback_dns', 'fallback')
    secondary = [{'exec': '$forward_fallback'}, {'matches': ['!has_resp'], 'exec': 'reject 3'}]
    parked = settings.get('_common_disabled', {})
    parked = parked if isinstance(parked, dict) else {}
    kinds = {'forward_fallback': 'forward', 'fallback_dns': 'sequence', 'fallback': 'fallback'}
    defaults = {'forward_fallback': {'upstreams': [{'addr': DNS_TARGET}], 'concurrent': 1},
                'fallback_dns': secondary,
                'fallback': {'primary': 'remote_dns', 'secondary': 'fallback_dns',
                             'threshold': 500, 'always_standby': False}}
    restored = {}
    for tag in tags:
        item = deepcopy(plugins.get(tag) or parked.get(tag) or
                        {'tag': tag, 'type': kinds[tag], 'args': defaults[tag]})
        args = item.get('args') if isinstance(item, dict) else None
        if (not isinstance(item, dict) or item.get('tag') != tag or item.get('type') != kinds[tag]
                or (tag == 'fallback_dns' and args != secondary)
                or (tag != 'fallback_dns' and not isinstance(args, dict))
                or (tag == 'fallback' and (args.get('primary') != 'remote_dns'
                                          or args.get('secondary') != 'fallback_dns'))
                or (tag == 'forward_fallback' and not isinstance(args.get('upstreams', []), list))):
            raise OxidnsSettingsError('原生备用 DNS 使用自定义插件，无法安全准备网关上游')
        restored[tag] = item

    # Unknown or dynamically expanded edges must not be redirected to Mihomo.
    def dynamic_reference(value, reference=False):
        if isinstance(value, dict):
            return any(dynamic_reference(item, key in
                       ('exec', 'matches', 'entry', 'primary', 'secondary', 'executors', 'provider', 'tag')
                       or key == 'args' and isinstance(item, (str, list))) for key, item in value.items())
        if isinstance(value, list):
            return any(dynamic_reference(item, reference) for item in value)
        return isinstance(value, str) and reference and bool(re.search(r'(?<!\$)\$\{[^}]+\}', value))

    remaining = deepcopy(document)
    for item in remaining.get('plugins', []):
        tag = item.pop('tag', None)
        item.pop('type', None)
        if tag in ('proxy_dns_seq', 'fallback_dns'):
            item.pop('args', None)
        elif tag == 'fallback' and isinstance(item.get('args'), dict):
            item['args'].pop('primary', None)
            item['args'].pop('secondary', None)
    if dynamic_reference(document) or any(_references(remaining, tag) for tag in tags):
        raise OxidnsSettingsError('原生备用 DNS 仍有自定义或动态引用，无法安全准备网关上游')
    if plugins['proxy_dns_seq']['args'] == [{'exec': '$fallback'}, {'exec': 'accept'}]:
        return settings
    for tag in tags:
        if tag not in plugins:
            document['plugins'].append(restored[tag])
    plugins['proxy_dns_seq']['args'] = [{'exec': '$fallback'}, {'exec': 'accept'}]
    result = deepcopy(settings)
    result['native_config'] = yaml.safe_dump(document, allow_unicode=True, sort_keys=False)
    return result


def _local_upstreams(value):
    if not isinstance(value, str) or not value.strip() or len(value.encode('utf-8')) > 16384:
        raise GatewayOnboardingError('请填写直连 DNS，使用 IP 地址的 DoH / DoT，例如 https://223.5.5.5/dns-query')
    try:
        upstreams = _forward(value, [])
    except OxidnsSettingsError as exc:
        raise GatewayOnboardingError(str(exc)) from exc
    if len(upstreams) > 16:
        raise GatewayOnboardingError('直连 DNS 最多支持 16 个上游')
    for upstream in upstreams:
        try:
            parts = urlsplit(upstream['addr'])
            address = ipaddress.ip_address(parts.hostname or '')
            port = parts.port
            if (parts.scheme not in ('https', 'tls') or address.version != 4
                    or address.is_loopback or address.is_unspecified or address.is_multicast
                    or address.is_link_local or parts.username or parts.password
                    or port in (53, 1053) or upstream.get('socks5') or upstream.get('outbound')):
                raise ValueError
            if parts.scheme == 'https' and not parts.path:
                raise ValueError
        except (ValueError, TypeError):
            raise GatewayOnboardingError('直连 DNS 必须使用 IPv4 地址的 DoH / DoT，不能指向本机、53 / 1053 端口或代理出口') from None
    return upstreams


def _input(value):
    if not isinstance(value, dict) or set(value) - INPUT_FIELDS:
        raise GatewayOnboardingError('安装设置必须是 JSON 对象，且只能包含向导字段')
    result = deepcopy(value)
    for key in ('rule_set_ids', 'direct_rule_set_ids', 'proxy_rule_set_ids'):
        ids = result.get(key)
        if (not isinstance(ids, list) or len(ids) > 1024 or any(
                not isinstance(item, str) or not item or len(item) > 256
                or any(ord(c) < 32 or ord(c) == 127 for c in item) for item in ids)):
            raise GatewayOnboardingError(f'{key} 必须为有效规则 ID 数组')
        result[key] = list(dict.fromkeys(ids))
    if set(result['direct_rule_set_ids']) & set(result['proxy_rule_set_ids']):
        raise GatewayOnboardingError('同一域名规则集不能同时选择直连和代理')
    # Optional so older clients keep the profile's current recorder state.
    if type(result.get('query_recorder', False)) is not bool:
        raise GatewayOnboardingError('query_recorder 必须为布尔值')
    if type(result.get('confirm_template_conversion', False)) is not bool:
        raise GatewayOnboardingError('confirm_template_conversion 必须为布尔值')
    if 'expected_config_revision' in result and (type(result['expected_config_revision']) is not int or result['expected_config_revision'] < 0):
        raise GatewayOnboardingError('配置版本无效，请刷新安装向导')
    if result.get('default_forward', 'forward_remote') not in ('forward_remote', 'forward_local'):
        raise GatewayOnboardingError('请选择有效的未匹配域名 DNS')
    _local_upstreams(result.get('local_dns'))
    return result


def _proxy_prerequisites(config, document=None):
    """Check emitted policy reachability, without guessing DNS rule assignments."""
    warnings = []
    if document is None:
        # The read screen remains inexpensive; the preview checks actual output.
        policies = [group['name'] for group in config.get('proxy_groups', [])
                    if group.get('enabled', True) and group.get('name')]
        sources = any(item.get('enabled', True) for key in ('nodes', 'subscriptions', 'subscription_aggregations')
                      for item in config.get(key, []))
        ready = bool(sources and policies)
        return {'ready': ready, 'policies': policies, 'warnings': [], 'verified': False}
    providers = document.get('proxy-providers', {})
    reachable = {proxy.get('name') for proxy in document.get('proxies', [])
                 if proxy.get('type') not in ('direct', 'reject', 'pass') and proxy.get('name')}
    groups = document.get('proxy-groups', [])
    while True:
        additions = {group.get('name') for group in groups if group.get('name') and (
            any(member in reachable for member in group.get('proxies', []))
            or any(name in providers for name in group.get('use', []))
            or group.get('include-all-providers') and bool(providers)
            or group.get('include-all-proxies') and bool(reachable))}
        if additions <= reachable:
            break
        reachable.update(additions)
    policies = sorted(reachable)
    active_rules = [item for item in config.get('rule_configs', []) if item.get('enabled', True) and item.get('library_enabled', True)]
    ready = any(item.get('policy') in reachable for item in active_rules)
    catchall = next((item for item in active_rules if item.get('itemType') == 'rule' and item.get('rule_type') == 'MATCH'), None)
    if not catchall:
        warnings.append('当前未配置 Mihomo MATCH 漏网规则，请在规则页确认未匹配连接的处理方式。')
    elif catchall.get('policy') not in reachable and catchall.get('policy') != 'DIRECT':
        warnings.append('Mihomo MATCH 漏网规则未指向可用代理策略或 DIRECT，请确认未匹配连接的处理方式。')
    return {'ready': ready, 'policies': policies, 'warnings': warnings, 'verified': True}


def onboarding_takeover(repository, profile_id, config):
    """How the installer takes over connections: 'full_tun', 'legacy' or 'selective'."""
    from backend.common.router_split import legacy_context, router_split_state
    state, _ = read_marker(config)
    if state == 'valid':
        return 'full_tun'
    if state == 'invalid':
        raise GatewayOnboardingError(MESSAGES['ROUTER_SPLIT_INVALID'])
    try:
        enabled = gateway_settings(config)['enabled']
    except LinuxGatewayError as exc:
        raise GatewayOnboardingError(str(exc)) from exc
    if enabled:
        return 'legacy' if router_split_state(repository, profile_id, config)['mode'] == 'legacy' else 'selective'
    context = legacy_context(repository, profile_id)
    # Prepared before this server knew the marker but not yet saved here: the
    # host has no gateway state, so the router split starts with the full TUN.
    if context['session_goal'] == 'router' and not context['session_completed']:
        return 'full_tun'
    return 'selective'


def _full_tun_view(config):
    # The editor marks forwarded rule sets by the gateway selection; show the
    # router split's own list instead, including entries deleted since.
    view = deepcopy(config)
    view.setdefault('mihomo', {})['linux_gateway'] = {'enabled': False, 'rule_set_ids': route_rule_set_ids(config)}
    return view


def gateway_onboarding_state(config, revision, base_url='', *, takeover='selective'):
    # The wizard presents usage guidance next to its fields and in its help panel.
    state = gateway_editor_state(_full_tun_view(config) if takeover == 'full_tun' else config,
                                 include_guidance=False)
    blockers = []
    conversion = False
    try:
        settings, values = _oxidns_values(config, base_url)
    except OxidnsSettingsError as exc:
        settings = config.get('oxidns', {})
        values = settings
        blockers.append('现有 OxiDNS 无法安全读取：' + str(exc))
        conversion = settings.get('config_mode') == 'native'
    direct = values.get('direct_rulesets') or []
    proxy = values.get('proxy_rulesets') or []
    local = values.get('local_dns') or DEFAULT_LOCAL_DNS
    recorder = values.get('query_recorder')
    recorder = isinstance(recorder, dict) and recorder.get('enabled') is True
    warnings = list(state['warnings'])
    field_warnings = {}
    server_url = config.get('system_config', {}).get('server_domain', '') or ''
    try:
        server_host = urlsplit(server_url).hostname or ''
    except ValueError:
        server_host = ''
    if server_host.lower() in ('localhost', '::1', '0.0.0.0', '::') or server_host.startswith('127.'):
        warnings.append('系统设置中的 ConfigFlow 服务地址是本机回环地址，Linux 无法刷新规则；请先改为 Linux 能访问的局域网地址或域名，并在安装向导使用同一地址。')
    try:
        _local_upstreams(local)
    except (GatewayOnboardingError, OxidnsSettingsError):
        field_warnings['local_dns'] = '当前 DNS 不适用于网关，请改用 IP 地址的 DoH / DoT，例如 https://223.5.5.5/dns-query。'
    if settings.get('config_mode') == 'native' and not blockers:
        try:
            _check_native_dns_path(settings, allow_disabled_fallback=True)
            _prepare_native_gateway_fallback(settings)
            caps = read_common_settings(settings, config, base_url)['capabilities']
            fields = ('listen', 'tcp_enabled', 'prefer_ipv4', 'local_dns', 'remote_dns', 'fallback_dns',
                      'default_forward', 'direct_rulesets', 'proxy_rulesets')
            unavailable = [field for field in fields if not caps.get(field, {}).get('editable')]
            if unavailable:
                blockers.append('现有 OxiDNS 自定义结构无法安全更新：' + '、'.join(unavailable) + '。请在完整参数中调整，或明确确认转换为模板。')
                conversion = True
        except OxidnsSettingsError as exc:
            blockers.append(str(exc))
            conversion = True
    prerequisites = _proxy_prerequisites(config)
    if not prerequisites['ready']:
        warnings.append('请先准备代理节点或订阅、策略组及对应规则；预览会检查生成配置中的代理出口。')
    return {'profile_id': config.get('profile_id'), 'config_revision': revision, 'server_url': server_url,
            'settings': {'rule_set_ids': state['settings']['rule_set_ids'],
                         'direct_rule_set_ids': direct, 'proxy_rule_set_ids': proxy, 'local_dns': local,
                         'default_forward': values.get('default_forward', 'forward_remote'),
                         'query_recorder': recorder},
            'gateway_enabled': state['settings']['enabled'] and takeover != 'full_tun',
            'ip_candidates': state['candidates'], 'takeover': takeover,
            'domain_candidates': _domain_candidates(config, direct + proxy), 'fake_ip_range': state['fake_ip_range'],
            'oxidns_mode': settings.get('config_mode', 'template'), 'warnings': warnings, 'field_warnings': field_warnings,
            'blockers': blockers, 'can_prepare': not blockers, 'template_conversion_available': conversion,
            'proxy_prerequisites': prerequisites}


MIHOMO_DNS_UPSTREAM_KEYS = ('nameserver', 'fallback', 'default-nameserver', 'proxy-server-nameserver',
                            'direct-nameserver', 'nameserver-policy', 'proxy-server-nameserver-policy')


def mihomo_dns_loop(dns, host_addresses=()):
    """The first ``(key, upstream)`` of Mihomo's dns section that resolves through this host, or None.

    Loopback, unspecified and ``localhost`` addresses, ``system``, and this
    host's own addresses on port 53 or 1053 (OxiDNS, Mihomo's DNS) would loop.
    """
    for key in MIHOMO_DNS_UPSTREAM_KEYS:
        values = dns.get(key, []) if isinstance(dns, dict) else []
        pending = list(values.values()) if isinstance(values, dict) else [values]
        while pending:
            value = pending.pop()
            if isinstance(value, list):
                pending.extend(value)
                continue
            if not isinstance(value, str):
                continue
            raw = value.split('#', 1)[0]
            try:
                parts = urlsplit(raw if '://' in raw else 'udp://' + raw)
                hostname, upstream_port = parts.hostname, parts.port
            except ValueError:
                continue  # Not an address Mihomo could dial; its own check reports it.
            try:
                address = ipaddress.ip_address(hostname or '')
                local_address = address.is_loopback or address.is_unspecified or bool(
                    getattr(address, 'ipv4_mapped', None) and address.ipv4_mapped.is_loopback)
                matches_host = address in host_addresses or getattr(address, 'ipv4_mapped', None) in host_addresses
            except ValueError:
                local_address = hostname == 'localhost'
                matches_host = False
            if upstream_port is None and parts.scheme in ('udp', 'tcp'):
                upstream_port = 53
            if local_address or (matches_host and upstream_port in (53, 1053)) or raw.strip() in ('system', 'system://'):
                return key, value
    return None


def _prepare_mihomo_dns(config, *, local_addresses=()):
    try:
        host_addresses = {ipaddress.ip_address(value) for value in local_addresses}
    except (TypeError, ValueError) as exc:
        raise GatewayOnboardingError('Linux 本机地址无效，请重新核对安装网络信息') from exc
    source = config.setdefault('mihomo', {}).get('custom_config', '')
    if not isinstance(source, str):
        raise GatewayOnboardingError('Mihomo 基础配置必须为 YAML 文本')
    document = yaml.safe_load(source) if source.strip() else _default_mihomo_template()
    if not isinstance(document, dict) or not isinstance(document.get('dns', {}), dict):
        raise GatewayOnboardingError('Mihomo 基础配置及 dns 必须是 YAML 对象，请先修正基础配置')
    dns = document.setdefault('dns', {})
    if not source.strip() or not dns.get('nameserver'):
        dns['nameserver'] = list(DEFAULT_MIHOMO_NAMESERVERS)
    if mihomo_dns_loop(dns, host_addresses):
        raise GatewayOnboardingError('Mihomo DNS 上游不能指向本机 OxiDNS 或系统 DNS，否则会形成解析循环；请先修改基础配置')
    config['mihomo']['custom_config'] = yaml.safe_dump(document, allow_unicode=True, sort_keys=False)


def _loopback_listen(listen):
    host = listen.strip()
    if host.startswith('['):
        host = host[1:host.find(']')]
    elif host.count(':') == 1:
        host = host.split(':')[0]
    if host.lower() == 'localhost':
        return True
    try:
        return ipaddress.ip_address(host).is_loopback
    except ValueError:
        return False


def _enable_template_api(api, warnings):
    """Turn on the management API the DNS panel needs, keeping deliberate settings.

    Sub-fields of a disabled API are never validated, so leftovers that would
    block the wizard or expose an unauthenticated listener are reset here.
    """
    api = deepcopy(api)
    if api.get('enabled'):
        return api
    api['enabled'] = True
    default = '127.0.0.1:9199'
    username, password = api.get('username') or '', api.get('password') or ''
    if bool(username) != bool(password):
        api['username'] = api['password'] = ''
        username = ''
        warnings.append('OxiDNS 管理 API 原有账号只填写了一半，已清空；如需认证，请在 OxiDNS 设置「API / 日志」中同时填写用户名和密码。')
    ssl = api.get('ssl') or {}
    if ssl and bool(ssl.get('cert')) != bool(ssl.get('key')):
        api['ssl'] = {}
        warnings.append('OxiDNS 管理 API 的 TLS 证书和私钥只填写了一项，已清除，API 使用 HTTP。')
    listen = api.get('listen') if isinstance(api.get('listen'), str) else ''
    if not listen.strip():
        api['listen'] = default
    elif not username and not _loopback_listen(listen):
        api['listen'] = default
        warnings.append(f'OxiDNS 管理 API 原监听 {listen} 且未设置账号，已改为仅本机 {default}；'
                        '如需局域网访问，请先在 OxiDNS 设置「API / 日志」中设置用户名和密码。')
    return api


def prepare_gateway_onboarding(config, value, base_url='', *, takeover='selective'):
    """Return a proposed snapshot and reviewable diagnostics; never persist."""
    value = _input(value)
    candidate = deepcopy(config)
    full_tun = takeover == 'full_tun'
    report = {'success': True, 'ready': False, 'blockers': [],
              'warnings': [LEGACY_TAKEOVER_WARNING] if takeover == 'legacy' else [],
              'changes': list(ROUTER_SPLIT_CHANGES if full_tun else GATEWAY_CHANGES), 'validation': {},
              'template_conversion_available': False}
    try:
        try:
            settings, current = _oxidns_values(config, base_url)
        except OxidnsSettingsError as exc:
            raw = config.get('oxidns', {})
            if raw.get('config_mode') != 'native':
                raise
            report['template_conversion_available'] = True
            if not value.get('confirm_template_conversion'):
                raise GatewayOnboardingError('现有 OxiDNS 无法安全读取：' + str(exc)
                    + '。请修正原生配置，或明确确认转换为模板。') from exc
            # Conversion is an explicit replacement of only the executable
            # native document. All original settings and files remain backed up.
            settings = normalize_oxidns_settings({**raw, 'config_mode': 'template', 'native_config': ''})
            settings['gateway_onboarding_native_backup'] = deepcopy(raw)
            current = settings
            report['warnings'].append('已按明确选择转换 OxiDNS 为模板；原始配置保存在 gateway_onboarding_native_backup 中。')
        selected_before = set((current.get('direct_rulesets') or []) + (current.get('proxy_rulesets') or []))
        choices = {item['id']: item for item in _domain_candidates(config, list(selected_before))}
        for identity in value['direct_rule_set_ids'] + value['proxy_rule_set_ids']:
            item = choices.get(identity)
            if item is None or not item['available'] and identity not in selected_before:
                raise GatewayOnboardingError('所选域名规则集不可用：' + (item['reason'] if item else '不存在或不属于当前配置空间'))
            if not item['available']:
                report['warnings'].append(item['name'] + '：' + item['reason'])
        if full_tun:
            ids = validate_rule_set_ids(candidate, value['rule_set_ids'], set(route_rule_set_ids(config)))
            draft = candidate.setdefault('mihomo', {})
            marker = read_marker(config)[1] or {'version': 1, 'takeover': 'full_tun', 'origin': 'setup'}
            draft['linux_gateway'] = {'enabled': False, 'rule_set_ids': []}
            draft[MARKER_KEY] = {**marker, 'route_rule_set_ids': ids}
            draft['custom_config'] = full_tun_base_yaml(draft.get('custom_config', ''))
        else:
            candidate.setdefault('mihomo', {})['linux_gateway'] = validate_gateway_selection(candidate, {
                'enabled': True, 'rule_set_ids': value['rule_set_ids']})
        _prepare_mihomo_dns(candidate)
        changes = {'listen': '0.0.0.0:53', 'tcp_enabled': True, 'prefer_ipv4': True,
                   'local_dns': value['local_dns'], 'remote_dns': DNS_TARGET, 'fallback_dns': DNS_TARGET,
                   'default_forward': value.get('default_forward', 'forward_remote'),
                   'direct_rulesets': value['direct_rule_set_ids'],
                   'proxy_rulesets': value['proxy_rule_set_ids']}
        if settings['config_mode'] == 'native':
            native_changes = {**changes, **{key: yaml.safe_dump(_forward(changes[key], []), allow_unicode=True, sort_keys=False)
                                          for key in ('local_dns', 'remote_dns', 'fallback_dns')}}
            # The DNS panel, rule updates and hot reloads all reach the core
            # through its API; keep an existing listener and credentials as-is.
            if not current['api']['enabled'] and read_common_settings(
                    settings, config, base_url)['capabilities']['api.enabled']['editable']:
                native_changes['api.enabled'] = True
            if 'query_recorder' in value and (current.get('query_recorder') or {}).get('enabled') is not value['query_recorder']:
                if read_common_settings(settings, config, base_url)['capabilities'].get('query_recorder.enabled', {}).get('editable'):
                    native_changes['query_recorder.enabled'] = value['query_recorder']
                else:
                    report['warnings'].append('OxiDNS 查询记录使用自定义结构，向导未修改；请在 OxiDNS 设置的「特色功能」中调整。')
            try:
                _check_native_dns_path(settings, allow_disabled_fallback=True)
                settings = _prepare_native_gateway_fallback(settings)
                settings = apply_common_settings(settings, native_changes, candidate, base_url)['settings']
            except OxidnsSettingsError as exc:
                if not value.get('confirm_template_conversion'):
                    report['template_conversion_available'] = True
                    raise GatewayOnboardingError('现有 OxiDNS 自定义结构无法安全更新：' + str(exc)
                        + '。请在完整参数中调整，或明确确认转换为模板；不会自动覆盖原生配置。') from exc
                settings = deepcopy(settings)
                # Retain every original field and attachment as one recoverable
                # backup, including a previous legacy backup if one existed.
                settings['gateway_onboarding_native_backup'] = deepcopy(settings)
                settings.update(config_mode='template', native_config='', **changes)
                report['warnings'].append('已按明确选择转换 OxiDNS 为模板；原生配置和附加文件保存在 gateway_onboarding_native_backup 中，可从配置导出恢复。')
        else:
            settings.update(changes)
        if settings['config_mode'] == 'template':
            settings['api'] = _enable_template_api(settings.get('api', {}), report['warnings'])
            if 'query_recorder' in value:
                settings['query_recorder'] = {**settings.get('query_recorder', {}), 'enabled': value['query_recorder']}
        candidate['oxidns'] = normalize_oxidns_settings(settings)
        mihomo = yaml.safe_load(generate_mihomo_config(candidate, base_url, preflight_providers=False))
        # Render both services before declaring the draft ready. Target cores
        # still perform their own native checks during the subsequent publish.
        generate_oxidns_config(candidate, base_url)
        report['validation'] = {'mihomo': {'valid': True, 'core_check_required': True},
                                'oxidns': validate_settings(candidate['oxidns'], candidate, base_url)}
        if not report['validation']['oxidns']['valid']:
            raise GatewayOnboardingError('OxiDNS 配置校验未通过，请根据 validation.oxidns.issues 修正后重试')
        prerequisites = _proxy_prerequisites(candidate, mihomo)
        report['proxy_prerequisites'] = prerequisites
        report['warnings'].extend(prerequisites['warnings'])
        if not prerequisites['ready']:
            raise GatewayOnboardingError('当前配置没有被启用规则引用的可用代理出口；请先添加节点或订阅、配置策略组，并为需要代理的规则选择该策略')
        selected = set(value['rule_set_ids']) | set(value['proxy_rule_set_ids'])
        for item in candidate.get('rule_configs', []):
            if item.get('id') in selected and item.get('policy') not in prerequisites['policies']:
                report['warnings'].append(f"规则集「{item.get('name', item.get('id'))}」当前策略为 {item.get('policy', '未设置')}；"
                                          f"DNS 或{'主路由转发' if full_tun else ' IP 接管'}不会自动把此策略改为代理。")
        report['warnings'].extend(direct_overlap_warnings(candidate))
        report['ready'] = True
    except (GatewayOnboardingError, LinuxGatewayError, OxidnsSettingsError, ValueError, yaml.YAMLError) as exc:
        report['blockers'].append(str(exc))
    return candidate, report


def assert_gateway_onboarding_ready(config, base_url='', *, local_addresses=()):
    """Reject unprepared saved drafts before an installation session is frozen."""
    state = gateway_editor_state(config)
    if is_full_tun(config):
        if gateway_settings(config)['enabled']:
            raise GatewayOnboardingError('主路由分流不能同时启用 Linux 网关，请重新预览并保存')
        try:
            document = yaml.safe_load(config.get('mihomo', {}).get('custom_config') or '')
        except (yaml.YAMLError, RecursionError):
            document = None
        if not is_full_tun_document(document):
            raise GatewayOnboardingError('Mihomo 基础 YAML 缺少完整 TUN，请重新预览并保存')
    elif not state['settings']['enabled']:
        raise GatewayOnboardingError('请先在安装向导预览并保存网关配置')
    try:
        settings, values = _oxidns_values(config, base_url)
        required = {'listen': '0.0.0.0:53', 'tcp_enabled': True, 'prefer_ipv4': True}
        if any(type(values.get(key)) is not type(expected) or values.get(key) != expected
               for key, expected in required.items()):
            raise GatewayOnboardingError('已保存的 OxiDNS 监听、IPv4 或默认分流不符合安装方案，请重新预览并保存')
        if values.get('default_forward') not in ('forward_local', 'forward_remote'):
            raise GatewayOnboardingError('已保存的 OxiDNS 默认 DNS 无效，请重新预览并保存')
        _local_upstreams(values.get('local_dns'))
        for key in ('remote_dns', 'fallback_dns'):
            upstreams = _forward(values.get(key) or '', [])
            if not upstreams or any(item.get('addr') not in (DNS_TARGET, 'udp://' + DNS_TARGET)
                                    or item.get('socks5') or item.get('outbound') for item in upstreams):
                raise GatewayOnboardingError('OxiDNS 代理和备用上游必须均为 127.0.0.1:1053，请重新预览并保存')
        # Ensure native defaults belong to the recognized routing graph, rather
        # than trusting inactive template fields kept next to native YAML.
        if settings['config_mode'] == 'native':
            _check_native_dns_path(settings)
            caps = read_common_settings(settings, config, base_url)['capabilities']
            if any(not caps.get(field, {}).get('editable') for field in ('default_forward', 'direct_rulesets', 'proxy_rulesets')):
                raise GatewayOnboardingError('OxiDNS 自定义分流结构无法确认，请回到安装向导重新预览')
        validation = validate_settings(settings, config, base_url)
        if not validation['valid']:
            raise GatewayOnboardingError('已保存的 OxiDNS 配置校验未通过，请修正后重新准备安装')
        candidate = deepcopy(config)
        _prepare_mihomo_dns(candidate, local_addresses=local_addresses)  # Detect circular DNS upstreams.
        document = yaml.safe_load(generate_mihomo_config(config, base_url, preflight_providers=False))
        if not document.get('dns', {}).get('nameserver'):
            raise GatewayOnboardingError('Mihomo 缺少 DNS 上游，请重新预览并保存网关配置')
        prerequisites = _proxy_prerequisites(config, document)
        if not prerequisites['ready']:
            raise GatewayOnboardingError('当前配置没有被启用规则引用的可用代理出口，请先完善节点和规则')
        return {'ready': True, 'proxy_prerequisites': prerequisites, 'validation': {'oxidns': validation}}
    except (LinuxGatewayError, OxidnsSettingsError, ValueError, yaml.YAMLError) as exc:
        if isinstance(exc, GatewayOnboardingError):
            raise
        raise GatewayOnboardingError(str(exc)) from exc
