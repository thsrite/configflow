"""R17 reservation oracle with actual Python vendor drivers and shared routerfake."""
import ipaddress
import json
import os
import sys
from urllib.parse import urlsplit
import requests
from backend.routers.routeros import RouterOSDriver
from backend.routers.ikuai import IKuaiDriver
from backend.routers import routeros_fixed_ip, ikuai_fixed_ip
case = json.load(sys.stdin)
endpoint = os.environ['R17_ENDPOINT']
assert ipaddress.ip_address(urlsplit(endpoint).hostname).is_loopback
vendor = case['vendor']
control = '/__r04' if vendor == 'routeros' else '/__r05'
requests.post(endpoint + control + '/reset',json=case['state'],timeout=5).raise_for_status()
module = routeros_fixed_ip if vendor == 'routeros' else ikuai_fixed_ip
driver = (RouterOSDriver(endpoint,'synthetic-user','synthetic-pass',verify_tls=False) if vendor == 'routeros'
          else IKuaiDriver(endpoint,'synthetic-admin','synthetic-secret',verify_tls=False))
result = {'preview':None,'binding':None,'error':None}
try:
    op = module.preview(driver,case['ip'],case['mac'],'Synthetic')
    result['preview'] = op
    if case['write']:
        receipt = module.apply(driver,op)
        result['binding'] = {key:value for key,value in receipt.items() if key != 'undo'}
        deletion = module.preview_delete(driver,receipt)
        removed = module.delete(driver,deletion)
        restored = module.restore_deleted(driver,removed)
        module.rollback(driver,{**restored,'undo':receipt['undo']})
except Exception as error:
    result['error'] = {'class':type(error).__name__,'message':str(error)}
finally:
    driver.close()
print(json.dumps(result,ensure_ascii=False))
