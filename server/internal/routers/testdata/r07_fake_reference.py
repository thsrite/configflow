"""Interpret captured scripts using the original Python test helpers."""
import copy
import json
import re
import sys
from backend.routeros_test_helpers import run_add_script,run_list_script,is_add_script,stored_address
from backend.test_ros_policy import NAMES_PATTERN,NAMES_EXCLUDED,_unescape

case=json.load(sys.stdin)
tables=copy.deepcopy(case['tables']);number=100

def add(path,values):
    global number
    values=copy.deepcopy(values)
    if path=='/ip/firewall/address-list':
        values['address']=stored_address(values['address'])
        if any(row.get('list')==values.get('list') and stored_address(row.get('address',''))==values['address'] for row in tables[path]):
            raise ValueError('already have such entry')
    number+=1
    while any(row.get('.id')==f'*{number:X}' for row in tables[path]): number+=1
    row={'.id':f'*{number:X}','disabled':'false',**values};before=row.pop('place-before',None)
    if before: tables[path].insert(next(i for i,r in enumerate(tables[path]) if r['.id']==before),row)
    else: tables[path].append(row)
    return row['.id']

script=case['script']
if is_add_script(script): result=run_add_script(script,add)
elif 'cf-names:' in script:
    pattern=re.compile(_unescape(NAMES_PATTERN.search(script)[1]))
    excluded={_unescape(name) for name in NAMES_EXCLUDED.findall(script)}
    names=sorted({row['list'] for row in tables['/ip/firewall/address-list'] if pattern.search(row.get('list','')) and row['list'] not in excluded})
    result={'ret':'cf-names:\r\n'+'\r\n'.join(names)}
else: result=run_list_script(script,tables['/ip/firewall/address-list'],lambda values:add('/ip/firewall/address-list',values))
# Console CRLF versus LF is an HTTP text-format detail; marker/row content stays exact.
result['ret']=result['ret'].replace('\r','')
print(json.dumps({'result':result,'tables':tables},ensure_ascii=False))
