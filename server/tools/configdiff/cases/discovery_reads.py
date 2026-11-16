"""Agent-originated traffic fixtures and domain/Agent/region read selectors."""
import json
from datetime import date, datetime, timedelta, timezone
from urllib.parse import urlencode

from harness import Pair, json_body
from seed import AGENT
from cases.domain_discovery import DIRECT, PROXY, options
from cases.rule_io import write_cache
from cases.subscriptions import put_file, require

HOSTS = ('www.uncovered.example.com', 'www.failing.example.org', 'www.ignored.example.net')


def report_clock(pair):
    day = datetime.fromtimestamp(pair.policy.start, timezone.utc).date().isoformat()
    pair.policy.times.add('files.traffic/agent-synthetic.json.content.status.at')
    for host in HOSTS:
        for field in ('first_seen', 'last_seen'):
            pair.policy.times.add('files.traffic/agent-synthetic.json.content.days.' + day + '.hosts.' + host + '.' + field)
    return day


def heartbeat(pair, version='1.8.60-go', variant='report-dataset'):
    pair.request('T20', 'POST /api/agents/<agent_id>/heartbeat', '/api/agents/agent-synthetic/heartbeat',
                 body=json_body({'version': version, 'service_status': 'running'}), auth=AGENT, expect=200,
                 writes=True, variant=variant + '/heartbeat')


def add_dataset(pair):
    report_clock(pair)
    heartbeat(pair)
    for library in (DIRECT, PROXY):
        write_cache(pair, library, library['content'].encode())
    items = [{'host': host, 'port': 443, 'network': 'tcp', 'rule': 'Match', 'rule_payload': '',
              'policy': 'DIRECT' if index else '代理🧪', 'outlet': 'direct' if index else 'proxy',
              'conns': 1 if index == 1 else 4, 'fails': 4 if index == 1 else 0,
              'fail_kinds': {'timeout': 4} if index == 1 else {}, 'zero_dl': 0, 'up': 10, 'down': 20}
             for index, host in enumerate(HOSTS)]
    pair.request('T20', 'POST /api/agents/<agent_id>/traffic-report', '/api/agents/agent-synthetic/traffic-report',
                 body=json_body({'schema': 1, 'status': 'running', 'mihomo_version': 'v1.19.33', 'items': items}),
                 auth=AGENT, expect=200, writes=True, variant='report-dataset/public-report')
    # The incoming protocol has already compared both stores. A common
    # synthetic historical bucket now isolates read-selector semantics without
    # requiring a frozen server clock or deriving expectations from Go.
    value = json.loads((pair.dirs[0] / 'traffic/agent-synthetic.json').read_text())
    old_day = (date.fromisoformat(max(value['days'])) - timedelta(days=3)).isoformat()
    stamp = old_day + 'T12:00:00'
    value['days'][old_day] = {'ip_only_conns': 0, 'dropped': 0, 'hosts': {'old.example.edu': {
        'first_seen': stamp, 'last_seen': stamp, 'ports': {'443': 1},
        'by_route': {'Match\t\tdirect\tDIRECT': {'conns': 1, 'fails': 0, 'fail_kinds': {}, 'zero_dl': 0, 'up': 1, 'down': 2}}}}}
    put_file(pair, 'traffic/agent-synthetic.json', value)
    for directory in pair.dirs:
        (directory / 'traffic/agent-synthetic.json').chmod(0o600)
    return stamp


def reply_clock(pair, reply, old_stamp):
    for index, item in enumerate(reply.json().get('items', [])):
        for field in ('first_seen', 'last_seen'):
            if item.get(field) != old_stamp:
                pair.policy.times.add(f'body.items[{index}].' + field)
        for child, host in enumerate(item.get('hosts', [])):
            for field in ('first_seen', 'last_seen'):
                if host.get(field) != old_stamp:
                    pair.policy.times.add(f'body.items[{index}].hosts[{child}].' + field)
    for index, _agent in enumerate(reply.json().get('agents', [])):
        pair.policy.times.add(f'body.agents[{index}].last_report')


def reads(binary, recorder):
    opts = options()
    opts['agent_updates'] = {'domain_discovery_enabled': True}
    opts['profile']['domain_discovery']['ignored'] = ['example.net']
    with Pair(binary, recorder, options=opts) as pair:
        old_stamp = add_dataset(pair)
        pair.policy.times.add('body.agents[0].last_report')
        pair.request('R045-S01', 'GET /api/domain-discovery/agents', expect=200, writes=True,
                     check=lambda r: require(len(r.json()['agents']) == 1 and r.json()['agents'][0]['supported'] is True
                         and r.json()['agents'][0]['status'] == 'running', 'real public heartbeat/report produces an enabled supported Agent state'))
        all_domains = {'example.com', 'example.org', 'example.edu'}
        today_domains = {'example.com', 'example.org'}
        vectors = [('S01', {'view': 'uncovered'}, all_domains), ('S02', {'view': 'failing'}, {'example.org'}),
            ('S03', {'view': 'all'}, all_domains), ('S04', {'view': 'ignored'}, {'example.net'}),
            ('S05', {'days': '1'}, today_domains), ('S06', {'days': '7'}, all_domains),
            ('S07', {'agent_id': 'agent-synthetic'}, all_domains), ('S07', {'agent_id': 'missing'}, set()),
            ('B01', {'days': 'abc'}, all_domains), ('B02', {'days': '0'}, today_domains),
            ('B03', {'days': '2'}, all_domains), ('B04', {'view': 'invalid'}, all_domains),
            ('B90', {'days': ' 1'}, today_domains), ('B90', {'days': '1_0'}, all_domains),
            ('B90', {'days': '-1'}, today_domains), ('B90', {'days': '٧'}, all_domains)]
        for case, query, expected in vectors:
            def check(reply):
                reply_clock(pair, reply, old_stamp)
                require({item['domain'] for item in reply.json()['items']} == expected,
                        'view/day/Agent selector returns the exact synthetic domain set')
            pair.request('R047-' + case, 'GET /api/domain-discovery/domains', '/api/domain-discovery/domains?' + urlencode(query),
                         expect=200, writes=True, check=check, variant='reported/' + repr(query))
        pair.request('R045-B02', 'GET /api/domain-discovery/agents', '/api/domain-discovery/agents?profile=other',
                     headers={'X-ConfigFlow-Profile': 'default'}, expect=200, writes=True,
                     check=lambda r: require(r.json()['agents'] == [], 'query selects other profile and excludes default-profile Agent'))
        pair.request('R053-B01', 'GET /api/domain-discovery/region', '/api/domain-discovery/region?profile=other',
                     expect=200, writes=True, check=lambda r: require(r.json()['agent'] is None and r.json()['domains'] == {},
                                                                   'another profile sees no bound Agent or region records'))
    with Pair(binary, recorder, options={'agents': []}) as pair:
        pair.request('R045-S02', 'GET /api/domain-discovery/agents', expect=200, writes=True,
                     check=lambda r: require(r.json()['agents'] == [], 'empty Agent catalog returns an empty array'))
        pair.request('R047-B05', 'GET /api/domain-discovery/domains', expect=200, writes=True,
                     check=lambda r: require(r.json()['items'] == [], 'missing reports produce an empty domain list'))
        pair.request('R053-S01', 'GET /api/domain-discovery/region', expect=200, writes=True,
                     check=lambda r: require(r.json()['result'] is None and r.json()['domains'] == {}, 'missing region data returns a default state'))


def versions(binary, recorder):
    for label, version, enabled, restart in [('old', '1.3.9-go', True, False), ('unknown', '', True, False),
                                           ('disabled', '1.8.60-go', False, False), ('offline-after-restart', '1.8.60-go', True, True)]:
        opts = options()
        opts['agent_updates'] = {'domain_discovery_enabled': enabled, 'version': version}
        with Pair(binary, recorder, options=opts) as pair:
            heartbeat(pair, version, label)
            if restart:
                pair.restart()
            pair.request('R045-B01', 'GET /api/domain-discovery/agents', expect=200, writes=True, variant=label,
                         check=lambda r: require(r.json()['agents'][0]['supported'] is (label not in ('old', 'unknown'))
                             and r.json()['agents'][0]['enabled'] is enabled, 'Agent support follows reported version and the independent discovery switch'))


GROUPS = {'discovery-reported-selectors': reads, 'discovery-agent-versions': versions}
