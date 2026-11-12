"""Profile-scoped EasyTier networking for generated Mihomo configs.

Mihomo v1.19.31 added a ``type: easytier`` outbound that embeds the EasyTier
core. The setting is stored at ``config['mihomo']['easytier']`` and only
changes the generated output: one EasyTier proxy, optionally a fallback group
whose health check starts the (otherwise lazy) instance, rules that send the
chosen overlay networks to it, and MagicDNS routing.

Every option of the outbound is kept under ``options`` with Mihomo's own
kebab-case keys, so the YAML written is exactly what the user configured.
Reference: https://wiki.metacubex.one/config/proxies/easytier/
"""
from copy import deepcopy
import ipaddress
import re

MIN_CORE_VERSION = (1, 19, 31)
APPLY_TO = ('subscription', 'agent', 'all')
IP_VERSIONS = ('dual', 'ipv4', 'ipv6', 'ipv4-prefer', 'ipv6-prefer')
DEFAULT_NAME = 'EasyTier'
DEFAULT_TLD = 'et.net'
DEFAULT_KEEPALIVE_INTERVAL = 300
MAX_LIST = 64
MAX_TEXT = 512
_PEER_SCHEMES = ('tcp', 'udp', 'ws', 'wss', 'wg', 'quic', 'faketcp', 'txt', 'srv', 'http', 'https')
_HOSTNAME = re.compile(r'^[A-Za-z0-9](?:[A-Za-z0-9-]{0,62})$')

# (key, kind). Kinds: text, secret, bool, int, list, choice.
FIELDS = (
    ('network-name', 'text'),
    ('network-secret', 'secret'),
    ('hostname', 'text'),
    ('ipv4', 'text'),
    ('dhcp', 'bool'),
    ('peers', 'list'),
    ('secure-mode', 'bool'),
    ('local-private-key', 'secret'),
    ('local-public-key', 'text'),
    ('listeners', 'list'),
    ('no-listener', 'bool'),
    ('mapped-listeners', 'list'),
    ('exit-nodes', 'list'),
    ('proxy-networks', 'list'),
    ('instance-name', 'text'),
    ('state-dir', 'text'),
    ('udp', 'bool'),
    ('accept-dns', 'bool'),
    ('enable-exit-node', 'bool'),
    ('enable-encryption', 'bool'),
    ('encryption-algorithm', 'text'),
    ('private-mode', 'bool'),
    ('latency-first', 'bool'),
    ('disable-p2p', 'bool'),
    ('enable-kcp-proxy', 'bool'),
    ('disable-kcp-input', 'bool'),
    ('enable-quic-proxy', 'bool'),
    ('disable-quic-input', 'bool'),
    ('mtu', 'int'),
    ('tld-dns-zone', 'text'),
    ('dialer-proxy', 'text'),
    ('interface-name', 'text'),
    ('routing-mark', 'int'),
    ('ip-version', 'choice'),
)
FIELD_KINDS = dict(FIELDS)
SECRET_FIELDS = tuple(key for key, kind in FIELDS if kind == 'secret')
_INT_RANGES = {'mtu': (576, 9000), 'routing-mark': (0, 2 ** 32 - 1)}


class EasyTierSettingsError(ValueError):
    """The EasyTier setting cannot be saved or applied."""


def default_settings():
    return {
        'enabled': False,
        'name': DEFAULT_NAME,
        'apply_to': 'subscription',
        'routes': [],
        'magic_dns': False,
        'keepalive': {'enabled': False, 'url': '', 'interval': DEFAULT_KEEPALIVE_INTERVAL},
        'options': {'udp': True},
    }


def _clean_text(value, label, *, required=False, limit=MAX_TEXT):
    if value is None:
        value = ''
    if not isinstance(value, str):
        raise EasyTierSettingsError(f'{label} 必须是文本')
    value = value.strip()
    if required and not value:
        raise EasyTierSettingsError(f'请填写{label}')
    if len(value) > limit or any(ord(c) < 32 or ord(c) == 127 for c in value):
        raise EasyTierSettingsError(f'{label} 过长或包含控制字符')
    return value


def _clean_list(value, label):
    if value is None:
        return []
    if not isinstance(value, list) or len(value) > MAX_LIST:
        raise EasyTierSettingsError(f'{label} 必须是列表，最多 {MAX_LIST} 项')
    items = [_clean_text(item, label) for item in value]
    return list(dict.fromkeys(item for item in items if item))


def _ipv4_network(value, label):
    if '->' in value:
        raise EasyTierSettingsError(
            f'{label}「{value}」：Mihomo 的 EasyTier 不支持「->」网段映射，只能原样填写网段')
    try:
        network = ipaddress.ip_network(value, strict=False)
    except ValueError as exc:
        raise EasyTierSettingsError(f'{label}「{value}」不是有效的网段') from exc
    if network.version != 4:
        raise EasyTierSettingsError(f'{label}「{value}」：EasyTier 虚拟网只支持 IPv4')
    return str(network)


def _uri(value, label):
    scheme, sep, rest = value.partition('://')
    if not sep or scheme.lower() not in _PEER_SCHEMES or not rest:
        raise EasyTierSettingsError(
            f'{label}「{value}」格式不对，应类似 tcp://203.0.113.10:11010（支持 {", ".join(_PEER_SCHEMES[:7])} 等）')
    return value


def _option(key, kind, value):
    label = key
    if kind == 'bool':
        if type(value) is not bool:
            raise EasyTierSettingsError(f'{label} 必须是开或关')
        return value
    if kind == 'int':
        if type(value) is not int:
            raise EasyTierSettingsError(f'{label} 必须是整数')
        low, high = _INT_RANGES[key]
        if not low <= value <= high:
            raise EasyTierSettingsError(f'{label} 需在 {low} ~ {high} 之间')
        return value
    if kind == 'list':
        items = _clean_list(value, label)
        if key in ('peers', 'listeners', 'mapped-listeners'):
            items = [_uri(item, label) for item in items]
        elif key == 'proxy-networks':
            items = [_ipv4_network(item, label) for item in items]
        elif key == 'exit-nodes':
            for item in items:
                try:
                    if ipaddress.ip_address(item).version != 4:
                        raise ValueError
                except ValueError as exc:
                    raise EasyTierSettingsError(f'exit-nodes「{item}」应填写出口节点的虚拟 IPv4 地址') from exc
        return items
    if kind == 'choice':
        if value not in IP_VERSIONS:
            raise EasyTierSettingsError(f'ip-version 只能是 {" / ".join(IP_VERSIONS)}')
        return value
    text = _clean_text(value, label, limit=4096 if kind == 'secret' else MAX_TEXT)
    if key == 'ipv4' and text:
        try:
            interface = ipaddress.ip_interface(text)
        except ValueError as exc:
            raise EasyTierSettingsError(f'ipv4「{text}」应类似 10.144.144.10 或 10.144.144.10/24') from exc
        if interface.version != 4:
            raise EasyTierSettingsError('ipv4 只能填写 IPv4 地址')
    if key == 'hostname' and text and not _HOSTNAME.match(text):
        raise EasyTierSettingsError('hostname 只能包含字母、数字和连字符，不超过 63 个字符')
    return text


def normalize_settings(value):
    """Validate a stored or submitted setting and return its canonical form."""
    if value is None:
        return default_settings()
    if not isinstance(value, dict):
        raise EasyTierSettingsError('EasyTier 设置必须是对象')
    result = default_settings()
    enabled = value.get('enabled', False)
    if type(enabled) is not bool:
        raise EasyTierSettingsError('enabled 必须是布尔值')
    result['enabled'] = enabled
    name = _clean_text(value.get('name', DEFAULT_NAME), '节点名称', required=True, limit=64)
    if ',' in name:
        raise EasyTierSettingsError('节点名称不能包含英文逗号，否则规则无法引用')
    result['name'] = name
    apply_to = value.get('apply_to', 'subscription')
    if apply_to not in APPLY_TO:
        raise EasyTierSettingsError('下发范围只能是 subscription、agent 或 all')
    result['apply_to'] = apply_to
    result['routes'] = [_ipv4_network(item, '走 EasyTier 的网段') for item in _clean_list(value.get('routes'), '走 EasyTier 的网段')]
    magic_dns = value.get('magic_dns', False)
    if type(magic_dns) is not bool:
        raise EasyTierSettingsError('magic_dns 必须是布尔值')
    result['magic_dns'] = magic_dns

    keepalive = value.get('keepalive') or {}
    if not isinstance(keepalive, dict):
        raise EasyTierSettingsError('keepalive 必须是对象')
    keep_enabled = keepalive.get('enabled', False)
    if type(keep_enabled) is not bool:
        raise EasyTierSettingsError('keepalive.enabled 必须是布尔值')
    url = _clean_text(keepalive.get('url', ''), '保活检测地址')
    interval = keepalive.get('interval', DEFAULT_KEEPALIVE_INTERVAL)
    if type(interval) is not int or not 10 <= interval <= 86400:
        raise EasyTierSettingsError('保活检测间隔需为 10 ~ 86400 秒的整数')
    if url and not re.match(r'^https?://\S+$', url):
        raise EasyTierSettingsError('保活检测地址应为 http:// 或 https:// 开头的网址')
    if keep_enabled and not url:
        raise EasyTierSettingsError('开启保活时请填写检测地址，例如虚拟网内某台设备上的网页')
    result['keepalive'] = {'enabled': keep_enabled, 'url': url, 'interval': interval}

    options = value.get('options', {})
    if not isinstance(options, dict):
        raise EasyTierSettingsError('options 必须是对象')
    unknown = sorted(set(options) - set(FIELD_KINDS))
    if unknown:
        raise EasyTierSettingsError(f'不认识的 EasyTier 参数：{", ".join(map(str, unknown))}')
    cleaned = {}
    for key, kind in FIELDS:
        if key not in options or options[key] is None:
            continue
        item = _option(key, kind, options[key])
        if item == '' or item == []:
            continue
        cleaned[key] = item
    result['options'] = cleaned

    if enabled:
        if not cleaned.get('network-name'):
            raise EasyTierSettingsError('请填写 network-name（EasyTier 网络名）')
        if cleaned.get('listeners') and cleaned.get('no-listener') is True:
            raise EasyTierSettingsError('填写了 listeners 时不能同时开启 no-listener')
        listening = bool(cleaned.get('listeners')) or cleaned.get('no-listener') is False
        if not cleaned.get('peers') and not listening:
            raise EasyTierSettingsError('不监听端口时至少要填写一个 peers（入网节点）')
        if bool(cleaned.get('local-public-key')) and not cleaned.get('local-private-key'):
            raise EasyTierSettingsError('local-public-key 需要和 local-private-key 一起填写')
    return result


def easytier_settings(config):
    mihomo = config.get('mihomo') if isinstance(config, dict) else None
    value = mihomo.get('easytier') if isinstance(mihomo, dict) else None
    return normalize_settings(value)


def keepalive_group_name(settings):
    return f"{settings['name']} 保活"


def _tld(settings):
    zone = settings['options'].get('tld-dns-zone') or DEFAULT_TLD
    return zone.strip('.') or DEFAULT_TLD


def _version(text):
    from backend.utils.proxy_server_dns import parse_core_version
    return parse_core_version(text)


def applies(settings, *, for_agent):
    if not settings['enabled']:
        return False
    return settings['apply_to'] == 'all' or settings['apply_to'] == ('agent' if for_agent else 'subscription')


def apply_easytier(mihomo_config, config_data, *, for_agent=False, core_version=None):
    """Add the EasyTier proxy, keep-alive group, rules and MagicDNS to ``mihomo_config``.

    Leaves the document untouched when the setting is off or not meant for
    this target. Rules go before every other rule so overlay traffic is never
    caught by a broader user rule first.
    """
    settings = easytier_settings(config_data)
    if not applies(settings, for_agent=for_agent):
        return None
    parsed = _version(core_version)
    if parsed is not None and parsed < MIN_CORE_VERSION:
        raise EasyTierSettingsError(
            f'设备上的 Mihomo {core_version} 不支持 EasyTier 节点（需要 v1.19.31 或更新），'
            '请先更新内核，或在 EasyTier 设置里不再下发到 Agent')
    name = settings['name']
    proxies = mihomo_config.setdefault('proxies', [])
    groups = mihomo_config.setdefault('proxy-groups', [])
    taken = {p.get('name') for p in proxies if isinstance(p, dict)} | {g.get('name') for g in groups if isinstance(g, dict)}
    target = name
    names = [name]
    if settings['keepalive']['enabled']:
        target = keepalive_group_name(settings)
        names.append(target)
    clash = [item for item in names if item in taken]
    if clash:
        raise EasyTierSettingsError(f'EasyTier 节点名「{clash[0]}」与已有节点或策略组重名，请在 EasyTier 设置里改名')
    dialer = settings['options'].get('dialer-proxy')
    if dialer and dialer not in taken and dialer not in ('DIRECT',):
        raise EasyTierSettingsError(f'EasyTier 的 dialer-proxy「{dialer}」不在生成的节点或策略组里')

    proxy = {'name': name, 'type': 'easytier'}
    proxy.update(deepcopy(settings['options']))
    proxies.append(proxy)
    if settings['keepalive']['enabled']:
        groups.append({'name': target, 'type': 'fallback', 'proxies': [name],
                       'url': settings['keepalive']['url'], 'interval': settings['keepalive']['interval']})

    added = [f'IP-CIDR,{network},{target},no-resolve' for network in settings['routes']]
    if settings['magic_dns']:
        added.append(f'DOMAIN-SUFFIX,{_tld(settings)},{target}')
        dns = mihomo_config.get('dns')
        if isinstance(dns, dict):
            policy = dns.get('nameserver-policy')
            if policy is None:
                policy = dns['nameserver-policy'] = {}
            if isinstance(policy, dict):
                key = f'+.{_tld(settings)}'
                policy.setdefault(key, f'et://{name}')
    mihomo_config['rules'] = added + list(mihomo_config.get('rules') or [])
    return {'proxy': name, 'target': target, 'rules': len(added)}


def redact_settings(value):
    """Copy for human-shareable exports: secrets replaced, shape kept."""
    if not isinstance(value, dict):
        return value
    result = deepcopy(value)
    options = result.get('options')
    if isinstance(options, dict):
        for key in SECRET_FIELDS:
            if options.get(key):
                options[key] = '[REDACTED]'
    return result
