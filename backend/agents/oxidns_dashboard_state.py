"""Small, durable DNS dashboard baselines; never store query records here."""
from contextlib import contextmanager
import hashlib
import json
import math
import os
from pathlib import Path
import re
import tempfile
import threading
import time

MAX_STATE_BYTES = 4 * 1024 * 1024
MAX_ROWS = 10_000
LOCK_TIMEOUT = 5
_REASONS = {'initial', 'manual', 'core_changed', 'configuration_changed', 'target_changed', 'counters_changed', 'publication'}
# A fixed set keeps lock bookkeeping bounded as Agents are added and removed.
_LOCKS = tuple(threading.RLock() for _ in range(64))
_FIELDS = {
    'servers': (('tag', 'protocol'), {'requests', 'failed', 'inflight', 'latency_sum_ms', 'latency_count'}),
    'caches': (('tag',), {'lookups', 'fresh_hits', 'stale_hits', 'misses', 'expired', 'entries'}),
    'upstreams': (('tag', 'upstream'), {'requests', 'failed', 'latency_sum_ms', 'latency_count'}),
}


class DashboardStateError(Exception):
    """The previous statistics boundary must not be silently discarded."""


class DashboardStateConflict(Exception):
    """A concurrent reset superseded this network snapshot."""


def _finite(value):
    if value is None:
        return True
    if isinstance(value, bool) or not isinstance(value, (int, float)) or value < 0:
        return False
    try:
        return math.isfinite(value)
    except OverflowError:
        return False


def validate_state(value):
    if not isinstance(value, dict):
        return False
    fields = {
            'schema', 'generation', 'started_at_ms', 'reason', 'target',
            'instance_id', 'source_revision', 'runtime_revision', 'baseline'}
    if value.get('schema') == 2:
        fields |= {'carried', 'carried_partial', 'publication_count', 'pending'}
    if set(value) != fields:
        return False
    if value.get('schema') not in (1, 2) or type(value.get('schema')) is not int:
        return False
    if any(not isinstance(value.get(key), str) or not re.fullmatch(pattern, value[key])
           for key, pattern in (('generation', r'[0-9a-f]{32}'), ('target', r'[0-9a-f]{64}'),
                                ('source_revision', r'[0-9a-f]{64}'), ('runtime_revision', r'[0-9a-f]{64}'))):
        return False
    if (type(value.get('started_at_ms')) is not int or not 0 <= value['started_at_ms'] <= 2 ** 53 - 1
            or value.get('reason') not in _REASONS or not isinstance(value.get('instance_id'), str)
            or not 0 < len(value['instance_id']) <= 128):
        return False
    if not validate_observations(value['baseline']):
        return False
    if value['schema'] == 1:
        return True
    if (not validate_observations(value['carried']) or type(value['carried_partial']) is not bool
            or type(value['publication_count']) is not int or not 0 <= value['publication_count'] <= 2 ** 53 - 1):
        return False
    pending = value['pending']
    if pending is None:
        return True
    return (isinstance(pending, dict) and set(pending) == {
                'deployment_id', 'expected_revision', 'collected_at_ms', 'totals', 'partial'}
            and isinstance(pending['deployment_id'], str)
            and re.fullmatch(r'[A-Za-z0-9_-]{1,128}', pending['deployment_id']) is not None
            and isinstance(pending['expected_revision'], str)
            and re.fullmatch(r'[0-9a-f]{64}', pending['expected_revision']) is not None
            and type(pending['collected_at_ms']) is int
            and value['started_at_ms'] <= pending['collected_at_ms'] <= 253402300799999
            and type(pending['partial']) is bool and validate_observations(pending['totals']))


def validate_observations(baseline):
    if not isinstance(baseline, dict) or set(baseline) != set(_FIELDS):
        return False
    total = 0
    for family, (labels, numbers) in _FIELDS.items():
        rows = baseline[family]
        if not isinstance(rows, list):
            return False
        total += len(rows)
        if total > MAX_ROWS:
            return False
        seen = set()
        for row in rows:
            if not isinstance(row, dict) or set(row) - set(labels) - numbers:
                return False
            if any(not isinstance(row.get(key), str) or not 0 < len(row[key]) <= 16_384 for key in labels):
                return False
            identity = tuple(row[key] for key in labels)
            if identity in seen or any(not _finite(row.get(key)) for key in numbers):
                return False
            seen.add(identity)
    return True


class DashboardStateStore:
    def __init__(self, data_dir):
        self.directory = Path(data_dir) / 'dns-monitor'

    def path(self, agent_id):
        digest = hashlib.sha256(str(agent_id).encode('utf-8')).hexdigest()
        return self.directory / (digest + '.json')

    @contextmanager
    def _locked(self, path):
        lock = _LOCKS[int(hashlib.sha256(str(path.resolve()).encode()).hexdigest(), 16) % len(_LOCKS)]
        if not lock.acquire(timeout=LOCK_TIMEOUT):
            raise DashboardStateError('DNS statistics state is busy')
        handle = None
        acquired = False
        try:
            self.directory.mkdir(mode=0o700, parents=True, exist_ok=True)
            fd = os.open(str(path.with_suffix('.lock')), os.O_CREAT | os.O_RDWR, 0o600)
            handle = os.fdopen(fd, 'r+b')
            deadline = time.monotonic() + LOCK_TIMEOUT
            if os.name == 'nt':
                import msvcrt
                if handle.seek(0, os.SEEK_END) == 0:
                    handle.write(b'0')
                    handle.flush()
                while True:
                    try:
                        handle.seek(0)
                        msvcrt.locking(handle.fileno(), msvcrt.LK_NBLCK, 1)
                        break
                    except OSError:
                        if time.monotonic() >= deadline:
                            raise DashboardStateError('DNS statistics state is busy') from None
                        time.sleep(0.02)
            else:
                import fcntl
                while True:
                    try:
                        fcntl.flock(handle.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
                        break
                    except BlockingIOError:
                        if time.monotonic() >= deadline:
                            raise DashboardStateError('DNS statistics state is busy') from None
                        time.sleep(0.02)
            acquired = True
            yield
        except OSError:
            raise DashboardStateError('Cannot access DNS statistics state') from None
        finally:
            try:
                if handle is not None:
                    if acquired:
                        if os.name == 'nt':
                            handle.seek(0)
                            msvcrt.locking(handle.fileno(), msvcrt.LK_UNLCK, 1)
                        else:
                            fcntl.flock(handle.fileno(), fcntl.LOCK_UN)
                    handle.close()
            finally:
                lock.release()

    def _read(self, path):
        try:
            with path.open('rb') as handle:
                encoded = handle.read(MAX_STATE_BYTES + 1)
        except FileNotFoundError:
            return None
        except OSError:
            raise DashboardStateError('Cannot read DNS statistics state') from None
        try:
            if len(encoded) > MAX_STATE_BYTES:
                raise ValueError
            value = json.loads(encoded)
            if not validate_state(value):
                raise ValueError
        except (ValueError, UnicodeError, TypeError, OverflowError, RecursionError):
            raise DashboardStateError('DNS statistics state is damaged') from None
        return value

    def read(self, agent_id):
        path = self.path(agent_id)
        with self._locked(path):
            return self._read(path)

    def compare_and_set(self, agent_id, expected, replacement=None, verify=None):
        """Check the generation under the same lock as the atomic replacement.

        No network operations belong in verify; it only checks the registered
        Agent still targets the device from which this snapshot was collected.
        A None replacement only checks that a read is still current.
        """
        path = self.path(agent_id)
        with self._locked(path):
            current = self._read(path)
            generation = current['generation'] if current else None
            if generation != expected:
                raise DashboardStateConflict()
            if verify is not None:
                verify()
            if replacement is None:
                return current
            if not validate_state(replacement):
                raise DashboardStateError('Invalid DNS statistics state')
            encoded = json.dumps(replacement, ensure_ascii=False, allow_nan=False,
                                 separators=(',', ':')).encode('utf-8')
            if len(encoded) > MAX_STATE_BYTES:
                raise DashboardStateError('DNS statistics state exceeds size limit')
            temporary = None
            try:
                fd, temporary = tempfile.mkstemp(prefix='.' + path.name + '.', suffix='.tmp', dir=self.directory)
                with os.fdopen(fd, 'wb') as handle:
                    handle.write(encoded)
                    handle.flush()
                    os.fsync(handle.fileno())
                os.replace(temporary, path)
                temporary = None
                if os.name != 'nt':
                    fd = os.open(str(self.directory), os.O_RDONLY)
                    try:
                        os.fsync(fd)
                    finally:
                        os.close(fd)
            except OSError:
                raise DashboardStateError('Cannot save DNS statistics state') from None
            finally:
                if temporary is not None:
                    try:
                        os.unlink(temporary)
                    except FileNotFoundError:
                        pass
            return replacement
