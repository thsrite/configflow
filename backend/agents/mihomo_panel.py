"""Mihomo panel: view and switch policies, connections and rules through a registered Agent.

The Agent forwards to its local Mihomo controller with the controller secret it
reads itself; neither the secret nor arbitrary controller paths pass through here.
"""
import json
from urllib.parse import quote

import requests

from .version import compare_versions
from .oxidns_monitor import MonitorError, _agent_origin

MIN_AGENT_VERSION = '1.8.15-go'
MAX_RESPONSE_BYTES = 16 * 1024 * 1024
MAX_NAME_LENGTH = 512
DEFAULT_TEST_URL = 'https://www.gstatic.com/generate_204'

# (resource, method) -> Agent path template. Names are independently encoded
# as single path segments, including a provider name for its individual nodes.
ROUTES = {
    ('version', 'GET'): 'version',
    ('configs', 'GET'): 'configs',
    ('configs', 'PATCH'): 'configs',
    ('proxies', 'GET'): 'proxies',
    ('proxy', 'PUT'): 'proxies/{name}',
    ('proxy', 'DELETE'): 'proxies/{name}',
    ('proxy/delay', 'GET'): 'proxies/{name}/delay',
    ('group/delay', 'GET'): 'group/{name}/delay',
    ('connections', 'GET'): 'connections',
    ('connections', 'DELETE'): 'connections',
    ('connection', 'DELETE'): 'connections/{name}',
    # Agent-side aggregated live traffic (source → rule → policy → outlet).
    ('flow', 'GET'): 'flow',
    ('rules', 'GET'): 'rules',
    ('providers/proxies', 'GET'): 'providers/proxies',
    ('providers/proxy', 'PUT'): 'providers/proxies/{name}',
    ('providers/proxy/healthcheck', 'GET'): 'providers/proxies/{name}/healthcheck',
    ('providers/proxy/delay', 'GET'): 'providers/proxies/{provider}/{name}/healthcheck',
    ('providers/rules', 'GET'): 'providers/rules',
    ('providers/rule', 'PUT'): 'providers/rules/{name}',
    ('cache/fakeip/flush', 'POST'): 'cache/fakeip/flush',
    ('cache/dns/flush', 'POST'): 'cache/dns/flush',
    # The Agent reloads the core's current config file and ignores any body.
    ('configs/reload', 'PUT'): 'configs',
    ('upgrade', 'POST'): 'upgrade',
}
# Core operations added after the first panel release need a newer Agent.
RESOURCE_MIN_VERSION = {
    'configs/reload': '1.8.16-go', 'upgrade': '1.8.16-go',
    'providers/proxy/delay': '1.8.18-go',
    'flow': '1.8.26-go',
}
RESOURCE_FEATURE = {'providers/proxy/delay': '订阅节点测速', 'flow': '实时流量'}
RESOURCE_TIMEOUT = {'configs/reload': 70, 'upgrade': 200}
MODES = ('rule', 'global', 'direct')
# What the panel reads from a node or group. Mihomo also reports transport
# flags and keeps up to ten delay results per test URL for every node, and its
# per-group Compatible providers repeat each group's members in full; with an
# airport subscription that made providers/proxies about 6 MB.
PROXY_FIELDS = ('name', 'type', 'udp', 'alive', 'now', 'all', 'fixed', 'testUrl', 'hidden', 'icon')

MESSAGES = {
    'CONTROLLER_UNAVAILABLE': 'Agent 未找到 Mihomo 控制接口，请确认配置里启用了 external-controller。',
    'CORE_UNREACHABLE': '暂时无法连接 Mihomo 控制接口，请检查 Mihomo 运行状态。',
    'CORE_AUTH_FAILED': 'Mihomo 控制接口拒绝了 Agent 的 secret，请检查配置。',
    'AGENT_UNREACHABLE': '暂时无法连接 Agent，请检查网络和 Agent 运行状态。',
    'AGENT_AUTH_FAILED': 'Agent 认证失败，请检查注册状态。',
    'INVALID_RESPONSE': 'Mihomo 返回的数据格式异常。',
    'INVALID_REQUEST': '请求参数无效。',
}


def _fail(code, status=502, message=None):
    return MonitorError(code, status, message or MESSAGES.get(code, MESSAGES['INVALID_RESPONSE']))


def _agent(manager, agent_id, min_version=MIN_AGENT_VERSION, feature=None):
    agent = manager.get_agent_by_id(agent_id)
    if not agent:
        raise _fail('AGENT_NOT_FOUND', 404, '未找到这台 Agent。')
    if agent.get('service_type', 'mihomo') != 'mihomo':
        raise _fail('WRONG_SERVICE', 400, 'Mihomo 面板仅适用于 Mihomo Agent。')
    try:
        supported = compare_versions(agent.get('version', '0'), min_version) >= 0
    except (ValueError, TypeError, AttributeError):
        supported = False
    if not supported:
        feature = feature or ('Mihomo 面板' if min_version == MIN_AGENT_VERSION else '这个内核操作')
        raise _fail('UPGRADE_REQUIRED', 409, f'请将此 Agent 升级到 {min_version} 或更新版本后使用{feature}。')
    return agent


def _name(args, key='name'):
    name = args.get(key, '')
    if not isinstance(name, str) or not name or len(name) > MAX_NAME_LENGTH \
            or any(ord(c) < 32 for c in name):
        raise _fail('INVALID_REQUEST', 400)
    return name


def _query(resource, args):
    if not resource.endswith('/delay'):
        return {}
    test_url = args.get('url') or DEFAULT_TEST_URL
    if not isinstance(test_url, str) or not test_url.startswith(('http://', 'https://')) or len(test_url) > 2048:
        raise _fail('INVALID_REQUEST', 400, '测速地址必须是 http(s) 链接。')
    try:
        timeout = int(args.get('timeout', 5000))
    except (TypeError, ValueError):
        raise _fail('INVALID_REQUEST', 400) from None
    if not 500 <= timeout <= 30000:
        raise _fail('INVALID_REQUEST', 400, '测速超时需在 500–30000 毫秒之间。')
    return {'url': test_url, 'timeout': str(timeout)}


def _body(resource, method, body):
    if method not in ('PUT', 'PATCH') or resource == 'configs/reload':
        return None
    if resource == 'proxy':
        # Switching a selector group: {"name": "<member>"}
        if not isinstance(body, dict) or set(body) != {'name'} or not isinstance(body['name'], str) \
                or not body['name'] or len(body['name']) > MAX_NAME_LENGTH:
            raise _fail('INVALID_REQUEST', 400, '请选择要切换的节点。')
        return {'name': body['name']}
    if resource == 'configs':
        # Only the outbound mode is editable here; other runtime settings stay owned by the pushed config.
        if not isinstance(body, dict) or set(body) != {'mode'} or body['mode'] not in MODES:
            raise _fail('INVALID_REQUEST', 400, '代理模式只能是 rule、global 或 direct。')
        return {'mode': body['mode']}
    return {}


def _slim_proxy(proxy):
    """The panel's fields of a node or group, with only the newest delay result per test URL."""
    if not isinstance(proxy, dict):
        return proxy
    slim = {key: proxy[key] for key in PROXY_FIELDS if key in proxy}
    if isinstance(proxy.get('history'), list):
        slim['history'] = proxy['history'][-1:]
    if isinstance(proxy.get('extra'), dict):
        slim['extra'] = {
            url: {**({'alive': entry['alive']} if 'alive' in entry else {}),
                  'history': entry['history'][-1:] if isinstance(entry.get('history'), list) else []}
            for url, entry in proxy['extra'].items() if isinstance(entry, dict)
        }
    return slim


def _slim(resource, data):
    """Drop what the panel never shows from the node listings before they reach the browser."""
    if not isinstance(data, dict):
        return data
    if resource == 'proxies' and isinstance(data.get('proxies'), dict):
        return {**data, 'proxies': {name: _slim_proxy(proxy) for name, proxy in data['proxies'].items()}}
    if resource == 'providers/proxies' and isinstance(data.get('providers'), dict):
        providers = {}
        for name, provider in data['providers'].items():
            if isinstance(provider, dict) and isinstance(provider.get('proxies'), list):
                # A group's Compatible provider holds config nodes and groups that
                # /proxies already lists in full; the panel needs only its names.
                compatible = provider.get('vehicleType') == 'Compatible'
                provider = {**provider, 'proxies': [
                    {'name': proxy.get('name')} if compatible and isinstance(proxy, dict) else _slim_proxy(proxy)
                    for proxy in provider['proxies']]}
            providers[name] = provider
        return {**data, 'providers': providers}
    return data


def call(manager, agent_id, resource, method, args, body=None):
    template = ROUTES.get((resource, method))
    if template is None:
        raise _fail('NOT_ALLOWED', 404, '面板不支持这个操作。')
    agent = _agent(manager, agent_id, RESOURCE_MIN_VERSION.get(resource, MIN_AGENT_VERSION),
                   RESOURCE_FEATURE.get(resource))
    path = template
    for key in ('provider', 'name'):
        if '{' + key + '}' in template:
            path = path.replace('{' + key + '}', quote(_name(args, key), safe=''))
    params = _query(resource, args)
    payload = _body(resource, method, body)
    url = _agent_origin(agent) + '/api/mihomo/' + path
    timeout = RESOURCE_TIMEOUT.get(resource) or (
        40 if resource.endswith(('delay', 'healthcheck')) or resource.startswith('providers/') else 20)
    try:
        with requests.Session() as session:
            session.trust_env = False
            with session.request(method, url, params=params,
                                 data=json.dumps(payload) if payload is not None else None,
                                 headers={'Authorization': 'Bearer ' + agent['token'],
                                          'Accept': 'application/json',
                                          **({'Content-Type': 'application/json'} if payload is not None else {})},
                                 timeout=(3, timeout), allow_redirects=False, stream=True) as response:
                chunks, size = [], 0
                for chunk in response.iter_content(64 * 1024):
                    size += len(chunk)
                    if size > MAX_RESPONSE_BYTES:
                        raise _fail('INVALID_RESPONSE')
                    chunks.append(chunk)
                raw = b''.join(chunks)
                status = response.status_code
    except MonitorError:
        raise
    except (requests.RequestException, KeyError, TypeError, ValueError):
        raise _fail('AGENT_UNREACHABLE') from None

    if status in (401, 403):
        raise _fail('AGENT_AUTH_FAILED')
    try:
        data = json.loads(raw) if raw.strip() else None
    except (ValueError, UnicodeError):
        data = None
        if status == 404:
            raise _fail('UPGRADE_REQUIRED', 409, f'请将此 Agent 升级到 {MIN_AGENT_VERSION} 或更新版本后使用 Mihomo 面板。')
        if status < 400:
            raise _fail('INVALID_RESPONSE')
    if status >= 400:
        if isinstance(data, dict) and data.get('success') is False and data.get('code') in MESSAGES:
            raise _fail(data['code'], 409 if data['code'] == 'CONTROLLER_UNAVAILABLE' else 502)
        # Mihomo's own errors (delay timeout, unknown proxy …) are short and safe to show.
        message = data.get('message') if isinstance(data, dict) else None
        if not isinstance(message, str) or not message:
            message = f'Mihomo 返回 HTTP {status}'
        raise _fail('CORE_ERROR', 504 if status in (408, 504) else 400 if status < 500 else 502, message[:300])
    return {'success': True, 'data': _slim(resource, data) if method == 'GET' else data}
