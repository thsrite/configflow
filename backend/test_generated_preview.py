"""Generated files, downloaded files and the actual upload must be identical."""
from copy import deepcopy
import hashlib
import io
import tarfile
import zipfile

from flask import Flask
import pytest
import yaml

from backend.agents import generated_preview as previews
from backend.agents.manager import AgentManager
from backend.routes import agents, register_blueprints
from backend.test_agent_content_preparation import content_env  # noqa: F401


@pytest.fixture
def preview_env(content_env, monkeypatch):
    config, remote, fetches, repository = content_env
    config['system_config']['config_token'] = 'actual-private-config-token'
    revision = [1]
    monkeypatch.setattr(repository, 'get_publish_snapshot', lambda _: (deepcopy(config), revision[0]))
    monkeypatch.setattr(repository, 'get_compat_config', lambda *_: deepcopy(config))
    manager = AgentManager(repository)
    monkeypatch.setattr(agents, 'get_agent_manager', lambda: manager)
    monkeypatch.setattr('backend.utils.provider_delivery.commit_provider_bundle', lambda *_: None)
    ids = {service: manager.register_agent({'name': service, 'host': service + '.test', 'service_type': service})['id']
           for service in ('mihomo', 'mosdns', 'oxidns', 'surge')}
    uploads = []
    calls = []
    capability = {'final_config_protocols': [1], 'deployment_protocols': [1], 'config_path': 'installed.yaml', 'mihomo_resources': []}

    def transport(agent, method, path, **kwargs):
        calls.append(path)
        if path == 'capabilities':
            return {**capability, 'service_type': agent['service_type']}
        if path == 'config/final-preview':
            content = kwargs['json']['content']
            if agent['service_type'] == 'mihomo':
                document = yaml.safe_load(content)
                document['x-device-rendered'] = True
                content = yaml.safe_dump(document)
            return {'success': True, 'content': content}
        if method == 'post' and path == 'deployments':
            uploads.append(kwargs['data'])
        return {'success': True, 'status': 'succeeded', 'config_version': '12345678'}

    monkeypatch.setattr(manager, '_deployment_request', transport)
    app = Flask(__name__)
    register_blueprints(app)
    return app.test_client(), manager, config, remote, revision, ids, uploads, calls, capability


def generate(env, service='mihomo'):
    client, _, _, _, _, ids, *_ = env
    response = client.post(f'/api/agents/{ids[service]}/generate-config', json={
        'expected_profile_id': 'default', 'expected_service_type': service, 'base_url': 'https://config.test'})
    assert response.status_code == 200, response.json
    return response.json


def push(env, result, **extra):
    return env[0].post(f'/api/agents/{result["agent_id"]}/push-config', json={
        'generation_id': result['id'], 'deployment_id': result['deployment_id'], **extra})


@pytest.mark.parametrize('service', ['mihomo', 'mosdns', 'oxidns'])
def test_preview_download_and_upload_share_exact_files(preview_env, monkeypatch, service):
    client, manager, config, remote, _, _, uploads, calls, _ = preview_env
    generated = generate(preview_env, service)
    assert not uploads
    assert not manager.get_agent_by_id(generated['agent_id']).get('deployments')
    if service == 'mihomo':
        assert 'x-device-rendered: true' in generated['content']
        assert 'actual-private-config-token' in generated['content']  # no response sanitizer drift
    artifact = client.get(f'/api/agents/{generated["agent_id"]}/generated-config/{generated["id"]}/download')
    assert artifact.status_code == 200
    with zipfile.ZipFile(io.BytesIO(artifact.data)) as archive:
        downloaded = {path: archive.read(path) for path in archive.namelist()}
    assert downloaded[generated['config_path']] == generated['content'].encode()
    for item in generated['files']:
        assert hashlib.sha256(downloaded[item['path']]).hexdigest() == item['sha256']
        file = client.get(f'/api/agents/{generated["agent_id"]}/generated-config/{generated["id"]}/file', query_string={'path': item['path']})
        assert file.data == downloaded[item['path']]
    remote['provider'] = 'proxies: [{name: CHANGED, type: http, server: changed.test, port: 80}]'
    monkeypatch.setattr(agents, '_prepare_transactional_content', lambda *_a, **_k: pytest.fail('must not regenerate'))
    monkeypatch.setattr('backend.agents.mihomo_resources.prepare_mihomo_resources', lambda *_a, **_k: pytest.fail('must not refetch Geo resources'))
    response = push(preview_env, generated)
    assert response.status_code == 200, response.json
    assert len(uploads) == 1
    with tarfile.open(fileobj=io.BytesIO(uploads[0]), mode='r:gz') as archive:
        delivered = {name.removeprefix('files/'): archive.extractfile(name).read()
                     for name in archive.getnames() if name.startswith('files/')}
    assert downloaded == delivered
    assert calls.count('config/final-preview') == 1
    # A lost response retries the same transaction even after a newer edit.
    config['mihomo']['custom_config'] = 'mode: direct'
    assert push(preview_env, generated).status_code == 200
    assert len(uploads) == 1


@pytest.mark.parametrize('change', ['draft', 'revision', 'binding', 'service', 'host', 'version', 'expired', 'missing', 'wrong-device', 'wrong-transaction'])
def test_stale_or_wrong_preview_never_falls_back_to_regeneration(preview_env, monkeypatch, change):
    _, manager, config, _, revision, ids, uploads, _, _ = preview_env
    generated = generate(preview_env)
    if change == 'draft':
        config['rule_library'][0]['content'] = '+.changed.test'
    elif change == 'revision':
        revision[0] += 1
    elif change in ('binding', 'service', 'host', 'version'):
        if change == 'binding':
            manager.repository.create_profile({'id': 'changed', 'name': 'Changed'})
        field = {'binding': 'profile_id', 'service': 'service_type'}.get(change, change)
        manager.repository.update_system_transaction(lambda system: next(a for a in system['agents'] if a['id'] == generated['agent_id']).update({field: 'changed'}))
    elif change == 'expired':
        monkeypatch.setattr(previews.time, 'time', lambda: generated['expires_at'] + 1)
    elif change == 'missing':
        generated['id'] = 'a' * 32
    elif change == 'wrong-device':
        generated['agent_id'] = ids['mosdns']
    else:
        generated['deployment_id'] = 'different'
    monkeypatch.setattr(agents, '_prepare_transactional_content', lambda *_a, **_k: pytest.fail('must not regenerate'))
    response = push(preview_env, generated)
    assert response.status_code == 409, response.json
    assert response.json['code'] == 'preview_changed'
    assert not uploads


def test_old_agent_and_target_changed_while_generating_fail_closed(preview_env, monkeypatch):
    client, _, _, _, revision, ids, uploads, _, capability = preview_env
    capability['final_config_protocols'] = []
    response = client.post(f'/api/agents/{ids["mihomo"]}/generate-config', json={
        'expected_profile_id': 'default', 'expected_service_type': 'mihomo'})
    assert response.status_code == 400 and '更新目标 Agent' in response.json['message']
    capability['final_config_protocols'] = [1]
    original = previews.finalize_bundle
    def changed(*args):
        bundle = original(*args)
        revision[0] += 1
        return bundle
    monkeypatch.setattr(previews, 'finalize_bundle', changed)
    response = client.post(f'/api/agents/{ids["mihomo"]}/generate-config', json={
        'expected_profile_id': 'default', 'expected_service_type': 'mihomo'})
    assert response.status_code == 409
    assert not uploads


@pytest.mark.parametrize('service', ['mihomo', 'surge', 'loon', 'mosdns', 'oxidns'])
def test_manual_export_downloads_the_generated_bytes_after_draft_changes(preview_env, service):
    client, _, config, *_ = preview_env
    response = client.post(f'/api/profiles/default/generate/{service}/artifact', json={'base_url': 'https://config.test'})
    assert response.status_code == 200, response.json
    generated = response.json
    config['mihomo']['custom_config'] = 'mode: direct'
    response = client.get(f'/api/generate/artifacts/{generated["id"]}/download')
    with zipfile.ZipFile(io.BytesIO(response.data)) as archive:
        assert archive.read(generated['config_path']) == generated['content'].encode()


def test_preview_storage_is_bounded_and_tampering_is_rejected(preview_env, monkeypatch):
    _, manager, *_ = preview_env
    monkeypatch.setattr(previews, 'MAX_PREVIEWS', 1)
    first = generate(preview_env)
    second = generate(preview_env)
    assert push(preview_env, first).status_code == 409
    (manager.repository.data_dir / '.generated-previews' / second['id'] / 'bundle.tar.gz').write_bytes(b'corrupt')
    assert push(preview_env, second).status_code == 409


def test_ha_generation_names_both_devices_and_rejects_changed_peer(preview_env):
    _, manager, _, _, _, ids, uploads, _, _ = preview_env
    peer = manager.register_agent({'name': 'DNS peer', 'host': 'peer.test', 'service_type': 'oxidns'})['id']
    owner = ids['oxidns']
    def pair(system):
        for item in system['agents']:
            if item['id'] in (owner, peer):
                item['oxidns_ha'] = {'enabled': True, 'group_id': 'pair', 'peer_id': peer if item['id'] == owner else owner}
    manager.repository.update_system_transaction(pair)
    generated = generate(preview_env, 'oxidns')
    assert set(generated['target_ids']) == {owner, peer}
    manager.update_heartbeat(peer, {'version': '1.8.54-go'})
    assert push(preview_env, generated).status_code == 409
    assert not uploads


def test_surge_push_uses_the_generated_text(preview_env, monkeypatch):
    _, manager, *_ = preview_env
    generated = generate(preview_env, 'surge')
    delivered = []
    monkeypatch.setattr(agents, 'generate_surge_config', lambda *_a, **_k: pytest.fail('must not regenerate Surge'))
    monkeypatch.setattr(manager, 'push_config_to_agent', lambda _id, content, **_kwargs:
                        delivered.append(content) or {'success': True})
    response = push(preview_env, generated, restart=False)
    assert response.status_code == 200, response.json
    assert delivered == [generated['content']]


def test_initial_subscription_generation_does_not_report_drift_after_push(preview_env):
    from backend.agents.deployment_bundle import deployment_content_fingerprint
    _, manager, config, *_ = preview_env
    generated = generate(preview_env)
    assert push(preview_env, generated).status_code == 200
    agent = manager.get_agent_by_id(generated['agent_id'])
    current = agents._prepare_content_bundle(agent, deepcopy(config), base_url='https://config.test')
    assert agent['applied_content']['fingerprint'] == deployment_content_fingerprint(current.manifest)
