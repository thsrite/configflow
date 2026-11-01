"""Bounded, process-local comparison of desired and successfully deployed files.

List requests only inspect snapshots/cache and schedule work. Downloads and bundle
generation run in at most two workers; a missing baseline never implies a match.
Remote changes are checked on a fifteen-minute successful-result cache interval.
``compare_now`` answers in its caller's thread, for callers that are already in
a background worker; it shares the cache with the list. ``get_push_diff`` asks
the same workers to also list what changed (``push_diff``), from the bundle the
comparison prepares anyway; a few of those answers are kept with the result,
each with every node and rule entry of its files (compressed, bounded in
bytes), which ``get_push_diff_entries`` reads page by page under the answer's
``version``.
"""
from collections import OrderedDict
from concurrent.futures import ThreadPoolExecutor
from copy import deepcopy
from dataclasses import dataclass
from datetime import datetime, timezone
import hashlib
import json
import re
import threading
import time
import uuid


_FINGERPRINT = re.compile(r'v1:[0-9a-f]{64}\Z')
_TERMINAL = frozenset({'succeeded', 'failed', 'rolled_back'})
_PROFILE_METADATA = frozenset({'agents', 'backup', '_revision', 'updated_at', 'created_at', 'name', 'description'})
_UNBOUND_REPOSITORY = object()


def _current_repository():
    # Observe identity without initializing a repository for standalone callers.
    from backend.common import config as config_module
    return config_module._repository


@dataclass
class _Entry:
    key: str
    repository: object = None
    result: dict | None = None
    expires: float = 0
    # Set by get_push_diff: the next worker for these inputs also lists changes.
    want_detail: bool = False


@dataclass
class _Detail:
    key: str
    repository: object
    value: dict
    expires: float
    # Every node / rule entry by file path (push_diff.FileEntries), read page by page.
    lists: dict | None = None
    version: str | None = None
    size: int = 0


# Kept entries of all cached change lists together, compressed; the oldest lists go first.
MAX_LIST_BYTES = 48 * 1024 * 1024
_CHANGES = frozenset({'added', 'removed', 'modified'})


def _unknown(reason):
    return {'status': 'unknown', 'reason': reason}


def _precondition(agent, config):
    tasks = list((agent.get('deployments') or {}).values())
    latest = agent.get('latest_deployment')
    if isinstance(latest, dict):
        tasks.append(latest)
    if any(isinstance(task, dict) and task.get('status') not in _TERMINAL for task in tasks):
        return _unknown('deployment_pending')
    baseline = agent.get('applied_content')
    if not baseline:
        successful = any(isinstance(task, dict) and task.get('status') == 'succeeded' for task in tasks)
        if successful or agent.get('config_version') not in (None, '', '0') or agent.get('config_revision') is not None:
            return _unknown('baseline_missing')
        return {'status': 'never-pushed'}
    if not isinstance(baseline, dict) or not isinstance(baseline.get('fingerprint'), str) or not _FINGERPRINT.fullmatch(baseline['fingerprint']):
        return _unknown('baseline_invalid')
    from backend.common.config import get_default_profile_id
    profile_id = agent.get('profile_id') or get_default_profile_id()
    if baseline.get('profile_id') != profile_id or config.get('profile_id', profile_id) != profile_id:
        return _unknown('profile_mismatch')
    if baseline.get('service_type') != agent.get('service_type', 'mihomo'):
        return _unknown('service_mismatch')
    if not baseline.get('config_version') or baseline['config_version'] != agent.get('config_version'):
        return _unknown('version_mismatch')
    if not baseline.get('deployment_id') or not isinstance(baseline.get('config_path'), str) or not baseline['config_path']:
        return _unknown('baseline_invalid')
    return None


def _input_key(agent, config, base_url):
    # Only remove known top-level metadata. Arbitrary nested node/plugin fields
    # (even fields named created_at) may affect a generated configuration.
    inputs = {key: value for key, value in config.items() if key not in _PROFILE_METADATA}
    identity = {key: agent.get(key) for key in ('id', 'profile_id', 'service_type', 'config_version', 'applied_content')}
    encoded = json.dumps({'agent': identity, 'config': inputs, 'base_url': base_url},
                         sort_keys=True, ensure_ascii=False, separators=(',', ':'), allow_nan=False)
    return hashlib.sha256(encoded.encode('utf-8')).hexdigest()


class ContentSyncChecker:
    def __init__(self, *, executor=None, clock=time.monotonic, max_workers=2,
                 success_ttl=900, error_ttl=30, max_entries=512, max_details=8, max_list_bytes=MAX_LIST_BYTES):
        self._executor = executor or ThreadPoolExecutor(max_workers=max_workers, thread_name_prefix='agent-content-sync')
        self._clock = clock
        self._success_ttl = success_ttl
        self._error_ttl = error_ttl
        self._max_entries = max(max_entries, max_workers)
        self._slots = threading.BoundedSemaphore(max_workers)
        self._lock = threading.RLock()
        self._entries = OrderedDict()
        # Change lists are larger than a status; keep only the latest few.
        self._details = OrderedDict()
        self._max_details = max(1, max_details)
        self._max_list_bytes = max_list_bytes
        self._running = {}
        # Bumped by invalidate(), so a compare_now that prepared from an older
        # cache does not store its answer.
        self._generation = 0

    def _trim(self):
        for identity in list(self._entries):
            if len(self._entries) <= self._max_entries:
                break
            if identity not in self._running:
                del self._entries[identity]

    def get_content_sync(self, agent, config, base_url, prepare, *, scope='', repository=_UNBOUND_REPOSITORY):
        if not isinstance(agent, dict) or not isinstance(config, dict) or not isinstance(agent.get('id'), str):
            return _unknown('invalid_inputs')
        identity = (scope, agent['id'])
        if repository is _UNBOUND_REPOSITORY:
            repository = _current_repository()
        try:
            agent_snapshot, config_snapshot = deepcopy(agent), deepcopy(config)
            unavailable = _precondition(agent_snapshot, config_snapshot)
            key = None if unavailable else _input_key(agent_snapshot, config_snapshot, base_url)
        except Exception:
            unavailable, key = _unknown('invalid_inputs'), None
        with self._lock:
            if _current_repository() is not repository:
                self._entries.pop(identity, None)
                return _unknown('check_failed')
            if unavailable:
                # Also invalidate an earlier worker, which may still be running.
                self._entries.pop(identity, None)
                return unavailable
            entry = self._entries.get(identity)
            if entry is None or entry.key != key or entry.repository is not repository:
                entry = _Entry(key, repository)
                self._entries[identity] = entry
            self._entries.move_to_end(identity)
            self._trim()
            if entry.result is not None and self._clock() < entry.expires:
                return dict(entry.result)
            if identity in self._running:
                return {'status': 'checking'}
            # ThreadPoolExecutor's own queue is unbounded. Reserve a worker slot
            # before submit so repeated polling cannot create an unbounded queue.
            if not self._slots.acquire(blocking=False):
                return {'status': 'checking'}
            token = object()
            self._running[identity] = token
            try:
                self._executor.submit(self._check, identity, entry, token, agent_snapshot,
                                      config_snapshot, base_url, prepare)
            except Exception:
                self._running.pop(identity, None)
                self._slots.release()
                entry.result = _unknown('check_failed')
                entry.expires = self._clock() + self._error_ttl
                return dict(entry.result)
            return {'status': 'checking'}

    def invalidate(self):
        """Forget finished results; running checks keep their slot but not their answer.

        Inputs outside the profile (subscription caches) can change the
        generated files without changing the cache key.
        """
        with self._lock:
            self._generation += 1
            self._details.clear()
            for identity, entry in list(self._entries.items()):
                if identity in self._running:
                    # The worker may have read the old cache; a fresh entry
                    # makes its result land nowhere.
                    self._entries[identity] = _Entry(entry.key, entry.repository)
                else:
                    entry.result, entry.expires = None, 0

    @staticmethod
    def _prepare_bundle(agent, config, base_url, prepare):
        """The candidate bundle, retargeted to the installed main filename."""
        from backend.common.config import reset_config_context
        from backend.agents.deployment_bundle import retarget_config_path
        reset_config_context()
        try:
            bundle = prepare(agent, config, base_url=base_url, deployment_id='check-' + uuid.uuid4().hex)
            return retarget_config_path(bundle, agent['applied_content']['config_path'])
        finally:
            reset_config_context()

    @classmethod
    def _prepare_fingerprint(cls, agent, config, base_url, prepare):
        from backend.agents.deployment_bundle import deployment_content_fingerprint
        return deployment_content_fingerprint(cls._prepare_bundle(agent, config, base_url, prepare).manifest)

    def _store_detail(self, identity, entry, value, ttl, lists=None):
        now = self._clock()
        size = sum(item.size for item in lists.values()) if lists else 0
        self._details[identity] = _Detail(entry.key, entry.repository, value, now + ttl, lists,
                                          value.get('version'), size)
        self._details.move_to_end(identity)
        for other in [other for other, detail in self._details.items() if other != identity and detail.expires <= now]:
            del self._details[other]
        # The newest list stays even alone over the byte limit (one comparison keeps at most
        # push_diff.MAX_KEPT_BYTES before compression).
        while len(self._details) > 1 and (len(self._details) > self._max_details or sum(
                detail.size for detail in self._details.values()) > self._max_list_bytes):
            self._details.popitem(last=False)

    def _check(self, identity, entry, token, agent, config, base_url, prepare):
        result = None
        detail = lists = None
        ttl = self._error_ttl
        with self._lock:
            want_detail = entry.want_detail
        try:
            if _current_repository() is not entry.repository:
                result = _unknown('check_failed')
            else:
                bundle = None
                if want_detail:
                    from backend.agents.deployment_bundle import deployment_content_fingerprint
                    bundle = self._prepare_bundle(agent, config, base_url, prepare)
                    fingerprint = deployment_content_fingerprint(bundle.manifest)
                else:
                    fingerprint = self._prepare_fingerprint(agent, config, base_url, prepare)
                result = {'status': 'synced' if fingerprint == agent['applied_content']['fingerprint'] else 'changed',
                          'checked_at': datetime.now(timezone.utc).isoformat().replace('+00:00', 'Z')}
                ttl = self._success_ttl
                if bundle is not None and result['status'] == 'changed':
                    detail, lists = self._build_detail(agent, bundle, entry.repository)
        except Exception:
            # Preparation exceptions can contain tokens, URLs or YAML excerpts.
            result = _unknown('check_failed')
        finally:
            with self._lock:
                # Cache loaders below preparation use the process-global repository.
                # A repository switch invalidates even an otherwise matching result.
                if _current_repository() is not entry.repository:
                    result, ttl, detail = _unknown('check_failed'), self._error_ttl, None
                if self._entries.get(identity) is entry and result is not None:
                    entry.result = result
                    entry.expires = self._clock() + ttl
                    if detail is not None:
                        # A failed listing is retried soon; the status itself stays cached. Pages of
                        # this answer name its version; a later answer has another one.
                        self._store_detail(identity, entry, {**detail, 'checked_at': result['checked_at'],
                                                             'version': uuid.uuid4().hex[:16]},
                                           self._error_ttl if detail.get('reason') == 'diff_failed' else ttl, lists)
                    if want_detail:
                        # Later list refreshes need no change list until one is asked for again.
                        entry.want_detail = False
                if self._running.get(identity) is token:
                    del self._running[identity]
                self._slots.release()

    @staticmethod
    def _build_detail(agent, bundle, repository):
        """The change list and every node / rule entry of its files."""
        from backend.agents.push_diff import build_push_diff
        lists = {}
        try:
            return build_push_diff(agent, bundle, repository, lists=lists), lists
        except Exception:
            # The status stands; only the itemized list is unavailable.
            return {'status': 'changed', 'files': None, 'reason': 'diff_failed'}, None

    def get_push_diff(self, agent, config, base_url, prepare, *, scope='', repository=_UNBOUND_REPOSITORY):
        """What a push would change, prepared by the same bounded workers.

        Answers ``checking`` while a worker runs, then ``synced``, the change
        list (``status: changed``), or the comparison's own unavailability.
        """
        if not isinstance(agent, dict) or not isinstance(config, dict) or not isinstance(agent.get('id'), str):
            return _unknown('invalid_inputs')
        identity = (scope, agent['id'])
        if repository is _UNBOUND_REPOSITORY:
            repository = _current_repository()
        try:
            agent_snapshot, config_snapshot = deepcopy(agent), deepcopy(config)
            unavailable = _precondition(agent_snapshot, config_snapshot)
            key = None if unavailable else _input_key(agent_snapshot, config_snapshot, base_url)
        except Exception:
            unavailable, key = _unknown('invalid_inputs'), None
        with self._lock:
            if _current_repository() is not repository:
                return _unknown('check_failed')
            if unavailable:
                return unavailable
            entry = self._entries.get(identity)
            if entry is None or entry.key != key or entry.repository is not repository:
                entry = _Entry(key, repository)
                self._entries[identity] = entry
            self._entries.move_to_end(identity)
            self._trim()
            now = self._clock()
            detail = self._details.get(identity)
            if detail is not None and detail.key == key and detail.repository is repository and now < detail.expires:
                return deepcopy(detail.value)
            if entry.result is not None and now < entry.expires and entry.result.get('status') != 'changed':
                return dict(entry.result)
            # A queued worker for these inputs reads this when it starts.
            entry.want_detail = True
            if identity in self._running:
                return {'status': 'checking'}
            if not self._slots.acquire(blocking=False):
                return {'status': 'checking'}
            token = object()
            self._running[identity] = token
            try:
                self._executor.submit(self._check, identity, entry, token, agent_snapshot,
                                      config_snapshot, base_url, prepare)
            except Exception:
                self._running.pop(identity, None)
                self._slots.release()
                return _unknown('check_failed')
            return {'status': 'checking'}

    def get_push_diff_entries(self, agent, config, base_url, *, version, path, change, offset=0, limit=50,
                              query=None, find=None, scope='', repository=_UNBOUND_REPOSITORY):
        """One page of a file's nodes or rules from the cached change list ``version``.

        Answers ``stale`` (with a reason) unless that list is still the current one for
        these inputs: ``expired`` (gone, past its interval or invalidated), ``changed``
        (the agent or configuration differs now), ``replaced`` (a newer list). Pages of
        different lists are never mixed. Raises KeyError for a file or group without kept
        entries. ``query`` is a ``push_diff.search_query``; ``find`` the stored-credential
        finder that search leaves out.
        """
        if not isinstance(agent, dict) or not isinstance(config, dict) or not isinstance(agent.get('id'), str):
            return {'status': 'stale', 'reason': 'changed'}
        identity = (scope, agent['id'])
        if repository is _UNBOUND_REPOSITORY:
            repository = _current_repository()
        try:
            unavailable = _precondition(agent, config)
            key = None if unavailable else _input_key(agent, config, base_url)
        except Exception:
            unavailable, key = True, None
        with self._lock:
            detail = self._details.get(identity)
            if (detail is None or detail.repository is not repository or _current_repository() is not repository
                    or self._clock() >= detail.expires):
                return {'status': 'stale', 'reason': 'expired'}
            if unavailable or detail.key != key:
                return {'status': 'stale', 'reason': 'changed'}
            if detail.version != version:
                return {'status': 'stale', 'reason': 'replaced'}
            files = detail.lists or {}
        if change not in _CHANGES:
            raise KeyError(change)
        # Read outside the lock: kept entries never change once stored.
        page = files[path].page(change, offset, limit, query, find)
        return {'status': 'ok', 'version': version, 'file': path, 'change': change, **page}

    def compare_now(self, agent, config, base_url, prepare, *, scope='', repository=_UNBOUND_REPOSITORY):
        """The same comparison, answered in the caller's thread.

        For a caller already off the request path that needs a definite answer
        (an IPv6 verification worker). A fresh result for the same inputs, for
        example the Agent page's, is reused; a new result is kept for it too.
        """
        if not isinstance(agent, dict) or not isinstance(config, dict) or not isinstance(agent.get('id'), str):
            return _unknown('invalid_inputs')
        identity = (scope, agent['id'])
        if repository is _UNBOUND_REPOSITORY:
            repository = _current_repository()
        try:
            agent_snapshot, config_snapshot = deepcopy(agent), deepcopy(config)
            unavailable = _precondition(agent_snapshot, config_snapshot)
            key = None if unavailable else _input_key(agent_snapshot, config_snapshot, base_url)
        except Exception:
            return _unknown('invalid_inputs')
        if unavailable:
            return unavailable
        with self._lock:
            if _current_repository() is not repository:
                return _unknown('check_failed')
            entry = self._entries.get(identity)
            if (entry is not None and entry.key == key and entry.repository is repository
                    and entry.result is not None and self._clock() < entry.expires):
                return dict(entry.result)
            generation = self._generation
        try:
            fingerprint = self._prepare_fingerprint(agent_snapshot, config_snapshot, base_url, prepare)
        except Exception:
            # Preparation exceptions can contain tokens, URLs or YAML excerpts.
            return _unknown('check_failed')
        result = {'status': 'synced' if fingerprint == agent_snapshot['applied_content']['fingerprint'] else 'changed',
                  'checked_at': datetime.now(timezone.utc).isoformat().replace('+00:00', 'Z')}
        with self._lock:
            if _current_repository() is not repository:
                return _unknown('check_failed')
            if self._generation != generation:
                return result
            entry = self._entries.get(identity)
            if entry is None or entry.key != key or entry.repository is not repository:
                # An older worker for other inputs then keeps its result to itself.
                entry = self._entries[identity] = _Entry(key, repository)
            entry.result = dict(result)
            entry.expires = self._clock() + self._success_ttl
            self._entries.move_to_end(identity)
            self._trim()
        return result


_checker = ContentSyncChecker()


def get_content_sync(agent, config, base_url, prepare, *, scope='', repository=_UNBOUND_REPOSITORY):
    return _checker.get_content_sync(agent, config, base_url, prepare, scope=scope, repository=repository)


def compare_now(agent, config, base_url, prepare, *, scope='', repository=_UNBOUND_REPOSITORY):
    return _checker.compare_now(agent, config, base_url, prepare, scope=scope, repository=repository)


def get_push_diff(agent, config, base_url, prepare, *, scope='', repository=_UNBOUND_REPOSITORY):
    return _checker.get_push_diff(agent, config, base_url, prepare, scope=scope, repository=repository)


def get_push_diff_entries(agent, config, base_url, **kwargs):
    return _checker.get_push_diff_entries(agent, config, base_url, **kwargs)


def invalidate_content_sync():
    _checker.invalidate()
