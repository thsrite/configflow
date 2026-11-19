"""Independent Python HTTP evidence for native machine-number refusal.

Run from repository root on the test server. Never reads Go outputs.
"""
import hashlib
import json
import tempfile
from pathlib import Path
from backend.test_agent_heartbeat_security import _heartbeat_app

huge = "1" + "0" * 400
inputs = [
    '{"system_metrics": {"memory": {"total": ' + huge + '}}}',
    '{"system_metrics": {"cpu": {"usage_percent": -' + huge + '}}}',
    '{"system_metrics": {"cpu": {"core_count": ' + huge + ', "usage_percent": true}}}',
    '{"version": 1' + '0' * 4300 + '}',
]
rows = []
with tempfile.TemporaryDirectory() as directory:
    app, repository, registered = _heartbeat_app(Path(directory))
    client = app.test_client()
    for body in inputs:
        response = client.post('/api/agents/' + registered['id'] + '/heartbeat',
                               data=body, content_type='application/json',
                               headers={'Authorization': 'Bearer ' + registered['token']})
        original = {'input': body, 'python_status': response.status_code,
                    'python_body': response.get_json()}
        encoded = json.dumps(original, sort_keys=True, ensure_ascii=True, separators=(',', ':')).encode()
        rows.append({'original': original, 'sha256': hashlib.sha256(encoded).hexdigest(),
                     'native_status': 400, 'native_body': {'success': False, 'message': 'Invalid heartbeat data'}})
path = Path('server/internal/api/agents/testdata/n810_heartbeat_policy.json')
path.write_text(json.dumps(rows, indent=2, ensure_ascii=True) + '\n')
