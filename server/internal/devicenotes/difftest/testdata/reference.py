"""Real public device-notes operations, synthetic data, no network."""
import json
import sys
from backend.common import device_notes as notes
for line in sys.stdin:
    request = json.loads(line)
    config = request['config']
    try:
        op = request['op']
        if op == 'set':
            notes.set_note(config, request['ip'], request['name'])
            value = config['device_notes']
        elif op == 'prepare':
            value = notes.prepare_policy_notes(config, request['rows'])
        elif op == 'save':
            settings = config.setdefault('oxidns', {})
            out = notes.save_policy_rows(config, settings, request['rows'], 'http://127.0.0.1:5001')
            value = {'same': out is settings, 'settings': out, 'notes': config['device_notes']}
        else:
            value = getattr(notes, op)(config)
        reply = {'value': value}
    except Exception as error:
        reply = {'error': str(error)}
    print(json.dumps(reply, ensure_ascii=False), flush=True)
