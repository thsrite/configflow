"""Real asynchronous DAV task acceptance and completion after HTTP disconnect."""
from datetime import datetime
import http.client
import threading
import time
from urllib.parse import unquote, urlsplit
from zoneinfo import ZoneInfo

from compare import database, database_diffs, differences, file_diffs
from fakes import WebDAV
from harness import Pair, Reply, json_body
from task_lifecycle import Completion
from cases.profile_concurrency import record
from cases.subscriptions import require
from cases.webdav import Exchange

ROUTE = 'POST /api/tasks/run'


class DavCompletion(Completion):
    def __init__(self, pair, fake, *, count=1):
        super().__init__(pair, 'webdav_backup', count=count)
        self.wire = Exchange(fake, pair, backup=True, put_count=1)
        self.before_db = [None, None]
        self.sql_unchanged = [None, None]

    def before(self, index):
        super().before(index)
        self.wire.before(index)
        self.before_db[index] = database(self.pair.dirs[index])

    def after(self, index, reply):
        super().after(index, reply)
        self.wire.after(index, reply)
        self.sql_unchanged[index] = database(self.pair.dirs[index]) == self.before_db[index]
        if index == 0:
            require(self.sql_unchanged[0], 'asynchronous upload does not alter any configuration row')
            put = next(row for row in self.wire.observations[0]['requests'] if row['method'] == 'PUT')
            name = unquote(put['path']).rsplit('/', 1)[-1]
            stamp = datetime.strptime(name, 'config_backup_%Y%m%d_%H%M%S_%f.json').replace(tzinfo=ZoneInfo(self.pair.tz)).timestamp()
            require(self.pair.policy.start - 2 <= stamp <= time.time() + 2,
                    'real asynchronous upload filename has the correct local-time format and current window')

    def compare(self, policy):
        return (super().compare(policy) + self.wire.compare(policy)
                + differences(*self.sql_unchanged, 'task.DAV.configuration-unchanged', policy))


def settings(fake):
    return {'webdav_url': fake.url, 'webdav_username': fake.username, 'webdav_password': fake.password,
            'webdav_path': '/backup/', 'auto_backup': False, 'keep_latest': 0, 'keep_daily': 0}


def asynchronous(binary, recorder):
    with WebDAV() as fake, Pair(binary, recorder, options={'backup': settings(fake)}) as pair:
        for count in (1, 2):
            observer = DavCompletion(pair, fake, count=count)
            pair.request('R227-S04', ROUTE, body=json_body({'key': 'webdav_backup'}), expect=202,
                         writes=True, runtime_writes=True, interaction=observer, variant='actual-full-DAV-task-run-' + str(count))


def disconnect(binary, recorder):
    with WebDAV() as fake, Pair(binary, recorder, options={'backup': settings(fake)}) as pair:
        registered = pair.route_available(ROUTE)
        available = registered and pair.route_available('GET /api/tasks')
        observer = DavCompletion(pair, fake)
        accepted, gate_errors = [], []
        for index in range(2 if available else 1):
            entered, release = threading.Event(), threading.Event()
            timed_out = []
            def gate(method, path):
                if method == 'HEAD' and not entered.is_set():
                    entered.set()
                    if not release.wait(10):
                        timed_out.append('fake DAV gate timed out')
            fake.before_reply = gate
            observer.before(index)
            target = urlsplit(pair.bases[index])
            connection = http.client.HTTPConnection(target.hostname, target.port, timeout=5)
            try:
                connection.request('POST', '/api/tasks/run', body=json_body({'key': 'webdav_backup'}).encode(),
                    headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + pair.tokens[index]})
                started = entered.wait(4)
            finally:
                # The client reads neither response headers nor body. The real
                # task is demonstrably blocked on DAV while this socket closes.
                connection.close()
                release.set()
            if index == 0:
                require(started, 'accepted asynchronous task reaches the held DAV request before client cancellation')
            accepted.append(started)
            # Status zero means deliberately unobserved; it is never reported
            # as an actual HTTP 202/200. Only real durable/wire state is compared.
            observer.after(index, Reply(0, {}, b'{}'))
            gate_errors.append(timed_out)
        fake.before_reply = None
        pair.policy.end = time.time()
        diffs = []
        if available:
            diffs += observer.compare(pair.policy)
            diffs += differences(*accepted, 'task.disconnected.accepted-at-real-DAV-gate', pair.policy)
            diffs += differences(*gate_errors, 'task.disconnected.gate-errors', pair.policy)
            diffs += database_diffs(*pair.dirs, pair.policy) + file_diffs(*pair.dirs, pair.policy)
        # This streaming test has no observed initiating response; record only
        # the actual completion evidence, not an invented success status.
        record(pair, 'R227-B02', ROUTE, 'socket-closed-before-real-DAV-task-release', available, registered, diffs,
               [[Reply(0, {}, b'')], [Reply(0, {}, b'')]])
        pair.recorder.results[-1].update(initiating_response_unobserved=True, actual_DAV_acceptance=accepted,
                                        actual_gate_errors=gate_errors, python_statuses=None, go_statuses=None)


GROUPS = {'tasks-real-WebDAV-completion': asynchronous, 'tasks-disconnected-WebDAV-completion': disconnect}
