"""规则匹配工具模块

提供域名和IP的规则匹配功能
"""
import re
import ipaddress
from typing import Any, Callable, Dict, List, Optional, Tuple

from backend.utils.logger import get_logger
from backend.utils.url_utils import safe_exception_details

logger = get_logger(__name__)


def is_valid_domain(query: str) -> bool:
    """验证输入是否为有效域名

    Args:
        query: 待验证的字符串

    Returns:
        是否为有效域名
    """
    if not query or len(query) > 253:
        return False

    # 域名正则表达式
    domain_pattern = r'^(?:[a-zA-Z0-9](?:[a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)*[a-zA-Z0-9](?:[a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?$'
    return bool(re.match(domain_pattern, query))


def is_valid_ip(query: str) -> bool:
    """验证输入是否为有效IP地址（IPv4或IPv6）

    Args:
        query: 待验证的字符串

    Returns:
        是否为有效IP地址
    """
    try:
        ipaddress.ip_address(query)
        return True
    except ValueError:
        return False


def parse_rule_line(line: str) -> Optional[Tuple[str, str]]:
    """解析规则行，提取规则类型和值（支持 Clash、MosDNS 及 List 格式）

    Args:
        line: 规则行，如 "DOMAIN-SUFFIX,google.com"、"domain:google.com" 或 "+.google.com"

    Returns:
        (规则类型, 规则值) 元组，如果解析失败返回 None
    """
    line = line.strip()

    # 跳过注释和空行
    if not line or line.startswith('#') or line.startswith('!') or line.startswith('//'):
        return None

    # 处理 YAML 列表前缀 (- value)
    if line.startswith('-'):
        line = line[1:].strip()
        if not line:
            return None

    # 去掉行内注释（# 之后内容）
    if '#' in line:
        line = line.split('#', 1)[0].strip()
        if not line:
            return None

    # 去掉包裹引号
    line = line.strip('\'"')

    # 1. Clash/YAML 逗号分隔格式
    if ',' in line:
        parts = line.split(',', 1)
        rule_type = parts[0].strip().upper()
        rule_value = parts[1].strip()
        # 剥离尾部的 no-resolve 参数，它不属于规则值（否则 IP 匹配和查重都会失效）
        if rule_value.lower().endswith(',no-resolve'):
            rule_value = rule_value[:-len(',no-resolve')].strip()
        return (rule_type, rule_value)

    # 2. MosDNS 格式（domain: / full: / keyword: / regexp: / ip:）
    if ':' in line:
        prefix, value = line.split(':', 1)
        prefix = prefix.strip().lower()
        value = value.strip()

        # YAML 规则集的 payload: 头行不是规则
        if prefix == 'payload' and not value:
            return None

        if prefix == 'domain':
            return ('DOMAIN-SUFFIX', value)
        if prefix == 'full':
            return ('DOMAIN', value)
        if prefix == 'keyword':
            return ('DOMAIN-KEYWORD', value)
        if prefix in ('regexp', 'regex'):
            return ('REGEX', value)
        if prefix == 'ip':
            return ('IP', value)

    # 3. List 格式（+.example.com / .example.com / *.example.com / example.com / IP）
    if line.startswith('+.'):
        return ('DOMAIN-SUFFIX', line[2:].strip())
    if line.startswith('*.'):
        return ('DIRECT-SUBDOMAIN', line[2:].strip())
    if line.startswith('.') and not line.startswith('..'):
        return ('SUBDOMAIN-ONLY', line[1:].strip())

    # 4. 纯 IP 或 CIDR
    try:
        ipaddress.ip_address(line)
        return ('IP', line)
    except ValueError:
        try:
            ipaddress.ip_network(line, strict=False)
            return ('IP-CIDR', line)
        except ValueError:
            pass

    # 5. 默认当作精确域名
    return ('DOMAIN', line)


def match_domain(domain: str, rule_type: str, rule_value: str) -> bool:
    """匹配域名规则

    Args:
        domain: 待匹配的域名
        rule_type: 规则类型（DOMAIN, DOMAIN-SUFFIX, DOMAIN-KEYWORD等）
        rule_value: 规则值

    Returns:
        是否匹配
    """
    domain = domain.lower()
    rule_value = rule_value.lower()

    if rule_type == 'DOMAIN':
        # 完全匹配
        return domain == rule_value

    elif rule_type == 'DOMAIN-SUFFIX':
        # 后缀匹配
        if domain == rule_value:
            return True
        return domain.endswith('.' + rule_value)

    elif rule_type == 'DOMAIN-KEYWORD':
        # 关键字匹配
        return rule_value in domain
    elif rule_type == 'SUBDOMAIN-ONLY':
        suffix = rule_value
        if not suffix or not domain.endswith('.' + suffix):
            return False
        return domain != suffix
    elif rule_type == 'DIRECT-SUBDOMAIN':
        suffix = rule_value
        if not suffix or not domain.endswith('.' + suffix):
            return False
        prefix = domain[:-(len(suffix) + 1)]
        return prefix != '' and '.' not in prefix
    elif rule_type in ('REGEX', 'REGEXP'):
        try:
            return bool(re.search(rule_value, domain))
        except re.error:
            return False

    return False


def match_ip(ip_str: str, rule_type: str, rule_value: str) -> bool:
    """匹配IP规则

    Args:
        ip_str: 待匹配的IP地址字符串
        rule_type: 规则类型（IP-CIDR, IP-CIDR6等）
        rule_value: 规则值（CIDR格式）

    Returns:
        是否匹配
    """
    try:
        ip = ipaddress.ip_address(ip_str)

        if rule_type in ('IP-CIDR', 'IP-CIDR6'):
            # CIDR匹配
            network = ipaddress.ip_network(rule_value, strict=False)
            return ip in network
        if rule_type == 'IP':
            return ip == ipaddress.ip_address(rule_value)

        return False
    except (ValueError, TypeError):
        return False


def match_query(query: str, rule_type: str, rule_value: str) -> bool:
    """根据查询内容自动判断并匹配规则

    Args:
        query: 待匹配的查询（域名或IP）
        rule_type: 规则类型
        rule_value: 规则值

    Returns:
        是否匹配
    """
    # 判断查询类型
    if is_valid_ip(query):
        # IP匹配
        return match_ip(query, rule_type, rule_value)
    elif is_valid_domain(query):
        # 域名匹配
        return match_domain(query, rule_type, rule_value)

    return False


class RuleConfigMatcher:
    """按 rule_configs 顺序匹配域名/IP，返回第一条命中的规则。

    规则集内容只在首次需要时通过 load_content 加载并解析，之后复用，
    因此同一个实例可以低成本地批量查询。
    """

    def __init__(self, rule_configs, rule_library, load_content: Callable[[dict, Optional[dict]], str]):
        self._items = [item for item in rule_configs
                       if item.get('enabled', True) and item.get('library_enabled', True)]
        self._library = {item.get('id'): item for item in rule_library if item.get('id')}
        self._load_content = load_content
        self._parsed = {}

    def _ruleset_entries(self, index: int, rule_item: dict, library_rule: Optional[dict]) -> List[Tuple[str, str, str]]:
        if index not in self._parsed:
            entries = []
            try:
                content = self._load_content(rule_item, library_rule) or ''
            except Exception as error:
                logger.error('Error processing ruleset "%s": %s', rule_item.get('name', '规则集'), safe_exception_details(error))
                content = ''
            if not content:
                logger.warning('Skipping ruleset "%s": unable to fetch content', rule_item.get('name', '规则集'))
            for line in content.splitlines():
                try:
                    parsed = parse_rule_line(line)
                except Exception:
                    continue
                if parsed:
                    entries.append((parsed[0], parsed[1], line.strip()))
            self._parsed[index] = entries
        return self._parsed[index]

    def match(self, query: str) -> Optional[Dict[str, Any]]:
        for index, rule_item in enumerate(self._items, start=1):
            item_type = rule_item.get('itemType', 'rule')
            policy = rule_item.get('policy', 'DIRECT')
            if item_type == 'rule':
                rule_type = rule_item.get('rule_type', '')
                rule_value = rule_item.get('value', '')
                if match_query(query, rule_type, rule_value):
                    return {
                        'rule_name': f'{rule_type} 规则',
                        'rule_type': 'rule',
                        'matched_line': f'{rule_type},{rule_value}',
                        'policy': policy,
                        'source': '直接配置的规则',
                        'priority': index,
                        'rule_id': rule_item.get('id'),
                        'item_type': 'rule',
                        'behavior': 'classical',
                    }
            elif item_type == 'ruleset':
                rule_set_name = rule_item.get('name', '规则集')
                library_rule = self._library.get(rule_item.get('library_rule_id', ''))
                for rule_type, rule_value, line in self._ruleset_entries(index, rule_item, library_rule):
                    if match_query(query, rule_type, rule_value):
                        return {
                            'rule_name': rule_set_name,
                            'rule_type': 'ruleset',
                            'matched_line': line,
                            'policy': policy,
                            'source': f'规则集: {rule_set_name}',
                            'priority': index,
                            'rule_id': rule_item.get('id'),
                            'item_type': 'ruleset',
                            'behavior': rule_item.get('behavior', 'classical'),
                        }
        return None
