"""Observe public task rows and durable receipts after asynchronous acceptance."""
import json
import time

from compare import differences
from harness import DesignError, send


def receipt_errors(value, *, count, status, error=''):
    expected = {'run_count': count, 'last_status': status, 'last_trigger': 'manual', 'last_error': error,
                'consecutive_failures': count if status == 'failed' else 0}
    if status == 'failed':
        expected['failure_count'] = count
    return differences(expected, {key: value.get(key) for key in expected}, 'task.receipt')


def configure(pair, key):
    prefix = 'files.task-status.json.content.' + key + '.'
    for field in ('last_run_at', 'last_finished_at', 'last_success_at', 'cache_updated_at'):
        pair.policy.times.add(prefix + field)
    pair.policy.durations.add(prefix + 'last_duration_ms')
    for field in ('version', 'first_seen_at', 'checked_at', 'last_seen_at'):
        pair.policy.times.add('files.notifications.json.content.items.task:' + key + '.' + field)


def row(pair, index, key):
    reply = send(pair.bases[index], 'GET', '/api/tasks', headers={'Authorization': 'Bearer ' + pair.tokens[index]})
    if reply.status != 200:
        return {'http_status': reply.status}
    rows = reply.json().get('tasks', [])
    item = next((item for item in rows if item.get('key') == key), {})
    return {field: item.get(field) for field in ('key', 'running', 'run_count', 'last_status', 'can_run')}


class Completion:
    def __init__(self, pair, key, *, count=1, status='success', error='', gate=None, source=None):
        self.pair, self.key, self.count = pair, key, count
        self.status, self.error, self.gate = status, error, gate
        self.source, self.source_counts = source, [None, None]
        self.observations = [None, None]
        self.poll_both = pair.route_available('GET /api/tasks')
        configure(pair, key)

    def before(self, index):
        self.previous = len(self.source.requests) if self.source else 0

    def after(self, index, reply):
        if self.source:
            self.source_counts[index] = len(self.source.requests) - self.previous
            if index == 0 and self.source_counts[index] != 0:
                raise DesignError('Python busy rejection unexpectedly started a download')
        # A busy response must be received by both servers before releasing the
        # shared source. Otherwise Python completion could hide a Go busy bug.
        if self.gate is not None:
            available = self.pair.route_available('POST /api/tasks/run') and not self.pair.unimplemented_mutations
            if index == 0 and available:
                return
            self.gate.set()
            for backend in range(index + 1):
                self.wait(backend)
        else:
            self.wait(index)

    def wait(self, index):
        path = self.pair.dirs[index] / 'task-status.json'
        deadline = time.monotonic() + 4
        value, current = {}, None
        while time.monotonic() < deadline:
            if path.exists():
                value = json.loads(path.read_text()).get(self.key, {})
            if value.get('run_count', 0) >= self.count:
                if index == 0 or self.poll_both:
                    current = row(self.pair, index, self.key)
                    if current.get('running') is False:
                        break
                else:
                    # A not-yet-wired GET cannot prove runtime lock release;
                    # the following repeated POST is the independent proof.
                    time.sleep(.15)
                    break
            time.sleep(.025)
        self.observations[index] = {'receipt_completed': value.get('run_count') == self.count}
        if self.poll_both:
            self.observations[index]['row'] = current
        if index == 0:
            errors = receipt_errors(value, count=self.count, status=self.status, error=self.error)
            if errors or current.get('running') is not False:
                raise DesignError('Python task did not finish with intended receipt and released lock: ' + repr((value, current, errors)))

    def compare(self, policy):
        out = differences(*self.observations, 'task.completion', policy)
        if self.source:
            out += differences(*self.source_counts, 'task.busy.extra_source_requests', policy)
        return out
