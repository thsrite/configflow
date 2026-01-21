"""Mihomo (Clash Meta) 配置生成器"""
import yaml
from typing import Dict, Any, List
from backend.utils.subscription_parser import parse_uri_list
from backend.utils.logger import get_logger

# 获取当前模块的日志记录器
logger = get_logger(__name__)


class IndentDumper(yaml.Dumper):
    """自定义 YAML Dumper，增加列表项缩进"""

    def increase_indent(self, flow=False, indentless=False):
        return super(IndentDumper, self).increase_indent(flow, False)


def apply_github_proxy_domain(url: str, config_data: Dict[str, Any]) -> str:
    """
    为 GitHub URL 添加代理前缀
    格式：代理地址/原地址

    Args:
        url: 原始 URL
        config_data: 配置数据

    Returns:
        添加代理后的 URL
    """
    if not url:
        return url

    # 获取代理配置
    proxy_url = config_data.get('system_config', {}).get('github_proxy_domain', '').strip()
    if not proxy_url:
        return url

    # 常见的 GitHub 域名列表
    github_domains = [
        'github.com',
        'raw.githubusercontent.com',
        'gist.githubusercontent.com',
        'api.github.com'
    ]

    # 检查URL是否包含GitHub域名
    contains_github = any(domain in url for domain in github_domains)
    if not contains_github:
        return url

    # 确保代理地址格式正确
    # 如果只是域名，添加 https://
    if not proxy_url.startswith('http://') and not proxy_url.startswith('https://'):
        proxy_url = f'https://{proxy_url}'

    # 确保代理地址以 / 结尾
    if not proxy_url.endswith('/'):
        proxy_url += '/'

    # 拼接格式：代理地址/原地址
    result_url = f'{proxy_url}{url}'

    return result_url


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


def normalize_find_process_mode(mihomo_config: Dict[str, Any]) -> None:
    """保留 find-process-mode 的字符串取值，避免 off 变成布尔类型"""
    value = mihomo_config.get('find-process-mode')
    if value is None:
        return

    if isinstance(value, bool):
        mihomo_config['find-process-mode'] = 'always' if value else 'off'
        return

    if isinstance(value, str):
        normalized = value.strip().lower()
        if normalized in ('always', 'strict', 'off'):
            mihomo_config['find-process-mode'] = normalized


def generate_mihomo_config(config_data: Dict[str, Any], base_url: str = '') -> str:
    """生成 Mihomo YAML 配置"""

    # 从合并数组中分离规则和规则集
    rules_list, rule_sets_list = split_rules_and_rulesets(config_data)

    # 获取规则库
    rule_library = config_data.get('rule_library', [])

    # 检查是否有自定义配置（从嵌套结构中读取）
    mihomo_config_data = config_data.get('mihomo', {})
    custom_mihomo_config = mihomo_config_data.get('custom_config', '')

    if custom_mihomo_config and custom_mihomo_config.strip():
        # 使用自定义配置作为基础
        try:
            mihomo_config = yaml.safe_load(custom_mihomo_config)
            if not isinstance(mihomo_config, dict):
                mihomo_config = {}
        except:
            # 如果解析失败，使用默认配置
            mihomo_config = {}
    else:
        # 使用默认基础配置
        mihomo_config = {}

    # 如果没有基础配置或基础配置为空，使用默认配置
    if not mihomo_config:
        mihomo_config = {
            'mixed-port': 7890,
            'allow-lan': True,
            'bind-address': '*',
            'mode': 'rule',
            'log-level': 'info',
            'external-controller': '127.0.0.1:9090',
            'dns': {
                'enable': True,
                'listen': '0.0.0.0:53',
                'enhanced-mode': 'fake-ip',
                'fake-ip-range': '198.18.0.1/16',
                'nameserver': [
                    '223.5.5.5',
                    '119.29.29.29'
                ],
                'fallback': [
                    'https://1.1.1.1/dns-query',
                    'https://dns.google/dns-query'
                ]
            }
        }

    normalize_find_process_mode(mihomo_config)

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
            followed_group = next((g for g in config_data.get('proxy_groups', []) if g.get('id') == follow_group_id),
                                  None)
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
                logger.debug(f"策略组 '{group.get('name')}' 添加手动节点到 used_node_ids: {node_id}")

        # 收集聚合ID（聚合本身作为 provider）
        if aggregation_ids:
            for agg_id in aggregation_ids:
                used_aggregation_ids.add(agg_id)

        # 收集该策略组所有聚合中包含的订阅ID
        subscriptions_in_group_aggregations = set()
        if aggregation_ids:
            aggregations = config_data.get('subscription_aggregations', [])
            for agg_id in aggregation_ids:
                agg = next((a for a in aggregations if a['id'] == agg_id and a.get('enabled', True)), None)
                if agg:
                    agg_subs = agg.get('subscriptions', [])
                    subscriptions_in_group_aggregations.update(agg_subs)

        # 收集直接引用的订阅（跳过已在聚合中的订阅）
        for sub_id in subscriptions:
            # 如果该订阅已经在某个聚合中，不需要单独添加到 proxy-providers
            if sub_id not in subscriptions_in_group_aggregations:
                used_subscription_ids.add(sub_id)

        # 收集 proxies_order 中的节点（精确排序中的节点）
        proxies_order = group.get('proxies_order', [])
        if proxies_order:
            for item in proxies_order:
                if item.get('type') == 'node':
                    node_id = item.get('id')
                    if node_id not in ['DIRECT', 'REJECT']:
                        used_node_ids.add(node_id)
                        logger.debug(
                            f"策略组 '{group.get('name')}' 从 proxies_order 添加节点到 used_node_ids: {node_id}")

    # 收集被策略组直接选择的节点ID（通过 manual_nodes 或 proxies_order）
    directly_selected_node_ids = set()
    for group in config_data.get('proxy_groups', []):
        if not group.get('enabled', True):
            continue

        # 处理跟随模式
        follow_group_id = group.get('follow_group')
        if follow_group_id:
            followed_group = next((g for g in config_data.get('proxy_groups', []) if g.get('id') == follow_group_id),
                                  None)
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

    # 添加代理节点（只添加被策略组直接使用且启用的节点，排除聚合中的节点）
    proxies = []
    logger.debug(f"收集到的被使用节点ID: {used_node_ids}")
    logger.debug(f"开始生成 proxies，总节点数: {len(config_data.get('nodes', []))}")

    for node in config_data.get('nodes', []):
        # 跳过禁用的节点或未被使用的节点
        if not node.get('enabled', True):
            logger.debug(f"跳过禁用节点: {node.get('name')}")
            continue
        if node.get('id') not in used_node_ids:
            logger.debug(f"跳过未被策略组使用的节点: {node.get('name')} (id: {node.get('id')})")
            continue
        # 跳过仅通过聚合使用的节点（这些节点通过 proxy-providers 提供）
        # 如果节点被策略组直接选择了，即使在聚合中也应该出现
        if node.get('id') in nodes_only_in_aggregations:
            logger.debug(f"跳过仅通过聚合使用的节点: {node.get('name')} (id: {node.get('id')})")
            continue
        proxy = convert_node_to_mihomo(node)
        if proxy:
            proxies.append(proxy)
            logger.debug(f"添加节点到 proxies: {node.get('name')}")

    logger.debug(f"最终生成的 proxies 数量: {len(proxies)}")

    mihomo_config['proxies'] = proxies

    # 获取 server_domain（优先使用配置的服务域名）
    server_domain = config_data.get('system_config', {}).get('server_domain', '').strip()
    effective_base_url = server_domain or base_url

    # 获取 config_token（用于授权访问）
    config_token = config_data.get('system_config', {}).get('config_token', '')

    # 添加订阅提供者 (Proxy Providers) - 只添加被策略组直接使用且启用的订阅
    # 使用本地接口而不是原始订阅 URL
    proxy_providers = {}
    logger.info(f"开始生成订阅 proxy-providers，使用本地接口")
    logger.info(f"Server domain: {effective_base_url}")
    logger.info(f"Config token: {'已配置' if config_token else '未配置'}")

    for sub in config_data.get('subscriptions', []):
        if sub.get('enabled', True) and sub.get('id') in used_subscription_ids:
            # 构建订阅 provider 的 URL（使用本地接口）
            sub_id = sub['id']
            sub_url = f"{effective_base_url}/api/subscriptions/{sub_id}/proxies"

            # 如果配置了令牌，添加到 URL
            if config_token:
                sub_url += f"?token={config_token}"

            logger.info(f"订阅 '{sub['name']}' 使用本地接口: {sub_url}")

            proxy_providers[sub['name']] = {
                'type': 'http',
                'url': sub_url,
                'path': f"./providers/{sub['name']}.yaml",
                'interval': 3600,
                'health-check': {
                    'enable': True,
                    'url': 'http://www.gstatic.com/generate_204',
                    'interval': 300
                }
            }

    # 添加聚合提供者 - 只添加被策略组使用且启用的聚合

    for agg in config_data.get('subscription_aggregations', []):
        if agg.get('enabled', True) and agg.get('id') in used_aggregation_ids:
            # 构建聚合 provider 的 URL（使用服务域名配置）
            agg_id = agg['id']
            agg_url = f"{effective_base_url}/api/aggregations/{agg_id}/provider"

            # 如果配置了令牌，添加到 URL
            if config_token:
                agg_url += f"?token={config_token}"

            proxy_providers[agg['name']] = {
                'type': 'http',
                'url': agg_url,
                'path': f"./providers/{agg['name']}.yaml",
                'interval': 3600,
                'health-check': {
                    'enable': True,
                    'url': 'http://www.gstatic.com/generate_204',
                    'interval': 300
                }
            }

    if proxy_providers:
        mihomo_config['proxy-providers'] = proxy_providers

    # 添加策略组（只添加启用的策略组）
    proxy_groups = []
    logger.debug(f"开始处理策略组，总数: {len(config_data.get('proxy_groups', []))}")
    for group in config_data.get('proxy_groups', []):
        logger.debug(f"处理策略组: {group.get('name')}")
        # 跳过禁用的策略组
        if not group.get('enabled', True):
            logger.debug(f"跳过禁用的策略组: {group.get('name')}")
            continue

        proxy_group = {
            'name': group['name'],
            'type': group['type']
        }

        # 处理跟随模式
        follow_group_id = group.get('follow_group')
        if follow_group_id:
            # 查找被跟随的策略组
            followed_group = next((g for g in config_data.get('proxy_groups', []) if g.get('id') == follow_group_id),
                                  None)
            if followed_group:
                # 跟随模式：复制被跟随策略组的所有配置，只保留自己的名称
                proxy_group['type'] = followed_group['type']  # 类型也跟随

                # 复制被跟随策略组的节点来源设置
                manual_nodes = followed_group.get('manual_nodes', [])
                include_groups = followed_group.get('include_groups', [])
                subscriptions = followed_group.get('subscriptions', [])

                # 处理订阅来源
                if subscriptions:
                    use_list = []
                    for sub_id in subscriptions:
                        sub = next((s for s in config_data.get('subscriptions', []) if s['id'] == sub_id), None)
                        if sub:
                            use_list.append(sub['name'])
                    if use_list:
                        proxy_group['use'] = use_list

                    # 复制正则过滤：订阅的regex + 聚合的aggregation_regex
                    all_regex_filters = []

                    # 添加订阅的正则过滤
                    regex = followed_group.get('regex', '')
                    if regex and regex.strip():
                        all_regex_filters.append(regex.strip())

                    # 添加聚合的正则过滤
                    agg_regex = followed_group.get('aggregation_regex', '')
                    if agg_regex and agg_regex.strip():
                        all_regex_filters.append(agg_regex.strip())

                    # 如果有多个过滤器，用 | (或) 连接；如果只有一个，直接使用
                    if all_regex_filters:
                        if len(all_regex_filters) == 1:
                            proxy_group['filter'] = all_regex_filters[0]
                        else:
                            # 多个过滤器用括号包裹后用 | 连接
                            combined_filter = '|'.join(f'({f})' for f in all_regex_filters)
                            proxy_group['filter'] = combined_filter

                # 处理手动节点和引用策略
                combined_proxies = []
                proxies_order = followed_group.get('proxies_order', [])

                if proxies_order:
                    # 收集聚合 providers（用于 use 字段）
                    aggregation_subscriptions_from_follow = []

                    for item in proxies_order:
                        if item.get('type') == 'node':
                            node_id = item.get('id')
                            if node_id in ['DIRECT', 'REJECT']:
                                combined_proxies.append(node_id)
                            else:
                                node = next((n for n in config_data.get('nodes', []) if n.get('id') == node_id), None)
                                if node:
                                    combined_proxies.append(node['name'])
                        elif item.get('type') == 'strategy':
                            group_id = item.get('id')
                            ref_group = next(
                                (g for g in config_data.get('proxy_groups', []) if g.get('id') == group_id), None)
                            if ref_group:
                                combined_proxies.append(ref_group['name'])
                        elif item.get('type') == 'aggregation':
                            # 处理订阅聚合 - 聚合直接作为 provider
                            agg_id = item.get('id')
                            aggregations = config_data.get('subscription_aggregations', [])
                            agg = next((a for a in aggregations if a['id'] == agg_id and a.get('enabled', True)), None)
                            if agg:
                                # 将聚合添加到 use 字段（聚合作为 provider）
                                if agg['name'] not in aggregation_subscriptions_from_follow:
                                    aggregation_subscriptions_from_follow.append(agg['name'])

                    # 同时也要处理 aggregation_ids 中的聚合（可能不在 proxies_order 中）
                    if aggregation_ids:
                        aggregations = config_data.get('subscription_aggregations', [])
                        for agg_id in aggregation_ids:
                            agg = next((a for a in aggregations if a['id'] == agg_id and a.get('enabled', True)), None)
                            if agg and agg['name'] not in aggregation_subscriptions_from_follow:
                                aggregation_subscriptions_from_follow.append(agg['name'])

                    # 如果有聚合，添加到 use 字段
                    if aggregation_subscriptions_from_follow:
                        use_list = proxy_group.get('use', [])
                        for provider_name in aggregation_subscriptions_from_follow:
                            if provider_name not in use_list:
                                use_list.append(provider_name)
                        if use_list:
                            proxy_group['use'] = use_list
                else:
                    nodes_list = []
                    strategies_list = []

                    if manual_nodes:
                        for node_id in manual_nodes:
                            if node_id in ['DIRECT', 'REJECT']:
                                nodes_list.append(node_id)
                            else:
                                node = next((n for n in config_data.get('nodes', []) if n.get('id') == node_id), None)
                                if node:
                                    nodes_list.append(node['name'])

                    if include_groups:
                        for group_id in include_groups:
                            ref_group = next(
                                (g for g in config_data.get('proxy_groups', []) if g.get('id') == group_id), None)
                            if ref_group:
                                strategies_list.append(ref_group['name'])

                    proxy_order = followed_group.get('proxy_order', 'nodes_first')
                    if proxy_order == 'strategies_first':
                        combined_proxies = strategies_list + nodes_list
                    else:
                        combined_proxies = nodes_list + strategies_list

                if combined_proxies:
                    proxy_group['proxies'] = combined_proxies

                # 复制测试URL和间隔
                if followed_group.get('url'):
                    proxy_group['url'] = followed_group['url']
                if followed_group.get('interval'):
                    proxy_group['interval'] = followed_group['interval']

                # 复制url-test类型的默认字段
                if followed_group['type'] == 'url-test':
                    proxy_group['tolerance'] = 100
                    proxy_group['lazy'] = True

                # 复制负载均衡特有字段
                if followed_group['type'] == 'load-balance':
                    proxy_group['tolerance'] = 100
                    if followed_group.get('strategy'):
                        proxy_group['strategy'] = followed_group['strategy']
                    if followed_group.get('lazy') is not None:
                        proxy_group['lazy'] = followed_group['lazy']
            else:
                # 如果找不到被跟随的策略组，跳过此策略组
                continue
        else:
            # 原有的非跟随模式逻辑
            # 处理多种来源（新格式）
            manual_nodes = group.get('manual_nodes', [])
            aggregation_ids = group.get('aggregations', [])
            include_groups = group.get('include_groups', [])
            subscriptions = group.get('subscriptions', [])

            # 调试日志
            logger.debug(
                f"策略组 {group.get('name')} - aggregations: {aggregation_ids}, manual_nodes: {manual_nodes}, subscriptions: {subscriptions}")

            # 兼容旧格式数据
            if not manual_nodes and not aggregation_ids and not include_groups and not subscriptions:
                source = group.get('source', 'subscription')
                proxies = group.get('proxies', [])

                if source == 'subscription':
                    subscriptions = group.get('subscriptions', [])
                elif source == 'node':
                    manual_nodes = proxies
                elif source == 'strategy':
                    include_groups = proxies

            # 处理聚合来源 - 聚合作为独立的 provider，添加到 use 字段
            aggregation_providers = []
            # 收集所有聚合中包含的订阅ID（用于去重）
            subscriptions_in_current_aggregations = set()

            if aggregation_ids:
                aggregations = config_data.get('subscription_aggregations', [])
                for agg_id in aggregation_ids:
                    agg = next((a for a in aggregations if a['id'] == agg_id and a.get('enabled', True)), None)
                    if agg:
                        aggregation_providers.append(agg['name'])
                        # 收集该聚合包含的订阅ID
                        agg_subs = agg.get('subscriptions', [])
                        subscriptions_in_current_aggregations.update(agg_subs)

            # 处理订阅和聚合来源 - 添加到 use 字段
            use_list = []

            # 添加订阅（跳过已在聚合中的订阅，避免重复）
            if subscriptions:
                for sub_id in subscriptions:
                    # 如果该订阅已经在某个聚合中，跳过
                    if sub_id in subscriptions_in_current_aggregations:
                        sub = next((s for s in config_data.get('subscriptions', []) if s['id'] == sub_id), None)
                        if sub:
                            logger.debug(f"策略组 {group.get('name')} 跳过订阅 '{sub['name']}'，因为它已包含在聚合中")
                        continue

                    sub = next((s for s in config_data.get('subscriptions', []) if s['id'] == sub_id), None)
                    if sub:
                        use_list.append(sub['name'])

            # 添加聚合 providers
            if aggregation_providers:
                use_list.extend(aggregation_providers)

            if use_list:
                proxy_group['use'] = use_list

                # 合并正则过滤：订阅的 regex + 聚合的 aggregation_regex
                all_regex_filters = []

                # 添加订阅的正则过滤
                group_regex = group.get('regex', '')
                if group_regex and group_regex.strip():
                    all_regex_filters.append(group_regex.strip())

                # 添加聚合的正则过滤（来自策略组设置的aggregation_regex，不是聚合定义的regex_filter）
                agg_regex = group.get('aggregation_regex', '')
                if agg_regex and agg_regex.strip():
                    all_regex_filters.append(agg_regex.strip())

                # 如果有多个过滤器，用 | (或) 连接；如果只有一个，直接使用
                if all_regex_filters:
                    if len(all_regex_filters) == 1:
                        proxy_group['filter'] = all_regex_filters[0]
                    else:
                        # 多个过滤器用括号包裹后用 | 连接
                        combined_filter = '|'.join(f'({f})' for f in all_regex_filters)
                        proxy_group['filter'] = combined_filter

            # 处理手动节点和引用策略
            combined_proxies = []

            # 检查是否有 proxies_order（精确排序）
            proxies_order = group.get('proxies_order', [])

            # 调试日志
            logger.debug(f"策略组 {group.get('name')} - proxies_order: {proxies_order}")

            if proxies_order:
                # 使用精确排序
                # 收集聚合 providers（用于 use 字段）
                aggregation_subscriptions_from_order = []

                for item in proxies_order:
                    if item.get('type') == 'node':
                        node_id = item.get('id')
                        # 特殊值（DIRECT, REJECT）直接添加
                        if node_id in ['DIRECT', 'REJECT']:
                            combined_proxies.append(node_id)
                        else:
                            # 根据 ID 查找节点名称
                            node = next((n for n in config_data.get('nodes', []) if n.get('id') == node_id), None)
                            if node:
                                combined_proxies.append(node['name'])
                    elif item.get('type') == 'strategy':
                        group_id = item.get('id')
                        # 根据 ID 查找策略组名称
                        ref_group = next((g for g in config_data.get('proxy_groups', []) if g.get('id') == group_id),
                                         None)
                        if ref_group:
                            combined_proxies.append(ref_group['name'])
                    elif item.get('type') == 'aggregation':
                        # 处理订阅聚合 - 聚合直接作为 provider
                        agg_id = item.get('id')
                        aggregations = config_data.get('subscription_aggregations', [])
                        agg = next((a for a in aggregations if a['id'] == agg_id and a.get('enabled', True)), None)
                        if agg:
                            # 将聚合添加到 use 字段（聚合作为 provider）
                            if agg['name'] not in aggregation_subscriptions_from_order:
                                aggregation_subscriptions_from_order.append(agg['name'])
                        else:
                            logger.debug(f"未找到聚合 ID: {agg_id}")

                # 同时也要处理 aggregation_ids 中的聚合（可能不在 proxies_order 中）
                # 这样可以确保所有配置的聚合都会被添加到 use 字段
                if aggregation_ids:
                    aggregations = config_data.get('subscription_aggregations', [])
                    for agg_id in aggregation_ids:
                        agg = next((a for a in aggregations if a['id'] == agg_id and a.get('enabled', True)), None)
                        if agg and agg['name'] not in aggregation_subscriptions_from_order:
                            aggregation_subscriptions_from_order.append(agg['name'])

                # 如果有聚合，添加到 use 字段
                if aggregation_subscriptions_from_order:
                    use_list = proxy_group.get('use', [])
                    for provider_name in aggregation_subscriptions_from_order:
                        if provider_name not in use_list:
                            use_list.append(provider_name)
                    if use_list:
                        proxy_group['use'] = use_list
            else:
                # 没有精确排序时，使用旧逻辑
                nodes_list = []
                strategies_list = []

                # 将节点 ID 转换为节点名称
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

                # 将策略组 ID 转换为策略组名称
                if include_groups:
                    for group_id in include_groups:
                        # 根据 ID 查找策略组名称
                        ref_group = next((g for g in config_data.get('proxy_groups', []) if g.get('id') == group_id),
                                         None)
                        if ref_group:
                            strategies_list.append(ref_group['name'])

                # 根据 proxy_order 设置决定顺序，默认节点优先
                proxy_order = group.get('proxy_order', 'nodes_first')
                if proxy_order == 'strategies_first':
                    # 策略优先：先添加策略组，再添加节点
                    combined_proxies = strategies_list + nodes_list
                else:
                    # 节点优先（默认）：先添加节点，再添加策略组
                    combined_proxies = nodes_list + strategies_list

            if combined_proxies:
                proxy_group['proxies'] = combined_proxies
                logger.debug(f"策略组 {group.get('name')} 最终 proxies 列表: {combined_proxies}")

        if group.get('url'):
            proxy_group['url'] = group['url']
        if group.get('interval'):
            proxy_group['interval'] = group['interval']

        # url-test 类型默认字段
        if group['type'] == 'url-test':
            proxy_group['tolerance'] = 100
            proxy_group['lazy'] = True

        # 负载均衡特有字段
        if group['type'] == 'load-balance':
            proxy_group['tolerance'] = 100
            if group.get('strategy'):
                proxy_group['strategy'] = group['strategy']
            if group.get('lazy') is not None:
                proxy_group['lazy'] = group['lazy']

        proxy_groups.append(proxy_group)

    # 添加默认策略组
    if not proxy_groups:
        all_proxy_names = [p['name'] for p in proxies]
        if all_proxy_names:
            proxy_groups = [
                {
                    'name': 'PROXY',
                    'type': 'select',
                    'proxies': ['Auto'] + all_proxy_names
                },
                {
                    'name': 'Auto',
                    'type': 'url-test',
                    'proxies': all_proxy_names,
                    'url': 'http://www.gstatic.com/generate_204',
                    'interval': 300,
                    'tolerance': 100,
                    'lazy': True
                }
            ]

    mihomo_config['proxy-groups'] = proxy_groups

    # 添加规则提供者 (Rule Providers)
    rule_providers = {}
    # 保存规则集名称和behavior的映射，用于后续添加no-resolve
    ruleset_behaviors = {}

    # 获取 server_domain（优先使用配置的域名）
    server_domain = config_data.get('system_config', {}).get('server_domain', '').strip()
    # 如果没有配置 server_domain，则使用 base_url
    effective_base_url = server_domain or base_url

    for rule_set in rule_sets_list:
        if rule_set.get('enabled', True):
            # 检查是否关联规则库规则
            library_rule_id = rule_set.get('library_rule_id')
            url = rule_set.get('url', '')
            # 保存原始 URL 用于格式判断
            original_url = url

            if library_rule_id:
                # 查找规则库中的规则
                library_rule = next((r for r in rule_library if r['id'] == library_rule_id), None)
                if library_rule:
                    # 从 library_rule 获取原始 URL 用于格式判断
                    original_url = library_rule.get('url', url)

                    # 使用本地缓存的规则文件接口，通过规则名称访问
                    # 这样所有规则都从本地获取，避免外部网络请求
                    rule_name = library_rule.get('name', '')
                    if rule_name:
                        url = f"/api/rules/local/{rule_name}"

            # 如果 URL 是相对路径，动态拼接 server_domain
            if url and url.startswith('/') and effective_base_url:
                url = f"{effective_base_url}{url}"
            # 如果 URL 是 GitHub 地址，应用代理配置
            elif url and url.startswith('http'):
                url = apply_github_proxy_domain(url, config_data)

            behavior = rule_set.get('behavior', 'domain')

            # 根据原始 URL 后缀判断格式（使用原始 URL 而不是转换后的）
            rule_format = 'text'
            path_extension = 'list'
            if original_url and (original_url.endswith('.yaml') or original_url.endswith('.yml')):
                rule_format = 'yaml'
                path_extension = 'yaml'

            rule_providers[rule_set['name']] = {
                'type': 'http',
                'behavior': behavior,
                'url': url,
                'path': f"./ruleset/{rule_set['name']}.{path_extension}",
                'interval': 86400,
                'format': rule_format
            }
            # 保存behavior映射
            ruleset_behaviors[rule_set['name']] = behavior

    if rule_providers:
        mihomo_config['rule-providers'] = rule_providers

    # 添加规则（按照 rule_configs 数组的顺序）
    rules = []

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

            # MATCH 规则只有两个字段：MATCH,POLICY
            if rule_type == 'MATCH':
                rules.append(f"MATCH,{policy}")
            elif rule_type == 'RULE-SET':
                rules.append(f"RULE-SET,{value},{policy}")
            else:
                # 其他规则有三个字段：RULE_TYPE,VALUE,POLICY
                # 对于IP类型的规则，添加no-resolve参数
                if rule_type in ['IP-CIDR', 'IP-CIDR6', 'IP-SUFFIX', 'GEOIP']:
                    rules.append(f"{rule_type},{value},{policy},no-resolve")
                else:
                    rules.append(f"{rule_type},{value},{policy}")

        elif item_type == 'ruleset':
            # 规则集引用
            policy = item.get('policy', 'PROXY')
            ruleset_name = item['name']
            # 检查规则集的behavior，如果是ipcidr则添加no-resolve
            if ruleset_behaviors.get(ruleset_name) == 'ipcidr':
                rules.append(f"RULE-SET,{ruleset_name},{policy},no-resolve")
            else:
                rules.append(f"RULE-SET,{ruleset_name},{policy}")

    mihomo_config['rules'] = rules

    # 转换为 YAML
    return yaml.dump(
        mihomo_config,
        Dumper=IndentDumper,
        allow_unicode=True,
        default_flow_style=False,
        sort_keys=False,
        indent=2
    )


def get_mihomo_provider_downloads(config_data: Dict[str, Any], base_url: str = '') -> List[Dict[str, str]]:
    """
    获取 Mihomo 需要下载的 proxy provider 文件列表

    Args:
        config_data: 配置数据字典
        base_url: 基础 URL

    Returns:
        下载列表，格式：[{'name': 'provider名称', 'url': 'URL', 'local_path': './providers/xxx.yaml'}]
    """
    downloads = []

    # 获取系统配置
    server_domain = config_data.get('system_config', {}).get('server_domain', '').strip()
    effective_base_url = server_domain or base_url
    config_token = config_data.get('system_config', {}).get('config_token', '')

    # 首先找出所有被策略组使用的订阅和聚合 ID
    used_subscription_ids = set()
    used_aggregation_ids = set()

    for group in config_data.get('proxy_groups', []):
        if not group.get('enabled', True):
            continue

        # 处理跟随模式
        follow_group_id = group.get('follow_group')
        if follow_group_id:
            # 查找被跟随的策略组
            followed_group = next((g for g in config_data.get('proxy_groups', []) if g.get('id') == follow_group_id),
                                  None)
            if followed_group:
                # 使用被跟随策略组的设置
                aggregation_ids = followed_group.get('aggregations', [])
                subscriptions = followed_group.get('subscriptions', [])
            else:
                continue
        else:
            # 使用自己的设置
            aggregation_ids = group.get('aggregations', [])
            subscriptions = group.get('subscriptions', [])

        # 收集聚合ID（聚合本身作为 provider）
        if aggregation_ids:
            for agg_id in aggregation_ids:
                used_aggregation_ids.add(agg_id)

        # 收集该策略组所有聚合中包含的订阅ID
        subscriptions_in_group_aggregations = set()
        if aggregation_ids:
            aggregations = config_data.get('subscription_aggregations', [])
            for agg_id in aggregation_ids:
                agg = next((a for a in aggregations if a['id'] == agg_id and a.get('enabled', True)), None)
                if agg:
                    agg_subs = agg.get('subscriptions', [])
                    subscriptions_in_group_aggregations.update(agg_subs)

        # 收集直接引用的订阅（跳过已在聚合中的订阅）
        for sub_id in subscriptions:
            # 如果该订阅已经在某个聚合中，不需要单独添加到 proxy-providers
            if sub_id not in subscriptions_in_group_aggregations:
                used_subscription_ids.add(sub_id)

    # 处理订阅 providers
    for sub in config_data.get('subscriptions', []):
        if sub.get('enabled', True) and sub.get('id') in used_subscription_ids:
            sub_id = sub['id']
            sub_url = f"{effective_base_url}/api/subscriptions/{sub_id}/proxies"

            # 如果配置了令牌，添加到 URL
            if config_token:
                sub_url += f"?token={config_token}"

            downloads.append({
                'name': sub['name'],
                'url': sub_url,
                'local_path': f"./providers/{sub['name']}.yaml"
            })

    # 处理聚合 providers
    for agg in config_data.get('subscription_aggregations', []):
        if agg.get('enabled', True) and agg.get('id') in used_aggregation_ids:
            agg_id = agg['id']
            agg_url = f"{effective_base_url}/api/aggregations/{agg_id}/provider"

            # 如果配置了令牌，添加到 URL
            if config_token:
                agg_url += f"?token={config_token}"

            downloads.append({
                'name': agg['name'],
                'url': agg_url,
                'local_path': f"./providers/{agg['name']}.yaml"
            })

    return downloads


def get_mihomo_ruleset_downloads(config_data: Dict[str, Any], base_url: str = '') -> List[Dict[str, str]]:
    """
    获取 Mihomo 需要下载的 ruleset 文件列表

    Args:
        config_data: 配置数据字典
        base_url: 基础 URL

    Returns:
        下载列表，格式：[{'name': 'ruleset名称', 'url': 'URL', 'local_path': './ruleset/xxx.list'}]
    """
    downloads = []

    # 获取系统配置
    server_domain = config_data.get('system_config', {}).get('server_domain', '').strip()
    effective_base_url = server_domain or base_url

    # 获取所有规则集
    rule_sets_list = config_data.get('rule_sets', [])
    rule_library = config_data.get('rule_library', [])

    for rule_set in rule_sets_list:
        if rule_set.get('enabled', True):
            # 检查是否关联规则库规则
            library_rule_id = rule_set.get('library_rule_id')
            url = rule_set.get('url', '')
            # 保存原始 URL 用于格式判断
            original_url = url

            if library_rule_id:
                # 查找规则库中的规则
                library_rule = next((r for r in rule_library if r['id'] == library_rule_id), None)
                if library_rule:
                    # 从 library_rule 获取原始 URL 用于格式判断
                    original_url = library_rule.get('url', url)

                    # 使用本地缓存的规则文件接口
                    rule_name = library_rule.get('name', '')
                    if rule_name:
                        url = f"/api/rules/local/{rule_name}"

            # 如果 URL 是相对路径，动态拼接 server_domain
            if url and url.startswith('/') and effective_base_url:
                url = f"{effective_base_url}{url}"
            # 如果 URL 是 GitHub 地址，应用代理配置
            elif url and url.startswith('http'):
                url = apply_github_proxy_domain(url, config_data)

            # 根据原始 URL 后缀判断格式
            path_extension = 'list'
            if original_url and (original_url.endswith('.yaml') or original_url.endswith('.yml')):
                path_extension = 'yaml'

            downloads.append({
                'name': rule_set['name'],
                'url': url,
                'local_path': f"./ruleset/{rule_set['name']}.{path_extension}"
            })

    return downloads


def convert_node_to_mihomo(node: Dict[str, Any]) -> Dict[str, Any]:
    """将通用节点格式转换为 Mihomo 格式"""
    # 保存外层节点名称（界面设置的名称）
    outer_name = node.get('name', '')

    # 如果节点有 proxy_string 字段，需要先解析
    if 'proxy_string' in node:
        try:
            parsed_nodes = parse_uri_list(node['proxy_string'])
            if not parsed_nodes:
                return None
            # 使用解析后的节点数据
            parsed_node = parsed_nodes[0]

            # 如果是原始对象格式（YAML 对象），转换为 params 结构以便统一处理
            if parsed_node.get('_raw_object'):
                # 将扁平结构转换为 params 结构
                flat_node = {k: v for k, v in parsed_node.items() if k != '_raw_object'}
                node = {
                    'name': outer_name,
                    'type': flat_node.get('type', ''),
                    'server': flat_node.get('server', ''),
                    'port': flat_node.get('port', 0),
                    'params': {k: v for k, v in flat_node.items() if k not in ['name', 'type', 'server', 'port']}
                }
            else:
                parsed_node['name'] = outer_name  # 使用界面设置的名称
                node = parsed_node
        except:
            return None

    # 如果是扁平结构（有 _raw_object 标记），转换为 params 结构
    if node.get('_raw_object'):
        flat_node = {k: v for k, v in node.items() if k != '_raw_object'}
        node = {
            'name': flat_node.get('name', outer_name),
            'type': flat_node.get('type', ''),
            'server': flat_node.get('server', ''),
            'port': flat_node.get('port', 0),
            'params': {k: v for k, v in flat_node.items() if k not in ['name', 'type', 'server', 'port']}
        }

    node_type = node.get('type', '').lower()
    if not node_type:
        return None

    params = node.get('params', {})

    base = {
        'name': node['name'],
        'type': node_type,
        'server': node.get('server', ''),
        'port': node.get('port', 0)
    }

    if node_type == 'ss':
        base.update({
            'cipher': params.get('cipher', 'aes-256-gcm'),
            'password': params.get('password', '')
        })
        # 添加 UDP 支持
        if params.get('udp') or params.get('udp-relay'):
            base['udp'] = True
        # 添加 plugin 支持（如 obfs、v2ray-plugin 等）
        if params.get('plugin'):
            base['plugin'] = params['plugin']
        if params.get('plugin-opts'):
            base['plugin-opts'] = params['plugin-opts']

    elif node_type == 'ssr':
        base.update({
            'cipher': params.get('cipher', 'aes-256-cfb'),
            'password': params.get('password', ''),
            'protocol': params.get('protocol', 'origin'),
            'protocol-param': params.get('protocol-param', ''),
            'obfs': params.get('obfs', 'plain'),
            'obfs-param': params.get('obfs-param', '')
        })

    elif node_type == 'vmess':
        base.update({
            'uuid': params.get('uuid', ''),
            'alterId': params.get('alterId', 0),
            'cipher': params.get('cipher', 'auto'),
            'network': params.get('network', 'tcp'),
            'tls': params.get('tls', False)
        })

        if params.get('network') == 'ws':
            base['ws-opts'] = params.get('ws-opts', {})

        # 添加 UDP 支持
        if params.get('udp') or params.get('udp-relay'):
            base['udp'] = True

        # 添加其他可选字段
        if params.get('skip-cert-verify'):
            base['skip-cert-verify'] = params['skip-cert-verify']
        if params.get('servername') or params.get('sni'):
            base['servername'] = params.get('servername') or params.get('sni')

    elif node_type == 'trojan':
        base.update({
            'password': params.get('password', ''),
            'sni': params.get('sni', ''),
            'skip-cert-verify': params.get('skip-cert-verify', False)
        })
        # 添加 UDP 支持
        if params.get('udp') or params.get('udp-relay'):
            base['udp'] = True

    elif node_type in ['hysteria', 'hysteria2']:
        base.update({
            'password': params.get('password', ''),
            'obfs': params.get('obfs', ''),
            'sni': params.get('sni', ''),
            'skip-cert-verify': params.get('skip-cert-verify', False)
        })

    elif node_type == 'vless':
        # 基本字段
        base['udp'] = params.get('udp', True)
        base['uuid'] = params.get('uuid', '')
        # network: 兼容 URI 格式 (type) 和 mihomo 格式 (network)
        base['network'] = params.get('network') or params.get('type', 'tcp')
        base['flow'] = params.get('flow', '')
        base['packet-encoding'] = params.get('packet-encoding', '')

        # 加密方式（VLESS 通常是 none）
        encryption = params.get('encryption', 'none')
        if encryption:
            base['encryption'] = encryption

        # 处理 security 字段（tls/reality）
        security = params.get('security', 'none')

        # 检测是否为 reality 模式（通过 security 字段或已存在 reality-opts）
        has_reality_opts = 'reality-opts' in params
        is_reality_mode = security == 'reality' or has_reality_opts

        base['fingerprint'] = params.get('fingerprint', '')
        # client-fingerprint: 兼容 URI 格式 (fp) 和 mihomo 格式 (client-fingerprint)
        base['client-fingerprint'] = params.get('client-fingerprint') or params.get('fp', '')
        base['skip-cert-verify'] = params.get('skip-cert-verify', False)

        # tls: 兼容已设置的 tls 字段
        if params.get('tls') is not None:
            base['tls'] = params.get('tls')

        # servername: 兼容 URI 格式 (sni) 和 mihomo 格式 (servername)
        servername = params.get('servername') or params.get('sni', '')
        if servername:
            base['servername'] = servername

        # smux
        smux_value = params.get('smux')
        if isinstance(smux_value, dict):
            base['smux'] = smux_value
        else:
            base['smux'] = {
                'enabled': smux_value if isinstance(smux_value, bool) else False
            }

        # ALPN
        alpn = params.get('alpn')
        if alpn:
            if isinstance(alpn, str):
                base['alpn'] = alpn.split(',')
            else:
                base['alpn'] = alpn
        else:
            base['alpn'] = []

        if is_reality_mode:
            # Reality 模式
            base['tls'] = True

            # servername (从 sni 转换，如果尚未设置)
            if 'servername' not in base:
                sni = params.get('sni', '')
                if sni:
                    base['servername'] = sni

            # client-fingerprint (从 fp 转换，如果为空)
            if not base.get('client-fingerprint'):
                fp = params.get('fp', '')
                if fp:
                    base['client-fingerprint'] = fp

            # reality-opts: 优先使用已存在的，否则从 pbk/sid/spx 构建
            if has_reality_opts:
                base['reality-opts'] = params['reality-opts']
            else:
                reality_opts = {}
                pbk = params.get('pbk', '')
                if pbk:
                    reality_opts['public-key'] = pbk
                sid = params.get('sid', '')
                if sid:
                    reality_opts['short-id'] = sid
                spx = params.get('spx', '')
                if spx:
                    reality_opts['spider-x'] = spx
                if reality_opts:
                    base['reality-opts'] = reality_opts

        elif security == 'tls':
            # TLS 模式
            base['tls'] = True

            if 'servername' not in base:
                sni = params.get('sni', '')
                if sni:
                    base['servername'] = sni

            if not base.get('client-fingerprint'):
                fp = params.get('fp', '')
                if fp:
                    base['client-fingerprint'] = fp

        # 流控参数 (flow)
        flow = params.get('flow', '')
        if flow:
            base['flow'] = flow

        # WebSocket 选项
        network = base.get('network', '')
        if network == 'ws':
            ws_opts = params.get('ws-opts', {})
            if ws_opts:
                base['ws-opts'] = ws_opts

        # gRPC 选项
        if network == 'grpc':
            grpc_opts = params.get('grpc-opts', {})
            if grpc_opts:
                base['grpc-opts'] = grpc_opts

        # UDP 支持
        if params.get('udp'):
            base['udp'] = True

    elif node_type == 'anytls':
        # anytls 类型节点，将所有 params 参数复制到 base 中
        # 跳过一些可能冲突的字段
        for key, value in params.items():
            if key not in base:
                # 将下划线转换为中划线（例如 skip_cert_verify -> skip-cert-verify）
                formatted_key = key.replace('_', '-')
                base[formatted_key] = value

    return base
