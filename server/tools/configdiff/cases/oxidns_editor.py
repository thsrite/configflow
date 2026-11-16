"""G12: public three-way draft editing, rename and standard-plugin lifecycle."""
import copy

import yaml

from compare import database
from harness import Pair, json_body
from oxidns import check as etag_check, capture as etag_capture
from seed import CURRENT
from cases.config_backup import export_capture
from cases.helpers import profile_policy
from cases.rule_library import DOMAIN
from cases.subscriptions import require


def doc(settings):
    return yaml.safe_load(settings['native_config'])


def plugin(document, tag):
    return next(item for item in document['plugins'] if item['tag'] == tag)


def text(document):
    return yaml.safe_dump(document, allow_unicode=True, sort_keys=False)


def matrix(binary, recorder):
    policies = [{'ip': '192.0.2.8', 'name': '设备备注🧪', 'mode': 'direct'},
                {'ip': '2001:db8::/64', 'name': 'IPv6 智能设备', 'mode': 'smart'}]
    for scoped in (False, True):
        prefix = '/api/profiles/default/oxidns' if scoped else '/api/oxidns'
        template = '/api/profiles/<profile_id>/oxidns' if scoped else '/api/oxidns'
        opts = {'shared': {'rule_library': [DOMAIN]}, 'profile': {'rule_configs': [
            {'id': 'editor-rule', 'itemType': 'ruleset', 'library_rule_id': 'lib-a', 'policy': 'DIRECT'}]}}
        with Pair(binary, recorder, options=opts) as pair:
            pair.policy = profile_policy(pair)
            initial = database(pair.dirs[0])
            def call(suffix, body, variant, check=None, expect=200):
                def readonly(reply):
                    require(database(pair.dirs[0]) == initial, 'editing a draft leaves the complete persisted store untouched')
                    if check:
                        check(reply)
                return pair.request('G12', 'POST ' + template + '/' + suffix, prefix + '/' + suffix,
                                    body=json_body(body), expect=expect, writes=True, check=readonly,
                                    variant=str(scoped) + '/' + variant)[0].json()

            start = call('editor', {'settings': {'worker_threads': 2, 'device_policies': policies,
                         'direct_rulesets': ['editor-rule'], 'api': {'username': 'synthetic', 'password': CURRENT}}},
                         'prepare-template')['settings']
            require(start['config_mode'] == 'native' and CURRENT in start['native_config']
                    and CURRENT in start['template_base_config'], 'editor exposes the exact native executable and baseline capability URLs')
            commented = copy.deepcopy(start)
            commented['native_config'] = '# 保留原始备注🧪\n' + commented['native_config']
            unchanged = call('template-preview', {'settings': commented, 'template_settings': start['template_settings']}, 'no-op-comments',
                             lambda r: require(r.json()['settings']['native_config'] == commented['native_config']
                                 and r.json()['changes'] == [] and r.json()['conflicts'] == [], 'no-op preview preserves comments byte-for-byte'))['settings']

            current = copy.deepcopy(start)
            document = doc(current)
            document['future'] = {'false': False, 'zero': 0, 'null': None}
            plugin(document, 'lazy_cache')['args'].update(size=321, experimental={'n': 0, 'enabled': False})
            document['plugins'].append({'tag': 'manual', 'type': 'future_plugin', 'args': {'nested': [0, False, None]}})
            current['native_config'] = text(document)
            inputs = {**start['template_settings'], 'worker_threads': 9, 'cache_lazy_ttl': 600}
            def manual_check(reply):
                value = reply.json()
                result = doc(value['settings'])
                require(result['runtime']['worker_threads'] == 9 and result['future'] == document['future']
                        and plugin(result, 'manual')['args'] == {'nested': [0, False, None]}, 'manual unknown data and scalar types survive an unrelated template update')
                require(plugin(result, 'lazy_cache')['args'] == {'size': 321, 'lazy_cache_ttl': 600,
                        'short_circuit': True, 'experimental': {'n': 0, 'enabled': False}} and value['conflicts'] == [],
                        'three-way merge retains manual edits and applies independently changed template fields')
                require(value['settings']['device_policies'] == policies, 'device policy remarks remain exact')
            call('template-preview', {'settings': current, 'template_settings': inputs}, 'merge-independent-fields', manual_check)

            conflict = copy.deepcopy(start)
            document = doc(conflict)
            document['runtime']['worker_threads'] = 5
            conflict['native_config'] = text(document)
            inputs = {**start['template_settings'], 'worker_threads': 7}
            resolved = None
            for choice in ('current', 'template'):
                body = {'settings': conflict, 'template_settings': inputs}
                if choice == 'template':
                    body['resolutions'] = {'/runtime/worker_threads': 'template'}
                def check(reply):
                    value = reply.json()
                    require(doc(value['settings'])['runtime']['worker_threads'] == (5 if choice == 'current' else 7), 'conflict choice determines the winning value')
                    require(value['conflicts'] == [{'path': '/runtime/worker_threads', 'label': 'runtime.worker_threads', 'choice': choice}],
                            'conflict metadata carries paths and choices without credential values')
                resolved = call('template-preview', body, 'conflict-' + choice, check)['settings']
            call('template-preview', {'settings': resolved, 'template_settings': resolved['template_settings']}, 'resolved-baseline-is-idempotent',
                 lambda r: require(r.json()['changes'] == [] and r.json()['conflicts'] == [], 'updated baseline does not replay the previous template change'))
            for value in ([], {'/x': 'discard'}, {'/x': None}):
                call('template-preview', {'settings': start, 'template_settings': start['template_settings'], 'resolutions': value},
                     'invalid-resolutions/' + repr(value), expect=400)

            for explicit in (False, True):
                document = doc(start)
                old = 'lazy_cache' if explicit else 'remote_dns'
                new = 'renamed_cache' if explicit else 'renamed_remote'
                plugin(document, old)['tag'] = new
                if explicit:
                    plugin(document, new)['args']['size'] = 987
                body = {'settings': start, 'native_config': text(document)}
                if explicit:
                    body['renames'] = {old: new}
                def renamed(reply):
                    result = doc(reply.json()['settings'])
                    require(plugin(result, new)['type'] == ('cache' if explicit else 'sequence'), 'rename retains plugin type')
                    require(result['api']['http']['auth'] == {'type': 'basic', 'username': 'synthetic', 'password': CURRENT}, 'reference rewriting never changes credential text')
                    if explicit:
                        require({'exec': '$renamed_cache'} in plugin(result, 'china_dns')['args']
                                and plugin(result, new)['args']['size'] == 987, 'explicit rename may change arguments and updates executor references')
                    else:
                        require({'exec': 'goto renamed_remote'} in plugin(result, 'proxy_dns_seq')['args'], 'tag-only rename updates bare goto references')
                call('native-settings/apply', body, 'rename/' + str(explicit), renamed)
                if explicit:
                    unsafe = copy.deepcopy(document)
                    unsafe['api']['http']['auth']['username'] = 'literal-$lazy_cache'
                    call('native-settings/apply', {**body, 'native_config': text(unsafe)}, 'rename-unresolved-custom-text',
                         lambda r: require(r.json()['references'][0]['path'] == ['api', 'http', 'auth', 'username'],
                                           'non-executor text containing the old tag is rejected rather than rewritten'), expect=400)

            document = doc(unchanged)
            document['plugins'] = [item for item in document['plugins'] if item['tag'] != 'lazy_cache']
            removed = call('native-settings/apply', {'settings': unchanged, 'native_config': text(document)}, 'remove-standard-cache',
                           lambda r: require('lazy_cache' not in {p['tag'] for p in doc(r.json()['settings'])['plugins']}
                               and r.json()['values']['cache_enabled'] is False and r.json()['notices'], 'deletion reconciles owned edges and parks feature parameters'))['settings']
            candidate = doc(removed)
            candidate['plugins'].append({'tag': 'lazy_cache', 'type': 'cache', 'args': {}})
            restored = call('native-settings/apply', {'settings': removed, 'native_config': text(candidate), 'standard_add': ['lazy_cache']},
                            'restore-standard-cache', lambda r: require(plugin(doc(r.json()['settings']), 'lazy_cache')['args']['size'] == 10240
                                and r.json()['values']['cache_enabled'] is True, 'standard addition restores parked cache values'))['settings']
            for intent in ('lazy_cache', [1], ['lazy_cache', 'lazy_cache'], ['unknown']):
                call('native-settings/apply', {'settings': removed, 'native_config': text(candidate), 'standard_add': intent},
                     'invalid-standard-add/' + repr(intent), expect=400)

            # Only an explicit save mutates persistence. Read/export must retain
            # original editor state; sanitized portable export must clear it.
            pair.request('G12', 'POST ' + template + '/settings', prefix + '/settings', body=json_body(restored),
                         expect=200, writes=True, check=etag_check(pair), capture=etag_capture(pair), variant=str(scoped) + '/save-editor')
            pair.request('G12', 'GET ' + template + '/settings', prefix + '/settings', expect=200, writes=True,
                         check=lambda r: require(r.json()['native_config'] == restored['native_config'] and r.json()['device_policies'] == policies,
                                                 'save/read keeps executable and device remarks exactly'), capture=etag_capture(pair), variant=str(scoped) + '/read-saved')
            for sanitized in (False, True):
                def exported(reply):
                    settings = reply.json()['profiles']['default']['oxidns']
                    if sanitized:
                        require(settings['native_config'] == '' and settings['template_base_config'] == '' and settings['additional_files'] == [],
                                'portable sanitized export clears executable/editor resource data')
                    else:
                        require(settings['native_config'] == restored['native_config'] and CURRENT in settings['template_base_config'],
                                'raw backup retains exact executable and secret-bearing baseline')
                pair.request('G12', 'GET /api/config/export', '/api/config/export?desensitize=' + str(sanitized).lower(),
                             expect=200, writes=True, check=exported, capture=export_capture(pair), variant=str(scoped) + '/export-' + str(sanitized))


GROUPS = {'oxidns-editor-roundtrips': matrix}
