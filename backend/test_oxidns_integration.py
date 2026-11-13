"""OxiDNS product boundaries: profiles, archive delivery, auth and MCP."""
import copy
import io
import json
import tarfile
import zipfile

import pytest
import yaml
from flask import Flask

from backend.common import config as config_store
from backend.common.config_repository import ProfileRepository, ProfileInUse
from backend.common.oxidns_settings import get_default_oxidns_settings
from backend.mcp_server import tools
from backend.routes import register_blueprints


@pytest.fixture
def oxidns_app(tmp_path):
    previous = config_store._repository
    repository = ProfileRepository(tmp_path)
    repository.create_profile({'id': 'office', 'name': 'Office'})
    repository.save_system({'system_config': {'config_token': 'admin-token', 'server_domain': 'http://config.test'}})
    repository.save_shared({'rule_library': [{
        'id': 'lib', 'name': '国内规则 / 中文', 'source_type': 'content',
        'behavior': 'domain', 'content': 'DOMAIN-SUFFIX,example.test\n', 'enabled': True,
    }]})
    repository.save_profile('office', {
        'rule_configs': [{'id': 'rs', 'itemType': 'ruleset', 'library_rule_id': 'lib', 'policy': 'DIRECT'},
                         {'id': 'r1', 'itemType': 'rule', 'rule_type': 'DOMAIN', 'value': 'one.test', 'policy': 'DIRECT'}],
        'mosdns': {'direct_rules': ['r1'], 'direct_rulesets': ['rs']},
        'oxidns': {'direct_rules': ['r1'], 'direct_rulesets': ['rs'], 'worker_threads': 3},
    })
    config_store.set_repository(repository)
    app = Flask(__name__)
    app.config['TESTING'] = True
    register_blueprints(app)
    yield app, repository
    config_store.set_repository(previous)


def test_defaults_and_legacy_profile_without_oxidns_are_compatible(tmp_path):
    repository = ProfileRepository(tmp_path)
    document = repository.export_all()
    document['profiles']['default'].pop('oxidns')
    repository.import_all(document)
    reopened = ProfileRepository(tmp_path)
    assert reopened.get_profile('default')['mosdns'] == document['profiles']['default']['mosdns']
    defaults = get_default_oxidns_settings()
    assert (defaults['worker_threads'], defaults['cache_lazy_ttl'], defaults['probe_cache_size']) == (2, 3600, 2048)
    assert defaults['cache_dump_enabled'] is False


def test_settings_are_independent_and_clone_backup_restore_roundtrip(oxidns_app):
    app, repository = oxidns_app
    client = app.test_client()
    before_mos = copy.deepcopy(repository.get_profile('office')['mosdns'])
    settings = client.get('/api/oxidns/settings?profile=office').get_json()
    assert settings['worker_threads'] == 3
    settings.update(worker_threads=2, cache_lazy_ttl=400)
    result = client.post('/api/oxidns/settings?profile=office', json=settings)
    assert result.status_code == 200, result.get_json()
    assert repository.get_profile('office')['mosdns'] == before_mos
    assert client.get('/api/oxidns/settings').get_json()['cache_lazy_ttl'] == 3600
    repository.create_profile({'id': 'copy', 'name': 'Copy'}, clone_from='office')
    assert repository.get_profile('copy')['oxidns'] == repository.get_profile('office')['oxidns']
    backup = repository.export_all()
    repository.save_profile('office', {'oxidns': {'worker_threads': 5}})
    repository.import_all(backup)
    assert repository.get_profile('office')['oxidns']['worker_threads'] == 2


def test_authenticated_draft_render_is_profile_scoped_lossless_and_does_not_save(oxidns_app):
    app, repository = oxidns_app
    client = app.test_client()
    before = repository.get_profile('office')
    settings = client.get('/api/oxidns/settings?profile=office').get_json()
    settings['worker_threads'] = 7
    settings['api'].update(enabled=True, username='draft', password='draft-secret')
    settings['rule_update']['enabled'] = True
    response = client.post('/api/profiles/office/oxidns/render', json={'settings': settings})
    assert response.status_code == 200, response.get_json()
    document = yaml.safe_load(response.get_json()['config'])
    assert document['runtime']['worker_threads'] == 7
    assert document['api']['http']['auth']['password'] == 'draft-secret'
    assert any(plugin['type'] == 'download' for plugin in document['plugins'])
    assert '[REDACTED]' not in response.get_json()['config']
    assert response.headers['Cache-Control'] == 'no-store'
    assert repository.get_profile('office') == before
    assert client.post('/api/oxidns/render', json={'settings': []}).status_code == 400
    # The rendered template contains authenticated rule callbacks. Editing and
    # saving it must not replace its entire native YAML string with [REDACTED].
    settings.update(config_mode='native', native_config=response.get_json()['config'],
                    additional_files=[{'path': 'private-notes.txt', 'encoding': 'text',
                                       'content': repository.get_compat_config('office')['system_config']['rule_proxy_token']}])
    saved = client.post('/api/profiles/office/oxidns/settings', json=settings)
    assert saved.get_json()['settings']['native_config'] == settings['native_config']
    loaded = client.get('/api/profiles/office/oxidns/settings').get_json()
    assert loaded['native_config'] == settings['native_config']
    assert loaded['additional_files'] == settings['additional_files']
    public_preview = client.post('/api/profiles/office/generate/oxidns/preview', json={}).get_json()['content']
    assert repository.get_compat_config('office')['system_config']['rule_proxy_token'] not in public_preview


def test_native_settings_files_clone_and_download_roundtrip(oxidns_app):
    app, repository = oxidns_app
    client = app.test_client()
    native = {'runtime': {'worker_threads': 5}, 'plugins': [
        {'tag': 'hosts', 'type': 'hosts', 'args': {'files': ['rules/native.txt']}}]}
    settings = {'config_mode': 'native', 'native_config': yaml.safe_dump(native),
                'additional_files': [{'path': 'rules/native.txt', 'encoding': 'text', 'content': 'full:example.test 192.0.2.1\n'}]}
    saved = client.post('/api/profiles/office/oxidns/settings', json=settings)
    assert saved.status_code == 200, saved.get_json()
    result = client.post('/api/profiles/office/generate/oxidns', json={})
    assert result.status_code == 200
    with zipfile.ZipFile(io.BytesIO(result.data)) as archive:
        assert yaml.safe_load(archive.read('config.yaml')) == native
        assert archive.read('rules/native.txt') == b'full:example.test 192.0.2.1\n'
    repository.create_profile({'id': 'native-copy', 'name': 'Copy'}, clone_from='office')
    assert repository.get_profile('native-copy')['oxidns'] == repository.get_profile('office')['oxidns']


@pytest.mark.parametrize('kind', ['http_server', 'quic_server', 'udp_server', 'tcp_server', 'forward'])
def test_incomplete_native_plugin_can_be_saved_as_draft_but_not_rendered(oxidns_app, kind):
    app, repository = oxidns_app
    client = app.test_client()
    before = repository.get_profile('office')
    settings = {'config_mode': 'native', 'native_config': yaml.safe_dump({
        'plugins': [{'tag': 'unfinished', 'type': kind, 'args': {}}],
    })}
    saved = client.post('/api/profiles/office/oxidns/settings', json=settings)
    assert saved.status_code == 200, saved.get_json()
    assert saved.get_json()['draft'] is True
    assert repository.get_profile('office')['oxidns']['native_config'] == settings['native_config']
    assert repository.get_profile('office')['mosdns'] == before['mosdns']
    persisted = copy.deepcopy(repository.get_profile('office'))
    response = client.post('/api/profiles/office/oxidns/render', json={'settings': settings})
    assert response.status_code == 400, response.get_json()
    assert 'plugins[0].args.' in response.get_json()['message']
    assert 'unfinished' not in response.get_json()['message']
    assert repository.get_profile('office') == persisted


def test_previously_saved_incomplete_plugin_remains_readable_and_can_be_repaired(oxidns_app):
    app, repository = oxidns_app
    client = app.test_client()
    legacy = {'config_mode': 'native', 'native_config': yaml.safe_dump({
        'plugins': [{'tag': 'unfinished', 'type': 'http_server', 'args': {}},
                    {'tag': 'reverse', 'type': 'reverse_lookup', 'args': {}}],
    })}
    repository.save_profile('office', {'oxidns': legacy})
    response = client.get('/api/profiles/office/oxidns/settings')
    assert response.status_code == 200, response.get_json()
    settings = response.get_json()
    assert settings['native_config'] == legacy['native_config']
    for suffix in ('', '/preview'):
        blocked = client.post(f'/api/profiles/office/generate/oxidns{suffix}', json={})
        assert blocked.status_code == 400, blocked.get_json()
        assert 'plugins[0].args.listen' in blocked.get_json()['message']
        assert 'unfinished' not in blocked.get_json()['message']
    blocked = client.get('/api/config/office/oxidns?token=admin-token')
    assert blocked.status_code == 400, blocked.get_json()
    assert 'plugins[0].args.listen' in blocked.get_json()['message']
    assert 'unfinished' not in blocked.get_json()['message']
    document = yaml.safe_load(settings['native_config'])
    document['plugins'][0]['args'] = {'listen': ':8053', 'entries': []}
    settings['native_config'] = yaml.safe_dump(document)
    response = client.post('/api/profiles/office/oxidns/settings', json=settings)
    assert response.status_code == 200, response.get_json()
    assert yaml.safe_load(repository.get_profile('office')['oxidns']['native_config']) == document
    repaired = client.post('/api/profiles/office/generate/oxidns/preview', json={})
    assert repaired.status_code == 200, repaired.get_json()
    assert yaml.safe_load(repaired.get_json()['content']) == document


def test_preview_subscription_and_complete_zip_match_selected_profile(oxidns_app):
    app, repository = oxidns_app
    client = app.test_client()
    preview = client.post('/api/profiles/office/generate/oxidns/preview', json={})
    assert preview.status_code == 200, preview.get_json()
    public = client.get('/api/config/office/oxidns?token=admin-token', headers={'X-ConfigFlow-Profile': 'missing'})
    assert public.status_code == 200
    from backend.common.config_export import sanitize_external_payload
    # Manual update URLs now exist even with scheduling off. The public config
    # is executable, while the human-readable preview still redacts credentials.
    assert sanitize_external_payload(yaml.safe_load(public.get_data())) == yaml.safe_load(preview.get_json()['content'])
    assert yaml.safe_load(public.get_data())['runtime']['worker_threads'] == 3
    download = client.post('/api/profiles/office/generate/oxidns', json={})
    assert download.status_code == 200, download.get_data(as_text=True)[:100] if download.is_json else ''
    archive = zipfile.ZipFile(io.BytesIO(download.data))
    config_name = next(name for name in archive.namelist() if name.endswith('config.yaml'))
    config = yaml.safe_load(archive.read(config_name))
    providers = [plugin for plugin in config['plugins'] if plugin['type'] in ('domain_set', 'ip_set', 'hosts', 'adguard_rule')]
    files = [path.removeprefix('./') for plugin in providers for path in plugin.get('args', {}).get('files', [])]
    assert files, 'Selected shared and inline rules must be materialized'
    assert set(files) <= set(archive.namelist())
    assert b'example.test' in b'\n'.join(archive.read(path) for path in files)
    assert repository.get_profile('default')['oxidns']['direct_rulesets'] == []


def test_rule_deletion_atomically_cleans_both_targets(oxidns_app):
    app, repository = oxidns_app
    client = app.test_client()
    for path in ('/api/rules/r1', '/api/rule-sets/rs'):
        response = client.delete(path, headers={'X-ConfigFlow-Profile': 'office'})
        assert response.status_code == 200, response.get_json()
    profile = repository.get_profile('office')
    for engine in ('mosdns', 'oxidns'):
        assert profile[engine]['direct_rules'] == []
        assert profile[engine]['direct_rulesets'] == []


def test_legacy_native_manual_update_is_prepared_for_render_archive_and_agent_without_timer(oxidns_app):
    from backend.common.oxidns_editor import prepare_editor
    from backend.routes.agents import _prepare_transactional_content
    app, repository = oxidns_app
    config = repository.get_compat_config('office')
    settings = prepare_editor(config['oxidns'], config, 'http://config.test')
    legacy = yaml.safe_load(settings['native_config'])
    legacy['plugins'] = [item for item in legacy['plugins'] if item['tag'] not in
                         ('rules_download', 'rules_reload', 'rules_refresh', 'rules_cron')]
    settings['native_config'] = '# existing native configuration\n' + yaml.safe_dump(legacy, sort_keys=False)
    repository.save_profile('office', {'oxidns': settings})
    before = copy.deepcopy(repository.get_profile('office'))
    client = app.test_client()
    rendered = client.post('/api/profiles/office/oxidns/render', json={'settings': settings})
    assert rendered.status_code == 200, rendered.get_json()
    expected = yaml.safe_load(rendered.get_json()['config'])
    plugins = {item['tag']: item for item in expected['plugins']}
    assert 'rules_cron' not in plugins
    assert plugins['rules_download']['args']['downloads']
    assert plugins['rules_reload']['args']
    assert plugins['rules_refresh']['args'] == [{'exec': '$rules_download'}, {'exec': '$rules_reload'}]
    assert [item for item in expected['plugins'] if item['tag'] not in
            ('rules_download', 'rules_reload', 'rules_refresh')] == legacy['plugins']
    download = client.post('/api/profiles/office/generate/oxidns', json={})
    assert download.status_code == 200
    with zipfile.ZipFile(io.BytesIO(download.data)) as archive:
        assert yaml.safe_load(archive.read('config.yaml')) == expected
    agent = {'id': 'manual-update-fixture', 'service_type': 'oxidns', 'profile_id': 'office'}
    prepared = _prepare_transactional_content(agent, repository.get_compat_config('office'), base_url='http://config.test')
    assert yaml.safe_load(prepared['config_content']) == expected
    with tarfile.open(fileobj=io.BytesIO(prepared['bundle'].archive), mode='r:gz') as archive:
        # Agent delivery also adds its existing local DNS watchdog listener.
        deployed = yaml.safe_load(archive.extractfile('files/config.yaml').read())
        delivered_plugins = {item['tag']: item for item in deployed['plugins']}
        assert 'rules_cron' not in delivered_plugins
        for tag in ('rules_download', 'rules_reload', 'rules_refresh'):
            assert delivered_plugins[tag] == plugins[tag]
    assert repository.get_profile('office') == before


def test_invalid_rule_selection_cannot_be_persisted(oxidns_app):
    _, repository = oxidns_app
    with pytest.raises(ProfileInUse, match='OxiDNS'):
        repository.save_profile('office', {'oxidns': {'direct_rulesets': ['missing']}})


def test_desensitized_export_removes_nested_oxidns_credentials(oxidns_app):
    _, repository = oxidns_app
    settings = {'api': {'username': 'private-user', 'password': 'private-api-password'},
                'ros_route': {'args': {'password': 'private-router-password'}},
                'local_dns': '- addr: https://user:private-dns-password@dns.test/query',
                'custom_config': 'password: private-yaml-password',
                'adguard': {'urls': ['https://rules.test/list?token=private-feed-token']},
                'rule_update': {'args': {'socks5': 'private-proxy-password'}}}
    repository.save_profile('office', {'oxidns': settings})
    full = repository.export_all()
    redacted = repository.export_all(desensitize=True)
    exported = json.dumps(redacted)
    for secret in ('private-user', 'private-api-password', 'private-router-password',
                   'private-dns-password', 'private-yaml-password', 'private-proxy-password', 'private-feed-token'):
        assert secret not in exported
    assert full['profiles']['office']['oxidns']['api']['password'] == 'private-api-password'


def test_mcp_deep_merge_keeps_other_feature_parameters_and_generates_zip(oxidns_app):
    app, repository = oxidns_app
    repository.save_profile('office', {'oxidns': {'ip_selector': {'args': {'top_n': 2}}}})
    with app.app_context():
        result = tools.call_tool('update_oxidns_settings', {'profile_id': 'office', 'data': {
            'worker_threads': 4, 'ip_selector': {'args': {'max_wait': 800}},
        }})
        assert result['success']
        settings = tools.call_tool('get_oxidns_settings', {'profile_id': 'office'})
        assert settings['ip_selector']['args']['top_n'] == 2
        assert settings['ip_selector']['args']['max_wait'] == 800
        generated = tools.call_tool('generate_config', {'profile_id': 'office', 'target': 'oxidns'})
        assert generated['success'] and generated['saved'] is False
    assert repository.get_profile('default')['oxidns']['worker_threads'] == 2


def test_custom_config_is_profile_scoped_and_bad_request_rejected(oxidns_app):
    app, repository = oxidns_app
    client = app.test_client()
    response = client.post('/api/custom-config/oxidns?profile=office', json={'config': 'network: {}\n'})
    assert response.status_code == 200
    assert client.get('/api/custom-config/oxidns?profile=office').get_json() == {'config': 'network: {}\n'}
    assert client.get('/api/custom-config/oxidns').get_json() == {'config': ''}
    assert client.post('/api/custom-config/oxidns', json={'config': []}).status_code == 400


def test_public_subscription_rejects_missing_and_invalid_auth(oxidns_app):
    app, _ = oxidns_app
    client = app.test_client()
    for suffix in ('', '?token=wrong'):
        assert client.get('/api/config/office/oxidns' + suffix).status_code == 401
    assert client.get('/api/config/office/oxidns?token=admin-token').status_code == 200


def test_subscription_token_works_with_global_login_wall_enabled(oxidns_app, monkeypatch):
    from backend.common import auth as auth_utils
    from backend.routes.auth import setup_before_request

    app, _ = oxidns_app
    monkeypatch.setattr(auth_utils, 'ADMIN_USERNAME', 'test-admin')
    monkeypatch.setattr(auth_utils, 'ADMIN_PASSWORD', 'test-password')
    setup_before_request(app)
    client = app.test_client()
    for path in ('/api/config/oxidns', '/api/config/office/oxidns'):
        assert client.get(path).status_code == 401
        assert client.get(path + '?token=invalid').status_code == 401
        assert client.get(path + '?token=admin-token').status_code == 200
    assert client.get('/api/oxidns/settings?token=admin-token').status_code == 401
    for path in ('/api/oxidns/render', '/api/profiles/office/oxidns/render'):
        assert client.post(path + '?token=admin-token', json={'settings': {}}).status_code == 401
        response = client.post(path, json={'settings': {}}, headers={
            'Authorization': 'Bearer ' + auth_utils.generate_token('test-admin')})
        assert response.status_code == 200


@pytest.mark.parametrize('scheduled', [False, True])
def test_rule_update_preview_redacts_only_capability_urls_and_full_download_keeps_them(oxidns_app, scheduled):
    app, repository = oxidns_app
    profile = repository.get_profile('office')
    profile['oxidns']['rule_update'] = {'enabled': scheduled}
    repository.save_profile('office', profile)
    config = repository.get_compat_config('office')
    private_token = config['system_config']['rule_proxy_token']
    client = app.test_client()
    response = client.post('/api/profiles/office/generate/oxidns/preview', json={})
    assert response.status_code == 200
    preview = response.get_json()['content']
    assert private_token not in preview
    parsed = yaml.safe_load(preview)
    assert parsed['runtime']['worker_threads'] == 3
    downloader = next(plugin for plugin in parsed['plugins'] if plugin['tag'] == 'rules_download')
    assert downloader['args']['downloads'][0]['url'] == '[REDACTED]'
    with app.app_context():
        mcp = tools.call_tool('preview_config', {'profile_id': 'office', 'target': 'oxidns'})
    assert private_token not in mcp['content']
    assert yaml.safe_load(mcp['content'])['runtime']['worker_threads'] == 3
    full = client.post('/api/profiles/office/generate/oxidns', json={})
    assert full.status_code == 200
    with zipfile.ZipFile(io.BytesIO(full.data)) as archive:
        assert private_token in archive.read('config.yaml').decode()
