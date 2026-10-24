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
    monkeypatch.setattr(metrics_flush, '_stopping', False)
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


def test_stop_signal_handler_saves_then_exits(tasks_app, monkeypatch):
    _, _, manager = tasks_app
    manager.update_heartbeat('agent_1', {'system_metrics': metrics(1)})
    with pytest.raises(SystemExit):
        metrics_flush.handle_stop_signal(signal.SIGTERM, None)
    assert manager.metrics_history.pending_count() == 0
    assert task_status.get('metrics_history_flush')['last_trigger'] == 'shutdown'
    # A second signal while stopping does not interrupt the save in progress.
    assert metrics_flush.handle_stop_signal(signal.SIGTERM, None) is None


SHUTDOWN_SCRIPT = r'''
import os, sys
from backend.agents import manager
from backend.agents.metrics_history import MetricsHistory
manager.MetricsHistory = lambda: MetricsHistory(os.path.join(os.environ['DATA_DIR'], 'metrics'))
from backend import http_server
original = http_server.server_options

def options():
    # Runs inside the real run_http_server, after app.py installed its handlers.
    from backend.common.agent_manager import get_agent_manager
    agents = get_agent_manager()
    agents.repository.update_system_transaction(lambda system: system.update(agents=[
        {'id': 'agent_1', 'name': 'Edge', 'service_type': 'mihomo', 'profile_id': 'default', 'token': 'agent-token-0123456789abcdef', 'host': 'h', 'port': 8080}]))
    assert agents.update_heartbeat('agent_1', {'system_metrics': {'cpu': {'usage_percent': 7}}})
    print('ready', flush=True)
    return {**original(), 'host': '127.0.0.1', 'port': 0}

http_server.server_options = options
import runpy
runpy.run_module('backend.app', run_name='__main__', alter_sys=True)
print('exited', flush=True)
'''


@pytest.mark.parametrize('signum', [signal.SIGTERM, signal.SIGINT])
def test_real_startup_path_flushes_on_stop_signal(tmp_path, signum):
    env = dict(os.environ, DATA_DIR=str(tmp_path), LOG_FILE=str(tmp_path / 'app.log'), LOG_LEVEL='INFO',
               ADMIN_USERNAME='', ADMIN_PASSWORD='', PYTHONDONTWRITEBYTECODE='1', PYTHONUNBUFFERED='1')
    process = subprocess.Popen([sys.executable, '-c', SHUTDOWN_SCRIPT], cwd=Path(__file__).resolve().parents[1],
                               env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    try:
        for line in process.stdout:
            if line.strip() == 'ready':
                break
        else:
            pytest.fail('server did not start: ' + process.stderr.read())
        # Give waitress a moment to enter its loop; the handler works either way.
        threading.Event().wait(0.5)
        process.send_signal(signum)
        out, err = process.communicate(timeout=60)
    finally:
        if process.poll() is None:
            process.kill()
    assert process.returncode == 0, err
    assert 'exited' in out
    # The handler app.py installed ran, not just the final flush after serve().
    assert f'收到停止信号 {signal.Signals(signum).name}' in err + (tmp_path / 'app.log').read_text(encoding='utf-8')
    saved = lines(tmp_path / 'metrics' / 'agent_1_metrics.jsonl')
    assert [json.loads(line)['cpu'] for line in saved] == [{'usage_percent': 7}]
    status = json.loads((tmp_path / 'task-status.json').read_text(encoding='utf-8'))['metrics_history_flush']
    assert (status['last_status'], status['last_trigger']) == ('success', 'shutdown')
