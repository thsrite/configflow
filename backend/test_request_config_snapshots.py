"""Per-request config reads: validate a profile once and copy no whole document, with the same semantics."""

import copy

import pytest
from flask import Blueprint, Flask, jsonify

from backend.common import config as config_module
from backend.common.config_export import sanitize_external_payload
from backend.common.config_repository import ProfileNotFound, ProfileRepository
from backend.common.profile_context import install_profile_context, resolve_profile_id

REDACTED = "[REDACTED]"


@pytest.fixture
def repository(tmp_path):
    previous = config_module._repository
    repository = ProfileRepository(tmp_path)
    repository.create_profile({'id': 'other', 'name': 'Other'})
    config_module.set_repository(repository)
    yield repository
    config_module.set_repository(previous)
    config_module.reset_config_context()


@pytest.fixture
def app():
    return Flask(__name__)


def test_reads_and_saves_never_modify_the_committed_document(repository):
    repository.update_shared_transaction(lambda shared: shared.update(
        nodes=[{'id': 'n', 'name': 'Node'}],
        rule_library=[{'id': 'r', 'name': 'Rule', 'enabled': {'custom': []}}],
    ))
    repository.update_profile_fields('default', {
        'proxy_groups': [{'id': 'g', 'name': 'Group', 'type': 'select', 'manual_nodes': ['n']}],
        'rule_configs': [{'id': 'rule', 'itemType': 'ruleset', 'library_rule_id': 'r'}],
    })
    entry = repository._read_entry()
    before = copy.deepcopy(entry.document)

    config = config_module.get_config('default')
    config['proxy_groups'][0]['name'] = 'Changed'
    config['rule_configs'][0]['library_enabled']['custom'].append('Changed')
    shared = config_module.get_shared_config()
    shared['nodes'][0]['name'] = 'Changed'
    system = config_module.get_system_config()
    system['system_config']['server_domain'] = 'https://changed.example'
    config_module.save_config(config, 'default')
    config_module.save_shared_config(shared)
    config_module.save_system_config(system)

    assert repository._read_entry() is not entry
    assert entry.document == before
    assert repository.get_profile('default')['proxy_groups'][0]['name'] == 'Changed'
    assert repository.get_shared()['nodes'][0]['name'] == 'Changed'


def test_profile_baseline_is_the_read_view_and_keeps_concurrent_edits(repository):
    config = config_module.get_config('default')
    read_view = copy.deepcopy(config)
    config['mihomo']['custom_config'] = 'mixed-port: 7901'
    repository.update_profile_fields('default', {'proxy_groups': [{'id': 'g', 'name': 'Concurrent', 'type': 'select'}]})

    assert config_module._baseline('default') == read_view
    config_module.save_config(config, 'default')
    saved = repository.get_profile('default')
    assert saved['mihomo']['custom_config'] == 'mixed-port: 7901'
    assert saved['proxy_groups'] == [{'id': 'g', 'name': 'Concurrent', 'type': 'select'}]


def test_shared_baseline_keeps_concurrent_edits(repository):
    shared = config_module.get_shared_config()
    shared['nodes'] = [{'id': 'mine', 'name': 'Mine'}]
    repository.update_shared_transaction(lambda current: current.update(
        subscriptions=[{'id': 's', 'name': 'Concurrent', 'url': 'https://example.test/s'}]))
    config_module.save_shared_config(shared)
    saved = repository.get_shared()
    assert saved['nodes'] == [{'id': 'mine', 'name': 'Mine'}]
    assert [item['id'] for item in saved['subscriptions']] == ['s']


def test_transaction_reads_keep_an_eager_baseline(repository):
    seen = {}

    def update(profile):
        seen['view'] = config_module.get_config('default')
        seen['baseline'] = config_module._CONFIG_BASELINES.get()['default']
        profile['name'] = 'Changed in transaction'

    repository.update_profile_transaction('default', update)
    # The transaction's working copy may still change: never keep it as a baseline source.
    assert type(seen['baseline']) is dict
    assert seen['baseline'] == seen['view'] and seen['baseline'] is not seen['view']


def test_request_reuses_the_validated_profile(repository, app, monkeypatch):
    calls = []
    metadata = repository._profile_metadata
    monkeypatch.setattr(repository, '_profile_metadata', lambda profile_id: calls.append(profile_id) or metadata(profile_id))
    with app.test_request_context('/api/rules', headers={'X-ConfigFlow-Profile': 'other'}):
        config_module.reset_config_context()
        assert resolve_profile_id() == 'other'
        assert config_module.get_config()['profile_id'] == 'other'
        assert config_module.get_config('other') is config_module.get_config()
        assert resolve_profile_id('other') == 'other'
        assert resolve_profile_id('default') == 'default'
        assert resolve_profile_id('default') == 'default'
    assert calls == ['other', 'default']


def test_deleting_a_profile_in_the_same_request_is_noticed(repository, app):
    with app.test_request_context('/api/rules', headers={'X-ConfigFlow-Profile': 'other'}):
        config_module.reset_config_context()
        assert resolve_profile_id() == 'other'
        assert resolve_profile_id('other') == 'other'
        config_module.get_config()
        repository.delete_profile('other')
        with pytest.raises(ProfileNotFound):
            resolve_profile_id()
        with pytest.raises(ProfileNotFound):
            resolve_profile_id('other')
        with pytest.raises(ProfileNotFound):
            config_module.get_config()


def test_switching_the_default_in_the_same_request_is_noticed(repository, app):
    with app.test_request_context('/api/rules'):
        config_module.reset_config_context()
        assert resolve_profile_id() == 'default'
        repository.set_default_profile('other')
        assert resolve_profile_id() == 'other'


def test_another_request_does_not_reuse_the_validated_profile(repository, app):
    with app.test_request_context('/api/rules', headers={'X-ConfigFlow-Profile': 'other'}):
        assert resolve_profile_id() == 'other'
        with app.test_request_context('/api/rules'):
            assert resolve_profile_id() == 'default'
        assert resolve_profile_id() == 'other'


def test_full_request_validates_the_profile_once(repository, monkeypatch):
    calls = []
    metadata = repository._profile_metadata
    monkeypatch.setattr(repository, '_profile_metadata', lambda profile_id: calls.append(profile_id) or metadata(profile_id))
    app = Flask(__name__)
    rules = Blueprint('rules', __name__)

    @rules.get('/api/rules')
    def list_rules():
        config = config_module.get_config()
        assert config_module.get_config(resolve_profile_id()) is config
        return jsonify(profile_id=config['profile_id'])

    app.register_blueprint(rules)
    install_profile_context(app)
    response = app.test_client().get('/api/rules', headers={'X-ConfigFlow-Profile': 'other'})

    assert response.get_json() == {'profile_id': 'other'}
    assert response.headers['X-ConfigFlow-Profile'] == 'other'
    assert calls == ['other']


def test_global_request_follows_a_default_switch(repository):
    app = Flask(__name__)
    settings = Blueprint('settings', __name__)

    @settings.post('/api/settings/default-profile')
    def switch_default():
        before = resolve_profile_id()
        repository.set_default_profile('other')
        return jsonify(before=before, after=resolve_profile_id())

    app.register_blueprint(settings)
    install_profile_context(app)
    response = app.test_client().post('/api/settings/default-profile')

    assert response.get_json() == {'before': 'default', 'after': 'other'}


def test_transaction_working_copy_is_validated_without_the_memo(repository, app):
    with app.test_request_context('/api/rules', headers={'X-ConfigFlow-Profile': 'other'}):
        config_module.reset_config_context()
        assert resolve_profile_id('other') == 'other'
        with pytest.raises(RuntimeError):
            with repository._document(write=True) as document:
                del document['profiles']['other']
                # commit_count only grows after the commit: never answer from the memo here.
                with pytest.raises(ProfileNotFound):
                    resolve_profile_id('other')
                raise RuntimeError('abort')
        assert resolve_profile_id('other') == 'other'


def test_redaction_tokens_are_read_once_per_generation_without_copying_system(repository, monkeypatch):
    repository.update_system_transaction(lambda system: system['agents'].append(
        {'id': 'a', 'name': 'A', 'profile_id': 'default', 'token': 'agent-token-one'}))
    rule_token = repository.get_system()['system_config']['rule_proxy_token']
    monkeypatch.setattr(repository, 'get_system', lambda: pytest.fail('redaction must not copy system'))
    reads = []
    tokens_of = repository._sanitization_tokens_of
    monkeypatch.setattr(repository, '_sanitization_tokens_of', lambda system: reads.append(1) or tokens_of(system))

    for _ in range(3):
        assert sanitize_external_payload({'a': 'agent-token-one', 'r': f'x?token={rule_token}'}) == {
            'a': REDACTED, 'r': REDACTED}
    assert len(reads) == 1


def test_rotated_tokens_are_redacted_by_the_next_response(repository):
    repository.update_system_transaction(lambda system: system['agents'].append(
        {'id': 'a', 'name': 'A', 'profile_id': 'default', 'token': 'agent-token-one'}))
    old_rule_token = repository.get_system()['system_config']['rule_proxy_token']
    assert sanitize_external_payload(['agent-token-one', old_rule_token]) == [REDACTED, REDACTED]

    def rotate(system):
        system['agents'][0]['token'] = 'agent-token-two'
        settings = system['system_config']
        settings['retired_rule_proxy_tokens'] = [old_rule_token]
        settings['rule_proxy_token'] = 'rule-token-two'

    repository.update_system_transaction(rotate)
    assert sanitize_external_payload(['agent-token-two', 'rule-token-two', old_rule_token, 'agent-token-one']) == [
        REDACTED, REDACTED, REDACTED, 'agent-token-one']


def test_tokens_written_by_another_repository_are_redacted(repository):
    sanitize_external_payload({})
    other = ProfileRepository(repository.data_dir)
    other.update_system_transaction(lambda system: system['agents'].append(
        {'id': 'b', 'name': 'B', 'profile_id': 'default', 'token': 'external-agent-token'}))
    assert sanitize_external_payload({'token': 'external-agent-token'}) == {'token': REDACTED}


def test_head_changes_with_every_commit_and_only_then(repository, tmp_path):
    head = repository.head()
    repository.get_system(), repository.get_publish_snapshot('default'), repository.get_shared()
    assert repository.head() == head
    seen = {head}
    for write in (lambda: repository.update_shared_transaction(lambda shared: shared['rule_library'].append(
                      {'id': 'r', 'name': 'Rule', 'source_type': 'content', 'content': 'a.test'})),
                  lambda: repository.update_profile_fields('other', {'description': 'edited'}),
                  lambda: repository.update_system_transaction(lambda system: system['agents'].append(
                      {'id': 'agent', 'name': 'Agent', 'token': 'agent-token', 'profile_id': 'default'}))):
        write()
        seen.add(repository.head())
    assert len(seen) == 4
    # A write by another process (another repository object on the same files) is noticed too.
    other = ProfileRepository(tmp_path)
    other.update_profile_fields('default', {'description': 'elsewhere'})
    assert repository.head() == other.head() and repository.head() not in seen
    # Saving a value and saving the old one back are two generations, not the first one again.
    before = repository.head()
    repository.update_profile_fields('default', {'description': 'x'})
    repository.update_profile_fields('default', {'description': 'elsewhere'})
    assert repository.head() != before


def test_publish_snapshot_without_agents_only_leaves_the_agent_records_out(repository):
    repository.update_system_transaction(lambda system: system['agents'].append(
        {'id': 'agent', 'name': 'Agent', 'token': 'agent-token', 'profile_id': 'default',
         'deployments': {'d': {'status': 'succeeded'}}}))
    repository.update_shared_transaction(lambda shared: shared['rule_library'].append(
        {'id': 'r', 'name': 'Rule', 'source_type': 'content', 'content': 'a.test'}))
    repository.update_profile_fields('default', {'rule_configs': [{'id': 'set', 'itemType': 'ruleset', 'library_rule_id': 'r'}]})
    full, revision = repository.get_publish_snapshot('default')
    bare, bare_revision = repository.get_publish_snapshot('default', agents=False)
    assert [agent['id'] for agent in full['agents']] == ['agent'] and 'agents' not in bare
    assert {key: value for key, value in full.items() if key != 'agents'} == bare and revision == bare_revision
    assert bare['system_config'] == full['system_config'] and bare['rule_configs'][0]['library_rule_id'] == 'r'
