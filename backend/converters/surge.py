"""Surge 配置生成器"""
import re
import os
from typing import Dict, Any, List
from backend.utils.subscription_parser import parse_uri_list
from backend.utils.logger import get_logger

logger = get_logger(__name__)


def get_aggregation_nodes(agg_id: str, config_data: Dict[str, Any]) -> List[Dict[str, Any]]:
    """从聚合 provider 文件中获取节点"""
    from backend.routes.aggregations import generate_aggregation_provider
    import yaml

    # 查找聚合
    aggregations = config_data.get('subscription_aggregations', [])
    aggregation = next((a for a in aggregations if a['id'] == agg_id), None)

    if not aggregation or not aggregation.get('enabled', True):
        return []

    try:
        # 生成 provider 文件（会自动重新解析订阅）
        result = generate_aggregation_provider(aggregation)
        file_path = result['file_path']

        # 读取生成的 YAML 文件
        with open(file_path, 'r', encoding='utf-8') as f:
            provider_data = yaml.safe_load(f)

        proxies = provider_data.get('proxies', [])
        logger.info(f"从聚合 '{aggregation['name']}' 获取了 {len(proxies)} 个节点")

        # 将 mihomo 格式的 proxy 转换为节点格式
        nodes = []
        for proxy in proxies:
            node = {'name': proxy.get('name'), **proxy}
            nodes.append(node)

        return nodes
    except Exception as e:
        logger.error(f"获取聚合节点失败: {e}")
        return []


def split_rules_and_rulesets(config_data: Dict[str, Any]) -> tuple:
    """从 rule_configs 中分离规则和规则集"""
    all_rules = config_data.get('rule_configs', [])
    rules = []
    rule_sets = []

    for item in all_rules:
        item_type = item.get('itemType', '')
        if item_type == 'rule':
            rules.append(item)
        elif item_type == 'ruleset':
            rule_sets.append(item)

    # 兼容旧格式
    if not all_rules:
        rules = config_data.get('rules', [])
        rule_sets = config_data.get('rule_sets', [])

    return rules, rule_sets


def generate_surge_config(config_data: Dict[str, Any], base_url: str = '') -> str:
    """
    生成 Surge 配置文件

    Args:
        config_data: 包含节点、策略组、规则等的配置字典
        base_url: 前端页面的 base URL（协议 + 主机 + 端口），用于构建完整的规则 URL

    Returns:
        str: Surge 格式的配置字符串
    """

    # 从合并数组中分离规则和规则集
    rules_list, rule_sets_list = split_rules_and_rulesets(config_data)

    # 获取 server_domain（优先使用配置的域名）
    server_domain = config_data.get('system_config', {}).get('server_domain', '').strip()
    # 如果没有配置 server_domain，则使用 base_url
    effective_base_url = server_domain or base_url

    sections = []
    wireguard_sections = []  # 存储WireGuard配置section

    # 检查是否有自定义配置（从嵌套结构中读取）
    surge_config_data = config_data.get('surge', {})
    custom_surge_config = surge_config_data.get('custom_config', '')

    # 读取 smart_groups 配置，构建 group_id → policy_priority 的映射
    smart_groups_config = surge_config_data.get('smart_groups', [])
    smart_group_map = {sg['group_id']: sg.get('policy_priority', '') for sg in smart_groups_config}

    if custom_surge_config and custom_surge_config.strip():
        # 使用自定义配置作为基础
        # 提取 [General] 部分（如果有）
        general_section = None
        lines = custom_surge_config.strip().split('\n')
        in_general = False
        general_lines = []

        for line in lines:
            if line.strip().startswith('[General]'):
                in_general = True
                general_lines.append(line)
            elif line.strip().startswith('[') and in_general:
                # 遇到下一个 section，停止
                break
            elif in_general:
                general_lines.append(line)

        if general_lines:
            general_section = '\n'.join(general_lines)
    else:
        general_section = None

    # 如果没有自定义 General 部分，使用默认配置
    if not general_section:
        general = [
            '[General]',
            'loglevel = notify',
            'internet-test-url = http://www.gstatic.com/generate_204',
            'proxy-test-url = http://www.gstatic.com/generate_204',
            'test-timeout = 3',
            'skip-proxy = localhost, *.local, injections.adguard.org, local.adguard.org, captive.apple.com, guzzoni.apple.com, 0.0.0.0/8, 10.0.0.0/8, 17.0.0.0/8, 100.64.0.0/10, 127.0.0.0/8, 169.254.0.0/16, 172.16.0.0/12, 192.0.0.0/24, 192.0.2.0/24, 192.168.0.0/16, 192.88.99.0/24, 198.18.0.0/15, 198.51.100.0/24, 203.0.113.0/24, 224.0.0.0/4, 240.0.0.0/4, 255.255.255.255/32',
            'dns-server = 223.5.5.5, 119.29.29.29, system',
            'ipv6 = true',
            'allow-wifi-access = true',
            'wifi-access-http-port = 6152',
            'wifi-access-socks5-port = 6153',
            'http-listen = 0.0.0.0:6152',
            'socks5-listen = 0.0.0.0:6153',
            'exclude-simple-hostnames = true'
        ]
        general_section = '\n'.join(general)

    sections.append(general_section)

    # 收集被策略组使用的节点ID、订阅ID和聚合ID
    used_node_ids = set()
    used_subscription_ids = set()
    used_aggregation_ids = set()

    logger.debug("开始收集被策略组使用的节点、订阅和聚合...")

    for group in config_data.get('proxy_groups', []):
        # 跳过禁用的策略组
        if not group.get('enabled', True):
            continue

        # 处理跟随模式
        follow_group_id = group.get('follow_group')
        if follow_group_id:
            # 查找被跟随的策略组
            followed_group = next((g for g in config_data.get('proxy_groups', []) if g.get('id') == follow_group_id), None)
            if followed_group:
                # 使用被跟随策略组的设置
                manual_nodes = followed_group.get('manual_nodes', [])
                aggregation_ids = followed_group.get('aggregations', [])
                subscriptions = followed_group.get('subscriptions', [])
            else:
                continue
        else:
            # 使用自己的设置
            manual_nodes = group.get('manual_nodes', [])
            aggregation_ids = group.get('aggregations', [])
            subscriptions = group.get('subscriptions', [])

        # 收集手动节点
        for node_id in manual_nodes:
            if node_id not in ['DIRECT', 'REJECT']:
                used_node_ids.add(node_id)

        # 收集直接引用的订阅
        for sub_id in subscriptions:
            used_subscription_ids.add(sub_id)

        # 收集聚合 ID（Surge 需要从聚合 provider 获取节点）
        if aggregation_ids:
            for agg_id in aggregation_ids:
                used_aggregation_ids.add(agg_id)

        # 收集 proxies_order 中的节点（精确排序中的节点）
        proxies_order = group.get('proxies_order', [])
        if proxies_order:
            for item in proxies_order:
                if item.get('type') == 'node':
                    node_id = item.get('id')
                    if node_id not in ['DIRECT', 'REJECT']:
                        used_node_ids.add(node_id)

    # 注意：订阅节点不再展开到 [Proxy] 部分，而是通过 policy-path 引用
    # 所以不需要收集订阅中的节点ID

    # 收集被策略组直接选择的节点ID（通过 manual_nodes 或 proxies_order）
    directly_selected_node_ids = set()
    for group in config_data.get('proxy_groups', []):
        if not group.get('enabled', True):
            continue

        # 处理跟随模式
        follow_group_id = group.get('follow_group')
        if follow_group_id:
            followed_group = next((g for g in config_data.get('proxy_groups', []) if g.get('id') == follow_group_id), None)
            if followed_group:
                manual_nodes = followed_group.get('manual_nodes', [])
                proxies_order = followed_group.get('proxies_order', [])
            else:
                continue
        else:
            manual_nodes = group.get('manual_nodes', [])
            proxies_order = group.get('proxies_order', [])

        # 收集直接选择的节点
        for node_id in manual_nodes:
            if node_id not in ['DIRECT', 'REJECT']:
                directly_selected_node_ids.add(node_id)

        # 收集 proxies_order 中的节点
        if proxies_order:
            for item in proxies_order:
                if item.get('type') == 'node':
                    node_id = item.get('id')
                    if node_id not in ['DIRECT', 'REJECT']:
                        directly_selected_node_ids.add(node_id)

    logger.debug(f"策略组直接选择的节点ID: {directly_selected_node_ids}")

    # 收集所有聚合中的节点ID，但排除被直接选择的节点
    # 只有"仅通过聚合使用"的节点才会被排除（没有被任何策略组直接选择）
    nodes_only_in_aggregations = set()
    for agg in config_data.get('subscription_aggregations', []):
        if agg.get('enabled', True) and agg.get('id') in used_aggregation_ids:
            agg_nodes = agg.get('nodes', [])
            # 只添加没有被直接选择的节点
            for node_id in agg_nodes:
                if node_id not in directly_selected_node_ids:
                    nodes_only_in_aggregations.add(node_id)
            logger.debug(f"聚合 '{agg.get('name')}' 包含节点: {agg_nodes}")

    logger.debug(f"仅通过聚合使用的节点ID（将被排除）: {nodes_only_in_aggregations}")

    # [Proxy] 部分（只添加被策略组直接使用且启用的节点，排除聚合中的节点）
    # 聚合节点通过 policy-path 引用，不需要添加到 [Proxy]
    proxies = ['[Proxy]']

    logger.debug(f"收集到的被使用节点ID: {used_node_ids}")
    logger.debug(f"开始生成 Surge proxies，总节点数: {len(config_data.get('nodes', []))}")

    # 添加手动节点
    for node in config_data.get('nodes', []):
        # 跳过禁用的节点或未被使用的节点
        if not node.get('enabled', True):
            logger.debug(f"跳过禁用节点: {node.get('name')}")
            continue
        if node.get('id') not in used_node_ids:
            logger.debug(f"跳过未被策略组使用的节点: {node.get('name')} (id: {node.get('id')})")
            continue
        # 跳过仅通过聚合使用的节点（这些节点通过 policy-path 提供）
        # 如果节点被策略组直接选择了，即使在聚合中也应该出现
        if node.get('id') in nodes_only_in_aggregations:
            logger.debug(f"跳过仅通过聚合使用的节点: {node.get('name')} (id: {node.get('id')})")
            continue
        proxy_line, wg_section = convert_node_to_surge(node)
        if proxy_line:
            proxies.append(proxy_line)
            logger.debug(f"添加节点到 Surge proxies: {node.get('name')}")
        # 如果有WireGuard section，添加到列表
        if wg_section:
            wireguard_sections.append(wg_section)

    logger.debug(f"最终生成的 Surge proxies 数量: {len(proxies) - 1}")  # -1 because of '[Proxy]' header

    # 注意：DIRECT 和 REJECT 是 Surge 内置策略，不需要在 [Proxy] 部分定义

    sections.append('\n'.join(proxies))

    # [Proxy Group] 部分（只添加启用的策略组）
    proxy_groups = ['[Proxy Group]']

    for group in config_data.get('proxy_groups', []):
        # 跳过禁用的策略组
        if not group.get('enabled', True):
            continue
        group_line = convert_proxy_group_to_surge(group, config_data, base_url, smart_group_map)
        if group_line:
            proxy_groups.append(group_line)

    # 如果没有策略组，添加默认策略组（只包含启用的节点）
    if len(proxy_groups) == 1:
        all_proxy_names = [node['name'] for node in config_data.get('nodes', []) if node.get('enabled', True)]
        if all_proxy_names:
            proxy_groups.append(f"Proxy = select, {', '.join(all_proxy_names)}")
            proxy_groups.append(f"Auto = url-test, {', '.join(all_proxy_names)}, url = http://www.gstatic.com/generate_204, interval = 300")

    sections.append('\n'.join(proxy_groups))

    # [Rule] 部分（按照 rule_configs 数组的顺序）
    rules = ['[Rule]']

    # 创建规则集名称和behavior的映射，用于后续添加no-resolve
    ruleset_behaviors = {}
    # 创建规则集名称和URL的映射，用于判断格式
    ruleset_urls = {}

    # 从 rule_library 中获取规则集的 behavior 和 URL 信息
    rule_library = config_data.get('rule_library', [])
    for lib_rule in rule_library:
        if lib_rule.get('enabled', True):
            ruleset_name = lib_rule.get('name', '')
            ruleset_behaviors[ruleset_name] = lib_rule.get('behavior', '')
            ruleset_urls[ruleset_name] = lib_rule.get('url', '')

    # Surge 规则类型映射（mihomo/Clash → Surge）
    # Surge 没有 SRC-IP-CIDR，SRC-IP 原生支持 CIDR；Surge 用 DEST-PORT 而非 DST-PORT
    surge_rule_type_map = {
        'SRC-IP-CIDR': 'SRC-IP',
        'DST-PORT': 'DEST-PORT',
    }

    # 遍历合并后的规则数组，保持用户配置的顺序
    for item in config_data.get('rule_configs', []):
        if not item.get('enabled', True):
            continue

        item_type = item.get('itemType', '')

        if item_type == 'rule':
            # 单条规则
            rule_type = item['rule_type']
            value = item.get('value', '')
            policy = item['policy']

            # MATCH 规则在 Surge 中是 FINAL，只有两个字段：FINAL,POLICY
            if rule_type == 'MATCH':
                rules.append(f"FINAL,{policy}")
            elif rule_type == 'RULE-SET':
                rules.append(f"RULE-SET,{value},{policy}")
            # 逻辑规则类型（AND、OR、NOT）需要特殊处理
            elif rule_type in ['AND', 'OR', 'NOT']:
                # Surge 逻辑规则格式：AND,((SRC-IP,192.168.1.110), (DOMAIN,example.com)),POLICY
                # 1. 子条件之间需要空格：), ( 而不是 ),(
                surge_value = value.replace('),(', '), (')
                # 2. 替换子条件中的规则类型为 Surge 等效类型
                for mihomo_type, surge_type in surge_rule_type_map.items():
                    surge_value = surge_value.replace(mihomo_type, surge_type)
                rules.append(f"{rule_type},{surge_value},{policy}")
            else:
                # 标准规则类型，映射为 Surge 等效类型
                surge_rule_type = surge_rule_type_map.get(rule_type, rule_type)
                # 根据配置决定是否添加 no-resolve 参数
                no_resolve = item.get('no_resolve', False)
                if no_resolve:
                    rules.append(f"{surge_rule_type},{value},{policy},no-resolve")
                else:
                    rules.append(f"{surge_rule_type},{value},{policy}")

        elif item_type == 'ruleset':
            # 规则集引用
            policy = item.get('policy', 'Proxy')
            url = item.get('url', '')
            ruleset_name = item.get('name', '')

            # 保存原始 URL 用于格式判断
            original_url = ruleset_urls.get(ruleset_name, url)

            # 如果 URL 是相对路径，动态拼接 server_domain
            if url and url.startswith('/') and effective_base_url:
                url = f"{effective_base_url}{url}"

            # 如果从 rule_library 获取不到 URL，使用 item 中的 URL
            if not url and ruleset_name in ruleset_urls:
                url = ruleset_urls[ruleset_name]
                if url and url.startswith('/') and effective_base_url:
                    url = f"{effective_base_url}{url}"

            # 构建规则选项列表
            rule_options = []

            # 根据配置决定是否添加 no-resolve 参数
            no_resolve = item.get('no_resolve', False)
            if no_resolve:
                rule_options.append('no-resolve')

            # 使用原始 URL 判断格式，如果是 YAML 格式则添加 rule-set-format
            if original_url and (original_url.endswith('.yaml') or original_url.endswith('.yml')):
                rule_options.append('rule-set-format=yaml')

            # 组合规则
            if rule_options:
                rules.append(f"RULE-SET,{url},{policy},{','.join(rule_options)}")
            else:
                rules.append(f"RULE-SET,{url},{policy}")

    sections.append('\n'.join(rules))

    # 添加WireGuard sections（如果有）
    if wireguard_sections:
        sections.extend(wireguard_sections)

    # 组合所有部分
    config_output = '\n\n'.join(sections)

    # 在最上方添加远程托管配置
    config_token = config_data.get('system_config', {}).get('config_token', '')
    surge_url = f"{effective_base_url}/api/config/surge"
    if config_token:
        surge_url += f"?token={config_token}"
    managed_line = f"#!MANAGED-CONFIG {surge_url} interval=86400 strict=true"

    return f"{managed_line}\n\n{config_output}"


def convert_proxies_to_surge_text(proxies: List[Dict[str, Any]]) -> str:
    """将 mihomo 格式 proxies 列表转换为 Surge 纯文本格式（每行一个节点）

    用于 policy-path 返回 Surge 原生格式，替代 YAML 格式。

    Args:
        proxies: mihomo 格式的 proxy dict 列表

    Returns:
        str: Surge 格式纯文本，每行一个节点
    """
    from backend.utils.sub_store_client import proxies_to_nodes

    nodes = proxies_to_nodes(proxies)
    lines = []
    for node in nodes:
        try:
            proxy_line, wireguard_section = convert_node_to_surge(node)
            # 跳过 WireGuard 节点（policy-path 不支持需要独立 section 的节点）
            if wireguard_section:
                continue
            if proxy_line:
                lines.append(proxy_line)
        except Exception as e:
            logger.warning(f"转换节点到 Surge 格式失败: {node.get('name', '?')}, 错误: {e}")
            continue
    return '\n'.join(lines)


def convert_node_to_surge(node: Dict[str, Any]) -> tuple:
    """
    将通用节点格式转换为 Surge 配置行

    Returns:
        tuple: (proxy_line, wireguard_section)
        - proxy_line: 节点配置行
        - wireguard_section: WireGuard section配置（仅WireGuard节点有，其他为None）
    """
    # 保存外层节点名称（界面设置的名称）
    outer_name = node.get('name', '')

    # 如果节点有 proxy_string 字段，需要先解析
    is_raw_object = False
    if 'proxy_string' in node:
        try:
            parsed_nodes = parse_uri_list(node['proxy_string'])
            if not parsed_nodes:
                return None, None
            # 使用解析后的节点数据
            parsed_node = parsed_nodes[0]

            # 检查是否是原始对象格式
            if parsed_node.get('_raw_object'):
                is_raw_object = True
                # 移除标记字段，使用外层节点名称
                parsed_node = {k: v for k, v in parsed_node.items() if k != '_raw_object'}
                parsed_node['name'] = outer_name  # 使用界面设置的名称
            else:
                parsed_node['name'] = outer_name  # 使用界面设置的名称

            node = parsed_node
        except:
            return None, None

    node_type = node.get('type', '').lower()
    if not node_type:
        return None, None

    name = node['name']
    server = node.get('server', '')
    port = node.get('port', 0)

    # 对象格式：参数在顶层；解析格式：参数在 params 中
    if is_raw_object:
        params = node  # 对象格式，所有参数都在顶层
    else:
        params = node.get('params', {})

    if node_type == 'ss':
        # Shadowsocks: name = ss, server, port, encrypt-method=cipher, password=pwd, udp-relay=true, test-url=xxx
        cipher = params.get('cipher', 'aes-256-gcm')
        password = params.get('password', '')

        parts = [f"{name} = ss", server, str(port), f"encrypt-method={cipher}", f"password={password}"]

        # 添加可选参数
        if params.get('udp') or params.get('udp-relay'):
            parts.append("udp-relay=true")

        if params.get('test-url'):
            parts.append(f"test-url={params['test-url']}")

        return ', '.join(parts), None

    elif node_type == 'vmess':
        # VMess: name = vmess, server, port, username=uuid, ...
        uuid = params.get('uuid', '')
        tls = 'tls=true' if params.get('tls', False) else ''
        network = params.get('network', 'tcp')

        parts = [f"{name} = vmess", server, str(port), f"username={uuid}"]

        if tls:
            parts.append(tls)

        if network == 'ws':
            ws_opts = params.get('ws-opts', {})
            path = ws_opts.get('path', '/')
            host = ws_opts.get('headers', {}).get('Host', '')

            parts.append('ws=true')
            parts.append(f'ws-path={path}')
            if host:
                parts.append(f'ws-headers=Host:{host}')

        # 添加 UDP 支持
        if params.get('udp') or params.get('udp-relay'):
            parts.append('udp-relay=true')

        return ', '.join(parts), None

    elif node_type == 'trojan':
        # Trojan: name = trojan, server, port, password=pwd, ...
        password = params.get('password', '')
        sni = params.get('sni', '')
        skip_cert = params.get('skip-cert-verify', False)

        parts = [f"{name} = trojan", server, str(port), f"password={password}"]

        if sni:
            parts.append(f"sni={sni}")
        if skip_cert:
            parts.append("skip-cert-verify=true")

        # 添加 UDP 支持
        if params.get('udp') or params.get('udp-relay'):
            parts.append('udp-relay=true')

        return ', '.join(parts), None

    elif node_type == 'hysteria2':
        # Hysteria2: name = hysteria2, server, port, password=pwd, ...
        password = params.get('password', '')
        sni = params.get('sni', '')
        skip_cert = params.get('skip-cert-verify', False)

        parts = [f"{name} = hysteria2", server, str(port), f"password={password}"]

        if sni:
            parts.append(f"sni={sni}")
        if skip_cert:
            parts.append("skip-cert-verify=true")

        # 添加 UDP 支持
        if params.get('udp') or params.get('udp-relay'):
            parts.append('udp-relay=true')

        return ', '.join(parts), None

    elif node_type == 'http' or node_type == 'https':
        # HTTP(S): name = http/https, server, port, username, password
        username = params.get('username', '')
        password = params.get('password', '')

        if username and password:
            return f"{name} = {node_type}, {server}, {port}, {username}, {password}", None
        else:
            return f"{name} = {node_type}, {server}, {port}", None

    elif node_type == 'snell':
        # Snell: name = snell, server, port, psk=password, version=4, reuse=true, udp-relay=true
        psk = params.get('psk', '') or params.get('password', '')
        version = params.get('version', 4)

        parts = [f"{name} = snell", server, str(port), f"psk={psk}"]

        if version:
            parts.append(f"version={version}")

        # 添加可选参数
        if params.get('reuse'):
            parts.append("reuse=true")

        if params.get('udp-relay') or params.get('udp'):
            parts.append("udp-relay=true")

        # obfs参数
        obfs = params.get('obfs', '')
        if obfs:
            parts.append(f"obfs={obfs}")
            obfs_host = params.get('obfs-host', '')
            if obfs_host:
                parts.append(f"obfs-host={obfs_host}")

        return ', '.join(parts), None

    elif node_type == 'tuic' or node_type == 'tuic-v5':
        # Tuic: name = tuic-v5, server, port, uuid=xxx, password=xxx, alpn=h3
        # 或者: name = tuic, server, port, token=xxx, alpn=h3
        uuid = params.get('uuid', '')
        password = params.get('password', '')
        token = params.get('token', '')

        parts = [f"{name} = tuic-v5", server, str(port)]

        if uuid:
            parts.append(f"uuid={uuid}")
        if password:
            parts.append(f"password={password}")
        elif token:
            parts.append(f"token={token}")

        # ALPN
        alpn = params.get('alpn', 'h3')
        if alpn:
            parts.append(f"alpn={alpn}")

        # SNI
        sni = params.get('sni', '')
        if sni:
            parts.append(f"sni={sni}")

        # 跳过证书验证
        if params.get('skip-cert-verify', False):
            parts.append("skip-cert-verify=true")

        # UDP relay
        if params.get('udp-relay') or params.get('udp'):
            parts.append("udp-relay=true")

        return ', '.join(parts), None

    elif node_type == 'vless':
        # VLESS 转换为 Surge 的 vmess (因为Surge不原生支持VLESS)
        # 或者使用 external 方式
        # 这里尝试转换为类似的格式
        uuid = params.get('uuid', '')
        flow = params.get('flow', '')
        tls = params.get('tls', False) or params.get('security', '') == 'tls'
        network = params.get('network', 'tcp')

        # Surge可能不完全支持VLESS，但可以尝试用vmess格式
        parts = [f"{name} = vmess", server, str(port), f"username={uuid}"]

        if tls:
            parts.append('tls=true')
            sni = params.get('sni', '') or params.get('servername', '')
            if sni:
                parts.append(f'sni={sni}')

        if network == 'ws':
            ws_opts = params.get('ws-opts', {}) or params.get('ws-config', {})
            path = ws_opts.get('path', '/') or params.get('path', '/')
            host = ws_opts.get('headers', {}).get('Host', '') or params.get('host', '')

            parts.append('ws=true')
            parts.append(f'ws-path={path}')
            if host:
                parts.append(f'ws-headers=Host:{host}')

        elif network == 'grpc':
            grpc_service = params.get('grpc-opts', {}).get('grpc-service-name', '') or params.get('serviceName', '')
            if grpc_service:
                parts.append(f'grpc-service-name={grpc_service}')

        # 跳过证书验证
        if params.get('skip-cert-verify', False):
            parts.append("skip-cert-verify=true")

        # 添加 UDP 支持
        if params.get('udp') or params.get('udp-relay'):
            parts.append('udp-relay=true')

        return ', '.join(parts), None

    elif node_type == 'wireguard' or node_type == 'wg':
        # WireGuard: name = wireguard, section-name = xxx
        # 需要生成两部分：1) 引用行 2) WireGuard section配置

        section_name = params.get('section-name', name.replace(' ', '-').replace('_', '-'))

        # 1. 生成引用行
        proxy_line_parts = [f"{name} = wireguard", f"section-name = {section_name}"]

        # 可选参数
        mtu = params.get('mtu')
        if mtu:
            proxy_line_parts.append(f"mtu={mtu}")

        proxy_line = ', '.join(proxy_line_parts)

        # 2. 生成WireGuard section配置
        wg_section_lines = [f"[WireGuard {section_name}]"]

        # 必需参数
        private_key = params.get('private-key', '') or params.get('privateKey', '')
        if private_key:
            wg_section_lines.append(f"private-key = {private_key}")

        # self-ip (客户端IP)
        self_ip = params.get('self-ip', '') or params.get('ip', '') or params.get('address', '')
        if self_ip:
            wg_section_lines.append(f"self-ip = {self_ip}")

        # DNS服务器
        dns = params.get('dns', '') or params.get('dns-server', '')
        if dns:
            if isinstance(dns, list):
                dns = ', '.join(dns)
            wg_section_lines.append(f"dns-server = {dns}")

        # MTU
        if mtu:
            wg_section_lines.append(f"mtu = {mtu}")

        # Peer配置（最重要的部分）
        peer_public_key = params.get('public-key', '') or params.get('publicKey', '') or params.get('peer-public-key', '')
        endpoint = params.get('endpoint', '') or f"{server}:{port}"
        allowed_ips = params.get('allowed-ips', '') or params.get('allowed_ips', '0.0.0.0/0, ::/0')

        if peer_public_key:
            peer_parts = [f"public-key = {peer_public_key}"]
            peer_parts.append(f"endpoint = {endpoint}")

            if isinstance(allowed_ips, list):
                allowed_ips = ', '.join(allowed_ips)
            peer_parts.append(f'allowed-ips = "{allowed_ips}"')

            # Keepalive
            keepalive = params.get('keepalive', '') or params.get('persistent-keepalive', '')
            if keepalive:
                peer_parts.append(f"keepalive = {keepalive}")

            # Preshared key
            preshared_key = params.get('preshared-key', '') or params.get('presharedKey', '')
            if preshared_key:
                peer_parts.append(f"preshared-key = {preshared_key}")

            # Client ID (某些实现需要)
            client_id = params.get('client-id', '') or params.get('reserved', '')
            if client_id:
                peer_parts.append(f"client-id = {client_id}")

            wg_section_lines.append(f"peer = ({', '.join(peer_parts)})")

        wg_section = '\n'.join(wg_section_lines)

        return proxy_line, wg_section

    # 不支持的类型，返回 None
    return None, None


def convert_proxy_group_to_surge(group: Dict[str, Any], config_data: Dict[str, Any], base_url: str = '', smart_group_map: Dict[str, str] = None) -> str:
    """将策略组转换为 Surge 格式"""
    # 获取 effective_base_url
    server_domain = config_data.get('system_config', {}).get('server_domain', '').strip()
    effective_base_url = server_domain or base_url

    name = group['name']
    group_type = group['type']

    # 检查是否需要以 smart 模式输出
    policy_priority = None
    if smart_group_map and group.get('id') in smart_group_map:
        policy_priority = smart_group_map[group['id']]
        group_type = 'smart'

    # 处理跟随模式
    follow_group_id = group.get('follow_group')
    if follow_group_id:
        # 查找被跟随的策略组
        followed_group = next((g for g in config_data.get('proxy_groups', []) if g.get('id') == follow_group_id), None)
        if followed_group:
            # 跟随模式：复制被跟随策略组的所有配置，只保留自己的名称
            manual_nodes = followed_group.get('manual_nodes', [])
            aggregation_ids = followed_group.get('aggregations', [])
            include_groups = followed_group.get('include_groups', [])
            subscriptions = followed_group.get('subscriptions', [])
            # 临时创建一个合并后的 group 对象用于后续处理（包含类型、正则、排序等）
            group = {
                **group,  # 保留原策略组的 name 和其他属性
                'type': followed_group['type'],  # 类型也跟随
                'manual_nodes': manual_nodes,
                'aggregations': aggregation_ids,
                'include_groups': include_groups,
                'subscriptions': subscriptions,
                'regex': followed_group.get('regex', ''),
                'proxies_order': followed_group.get('proxies_order', []),
                'proxy_order': followed_group.get('proxy_order', 'nodes_first'),
                'url': followed_group.get('url'),  # 跟随测试URL
                'interval': followed_group.get('interval')  # 跟随测试间隔
            }
            # 更新group_type为跟随的类型（但保留 smart 覆盖）
            if group_type != 'smart':
                group_type = followed_group['type']
        else:
            # 如果找不到被跟随的策略组，跳过此策略组
            return None
    else:
        # 原有的非跟随模式逻辑
        # 处理多种来源（新格式）
        manual_nodes = group.get('manual_nodes', [])
        aggregation_ids = group.get('aggregations', [])
        include_groups = group.get('include_groups', [])
        subscriptions = group.get('subscriptions', [])

    # 兼容旧格式数据
    if not manual_nodes and not aggregation_ids and not include_groups and not subscriptions:
        source = group.get('source', 'subscription')
        proxies_old = group.get('proxies', [])

        if source == 'subscription':
            subscriptions = group.get('subscriptions', [])
        elif source == 'node':
            manual_nodes = proxies_old
        elif source == 'strategy':
            include_groups = proxies_old

    # 处理聚合来源 - 使用 policy-path 方式引用
    # Surge 支持 policy-path，不需要展开聚合节点到 [Proxy] 部分

    # 合并所有来源的节点
    all_proxies = []

    # 注意：订阅节点不再展开到配置中，而是通过 policy-path 引用本地接口
    # 这样可以避免配置文件过大，并且节点更新时不需要重新生成配置
    # 订阅会在后面通过 policy-path 添加

    # 检查是否有 proxies_order（精确排序）
    proxies_order = group.get('proxies_order', [])

    if proxies_order:
        # 使用精确排序
        for item in proxies_order:
            if item.get('type') == 'node':
                node_id = item.get('id')
                # 特殊值（DIRECT, REJECT）直接添加
                if node_id in ['DIRECT', 'REJECT']:
                    all_proxies.append(node_id)
                else:
                    # 根据 ID 查找节点名称
                    node = next((n for n in config_data.get('nodes', []) if n.get('id') == node_id), None)
                    if node:
                        all_proxies.append(node['name'])
            elif item.get('type') == 'strategy':
                group_id = item.get('id')
                # 根据 ID 查找策略组名称
                ref_group = next((g for g in config_data.get('proxy_groups', []) if g.get('id') == group_id), None)
                if ref_group:
                    all_proxies.append(ref_group['name'])

        # 对于聚合展开的节点，如果不在 proxies_order 中，也需要添加
        # 获取 proxies_order 中已经包含的节点ID
        order_node_ids = {item.get('id') for item in proxies_order if item.get('type') == 'node'}

        # 添加来自聚合但不在 proxies_order 中的手动节点
        for node_id in manual_nodes:
            if node_id not in order_node_ids:
                if node_id in ['DIRECT', 'REJECT']:
                    all_proxies.append(node_id)
                else:
                    node = next((n for n in config_data.get('nodes', []) if n.get('id') == node_id), None)
                    if node:
                        all_proxies.append(node['name'])
                        logger.debug(f"Added aggregation node '{node['name']}' not in proxies_order")
    else:
        # 没有精确排序时，使用旧逻辑
        nodes_list = []
        strategies_list = []

        # 2. 添加手动节点（将节点 ID 转换为名称）
        if manual_nodes:
            for node_id in manual_nodes:
                # 特殊值（DIRECT, REJECT）直接添加
                if node_id in ['DIRECT', 'REJECT']:
                    nodes_list.append(node_id)
                else:
                    # 根据 ID 查找节点名称
                    node = next((n for n in config_data.get('nodes', []) if n.get('id') == node_id), None)
                    if node:
                        nodes_list.append(node['name'])

        # 3. 添加引用的策略组（将策略组 ID 转换为名称）
        if include_groups:
            for group_id in include_groups:
                # 根据 ID 查找策略组名称
                ref_group = next((g for g in config_data.get('proxy_groups', []) if g.get('id') == group_id), None)
                if ref_group:
                    strategies_list.append(ref_group['name'])

        # 根据 proxy_order 设置决定节点和策略的顺序，默认节点优先
        proxy_order = group.get('proxy_order', 'nodes_first')
        if proxy_order == 'strategies_first':
            # 策略优先：先添加策略组，再添加节点
            all_proxies.extend(strategies_list)
            all_proxies.extend(nodes_list)
        else:
            # 节点优先（默认）：先添加节点，再添加策略组
            all_proxies.extend(nodes_list)
            all_proxies.extend(strategies_list)

    # 构建 policy-path 参数
    policy_paths = []

    # 获取 config_token（用于授权访问）
    config_token = config_data.get('system_config', {}).get('config_token', '')

    # 添加订阅的 policy-path（使用本地接口）
    if subscriptions:
        all_subs = config_data.get('subscriptions', [])
        for sub_id in subscriptions:
            sub = next((s for s in all_subs if s['id'] == sub_id and s.get('enabled', True)), None)
            if sub:
                # 构建订阅 URL（使用本地接口）
                sub_url = f"{effective_base_url}/api/subscriptions/{sub_id}/proxies"
                # 如果配置了令牌，添加到 URL
                if config_token:
                    sub_url += f"?token={config_token}&format=surge"
                else:
                    sub_url += f"?format=surge"
                policy_paths.append(sub_url)

    # 添加聚合的 policy-path
    if aggregation_ids:
        aggregations = config_data.get('subscription_aggregations', [])
        for agg_id in aggregation_ids:
            agg = next((a for a in aggregations if a['id'] == agg_id and a.get('enabled', True)), None)
            if agg:
                # 构建聚合 URL
                agg_url = f"{effective_base_url}/api/aggregations/{agg_id}/provider"
                # 如果配置了令牌，添加到 URL
                if config_token:
                    agg_url += f"?token={config_token}&format=surge"
                else:
                    agg_url += f"?format=surge"
                policy_paths.append(agg_url)

    # 如果没有普通节点但有 policy-path，允许继续生成
    if not all_proxies and not policy_paths:
        logger.debug(f"Group '{name}' has no proxies and no policy-paths, skipping")
        return None

    # 构建基础策略组行
    proxy_list = ', '.join(all_proxies) if all_proxies else ''

    # 构建策略组字符串
    if group_type == 'select':
        group_line = f"{name} = select"
        if proxy_list:
            group_line += f", {proxy_list}"
    elif group_type == 'url-test':
        url = group.get('url', 'http://www.gstatic.com/generate_204')
        interval = group.get('interval', 300)
        group_line = f"{name} = url-test"
        if proxy_list:
            group_line += f", {proxy_list}"
        group_line += f", url = {url}, interval = {interval}"
    elif group_type == 'fallback':
        url = group.get('url', 'http://www.gstatic.com/generate_204')
        interval = group.get('interval', 300)
        group_line = f"{name} = fallback"
        if proxy_list:
            group_line += f", {proxy_list}"
        group_line += f", url = {url}, interval = {interval}"
    elif group_type == 'load-balance':
        url = group.get('url', 'http://www.gstatic.com/generate_204')
        interval = group.get('interval', 300)
        group_line = f"{name} = load-balance"
        if proxy_list:
            group_line += f", {proxy_list}"
        group_line += f", url = {url}, interval = {interval}"
    elif group_type == 'smart':
        url = group.get('url', 'http://www.gstatic.com/generate_204')
        interval = group.get('interval', 300)
        group_line = f"{name} = smart"
        if proxy_list:
            group_line += f", {proxy_list}"
        if policy_priority:
            group_line += f", policy-priority={policy_priority}"
        group_line += f", url = {url}, interval = {interval}"
    else:
        return None

    # 添加 policy-path 参数
    if policy_paths:
        for policy_path in policy_paths:
            group_line += f", policy-path = {policy_path}, update-interval = 86400"

    logger.debug(f"Group '{name}' final line: {group_line}")
    return group_line


def get_proxy_group_nodes(group: Dict[str, Any], config_data: Dict[str, Any]) -> List[str]:
    """
    获取策略组的节点列表
    合并订阅筛选的节点和手动选择的节点
    """
    result_nodes = []
    all_nodes = config_data.get('nodes', [])

    # 1. 处理订阅筛选的节点
    subscriptions = group.get('subscriptions', [])
    if subscriptions:
        logger.debug(f"get_proxy_group_nodes: Looking for nodes with subscription_id in {subscriptions}")
        logger.debug(f"Total nodes in config: {len(all_nodes)}")
        # 获取指定订阅的所有节点（只包含启用的节点）
        subscription_nodes = [
            node for node in all_nodes
            if node.get('subscription_id') in subscriptions and node.get('enabled', True)
        ]
        logger.debug(f"Found {len(subscription_nodes)} matching nodes")

        # 应用正则过滤
        regex_pattern = group.get('regex', '')
        if regex_pattern:
            try:
                regex = re.compile(regex_pattern)
                subscription_nodes = [
                    node for node in subscription_nodes
                    if regex.search(node['name'])
                ]
            except re.error:
                # 正则表达式无效，不过滤
                pass

        # 添加到结果列表
        result_nodes.extend([node['name'] for node in subscription_nodes])

    # 2. 添加手动选择的节点
    manual_proxies = group.get('proxies', [])
    if manual_proxies:
        for proxy_name in manual_proxies:
            if proxy_name not in result_nodes:
                result_nodes.append(proxy_name)

    # 去重并保持顺序
    seen = set()
    unique_nodes = []
    for node in result_nodes:
        if node not in seen:
            seen.add(node)
            unique_nodes.append(node)

    return unique_nodes
