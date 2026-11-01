"""Mihomo 流量记录：按小时保存各设备经过 Mihomo 的流量。

Agent 每 5 分钟上报一次增量，这里先在内存里累加，由系统设置 → 定时任务「监控历史保存」
按同一间隔写入数据目录下的 metrics/traffic_history.sqlite3，服务停止时再保存一次，
不随每次上报写盘。查询时把还没保存的部分一起算进去。

两张表：
- usage：设备 × 策略组 × 出口节点。来源为空的一行是「未归属」（采样间隔内开始又结束的短连接）。
- targets：设备 × 目标（主域名或 IP）× 出口节点。目标为 ``*`` 的是「其他」（超出上限的目标），
  仍按出口节点分开，直连与代理的合计不受影响。

超过 USAGE_HOURLY_DAYS / TARGET_HOURLY_DAYS 天的小时数据合并为按天（UTC）一行，
目标按天只保留每台设备流量最大的 TARGET_DAILY_TOP 个；超过保留天数的数据删除。
出口节点是直连、代理还是拦截由前端按 Mihomo 当前的节点类型判断，这里只按名称保存。
"""
from datetime import datetime, timezone
import ipaddress
import os
import sqlite3
import threading
import time
from typing import Any, Dict, Iterable, List, Optional, Tuple

from backend.utils.logger import get_logger

logger = get_logger(__name__)

FILENAME = 'traffic_history.sqlite3'
REPORT_SCHEMA = 1
REPORT_MAX_CONTENT_LENGTH = 4 * 1024 * 1024
MIN_AGENT_VERSION = '1.8.51-go'

RETENTION_SETTING = 'traffic_history_retention_days'
RETENTION_CHOICES = (30, 90, 180, 365)
DEFAULT_RETENTION = 90

HOUR = 3600
DAY = 86400
USAGE_HOURLY_DAYS = 7
TARGET_HOURLY_DAYS = 2
TARGET_DAILY_TOP = 100
# 单次上报的条数上限，与 Agent 内存上限一致
REPORT_MAX_ITEMS = 20000

UNATTRIBUTED = ''
OTHER_TARGET = '*'
RANGES = {'24h': DAY, '7d': 7 * DAY, '30d': 30 * DAY}
AGGREGATES = ('source', 'destination', 'outbound', 'group')
QUERY_ROW_LIMIT = 500
# 每行展开后最多列出的明细
QUERY_PART_LIMIT = 50

SCHEMA = (
    'CREATE TABLE IF NOT EXISTS usage ('
    ' agent_id TEXT NOT NULL, bucket INTEGER NOT NULL, span INTEGER NOT NULL,'
    ' source TEXT NOT NULL, policy_group TEXT NOT NULL, outbound TEXT NOT NULL,'
    ' up INTEGER NOT NULL, down INTEGER NOT NULL, conns INTEGER NOT NULL,'
    ' PRIMARY KEY (agent_id, bucket, span, source, policy_group, outbound)) WITHOUT ROWID',
    'CREATE TABLE IF NOT EXISTS targets ('
    ' agent_id TEXT NOT NULL, bucket INTEGER NOT NULL, span INTEGER NOT NULL,'
    ' source TEXT NOT NULL, target TEXT NOT NULL, outbound TEXT NOT NULL,'
    ' up INTEGER NOT NULL, down INTEGER NOT NULL, conns INTEGER NOT NULL,'
    ' PRIMARY KEY (agent_id, bucket, span, source, target, outbound)) WITHOUT ROWID',
)
USAGE_UPSERT = ('INSERT INTO usage VALUES (?, ?, {span}, ?, ?, ?, ?, ?, ?) '
                'ON CONFLICT (agent_id, bucket, span, source, policy_group, outbound) DO UPDATE '
                'SET up = up + excluded.up, down = down + excluded.down, conns = conns + excluded.conns')
TARGET_UPSERT = ('INSERT INTO targets VALUES (?, ?, {span}, ?, ?, ?, ?, ?, ?) '
                 'ON CONFLICT (agent_id, bucket, span, source, target, outbound) DO UPDATE '
                 'SET up = up + excluded.up, down = down + excluded.down, conns = conns + excluded.conns')

_MAX_TEXT = {'source': 64, 'group': 128, 'outbound': 128, 'domain': 253}


def retention_days(system=None) -> int:
    if system is None:
        from backend.common.config import get_repository
        system = get_repository().get_system()
    value = (system.get('system_config') or {}).get(RETENTION_SETTING, DEFAULT_RETENTION)
    return value if type(value) is int and value in RETENTION_CHOICES else DEFAULT_RETENTION


def set_retention_days(days) -> int:
    if type(days) is not int or days not in RETENTION_CHOICES:
        raise ValueError('保留时长只能是 30、90、180 或 365 天')
    from backend.common.config import get_repository
    get_repository().update_system_transaction(
        lambda system: system.setdefault('system_config', {}).update({RETENTION_SETTING: days}))
    return days


def agent_supported(version) -> bool:
    from backend.agents.version import compare_versions
    return isinstance(version, str) and version.endswith('-go') and compare_versions(version, MIN_AGENT_VERSION) >= 0


def _target_key(value: str) -> str:
    """目标归并到主域名；IP 原样保留，Agent 并入「其他」的空目标记为 ``*``。"""
    if not value:
        return OTHER_TARGET
    try:
        return str(ipaddress.ip_address(value))
    except ValueError:
        pass
    from backend.utils.domain_discovery import registrable_domain
    return registrable_domain(value.lower().rstrip('.'))


def _count(value) -> Optional[int]:
    return value if type(value) is int and 0 <= value < 2 ** 62 else None


def _text(item, field, limit) -> Optional[str]:
    value = item.get(field)
    return value if isinstance(value, str) and len(value) <= limit else None


def clean_report(report: Any, now: Optional[float] = None) -> Optional[Dict[str, List[tuple]]]:
    """校验 Agent 的上报，返回 {'usage': [...], 'targets': [...]}；格式不对返回 None。

    小时须对齐整点，且不早于 2 天前、不晚于下一个小时，Agent 积压太久的数据不再收。
    """
    if not isinstance(report, dict) or report.get('schema') != REPORT_SCHEMA:
        return None
    usage, domains = report.get('usage'), report.get('domains')
    if not isinstance(usage, list) or not isinstance(domains, list) or len(usage) + len(domains) > REPORT_MAX_ITEMS:
        return None
    now = time.time() if now is None else now
    oldest, newest = now - 2 * DAY, now + HOUR

    def counters(item):
        values = tuple(_count(item.get(field)) for field in ('up', 'down', 'conns'))
        return None if None in values else values

    def hour(item):
        value = item.get('hour')
        if type(value) is not int or value % HOUR:
            return None, False
        # 积压太久或时间不对的整点数据直接丢弃，不算格式错误
        return value, oldest <= value <= newest

    result = {'usage': [], 'targets': []}
    for item in usage:
        if not isinstance(item, dict):
            return None
        bucket, fresh = hour(item)
        fields = (bucket, _text(item, 'source', _MAX_TEXT['source']), _text(item, 'group', _MAX_TEXT['group']),
                  _text(item, 'outbound', _MAX_TEXT['outbound']), counters(item))
        if None in fields:
            return None
        if not fresh:
            continue
        bucket, source, group, outbound, values = fields
        if source == UNATTRIBUTED:
            group = outbound = ''
        result['usage'].append((bucket, source, group, outbound) + values)
    for item in domains:
        if not isinstance(item, dict):
            return None
        bucket, fresh = hour(item)
        fields = (bucket, _text(item, 'source', _MAX_TEXT['source']), _text(item, 'domain', _MAX_TEXT['domain']),
                  _text(item, 'outbound', _MAX_TEXT['outbound']), counters(item))
        if None in fields or fields[1] == UNATTRIBUTED:
            return None
        if not fresh:
            continue
        bucket, source, domain, outbound, values = fields
        result['targets'].append((bucket, source, _target_key(domain), outbound) + values)
    return result


def _add(table: Dict[Any, List[int]], key: Any, values: Iterable[int]):
    current = table.get(key)
    if current is None:
        table[key] = list(values)
    else:
        for index, value in enumerate(values):
            current[index] += value


def _rows(table: Dict[str, List[int]], limit: int) -> List[Dict[str, Any]]:
    rows = sorted(({'key': key, 'up': v[0], 'down': v[1], 'conns': v[2]} for key, v in table.items()),
                  key=lambda row: (-(row['up'] + row['down']), row['key']))
    return rows[:limit]


class TrafficHistory:
    """内存中的待保存增量 + SQLite 文件；所有方法线程安全。"""

    def __init__(self, data_dir: str):
        self.data_dir = data_dir
        self.path = os.path.join(data_dir, FILENAME)
        self._lock = threading.RLock()
        # (agent_id, bucket, source, group, outbound) / (agent_id, bucket, source, target, outbound) → [up, down, conns]
        self._usage: Dict[tuple, List[int]] = {}
        self._targets: Dict[tuple, List[int]] = {}

    # ---------- 写入 ----------

    def add_report(self, agent_id: str, cleaned: Dict[str, List[tuple]]):
        with self._lock:
            for bucket, source, group, outbound, up, down, conns in cleaned['usage']:
                _add(self._usage, (agent_id, bucket, source, group, outbound), (up, down, conns))
            for bucket, source, target, outbound, up, down, conns in cleaned['targets']:
                _add(self._targets, (agent_id, bucket, source, target, outbound), (up, down, conns))

    def pending_count(self) -> int:
        with self._lock:
            return len(self._usage) + len(self._targets)

    def _connect(self) -> sqlite3.Connection:
        os.makedirs(self.data_dir, exist_ok=True)
        connection = sqlite3.connect(self.path, timeout=10, isolation_level=None)
        # 与配置库一致，只用单个数据库文件，备份和数据卷里不会多出 -wal 文件
        connection.execute('PRAGMA journal_mode=DELETE')
        for statement in SCHEMA:
            connection.execute(statement)
        return connection

    def flush(self, retention: int = DEFAULT_RETENTION, now: Optional[float] = None) -> Dict[str, int]:
        """保存内存中的增量，合并旧的小时数据并删除过期数据；失败时增量留在内存里，异常照常抛出。"""
        now = time.time() if now is None else now
        with self._lock:
            usage, targets = self._usage, self._targets
            if not usage and not targets and not os.path.exists(self.path):
                return {'rows': 0, 'compacted': 0, 'pruned': 0}
            self._usage, self._targets = {}, {}
            try:
                connection = self._connect()
                try:
                    connection.execute('BEGIN IMMEDIATE')
                    connection.executemany(USAGE_UPSERT.format(span=HOUR), [key + tuple(v) for key, v in usage.items()])
                    connection.executemany(TARGET_UPSERT.format(span=HOUR), [key + tuple(v) for key, v in targets.items()])
                    compacted = self._compact(connection, now)
                    pruned = self._prune(connection, now - retention * DAY)
                    connection.execute('COMMIT')
                except BaseException:
                    if connection.in_transaction:
                        connection.execute('ROLLBACK')
                    raise
                finally:
                    connection.close()
            except BaseException:
                # 放回去，与保存期间新收到的上报合并，下次再保存
                for key, values in usage.items():
                    _add(self._usage, key, values)
                for key, values in targets.items():
                    _add(self._targets, key, values)
                raise
            return {'rows': len(usage) + len(targets), 'compacted': compacted, 'pruned': pruned}

    @staticmethod
    def _compact(connection: sqlite3.Connection, now: float) -> int:
        """把较早的小时行合并成按天的行；目标按天只保留每台设备流量最大的若干个，其余并入「其他」。"""
        compacted = 0
        usage_before = int(now // DAY * DAY - USAGE_HOURLY_DAYS * DAY)
        rows = connection.execute(
            'SELECT agent_id, bucket / 86400 * 86400, source, policy_group, outbound, SUM(up), SUM(down), SUM(conns) '
            'FROM usage WHERE span = 3600 AND bucket < ? GROUP BY 1, 2, 3, 4, 5', (usage_before,)).fetchall()
        if rows:
            compacted += connection.execute('DELETE FROM usage WHERE span = 3600 AND bucket < ?', (usage_before,)).rowcount
            connection.executemany(USAGE_UPSERT.format(span=DAY), rows)
        targets_before = int(now // DAY * DAY - TARGET_HOURLY_DAYS * DAY)
        rows = connection.execute(
            'SELECT agent_id, bucket / 86400 * 86400, source, target, outbound, SUM(up), SUM(down), SUM(conns) '
            'FROM targets WHERE span = 3600 AND bucket < ? GROUP BY 1, 2, 3, 4, 5', (targets_before,)).fetchall()
        if rows:
            compacted += connection.execute('DELETE FROM targets WHERE span = 3600 AND bucket < ?', (targets_before,)).rowcount
            connection.executemany(TARGET_UPSERT.format(span=DAY), rows)
            for agent_id, day in {(row[0], row[1]) for row in rows}:
                compacted += TrafficHistory._trim_day(connection, agent_id, day)
        return compacted

    @staticmethod
    def _trim_day(connection: sqlite3.Connection, agent_id: str, day: int) -> int:
        """每台设备只留当天流量最大的若干个目标（各出口合计排名），其余按出口并入「其他」。"""
        rows = connection.execute(
            'SELECT source, target, outbound, up, down, conns FROM targets WHERE agent_id = ? AND bucket = ? AND span = 86400',
            (agent_id, day)).fetchall()
        totals: Dict[Tuple[str, str], int] = {}
        for source, target, _outbound, up, down, _conns in rows:
            if target != OTHER_TARGET:
                totals[(source, target)] = totals.get((source, target), 0) + up + down
        ranked: Dict[str, List[str]] = {}
        for (source, target), total in sorted(totals.items(), key=lambda item: (-item[1], item[0][1])):
            ranked.setdefault(source, []).append(target)
        dropped = {(source, target) for source, targets in ranked.items() for target in targets[TARGET_DAILY_TOP:]}
        if not dropped:
            return 0
        merged: Dict[Tuple[str, str], List[int]] = {}
        for source, target, outbound, up, down, conns in rows:
            if (source, target) in dropped:
                _add(merged, (source, outbound), (up, down, conns))
        extra = [row for row in rows if (row[0], row[1]) in dropped]
        connection.executemany(
            'DELETE FROM targets WHERE agent_id = ? AND bucket = ? AND span = 86400 AND source = ? AND target = ? AND outbound = ?',
            [(agent_id, day, source, target, outbound) for source, target, outbound, *_ in extra])
        connection.executemany(TARGET_UPSERT.format(span=DAY),
                               [(agent_id, day, source, OTHER_TARGET, outbound, *values)
                                for (source, outbound), values in merged.items()])
        return len(extra)

    @staticmethod
    def _prune(connection: sqlite3.Connection, cutoff: float) -> int:
        # 按天的行整天过期才删除
        pruned = 0
        for table in ('usage', 'targets'):
            pruned += connection.execute(f'DELETE FROM {table} WHERE bucket + span <= ?', (int(cutoff),)).rowcount
        return pruned

    # ---------- 删除 ----------

    @staticmethod
    def _delete_agents(connection: sqlite3.Connection, agent_ids: Iterable[str]):
        connection.execute('BEGIN IMMEDIATE')
        try:
            for agent_id in agent_ids:
                connection.execute('DELETE FROM usage WHERE agent_id = ?', (agent_id,))
                connection.execute('DELETE FROM targets WHERE agent_id = ?', (agent_id,))
            connection.execute('COMMIT')
        except BaseException:
            connection.execute('ROLLBACK')
            raise

    def delete(self, agent_id: str):
        with self._lock:
            self._usage = {key: value for key, value in self._usage.items() if key[0] != agent_id}
            self._targets = {key: value for key, value in self._targets.items() if key[0] != agent_id}
            if os.path.exists(self.path):
                connection = self._connect()
                try:
                    self._delete_agents(connection, [agent_id])
                finally:
                    connection.close()

    def retain_agents(self, agent_ids: Iterable[str]) -> int:
        """删除已不存在的 Agent 的数据（导入、恢复或重置配置后）。"""
        keep = set(agent_ids)
        with self._lock:
            self._usage = {key: value for key, value in self._usage.items() if key[0] in keep}
            self._targets = {key: value for key, value in self._targets.items() if key[0] in keep}
            if not os.path.exists(self.path):
                return 0
            connection = self._connect()
            try:
                stored = {row[0] for row in connection.execute('SELECT DISTINCT agent_id FROM usage UNION SELECT DISTINCT agent_id FROM targets')}
                removed = stored - keep
                if removed:
                    self._delete_agents(connection, removed)
                return len(removed)
            finally:
                connection.close()

    # ---------- 查询 ----------

    def query(self, agent_id: str, range_key: str, by: str, now: Optional[float] = None) -> Dict[str, Any]:
        """返回范围内按 by 汇总的行（附明细）、总量、各出口节点的总量和趋势。

        明细：按设备、目标、策略组汇总时按出口节点拆开；按出站节点汇总时按设备拆开。
        趋势 24 小时与 7 天按小时，30 天按天。
        """
        now = time.time() if now is None else now
        start = now - RANGES[range_key]
        step = DAY if range_key == '30d' else HOUR
        usage_rows, target_rows = self._rows(agent_id, start, by == 'destination')

        totals = [0, 0, 0]
        outbounds: Dict[str, List[int]] = {}
        trend: Dict[int, List[int]] = {}
        grouped: Dict[str, List[int]] = {}
        parts: Dict[str, Dict[str, List[int]]] = {}

        def add(key, part, values):
            _add(grouped, key, values)
            if part is not None:
                _add(parts.setdefault(key, {}), part, values)

        for bucket, _span, source, group, outbound, up, down, conns in usage_rows:
            values = (up, down, conns)
            for index, value in enumerate(values):
                totals[index] += value
            if source != UNATTRIBUTED:
                _add(outbounds, outbound, values)
            point = trend.setdefault(int(bucket // step * step), [0, 0])
            point[0] += up
            point[1] += down
            if source == UNATTRIBUTED:
                add(UNATTRIBUTED, None, values)
            elif by == 'source':
                add(source, outbound, values)
            elif by == 'group':
                add(group, outbound, values)
            elif by == 'outbound':
                add(outbound, source, values)
        for _bucket, _span, source, target, outbound, up, down, conns in target_rows:
            add(target, outbound, (up, down, conns))

        rows = _rows(grouped, QUERY_ROW_LIMIT)
        for row in rows:
            row['parts'] = _rows(parts.get(row['key'], {}), QUERY_PART_LIMIT)
        first = int(start // step * step)
        return {
            'range': range_key, 'by': by, 'step': step,
            'start': datetime.fromtimestamp(start, timezone.utc).isoformat(),
            'rows': rows, 'omitted': max(0, len(grouped) - QUERY_ROW_LIMIT),
            'total': {'up': totals[0], 'down': totals[1], 'conns': totals[2]},
            'outbounds': _rows(outbounds, len(outbounds)),
            'trend': [{'at': datetime.fromtimestamp(at, timezone.utc).isoformat(), 'up': trend.get(at, [0, 0])[0],
                       'down': trend.get(at, [0, 0])[1]} for at in range(first, int(now) + 1, step)],
        }

    def _rows(self, agent_id: str, start: float, with_targets: bool) -> Tuple[List[tuple], List[tuple]]:
        """范围内的行；按天的行只要与范围有重叠就计入。"""
        usage, targets = [], []
        with self._lock:
            for (owner, bucket, source, group, outbound), values in self._usage.items():
                if owner == agent_id and bucket + HOUR > start:
                    usage.append((bucket, HOUR, source, group, outbound, *values))
            if with_targets:
                for (owner, bucket, source, target, outbound), values in self._targets.items():
                    if owner == agent_id and bucket + HOUR > start:
                        targets.append((bucket, HOUR, source, target, outbound, *values))
            if not os.path.exists(self.path):
                return usage, targets
            connection = self._connect()
            try:
                usage += connection.execute(
                    'SELECT bucket, span, source, policy_group, outbound, up, down, conns FROM usage '
                    'WHERE agent_id = ? AND bucket + span > ?', (agent_id, int(start))).fetchall()
                if with_targets:
                    targets += connection.execute(
                        'SELECT bucket, span, source, target, outbound, up, down, conns FROM targets '
                        'WHERE agent_id = ? AND bucket + span > ?', (agent_id, int(start))).fetchall()
            finally:
                connection.close()
        return usage, targets
