"""Real R08 public entry points against the Go loopback RouterOS fake.

Only synthetic inputs arrive on stdin. No production configuration is read.
"""
import copy
import ipaddress
import json
import os
import sys
from urllib.parse import urlsplit

import requests
from backend.routers.routeros import RouterOSDriver
from backend.routers import ros_policy as policy
from backend.routers import ros_policy_write as write
from backend.routers.ros_policy_view import read_policy_objects

case = json.load(sys.stdin)
endpoint = os.environ['R08_ENDPOINT']
assert ipaddress.ip_address(urlsplit(endpoint).hostname).is_loopback
requests.post(endpoint + '/__r04/reset', json=case['state'], timeout=5).raise_for_status()
driver = RouterOSDriver(endpoint, 'synthetic-user', 'synthetic-password', verify_tls=False)
out = {'results': [], 'error': None, 'manifest': None}


def plan(desired):
    state = driver.read_state()
    state.ros_policy = policy.snapshot(driver)
    state.nat_context.update(routing_rules=state.ros_policy[policy.RULES],
                             vrfs=state.ros_policy[policy.VRFS])
    if state.nat_context['mangle'] is None:
        state.nat_context['mangle'] = state.ros_policy[policy.MANGLE]
    result = policy.build(desired, state)
    # R12 build_plan always attaches this required field to the R08 operation.
    for op in result['ops']:
        op['static_checks'] = []
    return result


try:
    for step in case['steps']:
        action = step['action']
        if 'mutation' in step:
            requests.post(endpoint + '/__r04/mutate', json=step['mutation'], timeout=5).raise_for_status()
        if action == 'sources':
            out['results'].append(policy.smart_sources(step['rows'], step['gateway']))
            continue
        if action == 'inspect':
            router = step['router']
            out['results'].append(policy.inspect_desired(step['config'], router, step['desired'],
                content_loader=lambda item, library: step['contents'].get(item['id'], '')))
            continue
        if action == 'view':
            out['results'].append(read_policy_objects(driver, getattr(driver, 'ros_policy_manifest', None)))
            continue
        if action == 'legacy':
            manifest = copy.deepcopy(driver.ros_policy_manifest)
            rename = {'CF_Source_Proxy_IP_01': 'cfp_0123456789abcdef_src',
                      'CF_Destination_Proxy_IP_01': 'cfp_0123456789abcdef_dst',
                      'CF_Bypass_IP_01': 'cfp_0123456789abcdef_bypass',
                      'CF_Proxy_Routing_01': 'cfp_0123456789abcdef'}
            patches = {}
            for entry in manifest['resources']:
                config = entry['config']
                config.update({key: rename.get(value, value) for key, value in config.items()
                               if isinstance(value, str)})
                if entry['path'] == policy.MANGLE:
                    if config.get('action') == 'jump':
                        config['routing-mark'] = 'main'
                    config['invalid'] = 'true'
                patches.setdefault(entry['path'], {})[entry['id']] = {**config, 'invalid': 'false'}
            manifest['spec'].pop('naming_version')
            manifest['spec'].pop('policy_version')
            manifest['future'] = {'keep': True}
            driver.ros_policy_manifest = manifest
            requests.post(endpoint + '/__r04/mutate', json={'patch_rows': patches}, timeout=5).raise_for_status()
            continue
        if action == 'undo':
            out['results'].append(driver.apply(undo))
            continue
        desired = copy.deepcopy(step['desired'])
        desired['ros_policy']['previous'] = getattr(driver, 'ros_policy_manifest', None)
        result = plan(desired)
        out['results'].append(copy.deepcopy(result))
        if action == 'plan' or result['blockers'] or not result['ops']:
            continue
        op = result['ops'][0]
        op.update(step.get('prepare', {}))
        undo = driver.apply(op)
        out['results'].append(undo)
        if action == 'publish':
            write.verify(driver, result['check'])
        elif action == 'rollback':
            out['results'].append(driver.apply(undo))
except Exception as error:
    out['error'] = {'class': type(error).__name__, 'message': str(error)}
finally:
    out['manifest'] = getattr(driver, 'ros_policy_manifest', None)
    driver.close()
print(json.dumps(out, ensure_ascii=False))
