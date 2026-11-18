"""Run actual Python correlation with no linked router; transport is tested separately."""
import json
import sys
from backend.agents.ipv6_client_evidence import evaluate
for line in sys.stdin:
    data = json.loads(line)
    print(json.dumps(evaluate({}, {'id': 'source'}, data['report'], data['trace'], data['url']), ensure_ascii=False))
