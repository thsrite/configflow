"""Real read-only policy entry points; synthetic inputs only, no network I/O."""
import json
import sys
from backend.routers.ipv6_policy import evaluate_chain, policy_checks
out = []
for case in json.load(sys.stdin):
    snapshot = case['snapshot']
    errors = (case.get('errors') or [])
    # JSON null denotes unreadable, not a successfully read empty table.
    snapshot = {key: value for key, value in snapshot.items() if value is not None}
    if case['action'] == 'chain':
        out.append(evaluate_chain({**snapshot, '_errors': errors}, case['family'], case['table'], case['chain'], case['packet']))
    else:
        out.append(policy_checks(snapshot, errors, case['agent'], case['host'], case['configuration'], case['lan'], case['context']))
print(json.dumps(out, ensure_ascii=False))
