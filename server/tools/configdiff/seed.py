"""Create synthetic storage through Python's backup importer, then clone it.

No Go repository API is used. Run only in the isolated test-server environment.
"""
import json
import os
from pathlib import Path
import sys

CURRENT = 'synthetic-current-proxy/token+0123456789'
RETIRED = 'synthetic-retired-proxy/token+9876543210'
AGENT = 'synthetic-agent/token+01234567890123456789'
PULL = 'synthetic-public-pull-token'


def create(directory, options):
    os.environ['DATA_DIR'] = str(directory)
    from backend.common.config import get_repository, get_default_config, set_repository
    from backend.common.config_repository import ProfileRepository
    repository = ProfileRepository(directory, default_config_factory=get_default_config, initial_config_factory=get_default_config)
    set_repository(repository)
    document = repository.export_all()
    for key in ('subscriptions', 'subscription_aggregations', 'nodes', 'rule_library'):
        document['shared'][key] = []
    for profile in document['profiles'].values():
        profile.update(rule_configs=[], proxy_groups=[], routers={'items': []},
                       domain_discovery={'auto_probe': False, 'auto_apply': False})
        profile['mihomo'] = {'custom_config': 'dns:\n  enable: true\n  enhanced-mode: fake-ip\n  fake-ip-range: 198.18.0.1/16\n'}
    location = options.get('x01_location')
    payload = options.get('x01_payload', '')
    profile = document['profiles'][repository.default_profile_id()]
    if location == 'node':
        parameters = {'nested': {'rule_proxy_token': 'remove-current-key',
                                 'retired_rule_proxy_tokens': ['remove-retired-key'],
                                 'rule_fetch_proxy': 'remove-proxy-key', 'keep': 'visible'}}
        if options.get('depth'):
            nested = {'leaf': 'visible-at-depth'}
            for _ in range(options['depth']):
                nested = {'next': nested}
            parameters['deep'] = nested
        document['shared']['nodes'] = [{'id': 'node-synthetic', 'name': payload, 'type': 'ss',
                                        'server': 'node.example', 'port': 443, 'cipher': 'aes-128-gcm',
                                        'password': 'synthetic-node-password', 'params': parameters}]
    elif location == 'subscription':
        document['shared']['subscriptions'] = [{'id': 'sub-synthetic', 'name': '合成订阅',
            'url': 'http://source.example/sub?synthetic=' + payload, 'enabled': True, 'interval': 0}]
    elif location == 'rule':
        profile['rule_configs'] = [{'id': 'rule-synthetic', 'itemType': 'rule', 'type': 'DOMAIN',
                                    'value': payload, 'policy': 'DIRECT', 'enabled': True}]
    elif location == 'group':
        profile['proxy_groups'] = [{'id': 'group-synthetic', 'name': payload,
                                   'type': 'select', 'manual_nodes': ['DIRECT']}]
    if location in ('library', 'ruleset'):
        document['shared']['rule_library'] = [{'id': 'library-synthetic', 'name': payload,
            'source_type': 'content', 'content': 'DOMAIN,example.com', 'behavior': 'classical',
            'enabled': True, 'update_interval': 0}]
        profile['rule_configs'] = [{'id': 'ruleset-synthetic', 'itemType': 'ruleset',
            'library_rule_id': 'library-synthetic', 'policy': 'DIRECT', 'enabled': True}]
    elif location == 'aggregation':
        document['shared']['subscription_aggregations'] = [{'id': 'agg-synthetic', 'name': payload,
                                                          'subscriptions': [], 'manual_nodes': []}]
    elif location == 'custom':
        profile['mihomo']['custom_config'] += '# ' + payload + '\n'
    elif location == 'mosdns':
        from urllib.parse import quote
        profile['mosdns']['remote_dns'] = 'https://dns.example/dns-query?token=' + quote(CURRENT, safe='')
    elif location == 'domain':
        profile['domain_discovery']['history'] = [{'value': payload, 'target': 'direct',
            'ruleset_id': 'synthetic-missing', 'applied_at': '2026-01-01T00:00:00Z'}]
    elif location == 'tasks':
        document['shared']['subscriptions'] = [{'id': 'sub-synthetic', 'name': payload,
            'url': 'http://source.example/sub', 'enabled': True, 'interval': 0}]
    document['shared'].update(options.get('shared', {}))
    for field, value in options.get('profile', {}).items():
        if isinstance(value, dict) and isinstance(profile.get(field), dict):
            profile[field].update(value)
        else:
            profile[field] = value
    repository.import_all(document)
    if 'other' not in document['profiles']:
        repository.create_profile({'id': 'other', 'name': '空间乙🧪'})
    if options.get('other_profile'):
        def update_other(value):
            for key, item in options['other_profile'].items():
                if isinstance(item, dict) and isinstance(value.get(key), dict):
                    value[key].update(item)
                else:
                    value[key] = item
            return value
        repository.update_profile_transaction('other', update_other)
    default = repository.default_profile_id()
    def system(value):
        value['system_config'].update(server_domain='http://panel.example',
                                      rule_proxy_token=CURRENT,
                                      retired_rule_proxy_tokens=[RETIRED],
                                      config_token=options.get('config_token', ''),
                                      rule_fetch_proxy='', github_proxy_domain='')
        value['backup'].update(auto_backup=False, webdav_url='', webdav_username='', webdav_password='')
        value['agents'] = [{'id': 'agent-synthetic', 'name': '合成 Agent 🧪', 'token': AGENT,
                            'profile_id': default, 'service_type': 'mihomo', 'host': '127.0.0.1',
                            'port': 9, 'enabled': True, 'version': '1.8.60-go', 'deployment_method': 'shell'}]
        if 'agents' in options:
            value['agents'] = options['agents']
        elif options.get('agent_updates'):
            value['agents'][0].update(options['agent_updates'])
        if location == 'settings':
            from urllib.parse import quote
            value['system_config']['server_domain'] = 'http://panel.example/?token=' + quote(CURRENT, safe='')
        if options.get('legacy_dns'):
            endpoint = '192.0.2.53:15353'
            value['agents'][0]['node_test_dns_endpoints'] = {'legacy-synthetic': {
                'endpoint': endpoint, 'profile_id': default, 'status': 'installed', 'plugins': [], 'installed': True}}
            value['system_config']['node_test_dns_config'] = {
                'mode': 'custom', 'servers': [endpoint, 'https://dns.example/dns-query'], 'bootstrap': []}
        value['backup'].update(options.get('backup', {}))
        value['system_config'].update(options.get('system_config', {}))
        return value
    repository.update_system_transaction(system)
    if options.get('redaction'):
        repository.update_profile(default, {'description': options['redaction']})
    if location == 'notifications':
        from backend.notifications import record_notice
        record_notice(repository, 'notice:synthetic-x01', '合成通知', payload, '/synthetic')
    if options.get('x01_native'):
        from backend.converters.oxidns import generate_oxidns_config
        config = repository.get_compat_config(default)
        config['oxidns']['api'].update(username=AGENT, password=CURRENT)
        native = generate_oxidns_config(config, 'http://panel.example')
        def save_native(value):
            value['oxidns'].update(config_mode='native', native_config=native)
            return value
        repository.update_profile_transaction(default, save_native)
    # Compatibility fixtures are created by Python before either public server
    # starts; both receive the same complete SQL/session-file snapshot.
    if options.get('setup_sessions'):
        from backend.common import setup_flow
        for fixture in options['setup_sessions']:
            session = setup_flow.create(repository, {key: fixture[key] for key in ('id', 'name', 'goal')})
            if fixture.get('profile'):
                def patch(value):
                    for key, item in fixture['profile'].items():
                        if isinstance(item, dict) and isinstance(value.get(key), dict):
                            value[key].update(item)
                        else:
                            value[key] = item
                    return value
                repository.update_profile_transaction(session['profile_id'], patch)
            session.update(fixture.get('session', {}))
            setup_flow.write(repository, session)
    # Empty scheduler inputs and auto_backup=false are asserted by the harness.
    export = repository.export_all()
    Path(directory, 'configdiff-seed.json').write_text(json.dumps(export, ensure_ascii=False))
    if options.get('orphan_cache'):
        cache = Path(directory, 'shared/subscribes')
        cache.mkdir(parents=True, exist_ok=True)
        (cache / 'orphan-synthetic.json').write_text('{"nodes":[],"synthetic_orphan":true}')
    # Import snapshots are seed setup, not writes from a tested request.
    # They remain identical in both copies and are compared when relevant.


if __name__ == '__main__':
    options_json = Path(sys.argv[3]).read_text() if sys.argv[2] == '--options-file' else sys.argv[2]
    create(Path(sys.argv[1]), json.loads(options_json))
