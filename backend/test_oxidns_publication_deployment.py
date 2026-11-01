"""Real manager publication entry points bracket activation with snapshots."""
from copy import deepcopy
import hashlib

import pytest
import requests

from backend.agents import oxidns_monitor as monitor
from backend.agents.oxidns_dashboard_state import DashboardStateStore
from backend.test_agent_deployments import env
from backend.test_oxidns_deployment_receipt import oxidns_env, bundle_for
from backend.test_oxidns_dashboard import snapshot, ranking, transport
from backend.test_oxidns_ha_publish import ha_publish


@pytest.mark.parametrize('staged', [False, True])
@pytest.mark.parametrize('lost_reply', [False, True])
def test_direct_and_staged_activation_resume_after_lost_response(oxidns_env, monkeypatch, staged, lost_reply):
    repository, manager, agent, config, requests_seen, remote = oxidns_env
    aid = agent['id']
    manager._update_agents(lambda agents: next(a for a in agents if a['id'] == aid).update(version='1.8.53-go'))
    current, ranks, reads = transport(monkeypatch)
    monitor.overview(manager, aid)
    current.update(snapshot(now=2000, count=40, elapsed=900))
    ranks['collected_at_ms'] = 2000
    bundle = bundle_for(agent, config)
    revision = next(item['sha256'] for item in bundle.manifest['files'] if item['role'] == 'config')
    original = manager._deployment_request
    activated = []

    def request(a, method, path, **kwargs):
        activates = method == 'post' and (path.endswith('/activate') or
                     path == 'deployments' and kwargs['headers']['X-Activate'] == 'true')
        if activates:
            state = DashboardStateStore(repository.data_dir).read(aid)
            assert state['pending']['deployment_id'] == 'checked'
            activated.append(state['pending']['totals']['servers'][0]['requests'])
            current.update(snapshot(now=3000, count=6, elapsed=60),
                           source_revision=revision, runtime_revision='e' * 64)
            ranks.update(collected_at_ms=3000, source_revision=revision, runtime_revision='e' * 64)
            remote.update(success=True, status='succeeded', config_version=revision)
            if lost_reply:
                raise requests.ConnectionError('lost reply')
            return deepcopy(remote)
        return original(a, method, path, **kwargs)

    monkeypatch.setattr(manager, '_deployment_request', request)
    result = manager.publish_deployment(aid, bundle, activate=not staged)
    if staged:
        assert result['status'] == 'ready'
        assert len(reads) == 2  # only initial overview + rankings, no upload-only checkpoint
        assert DashboardStateStore(repository.data_dir).read(aid)['schema'] == 1
        result = manager.activate_deployment(aid, 'checked')
    if lost_reply:
        assert result['status'] == 'unknown'
        result = manager.get_deployment(aid, 'checked')
    assert result['status'] == 'succeeded'
    assert activated == [30]
    for _ in range(2):
        manager.get_deployment(aid, 'checked')
        response = monitor.overview(manager, aid)
        assert response['metrics']['requests_total'] == 36
        assert response['statistics']['started_at_ms'] == 1000
        assert response['statistics']['publication_count'] == 1


def test_unavailable_monitoring_never_blocks_successful_publication(oxidns_env, monkeypatch):
    repository, manager, agent, config, requests_seen, remote = oxidns_env
    manager._update_agents(lambda agents: agents[0].update(version='1.8.53-go'))
    transport(monkeypatch)
    monitor.overview(manager, agent['id'])
    state_path = DashboardStateStore(repository.data_dir).path(agent['id'])
    before = state_path.read_bytes()
    attempted = []
    def unavailable(*args, **kwargs):
        attempted.append(True)
        raise monitor.MonitorError('CORE_UNREACHABLE')
    monkeypatch.setattr(monitor, '_overview_snapshot', unavailable)
    remote.update(status='succeeded')
    assert manager.publish_deployment(agent['id'], bundle_for(agent, config))['status'] == 'succeeded'
    assert any(path == 'deployments' and method == 'post' for method, path, _ in requests_seen)
    assert attempted == [True]
    assert state_path.read_bytes() == before


@pytest.mark.parametrize('entry', ['primary', 'secondary'])
@pytest.mark.parametrize('staged', [False, True])
def test_ha_publication_preserves_each_members_own_statistics(ha_publish, monkeypatch, entry, staged):
    env = ha_publish
    env.pair()
    counts = {env.primary['id']: 10, env.secondary['id']: 10}
    originals = deepcopy(env.devices.installed)

    def request(agent, endpoint, params=None, **kwargs):
        files = env.devices.installed[agent['host']]
        applied = files != originals[agent['host']]
        count = 6 if applied else counts[agent['id']]
        now = 3000 if applied else 1000 if count == 10 else 2000
        data = snapshot(now=now, count=count)
        if applied:
            data.update(source_revision=hashlib.sha256(files['config.yaml']).hexdigest(), runtime_revision='e' * 64)
        if endpoint == 'rankings':
            return {**ranking(now), 'source_revision': data['source_revision'], 'runtime_revision': data['runtime_revision']}
        return data

    monkeypatch.setattr(monitor, '_request', request)
    for member in (env.primary, env.secondary):
        monitor.overview(env.manager, member['id'])
    counts.update({env.primary['id']: 40, env.secondary['id']: 70})
    owner = getattr(env, entry)
    bundle = env.bundle(owner)
    env.manager.publish_deployment(owner['id'], bundle, activate=not staged)
    env.drain(owner)
    if staged:
        env.manager.activate_deployment(owner['id'], bundle.manifest['deployment_id'])
        env.drain(owner)
    for member, expected in ((env.primary, 36), (env.secondary, 66)):
        result = monitor.overview(env.manager, member['id'])
        assert result['metrics']['requests_total'] == expected
        assert result['statistics']['publication_count'] == 1
        assert result['statistics']['started_at_ms'] == 1000
