"""R01 native request policy expectations, derived without any Go outputs.

Use real Python normalize after the explicitly approved text/integer projection;
strict identity failures have their own complete Chinese expectation per input.
"""
import copy
import gzip
import hashlib
import json
from pathlib import Path
import re
from backend.routers.store import normalize, RouterValidationError

TYPE_MESSAGE='路由器身份和凭据字段必须是文本'

def native_text(v):
    if isinstance(v,str):return v
    if type(v) in (int,float):return json.dumps(v,separators=(',',':'))
    return ''

def run(args):
    payload=copy.deepcopy(args['payload']);existing=copy.deepcopy(args.get('existing'))
    if not isinstance(payload,dict):raise RouterValidationError('路由器参数必须是对象')
    for key in ('type','endpoint','username','password','proxy_gateway_agent_id','dns_agent_id'):
        if key in payload and not isinstance(payload[key],str):raise RouterValidationError(TYPE_MESSAGE)
    if 'name' in payload:payload['name']=native_text(payload['name'])
    if 'verify_tls' in payload and type(payload['verify_tls']) is not bool:raise RouterValidationError(TYPE_MESSAGE)
    if 'rule_sync_interval' in payload:
        v=payload['rule_sync_interval']
        if type(v) is int:pass
        elif type(v) is float and v.is_integer():v=int(v)
        elif isinstance(v,str) and re.fullmatch('[+-]?[0-9]+',v.strip(' \t\r\n')):v=int(v)
        else:v=0
        if not -(1<<63)<=v<(1<<63):v=0
        payload['rule_sync_interval']=v
    for key in ('ip_rule_sets','ros_policy_rule_sets'):
        if key in payload and payload[key] is not None and not isinstance(payload[key],list):payload[key]=['']
    result=normalize(payload,existing)
    # R2/R3: runtime defaults do not materialize untouched absent fields.
    # An explicitly supplied setting is written even if it equals the default.
    if existing:
        defaults={'manage_return_routing':False,'auto_rule_sync':False,'rule_sync_interval':60,'verify_tls':True,
                  'proxy_gateway_agent_id':'','dns_agent_id':''}
        for key,value in defaults.items():
            if key not in existing and key not in payload and result.get(key)==value:result.pop(key,None)
    if result.get('id','').startswith('router_') and len(result['id'])==19:result['id']='router_GEN'
    return result

def main():
    root=Path(__file__).parent;out=[]
    for index,line in enumerate(gzip.open(root/'r01_corpus.jsonl.gz','rt')):
        row=json.loads(line)
        if row['fn']!='normalize':continue
        try:answer={'out':run(row['in'])}
        except Exception as e:answer={'err':[type(e).__name__,str(e)]}
        original={'err':row['err']} if 'err' in row else {'out':row['out']}
        if answer!=original:out.append({'line':index,'input_sha256':hashlib.sha256(line.rstrip('\n').encode()).hexdigest(),'answer':answer})
    (root/'n89_expectations.json').write_text(json.dumps(out,ensure_ascii=False,indent=2)+'\n')
    print(f'{len(out)} R01 independent row expectations')
if __name__=='__main__':main()
