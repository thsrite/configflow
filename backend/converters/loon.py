"""Loon 配置生成器

参考 Loon 官方文档：https://nsloon.app/docs/intro
以及官方示例配置：https://github.com/Loon0x00/LoonExampleConfig/blob/master/example2.lcf
- 手动节点写入 [Proxy]
- 订阅 / 聚合以 [Remote Proxy] 引用本服务的 ?format=loon 接口
- 策略组配置的节点名正则转换为 [Remote Filter] 的 NameRegex 过滤器
- 规则集写入 [Remote Rule]（Loon 中本地规则优先于订阅规则）
"""
import re
from typing import Dict, Any, List, Optional, Tuple
from backend.utils.subscription_parser import parse_uri_list
from backend.utils.logger import get_logger
from backend.common.profile_context import append_url_query, profile_api_path
from backend.converters.common import (
    after_match_comment, has_port_list, network_as_protocol, ruleset_format, set_general_option, udp_fallback,
)

logger = get_logger(__name__)

DEFAULT_TEST_URL = 'https://www.gstatic.com/generate_204'

# https://nsloon.app/docs/Policy/
LOON_BUILTIN_POLICIES = {'DIRECT', 'REJECT', 'REJECT-DROP', 'REJECT-IMG', 'REJECT-DICT', 'REJECT-ARRAY'}

# Loon 文档列出的本地规则类型（https://nsloon.app/docs/Rule/）；
# 其他类型（如 PROCESS-NAME、SRC-IP-CIDR）会被注释掉，避免配置解析失败
LOON_RULE_TYPES = {
    'DOMAIN', 'DOMAIN-SUFFIX', 'DOMAIN-KEYWORD',
    'IP-CIDR', 'IP-CIDR6', 'GEOIP', 'IP-ASN', 'SRC-PORT', 'DEST-PORT',
    'URL-REGEX', 'USER-AGENT', 'PROTOCOL', 'AND', 'OR', 'NOT',
}

# mihomo/Clash → Loon 规则类型映射
LOON_RULE_TYPE_MAP = {
    'DST-PORT': 'DEST-PORT',
}

LOAD_BALANCE_ALGORITHMS = {
    'consistent-hashing': 'pcc',
    'sticky-sessions': 'pcc',
    'round-robin': 'round-robin',
}

# 取值参考 https://nsloon.app/docs/General/ 与官方 example2.lcf。
# 注意 bypass-tun 不能包含 198.18.0.0/15：这是 Loon 的 Fake IP 网段。
DEFAULT_GENERAL = [
    '[General]',
    'ip-mode = dual',
    'dns-server = system,223.5.5.5,119.29.29.29',
    'doh-server = https://dns.alidns.com/dns-query',
    'hijack-dns = *:53',
    'proxy-test-url = https://www.gstatic.com/generate_204',
    'internet-test-url = http://wifi.vivo.com.cn/generate_204',
    'test-timeout = 5',
    'interface-mode = auto',
    'sni-sniffing = true',
    'udp-fallback-mode = DIRECT',
    'allow-wifi-access = false',
    'wifi-access-http-port = 7222',
    'wifi-access-socks5-port = 7221',
    'skip-proxy = 192.168.0.0/16,10.0.0.0/8,172.16.0.0/12,localhost,*.local,captive.apple.com',
    'bypass-tun = 10.0.0.0/8,100.64.0.0/10,127.0.0.0/8,169.254.0.0/16,172.16.0.0/12,192.0.0.0/24,192.0.2.0/24,192.88.99.0/24,192.168.0.0/16,198.51.100.0/24,203.0.113.0/24,224.0.0.0/4,255.255.255.255/32',
]


def _parse_custom_sections(custom_config: str) -> Dict[str, str]:
    """解析自定义配置，返回 section_name -> 含 [SectionName] 行的完整文本"""
    sections = {}
    if not custom_config or not custom_config.strip():
        return sections
    current_section = None
    current_lines = []
    for line in custom_config.strip().split('\n'):
        stripped = line.strip()
        if stripped.startswith('[') and ']' in stripped:
            if current_section:
                sections[current_section] = '\n'.join(current_lines)
            current_section = stripped.split(']')[0].split('[')[1]
            current_lines = [line]
        elif current_section:
            current_lines.append(line)
    if current_section:
        sections[current_section] = '\n'.join(current_lines)
    return sections


def _effective_group(group: Dict[str, Any], config_data: Dict[str, Any]) -> Optional[Dict[str, Any]]:
    """处理跟随模式：返回合并后的策略组（保留自身名称），被跟随的策略组不存在时返回 None"""
    follow_group_id = group.get('follow_group')
    if not follow_group_id:
        return group
    followed = next((g for g in config_data.get('proxy_groups', []) if g.get('id') == follow_group_id), None)
    if not followed:
        return None
    return {
        **group,
        'type': followed['type'],
        'manual_nodes': followed.get('manual_nodes', []),
        'aggregations': followed.get('aggregations', []),
        'include_groups': followed.get('include_groups', []),
        'subscriptions': followed.get('subscriptions', []),
        'regex': followed.get('regex', ''),
        'exclude_regex': followed.get('exclude_regex', ''),
        'aggregation_regex': followed.get('aggregation_regex', ''),
        'aggregation_exclude_regex': followed.get('aggregation_exclude_regex', ''),
        'proxies_order': followed.get('proxies_order', []),
        'proxy_order': followed.get('proxy_order', 'nodes_first'),
        'url': followed.get('url'),
        'interval': followed.get('interval'),
        'strategy': followed.get('strategy'),
    }


def _group_sources(group: Dict[str, Any]) -> Tuple[list, list, list, list]:
    """返回 (manual_nodes, aggregation_ids, include_groups, subscriptions)，兼容旧格式"""
    manual_nodes = group.get('manual_nodes', [])
    aggregation_ids = group.get('aggregations', [])
    include_groups = group.get('include_groups', [])
    subscriptions = group.get('subscriptions', [])
    if not manual_nodes and not aggregation_ids and not include_groups and not subscriptions:
        source = group.get('source', 'subscription')
        proxies_old = group.get('proxies', [])
        if source == 'node':
            manual_nodes = proxies_old
        elif source == 'strategy':
            include_groups = proxies_old
    return manual_nodes, aggregation_ids, include_groups, subscriptions


def _group_member_refs(group: Dict[str, Any]) -> List[Tuple[str, str]]:
    """按用户排序返回策略组成员引用列表：('node', id) / ('strategy', id) / ('builtin', name)"""
    manual_nodes, _, include_groups, _ = _group_sources(group)
    refs = []
    proxies_order = group.get('proxies_order', [])
    if proxies_order:
        for item in proxies_order:
            item_id = item.get('id')
            if item.get('type') == 'node':
                refs.append(('builtin', item_id) if item_id in ('DIRECT', 'REJECT') else ('node', item_id))
            elif item.get('type') == 'strategy':
                refs.append(('strategy', item_id))
        order_node_ids = {item.get('id') for item in proxies_order if item.get('type') == 'node'}
        for node_id in manual_nodes:
            if node_id not in order_node_ids:
                refs.append(('builtin', node_id) if node_id in ('DIRECT', 'REJECT') else ('node', node_id))
        return refs

    nodes_refs = [('builtin', n) if n in ('DIRECT', 'REJECT') else ('node', n) for n in manual_nodes]
    strategy_refs = [('strategy', g) for g in include_groups]
    if group.get('proxy_order', 'nodes_first') == 'strategies_first':
        return strategy_refs + nodes_refs
    return nodes_refs + strategy_refs


class _NameAllocator:
    """为 Remote Proxy / Remote Filter 分配不与节点、策略组冲突的名称"""

    def __init__(self, reserved):
        self.used = set(reserved) | LOON_BUILTIN_POLICIES

    def allocate(self, preferred: str, suffix: str) -> str:
        name = _sanitize_name(preferred) or suffix
        if name not in self.used:
            self.used.add(name)
            return name
        candidate = f"{name}-{suffix}"
        index = 2
        while candidate in self.used:
            candidate = f"{name}-{suffix}{index}"
            index += 1
        self.used.add(candidate)
        return candidate


def _sanitize_name(name: str) -> str:
    # 名称出现在 "name = ..." 与逗号分隔列表中，去掉会破坏解析的字符
    return (name or '').replace(',', ' ').replace('=', ' ').strip()


def generate_loon_config(config_data: Dict[str, Any], base_url: str = '') -> str:
    """
    生成 Loon 配置文件

    Args:
        config_data: 包含节点、策略组、规则等的配置字典
        base_url: 前端页面的 base URL（协议 + 主机 + 端口），用于构建订阅与规则 URL

    Returns:
        str: Loon 格式的配置字符串
    """
    from backend.utils.dialer_references import validate_dialers, reject_surge_dialers
    validate_dialers(config_data)
    reject_surge_dialers(config_data, client='Loon')

    server_domain = config_data.get('system_config', {}).get('server_domain', '').strip()
    effective_base_url = server_domain or base_url
    config_token = config_data.get('system_config', {}).get('config_token', '')

    custom_sections = _parse_custom_sections(config_data.get('loon', {}).get('custom_config', ''))
    # 节点不支持 UDP 时的处理：用户在基础配置里写了该项时以基础配置为准
    fallback = udp_fallback(config_data)
    general = custom_sections.get('General')
    if general:
        general = set_general_option(general, 'udp-fallback-mode', fallback) if fallback else general
    else:
        general = '\n'.join(f'udp-fallback-mode = {fallback}' if fallback and line.startswith('udp-fallback-mode')
                            else line for line in DEFAULT_GENERAL)
    sections = [general]

    all_groups = config_data.get('proxy_groups', [])
    nodes_by_id = {n.get('id'): n for n in config_data.get('nodes', [])}
    groups_by_id = {g.get('id'): g for g in all_groups}

    # 解析启用的策略组（含跟随模式）
    effective_groups = []
    for group in all_groups:
        if not group.get('enabled', True):
            continue
        effective = _effective_group(group, config_data)
        if effective is not None:
            effective_groups.append(effective)

    # 被直接选择的节点（聚合中的节点若未被直接选择，则通过 Remote Proxy 提供）
    used_node_ids = []
    for group in effective_groups:
        for kind, ref in _group_member_refs(group):
            if kind == 'node' and ref not in used_node_ids:
                used_node_ids.append(ref)

    # [Proxy]
    proxies = ['[Proxy]']
    node_names = {}
    for node in config_data.get('nodes', []):
        if not node.get('enabled', True) or node.get('id') not in used_node_ids:
            continue
        proxy_line = convert_node_to_loon(node)
        if proxy_line:
            proxies.append(proxy_line)
            node_names[node['id']] = proxy_line.split(' = ', 1)[0]
        else:
            logger.warning(f"Loon 不支持该节点类型，已跳过: {node.get('name')}")
    sections.append('\n'.join(proxies))

    allocator = _NameAllocator(set(node_names.values()) | {_sanitize_name(g.get('name')) for g in all_groups})

    # [Remote Proxy]：订阅与聚合
    remote_names = {}
    remote_lines = ['[Remote Proxy]']

    def remote_name(kind: str, resource_id: str, exclude_regex: str = '') -> Optional[str]:
        # 同一来源可以在不同策略组中采用不同排除条件，必须分别订阅。
        key = (kind, resource_id, exclude_regex)
        if key in remote_names:
            return remote_names[key]
        if kind == 'subscription':
            resources = config_data.get('subscriptions', [])
            path = f'/subscriptions/{resource_id}/proxies'
        else:
            resources = config_data.get('subscription_aggregations', [])
            path = f'/aggregations/{resource_id}/provider'
        resource = next((r for r in resources if r.get('id') == resource_id and r.get('enabled', True)), None)
        if not resource:
            remote_names[key] = None
            return None
        name = allocator.allocate(resource.get('name', ''), '订阅' if kind == 'subscription' else '聚合')
        query = {'format': 'loon'}
        if exclude_regex:
            query['exclude_regex'] = exclude_regex
        if config_token:
            query['token'] = config_token
        url = append_url_query(f"{effective_base_url}{profile_api_path(config_data, path)}", query)
        # default 表示保留订阅内节点自身的设置：https://nsloon.app/docs/Node/subscription
        remote_lines.append(f"{name} = {url},udp=default,block-quic=default,fast-open=default,"
                            f"vmess-aead=default,skip-cert-verify=default,enabled=true")
        remote_names[key] = name
        return name

    # [Remote Filter] 与策略组成员
    filter_lines = ['[Remote Filter]']
    group_members = {}
    for group in effective_groups:
        members = []
        for kind, ref in _group_member_refs(group):
            if kind == 'builtin':
                members.append(ref)
            elif kind == 'node' and ref in node_names:
                members.append(node_names[ref])
            elif kind == 'strategy' and ref in groups_by_id and ref != group.get('id'):
                members.append(('strategy', ref))

        _, aggregation_ids, _, subscriptions = _group_sources(group)
        # 两种来源独立筛选，手动节点和策略引用保持原样。
        # 排除条件交给本服务的动态订阅接口，正向筛选沿用 Loon NameRegex。
        for kind, resource_ids, prefix in (
            ('subscription', subscriptions, ''),
            ('aggregation', aggregation_ids, 'aggregation_'),
        ):
            regex = (group.get(f'{prefix}regex') or '').strip()
            exclude_regex = (group.get(f'{prefix}exclude_regex') or '').strip()
            remotes = [remote_name(kind, resource_id, exclude_regex) for resource_id in resource_ids]
            remotes = [remote for remote in remotes if remote]
            if not remotes:
                continue
            if regex:
                suffix = '筛选' if kind == 'subscription' else '聚合筛选'
                filter_name = allocator.allocate(f"{group['name']}-{suffix}", suffix)
                escaped = regex.replace('"', '\\"')
                filter_lines.append(f'{filter_name} = NameRegex,{",".join(remotes)},FilterKey="{escaped}"')
                members.append(filter_name)
            else:
                members.extend(remotes)
        group_members[group['id']] = (group, members)

    # 反复移除没有可用成员的策略组，避免引用不存在的策略
    emitted = set(group_members)
    changed = True
    while changed:
        changed = False
        for group_id in list(emitted):
            _, members = group_members[group_id]
            if not any(not isinstance(m, tuple) or m[1] in emitted for m in members):
                logger.debug(f"Loon 策略组没有可用成员，跳过: {group_members[group_id][0].get('name')}")
                emitted.discard(group_id)
                changed = True

    proxy_groups = ['[Proxy Group]']
    for group in effective_groups:
        if group['id'] not in emitted:
            continue
        _, members = group_members[group['id']]
        names = []
        for member in members:
            if isinstance(member, tuple):
                if member[1] in emitted:
                    names.append(_sanitize_name(groups_by_id[member[1]]['name']))
            else:
                names.append(member)
        line = convert_proxy_group_to_loon(group, names)
        if line:
            proxy_groups.append(line)

    if len(proxy_groups) == 1 and node_names:
        all_names = ','.join(node_names.values())
        proxy_groups.append(f"Proxy = select,{all_names}")
        proxy_groups.append(f"Auto = url-test,{all_names},url={DEFAULT_TEST_URL},interval=600")

    sections.append('\n'.join(remote_lines))
    sections.append('\n'.join(filter_lines))
    sections.append('\n'.join(proxy_groups))

    # [Rule] 与 [Remote Rule]
    rules, remote_rules = _build_rules(config_data, effective_base_url)
    sections.append('\n'.join(rules))
    sections.append('\n'.join(remote_rules))

    auto_sections = {'General', 'Proxy', 'Remote Proxy', 'Remote Filter', 'Proxy Group', 'Rule', 'Remote Rule'}
    for section_name, section_content in custom_sections.items():
        if section_name not in auto_sections:
            sections.append(section_content)

    from backend.utils.strategy_references import validate_rule_policies
    validate_rule_policies(
        config_data,
        [line.split('=', 1)[0].strip() for line in proxies[1:] + proxy_groups[1:] if '=' in line],
        'loon',
    )

    header = '# Generated by ConfigFlow for Loon'
    return f"{header}\n\n" + '\n\n'.join(sections) + '\n'


def _build_rules(config_data: Dict[str, Any], effective_base_url: str) -> Tuple[List[str], List[str]]:
    rules = ['[Rule]']
    remote_rules = ['[Remote Rule]']
    final_rule = None

    ruleset_urls = {}
    for lib_rule in config_data.get('rule_library', []):
        if lib_rule.get('enabled', True):
            ruleset_urls[lib_rule.get('name', '')] = lib_rule.get('url', '') or ''

    for item in config_data.get('rule_configs', []):
        if not item.get('enabled', True) or not item.get('library_enabled', True):
            continue
        item_type = item.get('itemType', '')

        # FINAL 固定放在最后：MATCH 之后的规则若照常输出会排到 FINAL 之前，改变匹配结果
        if final_rule:
            (remote_rules if item_type == 'ruleset' else rules).append(after_match_comment(item))
            continue

        if item_type == 'rule':
            rule_type = item['rule_type']
            value = item.get('value', '')
            policy = item['policy']
            if rule_type == 'MATCH':
                final_rule = f"FINAL,{policy}"
                continue
            if has_port_list(rule_type, value):
                rules.append(f"# Loon 不支持一条规则写多个端口，已忽略: {rule_type},{value},{policy}")
                continue
            # NETWORK 在 Loon 中是 PROTOCOL
            rule_type, value = network_as_protocol(rule_type, value)
            if rule_type in ('AND', 'OR', 'NOT'):
                loon_value = value
                for src, dst in LOON_RULE_TYPE_MAP.items():
                    loon_value = loon_value.replace(src, dst)
                line = f"{rule_type},{loon_value},{policy}"
                sub_types = set(re.findall(r'\(\s*([A-Z0-9-]+)\s*,', loon_value))
                if sub_types - LOON_RULE_TYPES:
                    rules.append(f"# Loon 不支持的规则类型，已忽略: {line}")
                else:
                    rules.append(line)
                continue
            loon_type = LOON_RULE_TYPE_MAP.get(rule_type, rule_type)
            line = f"{loon_type},{value},{policy}"
            if item.get('no_resolve', False):
                line += ',no-resolve'
            if loon_type not in LOON_RULE_TYPES:
                rules.append(f"# Loon 不支持的规则类型，已忽略: {line}")
            else:
                rules.append(line)

        elif item_type == 'ruleset':
            policy = item.get('policy', 'Proxy')
            name = item.get('name', '')
            url = item.get('url', '') or ruleset_urls.get(name, '')
            original_url = ruleset_urls.get(name, url) or url
            if url and url.startswith('/') and effective_base_url:
                url = f"{effective_base_url}{url}"
            if not url:
                continue
            if ruleset_format(item, config_data, original_url) in ('yaml', 'mrs'):
                remote_rules.append(f"# Loon 不支持 YAML/MRS 规则集，已忽略: {name} {url}")
                continue
            tag = _sanitize_name(name) or 'rule'
            remote_rules.append(f"{_loon_ruleset_url(url)},policy={policy},tag={tag},enabled=true")

    if final_rule:
        rules.append(final_rule)
    return rules, remote_rules


def _loon_ruleset_url(url: str) -> str:
    """blackmatrix7/ios_rule_script 的 Clash 规则含 PROCESS-NAME 等 Loon 不支持的类型，改用其 Loon 版本"""
    if 'blackmatrix7/ios_rule_script' in url and '/rule/Clash/' in url:
        return url.replace('/rule/Clash/', '/rule/Loon/')
    return url


def convert_proxy_group_to_loon(group: Dict[str, Any], members: List[str]) -> Optional[str]:
    """将策略组转换为 Loon 格式，members 为已解析的成员名称"""
    if not members:
        return None
    name = _sanitize_name(group['name'])
    group_type = group.get('type')
    member_list = ','.join(members)
    url = group.get('url') or DEFAULT_TEST_URL
    interval = group.get('interval') or 600

    if group_type == 'select':
        return f"{name} = select,{member_list}"
    if group_type == 'url-test':
        return f"{name} = url-test,{member_list},url={url},interval={interval},tolerance=100"
    if group_type == 'fallback':
        return f"{name} = fallback,{member_list},url={url},interval={interval},max-timeout=3000"
    if group_type == 'load-balance':
        algorithm = LOAD_BALANCE_ALGORITHMS.get(group.get('strategy') or '', 'random')
        return (f"{name} = load-balance,{member_list},url={url},interval={interval},"
                f"algorithm={algorithm},max-timeout=3000")
    return None


def convert_proxies_to_loon_text(proxies: List[Dict[str, Any]]) -> str:
    """将 mihomo 格式 proxies 列表转换为 Loon 节点文本（每行一个节点），用于 [Remote Proxy] 订阅"""
    from backend.utils.sub_store_client import proxies_to_nodes
    from backend.utils.dialer_references import DialerReferenceError
    if any(proxy.get('dialer-proxy') is not None for proxy in proxies):
        raise DialerReferenceError('Loon 订阅暂不支持 dialer-proxy，不能静默移除拨号配置')

    lines = []
    for node in proxies_to_nodes(proxies):
        try:
            line = convert_node_to_loon(node)
            if line:
                lines.append(line)
        except Exception as e:
            logger.warning(f"转换节点到 Loon 格式失败: {node.get('name', '?')}, 错误: {e}")
    return '\n'.join(lines)


def _resolve_node(node: Dict[str, Any]) -> Tuple[Optional[Dict[str, Any]], Dict[str, Any]]:
    """解析 proxy_string，返回 (node, params)"""
    if 'proxy_string' not in node:
        return node, node.get('params', {}) or {}
    outer_name = node.get('name', '')
    try:
        parsed_nodes = parse_uri_list(node['proxy_string'])
    except Exception:
        return None, {}
    if not parsed_nodes:
        return None, {}
    parsed = parsed_nodes[0]
    if parsed.get('_raw_object'):
        parsed = {k: v for k, v in parsed.items() if k != '_raw_object'}
        parsed['name'] = outer_name
        return parsed, parsed
    parsed['name'] = outer_name
    return parsed, parsed.get('params', {}) or {}


def _quote(value: Any) -> str:
    return '"' + str(value).replace('"', '\\"') + '"'


def _value(value: Any) -> str:
    """参数值含英文逗号时必须用双引号包裹：https://nsloon.app/docs/Node/"""
    text = str(value)
    return _quote(text) if ',' in text else text


def _bool(value: Any) -> str:
    return 'true' if value else 'false'


def _first(value: Any) -> str:
    if isinstance(value, list):
        return str(value[0]) if value else ''
    return str(value or '')


# Loon VMess 支持的加密方式：none、auto、aes-128-cfb、aes-128-gcm、chacha20-ietf-poly1305
VMESS_CIPHERS = {'none', 'auto', 'aes-128-cfb', 'aes-128-gcm', 'chacha20-ietf-poly1305'}


def _transport_opts(params: Dict[str, Any], network: str, allow_http: bool = True) -> Optional[List[str]]:
    """返回 Loon 传输层参数；不支持的传输方式（grpc、h2 等）返回 None"""
    if network in ('', 'tcp'):
        return ['transport=tcp']
    if network == 'ws':
        ws_opts = params.get('ws-opts', {}) or {}
        path = ws_opts.get('path') or params.get('path') or '/'
        host = (ws_opts.get('headers', {}) or {}).get('Host') or params.get('host', '')
        parts = ['transport=ws', f'path={_value(path)}']
        if host:
            parts.append(f'host={_value(host)}')
        return parts
    if network == 'http' and allow_http:
        http_opts = params.get('http-opts', {}) or {}
        path = _first(http_opts.get('path')) or '/'
        host = _first((http_opts.get('headers', {}) or {}).get('Host'))
        parts = ['transport=http', f'path={_value(path)}']
        if host:
            parts.append(f'host={_value(host)}')
        return parts
    return None


def _tls_opts(params: Dict[str, Any]) -> List[str]:
    """sni / skip-cert-verify / alpn / Reality 公共 TLS 参数"""
    parts = []
    sni = params.get('servername') or params.get('sni') or ''
    if sni:
        parts.append(f'sni={_value(sni)}')
    parts.append(f"skip-cert-verify={_bool(params.get('skip-cert-verify'))}")
    alpn = params.get('alpn')
    if alpn:
        parts.append(f"alpn={_quote(','.join(alpn) if isinstance(alpn, list) else alpn)}")
    reality = params.get('reality-opts', {}) or {}
    if reality.get('public-key'):
        parts.append(f"public-key={_quote(reality['public-key'])}")
        if reality.get('short-id'):
            parts.append(f"short-id={reality['short-id']}")
    return parts


def _shadow_tls_opts(params: Dict[str, Any]) -> Optional[List[str]]:
    """mihomo 的 shadow-tls 插件 → Loon shadow-tls-* 参数；非 shadow-tls 插件返回 None"""
    opts = params.get('plugin-opts', {}) or {}
    parts = [f"shadow-tls-password={_value(opts.get('password', ''))}"]
    if opts.get('host'):
        parts.append(f"shadow-tls-sni={_value(opts['host'])}")
    parts.append(f"shadow-tls-version={opts.get('version', 3)}")
    return parts


def convert_node_to_loon(node: Dict[str, Any]) -> Optional[str]:
    """将通用节点格式转换为 Loon 节点行（格式见 https://nsloon.app/docs/Node/），不支持的类型返回 None"""
    node, params = _resolve_node(node)
    if not node:
        return None
    node_type = (node.get('type') or '').lower()
    if not node_type:
        return None

    name = _sanitize_name(node.get('name', ''))
    server = node.get('server', '')
    port = node.get('port', 0)
    udp = f"udp={_bool(params.get('udp') or params.get('udp-relay'))}"
    plugin = params.get('plugin', '')

    if node_type == 'ss':
        parts = [f"{name} = Shadowsocks", server, str(port), params.get('cipher', 'aes-256-gcm'),
                 _quote(params.get('password', ''))]
        plugin_opts = params.get('plugin-opts', {}) or {}
        if plugin in ('obfs', 'simple-obfs'):
            parts.append(f"obfs-name={plugin_opts.get('mode', 'http')}")
            if plugin_opts.get('host'):
                parts.append(f"obfs-host={_value(plugin_opts['host'])}")
        elif plugin == 'shadow-tls':
            parts += _shadow_tls_opts(params)
        elif plugin:
            return None
        if params.get('udp-over-tcp'):
            parts.append('udp-over-tcp=true')
        parts += [f"fast-open={_bool(params.get('tfo'))}", udp]
        return ','.join(parts)

    if node_type == 'ssr':
        parts = [f"{name} = ShadowsocksR", server, str(port), params.get('cipher', ''),
                 _quote(params.get('password', '')), f"protocol={params.get('protocol', 'origin')}"]
        if params.get('protocol-param'):
            parts.append(f"protocol-param={_value(params['protocol-param'])}")
        parts.append(f"obfs={params.get('obfs', 'plain')}")
        if params.get('obfs-param'):
            parts.append(f"obfs-param={_value(params['obfs-param'])}")
        parts += [f"fast-open={_bool(params.get('tfo'))}", udp]
        return ','.join(parts)

    if node_type == 'vmess':
        transport = _transport_opts(params, params.get('network', 'tcp') or 'tcp')
        if transport is None:
            return None
        cipher = params.get('cipher', 'auto') or 'auto'
        parts = [f"{name} = VMess", server, str(port), cipher if cipher in VMESS_CIPHERS else 'auto',
                 _quote(params.get('uuid', ''))]
        parts += transport
        parts.append(f"alterId={params.get('alterId', 0) or 0}")
        tls = bool(params.get('tls'))
        parts.append(f'over-tls={_bool(tls)}')
        if tls:
            parts += _tls_opts(params)
        parts.append(udp)
        return ','.join(parts)

    if node_type == 'vless':
        transport = _transport_opts(params, params.get('network', 'tcp') or 'tcp')
        if transport is None:
            return None
        flow = params.get('flow', '')
        if flow and flow != 'xtls-rprx-vision':
            return None
        tls = bool(params.get('tls')) or params.get('security') in ('tls', 'reality') or bool(params.get('reality-opts'))
        parts = [f"{name} = VLESS", server, str(port), _quote(params.get('uuid', ''))]
        parts += transport
        if flow:
            parts.append(f'flow={flow}')
        parts.append(f'over-tls={_bool(tls)}')
        if tls:
            parts += _tls_opts(params)
        parts.append(udp)
        return ','.join(parts)

    if node_type == 'trojan':
        # Trojan 仅支持 tcp、ws（transport=http 按 WebSocket 处理）
        transport = _transport_opts(params, params.get('network', 'tcp') or 'tcp', allow_http=False)
        if transport is None:
            return None
        parts = [f"{name} = Trojan", server, str(port), _quote(params.get('password', ''))]
        parts += transport
        parts += _tls_opts(params)
        parts += [f"fast-open={_bool(params.get('tfo'))}", udp]
        return ','.join(parts)

    if node_type == 'hysteria2':
        parts = [f"{name} = Hysteria2", server, str(port), _quote(params.get('password', ''))]
        parts += _tls_opts(params)
        if params.get('obfs') == 'salamander' and params.get('obfs-password'):
            parts.append(f"salamander-password={_value(params['obfs-password'])}")
        ports = params.get('ports')
        if ports:
            parts.append(f"server-ports={_quote(ports)}")
            if params.get('hop-interval'):
                parts.append(f"hop-interval={params['hop-interval']}")
        down = params.get('down')
        if down:
            mbps = re.match(r'\s*(\d+)', str(down))
            if mbps:
                parts.append(f'download-bandwidth={mbps.group(1)}')
        parts += [f"fast-open={_bool(params.get('tfo'))}", 'udp=true']
        return ','.join(parts)

    if node_type == 'anytls':
        parts = [f"{name} = AnyTLS", server, str(port), _quote(params.get('password', ''))]
        parts += _tls_opts(params)
        if params.get('idle-session-timeout'):
            parts.append(f"idle-session-timeout={params['idle-session-timeout']}")
        parts.append(udp)
        return ','.join(parts)

    if node_type in ('http', 'https', 'socks5'):
        username = params.get('username', '')
        password = params.get('password', '')
        tls = node_type == 'https' or bool(params.get('tls'))
        if node_type == 'socks5':
            parts = [f"{name} = socks5", server, str(port)]
        else:
            parts = [f"{name} = {'https' if tls else 'http'}", server, str(port)]
        if username or password:
            parts += [_value(username), _quote(password)]
        if node_type == 'socks5':
            parts.append(f'over-tls={_bool(tls)}')
        if tls:
            parts += _tls_opts(params)
        if node_type == 'socks5':
            parts.append(udp)
        return ','.join(parts)

    if node_type in ('wireguard', 'wg'):
        private_key = params.get('private-key') or params.get('privateKey') or ''
        public_key = params.get('public-key') or params.get('publicKey') or params.get('peer-public-key') or ''
        if not private_key or not public_key:
            return None
        parts = [f"{name} = WireGuard"]
        ip = params.get('ip') or params.get('self-ip') or params.get('address') or ''
        if ip:
            parts.append(f"interface-ip={str(ip).split('/')[0]}")
        ipv6 = params.get('ipv6') or ''
        if ipv6:
            parts.append(f"interface-ipv6={str(ipv6).split('/')[0]}")
        parts.append(f"private-key={_quote(private_key)}")
        if params.get('mtu'):
            parts.append(f"mtu={params['mtu']}")
        dns = params.get('dns') or []
        dns = dns if isinstance(dns, list) else [d.strip() for d in str(dns).split(',')]
        dns_v4 = next((d for d in dns if d and ':' not in d), '')
        dns_v6 = next((d for d in dns if ':' in d), '')
        if dns_v4:
            parts.append(f'dns={dns_v4}')
        if dns_v6:
            parts.append(f'dnsv6={dns_v6}')
        keepalive = params.get('persistent-keepalive') or params.get('keepalive') or ''
        if keepalive:
            parts.append(f"keepalive={keepalive}")
        allowed_ips = params.get('allowed-ips') or params.get('allowed_ips') or '0.0.0.0/0'
        if isinstance(allowed_ips, list):
            allowed_ips = ','.join(allowed_ips)
        endpoint = params.get('endpoint') or (f'[{server}]:{port}' if ':' in str(server) else f'{server}:{port}')
        peer = [f"public-key={_quote(public_key)}"]
        preshared = params.get('pre-shared-key') or params.get('preshared-key') or params.get('presharedKey') or ''
        if preshared:
            peer.append(f"preshared-key={_quote(preshared)}")
        reserved = params.get('reserved')
        if isinstance(reserved, list) and reserved:
            peer.append(f"reserved=[{','.join(str(r) for r in reserved)}]")
        peer += [f'allowed-ips={_quote(allowed_ips)}', f'endpoint={endpoint}']
        parts.append(f"peers=[{{{','.join(peer)}}}]")
        parts.append(f"udp={_bool(params.get('udp', True))}")
        return ','.join(parts)

    # Loon 暂不支持 tuic、snell、hysteria(v1) 等协议
    return None
