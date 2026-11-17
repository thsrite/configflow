"""Hold the external worker while another public request tests batch locking."""
from concurrent.futures import ThreadPoolExecutor
import time

from compare import database, files, differences, response_diffs
from harness import Pair, send, json_body
from probe import Exchange
from cases.config_backup import message
from cases.nodes import NODE
from cases.probes import policy_for
from cases.subscriptions import require

ROUTE = 'POST /api/nodes/latency'


class BusyProbe(Exchange):
    def __init__(self, pair):
        super().__init__(pair)
        self.pair = pair
        self.entered, self.violations, self.first_replies = [False, False], [[], []], [None, None]
        self.release = self.path.with_suffix('.release')

    def before(self, index):
        super().before(index)
        self.release.unlink(missing_ok=True)
        self.previous = database(self.pair.dirs[index]), files(self.pair.dirs[index])
        self.pool = ThreadPoolExecutor(max_workers=1)
        self.future = self.pool.submit(send, self.pair.bases[index], 'POST', '/api/nodes/latency', '{}',
            {'Authorization': 'Bearer ' + self.pair.tokens[index], 'Content-Type': 'application/json'})
        deadline = time.monotonic() + 3
        while not self.path.read_text().strip() and not self.future.done() and time.monotonic() < deadline:
            time.sleep(0.01)
        self.entered[index] = bool(self.path.read_text().strip()) and not self.future.done()
        if index == 0 and not self.entered[0]:
            self.release.touch()
            self.pool.shutdown(wait=True)
            require(False, 'first Python request reaches the real external worker before the concurrent busy request')

    def after(self, index, reply):
        # The held first request cannot save results yet. Neither request may
        # mutate rows/files before the worker is explicitly released.
        self.violations[index] = differences(self.previous,
            (database(self.pair.dirs[index]), files(self.pair.dirs[index])), 'node.busy-before-release')
        self.release.touch()
        try:
            self.first_replies[index] = self.future.result(timeout=8)
        finally:
            self.pool.shutdown(wait=True)
        super().after(index, reply)
        if index == 0:
            require(not self.violations[0] and self.first_replies[0].status == 200,
                    'busy request writes nothing while first batch subsequently completes successfully')

    def compare(self, policy):
        return (super().compare(policy) + differences(True, self.entered[1], 'node.worker-entered')
                + self.violations[1] + response_diffs(*self.first_replies, policy, method='POST'))


def busy(binary, recorder):
    with Pair(binary, recorder, options={'shared': {'nodes': [NODE]}, 'probe_case': 'gate'}) as pair:
        policy_for(pair, ['["node","node-a"]'])
        pair.policy.times.add('files.shared/node_latency.json.bak.content.["node","node-a"].tested_at')
        pair.request('R098-F12', ROUTE, body='{}', expect=409, writes=True,
                     check=message('已有节点测速正在进行，请稍后重试'), interaction=BusyProbe(pair),
                     variant='first-worker-held-second-public-request')
        pair.request('T13', ROUTE, body=json_body({'keys': []}), expect=200, writes=True,
                     interaction=Exchange(pair, 0), variant='busy-lock-released-after-first-batch')


GROUPS = {'node-helper-concurrent-busy': busy}
