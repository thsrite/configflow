"""Real Flask backup endpoints against an isolated loopback WebDAV server."""
import contextlib
from datetime import datetime, timezone
import io
import json
import os
from pathlib import Path
import sys

from flask import Flask
import requests
from backend.common.config_repository import ProfileRepository
from backend.common import config
from backend.routes import register_blueprints
from backend.routes.auth import setup_before_request
from backend.utils import webdav_backup, task_status, notification_store

root = Path(os.environ['DATA_DIR'])
repository = ProfileRepository(root)
config.set_repository(repository)
app = Flask(__name__)
register_blueprints(app)
setup_before_request(app)

fixed = datetime(2026, 10, 22, 12, 0, 0, 123456)
class FixedDateTime(datetime):
    @classmethod
    def now(cls, tz=None):
        return fixed if tz is None else fixed.replace(tzinfo=timezone.utc).astimezone(tz)
webdav_backup.datetime = FixedDateTime
task_status.now_iso = lambda: '2026-10-22T12:00:00Z'
notification_store.now = lambda: '2026-10-22T12:00:00.123456+00:00'
original_request = requests.sessions.Session.request
allowed = os.environ['FAKE_DAV_URL']
def loopback_only(self, method, url, **kwargs):
    if not url.startswith(allowed + '/'):
        raise AssertionError('non-fixture network request refused')
    return original_request(self, method, url, **kwargs)
requests.sessions.Session.request = loopback_only

def state():
    files = {}
    for path in root.rglob('*.json'):
        files[path.relative_to(root).as_posix()] = json.loads(path.read_text())
    return {'document': repository.export_all(), 'files': files}

for line in sys.stdin:
    try:
        command = json.loads(line)
        headers = command.get('headers', {})
        if command.get('body') is not None:
            headers['Content-Type'] = command.get('content_type', 'application/json')
        with contextlib.redirect_stdout(io.StringIO()):
            response = app.test_client().open(command['path'], method=command['method'],
                                               headers=headers, data=command.get('body'))
            if command.get('wait_auto'):
                worker = webdav_backup._worker
                with worker._condition:
                    thread = worker._thread
                if thread is not None:
                    thread.join(5)
                    assert not thread.is_alive(), 'automatic upload did not finish'
            body = response.get_json() if response.is_json else response.get_data(as_text=True)
            result = {'status': response.status_code, 'body': body, 'state': state()}
        print(json.dumps(result, ensure_ascii=False), flush=True)
    except Exception as error:
        print(json.dumps({'oracle_error': str(error)}), flush=True)
