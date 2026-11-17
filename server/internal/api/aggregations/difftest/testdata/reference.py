"""Synthetic C31 boundary oracle. No background workers or real network access."""
import contextlib
from datetime import datetime, timezone
import io
import json
import os
from pathlib import Path
import sys
from flask import Flask
import yaml

from backend.common.config_repository import ProfileRepository
from backend.common import config
from backend.routes import register_blueprints
from backend.routes.auth import setup_before_request
from backend.utils import sub_store_client
from backend.routes import subscriptions, aggregations
import requests

root = Path(os.environ['DATA_DIR'])
repository = ProfileRepository(root)
config.set_repository(repository)
app = Flask(__name__)
register_blueprints(app)
setup_before_request(app)
control = {}
# Receipts/evidence use the same injected clock as the Go collaborators.
from backend.utils import subscription_health, task_status, notification_store
subscription_health._now = lambda: '2026-01-01T00:00:00Z'
task_status.now_iso = lambda: '2026-01-01T00:00:00Z'
notification_store.now = lambda: '2026-01-01T00:00:00.000000+00:00'
from backend.utils import subscription_refresh
class FixtureDateTime(datetime):
    @classmethod
    def now(cls, tz=None):
        return cls(2026, 1, 1, tzinfo=timezone.utc).astimezone(tz) if tz else cls(2026, 1, 1)
subscription_refresh.datetime = FixtureDateTime


def no_network(*args, **kwargs):
    raise AssertionError('real network forbidden in C31 differential')
requests.sessions.Session.request = no_network

def fetch(sub_id, url, **kwargs):
    if control.get('offline'):
        raise OSError('synthetic offline')
    if control.get('mutate'):
        control['mutate'] = False
        def change(profile):
            profile['proxy_groups'][0]['name'] = 'Changed'
            profile['proxy_groups'][0]['manual_nodes'] = ['exit']
        repository.update_profile_transaction('default', change)
    text = control.get('sources', {}).get(sub_id, 'proxies: []')
    return sub_store_client.SubscriptionResult(text, 'rendered_yaml', fetched_at='2026-01-01T00:00:00+00:00')

def convert(text, target='ClashMeta'):
    if text not in ('demo', 'opaque://exit'):
        return None
    proxy = {'name': 'Converted', 'type': 'http', 'server': 'example.com', 'port': 80}
    if text == 'opaque://exit' and control.get('uri_chain'):
        proxy['dialer-proxy'] = 'relay'
    return proxy
sub_store_client.get_subscription_proxies_yaml = fetch
subscriptions.get_subscription_proxies_yaml = fetch
aggregations.get_subscription_proxies_yaml = fetch
sub_store_client.convert_proxy_string = convert

def state():
    files = {}
    for path in root.rglob('*'):
        if not path.is_file() or path.suffix not in ('.json', '.yaml'):
            continue
        relative = path.relative_to(root).as_posix()
        text = path.read_text()
        files[relative] = json.loads(text) if path.suffix == '.json' else yaml.safe_load(text)
    return {'document': repository.export_all(), 'files': files}

for line in sys.stdin:
    try:
        request = json.loads(line)
        control = request.get('control', {})
        headers = request.get('headers', {})
        if request.get('body') is not None:
            headers['Content-Type'] = 'application/json'
        # Library diagnostics cannot corrupt the JSON-lines protocol.
        with contextlib.redirect_stdout(io.StringIO()):
            if request['method'] == 'GENERATE':
                args = json.loads(request['body'])
                try:
                    rendered = aggregations.generate_aggregation_provider(args['aggregation'], config=args['config'],
                        main_config=args.get('main'), persist=False, subscription_fetch=fetch,
                        source_prepared=args.get('prepared', False))
                    rendered.pop('cache_updates', None)
                    rendered['content'] = yaml.safe_load(rendered['content'])
                    result = {'status': 200, 'headers': {'Content-Type': 'application/json'}, 'body': rendered, 'comments': [], 'state': state()}
                except Exception as error:
                    from backend.utils.strategy_references import StrategyReferenceError
                    status = 400 if isinstance(error, StrategyReferenceError) else 500
                    result = {'status': status, 'headers': {'Content-Type': 'application/json'},
                              'body': {'success': False, 'message': str(error) if status == 400 else '聚合操作失败'}, 'comments': [], 'state': state()}
            else:
                reply = app.test_client().open(request['path'], method=request['method'], headers=headers,
                                               data=request.get('body'))
                text = reply.get_data(as_text=True)
                if reply.is_json:
                    body = reply.get_json()
                elif 'yaml' in reply.content_type:
                    body = yaml.safe_load(text)
                else:
                    body = text
                result = {'status': reply.status_code, 'headers': dict(reply.headers), 'body': body, 'comments': [line for line in text.splitlines() if line.startswith('#')] if 'yaml' in reply.content_type else [], 'state': state()}
        print(json.dumps(result, ensure_ascii=False), flush=True)
    except Exception as error:
        print(json.dumps({'oracle_error': str(error)}), flush=True)
