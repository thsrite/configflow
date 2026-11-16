"""G14: public restore/export round trips for all supported backup families."""
import copy

from harness import Pair
from seed import CURRENT, PULL
from cases.config_backup import seed_document, import_request, export_capture
from cases.helpers import profile_policy
from cases.subscriptions import require


def legacy_profile(suffix):
    return {'name': '恢复空间' + suffix + '🧪',
            'nodes': [{'id': 'restore-node-' + suffix, 'name': '节点' + suffix, 'type': 'socks5',
                       'server': '192.0.2.8', 'port': 1080, 'password': 'synthetic-restore-password'}],
            'subscriptions': [{'id': 'restore-sub-' + suffix, 'name': '来源' + suffix,
                               'url': 'https://source.example/' + suffix, 'interval': 0}],
            'rule_library': [{'id': 'restore-lib-' + suffix, 'name': '规则源' + suffix,
                              'source_type': 'content', 'content': '+.example.com\n',
                              'behavior': 'domain', 'format': 'text', 'update_interval': 0}],
            'proxy_groups': [{'id': 'restore-group-' + suffix, 'name': '策略' + suffix, 'type': 'select',
                              'manual_nodes': ['restore-node-' + suffix], 'subscriptions': ['restore-sub-' + suffix]}],
            'rule_configs': [{'id': 'restore-rule-' + suffix, 'itemType': 'ruleset',
                              'library_rule_id': 'restore-lib-' + suffix, 'policy': '策略' + suffix}],
            'mosdns': {'direct_rulesets': ['restore-rule-' + suffix]},
            'mihomo': {'custom_config': '# restored ' + suffix + '\ndns:\n  enable: true\n'}}


def check_resources(document, profiles):
    require(document['schema_version'] == 5 and set(document['profiles']) == set(profiles),
            'restored document has the current schema and exact profile closure')
    settings = document['system']['system_config']
    require(settings['rule_proxy_token'] == CURRENT and settings['config_token'] == PULL,
            'raw export retains both distinct capabilities')
    for kind, prefix in [('nodes', 'node'), ('subscriptions', 'sub'), ('rule_library', 'lib')]:
        require({item['id'] for item in document['shared'][kind]} == {'restore-' + prefix + '-' + p for p in profiles},
                'all original resources survive in the shared catalog: ' + kind)
    for pid in profiles:
        profile = document['profiles'][pid]
        require(profile['proxy_groups'][0]['manual_nodes'] == ['restore-node-' + pid]
                and profile['proxy_groups'][0]['subscriptions'] == ['restore-sub-' + pid]
                and profile['rule_configs'][0]['library_rule_id'] == 'restore-lib-' + pid
                and profile['rule_configs'][0]['policy'] == '策略' + pid
                and profile['mosdns']['direct_rulesets'] == ['restore-rule-' + pid],
                'profile-local references retain their exact shared targets')
        require('# restored ' + pid in profile['mihomo']['custom_config'], 'raw custom configuration survives')
    require(all(item['content'] == '+.example.com\n' for item in document['shared']['rule_library']),
            'inline source bytes survive restore and export')


def roundtrips(binary, recorder):
    for shape in ('current', 'legacy-single', 'schema2-multiple'):
        with Pair(binary, recorder) as pair:
            pair.policy = profile_policy(pair)
            profiles = ['default'] if shape == 'legacy-single' else ['default', 'other']
            settings = {'server_domain': 'https://restored.example/panel', 'rule_proxy_token': CURRENT,
                        'config_token': PULL, 'default_profile_id': 'default'}
            if shape == 'legacy-single':
                payload = {**legacy_profile('default'), 'system_config': settings, 'backup': {}, 'agents': []}
            elif shape == 'schema2-multiple':
                payload = {'schema_version': 2, 'system': {'schema_version': 2, 'system_config': settings,
                           'backup': {}, 'agents': [], 'profiles': [{'id': p, 'name': '恢复空间' + p} for p in profiles]},
                           'profiles': {p: legacy_profile(p) for p in profiles}}
            else:
                payload = seed_document(pair)
                payload['system']['system_config'].update(settings)
                for p in profiles:
                    legacy = legacy_profile(p)
                    for kind in ('nodes', 'subscriptions', 'rule_library'):
                        payload['shared'][kind].extend(legacy.pop(kind))
                    payload['profiles'][p].update(legacy)
            import_request(pair, 'G14', payload, variant=shape + '/import', expected_token=CURRENT)
            pair.restart()
            exported = pair.request('G14', 'GET /api/config/export', expect=200, writes=True,
                                    check=lambda r: check_resources(r.json(), profiles), capture=export_capture(pair),
                                    variant=shape + '/export-after-restart')[0].json()
            # The public backup is itself the input; no repository converter is
            # used to manufacture the current document for this second restore.
            import_request(pair, 'G14', copy.deepcopy(exported), variant=shape + '/reimport-export', expected_token=CURRENT)
            pair.restart()
            pair.request('G14', 'GET /api/config/export', expect=200, writes=True,
                         check=lambda r: check_resources(r.json(), profiles), capture=export_capture(pair),
                         variant=shape + '/export-again')


GROUPS = {'backup-format-roundtrips': roundtrips}
