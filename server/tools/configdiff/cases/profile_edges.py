"""Profile clone/reference/export and legacy Agent binding boundary recipes."""
import copy

from compare import database
from harness import Pair, json_body
from seed import CURRENT, RETIRED, AGENT
from sources import Source
from subscriptions import Exchange
from cases.config_backup import import_request, seed_document
from cases.helpers import profile_capture
from cases.nodes import NODE
from cases.pull_routes import SourceStable
from cases.setup_lifecycle import ROUTER
from cases.subscriptions import require


def persisted_implicit_agent(pair):
    """Restore a real legacy input; current-schema missing bindings are invalid."""
    source = seed_document(pair)
    agents = copy.deepcopy(source['system']['agents'])
    agents[0].pop('profile_id')
    payload = {'schema_version': 2, 'system': {'schema_version': 2,
        'system_config': source['system']['system_config'], 'backup': source['system']['backup'], 'agents': agents,
        'profiles': [{'id': pid, 'name': value['name']} for pid, value in source['profiles'].items()]},
        'profiles': source['profiles']}
    import_request(pair, 'R028-S03', payload, expected_token=CURRENT, variant='legacy-agent-without-profile-binding')


def binding_edges(binary, recorder):
    for legacy in (False, True):
        with Source(b'profile binding must not push') as source, Pair(binary, recorder,
                options={'agent_updates': {'port': source.server.server_port, 'enabled': False}}) as pair:
            if legacy:
                persisted_implicit_agent(pair)
            def bindings(reply):
                require([agent['id'] for agent in reply.json()['agents']] == ['agent-synthetic'],
                        'disabled Agent remains a usage; legacy missing binding resolves to actual default')
                require(all('token' not in agent and 'deployments' not in agent for agent in reply.json()['agents']),
                        'binding summaries expose identity without credentials or deployment histories')
            pair.request('R120-S02' if legacy else 'R120-B01', 'GET /api/profiles/<profile_id>/agents',
                '/api/profiles/default/agents', expect=200, writes=True, check=bindings, interaction=SourceStable(pair, source),
                variant='implicit-default' if legacy else 'disabled-counts-as-usage')
            pair.policy.times.add('db.fields.system//agents.value[0].updated_at')
            def moved(reply):
                require(reply.json()['success'] is True and reply.json()['from_profile']['id'] == 'default'
                        and reply.json()['to_profile']['id'] == 'other', 'URL-selected source rebinds to exact target')
                agents = database(pair.dirs[0])['fields']['system//agents']['value']
                require(agents[0]['profile_id'] == 'other' and agents[0]['enabled'] is False,
                        'all bindings become explicit at target without enabling the device')
            pair.request('R121-B01' if legacy else 'R121-B03', 'POST /api/profiles/<profile_id>/agents/rebind',
                '/api/profiles/default/agents/rebind?profile=other', headers={'X-ConfigFlow-Profile': 'other'},
                body=json_body({'target_profile_id': 'other', 'agent_ids': ['agent-synthetic']}), expect=200, writes=True,
                check=moved, interaction=Exchange(pair, source, expected=0),
                variant='implicit-default-becomes-explicit' if legacy else 'URL-over-query-header')


def clones(binary, recorder):
    profile = {'mihomo': {'custom_config': 'mixed-port: 19281\n'},
               'proxy_groups': [{'id': 'shared-reference-group', 'name': '共享引用🧪', 'type': 'select', 'manual_nodes': [NODE['id']]}],
               'routers': {'items': [{**ROUTER, 'id': 'source-router', 'enabled': False, 'auto_rule_sync': False}]},
               'client_fixed_ips': {'192.0.2.8': {'router_id': 'source-router', 'vendor': 'ikuai', 'id': 'synthetic-lease',
                   'ip': '192.0.2.8', 'mac': '02:00:00:00:00:08', 'comment': 'cf-client:synthetic'}}}
    with Source(b'clone must not contact a device') as source, Pair(binary, recorder,
            options={'shared': {'nodes': [NODE]}, 'profile': {**profile, 'routers': {'items': [
                {**profile['routers']['items'][0], 'endpoint': source.url}]}},
                     'other_profile': {'mihomo': {'custom_config': 'mixed-port: 19282\n'}}}) as pair:
        shared = copy.deepcopy(database(pair.dirs[0])['fields']['shared//nodes'])
        original = copy.deepcopy(database(pair.dirs[0])['fields'])
        for case, target in [('R123-B02', 'clone-references'), ('R123-B03', 'clone-url-selected')]:
            for key in ('created_at', 'updated_at'):
                pair.policy.times.update({'body.' + key, 'db.fields.profile/' + target + '/' + key + '.value'})
            def check(reply):
                fields = database(pair.dirs[0])['fields']
                own = lambda key: fields['profile/' + target + '/' + key]['value']
                require(own('proxy_groups') == profile['proxy_groups'] and fields['shared//nodes'] == shared,
                        'clone retains exact shared references and never duplicates shared resources')
                require(own('mihomo')['custom_config'] == profile['mihomo']['custom_config'], 'source URL wins query/header')
                require(own('routers') == {'items': []} and own('client_fixed_ips') == {},
                        'clone clears source router credentials/ownership and fixed client bindings')
                require(all(fields[key] == value for key, value in original.items()), 'clone leaves every preexisting field and row revision exact')
            pair.request(case, 'POST /api/profiles/<profile_id>/clone', '/api/profiles/default/clone?profile=other',
                headers={'X-ConfigFlow-Profile': 'other'}, body=json_body({'id': target, 'name': '克隆引用🧪'}), expect=201,
                writes=True, check=check, interaction=Exchange(pair, source, expected=0), variant='shared-references-and-device-isolation')


def exports(binary, recorder):
    extension = {'flag': False, 'values': [], 'note': '扩展中文🧪'}
    with Source(b'export must not download') as source, Pair(binary, recorder,
            options={'redaction': CURRENT + ' ' + RETIRED + ' ' + AGENT,
                     'profile': {'name': '导出中文🧪', 'synthetic_profile_extension': extension,
                                 'mihomo': {'synthetic_extension': extension}}}) as pair:
        def check(reply):
            value = reply.json()
            require(value['description'] == '[REDACTED]', 'profile export redacts the entire string containing internal capabilities/Agent token')
            require(value['mihomo']['synthetic_extension'] == extension and value['synthetic_profile_extension'] == extension
                    and value['name'] == '导出中文🧪',
                    'unknown nested fields and exact Unicode metadata survive export')
            require(reply.headers.get('content-disposition') == 'attachment; filename=default.json', 'export attachment is named by selected profile ID')
        for case in ('R125-S02', 'R125-B01', 'R125-B02'):
            pair.request(case, 'GET /api/profiles/<profile_id>/export', '/api/profiles/default/export', expect=200,
                writes=True, check=check, interaction=SourceStable(pair, source), variant='capabilities-extensions-Unicode')


def write_selectors(binary, recorder):
    with Source(b'profile deletion must not contact a device') as source, Pair(binary, recorder) as pair:
        def check(reply):
            fields = database(pair.dirs[0])['fields']
            require(not any(key.startswith('profile/other/') for key in fields)
                    and fields['system//system_config']['value'].get('default_profile_id', 'default') == 'default',
                    'URL-selected other is deleted while query/header default survives')
        folder_capture = profile_capture(pair)
        def capture(replies, policy):
            original_times = set(policy.times)
            out = folder_capture(replies, policy)
            # Deleted-profile contents retain preexisting timestamps from the
            # identical seed; only its fresh tombstone name needs binding.
            policy.times = original_times
            return out
        pair.request('R117-B03', 'DELETE /api/profiles/<profile_id>', '/api/profiles/other?profile=default',
            headers={'X-ConfigFlow-Profile': 'default'}, expect=204, writes=True, check=check,
            capture=capture, interaction=Exchange(pair, source, expected=0), variant='URL-over-query-header')
    with Source(b'default switch must not rebind a device') as source, Pair(binary, recorder) as pair:
        before = database(pair.dirs[0])
        def check(reply):
            fields = database(pair.dirs[0])['fields']
            require(fields['system//system_config']['value']['default_profile_id'] == 'other'
                    and fields['system//agents'] == before['fields']['system//agents'], 'URL-selected default switch does not move any Agent')
            require(all(fields[key] == row for key, row in before['fields'].items() if key.startswith(('profile/', 'shared/'))),
                    'switch changes only default pointer/system transaction, never copies or edits profile/shared contents')
        pair.request('R124-B02', 'POST /api/profiles/<profile_id>/default', '/api/profiles/other/default?profile=default',
            headers={'X-ConfigFlow-Profile': 'default'}, body='{}', expect=200, writes=True, check=check,
            interaction=Exchange(pair, source, expected=0), variant='URL-over-query-header')
    with Source(b'independent import must not download') as source, Pair(binary, recorder) as pair:
        before = database(pair.dirs[0])
        pair.policy.times.add('db.fields.profile/other/updated_at.value')
        def check(reply):
            fields = database(pair.dirs[0])['fields']
            require(fields['profile/other/mihomo']['value']['custom_config'] == 'mixed-port: 19289\n',
                    'URL-selected target receives exact independent client text')
            require(all(fields[key] == row for key, row in before['fields'].items() if not key.startswith('profile/other/')),
                    'independent import preserves every foreign row and revision')
        pair.request('R137-B03', 'POST /api/profiles/<profile_id>/import', '/api/profiles/other/import?profile=default',
            headers={'X-ConfigFlow-Profile': 'default'}, body=json_body({'mihomo': {'custom_config': 'mixed-port: 19289\n'}}),
            expect=200, writes=True, check=check, interaction=Exchange(pair, source, expected=0), variant='URL-over-query-header')


GROUPS = {'profile-binding-legacy-edges': binding_edges, 'profile-clone-reference-edges': clones,
          'profile-export-edges': exports, 'profile-metadata-write-selectors': write_selectors}
