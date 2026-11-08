"""Mihomo (Clash Meta) 配置生成器"""
import re
import yaml
from collections import OrderedDict
from contextlib import contextmanager
from copy import deepcopy
from sys import getsizeof
from threading import RLock, local
from urllib.parse import urlsplit
from typing import Dict, Any, List, Optional
from backend.utils.logger import get_logger
from backend.utils.proxy_utils import fix_proxy_fields
from backend.common.profile_context import append_url_query, profile_api_path
from backend.utils.url_utils import safe_url_for_log
from backend.converters.common import emitted_rules, parse_hosts_pairs, ruleset_format, udp_fallback

# 获取当前模块的日志记录器
logger = get_logger(__name__)

class CustomConfigYAMLError(Exception):
    """mihomo.custom_config 解析/校验失败。

    生成路径在解析失败时抛此异常，上游 Flask 路由透传到 HTTP 500，
    确保不会写 /data/config.yaml，保留上一份有效产物。
    """


def validate_ipv6_flags(document):
    """Do not let a visual boolean become a misspelled/quoted YAML string."""
    if not isinstance(document, dict):
        return
    if 'ipv6' in document and type(document['ipv6']) is not bool:
        raise CustomConfigYAMLError('ipv6 必须为 true 或 false，不能是字符串或拼写错误的值')
    dns = document.get('dns')
    if isinstance(dns, dict) and 'ipv6' in dns and type(dns['ipv6']) is not bool:
        raise CustomConfigYAMLError('dns 下的 ipv6 必须为 true 或 false，不能是字符串或拼写错误的值')


def _default_mihomo_template() -> dict:
    """首次初始化时的内嵌默认模板。

    仅用于 mihomo.custom_config 字段从未设置过的场景；
    解析失败时**不再** fallback 到这个模板，而是抛 CustomConfigYAMLError。
    """
    return {
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
            # 写入该键会替换内核默认名单，前三项沿用内核默认；对时服务器需要真实地址，
            # 否则客户端长期缓存的假地址被回收后，对时会发到无关域名
            'fake-ip-filter': [
                'dns.msftnsci.com',
                'www.msftnsci.com',
                'www.msftconnecttest.com',
                '+.pool.ntp.org',
            ],
            'nameserver': [
                '223.5.5.5',
                '119.29.29.29',
            ],
            'fallback': [
                'https://1.1.1.1/dns-query',
                'https://dns.google/dns-query',
            ],
        },
    }




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

    # 获取代理配置（添加空值检查）
    if not config_data:
        return url

    configured_proxy = config_data.get('system_config', {}).get('github_proxy_domain', '')
    proxy_url = configured_proxy.strip() if isinstance(configured_proxy, str) else ''

    if not proxy_url:
        return url

    # 常见的 GitHub 域名列表
    github_domains = [
        'github.com',
        'raw.githubusercontent.com',
        'gist.githubusercontent.com',
        'api.github.com'
    ]

    # 仅改写 GitHub 源站；内部回调的查询参数也可能包含 GitHub URL。
    if urlsplit(url).hostname not in github_domains:
        return url

    # 确保代理地址格式正确
    # 如果只是域名，添加 https://
    if not proxy_url.startswith('http://') and not proxy_url.startswith('https://'):
        proxy_url = f'https://{proxy_url}'

    # 确保代理地址以 / 结尾
    if not proxy_url.endswith('/'):
        proxy_url += '/'

    if url.startswith(proxy_url):
        return url

    # 拼接格式：代理地址/原地址
    result_url = f'{proxy_url}{url}'

    return result_url


def split_rules_and_rulesets(config_data: Dict[str, Any]) -> tuple:
    """从 rule_configs 中分离规则和规则集"""
    all_rules = config_data.get('rule_configs', [])
    rules = []
    rule_sets = []

    for item in all_rules:
        if not item.get('enabled', True) or not item.get('library_enabled', True):
            continue
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


# Provider 健康检查默认地址
DEFAULT_HEALTH_CHECK_URL = 'https://www.gstatic.com/generate_204'


def build_provider_health_check(source: Dict[str, Any]) -> Dict[str, Any]:
    """构建 Provider 的健康检查配置

    默认使用境外地址探测。回家/内网类订阅从国内网络出网，用境外地址
    检查必然超时而被误判为失活，客户端因此不会选用这些节点，故允许每个
    订阅/聚合单独指定检查地址（如 http://www.baidu.com）。
    """
    if 'health_check' in source:
        from backend.utils.health_check import provider_health_check
        return provider_health_check(source)
    url = str(source.get('health_check_url') or '').strip() or DEFAULT_HEALTH_CHECK_URL
    return {
        'enable': True,
        'url': url,
        'interval': 300,
        # 懒惰检测：仅当 Provider 被策略组实际使用时才探测，
        # 避免闲置节点持续占用 CPU（低配设备上尤其明显）
        'lazy': True
    }


# 逻辑规则类型：no-resolve 不能作为第四个字段追加，Mihomo 会把它当成策略名
LOGIC_RULE_TYPES = ('AND', 'OR', 'NOT')

# 会触发域名解析的目标 IP 类规则（SRC-IP-CIDR 匹配来源 IP，不触发解析）
_RESOLVE_TRIGGER_RULE_TYPES = ('IP-CIDR', 'IP-CIDR6', 'IP-SUFFIX', 'GEOIP', 'IP-ASN')


def apply_no_resolve_to_logic_rule(value: str) -> str:
    """为逻辑规则内部的目标 IP 类子条件补上 no-resolve

    Mihomo 的逻辑规则（AND/OR/NOT）不接受尾部的 no-resolve 参数，会报
    `proxy [no-resolve] not found` 导致配置加载失败；正确写法是写在子条件内：

        AND,((SRC-IP-CIDR,10.0.0.0/24),(IP-CIDR,10.0.0.0/24,no-resolve)),DIRECT
    """
    def _inject(match: 're.Match') -> str:
        inner = match.group(1)
        parts = [p.strip() for p in inner.split(',')]
        if not parts or parts[0].upper() not in _RESOLVE_TRIGGER_RULE_TYPES:
            return match.group(0)
        if any(p.lower() == 'no-resolve' for p in parts):
            return match.group(0)
        return f"({inner},no-resolve)"

    return re.sub(r'\(([^()]*)\)', _inject, value)


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


def parse_mosdns_custom_hosts(custom_hosts: str) -> Dict[str, str]:
    """解析 MosDNS 自定义 Hosts 文本为 {域名: IP} 映射

    与 MosDNS 转换器保持一致，支持两种格式：
    1. 域名在前，IP 在后：example.com 192.168.1.1（推荐格式）
    2. IP 在前，域名在后：192.168.1.1 example.com（传统 hosts 格式）
    """
    return dict(parse_hosts_pairs(custom_hosts))


def sync_mosdns_hosts(mihomo_config: Dict[str, Any], config_data: Dict[str, Any]) -> None:
    """将 MosDNS 自定义 Hosts 同步注入 Mihomo 配置

    MosDNS 的自定义 Hosts 只对查询 MosDNS 的客户端生效；Mihomo 自身
    使用独立的 DNS 上游（不能指回 MosDNS，否则形成解析回环），因此
    内网域名映射需要同步写入 Mihomo 顶层 hosts，避免 Mihomo 自身
    （订阅更新、节点域名解析）经公网 NAT 回环访问内网服务。

    注意：仅用于推送给局域网内 Agent 的配置（sync_lan_hosts=True）；
    订阅/下载配置可能被漫游设备（手机在外）使用，注入内网 IP 会导致
    这些域名在外网不可达，因此默认不注入。

    - 自定义配置中已显式写的同名 hosts 条目优先，不覆盖
    - hosts 参与解析需要 dns.use-hosts: true；用户显式配置过则不改动
    """
    synced = parse_mosdns_custom_hosts(
        config_data.get('mosdns', {}).get('custom_hosts', '')
    )
    if not synced:
        return

    hosts = mihomo_config.get('hosts')
    if not isinstance(hosts, dict):
        hosts = {}
    for domain, ip in synced.items():
        hosts.setdefault(domain, ip)
    mihomo_config['hosts'] = hosts

    dns_config = mihomo_config.get('dns')
    if isinstance(dns_config, dict) and 'use-hosts' not in dns_config:
        dns_config['use-hosts'] = True


REGION_PROBE_GROUP = 'ConfigFlow-Region-Probe'
REGION_PROBE_LISTENER = 'configflow-region-probe'
DEFAULT_REGION_PROBE_PORT = 17999
_INBOUND_PORT_KEYS = ('port', 'socks-port', 'mixed-port', 'redir-port', 'tproxy-port')


def mihomo_used_ports(mihomo_config: Dict[str, Any]) -> set:
    """配置里已经占用的入站端口。"""
    used = {mihomo_config.get(key) for key in _INBOUND_PORT_KEYS}
    listeners = mihomo_config.get('listeners')
    if isinstance(listeners, list):
        used |= {item.get('port') for item in listeners
                 if isinstance(item, dict) and item.get('name') != REGION_PROBE_LISTENER}
    return {port for port in used if isinstance(port, int)}


def mihomo_listen_ports(mihomo_config: Dict[str, Any]) -> Dict[int, str]:
    """{端口: 配置项}：入站、listeners 与 external-controller 占用的端口。"""
    owners = {mihomo_config[key]: key for key in _INBOUND_PORT_KEYS if isinstance(mihomo_config.get(key), int)}
    owners.update({port: 'listeners' for port in mihomo_used_ports(mihomo_config) if port not in owners})
    controller = mihomo_config.get('external-controller')
    port = controller.rpartition(':')[2] if isinstance(controller, str) else ''
    if port.isdigit():
        owners.setdefault(int(port), 'external-controller')
    return owners


def inject_region_probe(mihomo_config: Dict[str, Any], port: int) -> bool:
    """注入区域检测入口：只监听本机的 mixed 入站，流量全部交给隐藏的选择组。

    Agent 切换这个组到某个节点 / 策略组后经入口发请求，从而得到经该节点访问的完整响应。
    组不被任何规则引用，不影响真实流量。端口冲突时不注入并返回 False。
    """
    if port in mihomo_used_ports(mihomo_config):
        logger.warning('区域检测入口端口 %s 与已有入站冲突，本次未注入', port)
        return False
    groups = [group for group in mihomo_config.get('proxy-groups') or [] if group.get('name') != REGION_PROBE_GROUP]
    groups.append({
        'name': REGION_PROBE_GROUP,
        'type': 'select',
        # 策略组用于检测「当前路径」；include-all 收进全部节点（含订阅 provider 里的）
        'proxies': ['DIRECT', *[group['name'] for group in groups if group.get('name')]],
        'include-all': True,
        'hidden': True,
    })
    mihomo_config['proxy-groups'] = groups
    listeners = mihomo_config.get('listeners')
    listeners = [item for item in listeners if not (isinstance(item, dict) and item.get('name') == REGION_PROBE_LISTENER)] \
        if isinstance(listeners, list) else []
    listeners.append({'name': REGION_PROBE_LISTENER, 'type': 'mixed', 'listen': '127.0.0.1',
                      'port': port, 'proxy': REGION_PROBE_GROUP})
    mihomo_config['listeners'] = listeners
    return True


def generate_mihomo_config(config_data: Dict[str, Any], base_url: str = '',
                           sync_lan_hosts: bool = False, *, preflight_providers: bool = True,
                           region_probe: bool = False, report: Optional[Dict[str, Any]] = None,
                           mihomo_core_version: Optional[str] = None) -> str:
    """生成 Mihomo YAML 配置

    Args:
        sync_lan_hosts: 是否把 MosDNS 自定义 Hosts（内网域名映射）注入
            顶层 hosts。仅推送给局域网内 Agent 时为 True；订阅/下载
            配置保持 False，避免漫游设备在外网解析到内网 IP。
        report: 传入字典时写入 ``subscription_hosts``：从订阅合并、忽略的
            hosts 条目及原因，供预览展示；有「域名 → 域名」映射时含 ``dns``
            （被映射的节点域名改由本机 DNS 监听解析、映射目标加入 Fake-IP 过滤的结果）。
        mihomo_core_version: 目标设备上报的 Mihomo 版本（推送给 Agent 时已知才传）；
            早于 v1.19.20 时不写 ``proxy-server-nameserver-policy``。
    """

    from backend.utils.proxy_chains import lower_proxy_chains
    logical_config = config_data
    config_data = lower_proxy_chains(config_data)
    # 从合并数组中分离规则和规则集
    rules_list, rule_sets_list = split_rules_and_rulesets(config_data)

    # 获取规则库
    rule_library = config_data.get('rule_library', [])

    # 检查是否有自定义配置（从嵌套结构中读取）
    # 策略变更：v1.3.0 的实现会在 yaml.safe_load 抛异常时静默设置
    # mihomo_config = {} 并 fallback 到内嵌默认模板（mixed-port 7890 /
    # log-level info / external-controller 127.0.0.1:9090 / dns.listen
    # 0.0.0.0:53 / 223.5.5.5+119.29.29.29 / fallback 1.1.1.1+dns.google），
    # 导致用户的 tun / dns.listen / nameserver-policy 等被静默抹掉。
    #
    # 改为"硬失败"：
    # - custom_config 解析失败 / 结果非 dict 时抛 CustomConfigYAMLError
    # - 上游 Flask 路由透传到 HTTP 500，不写 config.yaml
    # - 上一份有效 /data/config.yaml 不会被覆盖
    # - 内嵌默认模板仅在 custom_config 字段从未被设置时使用（首次初始化）
    mihomo_config_data = config_data.get('mihomo', {})
    custom_mihomo_config = mihomo_config_data.get('custom_config', '')

    if not (custom_mihomo_config and custom_mihomo_config.strip()):
        # 没有任何 custom_config —— 首次初始化场景，使用内嵌默认模板
        mihomo_config = _default_mihomo_template()
    else:
        try:
            mihomo_config = yaml.safe_load(custom_mihomo_config)
        except yaml.YAMLError as e:
            logger.error(
                'mihomo.custom_config YAML 解析失败，本次生成被中止，'
                '/data/config.yaml 未被覆盖。原因: %s '
                '（常见原因：nameserver-policy / fake-ip-filter 等 mapping 段缩进错乱，'
                '请到 UI → Mihomo 基础配置 → 修正后重新保存并重新生成。）',
                e
            )
            raise CustomConfigYAMLError(
                f'mihomo.custom_config YAML 解析失败: {e}'
            ) from e

        if not isinstance(mihomo_config, dict):
            logger.error(
                'mihomo.custom_config 解析成功但不是 mapping（得到 %s），'
                '本次生成被中止，/data/config.yaml 未被覆盖。',
                type(mihomo_config).__name__
            )
            raise CustomConfigYAMLError(
                f'mihomo.custom_config 不是 mapping，得到 {type(mihomo_config).__name__}'
            )

    validate_ipv6_flags(mihomo_config)
    normalize_find_process_mode(mihomo_config)

    # 订阅 hosts 不能覆盖手写条目，也排在 MosDNS 同步条目之后
    written_hosts = mihomo_config.get('hosts')
    user_host_keys = list(written_hosts) if isinstance(written_hosts, dict) else []

    # 仅 Agent 推送场景同步 MosDNS 自定义 Hosts（内网域名映射）
    if sync_lan_hosts:
        sync_mosdns_hosts(mihomo_config, config_data)
    synced_hosts = mihomo_config.get('hosts')
    mosdns_host_keys = [key for key in synced_hosts if key not in user_host_keys] \
        if isinstance(synced_hosts, dict) else []

    # 收集被策略组使用的节点ID、订阅ID和聚合ID
    # Logical chains are policies in their own right, even without a parent group.
    used_node_ids = {n['id'] for n in config_data.get('nodes', []) if n.get('_proxy_chain_id')}
    used_node_ids.update(g['_chain_dialer']['id'] for g in config_data.get('proxy_groups', [])
                         if g.get('_chain_dialer', {}).get('type') == 'node')
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

    from backend.utils.dialer_references import dependency_node_ids, overlay_dialer, group_members
    for group in config_data.get('proxy_groups', []):
        if group.get('enabled', True):
            legacy_nodes = {id for kind, id in group_members(group) if kind == 'node' and id not in ('DIRECT', 'REJECT')}
            used_node_ids.update(legacy_nodes)
            directly_selected_node_ids.update(legacy_nodes)
    aggregation_roots = {id for agg in config_data.get('subscription_aggregations', [])
                         if agg.get('enabled', True) and agg.get('id') in used_aggregation_ids
                         for id in agg.get('nodes', [])}
    required_dependencies = dependency_node_ids(config_data, used_node_ids | aggregation_roots, include_roots=False)
    nodes_only_in_aggregations -= required_dependencies | directly_selected_node_ids
    used_node_ids = dependency_node_ids(config_data, used_node_ids | aggregation_roots)
    # 添加代理节点（包括拨号依赖）
    proxies = []
    inline_proxies = []
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
        proxy = overlay_dialer(config_data, node, convert_node_to_mihomo(node))
        if proxy:
            proxies.append(proxy)
            inline_proxies.append((node, proxy))
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
    from backend.utils.subscription_refresh import DEFAULT_PROVIDER_INTERVAL, validate_refresh_interval
    provider_interval = validate_refresh_interval(
        config_data.get('mihomo', {}).get('provider_update_interval', DEFAULT_PROVIDER_INTERVAL),
        'Mihomo 订阅同步间隔')
    logger.debug(f"开始生成订阅 proxy-providers，使用本地接口")
    logger.debug(f"Server domain: {safe_url_for_log(effective_base_url)}")
    logger.debug(f"Config token: {'已配置' if config_token else '未配置'}")

    for sub in config_data.get('subscriptions', []):
        if sub.get('enabled', True) and sub.get('id') in used_subscription_ids:
            # 构建订阅 provider 的 URL（使用本地接口）
            sub_id = sub['id']
            sub_url = f"{effective_base_url}{profile_api_path(config_data, f'/subscriptions/{sub_id}/proxies')}"

            # 如果配置了令牌，添加到 URL
            if config_token:
                sub_url = append_url_query(sub_url, {'token': config_token})

            logger.debug(f"订阅 '{sub['name']}' 使用本地接口: {safe_url_for_log(sub_url)}")

            proxy_providers[sub['name']] = {
                'type': 'http',
                'url': sub_url,
                'path': f"./providers/{sub['name']}.yaml",
                'interval': provider_interval,
                'health-check': build_provider_health_check(sub)
            }

    # 添加聚合提供者 - 只添加被策略组使用且启用的聚合

    for agg in config_data.get('subscription_aggregations', []):
        if agg.get('enabled', True) and agg.get('id') in used_aggregation_ids:
            # 构建聚合 provider 的 URL（使用服务域名配置）
            agg_id = agg['id']
            agg_url = f"{effective_base_url}{profile_api_path(config_data, f'/aggregations/{agg_id}/provider')}"

            # 如果配置了令牌，添加到 URL
            if config_token:
                agg_url = append_url_query(agg_url, {'token': config_token})

            proxy_providers[agg['name']] = {
                'type': 'http',
                'url': agg_url,
                'path': f"./providers/{agg['name']}.yaml",
                'interval': provider_interval,
                'health-check': build_provider_health_check(agg)
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

                from backend.utils.group_candidates import render_members
                proxy_group.update(render_members(followed_group, config_data))

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
                    if followed_group.get('strategy'):
                        proxy_group['strategy'] = followed_group['strategy']
                    if followed_group.get('lazy') is not None:
                        proxy_group['lazy'] = followed_group['lazy']
            else:
                # 如果找不到被跟随的策略组，跳过此策略组
                continue
        else:
            from backend.utils.group_candidates import render_members
            proxy_group.update(render_members(group, config_data))

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
            if group.get('strategy'):
                proxy_group['strategy'] = group['strategy']
            if group.get('lazy') is not None:
                proxy_group['lazy'] = group['lazy']
        settings = followed_group if follow_group_id else group
        for key in ('url', 'interval', 'tolerance', 'lazy', 'strategy', 'timeout', 'max-failed-times',
                    'expected-status', 'disable-udp', 'hidden', 'icon', 'exclude-filter', 'exclude-type'):
            if key in settings:
                proxy_group[key] = settings[key]

        # Follow mode can change the effective type; only url-test supports
        # tolerance, including values copied from legacy persisted settings.
        if proxy_group['type'] != 'url-test':
            proxy_group.pop('tolerance', None)
        proxy_groups.append(proxy_group)

    # 添加默认策略组
    if not proxy_groups and not any(n.get('_proxy_chain_id') for n in config_data.get('nodes', [])):
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
                    'url': 'https://www.gstatic.com/generate_204',
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
                        url = profile_api_path(config_data, f"/rules/local/{rule_name}")
                        if ruleset_format(rule_set, config_data, original_url) != 'mrs':
                            url = append_url_query(url, {'target': 'mihomo'})

            # 如果 URL 是相对路径，动态拼接 server_domain
            if url and url.startswith('/') and effective_base_url:
                url = f"{effective_base_url}{url}"
            # 如果 URL 是 GitHub 地址，应用代理配置
            elif url and url.startswith('http'):
                url = apply_github_proxy_domain(url, config_data)

            behavior = rule_set.get('behavior', 'domain')

            rule_format = ruleset_format(rule_set, config_data, original_url)
            path_extension = 'list' if rule_format == 'text' else rule_format

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

    # 网关只改生成结果；默认关闭时不触碰既有配置。
    from backend.common.linux_gateway import apply_linux_gateway
    apply_linux_gateway(mihomo_config, config_data, effective_base_url)

    # 添加规则（按照 rule_configs 数组的顺序）
    rules = []

    # 遍历合并后的规则数组，保持用户配置的顺序
    for item in emitted_rules(config_data):
        if not item.get('enabled', True) or not item.get('library_enabled', True):
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
            elif rule_type in LOGIC_RULE_TYPES:
                # 逻辑规则的 no-resolve 必须写在子条件内部，追加为第四个字段
                # 会被 Mihomo 当作策略名，导致配置加载失败
                if item.get('no_resolve', False):
                    value = apply_no_resolve_to_logic_rule(value)
                rules.append(f"{rule_type},{value},{policy}")
            else:
                # 其他规则有三个字段：RULE_TYPE,VALUE,POLICY
                # 根据配置决定是否添加 no-resolve 参数
                no_resolve = item.get('no_resolve', False)
                if no_resolve:
                    rules.append(f"{rule_type},{value},{policy},no-resolve")
                else:
                    rules.append(f"{rule_type},{value},{policy}")

        elif item_type == 'ruleset':
            # 规则集引用
            policy = item.get('policy', 'PROXY')
            ruleset_name = item['name']
            # 根据配置决定是否添加 no-resolve 参数
            no_resolve = item.get('no_resolve', False)
            if no_resolve:
                rules.append(f"RULE-SET,{ruleset_name},{policy},no-resolve")
            else:
                rules.append(f"RULE-SET,{ruleset_name},{policy}")

    # 节点不支持 UDP 时 Mihomo 跳过该规则继续向下匹配，全部跳过后默认 DIRECT；
    # 选择拒绝时在最后接住这些连接。DIRECT 与内核默认相同，不额外写规则。
    if udp_fallback(config_data) == 'REJECT':
        rules.append('NETWORK,udp,REJECT')

    mihomo_config['rules'] = rules
    from backend.utils.group_candidates import lower_group_provider_filters
    lower_group_provider_filters(config_data, mihomo_config)
    from backend.utils.proxy_chains import lower_chain_providers
    lower_chain_providers(config_data, mihomo_config)

    # 订阅自带的 hosts：只合并本配置实际下发节点自己的 server 域名
    from backend.utils.subscription_hosts import log_report, merge_subscription_hosts
    merged_hosts = {}
    hosts_report = merge_subscription_hosts(mihomo_config, config_data, user_keys=user_host_keys,
                                            mosdns_keys=mosdns_host_keys, inline_proxies=inline_proxies,
                                            merged_out=merged_hosts)
    # 「域名 → 域名」映射只有节点域名经本机 DNS 监听解析时才生效：只把被映射的节点域名
    # 交给监听（proxy-server-nameserver-policy）；没有这类映射时不做任何改动
    from backend.utils.proxy_server_dns import apply_node_dns
    proxy_dns = apply_node_dns(mihomo_config, merged_hosts, core_version=mihomo_core_version)
    if proxy_dns is not None:
        hosts_report['dns'] = proxy_dns
    log_report(logical_config.get('profile_id') or '', hosts_report)
    if report is not None:
        report['subscription_hosts'] = hosts_report

    # 订阅自带的链式节点：补上它们依赖的机场中转分组，补不上的节点在交付时跳过
    from backend.utils.subscription_dialers import add_dialer_groups
    dialer_report = add_dialer_groups(mihomo_config, config_data)
    if report is not None and dialer_report:
        report['subscription_dialers'] = dialer_report

    from backend.utils.strategy_references import validate_rule_policies
    validate_rule_policies(
        config_data,
        [p['name'] for p in mihomo_config['proxies']] + [g['name'] for g in mihomo_config['proxy-groups']],
        'mihomo',
    )

    from backend.utils.dialer_references import validate_emitted
    validate_emitted(mihomo_config)
    if preflight_providers:
        from backend.utils.provider_delivery import prepare_provider_bundle
        prepare_provider_bundle(logical_config, mihomo_config)
    # 检测入口只注入推送给开启了域名发现的 Agent 的配置；区域检测和网站识别共用
    discovery = logical_config.get('domain_discovery') or {}
    if region_probe:
        inject_region_probe(mihomo_config, int(discovery.get('region_probe_port') or DEFAULT_REGION_PROBE_PORT))
    from backend.utils.mihomo_listeners import validate_listener_policies
    validate_listener_policies(mihomo_config)
    # 转换为 YAML
    return yaml.dump(
        mihomo_config,
        Dumper=IndentDumper,
        allow_unicode=True,
        default_flow_style=False,
        sort_keys=False,
        indent=2
    )


def get_mihomo_provider_downloads(config_data: Dict[str, Any], base_url: str = '', *, main_config=None) -> List[Dict[str, str]]:
    """Return every native provider artifact, including independent chain copies."""
    main = main_config if main_config is not None else yaml.safe_load(
        generate_mihomo_config(config_data, base_url, preflight_providers=False))
    return [{'name': name, 'url': provider['url'], 'local_path': provider['path']}
            for name, provider in main.get('proxy-providers', {}).items() if provider.get('url')]


def get_mihomo_ruleset_downloads(config_data: Dict[str, Any], base_url: str = '') -> List[Dict[str, str]]:
    """
    获取 Mihomo 需要下载的 ruleset 文件列表

    Args:
        config_data: 配置数据字典
        base_url: 基础 URL

    Returns:
        下载列表，格式：[{'name': 'ruleset名称', 'url': 'URL', 'local_path': './ruleset/xxx.list'}]
    """
    # 网关派生 provider 与普通选中 provider 必须使用生成结果中的稳定 ID URL。
    from backend.common.linux_gateway import gateway_settings
    if gateway_settings(config_data)['enabled']:
        main = yaml.safe_load(generate_mihomo_config(config_data, base_url, preflight_providers=False))
        return [{'name': name, 'url': provider['url'], 'local_path': provider['path']}
                for name, provider in main.get('rule-providers', {}).items()
                if provider.get('type') == 'http' and provider.get('url')]
    downloads = []

    # 获取系统配置
    server_domain = config_data.get('system_config', {}).get('server_domain', '').strip()
    effective_base_url = server_domain or base_url

    # 获取所有规则集
    _, rule_sets_list = split_rules_and_rulesets(config_data)
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
                        url = profile_api_path(config_data, f"/rules/local/{rule_name}")
                        if ruleset_format(rule_set, config_data, original_url) != 'mrs':
                            url = append_url_query(url, {'target': 'mihomo'})

            # 如果 URL 是相对路径，动态拼接 server_domain
            if url and url.startswith('/') and effective_base_url:
                url = f"{effective_base_url}{url}"
            # 如果 URL 是 GitHub 地址，应用代理配置
            elif url and url.startswith('http'):
                url = apply_github_proxy_domain(url, config_data)

            rule_format = ruleset_format(rule_set, config_data, original_url)
            path_extension = 'list' if rule_format == 'text' else rule_format

            downloads.append({
                'name': rule_set['name'],
                'url': url,
                'local_path': f"./ruleset/{rule_set['name']}.{path_extension}"
            })

    return downloads


# Only the YAML branch is cached: URI/JSON already have cheap fast paths.
_PROXY_YAML_CACHE_SIZE = 128
_PROXY_YAML_MAX_INPUT_BYTES = 64 * 1024
_PROXY_YAML_MAX_RESULT_BYTES = 64 * 1024
_PROXY_YAML_MAX_OBJECTS = 2048
_PROXY_YAML_MAX_DEPTH = 32
_PROXY_YAML_CACHE = OrderedDict()
_PROXY_YAML_CACHE_LOCK = RLock()


def _cacheable_proxy_yaml(data):
    """Bound ordinary result trees; aliases/cycles and special YAML bypass cache.

    Repeated containers are rejected rather than expanded. The original loader
    result then goes straight to the caller, preserving alias identity/types.
    """
    seen, pending, size = set(), [(data, 0)], 0
    while pending:
        value, depth = pending.pop()
        kind = type(value)
        if depth > _PROXY_YAML_MAX_DEPTH or kind not in (dict, list, str, bool, int, float, type(None)):
            return False
        identity = id(value)
        if identity in seen:
            if kind in (dict, list):
                return False
            continue
        seen.add(identity)
        size += getsizeof(value)
        if len(seen) > _PROXY_YAML_MAX_OBJECTS or size > _PROXY_YAML_MAX_RESULT_BYTES:
            return False
        if kind is dict:
            if any(type(key) is not str for key in value):
                return False
            pending.extend((child, depth + 1) for pair in value.items() for child in pair)
        elif kind is list:
            pending.extend((child, depth + 1) for child in value)
    return True


def _load_proxy_yaml(text):
    # Avoid encoding an arbitrarily large input just to decide not to cache it.
    if (len(text) > _PROXY_YAML_MAX_INPUT_BYTES
            or len(text.encode('utf-8')) > _PROXY_YAML_MAX_INPUT_BYTES):
        return yaml.safe_load(text)
    with _PROXY_YAML_CACHE_LOCK:
        data = _PROXY_YAML_CACHE.get(text)
        if data is None:
            data = yaml.safe_load(text)
            if not (isinstance(data, dict) and data.get('type') and _cacheable_proxy_yaml(data)):
                return data
            _PROXY_YAML_CACHE[text] = data
            while len(_PROXY_YAML_CACHE) > _PROXY_YAML_CACHE_SIZE:
                _PROXY_YAML_CACHE.popitem(last=False)
        else:
            _PROXY_YAML_CACHE.move_to_end(text)
    # Cached objects stay private. Conversion changes name/nested proxy fields;
    # copying outside the lock keeps independent hits from blocking each other.
    return deepcopy(data)


def _parse_structured_proxy_string(proxy_string: str) -> Optional[Dict[str, Any]]:
    """尝试将 proxy_string 解析为结构化的 mihomo proxy dict（JSON/YAML 格式）。

    如果是 JSON/YAML 格式且包含 type 字段，直接返回解析后的 dict；
    否则返回 None，表示需要通过 Sub-Store 转换。
    """
    stripped = proxy_string.strip()

    # URI 链接（ss://, vmess://, vless://, trojan://, hysteria2:// 等）需要 Sub-Store
    if '://' in stripped.split('\n')[0] and not ':' == stripped.split('://')[0][-1:]:
        # 检查是否是代理协议 URI（第一行包含 :// 且不是 YAML 的 key: value）
        first_line = stripped.split('\n')[0].strip()
        if not first_line.endswith(':') and '://' in first_line:
            # 进一步确认：如果 :// 前面没有空格，则是 URI
            scheme_part = first_line.split('://')[0]
            if ' ' not in scheme_part and ':' not in scheme_part:
                return None

    # 尝试 JSON 解析（以 { 开头）
    if stripped.startswith('{'):
        try:
            import json
            data = json.loads(stripped)
            if isinstance(data, dict) and data.get('type'):
                return data
        except Exception:
            pass

    # 尝试 YAML 解析
    try:
        data = _load_proxy_yaml(stripped)
        if isinstance(data, dict) and data.get('type'):
            return data
    except Exception:
        pass

    # 无法本地解析，需要 Sub-Store（base64 等）
    return None



_node_conversions = local()


@contextmanager
def node_conversions(results, *, reuse=False):
    """Record Sub-Store conversions of URI node strings in this thread into ``results``.

    With ``reuse`` a string already in ``results`` is answered from it instead
    of asking Sub-Store again. A publication that renders its files a second
    time (after saving newly downloaded subscriptions) reuses the first
    rendering's conversions; one string always converts to the same proxy.
    """
    previous = getattr(_node_conversions, 'scope', None)
    _node_conversions.scope = (results, reuse)
    try:
        yield results
    finally:
        _node_conversions.scope = previous


def convert_node_to_mihomo(node: Dict[str, Any]) -> Dict[str, Any]:
    """将通用节点格式转换为 Mihomo 格式。

    JSON/YAML 格式的节点字符串直接本地解析；
    URI 链接或 base64 格式通过 Sub-Store 转换。
    """
    from backend.utils.sub_store_client import convert_proxy_string

    outer_name = node.get('name', '')

    # 有 proxy_string 的节点
    if node.get('proxy_string'):
        proxy_string = node['proxy_string']

        # 先尝试本地解析（JSON/YAML 格式）
        parsed = _parse_structured_proxy_string(proxy_string)
        if parsed:
            logger.info(f"节点 '{outer_name}' 为 JSON/YAML 格式，本地解析")
            parsed['name'] = outer_name
            fix_proxy_fields(parsed)
            return parsed

        # URI 或 base64 格式，通过 Sub-Store 转换
        scope = getattr(_node_conversions, 'scope', None) if isinstance(proxy_string, str) else None
        if scope is not None and scope[1] and proxy_string in scope[0]:
            proxy = deepcopy(scope[0][proxy_string])
        else:
            logger.info(f"节点 '{outer_name}' 为 URI/base64 格式，调用 Sub-Store 转换")
            proxy = convert_proxy_string(proxy_string)
            if scope is not None:
                scope[0][proxy_string] = deepcopy(proxy)
        if proxy:
            proxy['name'] = outer_name
            fix_proxy_fields(proxy)
            return proxy
        return None

    # 已经是结构化的节点（从缓存加载的），将 params 展开为扁平 mihomo 格式
    node_type = node.get('type', '').lower()
    if not node_type:
        return None

    params = node.get('params', {})
    base = {
        'name': outer_name,
        'type': node_type,
        'server': node.get('server', ''),
        'port': node.get('port', 0)
    }
    # 将 params 中的所有字段展开到顶层
    base.update(params)
    fix_proxy_fields(base)
    return base
