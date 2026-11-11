"""Only the exact candidate checked by the current rules can be activated."""
from copy import deepcopy
import hashlib

import pytest
import requests
from flask import Flask

from backend.agents.deployment_bundle import DeploymentBundle, _pack, build_deployment_bundle, retarget_config_path
from backend.agents.manager import AgentManager
from backend.agents.oxidns_validation_receipt import receipt_matches, validate_bundle
from backend.routes import register_blueprints
from backend.routes.agents import _public_agent, _public_deployment
from backend.test_agent_deployments import env


@pytest.fixture
def oxidns_env(env, monkeypatch):
    repository, manager, agent, config = env
    manager.update_agent(agent['id'], {'service_type': 'oxidns'})
    agent = manager.get_agent_by_id(agent['id'])
    calls = []
    remote = {'status': 'ready'}

    def request(_self, _agent, method, path, **kwargs):
        calls.append((method, path, kwargs))
        if path == 'capabilities':
            return {'deployment_protocols': [1], 'service_type': _agent['service_type'], 'config_path': 'installed.yaml'}
        if path.endswith('/activate'):
            return {'success': True, 'status': 'succeeded', 'config_version': 'activated'}
        return {'success': True, **deepcopy(remote)}

    monkeypatch.setattr(AgentManager, '_deployment_request', request)
    return repository, manager, agent, config, calls, remote


def bundle_for(agent, config, ident='checked', content='plugins: []\n'):
    return build_deployment_bundle(agent, config, content, deployment_id=ident)


def legacy_ready(manager, agent, bundle):
    ident = bundle.manifest['deployment_id']
    manager.begin_deployment(agent['id'], ident)
    manager.record_deployment(agent['id'], ident, {
        'status': 'ready', 'success': True, 'submitted': True, 'sha256': bundle.sha256})
    manager.finish_deployment_preparation(agent['id'], ident)


def test_legacy_ready_is_rejected_without_activation_and_can_be_replaced(oxidns_env, monkeypatch):
    repository, manager, agent, config, calls, remote = oxidns_env
    bad = bundle_for(agent, config, 'legacy', 'plugins: [{tag: c, type: cache, args: {size: -1}}]\n')
    legacy_ready(manager, agent, bad)
    # A current valid profile cannot attest the different bytes already staged.
    repository.save_profile('default', {'oxidns': {'config_mode': 'native', 'native_config': 'plugins: []\n'}})
    monkeypatch.setattr('backend.routes.agents.get_agent_manager', lambda: manager)
    app = Flask(__name__)
    register_blueprints(app)
    response = app.test_client().post(f'/api/agents/{agent["id"]}/deployments/legacy/activate')
    assert response.status_code == 409
    assert response.json['status'] == 'failed'
    assert response.json['failed_stage'] == 'verifying'
    assert '重新推送' in response.json['message']
    assert not any(method == 'post' for method, _, _ in calls)
    assert manager.get_agent_by_id(agent['id'])['config_version'] == '0'
    # Polling an old remote ready state cannot resurrect the refused candidate.
    assert manager.get_deployment(agent['id'], 'legacy')['status'] == 'failed'
    checked = manager.publish_deployment(agent['id'], bundle_for(agent, config, 'replacement'), activate=False)
    assert checked['status'] == 'ready'
    assert receipt_matches(manager.get_agent_by_id(agent['id']), checked)


@pytest.mark.parametrize('activate', [False, True])
def test_publication_checks_final_bundle_and_persists_receipt_before_upload(oxidns_env, monkeypatch, activate):
    repository, manager, agent, config, calls, remote = oxidns_env
    bundle = bundle_for(agent, config)
    final_bundle = retarget_config_path(bundle, 'installed.yaml')
    original_request = manager._deployment_request

    def request(target, method, path, **kwargs):
        if path == 'deployments':
            stored = manager.get_agent_by_id(agent['id'])['deployments']['checked']
            assert receipt_matches(agent, stored)
            assert stored['oxidns_validation_receipt']['sha256'] == final_bundle.sha256
            assert kwargs['data'] == final_bundle.archive
            assert kwargs['headers']['X-Activate'] == str(activate).lower()
        return original_request(target, method, path, **kwargs)

    monkeypatch.setattr(manager, '_deployment_request', request)
    # push_config_to_agent is also the common auto-sync publication path.
    result = manager.push_config_to_agent(agent['id'], 'ignored current input', extra_data={
        'deployment_bundle': bundle, 'activate': activate})
    assert result['status'] == 'ready'
    receipt = result['oxidns_validation_receipt']
    assert receipt['deployment_id'] == 'checked'
    assert receipt['sha256'] != bundle.sha256  # filename retargeting changes the archive
    restarted = AgentManager(repository)
    assert receipt_matches(restarted.get_agent_by_id(agent['id']), result)
    # Editing the draft does not change which already-checked candidate activates.
    repository.save_profile('default', {'oxidns': {'config_mode': 'native',
        'native_config': 'plugins: [{tag: c, type: cache, args: {size: -1}}]\n'}})
    assert restarted.activate_deployment(agent['id'], 'checked')['status'] == 'succeeded'
    assert any(path.endswith('/activate') for _, path, _ in calls)
    assert 'oxidns_validation_receipt' not in _public_deployment(result)
    assert 'oxidns_validation_receipt' not in _public_agent(restarted.get_agent_by_id(agent['id']))['latest_deployment']


def test_direct_manager_publication_cannot_attest_invalid_packaged_config(oxidns_env):
    _, manager, agent, config, calls, _ = oxidns_env
    bad = bundle_for(agent, config, content='plugins: [{tag: c, type: cache, args: {size: -1}}]\n')
    result = manager.publish_deployment(agent['id'], bad)
    assert result['status'] == 'failed'
    assert 'plugins[0].args.size' in result['message']
    assert not result.get('submitted')
    assert 'oxidns_validation_receipt' not in result
    assert not any(method == 'post' for method, _, _ in calls)


@pytest.mark.parametrize('change', ['rules', 'hash', 'id', 'profile', 'agent', 'fingerprint'])
def test_receipt_must_match_current_rules_and_exact_candidate_identity(oxidns_env, monkeypatch, change):
    repository, manager, agent, config, calls, _ = oxidns_env
    manager.publish_deployment(agent['id'], bundle_for(agent, config), activate=False)
    if change == 'rules':
        monkeypatch.setattr('backend.agents.oxidns_validation_receipt.rules_fingerprint', lambda: 'changed-rules')
    else:
        field = {'hash': 'sha256', 'id': 'deployment_id', 'profile': 'profile_id',
                 'agent': 'agent_id', 'fingerprint': 'fingerprint'}[change]
        def change_receipt(system):
            system['agents'][0]['deployments']['checked']['oxidns_validation_receipt'][field] = 'other'
        repository.update_system_transaction(change_receipt)
    result = manager.activate_deployment(agent['id'], 'checked')
    assert result['http_status'] == 409
    assert result['status'] == 'failed'
    assert not any(path.endswith('/activate') for _, path, _ in calls)


def test_agent_response_cannot_forge_receipt_and_same_id_retry_does_not_mint_one(oxidns_env):
    _, manager, agent, config, calls, remote = oxidns_env
    bundle = retarget_config_path(bundle_for(agent, config), 'installed.yaml')
    legacy_ready(manager, agent, bundle)
    remote['oxidns_validation_receipt'] = validate_bundle(agent['id'], bundle)
    result = manager.publish_deployment(agent['id'], bundle, activate=False)
    assert result['status'] == 'ready'
    assert 'oxidns_validation_receipt' not in result
    assert manager.activate_deployment(agent['id'], 'checked')['http_status'] == 409
    assert not any(method == 'post' for method, _, _ in calls)


def test_successful_legacy_candidate_is_idempotent_and_can_still_rollback(oxidns_env):
    _, manager, agent, config, calls, remote = oxidns_env
    legacy_ready(manager, agent, bundle_for(agent, config))
    manager.record_deployment(agent['id'], 'checked', {'success': True, 'status': 'succeeded'})
    assert manager.activate_deployment(agent['id'], 'checked')['status'] == 'succeeded'
    assert calls == []
    remote.update(status='rolled_back', manual_rollback=True)
    assert manager.rollback_deployment(agent['id'], 'checked')['status'] == 'rolled_back'
    assert any(path.endswith('/rollback') for _, path, _ in calls)


@pytest.mark.parametrize('status', ['verifying', 'stopping', 'unknown'])
def test_inflight_candidate_is_not_invalidated_for_missing_receipt(oxidns_env, status):
    _, manager, agent, config, calls, remote = oxidns_env
    legacy_ready(manager, agent, bundle_for(agent, config))
    remote['status'] = status
    result = manager.activate_deployment(agent['id'], 'checked')
    assert result['http_status'] == 409
    assert manager.get_agent_by_id(agent['id'])['deployments']['checked']['status'] == status
    assert not any(method == 'post' for method, _, _ in calls)


def test_offline_ready_cannot_be_activated_or_marked_failed(oxidns_env, monkeypatch):
    _, manager, agent, config, _, _ = oxidns_env
    legacy_ready(manager, agent, bundle_for(agent, config))
    def offline(*_args, **_kwargs):
        raise requests.Timeout()
    monkeypatch.setattr(manager, '_deployment_request', offline)
    result = manager.activate_deployment(agent['id'], 'checked')
    assert result['http_status'] == 409
    assert manager.get_agent_by_id(agent['id'])['deployments']['checked']['status'] == 'ready'


def test_status_progress_during_receipt_check_is_not_overwritten(oxidns_env, monkeypatch):
    _, manager, agent, config, calls, _ = oxidns_env
    legacy_ready(manager, agent, bundle_for(agent, config))
    def progressed(_agent, _state):
        manager.record_deployment(agent['id'], 'checked', {'success': True, 'status': 'stopping'})
        return False
    monkeypatch.setattr('backend.agents.oxidns_validation_receipt.receipt_matches', progressed)
    result = manager.activate_deployment(agent['id'], 'checked')
    assert result['http_status'] == 409
    assert manager.get_agent_by_id(agent['id'])['deployments']['checked']['status'] == 'stopping'
    assert not any(method == 'post' for method, _, _ in calls)


def test_mosdns_ready_activation_does_not_require_oxidns_receipt(oxidns_env):
    _, manager, agent, config, calls, _ = oxidns_env
    manager.update_agent(agent['id'], {'service_type': 'mosdns'})
    agent = manager.get_agent_by_id(agent['id'])
    legacy_ready(manager, agent, bundle_for(agent, config))
    assert manager.activate_deployment(agent['id'], 'checked')['status'] == 'succeeded'
    assert any(path.endswith('/activate') for _, path, _ in calls)


def test_receipt_inspects_archive_bytes_and_refuses_unexpanded_includes(oxidns_env):
    _, _, agent, config, _, _ = oxidns_env
    bundle = bundle_for(agent, config)
    with pytest.raises(ValueError, match='digest'):
        validate_bundle(agent['id'], DeploymentBundle(bundle.manifest, bundle.archive, '0' * 64, bundle.config_version))
    raw = b'include: [parts.yaml]\nplugins: []\n'
    manifest = deepcopy(bundle.manifest)
    manifest['files'][0].update(size=len(raw), sha256=hashlib.sha256(raw).hexdigest())
    bundle = _pack(manifest, {'config.yaml': (raw, 'config')}, bundle.config_version)
    with pytest.raises(ValueError, match='展开 include'):
        validate_bundle(agent['id'], bundle)
