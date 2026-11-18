"""The real Python HTTP transport against a loopback protocol receiver."""
import json
import sys
from backend.agents.ipv6_readiness import _request
from backend.agents.oxidns_monitor import MonitorError
for line in sys.stdin:
    agent = json.loads(line)
    try:
        result = {'result': _request(agent)}
    except MonitorError as error:
        result = {'code': error.code, 'status': error.status, 'message': error.message}
    print(json.dumps(result, ensure_ascii=False))
