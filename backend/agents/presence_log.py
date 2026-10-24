"""Log Agent online/offline transitions.

Offline is derived from the heartbeat age when Agents are listed, never stored,
so transitions are detected by comparing successive scans in this process.
Heartbeats are only kept in memory, so after a restart every Agent reads as
offline until it next reports. An Agent is therefore followed only from the
first scan that sees it online: a restart neither reports every Agent as back
online nor guesses at Agents it has not heard from since.
"""
import logging
import threading

from backend.utils.logger import log_text

logger = logging.getLogger(__name__)
_lock = threading.Lock()
_known = {}


def observe(agents):
    with _lock:
        current = {}
        for agent in agents:
            status = 'online' if agent.get('status') == 'online' else 'offline'
            if status == 'online' or agent['id'] in _known:
                current[agent['id']] = (status, agent.get('name') or agent['id'], agent.get('last_heartbeat') or '')
        for agent_id, (status, name, heartbeat) in current.items():
            previous = _known.get(agent_id)
            if previous is None or previous[0] == status:
                continue
            if status == 'online':
                logger.info('Agent %s 已恢复在线', log_text(name))
            else:
                logger.warning('Agent %s 已离线：超过 2 分钟没有心跳（最后心跳 %s）', log_text(name), log_text(heartbeat, 32))
        _known.clear()
        _known.update(current)
