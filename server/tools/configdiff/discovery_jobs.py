"""Validate fast async preflight failures before comparing complete final jobs."""
import copy
from datetime import datetime
import re
import time
from zoneinfo import ZoneInfo

from compare import Difference, differences, timestamp
from harness import DesignError, send

FIELDS = {'id', 'kind', 'status', 'total', 'done', 'errors', 'message', 'started_at', 'finished_at'}


def preflight_errors(value, policy, kind, message):
    if not isinstance(value, dict) or value.get('success') is not True or not isinstance(value.get('job'), dict):
        return ['missing success/job object']
    job, errors = value['job'], []
    if set(job) != FIELDS:
        errors.append('job field closure')
    if not isinstance(job.get('id'), str) or not re.fullmatch('[a-f0-9]{32}', job['id']):
        errors.append('job ID format')
    if job.get('kind') != kind or job.get('status') not in ('running', 'failed'):
        errors.append('wrong preflight kind/status')
    if any(type(job.get(field)) is not int or job[field] != 0 for field in ('done', 'total')):
        errors.append('preflight progress must be integer zero')
    if job.get('errors') != []:
        errors.append('preflight error list must be empty')
    if not timestamp(job.get('started_at'), policy):
        errors.append('job start window')
    if job.get('status') == 'running':
        if job.get('finished_at') is not None or job.get('message') != '':
            errors.append('running job has terminal fields')
    elif job.get('status') == 'failed':
        if not timestamp(job.get('finished_at'), policy):
            errors.append('job finish window')
        elif timestamp(job.get('started_at'), policy):
            def instant(text):
                value = datetime.fromisoformat(text.replace('Z', '+00:00'))
                return value.replace(tzinfo=ZoneInfo(policy.tz)).timestamp() if value.tzinfo is None else value.timestamp()
            if instant(job['finished_at']) < instant(job['started_at']):
                errors.append('finish precedes start')
        if job.get('message') != message:
            errors.append('wrong exact preflight message')
    return errors


def acceptance_view(value, valid):
    if not valid:
        return value
    result = copy.deepcopy(value)
    # Only the three phase-dependent fields change before the final public
    # poll. Their validity/messages were checked; the complete final object is
    # compared separately. IDs, progress, errors and extra fields remain intact.
    for field in ('status', 'message', 'finished_at'):
        result['job'][field] = {'validated_preflight_phase': field}
    return result


class Completion:
    def __init__(self, pair, kind, message):
        self.pair, self.kind, self.message = pair, kind, message
        self.ids, self.final = [None, None], [None, None]
        self.valid, self.violations = [False, False], [[], []]
        pair.policy.times.add('body.job.started_at')
        for field in ('started_at', 'finished_at'):
            pair.policy.times.add('job.final.job.' + field)

    def before(self, index):
        pass

    def after(self, index, reply):
        policy = copy.deepcopy(self.pair.policy)
        policy.end = time.time()
        try:
            value = reply.json()
        except (ValueError, UnicodeError):
            value = None
        errors = preflight_errors(value, policy, self.kind, self.message)
        if reply.status != 202:
            errors.append('acceptance status != 202')
        self.valid[index], self.violations[index] = not errors, errors
        previous = value.get('job') if isinstance(value, dict) else None
        ident = previous.get('id') if isinstance(previous, dict) else None
        if isinstance(ident, str) and re.fullmatch('[a-f0-9]{32}', ident):
            self.ids[index] = ident
            deadline = time.monotonic() + 4
            while time.monotonic() < deadline:
                poll = send(self.pair.bases[index], 'GET', '/api/domain-discovery/probe/' + ident,
                            headers={'Authorization': 'Bearer ' + self.pair.tokens[index], 'X-ConfigFlow-Profile': self.pair.default_profile})
                try:
                    candidate = poll.json()
                except (ValueError, UnicodeError):
                    candidate = None
                policy.end = time.time()
                problems = preflight_errors(candidate, policy, self.kind, self.message)
                if poll.status != 200:
                    problems.append('poll status != 200')
                self.final[index] = candidate
                self.violations[index] += problems
                if problems:
                    break
                current = candidate['job']
                if current['id'] != ident or current['started_at'] != previous.get('started_at'):
                    self.violations[index].append('job identity/start changed')
                if previous.get('status') == 'failed' and previous != current:
                    self.violations[index].append('terminal job mutated')
                previous = current
                if current['status'] == 'failed':
                    break
                time.sleep(.02)
            else:
                self.violations[index].append('preflight did not finish within four seconds')
        if index == 0 and self.violations[index]:
            raise DesignError('Python async preflight premise: ' + repr((self.violations[index], value, self.final[index])))

    def capture(self, replies, policy):
        out = []
        if all(self.ids):
            try:
                policy.bind(*self.ids, r'[a-f0-9]{32}', 'discovery-job')
            except AssertionError as error:
                out.append(Difference('job.id', 'reused-Go-job', self.ids[0], str(error)))
        return out

    def compare(self, policy):
        out = []
        if self.violations[1]:
            out.append(Difference('job.lifecycle', 'invalid-Go-preflight', [], self.violations[1]))
        return out + differences(self.final[0], self.final[1], 'job.final', policy)

    def projection(self, value, index):
        return acceptance_view(value, self.valid[index])
