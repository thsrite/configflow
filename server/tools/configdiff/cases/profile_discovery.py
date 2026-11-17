"""Profile priority for successful synchronous discovery writes."""
import copy
from urllib.parse import urlencode

from compare import database
from harness import Pair, json_body
from inventory import selector_rows
from cases.domain_discovery import options
from cases.profile_reads import selected
from cases.profile_writes import SelectedWrite
from cases.helpers import profile_policy
from cases.subscriptions import require

RECIPES = {
    'R049': {'values': ['selected.example']},
    'R050': {'values': ['selected.example']},
    'R058': {'auto_apply_min_confidence': 95},
    'R061': {'domain': 'selected.example', 'note': '选中空间的备注🧪'},
}


def writes(binary, recorder):
    for rid, route, recipes in selector_rows():
        if rid not in RECIPES:
            continue
        for suffix, number in recipes:
            fixture = options()
            if rid == 'R049':
                fixture['profile']['domain_discovery']['ignored'] = ['selected.example']
            fixture['other_profile'] = copy.deepcopy(fixture['profile'])
            with Pair(binary, recorder, options=fixture) as pair:
                pair.policy = profile_policy(pair)
                query, header, owner = selected(route, number, pair.default_profile)
                path = route.split(' ', 1)[1] + ('?' + urlencode(query) if query else '')
                def check(reply):
                    require(reply.headers.get('x-configflow-profile') == owner, 'discovery response names actual selected configuration space')
                    value = database(pair.dirs[0])['fields']['profile/' + owner + '/domain_discovery']['value']
                    if rid in ('R049', 'R050'):
                        require(value['ignored'] == ([] if rid == 'R049' else ['selected.example']), 'selected ignore set is really added or removed')
                    elif rid == 'R058':
                        require(value['auto_apply_min_confidence'] == 95, 'selected profile receives the new confidence threshold')
                    else:
                        require(value['website_notes']['selected.example'] == '选中空间的备注🧪', 'selected profile owns the exact Unicode note')
                pair.request(f'{rid}-B{suffix:02}', route, path, body=json_body(RECIPES[rid]), expect=200,
                     writes=True, check=check, interaction=SelectedWrite(pair, owner),
                     headers={'Host': 'panel.example', 'X-ConfigFlow-Profile': header},
                     variant='P' + str(number) + '/successful-discovery-write')


GROUPS = {'profile-discovery-write-selectors': writes}
