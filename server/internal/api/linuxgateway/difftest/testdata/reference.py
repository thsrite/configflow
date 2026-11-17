"""A20 synthetic Flask HTTP + SQLite/file oracle; network restricted to loopback."""
import contextlib
import hashlib
import io
import json
import os
from pathlib import Path
import sqlite3
import sys
from urllib.parse import urlsplit
import requests
import yaml
from flask import Flask
from backend.common import config
from backend.common.config_repository import ProfileRepository
from backend.routes import register_blueprints
from backend.routes.auth import setup_before_request

root = Path(os.environ['DATA_DIR'])
repository = ProfileRepository(root)
config.set_repository(repository)
app = Flask(__name__)
register_blueprints(app)
setup_before_request(app)
original_request = requests.sessions.Session.request

def loopback(self, method, url, **kwargs):
    if urlsplit(url).hostname not in ('127.0.0.1', '::1', 'localhost'):
        raise AssertionError('non-loopback network forbidden')
    return original_request(self, method, url, **kwargs)
requests.sessions.Session.request = loopback

def state():
    files = {}
    for path in root.rglob('*'):
        if not path.is_file() or path.name.startswith('configflow.sqlite3') or path.suffix in ('.lock', '.log'):
            continue
        relative = path.relative_to(root).as_posix()
        raw = path.read_bytes()
        files[relative] = json.loads(raw) if path.suffix == '.json' else yaml.safe_load(raw) if path.suffix in ('.yaml', '.yml') else 'sha256:' + hashlib.sha256(raw).hexdigest()
    with sqlite3.connect(root / 'configflow.sqlite3') as connection:
        sections = connection.execute('SELECT scope, owner, position FROM sections ORDER BY scope, owner').fetchall()
        fields = [(scope,owner,field,position,json.loads(value)) for scope,owner,field,position,value in connection.execute('SELECT scope, owner, field, position, value FROM fields ORDER BY scope, owner, field')]
    return {'document': repository.export_all(), 'files': files, 'sections': sections, 'fields': fields}

for line in sys.stdin:
    try:
        request = json.loads(line)
        with contextlib.redirect_stdout(io.StringIO()):
            headers = request.get('headers', {})
            if request.get('body') is not None:
                headers['Content-Type'] = 'application/json'
            if request['method'] == 'EDIT':
                payload = json.loads(request['body'])
                repository.update_profile_transaction(request['path'], lambda c: c.update(payload))
                answer = {'status': 200, 'headers': {}, 'body': {}, 'state': state()}
            elif request['method'] == 'AGENT':
                payload = json.loads(request['body'])
                def update(c):
                    c['agents'][0].update(payload)
                repository.update_system_transaction(update)
                answer = {'status': 200, 'headers': {}, 'body': {}, 'state': state()}
            elif request['method'] == 'GENERATE':
                from backend.converters.mihomo import generate_mihomo_config
                config.reset_config_context()
                content = generate_mihomo_config(repository.get_compat_config(request['path']), 'http://localhost', preflight_providers=False)
                answer = {'status': 200, 'headers': {}, 'body': yaml.safe_load(content), 'state': state()}
            else:
                reply = app.test_client().open(request['path'], method=request['method'], headers=headers, data=request.get('body'))
                body = '' if request['method'] == 'HEAD' else reply.get_json() if reply.is_json else reply.get_data(as_text=True)
                answer = {'status': reply.status_code, 'headers': dict(reply.headers), 'body': body, 'state': state()}
        print(json.dumps(answer, ensure_ascii=False), flush=True)
    except Exception as error:
        print(json.dumps({'oracle_error': str(error)}), flush=True)
