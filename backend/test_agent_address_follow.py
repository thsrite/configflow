"""A renumbered Agent is followed only after it is reached at the new address; HA members are only reported."""
import pytest
from flask import Flask

from backend import notifications
from backend.agents import address_follow
from backend.common import config as config_module
from backend.common.agent_manager import init_agent_manager
from backend.common.config_repository import ProfileRepository
from backend.routes import register_blueprints

OLD, NEW = '192.0.2.2', '192.0.2.11'


class Inline:
    """Run the background check inside the request so its outcome can be asserted."""
    def __init__(self, target, args, **_):
        self.target, self.args = target, args

    def start(self):
        self.target(*self.args)


@pytest.fixture
def env(tmp_path, monkeypatch):
    previous = config_module._repository
    repository = ProfileRepository(tmp_path)
    config_module.set_repository(repository)
    manager = init_agent_manager()
    agent = manager.register_agent({'name': 'dns', 'host': OLD, 'port': 8082, 'service_type': 'oxidns'})
    app = Flask(__name__)
    register_blueprints(app)
    client = app.test_client()
    reached = []
    monkeypatch.setattr(address_follow.threading, 'Thread', Inline)
    monkeypatch.setattr(address_follow, '_reaches', lambda record, host: reached.append((record['token'], host)) or env.reachable)
    address_follow._attempts.clear(); address_follow._reported.clear()

    def beat(**payload):
        return client.post(f"/api/agents/{agent['id']}/heartbeat", json={'version': '1.8.60-go', **payload},
                           headers={'Authorization': 'Bearer ' + agent['token']})

    env.reachable, env.reached = True, reached
    env.beat, env.manager, env.agent, env.repository, env.client = beat, manager, agent, repository, client
    env.host = lambda: manager.get_agent_by_id(agent['id'])['host']
    env.notes = lambda source: [item for item in notifications.snapshot() if item['source'] == source]
    yield env
    config_module.set_repository(previous)


def test_reply_announces_the_field_and_an_unchanged_or_absent_address_does_nothing(env):
    reply = env.beat()
    assert reply.status_code == 200 and reply.get_json()['reads_host'] is True
    assert env.beat(host=OLD).status_code == 200
    assert env.host() == OLD and env.reached == []
    assert notifications.snapshot() == []
    # Fields the panel does not know are still refused, as before.
    assert env.beat(address=NEW).status_code == 400


def test_record_follows_an_address_reached_with_the_agents_own_token(env):
    before = env.repository.get_system()['_revision']
    assert env.beat(host=NEW).status_code == 200
    assert env.host() == NEW
    assert env.reached == [(env.agent['token'], NEW)]
    notice, = env.notes(notifications.NOTICE_SOURCE)
    assert OLD in notice['message'] and NEW in notice['message'] and '上游路由' in notice['message']
    assert notice['target']['query'] == {'agent': env.agent['id']}
    # Later heartbeats from the new address change nothing more.
    revision = env.repository.get_system()['_revision']
    assert revision > before
    env.beat(host=NEW); env.beat(host=NEW)
    assert env.repository.get_system()['_revision'] == revision and len(env.reached) == 1


def test_unreachable_address_is_reported_not_adopted_and_retried_later(env, monkeypatch):
    env.reachable = False
    env.beat(host=NEW); env.beat(host=NEW)
    assert env.host() == OLD and len(env.reached) == 1          # one attempt per retry window
    note, = env.notes(notifications.AGENT_ADDRESS_SOURCE)
    assert NEW in note['title'] and '连不上' in note['status_label'] and '自动更新' in note['message']
    monkeypatch.setattr(address_follow, 'RETRY_SECONDS', 0)
    env.reachable = True
    env.beat(host=NEW)
    assert env.host() == NEW
    assert env.notes(notifications.AGENT_ADDRESS_SOURCE) == []   # the warning is withdrawn
    assert len(env.notes(notifications.NOTICE_SOURCE)) == 1


def test_high_availability_member_is_only_reported_with_the_steps(env, monkeypatch):
    def enable(system):
        next(item for item in system['agents'] if item['id'] == env.agent['id'])['oxidns_ha'] = {'enabled': True, 'vip': '192.0.2.53'}
    env.repository.update_system_transaction(enable)
    env.beat(host=NEW)
    assert env.host() == OLD and env.reached == []                # not even probed: the record must not move
    note, = env.notes(notifications.AGENT_ADDRESS_SOURCE)
    assert '高可用' in note['status_label']
    for step in ('停用 DNS 高可用', '重新配对', '发布上游路由'):
        assert step in note['message']
    # After HA is disabled the same report is acted on at once, without waiting for the retry window.
    def disable(system):
        del next(item for item in system['agents'] if item['id'] == env.agent['id'])['oxidns_ha']
    env.repository.update_system_transaction(disable)
    env.beat(host=NEW)
    assert env.host() == NEW and env.notes(notifications.AGENT_ADDRESS_SOURCE) == []


@pytest.mark.parametrize('value', ['172.17.0.2', '172.31.255.1', '127.0.0.1', '169.254.1.1', '0.0.0.0', '224.0.0.1',
                                   'fe80::1', 'dns.example', '192.0.2.11:8082', ''])
def test_addresses_the_panel_cannot_use_are_ignored(env, value):
    assert env.beat(host=value).status_code == 200
    assert env.host() == OLD and env.reached == [] and notifications.snapshot() == []


def test_address_already_used_by_a_same_named_record_is_not_taken(env):
    env.manager.register_agent({'name': 'dns', 'host': NEW, 'port': 8082, 'service_type': 'oxidns'})
    env.beat(host=NEW)
    assert env.host() == OLD and env.reached == []


def test_oversized_or_non_text_address_is_rejected(env):
    assert env.beat(host='1' * 65).status_code == 400
    assert env.beat(host=['192.0.2.11']).status_code == 400
    assert env.host() == OLD


def test_the_new_address_is_probed_with_the_agents_token_on_an_endpoint_the_go_agent_has(monkeypatch):
    calls = []

    class Reply:
        status_code = 200

    def get(url, headers=None, timeout=None):
        calls.append((url, headers, timeout))
        return Reply()

    monkeypatch.setattr(address_follow.requests, 'get', get)
    record = {'port': 8082, 'token': 'agent-token'}
    assert address_follow._reaches(record, NEW) is True
    assert calls == [(f'http://{NEW}:8082/api/capabilities', {'Authorization': 'Bearer agent-token'}, address_follow.VERIFY_TIMEOUT)]
    Reply.status_code = 401          # another Agent, or one that lost its identity
    assert address_follow._reaches(record, NEW) is False
    monkeypatch.setattr(address_follow.requests, 'get', lambda *a, **k: (_ for _ in ()).throw(
        address_follow.requests.exceptions.ConnectTimeout()))
    assert address_follow._reaches(record, NEW) is False


def test_the_agent_is_told_once_the_record_holds_the_address_it_reports(env):
    # The record has not followed yet when this reply is built: no confirmation.
    assert 'confirmed_host' not in env.beat(host=NEW).get_json()
    assert env.host() == NEW
    assert env.beat(host=NEW).get_json()['confirmed_host'] == NEW
    # Nothing reported (bridge container, older Agent): nothing to confirm.
    assert 'confirmed_host' not in env.beat().get_json()


def test_an_address_the_panel_did_not_adopt_is_never_confirmed(env):
    env.reachable = False
    env.beat(host=NEW)
    assert 'confirmed_host' not in env.beat(host=NEW).get_json()


class Device:
    """The Agent's /api/agent-address as seen from the panel."""
    def __init__(self, monkeypatch, addresses=(OLD, NEW), configured='', writable=True, status=200):
        self.addresses, self.configured, self.writable, self.status = list(addresses), configured, writable, status
        self.calls, self.down = [], set()
        monkeypatch.setattr(address_follow, '_ask', self.ask)

    def ask(self, agent, host, method='GET', body=None):
        self.calls.append((method, host, body))
        if host in self.down or host not in self.addresses:
            return None, {}
        if self.status != 200:
            return self.status, {}
        if method == 'GET':
            return 200, {'success': True, 'configured': self.configured, 'addresses': self.addresses, 'writable': self.writable}
        if not self.writable:
            return 409, {'success': False, 'message': 'container'}
        if body['host'] not in self.addresses:
            return 400, {'success': False, 'message': '这台设备没有地址 ' + body['host']}
        self.configured = body['host']
        return 200, {'success': True, 'host': body['host']}


def _address(env, method='get', **body):
    url = f"/api/agents/{env.agent['id']}/address"
    return env.client.get(url) if method == 'get' else env.client.put(url, json=body)


def test_dialog_lists_the_devices_own_addresses(env, monkeypatch):
    Device(monkeypatch, configured=OLD)
    data = _address(env).get_json()['data']
    assert data == {'host': OLD, 'port': 8082, 'mode': 'agent', 'message': '', 'addresses': [OLD, NEW],
                    'configured': OLD, 'reachable': True}


def test_manual_change_writes_the_device_first_then_the_record(env, monkeypatch):
    device = Device(monkeypatch, configured=OLD)
    reply = _address(env, 'put', host=f' {NEW} ')
    body = reply.get_json()
    assert reply.status_code == 200 and body['host'] == NEW and body['written'] is True and '上游路由' in body['message']
    assert env.host() == NEW and device.configured == NEW
    # Everything goes to the new address: the old one may already be gone.
    assert device.calls == [('GET', NEW, None), ('POST', NEW, {'host': NEW})]
    # The Agent now reports the pinned address; the record is not touched again.
    assert env.beat(host=NEW).get_json()['confirmed_host'] == NEW
    revision = env.repository.get_system()['_revision']
    env.beat(host=NEW); env.beat(host=NEW)
    assert env.repository.get_system()['_revision'] == revision and env.reached == []


def test_pinning_the_address_the_record_already_has_only_writes_the_device(env, monkeypatch):
    device = Device(monkeypatch)
    revision = env.repository.get_system()['_revision']
    body = _address(env, 'put', host=OLD).get_json()
    assert body['success'] and body['written'] is True and body['changed'] is False and '上游路由' not in body['message']
    assert device.configured == OLD and env.repository.get_system()['_revision'] == revision


def test_unreachable_or_foreign_address_changes_nothing(env, monkeypatch):
    device = Device(monkeypatch, addresses=(OLD,))
    reply = _address(env, 'put', host=NEW)
    assert reply.status_code == 502 and '连不上' in reply.get_json()['message']
    device.addresses, device.status = [OLD, NEW], 401        # something else answers there
    reply = _address(env, 'put', host=NEW)
    assert reply.status_code == 502 and '不是这台 Agent' in reply.get_json()['message']
    assert env.host() == OLD and device.configured == '' and all(call[0] == 'GET' for call in device.calls)


@pytest.mark.parametrize('value', ['172.17.0.2', '127.0.0.1', 'dns.example', '', None, 12])
def test_only_a_lan_ipv4_address_is_accepted(env, monkeypatch, value):
    device = Device(monkeypatch)
    assert _address(env, 'put', host=value).status_code == 400
    assert device.calls == [] and env.host() == OLD


def test_container_with_agent_ip_is_sent_to_its_environment(env, monkeypatch):
    device = Device(monkeypatch, configured=OLD, writable=False)
    data = _address(env).get_json()['data']
    assert data['mode'] == 'env' and 'AGENT_IP' in data['message'] and data['addresses'] == []
    reply = _address(env, 'put', host=NEW)
    assert reply.status_code == 409 and 'AGENT_IP' in reply.get_json()['message']
    assert env.host() == OLD and all(call[0] == 'GET' for call in device.calls)


def test_container_without_agent_ip_changes_only_the_record(env, monkeypatch):
    env.manager.update_agent(env.agent['id'], {'deployment_method': 'docker'})
    device = Device(monkeypatch, writable=False)
    assert _address(env).get_json()['data']['mode'] == 'record'
    body = _address(env, 'put', host=NEW).get_json()
    assert body['success'] and body['written'] is False and '只修改了面板记录' in body['message']
    assert env.host() == NEW and all(call[0] == 'GET' for call in device.calls)


def test_older_agent_is_verified_with_its_token_and_only_the_record_changes(env, monkeypatch):
    device = Device(monkeypatch, status=404)
    data = _address(env).get_json()['data']
    assert data['mode'] == 'record' and '版本较旧' in data['message']
    env.reachable = False
    assert _address(env, 'put', host=NEW).status_code == 502 and env.host() == OLD
    env.reachable = True
    body = _address(env, 'put', host=NEW).get_json()
    assert body['success'] and body['written'] is False and env.host() == NEW
    assert env.reached[-1] == (env.agent['token'], NEW) and all(call[0] == 'GET' for call in device.calls)


def test_high_availability_member_cannot_be_changed_here(env, monkeypatch):
    device = Device(monkeypatch)
    def enable(system):
        next(item for item in system['agents'] if item['id'] == env.agent['id'])['oxidns_ha'] = {'enabled': True, 'vip': '192.0.2.53'}
    env.repository.update_system_transaction(enable)
    data = _address(env).get_json()['data']
    assert data['mode'] == 'ha' and '停用 DNS 高可用' in data['message']
    reply = _address(env, 'put', host=NEW)
    assert reply.status_code == 409 and '高可用' in reply.get_json()['message']
    assert env.host() == OLD and device.calls == []


def test_dialog_still_opens_when_the_current_address_is_dead(env, monkeypatch):
    device = Device(monkeypatch, addresses=(NEW,))
    data = _address(env).get_json()['data']
    assert data['mode'] == 'unknown' and data['reachable'] is False and OLD in data['message']
    assert _address(env, 'put', host=NEW).get_json()['success'] and env.host() == NEW and device.configured == NEW


def test_manual_change_withdraws_the_pending_warning(env, monkeypatch):
    env.reachable = False
    env.beat(host=NEW)
    assert len(env.notes(notifications.AGENT_ADDRESS_SOURCE)) == 1
    Device(monkeypatch)
    assert _address(env, 'put', host=NEW).get_json()['success']
    assert env.notes(notifications.AGENT_ADDRESS_SOURCE) == []


def test_same_named_record_at_that_address_blocks_the_change(env, monkeypatch):
    device = Device(monkeypatch)
    env.manager.register_agent({'name': 'dns', 'host': NEW, 'port': 8082, 'service_type': 'oxidns'})
    assert _address(env, 'put', host=NEW).status_code == 409 and device.calls == [] and env.host() == OLD


def test_address_endpoint_requests_carry_the_agents_token(monkeypatch):
    calls = []

    class Reply:
        status_code = 200

        @staticmethod
        def json():
            return {'writable': True}

    def request(method, url, json=None, headers=None, timeout=None):
        calls.append((method, url, json, headers, timeout))
        return Reply()

    monkeypatch.setattr(address_follow.requests, 'request', request)
    record = {'port': 8082, 'token': 'agent-token'}
    assert address_follow._ask(record, NEW, 'POST', {'host': NEW}) == (200, {'writable': True})
    assert calls == [('POST', f'http://{NEW}:8082/api/agent-address', {'host': NEW},
                      {'Authorization': 'Bearer agent-token'}, address_follow.VERIFY_TIMEOUT)]
    monkeypatch.setattr(address_follow.requests, 'request', lambda *a, **k: (_ for _ in ()).throw(
        address_follow.requests.exceptions.ConnectionError()))
    assert address_follow._ask(record, NEW) == (None, {})
