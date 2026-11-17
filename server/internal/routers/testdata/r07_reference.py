"""Real Python R07 entry points against the same loopback device as Go."""
import json
import ipaddress
import os
import sys
import requests
from backend.routers.routeros import RouterOSDriver
from backend.routers import ros_address_lists as lists

case=json.load(sys.stdin)
endpoint=os.environ['R04_ENDPOINT']
requests.post(endpoint+'/__r04/reset',json=case['state'],timeout=5).raise_for_status()
driver=RouterOSDriver(endpoint,'synthetic-user','synthetic-password',verify_tls=False)
out={'result':None,'records':[],'error':None}
try:
    action=case['action']; name=case.get('op',{}).get('name','L')
    if action=='digest': out['result']=lists.digest(case['state']['tables']['/ip/firewall/address-list'])
    elif action=='fetch': out['result']=lists.fetch(driver,name)
    elif action=='nat_restore':
        undo=driver.apply(case['op']['sync'])
        out['records']=[{'undo':undo}]
        requests.post(endpoint+'/__r04/mutate',json=case['op']['mutation'],timeout=5).raise_for_status()
        out['result']=driver.apply(undo)
    elif action=='helper':
        from backend.routers import routeros_nat_list as nat
        p=case['op']; fn=p['function']
        if fn=='removals': out['result']=nat.removals_first(p['nat_ops'],p['route_ops'],p['gateway'])
        elif fn=='restrict':
            blocker=nat.restrict_egress(p['sync'],p['check'],p['interface'])
            out['result']={'blocker':blocker,'sync':p['sync'],'check':p['check']}
        elif fn=='member': out['result']=nat.member_update({'stages':p['stages']},{'nat_list':p.get('baseline')})
        elif fn=='migration': out['result']=nat.migration({'stages':p['stages']})
        elif fn=='policy_migrations': out['result']=nat.policy_migrations(p['row'],p['addresses'],[ipaddress.ip_network(v) for v in p['exclusions']])
        elif fn=='baseline': out['result']=nat.nat_resources(driver)
        elif fn=='verify': nat.verify(p['check'],driver.read_state())
        elif fn=='locate': out['result']=lists.locate(driver,p['ids'])
        elif fn=='occupied': out['result']=sorted(lists.occupied_names(driver,p['patterns'],p['exclude']))
        else: raise ValueError(fn)
    elif action=='addresses':
        from backend.routers import ros_policy_write as write, routeros_nat_list as nat
        journal=[]
        ids={lists.ADDRESSES:set(row['.id'] for row in case['state']['tables'].get(lists.ADDRESSES,[]))}
        try:
            entries=[{'list':v['list'],'address':v['address'],'comment':v['comment'],'disabled':v['disabled']} for v in case['op']['entries']]
            out['result']=write._create_addresses(driver,entries,journal,ids)
        finally: out['records']=[{'step':step} for step in journal]
    elif action in ('nat_apply','nat_roundtrip'):
        undo=driver.apply(case['op'])
        out['result']=undo
        if action=='nat_roundtrip': out['result']={'undo':undo,'restored':driver.apply(undo)}
    elif action=='plan':
        from backend.routers.report import build_report
        from backend.routers import routeros_nat_list as nat
        state=driver.read_state()
        report=build_report(case['op']['desired'],state)
        out['result']=nat.plan(report,state,case['op']['gateway'],set(case['op'].get('skipped',[])))
    elif action=='reuse':
        with lists.reuse(driver):
            rows=lists.read(driver,[name]);lists.expect(driver,name,rows[name]);out['result']=lists.read(driver,[name])
    else: raise ValueError(action)
except Exception as e:
    out['error']={'class':type(e).__name__,'message':str(e)}
finally: driver.close()
print(json.dumps(out,ensure_ascii=False))
