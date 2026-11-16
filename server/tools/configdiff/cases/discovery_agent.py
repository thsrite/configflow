"""Config discovery calls real held HTTP Agent fakes, including failed batches."""
import copy
import json

from agent_fake import Agent, probe_calls, probe_result
from agent_jobs import Running
from compare import database
from harness import Pair, json_body
from cases.discovery_reads import heartbeat
from cases.domain_discovery import options
from cases.helpers import profile_policy
from cases.subscriptions import require


def probe(binary, recorder):
    vectors = [('R051-S01', ['proxy.example.com'], None),
               ('R051-S02', ['x' + str(i) + '.example' for i in range(19)], None),
               ('R051-B01', ['x' + str(i) + '.example' for i in range(100)], None),
               ('R051-B02', ['proxy.example.com', 'proxy.example.com'], None)]
    vectors += [('R051-S02', ['x' + str(i) + '.example' for i in range(19)], scenario)
                for scenario in ('404', '500', 'invalid-json', 'false', 'bad-count')]
    for case, values, scenario in vectors:
        def responder(request, index):
            return probe_result(request, scenario if index == 0 and scenario else 'success')
        with Agent(responder) as left, Agent(responder) as right:
            opts = options()
            opts['agent_updates'] = {'domain_discovery_enabled': True}
            opts['agent_port_pairs'] = {'agent-synthetic': [left.port, right.port]}
            hosts = list(dict.fromkeys(values))
            variant = (scenario or 'success') + '/' + str(len(values))
            with Pair(binary, recorder, options=opts) as pair:
                pair.policy = profile_policy(pair)
                heartbeat(pair, variant=variant)
                for host in hosts:
                    pair.policy.times.add('files.probes/agent-synthetic.json.content.hosts.' + host + '.checked_at')
                observer = Running(pair, [left, right], 'probe', len(hosts))
                pair.request(case, 'POST /api/domain-discovery/probe', body=json_body({'domains': values}), expect=202, writes=True, runtime_writes=True,
                             interaction=observer, capture=observer.capture, projection=observer.projection, variant=variant)
                paths = ['/api/domain-discovery/probe/' + (ident or 'missing') for ident in observer.ids]
                pair.request('T19', 'GET /api/domain-discovery/probe/<job_id>', paths=paths, expect=200, writes=True, variant=variant + '/held',
                             check=lambda r: require(r.json()['job']['status'] == 'running' and r.json()['job']['done'] == 0
                                 and r.json()['job']['total'] == len(hosts), 'fake gate keeps an actual N-target job at running 0/N'))
                if case == 'R051-S01':
                    for ident, route, path in [('R051-F11', 'POST /api/domain-discovery/probe', '/api/domain-discovery/probe'),
                            ('R051-F12', 'POST /api/domain-discovery/probe', '/api/domain-discovery/probe?profile=other'),
                            ('H071', 'POST /api/domain-discovery/probe', '/api/domain-discovery/probe'),
                            ('R054-F11', 'POST /api/domain-discovery/region/check', '/api/domain-discovery/region/check'),
                            ('R054-F12', 'POST /api/domain-discovery/region/check', '/api/domain-discovery/region/check?profile=other')]:
                        pair.request(ident, route, path, body=json_body({'domains': ['example.net']}), expect=409, writes=True,
                                     check=lambda r, other='profile=other' in path: require((r.json()['job'] is None) if other else
                                         r.json()['job']['id'] == observer.ids[0], 'global busy hides foreign-profile job and retains same-profile identity'))
                observer.finish(probe_calls(hosts), errors=1 if scenario else 0, variant=variant)
                data_path = pair.dirs[0] / 'probes/agent-synthetic.json'
                saved = json.loads(data_path.read_text())['hosts'] if data_path.exists() else {}
                expected_hosts = hosts[18:] if scenario else hosts
                require(set(saved) == set(expected_hosts), 'failed first batch retains only later successful targets, with no control targets persisted')
                if case == 'R051-S01':
                    require(saved[hosts[0]]['verdict'] == 'needs_proxy' and saved[hosts[0]]['confidence'] == 85,
                            'fixed failed-direct/successful-proxy statistics yield 85 confidence')
                require(database(pair.dirs[0])['fields']['profile/default/domain_discovery']['value'].get('history', []) == [],
                        'manual probe does not auto-apply or append history')


GROUPS = {'discovery-agent-probe': probe}
