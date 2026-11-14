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
    env.beat, env.manager, env.agent, env.repository = beat, manager, agent, repository
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
