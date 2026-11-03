"""UDP a node cannot relay, and rules placed after MATCH, across Mihomo, Surge and Loon."""
import pytest
import yaml
from flask import Flask

from backend.agents.rule_sync_projection import rule_signature
from backend.common import config as config_module
from backend.common.config_repository import ProfileRepository
from backend.converters.loon import generate_loon_config
from backend.converters.mihomo import generate_mihomo_config
from backend.converters.surge import generate_surge_config
from backend.routes import register_blueprints


def _rule(ident, kind, value, policy='DIRECT', **extra):
    return {'id': ident, 'itemType': 'rule', 'rule_type': kind, 'value': value, 'policy': policy,
            'enabled': True, **extra}


def _config(rules, **clients):
    return {'profile_id': 'default', 'system_config': {'server_domain': 'https://config.test'},
            'rule_configs': rules, 'rule_library': [], **clients}


def _section(output, name):
    return output.split(f'[{name}]\n', 1)[1].split('\n\n[', 1)[0].strip().splitlines()


def _mihomo_rules(config):
    return yaml.safe_load(generate_mihomo_config(config, preflight_providers=False))['rules']


AFTER_MATCH = _rule('quic', 'AND', '((NETWORK,udp),(DST-PORT,443/3478))', 'REJECT')


@pytest.mark.parametrize('fallback, tail', [
    ('REJECT', ['MATCH,DIRECT', 'AND,((NETWORK,udp),(DST-PORT,443/3478)),REJECT', 'NETWORK,udp,REJECT']),
    ('DIRECT', ['MATCH,DIRECT', 'AND,((NETWORK,udp),(DST-PORT,443/3478)),REJECT']),
    (None, ['MATCH,DIRECT', 'AND,((NETWORK,udp),(DST-PORT,443/3478)),REJECT']),
])
def test_mihomo_rejects_skipped_udp_after_every_user_rule(fallback, tail):
    match = _rule('final', 'MATCH', '', **({'udp_fallback': fallback} if fallback else {}))
    rules = _mihomo_rules(_config([_rule('lan', 'DOMAIN', 'lan.test'), match, AFTER_MATCH]))
    assert rules[1:] == tail


def test_disabled_match_does_not_choose_the_fallback():
    match = _rule('final', 'MATCH', '', udp_fallback='REJECT', enabled=False)
    config = _config([_rule('lan', 'DOMAIN', 'lan.test'), match])
    assert 'NETWORK,udp,REJECT' not in _mihomo_rules(config)
    assert 'udp-policy-not-supported-behaviour' not in generate_surge_config(config)
    assert 'udp-fallback-mode = DIRECT' in generate_loon_config(config)


def test_surge_keeps_final_last_and_writes_the_native_fallback():
    config = _config([
        _rule('udp', 'NETWORK', 'udp', 'REJECT'),
        _rule('ntp', 'AND', '((NETWORK,udp),(DST-PORT,123))'),
        _rule('ports', 'DST-PORT', '443/3478', 'REJECT'),
        _rule('final', 'MATCH', '', udp_fallback='REJECT'),
        AFTER_MATCH,
    ])
    output = generate_surge_config(config)
    assert 'udp-policy-not-supported-behaviour = REJECT' in _section(output, 'General')
    assert _section(output, 'Rule') == [
        'PROTOCOL,UDP,REJECT',
        'AND,((PROTOCOL,UDP), (DEST-PORT,123)),DIRECT',
        '# Surge 不支持一条规则写多个端口，已忽略: DST-PORT,443/3478,REJECT',
        'FINAL,DIRECT',
        '# 仅 Mihomo 生效（位于 MATCH 之后，只匹配节点不支持 UDP 而跳过的连接），已忽略: '
        'AND,((NETWORK,udp),(DST-PORT,443/3478)),REJECT',
    ]


def test_surge_base_config_wins_over_the_option():
    config = _config([_rule('final', 'MATCH', '', udp_fallback='REJECT')],
                     surge={'custom_config': '[General]\nudp-policy-not-supported-behaviour = DIRECT\n'})
    general = _section(generate_surge_config(config), 'General')
    assert general == ['udp-policy-not-supported-behaviour = DIRECT']


def test_loon_rules_after_match_never_move_before_final():
    library = {'id': 'lib', 'name': 'Late', 'source_type': 'content', 'content': 'DOMAIN,late.test',
               'behavior': 'domain', 'url': '/rules/late'}
    config = _config([
        _rule('ntp', 'AND', '((NETWORK,udp),(DST-PORT,123))'),
        _rule('final', 'MATCH', '', udp_fallback='REJECT'),
        _rule('late', 'DOMAIN', 'late.test', 'REJECT'),
        {'id': 'set', 'itemType': 'ruleset', 'library_rule_id': 'lib', 'name': 'Late', 'url': '/rules/late',
         'policy': 'REJECT', 'enabled': True},
    ])
    config['rule_library'].append(library)
    output = generate_loon_config(config)
    assert 'udp-fallback-mode = REJECT' in _section(output, 'General')
    assert 'udp-fallback-mode = DIRECT' not in output
    rules = _section(output, 'Rule')
    assert rules[0] == 'AND,((PROTOCOL,UDP),(DEST-PORT,123)),DIRECT'
    assert rules[-1] == 'FINAL,DIRECT'
    assert rules[1].startswith('# 仅 Mihomo 生效') and rules[1].endswith('DOMAIN,late.test,REJECT')
    remote = _section(output, 'Remote Rule')
    assert remote == ['# 仅 Mihomo 生效（位于 MATCH 之后，只匹配节点不支持 UDP 而跳过的连接），已忽略: '
                      'RULE-SET,Late,REJECT']


def test_loon_custom_general_gets_the_option_unless_it_sets_one():
    rules = [_rule('final', 'MATCH', '', udp_fallback='REJECT'), _rule('port', 'DST-PORT', '443/3478', 'REJECT')]
    general = _section(generate_loon_config(_config(rules, loon={'custom_config': '[General]\nip-mode = ipv4-only'})),
                       'General')
    assert general == ['ip-mode = ipv4-only', 'udp-fallback-mode = REJECT']
    kept = _config(rules, loon={'custom_config': '[General]\nudp-fallback-mode = DIRECT'})
    assert _section(generate_loon_config(kept), 'General') == ['udp-fallback-mode = DIRECT']
    # Rules after MATCH are commented before the port-list check is reached.
    assert _section(generate_loon_config(kept), 'Rule')[0].startswith('# 仅 Mihomo 生效')


def test_loon_port_list_before_match_is_ignored_not_rewritten():
    output = generate_loon_config(_config([_rule('port', 'DST-PORT', '443/3478', 'REJECT'),
                                           _rule('final', 'MATCH', '')]))
    assert _section(output, 'Rule') == ['# Loon 不支持一条规则写多个端口，已忽略: DST-PORT,443/3478,REJECT',
                                        'FINAL,DIRECT']


def test_rule_sync_notices_a_changed_fallback():
    before = _config([_rule('final', 'MATCH', '')])
    after = _config([_rule('final', 'MATCH', '', udp_fallback='REJECT')])
    assert rule_signature(before, 'mihomo') != rule_signature(after, 'mihomo')


@pytest.fixture
def client(tmp_path):
    repository = ProfileRepository(tmp_path)
    repository.save_shared({'rule_library': []})
    repository.save_profile('default', {'proxy_groups': [], 'rule_configs': [
        _rule('final', 'MATCH', ''), _rule('lan', 'DOMAIN', 'lan.test'),
    ]})
    config_module.set_repository(repository)
    app = Flask(__name__)
    register_blueprints(app)
    yield app.test_client(), repository
    config_module.reset_config_context()


def _saved(repository, ident):
    return next(item for item in repository.get_profile('default')['rule_configs'] if item['id'] == ident)


def test_only_match_stores_a_valid_fallback(client):
    api, repository = client
    assert api.put('/api/rules/final', json={'udp_fallback': 'REJECT'}).status_code == 200
    assert _saved(repository, 'final')['udp_fallback'] == 'REJECT'
    assert api.put('/api/rules/final', json={'udp_fallback': 'PROXY'}).status_code == 400
    assert _saved(repository, 'final')['udp_fallback'] == 'REJECT'
    assert api.put('/api/rules/final', json={'udp_fallback': ''}).status_code == 200
    assert 'udp_fallback' not in _saved(repository, 'final')

    assert api.put('/api/rules/lan', json={'udp_fallback': 'REJECT'}).status_code == 200
    assert 'udp_fallback' not in _saved(repository, 'lan')
    api.put('/api/rules/final', json={'udp_fallback': 'DIRECT'})
    assert api.put('/api/rules/final', json={'rule_type': 'DOMAIN', 'value': 'x.test'}).status_code == 200
    assert 'udp_fallback' not in _saved(repository, 'final')


def test_new_match_keeps_its_fallback(client):
    api, repository = client
    response = api.post('/api/rules', json={'id': 'new', 'itemType': 'rule', 'rule_type': 'MATCH', 'value': '',
                                            'policy': 'DIRECT', 'udp_fallback': 'REJECT'})
    assert response.status_code == 200
    assert _saved(repository, 'new')['udp_fallback'] == 'REJECT'


NTP = 'AND,((NETWORK,udp),(DST-PORT,123)),DIRECT'


def test_ntp_direct_goes_first_in_every_client():
    config = _config([_rule('lan', 'DOMAIN', 'lan.test'), _rule('final', 'MATCH', '', ntp_direct=True)])
    assert _mihomo_rules(config)[:2] == [NTP, 'DOMAIN,lan.test,DIRECT']
    assert _section(generate_surge_config(config), 'Rule')[0] == 'AND,((PROTOCOL,UDP), (DEST-PORT,123)),DIRECT'
    assert _section(generate_loon_config(config), 'Rule')[0] == 'AND,((PROTOCOL,UDP),(DEST-PORT,123)),DIRECT'
    assert config['rule_configs'][0]['id'] == 'lan' and len(config['rule_configs']) == 2


@pytest.mark.parametrize('match', [_rule('final', 'MATCH', ''), _rule('final', 'MATCH', '', ntp_direct=True, enabled=False)])
def test_ntp_direct_is_off_unless_the_effective_match_enables_it(match):
    config = _config([_rule('lan', 'DOMAIN', 'lan.test'), match])
    assert NTP not in _mihomo_rules(config)
    assert 'DEST-PORT,123' not in generate_surge_config(config) + generate_loon_config(config)


def test_rule_sync_notices_a_changed_ntp_switch():
    before = _config([_rule('final', 'MATCH', '')])
    after = _config([_rule('final', 'MATCH', '', ntp_direct=True)])
    assert rule_signature(before, 'mihomo') != rule_signature(after, 'mihomo')


def test_only_match_stores_the_ntp_switch(client):
    api, repository = client
    assert api.put('/api/rules/final', json={'ntp_direct': True}).status_code == 200
    assert _saved(repository, 'final')['ntp_direct'] is True
    assert api.put('/api/rules/final', json={'ntp_direct': 'yes'}).status_code == 400
    assert api.put('/api/rules/final', json={'ntp_direct': False}).status_code == 200
    assert 'ntp_direct' not in _saved(repository, 'final')
    assert api.put('/api/rules/lan', json={'ntp_direct': True}).status_code == 200
    assert 'ntp_direct' not in _saved(repository, 'lan')


def test_default_templates_give_ntp_servers_real_addresses():
    import json
    from backend.common.resource import get_backend_resource
    from backend.converters.mihomo import _default_mihomo_template
    with open(get_backend_resource('config_template.json'), encoding='utf-8') as handle:
        template = yaml.safe_load(json.load(handle)['mihomo']['custom_config'])
    assert '+.pool.ntp.org' in template['dns']['fake-ip-filter']
    # The embedded template writes the key, so it must keep Mihomo's own defaults too.
    assert _default_mihomo_template()['dns']['fake-ip-filter'] == [
        'dns.msftnsci.com', 'www.msftnsci.com', 'www.msftconnecttest.com', '+.pool.ntp.org']
