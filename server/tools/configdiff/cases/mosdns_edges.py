"""Whole-editor profile selection, unresolved rules and actual SQLite failure."""
import copy
from urllib.parse import urlencode

from compare import database
from harness import Pair, json_body
from cases.config_backup import message
from cases.helpers import profile_policy, deny_writes
from cases.profile_reads import selected
from cases.profile_writes import SelectedWrite
from cases.setup import Stable
from cases.subscriptions import require

ROUTE = 'POST /api/mosdns/settings'


def selectors(binary, recorder):
    for case, number in [('R090-B05', 4), ('R090-B06', 5), ('R090-B07', 6)]:
        profile = {'mosdns': {'synthetic_extension': {'values': [], 'flag': False}, 'api_addr': ':18080'}}
        with Pair(binary, recorder, options={'profile': profile, 'other_profile': copy.deepcopy(profile)}) as pair:
            pair.policy = profile_policy(pair)
            query, header, owner = selected(ROUTE, number, pair.default_profile)
            path = '/api/mosdns/settings' + ('?' + urlencode(query) if query else '')
            def check(reply):
                require(reply.headers.get('x-configflow-profile') == owner, 'query/header/default precedence selects actual editor destination')
                value = database(pair.dirs[0])['fields']['profile/' + owner + '/mosdns']['value']
                require(value['local_dns'] == '192.0.2.53' and value['cache_size'] == 321
                        and value['synthetic_extension'] == profile['mosdns']['synthetic_extension'] and 'api_addr' not in value,
                        'whole editor really changes selected settings, preserves unknown nested values and removes legacy alias')
                revision = database(pair.dirs[0])['fields']['profile/' + owner + '/_revision']['value']
                shared = database(pair.dirs[0])['fields']['shared//_revision']['value']
                require(reply.json() == {'success': True, 'publish_guard': {'expected_profile_id': owner,
                    'expected_service_type': 'mosdns', 'expected_config_revision': revision + shared}},
                    'publish guard describes committed selected profile and exact current shared revision')
            pair.request(case, ROUTE, path, body=json_body({'local_dns': '192.0.2.53', 'cache_size': 321}),
                headers={'X-ConfigFlow-Profile': header}, expect=200, writes=True, check=check,
                interaction=SelectedWrite(pair, owner), variant='P' + str(number) + '/whole-editor-selected-transaction')


def failures(binary, recorder):
    with Pair(binary, recorder) as pair:
        name = database(pair.dirs[0])['fields']['profile/default/name']['value']
        for field in ('direct_rulesets', 'proxy_rulesets', 'direct_rules', 'proxy_rules'):
            pair.request('R090-F09', ROUTE, body=json_body({field: ['missing-synthetic-rule']}), expect=409,
                writes=True, interaction=Stable(pair), variant=field + '/unresolved-rule-selection',
                check=message('配置 ' + name + ': MosDNS ' + field + ' 仍引用不存在的规则 missing-synthetic-rule，请先取消选择'))
        with deny_writes(pair):
            pair.request('R090-F10', ROUTE, body=json_body({'local_dns': '192.0.2.53'}), expect=500,
                writes=True, interaction=Stable(pair), variant='SQLite-trigger-rejects-real-editor-commit')


GROUPS = {'mosdns-whole-editor-selectors': selectors, 'mosdns-whole-editor-failures': failures}
