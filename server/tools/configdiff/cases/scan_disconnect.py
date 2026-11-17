"""A browser disconnects while duplicate scan is waiting for its real source."""
import copy
import http.client
import socket
import threading
import time
from urllib.parse import urlsplit

from compare import database, differences, files
from composition import cache_name
from harness import Pair
from sources import Source
from subscriptions import Exchange
from cases.rule_library import DOMAIN
from cases.rules import options
from cases.subscriptions import require


class CancelledScan(Exchange):
    def __init__(self, pair, source, declaration):
        super().__init__(pair, source)
        self.declaration = declaration
        self.observed = [None, None]

    def before(self, index):
        super().before(index)
        self.baseline = database(self.pair.dirs[index]), files(self.pair.dirs[index])
        entered, release = threading.Event(), threading.Event()
        errors = []
        def gate(path):
            entered.set()
            if not release.wait(8):
                errors.append('source response was not released')
        self.source.before_reply = gate
        target = urlsplit(self.pair.bases[index])
        conn = http.client.HTTPConnection(target.hostname, target.port, timeout=5)
        conn.connect()
        transport = conn.sock
        try:
            conn.request('POST', '/api/rules/find-duplicates', body=b'{}', headers={
                'Content-Type': 'application/json', 'Authorization': 'Bearer ' + self.pair.tokens[index]})
            reached = entered.wait(5)
            # No getresponse(): neither the initiating status nor JSON is known.
            transport.shutdown(socket.SHUT_RDWR)
        finally:
            conn.close()
            release.set()
        path = self.pair.dirs[index] / cache_name(self.declaration)
        deadline = time.monotonic() + 5
        cached = False
        while time.monotonic() < deadline:
            try:
                cached = path.read_bytes() == self.source.body and not (path.parent / (path.name + '.bak')).exists()
            except OSError:
                pass
            if cached:
                break
            time.sleep(0.02)
        self.source.before_reply = lambda path: None
        self.observed[index] = {'held_source_reached': reached, 'socket_closed': transport.fileno() == -1,
            'complete_new_cache_without_invented_backup': cached, 'gate_errors': errors}
        if index == 0:
            require(self.observed[0] == {'held_source_reached': True, 'socket_closed': True,
                'complete_new_cache_without_invented_backup': True, 'gate_errors': []},
                'large real source remains completely cached after browser abort: ' + repr(self.observed[0]))

    def after(self, index, reply):
        super().after(index, reply)
        expected = copy.deepcopy(self.baseline[1])
        name = cache_name(self.declaration)
        expected[name] = {'mode': 0o600, 'content': self.source.body}
        self.observed[index].update({'configuration_unchanged': database(self.pair.dirs[index]) == self.baseline[0],
            'only_exact_rule_caches_written': files(self.pair.dirs[index]) == expected})
        if index == 0:
            require(self.observed[0]['configuration_unchanged'] and self.observed[0]['only_exact_rule_caches_written'],
                'disconnect and following scan only write the exact new raw source cache file')

    def compare(self, policy):
        return super().compare(policy) + differences(*self.observed, 'scan.disconnected', policy)


def cancelled(binary, recorder):
    body = ''.join('+.entry' + str(i) + '.example\n' for i in range(60000)).encode() + b'+.duplicate.example\n' * 2
    with Source(body) as source:
        declaration = {**DOMAIN, 'source_type': 'url', 'url': source.url + '/long-rules'}
        declaration.pop('content')
        fixture = options()
        fixture['shared']['rule_library'] = [declaration]
        fixture['profile']['rule_configs'] = [copy.deepcopy(fixture['profile']['rule_configs'][2])]
        with Pair(binary, recorder, options=fixture) as pair:
            pair.policy.elapsed_ms.add('body.elapsed_time')
            def check(reply):
                value = reply.json()
                require(value['success'] is True and value['stats'] == {'rules_checked': 0, 'rulesets_checked': 1,
                    'failed_rulesets': [], 'duplicate_groups': 1} and len(value['duplicates']) == 1
                    and value['duplicates'][0]['count'] == 2 and value['duplicates'][0]['value'] == 'duplicate.example',
                    'fresh real scan after aborted browser retains the complete large-source duplicate result')
            pair.request('R190-B04', 'POST /api/rules/find-duplicates', body='{}', expect=200, writes=True,
                interaction=CancelledScan(pair, source, declaration), check=check,
                variant='actual-browser-abort-at-source-gate-then-complete-cached-scan')


GROUPS = {'rule-duplicates-real-browser-disconnect': cancelled}
