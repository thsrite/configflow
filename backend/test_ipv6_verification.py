"""Post-publication verification must preserve scope and evidence boundaries."""
from copy import deepcopy
from concurrent.futures import Future
import threading

import pytest

from backend.agents import ipv6_verification as verify
from backend.agents import ipv6_readiness as bridge
from backend.agents.oxidns_monitor import MonitorError
from backend.test_ipv6_readiness import YAML, environment  # noqa: F401
from backend.test_agent_upgrade_auth import authenticated_app  # noqa: F401


class InlineExecutor:
    def submit(self, fn, *args):
        future = Future()
        try:
            future.set_result(fn(*args))
        except Exception as error:
            future.set_exception(error)
        return future


def complete_runtime():
    return {'success': True, 'checks': [{'id': ident, 'label': ident, 'status': 'ok', 'message': '已取得证据'}
                                      for ident in ('deployed_config', 'runtime_core', 'runtime_ipv6',
                                                    'runtime_tun', 'proxy_http', 'node_ipv6_transport',
                                                    'dns_a', 'dns_aaaa')]}


@pytest.fixture
def verification(environment, monkeypatch):
    client, manager, agent, headers, path, router, calls, fail = environment
    monkeypatch.setattr(verify, '_executor', InlineExecutor())
    monkeypatch.setattr(bridge, '_inspect', lambda *a, **k: {'checks': [], 'routers': []})
    responses = []
    def request(target, operation='', payload=None):
        calls.append((operation, payload))
        if responses:
            return responses.pop(0)
        return complete_runtime()
    monkeypatch.setattr(bridge, '_request', request)
    return client, manager, agent, headers, path, calls, responses


def publish(manager, agent, ident='publication-one', status='succeeded'):
    _, revision = manager.repository.get_publish_snapshot('v6')
    manager.begin_deployment(agent['id'], ident)
    manager.record_deployment(agent['id'], ident, {'status': status, 'success': status == 'succeeded',
                                                  'config_revision': revision, 'config_version': 'a' * 64})
    return ident, revision


def snapshot(monkeypatch, config, ident):
    from backend.agents import rule_sync_store
    monkeypatch.setattr(rule_sync_store, 'load', lambda *a, **k: {
        'source': deepcopy(config), 'manifest': {'deployment_id': ident,
                                               'files': [{'role': 'config', 'sha256': 'a' * 64}]}})


def test_manual_verification_persists_separate_results_without_claiming_client_path(verification):
    client, manager, agent, headers, path, calls, _ = verification
    before, revision = manager.repository.get_publish_snapshot('v6')
    response = client.post(path + '/verify', headers=headers, json={'expected_config_revision': revision})
    assert response.status_code == 200, response.get_json()
    report = response.get_json()['verification']
    assert report['status'] == 'warning' and report['scope'] == 'mihomo_runtime'
    assert next(c for c in report['checks'] if c['id'] == 'runtime_expected_config')['status'] == 'unknown'
    assert next(c for c in report['checks'] if c['id'] == 'client_not_configured')['status'] == 'unknown'
    after, current_revision = manager.repository.get_publish_snapshot('v6')
    # Device reports live in the repository too, but must not change generated
    # configuration or its revision.
    assert current_revision == revision
    assert after['mihomo'] == before['mihomo']
    assert after['proxy_groups'] == before['proxy_groups']
    assert after['rule_configs'] == before['rule_configs']
    saved = client.get(path + '/verify', headers=headers).get_json()['verification']
    assert saved == report
    assert [c[0] for c in calls] == ['verify']


def test_verification_rejects_external_targets_and_stale_manual_revision(verification):
    client, _, _, headers, path, calls, _ = verification
    for body in ({'test_url': 'http://other.test'}, {'host': '::1'}, {'expected_config_revision': -1}):
        assert client.post(path + '/verify', headers=headers, json=body).status_code in (400, 409)
    assert not calls
    assert client.post(path + '/verify', json={}).status_code == 401
    assert client.get(path + '/verify', headers={**headers, 'X-ConfigFlow-Profile': 'other'}).status_code == 409


@pytest.mark.parametrize('status', ['ready', 'failed', 'rolled_back', 'checking'])
def test_unconfirmed_publications_never_start_network_verification(verification, status):
    client, manager, agent, headers, path, calls, _ = verification
    ident, _ = publish(manager, agent, status=status)
    response = client.post(path + '/verify', headers=headers, json={'deployment_id': ident})
    assert response.status_code == 409
    assert not calls


def test_auto_verification_uses_exact_published_snapshot_and_is_idempotent(verification, monkeypatch):
    client, manager, agent, headers, path, calls, _ = verification
    original, _ = manager.repository.get_publish_snapshot('v6')
    ident, revision = publish(manager, agent)
    snapshot(monkeypatch, original, ident)
    manager.repository.save_profile('v6', {'mihomo': {'custom_config': 'ipv6: false'}})
    inspected = []
    monkeypatch.setattr(bridge, '_inspect', lambda m, a, p, config: inspected.append(config) or {'checks': [], 'routers': []})
    body = {'deployment_id': ident, 'expected_config_revision': revision}
    first = client.post(path + '/verify', headers=headers, json=body)
    assert first.status_code == 200, first.get_json()
    second = client.post(path + '/verify', headers=headers, json=body)
    assert second.get_json()['verification']['run_id'] == first.get_json()['verification']['run_id']
    assert len(calls) == 1 and inspected == [original]
    assert calls[0][1]['expected_config_sha256'] == 'a' * 64
    assert first.get_json()['verification']['config_revision'] == revision


def test_changed_target_invalidates_completed_results(verification, monkeypatch):
    client, manager, agent, headers, path, _, _ = verification
    def changed(*a, **k):
        manager.update_agent(agent['id'], {'host': '192.168.0.9'})
        return {'checks': [{'id': 'http', 'label': 'HTTP', 'status': 'ok', 'message': 'ok'}]}
    monkeypatch.setattr(bridge, '_request', changed)
    response = client.post(path + '/verify', headers=headers, json={})
    report = response.get_json()['verification']
    assert report['status'] == 'warning'
    assert [c['id'] for c in report['checks']] == ['result_stale']


def test_scheduler_runs_enrolled_success_even_without_frontend(verification, monkeypatch):
    _, manager, agent, _, _, calls, _ = verification
    config, _ = manager.repository.get_publish_snapshot('v6')
    ident, _ = publish(manager, agent)
    snapshot(monkeypatch, config, ident)
    verify.tick(manager)
    assert not calls  # Never enroll all historical success records on upgrade.
    verify.enroll(manager, agent['id'], ident, config)
    verify.tick(manager)
    verify.tick(manager)
    assert [c[0] for c in calls] == ['verify']
    assert manager.get_agent_by_id(agent['id'])['ipv6_verifications'][ident]['status'] == 'ok'


def test_scheduler_does_not_run_failed_or_manual_rollback(verification):
    _, manager, agent, _, _, calls, _ = verification
    config, _ = manager.repository.get_publish_snapshot('v6')
    ident, _ = publish(manager, agent, status='failed')
    verify.enroll(manager, agent['id'], ident, config)
    verify.tick(manager)
    assert not calls


def add_client(manager, *, profile='v6', host='192.168.0.4'):
    return manager.register_agent({'name': 'Probe', 'host': host, 'port': 27648, 'service_type': 'mosdns',
                                   'deployment_method': 'shell', 'version': '1.8.50-go', 'profile_id': profile})


def test_settings_require_explicit_independent_capable_client(verification, monkeypatch):
    client, manager, agent, headers, path, _, _ = verification
    probe = add_client(manager)
    monkeypatch.setattr(verify, '_capabilities', lambda *a: {'platform': 'linux', 'ipv6_client_probe_protocols': [1]})
    for ident in [agent['id'], 'missing', add_client(manager, profile='other', host='192.168.0.5')['id']]:
        response = client.put(path + '/verification-settings', headers=headers,
                              json={'client_agent_id': ident, 'test_url': ''})
        assert response.status_code == 409
    for url in ['file:///etc/passwd', 'https://user:password@example.test', 'https://example.test/#fragment']:
        assert client.put(path + '/verification-settings', headers=headers,
                          json={'client_agent_id': '', 'test_url': url}).status_code == 400
    response = client.put(path + '/verification-settings', headers=headers,
                          json={'client_agent_id': probe['id'], 'test_url': 'http://test.example/health'})
    assert response.status_code == 200, response.get_json()
    assert response.get_json()['settings']['client_agent_id'] == probe['id']
    assert [a['id'] for a in response.get_json()['candidates']] == [probe['id']]


def test_client_http_success_needs_matching_mihomo_flow_and_ipv6_evidence(verification, monkeypatch):
    _, manager, agent, _, _, _, _ = verification
    probe = add_client(manager)
    source = manager.get_agent_by_id(agent['id'])
    monkeypatch.setattr(verify, '_capabilities', lambda *a: {'platform': 'linux', 'ipv6_client_probe_protocols': [1]})
    def request(target, operation, payload):
        if operation == 'verify-connections':
            return {'matches': [{'source_ip': '192.168.0.4', 'source_port': 5555, 'host': 'site.test', 'chains': ['node']}],
                    'checks': []}
        return {'checks': [{'id': 'http', 'label': '网站访问', 'status': 'ok', 'message': '200'}],
                'target_host': 'site.test', 'requests': [{'source_ip': '192.168.0.4', 'source_port': 5556, 'status': 'ok'}]}
    monkeypatch.setattr(bridge, '_request', request)
    checks = verify._client_checks(manager, source, {'mihomo': {'custom_config': 'ipv6: true'}},
                                   {'client_agent_id': probe['id']}, 'https://site.test/', 'test-run')
    assert next(c for c in checks if c['id'] == 'client_mihomo_path')['status'] == 'unknown'
    assert next(c for c in checks if c['id'] == 'client_ipv6_transport')['status'] == 'unknown'


def test_runtime_failure_keeps_publication_success_separate(verification):
    client, manager, agent, headers, path, _, responses = verification
    responses.append({'checks': [{'id': 'http', 'label': '网站访问', 'status': 'error', 'message': '连接失败'}]})
    response = client.post(path + '/verify', headers=headers, json={})
    assert response.get_json()['verification']['status'] == 'error'
    assert not manager.get_agent_by_id(agent['id']).get('latest_deployment')


@pytest.mark.parametrize('when', ['during', 'after'])
def test_saved_manual_configuration_change_invalidates_evidence(verification, monkeypatch, when):
    client, manager, _, headers, path, _, _ = verification
    def change():
        manager.repository.save_profile('v6', {'mihomo': {'custom_config': YAML + 'log-level: debug\n'}})
    if when == 'during':
        def request(*a, **k):
            change()
            return complete_runtime()
        monkeypatch.setattr(bridge, '_request', request)
    response = client.post(path + '/verify', headers=headers, json={})
    if when == 'after':
        change()
        response = client.get(path + '/verify', headers=headers)
    report = response.get_json()['verification']
    assert report['stale'] and report['status'] == 'warning'
    assert '已保存配置已变化' in report['checks'][0]['details']


@pytest.mark.parametrize('when', ['during', 'after'])
def test_client_identity_is_bound_for_entire_report_lifetime(verification, monkeypatch, when):
    client, manager, agent, headers, path, _, _ = verification
    probe = add_client(manager)
    def set_client(agents):
        next(a for a in agents if a['id'] == agent['id'])['ipv6_verification_settings'] = {
            'client_agent_id': probe['id'], 'test_url': ''}
    manager._update_agents(set_client)
    def client_checks(*a, **k):
        if when == 'during':
            manager.update_agent(probe['id'], {'host': '192.168.0.24'})
        return [verify._check('client_ros_path', '客户端经过 ROS', 'ok', '已确认')]
    monkeypatch.setattr(verify, '_client_checks', client_checks)
    response = client.post(path + '/verify', headers=headers, json={})
    if when == 'after':
        manager.update_agent(probe['id'], {'host': '192.168.0.24'})
        response = client.get(path + '/verify', headers=headers)
    report = response.get_json()['verification']
    assert report['stale'] and report['status'] == 'warning'
    assert '检测客户端已变化' in report['checks'][0]['details']


@pytest.mark.parametrize('change', [{'enabled': False}, {'gateway_installation_id': 'replacement'}])
def test_identity_tracks_disable_and_reinstallation(change):
    original = {'id': 'one', 'host': '192.0.2.3', 'enabled': True, 'gateway_installation_id': 'first'}
    assert verify._identity(original) != verify._identity({**original, **change})


def test_new_publication_invalidates_completed_report(verification, monkeypatch):
    client, manager, agent, headers, path, _, _ = verification
    config, _ = manager.repository.get_publish_snapshot('v6')
    ident, _ = publish(manager, agent)
    snapshot(monkeypatch, config, ident)
    first = client.post(path + '/verify', headers=headers, json={'deployment_id': ident}).get_json()['verification']
    assert first['status'] == 'ok'
    manager.begin_deployment(agent['id'], 'replacement')
    report = client.get(path + '/verify', headers=headers).get_json()['verification']
    assert report['status'] == 'warning' and report['stale']
    assert '设备发布版本已变化' in report['checks'][0]['details']


def test_publication_failure_can_be_retried_explicitly_but_not_each_scheduler_tick(verification, monkeypatch):
    client, manager, agent, headers, path, calls, responses = verification
    config, _ = manager.repository.get_publish_snapshot('v6')
    ident, _ = publish(manager, agent)
    snapshot(monkeypatch, config, ident)
    verify.enroll(manager, agent['id'], ident, config)
    responses.append({'checks': [{'id': 'proxy_http', 'status': 'error', 'message': 'temporary failure'}]})
    verify.tick(manager)
    first = manager.get_agent_by_id(agent['id'])['ipv6_verifications'][ident]
    verify.tick(manager)
    assert first['status'] == 'error' and len(calls) == 1
    response = client.post(path + '/verify', headers=headers, json={'deployment_id': ident})
    second = response.get_json()['verification']
    assert second['status'] == 'ok' and second['run_id'] != first['run_id']
    assert len(calls) == 2


def test_old_result_cannot_bypass_expected_revision_validation(verification, monkeypatch):
    client, manager, agent, headers, path, calls, _ = verification
    config, _ = manager.repository.get_publish_snapshot('v6')
    ident, revision = publish(manager, agent)
    snapshot(monkeypatch, config, ident)
    client.post(path + '/verify', headers=headers, json={'deployment_id': ident})
    response = client.post(path + '/verify', headers=headers,
                           json={'deployment_id': ident, 'expected_config_revision': revision + 1})
    assert response.status_code == 409 and len(calls) == 1


@pytest.mark.parametrize('response', [{}, {'checks': None}, {'checks': 'ok'},
                                     {'checks': [{'id': 'proxy_http', 'status': 'ok', 'details': None}]},
                                     {'checks': [{'id': 'proxy_http', 'status': 'ok', 'details': 'text'}]}])
def test_incomplete_agent_reply_cannot_report_success(verification, monkeypatch, response):
    client, manager, agent, headers, path, _, responses = verification
    config, _ = manager.repository.get_publish_snapshot('v6')
    ident, _ = publish(manager, agent)
    snapshot(monkeypatch, config, ident)
    responses.append(response)
    report = client.post(path + '/verify', headers=headers, json={'deployment_id': ident}).get_json()['verification']
    assert report['status'] == 'warning'
    assert next(c for c in report['checks'] if c['id'] == 'runtime_evidence')['status'] == 'unknown'


def test_failed_submit_persists_retryable_failure_and_releases_queue_slot(verification, monkeypatch):
    _, manager, agent, _, _, _, _ = verification
    class BrokenExecutor:
        def submit(self, *args):
            raise RuntimeError('executor stopped')
    monkeypatch.setattr(verify, '_executor', BrokenExecutor())
    slots = threading.BoundedSemaphore(1)
    monkeypatch.setattr(verify, '_slots', slots)
    with pytest.raises(RuntimeError, match='executor stopped'):
        verify.start(manager, agent['id'], 'v6', {})
    assert manager.get_agent_by_id(agent['id'])['ipv6_verifications']['manual']['status'] == 'error'
    assert slots.acquire(blocking=False)
    slots.release()
    monkeypatch.setattr(verify, '_executor', InlineExecutor())
    report = verify.start(manager, agent['id'], 'v6', {})['verification']
    assert report['status'] == 'warning'
    assert all(c['id'] != 'interrupted' for c in report['checks'])


def test_worker_failure_is_persisted_without_stuck_running_state(verification, monkeypatch):
    _, manager, agent, _, _, _, _ = verification
    def broken(*args):
        raise RuntimeError('worker failed')
    monkeypatch.setattr(verify, '_execute', broken)
    report = verify.start(manager, agent['id'], 'v6', {})['verification']
    assert report['status'] == 'error' and report['checks'][0]['id'] == 'interrupted'


def test_interrupted_run_expires_and_late_worker_cannot_replace_retry(verification, monkeypatch):
    _, manager, agent, _, _, _, _ = verification
    pending = []
    class DeferredExecutor:
        def submit(self, fn):
            pending.append(fn)
    monkeypatch.setattr(verify, '_executor', DeferredExecutor())
    first = verify.start(manager, agent['id'], 'v6', {})['verification']
    def age(agents):
        next(a for a in agents if a['id'] == agent['id'])['ipv6_verifications']['manual']['started_epoch'] = 0
    manager._update_agents(age)
    verify.tick(manager)
    assert manager.get_agent_by_id(agent['id'])['ipv6_verifications']['manual']['status'] == 'error'
    monkeypatch.setattr(verify, '_executor', InlineExecutor())
    second = verify.start(manager, agent['id'], 'v6', {})['verification']
    assert second['run_id'] != first['run_id']
    pending[0]()
    assert verify.result(manager, agent['id'], 'v6')['verification'] == second


@pytest.mark.parametrize('epoch', [None, 'broken', float('nan'), float('inf'), 0])
def test_invalid_or_expired_scheduler_timestamp_is_not_stuck_running(epoch):
    assert verify._expired({'status': 'running', 'started_epoch': epoch})


def test_request_id_mismatch_cannot_confirm_client_path(verification, monkeypatch):
    _, manager, agent, _, _, _, _ = verification
    probe = add_client(manager)
    monkeypatch.setattr(verify, '_capabilities', lambda *a: {'platform': 'linux', 'ipv6_client_probe_protocols': [1]})
    monkeypatch.setattr(bridge, '_request', lambda *a: {'request_id': 'old-run', 'checks': []})
    checks = verify._client_checks(manager, manager.get_agent_by_id(agent['id']), {},
                                   {'client_agent_id': probe['id']}, 'https://site.test', 'new-run')
    assert next(c for c in checks if c['id'] == 'client_request_identity')['status'] == 'unknown'
    assert all(c['status'] == 'unknown' for c in checks)


def test_socket_check_retains_its_limited_scope():
    checks = verify._safe_checks({'checks': [{'id': 'node_ipv6_transport', 'status': 'ok',
                                            'scope': 'mihomo_ipv6_socket', 'details': None}]})
    assert checks[0]['scope'] == 'mihomo_ipv6_socket'


def test_scheduler_persists_missing_snapshot_once_and_manual_retry_recovers(verification, monkeypatch):
    from backend.agents import rule_sync_store
    client, manager, agent, headers, path, calls, _ = verification
    config, _ = manager.repository.get_publish_snapshot('v6')
    ident, _ = publish(manager, agent)
    verify.enroll(manager, agent['id'], ident, config)
    loaded = []
    monkeypatch.setattr(rule_sync_store, 'load', lambda *a, **k: loaded.append(True))
    verify.tick(manager)
    first = verify.result(manager, agent['id'], 'v6')['verification']
    assert first['status'] == 'error' and first['error_code'] == 'SNAPSHOT_UNAVAILABLE'
    assert first['deployment_id'] == ident and not first.get('stale')
    verify.tick(manager)
    assert len(loaded) == 1 and not calls
    snapshot(monkeypatch, config, ident)
    report = client.post(path + '/verify', headers=headers, json={'deployment_id': ident}).get_json()['verification']
    assert report['status'] == 'ok' and report['run_id'] != first['run_id']


@pytest.mark.parametrize('code', ['BUSY', 'DEPLOYMENT_CHANGED', 'AGENT_CHANGED'])
def test_scheduler_does_not_freeze_temporary_or_changed_target(verification, monkeypatch, code):
    _, manager, agent, _, _, _, _ = verification
    config, _ = manager.repository.get_publish_snapshot('v6')
    ident, _ = publish(manager, agent)
    verify.enroll(manager, agent['id'], ident, config)
    attempts = []
    def unavailable(*a, **k):
        attempts.append(True)
        raise MonitorError(code, 409, '暂不能开始')
    monkeypatch.setattr(verify, 'start', unavailable)
    verify.tick(manager)
    verify.tick(manager)
    assert len(attempts) == 2
    assert not manager.get_agent_by_id(agent['id']).get('ipv6_verifications')


@pytest.mark.parametrize('manual', [False, True])
def test_verification_uses_confirmed_adapted_file_digest_not_uploaded_yaml(verification, monkeypatch, manual):
    client, manager, agent, headers, path, calls, _ = verification
    config, _ = manager.repository.get_publish_snapshot('v6')
    ident, _ = publish(manager, agent)
    snapshot(monkeypatch, config, ident)  # Original uploaded YAML has digest a.
    def adapted(agents):
        current = next(a for a in agents if a['id'] == agent['id'])
        current['config_version'] = 'b' * 64
        current['deployments'][ident]['config_version'] = 'b' * 64
        current['latest_deployment']['config_version'] = 'b' * 64
        current['applied_content'] = {'deployment_id': ident}
    manager._update_agents(adapted)
    response = client.post(path + '/verify', headers=headers, json={} if manual else {'deployment_id': ident})
    assert response.status_code == 200, response.get_json()
    assert response.get_json()['verification']['status'] == 'ok'
    assert calls[0][1]['expected_config_sha256'] == 'b' * 64


@pytest.mark.parametrize('digest', ['a' * 8, '', None, 'g' * 64])
def test_invalid_final_digest_cannot_fall_back_to_upload_digest(verification, monkeypatch, digest):
    client, manager, agent, headers, path, calls, _ = verification
    config, _ = manager.repository.get_publish_snapshot('v6')
    ident, _ = publish(manager, agent)
    snapshot(monkeypatch, config, ident)
    def unconfirmed(agents):
        current = next(a for a in agents if a['id'] == agent['id'])
        current['deployments'][ident]['config_version'] = digest
        current['latest_deployment']['config_version'] = digest
    manager._update_agents(unconfirmed)
    report = client.post(path + '/verify', headers=headers, json={'deployment_id': ident}).get_json()['verification']
    assert report['status'] == 'warning'
    assert 'expected_config_sha256' not in calls[0][1]
    assert next(c for c in report['checks'] if c['id'] == 'runtime_expected_config')['status'] == 'unknown'


# The end-to-end report of 2026-10-31: Mihomo published at revision 81, the
# upstream router publication then recorded its readback in the same profile
# (82). Mihomo's files were unchanged, yet the manual verification asked for
# another Mihomo publication. Only what Mihomo receives decides it now.
BASE = 'http://configflow.test'


def confirm_publication(manager, agent, ident='publication-real', version='c' * 64):
    """A confirmed publication recorded as push-config records it: bundle, baseline, receipt."""
    from backend.agents import rule_sync_store
    from backend.agents.deployment_bundle import retarget_config_path
    from backend.routes.agents import _prepare_content_bundle
    config, revision = manager.repository.get_publish_snapshot('v6')
    manager.begin_deployment(agent['id'], ident)
    bundle = retarget_config_path(_prepare_content_bundle(manager.get_agent_by_id(agent['id']), config,
                                                          base_url=BASE, deployment_id=ident), 'config.yaml')
    manager._remember_deployment_content(agent['id'], bundle, BASE, None, config)
    rule_sync_store.capture(manager.repository, agent['id'], bundle, config, BASE,
                            comparison_config=bundle.comparison_config)
    manager.record_deployment(agent['id'], ident, {'status': 'succeeded', 'success': True,
                                                  'config_revision': revision, 'config_version': version})
    assert manager.get_agent_by_id(agent['id'])['config_revision'] == revision
    return ident, revision


def publish_router(manager):
    """What the upstream router publication writes back: readback records, same Mihomo files."""
    config, before = manager.repository.get_publish_snapshot('v6')
    router = config['routers']['items'][0]
    manager.repository.save_router_publication('v6', router, ipv6_fakeip={
        'owned': [{'.id': '*A1', 'dst-address': 'fc00::/64', 'gateway': 'fe80::3%lan4'}]},
        ipv6_routes={'owned': [{'.id': '*A2', 'dst-address': '2001:b28:f23d::/48'}]},
        last_release={'status': 'succeeded'})
    _, after = manager.repository.get_publish_snapshot('v6')
    assert after == before + 1
    return after


@pytest.fixture
def offline(monkeypatch):
    # Preparing Mihomo's files for the comparison must not reach out anywhere.
    monkeypatch.setattr('requests.sessions.Session.request',
                        lambda *_a, **_k: pytest.fail('unexpected network request'))


def verify_now(client, path, headers, revision):
    response = client.post(path + '/verify', headers=headers, json={'expected_config_revision': revision})
    assert response.status_code == 200, response.get_json()
    return response.get_json()['verification']


def test_router_publication_after_mihomo_publication_still_verifies(verification, offline):
    client, manager, agent, headers, path, calls, _ = verification
    confirm_publication(manager, agent)
    revision = publish_router(manager)
    report = verify_now(client, path, headers, revision)
    assert report['status'] == 'ok', report
    assert report['config_revision'] == revision
    assert calls[-1] == ('verify', {'test_url': calls[-1][1]['test_url'], 'expected_config_sha256': 'c' * 64})
    assert not any(c['id'] == 'runtime_expected_config' for c in report['checks'])
    # Another service's settings in the same profile move the revision too.
    manager.repository.save_profile('v6', {'oxidns': {**manager.repository.get_publish_snapshot('v6')[0]['oxidns'],
                                                      'custom_hosts': '192.0.2.8 nas.lab.test'}})
    _, later = manager.repository.get_publish_snapshot('v6')
    assert later == revision + 1
    assert verify_now(client, path, headers, later)['status'] == 'ok'
    assert calls[-1][1]['expected_config_sha256'] == 'c' * 64


@pytest.mark.parametrize('change', [
    {'mihomo': {'custom_config': YAML + 'log-level: debug\n'}},
    {'mihomo': {'custom_config': YAML.replace('fc00::1/64', 'fc00:0:0:2::1/64')}},
])
def test_a_real_mihomo_change_still_needs_publication(verification, offline, change):
    client, manager, agent, headers, path, calls, _ = verification
    confirm_publication(manager, agent)
    publish_router(manager)
    manager.repository.save_profile('v6', change)
    _, revision = manager.repository.get_publish_snapshot('v6')
    report = verify_now(client, path, headers, revision)
    expected = next(c for c in report['checks'] if c['id'] == 'runtime_expected_config')
    assert expected['status'] == 'unknown' and '还没有发布到这台 Mihomo' in expected['message']
    assert report['status'] == 'warning'
    assert 'expected_config_sha256' not in calls[-1][1]


def test_same_revision_needs_no_comparison(verification, monkeypatch):
    client, manager, agent, headers, path, calls, _ = verification
    config, _ = manager.repository.get_publish_snapshot('v6')
    ident, revision = publish(manager, agent)
    snapshot(monkeypatch, config, ident)
    manager._update_agents(lambda agents: next(a for a in agents if a['id'] == agent['id']).update(
        applied_content={'deployment_id': ident}))
    from backend.agents import content_sync
    monkeypatch.setattr(content_sync, 'compare_now', lambda *a, **k: pytest.fail('compared at the published revision'))
    assert verify_now(client, path, headers, revision)['status'] == 'ok'
    assert calls[-1][1]['expected_config_sha256'] == 'a' * 64


@pytest.mark.parametrize('status', ['unknown', 'checking'])
def test_unconfirmed_comparison_is_not_treated_as_published(verification, monkeypatch, status):
    client, manager, agent, headers, path, calls, _ = verification
    config, _ = manager.repository.get_publish_snapshot('v6')
    ident, _ = publish(manager, agent)
    snapshot(monkeypatch, config, ident)
    manager._update_agents(lambda agents: next(a for a in agents if a['id'] == agent['id']).update(
        applied_content={'deployment_id': ident}))
    revision = publish_router(manager)
    from backend.agents import content_sync
    monkeypatch.setattr(content_sync, 'compare_now', lambda *a, **k: {'status': status})
    report = verify_now(client, path, headers, revision)
    expected = next(c for c in report['checks'] if c['id'] == 'runtime_expected_config')
    assert expected['status'] == 'unknown' and '缺少与当前已保存配置一致的发布记录' in expected['message']
    assert 'expected_config_sha256' not in calls[-1][1]


# 「关闭」: the verification is about IPv6, so without IPv6 it does not apply.
OFF = 'ipv6: false\ndns:\n  enable: true\n  ipv6: false\n  enhanced-mode: fake-ip\n'


def test_manual_verification_without_ipv6_is_not_applicable(verification):
    client, manager, agent, headers, path, calls, _ = verification
    manager.repository.save_profile('v6', {'mihomo': {'custom_config': OFF}})
    _, revision = manager.repository.get_publish_snapshot('v6')
    for response in (client.post(path + '/verify', headers=headers, json={'expected_config_revision': revision}),
                     client.get(path + '/verify', headers=headers)):
        assert response.status_code == 200, response.get_json()
        data = response.get_json()
        assert data['applicable'] is False and data['verification'] is None
        assert data['message'] == '未启用 IPv6，无需验收'
    assert not calls
    assert not manager.get_agent_by_id(agent['id']).get('ipv6_verifications')


def test_publication_without_ipv6_is_judged_by_its_own_snapshot(verification, monkeypatch):
    client, manager, agent, headers, path, calls, _ = verification
    manager.repository.save_profile('v6', {'mihomo': {'custom_config': OFF}})
    published, _ = manager.repository.get_publish_snapshot('v6')
    ident, revision = publish(manager, agent)
    snapshot(monkeypatch, published, ident)
    # A later draft turns IPv6 on; the publication itself had it off.
    manager.repository.save_profile('v6', {'mihomo': {'custom_config': YAML}})
    response = client.post(path + '/verify', headers=headers,
                           json={'deployment_id': ident, 'expected_config_revision': revision})
    assert response.status_code == 200, response.get_json()
    assert response.get_json()['applicable'] is False and response.get_json()['verification'] is None
    assert not calls
    # Never enrolled, so the scheduler does not verify it either.
    verify.enroll(manager, agent['id'], ident, published)
    verify.tick(manager)
    assert not calls


def test_device_check_reports_publication_from_mihomo_files(verification, offline, monkeypatch):
    """The step 「保存并发布 Mihomo」: the same judgement, without blocking the device check."""
    from backend.agents import content_sync
    jobs = []

    class Deferred:
        def submit(self, fn, *args):
            jobs.append((fn, args))
            return Future()
    monkeypatch.setattr(content_sync, '_checker', content_sync.ContentSyncChecker(executor=Deferred()))
    client, manager, agent, headers, path, calls, _ = verification

    def publication():
        response = client.get(path, headers=headers)
        assert response.status_code == 200, response.get_json()
        return response.get_json()['publication']

    def settle():
        fn, args = jobs.pop()
        fn(*args)
        assert not jobs
    assert publication() == 'unknown'  # nothing published from here yet
    confirm_publication(manager, agent)
    assert publication() == 'synced' and not jobs  # the published revision needs no comparison
    publish_router(manager)
    assert publication() == 'checking'
    settle()
    assert publication() == 'synced'
    manager.repository.save_profile('v6', {'mihomo': {'custom_config': YAML + 'log-level: debug\n'}})
    assert publication() == 'checking'
    settle()
    assert publication() == 'changed'


# ---- the scheduler leaves an unchanged configuration alone ---------------------------------


def settled(manager, agent, monkeypatch, ident='publication-one'):
    """A published, enrolled and automatically verified device."""
    config, _ = manager.repository.get_publish_snapshot('v6')
    publish(manager, agent, ident)
    snapshot(monkeypatch, config, ident)
    verify.enroll(manager, agent['id'], ident, config)
    verify.tick(manager)
    verify.tick(manager)
    assert manager.get_agent_by_id(agent['id'])['ipv6_verifications'][ident]['status'] == 'ok'
    return config


def test_scheduler_reads_nothing_while_the_configuration_is_unchanged(verification, monkeypatch):
    _, manager, agent, _, _, calls, _ = verification
    settled(manager, agent, monkeypatch)
    head = manager.repository.head()
    with monkeypatch.context() as patched:
        patched.setattr(manager, 'get_all_agents', lambda: pytest.fail('idle round read the Agents'))
        patched.setattr(manager, 'get_agent_by_id', lambda *_a: pytest.fail('idle round read an Agent'))
        for _ in range(5):
            verify.tick(manager)
    assert manager.repository.head() == head and len(calls) == 1
    # Any commit ends the rest: the next publication is verified as before.
    config, _ = manager.repository.get_publish_snapshot('v6')
    publish(manager, agent, 'publication-two')
    snapshot(monkeypatch, config, 'publication-two')
    verify.enroll(manager, agent['id'], 'publication-two', config)
    verify.tick(manager)
    assert len(calls) == 2
    assert manager.get_agent_by_id(agent['id'])['ipv6_verifications']['publication-two']['status'] == 'ok'


def test_scheduler_does_not_reenter_start_for_a_publication_it_already_verified(verification, monkeypatch):
    _, manager, agent, _, _, calls, _ = verification
    settled(manager, agent, monkeypatch)
    monkeypatch.setattr(verify, 'start', lambda *_a, **_k: pytest.fail('verified publication was started again'))
    monkeypatch.setattr(verify, 'result', lambda *_a, **_k: pytest.fail('verified publication was read back'))
    for change in range(3):
        manager.repository.update_profile_fields('v6', {'description': f'edit {change}'})
        verify.tick(manager)
    assert len(calls) == 1


def test_running_report_expires_by_the_clock_without_any_commit(verification, monkeypatch):
    _, manager, agent, _, _, _, _ = verification
    import time
    started = time.time()
    def running(agents):
        next(a for a in agents if a['id'] == agent['id'])['ipv6_verifications'] = {
            'manual': {'status': 'running', 'started_epoch': started, 'profile_id': 'v6', 'checks': []}}
    manager._update_agents(running)
    for _ in range(3):
        verify.tick(manager)
    assert manager.get_agent_by_id(agent['id'])['ipv6_verifications']['manual']['status'] == 'running'
    monkeypatch.setattr(verify.time, 'time', lambda: started + 301)
    verify.tick(manager)
    report = manager.get_agent_by_id(agent['id'])['ipv6_verifications']['manual']
    assert report['status'] == 'error' and report['checks'][0]['id'] == 'interrupted'
    # Nothing is running any more: after one more look the following rounds rest.
    verify.tick(manager)
    monkeypatch.setattr(manager, 'get_all_agents', lambda: pytest.fail('idle round read the Agents'))
    verify.tick(manager)
    verify.tick(manager)


def test_unfinished_publication_is_polled_on_every_round(verification, monkeypatch):
    _, manager, agent, _, _, calls, _ = verification
    config, _ = manager.repository.get_publish_snapshot('v6')
    ident, _ = publish(manager, agent)
    snapshot(monkeypatch, config, ident)
    verify.enroll(manager, agent['id'], ident, config)
    def unfinished(agents):
        current = next(a for a in agents if a['id'] == agent['id'])
        current['deployments'][ident].update(status='verifying', submitted=True)
        current['latest_deployment'] = deepcopy(current['deployments'][ident])
    manager._update_agents(unfinished)
    polls, real = [], manager.get_deployment
    def get_deployment(agent_id, deployment_id):
        polls.append(deployment_id)
        return real(agent_id, deployment_id)
    monkeypatch.setattr(manager, 'get_deployment', get_deployment)
    monkeypatch.setattr(type(manager), '_deployment_request', lambda _self, *_a, **_k: {'status': 'verifying'}, raising=False)
    for _ in range(4):
        verify.tick(manager)
    assert polls == [ident] * 4 and not calls
    # The device finishes without any change on this side: the next round verifies it.
    monkeypatch.setattr(type(manager), '_deployment_request',
                        lambda _self, *_a, **_k: {'status': 'succeeded', 'success': True, 'config_version': 'a' * 64}, raising=False)
    verify.tick(manager)
    verify.tick(manager)
    assert [c[0] for c in calls] == ['verify']


@pytest.mark.parametrize('rest', [True, False])
def test_resting_between_rounds_changes_no_outcome(verification, monkeypatch, rest):
    from backend.agents import idle_rounds
    if not rest:
        monkeypatch.setattr(idle_rounds, 'RECHECK_SECONDS', 0)  # every round runs, as before
    _, manager, agent, _, _, calls, responses = verification
    trace = []
    def rounds(count=3):
        for _ in range(count):
            verify.tick(manager)
            runs = manager.get_agent_by_id(agent['id']).get('ipv6_verifications') or {}
            trace.append((sorted((key, run['status']) for key, run in runs.items()), len(calls)))
    rounds()
    config, _ = manager.repository.get_publish_snapshot('v6')
    publish(manager, agent, 'one')
    snapshot(monkeypatch, config, 'one')
    rounds()
    verify.enroll(manager, agent['id'], 'one', config)
    rounds()
    manager.repository.update_profile_fields('v6', {'description': 'edited'})
    rounds()
    responses.append({'checks': [{'id': 'proxy_http', 'status': 'error', 'message': 'temporary failure'}]})
    publish(manager, agent, 'two')
    snapshot(monkeypatch, config, 'two')
    verify.enroll(manager, agent['id'], 'two', config)
    rounds()
    publish(manager, agent, 'three', status='failed')
    verify.enroll(manager, agent['id'], 'three', config)
    rounds()
    assert trace == [([], 0)] * 6 + [([('one', 'ok')], 1)] * 6 + [([('one', 'ok'), ('two', 'error')], 2)] * 6
