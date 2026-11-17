"""P3–P6 select the actual destination of successful rule/group writes."""
import copy
from urllib.parse import urlencode

from compare import database, differences, files
from composition import capture as capture_composition
from harness import Pair, json_body
from inventory import selector_rows
from cases.groups import NODES, BASE
from cases.rules import RULES
from cases.rule_library import DOMAIN
from cases.profile_reads import selected
from cases.helpers import profile_policy
from cases.subscriptions import require

RECIPES = {
    'R149': ('/api/profiles/default/proxy-groups', {'id': 'selector-new-group', 'name': '选择器新组🧪', 'type': 'select', 'manual_nodes': ['n-a']}, 'group-add'),
    'R157': ('/api/proxy-groups', {'id': 'selector-new-group', 'name': '选择器新组🧪', 'type': 'select', 'manual_nodes': ['n-a']}, 'group-add'),
    'R158': ('/api/proxy-groups/g-b', None, 'group-delete'),
    'R159': ('/api/proxy-groups/g-b', {'remark': '选择器修改🧪'}, 'group-edit'),
    'R160': ('/api/proxy-groups/batch-health-url', {'ids': ['g-health'], 'url': 'https://probe.example/selector204'}, 'group-health'),
    'R163': ('/api/proxy-groups/reorder', {'ids': ['g-b'], 'position': 'top'}, 'group-reorder'),
    'R152': ('/api/profiles/default/rules', {'id': 'selector-new-rule', 'itemType': 'rule', 'rule_type': 'DOMAIN', 'value': 'selected.example', 'policy': 'DIRECT'}, 'rule-add'),
    'R183': ('/api/rules', {'id': 'selector-new-rule', 'itemType': 'rule', 'rule_type': 'DOMAIN', 'value': 'selected.example', 'policy': 'DIRECT'}, 'rule-add'),
    'R179': ('/api/rule-sets', {'id': 'selector-new-set', 'library_rule_id': 'lib-a', 'policy': 'DIRECT'}, 'set-add'),
    'R180': ('/api/rule-sets/rs-a', None, 'set-delete'),
    'R181': ('/api/rule-sets/rs-a', {'remark': '选择器规则集🧪'}, 'set-edit'),
    'R184': ('/api/rules/r-a', None, 'rule-delete'),
    'R185': ('/api/rules/r-a', {'remark': '选择器规则🧪'}, 'rule-edit'),
    'R186': ('/api/rules/batch', {'rule_type': 'DOMAIN', 'domains': ['selected.example'], 'policy': 'DIRECT'}, 'rule-batch'),
    'R187': ('/api/rules/compose', {'source': {'kind': 'values', 'rule_type': 'DOMAIN', 'text': 'selected.example'}, 'rule': {'policy': 'DIRECT'}, 'dns': 'default'}, 'rule-compose'),
    'R189': ('/api/rules/dns', {'routes': {'r-a': 'direct'}, 'base_url': 'http://panel.example'}, 'rule-dns'),
    'R191': ('/api/rules/group-name', {'ids': ['r-a'], 'group_name': '选择器标签🧪'}, 'rule-label'),
    'R194': ('/api/rules/policy', {'ids': ['r-a'], 'policy': 'REJECT'}, 'rule-policy'),
    'R195': ('/api/rules/reorder', {'ids': ['r-ip'], 'position': 'top'}, 'rule-reorder'),
}


def isolation_errors(before, after, owner):
    """Only the selected profile may change; every foreign row stays exact."""
    prefix = 'profile/' + owner + '/'
    out = differences({k: v for k, v in before['fields'].items() if not k.startswith(prefix)},
                      {k: v for k, v in after['fields'].items() if not k.startswith(prefix)}, 'unselected.fields')
    out += differences(before['sections'], after['sections'], 'sections')
    revision = prefix + '_revision'
    out += differences(before['fields'][revision]['value'] + 1, after['fields'][revision]['value'], 'selected.revision')
    out += differences(before['meta']['generation'] + 1, after['meta']['generation'], 'selected.generation')
    return out


class SelectedWrite:
    def __init__(self, pair, owner):
        self.pair, self.owner = pair, owner
        self.observed = [None, None]

    def before(self, index):
        self.before_db = database(self.pair.dirs[index])
        self.before_files = files(self.pair.dirs[index])

    def after(self, index, reply):
        self.observed[index] = isolation_errors(self.before_db, database(self.pair.dirs[index]), self.owner)
        # These recipes never fetch/create shared cache content or install a
        # service. An unexpected file write is observable on each server.
        self.observed[index] += differences(self.before_files, files(self.pair.dirs[index]), 'write.files')
        if index == 0:
            require(not self.observed[0], 'reference selector write changes one selected revision and no foreign row or file: ' + repr(self.observed[0]))

    def compare(self, policy):
        # Compare violations with the source's empty result; never normalize
        # foreign timestamps/revisions using the cross-implementation policy.
        return differences([], [vars(item) for item in self.observed[1]], 'profile.write-isolation')


def verify_result(pair, owner, operation):
    values = {key.rsplit('/', 1)[-1]: row['value'] for key, row in database(pair.dirs[0])['fields'].items()
              if key.startswith('profile/' + owner + '/')}
    groups, rules = values['proxy_groups'], values['rule_configs']
    def item(rows, ident):
        return next((value for value in rows if value['id'] == ident), None)
    checks = {
        'group-add': lambda: len(groups) == len(BASE) + 1 and item(groups, 'selector-new-group')['manual_nodes'] == ['n-a'],
        'group-delete': lambda: item(groups, 'g-b') is None and len(groups) == len(BASE) - 1,
        'group-edit': lambda: item(groups, 'g-b')['remark'] == '选择器修改🧪',
        'group-health': lambda: item(groups, 'g-health')['url'] == 'https://probe.example/selector204',
        'group-reorder': lambda: groups[0]['id'] == 'g-b',
        'rule-add': lambda: rules[0]['id'] == 'selector-new-rule' and rules[0]['value'] == 'selected.example',
        'set-add': lambda: item(rules, 'selector-new-set')['library_rule_id'] == 'lib-a',
        'set-delete': lambda: item(rules, 'rs-a') is None,
        'set-edit': lambda: item(rules, 'rs-a')['remark'] == '选择器规则集🧪',
        'rule-delete': lambda: item(rules, 'r-a') is None,
        'rule-edit': lambda: item(rules, 'r-a')['remark'] == '选择器规则🧪',
        'rule-batch': lambda: len(rules) == len(RULES) + 1 and rules[0]['value'] == 'selected.example',
        'rule-compose': lambda: len(rules) == len(RULES) + 1 and rules[0]['value'] == 'selected.example',
        'rule-dns': lambda: values['oxidns']['direct_rules'] == ['r-a'],
        'rule-label': lambda: item(rules, 'r-a')['group_name'] == '选择器标签🧪',
        'rule-policy': lambda: item(rules, 'r-a')['policy'] == 'REJECT',
        'rule-reorder': lambda: rules[0]['id'] == 'r-ip',
    }
    require(checks[operation](), 'the selected profile actually received the successful business edit: ' + operation)


def writes(binary, recorder):
    for rid, route, recipes in selector_rows():
        if rid not in RECIPES:
            continue
        path_base, body, operation = RECIPES[rid]
        for suffix, number in recipes:
            profile = {'proxy_groups': copy.deepcopy(BASE), 'rule_configs': copy.deepcopy(RULES)}
            fixture = {'shared': {'nodes': NODES, 'rule_library': [DOMAIN]},
                       'profile': profile, 'other_profile': copy.deepcopy(profile)}
            with Pair(binary, recorder, options=fixture) as pair:
                pair.policy = profile_policy(pair)
                query, header, owner = selected(route, number, pair.default_profile)
                path = path_base + ('?' + urlencode(query) if query else '')
                def check(reply):
                    require(reply.headers.get('x-configflow-profile') == owner, 'write response names URL/query/header/default winner')
                    verify_result(pair, owner, operation)
                pair.request(f'{rid}-B{suffix:02}', route, path, expect=200, writes=True,
                     body=json_body(body) if body is not None else None,
                     headers={'Host': 'panel.example', 'X-ConfigFlow-Profile': header},
                     check=check, interaction=SelectedWrite(pair, owner),
                     capture=capture_composition(pair) if operation == 'rule-compose' else None,
                     variant='P' + str(number) + '/successful-rule-group-write')


GROUPS = {'profile-rule-group-write-selectors': writes}
