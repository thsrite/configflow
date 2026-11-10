"""Frequent Agent reads skip the publication history; whoever needs it must ask for it."""
import copy
import json
import pickle

import pytest

from backend.agents import rule_sync
from backend.agents.deployment_bundle import build_deployment_bundle
from backend.common.config_repository import AGENT_HISTORY_FIELDS, AGENT_HISTORY_NOT_READ
from backend.test_agent_deployments import env  # noqa: F401
from backend.test_agent_rule_sync import (  # noqa: F401
    artifact, enroll, isolated_background, sync_env, tick, update_agent, worker_for,
)
from backend.test_agent_upgrade_auth import authenticated_app  # noqa: F401


def with_history(manager, agent_id):
    """Give the device publication records and a verification report."""
    def save(agents):
        agent = next(item for item in agents if item['id'] == agent_id)
        agent.setdefault('deployments', {}).update(
            {f'release-{index}': {'deployment_id': f'release-{index}', 'status': 'succeeded',
                                  'events': [{'message': 'x' * 200}]} for index in range(5)})
        agent['latest_deployment'] = copy.deepcopy(agent['deployments']['release-4'])
        agent['ipv6_verifications'] = {'release-4': {'status': 'ok', 'checks': [{'id': 'dns_aaaa', 'status': 'ok'}]}}
    manager._update_agents(save)


@pytest.fixture
def reads(monkeypatch):
    """Whether each repository read of the Agents asked for their history."""
    def watch(repository):
        seen, real = [], repository.get_agents
        monkeypatch.setattr(repository, 'get_agents', lambda *, history=True: seen.append(history) or real(history=history))
        monkeypatch.setattr(repository, 'get_system', lambda: pytest.fail('whole system section copied'))
        return seen
    return watch


def without_history(agent):
    return {key: value for key, value in agent.items() if key not in AGENT_HISTORY_FIELDS}


def test_records_without_history_equal_the_full_ones_in_every_other_field(env):
    repository, manager, agent, _ = env
    manager.register_agent({'name': 'second', 'host': 'second.test', 'port': 8080, 'service_type': 'oxidns'})
    with_history(manager, agent['id'])
    full, light = repository.get_agents(), repository.get_agents(history=False)
    assert full == repository.get_system()['agents'] and len(full) == 2
    assert [without_history(item) for item in light] == [without_history(item) for item in full]
    assert len(full[0]['deployments']) == 5 and 'release-4' in full[0]['ipv6_verifications']
    for item in light:
        # Also on a device without any history: a missing field must not read as "no publications".
        assert all(item[field] is AGENT_HISTORY_NOT_READ for field in AGENT_HISTORY_FIELDS)
    # Both are copies: editing them never reaches the stored document.
    light[0]['latest_deployment']['status'] = 'edited'
    full[0]['deployments']['release-0']['status'] = 'edited'
    again = repository.get_agents()
    assert again[0]['latest_deployment']['status'] == 'succeeded' and again[0]['deployments']['release-0']['status'] == 'succeeded'


def test_manager_reads_lay_heartbeat_state_over_both_kinds_of_record(env):
    _, manager, agent, _ = env
    with_history(manager, agent['id'])
    manager.update_heartbeat(agent['id'], {'version': '9.9.9-go', 'service_status': 'running',
                                           'system_metrics': {'cpu': 12.5}})
    full, light = manager.get_agent_by_id(agent['id']), manager.get_agent_by_id(agent['id'], history=False)
    assert light['service_status'] == 'running' and light['cpu'] == 12.5 and light['status'] == 'online'
    assert without_history(light) == without_history(full)
    assert len(full['deployments']) == 5 and light['deployments'] is AGENT_HISTORY_NOT_READ
    listed_full, listed_light = manager.get_all_agents(), manager.get_all_agents(history=False)
    assert [without_history(item) for item in listed_light] == [without_history(item) for item in listed_full]
    assert manager.get_agent_by_id('missing', history=False) is None


@pytest.mark.parametrize('use', [
    lambda value: value.get('release-0'), lambda value: value['release-0'], lambda value: 'release-0' in value,
    lambda value: list(value), lambda value: len(value), lambda value: bool(value), lambda value: value or {},
    lambda value: value.items(), lambda value: value.keys(), lambda value: value.values(),
    lambda value: value.setdefault('x', {}), lambda value: value.pop('x', None), lambda value: value.update({}),
    lambda value: value.__setitem__('x', 1), lambda value: {**value}, lambda value: dict(value),
    lambda value: [item for item in value], lambda value: value.copy(), lambda value: not value,
])
def test_every_use_of_unread_history_fails(use):
    with pytest.raises(RuntimeError, match='history=True'):
        use(AGENT_HISTORY_NOT_READ)


def test_unread_history_survives_copies_but_is_never_serialized_or_saved(env):
    repository, manager, agent, _ = env
    with_history(manager, agent['id'])
    light = manager.get_agent_by_id(agent['id'], history=False)
    for copied in (copy.deepcopy(light), copy.copy(light), {**light}, pickle.loads(pickle.dumps(light))):
        assert all(copied[field] is AGENT_HISTORY_NOT_READ for field in AGENT_HISTORY_FIELDS)
    assert repr(AGENT_HISTORY_NOT_READ) == '<Agent history not read>'
    with pytest.raises(TypeError):
        json.dumps(light)
    # A record read without history cannot be written back as if the device had none.
    head = repository.head()
    with pytest.raises(Exception):
        manager._update_agents(lambda agents: agents.__setitem__(0, light))
    assert repository.head() == head
    assert len(manager.get_agent_by_id(agent['id'])['deployments']) == 5


def test_heartbeat_and_agent_reports_never_copy_the_history(authenticated_app, reads):
    client, manager, agents, _, _ = authenticated_app
    agent = manager.get_agent_by_id(agents[0]['id'])
    with_history(manager, agent['id'])
    seen = reads(manager.repository)
    headers = {'Authorization': 'Bearer ' + agent['token']}
    response = client.post(f"/api/agents/{agent['id']}/heartbeat", headers=headers,
                           json={'version': '1.8.54-go', 'service_status': 'running'})
    assert response.status_code == 200 and response.get_json()['success'] is True
    assert client.post(f"/api/agents/{agent['id']}/heartbeat", headers={'Authorization': 'Bearer wrong'},
                       json={}).status_code == 401
    assert client.post(f"/api/agents/{agent['id']}/traffic-report", headers=headers, json={}).status_code in (400, 409)
    assert seen and not any(seen)
    # The heartbeat was recorded against the full record all the same.
    stored = manager.get_agent_by_id(agent['id'])
    assert stored['version'] == '1.8.54-go' and stored['service_status'] == 'running' and len(stored['deployments']) == 5


def test_agent_list_and_detail_still_return_the_publication_records(authenticated_app):
    client, manager, agents, admin, _ = authenticated_app
    with_history(manager, agents[0]['id'])
    listed = client.get('/api/agents', headers=admin).get_json()
    listed = listed if isinstance(listed, list) else listed.get('agents', listed)
    first = next(item for item in listed if item['id'] == agents[0]['id'])
    assert sorted(first['deployments']) == [f'release-{index}' for index in range(5)]
    assert first['ipv6_verifications']['release-4']['status'] == 'ok'
    detail = client.get(f"/api/agents/{agents[0]['id']}", headers=admin).get_json()
    assert sorted(detail['deployments']) == sorted(first['deployments'])
    other = next(item for item in listed if item['id'] == agents[1]['id'])
    assert not other.get('deployments')


def test_idle_rule_sync_rounds_and_status_never_copy_the_history(sync_env, reads):
    repository, manager, agent, _, _ = sync_env
    with_history(manager, agent['id'])
    worker, now = worker_for(manager)
    worker.run(agent['id'])
    tick(worker, now, agent['id'])
    seen = reads(repository)
    for _ in range(5):
        tick(worker, now, agent['id'])
    assert rule_sync.status(manager, agent['id'])['state']['status'] == 'idle'
    assert seen and not any(seen)


def test_rule_sync_publication_works_from_a_record_without_history(sync_env, monkeypatch):
    """The whole structural release runs although the worker never reads the history itself."""
    repository, manager, agent, source, _ = sync_env
    with_history(manager, agent['id'])
    rule_sync.configure(manager, agent['id'], {'library': False, 'rules': True})
    repository.update_profile_transaction('default', lambda profile: next(
        item for item in profile['rule_configs'] if item.get('id') == 'single').update(value='new-inline.test'))
    uploads = []
    def transport(target, method, path, **kwargs):
        if path == 'capabilities':
            return {'deployment_protocols': [1], 'reload_only_protocols': [1], 'service_type': 'mihomo'}
        uploads.append(kwargs['data'])
        return {'success': True, 'status': 'succeeded'}
    monkeypatch.setattr(manager, '_deployment_request', transport)
    worker, now = worker_for(manager, prepare=lambda target, projected, baseline, scopes, identity:
                             artifact(target, projected, identity, reload_only=True))
    worker.run(agent['id'])
    tick(worker, now, agent['id'])
    assert len(uploads) == 1
    stored = manager.get_agent_by_id(agent['id'])
    assert rule_sync.status(manager, agent['id'])['state']['status'] == 'succeeded'
    # The new publication was added to the history; the earlier records are all still there.
    assert len(stored['deployments']) >= 6 and 'release-0' in stored['deployments']
    assert stored['ipv6_verifications']['release-4']['status'] == 'ok'


def test_panel_lookups_do_not_copy_the_history_but_publication_statistics_do(env, reads):
    from backend.agents import mihomo_panel, oxidns_monitor, oxidns_rule_update
    repository, manager, agent, _ = env
    dns = manager.register_agent({'name': 'dns', 'host': 'dns.test', 'port': 8080, 'service_type': 'oxidns'})
    for agent_id in (agent['id'], dns['id']):
        update_agent(manager, agent_id, version='1.8.54-go')
        with_history(manager, agent_id)
    seen = reads(repository)
    assert mihomo_panel._agent(manager, agent['id'])['deployments'] is AGENT_HISTORY_NOT_READ
    assert oxidns_monitor._agent(manager, dns['id'])['deployments'] is AGENT_HISTORY_NOT_READ
    assert oxidns_rule_update._agent(manager, dns['id'])['deployments'] is AGENT_HISTORY_NOT_READ
    assert seen == [False, False, False]
    assert len(oxidns_monitor._agent(manager, dns['id'], history=True)['deployments']) == 5
    assert seen[-1] is True
