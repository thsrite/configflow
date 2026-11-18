"""C34 real Flask oracle on a private synthetic repository, without workers."""
import contextlib
import io
import json
import math
import os
from pathlib import Path
import sys
from flask import Flask
import yaml
import requests
import zipfile
from backend.common.config_repository import ProfileRepository
from backend.common import config
from backend.routes import register_blueprints
from backend.routes.auth import setup_before_request

root = Path(os.environ['DATA_DIR'])
repository = ProfileRepository(root)
config.set_repository(repository)
app = Flask(__name__)
register_blueprints(app)
setup_before_request(app)

def no_network(*args, **kwargs):
    raise AssertionError('network forbidden in C34 HTTP differential')
requests.sessions.Session.request = no_network

def state():
    files = {}
    for path in root.rglob('*'):
        if not path.is_file() or path.name in ('app.log', '.config.lock') or path.name.startswith('configflow.sqlite3'):
            continue
        relative = path.relative_to(root).as_posix()
        if path.suffix == '.json':
            files[relative] = json.loads(path.read_text())
        elif path.suffix in ('.yaml', '.yml'):
            files[relative] = yaml.safe_load(path.read_text())
        else:
            files[relative] = list(path.read_bytes())
    return {'document': repository.export_all(), 'files': files}

def json_safe(value):
    # Preserve non-finite type information in the oracle transport. Go's
    # standard JSON decoder deliberately rejects the non-standard literals.
    if isinstance(value, float) and not math.isfinite(value):
        return {'$oracle_nonfinite': 'NaN' if math.isnan(value) else 'Infinity' if value > 0 else '-Infinity'}
    if isinstance(value, dict):
        return {key: json_safe(item) for key, item in value.items()}
    if isinstance(value, list):
        return [json_safe(item) for item in value]
    return value

for line in sys.stdin:
    try:
        request = json.loads(line)
        with contextlib.redirect_stdout(io.StringIO()):
            reply = app.test_client().open(request['path'], method=request['method'],
                headers=request.get('headers', {}), data=request.get('body'))
            text = reply.get_data(as_text=True) if not reply.mimetype == 'application/zip' else ''
            body = '' if request['method'] == 'HEAD' else reply.get_json() if reply.is_json else text
            if reply.mimetype == 'application/zip':
                with zipfile.ZipFile(io.BytesIO(reply.get_data())) as z:
                    body = {name: z.read(name).decode() for name in z.namelist() if not name.endswith('/')}
            if request['method'] != 'HEAD' and reply.mimetype == 'application/json' and isinstance(body,str):
                body=json.loads(body)
            result = {'status': reply.status_code, 'headers': dict(reply.headers), 'body': body, 'state': state()}
        print(json.dumps(json_safe(result), ensure_ascii=False, allow_nan=False), flush=True)
    except Exception as error:
        print(json.dumps({'oracle_error': str(error)}), flush=True)
