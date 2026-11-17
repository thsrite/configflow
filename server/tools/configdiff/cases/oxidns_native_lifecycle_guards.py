"""Explicit complete-form edits reach native lifecycle guard branches."""
import copy

from harness import Pair, json_body
from sources import Source
from cases.config_backup import message
from cases.oxidns_editor import doc, text, plugin
from cases.oxidns_routes import BASE, OPTIONS, SCOPES, api
from cases.pull_routes import SourceStable
from cases.subscriptions import require


def reference_message(prefix):
    def check(reply):
        value = reply.json()
        refs = value.get('references')
        require(value.get('success') is False and isinstance(refs, list) and bool(refs),
            'native lifecycle refusal retains nonempty structured reference diagnostics')
        for item in refs:
            require(isinstance(item, dict) and isinstance(item.get('path'), list)
                and all(type(part) in (str, int) for part in item['path']) and isinstance(item.get('reason'), str)
                and (item.get('tag') is None or isinstance(item['tag'], str)), 'reference diagnostics have actual typed coordinates')
        locations = '、'.join(''.join('[' + str(part) + ']' if type(part) is int else ('.' if i else '') + part
            for i, part in enumerate(item['path'])) for item in refs[:4])
        message(prefix + ('（' + locations + '）' if locations else ''))(reply)
    return check


def without(settings, *tags):
    changed = doc(settings)
    changed['plugins'] = [item for item in changed['plugins'] if item['tag'] not in tags]
    return {**copy.deepcopy(settings), 'native_config': text(changed)}


def guards(binary, recorder):
    for template, prefix in SCOPES:
        with Source(b'native lifecycle refusals never contact a source') as source:
            fixture = copy.deepcopy(OPTIONS)
            rule = fixture['shared']['rule_library'][0]
            rule.update(source_type='url', url=source.url + '/rules')
            rule.pop('content')
            with Pair(binary, recorder, options=fixture) as pair:
                plain = api(pair, template, prefix, 'editor', 'S01', {'settings': {}, 'base_url': BASE},
                    variant='native-lifecycle/no-assets')['settings']
                asset = api(pair, template, prefix, 'editor', 'S01', {'settings': {'direct_rulesets': ['native-route-rule']},
                    'base_url': BASE}, variant='native-lifecycle/live-download')['settings']

                def reject(case, current, candidate, expected, *, reference=False, extra=None):
                    pair.request(case, 'POST ' + template + '/native-settings/apply', prefix + '/native-settings/apply',
                        body=json_body({'settings': current, 'native_config': candidate if isinstance(candidate, str) else text(candidate),
                            'base_url': BASE, **(extra or {})}), expect=400, writes=True,
                        interaction=SourceStable(pair, source), check=reference_message(expected) if reference else message(expected),
                        variant='explicit-native-lifecycle/' + case)

                reject('H437', plain, {'plugins': [None]}, '插件结构尚未完成，请先填写插件 tag 和参数')
                changed = doc(plain)
                changed['plugins'].append(copy.deepcopy(changed['plugins'][0]))
                reject('H438', plain, changed, '插件 tag 重复，请先使用不同名称再联动修改')
                changed = doc(plain)
                lazy = plugin(changed, 'lazy_cache')
                lazy.update(tag='', args={'size': 33})
                reject('H460', plain, changed, '请先填写非空的新插件 tag，再同步重命名或删除')
                changed = doc(plain)
                plugin(changed, 'lazy_cache')['tag'] = 'renamed_cache'
                for case, parked, expected in [
                    ('H463', [], '暂存参数结构无效，不能记录插件重命名'),
                    ('H464', {'renamed_plugins': []}, '插件重命名记录结构无效，请在完整参数中修正')]:
                    reject(case, {**plain, '_common_disabled': parked}, changed, expected,
                        extra={'renames': {'lazy_cache': 'renamed_cache'}})
                current = without(plain, 'forward_fallback')
                changed = doc(current)
                plugin(changed, 'forward_remote')['tag'] = 'forward_fallback'
                reject('H458', current, changed, '标准功能新增不能同时作为插件重命名目标',
                    extra={'renames': {'forward_remote': 'forward_fallback'}, 'standard_add': ['forward_fallback']})

                current = without(plain, 'query_recorder')
                changed = doc(current)
                changed['plugins'].append({'tag': 'query_recorder', 'type': 'query_recorder', 'args': {}})
                reject('H445', {**current, '_common_disabled': []}, changed, '暂存参数结构无效，请先在完整参数中修正')
                included = copy.deepcopy(changed)
                included['include'] = ['empty.yaml']
                extra_file = {'path': 'empty.yaml', 'encoding': 'text', 'content': 'plugins: []\n'}
                reject('H443', {**current, 'additional_files': [*current['additional_files'], extra_file]}, included,
                    '配置包含 include，不能自动接入标准功能', reference=True)
                shared = copy.deepcopy(changed)
                shared['plugins'].append({'tag': 'extra_query_consumer', 'type': 'sequence', 'args': [{'exec': '$query_recorder'}]})
                reject('H444', current, shared, '不能自动接入共享或自定义处理链，请先调整引用', reference=True)
                current = without(plain, 'rules_download', 'rules_reload', 'rules_refresh', 'rules_cron')
                changed = doc(current)
                changed['plugins'].append({'tag': 'rules_download', 'type': 'download', 'args': {'downloads': []}})
                reject('H447', current, changed, '规则定时更新需要当前配置引用至少一个可下载订阅', reference=True)
                current = without(asset, 'rules_download', 'rules_reload', 'rules_refresh', 'rules_cron')
                changed = doc(current)
                changed['plugins'].append({'tag': 'rules_refresh', 'type': 'sequence', 'args': [{'exec': 'accept'}]})
                reject('H448', current, changed, '规则更新包含自定义步骤，不能自动接入定时任务', reference=True)
                current = without(asset, 'rules_download')
                current['_common_disabled'] = {'rules_download_managed_paths': ['rules/withdrawn.txt']}
                changed = doc(current)
                changed['plugins'].append({'tag': 'rules_download', 'type': 'download', 'args': {'downloads': [
                    {'url': 'http://rules.example/withdrawn', 'dir': './rules', 'filename': 'withdrawn.txt'}]}})
                reject('H449', current, changed, '新增下载插件仍包含已撤回的受管资源，请先删除旧下载条目', reference=True)
                current = {**plain, 'additional_files': [*plain['additional_files'], extra_file]}
                changed = doc(without(current, 'lazy_cache'))
                changed['include'] = ['empty.yaml']
                reject('H466', current, changed, '配置包含 include，不能自动修改其依赖链', reference=True)
                current = copy.deepcopy(plain)
                original = doc(current)
                plugin(original, 'lazy_cache')['args']['dump_file'] = './state/cache.dump'
                duplicate = copy.deepcopy(plugin(original, 'lazy_cache'))
                duplicate['tag'] = 'renamed_cache'
                original['plugins'].append(duplicate)
                current.update(native_config=text(original), _common_disabled={'renamed_plugins': {'lazy_cache': 'renamed_cache'}})
                changed = copy.deepcopy(original)
                plugin(changed, 'lazy_cache')['args'].pop('dump_file')
                reject('H440', current, changed,
                    'cache_dump_enabled：受管插件已重命名为「renamed_cache」，请在完整参数中编辑自定义配置', reference=True)
                hosts = api(pair, template, prefix, 'editor', 'S01', {'settings': {
                    'custom_hosts': 'synthetic.example: 192.0.2.8\n'}, 'base_url': BASE},
                    variant='native-lifecycle/real-Hosts-source')['settings']
                current = {**hosts, 'additional_files': [*hosts['additional_files'], extra_file]}
                changed = doc(current)
                plugin(changed, 'custom_hosts')['args']['files'] = []
                changed['include'] = ['empty.yaml']
                reject('H441', current, changed, '配置包含 include，不能自动移除 Hosts 处理链', reference=True)
                original = doc(hosts)
                original.setdefault('api', {}).setdefault('http', {})['webui'] = {'root': 1}
                current = {**hosts, 'native_config': text(original)}
                changed = copy.deepcopy(original)
                plugin(changed, 'custom_hosts')['args']['files'] = []
                reject('H442', current, changed, 'OxiDNS WebUI 路径必须为文本', reference=True)
                for case, tag, kind, expected in [
                    ('H446', 'query_recorder', 'query_recorder',
                     'query_recorder.enabled：当前没有可识别的 ConfigFlow 处理链，请在完整参数中接入功能'),
                    ('H450', 'adguard_rules', 'adguard_rule', 'adguard.enabled：自定义过滤链请在完整参数中调整'),
                    ('H451', 'custom_hosts', 'hosts', '当前没有可识别的 ConfigFlow 处理链，请在完整参数中接入功能')]:
                    current = without(plain, tag)
                    original = doc(current)
                    main = plugin(original, 'sequence_main')
                    main.update(type='cache', args={'size': 10})
                    current['native_config'] = text(original)
                    changed = copy.deepcopy(original)
                    changed['plugins'].append({'tag': tag, 'type': kind, 'args': {'files': []} if tag != 'query_recorder' else {}})
                    reject(case, current, changed, expected, reference=True,
                        extra={'standard_add': [tag]} if case == 'H450' else None)
                current = without(plain, 'custom_hosts')
                original = doc(current)
                plugin(original, 'sequence_main')['args'][:0] = [{'exec': '$custom_hosts'}, {'exec': 'accept'}]
                current['native_config'] = text(original)
                changed = copy.deepcopy(original)
                changed['plugins'].append({'tag': 'custom_hosts', 'type': 'hosts', 'args': {'files': []}})
                reject('H452', current, changed, 'Hosts 使用了自定义返回处理，请在完整参数中调整', reference=True)
                current = without(plain, 'query_recorder')
                changed = doc(without(current, 'lazy_cache', 'china_dns'))
                changed['plugins'].append({'tag': 'query_recorder', 'type': 'query_recorder', 'args': {}})
                reject('H467', current, changed,
                    'cache_enabled：当前没有可识别的 ConfigFlow 处理链，请在完整参数中接入功能', reference=True)
                current = without(plain, 'query_recorder')
                current['_common_disabled'] = {'query_recorder': {'tag': 'query_recorder', 'type': 'query_recorder',
                    'args': {'future_reference': '$lazy_cache'}}}
                changed = doc(without(current, 'lazy_cache'))
                changed['plugins'].append({'tag': 'query_recorder', 'type': 'query_recorder', 'args': {}})
                reject('H468', current, changed, '删除后仍有插件引用，请在完整参数中调整', reference=True,
                    extra={'standard_add': ['query_recorder']})


GROUPS = {'oxidns-explicit-native-lifecycle-guard-branches': guards}
