"""Agent 监控数据历史存储

每台 Agent 最近 24 小时的监控点保存在内存中，心跳只追加内存。每个点按写入
文件的 JSON 行保存（约为解析后字典的六分之一），查询时再解析。
「监控历史保存」任务（agents/metrics_flush.py）按间隔把新增的点追加到
每台 Agent 的 JSON Lines 文件；文件行数明显多于内存保留的点时，从内存重写
临时文件后原子替换。服务启动时读回文件（容忍写到一半的行），旧版
<agent_id>_metrics.json 读入后改写为新格式并删除。
"""

import errno
import json
import logging
import os
import re
import tempfile
from collections import deque
from datetime import datetime, timedelta
from typing import Dict, List, Optional, Any
from threading import Lock

logger = logging.getLogger(__name__)

RETENTION_HOURS = 24
# 30 秒一次心跳时 24 小时约 2880 个点；异常频繁的心跳只保留最新的点
MAX_POINTS_PER_AGENT = 4000
# 文件行数超过内存保留点数的 1.5 倍时整理文件
COMPACT_RATIO = 1.5
FILE_SUFFIX = '_metrics.jsonl'
LEGACY_SUFFIX = '_metrics.json'
_AGENT_ID = re.compile(r'^[A-Za-z0-9_.-]+$')
_ERRNO_TEXT = {
    errno.ENOSPC: '磁盘空间不足',
    errno.EDQUOT: '超出磁盘配额',
    errno.EACCES: '没有写入权限',
    errno.EPERM: '没有写入权限',
    errno.EROFS: '文件系统只读',
}


class MetricsFlushError(RuntimeError):
    """One or more Agents' new points could not be written; they stay in memory."""


def describe_os_error(error: BaseException) -> str:
    """A short, readable reason for 定时任务; never includes file contents."""
    if isinstance(error, OSError):
        text = _ERRNO_TEXT.get(error.errno)
        if text:
            return text
        if error.strerror:
            return error.strerror
    return f'{type(error).__name__}: {error}' if str(error) else type(error).__name__


def _encode(point: Dict[str, Any]) -> str:
    return json.dumps(point, ensure_ascii=False, separators=(',', ':'))


class MetricsHistory:
    """Agent 监控数据历史管理"""

    def __init__(self, data_dir: str = None, max_points: int = MAX_POINTS_PER_AGENT):
        """
        初始化监控历史管理器

        Args:
            data_dir: 数据存储目录，None 则自动选择
            max_points: 每台 Agent 在内存中最多保留的监控点
        """
        # 如果未指定目录，根据环境自动选择
        if data_dir is None:
            # 检查是否在生产环境（/opt/configflow 存在）
            if os.path.exists('/opt/configflow'):
                data_dir = "/opt/configflow/data/metrics"
            else:
                # 开发环境：使用项目根目录下的 data 目录
                project_root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
                data_dir = os.path.join(project_root, "data", "metrics")

        self.data_dir = data_dir
        self.max_points = max_points
        # lock 保护内存；flush_lock 让保存、整理与删除逐个进行，写盘时不阻塞心跳
        self.lock = Lock()
        self.flush_lock = Lock()
        # agent_id -> deque[(datetime, 该点的 JSON 行)]
        self._points: Dict[str, deque] = {}
        # 每台 Agent 最新的多少个点还没有写入文件
        self._pending: Dict[str, int] = {}
        # 文件中的行数（含无法解析的行），用于判断是否需要整理
        self._file_lines: Dict[str, int] = {}
        # 写入失败过、文件内容不可信的 Agent，下次保存时从内存重写
        self._rewrite = set()
        # 迁移时没能改写的旧文件，新文件写入成功后再删除
        self._legacy: Dict[str, str] = {}

        # 确保数据目录存在
        try:
            os.makedirs(self.data_dir, exist_ok=True)
        except PermissionError:
            # 如果没有权限，回退到临时目录
            self.data_dir = os.path.join(tempfile.gettempdir(), "configflow_metrics")
            os.makedirs(self.data_dir, exist_ok=True)
            logger.warning('监控历史目录没有写入权限，改用临时目录 %s', self.data_dir)

    def _get_agent_file(self, agent_id: str) -> str:
        """获取 Agent 的历史数据文件路径"""
        return os.path.join(self.data_dir, f"{agent_id}{FILE_SUFFIX}")

    def _get_legacy_file(self, agent_id: str) -> str:
        """旧版整份 JSON 历史文件"""
        return os.path.join(self.data_dir, f"{agent_id}{LEGACY_SUFFIX}")

    def _cutoff(self, hours: float = RETENTION_HOURS) -> datetime:
        return datetime.now() - timedelta(hours=hours)

    def _prune(self, agent_id: str) -> deque:
        """丢弃超过 24 小时的点；调用方持有 lock。"""
        points = self._points.get(agent_id)
        if points is None:
            return deque()
        cutoff = self._cutoff()
        while points and points[0][0] < cutoff:
            points.popleft()
        if self._pending.get(agent_id, 0) > len(points):
            self._pending[agent_id] = len(points)
        return points

    # ---- 启动加载与迁移 -------------------------------------------------

    def _parse_lines(self, lines, cutoff):
        """Return (points, line_count). Unparseable lines (an append cut short
        by a crash) and duplicate timestamps are skipped."""
        points, seen, count = [], set(), 0
        for raw in lines:
            if not raw.strip():
                continue
            count += 1
            try:
                point = json.loads(raw)
                timestamp = datetime.fromisoformat(point['timestamp'])
                if timestamp < cutoff or point['timestamp'] in seen:
                    continue
            except (ValueError, TypeError, KeyError):
                continue
            seen.add(point['timestamp'])
            points.append((timestamp, _encode(point)))
        return points, count

    def _read_file(self, agent_id: str, cutoff):
        path = self._get_agent_file(agent_id)
        with open(path, 'r', encoding='utf-8', errors='replace') as handle:
            points, count = self._parse_lines(handle, cutoff)
        return points, count

    def _read_legacy(self, agent_id: str, cutoff):
        path = self._get_legacy_file(agent_id)
        with open(path, 'r', encoding='utf-8') as handle:
            data = json.load(handle)
        metrics = data.get('metrics', []) if isinstance(data, dict) else []
        points, _ = self._parse_lines((_encode(item) for item in metrics if isinstance(item, dict)), cutoff)
        return points

    def load(self, agent_ids) -> Dict[str, int]:
        """服务启动时读回现有 Agent 的历史，删除已删除 Agent 的文件。

        旧版整份 JSON 读入后改写为 JSON Lines 并删除。返回加载、迁移、
        删除的文件数，便于记录日志和测试。
        """
        agent_ids = set(agent_ids)
        result = {'loaded': 0, 'migrated': 0, 'removed': 0}
        try:
            names = os.listdir(self.data_dir)
        except OSError as error:
            logger.warning('读取监控历史目录失败：%s', describe_os_error(error))
            return result
        found: Dict[str, set] = {}
        for name in names:
            path = os.path.join(self.data_dir, name)
            if name.startswith('.') and name.endswith('.tmp'):
                # 整理文件时中断留下的临时文件
                self._remove(path)
                continue
            for suffix in (FILE_SUFFIX, LEGACY_SUFFIX):
                if name.endswith(suffix) and _AGENT_ID.match(name[:-len(suffix)]):
                    found.setdefault(name[:-len(suffix)], set()).add(suffix)
                    break

        cutoff = self._cutoff()
        migrate = []
        with self.flush_lock:
            for agent_id, kinds in found.items():
                if agent_id not in agent_ids:
                    for suffix in kinds:
                        if self._remove(os.path.join(self.data_dir, agent_id + suffix)):
                            result['removed'] += 1
                    continue
                points, lines = [], 0
                if FILE_SUFFIX in kinds:
                    try:
                        points, lines = self._read_file(agent_id, cutoff)
                    except OSError as error:
                        logger.warning('读取 Agent %s 的监控历史失败：%s', agent_id, describe_os_error(error))
                        continue
                    if LEGACY_SUFFIX in kinds:
                        # 新文件由旧文件改写而来，旧文件是迁移中断留下的
                        self._remove(self._get_legacy_file(agent_id))
                else:
                    try:
                        points = self._read_legacy(agent_id, cutoff)
                    except (OSError, ValueError) as error:
                        logger.warning('读取 Agent %s 的旧版监控历史失败，已忽略：%s', agent_id,
                                       describe_os_error(error) if isinstance(error, OSError) else '文件格式错误')
                        self._remove(self._get_legacy_file(agent_id))
                        continue
                    migrate.append(agent_id)
                points = points[-self.max_points:]
                with self.lock:
                    self._points[agent_id] = deque(points, maxlen=self.max_points)
                    self._file_lines[agent_id] = lines
                    if agent_id in migrate:
                        self._pending[agent_id] = len(points)
                        self._legacy[agent_id] = self._get_legacy_file(agent_id)
                        self._rewrite.add(agent_id)
                result['loaded'] += 1
            errors = self._flush_agents(migrate)['errors'] if migrate else []
        result['migrated'] = len(migrate) - len(errors)
        for agent_id, error in errors:
            logger.warning('Agent %s 的旧版监控历史改写失败，下次保存时重试：%s', agent_id, error)
        if result['migrated'] or result['removed']:
            logger.info('监控历史已加载：迁移旧格式 %d 个，删除已删除 Agent 的文件 %d 个',
                        result['migrated'], result['removed'])
        return result

    # ---- 心跳与查询 -----------------------------------------------------

    def add_metrics(self, agent_id: str, system_metrics: Dict[str, Any]) -> bool:
        """
        添加监控数据点（只写内存，由「监控历史保存」任务写入文件）

        Args:
            agent_id: Agent ID
            system_metrics: 系统监控数据

        Returns:
            bool: 是否添加成功
        """
        now = datetime.now()
        data_point = {
            'timestamp': now.isoformat(),
            'cpu': system_metrics.get('cpu', {}),
            'memory': system_metrics.get('memory', {}),
            'disk': system_metrics.get('disk', {}),
            'network': system_metrics.get('network', {})
        }
        try:
            # 查询时解析这一行，结果与以前从文件读回的一致，也不与心跳数据共享可变对象
            line = _encode(data_point)
        except (TypeError, ValueError) as error:
            logger.warning('Agent %s 的监控数据无法保存：%s', agent_id, error)
            return False
        with self.lock:
            points = self._points.get(agent_id)
            if points is None:
                points = self._points[agent_id] = deque(maxlen=self.max_points)
            points.append((now, line))
            self._pending[agent_id] = min(self._pending.get(agent_id, 0) + 1, len(points))
            self._prune(agent_id)
        return True

    def get_metrics(self, agent_id: str, hours: Optional[int] = None) -> List[Dict[str, Any]]:
        """
        获取 Agent 的监控历史数据

        Args:
            agent_id: Agent ID
            hours: 获取最近多少小时的数据（None 表示全部）

        Returns:
            List[Dict]: 历史数据列表
        """
        with self.lock:
            points = self._prune(agent_id)
            if hours is None:
                lines = [line for _, line in points]
            else:
                cutoff = self._cutoff(hours)
                lines = [line for timestamp, line in points if timestamp >= cutoff]
        return [json.loads(line) for line in lines]

    def get_latest_metrics(self, agent_id: str) -> Optional[Dict[str, Any]]:
        """
        获取 Agent 的最新监控数据

        Args:
            agent_id: Agent ID

        Returns:
            Dict: 最新监控数据，如果没有则返回 None
        """
        metrics = self.get_metrics(agent_id)

        if metrics:
            return metrics[-1]

        return None

    def pending_count(self) -> int:
        """内存中还没有写入文件的监控点数"""
        with self.lock:
            return sum(self._pending.values())

    def delete_agent_history(self, agent_id: str) -> bool:
        """
        删除 Agent 的所有历史数据（内存与文件）

        Args:
            agent_id: Agent ID

        Returns:
            bool: 是否删除成功
        """
        with self.flush_lock:
            with self.lock:
                for store in (self._points, self._pending, self._file_lines, self._legacy):
                    store.pop(agent_id, None)
                self._rewrite.discard(agent_id)
            removed = [self._remove(self._get_agent_file(agent_id), report=True),
                       self._remove(self._get_legacy_file(agent_id), report=True)]
        return all(item is not None for item in removed)

    def retain_agents(self, agent_ids) -> int:
        """删除不在 agent_ids 中的 Agent 的历史（内存与文件），返回删除的 Agent 数。

        导入备份、WebDAV 恢复或重置会直接替换 Agent 列表，不经过删除 Agent；
        每次保存前按当前列表清理一次。
        """
        agent_ids = set(agent_ids)
        with self.lock:
            stale = (set(self._points) | set(self._file_lines) | set(self._legacy)) - agent_ids
        for agent_id in sorted(stale):
            self.delete_agent_history(agent_id)
        return len(stale)

    # ---- 写入文件 -------------------------------------------------------

    def _remove(self, path: str, report: bool = False):
        """True when removed, False when missing, None on failure."""
        try:
            os.remove(path)
            return True
        except FileNotFoundError:
            return False
        except OSError as error:
            (logger.warning if report else logger.debug)('删除监控历史文件 %s 失败：%s',
                                                         os.path.basename(path), describe_os_error(error))
            return None

    def _append(self, path: str, lines: List[str]):
        with open(path, 'a+b') as handle:
            handle.seek(0, os.SEEK_END)
            if handle.tell():
                # 上次追加若被中断，最后一行没有换行；新行另起一行，坏行在读取时跳过
                handle.seek(-1, os.SEEK_END)
                if handle.read(1) != b'\n':
                    handle.write(b'\n')
            handle.write(''.join(line + '\n' for line in lines).encode('utf-8'))
            handle.flush()
            os.fsync(handle.fileno())

    def _replace(self, agent_id: str, path: str, lines: List[str]):
        if not lines:
            self._remove(path)
            return
        descriptor, temporary = tempfile.mkstemp(dir=self.data_dir, prefix=f'.{agent_id}', suffix='.tmp')
        try:
            with os.fdopen(descriptor, 'wb') as handle:
                handle.write(''.join(line + '\n' for line in lines).encode('utf-8'))
                handle.flush()
                os.fsync(handle.fileno())
            os.replace(temporary, path)
        finally:
            if os.path.exists(temporary):
                os.unlink(temporary)

    def _flush_agents(self, agent_ids) -> Dict[str, Any]:
        """Write the given Agents; caller holds flush_lock.

        Returns counts (agents, appended, compacted) and errors [(agent_id, reason)].
        """
        jobs = []
        with self.lock:
            for agent_id in agent_ids:
                points = self._prune(agent_id)
                pending = self._pending.get(agent_id, 0)
                lines_after = self._file_lines.get(agent_id, 0) + pending
                compact = agent_id in self._rewrite or lines_after > len(points) * COMPACT_RATIO
                if not compact and not pending:
                    continue
                if compact:
                    lines = [line for _, line in points]
                else:
                    lines = [line for _, line in list(points)[len(points) - pending:]]
                self._pending[agent_id] = 0
                jobs.append((agent_id, compact, pending, lines))
                if not points:
                    # 超过 24 小时没有心跳：删除文件，也不再保留空记录
                    self._points.pop(agent_id, None)
                    self._pending.pop(agent_id, None)

        result = {'agents': 0, 'appended': 0, 'compacted': 0, 'errors': []}
        for agent_id, compact, pending, lines in jobs:
            path = self._get_agent_file(agent_id)
            try:
                if compact:
                    self._replace(agent_id, path, lines)
                else:
                    self._append(path, lines)
            except Exception as error:
                with self.lock:
                    points = self._points.get(agent_id)
                    if points is not None:
                        self._pending[agent_id] = min(self._pending.get(agent_id, 0) + pending, len(points))
                        # 追加可能只写了一部分；下次从内存整份重写，避免重复的点
                        self._rewrite.add(agent_id)
                result['errors'].append((agent_id, describe_os_error(error)))
                continue
            with self.lock:
                if compact:
                    self._rewrite.discard(agent_id)
                if lines:
                    self._file_lines[agent_id] = len(lines) if compact else self._file_lines.get(agent_id, 0) + len(lines)
                else:
                    self._file_lines.pop(agent_id, None)
                legacy = self._legacy.pop(agent_id, None)
            if legacy:
                self._remove(legacy, report=True)
            result['agents'] += 1 if pending else 0
            result['appended'] += pending
            result['compacted'] += 1 if compact else 0
        return result

    def flush(self) -> Dict[str, int]:
        """
        把新增的监控点追加到文件；文件明显大于内存数据时原子整理

        Returns:
            Dict: agents（有新数据的 Agent 数）、appended（新写入的点）、compacted（整理的文件数）

        Raises:
            MetricsFlushError: 有 Agent 写入失败；这些点留在内存，下次保存时重试
        """
        with self.flush_lock:
            with self.lock:
                agent_ids = sorted(set(self._points) | set(self._file_lines) | set(self._legacy))
            result = self._flush_agents(agent_ids)
        errors = result.pop('errors')
        if errors:
            reasons = sorted({reason for _, reason in errors})
            raise MetricsFlushError(
                f'{len(errors)} 台 Agent 的监控历史写入失败（{"；".join(reasons)}），'
                f'数据仍在内存中，下次保存时重试。目录：{self.data_dir}')
        return result

    def get_metrics_summary(self, agent_id: str, hours: int = 1) -> Dict[str, Any]:
        """
        获取监控数据的统计摘要

        Args:
            agent_id: Agent ID
            hours: 统计最近多少小时的数据

        Returns:
            Dict: 统计摘要（平均值、最大值、最小值）
        """
        metrics = self.get_metrics(agent_id, hours=hours)

        if not metrics:
            return {}

        # 初始化统计数据
        cpu_values = []
        memory_values = []
        disk_values = []
        network_sent_values = []
        network_recv_values = []

        # 收集数据
        for metric in metrics:
            cpu = metric.get('cpu', {})
            memory = metric.get('memory', {})
            disk = metric.get('disk', {})
            network = metric.get('network', {})

            if 'usage_percent' in cpu:
                cpu_values.append(cpu['usage_percent'])
            if 'used_percent' in memory:
                memory_values.append(memory['used_percent'])
            if 'used_percent' in disk:
                disk_values.append(disk['used_percent'])
            if 'speed_sent' in network:
                network_sent_values.append(network['speed_sent'])
            if 'speed_recv' in network:
                network_recv_values.append(network['speed_recv'])

        # 计算统计值
        def calc_stats(values):
            if not values:
                return {'avg': 0, 'max': 0, 'min': 0}
            return {
                'avg': sum(values) / len(values),
                'max': max(values),
                'min': min(values)
            }

        return {
            'cpu': calc_stats(cpu_values),
            'memory': calc_stats(memory_values),
            'disk': calc_stats(disk_values),
            'network_sent': calc_stats(network_sent_values),
            'network_recv': calc_stats(network_recv_values),
            'data_points': len(metrics),
            'time_range_hours': hours
        }

    def get_traffic_stats(self, agent_id: str, period: str = 'total') -> Dict[str, Any]:
        """
        获取流量统计数据

        Args:
            agent_id: Agent ID
            period: 统计周期 ('total', 'today', 'week', 'hours_24')

        Returns:
            Dict: 流量统计数据
        """
        metrics = self.get_metrics(agent_id, hours=24)  # 最多获取24小时数据

        if not metrics:
            return {
                'bytes_sent': 0,
                'bytes_recv': 0,
                'bytes_sent_delta': 0,
                'bytes_recv_delta': 0,
                'avg_speed_sent': 0,
                'avg_speed_recv': 0,
                'period': period,
                'start_time': None,
                'end_time': None
            }

        # 根据周期过滤数据
        now = datetime.now()
        filtered_metrics = []

        if period == 'today':
            # 今日（从00:00开始）
            start_of_day = now.replace(hour=0, minute=0, second=0, microsecond=0)
            filtered_metrics = [m for m in metrics
                              if datetime.fromisoformat(m['timestamp']) >= start_of_day]
        elif period == 'week':
            # 本周（从周一00:00开始）
            days_since_monday = now.weekday()
            start_of_week = (now - timedelta(days=days_since_monday)).replace(
                hour=0, minute=0, second=0, microsecond=0)
            filtered_metrics = [m for m in metrics
                              if datetime.fromisoformat(m['timestamp']) >= start_of_week]
        elif period == 'hours_24':
            # 最近24小时
            filtered_metrics = metrics
        else:  # 'total'
            # 全部可用数据
            filtered_metrics = metrics

        if not filtered_metrics:
            return {
                'bytes_sent': 0,
                'bytes_recv': 0,
                'bytes_sent_delta': 0,
                'bytes_recv_delta': 0,
                'avg_speed_sent': 0,
                'avg_speed_recv': 0,
                'period': period,
                'start_time': None,
                'end_time': None
            }

        # 获取最新的累计流量值
        latest_metric = filtered_metrics[-1]
        latest_network = latest_metric.get('network', {})
        current_sent = latest_network.get('bytes_sent', 0)
        current_recv = latest_network.get('bytes_recv', 0)

        # 计算流量增量
        first_metric = filtered_metrics[0]
        first_network = first_metric.get('network', {})
        start_sent = first_network.get('bytes_sent', 0)
        start_recv = first_network.get('bytes_recv', 0)

        # 检测计数器重置（系统重启）
        sent_delta = current_sent - start_sent
        recv_delta = current_recv - start_recv

        # 如果差值为负，说明发生了重置，使用当前值作为增量
        if sent_delta < 0:
            sent_delta = current_sent
        if recv_delta < 0:
            recv_delta = current_recv

        # 计算平均速率
        start_time = datetime.fromisoformat(first_metric['timestamp'])
        end_time = datetime.fromisoformat(latest_metric['timestamp'])
        duration_seconds = (end_time - start_time).total_seconds()

        avg_speed_sent = sent_delta / duration_seconds if duration_seconds > 0 else 0
        avg_speed_recv = recv_delta / duration_seconds if duration_seconds > 0 else 0

        return {
            'bytes_sent': current_sent,  # 当前累计总上传
            'bytes_recv': current_recv,  # 当前累计总下载
            'bytes_sent_delta': sent_delta,  # 时间段内上传增量
            'bytes_recv_delta': recv_delta,  # 时间段内下载增量
            'avg_speed_sent': avg_speed_sent,  # 平均上传速率
            'avg_speed_recv': avg_speed_recv,  # 平均下载速率
            'period': period,
            'start_time': start_time.isoformat(),
            'end_time': end_time.isoformat(),
            'data_points': len(filtered_metrics)
        }

    def get_traffic_trend(self, agent_id: str, hours: int = 24,
                         interval_minutes: int = 5) -> List[Dict[str, Any]]:
        """
        获取流量趋势数据（用于绘制图表）

        Args:
            agent_id: Agent ID
            hours: 获取最近多少小时的数据
            interval_minutes: 数据点间隔（分钟）

        Returns:
            List[Dict]: 流量趋势数据点列表
        """
        metrics = self.get_metrics(agent_id, hours=hours)

        if not metrics:
            return []

        # 按时间间隔分组数据
        trend_data = []
        interval_seconds = interval_minutes * 60

        # 初始化上一个数据点
        prev_metric = None
        prev_time = None
        group_start_metric = None
        group_start_time = None

        for metric in metrics:
            timestamp = datetime.fromisoformat(metric['timestamp'])
            network = metric.get('network', {})

            # 第一个数据点
            if prev_metric is None:
                prev_metric = metric
                prev_time = timestamp
                group_start_metric = metric
                group_start_time = timestamp
                continue

            # 检查是否达到间隔时间
            time_diff = (timestamp - group_start_time).total_seconds()

            if time_diff >= interval_seconds:
                # 计算这个时间段的流量增量
                prev_network = prev_metric.get('network', {})

                sent_delta = network.get('bytes_sent', 0) - prev_network.get('bytes_sent', 0)
                recv_delta = network.get('bytes_recv', 0) - prev_network.get('bytes_recv', 0)

                # 处理计数器重置
                if sent_delta < 0:
                    sent_delta = network.get('bytes_sent', 0)
                if recv_delta < 0:
                    recv_delta = network.get('bytes_recv', 0)

                # 计算平均速率
                duration = (timestamp - prev_time).total_seconds()
                avg_speed_sent = sent_delta / duration if duration > 0 else 0
                avg_speed_recv = recv_delta / duration if duration > 0 else 0

                trend_data.append({
                    'timestamp': timestamp.isoformat(),
                    'bytes_sent': network.get('bytes_sent', 0),
                    'bytes_recv': network.get('bytes_recv', 0),
                    'sent_delta': sent_delta,
                    'recv_delta': recv_delta,
                    'speed_sent': avg_speed_sent,
                    'speed_recv': avg_speed_recv
                })

                # 更新分组起点
                group_start_metric = metric
                group_start_time = timestamp

            prev_metric = metric
            prev_time = timestamp

        # 添加最后一个数据点
        if prev_metric:
            network = prev_metric.get('network', {})
            trend_data.append({
                'timestamp': prev_time.isoformat(),
                'bytes_sent': network.get('bytes_sent', 0),
                'bytes_recv': network.get('bytes_recv', 0),
                'speed_sent': network.get('speed_sent', 0),
                'speed_recv': network.get('speed_recv', 0)
            })

        return trend_data
