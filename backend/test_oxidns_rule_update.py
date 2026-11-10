"""Rule refresh is a fixed, authenticated operation with truthful task results."""
import copy
import json

import pytest
import requests

from backend.agents import oxidns_rule_update as updates
from backend.agents.oxidns_monitor import MonitorError
from backend.test_agent_upgrade_auth import authenticated_app  # noqa: F401

AGENT = {'id': 'dns', 'name': 'DNS', 'host': '127.0.0.1', 'port': 8082,
         'service_type': 'oxidns', 'version': '1.8.30-go', 'token': 'agent-secret'}


class Manager:
    def __init__(self, agent=AGENT):
        self.agent = copy.deepcopy(agent)

    def get_agent_by_id(self, agent_id, history=True):
        return self.agent if agent_id == 'dns' else None


def task(status='running', **fields):
    return {'id': 'a' * 32, 'status': status,
            'phase': 'downloading' if status == 'running' else 'done',
            'downloaded': 0, 'download_total': 2, 'reloaded': 0, 'reload_total': 1,
            **fields}


@pytest.fixture
def calls(monkeypatch):
    seen = []
    replies = [(202, {'success': True, 'task': task()})]

    def request(session, method, url, **kwargs):
        seen.append({'method': method, 'url': url, 'trust_env': session.trust_env, **kwargs})
        status, data = replies.pop(0)
        response = requests.Response()
        response.status_code = status
        response._content = data if isinstance(data, bytes) else json.dumps(data).encode()
        response._content_consumed = True
        return response

    monkeypatch.setattr(requests.Session, 'request', request)
    return seen, replies


def test_start_uses_only_registered_agent_and_keeps_accepted_running(calls):
    seen, _ = calls
    result, status = updates.call(Manager(), 'dns', 'POST')
    assert status == 202 and result['task']['status'] == 'running'
    assert seen[0]['url'] == 'http://127.0.0.1:8082/api/oxidns/rules/update'
    assert seen[0]['data'] == '{}'
    assert seen[0]['headers']['Authorization'] == 'Bearer agent-secret'
    assert seen[0]['trust_env'] is False
    assert seen[0]['allow_redirects'] is False
    assert 'agent-secret' not in json.dumps(result)


def test_get_returns_actual_completed_counts_and_strips_private_fields(calls):
    seen, replies = calls
    replies[:] = [(200, {'success': True, 'task': task(
        'succeeded', downloaded=2, reloaded=1, token='secret', url='https://secret.test')})]
    result, status = updates.call(Manager(), 'dns', 'GET')
    assert status == 200 and result['task']['status'] == 'succeeded'
    assert result['task']['downloaded'] == 2 and result['task']['reloaded'] == 1
    assert 'secret' not in json.dumps(result)
    assert seen[0]['data'] is None


def test_failed_download_never_becomes_success_and_has_safe_actionable_message(calls):
    _, replies = calls
    replies[:] = [(200, {'success': True, 'task': task('failed', code='DOWNLOAD_FAILED',
                  downloaded=1, message='http://user:secret@upstream.test')})]
    result, _ = updates.call(Manager(), 'dns', 'GET')
    assert result['task']['status'] == 'failed'
    assert result['task']['code'] == 'DOWNLOAD_FAILED'
    assert '未执行热重载' in result['task']['message']
    assert 'secret' not in json.dumps(result)


def test_missing_update_config_points_to_downloadable_rules_not_scheduling(calls):
    _, replies = calls
    replies[:] = [(409, {'success': False, 'code': 'RULE_UPDATE_NOT_CONFIGURED',
                        'message': 'Enable native rule updates and publish the configuration'})]
    with pytest.raises(MonitorError) as error:
        updates.call(Manager(), 'dns', 'POST')
    assert error.value.code == 'RULE_UPDATE_NOT_CONFIGURED'
    assert error.value.status == 409
    assert error.value.message == '请先配置可下载的规则并重新发布 OxiDNS 配置，然后重试。'


@pytest.mark.parametrize('agent,agent_id,code', [
    (AGENT, 'missing', 'AGENT_NOT_FOUND'),
    ({**AGENT, 'service_type': 'mihomo'}, 'dns', 'WRONG_SERVICE'),
    ({**AGENT, 'version': '1.8.20-go'}, 'dns', 'UPGRADE_REQUIRED'),
    ({**AGENT, 'host': 'host/path?secret'}, 'dns', 'AGENT_UNREACHABLE'),
])
def test_rejects_unsupported_targets_before_request(calls, agent, agent_id, code):
    with pytest.raises(MonitorError) as error:
        updates.call(Manager(agent), agent_id, 'POST')
    assert error.value.code == code
    assert calls[0] == []


@pytest.mark.parametrize('status,data,code', [
    (401, b'secret', 'AGENT_AUTH_FAILED'),
    (302, b'secret', 'INVALID_RESPONSE'),
    (404, b'404 page not found', 'UPGRADE_REQUIRED'),
    (200, b'not json secret', 'INVALID_RESPONSE'),
    (200, b'x' * (updates.MAX_RESPONSE_BYTES + 1), 'INVALID_RESPONSE'),
    (409, {'success': False, 'code': 'SERVICE_BUSY', 'message': 'secret'}, 'SERVICE_BUSY'),
    (409, {'success': False, 'code': ['secret']}, 'RULE_UPDATE_FAILED'),
    (500, {'message': 'secret'}, 'RULE_UPDATE_FAILED'),
    (200, {'success': True, 'task': task('succeeded')}, 'INVALID_RESPONSE'),
    (200, {'success': True, 'task': task(downloaded=True)}, 'INVALID_RESPONSE'),
    (200, {'success': True, 'task': task(downloaded=3)}, 'INVALID_RESPONSE'),
    (200, {'success': True, 'task': task(id='http://secret')}, 'INVALID_RESPONSE'),
])
def test_rejects_invalid_or_private_upstream_responses(calls, status, data, code):
    _, replies = calls
    replies[:] = [(status, data)]
    with pytest.raises(MonitorError) as error:
        updates.call(Manager(), 'dns', 'GET')
    assert error.value.code == code
    assert 'secret' not in error.value.message


def test_transport_failure_is_sanitized(monkeypatch):
    def fail(*args, **kwargs):
        raise requests.Timeout('http://user:secret@private.test')
    monkeypatch.setattr(requests.Session, 'request', fail)
    with pytest.raises(MonitorError) as error:
        updates.call(Manager(), 'dns', 'POST')
    assert error.value.code == 'AGENT_UNREACHABLE'
    assert 'secret' not in error.value.message


def test_route_requires_admin_and_never_accepts_agent_token(authenticated_app, monkeypatch):
    client, _, (agent, _), admin, _ = authenticated_app
    seen = []
    monkeypatch.setattr(updates, 'call', lambda *args: (seen.append(args) or
                        ({'success': True, 'task': task()}, 202)))
    path = '/api/agents/' + agent['id'] + '/oxidns/rules/update'
    for headers in ({}, {'Authorization': 'Bearer ' + agent['token']}):
        assert client.post(path, json={}, headers=headers).status_code == 401
        assert client.get(path, headers=headers).status_code == 401
    assert seen == []
    result = client.post(path, json={}, headers={**admin, 'X-ConfigFlow-Profile': 'unrelated'})
    assert result.status_code == 202
    assert result.headers['Cache-Control'] == 'no-store'
    assert seen[0][1:] == (agent['id'], 'POST')


@pytest.mark.parametrize('method,suffix,data,content_type', [
    ('POST', '?url=http://example.test', '{}', 'application/json'),
    ('POST', '', '{"tag":"arbitrary"}', 'application/json'),
    ('POST', '', '[]', 'application/json'),
    ('POST', '', 'null', 'application/json'),
    ('POST', '', '{}', 'text/plain'),
    ('POST', '', '{', 'application/json'),
    ('POST', '', ' ' * 1025, 'application/json'),
    ('GET', '', '{}', 'application/json'),
])
def test_route_rejects_injected_or_invalid_payloads(authenticated_app, monkeypatch,
                                                 method, suffix, data, content_type):
    client, _, (agent, _), admin, _ = authenticated_app
    monkeypatch.setattr(updates, 'call', lambda *args: pytest.fail('unexpected Agent request'))
    path = '/api/agents/' + agent['id'] + '/oxidns/rules/update' + suffix
    response = client.open(path, method=method, data=data, content_type=content_type, headers=admin)
    assert response.status_code == 400
    assert response.get_json()['code'] == 'INVALID_REQUEST'
    assert response.headers['Cache-Control'] == 'no-store'


def test_targeted_paths_require_new_agent_and_are_sent_as_a_list(calls):
    seen, _ = calls
    with pytest.raises(MonitorError) as error:
        updates.call(Manager(), 'dns', 'POST', paths=['rules/a.txt'])
    assert error.value.code == 'UPGRADE_REQUIRED' and seen == []
    manager = Manager({**AGENT, 'version': '1.8.35-go'})
    for invalid in ([], 'rules/a.txt', [''], [1]):
        with pytest.raises(MonitorError):
            updates.call(manager, 'dns', 'POST', paths=invalid)
    with pytest.raises(MonitorError):
        updates.call(manager, 'dns', 'GET', paths=['rules/a.txt'])
    assert seen == []
    result, status = updates.call(manager, 'dns', 'POST', paths=['rules/a.txt', 'rules/b.txt'])
    assert status == 202 and result['task']['status'] == 'running'
    assert json.loads(seen[0]['data']) == {'paths': ['rules/a.txt', 'rules/b.txt']}


def test_unknown_target_is_reported_as_a_conflict(calls):
    _, replies = calls
    replies[:] = [(409, {'success': False, 'code': 'RULE_UPDATE_TARGET_UNKNOWN'})]
    with pytest.raises(MonitorError) as error:
        updates.call(Manager({**AGENT, 'version': '1.8.35-go'}), 'dns', 'POST', paths=['rules/a.txt'])
    assert error.value.code == 'RULE_UPDATE_TARGET_UNKNOWN' and error.value.status == 409
