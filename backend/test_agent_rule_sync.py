"""Rule reconciliation is device scoped and advances only confirmed receipts.

Real temporary ProfileRepository/AgentManager objects exercise persistence and
ownership. Native core calls and transport are replaced; no installed service
or external network is contacted.
"""
from copy import deepcopy
import hashlib
import json
import threading

import pytest
import requests
import yaml

from backend.agents import rule_sync, rule_sync_store as store
from backend.agents.deployment_bundle import (
    _pack, build_deployment_bundle, deployment_content_digests, deployment_content_fingerprint,
)
from backend.agents.manager import AgentManager
from backend.common import config as config_module
from backend.test_agent_deployments import env  # noqa: F401
from backend.test_agent_upgrade_auth import authenticated_app  # noqa: F401

REAL_START_LOOP = rule_sync.start_loop

@pytest.fixture(autouse=True)
def isolated_background(monkeypatch):
    # authenticated_app imports the production before_request hooks. Do not
    # start a persistent reconciliation thread from any HTTP test fixture.
    monkeypatch.setattr(rule_sync, 'start_loop', lambda: None)
    monkeypatch.setattr(requests.sessions.Session, 'request',
                        lambda *_a, **_k: pytest.fail('unexpected external network request'))
    monkeypatch.setattr(rule_sync, 'fetch_snapshot', lambda _manager, _agent, baseline: baseline['bundle'])
    previous = config_module._repository
    yield
    config_module._repository = previous
    config_module.reset_config_context()


def update_agent(manager, agent_id, **fields):
    manager._update_agents(lambda agents: next(item for item in agents if item['id'] == agent_id).update(deepcopy(fields)))
    return manager.get_agent_by_id(agent_id)


def artifact(agent, source, deployment_id='applied', *, reload_only=False):
    if agent['service_type'] == 'mihomo':
        document = {'mode': 'rule', 'dns': {'nameserver': ['192.0.2.1']},
                    'rule-providers': {'selected': {'type': 'http', 'behavior': 'domain', 'format': 'text',
                        'url': 'https://config.test/api/profiles/default/rule-library/content/lib-a',
                        'path': './ruleset/a.list'}}, 'rules': ['RULE-SET,selected,DIRECT', 'MATCH,DIRECT']}
        value = build_deployment_bundle(agent, source, yaml.safe_dump(document), deployment_id=deployment_id,
                                        base_url='https://config.test')
    else:
        value = build_deployment_bundle(agent, source, 'plugins: []', deployment_id=deployment_id)
    if reload_only:
        value = _pack({**value.manifest, 'reload_only': True}, store.bundle_files(value), value.config_version)
    return value


def enroll(manager, agent, source, bundle=None):
    bundle = bundle or artifact(agent, source)
    identity = bundle.manifest['deployment_id']
    assert manager.begin_deployment(agent['id'], identity)['success']
    manager._remember_deployment_content(agent['id'], bundle, 'https://config.test')
    store.capture(manager.repository, agent['id'], bundle, source, 'https://config.test')
    manager.record_deployment(agent['id'], identity, {'status': 'succeeded', 'success': True,
                                                    'config_version': bundle.config_version})
    manager.finish_deployment_preparation(agent['id'], identity)
    return manager.get_agent_by_id(agent['id']), bundle


@pytest.fixture
def sync_env(env):
    repository, manager, agent, _ = env
    repository.update_shared_transaction(lambda shared: shared['rule_library'].extend([
        {'id': 'lib-a', 'name': 'Selected', 'source_type': 'content', 'behavior': 'domain', 'content': 'old.test'},
        {'id': 'lib-unused', 'name': 'Unused', 'source_type': 'content', 'behavior': 'domain', 'content': 'unused.test'},
    ]))
    repository.update_profile_fields('default', {'rule_configs': [
        {'id': 'set-a', 'itemType': 'ruleset', 'library_rule_id': 'lib-a', 'policy': 'DIRECT', 'enabled': True},
        {'id': 'single', 'itemType': 'rule', 'rule_type': 'DOMAIN', 'value': 'old-inline.test', 'policy': 'DIRECT', 'enabled': True},
    ]})
    repository.update_profile_transaction('default', lambda profile: profile['oxidns'].update(
        direct_rulesets=['set-a'], direct_rules=['single']))
    agent = update_agent(manager, agent['id'], version='1.8.30-go')
    source, _ = repository.get_publish_snapshot('default')
    agent, bundle = enroll(manager, agent, source)
    rule_sync.configure(manager, agent['id'], {'library': True, 'rules': False})
    return repository, manager, manager.get_agent_by_id(agent['id']), source, bundle


def edit_library(repository, content='new.test', identity='lib-a'):
    repository.update_shared_transaction(lambda shared: next(
        item for item in shared['rule_library'] if item['id'] == identity).update(content=content))


def edit_inline(repository, value='new-inline.test', profile_id='default'):
    repository.update_profile_transaction(profile_id, lambda profile: next(
        item for item in profile['rule_configs'] if item.get('id') == 'single').update(value=value))


def tick(worker, now, agent_id, seconds=5):
    now[0] += seconds
    worker.run(agent_id)


def worker_for(manager, *, prepare=None):
    now = [100.0]
    worker = rule_sync.RuleSyncWorker(manager, clock=lambda: now[0],
        prepare=prepare or (lambda *_a: pytest.fail('content refresh must not generate a deployment')))
    return worker, now


def source_for(manager, agent_id):
    return store.load(manager.repository, manager.get_agent_by_id(agent_id))['source']


def test_capture_enrolls_only_confirmed_applied_publication_and_stays_private(env, monkeypatch):
    repository, manager, agent, source = env
    source['agents'] = [{'token': 'private-agent-token'}]
    source['system_config']['config_token'] = 'rendering-secret'
    bundle = build_deployment_bundle(agent, source, 'mode: rule', deployment_id='staged')
    requests_seen = []
    def transport(_agent, method, path, **kwargs):
        requests_seen.append((method, path))
        if path == 'capabilities':
            return {'deployment_protocols': [1], 'service_type': 'mihomo', 'config_path': 'config.yaml'}
        return {'status': 'staged', 'success': True}
    monkeypatch.setattr(manager, '_deployment_request', transport)
    assert manager.publish_deployment(agent['id'], bundle, activate=False, source_snapshot=source)['status'] == 'staged'
    assert store.load(repository, manager.get_agent_by_id(agent['id'])) is None
    manager.record_deployment(agent['id'], 'staged', {'status': 'succeeded', 'config_version': bundle.config_version})
    record = store.load(repository, manager.get_agent_by_id(agent['id']))
    assert 'agents' not in record['source']
    assert record['source']['system_config']['config_token'] == 'rendering-secret'
    source['system_config']['config_token'] = 'later-edit'
    assert record['source']['system_config']['config_token'] == 'rendering-secret'
    root = store.root_for(repository, agent['id'])
    for file in root.rglob('*'):
        assert file.stat().st_mode & 0o077 == 0
    exposed = json.dumps(rule_sync.status(manager, agent['id']))
    assert 'rendering-secret' not in exposed and 'private-agent-token' not in exposed
    assert requests_seen == [('get', 'capabilities'), ('post', 'deployments')]



def test_geo_resource_upload_keeps_content_receipt_and_comparison_metadata(env, monkeypatch):
    import io
    import tarfile
    from backend.agents.deployment_bundle import retarget_config_path

    repository, manager, agent, source = env
    source['system_config']['rule_download_proxy'] = 'published-proxy'
    original = build_deployment_bundle(agent, source, 'rules: ["GEOSITE,cn,DIRECT"]',
                                       deployment_id='geo-baseline')
    comparison = {**next(item for item in original.manifest['files'] if item['role'] == 'config'),
                  'sha256': 'a' * 64}
    bundle = _pack(original.manifest, store.bundle_files(original), original.config_version,
                   comparison_config=comparison)
    fetched, uploads = [], []
    def fetch(_url, config):
        fetched.append(deepcopy(config))
        return b'geo database'
    def transport(_agent, method, path, **kwargs):
        if path == 'capabilities':
            return {'deployment_protocols': [1], 'service_type': 'mihomo',
                    'config_path': 'managed.yaml', 'mihomo_resources': []}
        assert method == 'post' and path == 'deployments'
        uploads.append(kwargs['data'])
        return {'status': 'succeeded', 'success': True, 'config_version': bundle.config_version}
    monkeypatch.setattr('backend.agents.mihomo_resources._fetch_bytes', fetch)
    monkeypatch.setattr(manager, '_deployment_request', transport)
    assert manager.publish_deployment(agent['id'], bundle, source_snapshot=source)['status'] == 'succeeded'
    assert fetched == [source]
    with tarfile.open(fileobj=io.BytesIO(uploads[0]), mode='r:gz') as archive:
        assert archive.extractfile('files/GeoSite.dat').read() == b'geo database'
    agent = manager.get_agent_by_id(agent['id'])
    record = store.load(repository, agent)
    content_bundle = retarget_config_path(bundle, 'managed.yaml')
    assert record['bundle'].sha256 == content_bundle.sha256
    assert set(store.bundle_files(record['bundle'])) == {'managed.yaml'}
    assert record['comparison_config'] == {**comparison, 'path': 'managed.yaml'}
    comparison_manifest = deepcopy(content_bundle.manifest)
    comparison_manifest['files'] = [record['comparison_config']]
    assert agent['applied_content']['fingerprint'] == deployment_content_fingerprint(comparison_manifest)
    state = agent['deployments']['geo-baseline']
    assert state['source_sha256'] == content_bundle.sha256
    assert state['sha256'] != content_bundle.sha256


def test_receipt_rejects_rebound_target_changed_version_and_corrupted_archive(sync_env):
    repository, manager, agent, _, _ = sync_env
    for replacement in ({'profile_id': 'other'}, {'service_type': 'oxidns'}, {'config_version': 'foreign'}):
        assert store.load(repository, {**agent, **replacement}) is None
    record = store.load(repository, agent)
    archive = next(store.root_for(repository, agent['id']).rglob('bundle-' + record['sha256'] + '.tar.gz'))
    archive.write_bytes(b'corrupt')
    assert store.load(repository, agent) is None
    assert record['bundle'].archive != b'corrupt'


def test_adapted_main_uses_confirmed_live_version_without_changing_desired_fingerprint(sync_env):
    repository, manager, agent, source, original = sync_env
    original_entry = next(entry for entry in original.manifest['files'] if entry['role'] == 'config')
    files = store.bundle_files(original)
    config_path = original.manifest['config_path']
    live_document = yaml.safe_load(files[config_path][0])
    # The AIO/gateway activation step adapts the local listener and rewrites
    # YAML after upload. Its confirmed digest legitimately differs from the
    # server's original generated document.
    live_document['dns']['listen'] = '127.0.0.1:1053'
    files[config_path] = (yaml.safe_dump(live_document).encode(), 'config')
    actual_version = hashlib.sha256(files[config_path][0]).hexdigest()
    assert actual_version != original_entry['sha256']
    manager.record_deployment(agent['id'], original.manifest['deployment_id'], {
        'status': 'succeeded', 'config_version': actual_version})
    confirmed = manager.get_agent_by_id(agent['id'])
    receipt = store.load(repository, confirmed)
    assert receipt is not None
    assert receipt['source']['rule_library'][0]['content'] == 'old.test'
    assert receipt['bundle'].manifest == original.manifest
    # A native content refresh preserves the adapted live main for the next
    # isolated publication, while sync comparison uses the server main entry.
    for path, (_, role) in list(files.items()):
        if role == 'rule':
            files[path] = (b'new-native.test', role)
    manifest = deepcopy(original.manifest)
    manifest['files'] = [{'path': path, 'role': role, 'size': len(content),
                          'sha256': hashlib.sha256(content).hexdigest()}
                         for path, (content, role) in sorted(files.items())]
    live = _pack(manifest, files, actual_version)
    updated_source = deepcopy(source)
    updated_source['rule_library'][0]['content'] = 'new-native.test'
    assert store.advance_content(repository, confirmed, updated_source, live)
    current = manager.get_agent_by_id(agent['id'])
    refreshed = store.load(repository, current)
    assert store.bundle_files(refreshed['bundle'])[config_path] == files[config_path]
    assert refreshed['comparison_config'] == original_entry
    comparison = deepcopy(manifest)
    comparison['files'] = [original_entry if entry['role'] == 'config' else entry for entry in comparison['files']]
    expected = deployment_content_fingerprint(comparison)
    assert current['applied_content']['fingerprint'] == expected
    # Drift explanations compare per-file digests; they must advance together.
    assert current['applied_content']['files'] == deployment_content_digests(comparison)
    assert current['applied_content']['fingerprint'] != deployment_content_fingerprint(manifest)
    manager.record_deployment(agent['id'], original.manifest['deployment_id'], {'status': 'succeeded'})
    assert manager.get_agent_by_id(agent['id'])['applied_content']['fingerprint'] == expected


def test_failed_candidate_receipt_does_not_replace_last_applied_baseline(sync_env, monkeypatch):
    repository, manager, agent, source, old_bundle = sync_env
    current = deepcopy(source)
    current['rule_library'][0]['content'] = 'candidate.test'
    candidate = artifact(agent, current, 'failed-candidate')
    def transport(_agent, _method, path, **_kwargs):
        if path == 'capabilities':
            return {'deployment_protocols': [1], 'service_type': 'mihomo'}
        return {'success': False, 'status': 'failed'}
    monkeypatch.setattr(manager, '_deployment_request', transport)
    assert manager.publish_deployment(agent['id'], candidate, source_snapshot=current)['status'] == 'failed'
    applied = store.load(repository, manager.get_agent_by_id(agent['id']))
    assert applied['manifest']['deployment_id'] == old_bundle.manifest['deployment_id']
    assert applied['source']['rule_library'][0]['content'] == 'old.test'


def test_late_native_receipt_cannot_delete_concurrent_manual_publication_receipt(sync_env):
    repository, manager, agent, source, original = sync_env
    manual_source = deepcopy(source)
    manual_source['rule_library'][0]['content'] = 'manual-published.test'
    manual = artifact(agent, manual_source, 'manual-uploading')
    assert manager.begin_deployment(agent['id'], 'manual-uploading')['success']
    try:
        manager._remember_deployment_content(agent['id'], manual, 'https://config.test')
        store.capture(repository, agent['id'], manual, manual_source, 'https://config.test')
        manager.record_deployment(agent['id'], 'manual-uploading', {'status': 'uploading', 'submitted': True})
        # The native request finished just before this manual upload reserved
        # the device. Its older receipt must not prune the accepted candidate.
        store.advance_content(repository, agent, source, original)
        manager.record_deployment(agent['id'], 'manual-uploading', {
            'success': True, 'status': 'succeeded', 'config_version': manual.config_version})
        receipt = store.load(repository, manager.get_agent_by_id(agent['id']))
        assert receipt is not None
        assert receipt['source']['rule_library'][0]['content'] == 'manual-published.test'
    finally:
        manager.finish_deployment_preparation(agent['id'], 'manual-uploading')


def test_reregistration_preserves_independent_device_scope_settings(sync_env):
    _, manager, agent, _, _ = sync_env
    rule_sync.configure(manager, agent['id'], {'library': False, 'rules': True})
    registered = manager.register_agent({'id': agent['id'], 'name': agent['name'], 'host': agent['host'],
        'port': agent['port'], 'service_type': agent['service_type'], 'version': '1.8.30-go'},
        agent['token'])
    assert registered['id'] == agent['id']
    assert rule_sync.settings_for(manager.get_agent_by_id(agent['id'])) == {'library': False, 'rules': True}


def test_route_scopes_are_independent_authenticated_and_ignore_global_profile(authenticated_app):
    client, manager, (agent, other), admin, _ = authenticated_app
    path = '/api/agents/' + agent['id'] + '/rule-sync'
    for headers in ({}, {'Authorization': 'Bearer ' + agent['token']}):
        assert client.get(path, headers=headers).status_code == 401
        assert client.put(path, json={'library': True, 'rules': False}, headers=headers).status_code == 401
    assert client.get(path, headers=admin).get_json()['settings'] == {'library': False, 'rules': False}
    response = client.put(path, json={'library': True, 'rules': False},
                          headers={**admin, 'X-ConfigFlow-Profile': 'does-not-exist'})
    assert response.status_code == 200
    assert response.headers['Cache-Control'] == 'no-store'
    assert response.get_json()['settings'] == {'library': True, 'rules': False}
    assert rule_sync.settings_for(manager.get_agent_by_id(other['id'])) == {'library': False, 'rules': False}
    response = client.put(path, json={'library': False, 'rules': True}, headers=admin)
    assert response.get_json()['settings'] == {'library': False, 'rules': True}
    assert 'token' not in response.get_data(as_text=True)


@pytest.mark.parametrize('payload', [None, [], {'library': True}, {'library': True, 'rules': 1},
                                     {'library': 'yes', 'rules': False}, {'library': True, 'rules': False, 'profile_id': 'other'}])
def test_route_rejects_invalid_scope_payloads_without_mutation(authenticated_app, payload):
    client, manager, (agent, _), admin, _ = authenticated_app
    before = deepcopy(manager.get_agent_by_id(agent['id']))
    response = client.put('/api/agents/' + agent['id'] + '/rule-sync', json=payload, headers=admin)
    assert response.status_code == 400
    assert manager.get_agent_by_id(agent['id']) == before


def test_unused_library_and_disabled_inline_scope_do_not_publish(sync_env, monkeypatch):
    repository, manager, agent, _, _ = sync_env
    edit_library(repository, 'unused-new.test', 'lib-unused')
    edit_inline(repository)
    monkeypatch.setattr(manager, 'publish_deployment', lambda *_a, **_k: pytest.fail('unselected change was published'))
    worker, now = worker_for(manager)
    worker.run(agent['id'])
    tick(worker, now, agent['id'], 10)
    assert rule_sync.status(manager, agent['id'])['state']['status'] == 'idle'
    assert source_for(manager, agent['id'])['rule_configs'][1]['value'] == 'old-inline.test'


def test_native_content_refresh_debounces_edits_and_never_pushes_whole_config(sync_env, monkeypatch):
    repository, manager, agent, source, _ = sync_env
    from backend.agents import mihomo_panel
    calls = []
    monkeypatch.setattr(mihomo_panel, 'call', lambda *args: calls.append(args) or {'success': True})
    monkeypatch.setattr(manager, 'publish_deployment', lambda *_a, **_k: pytest.fail('whole config pushed'))
    worker, now = worker_for(manager)
    edit_library(repository, 'first.test')
    worker.run(agent['id'])
    tick(worker, now, agent['id'], 4)
    assert calls == []
    edit_library(repository, 'last.test')
    tick(worker, now, agent['id'], 1)
    assert calls == []
    tick(worker, now, agent['id'], 5)
    assert [entry[1:] for entry in calls] == [(agent['id'], 'providers/rule', 'PUT', {'name': 'selected'})]
    captured = source_for(manager, agent['id'])
    assert captured['rule_library'][0]['content'] == 'last.test'
    assert captured['mihomo'] == source['mihomo']
    assert rule_sync.status(manager, agent['id'])['state']['status'] == 'succeeded'
    tick(worker, now, agent['id'], 10)
    assert len(calls) == 1


def test_offline_device_keeps_change_pending_until_it_reconnects(sync_env, monkeypatch):
    repository, manager, agent, _, _ = sync_env
    from backend.agents import mihomo_panel
    calls = []
    monkeypatch.setattr(mihomo_panel, 'call', lambda *args: calls.append(args) or {'success': True})
    edit_library(repository)
    update_agent(manager, agent['id'], status='offline')
    worker, now = worker_for(manager)
    worker.run(agent['id'])
    tick(worker, now, agent['id'], 20)
    assert calls == []
    assert rule_sync.status(manager, agent['id'])['state']['status'] == 'pending'
    update_agent(manager, agent['id'], status='online')
    tick(worker, now, agent['id'])
    tick(worker, now, agent['id'])
    assert len(calls) == 1


def test_newer_edit_during_native_download_is_not_acknowledged(sync_env, monkeypatch):
    repository, manager, agent, _, _ = sync_env
    from backend.agents import mihomo_panel
    monkeypatch.setattr(mihomo_panel, 'call', lambda *_a: edit_library(repository, 'raced.test'))
    edit_library(repository)
    worker, now = worker_for(manager)
    worker.run(agent['id'])
    tick(worker, now, agent['id'])
    assert source_for(manager, agent['id'])['rule_library'][0]['content'] == 'old.test'
    assert rule_sync.status(manager, agent['id'])['state']['status'] == 'pending'


def test_native_failure_preserves_baseline_and_retries_after_backoff(sync_env, monkeypatch):
    repository, manager, agent, _, _ = sync_env
    from backend.agents import mihomo_panel
    calls = []
    def failed(*args):
        calls.append(args)
        raise requests.Timeout('private credential must not escape')
    monkeypatch.setattr(mihomo_panel, 'call', failed)
    edit_library(repository)
    worker, now = worker_for(manager)
    worker.run(agent['id'])
    tick(worker, now, agent['id'])
    assert source_for(manager, agent['id'])['rule_library'][0]['content'] == 'old.test'
    state = rule_sync.status(manager, agent['id'])['state']
    assert state['status'] == 'failed' and 'credential' not in state['message']
    tick(worker, now, agent['id'], 59)
    assert len(calls) == 1


def test_successful_native_call_without_verified_snapshot_does_not_advance_baseline(sync_env, monkeypatch):
    repository, manager, agent, _, _ = sync_env
    from backend.agents import mihomo_panel
    monkeypatch.setattr(mihomo_panel, 'call', lambda *_a: {'success': True})
    def unavailable(*_args):
        raise ValueError('device snapshot unavailable')
    monkeypatch.setattr(rule_sync, 'fetch_snapshot', unavailable)
    edit_library(repository)
    worker, now = worker_for(manager)
    worker.run(agent['id'])
    tick(worker, now, agent['id'])
    assert rule_sync.status(manager, agent['id'])['state']['status'] == 'failed'
    assert source_for(manager, agent['id'])['rule_library'][0]['content'] == 'old.test'
    assert manager.get_agent_by_id(agent['id'])['applied_content'] == agent['applied_content']


def test_structure_release_uses_captured_nonrule_source_and_reload_only(sync_env, monkeypatch):
    repository, manager, agent, source, _ = sync_env
    rule_sync.configure(manager, agent['id'], {'library': False, 'rules': True})
    edit_inline(repository)
    edit_library(repository, 'unselected.test')
    repository.update_profile_transaction('default', lambda profile: profile['mihomo'].update(custom_config='port: 9999'))
    candidates, uploads = [], []
    def prepare(target, projected, baseline, scopes, identity):
        candidates.append(deepcopy(projected))
        assert baseline['source']['mihomo'] == source['mihomo']
        return artifact(target, projected, identity, reload_only=True)
    def transport(target, method, path, **kwargs):
        if path == 'capabilities':
            return {'deployment_protocols': [1], 'reload_only_protocols': [1], 'service_type': 'mihomo'}
        uploads.append(kwargs['data'])
        return {'success': True, 'status': 'succeeded'}
    monkeypatch.setattr(manager, '_deployment_request', transport)
    worker, now = worker_for(manager, prepare=prepare)
    worker.run(agent['id'])
    tick(worker, now, agent['id'])
    assert len(candidates) == len(uploads) == 1
    assert candidates[0]['rule_configs'][1]['value'] == 'new-inline.test'
    assert candidates[0]['rule_library'][0]['content'] == 'old.test'
    assert candidates[0]['mihomo'] == source['mihomo']
    record = store.load(repository, manager.get_agent_by_id(agent['id']))
    assert record['manifest']['reload_only'] is True
    assert record['source']['rule_configs'][1]['value'] == 'new-inline.test'
    assert 'agents' not in record['source']


def test_confirmed_publication_does_not_force_a_republishing_refresh(sync_env, monkeypatch):
    repository, manager, agent, _, _ = sync_env
    from backend.converters.mihomo import generate_mihomo_config
    def inline_only(profile):
        # Mihomo cannot refresh a non-HTTP provider natively.
        profile['rule_configs'] = [item for item in profile['rule_configs'] if item['itemType'] == 'rule']
        profile['oxidns']['direct_rulesets'] = []
        profile['mihomo']['custom_config'] = ('mode: rule\nrule-providers:\n  lan:\n    type: inline\n'
                                              '    behavior: domain\n    payload: [nas.lan]\n')
    repository.update_profile_transaction('default', inline_only)
    source, _ = repository.get_publish_snapshot('default')
    content = generate_mihomo_config(source, base_url='https://config.test', sync_lan_hosts=True,
                                     preflight_providers=False)
    agent, _ = enroll(manager, agent, source, build_deployment_bundle(
        agent, source, content, deployment_id='manual', base_url='https://config.test'))
    rule_sync.configure(manager, agent['id'], {'library': True, 'rules': True})
    edit_inline(repository)
    uploads = []
    def transport(_agent, method, path, **kwargs):
        if path == 'capabilities':
            return {'deployment_protocols': [1], 'reload_only_protocols': [1], 'service_type': 'mihomo'}
        if method == 'post':
            uploads.append(kwargs['headers'])
            return {'success': True, 'status': 'activating'}
        # The Agent confirms activation on a later status poll.
        deployment = manager.get_agent_by_id(agent['id'])['latest_deployment']
        return {'success': True, 'status': 'succeeded', 'config_version': deployment['config_version']}
    monkeypatch.setattr(manager, '_deployment_request', transport)
    monkeypatch.setattr('backend.agents.mihomo_panel.call',
                        lambda *_a, **_k: pytest.fail('nothing changed after the publication'))
    now = [100.0]
    worker = rule_sync.RuleSyncWorker(manager, clock=lambda: now[0])
    for _ in range(12):
        tick(worker, now, agent['id'])
    assert len(uploads) == 1
    assert rule_sync.status(manager, agent['id'])['state']['status'] == 'succeeded'
    assert source_for(manager, agent['id'])['rule_configs'][0]['value'] == 'new-inline.test'


def test_unknown_structural_upload_is_reconciled_without_reupload(sync_env, monkeypatch):
    repository, manager, agent, _, old_bundle = sync_env
    rule_sync.configure(manager, agent['id'], {'library': False, 'rules': True})
    edit_inline(repository)
    uploads, prepared = [], []
    def prepare(target, projected, baseline, scopes, identity):
        result = artifact(target, projected, identity, reload_only=True)
        prepared.append(result)
        return result
    def transport(_agent, method, path, **kwargs):
        if path == 'capabilities':
            return {'deployment_protocols': [1], 'reload_only_protocols': [1], 'service_type': 'mihomo'}
        if method == 'post':
            uploads.append(path)
            raise requests.Timeout('upload receipt lost')
        return {'success': True, 'status': 'succeeded', 'config_version': prepared[0].config_version}
    monkeypatch.setattr(manager, '_deployment_request', transport)
    worker, now = worker_for(manager, prepare=prepare)
    worker.run(agent['id'])
    tick(worker, now, agent['id'])
    assert manager.get_agent_by_id(agent['id'])['latest_deployment']['status'] == 'unknown'
    assert store.load(repository, manager.get_agent_by_id(agent['id']))['manifest']['deployment_id'] == old_bundle.manifest['deployment_id']
    assert rule_sync.status(manager, agent['id'])['state']['status'] != 'succeeded'
    tick(worker, now, agent['id'])
    tick(worker, now, agent['id'])
    assert uploads == ['deployments']
    assert len(prepared) == 1
    assert source_for(manager, agent['id'])['rule_configs'][1]['value'] == 'new-inline.test'


@pytest.mark.parametrize('status', ['unknown', 'rollback_failed', 'verifying'])
def test_unfinished_manual_publication_prevents_automatic_operations(sync_env, monkeypatch, status):
    repository, manager, agent, _, _ = sync_env
    edit_library(repository)
    manager.begin_deployment(agent['id'], 'manual')
    manager.record_deployment(agent['id'], 'manual', {'status': status, 'submitted': True})
    manager.finish_deployment_preparation(agent['id'], 'manual')
    monkeypatch.setattr(manager, '_deployment_request', lambda *_a, **_k: {'status': status})
    worker, now = worker_for(manager)
    worker.run(agent['id'])
    tick(worker, now, agent['id'], 10)
    assert rule_sync.status(manager, agent['id'])['state']['status'] == 'pending'
    assert source_for(manager, agent['id'])['rule_library'][0]['content'] == 'old.test'


def test_disable_scope_during_preparation_cancels_before_upload(sync_env, monkeypatch):
    repository, manager, agent, _, _ = sync_env
    rule_sync.configure(manager, agent['id'], {'library': False, 'rules': True})
    edit_inline(repository)
    def prepare(target, projected, baseline, scopes, identity):
        rule_sync.configure(manager, agent['id'], {'library': False, 'rules': False})
        return artifact(target, projected, identity, reload_only=True)
    monkeypatch.setattr(manager, 'publish_deployment', lambda *_a, **_k: pytest.fail('disabled job uploaded'))
    worker, now = worker_for(manager, prepare=prepare)
    worker.run(agent['id'])
    tick(worker, now, agent['id'])
    assert rule_sync.status(manager, agent['id'])['state']['status'] == 'disabled'
    assert manager.get_agent_by_id(agent['id'])['latest_deployment']['status'] == 'failed'
    assert source_for(manager, agent['id'])['rule_configs'][1]['value'] == 'old-inline.test'


def test_other_profile_edit_does_not_change_device_rules(sync_env):
    repository, manager, agent, _, _ = sync_env
    repository.create_profile({'id': 'other'}, clone_from='default')
    rule_sync.configure(manager, agent['id'], {'library': False, 'rules': True})
    edit_inline(repository, profile_id='other')
    worker, now = worker_for(manager)
    worker.run(agent['id'])
    tick(worker, now, agent['id'])
    assert rule_sync.status(manager, agent['id'])['state']['status'] == 'idle'


def test_rebind_invalidates_old_profile_baseline(sync_env):
    repository, manager, agent, _, _ = sync_env
    repository.create_profile({'id': 'other'}, clone_from='default')
    manager.update_agent(agent['id'], {'profile_id': 'other'})
    worker, _ = worker_for(manager)
    worker.run(agent['id'])
    assert rule_sync.status(manager, agent['id'])['state']['status'] == 'blocked'
    assert rule_sync.status(manager, agent['id'])['baseline_ready'] is False


def test_concurrent_rule_workers_use_same_device_file_lock(sync_env, monkeypatch):
    repository, manager, agent, _, _ = sync_env
    from backend.agents import mihomo_panel
    entered, release = threading.Event(), threading.Event()
    calls = []
    def refresh(*args):
        calls.append(args)
        entered.set()
        assert release.wait(5)
    monkeypatch.setattr(mihomo_panel, 'call', refresh)
    edit_library(repository)
    worker, now = worker_for(manager)
    worker.run(agent['id'])
    now[0] += 5
    thread = threading.Thread(target=worker.run, args=(agent['id'],))
    thread.start()
    try:
        assert entered.wait(5)
        restarted = AgentManager(repository)
        second, _ = worker_for(restarted)
        second.run(agent['id'])
        assert len(calls) == 1
    finally:
        release.set()
        thread.join(5)
    assert not thread.is_alive()
    assert rule_sync.status(manager, agent['id'])['state']['status'] == 'succeeded'


def as_oxidns(sync_env):
    repository, manager, agent, source, _ = sync_env
    agent = update_agent(manager, agent['id'], service_type='oxidns')
    agent, bundle = enroll(manager, agent, source, artifact(agent, source, 'ox-applied'))
    return repository, manager, agent, source, bundle


def native_task(status='running', identity='native-one'):
    return {'success': True, 'task': {'id': '' if status == 'idle' else identity,
        'status': status, 'phase': 'downloading' if status == 'running' else 'done',
        'downloaded': int(status == 'succeeded'), 'download_total': 1,
        'reloaded': int(status == 'succeeded'), 'reload_total': 1}}


def test_native_accepted_task_survives_process_restart_without_reposting(sync_env, monkeypatch):
    repository, manager, agent, _, _ = as_oxidns(sync_env)
    from backend.agents import oxidns_rule_update
    calls, responses = [], [native_task('idle'), native_task(), native_task('succeeded')]
    def call(target, identity, method):
        calls.append(method)
        return responses.pop(0), 202 if method == 'POST' else 200
    monkeypatch.setattr(oxidns_rule_update, 'call', call)
    edit_library(repository)
    worker, now = worker_for(manager)
    worker.run(agent['id'])
    tick(worker, now, agent['id'])
    assert calls == ['GET', 'POST']
    assert source_for(manager, agent['id'])['rule_library'][0]['content'] == 'old.test'
    assert rule_sync.status(manager, agent['id'])['state']['status'] == 'running'
    restarted = AgentManager(repository)
    worker, now = worker_for(restarted)
    worker.run(agent['id'])
    assert calls == ['GET', 'POST', 'GET']
    assert source_for(restarted, agent['id'])['rule_library'][0]['content'] == 'new.test'
    assert rule_sync.status(restarted, agent['id'])['state']['status'] == 'succeeded'


def test_native_failed_download_does_not_acknowledge_new_source(sync_env, monkeypatch):
    repository, manager, agent, _, _ = as_oxidns(sync_env)
    from backend.agents import oxidns_rule_update
    responses = [native_task('idle'), native_task(), native_task('failed')]
    responses[-1]['task'].update(code='DOWNLOAD_FAILED', message='下载失败，未执行热重载')
    monkeypatch.setattr(oxidns_rule_update, 'call', lambda *_a: (responses.pop(0), 200))
    edit_library(repository)
    worker, now = worker_for(manager)
    worker.run(agent['id'])
    tick(worker, now, agent['id'])
    tick(worker, now, agent['id'])
    assert source_for(manager, agent['id'])['rule_library'][0]['content'] == 'old.test'
    status = rule_sync.status(manager, agent['id'])['state']
    assert status['status'] == 'failed' and '未执行热重载' in status['message']


@pytest.mark.parametrize('completion', ['succeeded', 'failed'])
def test_disabling_library_mid_native_task_reconciles_without_another_download(sync_env, monkeypatch, completion):
    repository, manager, agent, _, _ = as_oxidns(sync_env)
    from backend.agents import oxidns_rule_update
    calls = []
    responses = [native_task('idle'), native_task(), native_task(completion)]
    def call(*args):
        calls.append(args[-1])
        assert responses, 'disabled library must not issue more native requests'
        return responses.pop(0), 200
    monkeypatch.setattr(oxidns_rule_update, 'call', call)
    edit_library(repository)
    worker, now = worker_for(manager)
    worker.run(agent['id'])
    tick(worker, now, agent['id'])
    rule_sync.configure(manager, agent['id'], {'library': False, 'rules': True})
    for _ in range(4):
        tick(worker, now, agent['id'])
    assert calls == ['GET', 'POST', 'GET']
    assert rule_sync.status(manager, agent['id'])['state']['status'] == 'idle'
    assert source_for(manager, agent['id'])['rule_library'][0]['content'] == 'old.test'


def test_reverted_source_after_accepted_download_is_refreshed_again(sync_env, monkeypatch):
    repository, manager, agent, _, _ = as_oxidns(sync_env)
    from backend.agents import oxidns_rule_update
    calls = []
    responses = [native_task('idle'), native_task(), native_task('succeeded'),
                 native_task('succeeded'), native_task(identity='native-two'),
                 native_task('succeeded', identity='native-two')]
    def call(*args):
        calls.append(args[-1])
        return responses.pop(0), 200
    monkeypatch.setattr(oxidns_rule_update, 'call', call)
    edit_library(repository)
    worker, now = worker_for(manager)
    worker.run(agent['id'])
    tick(worker, now, agent['id'])
    edit_library(repository, 'old.test')
    tick(worker, now, agent['id'])
    assert rule_sync.status(manager, agent['id'])['state']['status'] != 'succeeded'
    tick(worker, now, agent['id'])
    tick(worker, now, agent['id'])
    assert calls.count('POST') == 2
    assert rule_sync.status(manager, agent['id'])['state']['status'] == 'succeeded'


def test_temporary_status_failure_keeps_accepted_task_for_later_reconciliation(sync_env, monkeypatch):
    repository, manager, agent, _, _ = as_oxidns(sync_env)
    from backend.agents import oxidns_rule_update
    calls = []
    responses = [native_task('idle'), native_task(), requests.Timeout('disconnected'), native_task('succeeded')]
    def call(*args):
        calls.append(args[-1])
        result = responses.pop(0)
        if isinstance(result, Exception):
            raise result
        return result, 200
    monkeypatch.setattr(oxidns_rule_update, 'call', call)
    edit_library(repository)
    worker, now = worker_for(manager)
    worker.run(agent['id'])
    tick(worker, now, agent['id'])
    tick(worker, now, agent['id'])
    restarted = AgentManager(repository)
    worker = rule_sync.RuleSyncWorker(restarted, clock=lambda: now[0])
    tick(worker, now, agent['id'], 65)
    assert calls == ['GET', 'POST', 'GET', 'GET']
    assert source_for(manager, agent['id'])['rule_library'][0]['content'] == 'new.test'
    assert rule_sync.status(manager, agent['id'])['state']['status'] == 'succeeded'


def test_same_config_version_manual_publication_cannot_be_overwritten_by_old_refresh(sync_env, monkeypatch):
    repository, manager, agent, source, old_bundle = sync_env
    from backend.agents import mihomo_panel
    edit_library(repository, 'automatic.test')
    manual_source = deepcopy(source)
    manual_source['rule_library'][0]['content'] = 'manual.test'
    manual_bundle = artifact(agent, manual_source, 'manual-new')
    assert manual_bundle.config_version == old_bundle.config_version
    def manual_publication(*_args):
        enroll(manager, manager.get_agent_by_id(agent['id']), manual_source, manual_bundle)
    monkeypatch.setattr(mihomo_panel, 'call', manual_publication)
    worker, now = worker_for(manager)
    worker.run(agent['id'])
    tick(worker, now, agent['id'])
    current = manager.get_agent_by_id(agent['id'])
    assert current['applied_content']['deployment_id'] == 'manual-new'
    assert source_for(manager, agent['id'])['rule_library'][0]['content'] == 'manual.test'
    assert rule_sync.status(manager, agent['id'])['state']['status'] != 'succeeded'


def test_native_refresh_then_inline_only_publish_keeps_updated_runtime_rule_bytes(sync_env, monkeypatch):
    repository, manager, agent, source, _ = sync_env
    from backend.agents import mihomo_panel
    from backend.converters.mihomo import generate_mihomo_config
    content = generate_mihomo_config(source, base_url='https://config.test',
                                    sync_lan_hosts=True, preflight_providers=False, region_probe=False)
    original = build_deployment_bundle(agent, source, content, base_url='https://config.test',
                                       deployment_id='real-generated-baseline')
    agent, original = enroll(manager, agent, source, original)
    files = store.bundle_files(original)
    rule_paths = [path for path, (_, role) in files.items() if role == 'rule']
    assert rule_paths
    for path in rule_paths:
        files[path] = (b'new-native.test', 'rule')
    manifest = deepcopy(original.manifest)
    manifest['files'] = [{'path': path, 'role': role, 'size': len(value),
                          'sha256': hashlib.sha256(value).hexdigest()}
                         for path, (value, role) in sorted(files.items())]
    live = _pack(manifest, files, original.config_version)
    snapshot_calls = []
    def snapshot(_manager, target, baseline):
        snapshot_calls.append(target['id'])
        assert baseline['bundle'].manifest['deployment_id'] == original.manifest['deployment_id']
        return live
    monkeypatch.setattr(rule_sync, 'fetch_snapshot', snapshot)
    monkeypatch.setattr(mihomo_panel, 'call', lambda *_a: {'success': True})
    edit_library(repository, 'new-native.test')
    worker, now = worker_for(manager)
    worker.run(agent['id'])
    tick(worker, now, agent['id'])
    refreshed = manager.get_agent_by_id(agent['id'])
    assert refreshed['applied_content']['fingerprint'] == deployment_content_fingerprint(live.manifest)
    receipt = store.load(repository, refreshed)
    assert {path: store.bundle_files(receipt['bundle'])[path] for path in rule_paths} == {
        path: (b'new-native.test', 'rule') for path in rule_paths}
    # A subsequent inline-only publication must pin the just-refreshed files,
    # even though it intentionally ignores future rule-library edits.
    rule_sync.configure(manager, agent['id'], {'library': False, 'rules': True})
    edit_inline(repository)
    edit_library(repository, 'unpublished-future.test')
    published = []
    def transport(_agent, method, path, **kwargs):
        if path == 'capabilities':
            return {'deployment_protocols': [1], 'reload_only_protocols': [1], 'service_type': 'mihomo'}
        published.append(kwargs['data'])
        return {'success': True, 'status': 'succeeded'}
    monkeypatch.setattr(manager, '_deployment_request', transport)
    worker = rule_sync.RuleSyncWorker(manager, clock=lambda: now[0])
    tick(worker, now, agent['id'])
    tick(worker, now, agent['id'])
    assert len(published) == 1 and len(snapshot_calls) == 2
    receipt = store.load(repository, manager.get_agent_by_id(agent['id']))
    assert receipt['manifest']['reload_only'] is True
    final = store.bundle_files(receipt['bundle'])
    assert {path: final[path] for path in rule_paths} == {path: (b'new-native.test', 'rule') for path in rule_paths}
    assert receipt['source']['rule_library'][0]['content'] == 'new-native.test'
    assert receipt['source']['rule_configs'][1]['value'] == 'new-inline.test'


def test_interrupted_refresh_without_debounce_timestamp_recovers(sync_env, monkeypatch):
    repository, manager, agent, _, _ = sync_env
    from backend.agents import mihomo_panel
    calls = []
    monkeypatch.setattr(mihomo_panel, 'call', lambda *args: calls.append(args) or {'success': True})
    edit_library(repository)
    worker, now = worker_for(manager)
    worker.run(agent['id'])
    state = manager.get_agent_by_id(agent['id'])['rule_sync']['state']
    state.pop('noticed_at')
    state['status'] = 'running'
    update_agent(manager, agent['id'], rule_sync={'settings': {'library': True, 'rules': False}, 'state': state})
    tick(worker, now, agent['id'])
    assert len(calls) == 1
    assert rule_sync.status(manager, agent['id'])['state']['status'] == 'succeeded'


def test_scheduler_rotates_devices_while_one_worker_remains_pending(sync_env, monkeypatch):
    _, manager, agent, _, _ = sync_env
    identities = [agent['id']]
    for index in range(1, 5):
        registered = manager.register_agent({'name': 'device-' + str(index), 'host': 'agent-' + str(index) + '.test',
            'port': 8080, 'service_type': 'mihomo', 'version': '1.8.30-go'})
        identities.append(registered['id'])
        rule_sync.configure(manager, registered['id'], {'library': True, 'rules': False})
    thread_targets, submitted = [], []
    class FakeThread:
        def __init__(self, *, target, **_kwargs):
            thread_targets.append(target)
        def start(self):
            pass
    class Future:
        def __init__(self, identity):
            self.identity = identity
        def done(self):
            return self.identity != identities[0]
    class Pool:
        def __init__(self, max_workers, **_kwargs):
            assert max_workers == 2
        def submit(self, _function, identity):
            submitted.append(identity)
            return Future(identity)
    class Finished(BaseException):
        pass
    rounds = [0]
    def sleep(_seconds):
        rounds[0] += 1
        if rounds[0] == 5:
            raise Finished()
    monkeypatch.setattr(rule_sync, '_started_pid', None)
    monkeypatch.setattr(rule_sync.threading, 'Thread', FakeThread)
    monkeypatch.setattr(rule_sync, 'ThreadPoolExecutor', Pool)
    monkeypatch.setattr(rule_sync.time, 'sleep', sleep)
    monkeypatch.setattr('backend.common.agent_manager.get_agent_manager', lambda: manager)
    REAL_START_LOOP()
    assert len(thread_targets) == 1
    with pytest.raises(Finished):
        thread_targets[0]()
    assert submitted.count(identities[0]) == 1
    assert set(submitted) == set(identities)


def test_content_refresh_updates_only_changed_mihomo_providers(sync_env, monkeypatch):
    repository, manager, agent, source, _ = sync_env
    from backend.agents import mihomo_panel
    document = {'mode': 'rule', 'rule-providers': {
        name: {'type': 'http', 'behavior': 'domain', 'format': 'text', 'path': f'./ruleset/{name}.list',
               'url': f'https://config.test/api/profiles/default/rule-library/content/{identity}'}
        for name, identity in (('selected', 'lib-a'), ('other', 'lib-unused'))},
        'rules': ['RULE-SET,selected,DIRECT', 'RULE-SET,other,DIRECT', 'MATCH,DIRECT']}
    bundle = build_deployment_bundle(agent, source, yaml.safe_dump(document), deployment_id='two-providers',
                                     base_url='https://config.test')
    agent, _ = enroll(manager, agent, source, bundle)
    calls = []
    monkeypatch.setattr(mihomo_panel, 'call', lambda *args: calls.append(args) or {'success': True})
    edit_library(repository, 'new.test')
    worker, now = worker_for(manager)
    worker.run(agent['id'])
    tick(worker, now, agent['id'])
    assert [entry[4] for entry in calls] == [{'name': 'selected'}]
    assert rule_sync.status(manager, agent['id'])['state']['status'] == 'succeeded'


def targeted_oxidns(sync_env, version):
    repository, manager, agent, source, bundle = as_oxidns(sync_env)
    repository.update_system_transaction(lambda system: system.setdefault('system_config', {}).update(
        rule_proxy_token='proxy-token'))
    source, _ = repository.get_publish_snapshot('default')
    agent = update_agent(manager, agent['id'], version=version)
    agent, bundle = enroll(manager, agent, source, artifact(agent, source, 'ox-targeted'))
    return repository, manager, agent


def test_native_content_refresh_sends_only_changed_files_to_new_agents(sync_env, monkeypatch):
    repository, manager, agent = targeted_oxidns(sync_env, '1.8.35-go')
    from backend.agents import oxidns_rule_update
    from backend.converters.oxidns import _rule_path
    calls, responses = [], [native_task('idle'), native_task(), native_task('succeeded')]
    def call(_target, _identity, method, paths=None):
        calls.append((method, paths))
        return responses.pop(0), 202 if method == 'POST' else 200
    monkeypatch.setattr(oxidns_rule_update, 'call', call)
    edit_library(repository)
    worker, now = worker_for(manager)
    worker.run(agent['id'])
    tick(worker, now, agent['id'])
    expected = _rule_path({'id': 'set-a'}, 'domain').removeprefix('./')
    assert calls == [('GET', None), ('POST', [expected])]
    assert '1 个有变化的规则文件' in rule_sync.status(manager, agent['id'])['state']['message']
    tick(worker, now, agent['id'])
    assert source_for(manager, agent['id'])['rule_library'][0]['content'] == 'new.test'
    assert rule_sync.status(manager, agent['id'])['state']['status'] == 'succeeded'


@pytest.mark.parametrize('version', ['1.8.34-go', '1.8.35-go'])
def test_native_content_refresh_falls_back_to_full_update(sync_env, monkeypatch, version):
    repository, manager, agent = targeted_oxidns(sync_env, version)
    from backend.agents import oxidns_rule_update
    from backend.agents.oxidns_monitor import MonitorError
    calls, responses = [], [native_task('idle'), native_task()]
    def call(_target, _identity, method, paths=None):
        calls.append((method, paths))
        if paths:
            raise MonitorError('RULE_UPDATE_TARGET_UNKNOWN', 409)
        return responses.pop(0), 202 if method == 'POST' else 200
    monkeypatch.setattr(oxidns_rule_update, 'call', call)
    edit_library(repository)
    worker, now = worker_for(manager)
    worker.run(agent['id'])
    tick(worker, now, agent['id'])
    assert calls[-1] == ('POST', None)
    assert len([entry for entry in calls if entry[0] == 'POST']) == (2 if version == '1.8.35-go' else 1)
    assert rule_sync.status(manager, agent['id'])['state']['status'] == 'running'


def test_unpublished_pinned_upstream_is_withheld_and_reported(sync_env, monkeypatch):
    repository, manager, agent, _, _ = as_oxidns(sync_env)
    rule_sync.configure(manager, agent['id'], {'library': False, 'rules': True})
    draft = {'action': 'upstream', 'source': 'local', 'upstream': {'addr': 'udp://192.0.2.77'}}
    repository.update_profile_transaction('default', lambda profile: profile['oxidns'].update(
        local_dns='udp://192.0.2.77', direct_rules=[], rule_actions={'single': draft}))
    monkeypatch.setattr(manager, 'publish_deployment', lambda *_a, **_k: pytest.fail('DNS draft was published'))
    worker, now = worker_for(manager)
    worker.run(agent['id'])
    tick(worker, now, agent['id'], 10)
    state = rule_sync.status(manager, agent['id'])['state']
    assert state['status'] == 'idle' and state['message'].endswith('指定上游需手动发布')
    assert 'single' not in (source_for(manager, agent['id'])['oxidns'].get('rule_actions') or {})


def test_pinned_withheld_reports_only_auto_synced_unpublished_pins(sync_env):
    repository, manager, agent, _, _ = as_oxidns(sync_env)
    draft = {'action': 'upstream', 'source': 'local', 'upstream': {'addr': 'udp://192.0.2.77'}}
    repository.update_profile_transaction('default', lambda profile: profile['oxidns'].update(
        local_dns='udp://192.0.2.77', direct_rules=[], rule_actions={'single': draft}))
    # Only the library scope syncs automatically, so the inline rule waits for a manual publish anyway.
    assert rule_sync.pinned_withheld(repository, 'default', ['single']) is False
    rule_sync.configure(manager, agent['id'], {'library': False, 'rules': True})
    assert rule_sync.pinned_withheld(repository, 'default', ['single']) is True
    assert rule_sync.pinned_withheld(repository, 'default', ['set-a']) is False
    assert rule_sync.pinned_withheld(repository, 'default', []) is False
    # Another profile's devices and disabled or MosDNS-type devices never withhold.
    assert rule_sync.pinned_withheld(repository, 'missing-profile-devices', ['single']) is False
    update_agent(manager, agent['id'], enabled=False)
    assert rule_sync.pinned_withheld(repository, 'default', ['single']) is False
    update_agent(manager, agent['id'], enabled=True, service_type='mihomo')
    assert rule_sync.pinned_withheld(repository, 'default', ['single']) is False


# ---- a device nothing happened to is not compared again ---------------------------------------


@pytest.fixture
def comparisons(monkeypatch):
    """Every full comparison the worker runs, as (service type, kind or error)."""
    from backend.agents import rule_sync_projection
    seen, real = [], rule_sync_projection.rule_changes

    def rule_changes(base, current, service_type, settings):
        try:
            result = real(base, current, service_type, settings)
        except Exception as error:
            seen.append((service_type, type(error).__name__))
            raise
        seen.append((service_type, result[0]))
        return result
    monkeypatch.setattr(rule_sync_projection, 'rule_changes', rule_changes)
    return seen


def settle(worker, now, agent_id, comparisons):
    """Run until the device is idle and remembered, then forget the comparisons so far."""
    worker.run(agent_id)
    tick(worker, now, agent_id)  # the first round saved its state, which is a new generation
    tick(worker, now, agent_id)
    del comparisons[:]


def state_of(manager, agent_id):
    state = manager.get_agent_by_id(agent_id)['rule_sync']['state']
    return state['status'], state['message']


@pytest.mark.parametrize('service', ['mihomo', 'oxidns'])
def test_unchanged_device_is_compared_once_and_then_left_alone(sync_env, comparisons, service):
    repository, manager, agent, _, _ = as_oxidns(sync_env) if service == 'oxidns' else sync_env
    worker, now = worker_for(manager)
    worker.run(agent['id'])
    assert comparisons == [(service, 'none')]
    tick(worker, now, agent['id'])
    assert comparisons == [(service, 'none')] * 2  # the saved state moved the generation once
    head, saved = repository.head(), manager.get_agent_by_id(agent['id'])['rule_sync']['state']
    for _ in range(20):
        tick(worker, now, agent['id'])
    assert comparisons == [(service, 'none')] * 2
    # Nothing was written either: same generation, same state timestamp.
    assert repository.head() == head
    assert manager.get_agent_by_id(agent['id'])['rule_sync']['state'] == saved
    assert state_of(manager, agent['id']) == ('idle', '已引用的规则没有待同步变化')


def rewrite_receipt(repository, manager, agent_id):
    agent = manager.get_agent_by_id(agent_id)
    record = store.load(repository, agent)
    store.capture(repository, agent_id, record['bundle'], record['source'], record['base_url'])


def touch_rule_cache(repository, _manager, _agent_id):
    folder = repository.shared_rules_dir() / 'by-source'
    folder.mkdir(parents=True, exist_ok=True)
    (folder / 'cached.list').write_text('example.test\n')


@pytest.mark.parametrize('change', [
    lambda repository, manager, agent_id: edit_library(repository, 'unused-new.test', 'lib-unused'),
    lambda repository, manager, agent_id: repository.update_profile_transaction(
        'default', lambda profile: profile['mihomo'].update(custom_config='port: 9999')),
    lambda repository, manager, agent_id: update_agent(manager, agent_id, name='renamed'),
    lambda repository, manager, agent_id: rule_sync.configure(manager, agent_id, {'library': True, 'rules': True}),
    rewrite_receipt, touch_rule_cache,
], ids=['shared edit', 'profile edit', 'agent edit', 'scope', 'receipt', 'rule cache'])
def test_any_changed_input_is_compared_again_exactly_once(sync_env, comparisons, change):
    repository, manager, agent, _, _ = sync_env
    worker, now = worker_for(manager)
    settle(worker, now, agent['id'], comparisons)
    change(repository, manager, agent['id'])
    for _ in range(5):
        tick(worker, now, agent['id'])
    assert comparisons == [('mihomo', 'none')]
    assert state_of(manager, agent['id'])[0] == 'idle'


def test_rule_cache_stamp_follows_content_size_time_and_subfolders(sync_env):
    repository, _, _, _, _ = sync_env
    import os
    root = repository.shared_rules_dir()
    (root / 'by-id').mkdir(parents=True, exist_ok=True)
    first = root / 'a.list'
    first.write_text('one\n')
    stamps = [rule_sync._rule_cache_stamp(repository)]
    def changed():
        stamps.append(rule_sync._rule_cache_stamp(repository))
        return stamps[-1] != stamps[-2]
    assert not changed()
    first.write_text('one\ntwo\n')
    assert changed()                                           # size
    stat = first.stat()
    os.utime(first, ns=(stat.st_atime_ns, stat.st_mtime_ns + 1_000_000_000))
    assert changed()                                           # same bytes refreshed
    (root / 'by-id' / 'nested.list').write_text('three\n')
    assert changed()                                           # a file in a subfolder
    (root / 'by-id' / 'nested.list').unlink()
    assert changed()
    assert not changed()


def test_comparison_is_repeated_after_the_recheck_interval_and_after_a_clock_step_back(sync_env, comparisons):
    _, manager, agent, _, _ = sync_env
    worker, now = worker_for(manager)
    settle(worker, now, agent['id'], comparisons)
    tick(worker, now, agent['id'], rule_sync.RECHECK_SECONDS - 20)
    assert comparisons == []
    tick(worker, now, agent['id'], 20)
    assert comparisons == [('mihomo', 'none')]
    tick(worker, now, agent['id'])
    assert comparisons == [('mihomo', 'none')]
    now[0] -= 600  # the system clock was set back
    worker.run(agent['id'])
    assert comparisons == [('mihomo', 'none')] * 2
    tick(worker, now, agent['id'])
    assert comparisons == [('mihomo', 'none')] * 2


def test_remembered_device_still_notices_and_syncs_a_later_edit(sync_env, comparisons, monkeypatch):
    repository, manager, agent, _, _ = sync_env
    from backend.agents import mihomo_panel
    calls = []
    monkeypatch.setattr(mihomo_panel, 'call', lambda *args: calls.append(args) or {'success': True})
    worker, now = worker_for(manager)
    settle(worker, now, agent['id'], comparisons)
    for _ in range(3):
        tick(worker, now, agent['id'])
    edit_library(repository, 'later.test')
    tick(worker, now, agent['id'])
    assert state_of(manager, agent['id']) == ('pending', '检测到规则变化，等待连续编辑完成')
    tick(worker, now, agent['id'])
    assert [entry[1:] for entry in calls] == [(agent['id'], 'providers/rule', 'PUT', {'name': 'selected'})]
    assert state_of(manager, agent['id'])[0] == 'succeeded'
    assert source_for(manager, agent['id'])['rule_library'][0]['content'] == 'later.test'
    # A pending change is compared on every round, as before; only "no change" is remembered.
    assert [kind for _, kind in comparisons[:2]] == ['content', 'content']
    del comparisons[:]
    for _ in range(4):
        tick(worker, now, agent['id'])
    assert len(calls) == 1 and len(comparisons) <= 2 and state_of(manager, agent['id'])[0] == 'succeeded'


def test_edit_reverted_before_the_next_round_is_not_mistaken_for_the_remembered_state(sync_env, comparisons):
    repository, manager, agent, _, _ = sync_env
    worker, now = worker_for(manager)
    settle(worker, now, agent['id'], comparisons)
    edit_library(repository, 'brief.test')
    edit_library(repository, 'old.test')
    tick(worker, now, agent['id'])
    assert comparisons == [('mihomo', 'none')]  # two saves are a new generation even with the old content
    assert state_of(manager, agent['id'])[0] == 'idle'


def test_unsafe_structure_is_remembered_until_something_changes(sync_env, comparisons, monkeypatch):
    repository, manager, agent, _, _ = sync_env
    from backend.agents import rule_sync_projection
    real = rule_sync_projection.rule_changes
    unsafe = [True]

    def rule_changes(*args):
        if unsafe[0]:
            comparisons.append(('mihomo', 'RuleSyncProjectionError'))
            raise rule_sync_projection.RuleSyncProjectionError('custom structure')
        return real(*args)
    monkeypatch.setattr(rule_sync_projection, 'rule_changes', rule_changes)
    worker, now = worker_for(manager)
    worker.run(agent['id'])
    blocked = ('blocked', '当前原生规则结构无法安全自动合并，请手动检查并发布配置')
    assert state_of(manager, agent['id']) == blocked
    tick(worker, now, agent['id'])
    assert len(comparisons) == 2  # once more for the generation its own state created
    head = repository.head()
    for _ in range(10):
        tick(worker, now, agent['id'])
    assert len(comparisons) == 2 and repository.head() == head and state_of(manager, agent['id']) == blocked
    # The user repairs the configuration: the next round compares again and recovers.
    unsafe[0] = False
    edit_library(repository, 'unused-new.test', 'lib-unused')
    tick(worker, now, agent['id'])
    assert state_of(manager, agent['id']) == ('idle', '已引用的规则没有待同步变化')


def test_forced_refresh_is_not_skipped_by_a_remembered_comparison(sync_env, comparisons, monkeypatch):
    _, manager, agent, _, _ = sync_env
    from backend.agents import mihomo_panel
    calls = []
    monkeypatch.setattr(mihomo_panel, 'call', lambda *args: calls.append(args) or {'success': True})
    worker, now = worker_for(manager)
    settle(worker, now, agent['id'], comparisons)
    # An interrupted download left the device asking for a refresh although the rules compare equal.
    state = {'status': 'pending', 'message': '等待设备上线后同步规则', 'force_refresh': True}
    update_agent(manager, agent['id'], rule_sync={'settings': {'library': True, 'rules': False}, 'state': state})
    worker.run(agent['id'])
    worker.run(agent['id'])
    assert calls == []  # debounce
    tick(worker, now, agent['id'])
    assert [entry[2:] for entry in calls] == [('providers/rule', 'PUT', {'name': 'selected'})]
    assert state_of(manager, agent['id'])[0] == 'succeeded'
    assert all(kind == 'none' for _, kind in comparisons) and len(comparisons) >= 3


def test_devices_and_repositories_do_not_share_remembered_comparisons(sync_env, comparisons, tmp_path_factory):
    repository, manager, agent, source, _ = sync_env
    worker, now = worker_for(manager)
    settle(worker, now, agent['id'], comparisons)
    # A second device of the same profile is compared on its own.
    registration = manager.register_agent({'name': 'second', 'host': 'second.test', 'port': 8080, 'service_type': 'mihomo'})
    second = update_agent(manager, registration['id'], version='1.8.30-go')
    second, _ = enroll(manager, second, source, artifact(second, source, 'second-applied'))
    rule_sync.configure(manager, second['id'], {'library': True, 'rules': False})
    del comparisons[:]
    worker.run(second['id'])
    assert comparisons == [('mihomo', 'none')]
    assert set(rule_sync._comparisons[repository]) == {agent['id'], second['id']}
    # Another repository (another data directory) starts with nothing remembered.
    from backend.common.config_repository import ProfileRepository
    other = ProfileRepository(tmp_path_factory.mktemp('other'))
    assert other not in rule_sync._comparisons


def test_idle_rounds_need_no_publication_snapshot_or_receipt_read(sync_env, comparisons, monkeypatch):
    repository, manager, agent, _, _ = sync_env
    worker, now = worker_for(manager)
    settle(worker, now, agent['id'], comparisons)
    monkeypatch.setattr(repository, 'get_publish_snapshot', lambda *_a, **_k: pytest.fail('snapshot read'))
    monkeypatch.setattr(store, 'load', lambda *_a, **_k: pytest.fail('receipt read'))
    monkeypatch.setattr(manager, 'get_agent_by_id', lambda *_a, **_k: pytest.fail('second Agent read'))
    monkeypatch.setattr(manager, '_update_agents', lambda *_a, **_k: pytest.fail('state write'))
    for _ in range(5):
        tick(worker, now, agent['id'])
    assert comparisons == []


def test_withheld_pin_notice_stays_while_the_comparison_is_remembered(sync_env, comparisons, monkeypatch):
    repository, manager, agent, _, _ = as_oxidns(sync_env)
    rule_sync.configure(manager, agent['id'], {'library': False, 'rules': True})
    draft = {'action': 'upstream', 'source': 'local', 'upstream': {'addr': 'udp://192.0.2.77'}}
    repository.update_profile_transaction('default', lambda profile: profile['oxidns'].update(
        local_dns='udp://192.0.2.77', direct_rules=[], rule_actions={'single': draft}))
    monkeypatch.setattr(manager, 'publish_deployment', lambda *_a, **_k: pytest.fail('DNS draft was published'))
    worker, now = worker_for(manager)
    settle(worker, now, agent['id'], comparisons)
    head = repository.head()
    for _ in range(5):
        tick(worker, now, agent['id'])
    assert comparisons == [] and repository.head() == head
    assert state_of(manager, agent['id']) == ('idle', '已引用的规则没有待同步变化；指定上游需手动发布')
    # Publishing the upstream by hand is a new receipt: the notice goes away.
    current, _ = repository.get_publish_snapshot('default')
    enroll(manager, manager.get_agent_by_id(agent['id']), current, artifact(agent, current, 'manual'))
    tick(worker, now, agent['id'])
    tick(worker, now, agent['id'])
    assert state_of(manager, agent['id']) == ('idle', '已引用的规则没有待同步变化')


def test_ha_pair_is_only_looked_up_for_paired_oxidns_devices(sync_env, comparisons, monkeypatch):
    _, manager, agent, _, _ = as_oxidns(sync_env)
    from backend.agents import oxidns_ha_publish
    monkeypatch.setattr(oxidns_ha_publish, 'pair', lambda *_a: pytest.fail('unpaired device looked up a peer'))
    worker, _ = worker_for(manager)
    worker.run(agent['id'])
    assert comparisons == [('oxidns', 'none')]


SCENARIO_TRACE = [
    ('idle', '已引用的规则没有待同步变化', 0),
    ('idle', '已引用的规则没有待同步变化', 0),
    ('idle', '已引用的规则没有待同步变化', 0),          # unused library entry edited
    ('pending', '检测到规则变化，等待连续编辑完成', 0),  # used entry edited
    ('pending', '检测到规则变化，等待连续编辑完成', 0),  # edited again within the debounce
    ('succeeded', '已引用的规则集已下载并热重载', 1),
    ('succeeded', '已引用的规则集已下载并热重载', 1),
    ('succeeded', '已引用的规则集已下载并热重载', 1),    # inline rule edited: scope is off
    ('pending', '检测到规则变化，等待连续编辑完成', 1),  # scope switched on
    ('pending', '设备正在执行其他操作，稍后同步规则', 1),
    ('idle', '已引用的规则没有待同步变化', 1),          # inline edit reverted
    ('idle', '已引用的规则没有待同步变化', 1),
]


@pytest.mark.parametrize('remember', [True, False])
def test_remembering_comparisons_changes_no_state_transition(sync_env, monkeypatch, remember):
    """The same edits give the same states whether or not unchanged rounds are skipped."""
    repository, manager, agent, _, _ = sync_env
    if not remember:
        monkeypatch.setattr(rule_sync, 'RECHECK_SECONDS', 0)  # every round compares, as before
    from backend.agents import mihomo_panel
    calls = []
    monkeypatch.setattr(mihomo_panel, 'call', lambda *args: calls.append(args) or {'success': True})
    monkeypatch.setattr(manager, 'begin_deployment', lambda *_a, **_k: {'success': False})
    worker, now = worker_for(manager)
    trace = []
    def step(edit=None, seconds=5):
        if edit:
            edit()
        tick(worker, now, agent['id'], seconds)
        trace.append((*state_of(manager, agent['id']), len(calls)))
    step()
    step()
    step(lambda: edit_library(repository, 'unused-new.test', 'lib-unused'))
    step(lambda: edit_library(repository, 'first.test'))
    step(lambda: edit_library(repository, 'second.test'), seconds=1)
    step()
    step()
    step(lambda: edit_inline(repository))
    step(lambda: rule_sync.configure(manager, agent['id'], {'library': True, 'rules': True}))
    step()
    step(lambda: edit_inline(repository, 'old-inline.test'))
    step()
    assert trace == SCENARIO_TRACE


def test_receipt_stamp_changes_exactly_when_the_receipt_file_is_replaced(sync_env):
    repository, manager, agent, source, _ = sync_env
    stamp = store.stamp(repository, agent)
    assert stamp is not None and store.stamp(repository, agent) == stamp
    assert store.stamp(repository, {**agent, 'applied_content': None}) is None
    assert store.stamp(repository, {**agent, 'applied_content': {'deployment_id': 'never-captured'}}) is None
    store.load(repository, agent)
    assert store.stamp(repository, agent) == stamp  # reading does not touch it
    rewrite_receipt(repository, manager, agent['id'])
    replaced = store.stamp(repository, agent)
    assert replaced != stamp
    # A new publication is another receipt file altogether.
    agent, _ = enroll(manager, agent, source, artifact(agent, source, 'next'))
    assert store.stamp(repository, agent) not in (stamp, replaced)
