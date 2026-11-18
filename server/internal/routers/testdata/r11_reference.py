"""R11 real Python modules, synthetic loopback inputs only."""
import copy
import ipaddress
import json
import os
import sys
from urllib.parse import urlsplit
import requests
from backend.routers.routeros import RouterOSDriver
from backend.routers import ipv6_dns, ipv6_pd_guard, ipv6_readiness

case = json.load(sys.stdin)
endpoint = os.environ['R09_ENDPOINT']
assert ipaddress.ip_address(urlsplit(endpoint).hostname).is_loopback
requests.post(endpoint + '/__r04/reset', json=case['state'], timeout=5).raise_for_status()
driver = RouterOSDriver(endpoint, 'synthetic-user', 'synthetic-password', verify_tls=False)
module = ipv6_pd_guard if case['feature'] == 'pd' else ipv6_dns
out = {'results': [], 'error': None, 'manifest': None}
plan = undo = None
try:
    for step in case['steps']:
        if step.get('mutation'):
            requests.post(endpoint + '/__r04/mutate', json=step['mutation'], timeout=5).raise_for_status()
        action = step['action']
        if case['feature'] == 'readiness':
            out['results'].append(ipv6_readiness.inspect(driver, case['router'], step['agent'], step['host'],
                step['configuration'], step.get('dns_target'), step.get('policy_context')))
            continue
        if action == 'undo':
            out['results'].append(driver.apply(undo))
            continue
        if action == 'apply':
            undo = driver.apply(plan['ops'][0])
            out['results'].append(undo)
            continue
        if case['feature'] == 'pd':
            router = copy.deepcopy(case['router'])
            router['ipv6_pd_guard'] = {'enabled': step.get('enabled', True)}
            if hasattr(driver, 'ipv6_pd_guard_manifest'):
                router['ipv6_pd_guard_state'] = copy.deepcopy(driver.ipv6_pd_guard_manifest)
            plan = module.inspect(router, '192.0.2.4', driver)
        else:
            desired = copy.deepcopy(step['desired'])
            desired['ipv6_dns_scope'] = copy.deepcopy(step['scope'])
            plan = module.inspect(desired, driver)
        out['results'].append(copy.deepcopy(plan))
        if action == 'report':
            from backend.routers.report import build_report
            state = driver.read_state()
            if case['feature'] == 'pd':
                state.ipv6_pd_guard_plan = plan
            else:
                state.ipv6_dns_plan = plan
            out['results'].append(build_report(step['desired'], state))
            continue
        if action == 'plan' or plan['blockers']:
            continue
        if plan['ops']:
            undo = driver.apply(plan['ops'][0])
            out['results'].append(undo)
        module.verify(driver, plan['check'])
        if action == 'rollback' and plan['ops']:
            out['results'].append(driver.apply(undo))
except Exception as error:
    out['error'] = {'class': type(error).__name__, 'message': str(error)}
finally:
    out['manifest'] = getattr(driver, 'ipv6_pd_guard_manifest', None)
    driver.close()
print(json.dumps(out, ensure_ascii=False))
