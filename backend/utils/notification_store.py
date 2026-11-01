"""Persistent attention items, separate from configuration revisions and exports."""
from contextlib import contextmanager
from copy import deepcopy
from datetime import datetime, timedelta, timezone
import fcntl
import json
import os
from pathlib import Path
import tempfile
import threading

_guard = threading.RLock()
RETENTION_DAYS = 30


def now():
    return datetime.now(timezone.utc).isoformat(timespec='microseconds')


@contextmanager
def transaction(data_dir):
    path = Path(data_dir) / 'notifications.json'
    with _guard:
        path.parent.mkdir(parents=True, exist_ok=True)
        with open(path.with_suffix('.lock'), 'a') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX)
            try:
                data = json.loads(path.read_text(encoding='utf-8'))
                if not isinstance(data, dict) or not isinstance(data.get('items'), dict):
                    raise ValueError('Invalid notification store')
            except FileNotFoundError:
                data = {'items': {}}
            before = deepcopy(data)
            cutoff = (datetime.now(timezone.utc) - timedelta(days=RETENTION_DAYS)).isoformat()
            data['items'] = {key: item for key, item in data['items'].items()
                             if item.get('state') == 'pending' or item.get('resolved_at', '') >= cutoff}
            yield data['items']
            if data != before:
                fd, temporary = tempfile.mkstemp(dir=path.parent, prefix='.notifications-')
                try:
                    with os.fdopen(fd, 'w', encoding='utf-8') as handle:
                        json.dump(data, handle, ensure_ascii=False, separators=(',', ':'))
                        handle.flush()
                        os.fsync(handle.fileno())
                    os.replace(temporary, path)
                finally:
                    if os.path.exists(temporary):
                        os.unlink(temporary)


def observe(items, key, observation):
    """Unknown/running never resolves an issue. Repeated receipts never add unread items."""
    status = observation.pop('state')
    previous = items.get(key)
    if status == 'unknown':
        return
    if status in ('resolved', 'inactive'):
        if previous and previous['state'] == 'pending':
            previous.update(state='resolved', resolved_at=now(),
                            resolution='已不再适用' if status == 'inactive' else '已恢复')
        return
    stamp = now()
    evidence = observation.pop('evidence', '')
    if previous and previous['state'] == 'pending':
        changed = any(previous.get(field) != observation.get(field) for field in ('title', 'message', 'severity', 'status_label', 'target'))
        previous.update(observation)
        if evidence != previous.get('evidence'):
            previous['occurrences'] += 1
            previous['last_seen_at'] = observation.get('checked_at') or stamp
        if changed:
            previous.update(read=False, version=stamp)
        previous['evidence'] = evidence
    else:
        items[key] = {**observation, 'id': key, 'state': 'pending', 'read': False,
                      'version': stamp, 'first_seen_at': stamp,
                      'last_seen_at': observation.get('checked_at') or stamp,
                      'occurrences': 1, 'evidence': evidence}


def mark_read(data_dir, versions):
    with transaction(data_dir) as items:
        for key, version in versions.items():
            if key in items and items[key].get('version') == version:
                items[key]['read'] = True
