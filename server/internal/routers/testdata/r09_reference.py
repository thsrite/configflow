"""Real Python R09 entry points against an isolated loopback RouterOS fake."""
import copy
import json
import os
import sys
from urllib.parse import urlsplit
import requests
from backend.routers import dns_guard, forward_hint, ipv6_policy
from backend.routers.routeros import RouterOSDriver

case = json.load(sys.stdin)
endpoint = os.environ['R09_ENDPOINT']
assert urlsplit(endpoint).hostname in ('127.0.0.1', '::1')
requests.post(endpoint + '/__r04/reset', json=case['state'], timeout=5).raise_for_status()
driver = RouterOSDriver(endpoint, 'synthetic-user', 'synthetic-password', verify_tls=False)
out = {'steps': []}
previous = case.get('previous')
desired = case['desired']
settings = case['settings']
try:
    for step in case['steps']:
        if step.get('mutation'):
            requests.post(endpoint + '/__r04/mutate', json=step['mutation'], timeout=5).raise_for_status()
        settings = step.get('settings', settings)
        desired = step.get('desired', desired)
        driver.dns_guard_manifest = previous
        state = driver.read_state()
        state.dns_guard = dns_guard.snapshot(driver)
        request = {**copy.deepcopy(desired), 'dns_guard': {'settings': settings, 'previous': previous}}
        if case.get('policy_cidrs'):
            request['ros_policy'] = {'cidrs': case['policy_cidrs']}
        plan = dns_guard.build(request, state)
        row = {'plan': plan, 'undo': None, 'error': None, 'manifest': previous}
        if step.get('after_preview'):
            requests.post(endpoint + '/__r04/mutate', json=step['after_preview'], timeout=5).raise_for_status()
        if step['action'] != 'plan' and not plan['blockers']:
            try:
                if plan['ops']:
                    row['undo'] = driver.apply(plan['ops'][0])
                if step['action'] == 'roundtrip' and row['undo']:
                    driver.apply(row['undo'])
                else:
                    dns_guard.verify(driver, {'roles': plan['roles']})
            except Exception as error:
                row['error'] = {'class': type(error).__name__, 'message': str(error)}
            previous = driver.dns_guard_manifest
            row['manifest'] = previous
        out['steps'].append(row)
finally:
    driver.close()
snapshot = requests.get(endpoint + '/__r04/state', timeout=5).json()
out['tables'] = snapshot['tables']
out['writes'] = [call for call in snapshot['calls'] if call['method'] != 'GET']
print(json.dumps(out, ensure_ascii=False))
