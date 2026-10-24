"""Agent heartbeat state kept in process memory.

A heartbeat's liveness, service status and metrics are only worth showing
while they are fresh, so they live here and are laid over the stored Agent on
read; a restart forgets them and each Agent reappears with its next heartbeat.
The reported version is the exception: the manager writes it into the
configuration document whenever it changes, because update checks and
version gates must not fall back to the version an Agent registered with.

Whichever side holds the newer heartbeat wins: registration writes the
document, so a re-registered Agent shows its new version until its next
heartbeat. One process serves every request, and all AgentManager instances
for the same data directory share one state, like they share the document.
"""
from copy import deepcopy
from datetime import datetime
import json
import logging
from pathlib import Path
import sqlite3
import threading
import time

logger = logging.getLogger(__name__)

RUNTIME_FIELDS = ('version', 'service_status', 'system_metrics', 'cpu', 'memory', 'disk', 'network')
_METRIC_FIELDS = ('cpu', 'memory', 'disk', 'network')
# Written by #369, which kept this state on disk; read once, then removed.
LEGACY_DATABASE = 'runtime.sqlite3'

_states = {}
_legacy_checked = set()
_states_lock = threading.Lock()


def _timestamp(value):
    try:
        moment = datetime.fromisoformat(value).timestamp()
    except (TypeError, ValueError):
        return float('-inf')
    # A registration time ahead of the clock (time zone changed, clock stepped
    # back) must not outrank every later heartbeat.
    return moment if moment <= time.time() else float('-inf')


class AgentRuntimeState:
    def __init__(self):
        self._lock = threading.Lock()
        # agent_id -> (heartbeat_at, last_heartbeat, fields)
        self._entries = {}
        # Bumped by each delete, so a heartbeat that looked the Agent up
        # before its deletion cannot put the entry back afterwards.
        self._deletions = 0
        self._deleted = {}

    def _newer(self, agent):
        entry = self._entries.get(agent.get('id'))
        return entry if entry and entry[0] > _timestamp(agent.get('last_heartbeat')) else None

    def overlay(self, agents):
        """Lay newer heartbeat state over the given (copied) Agents in place."""
        with self._lock:
            for agent in agents:
                entry = self._newer(agent)
                if entry:
                    agent.update(deepcopy(entry[2]))
                    agent['last_heartbeat'] = entry[1]
                    agent['status'] = 'online'
        return agents

    def mark(self):
        """Take before looking the Agent up; pass to record_heartbeat."""
        with self._lock:
            return self._deletions

    def record_heartbeat(self, agent, payload, mark):
        """Merge one heartbeat into the newer of the remembered and registered state.

        Returns False, recording nothing, when the Agent was deleted after mark.
        """
        now = time.time()
        with self._lock:
            if self._deleted.get(agent['id'], 0) > mark:
                return False
            entry = self._newer(agent)
            fields = deepcopy(entry[2]) if entry else {key: deepcopy(agent[key]) for key in RUNTIME_FIELDS if key in agent}
            # Fields a heartbeat leaves out keep their last value.
            for key in ('version', 'service_status'):
                if key in payload:
                    fields[key] = payload[key]
            if 'system_metrics' in payload:
                metrics = deepcopy(payload['system_metrics'])
                fields['system_metrics'] = metrics
                fields.update({key: metrics[key] for key in _METRIC_FIELDS if key in metrics})
            self._entries[agent['id']] = (now, datetime.fromtimestamp(now).isoformat(), fields)
            return True

    def seed(self, agent_id, heartbeat_at, last_heartbeat, fields):
        """Restore a heartbeat remembered elsewhere unless a newer one arrived."""
        with self._lock:
            entry = self._entries.get(agent_id)
            if entry is None or entry[0] < heartbeat_at:
                self._entries[agent_id] = (heartbeat_at, last_heartbeat, deepcopy(fields))

    def delete(self, agent_id):
        with self._lock:
            self._entries.pop(agent_id, None)
            self._deletions += 1
            self._deleted[agent_id] = self._deletions


def for_data_dir(data_dir):
    key = str(Path(data_dir).resolve())
    with _states_lock:
        return _states.setdefault(key, AgentRuntimeState())


def reset():
    """Forget every Agent's heartbeat state, as a process restart does (tests)."""
    with _states_lock:
        _states.clear()
        _legacy_checked.clear()


def _legacy_rows(path):
    try:
        connection = sqlite3.connect(path, timeout=5)
        try:
            rows = connection.execute(
                'SELECT agent_id, heartbeat_at, last_heartbeat, fields FROM agent_runtime').fetchall()
        finally:
            connection.close()
    except sqlite3.Error as error:
        logger.warning('旧版 Agent 心跳状态库无法读取，已忽略：%s', error)
        return {}
    result = {}
    for agent_id, heartbeat_at, last_heartbeat, raw in rows:
        try:
            fields = json.loads(raw)
        except (TypeError, ValueError):
            continue
        if (isinstance(agent_id, str) and isinstance(heartbeat_at, (int, float))
                and isinstance(last_heartbeat, str) and isinstance(fields, dict)):
            result[agent_id] = (float(heartbeat_at), last_heartbeat, fields)
    return result


def _legacy_files(path):
    names = [path.name + suffix for suffix in ('', '-journal', '-wal', '-shm')]
    return [path.with_name(name) for name in names] + sorted(path.parent.glob(path.name + '.corrupt-*'))


def _remove(paths):
    for item in paths:
        try:
            item.unlink()
        except FileNotFoundError:
            pass
        except OSError as error:
            logger.warning('旧版 Agent 心跳状态文件 %s 删除失败：%s', item.name, error)


def import_legacy_database(repository, state, persist_versions):
    """Carry #369's on-disk heartbeat state into memory once, then remove it.

    Agents newer there than in the document are seeded so they do not flash
    offline after the upgrade, and a version that only reached that database
    is handed to persist_versions({agent_id: {'version': version}}).

    The file is read at most once: later heartbeats persist newer versions,
    which a second read of the old rows would overwrite, and the document's
    last_heartbeat no longer moves to tell them apart. So nothing is applied
    unless the file could be removed, and a failed version write is left to
    the Agent's next heartbeat, which reports and writes it again.
    """
    path = Path(repository.data_dir) / LEGACY_DATABASE
    key = str(path.resolve())
    with _states_lock:
        if key in _legacy_checked:
            return
        _legacy_checked.add(key)
    if not any(item.exists() for item in _legacy_files(path)):
        return
    rows = _legacy_rows(path) if path.is_file() else {}
    # Listed after reading, which may leave a journal behind.
    _remove(_legacy_files(path))
    if path.exists():
        logger.warning('旧版 %s 无法删除，未导入其中的 Agent 心跳状态', LEGACY_DATABASE)
        return
    agents = {agent['id']: agent for agent in repository.get_system().get('agents', [])}
    current = {agent_id: row for agent_id, row in rows.items()
               if agent_id in agents and row[0] > _timestamp(agents[agent_id].get('last_heartbeat'))}
    versions = {agent_id: row[2]['version'] for agent_id, row in current.items()
                if isinstance(row[2].get('version'), str) and row[2]['version'] != agents[agent_id].get('version')}
    for agent_id, row in current.items():
        state.seed(agent_id, *row)
    if versions:
        try:
            persist_versions({agent_id: {'version': version} for agent_id, version in versions.items()})
        except Exception as error:  # noqa: BLE001 - the next heartbeat writes it
            logger.warning('旧版 Agent 心跳状态里的版本号写入配置失败，将在 Agent 下次心跳时写入：%s', error)
            versions = {}
    logger.info('Agent 心跳状态改为只保存在内存：已从旧版 %s 恢复 %d 个 Agent 的状态（%d 个版本号写入配置），并删除该文件',
                LEGACY_DATABASE, len(current), len(versions))
