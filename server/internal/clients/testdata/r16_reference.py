"""Real Flask boundary oracle for R16; only R17 reservation enrichment is disabled.

The Go HTTP adapter is test-only until R17 registers the production routes.
All network reads use actual vendor drivers against synthetic loopback servers.
"""
import copy
import ipaddress
import json
import os
import shutil
import sys
from pathlib import Path
from unittest.mock import patch
from urllib.parse import urlsplit

case = json.load(sys.stdin)
root = Path(os.environ['R16_DATA'])
os.environ['DATA_DIR'] = str(root / 'python')
from flask import Flask
from backend.common import config as config_store
from backend.common.config_repository import ProfileRepository
from backend.common import client_discovery, client_fixed_ip
from backend.common.client_devices import list_devices
from backend.common.oxidns_device_policy import apply_policies
from backend.routes.client_devices import client_devices_bp
from backend.common import auth
from backend.common.agent_manager import get_agent_manager

repository = ProfileRepository(root / 'python')
config_store.set_repository(repository)
repository.save_system({'system_config': {'rule_proxy_token': 'synthetic-rule-token'}})
repository.save_profile('default', case['config'])
config = repository.get_compat_config('default')
if case.get('policies'):
    repository.update_profile_transaction('default', lambda current: current.update(
        oxidns=apply_policies(current['oxidns'], config, case['policies'])))
if case.get('hand_edit'):
    import yaml
    def hand_edit(current):
        document = yaml.safe_load(current['oxidns']['native_config'])
        next(p for p in document['plugins'] if p['tag'] == 'cf_device_direct')['args'][0] = {'exec': '$forward_remote'}
        current['oxidns']['native_config'] = yaml.safe_dump(document)
    repository.update_profile_transaction('default', hand_edit)
initial = repository.get_compat_config('default')
for router in initial.get('routers', {}).get('items', []):
    assert ipaddress.ip_address(urlsplit(router['endpoint']).hostname).is_loopback
shutil.copytree(root / 'python', root / 'go')
app = Flask(__name__)
app.config['TESTING'] = True
app.register_blueprint(client_devices_bp)
auth.ADMIN_USERNAME = ''
auth.ADMIN_PASSWORD = ''
responses = []
stored = []
with patch.object(client_fixed_ip, 'enrich', lambda *_: None), patch.object(client_discovery, 'gateway_host', lambda _: ''), patch.object(get_agent_manager(), 'get_all_agents', lambda: []):
    for request in case['requests']:
        response = app.test_client().open(request['path'], method=request['method'], json=request.get('body'))
        payload = response.get_json() if request['method'] != 'HEAD' else None
        if payload and request['path'].split('?')[0] == '/api/client-devices' and response.status_code == 200:
            payload = {key: payload[key] for key in ('devices', 'editable', 'reason', 'side_entries')}
        responses.append({'status': response.status_code, 'payload': payload, 'cache_control': response.headers.get('Cache-Control')})
        stored.append(repository.get_compat_config('default'))
print(json.dumps({'initial': initial, 'responses': responses, 'after': repository.get_compat_config('default'), 'stored': stored}, ensure_ascii=False))
