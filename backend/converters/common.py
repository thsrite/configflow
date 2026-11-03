"""Small input conversions shared by client configuration generators."""
import re
from ipaddress import ip_address
from urllib.parse import urlsplit


UDP_FALLBACK_POLICIES = ('DIRECT', 'REJECT')
LOGIC_TYPES = ('AND', 'OR', 'NOT')
PORT_RULE_TYPES = ('DST-PORT', 'SRC-PORT', 'IN-PORT')
_NETWORK_CONDITION = re.compile(r'\(\s*NETWORK\s*,\s*(tcp|udp)\s*\)', re.IGNORECASE)
_PORT_CONDITION = re.compile(r'\(\s*(?:DST|DEST|SRC|IN)-PORT\s*,\s*([^()]*)\)', re.IGNORECASE)
AFTER_MATCH_NOTE = '仅 Mihomo 生效（位于 MATCH 之后，只匹配节点不支持 UDP 而跳过的连接），已忽略'


# NTP 只需对时服务器的真实地址，走代理只会增加延迟；放在最前面，不受代理规则和节点 UDP 能力影响
NTP_DIRECT_RULE = {'id': 'builtin-ntp-direct', 'itemType': 'rule', 'rule_type': 'AND',
                   'value': '((NETWORK,udp),(DST-PORT,123))', 'policy': 'DIRECT', 'enabled': True}


def _effective_match(config_data):
    """The first enabled MATCH rule, which carries the profile's UDP choices."""
    return next((item for item in config_data.get('rule_configs', [])
                 if item.get('itemType') == 'rule' and item.get('rule_type') == 'MATCH'
                 and item.get('enabled', True)), {})


def udp_fallback(config_data):
    """DIRECT or REJECT chosen on the effective MATCH rule; None follows the client default."""
    choice = _effective_match(config_data).get('udp_fallback')
    return choice if choice in UDP_FALLBACK_POLICIES else None


def ntp_direct(config_data):
    return _effective_match(config_data).get('ntp_direct') is True


def emitted_rules(config_data):
    """Rule items in output order, with the built-in NTP rule first when enabled."""
    return ([dict(NTP_DIRECT_RULE)] if ntp_direct(config_data) else []) + list(config_data.get('rule_configs', []))


def after_match_comment(item):
    """Surge and Loon have no rules past FINAL; keep the Mihomo-only line visible."""
    if item.get('itemType') == 'ruleset':
        line = f"RULE-SET,{item.get('name', '')},{item.get('policy', '')}"
    else:
        line = f"{item.get('rule_type', '')},{item.get('value', '')},{item.get('policy', '')}"
    return f'# {AFTER_MATCH_NOTE}: {line}'


def network_as_protocol(rule_type, value):
    """Surge and Loon match the transport with PROTOCOL,TCP/UDP instead of NETWORK."""
    if rule_type == 'NETWORK':
        return 'PROTOCOL', value.strip().upper()
    if rule_type in LOGIC_TYPES:
        return rule_type, _NETWORK_CONDITION.sub(lambda match: f'(PROTOCOL,{match.group(1).upper()})', value)
    return rule_type, value


def has_port_list(rule_type, value):
    """Surge and Loon take one port, range or comparison per rule, not a / or , list."""
    if rule_type in PORT_RULE_TYPES:
        values = [value]
    elif rule_type in LOGIC_TYPES:
        values = _PORT_CONDITION.findall(value)
    else:
        return False
    return any('/' in item or ',' in item for item in values)


def set_general_option(section, key, value):
    """Add ``key = value`` to a [General] section unless it already sets the key."""
    if re.search(rf'^\s*{re.escape(key)}\s*=', section, re.MULTILINE):
        return section
    return f'{section.rstrip()}\n{key} = {value}'


def ruleset_format(rule, config_data, source_url=None):
    """Prefer the declared format; infer handwritten payloads or legacy URL paths."""
    library = next((item for item in config_data.get('rule_library', [])
                    if (item.get('id') == rule.get('library_rule_id')
                        if rule.get('library_rule_id') else item.get('name') == rule.get('name'))), {})
    for source in (library, rule):
        declared = source.get('format')
        if declared in ('yaml', 'text', 'mrs'):
            return declared
    source = library or rule
    if source.get('source_type') == 'content':
        from backend.utils.ruleset_content import _has_yaml_payload
        content = source.get('content', '')
        return 'yaml' if isinstance(content, str) and _has_yaml_payload(content) else 'text'
    url = library.get('url') or source_url or rule.get('url') or ''
    path = urlsplit(url).path.lower()
    if path.endswith(('.yaml', '.yml')):
        return 'yaml'
    return 'mrs' if path.endswith('.mrs') else 'text'


def parse_hosts_pairs(text):
    """Read domain/address and legacy address/domain rows, for IPv4 and IPv6."""
    def is_ip(value):
        try:
            ip_address(value)
            return True
        except ValueError:
            return False

    pairs = []
    for line in (text or '').splitlines():
        fields = line.partition('#')[0].split()
        if len(fields) < 2:
            continue
        first, second = fields[:2]
        if is_ip(second):
            pairs.append((first, second))
        elif is_ip(first):
            pairs.append((second, first))
    return pairs
