"""Held real region RPCs, target selection, 12-request batches and partial errors."""
import copy
import json

from agent_fake import Agent
from agent_jobs import Running
from compare import Difference, differences
from discovery_jobs import Completion
from harness import Pair, json_body
from region_fake import region_calls, target_call, result, PICKED
from cases.discovery_reads import heartbeat
from cases.domain_discovery import options
from cases.helpers import profile_policy
from cases.subscriptions import require


def region(binary, recorder):
    for case, services, domains, scenario in [('R054-S01', True, [], 'success'),
            ('R054-S02', False, ['example.com'], 'success'),
            ('R054-B02', False, ['xn--fiqs8s.example'], 'success'),
            ('R054-B01', True, ['x' + str(i) + '.example' for i in range(10)], 'success'),
            ('R054-B03', False, ['example.com'], 'partial')]:
        def responder(request, index):
            return result(request, index, scenario)
        with Agent(responder, gate_index=1) as left, Agent(responder, gate_index=1) as right:
            opts = options()
            opts['agent_updates'] = {'domain_discovery_enabled': True}
            opts['agent_port_pairs'] = {'agent-synthetic': [left.port, right.port]}
            opts['profile']['domain_discovery']['region_check_enabled'] = True
            with Pair(binary, recorder, options=opts) as pair:
                pair.policy = profile_policy(pair)
                variant = case + '/' + scenario + '/' + ('services' if services else 'domains') + '/' + str(len(domains))
                heartbeat(pair, variant=variant)
                pair.policy.times.add('files.regions/agent-synthetic.json.content.services.checked_at')
                pair.policy.times.add('body.result.checked_at')
                for host in domains:
                    pair.policy.times.add('files.regions/agent-synthetic.json.content.domains.' + host + '.checked_at')
                    pair.policy.times.add('body.domains.' + host + '.checked_at')
                observer = Running(pair, [left, right], 'region', 3)
                pair.request(case, 'POST /api/domain-discovery/region/check', body=json_body({'services': services, 'domains': domains}),
                             expect=202, writes=True, interaction=observer, capture=observer.capture, projection=observer.projection, variant=variant)
                pair.request('T19', 'GET /api/domain-discovery/probe/<job_id>',
                             paths=['/api/domain-discovery/probe/' + (ident or 'missing') for ident in observer.ids], expect=200, writes=True,
                             check=lambda r: require(r.json()['job']['status'] == 'running' and r.json()['job']['total'] == 3,
                                                     'target catalog finished and first region POST remains held at total 3'), variant=variant)
                expected = region_calls(domains, services)
                # Failing the first batch of the proxy target prevents a second
                # request for that target, while the other targets continue.
                if scenario == 'partial':
                    seen_proxy = False
                    selected = []
                    for call in expected:
                        if call['method'] == 'POST' and call['body']['target'] == '代理🧪':
                            if seen_proxy:
                                continue
                            seen_proxy = True
                        selected.append(call)
                    expected = selected
                observer.finish(expected, errors=1 if scenario == 'partial' else 0, variant=variant)
                saved = json.loads((pair.dirs[0] / 'regions/agent-synthetic.json').read_text())
                require(set(saved['domains']) == set(domains), 'exact requested domain result closure')
                if services:
                    require([t['name'] for t in saved['services']['targets']] == PICKED and len(saved['services']['matrix']) == 3,
                            'direct/preferred-group/first-alive-HK selected; global/internal/duplicate/dead nodes excluded')
                for host in domains:
                    require(saved['domains'][host]['restricted'] == ['DIRECT'] and saved['domains'][host]['targets']['香港节点🧪']['state'] == 'error',
                            '451 and per-page error classifications survive with other target data')
                pair.request('R053-S02', 'GET /api/domain-discovery/region', expect=200, writes=True, variant=variant)


def unavailable(binary, recorder):
    cases = [('404', 404, {'success': False}, 'Agent 合成 Agent 🧪 不支持区域检测，请升级'),
             ('409', 409, {'success': False}, 'Agent 上的 Mihomo 还没有检测入口，请为该 Agent 开启域名发现后部署一次配置'),
             ('invalid-json', 200, b'invalid synthetic JSON', 'Agent 返回 HTTP 200'),
             ('false', 200, {'success': False, 'message': '合成目标列表拒绝'}, '合成目标列表拒绝'),
             ('500-helper', 500, {'success': False, 'message': '合成列表错误'}, '合成列表错误')]
    for variant, status, body, message in cases:
        def responder(request, index):
            return status, body
        with Agent(responder) as left, Agent(responder) as right:
            left.release.set()
            right.release.set()
            opts = options()
            opts['agent_updates'] = {'domain_discovery_enabled': True}
            opts['agent_port_pairs'] = {'agent-synthetic': [left.port, right.port]}
            opts['profile']['domain_discovery']['region_check_enabled'] = True
            with Pair(binary, recorder, options=opts) as pair:
                pair.policy = profile_policy(pair)
                heartbeat(pair, variant=variant)
                observer = Completion(pair, 'region', message)
                def capture(replies, policy):
                    out = observer.capture(replies, policy)
                    if left.snapshot() != [target_call()] or left.errors:
                        from harness import DesignError
                        raise DesignError('Python target GET trace: ' + repr((left.snapshot(), left.errors)))
                    return out + differences(left.snapshot(), right.snapshot(), 'agent.requests', policy)
                pair.request('H075' if variant == '500-helper' else 'R054-F15', 'POST /api/domain-discovery/region/check', body=json_body({'services': True}),
                             expect=202, writes=True, interaction=observer, capture=capture, projection=observer.projection, variant=variant)


GROUPS = {'discovery-agent-region': region, 'discovery-region-target-errors': unavailable}
