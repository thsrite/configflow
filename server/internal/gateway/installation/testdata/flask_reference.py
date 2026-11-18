"""Real Flask routes with real installation owners and isolated Agent transports."""
import contextlib
import io
import json
import os
from pathlib import Path
import sys
import threading
from urllib.parse import urlsplit, urlunsplit
import requests
from flask import Flask
from backend.common import config
from backend.common.config_repository import ProfileRepository
from backend.agents import ipv6_verification
from backend.routes import register_blueprints
from backend.routes.auth import setup_before_request

root = Path(os.environ['DATA_DIR'])
repository = ProfileRepository(root)
config.set_repository(repository)
ipv6_verification.enroll = lambda *args, **kwargs: None  # separate A23 owner
app = Flask(__name__)
register_blueprints(app)
setup_before_request(app)
destinations = json.loads(os.environ['A22_LOOPBACK_DESTINATIONS'])
original = requests.sessions.Session.request

def loopback(self, method, url, **kwargs):
    parsed = urlsplit(url)
    destination = destinations.get(parsed.netloc)
    if not destination:
        raise AssertionError('unregistered network origin forbidden')
    target = urlsplit(destination)
    assert target.hostname in ('127.0.0.1', 'localhost', '::1')
    redirected = urlunsplit((target.scheme, target.netloc, parsed.path, parsed.query, parsed.fragment))
    return original(self, method, redirected, **kwargs)

requests.sessions.Session.request = loopback

def state():
    files = {str(path.relative_to(root)): json.loads(path.read_bytes())
             for path in (root / 'gateway-installations').glob('*.json')}
    return dict(document=repository.export_all(), files=files)

for line in sys.stdin:
    try:
        request = json.loads(line)
        with contextlib.redirect_stdout(io.StringIO()):
            if request['method'] == 'WAIT':
                for thread in threading.enumerate():
                    if thread.name.startswith('gateway-onboarding-'):
                        thread.join(timeout=30)
                        assert not thread.is_alive(), 'publication did not finish'
                answer = dict(status=200, headers={}, body={}, state=state())
            elif request['method'] == 'EDIT':
                payload = json.loads(request['body'])
                repository.update_profile_transaction(request['path'], lambda current: current.update(payload))
                answer = dict(status=200, headers={}, body={}, state=state())
            else:
                headers = request.get('headers', {})
                # Match _body's deliberate disregard for Content-Type.
                headers['Content-Type'] = 'application/json' if request['path'] == '/api/auth/login' else 'text/plain'
                reply = app.test_client().open(request['path'], method=request['method'], headers=headers, data=request.get('body'))
                body = '' if request['method'] == 'HEAD' else reply.get_json() if reply.is_json else reply.get_data(as_text=True)
                answer = dict(status=reply.status_code, headers=dict(reply.headers), body=body, state=state())
        print(json.dumps(answer, ensure_ascii=False), flush=True)
    except Exception as error:
        print(json.dumps({'oracle_error': str(error)}), flush=True)
