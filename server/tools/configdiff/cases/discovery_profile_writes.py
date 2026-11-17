"""Successful shared-library writes with independent profiles and cache bytes."""
import copy
from urllib.parse import urlencode

from compare import database
from composition import cache_name
from discovery import Allocations
from harness import Pair, json_body
from inventory import selector_rows
from cases.domain_discovery import DIRECT, PROXY, GROUP, options
from cases.helpers import profile_policy
from cases.profile_reads import selected
from cases.rule_io import write_cache
from cases.setup import Stable
from cases.subscriptions import require


def fixture():
    value = options()
    value['other_profile'] = copy.deepcopy(value['profile'])
    other = value['other_profile']
    other_group = '其他空间组🧪'
    other['proxy_groups'][0]['name'] = other_group
    for old in (DIRECT, PROXY):
        new = {**copy.deepcopy(old), 'id': 'other-' + old['id'], 'name': '其他-' + old['name']}
        value['shared']['rule_library'].append(new)
        target = 'direct' if old['id'] == DIRECT['id'] else 'proxy'
        other['domain_discovery'][target + '_ruleset'] = new['id']
        for row in other['rule_configs']:
            if row.get('library_rule_id') == old['id']:
                row['library_rule_id'] = new['id']
                if target == 'proxy':
                    row['policy'] = other_group
    for owner, profile in (('default', value['profile']), ('other', other)):
        name = GROUP['name'] if owner == 'default' else other_group
        regional = {**copy.deepcopy(DIRECT), 'id': 'regional-' + owner, 'name': '域名发现-' + name}
        value['shared']['rule_library'].append(regional)
        profile['domain_discovery']['regional_rulesets'] = {name: regional['id']}
        profile['rule_configs'].insert(0, {'id': owner + '-regional-ref', 'itemType': 'ruleset',
            'library_rule_id': regional['id'], 'policy': name, 'enabled': True})
    return value


def foreign_snapshot(value, owner):
    allowed = {'shared//rule_library', 'shared//_revision'}
    return {'fields': {key: row for key, row in value['fields'].items()
                       if key not in allowed and not key.startswith('profile/' + owner + '/')},
            'sections': value['sections'],
            'meta': {key: row for key, row in value['meta'].items() if key not in ('commit_id', 'generation')}}


class ForeignRows(Stable):
    def __init__(self, pair, owner):
        super().__init__(pair)
        self.owner = owner

    def snapshot(self, index):
        return foreign_snapshot(database(self.pair.dirs[index]), self.owner)


def writes(binary, recorder):
    for rid, route, recipes in selector_rows():
        if rid not in ('R046', 'R055', 'R056', 'R059'):
            continue
        for suffix, number in recipes:
            if number == 3:
                continue
            with Pair(binary, recorder, options=fixture()) as pair:
                pair.policy = profile_policy(pair)
                query, header, owner = selected(route, number, pair.default_profile)
                path = route.split(' ', 1)[1] + ('?' + urlencode(query) if query else '')
                initial = database(pair.dirs[0])['fields']
                before_libraries = {row['id']: row for row in initial['shared//rule_library']['value']}
                for library in before_libraries.values():
                    if library.get('source_type') == 'content':
                        write_cache(pair, library, library['content'].encode())
                settings = initial['profile/' + owner + '/domain_discovery']['value']
                direct = settings['direct_ruleset']
                group = initial['profile/' + owner + '/proxy_groups']['value'][0]['name']
                bodies = {'R046': {'items': [{'value': 'selected.example', 'rule_type': 'DOMAIN-SUFFIX', 'target': 'direct'}]},
                          'R055': {'kind': 'domain', 'value': 'selected.example', 'policy': group},
                          'R056': {'targets': ['direct']}, 'R059': {'value': 'keep.example', 'target': 'direct'}}
                allocations = Allocations(pair, 1 if rid == 'R056' else 0, profile=owner)
                pair.policy.times.add('db.fields.profile/' + owner + '/domain_discovery.value.history[0].applied_at')
                def check(reply):
                    require(reply.headers.get('x-configflow-profile') == owner, 'successful shared mutation names its selected profile')
                    fields = database(pair.dirs[0])['fields']
                    current = fields['profile/' + owner + '/domain_discovery']['value']
                    libs = {row['id']: row for row in fields['shared//rule_library']['value']}
                    if rid == 'R046':
                        require('DOMAIN-SUFFIX,selected.example\n' in libs[direct]['content']
                            and current['history'][0]['value'] == 'selected.example', 'only selected direct library and adoption history receive the domain')
                    elif rid == 'R059':
                        require(libs[direct]['content'] == '# keep comment\n', 'selected direct source loses only requested domain, preserving its comment')
                    elif rid == 'R055':
                        regional = current['regional_rulesets'][group]
                        require('DOMAIN-SUFFIX,selected.example\n' in libs[regional]['content']
                            and current['history'][0]['target'] == 'policy:' + group, 'selected regional library and policy history receive domain')
                    else:
                        allocations.reference(reply)
                        require(current['direct_ruleset'] not in before_libraries
                            and reply.json()['direct']['id'] == current['direct_ruleset'], 'fresh init points the selected profile to its actual new source')
                    changed = [row for ident, row in libs.items() if before_libraries.get(ident) != row]
                    require(bool(changed), 'the valid mutation really changes/creates a shared source')
                    for library in changed:
                        require((pair.dirs[0] / cache_name(library)).read_bytes() == library['content'].encode(),
                                'each changed shared library cache contains the complete newly committed source bytes')
                pair.request(rid + '-B' + format(suffix, '02'), route, path, body=json_body(bodies[rid]),
                    headers={'X-ConfigFlow-Profile': header}, expect=200, writes=True, check=check,
                    interaction=ForeignRows(pair, owner), capture=allocations.capture,
                    variant='P' + str(number) + '/successful-shared-discovery-mutation')


GROUPS = {'discovery-shared-write-profile-selectors': writes}
