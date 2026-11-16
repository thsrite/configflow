"""P3–P6 on config GET routes: full replies plus observable selected profile.

This covers selection, not a replacement for each route's business S/F inputs.
Dynamic resource templates deliberately name absent objects; config pulls and
settings/list reads use real empty Python-imported profiles. No bad JSON body.
"""
import copy
from urllib.parse import urlencode

from compare import database, structure
from harness import Pair
from inventory import selector_rows
from cases.admin_auth import concrete
from cases.crosscutting import pull_filename
from cases.helpers import profile_policy, profile_capture
from cases.subscriptions import require

PREFIXES = ('/api/config/', '/api/custom-config/', '/api/domain-discovery/', '/api/mosdns/',
            '/api/oxidns/', '/api/profiles/', '/api/proxy-groups', '/api/rules', '/api/rule-sets')
OTHER_CONFIG = 'mixed-port: 19999\n# selector-other\n'


def project(route):
    def parse(value, index):
        if '/custom-config/' not in route or not isinstance(value, dict) or not isinstance(value.get('default_config'), str):
            return value
        service = route.rsplit('/', 1)[-1]
        name = {'surge': 'surge.conf', 'loon': 'loon.lcf'}.get(service, 'config.yaml')
        value = copy.deepcopy(value)
        # Only generated defaults are semantic text. The user's editable
        # config string, including comments/line endings, remains byte-exact.
        value['default_config'] = {'generated_text': True, 'parsed': structure(value['default_config'].encode(), filename=name)}
        return value
    return parse


def selected(route, number, default):
    explicit = '<profile_id>' in route
    if number == 3:
        query = {'profile': 'other'} if explicit else {'profile': default}
        header = 'other'
    elif number == 4:
        query, header = {'profile': default, 'profile_id': 'other'}, 'other'
    elif number == 5:
        query, header = {}, 'other'
    else:
        query, header = {}, None
    expected = default if explicit or number != 5 else 'other'
    return query, header, expected


def reads(binary, recorder):
    for rid, route, recipes in selector_rows():
        if not route.startswith('GET '):
            continue
        path_template = route.split(' ', 1)[1]
        provider = path_template.endswith(('/proxies', '/provider'))
        if not path_template.startswith(PREFIXES) and not provider:
            continue  # Live stats/dependencies need their dedicated observers.
        options = {'other_profile': {'description': 'selector-other🧪', 'mihomo': {'custom_config': OTHER_CONFIG},
            'proxy_groups': [{'id': 'selector-other-group', 'name': '其他空间组🧪', 'type': 'select', 'manual_nodes': ['DIRECT']}],
            'rule_configs': [{'id': 'selector-other-match', 'itemType': 'rule', 'rule_type': 'MATCH', 'policy': 'DIRECT'}],
            'domain_discovery': {'ignored': ['ignored.selector.example']}}}
        with Pair(binary, recorder, options=options) as pair:
            pair.policy = profile_policy(pair)
            default = pair.default_profile
            for suffix, number in recipes:
                query, header, expected = selected(route, number, default)
                path = concrete(path_template, default)
                if query:
                    path += '?' + urlencode(query)
                before = database(pair.dirs[0])
                def check(reply):
                    require(reply.headers.get('x-configflow-profile') == expected,
                            'public response identifies exact URL/query/header/default-selected profile')
                    require(database(pair.dirs[0]) == before, 'selector read preserves all DB rows, revisions and commit metadata')
                    if route.endswith('/custom-config/mihomo') and expected == 'other':
                        require(reply.json()['config'] == OTHER_CONFIG, 'selected other profile returns its own raw editor text')
                    if rid == 'R057':
                        require(reply.json()['ignored'] == (['ignored.selector.example'] if expected == 'other' else []),
                                'selected discovery settings carry the exact profile-specific ignored list')
                    if rid == 'R045':
                        require(len(reply.json()['agents']) == (0 if expected == 'other' else 1),
                                'selected Agent catalog follows seeded binding, without claiming an online heartbeat')
                capture = profile_capture(pair) if rid == 'R118' else None
                pair.request(f'{rid}-B{suffix:02}', route, path, expect=None, writes=True, check=check,
                             headers={'Host': 'panel.example', 'X-ConfigFlow-Profile': header}, capture=capture,
                             filename=pull_filename(route) if '/api/config/' in route else '',
                             projection=project(route),
                             variant='P' + str(number) + '/all-config-read-selectors')


GROUPS = {'profile-read-selectors': reads}
