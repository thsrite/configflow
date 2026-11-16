"""Observe real subscription HTTP exchanges and validated cache identities/times."""
import copy
import hashlib
import json
import re

from compare import Difference, differences
from harness import DesignError


def configure(pair, url):
    policy = pair.policy
    transport = 'shared/subscription-downloads/' + hashlib.sha256(url.encode()).hexdigest() + '.json'
    policy.times.add('files.' + transport + '.content.fetched_at')
    for field in ('updated_at', 'metadata.fetched_at'):
        policy.times.add('files.shared/subscribes/sub-a.json.content.' + field)
    for field in ('last_run_at', 'last_finished_at', 'last_success_at', 'cache_updated_at'):
        policy.times.add('files.task-status.json.content.subscription:sub-a.' + field)
    policy.durations.add('files.task-status.json.content.subscription:sub-a.last_duration_ms')
    policy.times.update({'body.cached_updated_at', 'files.shared/subscription_health.json.content.sub-a.traffic_at'})
    for history in ('shared/subscription_health.json', 'shared/subscription_health.json.bak'):
        policy.times.add('files.' + history + '.content.sub-a.history[0].at')
    for field in ('version', 'first_seen_at', 'checked_at', 'last_seen_at'):
        policy.times.add('files.notifications.json.content.items.task:subscription:sub-a.' + field)


def cached(directory):
    path = directory / 'shared/subscribes/sub-a.json'
    return json.loads(path.read_text()) if path.exists() else None


def validate_ids(directory):
    cache = cached(directory)
    if cache is None:
        return
    ids = [item.get('id') for item in cache.get('nodes', []) if isinstance(item, dict)]
    if any(not isinstance(value, str) or not re.fullmatch(r'node_[0-9a-f]{8}', value) for value in ids) or len(ids) != len(set(ids)):
        raise DesignError('Python downloaded node IDs are not formatted/unique')


class Exchange:
    def __init__(self, pair, source, expected=1, unordered=False):
        self.pair, self.source, self.expected = pair, source, expected
        self.transcripts = [None, None]
        self.unordered = unordered

    def before(self, index):
        self.source.requests.clear()

    def after(self, index, reply):
        self.transcripts[index] = copy.deepcopy(self.source.requests)
        if index == 0 and len(self.transcripts[0]) != self.expected:
            raise DesignError('Python source request count does not demonstrate intended path: ' + repr(self.transcripts[0]))

    def compare(self, policy):
        transcripts = self.transcripts
        if self.unordered:
            transcripts = [sorted(rows, key=lambda item: json.dumps(item, sort_keys=True)) for rows in transcripts]
        return differences(*transcripts, 'subscription.source.requests', policy)


def capture_ids(pair):
    def capture(replies, policy):
        out = []
        caches = [cached(d) for d in pair.dirs]
        if not all(caches):
            return out  # Ordinary file comparison reports a missing cache.
        nodes = [cache.get('nodes', []) for cache in caches]
        if len(nodes[0]) != len(nodes[1]):
            return out  # Full cache/body comparison reports list mismatch.
        right_ids = [item.get('id') for item in nodes[1] if isinstance(item, dict)]
        string_ids = [value for value in right_ids if isinstance(value, str)]
        if len(string_ids) != len(set(string_ids)):
            return [Difference('cache.nodes.id', 'duplicate-Go-identity', '<unique>', right_ids)]
        for index, (a, b) in enumerate(zip(*nodes)):
            if not isinstance(a, dict) or not isinstance(b, dict):
                continue
            left, right = a.get('id'), b.get('id')
            if left == right:
                continue
            if not isinstance(left, str) or not re.fullmatch(r'node_[0-9a-f]{8}', left):
                continue  # Fixed legacy cache identities are compared verbatim.
            if not isinstance(right, str) or not re.fullmatch(r'node_[0-9a-f]{8}', right):
                out.append(Difference(f'cache.nodes[{index}].id', 'invalid-cached-node-identity', 'node_<8hex>', right))
                continue
            policy.bind(left, right, r'node_[0-9a-f]{8}', 'downloaded-node')
        return out
    return capture


def blocked_error_projection(value, index):
    # O6 permits internal English exception details to vary. This is narrower:
    # only the optional status=403 suffix proven by this fixed fake response is
    # canonicalized; the exception class and every Chinese character remain.
    value = copy.deepcopy(value)
    if isinstance(value, dict):
        for key in ('fetch_error', 'message'):
            if isinstance(value.get(key), str):
                value[key] = value[key].replace('request_failed (exception_type=SubscriptionFetchError status=403)',
                                                'request_failed (exception_type=SubscriptionFetchError)')
    return value


def notice_errors(item, task):
    expected = f'{task.get("run_count", 0)}:{task.get("last_run_at") or ""}:{task.get("last_status")}'
    out = differences(expected, item.get('evidence'), 'notification.evidence')
    out += differences(task.get('last_run_at'), item.get('checked_at'), 'notification.checked_at')
    out += differences(task.get('last_run_at'), item.get('last_seen_at'), 'notification.last_seen_at')
    out += differences(task.get('consecutive_failures') or 0, item.get('failure_count'), 'notification.failure_count')
    return out


def capture_notice(pair, key='subscription:sub-a'):
    def capture(replies, policy):
        out, evidence = [], []
        for index, directory in enumerate(pair.dirs):
            path = directory / 'notifications.json'
            if not path.exists():
                evidence.append(None)
                continue  # The complete file comparator reports its absence.
            items = json.loads(path.read_text()).get('items', {})
            item = items.get('task:' + key)
            if item is None:
                evidence.append(None)
                continue
            task = json.loads((directory / 'task-status.json').read_text())[key]
            errors = notice_errors(item, task)
            if index == 0 and errors:
                raise DesignError('Python notification does not describe its actual task receipt')
            out += errors
            evidence.append(item.get('evidence') if not errors else None)
        if all(isinstance(v, str) for v in evidence):
            policy.bind(*evidence, r'[0-9]+:[0-9T:Z.+-]+:(?:failed|success|skipped)', 'task-notice-evidence')
        return out
    return capture
