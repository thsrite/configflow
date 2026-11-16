"""Observe real public async jobs while independent fake Agent replies are held."""
import copy
import re
import time

from compare import Difference, differences, database_diffs, file_diffs, timestamp
from discovery_jobs import FIELDS, instant
from harness import DesignError, send


def job_errors(value, policy, kind, total, *, terminal=False):
    if not isinstance(value, dict) or value.get('success') is not True or not isinstance(value.get('job'), dict):
        return ['missing success/job']
    job, out = value['job'], []
    if set(job) != FIELDS:
        out.append('job field closure')
    if not isinstance(job.get('id'), str) or not re.fullmatch('[a-f0-9]{32}', job['id']):
        out.append('job ID format')
    if job.get('kind') != kind:
        out.append('wrong job kind')
    if any(type(job.get(k)) is not int for k in ('done', 'total')):
        out.append('noninteger progress')
    elif not 0 <= job['done'] <= job['total'] <= total or job['total'] not in (0, total):
        out.append('progress bounds')
    if not isinstance(job.get('errors'), list) or not isinstance(job.get('message'), str):
        out.append('errors/message types')
    if not timestamp(job.get('started_at'), policy):
        out.append('start timestamp window')
    if terminal:
        if job.get('status') not in ('done', 'failed') or not timestamp(job.get('finished_at'), policy):
            out.append('missing terminal state/time')
        elif timestamp(job.get('started_at'), policy) and instant(job['finished_at'], policy) < instant(job['started_at'], policy):
            out.append('finish precedes start')
    elif job.get('status') != 'running' or job.get('done') != 0 or job.get('errors') != [] or job.get('message') != '' or job.get('finished_at') is not None:
        out.append('gate-closed job must remain running without results/errors')
    return out


def progress_errors(previous, current):
    out = []
    if current.get('id') != previous.get('id') or current.get('started_at') != previous.get('started_at'):
        out.append('job identity/start changed')
    if type(previous.get('done')) is int and type(current.get('done')) is int and current['done'] < previous['done']:
        out.append('progress decreased')
    if previous.get('total', 0) and current.get('total') != previous['total']:
        out.append('total changed after becoming nonzero')
    if previous.get('status') in ('done', 'failed') and previous != current:
        out.append('terminal job mutated')
    return out


class Running:
    def __init__(self, pair, agents, kind, total):
        self.pair, self.agents, self.kind, self.total = pair, agents, kind, total
        self.ids, self.valid, self.previous = [None, None], [False, False], [None, None]
        self.violations = [[], []]
        pair.policy.times.add('body.job.started_at')
        for field in ('started_at', 'finished_at'):
            pair.policy.times.add('job.final.job.' + field)

    def before(self, index):
        pass

    def after(self, index, reply):
        value = self.decode(reply)
        policy = copy.deepcopy(self.pair.policy)
        policy.end = time.time()
        errors = job_errors(value, policy, self.kind, self.total)
        if reply.status != 202:
            errors.append('acceptance status != 202')
        self.valid[index] = not errors
        if isinstance(value, dict) and isinstance(value.get('job'), dict):
            self.previous[index] = value['job']
            ident = value['job'].get('id')
            if isinstance(ident, str) and re.fullmatch('[a-f0-9]{32}', ident):
                self.ids[index] = ident
        if not self.agents[index].seen.wait(4):
            errors.append('no outgoing Agent call while job was running')
        errors += self.agents[index].errors
        self.violations[index] += errors
        if index == 0 and errors:
            raise DesignError('Python running-job premise: ' + repr((errors, value)))

    @staticmethod
    def decode(reply):
        try:
            return reply.json()
        except (ValueError, UnicodeError):
            return None

    def capture(self, replies, policy):
        if all(self.ids):
            try:
                policy.bind(*self.ids, '[a-f0-9]{32}', 'discovery-job')
            except AssertionError as error:
                return [Difference('job.id', 'job-identity-binding', self.ids[0], str(error))]
        return []

    def projection(self, value, index):
        if not self.valid[index]:
            return value
        result = copy.deepcopy(value)
        # Worker may set the known final total before or after the 202 snapshot.
        # Every other field is compared; subsequent held poll requires total N.
        result['job']['total'] = 'validated-initial-total'
        return result

    def compare(self, policy):
        return [Difference('job.running', 'invalid-Go-lifecycle', [], self.violations[1])] if self.violations[1] else []

    def poll(self, index):
        return send(self.pair.bases[index], 'GET', '/api/domain-discovery/probe/' + (self.ids[index] or 'missing'),
                    headers={'Authorization': 'Bearer ' + self.pair.tokens[index], 'X-ConfigFlow-Profile': self.pair.default_profile})

    def current(self, field):
        self.pair.policy.times.add('body.' + field + '.started_at')
        def capture(replies, policy):
            for index, reply in enumerate(replies):
                value = self.decode(reply)
                job = value.get(field) if isinstance(value, dict) else None
                errors = job_errors({'success': True, 'job': job}, policy, self.kind, self.total)
                if index == 0 and errors:
                    raise DesignError('Python current running state: ' + repr((errors, value)))
                self.violations[index] += errors
                if not errors:
                    self.ids[index], self.previous[index] = job['id'], job
            return self.capture(replies, policy) + self.compare(policy)
        return capture

    def reference_current(self, reply, field):
        value = self.decode(reply)
        job = value.get(field) if isinstance(value, dict) else None
        policy = copy.deepcopy(self.pair.policy)
        policy.end = time.time()
        errors = job_errors({'success': True, 'job': job}, policy, self.kind, self.total)
        if errors:
            raise DesignError('Python current running state: ' + repr((errors, value)))
        self.ids[0], self.previous[0] = job['id'], job

    def finish(self, expected_calls, *, errors=0, status='done', variant='', case='T20', expected_done=None, route=None):
        expected_done = self.total if expected_done is None else expected_done
        path = {'probe': 'probe', 'region': 'region/check', 'website': 'website/identify'}[self.kind]
        route = route or 'POST /api/domain-discovery/' + path
        registered = self.pair.route_available(route)
        available = registered and not self.pair.unimplemented_mutations
        for agent in self.agents:
            agent.release.set()
        final, statuses = [None, None], [None, None]
        for index in range(2 if available else 1):
            deadline = time.monotonic() + 10
            while time.monotonic() < deadline:
                reply = self.poll(index)
                value = self.decode(reply)
                final[index] = value
                statuses[index] = reply.status
                job = value.get('job') if isinstance(value, dict) else None
                if isinstance(job, dict):
                    if self.previous[index] is not None:
                        self.violations[index] += progress_errors(self.previous[index], job)
                    self.previous[index] = job
                    if job.get('status') in ('done', 'failed'):
                        break
                time.sleep(.02)
            else:
                self.violations[index].append('job did not finish within ten seconds after release')
            policy = copy.deepcopy(self.pair.policy)
            policy.end = time.time()
            self.violations[index] += job_errors(value, policy, self.kind, self.total, terminal=True)
            if reply.status != 200:
                self.violations[index].append('final poll status != 200')
            if index == 0:
                if self.violations[0]:
                    raise DesignError('Python job lifecycle: ' + repr(self.violations[0]))
                if job['status'] != status or len(job['errors']) != errors or job['done'] != expected_done or job['total'] != self.total:
                    raise DesignError('Python terminal outcome: ' + repr(job))
                actual = self.agents[0].snapshot()
                if actual != expected_calls or self.agents[0].errors:
                    raise DesignError('Python fake Agent protocol: ' + repr((expected_calls, actual, self.agents[0].errors)))
        category = re.search(r'-(S|F|B)\d+', case)[1] if case.startswith('R') else case
        if not available:
            self.pair.recorder.record(case, variant, route, category,
                'go-unimplemented-dependency' if registered else 'go-unimplemented',
                python_status=statuses[0], go_status=None, blocked_by=sorted(self.pair.unimplemented_mutations))
            return final
        policy = self.pair.policy
        policy.end = time.time()
        diffs = self.compare(policy)
        diffs += differences(expected_calls, self.agents[1].snapshot(), 'agent.requests', policy)
        if self.agents[1].errors:
            diffs.append(Difference('agent.transport', 'fake-errors', [], self.agents[1].errors))
        diffs += differences(final[0], final[1], 'job.final', policy)
        diffs += database_diffs(*self.pair.dirs, policy) + file_diffs(*self.pair.dirs, policy)
        self.pair.recorder.record(case, variant, route,
                                  category, 'different' if diffs else 'compared', diffs, python_status=statuses[0], go_status=statuses[1])
        return final
