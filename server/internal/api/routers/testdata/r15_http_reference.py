"""Real Flask routers blueprint, repository and loopback drivers; no business stubs."""
import copy
import ipaddress
import json
import os
from pathlib import Path
import shutil
import sys
from urllib.parse import urlsplit

case = json.load(sys.stdin)
root = Path(os.environ['R15_DATA'])
os.environ['DATA_DIR'] = str(root / 'python')
from flask import Flask
from backend.common import config as config_module, auth
from backend.common.config_repository import ProfileRepository
from backend.routes import routers_bp, agents_bp
from backend.routes import routers, upstream_router_pending
repository = ProfileRepository(root / 'python')
config_module.set_repository(repository)
repository.create_profile({'id': 'other', 'name': 'Other'})
repository.save_shared(case.get('shared', {}))
repository.save_system(case['system'])
repository.save_profile('default', case['config'])
for router in case['config']['routers']['items']:
    assert ipaddress.ip_address(urlsplit(router['endpoint']).hostname).is_loopback
shutil.copytree(root / 'python', root / 'go')
auth.ADMIN_USERNAME = ''
auth.ADMIN_PASSWORD = ''
auth.JWT_SECRET_KEY = 'synthetic-r15-secret'
app = Flask(__name__)
app.config['TESTING'] = True
app.config['PROPAGATE_EXCEPTIONS'] = False
app.register_blueprint(routers_bp)
app.register_blueprint(agents_bp)
responses, stored, files, systems, shared = [], [], [], [], []
plan, route, workspace, created = {}, {}, {}, ''

def snapshots():
    out = {}
    for path in (root / 'python').rglob('*'):
        if path.is_file() and path.suffix in ('.json', '.yaml', '.yml'):
            out[str(path.relative_to(root / 'python'))] = path.read_text()
    return out

for step in case['requests']:
    body = copy.deepcopy(step.get('body'))
    if step.get('use') == 'plan': body = {'fingerprint': plan['fingerprint']}
    if step.get('use') == 'route': body['fingerprint'] = route['fingerprint']
    if step.get('use') == 'workspace':
        body['revision'] = workspace['revision']
        if 'policies' in body: body['names_revision'] = workspace['names_revision']
    path = step['path'].replace('$new', created)
    response = app.test_client().open(path, method=step['method'], json=body)
    payload = response.get_json() if step['method'] != 'HEAD' else None
    if payload and payload.get('plan'):
        if '/routes/' in path: route = payload['plan']
        else: plan = payload['plan']
    if payload and payload.get('workspace'): workspace = payload['workspace']
    if step.get('create') and payload and payload.get('router'): created = payload['router']['id']
    responses.append({'status': response.status_code, 'payload': payload, 'cache': response.headers.get('Cache-Control','')})
    if step.get('settle'):
        from backend.routers import rule_sync, rule_sync_store
        if not rule_sync._worker.acquire(timeout=5):
            raise RuntimeError('rule sync owner did not finish')
        rule_sync._worker.release()
        value = rule_sync_store.read('default', 'router-one')
        if value.get('status') != 'synced':
            raise RuntimeError('rule sync did not settle successfully: ' + str(value.get('message')))
        responses[-1]['settled'] = rule_sync_store.public(value, True)
    stored.append(repository.get_profile('default'))
    systems.append(repository.get_system())
    shared.append(repository.get_shared())
    files.append(snapshots())
print(json.dumps({'responses': responses, 'stored': stored, 'files': files, 'systems': systems, 'shared': shared}, ensure_ascii=False))
