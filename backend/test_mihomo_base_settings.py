"""The Mihomo base settings form's checks, held on the server for every save."""
import json
from copy import deepcopy

import pytest
import yaml
from flask import Flask

from backend.agents.manager import AgentManager
from backend.common import config as config_store
from backend.common.config_repository import ProfileRepository
from backend.common.gateway_onboarding import mihomo_dns_loop
from backend.common.mihomo_base_settings import BaseSettingsError, validate_base_settings
from backend.common.resource import get_backend_resource
from backend.routes import register_blueprints


def template():
    with open(get_backend_resource('config_template.json'), encoding='utf-8') as handle:
        return yaml.safe_load(json.load(handle)['mihomo']['custom_config'])


def check(change, previous=None, **kwargs):
    saved = template() if previous is None else previous
    document = deepcopy(saved)
    change(document)
    validate_base_settings(document, saved, **kwargs)


def rejects(change, text, **kwargs):
    with pytest.raises(BaseSettingsError) as error:
        check(change, **kwargs)
    assert text in str(error.value)


def test_unchanged_template_with_default_secret_can_still_be_saved():
    # The template's 123456 is pointed out on the page; it does not block other edits.
    check(lambda doc: doc.update({'log-level': 'info'}))
    check(lambda doc: None)


@pytest.mark.parametrize(('secret', 'text'), [
    ('short', '8–64'), ('has space 123', '8–64'), ('123456', '123456'), ('中文密码中文密码', '8–64'),
    (12345678, '必须是文本'),
])
def test_changed_secret_follows_the_wizard_rules(secret, text):
    rejects(lambda doc: doc.update(secret=secret), text, previous={**template(), 'secret': 'Old-Pass_2026'})


def test_valid_secret_and_loopback_controller_without_secret():
    check(lambda doc: doc.update(secret='Fine-Pass_2026'))
    check(lambda doc: (doc.update({'external-controller': '127.0.0.1:9090'}), doc.pop('secret')))
    check(lambda doc: (doc.update({'external-controller': '[::1]:9091'}), doc.update(secret='')))


def test_lan_controller_needs_a_valid_secret():
    rejects(lambda doc: doc.pop('secret'), '必须设置控制面板密码')
    # Moving the LAN listener re-checks the template password it would expose.
    rejects(lambda doc: doc.update({'external-controller': '0.0.0.0:9091'}), '123456')
    check(lambda doc: doc.update({'external-controller': '[::]:9091', 'secret': 'Fine-Pass_2026'}))


@pytest.mark.parametrize(('value', 'text'), [
    ('192.168.1.2:9090', '只能监听'), ('0.0.0.0', '地址:端口'), ('0.0.0.0:70000', '地址:端口'), (':abc', '地址:端口'),
])
def test_controller_listener_must_be_reachable_from_the_agent(value, text):
    rejects(lambda doc: doc.update({'external-controller': value, 'secret': 'Fine-Pass_2026'}), text)


def test_port_conflicts_between_inbounds_dns_controller_and_agents():
    rejects(lambda doc: doc.update({'socks-port': 7893}), '端口 7893 同时被 mixed-port 和 socks-port 使用')
    rejects(lambda doc: doc.update({'mixed-port': 1053}), 'dns.listen')
    rejects(lambda doc: doc.update({'mixed-port': 9090}), 'external-controller')
    rejects(lambda doc: doc.update({'mixed-port': 8080}), 'Mihomo Agent「box」的端口', reserved_ports={8080: 'Mihomo Agent「box」的端口'})
    check(lambda doc: doc.update({'mixed-port': 7895}), reserved_ports={8080: 'Agent'})
    # Removing optional inbounds is fine; an untouched conflict is not re-checked.
    check(lambda doc: [doc.pop(key) for key in ('port', 'socks-port', 'redir-port', 'tproxy-port')])
    clash = {**template(), 'socks-port': 7893}
    check(lambda doc: doc.update({'log-level': 'debug'}), previous=clash)


@pytest.mark.parametrize(('value', 'text'), [(0, '1–65535'), (70000, '1–65535'), ('7890', '1–65535'), (True, '1–65535')])
def test_port_type_and_range(value, text):
    rejects(lambda doc: doc.update({'mixed-port': value}), text)


def test_dns_upstreams_cannot_loop_through_this_host():
    rejects(lambda doc: doc['dns'].update(nameserver=['127.0.0.1']), '解析循环')
    rejects(lambda doc: doc['dns'].update({'direct-nameserver': ['system']}), 'dns.direct-nameserver')
    rejects(lambda doc: doc['dns']['nameserver-policy'].update({'+.lan': 'udp://localhost:53'}), 'nameserver-policy')
    rejects(lambda doc: doc['dns'].update(nameserver=['10.0.0.5']), '解析循环', local_addresses=['10.0.0.5'])
    check(lambda doc: doc['dns'].update(nameserver=['10.0.0.5:5353']), local_addresses=['10.0.0.5'])
    check(lambda doc: doc['dns'].update(nameserver=['https://dns.google/dns-query']))


def test_dns_requirements():
    rejects(lambda doc: doc['dns'].update(nameserver=[]), '至少填写一个')
    rejects(lambda doc: doc['dns'].pop('proxy-server-nameserver'), 'proxy-server-nameserver')
    rejects(lambda doc: doc['dns'].update(nameserver=['ok', '  ']), '不能有空项')
    rejects(lambda doc: doc['dns'].update({'respect-rules': 'true'}), 'true 或 false')


@pytest.mark.parametrize(('path', 'value', 'text'), [
    (('log-level',), 'verbose', 'silent / error'),
    (('find-process-mode',), 'sometimes', 'always / strict / off'),
    (('dns', 'fake-ip-filter-mode'), 'greylist', 'blacklist'),
    (('tun', 'stack'), 'lwip', 'system / gvisor / mixed'),
    (('global-client-fingerprint',), 'opera', 'chrome'),
    (('tun', 'mtu'), 100, '576–65535'),
    (('keep-alive-idle',), 0, '1–86400'),
    (('allow-lan',), 'yes', 'true 或 false'),
    (('profile', 'store-selected'), 1, 'true 或 false'),
    (('sniffer', 'skip-domain'), 'example.com', '文本列表'),
    (('bind-address',), 'lan', 'bind-address'),
    (('external-ui-url',), 'ftp://example.com/ui.zip', 'http(s)'),
    (('external-ui',), '', '非空文本'),
])
def test_enum_and_type_checks(path, value, text):
    def change(doc):
        target = doc
        for key in path[:-1]:
            target = target[key]
        target[path[-1]] = value
    rejects(change, text)


def test_yaml_spellings_generation_accepts():
    check(lambda doc: doc.update({'find-process-mode': False}))  # Unquoted off.
    check(lambda doc: doc.update({'global-client-fingerprint': 360}))
    check(lambda doc: doc['tun'].update(stack='gVisor', mtu=1500))
    check(lambda doc: doc.update({'bind-address': '::'}))
    check(lambda doc: doc['sniffer'].update({'skip-domain': None}))


def test_everything_is_checked_when_the_saved_base_is_unreadable():
    with pytest.raises(BaseSettingsError):
        validate_base_settings(template(), None)


def test_dns_loop_helper_keeps_the_gateway_wizard_rules():
    assert mihomo_dns_loop({'nameserver': ['https://223.5.5.5/dns-query']}) is None
    assert mihomo_dns_loop({'fallback': ['tcp://[::1]:53']}) == ('fallback', 'tcp://[::1]:53')
    assert mihomo_dns_loop({'nameserver-policy': {'a': ['system']}}) == ('nameserver-policy', 'system')
    assert mihomo_dns_loop({'nameserver': ['udp://bad:port']}) is None


@pytest.fixture
def app(tmp_path):
    previous = config_store._repository
    repository = ProfileRepository(tmp_path)
    repository.create_profile({'id': 'office', 'name': 'Office'})
    repository.save_shared({'rule_library': []})
    config_store.set_repository(repository)
    flask_app = Flask(__name__)
    flask_app.config['TESTING'] = True
    register_blueprints(flask_app)
    yield flask_app.test_client(), repository
    config_store.set_repository(previous)


def test_save_route_rejects_and_keeps_the_saved_base(app):
    client, repository = app
    base = yaml.safe_dump({**template(), 'secret': 'Fine-Pass_2026'}, allow_unicode=True, sort_keys=False)
    assert client.post('/api/custom-config/mihomo?profile=office', json={'config': base}).status_code == 200
    for bad, text in [(base.replace('socks-port: 7891', 'socks-port: 7893'), '端口 7893'),
                      (base.replace('Fine-Pass_2026', 'short'), '8–64')]:
        result = client.post('/api/custom-config/mihomo?profile=office', json={'config': bad})
        assert result.status_code == 400
        assert text in result.json['message']
        assert 'Fine-Pass_2026' not in result.json['message']
        assert repository.get_profile('office')['mihomo']['custom_config'] == base


def test_agent_ports_and_takeover_are_reported_and_enforced(app):
    client, repository = app
    manager = AgentManager(repository)  # Agents live in the repository the route reads.
    manager.register_agent({'name': 'box', 'host': '10.0.0.5', 'port': 7999, 'service_type': 'mihomo',
                            'profile_id': 'office', 'deployment_method': 'shell'})
    manager.register_agent({'name': 'dns', 'host': '10.0.0.5', 'port': 8082, 'service_type': 'oxidns',
                            'profile_id': 'office', 'deployment_method': 'shell'})
    manager.register_agent({'name': 'docker', 'host': '10.0.0.5', 'port': 7890, 'service_type': 'mihomo',
                            'profile_id': 'office', 'deployment_method': 'docker'})
    data = client.get('/api/custom-config/mihomo?profile=office').json
    assert data['base_settings'] == {
        'takeover': None, 'local_addresses': ['10.0.0.5'],
        'reserved_ports': [{'port': 7999, 'owner': 'Mihomo Agent「box」的端口'},
                           {'port': 8082, 'owner': 'OxiDNS Agent「dns」的端口'}]}
    result = client.post('/api/custom-config/mihomo?profile=office', json={'config': 'mixed-port: 8082\n'})
    assert result.status_code == 400 and 'OxiDNS Agent「dns」' in result.json['message']
    result = client.post('/api/custom-config/mihomo?profile=office',
                         json={'config': 'dns:\n  enable: true\n  nameserver: [10.0.0.5]\n'})
    assert result.status_code == 400 and '解析循环' in result.json['message']
