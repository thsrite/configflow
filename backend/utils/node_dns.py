"""Global standalone probe DNS settings."""
import ipaddress
import json
import os
import re
import subprocess
from urllib.parse import urlsplit, urlunsplit

from backend.common.config import get_repository, get_system_config, reset_config_context

CONFIG_KEY = 'node_test_dns_config'
# Port 443 is not taken over by a gateway's DNS hijack, and an IP needs no bootstrap lookup.
DIRECT_DOH = ('https://223.5.5.5/dns-query', 'https://1.12.12.12/dns-query')
DNS_ERROR = '测速 DNS 须为 IPv4[:端口]、https://DoH 或 tls://DoT，最多 4 个；不支持凭据、查询参数或片段'
BOOTSTRAP_ERROR = '引导 DNS 须为 IPv4 或 IPv4:端口，最多 4 个'
CHECK_MESSAGES = {
    'config': 'DNS 设置无效，请检查地址和引导 DNS',
    'dns_bootstrap': '引导 DNS 无法解析上游地址，请检查引导 DNS',
    'dns_fake_ip': 'DNS 返回了 Fake-IP，请改用返回真实 IP 的解析路径',
    'timeout': 'DNS 查询超时，请检查网络和服务状态',
    'tls': 'DNS TLS 验证失败，请检查上游地址和证书',
    'dns': 'DNS 未返回有效地址，请检查解析路径',
}


def _ipv4_server(value, message):
    if not isinstance(value, str):
        raise ValueError(message)
    host, separator, port = value.strip().partition(':')
    try:
        address = ipaddress.IPv4Address(host)
        if address.is_unspecified or address.is_multicast:
            raise ValueError
        if separator and (not port.isascii() or not port.isdecimal() or not 1 <= int(port) <= 65535):
            raise ValueError
    except ValueError as exc:
        raise ValueError(message) from exc
    return str(address) + (f':{int(port)}' if separator and int(port) != 53 else '')


def _server(value):
    if not isinstance(value, str) or not value.strip():
        raise ValueError(DNS_ERROR)
    value = value.strip()
    if '://' not in value:
        return _ipv4_server(value, DNS_ERROR)
    try:
        if len(value) > 2048 or any(c.isspace() or ord(c) < 32 or ord(c) == 127 for c in value) or any(c in value for c in ('?', '#', '\\')):
            raise ValueError
        parsed = urlsplit(value)
        if parsed.scheme not in ('https', 'tls') or not parsed.hostname or parsed.username is not None or parsed.password is not None:
            raise ValueError
        host = parsed.hostname.encode('idna').decode('ascii').lower()
        try:
            address = ipaddress.ip_address(host)
        except ValueError:
            if len(host) > 253 or not all(re.fullmatch(r'[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?', label) for label in host.removesuffix('.').split('.')):
                raise ValueError
        else:
            if address.version != 4 or address.is_unspecified or address.is_multicast:
                raise ValueError
        if re.search(r'%(?![0-9a-fA-F]{2})', parsed.path):
            raise ValueError
        port = parsed.port
        if (port is not None and not 1 <= port <= 65535) or parsed.netloc.endswith(':'):
            raise ValueError
        if parsed.scheme == 'tls' and parsed.path:
            raise ValueError
        default = 443 if parsed.scheme == 'https' else 853
        authority = host + (f':{port}' if port is not None and port != default else '')
        path = (parsed.path or '/dns-query') if parsed.scheme == 'https' else ''
        return urlunsplit((parsed.scheme, authority, path, '', ''))
    except (ValueError, UnicodeError) as exc:
        raise ValueError(DNS_ERROR) from exc


def normalize_dns_servers(value, *, bootstrap=False):
    message = BOOTSTRAP_ERROR if bootstrap else DNS_ERROR
    if not isinstance(value, list) or len(value) > 4:
        raise ValueError(message)
    result = []
    for item in value:
        normalized = _ipv4_server(item, message) if bootstrap else _server(item)
        if normalized not in result:
            result.append(normalized)
    return result


def _needs_bootstrap(server):
    if '://' not in server:
        return False
    try:
        ipaddress.IPv4Address(urlsplit(server).hostname)
        return False
    except ValueError:
        return True


def normalize_config(value):
    if not isinstance(value, dict) or value.get('mode') not in ('system', 'custom'):
        raise ValueError('请选择系统 DNS 或自定义 DNS')
    mode = value['mode']
    result = {'mode': mode, 'servers': [], 'bootstrap': []}
    if mode == 'custom':
        result['servers'] = normalize_dns_servers(value.get('servers', []))
        result['bootstrap'] = normalize_dns_servers(value.get('bootstrap', []), bootstrap=True)
        if not result['servers']:
            raise ValueError('自定义 DNS 至少需要一个服务器地址')
        if any(_needs_bootstrap(server) for server in result['servers']) and not result['bootstrap']:
            raise ValueError('域名形式的 DoH / DoT 需要指定引导 DNS（IPv4 或 IPv4:端口）')
    return result


def load_config():
    system = get_system_config().get('system_config', {})
    saved = system.get(CONFIG_KEY)
    if isinstance(saved, dict) and saved.get('mode') != 'managed':
        return normalize_config(saved)
    # Before node_test_dns_config existed. The removed "OxiDNS 托管" mode is
    # converted at startup (agents.node_test_dns_cleanup); until then its
    # address, always mirrored into node_test_dns, is read the same way.
    # Nothing is rewritten while reading.
    servers = normalize_dns_servers(system.get('node_test_dns', []))
    return normalize_config({'mode': 'custom' if servers else 'system', 'servers': servers})


def effective_settings(value=None, *, manager=None):
    config = load_config() if value is None else normalize_config(value)
    return {'dns_servers': config['servers'], 'dns_bootstrap': config['bootstrap']}


def describe_settings(value=None, *, manager=None):
    config = load_config() if value is None else normalize_config(value)
    return {'dns': config, **effective_settings(config, manager=manager)}


def save_config(value, *, manager=None):
    config = normalize_config(value)
    effective = effective_settings(config, manager=manager)

    def update(system):
        target = system.setdefault('system_config', {})
        target[CONFIG_KEY] = config
        target['node_test_dns'] = effective['dns_servers']
    get_repository().update_system_transaction(update)
    reset_config_context()
    return {'dns': config, **effective}


def check_settings(value, *, manager=None):
    """Query a fixed public name with the same worker resolver as node probing."""
    from backend.utils.node_latency import _probe_binary, ProbeUnavailable
    options = effective_settings(value, manager=manager)
    payload = {'operation': 'dns_check', 'timeout_ms': 5000, **options}
    env = dict(os.environ)
    env.setdefault('GOMAXPROCS', '2')
    env.setdefault('GOMEMLIMIT', '128MiB')
    try:
        process = subprocess.run([_probe_binary()], input=json.dumps(payload), capture_output=True,
                                 text=True, timeout=10, env=env)
    except subprocess.TimeoutExpired:
        return {'success': False, 'addresses': [], 'error_code': 'timeout', 'message': CHECK_MESSAGES['timeout']}
    except OSError as exc:
        raise ProbeUnavailable('测速组件无法启动，请检查可执行权限和 CPU 架构') from exc
    if process.returncode:
        raise ProbeUnavailable('DNS 检测组件异常退出，请检查组件版本后重试')
    try:
        response = json.loads(process.stdout)
        if response['version'] != 4:
            raise ProbeUnavailable('测速组件版本不匹配，请更新 ConfigFlow 镜像；源码部署请重新构建 backend/node-probe')
        result = response['dns_check']
        if (type(result.get('success')) is not bool
                or not isinstance(result.get('addresses'), list)
                or not all(isinstance(address, str) for address in result['addresses'])):
            raise ValueError
        addresses = list(dict.fromkeys(str(ipaddress.ip_address(address)) for address in result['addresses']))
        latency = result.get('latency_ms')
        if latency is not None and (type(latency) is not int or latency < 0):
            raise ValueError
        if result['success'] and (not addresses or any(ipaddress.ip_address(address) in ipaddress.ip_network('198.18.0.0/15') for address in addresses)):
            raise ValueError
        code = result.get('error_code', 'dns')
        if code not in CHECK_MESSAGES:
            code = 'dns'
        return {'success': result['success'], 'addresses': addresses, 'latency_ms': latency,
                **({'message': '解析成功，返回真实 IP'} if result['success'] else
                   {'error_code': code, 'message': CHECK_MESSAGES[code]})}
    except (ValueError, TypeError, KeyError, AttributeError) as exc:
        raise ProbeUnavailable('DNS 检测组件返回了无效结果，请检查组件版本') from exc
