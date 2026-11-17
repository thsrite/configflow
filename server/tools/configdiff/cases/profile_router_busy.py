"""Real router release HTTP work blocks profile deletion until it finishes."""
import threading
from concurrent.futures import ThreadPoolExecutor

from compare import response_diffs
from harness import Pair, send
from sources import Source
from subscriptions import Exchange
from cases.config_backup import message
from cases.setup import Stable
from cases.subscriptions import require

APPLY = 'POST /api/routers/<router_id>/apply'


class Busy(Stable):
    def __init__(self, pair, device):
        super().__init__(pair)
        self.device = device
        self.exchange = Exchange(pair, device)
        self.secondary = [None, None]

    def before(self, index):
        self.exchange.before(index)
        self.entered, self.release = threading.Event(), threading.Event()
        def gate(path):
            if path == '/rest/ip/route':
                self.entered.set()
                self.release.wait(10)
        self.device.before_reply = gate
        self.pool = ThreadPoolExecutor(max_workers=1)
        self.future = self.pool.submit(send, self.pair.bases[index], 'POST',
            '/api/routers/router-busy/apply?profile=other', '{}',
            {'Content-Type': 'application/json', 'Authorization': 'Bearer ' + self.pair.tokens[index]}, 15)
        started = self.entered.wait(5)
        if not started:
            self.release.set()
            self.pool.shutdown(wait=True)
        if index == 0:
            require(started, 'actual router apply holds its operation while the first real REST read is blocked')
        self.started = started
        super().before(index)

    def after(self, index, reply):
        try:
            # Check before the held publishing request is allowed to finish.
            super().after(index, reply)
        finally:
            self.release.set()
            try:
                self.secondary[index] = self.future.result(timeout=15)
            finally:
                self.pool.shutdown(wait=True)
                self.device.before_reply = lambda path: None
        self.exchange.after(index, reply)
        if index == 0:
            require(self.secondary[0].status == 502, 'released fake REST read fails cleanly without touching the device')
            require(self.exchange.transcripts[0][0]['method'] == 'GET'
                and self.exchange.transcripts[0][0]['path'] == '/rest/ip/route',
                'release never passes its failed first read into any device mutation')

    def compare(self, policy):
        return (super().compare(policy) + self.exchange.compare(policy)
                + response_diffs(*self.secondary, policy))


def blocked(binary, recorder):
    with Source(b'synthetic-device-refuses-read') as device:
        device.status = 401
        router = {'id': 'router-busy', 'name': '正在发布的路由器🧪', 'type': 'routeros', 'endpoint': device.url,
                  'username': 'synthetic-user', 'password': 'synthetic-password', 'auto_rule_sync': False}
        with Pair(binary, recorder, options={'other_profile': {'routers': {'items': [router]}}}) as pair:
            if not pair.route_available(APPLY):
                pair.unimplemented_mutations.add(APPLY)
            pair.request('R117-F13', 'DELETE /api/profiles/<profile_id>', '/api/profiles/other',
                         expect=409, writes=True, interaction=Busy(pair, device),
                         check=message('路由器正在发布或同步，不能替换连接或删除记录，请稍后重试'),
                         variant='real-router-apply-blocked-on-first-device-read')


GROUPS = {'profile-delete-real-router-publication-busy': blocked}
