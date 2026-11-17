"""Real Python provider-response boundary with synthetic, delayed rendering only."""
from concurrent.futures import ThreadPoolExecutor
import json
from pathlib import Path
import sys
import threading
import time

from flask import Flask, Response
from backend.common import config
from backend.common.config_repository import ProfileRepository
from backend.utils import provider_response

request = json.load(sys.stdin)
repository = ProfileRepository(Path(request['directory']))
repository.create_profile({'id': 'alpha', 'name': 'Alpha'})
config.set_repository(repository)
old = 'proxies: [{name: OldOne}, {name: OldTwo}]\n'
fresh = 'proxies: [{name: Fresh}]\n'
repository.write_profile_text('alpha', 'providers/a.yaml', old)
release = threading.Event()
if request['mode'] == 'fresh':
    sys.path.insert(0, str(Path(__file__).resolve().parents[4] / 'tools/proddiff'))
    from provider_policy import wait_for_fresh_provider
    wait_for_fresh_provider()


def render():
    if request['mode'] == 'fresh':
        time.sleep(0.25)
    else:
        # Keep the worker blocked until the real production deadline replies.
        release.wait()
    return Response(fresh, mimetype='text/yaml')


app = Flask(__name__)
with ThreadPoolExecutor(max_workers=8) as pool:
    provider_response._pool = pool
    with app.test_request_context(request['path']):
        response = provider_response.serve_provider('aggregation', 'a', 'alpha', {'fixture': request['path']}, render)
        answer = {'status': response.status_code, 'body': response.get_data(as_text=True),
                  'marker': response.headers.get('X-ConfigFlow-Provider', '')}
        release.set()
answer['stored'] = repository.profile_path('alpha', 'providers/served/aggregation-a.yaml').read_text()
json.dump(answer, sys.stdout)
