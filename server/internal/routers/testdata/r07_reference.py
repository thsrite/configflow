"""Real Python R07 entry points against the same loopback device as Go."""
import json
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
    elif action=='reuse':
        with lists.reuse(driver):
            rows=lists.read(driver,[name]);lists.expect(driver,name,rows[name]);out['result']=lists.read(driver,[name])
    else: raise ValueError(action)
except Exception as e:
    out['error']={'class':type(e).__name__,'message':str(e)}
finally: driver.close()
print(json.dumps(out,ensure_ascii=False))
