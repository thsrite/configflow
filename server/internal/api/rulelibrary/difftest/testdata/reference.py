"""C32 synthetic Flask HTTP + SQLite/file oracle; network restricted to loopback."""
import contextlib
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
from datetime import datetime, timezone
from backend.utils import task_status, notification_store, subscription_refresh
task_status.now_iso = lambda: '2026-01-01T00:00:00Z'
notification_store.now = lambda: '2026-01-01T00:00:00.000000+00:00'
class FixtureDateTime(datetime):
    @classmethod
    def now(cls, tz=None):
        return cls(2026, 1, 1, tzinfo=timezone.utc).astimezone(tz) if tz else cls(2026, 1, 1)
subscription_refresh.datetime = FixtureDateTime
from types import SimpleNamespace
from backend.utils import scheduled_refresh
scheduled_refresh.time = SimpleNamespace(monotonic=lambda: 0)
original_request = requests.sessions.Session.request

def loopback(self, method, url, **kwargs):
    if urlsplit(url).hostname not in ('127.0.0.1', '::1', 'localhost'):
        raise AssertionError('non-loopback network forbidden')
    return original_request(self, method, url, **kwargs)
requests.sessions.Session.request = loopback

def state():
    files = {}
    for path in root.rglob('*'):
        if not path.is_file() or path.suffix not in ('.json', '.yaml', '.list'):
            continue
        relative = path.relative_to(root).as_posix()
        if relative.endswith('.lock'):
            continue
        text = path.read_bytes().decode('utf-8')
        files[relative] = json.loads(text) if path.suffix == '.json' else yaml.safe_load(text) if path.suffix == '.yaml' else text
    with sqlite3.connect(root / 'configflow.sqlite3') as connection:
        sections = connection.execute('SELECT scope, owner, position FROM sections ORDER BY scope, owner').fetchall()
        fields = [(scope,owner,field,position,json.loads(value)) for scope,owner,field,position,value in connection.execute('SELECT scope, owner, field, position, value FROM fields ORDER BY scope, owner, field')]
    return {'document': repository.export_all(), 'files': files, 'sections': sections, 'fields': fields}

for line in sys.stdin:
    try:
        request = json.loads(line)
        with contextlib.redirect_stdout(io.StringIO()):
            headers = request.get('headers', {})
            if request.get('body') is not None and 'Content-Type' not in headers:
                headers['Content-Type'] = 'application/json'
            if request['method'] == 'DESIRED':
                from backend.routers.desired import compute_desired
                payload = json.loads(request['body'])
                attempts = []
                previous = requests.sessions.Session.request
                def reject_outbound(*args, **kwargs):
                    attempts.append(True)
                    raise AssertionError('offline desired must not issue requests')
                requests.sessions.Session.request = reject_outbound
                try:
                    body = compute_desired(repository.get_compat_config(request['path']), payload['gateway'], payload['rule_ids'])
                finally:
                    requests.sessions.Session.request = previous
                assert not attempts, 'offline desired issued network requests'
                answer = {'status': 200, 'headers': {'Content-Type': 'application/json'}, 'body': body, 'state': state()}
            elif request['method'] == 'TWIN_DRAFT':
                from concurrent.futures import ThreadPoolExecutor
                def submit():
                    return app.test_client().open(request['path'], method='PUT', headers=headers, data=request['body']).status_code
                with ThreadPoolExecutor(max_workers=2) as workers:
                    replies = list(workers.map(lambda _: submit(), range(2)))
                answer = {'status': 200, 'headers': {'Content-Type': 'application/json'}, 'body': {'statuses': sorted(replies)}, 'state': state()}
            else:
                reply = app.test_client().open(request['path'], method=request['method'], headers=headers, data=request.get('body'))
                body = '' if request['method'] == 'HEAD' else reply.get_json() if reply.is_json else reply.get_data(as_text=True)
                answer = {'status': reply.status_code, 'headers': dict(reply.headers), 'body': body, 'state': state()}
        print(json.dumps(answer, ensure_ascii=False), flush=True)
    except Exception as error:
        print(json.dumps({'oracle_error': str(error)}), flush=True)
