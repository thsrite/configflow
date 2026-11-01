"""系统设置 → 定时任务「监控历史保存」.

Agent heartbeats only append to MetricsHistory in memory. This task writes the
new points to disk on the interval chosen in system_config, and once more when
the service stops (SIGTERM from docker stop / supervisord, or Ctrl+C), so a
graceful stop loses nothing. Every run, including failures such as a full disk
or a read-only directory, is recorded in task_status for the task list.
"""
from datetime import datetime, timedelta, timezone
import logging
import os
import signal
import sys
import threading
import time

from backend.utils import task_status
from backend.utils.logger import get_logger

logger = get_logger(__name__)

TASK_KEY = 'metrics_history_flush'
SETTING = 'metrics_history_flush_interval'
INTERVAL_CHOICES = (300, 600, 1800, 3600)
DEFAULT_INTERVAL = 1800
TICK_SECONDS = 30

_run_lock = threading.Lock()
_wake = threading.Event()
_start_guard = threading.Lock()
_started_at = None
# 本进程上次开始保存的时间：运行记录写不进去时，也不会每个 TICK 都重跑
_attempted_at = None
# 服务正在停止（收到停止信号，或 app.py 已在做最后一次保存）
_stopping = False
_shutdown_flushing = False
_stop_signum = None


def validate_interval(value):
    if type(value) is not int or value not in INTERVAL_CHOICES:
        raise ValueError('保存间隔只能是 5、10、30 或 60 分钟')
    return value


def interval_seconds(system=None):
    """The configured interval; anything unexpected falls back to 30 minutes."""
    if system is None:
        from backend.common.config import get_repository
        system = get_repository().get_system()
    value = (system.get('system_config') or {}).get(SETTING, DEFAULT_INTERVAL)
    return value if type(value) is int and value in INTERVAL_CHOICES else DEFAULT_INTERVAL


def set_interval(seconds):
    from backend.common.config import get_repository
    seconds = validate_interval(seconds)
    get_repository().update_system_transaction(
        lambda system: system.setdefault('system_config', {}).update({SETTING: seconds}))
    # 循环按新间隔重新计算下次运行，不需要重启
    _wake.set()
    return seconds


def is_running():
    return _run_lock.locked()


def next_run_at(entry, interval):
    """Due time of the next scheduled run; None before the loop has started."""
    last = task_status.parse_time(entry.get('last_run_at'))
    base = max(filter(None, (last, _started_at, _attempted_at)), default=None)
    if base is None:
        return None
    return base + timedelta(seconds=interval)


def run(trigger='schedule', record_idle=True):
    """Flush every Agent's new points; returns MetricsHistory.flush()'s counts.

    The result, or the reason it failed, lands in task_status. A failed run
    keeps the points in memory for the next one, and re-raises.
    """
    global _attempted_at
    from backend.common.agent_manager import get_agent_manager
    with _run_lock:
        _attempted_at = datetime.now(timezone.utc)
        started, clock = task_status.now_iso(), time.monotonic()
        failures = []
        result = {'agents': 0, 'appended': 0, 'compacted': 0}
        try:
            manager = get_agent_manager()
            system = manager.repository.get_system()
            agent_ids = [agent['id'] for agent in system.get('agents', [])]
        except Exception as error:
            failures.append(error)
        else:
            try:
                # 导入、恢复或重置配置后已不存在的 Agent，不再保存它们的历史
                manager.metrics_history.retain_agents(agent_ids)
                result = manager.metrics_history.flush()
            except Exception as error:
                failures.append(error)
            # 流量记录与监控历史一起保存；一项失败不影响另一项
            try:
                from backend.agents import traffic_history
                manager.traffic_history.retain_agents(agent_ids)
                result['traffic_rows'] = manager.traffic_history.flush(traffic_history.retention_days(system))['rows']
            except Exception as error:
                failures.append(TrafficHistoryFlushError(error))
        if failures:
            reason = '；'.join(str(error) or type(error).__name__ for error in failures)
            logger.warning('监控历史保存失败：%s', reason)
            _record(ok=False, started_at=started, trigger=trigger, error=reason,
                    duration_ms=(time.monotonic() - clock) * 1000)
            raise failures[0]
        if record_idle or result['appended'] or result['compacted'] or result.get('traffic_rows'):
            _record(ok=True, started_at=started, trigger=trigger, duration_ms=(time.monotonic() - clock) * 1000)
        return result


class TrafficHistoryFlushError(RuntimeError):
    def __init__(self, error):
        super().__init__(f'流量记录：{error}' if str(error) else f'流量记录：{type(error).__name__}')


def _record(**fields):
    try:
        task_status.record(TASK_KEY, **fields)
    except Exception as error:
        # 磁盘满时运行记录也可能写不进去；日志里仍能看到保存结果
        logger.warning('监控历史保存结果记录失败：%s', error)


def run_due(now=None):
    """Run once if the interval has passed; reads the interval every time."""
    due = next_run_at(task_status.get(TASK_KEY), interval_seconds())
    if due is None or due > (now or datetime.now(timezone.utc)):
        return False
    run('schedule')
    return True


def _loop():
    from backend.common.config import reset_config_context
    while not _stopping:
        try:
            run_due()
        except Exception:
            pass  # recorded in task_status and logged by run()
        finally:
            reset_config_context()
        _wake.wait(TICK_SECONDS)
        _wake.clear()


def load_history():
    """Read saved history back at service startup; adopts files from the old
    default directories and migrates the old format.

    Only the service process does this. Every AgentManager now keeps its history
    under its own repository's data directory, but other processes can still open
    the same DATA_DIR (the debug reloader's parent, command-line tools): load()
    deletes leftover compaction temp files and files of Agents that no longer
    exist, which would race the service's own saves.
    """
    from backend.common.agent_manager import get_agent_manager
    manager = get_agent_manager()
    history = manager.metrics_history
    try:
        agent_ids = [agent['id'] for agent in manager.repository.get_system().get('agents', [])]
    except Exception as error:
        logger.warning('监控历史加载失败：%s', error)
        return None
    try:
        history.adopt_legacy_dirs(agent_ids)
    except Exception as error:
        logger.warning('旧目录中的监控历史移入失败：%s', error)
    try:
        return history.load(agent_ids)
    except Exception as error:
        logger.warning('监控历史加载失败：%s', error)
        return None


def start_loop():
    """Start the per-process flush loop; memory is per process, so is the loop."""
    global _started_at
    with _start_guard:
        if _started_at is not None:
            return
        _started_at = datetime.now(timezone.utc)
    threading.Thread(target=_loop, name='metrics-history-flush', daemon=True).start()


def flush_on_shutdown(record_idle=True):
    """Final flush when the service stops; never raises.

    Marks the service as stopping first, so a stop signal that arrives during
    this flush (on the same main thread) does not try to take _run_lock again.
    """
    global _stopping, _shutdown_flushing
    _stopping = True
    _wake.set()
    _shutdown_flushing = True
    try:
        result = run('shutdown', record_idle=record_idle)
    except Exception:
        return None
    finally:
        _shutdown_flushing = False
    if result['appended']:
        logger.info('服务停止前已保存 %d 个监控点', result['appended'])
    return result


def handle_stop_signal(signum, frame):
    """SIGTERM / SIGINT: save first, then stop the server.

    Waitress (and the debug server) treat SystemExit like Ctrl+C and return
    from serve(); app.py then flushes once more for heartbeats that finished
    during shutdown and calls exit_after_stop_signal(). A repeated signal is
    ignored while a shutdown save runs; after it, the signal's default action
    ends the process, so a second Ctrl+C still force-quits a hung shutdown.
    """
    global _stop_signum
    if _stopping:
        if _shutdown_flushing:
            return None
        signal.signal(signum, signal.SIG_DFL)
        os.kill(os.getpid(), signum)
        return None
    _stop_signum = signum
    logger.info('收到停止信号 %s，正在保存监控历史', signal.Signals(signum).name)
    flush_on_shutdown()
    raise SystemExit(0)


def exit_after_stop_signal():
    """After the final save of a SIGTERM stop, exit without waiting for threads.

    SIGTERM used to end the process at once. Keep that: busy executor workers
    (subscription downloads, rule sync) would otherwise hold the process until
    supervisord's SIGKILL, using up docker stop's grace period. Ctrl+C keeps the
    normal interpreter exit.
    """
    if _stop_signum != signal.SIGTERM:
        return
    logging.shutdown()
    for stream in (sys.stdout, sys.stderr):
        try:
            stream.flush()
        except Exception:
            pass
    os._exit(0)


def install_signal_handlers():
    for signum in (signal.SIGTERM, signal.SIGINT):
        signal.signal(signum, handle_stop_signal)
