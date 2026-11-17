"""Synthetic Python writers for HR records; no device or production access."""
import json
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[4]))
from backend.agents.operation_events import append_event
from backend.agents.oxidns_ha import _record_leftover

class Manager:
    def __init__(self):
        self.agents = [{'id': 'agent-example', 'enabled': False}]

    def _update_agents(self, callback):
        return callback(self.agents)

manager = Manager()
_record_leftover(manager, ['agent-example', 'peer-example'], 'synthetic failure',)
events = append_event([], 'preparing', 'synthetic preparation', at='2026-01-01T00:00:00+00:00')
agent = manager.agents[0]
agent.update(dns_follow={'after_deployment_id': None}, deployments={'task-example': {
    'deployment_id': 'task-example', 'status': 'preparing', 'submitted': True,
    'events': events, 'config_revision': None,
}}, latest_deployment={'deployment_id': 'task-example', 'status': 'checking'},
    rule_sync={'settings': {'library': True, 'rules': False},
               'state': {'status': 'running', 'native_task': 'native-example', 'retry_at': 1.25}},
    dns_follow_job={'binding_id': 'binding-example', 'profile_id': 'profile-example',
                    'original_settings_digest': 'stored-digest', 'candidate_signature': 'stored-signature',
                    'candidate_settings': {'plugins': [{'tag': 'example', 'args': {'future': [1, None]}}]}},
    oxidns_ha_publication={'id': 'ha-example', 'active_agent_id': None, 'enable': None,
        'handoff_required': True, 'handoff': {'operation_id': 'ha-example',
        'source_agent_id': 'agent-example', 'target_agent_id': 'peer-example',
        'requested_at': 1.25, 'confirmed': False, 'released': False, 'confirmations': 0}})
rows = [{'name': 'Agent', 'value': agent}]
with open(sys.argv[1], 'w') as handle:
    json.dump(rows, handle, ensure_ascii=True)
