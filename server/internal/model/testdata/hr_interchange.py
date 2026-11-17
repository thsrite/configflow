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
# Real Python stable-resource writer and strict classifier provide additional
# non-SQLite ownership/baseline shapes without involving a device.
from backend.routers.ros_policy_write import _resource
from backend.routers.rule_sync_rules import classify
resource = _resource('/ip/route', {'.id': '*synthetic', 'dst-address': '198.51.100.0/24',
    'gateway': '192.0.2.1', 'future-config': {'ordered': [1, True, None]}, 'bytes': 123})
policy = {'spec': {'sources': ['192.0.2.0/24'], 'destinations': ['198.51.100.0/24'],
    'gateway': '192.0.2.1', 'naming_version': 1, 'policy_version': 1}, 'resources': [resource]}
router = {'id': 'router-example', 'verify_tls': False,
    'dns_guard': {'enabled': True, 'failover': False}, 'ros_policy_state': policy,
    'dns_guard_state': {'version': 1, 'spec': {'agent': 'agent-example', 'interface': 'bridge-example',
        'network': '192.0.2.0/24', 'bypass': [], 'block_dot': False, 'failover': False, 'exempt': []},
        'resources': [{'role': 'hijack', **resource}]},
    'ipv6_fakeip_state': {'version': 1, 'spec': {'cidr': '2001:db8::/64', 'gateway': ''}, 'resources': [resource]},
    'ipv6_routes_state': {'version': 1, 'gateway': '2001:db8::1', 'resources': [resource]},
    'ipv6_pd_guard_state': {'version': 1, 'spec': {'lan': 'bridge-example', 'pool': 'pool-example',
        'client': '*client-example', 'client_interface': 'ether-example', 'hold': 3600},
        'resources': [{'role': 'hook', 'path': '/ipv6/dhcp-client', 'id': '*client-example'}]}}
rule = {**classify('IP-CIDR,198.51.100.0/24\nDOMAIN,example.com'),
    'name': 'Synthetic rule', 'source_type': 'content', 'library_id': 'library-example'}
receipt = {'status': 'synced', 'message': 'synthetic', 'rules': {'rule-example': rule},
    'baseline': {'control': 'opaque-control', 'rules': {'rule-example': rule},
        'desired': {'fake_ip_range': '198.18.0.0/16', 'proxy_gateway': '192.0.2.1',
                    'proxy_cidrs': [{'cidr': '198.51.100.0/24', 'sources': ['Synthetic rule']}], 'warnings': []},
        'static_resources': [resource], 'policy': policy,
        'nat_list': {'rule': resource, 'src': [resource]}}}
rows.extend([{'name': 'Router', 'value': router}, {'name': 'RouterRuleSyncReceipt', 'value': receipt}])
with open(sys.argv[1], 'w') as handle:
    json.dump(rows, handle, ensure_ascii=True)
