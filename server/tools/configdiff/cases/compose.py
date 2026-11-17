"""Atomic rule composition, source limits and generated resource identities."""
import copy

from compare import database
from harness import json_body
from cases.helpers import profile_pair as Pair
from composition import capture, identities
from sources import Source
from subscriptions import Exchange
from cases.rules import options, rules
from cases.rule_library import DOMAIN
from cases.subscriptions import require

ROUTE = 'POST /api/rules/compose'
VALUES = {'source': {'kind': 'values', 'rule_type': 'DOMAIN-SUFFIX', 'text': 'first.example\nsecond.example'},
          'rule': {'policy': 'DIRECT'}, 'dns': 'default', 'base_url': 'http://panel.example'}


def successful(binary, recorder):
    tests = [('S01', {}), ('S02', {'source': {'kind': 'values', 'text': 'IP-CIDR,192.0.2.0/24,no-resolve\nDOMAIN-REGEX,^a,b[.]example$'}}),
             ('S03', {'source': {'kind': 'library', 'library_rule_ids': ['lib-second']}}),
             ('S04', {'source': {'kind': 'new_library', 'library': {**DOMAIN, 'id': 'client-id-ignored', 'name': '新规则🧪'}}}),
             ('S06', {'rule': {'policy': 'DIRECT', 'position': 'bottom'}}),
             ('B01', {'source': {'kind': 'values', 'rule_type': 'DOMAIN', 'text': '\n'.join('limit' + str(i) + '.example' for i in range(200))}}),
             ('B03', {'source': {'kind': 'values', 'rule_type': 'DOMAIN', 'text': 'example.com\nexample.com\nnew.example\nnew.example'}}),
             ('B04', {'rule': {'policy': 'DIRECT', 'group_name': '中文组🧪', 'remark': '备注🧪'}}),
             ('B05', {'source': {'kind': 'new_library', 'library': {**DOMAIN, 'name': '缓存失败但提交'}}})]
    for case, patch in tests:
        opts = options()
        opts['shared']['rule_library'].append({**DOMAIN, 'id': 'lib-second', 'name': '另一个来源'})
        with Pair(binary, recorder, options=opts) as pair:
            if case == 'B05':
                for directory in pair.dirs:
                    parent = directory / 'shared/rules'
                    parent.mkdir(parents=True, exist_ok=True)
                    (parent / 'by-id').write_bytes(b'synthetic-cache-path-blocker')
            body = {**copy.deepcopy(VALUES), **patch}
            before = rules(pair)
            def check(reply):
                ids = identities(reply.json())
                created = reply.json()['data']['rules']
                require(bool(created) and bool(ids), 'composition really creates formatted resource IDs')
                if case == 'B01':
                    require(len(created) == 200, 'exact paste limit accepts 200 entries')
                if case == 'B03':
                    require(len(created) == 1 and len(reply.json()['skipped']) == 1 and created[0]['value'] == 'new.example', 'deduplicate paste and skip already stored value')
                if case == 'S06':
                    require(rules(pair)[:len(before)] == before, 'bottom insertion preserves all previous rule positions')
                if case == 'S04':
                    require(reply.json()['data']['library']['id'] != 'client-id-ignored', 'new library IDs are server-owned')
                if case == 'B05':
                    require(reply.json()['warnings'] == ['规则集已保存，但本地缓存失败，发布时会重新获取'], 'post-commit cache failure has exact warning')
                    require(any(item['id'] == reply.json()['data']['library']['id'] for item in database(pair.dirs[0])['fields']['shared//rule_library']['value']),
                            'cache failure leaves successfully committed shared source in database')
            pair.request('R187-' + case, ROUTE, body=json_body(body), expect=200, writes=True, check=check, capture=capture(pair))
    opts = options()
    opts['shared']['rule_library'] = [{**DOMAIN, 'id': 'many-' + str(i), 'name': '来源' + str(i)} for i in range(50)]
    opts['profile']['rule_configs'] = []
    with Pair(binary, recorder, options=opts) as pair:
        pair.request('R187-B02', ROUTE, body=json_body({**VALUES, 'source': {'kind': 'library', 'library_rule_ids': ['many-' + str(i) for i in range(50)]}}),
                     expect=200, writes=True, capture=capture(pair), check=lambda r: require(len(r.json()['data']['rules']) == 50, 'exactly 50 shared library references accepted'))


def reject(binary, recorder):
    tests = [('F08', []), ('F09', {'rule': {}}), ('F10', {'rule': {'policy': 'DIRECT', 'position': 'middle'}}),
             ('F11', {'source': {'kind': 'values', 'text': 1}}),
             ('F12', {'source': {'kind': 'values', 'rule_type': 'DOMAIN', 'text': '\n'.join(['example.com'] * 201)}}),
             ('F13', {'source': {'kind': 'values', 'rule_type': 'DOMAIN', 'text': 'example.com\nbad domain'}}),
             ('F14', {'source': {'kind': 'values', 'rule_type': 'DOMAIN', 'text': ' \n# only comment\n// comment'}}),
             ('F15', {'source': {'kind': 'values', 'rule_type': 'SRC-PORT', 'text': '80'}, 'dns': 'direct'}),
             ('F16', {'source': {'kind': 'library', 'library_rule_ids': []}}),
             ('F16', {'source': {'kind': 'library', 'library_rule_ids': ['lib-a'] * 51}}),
             ('F17', {'source': {'kind': 'library', 'library_rule_ids': ['lib-a', 'lib-a']}}),
             ('F18', {'source': {'kind': 'library', 'library_rule_ids': [1]}}),
             ('F19', {'source': {'kind': 'unknown'}}), ('F20', {'source': {'kind': 'new_library', 'library': []}}),
             ('F21', {'source': {'kind': 'new_library', 'library': {**DOMAIN, 'name': ' '}}}),
             ('F23', {'source': {'kind': 'new_library', 'library': {**DOMAIN, 'name': '无URL', 'source_type': 'url', 'url': ''}}}),
             ('F24', {'source': {'kind': 'new_library', 'library': DOMAIN}}), ('F25', {'rule': {'policy': 'missing'}}),
             ('F26', {'source': {'kind': 'library', 'library_rule_ids': ['missing']}}),
             ('F27', {'source': {'kind': 'library', 'library_rule_ids': ['lib-a']}})]
    with Pair(binary, recorder, options=options()) as pair:
        for number, (case, patch) in enumerate(tests):
            body = {**copy.deepcopy(VALUES), **patch} if isinstance(patch, dict) else patch
            before = database(pair.dirs[0])
            status = 404 if case == 'F26' else 409 if case == 'F27' else 400
            pair.request('R187-' + case, ROUTE, body=json_body(body), expect=status, writes=True,
                         check=lambda r: require(database(pair.dirs[0]) == before, 'composition error rolls back shared catalog and profile together'),
                         variant=str(number) if case == 'F16' else '')
    opts = options()
    opts['profile']['oxidns'] = {'config_mode': 'native', 'native_config': 'plugins: []\n'}
    with Pair(binary, recorder, options=opts) as pair:
        before = database(pair.dirs[0])
        pair.request('R187-F28', ROUTE,
                     body=json_body({**VALUES, 'dns': 'direct', 'source': {'kind': 'new_library', 'library': {**DOMAIN, 'name': '不得落盘'}}}),
                     expect=409, writes=True, check=lambda r: require(database(pair.dirs[0]) == before, 'native DNS rejection rolls back newly generated shared library too'))


def remote(binary, recorder):
    with Source(b'+.downloaded.example\n') as source:
        for case, status in [('S05', 200), ('F22', 403)]:
            source.status = status
            with Pair(binary, recorder, options=options()) as pair:
                body = {**VALUES, 'source': {'kind': 'new_library', 'library': {**DOMAIN, 'name': '新远端🧪', 'source_type': 'url', 'url': source.url + '/rules'}}}
                before = database(pair.dirs[0])
                def check(reply):
                    if status == 403:
                        require(database(pair.dirs[0]) == before, 'download failure never adds shared resource or profile composition')
                    else:
                        identities(reply.json())
                pair.request('R187-' + case, ROUTE, body=json_body(body), expect=200 if status == 200 else 502, writes=True,
                             capture=capture(pair) if status == 200 else None, interaction=Exchange(pair, source), check=check)


GROUPS = {'rules-compose-success': successful, 'rules-compose-reject': reject, 'rules-compose-remote': remote}
