"""Results of background tasks, shown in 系统设置 → 定时任务.

Runtime receipts live beside the configuration, not inside it: they are not
part of revisions, exports or backups, and survive restarts so the page can
still say why the last automatic run failed after a reload.
"""
from contextlib import contextmanager
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import tempfile
import threading

FILENAME = 'task-status.json'
MAX_ERROR = 2000

_guard = threading.Lock()


def now_iso():
    return datetime.now(timezone.utc).isoformat(timespec='seconds').replace('+00:00', 'Z')


def parse_time(value):
    """Aware datetime for an ISO timestamp, or None."""
    if not isinstance(value, str) or not value:
        return None
    try:
        parsed = datetime.fromisoformat(value.replace('Z', '+00:00'))
    except ValueError:
        return None
    return parsed if parsed.tzinfo else parsed.replace(tzinfo=timezone.utc)


def _path():
    from backend.common.config import get_repository
    return Path(get_repository().data_dir) / FILENAME


@contextmanager
def _locked(path):
    import fcntl
    with _guard:
        path.parent.mkdir(parents=True, exist_ok=True)
        with open(path.with_name(path.name + '.lock'), 'a') as handle:
            fcntl.flock(handle, fcntl.LOCK_EX)
            yield


def _read(path):
    try:
        value = json.loads(path.read_text(encoding='utf-8'))
    except FileNotFoundError:
        return {}
    except (OSError, ValueError):
        return {}
    return value if isinstance(value, dict) else {}


def _write(path, value):
    descriptor, name = tempfile.mkstemp(dir=path.parent, prefix='.task-status-')
    try:
        with os.fdopen(descriptor, 'w', encoding='utf-8') as handle:
            json.dump(value, handle, ensure_ascii=False, separators=(',', ':'))
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(name, path)
    finally:
        if os.path.exists(name):
            os.unlink(name)


def load():
    path = _path()
    with _locked(path):
        return _read(path)


def get(key):
    value = load().get(key)
    return value if isinstance(value, dict) else {}


def record(key, *, ok, started_at=None, duration_ms=0, error='', trigger='schedule',
           cache_retained=None, cache_updated_at=None, skipped=False):
    """Append one finished run. A skipped run (nothing to do, or a
    prerequisite is missing) keeps the previous success and failure counts."""
    path = _path()
    with _locked(path):
        data = _read(path)
        entry = data.get(key) if isinstance(data.get(key), dict) else {}
        finished = now_iso()
        status = 'skipped' if skipped else ('success' if ok else 'failed')
        entry.update(last_run_at=started_at or finished, last_finished_at=finished, last_status=status,
                     last_duration_ms=max(0, int(duration_ms or 0)), last_trigger=trigger,
                     last_error='' if ok else str(error or '')[:MAX_ERROR])
        if not skipped:
            entry['run_count'] = int(entry.get('run_count') or 0) + 1
            if ok:
                entry['last_success_at'] = finished
                entry['consecutive_failures'] = 0
            else:
                entry['failure_count'] = int(entry.get('failure_count') or 0) + 1
                entry['consecutive_failures'] = int(entry.get('consecutive_failures') or 0) + 1
        if ok or skipped:
            entry.pop('cache_retained', None)
        elif cache_retained is not None:
            entry['cache_retained'] = bool(cache_retained)
        if cache_updated_at:
            entry['cache_updated_at'] = cache_updated_at
        data[key] = entry
        _write(path, data)
        result = dict(entry)
    from backend.notifications import safely, refresh_tasks
    safely(refresh_tasks, key)
    return result


def forget(*keys):
    path = _path()
    with _locked(path):
        data = _read(path)
        removed = [key for key in keys if data.pop(key, None) is not None]
        if removed:
            _write(path, data)
