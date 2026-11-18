"""Real Flask route, repository and vendor-driver oracle; loopback only."""
import ipaddress
import json
import os
from pathlib import Path
import shutil
import sys
from urllib.parse import urlsplit

case = json.load(sys.stdin)
root = Path(os.environ['R17_DATA'])
os.environ['DATA_DIR'] = str(root / 'python')
from flask import Flask
from backend.common import config as config_module
from backend.common.config_repository import ProfileRepository
from backend.common import auth
from backend.routes.client_devices import client_devices_bp
repository = ProfileRepository(root / 'python')
config_module.set_repository(repository)
repository.save_system({'system_config': {'rule_proxy_token':'synthetic-rule-token'}})
repository.save_profile('default',case['config'])
for router in case['config'].get('routers',{}).get('items',[]):
    assert ipaddress.ip_address(urlsplit(router['endpoint']).hostname).is_loopback
shutil.copytree(root / 'python',root / 'go')
auth.ADMIN_USERNAME = ''
auth.ADMIN_PASSWORD = ''
auth.JWT_SECRET_KEY = 'synthetic-fixed-secret'
app = Flask(__name__)
app.config['TESTING'] = True
app.register_blueprint(client_devices_bp)
responses, stored, token = [], [], ''
for request in case['requests']:
    body = request.get('body')
    if request.get('use_token'):
        body = {**body,'token':token}
    response = app.test_client().open(request['path'],method=request['method'],json=body)
    payload = response.get_json() if request['method'] != 'HEAD' else None
    if payload and payload.get('token'):
        token = payload['token']
    responses.append({'status':response.status_code,'payload':payload,'cache':response.headers.get('Cache-Control')})
    stored.append(repository.get_profile('default'))
print(json.dumps({'responses':responses,'stored':stored},ensure_ascii=False))
