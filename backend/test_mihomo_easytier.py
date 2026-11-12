"""EasyTier networking setting: validation, generation, target scope, API and exports."""
from copy import deepcopy

import pytest
import yaml
from flask import Flask

from backend.common import config as config_store
from backend.common.config_repository import ProfileRepository
from backend.common.mihomo_easytier import (
    FIELDS, EasyTierSettingsError, apply_easytier, normalize_settings, redact_settings,
)
from backend.converters.loon import generate_loon_config
from backend.converters.mihomo import generate_mihomo_config
from backend.converters.surge import generate_surge_config
from backend.routes import register_blueprints

BASE = 'mixed-port: 7890\ndns:\n  enable: true\n  nameserver: [https://223.5.5.5/dns-query]\n'


def _settings(**extra):
    value = {'enabled': True, 'name': 'EasyTier', 'routes': ['10.144.144.0/24', '10.200.0.5/24'],
             'options': {'network-name': 'home', 'network-secret': 's3cret-value',
                         'peers': ['tcp://203.0.113.10:11010'], 'udp': True}}
    value.update(extra)
    return value


def _config(easytier, rules=None):
    return {'profile_id': 'default', 'system_config': {'server_domain': 'https://config.test'},
            'rule_configs': rules if rules is not None else [
                {'id': 'm', 'itemType': 'rule', 'rule_type': 'MATCH', 'value': '', 'policy': 'DIRECT', 'enabled': True}],
            'rule_library': [], 'mihomo': {'custom_config': BASE, 'easytier': easytier}}


def _generate(config, **kwargs):
    return yaml.safe_load(generate_mihomo_config(config, preflight_providers=False, **kwargs))


def test_every_documented_outbound_option_is_accepted_and_written_verbatim():
    options = {
        'network-name': 'home', 'network-secret': 'secret', 'hostname': 'phone-1', 'ipv4': '10.144.144.10/24',
        'dhcp': False, 'peers': ['tcp://203.0.113.10:11010', 'udp://[2001:db8::1]:11010'], 'secure-mode': True,
        'local-private-key': 'cHJpdmF0ZQ==', 'local-public-key': 'cHVibGlj', 'listeners': ['tcp://0.0.0.0:11010'],
        'mapped-listeners': ['tcp://203.0.113.20:11010'], 'exit-nodes': ['10.144.144.1'],
        'proxy-networks': ['192.168.50.0/24'], 'instance-name': 'inst', 'state-dir': './easytier', 'udp': True,
        'accept-dns': False, 'enable-exit-node': False, 'enable-encryption': True, 'encryption-algorithm': 'aes-gcm',
        'private-mode': True, 'latency-first': True, 'disable-p2p': False, 'enable-kcp-proxy': True,
        'disable-kcp-input': False, 'enable-quic-proxy': True, 'disable-quic-input': False, 'mtu': 1380,
        'tld-dns-zone': 'et.net.', 'dialer-proxy': 'DIRECT', 'interface-name': 'eth0', 'routing-mark': 6666,
        'ip-version': 'ipv4-prefer',
    }
    assert set(options) | {'no-listener'} == {key for key, _ in FIELDS}
    proxy = _generate(_config(_settings(options=options)))['proxies'][-1]
    assert proxy == {'name': 'EasyTier', 'type': 'easytier', **options}
    # no server/port: the outbound has none, unlike flattened library nodes.
    assert 'server' not in proxy and 'port' not in proxy


def test_routes_go_before_every_rule_and_disabled_changes_nothing():
    document = _generate(_config(_settings()))
    assert document['rules'][:2] == ['IP-CIDR,10.144.144.0/24,EasyTier,no-resolve',
                                     'IP-CIDR,10.200.0.0/24,EasyTier,no-resolve']
    assert document['rules'][-1] == 'MATCH,DIRECT'
    off = _config(_settings(enabled=False))
    without = _config(None)
    without['mihomo'].pop('easytier')
    assert generate_mihomo_config(off, preflight_providers=False) == generate_mihomo_config(without, preflight_providers=False)


def test_keepalive_group_starts_the_lazy_instance_and_takes_the_routes():
    document = _generate(_config(_settings(keepalive={'enabled': True, 'url': 'http://10.144.144.2/', 'interval': 60})))
    group = document['proxy-groups'][-1]
    assert group == {'name': 'EasyTier 保活', 'type': 'fallback', 'proxies': ['EasyTier'],
                     'url': 'http://10.144.144.2/', 'interval': 60}
    assert document['rules'][0] == 'IP-CIDR,10.144.144.0/24,EasyTier 保活,no-resolve'


def test_magic_dns_routes_the_zone_and_keeps_a_user_policy():
    settings = _settings(magic_dns=True, options={**_settings()['options'], 'tld-dns-zone': 'lan.example.'})
    document = _generate(_config(settings))
    assert 'DOMAIN-SUFFIX,lan.example,EasyTier' in document['rules'][:3]
    assert document['dns']['nameserver-policy'] == {'+.lan.example': 'et://EasyTier'}
    config = _config(settings)
    config['mihomo']['custom_config'] = BASE + '  nameserver-policy:\n    +.lan.example: 192.0.2.53\n'
    assert _generate(config)['dns']['nameserver-policy'] == {'+.lan.example': '192.0.2.53'}


@pytest.mark.parametrize('apply_to, subscription, agent', [
    ('subscription', True, False), ('agent', False, True), ('all', True, True)])
def test_target_scope(apply_to, subscription, agent):
    config = _config(_settings(apply_to=apply_to))
    has = lambda doc: any(p.get('type') == 'easytier' for p in doc.get('proxies') or [])
    assert has(_generate(config)) is subscription
    assert has(_generate(config, for_agent=True)) is agent


def test_old_agent_core_is_refused_and_unknown_versions_pass():
    config = _config(_settings(apply_to='agent'))
    with pytest.raises(EasyTierSettingsError, match='v1.19.31'):
        _generate(config, for_agent=True, mihomo_core_version='v1.19.30')
    assert _generate(config, for_agent=True, mihomo_core_version='v1.19.34')['proxies'][-1]['type'] == 'easytier'
    assert _generate(config, for_agent=True, mihomo_core_version='alpha-abc')['proxies'][-1]['type'] == 'easytier'


def test_name_clash_and_unknown_dialer_are_refused():
    document = {'proxies': [{'name': 'EasyTier', 'type': 'ss'}], 'proxy-groups': [], 'rules': []}
    with pytest.raises(EasyTierSettingsError, match='重名'):
        apply_easytier(document, _config(_settings()))
    settings = _settings(options={**_settings()['options'], 'dialer-proxy': 'missing'})
    with pytest.raises(EasyTierSettingsError, match='dialer-proxy'):
        apply_easytier({'proxies': [], 'proxy-groups': [], 'rules': []}, _config(settings))


def test_user_rules_may_target_the_easytier_proxy():
    rules = [{'id': 'r', 'itemType': 'rule', 'rule_type': 'IP-CIDR', 'value': '10.9.0.0/16', 'policy': 'EasyTier',
              'enabled': True},
             {'id': 'm', 'itemType': 'rule', 'rule_type': 'MATCH', 'value': '', 'policy': 'DIRECT', 'enabled': True}]
    assert 'IP-CIDR,10.9.0.0/16,EasyTier' in _generate(_config(_settings(), rules))['rules']


def test_surge_and_loon_ignore_the_setting():
    config = _config(_settings(apply_to='all'))
    for generate in (generate_surge_config, generate_loon_config):
        assert 'easytier' not in generate(config).lower()


@pytest.mark.parametrize('change, text', [
    (lambda s: s['options'].pop('network-name'), 'network-name'),
    (lambda s: s['options'].pop('peers'), 'peers'),
    (lambda s: s['options'].update({'listeners': ['tcp://0.0.0.0:11010'], 'no-listener': True}), 'no-listener'),
    (lambda s: s['options'].update({'proxy-networks': ['192.168.0.0/24->10.200.0.0/24']}), '映射'),
    (lambda s: s.update(routes=['2001:db8::/64']), 'IPv4'),
    (lambda s: s['options'].update({'peers': ['203.0.113.10:11010']}), '格式不对'),
    (lambda s: s['options'].update({'mtu': 100}), 'mtu'),
    (lambda s: s['options'].update({'ip-version': 'v4'}), 'ip-version'),
    (lambda s: s['options'].update({'udp': 'yes'}), 'udp'),
    (lambda s: s['options'].update({'bogus': 1}), 'bogus'),
    (lambda s: s['options'].update({'exit-nodes': ['node-a']}), 'exit-nodes'),
    (lambda s: s['options'].update({'local-public-key': 'abc'}), 'local-private-key'),
    (lambda s: s.update(keepalive={'enabled': True, 'url': '', 'interval': 300}), '检测地址'),
    (lambda s: s.update(name='a,b'), '逗号'),
    (lambda s: s.update(apply_to='phones'), '下发范围'),
])
def test_invalid_settings_are_refused(change, text):
    value = deepcopy(_settings())
    change(value)
    with pytest.raises(EasyTierSettingsError, match=text):
        normalize_settings(value)


def test_listening_without_peers_is_allowed_and_blank_values_are_dropped():
    value = _settings(options={'network-name': 'home', 'listeners': ['tcp://0.0.0.0:11010'], 'hostname': ' ',
                               'peers': [], 'mtu': None})
    assert normalize_settings(value)['options'] == {'network-name': 'home', 'listeners': ['tcp://0.0.0.0:11010']}
    disabled = normalize_settings({'enabled': False, 'options': {}})
    assert disabled['enabled'] is False and disabled['options'] == {}


def test_redacted_copy_keeps_shape():
    value = normalize_settings(_settings(options={**_settings()['options'], 'local-private-key': 'cHJpdg=='}))
    redacted = redact_settings(value)
    assert redacted['options']['network-secret'] == '[REDACTED]'
    assert redacted['options']['local-private-key'] == '[REDACTED]'
    assert redacted['options']['network-name'] == 'home'
    assert value['options']['network-secret'] == 's3cret-value'


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


def test_api_round_trip_rejection_and_desensitized_export(app):
    client, repository = app
    data = client.get('/api/custom-config/mihomo?profile=office').json
    assert data['easytier']['enabled'] is False and data['easytier']['options'] == {'udp': True}
    config = data['config']
    saved = client.post('/api/custom-config/mihomo?profile=office', json={'config': config, 'easytier': _settings()})
    assert saved.status_code == 200, saved.json
    stored = repository.get_profile('office')['mihomo']['easytier']
    assert stored['options']['network-secret'] == 's3cret-value'
    assert client.get('/api/custom-config/mihomo?profile=office').json['easytier'] == stored

    bad = client.post('/api/custom-config/mihomo?profile=office',
                      json={'config': config, 'easytier': _settings(routes=['192.168.0.0/24->10.200.0.0/24'])})
    assert bad.status_code == 400 and 'EasyTier' in bad.json['message']
    assert repository.get_profile('office')['mihomo']['easytier'] == stored

    # Saving only the YAML keeps the EasyTier setting.
    assert client.post('/api/custom-config/mihomo?profile=office', json={'config': config}).status_code == 200
    assert repository.get_profile('office')['mihomo']['easytier'] == stored

    exported = repository.export_all(desensitize=True)
    text = yaml.safe_dump(exported, allow_unicode=True)
    assert 's3cret-value' not in text
    assert exported['profiles']['office']['mihomo']['easytier']['options']['network-name'] == 'home'
