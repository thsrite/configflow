"""C07 Flask/storage boundary on synthetic copied data; no background workers."""
import contextlib
from datetime import datetime, timezone
import io
import json
import os
from pathlib import Path
import sys

from flask import Flask
import requests
from backend.common import config
from backend.common.config_repository import ProfileRepository
from backend.routes import register_blueprints
from backend.routes.auth import setup_before_request
from backend.routes import stats, tasks
from backend.utils import notification_store

root = Path(os.environ['DATA_DIR'])
repository = ProfileRepository(root)
config.set_repository(repository)
app = Flask(__name__)
register_blueprints(app)
setup_before_request(app)

fixed = datetime(2026, 11, 18, 12, 0, 0, 123456, tzinfo=timezone.utc)
class FixedDateTime(datetime):
    @classmethod
    def now(cls, tz=None):
        return fixed.replace(tzinfo=None) if tz is None else fixed.astimezone(tz)
stats.datetime = FixedDateTime
notification_store.now = lambda: fixed.isoformat(timespec='microseconds')

def no_network(*args, **kwargs):
    raise AssertionError('C07 oracle refuses external I/O')
requests.sessions.Session.request = no_network
# Run admission is the real route with a supplied owner. C15 proves fetching;
# C07 checks category dispatch, request validation and HTTP admission only.
tasks.refresh.run_now = lambda category, resource: None

def state():
    files = {}
    for path in root.rglob('*'):
        if not path.is_file():
            continue
        if path.suffix == '.json':
            files[path.relative_to(root).as_posix()] = json.loads(path.read_text())
        elif path.name == 'app.log':
            files['app.log'] = path.read_text()
    return {'document': repository.export_all(), 'files': files}

commands = json.load(sys.stdin)
results = []
for command in commands:
    with contextlib.redirect_stdout(io.StringIO()):
        for name, value in command.get('write', {}).items():
            (root / name).write_text(json.dumps(value, ensure_ascii=False))
        response = app.test_client().open(command['path'], method=command['method'],
            headers=command.get('headers', {}), data=command.get('body'))
        body = '' if command['method'] == 'HEAD' else response.get_json() if response.is_json else response.get_data(as_text=True)
        results.append({'status': response.status_code, 'headers': dict(response.headers), 'body': body, 'state': state()})
json.dump(results, sys.stdout, ensure_ascii=False)
