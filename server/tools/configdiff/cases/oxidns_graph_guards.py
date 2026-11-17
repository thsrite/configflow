"""Native graph/updater refusals through actual transient common-editor calls."""
import copy

from harness import Pair, json_body
from sources import Source
from cases.config_backup import message
from cases.oxidns_editor import doc, text, plugin
from cases.oxidns_routes import BASE, SCOPES, OPTIONS, api
from cases.pull_routes import SourceStable

ERROR = '规则更新流水线仍被自定义执行链引用或包含自定义步骤，请先在完整参数中调整，不能自动停用'


def set_plugin(document, tag, kind, args):
    document['plugins'] = [item for item in document['plugins'] if item.get('tag') != tag]
    document['plugins'].append({'tag': tag, 'type': kind, 'args': args})


def pipeline(document, *, downloads=()):
    set_plugin(document, 'rules_download', 'download', {'downloads': list(downloads)})
    set_plugin(document, 'rules_reload', 'reload_provider', [])
    set_plugin(document, 'rules_refresh', 'sequence', [{'exec': '$rules_download'}, {'exec': '$rules_reload'}])


def guards(binary, recorder):
    for template, prefix in SCOPES:
        with Source(b'pure native drafts never fetch') as source:
            fixture = copy.deepcopy(OPTIONS)
            rule = fixture['shared']['rule_library'][0]
            rule.update(source_type='url', url=source.url + '/rules')
            rule.pop('content')
            with Pair(binary, recorder, options=fixture) as pair:
                plain = api(pair, template, prefix, 'editor', 'S01',
                    {'settings': {}, 'base_url': BASE}, variant='graph-guard/no-assets')['settings']
                asset = api(pair, template, prefix, 'editor', 'S01',
                    {'settings': {'direct_rulesets': ['native-route-rule']}, 'base_url': BASE},
                    variant='graph-guard/actual-URL-rule')['settings']

                def reject(case, settings, changes, expected, variant):
                    pair.request(case, 'POST ' + template + '/common-settings/apply', prefix + '/common-settings/apply',
                        body=json_body({'settings': settings, 'changes': changes, 'base_url': BASE}), expect=400,
                        writes=True, interaction=SourceStable(pair, source), check=message(expected), variant=variant)

                reject('H364', plain, {'rule_update.enabled': True},
                    '规则定时更新需要当前配置引用至少一个可下载订阅', 'no-live-downloadable-resource')
                reject('H371', {'config_mode': 'native', 'native_config': 'plugins: []\n'}, {'cache_enabled': True},
                    'cache_enabled：当前没有可识别的 ConfigFlow 处理链，请在完整参数中接入功能', 'uneditable-before-patch')
                pair.request('H350', 'POST ' + template + '/common-settings', prefix + '/common-settings',
                    body=json_body({'settings': {'config_mode': 'native', 'native_config': 'plugins: [null]\n'}, 'base_url': BASE}),
                    expect=400, writes=True, interaction=SourceStable(pair, source),
                    check=message('插件结构尚未完成，请在完整参数或 YAML 中继续编辑'), variant='unfinished-native-plugin-in-read-projection')
                changed = doc(plain)
                changed.setdefault('api', {}).setdefault('http', {})['auth'] = 'custom scalar'
                reject('H349', {**plain, 'native_config': text(changed)}, {'api.enabled': True, 'api.username': 'synthetic'},
                    '此参数包含自定义值类型，请在完整参数中编辑', 'explicit-API-enable-bypasses-uneditable-field-capability')
                changed = doc(plain)
                changed['plugins'] = [item for item in changed['plugins'] if item['tag'] != 'china_dns']
                missing_chain = {**plain, 'native_config': text(changed)}
                reject('H399', missing_chain, {'default_forward': 'forward_local'},
                    '目标分流执行链未配置，请在完整参数中添加后再切换。', 'switch-default-into-missing-china-chain')
                reject('H403', missing_chain, {'direct_rulesets': ['native-route-rule']},
                    '分流执行链 china_dns 未配置，请在完整参数中添加后再调整。', 'new-rule-selection-needs-missing-china-chain')
                changed = doc(plain)
                changed['plugins'] = [item for item in changed['plugins'] if item['tag'] != 'local_probe']
                changed['plugins'].append({'tag': 'rules_ip_1', 'type': 'ip_set', 'args': {'ips': ['192.0.2.0/24']}})
                plugin(changed, 'sequence_main')['args'][-1:-1] = [
                    {'matches': ['resp_ip $rules_ip_1'], 'exec': 'goto ip_requery_proxy'}]
                reject('H400', {**plain, 'native_config': text(changed)}, {'default_forward': 'forward_local'},
                    '分流执行链 local_probe 未配置，请在完整参数中添加后再调整。',
                    'legacy-unmatched-IP-block-default-switch-needs-missing-probe')
                changed = doc(plain)
                changed['plugins'].append({'tag': 'cf_dns_blackhole', 'type': 'cache', 'args': {'size': 10}})
                reject('H376', {**plain, 'native_config': text(changed)},
                    {'rule_actions': {'native-route-rule': {'action': 'blackhole'}}},
                    '已有同名插件 cf_dns_blackhole，无法创建 DNS 处理所需插件，请先在完整参数中改名', 'new-action-collides-with-unowned-plugin')
                changed = doc(asset)
                plugin(changed, 'rules_download')['args'] = []
                reject('H377', {**asset, 'native_config': text(changed)}, {'direct_rulesets': []},
                    '规则定时更新包含自定义结构，请先在完整参数中调整下载与重载任务。', 'routing-edit-refuses-malformed-maintenance-chain')
                custom = api(pair, template, prefix, 'editor', 'S01', {'settings': {'custom_matches': [
                    {'matches': ['qname synthetic.example'], 'exec': 'goto china_dns'}]}, 'base_url': BASE},
                    variant='graph-guard/custom-match-before-actual-rename')['settings']
                changed = doc(custom)
                plugin(changed, 'lazy_cache')['tag'] = 'renamed_cache'
                custom = api(pair, template, prefix, 'native-settings/apply', 'S02', {'settings': custom,
                    'native_config': text(changed), 'renames': {'lazy_cache': 'renamed_cache'}, 'base_url': BASE},
                    variant='graph-guard/actual-recorded-managed-rename')['settings']
                reject('H394', custom, {'custom_match_position': None}, '自定义匹配位置必须为 head 或 tail。',
                    'renamed-graph-custom-position-null')
                reject('H395', custom, {'custom_matches': [], 'default_forward': 'forward_local'},
                    '执行链已重命名，请单独调整自定义匹配，再编辑其他分流设置。', 'renamed-graph-mixes-custom-and-routing-edit')
                changed = doc(plain)
                changed.setdefault('api', {}).setdefault('http', {})['webui'] = {'root': 1}
                reject('H499', {**plain, 'native_config': text(changed)}, {'custom_hosts': ''},
                    'OxiDNS WebUI 路径必须为文本', 'native-resource-read-before-argument-validation')
                changed = doc(plain)
                changed['plugins'] = [item for item in changed['plugins'] if item['tag'] != 'direct_ip']
                reject('H363', {**plain, 'native_config': text(changed)}, {'dynamic_domain_set.enabled': True,
                    'dynamic_domain_set.route': 'direct', 'dynamic_domain_set.learn': True},
                    '缺少动态学习的目标执行链，请在完整参数中配置', 'new-learning-route-has-no-target-chain')

                for case, operation, expected in [
                    ('H365', 'existing-bad-download', '下载插件使用自定义参数，请在完整参数中编辑'),
                    ('H405', 'custom-type', '已有自定义规则更新插件，请在完整参数中配置其定时任务，不能重复创建更新链'),
                    ('H406', 'parked-download', ERROR), ('H407', 'parked-reload', ERROR),
                    ('H404', 'included-download', '规则更新流水线来自 include 文件或已重命名，请在完整参数或附加文件中调整')]:
                    settings, changed = copy.deepcopy(asset), doc(asset)
                    if operation == 'existing-bad-download':
                        set_plugin(changed, 'rules_download', 'download', [])
                    elif operation == 'custom-type':
                        set_plugin(changed, 'rules_download', 'cache', {})
                    else:
                        tag = 'rules_reload' if operation == 'parked-reload' else 'rules_download'
                        saved = next(item for item in changed['plugins'] if item['tag'] == tag)
                        changed['plugins'] = [item for item in changed['plugins'] if item['tag'] != tag]
                        if operation == 'included-download':
                            changed['include'] = ['pipeline.yaml']
                            settings['additional_files'].append({'path': 'pipeline.yaml', 'encoding': 'text',
                                'content': text({'plugins': [saved]})})
                        else:
                            saved['args'] = {} if tag == 'rules_reload' else []
                            settings.setdefault('_common_disabled', {})[tag] = saved
                    settings['native_config'] = text(changed)
                    reject(case, settings, {'rule_update.enabled': True}, expected, operation)

                for case in ('H408', 'H409', 'H410', 'H411', 'H412', 'H413', 'H414', 'H415', 'H368'):
                    changed = doc(plain)
                    downloads = [{'url': 'http://rules.example/custom', 'dir': './rules', 'filename': 'custom.txt'}] \
                        if case in ('H413', 'H414', 'H415') else []
                    pipeline(changed, downloads=downloads)
                    if case == 'H408':
                        set_plugin(changed, 'rules_cron', 'cache', {})
                    elif case == 'H409':
                        job = {'name': 'refresh_rules', 'executors': ['$rules_refresh']}
                        set_plugin(changed, 'rules_cron', 'cron', {'jobs': [job, copy.deepcopy(job)]})
                    elif case == 'H410':
                        set_plugin(changed, 'rules_refresh', 'cache', {})
                    elif case in ('H411', 'H413'):
                        set_plugin(changed, 'rules_refresh', 'sequence', [{'exec': 'accept'}])
                    elif case in ('H412', 'H414'):
                        target = 'rules_download' if case == 'H412' else 'rules_reload'
                        set_plugin(changed, 'custom_consumer', 'sequence', [{'exec': '$' + target}])
                    elif case == 'H415':
                        changed['future_custom_root'] = '$rules_reload'
                    else:
                        set_plugin(changed, 'rules_download', 'download', [])
                    reject(case, {**plain, 'native_config': text(changed)}, {'adguard.urls': []},
                        '规则更新使用自定义下载/重载结构，请在完整参数中调整' if case == 'H368' else ERROR,
                        'real-empty-pipeline-or-reload/' + case)


GROUPS = {'oxidns-native-common-graph-and-updater-guards': guards}


def missing_origin(binary, recorder):
    fixture = copy.deepcopy(OPTIONS)
    fixture['shared']['rule_library'][0].update(source_type='url', url='')
    fixture['shared']['rule_library'][0].pop('content')
    for template, prefix in SCOPES:
        with Source(b'missing source URL never contacts a peer') as source, Pair(binary, recorder, options=fixture) as pair:
            start = api(pair, template, prefix, 'editor', 'S01', {'settings': {}, 'base_url': BASE},
                variant='retained-empty-rule-URL/no-selected-rules')['settings']
            pair.request('H375', 'POST ' + template + '/common-settings/apply', prefix + '/common-settings/apply',
                body=json_body({'settings': start, 'changes': {'direct_rulesets': ['native-route-rule']}, 'base_url': BASE}),
                expect=400, writes=True, interaction=SourceStable(pair, source), check=message('已选择的规则集缺少下载来源。'),
                variant='retained-empty-rule-URL-is-needed-by-new-selection')


GROUPS['oxidns-retained-selected-source-missing-URL'] = missing_origin
