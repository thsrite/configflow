"""Two real Agent endpoints prove top-three host selection and failure isolation."""
from contextlib import ExitStack
from datetime import datetime, timezone
import json
import threading

from agent_fake import Agent, probe_calls, probe_result
from agent_jobs import Running
from harness import Pair, json_body
from seed import AGENT
from cases.domain_discovery import options
from cases.helpers import profile_policy
from cases.subscriptions import require

IDS = ('agent-synthetic', 'agent-second')
TOKENS = (AGENT, AGENT + '-second')
HOSTS = ('a.example.com', 'b.example.com', 'c.example.com', 'd.example.com')


class Fleet:
    def __init__(self, agents):
        self.agents = agents
        self.seen, self.release = threading.Event(), threading.Event()
        for agent in agents:
            agent.seen, agent.release = self.seen, self.release

    @property
    def errors(self):
        return [{'agent_id': ident, 'error': error} for ident, agent in zip(IDS, self.agents) for error in agent.errors]

    def snapshot(self):
        # Agent iteration uses a Python set. Retain every per-device request in
        # order, while comparing each independently identified endpoint by ID.
        return [{'agent_id': ident, 'requests': agent.snapshot()} for ident, agent in zip(IDS, self.agents)]


def reports(pair, ident, hosts, scenario):
    token = TOKENS[IDS.index(ident)]
    pair.request('T20', 'POST /api/agents/<agent_id>/heartbeat', '/api/agents/' + ident + '/heartbeat',
                 body=json_body({'version': '1.8.60-go', 'service_status': 'running'}), auth=token, expect=200, writes=True,
                 variant='multi/' + scenario + '/' + ident + '/heartbeat')
    day = datetime.fromtimestamp(pair.policy.start, timezone.utc).date().isoformat()
    prefix = 'files.traffic/' + ident + '.json.content.'
    pair.policy.times.add(prefix + 'status.at')
    items = []
    for host in hosts:
        for field in ('first_seen', 'last_seen'):
            pair.policy.times.add(prefix + 'days.' + day + '.hosts.' + host + '.' + field)
        items.append({'host': host, 'port': 443, 'network': 'tcp', 'rule': 'Match', 'rule_payload': '',
            'policy': 'DIRECT', 'outlet': 'direct', 'conns': {HOSTS[0]: 10, HOSTS[1]: 8, HOSTS[2]: 6, HOSTS[3]: 1}[host],
            'fails': 0, 'fail_kinds': {}, 'zero_dl': 0, 'up': 10, 'down': 20})
    pair.request('T20', 'POST /api/agents/<agent_id>/traffic-report', '/api/agents/' + ident + '/traffic-report',
                 body=json_body({'schema': 1, 'status': 'running', 'mihomo_version': 'v1.19.33', 'items': items}), auth=token,
                 expect=200, writes=True, variant='multi/' + scenario + '/' + ident + '/traffic')


def multi(binary, recorder):
    for scenario in ('success', 'one-agent-failed'):
        with ExitStack() as stack:
            fakes = [[stack.enter_context(Agent()) for _ in IDS] for _ in range(2)]
            if scenario == 'one-agent-failed':
                for backend in fakes:
                    backend[1].responder = lambda request, index: probe_result(request, '500')
            fleets = [Fleet(backend) for backend in fakes]
            opts = options()
            opts['agents'] = [{'id': ident, 'name': '合成多Agent-' + str(i), 'token': TOKENS[i], 'profile_id': 'default',
                              'service_type': 'mihomo', 'host': '127.0.0.1', 'port': 9, 'enabled': True,
                              'version': '1.8.60-go', 'deployment_method': 'shell', 'domain_discovery_enabled': True}
                             for i, ident in enumerate(IDS)]
            opts['agent_port_pairs'] = {ident: [fakes[0][i].port, fakes[1][i].port] for i, ident in enumerate(IDS)}
            with Pair(binary, recorder, options=opts) as pair:
                pair.policy = profile_policy(pair)
                reports(pair, IDS[0], [HOSTS[0], HOSTS[2], HOSTS[3]], scenario)
                reports(pair, IDS[1], [HOSTS[1]], scenario)
                for ident, selected in zip(IDS, ([HOSTS[0], HOSTS[2]], [HOSTS[1]])):
                    for host in selected:
                        pair.policy.times.add('files.probes/' + ident + '.json.content.hosts.' + host + '.checked_at')
                observer = Running(pair, fleets, 'probe', 3)
                pair.request('T20', 'POST /api/domain-discovery/probe', body=json_body({'domains': ['example.com']}),
                             expect=202, writes=True, interaction=observer, capture=observer.capture, projection=observer.projection,
                             variant='multi/' + scenario + '/start')
                pair.request('R052-S01', 'GET /api/domain-discovery/probe/<job_id>',
                             paths=['/api/domain-discovery/probe/' + (ident or 'missing') for ident in observer.ids],
                             expect=200, writes=True, check=lambda r: require(r.json()['job']['status'] == 'running'
                                 and r.json()['job']['done'] == 0 and r.json()['job']['total'] == 3, 'held first Agent preserves top-three job at 0/3'),
                             variant='multi/' + scenario)
                expected = [{'agent_id': IDS[0], 'requests': probe_calls([HOSTS[0], HOSTS[2]])},
                            {'agent_id': IDS[1], 'requests': probe_calls([HOSTS[1]], token=TOKENS[1])}]
                observer.finish(expected, errors=int(scenario == 'one-agent-failed'), variant='multi/' + scenario)
                actual = {}
                for ident in IDS:
                    path = pair.dirs[0] / ('probes/' + ident + '.json')
                    actual[ident] = json.loads(path.read_text())['hosts'] if path.exists() else {}
                require(set(actual[IDS[0]]) == {HOSTS[0], HOSTS[2]} and set(actual[IDS[1]]) ==
                        (set() if scenario == 'one-agent-failed' else {HOSTS[1]}),
                        'only top three concrete hosts selected; each goes to its reporting Agent and failure preserves other Agent results')


GROUPS = {'discovery-multiple-agents': multi}
