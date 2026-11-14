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
_log = logging.getLogger(__name__)
_lock = threading.Lock()
_attempts = {}   # agent id -> (reported host, HA member, monotonic time of the last attempt)
_reported = set()   # agent ids with a pending "address changed" notice


def usable(value):
    """A LAN address ConfigFlow can connect to; a Docker bridge address is not one."""
    try:
        address = ipaddress.IPv4Address(value)
    except (ValueError, TypeError):
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
