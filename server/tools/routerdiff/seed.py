"""Create synthetic interchange storage with Python's public repository API.

Run in a separate test-server process so importing config cannot retain another
fixture's DATA_DIR. No Go private helper or production snapshot is used.
"""
import argparse
import base64
import copy
import json
import os
from pathlib import Path
import re
import sys

from inventory import ROOT

ADMIN = 'routerdiff-admin'
PASSWORD = 'synthetic-routerdiff-admin-password'
JWT_SECRET = 'synthetic-routerdiff-jwt-secret-0123456789abcdef'
DEVICE_PASSWORD = 'synthetic-device-password-0123456789'
CURRENT_PROXY = 'synthetic-current-rule-proxy/token+01234567890123456789'
RETIRED_PROXY = 'synthetic-retired-rule-proxy/token+98765432109876543210'
AGENT_TOKEN = 'synthetic-routerdiff-agent/token+01234567890123456789'
CONFIG_TOKEN = 'synthetic-routerdiff-config-download-token-01234567890123456789'


def merge(target, patch):
    """Apply explicit fixture options, not product normalization rules."""
    for key, value in patch.items():
        if isinstance(value, dict) and isinstance(target.get(key), dict):
            merge(target[key], value)
        else:
            target[key] = copy.deepcopy(value)
    return target


def router(ident='r-ros', vendor='routeros', **updates):
    value = {'id': ident, 'name': '合成路由器🧪 ' + vendor, 'type': vendor,
             'endpoint': 'http://10.44.0.1:' + ('18081' if vendor == 'routeros' else '18082'),
             'username': 'test-admin', 'password': DEVICE_PASSWORD, 'verify_tls': True,
             'proxy_gateway_agent_id': '', 'dns_agent_id': '', 'ip_rule_sets': [],
             'ros_policy_rule_sets': [], 'manage_return_routing': False,
             'auto_rule_sync': False, 'rule_sync_interval': 60}
    value.update(updates)
    return value


def create(directory, options):
    directory = Path(directory).resolve()
    directory.mkdir(parents=True, exist_ok=True)
    os.environ['DATA_DIR'] = str(directory)
    sys.path.insert(0, str(ROOT))
    from backend.common.config import get_default_config, set_repository
    from backend.common.config_repository import ProfileRepository

    repository = ProfileRepository(directory, default_config_factory=get_default_config,
                                   initial_config_factory=get_default_config)
    set_repository(repository)
    document = repository.export_all()
    default = document['profiles'][repository.default_profile_id()]
    profiles = {}
    extras = options.get('additional_profiles', {})
    if not isinstance(extras, dict) or len(extras) > 256 or any(
            not isinstance(ident, str) or not re.fullmatch(r'(?:cache-[0-9]{3}|setup_[a-f0-9]{20})', ident)
            or not isinstance(patch, dict) for ident, patch in extras.items()):
        raise ValueError('additional profiles must be bounded, explicitly synthetic cache/setup fixtures')
    specs = [('alpha', '空间甲🧪', options.get('alpha', {})), ('beta', '空间乙🧪', options.get('beta', {}))]
    specs += [(ident, '合成缓存空间 ' + ident, patch) for ident, patch in extras.items()]
    for ident, name, patch in specs:
        profile = copy.deepcopy(default)
        profile.update(id=ident, name=name, description='合成差分夹具', proxy_groups=[], rule_configs=[],
                       routers={'items': []}, device_notes={}, client_fixed_ips={},
                       domain_discovery={'auto_probe': False, 'auto_apply': False})
        profile['mihomo'] = {'custom_config': 'ipv6: false\ndns:\n  enable: true\n  enhanced-mode: fake-ip\n  fake-ip-range: 198.18.0.1/16\n'}
        merge(profile, patch)
        profiles[ident] = profile
    document['profiles'] = profiles
    document['shared'].update(subscriptions=[], subscription_aggregations=[], nodes=[], rule_library=[])
    merge(document['shared'], options.get('shared', {}))
    document['system']['system_config'].update(default_profile_id='alpha', server_domain='http://panel.example',
        rule_proxy_token=CURRENT_PROXY, retired_rule_proxy_tokens=[RETIRED_PROXY],
        config_token='', github_proxy_domain='', rule_fetch_proxy='')
    document['system']['backup'].update(auto_backup=False, webdav_url='', webdav_username='', webdav_password='')
    document['system']['agents'] = []
    merge(document['system'], options.get('system', {}))
    repository.import_all(document)
    # Guard neutral background sources before either public server starts.
    stored = repository.export_all()
    if stored['system']['backup'].get('auto_backup') and not options.get('allow_backup'):
        raise ValueError('backup is allowed only in a dedicated X08 fixture')
    for item in stored['shared']['subscriptions'] + stored['shared']['rule_library']:
        if item.get('enabled', True) and item.get('interval', item.get('update_interval', 0)):
            raise ValueError('refreshable sources require a dedicated scheduler fixture')
    for relative, encoded in options.get('files', {}).items():
        path = (directory / relative).resolve()
        if (path == directory or not path.is_relative_to(directory)
                or path.relative_to(directory).parts[0].startswith('configflow.sqlite3')):
            raise ValueError('fixture file must stay inside its owned non-database DATA_DIR')
        content = base64.b64decode(encoded, validate=True)
        if len(content) > 4 * 1024 * 1024:
            raise ValueError('fixture file exceeds reviewed per-file limit')
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(content)
    for relative, mode in options.get('file_modes', {}).items():
        if relative not in options.get('files', {}) or type(mode) is not int or not 0 <= mode <= 0o777:
            raise ValueError('fixture file mode must name an explicitly seeded file and permission bits')
        (directory / relative).chmod(mode)
    for relative, mode in options.get('directory_modes', {}).items():
        path = (directory / relative).resolve()
        if (path == directory or not path.is_relative_to(directory) or not path.is_dir()
                or type(mode) is not int or not 0 <= mode <= 0o777):
            raise ValueError('fixture directory mode must name an owned seeded directory and permission bits')
        path.chmod(mode)
    return {'profiles': list(stored['profiles']), 'default_profile': repository.default_profile_id()}


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('directory', type=Path)
    parser.add_argument('--options', type=Path)
    args = parser.parse_args()
    options = json.loads(args.options.read_text()) if args.options else {}
    print(json.dumps(create(args.directory, options), ensure_ascii=False))
