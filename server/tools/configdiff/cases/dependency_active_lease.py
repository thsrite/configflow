"""An actual conversion's runtime lease excludes a public mode-change writer."""
import json
import threading
from concurrent.futures import ThreadPoolExecutor

from compare import differences, response_diffs
from dependency_control import managed
from harness import json_body, send
from sources import FEED
from substore import normalized_node
from cases.config_backup import message
from cases.dependency_modes import ModeProtocol
from cases.node_parse_lines import SURGE, PROXY
from cases.setup import Stable
from cases.subscriptions import require


class ActiveConversion(ModeProtocol):
    def __init__(self, pair, health):
        super().__init__(pair, health, 0, readonly=True)
        self.conversion_stable = Stable(pair)
        self.secondary, self.protocol, self.proofs = [None, None], [None, None], [None, None]

    def before(self, index):
        self.conversion_stable.before(index)
        self.conversion_offset = len(self.health.requests)
        self.entered, self.release = threading.Event(), threading.Event()
        self.timed_out, self.created = False, set()
        self.previous_hook = self.health.before_reply
        def gate(path):
            self.previous_hook(path)
            record = self.health.requests[-1]
            if path == '/api/subs' and record['method'] == 'POST':
                body = json.loads(bytes.fromhex(record['body_hex']))
                self.created.add(body['name'])
                self.health.responses[path] = (201, b'{"status":"success"}', {'Content-Type': 'application/json'})
                self.entered.set()
                if not self.release.wait(15):
                    self.timed_out = True
            elif path.startswith('/download/'):
                self.health.responses[path] = (200, FEED, {'Content-Type': 'text/yaml'})
            elif path.startswith('/api/sub/') and record['method'] == 'DELETE':
                self.created.discard(path.rsplit('/', 1)[1])
                self.health.responses[path] = (204, b'', {})
        self.health.before_reply = gate
        self.pool = ThreadPoolExecutor(max_workers=1)
        self.future = self.pool.submit(send, self.pair.bases[index], 'POST', '/api/nodes/parse-lines',
            json_body({'lines': [SURGE]}), {'Authorization': 'Bearer ' + self.pair.tokens[index], 'Content-Type': 'application/json'}, 20)
        if not self.entered.wait(10):
            self.release.set()
            self.pool.shutdown(wait=True)
            self.health.before_reply = self.previous_hook
            raise RuntimeError('real converter never acquired its runtime lease and reached Sub-Store create')
        # These offsets start AFTER the converter is demonstrably in flight.
        # The primary mode request must do no health/control work until rejection.
        super().before(index)

    def after(self, index, reply):
        try:
            super().after(index, reply)
        finally:
            self.release.set()
            try:
                self.secondary[index] = self.future.result(timeout=20)
            finally:
                self.pool.shutdown(wait=True)
                self.health.before_reply = self.previous_hook
        rows = []
        for record in self.health.requests[self.conversion_offset:]:
            if record['path'].startswith(('/api/subs', '/download/', '/api/sub/')):
                rows.append({'method': record['method'], 'path': record['path'],
                    'body': json.loads(bytes.fromhex(record['body_hex'])) if record['body_hex'] else None})
        self.protocol[index] = normalized_node(rows, SURGE)
        self.proofs[index] = {'lease_gate_reached': self.entered.is_set(), 'lease_gate_timeout': self.timed_out,
            'temporary_subscriptions_cleaned': not self.created}
        self.conversion_stable.after(index, self.secondary[index])
        if index == 0:
            require(self.proofs[0] == {'lease_gate_reached': True, 'lease_gate_timeout': False, 'temporary_subscriptions_cleaned': True}
                and self.secondary[0].status == 200 and self.secondary[0].json() == {'success': True, 'results': [{'proxy': PROXY}]},
                'actual converter completes its exact HTTP protocol after the busy mode writer is rejected')

    def compare(self, policy):
        return (super().compare(policy) + self.conversion_stable.compare(policy)
            + differences(*self.protocol, 'dependency.active-converter-wire', policy)
            + differences(*self.proofs, 'dependency.active-lease-proof', policy)
            + response_diffs(*self.secondary, policy, method='POST'))


def busy(binary, recorder):
    with managed(binary, recorder) as (pair, health):
        if not pair.route_available('POST /api/nodes/parse-lines'):
            pair.unimplemented_mutations.add('POST /api/nodes/parse-lines')
        pair.request('H1075', 'POST /api/settings/dependencies/sub-store/mode', body=json_body({'mode': 'always'}),
            expect=409, writes=True, check=message('Sub-Store 正忙或正在更新，请稍后重试'),
            interaction=ActiveConversion(pair, health), variant='public-mode-writer-conflicts-with-real-in-flight-conversion')


GROUPS = {'dependency-real-active-conversion-excludes-mode-writer': busy}
