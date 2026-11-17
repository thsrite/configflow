"""Setup session locking and continuation after a real browser disconnect."""
from concurrent.futures import ThreadPoolExecutor
import http.client
import threading
import time
from urllib.parse import urlsplit

from compare import Difference, database, database_diffs, file_diffs, response_diffs, differences
from harness import Reply, send, json_body
from cases.setup import ACTION, PATH, PID, SID, call, session
from cases.setup_completion import prepared, StatusExchange
from cases.subscriptions import require


def availability(pair):
    registered = pair.route_available(ACTION)
    return registered, registered and not pair.unimplemented_mutations


def record(pair, case, variant, diffs, *, registered, available, **extra):
    state = 'different' if diffs else 'compared'
    if not available:
        if registered:
            state = 'go-unimplemented-dependency'
            extra['blocked_by'] = sorted(pair.unimplemented_mutations)
        elif pair.declared.get(tuple(ACTION.split(' ', 1))) == 'done':
            state = 'different'
            diffs = [Difference('route.registration', 'declared-done-but-unregistered', True, False)]
        else:
            state = 'go-unimplemented'
    pair.recorder.record(case, variant, ACTION, 'B', state, diffs,
                         **extra)


def headers(pair, index):
    return {'Authorization': 'Bearer ' + pair.tokens[index], 'Content-Type': 'application/json'}


def saved_session(pair, index):
    try:
        return session(pair, index=index)
    except (ValueError, FileNotFoundError, AttributeError):
        if index == 0:
            raise
        return {}  # Missing/bad Go persistence remains a reported difference.


def serial(binary, recorder):
    with prepared(binary, recorder, 'router', 'session-serial') as (pair, agents, outbound):
        registered, available = availability(pair)
        before_db = database(pair.dirs[0])
        observer, batches, outcomes = StatusExchange(pair, agents), [], []
        for index, base in enumerate(pair.bases):
            if index and not available:
                break
            seen, release = threading.Event(), threading.Event()
            def gate():
                seen.set()
                if not release.wait(8):
                    agents[0].errors.append('same-session ordering gate was not released')
            agents[0].before_reply = gate
            observer.before(index)
            with ThreadPoolExecutor(max_workers=2) as pool:
                first = pool.submit(send, base, 'POST', PATH + '/check', json_body({}), headers(pair, index))
                contacted = seen.wait(5)
                second = pool.submit(send, base, 'PATCH', PATH, json_body({'step': 2}), headers(pair, index))
                # A separate public request must still be admitted while the
                # session action is held, avoiding a one-worker starvation
                # fixture that would appear serialized without a session lock.
                admitted = send(base, 'GET', '/api/auth/status', headers=headers(pair, index))
                time.sleep(0.25)
                blocked = not second.done()
                release.set()
                batch = [first.result(timeout=12), second.result(timeout=12)]
            agents[0].before_reply = None
            observer.after(index, batch[0])
            batches.append(batch)
            outcomes.append({'reached_agent': contacted, 'second_waited_for_session_lock': blocked,
                             'independent_request_status': admitted.status,
                             'final_step': saved_session(pair, index).get('step'),
                             'final_max_step': saved_session(pair, index).get('max_step')})
            if index == 0:
                require(outcomes[0] == {'reached_agent': True, 'second_waited_for_session_lock': True,
                    'independent_request_status': 200, 'final_step': 2, 'final_max_step': 4}
                    and [r.status for r in batch] == [200, 200] and batch[0].json()['step'] == 4
                    and batch[1].json()['step'] == 2 and database(pair.dirs[0]) == before_db,
                    'PATCH to the same session waits for checked installation, then preserves max_step=4 without SQL writes')
        pair.policy.end = time.time()
        diffs = []
        if available:
            for left, right in zip(*batches):
                diffs += response_diffs(left, right, pair.policy, method='POST')
            diffs += differences(*outcomes, 'setup.same-session-order') + observer.compare(pair.policy)
            diffs += database_diffs(*pair.dirs, pair.policy) + file_diffs(*pair.dirs, pair.policy)
        record(pair, 'R211-B02', 'check-held-then-same-session-PATCH', diffs, registered=registered, available=available,
               python_statuses=[r.status for r in batches[0]], go_statuses=[r.status for r in batches[1]] if available else None)


def disconnected(binary, recorder):
    with prepared(binary, recorder, 'router', 'setup-disconnect') as (pair, agents, outbound):
        registered, available = availability(pair)
        before_db = database(pair.dirs[0])
        observer, outcomes = StatusExchange(pair, agents), []
        for index, base in enumerate(pair.bases):
            if index and not available:
                break
            seen, release = threading.Event(), threading.Event()
            def gate():
                seen.set()
                if not release.wait(8):
                    agents[0].errors.append('disconnected-browser gate was not released')
            agents[0].before_reply = gate
            observer.before(index)
            url = urlsplit(base)
            client = http.client.HTTPConnection(url.hostname, url.port, timeout=5)
            try:
                client.request('POST', PATH + '/check', body=json_body({}).encode(), headers=headers(pair, index))
                contacted = seen.wait(5)
            finally:
                # No response read: close the browser before Agent work may
                # return, then observe actual persisted continuation.
                client.close()
                release.set()
            deadline = time.monotonic() + 5
            while time.monotonic() < deadline:
                try:
                    value = session(pair, index=index)
                    if value.get('step') == 4:
                        break
                except (ValueError, FileNotFoundError):
                    value = {}
                time.sleep(0.02)
            observer.after(index, Reply(0, {}, b''))
            agents[0].before_reply = None
            outcomes.append({'reached_agent_before_disconnect': contacted,
                             'step': value.get('step'), 'max_step': value.get('max_step'),
                             'installation': value.get('installation', {}).get('id')})
            if index == 0:
                from cases.setup_completion import INSTALL
                require(outcomes[0] == {'reached_agent_before_disconnect': True, 'step': 4, 'max_step': 4, 'installation': INSTALL}
                    and database(pair.dirs[0]) == before_db,
                    'closed browser does not cancel successful check or lose the durable receipt; no configuration is recommitted')
        pair.policy.end = time.time()
        diffs = []
        if available:
            diffs += differences(*outcomes, 'setup.closed-browser') + observer.compare(pair.policy)
            diffs += database_diffs(*pair.dirs, pair.policy) + file_diffs(*pair.dirs, pair.policy)
        record(pair, 'R211-B01', 'browser-closed-before-Agent-status-release', diffs, registered=registered, available=available,
               original_response='unobservable: browser socket closed before Agent response')
        call(pair, 'R209-S02', 'GET /api/setup/<session_id>', PATH, readonly=True,
             variant='setup-disconnect/resumed-by-public-read',
             check=lambda r: require(r.json()['step'] == r.json()['max_step'] == 4 and r.json()['profile_id'] == PID,
                                     'new browser reads the successful persisted check after the previous socket disconnected'))


GROUPS = {'setup-same-session-serial': serial, 'setup-browser-disconnect': disconnected}


def timeout_errors(elapsed):
    if type(elapsed) in (int, float) and 44 <= elapsed <= 50:
        return []
    return [Difference('setup.agent-status.elapsed', 'outside-production-read-timeout-window',
                       '44–50 seconds around the actual 45-second read timeout', elapsed)]


class TimedStatus(StatusExchange):
    def __init__(self, pair, agents, source=None):
        super().__init__(pair, agents, readonly=True)
        self.elapsed = [None, None]
        self.source, self.outbound = source, [[], []]

    def before(self, index):
        super().before(index)
        self.source_offset = len(self.source.requests) if self.source else 0
        self.started = time.monotonic()

    def after(self, index, reply):
        self.elapsed[index] = time.monotonic() - self.started
        if index == 0:
            require(not timeout_errors(self.elapsed[0]), 'reference actually reaches the unmodified production 45-second Agent read deadline')
        if self.source:
            self.outbound[index] = self.source.requests[self.source_offset:]
            if index == 0:
                require(not self.outbound[index], 'interval-zero synthetic library fixture makes no unsolicited upstream requests during the full timeout')
        super().after(index, reply)

    def compare(self, policy):
        return super().compare(policy) + timeout_errors(self.elapsed[1]) + differences([], self.outbound[1], 'setup.timeout-unsolicited-rule-fetch')


def status_timeout(binary, recorder):
    from cases.config_backup import message
    with prepared(binary, recorder, 'router', 'production-status-timeout', quiet_library=True) as (pair, agents, outbound):
        library = database(pair.dirs[0])['fields']['shared//rule_library']['value']
        require(bool(library) and all(item.get('source_type') == 'content' and item.get('update_interval') == 0 for item in library),
                'long request starts from synthetic content-only library names with every refresh interval zero')
        # Only the external fake waits; no production clock, request timeout or
        # timer is patched. The browser has its own longer 60-second budget.
        agents[0].expected_disconnect = True
        agents[0].before_reply = lambda: time.sleep(46)
        call(pair, 'R211-F22', ACTION, PATH + '/check', body={}, expect=502, timeout=60,
             variant='H998/actual-45-second-Agent-read-timeout',
             check=message('暂时无法连接 Agent，请检查设备连接。'), interaction=TimedStatus(pair, agents, outbound))
        agents[0].before_reply = None


GROUPS['setup-agent-status-timeout'] = status_timeout
