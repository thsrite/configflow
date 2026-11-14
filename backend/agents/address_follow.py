"""Follow an Agent whose host was renumbered.

An Agent reports the address it should be reached at with every heartbeat. The
record follows it once ConfigFlow has reached that address with the Agent's own
token, so a wrong interface or an unreachable address never replaces a working
one. Members of a DNS high-availability pair are only reported: both devices'
VRRP configuration and the router's exemptions name the old address, and
changing the record alone would hide a broken pair.
"""
import ipaddress
import logging
import threading
import time

import requests

from .manager import AgentDeploymentConflict, _agent_label, _oxidns_ha_busy, _oxidns_ha_enabled

RETRY_SECONDS = 120
VERIFY_TIMEOUT = 4
HA_STEPS = ('它是 DNS 高可用成员，面板没有修改记录：两台设备的主备配置和路由器的放行名单里写的都是旧地址。'
            '请依次：1. 停用 DNS 高可用；2. 等这台设备的地址在这里自动更新（约两分钟内）；'
            '3. 重新配对并开启高可用；4. 重新预览并发布上游路由。')
HA_EDIT = ('这台设备是 DNS 高可用成员，不能在这里修改地址：两台设备的主备配置和路由器的放行名单里写的都是当前地址。'
           '请依次：1. 停用 DNS 高可用；2. 修改地址；3. 重新配对并开启高可用；4. 重新预览并发布上游路由。')
ENV_EDIT = '这个容器的地址由环境变量 AGENT_IP 指定，容器每次启动都会用它。请修改 AGENT_IP 后重建容器，面板记录会自动更新。'
ROUTER_HINT = '如果上游路由关联了这台设备（代理网关或 DHCP DNS），请重新预览并发布上游路由。'
_log = logging.getLogger(__name__)
_lock = threading.Lock()
_attempts = {}   # agent id -> (reported host, HA member, monotonic time of the last attempt)
_reported = set()   # agent ids with a pending "address changed" notice


def usable(value):
    """A LAN address ConfigFlow can connect to; a Docker bridge address is not one."""
    if not isinstance(value, str):   # IPv4Address(12) is 0.0.0.12
        return None
    try:
        address = ipaddress.IPv4Address(value)
    except ValueError:
        return None
    if (address.is_loopback or address.is_link_local or address.is_multicast or address.is_unspecified
            or address.is_reserved or address in ipaddress.IPv4Network('172.16.0.0/12')
            and 17 <= address.packed[1] <= 31):
        return None
    return str(address)


def follow(manager, agent, reported):
    """Called for every heartbeat; returns at once and verifies in the background."""
    host = usable(reported)
    agent_id = agent['id']
    if host is None or host == agent.get('host'):
        if host is not None and agent_id in _reported:
            _resolve(manager, agent_id)
        return
    member = _oxidns_ha_enabled(agent) or _oxidns_ha_busy(agent)
    now = time.monotonic()
    with _lock:
        previous = _attempts.get(agent_id)
        if previous and previous[:2] == (host, member) and now - previous[2] < RETRY_SECONDS:
            return
        _attempts[agent_id] = (host, member, now)
    threading.Thread(target=_follow, args=(manager, agent_id, host), name='agent-address', daemon=True).start()


def _reaches(agent, host):
    """Only the Agent holding this record's token answers this authenticated endpoint."""
    try:
        response = requests.get(f"http://{host}:{agent['port']}/api/capabilities",
                                headers={'Authorization': f"Bearer {agent['token']}"}, timeout=VERIFY_TIMEOUT)
        return response.status_code == 200
    except requests.exceptions.RequestException:
        return False


def _follow(manager, agent_id, host):
    try:
        agent = manager.get_agent_by_id(agent_id)
        if not agent or agent.get('host') == host:
            return
        old, label = agent.get('host') or '', _agent_label(agent)
        if _oxidns_ha_enabled(agent) or _oxidns_ha_busy(agent):
            _log.warning('Agent %s 上报地址由 %s 变为 %s；它是 DNS 高可用成员，记录未修改', label, old, host)
            _report(manager, agent, host, '是 DNS 高可用成员，记录未修改', HA_STEPS)
            return
        if any(other['id'] != agent_id and other.get('name') == agent.get('name') and other.get('host') == host
               for other in manager.get_all_agents()):
            _log.warning('Agent %s 上报地址 %s 与另一条同名记录相同，记录未修改', label, host)
            return
        if not _reaches(agent, host):
            _log.warning('Agent %s 上报地址由 %s 变为 %s，但面板用它的凭据连不上新地址，记录未修改', label, old, host)
            _report(manager, agent, host, '面板连不上新地址，记录未修改',
                    f'面板用这台 Agent 的凭据访问 {host}:{agent["port"]} 没有成功，所以仍按 {old} 联系它。'
                    '请确认面板所在的机器能访问新地址和 Agent 端口；能访问后会自动更新。')
            return
        try:
            if not manager.update_agent(agent_id, {'host': host}):
                return
        except AgentDeploymentConflict as conflict:
            # A publication or update is in flight; the next heartbeat tries again.
            _log.info('Agent %s 的地址暂未更新为 %s：%s', label, host, conflict)
            with _lock:
                _attempts.pop(agent_id, None)
            return
        _log.info('Agent %s 的地址已由 %s 更新为 %s（Agent 上报，已用其凭据核实可连接）', label, old, host)
        from backend import notifications
        _resolve(manager, agent_id)
        notifications.safely(
            notifications.record_notice, manager.repository, 'notice:agent-address:' + agent_id,
            f'{agent.get("name") or agent_id} · Agent 地址已自动更新',
            f'设备的地址由 {old} 变为 {host}，面板已核实能用新地址连上它并更新了记录。'
            '如果上游路由关联了这台设备（代理网关或 DHCP DNS），请重新预览并发布上游路由。',
            notifications.target('/agents', '查看 Agent', agent=agent_id))
    except Exception:
        _log.exception('Could not follow the reported address of Agent %s', agent_id)


def _report(manager, agent, host, label, message):
    from backend import notifications
    _reported.add(agent['id'])
    notifications.safely(notifications.record_agent_address, manager.repository, agent, host, label, message)


def _resolve(manager, agent_id):
    from backend import notifications
    _reported.discard(agent_id)
    notifications.safely(notifications.resolve_agent_address, manager.repository, agent_id)


def confirmed(agent, reported):
    """The reported address once the record holds it; the Agent may then store it in its config."""
    host = usable(reported)
    return host if host is not None and host == agent.get('host') else None


def _ask(agent, host, method='GET', body=None):
    """The Agent's own address endpoint: (HTTP status or None when unreachable, JSON body)."""
    try:
        response = requests.request(method, f"http://{host}:{agent['port']}/api/agent-address", json=body,
                                    headers={'Authorization': f"Bearer {agent['token']}"}, timeout=VERIFY_TIMEOUT)
    except requests.exceptions.RequestException:
        return None, {}
    try:
        data = response.json()
    except ValueError:
        data = {}
    return response.status_code, data if isinstance(data, dict) else {}


def _mode(agent, status, info):
    """What changing the address of this Agent changes: 'agent' (record and config file) or 'record'."""
    if status == 200 and info.get('writable') is True:
        return 'agent', ''
    if status == 200 and info.get('configured'):
        return 'env', ENV_EDIT
    if agent.get('deployment_method') == 'docker':
        return 'record', '容器没有指定 AGENT_IP，这里只修改面板记录的地址（填宿主机的局域网地址）。'
    if status == 404:
        return 'record', '这台 Agent 版本较旧，只修改面板记录的地址；升级 Agent 后可同时写入设备上的配置文件。'
    return 'record', ''


def describe(agent):
    """What the 修改地址 dialog shows before anything is changed."""
    result = {'host': agent.get('host') or '', 'port': agent.get('port'), 'mode': 'agent', 'message': '',
              'addresses': [], 'configured': '', 'reachable': False}
    if _oxidns_ha_enabled(agent) or _oxidns_ha_busy(agent):
        return {**result, 'mode': 'ha', 'message': HA_EDIT}
    status, info = _ask(agent, agent.get('host'))
    if status is None or status not in (200, 404):
        return {**result, 'mode': 'unknown',
                'message': f'面板用当前地址 {agent.get("host")}:{agent.get("port")} 连不上这台 Agent。填写它现在的地址，保存时会先核实能连上。'}
    mode, message = _mode(agent, status, info)
    addresses = [value for value in info.get('addresses') or [] if isinstance(value, str) and usable(value)]
    return {**result, 'mode': mode, 'message': message, 'reachable': True,
            'addresses': addresses if mode == 'agent' else [], 'configured': str(info.get('configured') or '')}


def change(manager, agent, value):
    """Set the address by hand: reach the Agent there first, store it on the device, then in the record."""
    host = usable(value.strip() if isinstance(value, str) else value)
    if host is None:
        return {'success': False, 'message': '请输入这台设备的局域网 IPv4 地址'}, 400
    agent_id, old, label = agent['id'], agent.get('host') or '', _agent_label(agent)
    if _oxidns_ha_enabled(agent) or _oxidns_ha_busy(agent):
        return {'success': False, 'message': HA_EDIT}, 409
    if any(other['id'] != agent_id and other.get('name') == agent.get('name') and other.get('host') == host
           for other in manager.get_all_agents()):
        return {'success': False, 'message': f'另一条同名的 Agent 记录已经在用 {host}'}, 409
    status, info = _ask(agent, host)
    if status == 404:
        # An Agent without the address endpoint: prove it is this Agent another way.
        status = 404 if _reaches(agent, host) else None
    if status is None:
        return {'success': False, 'message': f'面板连不上 {host}:{agent.get("port")}，没有修改。请确认地址正确，且面板所在的机器能访问它。'}, 502
    if status not in (200, 404):
        return {'success': False, 'message': f'{host}:{agent.get("port")} 有应答，但不是这台 Agent（凭据不匹配），没有修改。'}, 502
    mode, message = _mode(agent, status, info)
    if mode == 'env':
        return {'success': False, 'message': message}, 409
    written = False
    if mode == 'agent':
        status, reply = _ask(agent, host, 'POST', {'host': host})
        if status != 200:
            return {'success': False,
                    'message': reply.get('message') or f'设备没有接受这个地址（{status or "无应答"}），没有修改。'}, 400 if status == 400 else 502
        written = True
    if host != old:
        try:
            if not manager.update_agent(agent_id, {'host': host}):
                return {'success': False, 'message': 'Agent not found'}, 404
        except AgentDeploymentConflict as conflict:
            return {'success': False, 'message': str(conflict)}, 409
    with _lock:
        _attempts.pop(agent_id, None)
    _resolve(manager, agent_id)
    _log.info('Agent %s 的地址已由 %s 改为 %s（手动修改，已核实可连接%s）', label, old, host, '，已写入设备配置' if written else '')
    return {'success': True, 'host': host, 'written': written, 'changed': host != old,
            'message': ('地址已改为 ' + host + ('，并写入了设备上的 Agent 配置。' if written else '（只修改了面板记录）。')
                        + (ROUTER_HINT if host != old else ''))}, 200
