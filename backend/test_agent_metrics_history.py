"""Agent monitoring history: memory window, append-only saves, compaction,
migration, deletion, the 定时任务 row and the flush on graceful shutdown."""
import errno
import json
import os
import signal
import subprocess
import sys
import threading
from datetime import datetime, timedelta, timezone
from pathlib import Path

import pytest

from backend.agents import metrics_flush
from backend.agents import metrics_history as module
from backend.agents.metrics_history import MetricsFlushError, MetricsHistory
from backend.common import config as config_module
from backend.test_qa_integrity import make_app
from backend.utils import task_status

START = datetime(2026, 10, 24, 12, 0, 0)


class Clock(datetime):
    current = START

    @classmethod
    def now(cls, tz=None):
        return cls.current if tz is None else cls.current.replace(tzinfo=tz)


@pytest.fixture
def clock(monkeypatch):
    Clock.current = START
    monkeypatch.setattr(module, 'datetime', Clock)
    return Clock


def metrics(i):
    return {'cpu': {'usage_percent': i}, 'memory': {'used_percent': 40 + i}, 'disk': {'used_percent': 50},
            'network': {'bytes_sent': 1000 * i, 'bytes_recv': 2000 * i, 'speed_sent': i, 'speed_recv': 2 * i},
            'collected_at': 'ignored'}


def add(history, clock, count, agent='a1', step=30):
    for i in range(count):
        clock.current += timedelta(seconds=step)
        assert history.add_metrics(agent, metrics(i))


def lines(path):
    return Path(path).read_text(encoding='utf-8').splitlines()


def test_memory_keeps_24_hours_and_caps_points_per_agent(tmp_path, clock):
    history = MetricsHistory(str(tmp_path), max_points=5)
    add(history, clock, 8, step=1)
    kept = history.get_metrics('a1')
    assert [point['cpu']['usage_percent'] for point in kept] == [3, 4, 5, 6, 7]
    assert history.pending_count() == 5
    assert set(kept[0]) == {'timestamp', 'cpu', 'memory', 'disk', 'network'}
    assert not list(tmp_path.iterdir())  # heartbeats never touch the disk

    history = MetricsHistory(str(tmp_path / 'window'))
    add(history, clock, 3, step=3600 * 10)
    assert len(history.get_metrics('a1', hours=24)) == 3
    assert len(history.get_metrics('a1', hours=1)) == 1
    clock.current += timedelta(hours=15)
    assert [point['cpu']['usage_percent'] for point in history.get_metrics('a1')] == [2]
    assert history.pending_count() == 1


def test_flush_appends_only_new_points(tmp_path, clock):
    history = MetricsHistory(str(tmp_path))
    add(history, clock, 3)
    assert history.flush() == {'agents': 1, 'appended': 3, 'compacted': 0}
    path = tmp_path / 'a1_metrics.jsonl'
    first = path.read_bytes()
    assert [json.loads(line) for line in lines(path)] == history.get_metrics('a1')

    assert history.flush() == {'agents': 0, 'appended': 0, 'compacted': 0}
    assert path.read_bytes() == first
    add(history, clock, 2)
    history.flush()
    assert path.read_bytes().startswith(first)
    assert len(lines(path)) == 5
    assert [json.loads(line) for line in lines(path)] == history.get_metrics('a1')


def test_compaction_is_size_triggered_and_atomic(tmp_path, clock, monkeypatch):
    history = MetricsHistory(str(tmp_path))
    add(history, clock, 4, step=3600 * 6)
    history.flush()
    path = tmp_path / 'a1_metrics.jsonl'
    # Two points expire: 5 lines on disk for 3 retained points.
    clock.current += timedelta(hours=13)
    add(history, clock, 1)
    assert len(history.get_metrics('a1')) == 3
    original = path.read_bytes()

    def full_disk(*args):
        raise OSError(errno.ENOSPC, 'No space left on device')
    monkeypatch.setattr(module.os, 'replace', full_disk)
    with pytest.raises(MetricsFlushError, match='磁盘空间不足'):
        history.flush()
    assert path.read_bytes() == original
    assert [item.name for item in tmp_path.iterdir()] == ['a1_metrics.jsonl']
    monkeypatch.undo()
    monkeypatch.setattr(module, 'datetime', Clock)

    replaced = []
    real_replace = os.replace
    monkeypatch.setattr(module.os, 'replace', lambda source, target: (replaced.append(target), real_replace(source, target)))
    assert history.flush() == {'agents': 1, 'appended': 1, 'compacted': 1}
    assert replaced == [str(path)]
    assert [json.loads(line) for line in lines(path)] == history.get_metrics('a1')
    assert [item.name for item in tmp_path.iterdir()] == ['a1_metrics.jsonl']
    # Below the threshold again: the next save appends.
    add(history, clock, 1)
    assert history.flush()['compacted'] == 0
    assert len(lines(path)) == 4


def test_failed_append_keeps_points_and_rewrites_next_time(tmp_path, clock, monkeypatch):
    history = MetricsHistory(str(tmp_path))
    add(history, clock, 2)
    history.flush()

    def partial(path, new_lines):
        with open(path, 'a', encoding='utf-8') as handle:
            handle.write(new_lines[0][:10])
        raise PermissionError(errno.EACCES, 'Permission denied')
    add(history, clock, 2)
    monkeypatch.setattr(history, '_append', partial)
    with pytest.raises(MetricsFlushError, match='没有写入权限'):
        history.flush()
    assert history.pending_count() == 2
    monkeypatch.undo()
    monkeypatch.setattr(module, 'datetime', Clock)
    assert history.flush()['compacted'] == 1
    assert [json.loads(line) for line in lines(tmp_path / 'a1_metrics.jsonl')] == history.get_metrics('a1')
    assert len(history.get_metrics('a1')) == 4


def test_loading_skips_a_truncated_last_line_and_old_points(tmp_path, clock):
    path = tmp_path / 'a1_metrics.jsonl'
    old = {'timestamp': (START - timedelta(hours=30)).isoformat(), 'cpu': {}, 'memory': {}, 'disk': {}, 'network': {}}
    good = [{'timestamp': (START - timedelta(minutes=m)).isoformat(), 'cpu': {'usage_percent': m},
             'memory': {}, 'disk': {}, 'network': {}} for m in range(6, 0, -1)]
    path.write_text('\n'.join(json.dumps(item) for item in [old, *good]) + '\n{"timestamp": "2026-10-2',
                    encoding='utf-8')
    history = MetricsHistory(str(tmp_path))
    assert history.load(['a1'])['loaded'] == 1
    assert history.get_metrics('a1') == good
    add(history, clock, 1)
    history.flush()
    # The new line starts on its own line instead of joining the broken one.
    assert lines(path)[-2] == '{"timestamp": "2026-10-2'
    reloaded = MetricsHistory(str(tmp_path))
    reloaded.load(['a1'])
    assert reloaded.get_metrics('a1') == history.get_metrics('a1')
    assert len(reloaded.get_metrics('a1')) == 7


def test_old_json_files_migrate_once_and_deleted_agents_are_removed(tmp_path, clock):
    recent = [{'timestamp': (START - timedelta(minutes=m)).isoformat(), 'cpu': {'usage_percent': 1.5, 'cores': 4},
               'memory': {'used_percent': 33.3}, 'disk': {}, 'network': {'bytes_sent': 10, 'speed_sent': 0.5}}
              for m in (3, 2, 1)]
    expired = {**recent[0], 'timestamp': (START - timedelta(hours=25)).isoformat()}
    for agent in ('a1', 'gone'):
        (tmp_path / f'{agent}_metrics.json').write_text(json.dumps(
            {'agent_id': agent, 'updated_at': START.isoformat(), 'metrics': [expired, *recent]}, indent=2), encoding='utf-8')
    (tmp_path / 'gone_metrics.jsonl').write_text(json.dumps(recent[0]) + '\n', encoding='utf-8')
    (tmp_path / '.a1_interrupted.tmp').write_text('partial', encoding='utf-8')

    history = MetricsHistory(str(tmp_path))
    assert history.load(['a1', 'never-reported']) == {'loaded': 1, 'migrated': 1, 'removed': 2}
    assert sorted(item.name for item in tmp_path.iterdir()) == ['a1_metrics.jsonl']
    # Same points, same shape as the old file served before.
    assert json.dumps(history.get_metrics('a1', hours=24)) == json.dumps(recent)
    assert [json.loads(line) for line in lines(tmp_path / 'a1_metrics.jsonl')] == recent
    assert history.pending_count() == 0

    # A migration interrupted after the new file was written keeps the new file.
    (tmp_path / 'a1_metrics.json').write_text(json.dumps({'metrics': [expired]}), encoding='utf-8')
    again = MetricsHistory(str(tmp_path))
    assert again.load(['a1']) == {'loaded': 1, 'migrated': 0, 'removed': 0}
    assert again.get_metrics('a1') == recent
    assert sorted(item.name for item in tmp_path.iterdir()) == ['a1_metrics.jsonl']


def test_api_shapes_match_the_points_kept_in_memory(tmp_path, clock):
    history = MetricsHistory(str(tmp_path))
    add(history, clock, 12)
    points = history.get_metrics('a1', hours=24)
    clock.current += timedelta(seconds=1)
    summary = history.get_metrics_summary('a1', hours=1)
    assert summary['data_points'] == 12 and summary['cpu'] == {'avg': 5.5, 'max': 11, 'min': 0}
    stats = history.get_traffic_stats('a1', period='hours_24')
    assert (stats['bytes_sent'], stats['bytes_sent_delta'], stats['data_points']) == (11000, 11000, 12)
    assert stats['start_time'] == points[0]['timestamp'] and stats['end_time'] == points[-1]['timestamp']
    trend = history.get_traffic_trend('a1', hours=24, interval_minutes=5)
    assert trend[-1]['timestamp'] == points[-1]['timestamp']
    # Each history point is exactly what a JSON round trip of the heartbeat gives.
    assert points[0] == json.loads(json.dumps({key: metrics(0)[key] for key in ('cpu', 'memory', 'disk', 'network')}
                                              | {'timestamp': points[0]['timestamp']}))


def agent_manager(tmp_path):
    from backend.agents.manager import AgentManager
    _, repo = make_app(tmp_path / 'config')
    repo.update_system_transaction(lambda system: system.update(agents=[
        {'id': 'agent_1', 'name': 'Edge', 'service_type': 'mihomo', 'profile_id': 'default', 'token': 'agent-token-0123456789abcdef', 'host': 'h', 'port': 8080,
         'last_heartbeat': datetime.now().isoformat()}]))
    return AgentManager(repo), repo


def test_heartbeats_stay_in_memory_and_deleting_an_agent_drops_its_history(tmp_path):
    manager, _ = agent_manager(tmp_path)
    try:
        assert manager.update_heartbeat('agent_1', {'system_metrics': metrics(1)})
        history = manager.metrics_history
        path = Path(history.data_dir) / 'agent_1_metrics.jsonl'
        assert not path.exists() and len(history.get_metrics('agent_1')) == 1
        history.flush()
        assert path.exists()
        assert manager.delete_agent('agent_1')
        assert not path.exists() and history.get_metrics('agent_1') == []
        assert history.flush() == {'agents': 0, 'appended': 0, 'compacted': 0}
    finally:
        config_module.reset_config_context()


@pytest.fixture
def tasks_app(tmp_path, monkeypatch):
    app, repo = make_app(tmp_path / 'config')
    repo.update_system_transaction(lambda system: system.update(agents=[
        {'id': 'agent_1', 'name': 'Edge', 'service_type': 'mihomo', 'profile_id': 'default', 'token': 'agent-token-0123456789abcdef', 'host': 'h', 'port': 8080}]))
    from backend.common import agent_manager as singleton
    monkeypatch.setattr(singleton, '_agent_manager', None)
    monkeypatch.setattr(metrics_flush, '_started_at', None)
    for name, value in (('_stopping', False), ('_shutdown_flushing', False), ('_stop_signum', None),
                        ('_attempted_at', None)):
        monkeypatch.setattr(metrics_flush, name, value)
    yield app, repo, singleton.get_agent_manager()
    config_module.reset_config_context()


def task_row(app):
    response = app.test_client().get('/api/tasks')
    assert response.status_code == 200, response.get_data(as_text=True)
    return next(row for row in response.get_json()['tasks'] if row['key'] == 'metrics_history_flush')


def wait_background():
    for thread in threading.enumerate():
        if thread.name.startswith('task-'):
            thread.join(timeout=10)


def test_task_row_runs_now_and_surfaces_failures(tasks_app, monkeypatch):
    app, _, manager = tasks_app
    row = task_row(app)
    assert (row['category'], row['name'], row['interval_seconds'], row['editable'], row['can_run'], row['enabled']) == \
        ('metrics_history', '监控历史保存', 1800, True, True, True)
    assert (row['last_run_at'], row['next_run_at'], row['link'], row['profile_id']) == (None, None, '/agents', None)

    manager.update_heartbeat('agent_1', {'system_metrics': metrics(1)})
    response = app.test_client().post('/api/tasks/run', json={'key': 'metrics_history_flush'})
    assert response.status_code == 202
    wait_background()
    row = task_row(app)
    assert (row['last_status'], row['last_trigger'], row['run_count']) == ('success', 'manual', 1)
    assert len(lines(Path(manager.metrics_history.data_dir) / 'agent_1_metrics.jsonl')) == 1

    def read_only(*args):
        raise OSError(errno.EROFS, 'Read-only file system')
    monkeypatch.setattr(manager.metrics_history, '_append', read_only)
    manager.update_heartbeat('agent_1', {'system_metrics': metrics(2)})
    assert app.test_client().post('/api/tasks/run', json={'key': 'metrics_history_flush'}).status_code == 202
    wait_background()
    row = task_row(app)
    assert (row['last_status'], row['failure_count']) == ('failed', 1)
    assert '文件系统只读' in row['last_error'] and '下次保存时重试' in row['last_error']
    assert manager.metrics_history.pending_count() == 1


def test_interval_accepts_only_the_offered_choices_and_reschedules(tasks_app):
    app, repo, _ = tasks_app
    client = app.test_client()
    for value in (120, 900, '600', True, None, 0):
        response = client.put('/api/tasks/interval', json={'key': 'metrics_history_flush', 'interval_seconds': value})
        assert response.status_code == 400
        assert response.get_json()['message'] == '保存间隔只能是 5、10、30 或 60 分钟'
    assert metrics_flush.SETTING not in repo.get_system()['system_config']

    metrics_flush._started_at = started = datetime(2026, 10, 24, tzinfo=timezone.utc)
    assert task_row(app)['next_run_at'] == '2026-10-24T00:30:00Z'
    assert not metrics_flush.run_due(started + timedelta(minutes=10))
    metrics_flush._wake.clear()
    response = client.put('/api/tasks/interval', json={'key': 'metrics_history_flush', 'interval_seconds': 300})
    assert response.status_code == 200
    assert response.get_json()['task']['interval_seconds'] == 300
    assert repo.get_system()['system_config'][metrics_flush.SETTING] == 300
    assert metrics_flush._wake.is_set()  # the loop recomputes without a restart
    assert metrics_flush.run_due(started + timedelta(minutes=10))
    assert task_status.get('metrics_history_flush')['last_trigger'] == 'schedule'
    # An unexpected stored value falls back to the default instead of breaking the loop.
    repo.update_system_transaction(lambda system: system['system_config'].update({metrics_flush.SETTING: 7}))
    assert metrics_flush.interval_seconds() == 1800


def test_failed_status_record_does_not_rerun_every_tick(tasks_app, monkeypatch):
    _, _, manager = tasks_app

    def disk_full(*args, **kwargs):
        raise OSError(errno.ENOSPC, 'No space left on device')
    monkeypatch.setattr(task_status, 'record', disk_full)
    now = datetime.now(timezone.utc)
    metrics_flush._started_at = now - timedelta(minutes=31)
    manager.update_heartbeat('agent_1', {'system_metrics': metrics(1)})
    assert metrics_flush.run_due(now)
    assert task_status.get('metrics_history_flush').get('last_run_at') is None
    # The next tick waits a full interval from this attempt, not from startup.
    assert not metrics_flush.run_due(now + timedelta(seconds=metrics_flush.TICK_SECONDS))
    assert metrics_flush.run_due(now + timedelta(minutes=31))


def test_history_of_agents_removed_by_import_is_dropped_on_the_next_save(tasks_app):
    _, repo, manager = tasks_app
    history = manager.metrics_history
    manager.update_heartbeat('agent_1', {'system_metrics': metrics(1)})
    metrics_flush.run('manual')
    path = Path(history.data_dir) / 'agent_1_metrics.jsonl'
    assert path.exists()
    # Backup restore / reset replace the Agent list without deleting Agents.
    repo.update_system_transaction(lambda system: system.update(agents=[]))
    metrics_flush.run('manual')
    assert not path.exists() and history.get_metrics('agent_1') == [] and history.pending_count() == 0
    assert history.flush() == {'agents': 0, 'appended': 0, 'compacted': 0}


def test_building_an_agent_manager_does_not_touch_saved_history(tmp_path):
    # Tests, integration harnesses and other repositories build AgentManagers
    # that share the checkout's default directory; only the service loads it.
    metrics_dir = tmp_path / 'metrics'
    metrics_dir.mkdir()
    (metrics_dir / 'someone_else_metrics.jsonl').write_text('{"timestamp":"2026-10-24T12:00:00"}\n', encoding='utf-8')
    (metrics_dir / 'agent_1_metrics.json').write_text('{"metrics":[]}', encoding='utf-8')
    manager, _ = agent_manager(tmp_path)
    config_module.reset_config_context()
    assert manager.metrics_history.data_dir == str(metrics_dir)
    assert sorted(path.name for path in metrics_dir.iterdir()) == ['agent_1_metrics.json', 'someone_else_metrics.jsonl']


def test_points_are_kept_as_compact_lines_and_read_back_as_fresh_dicts(tmp_path, clock):
    import tracemalloc
    history = MetricsHistory(str(tmp_path))
    sample = {'cpu': {'usage_percent': 12.5, 'core_count': 4},
              'memory': {'total': 8123456789, 'used': 4123456789, 'available': 4000000000, 'used_percent': 50.7},
              'disk': {'total': 256123456789, 'used': 100123456789, 'free': 156000000000, 'used_percent': 39.1},
              'network': {'bytes_sent': 123456789012, 'bytes_recv': 987654321098, 'speed_sent': 12345, 'speed_recv': 54321}}
    tracemalloc.start()
    try:
        before = tracemalloc.take_snapshot()
        for i in range(2000):
            clock.current += timedelta(seconds=30)
            history.add_metrics('a1', {**sample, 'network': {**sample['network'], 'bytes_sent': i}})
        grown = sum(stat.size_diff for stat in tracemalloc.take_snapshot().compare_to(before, 'filename'))
    finally:
        tracemalloc.stop()
    # A decoded nested dict per point costs about 2.4 KB; the JSON line about 0.5 KB.
    assert grown / 2000 < 1200
    first = history.get_metrics('a1')[0]
    first['cpu']['usage_percent'] = 99
    assert history.get_metrics('a1')[0]['cpu']['usage_percent'] == 12.5


def test_stop_signal_handler_saves_then_exits(tasks_app, monkeypatch):
    _, _, manager = tasks_app
    manager.update_heartbeat('agent_1', {'system_metrics': metrics(1)})
    with pytest.raises(SystemExit):
        metrics_flush.handle_stop_signal(signal.SIGTERM, None)
    assert manager.metrics_history.pending_count() == 0
    assert task_status.get('metrics_history_flush')['last_trigger'] == 'shutdown'
    # Once the save is done, a repeated signal takes its default action, so a
    # second Ctrl+C still force-quits a shutdown that hangs on busy threads.
    actions = []
    monkeypatch.setattr(metrics_flush.signal, 'signal', lambda signum, handler: actions.append((signum, handler)))
    monkeypatch.setattr(metrics_flush.os, 'kill', lambda pid, signum: actions.append((pid, signum)))
    assert metrics_flush.handle_stop_signal(signal.SIGINT, None) is None
    assert actions == [(signal.SIGINT, signal.SIG_DFL), (os.getpid(), signal.SIGINT)]


def test_stop_signal_during_a_shutdown_save_is_ignored_instead_of_deadlocking(tasks_app, monkeypatch):
    # app.py's final flush can run without the handler (the debug server's own
    # SIGTERM handler, a port already in use); a signal then arrives on the
    # same thread that holds _run_lock.
    _, _, manager = tasks_app
    history = manager.metrics_history
    original, outcome = history.flush, []

    def flush_interrupted_by_signal():
        try:
            outcome.append(metrics_flush.handle_stop_signal(signal.SIGINT, None))
        except BaseException as error:
            outcome.append(error)
        return original()
    monkeypatch.setattr(history, 'flush', flush_interrupted_by_signal)
    manager.update_heartbeat('agent_1', {'system_metrics': metrics(1)})
    worker = threading.Thread(target=metrics_flush.flush_on_shutdown, kwargs={'record_idle': False}, daemon=True)
    worker.start()
    worker.join(timeout=10)
    assert not worker.is_alive(), 'stop signal deadlocked on the shutdown save'
    assert outcome == [None] and history.pending_count() == 0


SHUTDOWN_SCRIPT = r'''
import json, os, sys, time
from concurrent.futures import ThreadPoolExecutor
from backend.agents import manager
from backend.agents.metrics_history import MetricsHistory
manager.MetricsHistory = lambda: MetricsHistory(os.path.join(os.environ['DATA_DIR'], 'metrics'))
from backend.common.config import get_repository
# The Agent exists before startup, so the service loads (and migrates) its history.
get_repository().update_system_transaction(lambda system: system.update(agents=[
    {'id': 'agent_1', 'name': 'Edge', 'service_type': 'mihomo', 'profile_id': 'default', 'token': 'agent-token-0123456789abcdef', 'host': 'h', 'port': 8080}]))
busy = ThreadPoolExecutor(1)


def heartbeat():
    from backend.common.agent_manager import get_agent_manager
    assert get_agent_manager().update_heartbeat('agent_1', {'system_metrics': {'cpu': {'usage_percent': 7}}})
    # A download still running in a worker thread when the service stops.
    busy.submit(time.sleep, 120)
    print('ready', flush=True)


from backend import http_server
original = http_server.server_options

def options():
    # Runs inside the real run_http_server, after app.py installed its handlers.
    heartbeat()
    return {**original(), 'host': '127.0.0.1', 'port': 0}

http_server.server_options = options
import flask
flask_run = flask.Flask.run

def debug_run(self, host=None, port=None, **kwargs):
    # LOG_LEVEL=DEBUG: the reloader re-runs this script as the serving child.
    if os.environ.get('WERKZEUG_RUN_MAIN') == 'true':
        heartbeat()
    return flask_run(self, host='127.0.0.1', port=0, **kwargs)

flask.Flask.run = debug_run
import runpy
runpy.run_module('backend.app', run_name='__main__', alter_sys=True)
print('exited', flush=True)
'''


def start_service(tmp_path, log_level):
    metrics_dir = tmp_path / 'metrics'
    metrics_dir.mkdir()
    # An old-format file to migrate, and a deleted Agent's file to remove.
    old_point = {'timestamp': (datetime.now() - timedelta(minutes=1)).isoformat(), 'cpu': {'usage_percent': 3},
                 'memory': {}, 'disk': {}, 'network': {}}
    (metrics_dir / 'agent_1_metrics.json').write_text(json.dumps({'agent_id': 'agent_1', 'metrics': [old_point]}),
                                                      encoding='utf-8')
    (metrics_dir / 'deleted_metrics.jsonl').write_text(json.dumps(old_point) + '\n', encoding='utf-8')
    env = dict(os.environ, DATA_DIR=str(tmp_path), LOG_FILE=str(tmp_path / 'app.log'), LOG_LEVEL=log_level,
               ADMIN_USERNAME='', ADMIN_PASSWORD='', PYTHONDONTWRITEBYTECODE='1', PYTHONUNBUFFERED='1')
    env.pop('WERKZEUG_RUN_MAIN', None)
    env.pop('WERKZEUG_SERVER_FD', None)
    process = subprocess.Popen([sys.executable, '-c', SHUTDOWN_SCRIPT], cwd=Path(__file__).resolve().parents[1],
                               env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    for line in process.stdout:
        if line.strip() == 'ready':
            break
    else:
        process.kill()
        pytest.fail('server did not start: ' + process.stderr.read())
    # Give the server a moment to enter its loop; the handlers work either way.
    threading.Event().wait(0.5)
    return process


def assert_saved(tmp_path):
    metrics_dir = tmp_path / 'metrics'
    assert sorted(path.name for path in metrics_dir.iterdir()) == ['agent_1_metrics.jsonl']
    saved = lines(metrics_dir / 'agent_1_metrics.jsonl')
    assert [json.loads(line)['cpu'] for line in saved] == [{'usage_percent': 3}, {'usage_percent': 7}]
    status = json.loads((tmp_path / 'task-status.json').read_text(encoding='utf-8'))['metrics_history_flush']
    assert (status['last_status'], status['last_trigger']) == ('success', 'shutdown')


def test_real_startup_path_saves_on_sigterm_without_waiting_for_busy_threads(tmp_path):
    process = start_service(tmp_path, 'INFO')
    try:
        process.send_signal(signal.SIGTERM)
        # Busy executor threads would hold the process for 120 s.
        out, err = process.communicate(timeout=60)
    finally:
        if process.poll() is None:
            process.kill()
    assert process.returncode == 0, err
    # The handler app.py installed ran, not just the final flush after serve().
    assert '收到停止信号 SIGTERM' in err + (tmp_path / 'app.log').read_text(encoding='utf-8')
    assert_saved(tmp_path)


def test_real_startup_path_saves_on_ctrl_c_and_a_second_one_force_quits(tmp_path):
    process = start_service(tmp_path, 'INFO')
    try:
        process.send_signal(signal.SIGINT)
        for line in process.stdout:
            if line.strip() == 'exited':
                break
        # The interpreter now waits for the busy worker; Ctrl+C again ends it.
        assert process.poll() is None
        assert_saved(tmp_path)
        process.send_signal(signal.SIGINT)
        process.communicate(timeout=60)
    finally:
        if process.poll() is None:
            process.kill()
    assert process.returncode == -signal.SIGINT
    assert '收到停止信号 SIGINT' in (tmp_path / 'app.log').read_text(encoding='utf-8')


def test_debug_mode_stop_reaches_the_serving_child_and_saves(tmp_path):
    # LOG_LEVEL=DEBUG: supervisord / docker stop signal the reloader parent.
    process = start_service(tmp_path, 'DEBUG')
    try:
        process.send_signal(signal.SIGTERM)
        out, err = process.communicate(timeout=60)
    finally:
        if process.poll() is None:
            process.kill()
    assert process.returncode == 0, err
    # The child ran app.py's handler (Werkzeug's own one would not log this).
    assert '收到停止信号 SIGTERM' in (tmp_path / 'app.log').read_text(encoding='utf-8')
    assert_saved(tmp_path)

