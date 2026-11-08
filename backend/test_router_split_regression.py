"""The router split's full TUN leaves every other path exactly as 0ef9e89 had it.

The helpers below only use code that already existed in 0ef9e89, so the
fixture in ``testdata/router_split_regression_0ef9e89.json`` was produced by
running them against that release with the same inputs.
"""
from copy import deepcopy
import hashlib
import json
from pathlib import Path
import re

import pytest
import yaml
from flask import Flask

from backend.agents.go_install_script import generate_go_agent_install_script
from backend.common import config as config_store
from backend.common.config_repository import ProfileRepository
from backend.common.gateway_onboarding import prepare_gateway_onboarding
from backend.converters.mihomo import generate_mihomo_config
from backend.routes import register_blueprints
from backend.test_agent_upgrade_auth import authenticated_app  # noqa: F401
from backend.test_gateway_onboarding import form, snapshot

BASELINE = Path(__file__).parent / 'testdata' / 'router_split_regression_0ef9e89.json'
SESSION = 'c' * 32
SECRET = 'lab-panel-pass'
NODE = {'id': 'node', 'name': 'Test node', 'type': 'ss', 'server': '192.0.2.1',
        'port': 443, 'cipher': 'aes-128-gcm', 'password': 'private'}
IP_RULE = {'id': 'chat', 'name': '聊天', 'behavior': 'classical', 'format': 'text', 'source_type': 'content',
           'content': 'DOMAIN-SUFFIX,chat.test\nIP-CIDR,198.51.100.0/24,no-resolve', 'enabled': True}
DOMAIN_RULES = {'direct': ['builtin:direct'], 'proxy': ['builtin:telegram', 'library:chat', 'builtin:proxy']}

# bbb5fd33 added these two HA-only dependency cases to the shared installer
# helper. Ordinary installers never request either tool, so compare every other
# byte with the original fixture. Freeze the exact reviewed addition rather
# than dropping the whole helper or regenerating its baseline hash.
_NATIVE_HA_DEPENDENCY_CASES = '''        if [ "$agent_tool" = ha_ip ]; then
            # BusyBox ip lacks JSON output required by the HA Agent.
            if ! command -v ip >/dev/null 2>&1 || ! ip -j -4 addr show >/dev/null 2>&1; then
                AGENT_MISSING_DEPENDENCIES="${AGENT_MISSING_DEPENDENCIES:+${AGENT_MISSING_DEPENDENCIES} }iproute2(JSON)"
                case "$AGENT_PACKAGE_MANAGER" in
                    dnf|yum) agent_add_package iproute ;;
                    *) agent_add_package iproute2 ;;
                esac
            fi
            continue
        fi
        if [ "$agent_tool" = ha_service ]; then
            if [ "$INIT_SYSTEM" = openrc ]; then
                if [ ! -x /etc/init.d/keepalived ]; then
                    AGENT_MISSING_DEPENDENCIES="${AGENT_MISSING_DEPENDENCIES:+${AGENT_MISSING_DEPENDENCIES} }keepalived(OpenRC服务)"
                    if [ "$AGENT_PACKAGE_MANAGER" = apk ]; then agent_add_package keepalived-openrc; else agent_add_package keepalived; fi
                fi
            else
                agent_keepalived_unit=$(systemctl show keepalived.service --property=LoadState --value 2>/dev/null) || agent_keepalived_unit=""
                case "$agent_keepalived_unit" in
                    loaded|masked) ;;
                    *) AGENT_MISSING_DEPENDENCIES="${AGENT_MISSING_DEPENDENCIES:+${AGENT_MISSING_DEPENDENCIES} }keepalived(systemd服务)"; agent_add_package keepalived ;;
                esac
            fi
            continue
        fi
'''
_NATIVE_HA_PACKAGE_CASE = '                keepalived|jq) agent_add_package "$agent_tool" ;;\n'
# The Linux gateway installer reuses the shared helper for its own tools (nft,
# ss, tar, gzip) and a failed package transaction prints a copyable command.
# Ordinary installers never request these tools; freeze the reviewed text.
_GATEWAY_DEPENDENCY_CASES = '''                # Linux gateway tools; BusyBox has no ss or nft.
                nft) agent_add_package nftables ;;
                ss)
                    case "$AGENT_PACKAGE_MANAGER" in
                        dnf|yum) agent_add_package iproute ;;
                        *) agent_add_package iproute2 ;;
                    esac
                    ;;
                tar|gzip) agent_add_package "$agent_tool" ;;
'''
_MANUAL_INSTALL_COMMAND = '''# A command the user can copy when automatic installation cannot finish.
agent_manual_install_command() {
    case "$AGENT_PACKAGE_MANAGER" in
        apk) printf 'apk add --no-cache %s' "$AGENT_PACKAGES" ;;
        apt-get) printf 'apt-get update && apt-get install -y --no-install-recommends %s' "$AGENT_PACKAGES" ;;
        dnf|yum) printf '%s install -y %s' "$AGENT_PACKAGE_MANAGER" "$AGENT_PACKAGES" ;;
        pacman) printf 'pacman -S --needed %s' "$AGENT_PACKAGES" ;;
        zypper) printf 'zypper install %s' "$AGENT_PACKAGES" ;;
    esac
}
'''
_GATEWAY_DEPENDENCY_REWORDS = (
    ('# Shared by the native single-Agent, All-in-One and Linux gateway installers. Call only after\n',
     '# Shared by the native single-Agent and All-in-One installers. Call only after\n'),
    ('缺少的工具：${AGENT_MISSING_DEPENDENCIES}。可在 root 下手动执行：$(agent_manual_install_command)"\n',
     '缺少的工具：${AGENT_MISSING_DEPENDENCIES}。"\n'),
)
_CORE_INSTALL_PATH = 'PATH="${PATH:-/usr/sbin:/usr/bin:/sbin:/bin}:/usr/local/sbin:/usr/local/bin"\nexport PATH\n'
_CORE_AGENT_FOLLOWS = 'CF_OUTPUT_OWNER=false\n'


def without_reviewed_installer_deltas(script, *, core_path=False):
    count = script.count(_NATIVE_HA_DEPENDENCY_CASES)
    assert count >= 1, 'the reviewed native HA dependency addition changed'
    assert script.count(_NATIVE_HA_DEPENDENCY_CASES + '        if ! command -v "$agent_tool"') == count
    assert script.count(_NATIVE_HA_PACKAGE_CASE) == count
    script = script.replace(_NATIVE_HA_DEPENDENCY_CASES, '').replace(_NATIVE_HA_PACKAGE_CASE, '')
    assert script.count('                cmp) agent_add_package diffutils ;;\n' + _GATEWAY_DEPENDENCY_CASES) == count
    assert script.count(_MANUAL_INSTALL_COMMAND + 'agent_install_packages() {') == count
    script = script.replace(_GATEWAY_DEPENDENCY_CASES, '').replace(_MANUAL_INSTALL_COMMAND, '')
    for new, old in _GATEWAY_DEPENDENCY_REWORDS:
        assert script.count(new) == count, new
        script = script.replace(new, old)
    if core_path:
        # 707208f4 makes combined single-service installs find cores placed in
        # /usr/local/bin, even in minimal root shells. Require the exact location.
        assert script.count('set -eu\n' + _CORE_INSTALL_PATH + 'for cf_argument do\n') == 1
        script = script.replace(_CORE_INSTALL_PATH, '', 1)
        # The Agent follows the embedded core phase, so that phase no longer
        # asks for a separate Agent install. Require the exact location.
        embedded = re.findall(r'\(\nset -- --core [a-z]+\n' + _CORE_AGENT_FOLLOWS, script)
        assert len(embedded) == 1, 'the embedded core phase must not ask for a separate Agent'
        script = script.replace(embedded[0], embedded[0].removesuffix(_CORE_AGENT_FOLLOWS), 1)
    return script


def _app(tmp_path):
    repository = ProfileRepository(tmp_path)
    config_store.set_repository(repository)
    app = Flask(__name__)
    register_blueprints(app)
    return app.test_client(), repository


def gateway_goal_fields(tmp_path):
    """The wizard's gateway goal from creation through the installer's saved drafts."""
    previous = config_store._repository
    try:
        client, repository = _app(tmp_path)
        session = client.post('/api/setup', json={'id': SESSION, 'name': '网关配置', 'goal': 'gateway'}).get_json()
        repository.update_shared_transaction(lambda shared: shared.update(
            nodes=[deepcopy(NODE)], rule_library=[deepcopy(IP_RULE)]))
        assert client.post(f'/api/setup/{SESSION}/source', json={'node_ids': ['node']}).status_code == 200
        # The DNS query recorder switch postdates 0ef9e89, which left it off.
        response = client.post(f'/api/setup/{SESSION}/prepare', json={'controller_secret': SECRET, 'domain_rules': DOMAIN_RULES, 'query_recorder': False})
        assert response.status_code == 200, response.get_json()
        url = f"/api/profiles/{session['profile_id']}/gateway-onboarding"
        state = client.get(url).get_json()
        # The installation wizard sends back the state's settings as they are.
        body = {**state['settings'], 'expected_config_revision': state['config_revision']}
        preview = client.post(url + '/preview', json=body).get_json()
        assert preview['ready'], preview
        saved = client.post(url, json=body)
        assert saved.status_code == 200, saved.get_json()
        profile = repository.get_profile(session['profile_id'])
        return {key: profile[key] for key in ('proxy_groups', 'rule_configs', 'mihomo', 'oxidns')}
    finally:
        config_store.set_repository(previous)


def single_service_script_digests():
    """Single-service installers, which the full TUN never reaches."""
    result = {}
    for service, cores in (('mihomo', (False, True)), ('oxidns', (False, True)), ('mosdns', (False,))):
        for install_core in cores:
            script = generate_go_agent_install_script(
                'https://config.test', 'single', service, agent_ip='192.0.2.20',
                binary_download_url='https://config.test/api/agents/download', install_core=install_core)
            result[f'{service}:{install_core}'] = hashlib.sha256(without_reviewed_installer_deltas(script, core_path=install_core).encode()).hexdigest()
    return result


def unmarked_profiles():
    """Profiles without a router split marker: ordinary, selective gateway, and a tun of the user's own."""
    ordinary = snapshot()
    gateway, report = prepare_gateway_onboarding(snapshot(), form())
    assert report['ready'], report
    own_tun = snapshot()
    own_tun['mihomo']['custom_config'] = ('mixed-port: 7890\ntun:\n  enable: true\n  stack: system\n'
                                          '  auto-route: true\n  route-address-set: [proxy-ip]\n')
    return {'ordinary': ordinary, 'gateway': gateway, 'own_tun': own_tun}


def unmarked_mihomo_outputs():
    return {name: generate_mihomo_config(config, preflight_providers=False)
            for name, config in unmarked_profiles().items()}


def baseline():
    return json.loads(BASELINE.read_text(encoding='utf-8'))


# Per-rule OxiDNS DNS settings postdate 0ef9e89. Stored empty, they leave every
# generated output unchanged, so they must be empty and are otherwise ignored.
RULE_DNS_DEFAULTS = {'rule_actions': {}, 'rule_dns_follow': [], 'rule_dns_parked': {}}


_PLAIN_SETUP_NAMESERVER = '  nameserver:\n  - 223.5.5.5\n  - 119.29.29.29\n'


def with_current_setup_nameserver(fields):
    """0ef9e89's wizard wrote plain Mihomo upstreams; it now writes the gateway's DoH default."""
    from backend.common.gateway_onboarding import DEFAULT_MIHOMO_NAMESERVERS
    source = fields['mihomo']['custom_config']
    assert source.count(_PLAIN_SETUP_NAMESERVER) == 1 and source.endswith(_PLAIN_SETUP_NAMESERVER)
    current = '  nameserver:\n' + ''.join(f'  - {value}\n' for value in DEFAULT_MIHOMO_NAMESERVERS)
    return {**fields, 'mihomo': {**fields['mihomo'], 'custom_config': source.replace(_PLAIN_SETUP_NAMESERVER, current)}}


# 0ef9e89's wizard replaced the base YAML with a short one. It now keeps the
# profile's base and only sets what the plan needs; the new setup profile's base
# is empty, so it starts from the initial template (config_template.json), with
# the user's controller password, Mihomo DNS on 127.0.0.1:1053 and the
# template's TUN left to the Linux gateway. Freeze exactly this reviewed change.
_SETUP_SHORT_BASE = ("mixed-port: 7890\nallow-lan: true\nbind-address: '*'\nmode: rule\nlog-level: info\n"
                     'external-controller: 127.0.0.1:9090\ndns:\n  enable: true\n  listen: 127.0.0.1:1053\n'
                     '  enhanced-mode: fake-ip\n')


def with_current_setup_base(fields):
    """The wizard's own base for a new gateway profile: the initial template with the plan's settings."""
    from backend.common.resource import get_backend_resource
    fields = with_current_setup_nameserver(fields)
    source = fields['mihomo']['custom_config']
    assert source.startswith(_SETUP_SHORT_BASE) and 'tun:' not in source
    template = yaml.safe_load(json.loads(Path(get_backend_resource('config_template.json')).read_text(encoding='utf-8'))['mihomo']['custom_config'])
    template['secret'] = SECRET
    template['dns']['listen'] = '127.0.0.1:1053'
    template['tun']['enable'] = False
    current = yaml.safe_dump(template, allow_unicode=True, sort_keys=False)
    return {**fields, 'mihomo': {**fields['mihomo'], 'custom_config': current}}


def test_gateway_goal_drafts_match_the_previous_release(tmp_path):
    fields = gateway_goal_fields(tmp_path)
    oxidns = fields['oxidns']
    assert {key: oxidns.get(key) for key in RULE_DNS_DEFAULTS} == RULE_DNS_DEFAULTS
    fields['oxidns'] = {key: value for key, value in oxidns.items() if key not in RULE_DNS_DEFAULTS}
    assert fields == with_current_setup_base(baseline()['gateway_goal_fields'])


def test_single_service_scripts_match_the_previous_release():
    assert single_service_script_digests() == baseline()['single_service_script_sha256']


@pytest.mark.parametrize('name', ['ordinary', 'gateway', 'own_tun'])
def test_mihomo_without_marker_matches_the_previous_release(name):
    expected = baseline()['unmarked_mihomo'][name]
    # Text delivery compatibility postdates this baseline; only its explicit
    # local-provider marker may differ. All routing/TUN bytes stay identical.
    for provider in yaml.safe_load(expected).get('rule-providers', {}).values():
        url = provider.get('url', '')
        if '/rules/local/' in url and provider.get('format') != 'mrs':
            expected = expected.replace('url: ' + url + '\n',
                                        'url: ' + url + '?target=mihomo\n')
    if name == 'gateway':
        expected = with_current_gateway_nameserver(expected)
    assert unmarked_mihomo_outputs()[name] == expected


_PREVIOUS_GATEWAY_NAMESERVER = '  nameserver:\n    - https://223.5.5.5/dns-query\n    - https://1.1.1.1/dns-query\n'


def with_current_gateway_nameserver(expected):
    """0ef9e89 filled the gateway's missing Mihomo upstreams with Cloudflare DoH; it is now domestic."""
    from backend.common.gateway_onboarding import DEFAULT_MIHOMO_NAMESERVERS
    assert expected.count(_PREVIOUS_GATEWAY_NAMESERVER) == 1
    current = '  nameserver:\n' + ''.join(f'    - {value}\n' for value in DEFAULT_MIHOMO_NAMESERVERS)
    return expected.replace(_PREVIOUS_GATEWAY_NAMESERVER, current)


def test_router_setup_runs_end_to_end_on_a_full_tun(authenticated_app, monkeypatch):
    """Setup, installer drafts, installer script, publication and the final check, with fake Agents."""
    from backend.agents import gateway_installation as installations
    from backend.common.router_split import FULL_TUN, is_full_tun, router_split_state
    from backend.routes.agents import _prepare_transactional_content
    client, manager, _, admin, _ = authenticated_app
    repository = manager.repository
    repository.update_system_transaction(
        lambda system: system['system_config'].update(server_domain='https://config.example'))
    repository.update_shared_transaction(lambda shared: shared.update(
        nodes=[deepcopy(NODE)], rule_library=[deepcopy(IP_RULE)]))
    session = client.post('/api/setup', headers=admin, json={'id': SESSION, 'name': '分流配置', 'goal': 'router'}).get_json()
    profile_id = session['profile_id']
    assert client.post(f'/api/setup/{SESSION}/source', headers=admin, json={'node_ids': ['node']}).status_code == 200
    prepared = client.post(f'/api/setup/{SESSION}/prepare', headers=admin, json={'controller_secret': SECRET, 'domain_rules': DOMAIN_RULES})
    assert prepared.status_code == 200, prepared.get_json()
    assert prepared.get_json()['legacy_takeover'] is False

    # The installer's drafts keep the gateway off and the base YAML on the full TUN.
    url = f'/api/profiles/{profile_id}/gateway-onboarding'
    state = client.get(url, headers=admin).get_json()
    assert state['takeover'] == 'full_tun' and state['gateway_enabled'] is False
    body = {**state['settings'], 'rule_set_ids': [], 'expected_config_revision': state['config_revision']}
    assert client.post(url + '/preview', headers=admin, json=body).get_json()['ready']
    saved = client.post(url, headers=admin, json=body)
    assert saved.status_code == 200, saved.get_json()
    config, revision = repository.get_publish_snapshot(profile_id)
    assert is_full_tun(config) and config['mihomo']['linux_gateway'] == {'enabled': False, 'rule_set_ids': []}
    assert yaml.safe_load(config['mihomo']['custom_config'])['tun'] == FULL_TUN
    assert router_split_state(repository, profile_id)['mode'] == 'full_tun'

    # The installer persists forwarding and prepares the TUN itself.
    created = client.post('/api/agents/gateway-installations', headers=admin, json={
        'profile_id': profile_id, 'name': 'split-linux', 'server_url': 'https://config.example',
        'auto_network': True, 'agent_ip': '192.0.2.10', 'config_revision': revision})
    assert created.status_code == 200, created.get_json()
    assert created.get_json()['takeover'] == 'full_tun'
    script = installations._read(repository, created.get_json()['id'])['script']
    assert '-prepare-tun -assume-tun-enabled' in script and 'persist_ipv4_forwarding' in script
    assert 'CONFIGFLOW_FORWARDING_OWNER' not in script

    # Publishing generates the full TUN and refuses a shell Agent that would drop forwarding.
    agent = {'id': 'm', 'service_type': 'mihomo', 'deployment_method': 'shell', 'version': '1.8.40-go'}
    monkeypatch.setattr('backend.agents.linux_gateway.call', lambda *args: {
        'success': True, 'managed': {'active': False, 'healthy': False, 'preserves_forwarding': True}})
    main = yaml.safe_load(_prepare_transactional_content(agent, config)['config_content'])
    assert main['tun']['enable'] is True and main['tun']['auto-redirect'] is True
    assert 'x-configflow-linux-gateway' not in main
    with pytest.raises(ValueError, match='1.8.40-go'):
        _prepare_transactional_content({**agent, 'version': '1.8.39-go'}, config)

    # The final check reads forwarding and TUN, then hands the router only fake-IP.
    agents = {service: {'id': service, 'name': service, 'host': '192.0.2.10', 'status': 'online'}
              for service in ('mihomo', 'oxidns')}
    monkeypatch.setattr('backend.agents.gateway_installation.latest', lambda *_: {'session': {
        'id': 'installation', 'phase': 'published', 'target_changed': False, 'agents': agents,
        'config_changed': repository.config_revision(profile_id) != revision}})
    status = {'success': True, 'supported': True, 'enabled_in_config': False,
              'managed': {'active': False, 'healthy': False, 'preserves_forwarding': True},
              'ipv4_forwarding': {'protocol': 1, 'current': '1', 'config': 'managed', 'hook_script': 'managed',
                                  'startup_hook': 'registered', 'persisted': True, 'supported': True},
              'tun': {'configured': True, 'device': 'Meta', 'present': True, 'gateway_marker': False}}
    calls = []
    monkeypatch.setattr('backend.agents.linux_gateway.call', lambda *args: calls.append(args[2:4]) or deepcopy(status))
    status['ipv4_forwarding']['persisted'] = False
    refused = client.post(f'/api/setup/{SESSION}/check', headers=admin, json={})
    assert refused.status_code == 409 and '持久化' in refused.get_json()['message']
    status['ipv4_forwarding']['persisted'] = True
    checked = client.post(f'/api/setup/{SESSION}/check', headers=admin, json={})
    assert checked.status_code == 200, checked.get_json()
    assert set(calls) == {('status', 'GET')}
    fake_ip = checked.get_json()['route_instructions']['routes']
    assert [item['cidr'] for item in fake_ip] == ['198.18.0.0/16']
    assert fake_ip[0]['requires_source_exclusion'] is False
    finished = client.post(f'/api/setup/{SESSION}/finish', headers=admin,
                           json={'router_mode': 'manual', 'ack_routes': True, 'ack_dns': True})
    assert finished.status_code == 200, finished.get_json()
    assert finished.get_json()['completed'] and finished.get_json()['router_verification'] == 'manual'
    # Nothing on the way turned the selective gateway on.
    final = repository.get_profile(profile_id)['mihomo']
    assert final['linux_gateway']['enabled'] is False and final['router_split']['route_rule_set_ids'] == []
