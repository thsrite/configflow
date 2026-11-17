"""Reference the actual shared Python three-valued packet evaluator."""
import json
import sys
from backend.routers import ipv6_policy as policy
out = []
for case in json.load(sys.stdin):
    snapshot = dict(case['snapshot'])
    # R09's typed snapshot contains one family's filter and address-list rows.
    if case.get('family', 4) == 4:
        snapshot['ipv4_filter'] = snapshot.pop('filter')
        snapshot['ipv4_address_lists'] = snapshot.pop('address_lists')
    family = case.get('family', 4)
    if snapshot.get(('ipv4_' if family == 4 else '') + 'filter') is None:
        snapshot.pop(('ipv4_' if family == 4 else '') + 'filter', None)
    if snapshot.get(('ipv4_' if family == 4 else '') + 'address_lists') is None:
        snapshot.pop(('ipv4_' if family == 4 else '') + 'address_lists', None)
    out.append({'match': policy._rule_match(snapshot, family, case['row'], case['packet']),
                'decision': policy.evaluate_chain(snapshot, family, 'filter', 'forward', case['packet'])['decision']})
print(json.dumps(out))
