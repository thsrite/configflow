"""Real Flask application, real IPv6 routes; only the remote device boundary is fake."""
import contextlib
import io
import json
from pathlib import Path
import sys
import tempfile
import time
import pytest
from backend.test_agent_upgrade_auth import authenticated_app
from backend.agents import ipv6_readiness as bridge

def project(value):
    if isinstance(value, dict):
        return {k: ('opaque' if k == 'check_token' else 'timestamp' if k == 'checked_at' else project(v)) for k, v in value.items()}
    if isinstance(value, list):
        return [project(v) for v in value]
    return value

cases = json.load(sys.stdin)
with tempfile.TemporaryDirectory() as temp, pytest.MonkeyPatch.context() as patch, contextlib.redirect_stdout(io.StringIO()):
    fixture = authenticated_app.__wrapped__(Path(temp), patch)
    client, manager, _, admin, _ = next(fixture)
    manager.repository.create_profile({'id': 'other', 'name': 'Other'})
    agent = {'id': 'source', 'name': 'Synthetic', 'host': '192.0.2.2', 'port': 8080, 'token': 'synthetic-source-token',
             'profile_id': 'default', 'service_type': 'mihomo', 'deployment_method': 'shell', 'enabled': True,
             'status': 'online', 'last_heartbeat': bridge._now(), 'version': '1.8.50-go'}
    old_client = {'id': 'client', 'name': 'Client', 'host': '192.0.2.3', 'port': 8080,
                  'token': 'synthetic-client-token', 'profile_id': 'default', 'service_type': 'oxidns',
                  'deployment_method': 'shell', 'enabled': True, 'status': 'online',
                  'last_heartbeat': '2000-01-01T00:00:00+00:00'}
    manager._update_agents(lambda rows: rows.__setitem__(slice(None), [agent, old_client]))
    patch.setattr(manager, '_deployment_request', lambda *args, **kwargs: {})
    manager.repository.save_profile('default', {'mihomo': {'custom_config': 'ipv6: true\ndns: {ipv6: false}\n', 'linux_gateway': {'enabled': True}}})
    def request(target, operation='', payload=None):
        if operation == 'probe':
            return {'success': True, 'connectivity': {'status': 'ok', 'message': 'synthetic TCP'}}
        return {'success': True, 'interface': 'eth0', 'interface_index': 2, 'can_prepare': True,
                'checks': [{'id': 'interface', 'status': 'pass', 'message': 'synthetic evidence'}],
                'persistence': {}, 'core': {'ipv6': True, 'dns.ipv6': False}}
    patch.setattr(bridge, '_request', request)
    answers = []
    last_token = ""
    for case in cases:
        headers = dict(admin) if case['Admin'] else {}
        if case['Profile']:
            headers['X-ConfigFlow-Profile'] = case['Profile']
        response = client.open(case['Path'], method=case['Method'], data=case['Body'].replace('@last', last_token), content_type='application/json', headers=headers)
        data = response.get_json() if case['Method'] != 'HEAD' else None
        if isinstance(data, dict) and data.get('check_token'):
            last_token = data['check_token']
        answers.append({'status': response.status_code, 'body': project(data),
                        'cache': response.headers.get('Cache-Control', '')})
    try:
        next(fixture)
    except StopIteration:
        pass
print(json.dumps(answers, ensure_ascii=False))
