"""R12 real Python plan/executor, synthetic loopback transport only."""
import copy
import ipaddress
import json
import os
import sys
from unittest.mock import patch
from urllib.parse import urlsplit
import requests
from backend.routers.routeros import RouterOSDriver
from backend.routers.ikuai import IKuaiDriver
from backend.routers.base import retire_policy_routes
from backend.routers.report import build_report
from backend.routers.plan import build_plan
from backend.routers.release import execute
from backend.test_router_release import FakeAgent

case=json.load(sys.stdin)
endpoint=os.environ['R12_ENDPOINT']
assert ipaddress.ip_address(urlsplit(endpoint).hostname).is_loopback
vendor=case['router']['type']
control='/__r04' if vendor=='routeros' else '/__r05'
requests.post(endpoint+control+'/reset',json=case['state'],timeout=5).raise_for_status()
cls=RouterOSDriver if vendor=='routeros' else IKuaiDriver
user='synthetic-user' if vendor=='routeros' else 'synthetic-admin'
password='synthetic-password' if vendor=='routeros' else 'synthetic-secret'
driver=cls(endpoint,user,password,verify_tls=False)
router=case['router']
desired=case['desired']
status=copy.deepcopy(case['return_status'])
agent=FakeAgent(status) if router.get('manage_return_routing') else None
try:
    state=driver.read_state()
    if not router.get('manage_return_routing'):
        retire_policy_routes(state)
    if case.get('dhcp'):
        state.dhcp_dns=driver.read_dhcp_dns()
    for key in ['ipv6_fakeip_plan','ipv6_routes_plan','ipv6_dns_plan','ipv6_pd_guard_plan','ros_policy_plan','dns_guard_plan']:
        if key in case:
            setattr(state,key,case[key])
    report=build_report(desired,state)
    plan=build_plan(router,desired,state,report,status,driver)
    result=execute(plan,driver,agent) if case.get('execute') else None
    print(json.dumps({'plan':plan,'result':result,'agent_calls':agent.calls if agent else []},ensure_ascii=False))
finally:
    driver.close()
