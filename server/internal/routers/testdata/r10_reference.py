"""Real Python R10 entry points, synthetic stdin, loopback RouterOS only."""
import copy
import ipaddress
import json
import os
import sys
from urllib.parse import urlsplit
import requests
from backend.routers.routeros import RouterOSDriver
from backend.routers import ipv6_fakeip, ipv6_routes

case = json.load(sys.stdin)
endpoint = os.environ['R09_ENDPOINT']
assert ipaddress.ip_address(urlsplit(endpoint).hostname).is_loopback
requests.post(endpoint + '/__r04/reset', json=case['state'], timeout=5).raise_for_status()
driver = RouterOSDriver(endpoint, 'synthetic-user', 'synthetic-password', verify_tls=False)
module = ipv6_fakeip if case['feature'] == 'fake' else ipv6_routes
attribute = 'ipv6_fakeip_manifest' if case['feature'] == 'fake' else 'ipv6_routes_manifest'
field = 'ipv6_fakeip_state' if case['feature'] == 'fake' else 'ipv6_routes_state'
out = {'results': [], 'error': None, 'manifest': None}
undo = None
plan = None
try:
    for step in case['steps']:
        if step.get('mutation'):
            requests.post(endpoint + '/__r04/mutate', json=step['mutation'], timeout=5).raise_for_status()
        action = step['action']
        if action == 'undo':
            out['results'].append(module.restore(driver, undo))
            continue
        if action == 'apply':
            undo = driver.apply(plan['ops'][0])
            out['results'].append(undo)
            continue
        router = copy.deepcopy(case['router'])
        if hasattr(driver, attribute):
            router[field] = copy.deepcopy(getattr(driver, attribute))
        args = [step['config'], router]
        if case['feature'] != 'fake':
            args.append(step['desired'])
        args += [step.get('agent'), step.get('host', {}), driver]
        plan = module.inspect(*args)
        out['results'].append(copy.deepcopy(plan))
        if action == 'plan' or plan.get('blockers') or plan.get('skip'):
            continue
        if plan['ops']:
            undo = driver.apply(plan['ops'][0])
            out['results'].append(undo)
        module.verify(driver, plan['check'])
        if action == 'rollback' and plan['ops']:
            out['results'].append(module.restore(driver, undo))
except Exception as error:
    out['error'] = {'class': type(error).__name__, 'message': str(error)}
finally:
    out['manifest'] = getattr(driver, attribute, None)
    driver.close()
print(json.dumps(out, ensure_ascii=False))
