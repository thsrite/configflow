"""Named R106–R147 editor draft cases, using only public preparation APIs."""
import copy
from urllib.parse import urlencode

import yaml

from compare import database, structure
from harness import Pair, json_body
from inventory import routes, selector_rows
from seed import CURRENT
from oxidns import check as etag_check, capture as etag_capture
from cases.helpers import profile_policy
from cases.oxidns_editor import doc, plugin, text
from cases.profile_reads import selected
from cases.rule_library import DOMAIN
from cases.subscriptions import require

MATRIX = routes()
SCOPES = [('/api/oxidns', '/api/oxidns'), ('/api/profiles/<profile_id>/oxidns', '/api/profiles/default/oxidns')]
OPTIONS = {'shared': {'rule_library': [DOMAIN]}, 'profile': {'rule_configs': [
    {'id': 'native-route-rule', 'itemType': 'ruleset', 'library_rule_id': DOMAIN['id'], 'policy': 'DIRECT'}]}}
BASE = 'http://panel.example'


def render_projection(value, index):
    if isinstance(value, dict) and isinstance(value.get('config'), str):
        value = copy.deepcopy(value)
        value['config'] = {'generated_yaml': True, 'parsed': structure(value['config'].encode(), filename='config.yaml')}
    return value


def api(pair, template, prefix, suffix, case, body, *, check=None, expect=200, variant='', query=None, headers=None):
    route = 'POST ' + template + '/' + suffix
    before = database(pair.dirs[0])
    def reference(reply):
        require(database(pair.dirs[0]) == before, 'draft operation preserves every SQL row, revision and commit field')
        require(reply.headers.get('cache-control') == 'no-store' or expect != 200,
                'successful native draft and diagnostic responses forbid caching')
        if check:
            check(reply)
    return pair.request(MATRIX[route] + '-' + case, route, prefix + '/' + suffix + ('?' + urlencode(query) if query else ''),
                        body=json_body(body), expect=expect, writes=True, check=reference, headers=headers, variant=variant,
                        projection=render_projection if suffix == 'render' else None)[0].json()


def prepared(pair, template, prefix, label):
    return api(pair, template, prefix, 'editor', 'S01', {'settings': {'worker_threads': 2, 'direct_rulesets': ['native-route-rule']},
        'base_url': BASE}, variant=label, check=lambda r: require(r.json()['settings']['config_mode'] == 'native', 'template becomes executable native draft'))['settings']


def common_reads(binary, recorder):
    for template, prefix in SCOPES:
        with Pair(binary, recorder, options=OPTIONS) as pair:
            pair.policy = profile_policy(pair)
            start = prepared(pair, template, prefix, 'common-reads-precondition')
            api(pair, template, prefix, 'common-settings', 'S01', {'settings': start, 'base_url': BASE},
                check=lambda r: require(r.json()['values']['worker_threads'] == 2 and r.json()['capabilities']['worker_threads']['editable'],
                                        'native worker control is recognized with exact current value'))
            custom = copy.deepcopy(start)
            document = doc(custom)
            document['runtime']['worker_threads'] = 9
            document['future'] = {'zero': 0, 'false': False, 'null': None}
            document['plugins'].append({'tag': 'manual', 'type': 'future_plugin', 'args': {'unknown': [0, False, None]}})
            custom['native_config'] = text(document)
            api(pair, template, prefix, 'common-settings', 'S02', {'settings': custom, 'base_url': BASE},
                check=lambda r: require(r.json()['values']['worker_threads'] == 9, 'actual YAML wins over retained template input 2'))
            minimal = {'config_mode': 'native', 'native_config': 'plugins: []\n', 'worker_threads': 91}
            api(pair, template, prefix, 'common-settings', 'B01', {'settings': minimal, 'base_url': BASE},
                check=lambda r: require(r.json()['values']['worker_threads'] is None and r.json()['capabilities']['worker_threads']['absent']
                    and not r.json()['capabilities']['query_recorder.enabled']['editable']
                    and r.json()['capabilities']['query_recorder.enabled']['reason'], 'missing standard features have explicit capability reasons, without phantom template defaults'))
            api(pair, template, prefix, 'common-settings', 'B02', {'settings': start, 'base_url': BASE},
                check=lambda r: require('changes' not in r.json(), 'read-only projection has no changes request or persistence'))


def common_applies(binary, recorder):
    for template, prefix in SCOPES:
        with Pair(binary, recorder, options=OPTIONS) as pair:
            pair.policy = profile_policy(pair)
            start = prepared(pair, template, prefix, 'common-applies-precondition')
            for case, changes, check, variant in [
                ('S01', {'cache_size': 300}, lambda v: plugin(doc(v['settings']), 'lazy_cache')['args']['size'] == 300, 'cache'),
                ('S02', {'local_dns': '- addr: udp://192.0.2.53\n'},
                 lambda v: plugin(doc(v['settings']), 'forward_local')['args']['upstreams'] == [{'addr': 'udp://192.0.2.53'}], 'upstream'),
                ('S03', {'direct_rulesets': []}, lambda v: v['values']['direct_rulesets'] == [], 'routing-clear'),
                ('S04', {'adguard.enabled': True, 'adguard.content': '||ads.example^\n', 'adguard.mode': 'refused'},
                 lambda v: v['values']['adguard']['enabled'] and v['values']['adguard']['content'] == '||ads.example^\n', 'adguard'),
                ('S05', {'worker_threads': 2}, lambda v: doc(v['settings']) == doc(start), 'same-value'),
                ('B02', {'block_qtypes': []}, lambda v: v['values']['block_qtypes'] == [], 'empty-list'),
                ('B03', {'worker_threads': 7}, lambda v: doc(v['settings'])['runtime']['worker_threads'] == 7, 'draft-only')]:
                api(pair, template, prefix, 'common-settings/apply', case, {'settings': start, 'changes': changes, 'base_url': BASE},
                    check=lambda r: require(check(r.json()), 'requested common control changes exactly the intended native draft'), variant=variant)
            # Device remarks are retained editing data; common controls do not
            # expose a device_policies patch key. An unrelated patch must keep them.
            policies = [{'ip': '192.0.2.8', 'name': '中文设备🧪', 'mode': 'direct'}]
            remarks = {**start, 'device_policies': policies}
            api(pair, template, prefix, 'common-settings/apply', 'B01', {'settings': remarks, 'changes': {'worker_threads': 7}, 'base_url': BASE},
                check=lambda r: require(r.json()['settings']['device_policies'] == policies, 'Chinese and emoji device remarks survive unrelated draft changes'))
            for value in ([], None, {}):
                api(pair, template, prefix, 'common-settings/apply', 'F12', {'settings': start, 'changes': value}, expect=400, variant=repr(value))
            api(pair, template, prefix, 'common-settings/apply', 'F13', {'settings': start, 'changes': {'unknown🧪': True}}, expect=400,
                check=lambda r: require(r.json()['message'] == '不支持的常用参数：unknown🧪', 'unknown control is not silently dropped'))
            custom = copy.deepcopy(start)
            document = doc(custom)
            plugin(document, 'sequence_main')['args'].append({'exec': '$manual', 'future': True})
            document['plugins'].append({'tag': 'manual', 'type': 'sequence', 'args': [{'exec': 'return'}]})
            custom['native_config'] = text(document)
            api(pair, template, prefix, 'common-settings/apply', 'F14', {'settings': custom, 'changes': {'direct_rulesets': []}}, expect=400,
                check=lambda r: require('自定义' in r.json()['message'], 'custom routing chain cannot be overwritten by common controls'))


def editor_render_validate(binary, recorder):
    for template, prefix in SCOPES:
        with Pair(binary, recorder, options=OPTIONS) as pair:
            pair.policy = profile_policy(pair)
            start = prepared(pair, template, prefix, 'editor-conversion')
            api(pair, template, prefix, 'editor', 'S02', {'settings': start, 'base_url': BASE},
                check=lambda r: require(r.json()['settings']['native_config'] == start['native_config'], 'already native draft remains exact'))
            api(pair, template, prefix, 'editor', 'B01', {'settings': {}, 'base_url': BASE},
                check=lambda r: require(bool(doc(r.json()['settings'])['plugins']), 'empty template inputs receive real default executable plugins'))
            commented = {**start, 'native_config': '# 手写备注🧪\n' + start['native_config'],
                         'additional_files': [{'path': 'rules/备注🧪.txt', 'content': '# 内容\n', 'encoding': 'text'}]}
            api(pair, template, prefix, 'editor', 'B02', {'settings': commented, 'base_url': BASE},
                check=lambda r: require(r.json()['settings']['native_config'] == commented['native_config']
                    and r.json()['settings']['additional_files'] == commented['additional_files'], 'native comments and extra file contents remain exact'))
            for case, settings, explicit in [('S01', {'worker_threads': 3}, True), ('S02', start, True), ('S03', start, False),
                                              ('B01', {**start, 'additional_files': []}, True), ('B02', start, True)]:
                capability = BASE + '/api/mosdns/rule-proxy?' + urlencode({'token': CURRENT, 'url': 'http://rules.example/source'})
                if case == 'B02':
                    settings = copy.deepcopy(settings)
                    candidate = doc(settings)
                    candidate['api'] = {'http': {'listen': '127.0.0.1:9199',
                        'auth': {'type': 'basic', 'username': 'synthetic', 'password': capability}}}
                    settings['native_config'] = text(candidate)
                body = {'settings': settings}
                if explicit:
                    body['base_url'] = BASE
                def rendered(reply):
                    require(yaml.safe_load(reply.json()['config'])['runtime']['worker_threads'] == (3 if case == 'S01' else 2),
                            'render returns the intended executable rather than persisted settings')
                    if case == 'B02':
                        require(yaml.safe_load(reply.json()['config'])['api']['http']['auth']['password'] == capability
                            and '[REDACTED]' not in reply.json()['config'],
                                'generated capability URLs retain their exact executable credential')
                api(pair, template, prefix, 'render', case, body, headers={'Host': 'panel.example'}, check=rendered)
            for case, settings, valid in [('S01', {'worker_threads': 2}, True), ('S02', start, True),
                    ('S03', {'config_mode': 'native', 'native_config': 'plugins:\n- tag: incomplete\n  type: forward\n  args: {}\n'}, False),
                    ('B01', {'config_mode': 'native', 'native_config': 'plugins:\n- tag: broken\n  type: sequence\n  args:\n  - exec: $missing\n'}, False),
                    ('B02', {'config_mode': 'native', 'native_config': 'plugins:\n- tag: custom\n  type: future_plugin\n  args: {}\n'}, True)]:
                def check(reply):
                    value = reply.json()
                    require(value['valid'] is valid and value['core_check_required'] is True and value['schema_version'] == '1.6.1',
                            'validation distinguishes complete, incomplete and unknown-plugin drafts')
                    if not valid:
                        require(value['issues'] and all(isinstance(i['path'], list) and i['source'] == 'config.yaml' for i in value['issues']),
                                'structured diagnostic paths and sources remain visible for complete comparison')
                    if case == 'B02':
                        require(any(i['code'] == 'unknown-plugin' and i['severity'] == 'warning' and i['path'] == ['plugins', 0, 'type']
                                    for i in value['issues']), 'unknown plugin is an explicit warning requiring the target core, not an invented validation error')
                api(pair, template, prefix, 'validate', case, {'settings': settings, 'base_url': BASE}, check=check)


GROUPS = {'oxidns-named-common-reads': common_reads, 'oxidns-named-common-applies': common_applies,
          'oxidns-named-editor-render-validate': editor_render_validate}


def template_previews(binary, recorder):
    for template, prefix in SCOPES:
        with Pair(binary, recorder, options=OPTIONS) as pair:
            pair.policy = profile_policy(pair)
            start = prepared(pair, template, prefix, 'template-preview-precondition')
            api(pair, template, prefix, 'template-preview', 'S01', {'settings': start, 'template_settings': start['template_settings'], 'base_url': BASE},
                check=lambda r: require(r.json()['changes'] == r.json()['conflicts'] == []
                    and r.json()['settings']['native_config'] == start['native_config'], 'unchanged baseline is byte-preserving and has no conflicts'))
            conflict = copy.deepcopy(start)
            document = doc(start)
            document['runtime']['worker_threads'] = 5
            conflict['native_config'] = text(document)
            for case, choice, expected in [('S02', 'current', 5), ('S03', 'template', 7)]:
                api(pair, template, prefix, 'template-preview', case, {'settings': conflict,
                    'template_settings': {**start['template_settings'], 'worker_threads': 7},
                    'resolutions': {'/runtime/worker_threads': choice}, 'base_url': BASE},
                    check=lambda r: require(doc(r.json()['settings'])['runtime']['worker_threads'] == expected
                        and r.json()['conflicts'] == [{'path': '/runtime/worker_threads', 'label': 'runtime.worker_threads', 'choice': choice}],
                        'three-way conflict applies requested choice and retains precise path metadata'))
            manual = copy.deepcopy(start)
            document = doc(start)
            document['future'] = {'zero': 0, 'false': False, 'null': None}
            manual['native_config'] = text(document)
            api(pair, template, prefix, 'template-preview', 'B01', {'settings': manual,
                'template_settings': {**start['template_settings'], 'cache_size': 300}, 'base_url': BASE},
                check=lambda r: require(doc(r.json()['settings'])['future'] == document['future'], 'unrelated unknown fields keep their values and scalar types'))
            asset = {'path': 'rules/备注🧪.txt', 'content': '# 中文\n', 'encoding': 'text'}
            api(pair, template, prefix, 'template-preview', 'B02', {'settings': {**start, 'additional_files': [asset]},
                'template_settings': start['template_settings'], 'base_url': BASE},
                check=lambda r: require(r.json()['settings']['additional_files'] == [asset], 'Chinese emoji attachment names and bytes survive preview'))
            for value in (None, [], 'template'):
                api(pair, template, prefix, 'template-preview', 'F12', {'settings': start, 'template_settings': value}, expect=400, variant=repr(value))
            for value in ([], {'/runtime/worker_threads': 'discard'}, {'/x': None}):
                api(pair, template, prefix, 'template-preview', 'F13', {'settings': start, 'template_settings': start['template_settings'], 'resolutions': value},
                    expect=400, variant=repr(value))
            resources = api(pair, template, prefix, 'editor', 'S01', {'settings': {'custom_hosts': 'full:host.example 192.0.2.1'}, 'base_url': BASE},
                            variant='template-resource-precondition')['settings']
            document = doc(resources)
            plugin(document, 'custom_hosts')['args']['manual'] = True
            resources['native_config'] = text(document)
            api(pair, template, prefix, 'template-preview', 'F14', {'settings': resources,
                'template_settings': {**resources['template_settings'], 'custom_hosts': ''},
                'resolutions': {'/resources/custom_hosts': 'template'}, 'base_url': BASE}, expect=400,
                check=lambda r: require('仍被完整配置引用' in r.json()['message'], 'referenced managed content cannot be cleared by choosing empty template data'))


def native_applies(binary, recorder):
    for template, prefix in SCOPES:
        with Pair(binary, recorder, options=OPTIONS) as pair:
            pair.policy = profile_policy(pair)
            start = prepared(pair, template, prefix, 'native-apply-precondition')
            candidate = doc(start)
            candidate['runtime']['worker_threads'] = 8
            api(pair, template, prefix, 'native-settings/apply', 'S01', {'settings': start, 'native_config': text(candidate), 'base_url': BASE},
                check=lambda r: require(doc(r.json()['settings'])['runtime']['worker_threads'] == 8, 'complete form changes actual executable value'))
            renamed = doc(start)
            plugin(renamed, 'lazy_cache')['tag'] = 'renamed_cache'
            body = {'settings': start, 'native_config': text(renamed), 'renames': {'lazy_cache': 'renamed_cache'}, 'base_url': BASE}
            api(pair, template, prefix, 'native-settings/apply', 'S02', body,
                check=lambda r: require({'exec': '$renamed_cache'} in plugin(doc(r.json()['settings']), 'china_dns')['args'], 'renamed plugin rewrites exact executor reference'))
            for value in (None, [], 1):
                api(pair, template, prefix, 'native-settings/apply', 'F12', {'settings': start, 'native_config': value}, expect=400, variant=repr(value))
            for value in ([], {'lazy_cache': 1}):
                api(pair, template, prefix, 'native-settings/apply', 'F13', {**body, 'renames': value}, expect=400, variant=repr(value))
            for value in ('lazy_cache', [1], ['lazy_cache', 'lazy_cache'], ['unknown']):
                api(pair, template, prefix, 'native-settings/apply', 'F14', {'settings': start, 'native_config': start['native_config'], 'standard_add': value},
                    expect=400, variant=repr(value))
            api(pair, template, prefix, 'native-settings/apply', 'F15', {**body, 'renames': {'lazy_cache': 'forward_remote'}}, expect=400)
            dynamic = copy.deepcopy(renamed)
            dynamic['plugins'].append({'tag': 'manual', 'type': 'sequence', 'args': [{'exec': '${EXECUTOR}'}]})
            api(pair, template, prefix, 'native-settings/apply', 'F16', {**body, 'native_config': text(dynamic)}, expect=400,
                check=lambda r: require(any(ref['path'] == ['plugins', len(dynamic['plugins']) - 1, 'args', 0, 'exec'] for ref in r.json()['references']),
                                        'dynamic executor is reported by exact candidate path'))
            for tag in ('中文', 'emoji🧪'):
                bad = doc(start)
                plugin(bad, 'lazy_cache')['tag'] = tag
                api(pair, template, prefix, 'native-settings/apply', 'B01', {**body, 'native_config': text(bad), 'renames': {'lazy_cache': tag}},
                    expect=400, variant=tag)
            api(pair, template, prefix, 'native-settings/apply', 'B02', {'settings': start, 'native_config': start['native_config'], 'renames': {}, 'base_url': BASE},
                check=lambda r: require(doc(r.json()['settings']) == doc(start), 'empty rename map makes no executable change'))
            document = doc(start)
            document['plugins'].append({'tag': 'shared', 'type': 'fallback', 'args': {'primary': 'lazy_cache', 'secondary': 'forward_remote'}})
            shared = {**start, 'native_config': text(document)}
            document['plugins'] = [p for p in document['plugins'] if p['tag'] != 'lazy_cache']
            reject = {'settings': shared, 'native_config': text(document), 'base_url': BASE}
            def refs(reply):
                require(reply.json()['references'][0]['path'] == ['plugins', len(document['plugins']) - 1, 'args', 'primary'],
                        'reference diagnostics index the candidate YAML rather than stale original positions')
            for case in ('F17', 'B03'):
                api(pair, template, prefix, 'native-settings/apply', case, reject, expect=400, check=refs)
            minimal = doc(start)
            minimal['plugins'] = [p for p in minimal['plugins'] if p['tag'] != 'lazy_cache']
            removed = api(pair, template, prefix, 'native-settings/apply', 'S04', {'settings': start, 'native_config': text(minimal), 'base_url': BASE},
                check=lambda r: require(r.json()['values']['cache_enabled'] is False and r.json()['notices']
                    and not any(p['tag'] == 'lazy_cache' for p in doc(r.json()['settings'])['plugins']), 'safe managed deletion reconciles owned edges and parks previous settings'))['settings']
            restore = doc(removed)
            restore['plugins'].append({'tag': 'lazy_cache', 'type': 'cache', 'args': {}})
            api(pair, template, prefix, 'native-settings/apply', 'S03', {'settings': removed, 'native_config': text(restore), 'standard_add': ['lazy_cache'], 'base_url': BASE},
                check=lambda r: require(r.json()['values']['cache_enabled'] is True and plugin(doc(r.json()['settings']), 'lazy_cache')['args']['size'] == 10240,
                                        'standard addition restores valid parked cache arguments'))
            dynamic_start = api(pair, template, prefix, 'editor', 'S01', {'settings': {'dynamic_domain_set': {'enabled': True, 'learn': False}},
                'base_url': BASE}, variant='native-dependency-precondition')['settings']
            changed = doc(dynamic_start)
            changed['plugins'] = [p for p in changed['plugins'] if p['tag'] != 'dynamic_domains']
            changed['plugins'].append({'tag': 'learn_classified_domain', 'type': 'learn_domain', 'args': {}})
            chain_index = next(i for i, p in enumerate(changed['plugins']) if p['tag'] == 'proxy_dns_seq')
            api(pair, template, prefix, 'native-settings/apply', 'F18', {'settings': dynamic_start, 'native_config': text(changed), 'base_url': BASE},
                expect=400, check=lambda r: require(r.json()['message'] == f'同一次修改不能删除标准功能的依赖并重新启用该功能，请分步调整（plugins[{chain_index}].args）'
                    and r.json()['references'] == [{'path': ['plugins', chain_index, 'args'], 'reason': '新增功能仍需要本次删除的依赖', 'tag': 'proxy_dns_seq'}],
                    'one complete edit cannot add a standard feature while deleting its provider dependency: ' + repr(r.json())))


GROUPS.update({'oxidns-named-template-previews': template_previews, 'oxidns-named-native-applies': native_applies})


def structural_guards(binary, recorder):
    suffixes = ('common-settings', 'common-settings/apply', 'editor', 'native-settings/apply', 'render', 'template-preview', 'validate')
    for template, prefix in SCOPES:
        with Pair(binary, recorder, options=OPTIONS) as pair:
            pair.policy = profile_policy(pair)
            start = prepared(pair, template, prefix, 'structure-precondition')
            for suffix in suffixes:
                for case, native in [('F10', 'plugins: [invalid YAML'),
                    ('F11', 'plugins:\n- {tag: dup, type: sequence, args: []}\n- {tag: dup, type: sequence, args: []}\n')]:
                    body = {'settings': {**start, 'native_config': native}, 'base_url': BASE}
                    if suffix == 'common-settings/apply':
                        body['changes'] = {'worker_threads': 3}
                    if suffix == 'native-settings/apply':
                        body['native_config'] = native
                    if suffix == 'template-preview':
                        body['template_settings'] = start['template_settings']
                    # The editor and template preview deliberately keep safe but
                    # incomplete native drafts; the diagnostic endpoint always
                    # reports issues in a 200 envelope. Publication is stricter.
                    status = 200 if suffix == 'validate' or case == 'F11' and suffix not in ('native-settings/apply', 'render') else 400
                    def check(reply):
                        if suffix == 'validate':
                            require(reply.json()['valid'] is False and bool(reply.json()['issues']), 'diagnostics expose malformed YAML or duplicate-plugin errors')
                        elif status == 400:
                            require(reply.json()['success'] is False and bool(reply.json()['message']), 'strict operation rejects invalid YAML or duplicate plugin identities')
                        elif suffix == 'common-settings':
                            require(reply.json()['values']['worker_threads'] is None, 'projection reads incomplete executable values without inventing defaults')
                        elif suffix == 'common-settings/apply':
                            document = doc(reply.json()['settings'])
                            require(document['runtime']['worker_threads'] == 3 and [p['tag'] for p in document['plugins']] == ['dup', 'dup'],
                                    'unrelated safe scalar patch keeps incomplete duplicate plugins for later repair')
                        else:
                            require(reply.json()['settings']['native_config'] == native, 'draft tools retain incomplete native syntax for explicit repair')
                    api(pair, template, prefix, suffix, case, body, expect=status, check=check, variant='native-structure')


def profile_selectors(binary, recorder):
    for rid, route, recipes in selector_rows():
        if not route.startswith(('POST /api/oxidns/', 'POST /api/profiles/<profile_id>/oxidns/')) or route.endswith('/settings'):
            continue
        suffix = route.split('/oxidns/', 1)[1]
        template = route.split(' ', 1)[1].split('/oxidns/', 1)[0] + '/oxidns'
        prefix = template.replace('<profile_id>', 'default')
        opts = copy.deepcopy(OPTIONS)
        opts['other_profile'] = {'rule_configs': [{'id': 'selector-other-native-rule', 'itemType': 'rule',
            'rule_type': 'DOMAIN', 'value': 'other.selector.example', 'policy': 'DIRECT'}]}
        with Pair(binary, recorder, options=opts) as pair:
            pair.policy = profile_policy(pair)
            for number, selector in recipes:
                query, header, expected = selected(route, selector, pair.default_profile)
                body = {'settings': {'worker_threads': 2}, 'base_url': BASE}
                if suffix == 'common-settings/apply':
                    body['changes'] = {'worker_threads': 7}
                if suffix == 'native-settings/apply':
                    body['native_config'] = 'runtime: {worker_threads: 7}\nplugins: []\n'
                if suffix == 'template-preview':
                    body['template_settings'] = {'worker_threads': 7}
                api(pair, template, prefix, suffix, f'B{number:02}', body, query=query,
                    headers={'X-ConfigFlow-Profile': header}, variant='P' + str(selector) + '/native-draft-selection',
                    check=lambda r: require(r.headers.get('x-configflow-profile') == expected, 'successful native draft uses URL/query/header/default-selected profile'))


GROUPS.update({'oxidns-named-structure': structural_guards, 'oxidns-named-selectors': profile_selectors})


def stored_settings(binary, recorder):
    fixture = {'x01_native': True, 'profile': {'oxidns': {'future_extra': {'zero': 0, 'false': False, 'null': None, '中文': '🧪'}}}}
    for template, prefix in SCOPES:
        with Pair(binary, recorder, options=fixture) as pair:
            pair.policy = profile_policy(pair)
            before = database(pair.dirs[0])
            def reference(reply):
                etag_check(pair)(reply)
                value = reply.json()
                require(database(pair.dirs[0]) == before and value['future_extra'] == fixture['profile']['oxidns']['future_extra'],
                        'GET settings preserves unknown editor metadata and performs no SQL write')
                require(value['native_config'] == before['fields']['profile/default/oxidns']['value']['native_config']
                    and CURRENT in value['native_config'], 'authenticated native settings keep the exact secret-bearing editable executable')
            route = 'GET ' + template + '/settings'
            pair.request(MATRIX[route] + '-B02', route, prefix + '/settings', expect=200, writes=True, check=reference, capture=etag_capture(pair))
    for rid, route, recipes in selector_rows():
        if route not in ('POST /api/oxidns/settings', 'POST /api/profiles/<profile_id>/oxidns/settings'):
            continue
        with Pair(binary, recorder, options={'other_profile': {'oxidns': {'worker_threads': 11}}}) as pair:
            pair.policy = profile_policy(pair)
            for number, selector in recipes:
                query, header, expected = selected(route, selector, pair.default_profile)
                path = route.split(' ', 1)[1].replace('<profile_id>', pair.default_profile)
                if query:
                    path += '?' + urlencode(query)
                before = database(pair.dirs[0])
                def reference(reply):
                    etag_check(pair, expected)(reply)
                    after = database(pair.dirs[0])
                    require(reply.headers.get('x-configflow-profile') == expected
                        and after['fields']['profile/' + expected + '/oxidns']['value']['worker_threads'] == 7,
                        'settings save commits exact value to the selected profile and returns its publish guard')
                    other = 'other' if expected == 'default' else 'default'
                    require({k: v for k, v in before['fields'].items() if k.startswith('profile/' + other + '/')}
                        == {k: v for k, v in after['fields'].items() if k.startswith('profile/' + other + '/')},
                        'other profile rows and metadata remain exact after selector write')
                pair.request(rid + f'-B{number:02}', route, path, body=json_body({'worker_threads': 7}), expect=200, writes=True,
                             headers={'X-ConfigFlow-Profile': header}, check=reference, capture=etag_capture(pair, expected),
                             variant='P' + str(selector) + '/stored-native-settings-selection')


GROUPS['oxidns-named-stored-settings'] = stored_settings
