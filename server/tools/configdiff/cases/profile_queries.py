"""Profile selectors for read-only duplicate, matcher and shared regex previews."""
from urllib.parse import urlencode
from datetime import datetime, timezone

from harness import Pair, json_body
from inventory import selector_rows
from sources import Source
from cases.helpers import profile_policy
from cases.nodes import NODE
from cases.profile_reads import selected
from cases.pull_routes import SourceStable
from cases.subscriptions import require, put_file


def profile(owner):
    domain = 'selected.example' if owner == 'default' else 'other.example'
    return {'rule_configs': [{'id': owner + '-rule-' + str(i), 'itemType': 'rule', 'rule_type': 'DOMAIN',
                             'value': domain, 'policy': 'DIRECT', 'enabled': True} for i in range(2)]}


def selectors(binary, recorder):
    for rid, route, recipes in selector_rows():
        if rid not in ('R162', 'R190', 'R193', 'R212'):
            continue
        opts = {'profile': profile('default'), 'other_profile': profile('other'),
                'shared': {'nodes': [NODE], 'subscription_aggregations': [{'id': 'selector-aggregate', 'name': '共享候选🧪',
                                                                         'nodes': [NODE['id']], 'subscriptions': [], 'enabled': True}]}}
        if rid == 'R212':
            opts['other_profile']['rule_configs'].append({**opts['other_profile']['rule_configs'][0], 'id': 'other-rule-2'})
        with Source(b'local-only query fixture') as source, Pair(binary, recorder, options=opts) as pair:
            pair.policy = profile_policy(pair)
            pair.policy.elapsed_ms.add('body.elapsed_time')
            if rid == 'R212':
                # A real recent persisted sample takes the production throttle
                # path, so selection must neither rewrite nor move histories.
                stamp = datetime.now(timezone.utc).isoformat().replace('+00:00', 'Z')
                for profile_id in ('default', 'other'):
                    put_file(pair, 'profiles/' + profile_id + '/stats_history.json', {'samples': [{'at': stamp,
                        'subscriptions': 0, 'nodes': 0, 'proxyGroups': 0, 'rules': 0}]})
            for suffix, number in recipes:
                query, header, owner = selected(route, number, pair.default_profile)
                path = route.split(' ', 1)[1] + ('?' + urlencode(query) if query else '')
                body = {'query': 'selected.example'} if rid == 'R193' else ({'source': 'aggregation',
                        'aggregations': ['selector-aggregate'], 'regex': '节点'} if rid == 'R162' else {})
                def check(reply):
                    require(reply.headers.get('x-configflow-profile') == owner, 'read-only query identifies exact selected profile')
                    data = reply.json()
                    if rid == 'R190':
                        require(len(data['duplicates']) == 1 and data['duplicates'][0]['value'] ==
                            ('selected.example' if owner == 'default' else 'other.example')
                            and [item['rule_id'] for item in data['duplicates'][0]['occurrences']] == [owner + '-rule-0', owner + '-rule-1'],
                            'duplicate scanner actually reads selected composition, preserving occurrence order')
                    elif rid == 'R193':
                        require(data['matched'] is (owner == 'default') and data.get('rule_id') ==
                            ('default-rule-0' if owner == 'default' else None), 'matcher reads selected space rather than header-only labelling')
                    elif rid == 'R212':
                        expected = 2 if owner == 'default' else 3
                        require(data['data']['rules']['total'] == expected and data['data']['rules']['history'][-1] == expected
                                and data['data']['nodes']['total'] == 1,
                                'overview uses selected composition counts and shared nodes; throttle preserves both actual history files')
                    else:
                        require(data['count'] == data['total_candidates'] == 1 and data['nodes'][0]['name'] == NODE['name'],
                            'regex preview deliberately uses shared resources in every profile, without source downloads')
                pair.request(f'{rid}-B{suffix:02}', route, path, body=None if rid == 'R212' else json_body(body), expect=200, writes=True,
                    headers={'Host': 'panel.example', 'X-ConfigFlow-Profile': header}, check=check,
                    interaction=SourceStable(pair, source), variant='P' + str(number) + '/read-only-composition-query')


GROUPS = {'profile-query-selectors': selectors}
