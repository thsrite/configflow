"""Real Python forward hints; every request targets the supplied loopback fake."""
import json
import os
import sys
from urllib.parse import urlsplit
import requests
from backend.routers import forward_hint
from backend.routers.base import RouterState
from backend.routers.routeros import RouterOSDriver
case = json.load(sys.stdin)
endpoint = os.environ['R09_ENDPOINT']
assert urlsplit(endpoint).hostname in ('127.0.0.1', '::1')
requests.post(endpoint + '/__r04/reset', json=case['state'], timeout=5).raise_for_status()
driver = RouterOSDriver(endpoint, 'synthetic-user', 'synthetic-password', verify_tls=False)
try:
    result = forward_hint.check(driver, case.get('family', 4), case['lan'], case['targets'])
    state = RouterState(routes=[], nat_bypass=[], address_sets={})
    state.nat_context = {'routes': case['state']['tables'].get('/ip/route', [])}
    hint = forward_hint.ipv4(case['router'], case['desired'], state, case['report'], driver)
finally:
    driver.close()
snapshot = requests.get(endpoint + '/__r04/state', timeout=5).json()
print(json.dumps({'warning4': forward_hint.drop_warning(4), 'warning6': forward_hint.drop_warning(6), 'check': result, 'hint': hint, 'calls': snapshot['calls'], 'tables': snapshot['tables']}, ensure_ascii=False))
