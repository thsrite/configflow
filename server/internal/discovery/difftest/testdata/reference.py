"""Synthetic, loopback-only Flask reference for the C37 HTTP differential."""
import json
import os
import sys
from datetime import datetime
from pathlib import Path

from backend.common import config
from backend.common.config_repository import ProfileRepository
from backend.common.agent_manager import init_agent_manager

repository = ProfileRepository(os.environ['DATA_DIR'])
config.set_repository(repository)
if sys.argv[1] == 'seed':
    port = int(sys.argv[2])
    repository.save_shared({'rule_library': [
        {'id': 'd1', 'name': 'Direct', 'source_type': 'content', 'behavior': 'domain', 'content': '+.exists.example.net\n', 'future_field': {'keep': True}},
        {'id': 'p1', 'name': 'Proxy', 'source_type': 'content', 'behavior': 'classical', 'content': ''},
        {'id': 'u1', 'name': 'Remote', 'source_type': 'url', 'behavior': 'classical', 'url': 'https://example.invalid/list'},
    ]})
    repository.save_profile('default', {
        'proxy_groups': [{'id': 'g1', 'name': 'PROXY', 'type': 'select'}, {'id': 'g2', 'name': 'Region', 'type': 'select'}],
        'rule_configs': [
            {'id': 'earlier', 'itemType': 'rule', 'rule_type': 'DOMAIN-SUFFIX', 'value': 'pending.example.org', 'policy': 'PROXY', 'enabled': True, 'future': 'keep'},
            {'id': 'ref-d', 'itemType': 'ruleset', 'library_rule_id': 'd1', 'policy': 'DIRECT', 'enabled': True},
            {'id': 'ref-p', 'itemType': 'ruleset', 'library_rule_id': 'p1', 'policy': 'PROXY', 'enabled': True},
            {'id': 'fallback', 'itemType': 'rule', 'rule_type': 'MATCH', 'value': '', 'policy': 'DIRECT', 'enabled': True},
        ],
        'domain_discovery': {'direct_ruleset': 'd1', 'proxy_ruleset': 'p1', 'auto_apply_min_confidence': 80.0,
                             'region_probe_port': 'bad', 'ignored': [None, 7], 'history': [None, 'invalid', {}]},
        'mihomo': {'custom_config': 'mixed-port: 18001\nlisteners:\n  - name: custom\n    type: http\n    port: 18002\n'},
    })
    repository.create_profile({'id': 'other', 'name': 'Other'})
    manager = init_agent_manager()
    a = manager.register_agent({'name': 'Synthetic', 'host': '127.0.0.1', 'port': port, 'service_type': 'mihomo'})
    system = repository.get_system()
    system['agents'][0].update(id='agent-1', domain_discovery_enabled=True, version='1.6.0-go')
    repository.save_system(system)
    from backend.agents.traffic_store import get_traffic_store
    def item(host, conns=1, fails=0):
        return {'host': host, 'port': 443, 'network': 'tcp', 'rule': 'Match', 'rule_payload': '', 'policy': 'DIRECT',
                'outlet': 'direct', 'conns': conns, 'fails': fails, 'fail_kinds': {'timeout': fails} if fails else {}, 'zero_dl': 0, 'up': 10, 'down': 20}
    at = datetime.now().replace(microsecond=0).isoformat()
    get_traffic_store().add_report('agent-1', {'schema': 1, 'status': 'running', 'mihomo_version': 'v1.19.0',
        'window_start': at+'Z', 'window_end': at+'Z', 'ip_only_conns': 5, 'dropped': 0,
        'items': [item('www.example.com', 1, 8), item('cdn.example.com', 4), item('api.pending.example.org', 2), item('noise.example.dev', 3)]})
    sys.exit(0)

from flask import Flask, jsonify
from backend.routes import register_blueprints
app = Flask(__name__)
register_blueprints(app)
init_agent_manager()

@app.get('/__state')
def state():
    config.reset_config_context()
    p = repository.get_profile('default')
    shared = repository.get_shared()
    root = Path(os.environ['DATA_DIR'])
    files = {}
    for folder in ('probes', 'regions'):
        files[folder] = {p.stem: json.loads(p.read_text()) for p in (root/folder).glob('*.json')}
    files['cache'] = sorted(p.read_text() for p in (root/'shared'/'rules').glob('*') if p.is_file() and not p.name.endswith('.lock'))
    return jsonify(profile={k: p.get(k) for k in ('domain_discovery', 'rule_configs')}, library=shared['rule_library'], files=files)

from waitress import serve
serve(app, host='127.0.0.1', port=int(sys.argv[2]), threads=8)
