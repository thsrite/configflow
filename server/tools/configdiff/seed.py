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
                       domain_discovery={'auto_probe_enabled': False})
        profile['mihomo'] = {'custom_config': 'dns:\n  enable: true\n  enhanced-mode: fake-ip\n  fake-ip-range: 198.18.0.1/16\n'}
    repository.import_all(document)
    if 'other' not in document['profiles']:
        repository.create_profile({'id': 'other', 'name': '空间乙🧪'})
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
        return value
    repository.update_system_transaction(system)
    if options.get('redaction'):
        repository.update_profile(default, {'description': options['redaction']})
    # Empty scheduler inputs and auto_backup=false are asserted by the harness.
    export = repository.export_all()
    Path(directory, 'configdiff-seed.json').write_text(json.dumps(export, ensure_ascii=False))
    if options.get('orphan_cache'):
        cache = Path(directory, 'shared/subscribes')
        cache.mkdir(parents=True, exist_ok=True)
        (cache / 'orphan-synthetic.json').write_text('{"nodes":[],"synthetic_orphan":true}')
    if options.get('legacy_dns'):
        def legacy(value):
            value['system_config']['node_test_dns'] = 'synthetic-legacy-oxidns'
            return value
        repository.update_system_transaction(legacy)
    # Import snapshots are seed setup, not writes from a tested request.
    # They remain identical in both copies and are compared when relevant.


if __name__ == '__main__':
    create(Path(sys.argv[1]), json.loads(sys.argv[2]))
