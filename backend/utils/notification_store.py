"""Persistent attention items, separate from configuration revisions and exports."""
from contextlib import contextmanager
from copy import deepcopy
from datetime import datetime, timezone
import fcntl
import hashlib
import json
import os
from pathlib import Path
import tempfile
import threading

_guard = threading.RLock()


def now():
    return datetime.now(timezone.utc).isoformat(timespec='microseconds')


def fingerprint(key, item):
    """Identify an incident, not a polling result or the time it was displayed."""
    if item.get('source') == 'router-check' and key.endswith(':changes'):
        # Report and preview describe the same unpublished configuration differently.
        fields = ('signature',)
    else:
        fields = ('title', 'message', 'severity', 'status_label', 'target')
        fields += ('signature',) if item.get('source') == 'router-check' else ('evidence',)
    value = {field: item.get(field) for field in fields}
    return hashlib.sha256(json.dumps(value, sort_keys=True, ensure_ascii=False).encode()).hexdigest()


def dismissed(key, item):
    # No notification body/history: just enough to avoid replaying an acknowledged receipt.
    return {'state': 'dismissed', 'fingerprint': fingerprint(key, item), 'source': item.get('source')}


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
            # Migrate old read/resolved records on first access, without replaying old failures.
            data['items'] = {key: dismissed(key, item) if item.get('read') else item
                             for key, item in data['items'].items()
                             if item.get('state') in ('pending', 'dismissed')}
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
    """Keep unread incidents; unknown/running does not count as recovery."""
    status = observation.pop('state')
    previous = items.get(key)
    if status == 'unknown':
        return
    if status in ('resolved', 'inactive'):
        items.pop(key, None)
        return
    identity = fingerprint(key, observation)
    if previous and previous['state'] == 'dismissed' and previous['fingerprint'] == identity:
        return
    stamp = now()
    evidence = observation.pop('evidence', '')
    if previous and previous['state'] == 'pending':
        changed = fingerprint(key, previous) != identity
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
                items[key] = dismissed(key, items[key])
