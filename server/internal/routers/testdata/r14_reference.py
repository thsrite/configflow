"""Real Python R14 boundaries. Only the caller's loopback routerfake is contacted."""
import copy
import ipaddress
import json
import os
import sys
from urllib.parse import urlsplit
import requests

from backend.routers.rule_sync_rules import classify
from backend.routers.base import RouterError, retire_policy_routes

case = json.load(sys.stdin)
if case['action'] == 'classify':
    results = []
    for item in case['cases']:
        try:
            results.append({'value': classify(item['content'], item['behavior'])})
        except RouterError as error:
            results.append({'error': str(error)})
    print(json.dumps(results, ensure_ascii=False))
    sys.exit()

if case['action'] == 'service':
    import shutil
    import tempfile
    from unittest.mock import patch
    from backend.common import config as config_module
    from backend.common.config_repository import ProfileRepository
    from backend.routers import rule_sync, rule_sync_store
    from backend.routes import routers as api
    from backend.routers.routeros import RouterOSDriver
    endpoint = os.environ['R14_ENDPOINT']
    assert ipaddress.ip_address(urlsplit(endpoint).hostname).is_loopback
    requests.post(endpoint + '/__r04/reset', json=case['state'], timeout=5).raise_for_status()
    with tempfile.TemporaryDirectory() as directory:
        shutil.copytree(case['data_dir'], directory, dirs_exist_ok=True)
        repository = ProfileRepository(directory)
        agent = case['agent']
        with patch.object(config_module, 'DATA_DIR', directory), patch.object(config_module, '_repository', repository), patch.object(api, '_agent', lambda key: agent if key == agent['id'] else None), patch.object(api, '_driver', lambda router: RouterOSDriver(router['endpoint'], router['username'], router['password'], verify_tls=router['verify_tls'])):
            config_module.reset_config_context()
            config = rule_sync.config_for('default')
            router = next(r for r in config['routers']['items'] if r['id'] == case['id'])
            receipt = rule_sync_store.read('default', router['id'])
            receipt['baseline']['control'] = rule_sync.controls(config, router)
            if case.get('changed_control'):
                receipt['baseline']['control'] = 'changed'
            rule_sync_store.write('default', router['id'], receipt)
            def download(*_):
                if case.get('download_error'):
                    raise rule_sync.DownloadError('远程规则下载失败：ConnectionError')
                return case['content']
            result = rule_sync.sync('default', router['id'], download, trigger='manual')
            print(json.dumps(result, ensure_ascii=False))
    sys.exit()

from backend.routers.routeros import RouterOSDriver
from backend.routers.report import build_report
from backend.routers.plan import build_plan
from backend.routers.rule_sync_write import apply_delta
from backend.routers.ros_policy import snapshot, owned_rows, MANGLE, RULES, VRFS, ROUTES

endpoint = os.environ['R14_ENDPOINT']
assert ipaddress.ip_address(urlsplit(endpoint).hostname).is_loopback
requests.post(endpoint + '/__r04/reset', json=case['state'], timeout=5).raise_for_status()
driver = RouterOSDriver(endpoint, 'synthetic-user', 'synthetic-password', verify_tls=False)
driver.ros_policy_manifest = case['baseline']['policy']
router, desired = case['router'], case['desired']
try:
    state = driver.read_state()
    state.ros_policy = snapshot(driver)
    try:
        owned = owned_rows(driver.ros_policy_manifest, state.ros_policy)
    except RouterError:
        owned = []
    ids = {item['id'] for item in owned if item['path'] == MANGLE}
    state.nat_context.update(mangle=[{k:v for k,v in row.items() if k not in {'bytes','packets'}}
        for row in state.ros_policy[MANGLE] if row.get('.id') not in ids],
        routing_rules=state.ros_policy[RULES], vrfs=state.ros_policy[VRFS], routes=state.ros_policy[ROUTES])
    retire_policy_routes(state)
    if case.get('ipv6'):
        from backend.routers import ipv6_routes
        from backend.routers.rule_sync import _ipv6_routes_delta
        state.ipv6_routes_plan = ipv6_routes.inspect(case['config'], router, desired, case['agent'], case['host'], driver)
    report = build_report(desired, state)
    plan = build_plan(router, desired, state, report, {'status':'disabled'}, driver)
    note = _ipv6_routes_delta(state, plan) if case.get('ipv6') else ''
    if case.get('ipv6_plan'):
        plan['stages'].append(case['ipv6_plan']['stage'])
        plan['checks']['ipv6_routes'] = case['ipv6_plan']['check']
        driver.ipv6_routes_manifest = case['ipv6_plan']['previous']
    try:
        outcome = apply_delta(driver, desired, plan, case['baseline'])
        result = {'value':outcome}
    except Exception as error:
        result = {'error':str(error), 'class':type(error).__name__}
    result['note'] = note
    result['ipv6_manifest'] = getattr(driver, 'ipv6_routes_manifest', None)
    result['policy'] = driver.ros_policy_manifest
    print(json.dumps(result, ensure_ascii=False))
finally:
    driver.close()
