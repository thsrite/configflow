"""Website identification uses actual incoming traffic and held outgoing Agent calls."""
import copy
from datetime import datetime, timezone
import json

from agent_fake import Agent
from agent_jobs import Running
from harness import Pair, json_body
from seed import AGENT
from website_fake import calls, result, collected_host
from cases.discovery_reads import heartbeat
from cases.domain_discovery import options
from cases.helpers import profile_policy
from cases.subscriptions import require


def traffic(pair, domains, *, agent='agent-synthetic', token=AGENT, variant='website-dataset'):
    day = datetime.fromtimestamp(pair.policy.start, timezone.utc).date().isoformat()
    prefix = 'files.traffic/' + agent + '.json.content.'
    pair.policy.times.add(prefix + 'status.at')
    items = []
    for domain in domains:
        host = collected_host(domain)
        for field in ('first_seen', 'last_seen'):
            pair.policy.times.add(prefix + 'days.' + day + '.hosts.' + host + '.' + field)
        items.append({'host': host, 'port': 443, 'network': 'tcp', 'rule': 'Match', 'rule_payload': '',
                      'policy': 'DIRECT', 'outlet': 'direct', 'conns': 5, 'fails': 0, 'fail_kinds': {}, 'zero_dl': 0, 'up': 10, 'down': 20})
    pair.request('T20', 'POST /api/agents/<agent_id>/traffic-report', '/api/agents/' + agent + '/traffic-report',
                 body=json_body({'schema': 1, 'status': 'running', 'mihomo_version': 'v1.19.33', 'items': items}), auth=token,
                 expect=200, writes=True, variant=variant)


def website(binary, recorder):
    for case, scenario, domains, missing in [
            ('R060-S01', 'meta', ['example.com'], False),
            ('R060-S02', 'meta', ['example.com', 'example.org'], False),
            ('R060-B01', 'meta', ['xn--fiqs8s.example'], False),
            ('R060-B03', 'title', ['example.com'], False),
            ('R060-B03', 'long', ['example.com'], False),
            ('R060-S02', 'challenge', ['example.com'], False),
            ('R060-S02', 'empty', ['example.com'], False),
            ('R060-S02', 'rpc404', ['example.com', 'example.org'], False),
            ('R060-S02', 'meta', ['site' + str(i) + '.example' for i in range(30)], False),
            ('R060-S02', 'meta', ['example.com'], True)]:
        def responder(request, index):
            return result(request, index, scenario)
        with Agent(responder) as left, Agent(responder) as right:
            opts = options()
            opts['agent_updates'] = {'domain_discovery_enabled': True}
            opts['agent_port_pairs'] = {'agent-synthetic': [left.port, right.port]}
            with Pair(binary, recorder, options=opts) as pair:
                pair.policy = profile_policy(pair)
                variant = case + '/' + scenario + '/' + str(len(domains)) + ('/missing' if missing else '')
                heartbeat(pair, variant=variant)
                traffic(pair, domains, variant=variant + '/traffic')
                for domain in domains:
                    pair.policy.times.add('files.regions/agent-synthetic.json.content.websites.' + collected_host(domain) + '.checked_at')
                observer = Running(pair, [left, right], 'website', len(domains))
                body = {'domains': domains + (['not-reported.example'] if missing else [])}
                pair.request(case, 'POST /api/domain-discovery/website/identify', body=json_body(body), expect=202, writes=True, runtime_writes=True,
                             interaction=observer, capture=observer.capture, projection=observer.projection, variant=variant)
                if case == 'R060-S01':
                    pair.request('R060-F12', 'POST /api/domain-discovery/website/identify', body=json_body(body), expect=409, writes=True,
                                 check=lambda r: require(r.json()['job']['id'] == observer.ids[0], 'held website job prevents a second valid same-profile identification'))
                observer.finish(calls(domains), errors=(1 if scenario in ('challenge', 'empty', 'rpc404') or missing else 0), variant=variant)
                data = json.loads((pair.dirs[0] / 'regions/agent-synthetic.json').read_text())['websites']
                expected_domains = domains[1:] if scenario == 'rpc404' else domains
                require(set(data) == {collected_host(domain) for domain in expected_domains}, 'cache keyed by observed traffic host; RPC failures do not create a page result')
                for value in data.values():
                    expected_name = {'title': '合成 & 网站🧪', 'long': '中🧪' * 100,
                                     'challenge': '', 'empty': ''}.get(scenario, '合成 & 站点🧪')
                    require(value['name'] == expected_name, 'HTML entity/meta/title cleanup or 200-codepoint boundary matches the known synthetic value')
                    if expected_name:
                        require(value['host'].startswith('www.') and value['final_host'] == 'redirect.example', 'www candidate selected, final URL reduced to hostname')
                require('synthetic-private' not in json.dumps(data), 'redirect query token never persisted')
                policy = copy.deepcopy(pair.policy)
                policy.times.add('body.job.finished_at')
                pair.request('R052-S02', 'GET /api/domain-discovery/probe/<job_id>',
                             paths=['/api/domain-discovery/probe/' + (ident or 'missing') for ident in observer.ids], expect=200, writes=True,
                             policy=policy, variant=variant)


GROUPS = {'discovery-agent-website': website}


def foreign_busy(binary, recorder):
    from cases.setup_completion import StatusExchange
    with Agent(lambda request, index: result(request, index, 'meta')) as left, \
            Agent(lambda request, index: result(request, index, 'meta')) as right, Agent() as other_left, Agent() as other_right:
        opts = options()
        opts['other_profile'] = copy.deepcopy(opts['profile'])
        ids = ('agent-synthetic', 'agent-second')
        tokens = (AGENT, AGENT + '-second')
        opts['agents'] = [{'id': ident, 'name': '网站采集🧪' + str(index), 'token': tokens[index],
            'profile_id': ('default', 'other')[index], 'service_type': 'mihomo', 'host': '127.0.0.1', 'port': 9,
            'enabled': True, 'version': '1.8.60-go', 'deployment_method': 'shell', 'domain_discovery_enabled': True}
            for index, ident in enumerate(ids)]
        opts['agent_port_pairs'] = {ids[0]: [left.port, right.port], ids[1]: [other_left.port, other_right.port]}
        with Pair(binary, recorder, options=opts) as pair:
            pair.policy = profile_policy(pair)
            for ident, token in zip(ids, tokens):
                pair.request('T20', 'POST /api/agents/<agent_id>/heartbeat', '/api/agents/' + ident + '/heartbeat',
                    body=json_body({'version': '1.8.60-go', 'service_status': 'running'}), auth=token,
                    expect=200, writes=True, variant='foreign-website-busy/' + ident + '/heartbeat')
                traffic(pair, ['example.com'], agent=ident, token=token, variant='foreign-website-busy/' + ident + '/traffic')
            pair.policy.times.add('files.regions/agent-synthetic.json.content.websites.' + collected_host('example.com') + '.checked_at')
            observer = Running(pair, [left, right], 'website', 1)
            pair.request('R060-S01', 'POST /api/domain-discovery/website/identify', body=json_body({'domains': ['example.com']}),
                expect=202, writes=True, runtime_writes=True, interaction=observer,
                capture=observer.capture, projection=observer.projection, variant='foreign-profile-busy/held-source-website-job')
            def rejected(reply):
                require(reply.json() == {'success': False, 'message': '另一个配置空间的探测任务正在运行，请稍后再试', 'job': None},
                        'foreign busy response reveals no job identity even though the other profile has valid online collected traffic')
            pair.request('R060-F13', 'POST /api/domain-discovery/website/identify', '/api/domain-discovery/website/identify?profile=other',
                body=json_body({'domains': ['example.com']}), expect=409, writes=True, check=rejected,
                interaction=StatusExchange(pair, [other_left, other_right], expected=0, readonly=True),
                variant='real-other-profile-online-Agent-and-traffic-while-default-job-held')
            observer.finish(calls(['example.com']), variant='foreign-profile-busy/original-job-completes')


GROUPS['discovery-website-foreign-profile-busy'] = foreign_busy
