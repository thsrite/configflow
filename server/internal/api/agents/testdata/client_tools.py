"""A24 Flask boundary oracle: synthetic DB and files, no network/workers."""
import contextlib
import io
import json
import os
from pathlib import Path
import sys
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

def no_network(*args, **kwargs):
    raise AssertionError('A24 forbids external network')
requests.sessions.Session.request = no_network

def state():
    files = {}
    for path in root.rglob('*'):
        relative = path.relative_to(root).as_posix()
        if not path.is_file() or path.suffix not in ('.json', '.yaml'):
            continue
        text = path.read_text()
        files[relative] = json.loads(text) if path.suffix == '.json' else yaml.safe_load(text)
    return {'document': repository.export_all(), 'files': files}

for line in sys.stdin:
    try:
        request = json.loads(line)
        with contextlib.redirect_stdout(io.StringIO()):
            headers = request.get('headers', {})
            reply = app.test_client().open(request['path'], method=request['method'],
                data=request.get('body'), content_type='application/json', headers=headers)
            body = "" if request["method"] == "HEAD" else (reply.get_json() if reply.is_json else reply.get_data(as_text=True))
            result = {'status':reply.status_code,'body':body,'cache':reply.headers.get('Cache-Control',''),'state':state()}
        print(json.dumps(result, ensure_ascii=False), flush=True)
    except Exception as error:
        import traceback
        traceback.print_exc(file=sys.stderr)
        print(json.dumps({'error':str(error)}),flush=True)
