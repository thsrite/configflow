"""Managed setup completion reads a real REST fake on an isolated client LAN."""
from contextlib import contextmanager
import base64
import copy
import json
import re
import subprocess
import threading
from concurrent.futures import ThreadPoolExecutor

from compare import database, differences, response_diffs, timestamp
from harness import json_body, send
from dependency_tls import require_native_network
from sources import Source
from cases.config_backup import message
from cases.setup import ACTION, PATH, PID, call, row, session
from cases.setup_completion import IDS, TOKENS, prepared, StatusExchange
from cases.setup_lifecycle import ROUTER, capture_draft
from cases.subscriptions import require

HOST = '192.0.2.240'


@contextmanager
def client_lan():
    # This guard already protects the dependency MITM: native Linux, explicit
    # wrapper marker, and the sole network interface is the private loopback.
    require_native_network()
    subprocess.run(['sudo', 'ip', 'address', 'add', HOST + '/32', 'dev', 'lo'], check=True, capture_output=True)
    try:
        yield
    finally:
        subprocess.run(['sudo', 'ip', 'address', 'del', HOST + '/32', 'dev', 'lo'], check=True, capture_output=True)


def router_responses(fake):
    tables = {'/ip/route': [
        {'.id': '*1', 'dst-address': '198.18.0.0/16', 'gateway': HOST, 'routing-table': 'main',
         'comment': 'cf:fake-ip', 'distance': '1', 'active': 'true', 'disabled': 'false'},
        {'.id': '*2', 'dst-address': '192.0.2.0/24', 'gateway': 'bridge1', 'routing-table': 'main',
         'comment': '', 'distance': '0', 'active': 'true', 'disabled': 'false', 'dynamic': 'true'}],
        '/ip/firewall/nat': [], '/ip/firewall/mangle': [], '/ip/firewall/filter': [],
        '/ip/dhcp-server/network': [{'.id': '*N', 'address': '192.0.2.0/24', 'gateway': '192.0.2.1',
                                    'dns-server': HOST, 'dns-none': 'false'}],
        '/ip/dhcp-server': [{'.id': '*S', 'name': 'synthetic-dhcp', 'disabled': 'false', 'use-radius': 'no'}],
        '/ip/dhcp-server/lease': [], '/ip/dhcp-server/option': [], '/ip/dhcp-server/option/sets': [], '/ip/dhcp-server/matcher': [],
        '/ip/address': [{'.id': '*A', 'address': '192.0.2.1/24', 'interface': 'bridge1', 'disabled': 'false'}],
        '/ipv6/nd': [{'.id': '*RA', 'interface': 'bridge1', 'advertise-dns': 'no', 'disabled': 'false', 'dynamic': 'false'}],
        '/interface/list': [], '/interface/list/member': [],
        '/interface': [{'.id': '*I', 'name': 'bridge1', 'type': 'bridge', 'running': 'true', 'disabled': 'false'}]}
    fake.status, fake.body = 404, b'{"message":"unknown synthetic RouterOS read"}'
    fake.responses = {'/rest' + path: (200, json.dumps(value).encode(), {'Content-Type': 'application/json'})
                      for path, value in tables.items()}
    return set(fake.responses)


class ManagedExchange:
    def __init__(self, pair, agents, fake, known, outbound=None, *, return_status=True):
        self.status = StatusExchange(pair, agents)
        self.pair, self.fake, self.known = pair, fake, known
        self.observed = [None, None]
        self.outbound, self.source_requests = outbound, [None, None]
        self.return_status = return_status

    def before(self, index):
        self.status.before(index)
        self.fake.requests.clear()
        self.source_offset = len(self.outbound.requests) if self.outbound else 0

    def after(self, index, reply):
        self.status.observed[index] = [{'requests': agent.snapshot()[offset:], 'errors': list(agent.errors)}
            for agent, offset in zip(self.status.agents, self.status.offsets)]
        self.observed[index] = copy.deepcopy(self.fake.requests)
        self.source_requests[index] = copy.deepcopy(self.outbound.requests[self.source_offset:]) if self.outbound else []
        if index == 0:
            require(self.source_requests[0] == [], 'known fake-ip-only managed finish opens no rule-source connection')
            expected = [{'method': 'GET', 'path': path, 'body_hex': '',
                'authorization': 'Bearer ' + TOKENS[0], 'accept': accept, 'content_type': ''}
                for path, accept in ([('/api/linux-gateway/status', 'application/json')]
                    + ([('/api/return-routing', '*/*')] if self.return_status else []))]
            require(self.status.observed[0] == [{'requests': expected, 'errors': []}, {'requests': [], 'errors': []}],
                    'managed finish reads full-TUN status and return-routing cleanup status only: ' + repr(self.status.observed[0]))
            wanted = 'Basic ' + base64.b64encode((ROUTER['username'] + ':' + ROUTER['password']).encode()).decode()
            require(bool(self.observed[0]) and all(item['method'] == 'GET' and item['path'] in self.known
                and item['authorization'] == wanted for item in self.observed[0]),
                'finish uses only known authenticated read-only RouterOS REST paths')

    def compare(self, policy):
        return (self.status.compare(policy) + differences(*self.observed, 'setup.managed.router-REST', policy)
                + differences(*self.source_requests, 'setup.managed.rule-source', policy))


class ChangedDuringInspection(ManagedExchange):
    """A real router PUT commits while finish waits on its first REST read."""
    def __init__(self, *args):
        super().__init__(*args)
        self.secondary = [None, None]
        self.proofs = [None, None]

    def before(self, index):
        super().before(index)
        self.baseline = database(self.pair.dirs[index])
        self.entered, self.release = threading.Event(), threading.Event()
        def gate(path):
            if path == '/rest/ip/route':
                self.entered.set()
                if not self.release.wait(10):
                    raise RuntimeError('concurrent router edit did not finish')
        self.fake.before_reply = gate
        identity = row(self.pair, index=index)['routers']['items'][0]['id']
        def edit():
            try:
                if not self.entered.wait(5):
                    raise RuntimeError('finish never reached the actual REST read')
                return send(self.pair.bases[index], 'PUT', '/api/routers/' + identity + '?profile=' + PID,
                    json_body({'name': '检查期间实际修改🧪'}),
                    {'Content-Type': 'application/json', 'Authorization': 'Bearer ' + self.pair.tokens[index]}, 5)
            finally:
                self.release.set()
        self.pool = ThreadPoolExecutor(max_workers=1)
        self.future = self.pool.submit(edit)

    def after(self, index, reply):
        try:
            self.secondary[index] = self.future.result(timeout=10)
        finally:
            self.release.set()
            self.pool.shutdown(wait=True)
            self.fake.before_reply = lambda path: None
        super().after(index, reply)
        after = database(self.pair.dirs[index])
        before = copy.deepcopy(self.baseline)
        key = 'profile/' + PID + '/routers'
        before['fields'][key]['value']['items'][0]['name'] = '检查期间实际修改🧪'
        # The public PUT owns one profile transaction; no other data may change.
        before['fields'][key]['revision'] += 1
        revision_key = 'profile/' + PID + '/_revision'
        before['fields'][revision_key]['value'] += 1
        before['fields'][revision_key]['revision'] += 1
        before['meta']['generation'] += 1
        updated = 'profile/' + PID + '/updated_at'
        before['fields'][updated]['revision'] = before['meta']['generation']
        before['fields'][updated]['value'] = after['fields'][updated]['value']
        commit = after['meta']['commit_id']
        new_commit = bool(re.fullmatch(r'[0-9a-f]{32}', commit)) and commit != before['meta']['commit_id']
        before['meta']['commit_id'] = commit
        self.proofs[index] = {'edit_status': self.secondary[index].status,
            'exact_single_router_edit': before == after, 'new_commit': new_commit,
            'actual_edit_time': timestamp(after['fields'][updated]['value'], self.pair.policy)}
        if index == 0:
            require(self.proofs[0] == {'edit_status': 200, 'exact_single_router_edit': True,
                'new_commit': True, 'actual_edit_time': True},
                'concurrent public PUT is the sole SQL change: ' + repr(differences(before, after, 'race.SQL')[:5]))

    def compare(self, policy):
        return (super().compare(policy) + response_diffs(*self.secondary, policy)
                + differences(*self.proofs, 'setup.managed.concurrent-edit', policy))


def finished(binary, recorder):
    with client_lan(), Source() as fake:
        known = router_responses(fake)
        with prepared(binary, recorder, 'router', 'managed-finish', service_host=HOST) as (pair, agents, outbound):
            agents[0].responses['/api/return-routing'] = {'success': True, 'managed': {'active': False, 'spec': None},
                                                        'safety': {'ok': True, 'blockers': []}}
            router = {**ROUTER, 'type': 'routeros', 'endpoint': fake.url}
            call(pair, 'R211-S08', ACTION, PATH + '/router-draft', body={'router': router},
                variant='managed-finish/private-draft', capture=capture_draft(pair),
                interaction=StatusExchange(pair, agents, expected=0))
            call(pair, 'R211-S10', ACTION, PATH + '/check', body={}, variant='managed-finish/attach-checked-router',
                interaction=StatusExchange(pair, agents))
            baseline = database(pair.dirs[0])
            route_response = fake.responses['/rest/ip/route']
            denied = ('RouterOS 认证失败，请核对 ConfigFlow 中的用户名、密码和账号允许的来源地址；'
                      '在 WinBox → System → Users / Groups 检查用户组的 api、rest-api 权限')
            fake.responses['/rest/ip/route'] = (401, b'{}', {'Content-Type': 'application/json'})
            call(pair, 'H1005', ACTION, PATH + '/finish', body={'router_mode': 'managed'}, expect=502,
                variant='real-router-first-REST-read-denied',
                check=lambda reply: (message(denied)(reply), require(database(pair.dirs[0]) == baseline,
                    'failed live router inspection never edits configuration')),
                interaction=ManagedExchange(pair, agents, fake, known, outbound, return_status=False))
            rows = json.loads(route_response[1])
            rows[0]['gateway'] = '192.0.2.241'
            fake.responses['/rest/ip/route'] = (200, json.dumps(rows).encode(), {'Content-Type': 'application/json'})
            call(pair, 'H1006', ACTION, PATH + '/finish', body={'router_mode': 'managed'}, expect=409,
                variant='real-router-drift-plan-is-not-ready',
                check=lambda reply: (message('路由器配置尚未同步，请在差异报告中预览并完成发布')(reply),
                    require(database(pair.dirs[0]) == baseline, 'nonempty or blocked live plan leaves every SQL row exact')),
                interaction=ManagedExchange(pair, agents, fake, known, outbound))
            fake.responses['/rest/ip/route'] = route_response
            call(pair, 'H1007', ACTION, PATH + '/finish', body={'router_mode': 'managed'}, expect=409,
                variant='actual-router-PUT-commits-during-live-inspection',
                check=message('路由器设置在检查期间已变化，请重新检查'),
                interaction=ChangedDuringInspection(pair, agents, fake, known, outbound))
            baseline = database(pair.dirs[0])
            def complete(reply):
                value = reply.json()
                require(value['completed'] is True and value['router_mode'] == 'managed'
                    and value['router_verification'] == 'verified' and value['step'] == value['max_step'] == 5,
                    'real read-only live router inspection yields managed completion')
                require(value['route_instructions'] == {'gateway': HOST, 'dns': HOST,
                    'routes': [{'cidr': '198.18.0.0/16', 'sources': ['fake-ip'], 'requires_source_exclusion': False}],
                    'warnings': [], 'incomplete': False}, 'managed completion retains the exact checked client-LAN route table')
                require(database(pair.dirs[0]) == baseline and row(pair)['routers']['items'][0]['proxy_gateway_agent_id'] == IDS[0]
                    and row(pair)['routers']['items'][0]['dns_agent_id'] == IDS[1],
                    'managed finish keeps exact SQL and the checked pair of installed Agents')
            call(pair, 'R211-S12', ACTION, PATH + '/finish', body={'router_mode': 'managed'},
                variant='real-REST-empty-plan-and-isolated-client-LAN', check=complete,
                interaction=ManagedExchange(pair, agents, fake, known, outbound))


GROUPS = {'setup-completion-real-managed-router': finished}
