"""Server-side checks for the Mihomo base settings form.

The base YAML stays the only source of truth; these checks mirror the form
(frontend/src/lib/mihomoBaseSettings.ts) so that a hand-edited or scripted save
is held to the same rules. Only values the save changes are checked: an older
base config (for example the initial template's controller password) can still
be saved with unrelated edits, and the page points out what needs fixing.
"""
import ipaddress
from urllib.parse import urlsplit

INBOUND_PORT_KEYS = ('mixed-port', 'port', 'socks-port', 'redir-port', 'tproxy-port')
LOG_LEVELS = ('silent', 'error', 'warning', 'info', 'debug')
FIND_PROCESS_MODES = ('always', 'strict', 'off')
FAKE_IP_FILTER_MODES = ('blacklist', 'whitelist', 'rule')
TUN_STACKS = ('system', 'gvisor', 'mixed')
CLIENT_FINGERPRINTS = ('chrome', 'firefox', 'safari', 'ios', 'android', 'edge', '360', 'qq', 'random')
DNS_LIST_KEYS = ('default-nameserver', 'nameserver', 'proxy-server-nameserver', 'direct-nameserver', 'fallback')
DNS_UPSTREAM_PATHS = tuple(('dns', key) for key in DNS_LIST_KEYS + ('nameserver-policy', 'proxy-server-nameserver-policy'))
REGION_PROBE_LISTENER = 'configflow-region-probe'

BOOLEANS = {
    ('allow-lan',): 'allow-lan',
    ('unified-delay',): 'unified-delay',
    ('tcp-concurrent',): 'tcp-concurrent',
    ('disable-keep-alive',): 'disable-keep-alive',
    ('profile', 'store-selected'): 'profile.store-selected',
    ('profile', 'store-fake-ip'): 'profile.store-fake-ip',
    ('sniffer', 'enable'): 'sniffer.enable',
    ('dns', 'respect-rules'): 'dns.respect-rules',
    ('dns', 'direct-nameserver-follow-policy'): 'dns.direct-nameserver-follow-policy',
}
INTEGERS = {
    **{(key,): (key, 1, 65535) for key in INBOUND_PORT_KEYS},
    ('tun', 'mtu'): ('tun.mtu', 576, 65535),
    ('keep-alive-idle',): ('keep-alive-idle', 1, 86400),
    ('keep-alive-interval',): ('keep-alive-interval', 1, 86400),
}
ENUMS = {
    ('log-level',): ('log-level', LOG_LEVELS),
    ('find-process-mode',): ('find-process-mode', FIND_PROCESS_MODES),
    ('dns', 'fake-ip-filter-mode'): ('dns.fake-ip-filter-mode', FAKE_IP_FILTER_MODES),
    ('tun', 'stack'): ('tun.stack', TUN_STACKS),
    ('global-client-fingerprint',): ('global-client-fingerprint', CLIENT_FINGERPRINTS),
}
STRING_LISTS = {
    **{('dns', key): f'dns.{key}' for key in DNS_LIST_KEYS},
    ('dns', 'fake-ip-filter'): 'dns.fake-ip-filter',
    ('sniffer', 'skip-domain'): 'sniffer.skip-domain',
    ('sniffer', 'force-domain'): 'sniffer.force-domain',
}
TEXTS = {('external-ui',): 'external-ui', ('global-ua',): 'global-ua'}
_MISSING = object()


class BaseSettingsError(ValueError):
    """A base settings value the form would not accept."""


def _get(document, path):
    value = document
    for key in path:
        if not isinstance(value, dict) or key not in value:
            return _MISSING
        value = value[key]
    return value


def _same(left, right):
    # True == 1 in Python; a quoted "7890" is not the port 7890.
    if type(left) is not type(right):
        return False
    if isinstance(left, list):
        return len(left) == len(right) and all(_same(a, b) for a, b in zip(left, right))
    if isinstance(left, dict):
        return left.keys() == right.keys() and all(_same(left[key], right[key]) for key in left)
    return left == right


def split_host_port(value):
    """``(host, port)`` of a ``host:port`` listener, brackets removed; None when malformed."""
    if not isinstance(value, str):
        return None
    host, separator, port = value.strip().rpartition(':')
    if not separator or not port.isdigit() or not 1 <= int(port) <= 65535:
        return None
    if host.startswith('['):
        if not host.endswith(']'):
            return None
        host = host[1:-1]
    elif ':' in host:
        return None
    return host, int(port)


def controller_scope(host):
    """'lan' for a wildcard listener, 'local' for loopback, otherwise 'other'."""
    if host in ('', '0.0.0.0', '::'):
        return 'lan'
    if host.lower() == 'localhost':
        return 'local'
    try:
        return 'local' if ipaddress.ip_address(host).is_loopback else 'other'
    except ValueError:
        return 'other'


def _listen_port(value):
    parsed = split_host_port(value)
    return parsed[1] if parsed else None


def _port_owners(document):
    owners = [(document[key], key) for key in INBOUND_PORT_KEYS
              if type(document.get(key)) is int and 1 <= document[key] <= 65535]
    controller = _listen_port(document.get('external-controller'))
    if controller:
        owners.append((controller, 'external-controller'))
    dns = document.get('dns')
    if isinstance(dns, dict) and dns.get('enable', True) is not False:
        port = _listen_port(dns.get('listen'))
        if port:
            owners.append((port, 'dns.listen'))
    listeners = document.get('listeners')
    if isinstance(listeners, list):
        for item in listeners:
            if (isinstance(item, dict) and item.get('name') != REGION_PROBE_LISTENER
                    and type(item.get('port')) is int):
                owners.append((item['port'], f"listeners（{item.get('name') or '未命名'}）"))
    return owners


def _check_types(document, changed):
    for path, label in BOOLEANS.items():
        value = _get(document, path)
        if changed(path) and value is not _MISSING and type(value) is not bool:
            raise BaseSettingsError(f'{label} 只能填写 true 或 false，不要加引号')
    for path, (label, low, high) in INTEGERS.items():
        value = _get(document, path)
        if changed(path) and value is not _MISSING and (type(value) is not int or not low <= value <= high):
            raise BaseSettingsError(f'{label} 需为 {low}–{high} 的整数')
    for path, (label, options) in ENUMS.items():
        value = _get(document, path)
        if not changed(path) or value is _MISSING:
            continue
        if path == ('find-process-mode',) and type(value) is bool:
            continue  # Unquoted off / on; generation maps them back to the mode names.
        if type(value) is int:
            value = str(value)  # global-client-fingerprint: 360 without quotes.
        if not isinstance(value, str) or value.strip().lower() not in options:
            raise BaseSettingsError(f'{label} 只能是 {" / ".join(options)} 之一')
    for path, label in STRING_LISTS.items():
        value = _get(document, path)
        if changed(path) and value is not _MISSING and value is not None and (
                not isinstance(value, list) or not all(isinstance(item, str) and item.strip() for item in value)):
            raise BaseSettingsError(f'{label} 需为文本列表，每行一项，不能有空项')
    for path, label in TEXTS.items():
        value = _get(document, path)
        if changed(path) and value is not _MISSING and (not isinstance(value, str) or not value.strip()):
            raise BaseSettingsError(f'{label} 需为非空文本；不需要时删除该项')


def _check_controller(document, changed):
    from backend.common.setup_flow import controller_secret_error
    controller = _get(document, ('external-controller',))
    exposed = False
    if controller is not _MISSING and controller is not None:
        parsed = split_host_port(controller)
        if changed(('external-controller',)) and parsed is None:
            raise BaseSettingsError('external-controller 需写成「地址:端口」，例如 0.0.0.0:9090 或 127.0.0.1:9090，端口 1–65535')
        scope = controller_scope(parsed[0]) if parsed else 'other'
        if changed(('external-controller',)) and scope == 'other':
            raise BaseSettingsError('external-controller 只能监听 0.0.0.0 / ::（局域网可访问）或 127.0.0.1 / ::1（仅本机）：'
                                    'Agent 经本机回环地址读取连接、流量并热重载配置，监听其他地址时这些功能不可用')
        exposed = scope != 'local'
    secret = _get(document, ('secret',))
    if secret is not _MISSING and secret is not None and not isinstance(secret, str):
        if changed(('secret',)):
            raise BaseSettingsError('控制面板密码（secret）必须是文本，纯数字等请加引号')
        secret = _MISSING
    if not (changed(('secret',)) or changed(('external-controller',))):
        return
    if exposed:
        if secret is _MISSING or secret in (None, ''):
            raise BaseSettingsError('控制面板对局域网开放（external-controller 监听所有地址）时必须设置控制面板密码（secret）')
        error = controller_secret_error(secret)
        if error:
            raise BaseSettingsError(error)
    elif changed(('secret',)) and secret not in (_MISSING, None, ''):
        error = controller_secret_error(secret)
        if error:
            raise BaseSettingsError(error)


def _check_access(document, changed):
    url = _get(document, ('external-ui-url',))
    if changed(('external-ui-url',)) and url is not _MISSING:
        try:
            parts = urlsplit(url) if isinstance(url, str) else None
            valid = bool(parts and parts.scheme in ('http', 'https') and parts.hostname)
        except ValueError:
            valid = False
        if not valid:
            raise BaseSettingsError('external-ui-url 需为 http(s) 下载地址，例如 https://github.com/…/gh-pages.zip')
    bind = _get(document, ('bind-address',))
    if changed(('bind-address',)) and bind is not _MISSING:
        text = bind.strip().strip('[]') if isinstance(bind, str) else ''
        try:
            valid = text == '*' or bool(ipaddress.ip_address(text))
        except ValueError:
            valid = False
        if not valid:
            raise BaseSettingsError('bind-address 只能填写 *（所有地址）或本机的一个 IP 地址')


def _check_ports(document, changed, reserved):
    paths = [(key,) for key in INBOUND_PORT_KEYS] + [('external-controller',), ('dns', 'listen'), ('dns', 'enable'), ('listeners',)]
    if not any(changed(path) for path in paths):
        return
    seen = {}
    for port, owner in _port_owners(document):
        if port in seen:
            raise BaseSettingsError(f'端口 {port} 同时被 {seen[port]} 和 {owner} 使用，请改用不同端口')
        seen[port] = owner
        if port in reserved:
            raise BaseSettingsError(f'{owner} 使用端口 {port}，与{reserved[port]}冲突，请改用其他端口')


def _check_dns(document, changed, host_addresses):
    from backend.common.gateway_onboarding import mihomo_dns_loop
    dns = document.get('dns')
    if not isinstance(dns, dict):
        return
    if any(changed(path) for path in DNS_UPSTREAM_PATHS):
        loop = mihomo_dns_loop(dns, host_addresses)
        if loop:
            raise BaseSettingsError(f'dns.{loop[0]} 中的 {loop[1]} 指向本机或系统 DNS，会形成解析循环；'
                                    '请改用外部 DNS，例如 https://223.5.5.5/dns-query')
    if changed(('dns', 'nameserver')) and dns.get('enable') is True and not dns.get('nameserver'):
        raise BaseSettingsError('dns.nameserver 至少填写一个上游')
    if ((changed(('dns', 'respect-rules')) or changed(('dns', 'proxy-server-nameserver')))
            and dns.get('respect-rules') is True and not dns.get('proxy-server-nameserver')):
        raise BaseSettingsError('开启 dns.respect-rules 时必须填写 dns.proxy-server-nameserver，否则 Mihomo 无法启动')


def validate_base_settings(document, previous=None, *, reserved_ports=None, local_addresses=()):
    """Raise BaseSettingsError for a value this save changes that the form would reject.

    ``previous`` is the saved base document (None checks every value);
    ``reserved_ports`` maps ports of Agents next to this Mihomo to a description;
    ``local_addresses`` are their host addresses, a DNS loop when used on :53 / :1053.
    """
    if not isinstance(document, dict):
        return
    if not isinstance(previous, dict):
        previous = None

    def changed(path):
        return previous is None or not _same(_get(document, path), _get(previous, path))

    hosts = set()
    for value in local_addresses or ():
        try:
            hosts.add(ipaddress.ip_address(value))
        except (TypeError, ValueError):
            continue
    _check_types(document, changed)
    _check_controller(document, changed)
    _check_access(document, changed)
    _check_ports(document, changed, reserved_ports or {})
    _check_dns(document, changed, hosts)


def takeover_mode(config):
    """Who rewrites the TUN on publish: 'linux_gateway', 'full_tun' (router split) or None."""
    from backend.common.linux_gateway import gateway_settings
    from backend.common.router_split import is_full_tun
    try:
        if gateway_settings(config).get('enabled'):
            return 'linux_gateway'
    except Exception:  # A malformed gateway setting is reported by generation itself.
        pass
    return 'full_tun' if is_full_tun(config) else None


def agent_context(repository, manager, profile_id):
    """Ports and host addresses of the Agents that run next to this profile's Mihomo.

    A Mihomo Agent always shares the host with its core; an OxiDNS Agent bound to
    the same profile counts when it is registered on one of those hosts. Docker
    Agents are skipped: their ports are mapped and may not be the core's host ports.
    """
    reserved, hosts = {}, []
    try:
        bindings = repository.get_profile_agent_bindings(profile_id)['agents']
    except Exception:
        return {}, []
    agents = [manager.get_agent_by_id(item['id']) or {} for item in bindings]
    agents = [agent for agent in agents if agent.get('deployment_method') != 'docker']
    mihomo_hosts = {agent.get('host') for agent in agents if (agent.get('service_type') or 'mihomo') == 'mihomo'}
    for agent in agents:
        service = agent.get('service_type') or 'mihomo'
        if service != 'mihomo' and agent.get('host') not in mihomo_hosts:
            continue
        port = agent.get('port')
        if type(port) is int:
            name = agent.get('name') or agent.get('id')
            reserved.setdefault(port, f"{'Mihomo' if service == 'mihomo' else 'OxiDNS'} Agent「{name}」的端口")
        if service == 'mihomo' and agent.get('host') and agent['host'] not in hosts:
            hosts.append(agent['host'])
    return reserved, hosts
