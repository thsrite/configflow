"""A23 saved configuration and target selection through real Python leaves."""
import json
import sys
from backend.agents.ipv6_readiness import configuration, _probe_targets
for line in sys.stdin:
    source = json.loads(line)
    settings, document = configuration(source)
    print(json.dumps({'configuration': settings, 'targets': _probe_targets(source, document)}, ensure_ascii=False))
