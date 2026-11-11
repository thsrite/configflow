"""OxiDNS 请求排行的完整统计：按时段从 Agent 取回，按小时保存，面板按范围相加。

核心每次排行最多统计范围内最新的 10,000 条查询记录（OxiDNS 1.6.1 写死，不能调大），
查询量大时排行只覆盖最近几十分钟。这里每 15 分钟向 Agent 取刚结束的一个时段：
时段内的记录少于上限，核心统计的就是全部记录。每个时段的客户端、域名、被拦截域名
各保存最多 ROWS 项，按小时累加写入数据目录下的 metrics/oxidns_rankings.sqlite3。

- 一个时段的记录达到上限时对半拆开再取，直到 MIN_SLOT 秒；仍达到上限的记为不完整。
- 超过 HOURLY_DAYS 天的小时数据合并为按天（UTC）一行，每类只留 DAILY_TOP 项；
  超过 RETENTION_DAYS 天的删除。
- 首次启用或停机后，补齐核心仍保留的时段（核心保留期，最多 RETENTION_DAYS 天），
  新的时段优先；每轮最多取 SLOTS_PER_ROUND 个，不集中占用核心。
- 只取 HISTORY_AGENT_VERSION 及以上的 Agent：更早的排行接口每类只返回前 10 项。
"""
import os
import sqlite3
import threading
import time

from backend.utils.logger import get_logger

logger = get_logger(__name__)

FILENAME = 'oxidns_rankings.sqlite3'
HISTORY_AGENT_VERSION = '1.8.55-go'

SLOT = 900
MIN_SLOT = 60
HOUR = 3600
DAY = 86400
# 时段结束后再等一会儿，等核心写完这段时间的记录
SETTLE_SECONDS = 60
ROWS = 2000
DAILY_TOP = 2000
HOURLY_DAYS = 7
RETENTION_DAYS = 90
CORE_RETENTION_DAYS = 7
SLOTS_PER_ROUND = 24
ROUND_SECONDS = 60
KINDS = ('clients', 'domains', 'blocked')
RANGES = {'24h': DAY, '7d': 7 * DAY, '30d': 30 * DAY}
TOP = 10

_SCHEMA = (
    # 一行一个小时（seconds=3600）或一天（86400）；slots 是其中已取回的 15 分钟时段数
    'CREATE TABLE IF NOT EXISTS ranking_periods ('
    ' agent_id TEXT NOT NULL, recorder TEXT NOT NULL, start INTEGER NOT NULL, seconds INTEGER NOT NULL,'
    ' records INTEGER NOT NULL DEFAULT 0, blocked_records INTEGER NOT NULL DEFAULT 0,'
    ' blocked_slots INTEGER NOT NULL DEFAULT 0, slots INTEGER NOT NULL DEFAULT 0,'
    ' partial_slots INTEGER NOT NULL DEFAULT 0,'
    ' PRIMARY KEY (agent_id, recorder, start, seconds))',
    'CREATE TABLE IF NOT EXISTS ranking_rows ('
    ' agent_id TEXT NOT NULL, recorder TEXT NOT NULL, start INTEGER NOT NULL, seconds INTEGER NOT NULL,'
    ' kind TEXT NOT NULL, key TEXT NOT NULL, count INTEGER NOT NULL,'
    ' PRIMARY KEY (agent_id, recorder, start, seconds, kind, key))',
    # 已取回的 15 分钟时段，避免重复累加
    'CREATE TABLE IF NOT EXISTS ranking_slots ('
    ' agent_id TEXT NOT NULL, recorder TEXT NOT NULL, start INTEGER NOT NULL,'
    ' PRIMARY KEY (agent_id, recorder, start))',
)


def supported(agent):
    from backend.agents.version import compare_versions
    return compare_versions((agent or {}).get('version') or '0', HISTORY_AGENT_VERSION) >= 0


class SlotIncomplete(Exception):
    """A slot could not be read in full; it is asked for again in a later round."""


class RankingHistory:
    def __init__(self, data_dir):
        self.path = os.path.join(str(data_dir), 'metrics', FILENAME)
        self._lock = threading.Lock()

    def _connect(self):
        os.makedirs(os.path.dirname(self.path), exist_ok=True)
        connection = sqlite3.connect(self.path, timeout=30)
        connection.execute('PRAGMA journal_mode=WAL')
        for statement in _SCHEMA:
            connection.execute(statement)
        return connection

    # --- writing -----------------------------------------------------------

    def fetched(self, agent_id, recorder, starts):
        """The slot starts among `starts` already stored."""
        starts = list(starts)
        if not starts or not os.path.exists(self.path):
            return set()
        with self._lock, self._connect() as connection:
            rows = connection.execute(
                'SELECT start FROM ranking_slots WHERE agent_id = ? AND recorder = ? AND start BETWEEN ? AND ?',
                (agent_id, recorder, min(starts), max(starts)))
            return {start for (start,) in rows} & set(starts)

    def add_slot(self, agent_id, recorder, start, slot):
        """Add one 15-minute slot to its hour. slot: records, blocked (None if unknown),
        partial, and {kind: {key: count}}."""
        hour = start - start % HOUR
        with self._lock, self._connect() as connection:
            inserted = connection.execute(
                'INSERT OR IGNORE INTO ranking_slots (agent_id, recorder, start) VALUES (?, ?, ?)',
                (agent_id, recorder, start)).rowcount
            if not inserted:
                return False
            connection.execute(
                'INSERT INTO ranking_periods (agent_id, recorder, start, seconds, records, blocked_records,'
                ' blocked_slots, slots, partial_slots) VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?)'
                ' ON CONFLICT (agent_id, recorder, start, seconds) DO UPDATE SET'
                ' records = records + excluded.records, blocked_records = blocked_records + excluded.blocked_records,'
                ' blocked_slots = blocked_slots + excluded.blocked_slots, slots = slots + 1,'
                ' partial_slots = partial_slots + excluded.partial_slots',
                (agent_id, recorder, hour, HOUR, slot['records'], slot['blocked'] or 0,
                 int(slot['blocked'] is not None), int(slot['partial'])))
            connection.executemany(
                'INSERT INTO ranking_rows (agent_id, recorder, start, seconds, kind, key, count) VALUES (?, ?, ?, ?, ?, ?, ?)'
                ' ON CONFLICT (agent_id, recorder, start, seconds, kind, key) DO UPDATE SET count = count + excluded.count',
                [(agent_id, recorder, hour, HOUR, kind, key, count)
                 for kind, rows in slot['rows'].items() for key, count in rows.items()])
            return True

    def maintain(self, agent_ids, now=None):
        """Merge old hours into days, drop expired data and removed Agents."""
        if not os.path.exists(self.path):
            return
        now = int(now if now is not None else time.time())
        hourly_before = now - now % DAY - HOURLY_DAYS * DAY
        cutoff = now - RETENTION_DAYS * DAY
        with self._lock, self._connect() as connection:
            known = set(agent_ids)
            for (agent_id,) in connection.execute('SELECT DISTINCT agent_id FROM ranking_slots').fetchall():
                if agent_id not in known:
                    for table in ('ranking_periods', 'ranking_rows', 'ranking_slots'):
                        connection.execute(f'DELETE FROM {table} WHERE agent_id = ?', (agent_id,))
            old = connection.execute(
                'SELECT DISTINCT agent_id, recorder, start - start % ? FROM ranking_periods WHERE seconds = ? AND start < ?',
                (DAY, HOUR, hourly_before)).fetchall()
            for agent_id, recorder, day in old:
                self._merge_day(connection, agent_id, recorder, day)
            for table in ('ranking_periods', 'ranking_rows'):
                connection.execute(f'DELETE FROM {table} WHERE start < ?', (cutoff,))
            connection.execute('DELETE FROM ranking_slots WHERE start < ?', (cutoff,))

    @staticmethod
    def _merge_day(connection, agent_id, recorder, day):
        hours = (agent_id, recorder, HOUR, day, day + DAY)
        totals = connection.execute(
            'SELECT SUM(records), SUM(blocked_records), SUM(blocked_slots), SUM(slots), SUM(partial_slots)'
            ' FROM ranking_periods WHERE agent_id = ? AND recorder = ? AND seconds = ? AND start >= ? AND start < ?',
            hours).fetchone()
        connection.execute(
            'INSERT INTO ranking_periods (agent_id, recorder, start, seconds, records, blocked_records, blocked_slots,'
            ' slots, partial_slots) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)'
            ' ON CONFLICT (agent_id, recorder, start, seconds) DO UPDATE SET'
            ' records = records + excluded.records, blocked_records = blocked_records + excluded.blocked_records,'
            ' blocked_slots = blocked_slots + excluded.blocked_slots, slots = slots + excluded.slots,'
            ' partial_slots = partial_slots + excluded.partial_slots',
            (agent_id, recorder, day, DAY, *[value or 0 for value in totals]))
        for kind in KINDS:
            rows = connection.execute(
                'SELECT key, SUM(count) AS total FROM ranking_rows WHERE agent_id = ? AND recorder = ? AND seconds = ?'
                ' AND start >= ? AND start < ? AND kind = ? GROUP BY key',
                (*hours, kind)).fetchall()
            existing = dict(connection.execute(
                'SELECT key, count FROM ranking_rows WHERE agent_id = ? AND recorder = ? AND start = ? AND seconds = ?'
                ' AND kind = ?', (agent_id, recorder, day, DAY, kind)).fetchall())
            for key, total in rows:
                existing[key] = existing.get(key, 0) + total
            kept = sorted(existing.items(), key=lambda item: (-item[1], item[0]))[:DAILY_TOP]
            connection.execute('DELETE FROM ranking_rows WHERE agent_id = ? AND recorder = ? AND start = ?'
                               ' AND seconds = ? AND kind = ?', (agent_id, recorder, day, DAY, kind))
            connection.executemany(
                'INSERT INTO ranking_rows (agent_id, recorder, start, seconds, kind, key, count) VALUES (?, ?, ?, ?, ?, ?, ?)',
                [(agent_id, recorder, day, DAY, kind, key, count) for key, count in kept])
        for table in ('ranking_periods', 'ranking_rows'):
            connection.execute(f'DELETE FROM {table} WHERE agent_id = ? AND recorder = ? AND seconds = ?'
                               ' AND start >= ? AND start < ?', hours)

    # --- reading -----------------------------------------------------------

    def query(self, agent_id, recorder, since, until):
        """Totals and the top entries of the stored periods starting in [since, until)."""
        empty = {'records': 0, 'blocked_records': 0, 'blocked_slots': 0, 'slots': 0, 'partial_slots': 0,
                 'first': None, 'last': None, 'rows': {kind: [] for kind in KINDS}}
        if not os.path.exists(self.path):
            return empty
        where = 'agent_id = ? AND recorder = ? AND start >= ? AND start + seconds <= ?'
        args = (agent_id, recorder, since, until)
        with self._lock, self._connect() as connection:
            totals = connection.execute(
                'SELECT SUM(records), SUM(blocked_records), SUM(blocked_slots), SUM(slots), SUM(partial_slots),'
                f' MIN(start), MAX(start + seconds) FROM ranking_periods WHERE {where} AND slots > 0', args).fetchone()
            if not totals[3]:
                return empty
            last_slot = connection.execute(
                'SELECT MAX(start) FROM ranking_slots WHERE agent_id = ? AND recorder = ? AND start >= ? AND start < ?',
                args).fetchone()[0]
            first_slot = connection.execute(
                'SELECT MIN(start) FROM ranking_slots WHERE agent_id = ? AND recorder = ? AND start >= ? AND start < ?',
                args).fetchone()[0]
            rows = {}
            for kind in KINDS:
                rows[kind] = [{'key': key, 'count': count} for key, count in connection.execute(
                    f'SELECT key, SUM(count) AS total FROM ranking_rows WHERE {where} AND kind = ?'
                    ' GROUP BY key ORDER BY total DESC, key ASC LIMIT ?', (*args, kind, TOP))]
        return {'records': totals[0] or 0, 'blocked_records': totals[1] or 0, 'blocked_slots': totals[2] or 0,
                'slots': totals[3] or 0, 'partial_slots': totals[4] or 0,
                # Hours and days hold the slots fetched; the newest is known by its own record.
                'first': first_slot if first_slot is not None else totals[5],
                'last': (last_slot + SLOT) if last_slot is not None else totals[6], 'rows': rows}


# --- collecting ----------------------------------------------------------------

def _slot_ranking(agent, recorder, since, until):
    """One window's complete counts from the Agent, split while it holds too many records."""
    from backend.agents import oxidns_monitor as monitor
    data = monitor._request(agent, 'rankings', {'recorder': recorder, 'since_ms': str(since * 1000),
                                                 'until_ms': str(until * 1000), 'rows': str(ROWS)},
                            min_version=HISTORY_AGENT_VERSION)
    if data.get('success') is not True or data.get('recorder') != recorder:
        raise SlotIncomplete('unexpected answer')
    sections = {kind: monitor._ranking_section(data.get(kind), kind, rows=ROWS) for kind in ('clients', 'domains')}
    blocked = data.get('blocked')
    if not isinstance(blocked, dict) or type(blocked.get('available')) is not bool:
        raise SlotIncomplete('unexpected answer')
    if blocked['available']:
        sections['blocked'] = monitor._ranking_section(blocked, 'domains', rows=ROWS)
    if any(section['at_limit'] for section in sections.values()) and until - since > MIN_SLOT:
        middle = since + (until - since) // 2
        middle -= middle % MIN_SLOT
        return _merge(_slot_ranking(agent, recorder, since, middle), _slot_ranking(agent, recorder, middle, until))
    return {
        'records': sections['clients']['sample_size'],
        'blocked': sections['blocked']['sample_size'] if 'blocked' in sections else None,
        'partial': any(section['at_limit'] for section in sections.values()),
        'rows': {kind: {row['key']: row['count'] for row in section['rows']} for kind, section in sections.items()},
        'retention_days': data.get('retention_days'),
    }


def _merge(first, second):
    rows = {}
    for kind in KINDS:
        merged = dict(first['rows'].get(kind, {}))
        for key, count in second['rows'].get(kind, {}).items():
            merged[key] = merged.get(key, 0) + count
        if kind in first['rows'] or kind in second['rows']:
            rows[kind] = merged
    blocked = None if first['blocked'] is None or second['blocked'] is None else first['blocked'] + second['blocked']
    return {'records': first['records'] + second['records'], 'blocked': blocked,
            'partial': first['partial'] or second['partial'], 'rows': rows,
            'retention_days': first.get('retention_days')}


def last_complete_slot_end(now):
    now = int(now)
    return (now - SETTLE_SECONDS) - (now - SETTLE_SECONDS) % SLOT


def collect_agent(history, agent, recorders, now=None, budget=SLOTS_PER_ROUND, retention_days=CORE_RETENTION_DAYS,
                  pause=0.2):
    """Fetch up to `budget` missing slots, newest first. Returns the number stored."""
    from backend.agents.oxidns_monitor import MonitorError
    end = last_complete_slot_end(now if now is not None else time.time())
    days = min(RETENTION_DAYS, max(1, retention_days or CORE_RETENTION_DAYS))
    starts = list(range(end - SLOT, end - days * DAY - 1, -SLOT))
    stored = 0
    for recorder in recorders:
        done = history.fetched(agent['id'], recorder, starts)
        for start in (start for start in starts if start not in done):
            if budget <= 0:
                return stored
            budget -= 1
            try:
                slot = _slot_ranking(agent, recorder, start, start + SLOT)
            except (MonitorError, SlotIncomplete) as error:
                logger.info('OxiDNS 排行统计暂未取到 %s 的时段（%s），下一轮再试', agent.get('name') or agent['id'],
                            getattr(error, 'code', None) or error)
                return stored  # the device is busy or away: try again next round
            stored += history.add_slot(agent['id'], recorder, start, slot)
            if pause:
                time.sleep(pause)
    return stored


def _recorders(agent):
    from backend.agents import oxidns_monitor as monitor
    _, _, recorders, _, _ = monitor._overview_snapshot(agent, True)
    return recorders


def collect_round(manager, history=None, now=None):
    from backend.agents.manager import is_usable
    from backend.agents.oxidns_monitor import MonitorError
    history = history or get_history(manager.repository.data_dir)
    agents = manager.get_all_agents(history=False)
    for agent in agents:
        if agent.get('service_type') != 'oxidns' or not supported(agent) or not is_usable(agent):
            continue
        try:
            recorders = _recorders(agent)
        except MonitorError:
            continue
        collect_agent(history, agent, recorders, now=now)
    history.maintain([agent['id'] for agent in agents], now=now)


_histories = {}
_histories_lock = threading.Lock()


def get_history(data_dir):
    key = str(data_dir)
    with _histories_lock:
        if key not in _histories:
            _histories[key] = RankingHistory(key)
        return _histories[key]


_started = False
_start_guard = threading.Lock()


def start_loop():
    """One collector across processes, guarded by a file lock in the data directory."""
    global _started
    with _start_guard:
        if _started:
            return
        _started = True

    def loop():
        import fcntl
        from backend.common.agent_manager import get_agent_manager
        from backend.common.config import get_repository, reset_config_context
        lock_path = os.path.join(str(get_repository().data_dir), '.oxidns-rankings.lock')
        handle = open(lock_path, 'a')
        try:
            fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except OSError:
            handle.close()
            return
        while True:
            time.sleep(ROUND_SECONDS)
            try:
                collect_round(get_agent_manager())
            except Exception:
                logger.exception('OxiDNS ranking collection round failed')
            finally:
                reset_config_context()

    threading.Thread(target=loop, name='oxidns-ranking-history', daemon=True).start()
