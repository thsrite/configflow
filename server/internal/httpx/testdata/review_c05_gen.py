#!/usr/bin/env python3
"""C05 references from real Python profile hooks and authentication gates.

Run via rt only. Views merely observe the selected profile; classification,
selection, memoization, error handling and both auth checks are the real code.
All data, credentials and paths are synthetic and the repository is temporary.
"""
import ast
from collections import defaultdict
import gzip
import json
import logging
import os
from pathlib import Path
import sys
import tempfile
from urllib.parse import urlencode

logging.disable(logging.CRITICAL)
os.environ.update(ADMIN_USERNAME='c05-admin', ADMIN_PASSWORD='c05-password',
                  JWT_SECRET_KEY='c05-synthetic-secret')
temporary = tempfile.TemporaryDirectory(prefix='review-c05-')
os.environ['DATA_DIR'] = temporary.name

from flask import Flask, g, jsonify, request
from backend.common.auth import generate_token, validate_token_or_jwt
from backend.common.config import get_config, reset_config_context, set_repository
from backend.common.config_repository import ProfileNotFound, ProfileRepository, ProfileValidationError
from backend.common.profile_context import (append_url_query, config_api_path, install_profile_context,
                                            profile_api_path, resolve_profile_id, _is_global_request)
from backend.routes.auth import setup_before_request

ROWS = []
HITS = defaultdict(set)
SOURCES = ('backend/common/profile_context.py', 'backend/routes/auth.py')


def trace(frame, event, arg):
    if event == 'line':
        for source in SOURCES:
            if frame.f_code.co_filename.endswith('/' + source):
                HITS[source].add(frame.f_lineno)
    return trace


repository = ProfileRepository(temporary.name)
set_repository(repository)
for identity in ('path', 'query', 'query-id', 'header', 'explicit', 'fallback'):
    repository.create_profile({'id': identity, 'name': identity})
app = Flask(__name__)


@app.errorhandler(ProfileValidationError)
def validation_error(error):
    return {'success': False, 'message': str(error)}, 400


@app.errorhandler(ProfileNotFound)
def not_found(error):
    return {'success': False, 'message': f'Profile not found: {error.args[0]}'}, 404


def observe(**kwargs):
    if '/proxies' in request.path or request.path.endswith('/provider'):
        result = validate_token_or_jwt(request, {'system_config': {'config_token': 'c05-config-token'}})
        if not result['valid']:
            return jsonify(result), 401
    profile = resolve_profile_id(request.args.get('explicit'), request.args.get('fallback'))
    first = get_config(profile)
    return {'profile': profile, 'middleware_profile': getattr(g, 'configflow_profile_id', None) or '',
            'config_profile': first['profile_id'], 'reused': first is get_config(profile)}


routes = [('/api/rules', 'rules.get_rules'), ('/api/nodes', 'nodes.get_nodes'),
          ('/api/profiles/<profile_id>/rules', 'profiles.profile_rules'),
          ('/api/subscriptions/s/proxies', 'subscriptions.get_subscription_proxies'),
          ('/api/subscriptions/all/proxies', 'subscriptions.get_all_subscription_proxies'),
          ('/api/aggregations/a/provider', 'subscription_aggregations.get_aggregation_provider'),
          ('/api/profiles/<profile_id>/subscriptions/s/proxies', 'profiles.profile_subscription_proxies'),
          ('/api/profiles/<profile_id>/aggregations/a/provider', 'profiles.profile_aggregation_provider'),
          ('/health', 'misc.health'), ('/api', 'misc.api'), ('/apiary/probe', 'misc.other')]
for blueprint in ('auth', 'settings', 'mcp', 'agents', 'logs', 'nodes', 'subscriptions',
                  'subscription_aggregations', 'rule_library', 'profiles', 'tasks', 'notifications',
                  'rules', 'oxidns', 'unknown'):
    routes.append((f'/api/classify/{blueprint}', f'{blueprint}.probe'))
for operation in ('export_config', 'import_config', 'reset_config', 'other'):
    routes.append((f'/api/classify/config/{operation}', f'config.{operation}'))
for path, endpoint in routes:
    app.add_url_rule(path, endpoint, observe)
install_profile_context(app)
setup_before_request(app)
client = app.test_client()
jwt = generate_token('c05-admin')
sys.settrace(trace)

# All sources of a selection, including present-but-empty, malformed and unknown
# IDs, are run through the whole pipeline. Distinct IDs reveal priority mistakes.
selections = [([], {}), ([('profile', 'query')], {}), ([('profile_id', 'query-id')], {}),
              ([], {'X-ConfigFlow-Profile': 'header'}),
              ([('profile_id', 'query-id')], {'X-ConfigFlow-Profile': 'header'}),
              ([('profile', 'query'), ('profile_id', 'query-id')], {'X-ConfigFlow-Profile': 'header'}),
              ([('profile', 'query'), ('profile', 'missing')], {}),
              ([('profile', '')], {'X-ConfigFlow-Profile': 'header'}),
              ([('profile_id', '')], {'X-ConfigFlow-Profile': 'header'}),
              ([], {'X-ConfigFlow-Profile': ''}), ([('profile', 'missing')], {}),
              ([('profile', '../escape')], {}), ([('profile', '中文')], {}),
              ([('explicit', 'explicit'), ('profile', 'query')], {}),
              ([('explicit', '')], {}), ([('fallback', 'fallback')], {}),
              ([('fallback', 'missing'), ('profile', 'query')], {}),
              ([('explicit', 'explicit'), ('fallback', 'fallback')], {})]
for route, endpoint in routes:
    paths = [route.replace('<profile_id>', 'path')]
    if '<profile_id>' in route:
        paths += [route.replace('<profile_id>', 'missing'), route.replace('<profile_id>', 'bad.id')]
    for path in paths:
        for index, (query, headers) in enumerate(selections):
            for logged_in in (False, True):
                params = query + ([('token', 'c05-config-token')] if '/proxies' in path or path.endswith('/provider') else [])
                url = path + ('?' + urlencode(params) if params else '')
                actual_headers = dict(headers)
                if logged_in:
                    actual_headers['Authorization'] = f'Bearer {jwt}'
                response = client.get(url, headers=actual_headers)
                ROWS.append({'fn': 'http', 'test': f'{endpoint}/{path}/{index}/{logged_in}',
                             'path': url, 'headers': headers, 'jwt': logged_in,
                             'status': response.status_code, 'out': response.get_json(),
                             'profile_header': response.headers.get('X-ConfigFlow-Profile', '')})

# The scoped provider's exemption removes only the JWT gate. It still needs its
# own config token (or a JWT). The global path always requires the JWT gate.
for path in ('/api/subscriptions/s/proxies', '/api/aggregations/a/provider',
             '/api/profiles/path/subscriptions/s/proxies', '/api/profiles/path/aggregations/a/provider'):
    for token in ('', 'wrong', 'c05-config-token'):
        for logged_in in (False, True):
            url = path + '?' + urlencode({'token': token, 'profile': 'query'})
            response = client.get(url, headers={'Authorization': f'Bearer {jwt}'} if logged_in else {})
            ROWS.append({'fn': 'http', 'test': f'provider/{path}/{token}/{logged_in}', 'path': url,
                         'headers': {}, 'jwt': logged_in, 'status': response.status_code,
                         'out': response.get_json(), 'profile_header': response.headers.get('X-ConfigFlow-Profile', '')})

for route, endpoint in routes:
    path = route.replace('<profile_id>', 'path')
    with app.test_request_context(path):
        ROWS.append({'fn': 'classify', 'test': endpoint, 'endpoint': endpoint,
                     'blueprint': request.blueprint, 'path': path, 'has_path': '<profile_id>' in route,
                     'out': _is_global_request()})

# Direct calls have a different no-route rule than the before-request hook.
# Include background calls, global calls, fallback bypass and a memo key sentinel.
for path in (None, '/not-a-route?profile=query', '/api/rules?profile=query', '/api/nodes?profile=query'):
    def calls():
        reset_config_context()
        for explicit, fallback in ((None, None), (None, None), ('explicit', None),
                                   (None, 'fallback'), ('', None), ('\0', None),
                                   ('missing', None), (None, None)):
            row = {'fn': 'resolve', 'test': f'{path}/{explicit!r}/{fallback!r}',
                   'request': path, 'explicit': explicit, 'fallback': fallback}
            try:
                row['out'] = resolve_profile_id(explicit, fallback)
            except Exception as error:
                row['error'] = type(error).__name__
            ROWS.append(row)
    if path is None:
        calls()
    else:
        with app.test_request_context(path):
            calls()

for profile in (None, '', 'path', 0, False):
    config = {'profile_id': profile}
    ROWS.append({'fn': 'paths', 'test': repr(profile), 'config': config,
                 'out': [profile_api_path(config, '/rules'), config_api_path(config, 'mihomo')]})
for url in ('https://panel.example/config?blank=&x=1&x=2#section', '/api/config?token=a%2Bb',
            'https://[2001:db8::1]:443/config', 'https://panel.example/中文',
            'https://panel.example/config?q=%FF&flag', 'https://[2001:db8::1/config',
            'https://[not-an-ip]/config', 'https://[192.0.2.1]/config'):
    for pairs in ([], [['token', 'a+b/c?d=e&中文']], [['x', ['', 'a b', 'a+b']], ['flag', None]],
                  [['x', []], ['enabled', True], ['zero', 0]], [['dict', {'a': 1, 'b': 2}]]):
        row = {'fn': 'url', 'test': f'{url}/{pairs}', 'url': url, 'params': pairs}
        try:
            row['out'] = append_url_query(url, [(key, value) for key, value in pairs])
        except Exception as error:
            row['error'] = type(error).__name__
            row['error_detail'] = str(error)
        ROWS.append(row)

sys.settrace(None)
output = Path(__file__).with_name('review_c05.jsonl.gz')
with output.open('wb') as stream, gzip.GzipFile(filename='', fileobj=stream, mode='wb', mtime=0) as compressed:
    for row in ROWS:
        compressed.write((json.dumps(row, ensure_ascii=False, separators=(',', ':')) + '\n').encode())
coverage = {}
for source in SOURCES:
    coverage[source] = {node.name: sorted(HITS[source] & set(range(node.lineno, node.end_lineno + 1)))
                        for node in ast.walk(ast.parse(Path(source).read_text())) if isinstance(node, ast.FunctionDef)}
output.with_suffix('').with_suffix('.coverage.json').write_text(json.dumps(coverage, indent=2) + '\n')
print(json.dumps({'rows': len(ROWS), 'by_fn': {fn: sum(r['fn'] == fn for r in ROWS) for fn in sorted({r['fn'] for r in ROWS})}}))
