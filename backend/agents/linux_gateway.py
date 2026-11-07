"""Bounded, profile-scoped bridge to the Agent's opt-in Linux gateway API."""
import ipaddress
import json
import re
import unicodedata

import requests

from backend.common.config import get_config
from .manager import is_online
from .oxidns_monitor import MonitorError, _agent_origin
from .version import compare_versions

MIN_AGENT_VERSION = '1.8.25-go'
# Persists AIO forwarding and keeps it when a gateway record is removed.
FORWARDING_MIN_AGENT_VERSION = '1.8.40-go'
# Single-service Shell Mihomo Agents persist forwarding from this version on.
STANDALONE_FORWARDING_MIN_AGENT_VERSION = '1.8.41-go'
FORWARDING_CONFIG_PATH = '/etc/sysctl.d/99-configflow-aio-forward.conf'
STANDALONE_UPGRADE = (f'单独安装的 Mihomo Agent 需更新到 {STANDALONE_FORWARDING_MIN_AGENT_VERSION} '
                      '或更新版本才能开启并持久化 IPv4 转发。')
ROUTER_SPLIT_REQUIRED = '仅主路由分流（完整 TUN）的配置空间，或「上游路由」中作为代理网关的 Mihomo Agent 可开启转发持久化。'
MAX_RESPONSE_BYTES = 512 * 1024
OPERATIONS = {'status': 'GET', 'preview': 'POST', 'apply': 'POST', 'remove': 'POST', 'forwarding': 'POST'}
# Operations that change host networking; the router split guards each one.
HOST_OPERATIONS = ('apply', 'remove', 'forwarding')
SPEC_FIELDS = {'interface', 'address', 'lan_cidr', 'upstream_gateway'}
FORWARDING_NOT_KEPT = ('设备上仍有旧版 Linux 网关记录，且 IPv4 转发尚未持久化：{action}Agent 撤销该记录时会把 IPv4 转发还原为关闭，'
                       '主路由转来的连接会中断。请先在 Linux 网关安装向导的「检查转发与 TUN」中开启并持久化 IPv4 转发。')
# Turning the gateway off on an old router split host whose base YAML gained a full TUN.
GATEWAY_OFF_NOT_KEPT = ('关闭 Linux 网关会让 Agent 撤销「{name}」上的旧版网关记录，并把这台 Linux 的 IPv4 转发还原为关闭，'
                        '主路由分流转来的连接会中断。这台设备尚未持久化 IPv4 转发，已阻止；请保持 Linux 网关开启。'
                        '如这台设备是路由器的代理网关，可在 Agent 页这台设备的卡片上开启并持久化 IPv4 转发后再操作。')
GATEWAY_OFF_UPGRADE = ('关闭 Linux 网关前需要确认「{name}」已持久化 IPv4 转发，'
                       f'请先将 Mihomo Agent 更新到 {FORWARDING_MIN_AGENT_VERSION} 或更新版本。')
GATEWAY_OFF_UNKNOWN = '无法读取「{name}」的 IPv4 转发状态，已阻止关闭 Linux 网关：{reason}。请确认设备在线后重试。'
PUBLISH_GATEWAY_OFF_NOT_KEPT = ('待发布的 Mihomo 配置关闭了 Linux 网关：发布后 Agent 会撤销设备上的旧版网关记录，'
                                '并把 IPv4 转发还原为关闭，主路由分流转来的连接会中断。这台设备尚未持久化 IPv4 转发，已停止发布。'
                                '请在 Linux 网关中重新开启并保存后再发布。')
# Removing the record of an old router split whose base YAML gained a full TUN.
LEGACY_REMOVE_NOT_KEPT = ('撤销会删除旧版网关记录，并把这台 Linux 的 IPv4 转发还原为关闭，主路由分流转来的连接会中断。'
                          '这台设备尚未持久化 IPv4 转发，已阻止；请保持网关转发。如这台设备是路由器的代理网关，可在 Agent 页这台设备的卡片上开启并持久化 IPv4 转发后再操作。')
LEGACY_REMOVE_UPGRADE = ('撤销前需要确认这台 Linux 已持久化 IPv4 转发，'
                         f'请先将 Mihomo Agent 更新到 {FORWARDING_MIN_AGENT_VERSION} 或更新版本。')
# The Agent's own remove text tells clients to restore their gateway; a router
# split's clients never changed it.
REMOVED_MESSAGES = {
    'full_tun': '旧网关记录已清理，IPv4 转发保持开启。',
    'full_tun_none': '设备上没有旧网关记录，无需清理；IPv4 转发保持不变。',
    'legacy': '网关转发已撤销，主路由转来的连接已中断。请删除主路由上指向这台 Linux 的静态路由，或重新应用网关转发。',
}


def _at_least(agent, minimum):
    version = agent.get('version', '')
    if not isinstance(version, str) or not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+(?:-go)?', version):
        return False
    try:
        return compare_versions(version, minimum) >= 0
    except (ValueError, TypeError, AttributeError):
        return False


def supports_gateway(agent):
    return _at_least(agent, MIN_AGENT_VERSION)


def supports_forwarding(agent):
    return _at_least(agent, FORWARDING_MIN_AGENT_VERSION)


def supports_standalone_forwarding(agent):
    return _at_least(agent, STANDALONE_FORWARDING_MIN_AGENT_VERSION)


def proxy_gateway_routers(config, agent_id):
    """Upstream routers in this profile whose proxied traffic goes to ``agent_id``."""
    from backend.routers.store import routers_of
    return [item for item in routers_of(config) if agent_id and item.get('proxy_gateway_agent_id') == agent_id]


def proxy_gateways_all_profiles():
    """agent_id -> every upstream router, in any profile, sending proxied traffic to it.

    The Agents page lists devices from all profiles, so its forwarding view
    follows router links wherever they are; each reference names its profile.
    """
    from backend.common.config import get_repository
    from backend.routers.store import routers_of
    linked = {}
    for profile in get_repository().list_profiles():
        for item in routers_of(get_config(profile['id'])):
            agent_id = item.get('proxy_gateway_agent_id')
            if agent_id and isinstance(agent_id, str):
                linked.setdefault(agent_id, []).append({
                    'id': item.get('id'), 'name': item.get('name') or item.get('id'),
                    'profile_id': profile['id'], 'profile_name': profile.get('name') or profile['id']})
    return linked


def _forwarding_upgrade_needed(status, agent):
    """Whether only a newer Agent could persist forwarding here.

    The host's platform is supported, but this Agent does not recognise its
    installation: a single-service install below 1.8.41-go, or any Agent
    without the forwarding status (below 1.8.40-go).
    """
    forwarding = status.get('ipv4_forwarding') if isinstance(status, dict) else None
    if not isinstance(forwarding, dict):
        return True
    return (forwarding.get('supported') is False and status.get('supported') is True
            and not supports_standalone_forwarding(agent))


def _managed(status):
    managed = status.get('managed') if isinstance(status, dict) else None
    return managed if isinstance(managed, dict) else {}


def has_record(status):
    """Whether the host still has a gateway record (an unreadable one counts)."""
    managed = _managed(status)
    return bool(managed.get('active') or managed.get('pending') or managed.get('error'))


def keeps_forwarding(status):
    """Whether removing the host's gateway record, if any, leaves forwarding on."""
    if not has_record(status):
        return True
    # Only the persisted forwarding (AIO 1.8.40-go, single-service 1.8.41-go)
    # stops the Agent writing the original values back.
    return _managed(status).get('preserves_forwarding') is True


def _record_kept(agent, status):
    # Only an Agent from FORWARDING_MIN_AGENT_VERSION on persists forwarding.
    return not has_record(status) or (supports_forwarding(agent) and keeps_forwarding(status))


def record_status(manager, agent, profile_id):
    """The host's gateway status, or None when the Agent cannot hold a gateway record.

    An Agent below MIN_AGENT_VERSION, or one without the gateway endpoint
    (404), never applied a gateway, so it has nothing to remove.
    """
    if not supports_gateway(agent):
        return None
    try:
        return call(manager, agent.get('id'), 'status', 'GET', profile_id)
    except MonitorError as error:
        if error.code == 'UPGRADE_REQUIRED':
            return None
        raise


def require_forwarding_kept(manager, agent):
    """A full TUN publish makes the Agent remove an old gateway record; refuse if that turns forwarding off."""
    try:
        status = call(manager, agent.get('id'), 'status', 'GET', agent.get('profile_id'))
    except MonitorError as error:
        raise ValueError(f'无法确认设备上是否仍有旧版 Linux 网关记录，已停止发布：{error.message}') from None
    if not keeps_forwarding(status):
        raise ValueError(FORWARDING_NOT_KEPT.format(action='发布完整 TUN 后，'))


def require_gateway_off_kept(manager, agent):
    """An old router split's gateway-off publish makes the Agent remove its record; refuse if that turns forwarding off."""
    try:
        status = record_status(manager, agent, agent.get('profile_id'))
    except MonitorError as error:
        raise ValueError(f'无法确认设备上是否仍有旧版 Linux 网关记录，已停止发布：{error.message}') from None
    if not _record_kept(agent, status):
        raise ValueError(PUBLISH_GATEWAY_OFF_NOT_KEPT)


def gateway_off_refusal(manager, profile_id, agent_ids):
    """Why turning the gateway off would turn IPv4 forwarding off on a bound host, or None.

    Only the persisted forwarding (1.8.40-go AIO, 1.8.41-go single-service) keeps it when the Agent
    removes its gateway record; a host without a record keeps what it has,
    whatever its version. ``agent_ids`` are the hosts the old split's
    gateway may be on (``router_split.legacy_gateway_agent_ids``).
    """
    for agent_id in agent_ids:
        agent = manager.get_agent_by_id(agent_id)
        if not agent or agent.get('service_type') != 'mihomo' or agent.get('deployment_method') == 'docker':
            continue  # Docker never applies the gateway on the host.
        name = agent.get('name') or agent_id
        try:
            status = record_status(manager, agent, profile_id)
        except MonitorError as error:
            return GATEWAY_OFF_UNKNOWN.format(name=name, reason=error.message.rstrip('。'))
        if _record_kept(agent, status):
            continue
        # A record without persisted forwarding: only 1.8.40-go can persist it.
        return GATEWAY_OFF_NOT_KEPT.format(name=name) if supports_forwarding(agent) else GATEWAY_OFF_UPGRADE.format(name=name)
    return None


def require_gateway_agent(agent):
    if not supports_gateway(agent):
        raise ValueError(f'Linux 网关需要 Mihomo Agent {MIN_AGENT_VERSION} 或更新版本，请先升级 Agent 后再发布。')


def require_gateway_bundle_agent(agent, bundle):
    """Guard upload even if a caller bypassed shared profile preparation."""
    if agent.get('service_type', 'mihomo') != 'mihomo' or supports_gateway(agent):
        return
    import io
    import tarfile
    import yaml

    with tarfile.open(fileobj=io.BytesIO(bundle.archive), mode='r:gz') as archive:
        document = yaml.safe_load(archive.extractfile('files/' + bundle.manifest['config_path']).read())
    gateway = document.get('x-configflow-linux-gateway') if isinstance(document, dict) else None
    if isinstance(gateway, dict) and gateway.get('enabled') is True:
        require_gateway_agent(agent)


def _error(code, status, message):
    return MonitorError(code, status, message)


def _spec(body):
    if not isinstance(body, dict) or set(body) != SPEC_FIELDS:
        raise _error('INVALID_REQUEST', 400, '请填写网卡、Linux IPv4、LAN 网段和主路由 IPv4。')
    if any(not isinstance(v, str) or not v or len(v) > 64 for v in body.values()):
        raise _error('INVALID_REQUEST', 400, '网关转发的网络参数必须是有效的短文本。')
    interface = body['interface']
    if not re.fullmatch(r'[A-Za-z0-9_][A-Za-z0-9_.-]{0,14}', interface) or interface == 'lo':
        raise _error('INVALID_REQUEST', 400, '请选择实际的 LAN 网卡，不能使用回环网卡。')
    try:
        address = ipaddress.IPv4Address(body['address'])
        upstream = ipaddress.IPv4Address(body['upstream_gateway'])
        network = ipaddress.IPv4Network(body['lan_cidr'], strict=True)
    except ValueError:
        raise _error('INVALID_REQUEST', 400, '需要合法 IPv4 地址和规范 LAN 网段，例如 192.168.1.0/24。') from None
    if not 8 <= network.prefixlen <= 30 or any(
        value not in network or value in (network.network_address, network.broadcast_address)
        or value.is_multicast or value.is_unspecified or value.is_loopback or value.is_link_local
        for value in (address, upstream)
    ) or address == upstream:
        raise _error('INVALID_REQUEST', 400, 'Linux 和主路由需要不同的可用 IPv4 地址，且位于同一个 /8 至 /30 LAN 网段。')
    return {**body, 'address': str(address), 'upstream_gateway': str(upstream), 'lan_cidr': str(network)}


def _router_split_mode(profile_id, config):
    """The profile's takeover; the setup and topology lookups run only if needed."""
    from backend.common.linux_gateway import LinuxGatewayError, gateway_settings
    from backend.common.router_split import read_marker, router_split_state
    marker = read_marker(config)[0]
    if marker != 'absent':
        return 'full_tun' if marker == 'valid' else 'invalid'
    try:
        if not gateway_settings(config)['enabled']:
            return None
    except LinuxGatewayError:
        return None
    from backend.common.config import get_repository
    from backend.common.router_split import base_is_full_tun, legacy_context, legacy_source
    repository = get_repository()
    mode = router_split_state(repository, profile_id, config)['mode']
    if mode is None and base_is_full_tun(config) and legacy_source(config, legacy_context(repository, profile_id)):
        # An old router split whose base YAML gained a full TUN: removing the
        # record keeps the TUN, but turns forwarding off unless it is persisted.
        return 'legacy_full_tun'
    return mode


def _router_split_guard(manager, agent, operation, profile_id, body):
    """Keep the main router's forwarded connections.

    Returns the Agent payload and the takeover, which a successful remove
    reports with ``REMOVED_MESSAGES`` (``full_tun_none``: no record was there).
    """
    from backend.common.router_split import MESSAGES
    confirmed = isinstance(body, dict) and set(body) == {'confirm_router_split_teardown'} \
        and body['confirm_router_split_teardown'] is True
    if operation == 'remove' and body not in (None, {}) and not confirmed:
        raise _error('INVALID_REQUEST', 400, '撤销网关转发不接受自定义命令或网络参数。')
    mode = _router_split_mode(profile_id, get_config(profile_id))
    if operation == 'apply':
        if mode == 'full_tun':
            raise _error('ROUTER_SPLIT_FULL_TUN', 409, MESSAGES['ROUTER_SPLIT_FULL_TUN'])
        if mode == 'invalid':
            raise _error('ROUTER_SPLIT_INVALID', 409, MESSAGES['ROUTER_SPLIT_INVALID'])
        return None, mode
    if operation == 'remove':
        if mode == 'legacy' and not confirmed:
            raise _error('ROUTER_SPLIT_LEGACY_TEARDOWN', 409, MESSAGES['ROUTER_SPLIT_LEGACY_TEARDOWN'])
        if mode == 'full_tun':
            if not supports_forwarding(agent):
                # An older Agent writes the original forwarding values back.
                raise _error('UPGRADE_REQUIRED', 409, f'请先将 Mihomo Agent 更新到 {FORWARDING_MIN_AGENT_VERSION} 或更新版本。')
            # So does a current one unless the AIO forwarding is persisted.
            status = call(manager, agent['id'], 'status', 'GET', profile_id)
            if not keeps_forwarding(status):
                raise _error('FORWARDING_NOT_PERSISTED', 409, FORWARDING_NOT_KEPT.format(action=''))
            if not has_record(status):
                mode = 'full_tun_none'
        elif mode == 'legacy_full_tun':
            # The same rule as turning the gateway off in the editor; a
            # confirmed teardown accepts the interruption, as for a legacy split.
            status = call(manager, agent['id'], 'status', 'GET', profile_id)
            if not has_record(status):
                mode = 'full_tun_none'
            elif _record_kept(agent, status):
                mode = 'full_tun'
            elif confirmed:
                mode = 'legacy'
            elif not supports_forwarding(agent):
                raise _error('UPGRADE_REQUIRED', 409, LEGACY_REMOVE_UPGRADE)
            else:
                raise _error('FORWARDING_NOT_PERSISTED', 409, LEGACY_REMOVE_NOT_KEPT)
        return {}, mode
    if not supports_forwarding(agent):
        raise _error('UPGRADE_REQUIRED', 409, f'请先将 Mihomo Agent 更新到 {FORWARDING_MIN_AGENT_VERSION} 或更新版本。')
    if mode == 'full_tun':
        if body not in (None, {}) and (not isinstance(body, dict) or set(body) != {'prepare_tun'}
                                       or type(body['prepare_tun']) is not bool):
            raise _error('INVALID_REQUEST', 400, '开启转发持久化只接受 prepare_tun 参数。')
        payload = {} if body in (None, {}) else {'prepare_tun': body['prepare_tun']}
    elif proxy_gateway_routers(get_config(profile_id), agent['id']):
        # Persisting only turns 0 into 1 and never touches a gateway journal; with
        # it, removing that gateway later keeps forwarding instead of restoring 0.
        if body not in (None, {}):
            raise _error('INVALID_REQUEST', 400, '为代理网关开启转发持久化不接受额外参数。')
        payload = {}
    else:
        raise _error('ROUTER_SPLIT_REQUIRED', 409, ROUTER_SPLIT_REQUIRED)
    _require_forwarding_layout(agent, lambda: _request(agent, 'status', 'GET'))
    return payload, mode


def _require_forwarding_layout(agent, read_status):
    """Below 1.8.41-go only the AIO layout persists; say so instead of the Agent's refusal."""
    if not supports_standalone_forwarding(agent) and _forwarding_upgrade_needed(read_status(), agent):
        raise _error('UPGRADE_REQUIRED', 409, STANDALONE_UPGRADE)


def _check_agent(agent):
    if agent.get('service_type') != 'mihomo':
        raise _error('WRONG_SERVICE', 400, 'Linux 网关只能应用到 Mihomo Agent。')
    if agent.get('deployment_method') == 'docker':
        raise _error('UNSUPPORTED_ENVIRONMENT', 409, '自动网络配置适用于 Linux Shell 安装，请选择原生安装的 Mihomo Agent。')
    if not supports_gateway(agent):
        raise _error('UPGRADE_REQUIRED', 409, f'请先将 Mihomo Agent 更新到 {MIN_AGENT_VERSION} 或更新版本。')


def call(manager, agent_id, operation, method, profile_id, body=None):
    if OPERATIONS.get(operation) != method:
        raise _error('NOT_ALLOWED', 404, '不支持这个网关操作。')
    agent = manager.get_agent_by_id(agent_id)
    if not agent:
        raise _error('AGENT_NOT_FOUND', 404, '未找到这台 Agent。')
    if not profile_id or agent.get('profile_id') != profile_id:
        raise _error('PROFILE_CHANGED', 409, '设备绑定的配置空间已变化，请重新打开网关设置。')
    _check_agent(agent)
    split = None
    if operation in HOST_OPERATIONS:
        payload, split = _router_split_guard(manager, agent, operation, profile_id, body)
    if operation == 'apply':
        config = get_config(profile_id)
        if (config.get('mihomo', {}).get('linux_gateway') or {}).get('enabled') is not True:
            raise _error('GATEWAY_DISABLED', 409, '请先启用并保存当前配置空间的 Linux 网关。')
    if operation in ('preview', 'apply'):
        payload = {**_spec(body), 'profile_id': profile_id}
    elif operation not in HOST_OPERATIONS:
        payload = None
    data = _request(agent, operation, method, payload)
    if operation == 'status' and 'forward_filter' in data:
        forward_filter = sanitize_forward_filter(data['forward_filter'])
        if forward_filter is None:
            del data['forward_filter']
        else:
            data['forward_filter'] = forward_filter
    if operation == 'remove' and split in REMOVED_MESSAGES and data.get('success') is True:
        data['message'] = REMOVED_MESSAGES[split]
    return data


def _request(agent, operation, method, payload=None):
    """One bounded request to the Agent's gateway API, with its errors mapped."""
    try:
        with requests.Session() as session:
            session.trust_env = False
            with session.request(method, _agent_origin(agent) + '/api/linux-gateway/' + operation,
                                 json=payload, headers={'Authorization': 'Bearer ' + agent['token'],
                                                        'Accept': 'application/json'},
                                 timeout=(3, 45), allow_redirects=False, stream=True) as response:
                chunks, size = [], 0
                for chunk in response.iter_content(64 * 1024):
                    size += len(chunk)
                    if size > MAX_RESPONSE_BYTES:
                        raise _error('INVALID_RESPONSE', 502, 'Agent 返回的网关数据过大。')
                    chunks.append(chunk)
                status = response.status_code
                raw = b''.join(chunks)
    except MonitorError:
        raise
    except (requests.RequestException, KeyError, TypeError, ValueError):
        if operation in ('apply', 'remove'):
            message = '未能确认网关操作结果，请先刷新设备网络状态，勿重复应用。'
        elif operation == 'forwarding':
            message = '未能确认转发持久化结果，请先刷新设备状态，勿重复操作。'
        else:
            message = '暂时无法连接 Agent，请检查设备连接。'
        raise _error('AGENT_UNREACHABLE', 502, message) from None
    if status == 404:
        minimum = FORWARDING_MIN_AGENT_VERSION if operation == 'forwarding' else MIN_AGENT_VERSION
        raise _error('UPGRADE_REQUIRED', 409, f'设备尚未提供网关接口，请更新到包含本功能的 {minimum} 或更新版本。')
    if status in (401, 403):
        raise _error('AGENT_AUTH_FAILED', 502, 'Agent 认证失败，请检查设备注册状态。')
    try:
        data = json.loads(raw)
    except (ValueError, UnicodeError):
        raise _error('INVALID_RESPONSE', 502, 'Agent 返回的网关数据无法解析，请刷新设备状态。') from None
    if not isinstance(data, dict):
        raise _error('INVALID_RESPONSE', 502, 'Agent 返回的网关数据格式异常。')
    if status >= 400:
        message = data.get('message') or data.get('error')
        if not isinstance(message, str) or not message:
            message = '网关操作未完成，请刷新状态后查看检查结果。'
        raise _error('GATEWAY_ERROR', status if status in (400, 409, 422) else 502, message[:1000])
    return data


# IPv4 forwarding on each router's proxy gateway: shown and persisted on the
# Agents page (router links in any profile), and read by the 上游路由 page to
# warn about its own routers. Both authorise by the router link, not the
# Agent's profile binding.
GATEWAY_FORWARDING_MESSAGES = {
    'missing': '关联的 Agent 已删除，请在路由器设置中重新选择代理网关。',
    'not_mihomo': '代理网关不是 Mihomo Agent。',
    'docker': 'Docker 部署的 Agent 不管理宿主机网络，请在宿主机上开启并持久化 IPv4 转发。',
    'offline': 'Agent 离线。',
    'upgrade_required': (f'请先将 Mihomo Agent 更新到 {STANDALONE_FORWARDING_MIN_AGENT_VERSION} '
                         '或更新版本，才能在此开启并持久化 IPv4 转发。'),
    'config_blocked': (f'IPv4 转发持久化文件 {FORWARDING_CONFIG_PATH} 已被修改或不是 ConfigFlow 写入的内容，'
                       '请先在设备上核对。'),
    'hook_blocked': 'IPv4 转发启动脚本已被修改，请先在设备上核对。',
}
_FORWARDING_FIELDS = ('current', 'persisted', 'config', 'hook_script', 'startup_hook', 'layout')

# Full-TUN FORWARD filtering, a warning only (Agent 1.8.42-go and later). The
# Agent's report is rendered as copy-paste commands, so it is reduced to known
# values; an invalid entry is dropped rather than failing the whole status.
FORWARD_FILTER_STATES = frozenset({'ok', 'may_drop', 'unknown', 'not_applicable'})
FORWARD_FILTER_SOURCES = frozenset({'docker', 'iptables', 'nftables', 'firewalld', 'ufw'})
# Blocked paths, in the Agent's display order (netfilter.Flow*).
FORWARD_FILTER_FLOWS = ('forward', 'dns', 'local_dns', 'redirect_tcp', 'tun_tcp')
# Protocols the forward flow is blocked for (Result.ForwardProtocols), in order;
# empty for an Agent that does not report them.
FORWARD_FILTER_PROTOCOLS = ('tcp', 'udp', 'icmp')
FORWARD_FILTER_MAX_SOURCES = 8
FORWARD_FILTER_MAX_REMEDIES = 6
FORWARD_FILTER_MAX_COMMANDS = 12
FORWARD_FILTER_MAX_COMMAND = 240
# Same as the Agent's TUN device pattern (aioDevicePattern, netfilter.DevicePattern).
_FORWARD_FILTER_DEVICE = re.compile(r'[A-Za-z0-9_][A-Za-z0-9_.-]{0,14}')
_FORWARD_FILTER_NFT_NAME = re.compile(r'[A-Za-z_][A-Za-z0-9_.-]{0,63}')
_FORWARD_FILTER_REASON = re.compile(r'[a-z][a-z0-9_]{0,31}')
# One firewall tool per '&&'-joined step; no shell expansion, pipes or redirects.
_FORWARD_FILTER_COMMAND = re.compile(r'(?:iptables|iptables-legacy|nft|firewall-cmd|ufw)( [A-Za-z0-9_.:=/,+"\'-]+)+')
# The only compound line: UFW has no command for conntrack DNAT, so its rule is
# added once to /etc/ufw/before.rules (netfilter.UFWBeforeRulesCommand). Kept as
# an exact string instead of allowing grep or sed in general.
_UFW_DNAT_RULE = '-A ufw-before-input -m conntrack --ctstate DNAT -j ACCEPT'
FORWARD_FILTER_UFW_BEFORE_RULES = (f"grep -q -- '{_UFW_DNAT_RULE}' /etc/ufw/before.rules || "
                                   f"sed -i '/^# End required lines/a {_UFW_DNAT_RULE}' /etc/ufw/before.rules")
# firewalld's --new-zone / --new-policy fail with NAME_CONFLICT when re-run, so
# they are guarded by an existence check (netfilter.FirewalldNew*Command); only
# these exact lines may redirect output or use '||'.
FORWARD_FILTER_FIREWALLD_TEMPLATES = frozenset(
    f'firewall-cmd --permanent --info-{kind}={name} >/dev/null 2>&1 || firewall-cmd --permanent --new-{kind}={name}'
    for kind, name in (('zone', 'configflow-tun'), ('policy', 'configflow-fwd')))


def _forward_filter_text(value, limit):
    if not isinstance(value, str):
        return ''
    text = ''.join(c for c in value if unicodedata.category(c) not in ('Cc', 'Cf', 'Zl', 'Zp'))
    return text.strip()[:limit]


def _forward_filter_choice(value, allowed):
    return value if isinstance(value, str) and value in allowed else ''


def _forward_filter_command(value):
    if not isinstance(value, str) or not value or len(value) > FORWARD_FILTER_MAX_COMMAND:
        return None
    if value == FORWARD_FILTER_UFW_BEFORE_RULES or value in FORWARD_FILTER_FIREWALLD_TEMPLATES:
        return value
    if not all(_FORWARD_FILTER_COMMAND.fullmatch(step) for step in value.split(' && ')):
        return None
    return value


def _forward_filter_source(value):
    if not isinstance(value, dict) or value.get('kind') not in FORWARD_FILTER_SOURCES:
        return None
    names = {key: value.get(key) if isinstance(value.get(key), str) and _FORWARD_FILTER_NFT_NAME.fullmatch(value.get(key))
             else '' for key in ('table', 'chain')}
    return {
        'kind': value['kind'],
        'backend': _forward_filter_choice(value.get('backend'), ('nft', 'legacy')),
        'hook': _forward_filter_choice(value.get('hook'), ('forward', 'input')),
        'family': _forward_filter_choice(value.get('family'), ('ip', 'inet')),
        **names,
        'policy': _forward_filter_choice(value.get('policy'), ('drop', 'accept')),
        'detail': _forward_filter_text(value.get('detail'), 200),
    }


def _forward_filter_remedy(value):
    if not isinstance(value, dict) or value.get('source') not in FORWARD_FILTER_SOURCES:
        return None
    device = value.get('device')
    device = device if isinstance(device, str) and _FORWARD_FILTER_DEVICE.fullmatch(device) else ''
    commands = value.get('commands') if isinstance(value.get('commands'), list) else []
    # Commands name the TUN device; without a valid one none can be trusted.
    commands = [command for command in map(_forward_filter_command, commands) if command] if device else []
    return {
        'source': value['source'],
        'label': _forward_filter_text(value.get('label'), 80),
        'device': device,
        'commands': commands[:FORWARD_FILTER_MAX_COMMANDS],
        'persist_note': _forward_filter_text(value.get('persist_note'), 300),
    }


def sanitize_forward_filter(value):
    """The Agent's forward_filter report reduced to known values, or None when it is unusable."""
    if not isinstance(value, dict) or value.get('state') not in FORWARD_FILTER_STATES:
        return None
    device = value.get('device')
    reason = value.get('reason')
    sources = value.get('sources') if isinstance(value.get('sources'), list) else []
    remedies = value.get('remedies') if isinstance(value.get('remedies'), list) else []
    flows = value.get('flows') if isinstance(value.get('flows'), list) else []
    protocols = value.get('forward_protocols') if isinstance(value.get('forward_protocols'), list) else []
    return {
        'protocol': value.get('protocol') if type(value.get('protocol')) is int else 0,
        'state': value['state'],
        'reason': reason if isinstance(reason, str) and _FORWARD_FILTER_REASON.fullmatch(reason) else '',
        'device': device if isinstance(device, str) and _FORWARD_FILTER_DEVICE.fullmatch(device) else '',
        'auto_redirect': value.get('auto_redirect') is True,
        'policy': _forward_filter_choice(value.get('policy'), ('drop', 'accept')),
        'input_policy': _forward_filter_choice(value.get('input_policy'), ('drop', 'accept')),
        'flows': [flow for flow in FORWARD_FILTER_FLOWS if flow in flows],
        'forward_protocols': [proto for proto in FORWARD_FILTER_PROTOCOLS if proto in protocols] if 'forward' in flows else [],
        'sources': [source for source in map(_forward_filter_source, sources) if source][:FORWARD_FILTER_MAX_SOURCES],
        'remedies': [remedy for remedy in map(_forward_filter_remedy, remedies) if remedy][:FORWARD_FILTER_MAX_REMEDIES],
        'hint': _forward_filter_text(value.get('hint'), 400),
        'verify': _forward_filter_text(value.get('verify'), 300),
        'checked_at': _forward_filter_text(value.get('checked_at'), 40),
        'cached': value.get('cached') is True,
    }


def _gateway_entry(agent_id, agent, routers, state, message='', status=None, can_persist=False):
    forwarding = status.get('ipv4_forwarding') if isinstance(status, dict) else None
    forwarding = forwarding if isinstance(forwarding, dict) else {}
    agent = agent or {}
    return {
        'agent_id': agent_id,
        'agent_name': agent.get('name') or agent_id,
        'agent_version': agent.get('version') if isinstance(agent.get('version'), str) else '',
        'routers': list(routers),
        'state': state,
        'can_persist': can_persist,
        'message': message,
        'minimum_version': STANDALONE_FORWARDING_MIN_AGENT_VERSION if state == 'upgrade_required' else None,
        **{field: forwarding.get(field) for field in _FORWARDING_FIELDS},
        'gateway_record': has_record(status) if isinstance(status, dict) else False,
        'preserves_forwarding': _managed(status).get('preserves_forwarding') is True,
        'forward_filter': sanitize_forward_filter(status.get('forward_filter')) if isinstance(status, dict) else None,
        'system_source': _system_source(forwarding) if state == 'system_persisted' else None,
    }


def _system_source(forwarding):
    """The sysctl file:line that persists forwarding on the host (Agent 1.8.43-go and later)."""
    source = forwarding.get('system_source')
    if isinstance(source, str) and 0 < len(source) <= 300 and source.isprintable():
        return source
    return None


def _entry_from_status(agent_id, agent, routers, status):
    forwarding = status.get('ipv4_forwarding') if isinstance(status, dict) else None
    if _forwarding_upgrade_needed(status, agent):
        message = STANDALONE_UPGRADE if isinstance(forwarding, dict) else GATEWAY_FORWARDING_MESSAGES['upgrade_required']
        return _gateway_entry(agent_id, agent, routers, 'upgrade_required', message, status)
    if forwarding.get('supported') is False:
        reason = forwarding.get('unsupported_reason')
        return _gateway_entry(agent_id, agent, routers, 'unsupported', reason if isinstance(reason, str) else '', status)
    if forwarding.get('config') in ('foreign', 'symlink'):
        return _gateway_entry(agent_id, agent, routers, 'blocked', GATEWAY_FORWARDING_MESSAGES['config_blocked'], status)
    if forwarding.get('hook_script') == 'modified':
        return _gateway_entry(agent_id, agent, routers, 'blocked', GATEWAY_FORWARDING_MESSAGES['hook_blocked'], status)
    if forwarding.get('current') == '1' and forwarding.get('persisted') is True:
        return _gateway_entry(agent_id, agent, routers, 'persisted', '', status)
    if forwarding.get('current') == '1' and forwarding.get('system_persisted') is True:
        # The host's own sysctl configuration keeps forwarding on across reboots.
        # ConfigFlow's persistence is still offered: only it keeps forwarding
        # when a Linux gateway record is removed.
        return _gateway_entry(agent_id, agent, routers, 'system_persisted', '', status, can_persist=True)
    error = forwarding.get('error')
    if not isinstance(error, str) or not error:
        # Newer Agents: why ConfigFlow's files are not yet persisted at boot
        # (OpenRC's sysctl service in no runlevel) and what persisting will do.
        error = forwarding.get('notice')
    return _gateway_entry(agent_id, agent, routers, 'needs_persist', error if isinstance(error, str) else '',
                          status, can_persist=True)


def _gateway_precheck(agent_id, agent, routers):
    """The entry for an Agent that cannot be asked, or None when its status should be read."""
    if not agent:
        return _gateway_entry(agent_id, None, routers, 'missing', GATEWAY_FORWARDING_MESSAGES['missing'])
    if agent.get('service_type') != 'mihomo':
        return _gateway_entry(agent_id, agent, routers, 'not_mihomo', GATEWAY_FORWARDING_MESSAGES['not_mihomo'])
    if agent.get('deployment_method') == 'docker':
        return _gateway_entry(agent_id, agent, routers, 'docker', GATEWAY_FORWARDING_MESSAGES['docker'])
    if not is_online(agent):
        return _gateway_entry(agent_id, agent, routers, 'offline', GATEWAY_FORWARDING_MESSAGES['offline'])
    if not supports_forwarding(agent):
        # Below 1.8.40-go no Agent persists forwarding; offer the update directly.
        return _gateway_entry(agent_id, agent, routers, 'upgrade_required', GATEWAY_FORWARDING_MESSAGES['upgrade_required'])
    return None


def gateway_forwarding_entry(manager, agent_id, routers):
    """Per-Agent IPv4 forwarding view for a router's proxy gateway; never raises for one Agent."""
    agent = manager.get_agent_by_id(agent_id)
    entry = _gateway_precheck(agent_id, agent, routers)
    if entry is not None:
        return entry
    try:
        status = _request(agent, 'status', 'GET')
    except MonitorError as error:
        if error.code == 'UPGRADE_REQUIRED':
            return _gateway_entry(agent_id, agent, routers, 'upgrade_required',
                                  GATEWAY_FORWARDING_MESSAGES['upgrade_required'])
        return _gateway_entry(agent_id, agent, routers, 'error', error.message)
    return _entry_from_status(agent_id, agent, routers, status)


def persist_gateway_forwarding(manager, agent_id, routers):
    """Persist forwarding on a router's proxy gateway; the caller has checked the router link."""
    agent = manager.get_agent_by_id(agent_id)
    if not agent:
        raise _error('AGENT_NOT_FOUND', 404, GATEWAY_FORWARDING_MESSAGES['missing'])
    if agent.get('service_type') != 'mihomo':
        raise _error('WRONG_SERVICE', 400, GATEWAY_FORWARDING_MESSAGES['not_mihomo'])
    if agent.get('deployment_method') == 'docker':
        raise _error('UNSUPPORTED_ENVIRONMENT', 409, GATEWAY_FORWARDING_MESSAGES['docker'])
    if not is_online(agent):
        raise _error('AGENT_OFFLINE', 409, 'Agent 离线，无法开启转发。')
    if not supports_forwarding(agent):
        raise _error('UPGRADE_REQUIRED', 409, GATEWAY_FORWARDING_MESSAGES['upgrade_required'])
    _require_forwarding_layout(agent, lambda: _request(agent, 'status', 'GET'))
    data = _request(agent, 'forwarding', 'POST', {})
    data['gateway'] = _entry_from_status(agent_id, agent, routers, {
        'supported': True, 'ipv4_forwarding': data.get('ipv4_forwarding'), 'managed': data.get('managed')})
    return data
