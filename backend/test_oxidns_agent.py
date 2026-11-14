"""OxiDNS deployment keeps managed inputs separate from mutable core state."""
import io
import json
import subprocess
import tarfile

import pytest
import yaml

from backend.agents.deployment_bundle import (
    DeploymentPreparationError, build_deployment_bundle, deployment_content_fingerprint,
)
from backend.agents.go_install_script import generate_go_agent_install_script
from backend.agents.install_script import (
    generate_docker_agent_compose, generate_docker_agent_run,
)

AGENT = {'id': 'oxi-test', 'service_type': 'oxidns', 'profile_id': 'default'}


def test_complete_oxidns_bundle_converts_rules_and_preserves_filter_bytes():
    config = '''plugins:
- tag: domains
  type: domain_set
  args: {files: ['./rules/domains.txt']}
- tag: ips
  type: ip_set
  args: {files: ['./rules/ips.txt']}
- tag: filters
  type: adguard_rule
  args: {files: ['./rules/filters.txt']}
- tag: learned
  type: dynamic_domain_set
  args: {path: './state/learned.txt', bootstrap_rules: ['domain:seed.test']}
- tag: recorder
  type: query_recorder
  args: {path: './state/queries.db'}
'''
    bundle = build_deployment_bundle(AGENT, {}, config, custom_files=[
        {'path': 'rules/domains.txt', 'content': 'DOMAIN-SUFFIX,example.test\nIP-CIDR,192.0.2.0/24'},
        {'path': 'rules/ips.txt', 'content': 'DOMAIN-SUFFIX,example.test\nIP-CIDR,192.0.2.0/24'},
        {'path': 'rules/filters.txt', 'content': '||ads.test^$dnstype=A\n@@||allow.test^'},
    ])
    assert bundle.manifest['service_type'] == 'oxidns'
    assert deployment_content_fingerprint(bundle.manifest).startswith('v1:')
    paths = [f['path'] for f in bundle.manifest['files']]
    assert not any(p.startswith('state/') for p in paths)
    with tarfile.open(fileobj=io.BytesIO(bundle.archive), mode='r:gz') as archive:
        assert archive.extractfile('files/rules/domains.txt').read() == b'domain:example.test'
        assert archive.extractfile('files/rules/ips.txt').read() == b'192.0.2.0/24'
        assert archive.extractfile('files/rules/filters.txt').read() == b'||ads.test^$dnstype=A\n@@||allow.test^'


def test_oxidns_bundle_refuses_mutable_state_payload_and_missing_file():
    with pytest.raises(DeploymentPreparationError, match='runtime state'):
        build_deployment_bundle(AGENT, {}, 'plugins: []', custom_files=[{'path': 'state/learned.txt', 'content': 'bad'}])
    with pytest.raises(DeploymentPreparationError, match='附加文件里没有它'):
        build_deployment_bundle(AGENT, {}, 'plugins: [{tag: filters, type: adguard_rule, args: {files: [rules/missing.txt]}}]')


def test_oxidns_native_install_and_legacy_guard(tmp_path):
    script = generate_go_agent_install_script('https://config.test', 'oxi-test', 'oxidns')
    config = json.loads(script.split("<<'CONFIGFLOW_JSON_EOF'\n")[1].split('\nCONFIGFLOW_JSON_EOF')[0])
    assert config['service_binary'] == 'oxidns'
    assert config['service_unit'] == 'oxidns'
    assert config['config_path'] == '/etc/oxidns/config.yaml'
    assert 'Requires=configflow-recover-oxidns.service' not in script
    assert '-validate-service' in script
    assert '-check-deployments' in script
    assert '[ "$other_type" != "oxidns" ]' in script
    installer = tmp_path / 'install.sh'
    installer.write_text(script)
    assert subprocess.run(['sh', '-n', str(installer)]).returncode == 0


def test_oxidns_docker_persists_state_and_maps_udp_tcp():
    cfg = yaml.safe_load(generate_docker_agent_compose('https://config.test', enable_mihomo=False, enable_oxidns=True, network_mode='bridge'))
    service = cfg['services']['agent']
    assert './agent_data/oxidns:/etc/oxidns' in service['volumes']
    assert 'ENABLE_OXIDNS=true' in service['environment']
    assert {'8082:8082', '53:53/tcp', '53:53/udp'} <= set(service['ports'])
    command = generate_docker_agent_run('https://config.test', enable_mihomo=False, enable_oxidns=True, network_mode='bridge')
    assert '-e ENABLE_OXIDNS="true"' in command
    assert '-p 53:53/tcp' in command and '-p 53:53/udp' in command
    with pytest.raises(ValueError, match='53'):
        generate_docker_agent_compose('https://config.test', enable_mosdns=True, enable_oxidns=True)


def test_docker_oxidns_boot_starts_without_recovery_gate(tmp_path):
    from backend.test_agent_transaction_install import entrypoint_harness
    entrypoint, env = entrypoint_harness(tmp_path)
    env.update(SERVICE_TYPE='oxidns', ENABLE_MIHOMO='false', ENABLE_MOSDNS='false')
    supervisor = tmp_path / 'etc/supervisor/conf.d'
    (supervisor / 'oxidns.conf').write_text('command=old\nautostart=false\n')
    (supervisor / 'agent-oxidns.conf').write_text('autostart=false\n')
    result = subprocess.run(['sh', str(entrypoint), 'sh', '-c', 'echo supervisor >> "$EVENTS"'], env=env, capture_output=True, text=True)
    assert result.returncode == 0, result.stderr
    events = (tmp_path / 'events').read_text().splitlines()
    assert events == ['supervisor']
    config = yaml.safe_load((tmp_path / 'etc/oxidns/config.yaml').read_text())
    assert config['runtime']['worker_threads'] == 2
    assert (tmp_path / 'etc/oxidns/state').is_dir()
    assert 'autostart=true' in (supervisor / 'oxidns.conf').read_text()
    assert ' start -c ' in (supervisor / 'oxidns.conf').read_text()
    assert ' -d ' in (supervisor / 'oxidns.conf').read_text()


@pytest.mark.parametrize('args', [{'file': 'geoip.dat'}, {'persistent': {'files': ['rules/permanent.txt']}}])
def test_oxidns_bundle_requires_nested_and_singular_files(args):
    config = yaml.safe_dump({'plugins': [{'tag': 'custom', 'type': 'geoip', 'args': args}]})
    with pytest.raises(DeploymentPreparationError, match='附加文件里没有它'):
        build_deployment_bundle(AGENT, {}, config)


def test_oxidns_transaction_preparation_uses_profile_rules_and_independent_settings():
    from backend.common.config import get_default_config
    from backend.routes.agents import _prepare_transactional_content
    from backend.agents.config_generator import generate_agent_config
    config = get_default_config()
    config.update(profile_id='default', rule_library=[{
        'id': 'local-rule', 'name': 'local-rule', 'source_type': 'content',
        'content': 'DOMAIN-SUFFIX,example.test\nIP-CIDR,192.0.2.0/24', 'enabled': True,
    }], rule_configs=[{
        'id': 'selected', 'name': 'Local', 'itemType': 'ruleset', 'enabled': True,
        'library_rule_id': 'local-rule', 'behavior': 'classical',
        'url': 'https://config.test/api/profiles/default/rules/local/local-rule',
    }])
    config['system_config'].update(server_domain='https://config.test', rule_proxy_token='fixture')
    config['oxidns']['direct_rulesets'] = ['selected']
    config['oxidns']['query_recorder']['enabled'] = True
    config['oxidns']['dynamic_domain_set']['enabled'] = True
    prepared = _prepare_transactional_content(AGENT, config, base_url='https://config.test')
    bundle = prepared['bundle']
    assert bundle.manifest['service_type'] == 'oxidns'
    assert len(bundle.manifest['files']) == 2
    assert not any(f['path'].endswith('_ip.txt') for f in bundle.manifest['files'])
    assert not any(f['path'].startswith('state/') for f in bundle.manifest['files'])
    assert generate_agent_config(config, AGENT)['service_type'] == 'oxidns'
    original = deployment_content_fingerprint(bundle.manifest)
    config['mosdns']['cache_size'] = 333
    assert deployment_content_fingerprint(_prepare_transactional_content(AGENT, config)['bundle'].manifest) == original
    config['oxidns']['worker_threads'] = 3
    assert deployment_content_fingerprint(_prepare_transactional_content(AGENT, config)['bundle'].manifest) != original
