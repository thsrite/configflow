"""Automatic rule updates must never publish unrelated saved draft changes."""
from copy import deepcopy
import hashlib

import pytest
import yaml

from backend.agents.deployment_bundle import _pack, deployment_content_fingerprint
from backend.agents.rule_sync_projection import (
    RuleSyncProjectionError, isolate_rule_bundle, project_rule_changes,
    read_bundle_files, rule_change_kind, rule_signature,
)
from backend.converters.oxidns import generate_oxidns_config, _rule_path


def fixture():
    source = {'id': 'lib-a', 'name': 'A', 'source_type': 'content', 'behavior': 'domain', 'content': 'old.test'}
    return {'profile_id': 'home', 'nodes': [{'id': 'node', 'server': 'old-node.test'}],
            'system_config': {'server_domain': 'https://config.test', 'rule_proxy_token': 'test-token'},
            'rule_library': [source], 'rule_configs': [
                {**source, 'id': 'set-a', 'library_rule_id': 'lib-a', 'itemType': 'ruleset', 'policy': 'DIRECT',
                 'url': '/api/profiles/home/rule-library/content/lib-a', 'library_enabled': True},
                {'id': 'single', 'itemType': 'rule', 'rule_type': 'DOMAIN', 'value': 'old-inline.test', 'policy': 'DIRECT'}],
            'oxidns': {'direct_rulesets': ['set-a'], 'direct_rules': ['single'], 'local_dns': 'udp://192.0.2.1'}}


def native(config):
    result = deepcopy(config)
    result['oxidns']['native_config'] = generate_oxidns_config(config)
    result['oxidns']['config_mode'] = 'native'
    return result


def add_set(config, identity='b'):
    source = {'id': 'lib-' + identity, 'name': identity.upper(), 'source_type': 'content',
              'behavior': 'domain', 'content': identity + '.test'}
    config['rule_library'].append(source)
    config['rule_configs'].insert(0, {**source, 'id': 'set-' + identity, 'itemType': 'ruleset',
                                     'library_rule_id': source['id'], 'policy': 'DIRECT',
                                     'url': '/api/profiles/home/rule-library/content/' + source['id']})


def bundle(document, service='mihomo', extras=None, deployment='old'):
    content = yaml.safe_dump(document, sort_keys=False).encode()
    files = {'config.yaml': (content, 'config'), **(extras or {})}
    manifest = {'protocol_version': 1, 'agent_id': 'agent', 'profile_id': 'home', 'service_type': service,
                'deployment_id': deployment, 'config_path': 'config.yaml', 'files': [
                    {'path': path, 'role': role, 'size': len(value), 'sha256': hashlib.sha256(value).hexdigest()}
                    for path, (value, role) in sorted(files.items())]}
    return _pack(manifest, files, hashlib.sha256(content).hexdigest()[:8])


def unpack_document(value):
    return yaml.safe_load(read_bundle_files(value)[value.manifest['config_path']][0])


def test_library_projection_preserves_inline_nodes_dns_and_input_snapshots():
    base = fixture()
    current = deepcopy(base)
    current['nodes'][0]['server'] = 'new-node.test'
    current['oxidns']['local_dns'] = 'udp://192.0.2.2'
    current['rule_configs'][1]['value'] = 'new-inline.test'
    current['rule_library'][0]['content'] = 'new.test'
    original = deepcopy((base, current))
    projected = project_rule_changes(base, current, {'library': True})
    assert projected['nodes'] == base['nodes']
    assert projected['oxidns']['local_dns'] == base['oxidns']['local_dns']
    assert projected['rule_configs'][1] == base['rule_configs'][1]
    assert projected['rule_configs'][0]['content'] == 'new.test'
    assert (base, current) == original
    for service in ('mihomo', 'oxidns'):
        assert rule_change_kind(base, current, service, {'library': True}) == 'content'


def test_inline_projection_keeps_library_and_its_selection_unchanged():
    base = fixture()
    current = deepcopy(base)
    current['rule_library'][0]['content'] = 'new.test'
    current['rule_configs'][1]['value'] = 'new-inline.test'
    current['oxidns']['direct_rulesets'] = []
    current['oxidns']['direct_rules'] = []
    current['oxidns']['proxy_rules'] = ['single']
    projected = project_rule_changes(base, current, {'rules': True})
    assert projected['rule_library'] == base['rule_library']
    assert projected['rule_configs'][0]['content'] == 'old.test'
    assert projected['oxidns']['direct_rulesets'] == ['set-a']
    assert projected['oxidns']['proxy_rules'] == ['single']
    assert rule_change_kind(base, current, 'oxidns', {'rules': True}) == 'structure'
    assert project_rule_changes(base, current, {}) == base


def test_unused_library_addition_has_no_effect_on_either_service():
    base = fixture()
    current = deepcopy(base)
    current['rule_library'].append({'id': 'unused', 'name': 'Unused', 'source_type': 'content', 'content': 'unused.test'})
    for service in ('mihomo', 'oxidns'):
        assert rule_change_kind(base, current, service, {'library': True}) == 'none'


def test_new_reference_changes_mihomo_but_only_selected_reference_changes_oxidns():
    base = fixture()
    current = deepcopy(base)
    add_set(current)
    assert rule_change_kind(base, current, 'mihomo', {'library': True}) == 'structure'
    assert rule_change_kind(base, current, 'oxidns', {'library': True}) == 'none'
    current['oxidns']['proxy_rulesets'] = ['set-b']
    assert rule_change_kind(base, current, 'oxidns', {'library': True}) == 'structure'


def test_inactive_scope_order_is_anchored_and_desired_scope_order_is_retained():
    base = fixture()
    add_set(base)
    base['rule_configs'] = [base['rule_configs'][1], base['rule_configs'][2], base['rule_configs'][0]]
    current = deepcopy(base)
    current['rule_configs'] = list(reversed(current['rule_configs']))
    current['rule_configs'][1]['value'] = 'unsaved.test'
    add_set(current, 'c')
    projected = project_rule_changes(base, current, {'library': True})
    assert [item['id'] for item in projected['rule_configs']] == ['set-c', 'single', 'set-b', 'set-a']
    assert projected['rule_configs'][1]['value'] == 'old-inline.test'


def test_library_metadata_and_source_url_changes_are_structural():
    base = fixture()
    for field, value in [('name', 'Renamed'), ('behavior', 'classical'), ('source_type', 'url')]:
        current = deepcopy(base)
        current['rule_library'][0][field] = value
        if field == 'source_type':
            current['rule_library'][0]['url'] = 'https://source.test/new.list'
        assert rule_change_kind(base, current, 'mihomo', {'library': True}) == 'structure'


def test_native_inline_update_preserves_custom_dns_and_other_draft_changes():
    base = native(fixture())
    doc = yaml.safe_load(base['oxidns']['native_config'])
    doc['log']['level'] = 'debug'
    base['oxidns']['native_config'] = yaml.safe_dump(doc)
    current = deepcopy(base)
    current['rule_configs'][1]['value'] = 'new-inline.test'
    current_doc = yaml.safe_load(current['oxidns']['native_config'])
    next(plugin for plugin in current_doc['plugins'] if plugin['tag'] == 'forward_local')['args']['upstreams'] = [{'addr': 'udp://192.0.2.9'}]
    current['oxidns']['native_config'] = yaml.safe_dump(current_doc)
    # A native YAML change with stale inline atoms is ambiguous: explicitly block
    # instead of treating the source edit as synchronized.
    with pytest.raises(RuleSyncProjectionError):
        project_rule_changes(base, current, {'rules': True})
    current['oxidns']['native_config'] = base['oxidns']['native_config']
    projected = project_rule_changes(base, current, {'rules': True})
    result = yaml.safe_load(projected['oxidns']['native_config'])
    before = {plugin['tag']: plugin for plugin in doc['plugins']}
    after = {plugin['tag']: plugin for plugin in result['plugins']}
    assert before['forward_local'] == after['forward_local']
    assert result['log'] == doc['log']
    # A changed provider is renamed beside the old one, as the editor does.
    assert any('full:new-inline.test' in str(plugin) for tag, plugin in after.items() if tag.startswith('rules_domain_'))
    assert rule_change_kind(base, current, 'oxidns', {'rules': True}) == 'structure'


def test_native_new_selected_set_is_applied_without_copying_other_native_edits():
    original = fixture()
    base = native(original)
    current_template = deepcopy(original)
    add_set(current_template)
    current_template['oxidns']['proxy_rulesets'] = ['set-b']
    current_template['oxidns']['local_dns'] = 'udp://192.0.2.9'
    current = native(current_template)
    projected = project_rule_changes(base, current, {'library': True})
    previous = yaml.safe_load(base['oxidns']['native_config'])
    proposed = yaml.safe_load(projected['oxidns']['native_config'])
    get = lambda doc, tag: next(plugin for plugin in doc['plugins'] if plugin['tag'] == tag)
    assert get(proposed, 'forward_local') == get(previous, 'forward_local')
    assert _rule_path(current['rule_configs'][0], 'domain') in str(proposed)
    assert rule_change_kind(base, current, 'oxidns', {'library': True}) == 'structure'


def test_native_library_content_stays_content_only_and_preserves_exact_yaml():
    base = native(fixture())
    current = deepcopy(base)
    current['rule_library'][0]['content'] = 'new.test'
    projected = project_rule_changes(base, current, {'library': True})
    assert projected['oxidns']['native_config'] == base['oxidns']['native_config']
    assert rule_change_kind(base, current, 'oxidns', {'library': True}) == 'content'


def test_native_ip_addition_and_removal_keep_sequence_surroundings():
    original = fixture()
    base = native(original)
    current = deepcopy(base)
    current['rule_configs'][1].update(rule_type='IP-CIDR', value='203.0.113.0/24')
    projected = project_rule_changes(base, current, {'rules': True})
    before_doc = yaml.safe_load(base['oxidns']['native_config'])
    after_doc = yaml.safe_load(projected['oxidns']['native_config'])
    path = _rule_path(base['rule_configs'][0], 'domain').removeprefix('./')
    old = bundle(before_doc, 'oxidns', {path: (b'old.test', 'rule')})
    new = bundle(after_doc, 'oxidns', {path: (b'old.test', 'rule')})
    isolated = isolate_rule_bundle(old, new, 'oxidns')
    isolated_doc = unpack_document(isolated)
    assert not any(plugin['type'] == 'ip_set' for plugin in isolated_doc['plugins'])
    assert 'resp_ip ' not in str(isolated_doc)
    assert 'full:old-inline.test' not in str(isolated_doc)
    # The remaining library rule and every surrounding native plugin survive
    # rule-only isolation unchanged; the new IP rule has no DNS consumer.
    assert isolated_doc == after_doc
    reverted = project_rule_changes(projected, base, {'rules': True})
    reverted_doc = yaml.safe_load(reverted['oxidns']['native_config'])
    assert 'resp_ip ' not in str(reverted_doc)
    restored = isolate_rule_bundle(isolated, bundle(reverted_doc, 'oxidns', {path: (b'old.test', 'rule')}), 'oxidns')
    assert unpack_document(restored) == before_doc


def test_native_download_and_reload_follow_new_selected_sources():
    original = fixture()
    original['oxidns']['rule_update'] = {'enabled': True}
    base = native(original)
    desired = deepcopy(original)
    add_set(desired)
    desired['oxidns']['proxy_rulesets'] = ['set-b']
    current = native(desired)
    projected = project_rule_changes(base, current, {'library': True})
    previous = yaml.safe_load(base['oxidns']['native_config'])
    proposed = yaml.safe_load(projected['oxidns']['native_config'])
    old_files = {_rule_path(base['rule_configs'][0], 'domain').removeprefix('./'): (b'old.test', 'rule')}
    new_files = {**old_files, _rule_path(current['rule_configs'][0], 'domain').removeprefix('./'): (b'b.test', 'rule')}
    isolated = isolate_rule_bundle(bundle(previous, 'oxidns', old_files), bundle(proposed, 'oxidns', new_files), 'oxidns')
    plugins = {plugin['tag']: plugin for plugin in unpack_document(isolated)['plugins']}
    assert len(plugins['rules_download']['args']['downloads']) == 2
    assert len(plugins['rules_reload']['args']) == 2


@pytest.mark.parametrize('changes', [
    {'proxy_rules': ['single'], 'direct_rules': []},
    {'direct_rulesets': ['set-a'], 'proxy_rulesets': ['set-b', 'set-c']},
    {'direct_rulesets': ['set-c']},
])
def test_native_projection_reproduces_routing_edit_saved_in_draft(changes):
    # DNS follow compares the rendered draft with the deployed bytes. A routing
    # edit saved in the draft and then published by rule sync must therefore
    # keep the draft's provider tags and plugin order, not rename or regroup.
    from backend.common.oxidns_common import apply_common_settings
    template = fixture()
    add_set(template)
    add_set(template, 'c')
    template['oxidns'].update(proxy_rulesets=['set-b'], direct_rulesets=['set-a', 'set-c'])
    base = native(template)
    current = deepcopy(base)
    current['oxidns'] = apply_common_settings(base['oxidns'], changes, base)['settings']
    draft = yaml.safe_load(current['oxidns']['native_config'])
    assert draft != yaml.safe_load(base['oxidns']['native_config'])
    projected = project_rule_changes(base, current, {'library': True, 'rules': True})
    assert yaml.safe_load(projected['oxidns']['native_config']) == draft


@pytest.mark.parametrize('changes', [
    {'proxy_rules': ['single'], 'direct_rules': []},
    {'direct_rulesets': ['set-a'], 'proxy_rulesets': ['set-b', 'set-c']},
    {'direct_rulesets': ['set-c']},
])
def test_native_projection_keeps_the_drafts_reload_order(changes):
    # The reload list is an unordered set to OxiDNS, but sync status and DNS
    # follow compare bytes. A hand-ordered list must not be regrouped.
    from backend.common.oxidns_common import apply_common_settings
    template = fixture()
    template['oxidns']['rule_update'] = {'enabled': True}
    add_set(template)
    add_set(template, 'c')
    template['oxidns'].update(proxy_rulesets=['set-b'], direct_rulesets=['set-a', 'set-c'])
    base = native(template)
    document = yaml.safe_load(base['oxidns']['native_config'])
    reload = next(plugin for plugin in document['plugins'] if plugin['tag'] == 'rules_reload')
    reload['args'].reverse()
    base['oxidns']['native_config'] = yaml.safe_dump(document, allow_unicode=True, sort_keys=False)
    current = deepcopy(base)
    current['oxidns'] = apply_common_settings(base['oxidns'], changes, base)['settings']
    draft = yaml.safe_load(current['oxidns']['native_config'])
    projected = project_rule_changes(base, current, {'library': True, 'rules': True})
    assert yaml.safe_load(projected['oxidns']['native_config']) == draft
    args = next(plugin for plugin in draft['plugins'] if plugin['tag'] == 'rules_reload')['args']
    retained = [value for value in reload['args'] if value in args]
    assert args[:len(retained)] == retained


def test_reload_references_edit_in_place():
    from backend.common.oxidns_common_routing import reload_references
    existing = ['$rules_ip_1', '$adguard_rules', '$rules_domain_1', '$rules_domain_2']
    assert reload_references(existing, {'rules_ip_1', 'rules_domain_1', 'rules_domain_2'},
                             ['rules_domain_1', 'rules_ip_1', 'rules_domain_3'], {'adguard_rules'}) == [
        '$rules_ip_1', '$adguard_rules', '$rules_domain_1', '$rules_domain_3']


def test_mihomo_bundle_cannot_publish_nodes_dns_or_refetched_subscription_bytes():
    previous = {'dns': {'enable': True}, 'proxies': [{'name': 'old', 'server': 'old.test'}],
                'rules': ['RULE-SET,A,DIRECT'], 'rule-providers': {'A': {'type': 'http', 'path': './rules/a.list'}}}
    proposed = deepcopy(previous)
    proposed['dns']['enable'] = False
    proposed['proxies'][0]['server'] = 'new.test'
    proposed['rules'] = ['RULE-SET,A,REJECT']
    old = bundle(previous, extras={'rules/a.list': (b'old.test', 'rule'), 'providers/nodes.yaml': (b'old nodes', 'provider')})
    new = bundle(proposed, extras={'rules/a.list': (b'new.test', 'rule'), 'providers/nodes.yaml': (b'new nodes', 'provider')}, deployment='new')
    isolated = isolate_rule_bundle(old, new, 'mihomo')
    actual = unpack_document(isolated)
    assert actual['dns'] == previous['dns'] and actual['proxies'] == previous['proxies']
    assert actual['rules'] == proposed['rules']
    assert read_bundle_files(isolated)['providers/nodes.yaml'] == (b'old nodes', 'provider')
    assert read_bundle_files(isolated)['rules/a.list'] == (b'new.test', 'rule')
    assert isolated.manifest['deployment_id'] == 'new'
    assert deployment_content_fingerprint(isolated.manifest) != deployment_content_fingerprint(old.manifest)


def test_oxidns_bundle_preserves_custom_files_and_other_rule_subscriptions():
    base = fixture()
    current = deepcopy(base)
    current['rule_configs'][1]['value'] = 'new-inline.test'
    previous = yaml.safe_load(generate_oxidns_config(base))
    proposed = yaml.safe_load(generate_oxidns_config(current))
    path = _rule_path(base['rule_configs'][0], 'domain').removeprefix('./')
    old = bundle(previous, 'oxidns', {path: (b'old.test', 'rule'), 'rules/adguard.txt': (b'old ads', 'rule'),
                                    'rules/custom.txt': (b'old custom', 'custom')})
    new = bundle(proposed, 'oxidns', {path: (b'new.test', 'rule'), 'rules/adguard.txt': (b'new ads', 'rule'),
                                    'rules/custom.txt': (b'new custom', 'custom')}, deployment='new')
    isolated = isolate_rule_bundle(old, new, 'oxidns')
    files = read_bundle_files(isolated)
    assert files['rules/adguard.txt'] == (b'old ads', 'rule')
    assert files['rules/custom.txt'] == (b'old custom', 'custom')
    assert 'new-inline.test' in str(unpack_document(isolated))


@pytest.mark.parametrize('mutate', ['upstream', 'sequence'])
def test_oxidns_bundle_rejects_unrelated_plugin_or_sequence_mutation(mutate):
    previous = yaml.safe_load(generate_oxidns_config(fixture()))
    proposed = deepcopy(previous)
    tag = 'forward_local' if mutate == 'upstream' else 'sequence_main'
    plugin = next(value for value in proposed['plugins'] if value['tag'] == tag)
    if mutate == 'upstream':
        plugin['args']['upstreams'] = [{'addr': 'udp://192.0.2.8'}]
    else:
        plugin['args'].insert(0, {'exec': 'reject 3'})
    with pytest.raises(RuleSyncProjectionError):
        isolate_rule_bundle(bundle(previous, 'oxidns'), bundle(proposed, 'oxidns'), 'oxidns')


def test_bundle_isolation_rejects_cross_target_and_file_role_conflicts():
    doc = {'rules': [], 'rule-providers': {'A': {'type': 'http', 'path': './rules/a.list'}}}
    old = bundle(doc, extras={'rules/a.list': (b'old', 'rule')})
    new = bundle(doc, extras={'rules/a.list': (b'new', 'custom')})
    with pytest.raises(RuleSyncProjectionError):
        isolate_rule_bundle(old, new, 'mihomo')
    new.manifest['agent_id'] = 'other'
    with pytest.raises(RuleSyncProjectionError):
        isolate_rule_bundle(old, new, 'mihomo')


def real_source():
    from backend.common.config import get_default_config
    source = get_default_config()
    source.update(deepcopy(fixture()))
    source['nodes'][0].update(name='Node', type='http', port=8080)
    source['mihomo']['custom_config'] = 'mode: rule\ndns: {enable: false}\n'
    return source


def real_baseline(source, service, *, pinned_files=(), local_dns_watchdog=True):
    """Exercise the production converters and materializer with offline fixtures."""
    from backend.agents.deployment_bundle import build_deployment_bundle, retarget_config_path, DeploymentBundle
    agent = {'id': 'agent', 'profile_id': 'home', 'service_type': service}
    if service == 'mihomo':
        from backend.converters.mihomo import generate_mihomo_config
        content = generate_mihomo_config(source, base_url='https://config.test', sync_lan_hosts=True,
                                         preflight_providers=False)
        providers = [{'name': name, 'local_path': value['path'],
                      'content': b'proxies: [{name: Offline, type: http, server: baseline.test, port: 8080}]\n'}
                     for name, value in yaml.safe_load(content).get('proxy-providers', {}).items()]
        rules, custom = [], []
    else:
        from backend.converters.oxidns import get_oxidns_ruleset_downloads, get_oxidns_custom_files
        content = generate_oxidns_config(source, base_url='https://config.test')
        providers, rules, custom = [], get_oxidns_ruleset_downloads(source), get_oxidns_custom_files(source)
    value = build_deployment_bundle(agent, source, content, provider_downloads=providers,
                                   ruleset_downloads=rules, custom_files=custom, pinned_files=pinned_files,
                                   base_url='https://config.test', deployment_id='baseline',
                                   local_dns_watchdog=local_dns_watchdog)
    value = retarget_config_path(value, 'installed.yml')
    digest = next(item['sha256'] for item in value.manifest['files'] if item['role'] == 'config')
    # Receipts can store the complete core-reported digest, unlike the short
    # converter preview version. Candidate creation must accept both.
    value = DeploymentBundle(value.manifest, value.archive, value.sha256, digest)
    return agent, {'bundle': value, 'source': deepcopy(source), 'base_url': 'https://config.test'}


def no_network(monkeypatch):
    monkeypatch.setattr('requests.sessions.Session.request',
                        lambda *_args, **_kwargs: pytest.fail('candidate must not access external network'))
    monkeypatch.setattr('backend.utils.sub_store_client.get_subscription_proxies_yaml',
                        lambda *_args, **_kwargs: pytest.fail('candidate must preserve subscription bytes'))


def test_prepare_candidate_real_mihomo_keeps_offline_subscription_and_retargets_digest(monkeypatch):
    from backend.agents.rule_sync import prepare_candidate
    no_network(monkeypatch)
    base = real_source()
    base['subscriptions'] = [{'id': 'feed', 'name': 'OfflineFeed', 'url': 'https://offline.test/sub', 'enabled': True}]
    base['proxy_groups'] = [{'id': 'group', 'name': 'Proxy', 'type': 'select', 'subscriptions': ['feed']}]
    agent, receipt = real_baseline(base, 'mihomo')
    current = deepcopy(base)
    current['subscriptions'][0]['url'] = 'https://unpublished.test/sub'
    current['rule_configs'][1]['value'] = 'new-inline.test'
    settings = {'library': False, 'rules': True}
    projected = project_rule_changes(base, current, settings, service_type='mihomo')
    candidate = prepare_candidate(agent, projected, receipt, settings, 'new-deployment')
    files = read_bundle_files(candidate)
    original = read_bundle_files(receipt['bundle'])
    assert files['providers/OfflineFeed.yaml'] == original['providers/OfflineFeed.yaml']
    assert files['ruleset/A.list'] == original['ruleset/A.list']
    assert 'DOMAIN,new-inline.test,DIRECT' in unpack_document(candidate)['rules']
    assert candidate.manifest['config_path'] == 'installed.yml'
    assert candidate.manifest['deployment_id'] == 'new-deployment'
    assert candidate.manifest['reload_only'] is True
    assert len(receipt['bundle'].config_version) == 64
    config_digest = next(item['sha256'] for item in candidate.manifest['files'] if item['role'] == 'config')
    assert candidate.config_version in (config_digest, config_digest[:8])


@pytest.mark.parametrize('service', ['mihomo', 'oxidns'])
def test_prepare_candidate_library_off_never_refetches_remote_rule(service, monkeypatch):
    from backend.agents.rule_sync import prepare_candidate
    no_network(monkeypatch)
    base = real_source()
    base['rule_library'][0].update(source_type='url', url='https://offline.test/rules.list')
    base['rule_library'][0].pop('content')
    base = project_rule_changes(base, base, {'library': True, 'rules': True}, service_type=service)
    path = 'ruleset/A.list' if service == 'mihomo' else _rule_path(base['rule_configs'][0], 'domain').removeprefix('./')
    payload = b'old.test\n' if service == 'mihomo' else b'full:old.test\n'
    agent, receipt = real_baseline(base, service, pinned_files=[{'path': path, 'content': payload, 'role': 'rule'}])
    current = deepcopy(base)
    current['rule_library'][0]['url'] = 'https://unpublished.test/rules.list'
    current['rule_configs'][1]['value'] = 'new-inline.test'
    settings = {'library': False, 'rules': True}
    projected = project_rule_changes(base, current, settings, service_type=service)
    candidate = prepare_candidate(agent, projected, receipt, settings, 'offline-candidate')
    assert read_bundle_files(candidate)[path] == read_bundle_files(receipt['bundle'])[path]
    assert 'new-inline.test' in str(unpack_document(candidate))


@pytest.mark.parametrize('change', ['inline', 'new-set', 'dns'])
def test_prepare_candidate_real_oxidns_native_graph_with_offline_adguard(change, monkeypatch):
    from backend.agents.rule_sync import prepare_candidate
    from backend.converters.oxidns import _tag
    no_network(monkeypatch)
    base_template = real_source()
    adguard_url = 'https://offline.test/adguard.txt'
    base_template['oxidns']['adguard'] = {'enabled': True, 'urls': [adguard_url], 'content': '||local-ad.test^'}
    base_template['oxidns']['rule_update'] = {'enabled': True}
    base = native(base_template)
    adguard_path = 'rules/' + _tag('adguard', adguard_url) + '.txt'
    agent, receipt = real_baseline(base, 'oxidns', pinned_files=[
        {'path': adguard_path, 'content': b'||baseline-ad.test^\n', 'role': 'rule'}])
    current_template = deepcopy(base_template)
    if change == 'new-set':
        add_set(current_template)
        current_template['oxidns']['proxy_rulesets'] = ['set-b']
        current = native(current_template)
        settings = {'library': True, 'rules': False}
    elif change == 'dns':
        from backend.common.rule_dns import entry_key
        current = with_dns(base, {'set-a': 'blackhole', 'single': 'upstream:' + entry_key(LOCAL['upstream'], 'local')})
        settings = {'library': True, 'rules': True}
    else:
        current = deepcopy(base)
        current['rule_configs'][1]['value'] = 'new-inline.test'
        settings = {'library': False, 'rules': True}
    current['oxidns']['adguard']['content'] = '||unpublished-ad.test^'
    projected = project_rule_changes(base, current, settings, service_type='oxidns')
    candidate = prepare_candidate(agent, projected, receipt, settings, 'native-candidate')
    files = read_bundle_files(candidate)
    original = read_bundle_files(receipt['bundle'])
    assert files[adguard_path] == original[adguard_path]
    assert files['rules/adguard-custom.txt'] == original['rules/adguard-custom.txt']
    actual = unpack_document(candidate)
    if change == 'inline':
        assert 'full:new-inline.test' in str(actual)
    elif change == 'dns':
        tags = {plugin['tag'] for plugin in actual['plugins']}
        assert 'cf_dns_blackhole' in tags and any(tag.startswith('cf_dns_up_') for tag in tags)
    else:
        assert _rule_path(current['rule_configs'][0], 'domain') in str(actual)
    assert candidate.manifest['config_path'] == 'installed.yml'


@pytest.mark.parametrize('guarded', [False, True])
@pytest.mark.parametrize('mode', ['template', 'native'])
def test_rule_sync_preserves_watchdog_migration_boundary(guarded, mode, monkeypatch):
    from backend.agents.rule_sync import prepare_candidate
    from backend.common.dns_watchdog import ensure_local_dns_watchdog
    no_network(monkeypatch)
    base = real_source()
    if mode == 'native':
        base = native(base)
        document = yaml.safe_load(base['oxidns']['native_config'])
        # A native listener may directly invoke an executor. Its generated
        # watchdog wrapper must remain stable during subsequent rule updates.
        document['plugins'].append({'tag': 'custom_udp', 'type': 'udp_server',
                                    'args': {'listen': '127.0.0.1:15353', 'entry': 'forward_local'}})
        base['oxidns']['native_config'] = yaml.safe_dump(document, sort_keys=False)
    original_source = deepcopy(base)
    agent, receipt = real_baseline(base, 'oxidns', local_dns_watchdog=guarded)
    previous = unpack_document(receipt['bundle'])
    assert (ensure_local_dns_watchdog(previous) == previous) is guarded
    current = deepcopy(base)
    current['rule_configs'][1]['value'] = 'new-inline.test'
    settings = {'library': False, 'rules': True}
    projected = project_rule_changes(base, current, settings, service_type='oxidns')

    candidate = prepare_candidate(agent, projected, receipt, settings, 'watchdog-rule-sync')

    actual = unpack_document(candidate)
    assert 'full:new-inline.test' in str(actual)
    assert (ensure_local_dns_watchdog(actual) == actual) is guarded
    assert candidate.manifest['reload_only'] is True
    assert base == original_source
    old_plugins = {plugin['tag']: plugin for plugin in previous['plugins']}
    new_plugins = {plugin['tag']: plugin for plugin in actual['plugins']}
    for tag, plugin in old_plugins.items():
        if tag not in ('sequence_main', 'rules_reload') and not tag.startswith('rules_domain_'):
            assert new_plugins[tag] == plugin
    # An ordinary publication always completes migration, including after an
    # old deployment has accepted a rule-only update without the local guard.
    _, published = real_baseline(projected, 'oxidns')
    document = unpack_document(published['bundle'])
    assert ensure_local_dns_watchdog(document) == document


def test_prepare_candidate_mihomo_new_rule_cannot_smuggle_an_unpublished_group(monkeypatch):
    from backend.agents.rule_sync import prepare_candidate
    from backend.utils.strategy_references import StrategyReferenceError
    no_network(monkeypatch)
    base = real_source()
    agent, receipt = real_baseline(base, 'mihomo')
    current = deepcopy(base)
    current['proxy_groups'].append({'id': 'unpublished', 'name': 'Unpublished', 'type': 'select', 'manual_nodes': ['DIRECT']})
    current['rule_configs'].insert(0, {'id': 'new', 'itemType': 'rule', 'rule_type': 'DOMAIN',
                                     'value': 'new.test', 'policy': 'Unpublished'})
    settings = {'library': False, 'rules': True}
    projected = project_rule_changes(base, current, settings, service_type='mihomo')
    assert projected['proxy_groups'] == base['proxy_groups']
    with pytest.raises(StrategyReferenceError):
        prepare_candidate(agent, projected, receipt, settings, 'blocked-group')


def test_adapted_device_dns_is_preserved_without_a_false_pending_badge(monkeypatch):
    from types import SimpleNamespace
    from backend.agents.rule_sync import prepare_candidate
    from backend.agents.manager import AgentManager
    no_network(monkeypatch)
    base = real_source()
    agent, receipt = real_baseline(base, 'mihomo')
    actual = unpack_document(receipt['bundle'])
    actual['dns']['listen'] = '127.0.0.1:1053'  # NativeAIO's confirmed effective port
    files = read_bundle_files(receipt['bundle'])
    path = receipt['bundle'].manifest['config_path']
    data = yaml.safe_dump(actual, allow_unicode=True, sort_keys=False).encode()
    files[path] = (data, 'config')
    manifest = deepcopy(receipt['bundle'].manifest)
    for entry in manifest['files']:
        if entry['role'] == 'config':
            entry.update(size=len(data), sha256=hashlib.sha256(data).hexdigest())
    receipt['bundle'] = _pack(manifest, files, hashlib.sha256(data).hexdigest())
    current = deepcopy(base)
    current['rule_configs'][1]['value'] = 'new-inline.test'
    settings = {'library': False, 'rules': True}
    source = project_rule_changes(base, current, settings, service_type='mihomo')
    candidate = prepare_candidate(agent, source, receipt, settings, 'adapted-rules')
    assert unpack_document(candidate)['dns']['listen'] == '127.0.0.1:1053'
    _, expected = real_baseline(source, 'mihomo')
    state = {'profile_id': source['profile_id']}
    managed = {**agent, 'deployments': {'adapted-rules': state}}
    manager = SimpleNamespace(_update_agents=lambda function: function([managed]))
    AgentManager._remember_deployment_content(manager, agent['id'], candidate, 'https://config.test')
    assert state['prepared_content']['fingerprint'] == deployment_content_fingerprint(expected['bundle'].manifest)


def remote_sets(service):
    """Two selected remote rule sets with offline baseline bytes."""
    base = real_source()
    base['rule_library'][0].update(source_type='url', url='https://offline.test/a.list')
    base['rule_library'][0].pop('content')
    add_set(base)
    base['rule_library'][1].update(source_type='url', url='https://offline.test/b.list')
    base['rule_library'][1].pop('content')
    base['oxidns']['direct_rulesets'].append('set-b')
    base = project_rule_changes(base, base, {'library': True, 'rules': True}, service_type=service)
    paths = {}
    for identity, name in (('set-a', 'A'), ('set-b', 'B')):
        item = next(entry for entry in base['rule_configs'] if entry['id'] == identity)
        paths[identity] = (f'ruleset/{name}.list' if service == 'mihomo'
                           else _rule_path(item, 'domain').removeprefix('./'))
    payload = (lambda name: f'{name}.test\n'.encode()) if service == 'mihomo' else (
        lambda name: f'full:{name}.test\n'.encode())
    pinned = [{'path': paths['set-a'], 'content': payload('live-a'), 'role': 'rule'},
              {'path': paths['set-b'], 'content': payload('live-b'), 'role': 'rule'}]
    return base, paths, pinned


@pytest.mark.parametrize('service', ['mihomo', 'oxidns'])
def test_prepare_candidate_library_scope_reuses_unchanged_rule_files(service, monkeypatch):
    from backend.agents import deployment_bundle
    from backend.agents.rule_sync import prepare_candidate
    no_network(monkeypatch)
    base, paths, pinned = remote_sets(service)
    agent, receipt = real_baseline(base, service, pinned_files=pinned)
    fetched = []
    monkeypatch.setattr(deployment_bundle, '_fetch_bytes', lambda url, _config: fetched.append(url) or b'new-a.test\n')
    current = deepcopy(base)
    current['rule_configs'][-1]['value'] = 'new-inline.test'
    settings = {'library': True, 'rules': True}
    projected = project_rule_changes(base, current, settings, service_type=service)
    candidate = prepare_candidate(agent, projected, receipt, settings, 'structure-only')
    files, original = read_bundle_files(candidate), read_bundle_files(receipt['bundle'])
    assert fetched == []
    assert all(files[path] == original[path] for path in paths.values())
    assert 'new-inline.test' in str(unpack_document(candidate))

    # Only the edited source is downloaded again; the other keeps live bytes.
    current['rule_library'][0]['url'] = 'https://offline.test/new-a.list'
    projected = project_rule_changes(base, current, settings, service_type=service)
    candidate = prepare_candidate(agent, projected, receipt, settings, 'one-source')
    files = read_bundle_files(candidate)
    assert fetched == ['https://offline.test/new-a.list']
    assert files[paths['set-b']] == original[paths['set-b']]
    assert b'new-a.test' in files[paths['set-a']][0]


def test_rule_source_fingerprint_follows_local_callbacks_without_fetching():
    from backend.agents.rule_sync_projection import rule_source_fingerprint, unchanged_rule_paths
    base = fixture()
    current = deepcopy(base)
    current['rule_library'][0]['content'] = 'new.test'
    local = 'https://config.test/api/profiles/home/rule-library/content/lib-a'
    proxy = 'https://config.test/api/profiles/home/mosdns/rule-proxy?url=%2Fapi%2Fprofiles%2Fhome%2Frule-library%2Fcontent%2Flib-a&part=domain'
    for url in (local, proxy, '/api/profiles/home/rules/local/A'):
        before = rule_source_fingerprint(url, base, 'https://config.test')
        assert before is not None and before != rule_source_fingerprint(url, current, 'https://config.test')
    assert rule_source_fingerprint('https://rules.test/a.list', base, '') == ['url', 'https://rules.test/a.list']
    assert rule_source_fingerprint('https://config.test/api/profiles/home/linux-gateway/rules/x', base, '') is None
    entries = [{'url': local, 'local_path': './ruleset/A.list'},
               {'url': 'https://rules.test/b.list', 'local_path': './ruleset/B.list'}]
    assert unchanged_rule_paths(entries, entries, base, current, 'https://config.test') == {'ruleset/B.list'}
    moved = [{**entries[1], 'url': 'https://rules.test/moved.list'}]
    assert unchanged_rule_paths(entries, moved, base, base, 'https://config.test') == set()


# ---- Per-rule OxiDNS DNS handling ---------------------------------------------

LOCAL = {'action': 'upstream', 'source': 'local', 'upstream': {'addr': 'udp://192.0.2.1'}}
DRAFT = {'action': 'upstream', 'source': 'local', 'upstream': {'addr': 'udp://192.0.2.7'}}


def with_dns(config, assignments):
    from backend.common.rule_dns import apply_rule_dns
    result = deepcopy(config)
    apply_rule_dns(deepcopy(result), result, assignments)
    return result


def plugins_of(config):
    return {plugin['tag']: plugin for plugin in yaml.safe_load(config['oxidns']['native_config'])['plugins']}


def main_execs(config):
    return {entry['matches'][0]: entry['exec'] for entry in plugins_of(config)['sequence_main']['args']
            if entry.get('matches') and entry['matches'][0].startswith(('qname $rules_', 'resp_ip $rules_'))}


def test_projection_copies_rule_actions_and_follow_by_scope():
    base = fixture()
    base['oxidns'].update(rule_actions={'set-a': {'action': 'refused'}}, direct_rulesets=[])
    current = deepcopy(base)
    current['oxidns'].update(rule_actions={'set-a': {'action': 'blackhole'}, 'single': {'action': 'nxdomain'}},
                             direct_rules=[], rule_dns_follow=[], rule_dns_parked={})
    rules = project_rule_changes(base, current, {'rules': True})['oxidns']
    assert rules['rule_actions'] == {'set-a': {'action': 'refused'}, 'single': {'action': 'nxdomain'}}
    assert rules['direct_rules'] == []
    library = project_rule_changes(base, current, {'library': True})['oxidns']
    assert library['rule_actions'] == {'set-a': {'action': 'blackhole'}}
    assert library['direct_rules'] == ['single']
    # Follow intent and a removed rule's choices travel with their scope too.
    current['oxidns'].update(rule_actions={'set-a': {'action': 'blackhole'}}, direct_rules=['single'],
                             rule_dns_follow=['single'])
    assert project_rule_changes(base, current, {'library': True})['oxidns']['rule_dns_follow'] == []
    assert project_rule_changes(base, current, {'rules': True})['oxidns']['rule_dns_follow'] == ['single']
    current['rule_configs'].pop(0)
    assert project_rule_changes(base, current, {'library': True})['oxidns']['rule_actions'] == {}
    for scope in ('library', 'rules'):
        kind = rule_change_kind(base, with_dns(base, {'set-a' if scope == 'library' else 'single': 'blackhole'}),
                                'oxidns', {scope: True})
        assert kind == 'structure'


@pytest.mark.parametrize('mode', ['template', 'native'])
def test_projection_withholds_unpublished_pinned_upstream(mode):
    base = fixture() if mode == 'template' else native(fixture())
    draft = deepcopy(base)
    if mode == 'template':
        draft['oxidns']['local_dns'] = 'udp://192.0.2.1\nudp://192.0.2.7'
    else:
        document = yaml.safe_load(draft['oxidns']['native_config'])
        next(plugin for plugin in document['plugins'] if plugin['tag'] == 'forward_local')['args']['upstreams'].append(
            {'addr': 'udp://192.0.2.7'})
        draft['oxidns']['native_config'] = yaml.safe_dump(document, sort_keys=False)
    from backend.common.rule_dns import entry_key
    current = with_dns(draft, {'single': 'upstream:' + entry_key(DRAFT['upstream'], 'local'),
                               'set-a': 'upstream:' + entry_key(LOCAL['upstream'], 'local')})
    assert current['oxidns']['rule_actions'] == {'set-a': LOCAL, 'single': DRAFT}
    withheld = set()
    projected = project_rule_changes(base, current, {'library': True, 'rules': True}, service_type='oxidns',
                                     withheld=withheld)
    assert withheld == {'single'}
    # The published upstream is pinned; the draft one keeps the published route.
    assert projected['oxidns']['rule_actions'] == {'set-a': LOCAL}
    assert 'single' in projected['oxidns']['direct_rules']
    rendered = generate_oxidns_config(projected)
    assert '192.0.2.7' not in rendered and 'cf_dns_up_' + entry_key(LOCAL['upstream'], 'local') in rendered
    # Pins already published are not reported again.
    withheld.clear()
    projected = project_rule_changes(projected, with_dns(current, {'single': 'direct'}),
                                     {'library': True, 'rules': True}, withheld=withheld)
    assert withheld == set() and projected['oxidns']['rule_actions'] == {'set-a': LOCAL}


def test_native_projection_emits_actions_via_apply_routing():
    from backend.common.oxidns_common_routing import read_routing
    from backend.common.rule_dns import entry_key
    base = native(fixture())
    current = with_dns(base, {'set-a': 'blackhole', 'single': 'upstream:' + entry_key(LOCAL['upstream'], 'local')})
    current_doc = yaml.safe_load(current['oxidns']['native_config'])
    next(plugin for plugin in current_doc['plugins'] if plugin['tag'] == 'forward_remote')['args']['upstreams'] = [
        {'addr': 'udp://192.0.2.9'}]
    current['oxidns']['native_config'] = yaml.safe_dump(current_doc, sort_keys=False)
    projected = project_rule_changes(base, current, {'library': True, 'rules': True}, service_type='oxidns')
    plugins = plugins_of(projected)
    # Only rule routing and its owned helpers move; the draft's upstream edit stays out.
    assert plugins['forward_remote'] == plugins_of(base)['forward_remote']
    assert plugins['cf_dns_blackhole']['args'] == {'mode': 'null', 'short_circuit': True}
    key = entry_key(LOCAL['upstream'], 'local')
    assert sorted(main_execs(projected).values()) == ['$cf_dns_blackhole', 'goto cf_dns_up_' + key]
    values = read_routing(yaml.safe_load(projected['oxidns']['native_config']), projected['oxidns'], projected)['values']
    assert values['rule_actions'] == {'set-a': {'action': 'blackhole'}, 'single': LOCAL}
    assert projected['oxidns']['rule_actions'] == values['rule_actions']
    assert rule_change_kind(base, current, 'oxidns', {'library': True, 'rules': True}) == 'structure'
    path = _rule_path(base['rule_configs'][0], 'domain').removeprefix('./')
    files = {path: (b'old.test', 'rule')}
    isolate_rule_bundle(bundle(yaml.safe_load(base['oxidns']['native_config']), 'oxidns', files),
                        bundle(yaml.safe_load(projected['oxidns']['native_config']), 'oxidns', files), 'oxidns')
    # Only the library scope: the inline rule keeps its published direct route.
    library = project_rule_changes(base, current, {'library': True}, service_type='oxidns')
    assert sorted(main_execs(library).values()) == ['$cf_dns_blackhole', 'goto china_dns']
    assert 'cf_dns_up_' + key not in plugins_of(library)
    # Reverting the draft removes the helpers again.
    reverted = project_rule_changes(projected, base, {'library': True, 'rules': True}, service_type='oxidns')
    assert not [tag for tag in plugins_of(reverted) if tag.startswith('cf_dns_')]


def test_native_projection_without_custom_matches():
    original = fixture()
    original['oxidns']['direct_rules'] = []
    base = native(original)
    from backend.common.oxidns_common_routing import read_routing
    before = read_routing(yaml.safe_load(base['oxidns']['native_config']), base['oxidns'], base)['values']
    assert before['custom_matches'] == [] and before['custom_match_position'] is None
    current = with_dns(base, {'single': 'refused'})
    projected = project_rule_changes(base, current, {'rules': True}, service_type='oxidns')
    assert sorted(main_execs(projected).values()) == ['goto china_dns', 'reject']
    assert projected['oxidns']['rule_actions'] == {'single': {'action': 'refused'}}


def test_isolation_allows_owned_dns_helpers():
    from backend.common.rule_dns import entry_key
    base = fixture()
    previous = yaml.safe_load(generate_oxidns_config(base))
    key = entry_key(LOCAL['upstream'], 'local')
    candidate = yaml.safe_load(generate_oxidns_config(
        with_dns(base, {'set-a': 'blackhole', 'single': 'upstream:' + key})))
    path = _rule_path(base['rule_configs'][0], 'domain').removeprefix('./')
    files = {path: (b'old.test', 'rule')}
    isolated = unpack_document(isolate_rule_bundle(bundle(previous, 'oxidns', files),
                                                   bundle(candidate, 'oxidns', files), 'oxidns'))
    tags = [plugin['tag'] for plugin in isolated['plugins']]
    assert {'cf_dns_blackhole', 'cf_dns_up_' + key, 'cf_dns_fwd_' + key, 'cf_dns_cache_' + key} <= set(tags)
    assert tags.index('cf_dns_up_' + key) < tags.index('sequence_main')
    # Reverting to the published routing drops them from the next candidate.
    isolate_rule_bundle(bundle(isolated, 'oxidns', files), bundle(previous, 'oxidns', files), 'oxidns')
    # A candidate pinning an upstream the device does not have, or carrying a
    # hand-shaped helper, is refused at this second boundary.
    unpublished = deepcopy(candidate)
    for plugin in unpublished['plugins']:
        if plugin['tag'] == 'cf_dns_fwd_' + key:
            plugin['args']['upstreams'] = [{'addr': 'udp://192.0.2.7'}]
    edited = deepcopy(candidate)
    next(plugin for plugin in edited['plugins'] if plugin['tag'] == 'cf_dns_blackhole')['args']['mode'] = None
    for value in (unpublished, edited):
        with pytest.raises(RuleSyncProjectionError):
            isolate_rule_bundle(bundle(previous, 'oxidns', files), bundle(value, 'oxidns', files), 'oxidns')


# ---- one pass for the projection and both signatures ------------------------------------------


def _edits():
    """(name, edit of the current configuration) pairs covering none, content and structure."""
    def unused(config):
        config['rule_library'].append({'id': 'unused', 'name': 'Unused', 'source_type': 'content', 'content': 'unused.test'})
    def content(config):
        config['rule_library'][0]['content'] = 'new.test'
    def inline(config):
        config['rule_configs'][1]['value'] = 'new-inline.test'
    def reference(config):
        add_set(config)
        config['oxidns']['proxy_rulesets'] = ['set-b']
    def metadata(config):
        config['rule_library'][0]['behavior'] = 'classical'
    def unrelated(config):
        config['nodes'][0]['server'] = 'new-node.test'
        config['oxidns']['local_dns'] = 'udp://192.0.2.9'
    def everything(config):
        content(config), inline(config), reference(config)
    return [('nothing', lambda config: None), ('unused', unused), ('content', content), ('inline', inline),
            ('reference', reference), ('metadata', metadata), ('unrelated', unrelated), ('everything', everything)]


def _reference_kind(base, current, service, settings):
    """The classification as it was computed before rule_changes: one rendering per signature."""
    projected = project_rule_changes(base, current, settings, service_type=service)
    if rule_signature(base, service) == rule_signature(projected, service):
        return 'none'
    if rule_signature(base, service, include_content=False) == rule_signature(projected, service, include_content=False):
        return 'content'
    return 'structure'


@pytest.mark.parametrize('mode', ['mihomo', 'template', 'native'])
def test_signatures_from_one_rendering_equal_the_separate_ones(mode):
    from backend.agents.rule_sync_projection import rule_signatures
    service = 'mihomo' if mode == 'mihomo' else 'oxidns'
    for _, edit in _edits():
        config = native(fixture()) if mode == 'native' else fixture()
        edit(config)
        assert rule_signatures(config, service) == (
            rule_signature(config, service), rule_signature(config, service, include_content=False))
    # The two differ exactly when content is part of what the device consumes.
    with_content, without = rule_signatures(fixture(), service)
    assert with_content != without


@pytest.mark.parametrize('mode', ['mihomo', 'template', 'native'])
@pytest.mark.parametrize('settings', [{'library': True}, {'rules': True}, {'library': True, 'rules': True}, {}])
def test_rule_changes_matches_the_separate_projection_and_classification(mode, settings):
    from backend.agents.rule_sync_projection import rule_changes
    service = 'mihomo' if mode == 'mihomo' else 'oxidns'
    kinds = set()
    for name, edit in _edits():
        base = native(fixture()) if mode == 'native' else fixture()
        current = deepcopy(base)
        edit(current)
        before, draft = deepcopy(base), deepcopy(current)
        expected_withheld = set()
        expected = project_rule_changes(base, current, settings, service_type=service, withheld=expected_withheld)
        kind, projected, withheld = rule_changes(base, current, service, settings)
        assert (kind, projected, withheld) == (_reference_kind(base, current, service, settings), expected, expected_withheld), name
        # Repeating it gives the same answer, and nothing is mutated.
        assert rule_changes(base, current, service, settings) == (kind, projected, withheld), name
        assert rule_change_kind(base, current, service, settings) == kind
        assert (base, current) == (before, draft), name
        kinds.add(kind)
    if settings.get('library') and settings.get('rules'):
        assert kinds == {'none', 'content', 'structure'}


def test_rule_changes_reports_withheld_pins_and_unsafe_structures():
    from backend.agents.rule_sync_projection import rule_changes
    from backend.common.rule_dns import entry_key
    base = fixture()
    draft = deepcopy(base)
    draft['oxidns']['local_dns'] = 'udp://192.0.2.1\nudp://192.0.2.7'
    current = with_dns(draft, {'single': 'upstream:' + entry_key(DRAFT['upstream'], 'local')})
    kind, projected, withheld = rule_changes(base, current, 'oxidns', {'library': True, 'rules': True})
    assert withheld == {'single'} and kind == 'none'
    assert 'single' not in (projected['oxidns'].get('rule_actions') or {})
    broken = deepcopy(base)
    broken['rule_configs'][0]['library_rule_id'] = 'missing'
    with pytest.raises(RuleSyncProjectionError):
        rule_changes(base, broken, 'oxidns', {'library': True})


@pytest.mark.parametrize('mode', ['mihomo', 'template', 'native'])
def test_agent_records_in_the_current_snapshot_do_not_affect_rule_changes(mode):
    from backend.agents.rule_sync_projection import rule_changes
    service = 'mihomo' if mode == 'mihomo' else 'oxidns'
    for name, edit in _edits():
        base = native(fixture()) if mode == 'native' else fixture()
        current = deepcopy(base)
        edit(current)
        with_agents = {**deepcopy(current), 'agents': [
            {'id': 'agent', 'profile_id': 'home', 'service_type': service, 'token': 'secret', 'host': '192.0.2.50',
             'oxidns_ha': {'enabled': True, 'role': 'primary'}, 'deployments': {'d': {'status': 'succeeded'}}}]}
        settings = {'library': True, 'rules': True}
        assert rule_changes(base, with_agents, service, settings) == rule_changes(base, current, service, settings), name
