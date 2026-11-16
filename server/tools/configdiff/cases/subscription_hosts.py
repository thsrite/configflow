"""G16: cached subscription hosts, DNS policy and manual artifact reports."""
import yaml

from artifacts import Observer
from harness import Pair, json_body
from seed import AGENT
from cases.generator_shapes import project
from cases.subscriptions import SUB, put_file, require


def matrix(binary, recorder):
    scenarios = ('absent', 'domain-unknown-core', 'ip-address', 'custom-conflict', 'name-only', 'cycle',
                 'dns-disabled', 'no-proxy-nameserver', 'policy-conflict', 'whitelist', 'two-sources', 'old-core', 'new-core')
    for scenario in scenarios:
        entries = {'node.example': 'entry.example.org'}
        custom = {'dns': {'enable': True, 'listen': '0.0.0.0:15353', 'enhanced-mode': 'fake-ip',
                          'fake-ip-range': '198.18.0.1/16', 'nameserver': ['https://dns.example/dns-query'],
                          'proxy-server-nameserver': ['https://proxy-dns.example/dns-query']}}
        if scenario == 'ip-address':
            entries = {'node.example': ['192.0.2.8', '2001:db8::8']}
        elif scenario == 'custom-conflict':
            custom['hosts'] = {'node.example': '192.0.2.99'}
        elif scenario == 'name-only':
            entries = {'display.example': 'entry.example.org'}
        elif scenario == 'cycle':
            custom['hosts'] = {'entry.example.org': 'node.example'}
        elif scenario == 'dns-disabled':
            custom['dns']['enable'] = False
        elif scenario == 'no-proxy-nameserver':
            del custom['dns']['proxy-server-nameserver']
        elif scenario == 'policy-conflict':
            custom['dns']['proxy-server-nameserver-policy'] = {'node.example': 'https://custom-dns.example/dns-query'}
        elif scenario == 'whitelist':
            custom['dns'].update({'fake-ip-filter-mode': 'whitelist', 'fake-ip-filter': ['entry.example.org']})
        subs = [{**SUB, 'url': ''}]
        if scenario == 'two-sources':
            subs.append({**SUB, 'id': 'sub-b', 'name': '来源乙🧪', 'url': ''})
        opts = {'shared': {'subscriptions': subs}, 'profile': {
            'proxy_groups': [{'id': 'hosts-group', 'name': '来源组🧪', 'type': 'select', 'subscriptions': [s['id'] for s in subs]}],
            'mihomo': {'custom_config': yaml.safe_dump(custom, sort_keys=False)}}}
        if scenario in ('old-core', 'new-core'):
            opts['agent_updates'] = {'domain_discovery_enabled': True}
        with Pair(binary, recorder, options=opts) as pair:
            if scenario in ('old-core', 'new-core'):
                for prefix in ('files.', 'artifact.files.ordinary.'):
                    pair.policy.times.add(prefix + 'traffic/agent-synthetic.json.content.status.at')
                version = 'v1.19.0' if scenario == 'old-core' else 'v1.19.33'
                pair.request('G16', 'POST /api/agents/<agent_id>/traffic-report', '/api/agents/agent-synthetic/traffic-report',
                             body=json_body({'schema': 1, 'status': 'running', 'mihomo_version': version, 'items': []}),
                             auth=AGENT, expect=200, writes=True, variant=scenario + '/public-core-report')
            for index, sub in enumerate(subs):
                metadata = {'url': ''}
                if scenario != 'absent':
                    metadata['hosts'] = {'entries': entries if index == 0 else {'node.example': 'other-entry.example.org'},
                                         'total': len(entries), 'skipped_count': 0, 'skipped': []}
                put_file(pair, 'shared/subscribes/' + sub['id'] + '.json', {'subscription_id': sub['id'],
                    'updated_at': '2026-01-01T00:00:00Z', 'count': 1, 'metadata': metadata,
                    'nodes': [{'id': 'hosts-node-' + sub['id'], 'name': 'display.example' if scenario == 'name-only' else '节点' + str(index),
                               'type': 'socks5', 'server': 'node.example', 'port': 1080}]})

            def check(reply):
                value = reply.json()
                main = yaml.safe_load(value['content'])
                report = value['subscription_hosts']
                merged = scenario not in ('absent', 'name-only', 'cycle', 'custom-conflict')
                require(report['merged'] == int(merged), 'hosts are merged only for a delivered server domain without higher-priority/cyclic mapping')
                if merged:
                    expected = ['192.0.2.8', '2001:db8::8'] if scenario == 'ip-address' else 'entry.example.org'
                    require(main['hosts']['node.example'] == expected, 'exact subscription hosts values appear in main configuration')
                if scenario == 'custom-conflict':
                    require(main['hosts']['node.example'] == '192.0.2.99', 'handwritten hosts keep precedence')
                reasons = {item.get('reason') for item in report['items'] if item['status'] == 'ignored'}
                reason = {'name-only': 'not_node_domain', 'cycle': 'cycle', 'custom-conflict': 'custom_hosts', 'two-sources': 'conflict'}.get(scenario)
                if reason:
                    require(reason in reasons and report['ignored'] == 1, 'report records the exact reason and affected item count')
                if not merged or scenario == 'ip-address':
                    require(report.get('dns') is None, 'no domain-to-domain mapping means no synthetic proxy DNS policy/report')
                    return
                dns = report['dns']
                applied = scenario in ('domain-unknown-core', 'two-sources', 'old-core', 'new-core')
                require(dns['applied'] is applied, 'DNS applied flag matches listener/upstream/policy/filter prerequisites')
                if applied:
                    require(main['dns']['proxy-server-nameserver-policy']['node.example'] == 'udp://127.0.0.1:15353'
                            and 'entry.example.org' in main['dns']['fake-ip-filter'], 'domain mapping is resolved through own listener with real-IP target exemption')
                    expected_old = [{'name': '合成 Agent 🧪', 'version': 'v1.19.0'}] if scenario == 'old-core' else []
                    require(dns['outdated_agents'] == expected_old and dns['min_version'] == 'v1.19.20'
                            and any('v1.19.20' in item for item in dns['notes']),
                            'only genuinely reported old cores are named, while unknown versions keep the minimum-version note')
                else:
                    require(dns['warnings'] or dns['skipped'], 'unapplied mapping has an explicit warning or conflict reason')

            pair.request('G16', 'POST /api/generate/mihomo/preview', body=json_body({'base_url': 'http://panel.example'}),
                         expect=200, writes=True, projection=project('mihomo'), check=check, variant=scenario + '/preview')
            observer = Observer(pair)
            def artifact_check(reply):
                observer.reference(reply)
                check(reply)
            pair.request('G16', 'POST /api/generate/<service>/artifact', '/api/generate/mihomo/artifact',
                         body=json_body({'base_url': 'http://panel.example'}), expect=200, writes=True, files=False,
                         check=artifact_check, capture=observer.capture, projection=observer.public, variant=scenario + '/artifact')


GROUPS = {'subscription-hosts-reports': matrix}
