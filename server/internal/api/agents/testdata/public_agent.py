"""Real Python reference for the shared public Agent boundary."""
import json
import sys

from backend.routes.agents import _public_agent
from backend.common.config_repository import AGENT_HISTORY_NOT_READ

for line in sys.stdin:
    request = json.loads(line)
    record = request['record']
    for field in request.get('unread', []):
        record[field] = AGENT_HISTORY_NOT_READ
    print(json.dumps(_public_agent(record), ensure_ascii=False), flush=True)
