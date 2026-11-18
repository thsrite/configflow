"""Compare only generated cf-pd source bytes for synthetic parameters."""
import json
import sys
from backend.routers.ipv6_pd_guard import watch_source
lan, pool = json.load(sys.stdin)
print(json.dumps(watch_source(lan, pool), ensure_ascii=False))
