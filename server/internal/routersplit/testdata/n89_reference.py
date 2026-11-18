"""Independent R5 projection of the frozen R02 inputs, then real Python business.

No Go output is read. Only rows changed by the declared typed boundary are saved;
the original corpus and ordinary Python answers remain authoritative.
"""
import copy
import gzip
import hashlib
import json
import os
import sys
import tempfile
from pathlib import Path

os.environ['DATA_DIR'] = tempfile.mkdtemp(prefix='n89-router-split-')
os.environ['LOG_FILE'] = os.path.join(os.environ['DATA_DIR'], 'app.log')
from backend.common import router_split as split, setup_flow
from backend.common.linux_gateway import LinuxGatewayError

_original_router_ids=split._router_ids
# Empty identities never prove a forwarding relationship (R13 fail-closed rule).
split._router_ids=lambda config,ids:_original_router_ids(config,[x for x in ids if x])


def text(v, default=''):
    if isinstance(v, str): return v
    if type(v) in (int, float): return json.dumps(v, ensure_ascii=False, separators=(',', ':'))
    return default


def record(v): return v if isinstance(v, dict) else {}

def strings(v):
    if not isinstance(v, list): return []
    if any(x is not None and not isinstance(x, str) and type(x) not in (int, float) for x in v): return []
    return [text(x) for x in v]


def profile(v):
    p = copy.deepcopy(record(v))
    bad=False
    m = p['mihomo'] = copy.deepcopy(record(p.get('mihomo')))
    m['custom_config'] = text(m.get('custom_config'))
    g = m['linux_gateway'] = copy.deepcopy(record(m.get('linux_gateway')))
    g['enabled'] = g.get('enabled') is True
    routers = p['routers'] = copy.deepcopy(record(p.get('routers')))
    items = routers.get('items')
    if not isinstance(items, list) or any(not isinstance(x, dict) for x in items): items = []
    routers['items'] = []
    for item in items:
        item = copy.deepcopy(item)
        link=item.get('proxy_gateway_agent_id')
        if link is not None and not isinstance(link,str): bad=True
        # Required router IDs are checked only when a matching binding is consumed.
        item['_n89_raw_id']=item.get('id')
        for k in ('id', 'proxy_gateway_agent_id'):
            item[k] = text(item.get(k))
        routers['items'].append(item)
    p['_n89_bad_identity']=bad
    return p


def context(v):
    v = record(v)
    ids=v.get('mihomo_agent_ids')
    bad=ids is not None and (not isinstance(ids,list) or any(not isinstance(x,str) or not x for x in ids))
    bad=bad or (v.get('session_id') is not None and not isinstance(v['session_id'],str))
    return {'_n89_bad_identity':bad,'session_goal': text(v.get('session_goal'), None),
            'session_id': text(v.get('session_id'), None),
            'session_completed': v.get('session_completed') is True,
            'has_gateway_session': v.get('has_gateway_session') is True,
            'mihomo_agent_ids': strings(v.get('mihomo_agent_ids'))}


def session(v):
    v = record(v)
    return {'id': text(v.get('id'), None), 'goal': text(v.get('goal'), None),
            'abandoned': v.get('abandoned') is True, 'completed': v.get('completed') is True}


def decode(v):
    if isinstance(v, dict):
        if set(v) == {'__tuple__'}: return decode(v['__tuple__'])
        return {k: decode(x) for k,x in v.items()}
    if isinstance(v,list): return [decode(x) for x in v]
    return v


class ReplayError(Exception):
    def __init__(self, value): self.value=value


class Repository:
    def __init__(self, env): self.env=env; self.next=0
    def take(self, name, _id):
        if self.next == len(self.env): raise RuntimeError('unexpected collaborator call')
        entry=self.env[self.next]; self.next+=1
        assert entry['fn']==name, (entry['fn'],name)
        if entry.get('err'): raise ReplayError(entry['err'])
        out=decode(entry.get('out'))
        if name=='get_profile': return profile(out)
        if name=='profile_sessions': return [session(x) for x in out]
        if name=='get_profile_agent_bindings':
            agents=record(out).get('agents')
            if not isinstance(agents,list) or any(not isinstance(x,dict) for x in agents): raise ValueError('主路由分流的 Agent 绑定身份无效')
            if any(x.get('service_type')=='mihomo' and (not isinstance(x.get('id'),str) or not x['id']) for x in agents): raise ValueError('主路由分流的 Agent 绑定身份无效')
            return {'agents':[{'id':text(x.get('id')), 'service_type':text(x.get('service_type'))} for x in agents if text(x.get('id'))]}
        raise AssertionError(name)
    def get_profile(self, id): return self.take('get_profile',id)
    def get_profile_agent_bindings(self,id): return self.take('get_profile_agent_bindings',id)


def error(e):
    if isinstance(e,ReplayError): return e.value
    out={'class':type(e).__name__,'msg':str(e),'gateway':isinstance(e,LinuxGatewayError)}
    if isinstance(e,split.RouterSplitError): out.update(code=e.code,status=e.status)
    return out


def run(row):
    name=row['fn'];args=copy.deepcopy(decode(row['args']));r=Repository(row.get('env',[]))
    args.pop('repository',None)
    if 'config' in args and args['config'] is not None: args['config']=profile(args['config'])
    elif 'config' in args and name!='router_split_state': args['config']=profile(None)
    if 'context' in args and (args['context'] is not None or name!='router_split_state'): args['context']=context(args['context'])
    if name=='gateway_was_used': args['agent']={'gateway_installation_id':text(record(args.get('agent')).get('gateway_installation_id'))}
    
    setup_flow.profile_sessions=lambda _,id:r.take('profile_sessions',id)
    try:
        if name=='full_tun_document' and not isinstance(args.get('document'),dict): raise LinuxGatewayError('Mihomo 配置必须是对象')
        if name in ('legacy_context','publish_legacy_source','router_split_state'):
            args['repository']=r
        if name=='publish_legacy_source' and split.read_marker(args['config'])[0]=='absent' and args.get('profile_id') and (not isinstance(args.get('agent_id'),str) or not args['agent_id']):
            split._session_context(r,args['profile_id'])
            raise ValueError('主路由分流的拓扑身份必须是非空文本')
        answer={'out':getattr(split,name)(**args)}
    except Exception as e:
        answer={'err':error(e)}
    answer['calls']=r.next
    return answer


def validate(p,c):
    if p.get('_n89_bad_identity') or c.get('_n89_bad_identity'):
        raise ValueError('主路由分流的拓扑身份必须是非空文本')
    ids=c.get('mihomo_agent_ids') or []
    for r in p['routers']['items']:
        if r.get('proxy_gateway_agent_id') and r['proxy_gateway_agent_id'] in ids:
            if not isinstance(r.get('_n89_raw_id'),str) or not r['_n89_raw_id']:
                raise ValueError('主路由分流的拓扑身份必须是非空文本')

_original_source=split.legacy_source
_original_ids=split.legacy_gateway_agent_ids
_original_state=split._state

def checked_source(config,context):
    validate(config,context)
    return _original_source(config,context)

def checked_ids(config,context):
    validate(config,context)
    return _original_ids(config,context)

def checked_state(p,c):
    validate(p,c)
    return _original_state(p,c)

split.legacy_source=checked_source
split.legacy_gateway_agent_ids=checked_ids
split._state=checked_state

def main():
    root=Path(__file__).parent
    result=[]
    for file in ('r02_recorded.jsonl.gz','r02_corpus.jsonl.gz','r02_edge.jsonl.gz'):
        for line_index,line in enumerate(gzip.open(root/file,'rt')):
            row=json.loads(line)
            if row['fn']=='classical_ip_lines': continue
            if row['args'].get('profile_id') is not None and not isinstance(row['args'].get('profile_id'),str): continue
            answer=run(row)
            old={'err':row['err']} if row.get('err') else {'out':decode(row.get('out'))}
            old['calls']=len(row.get('env',[]))
            if answer!=old:
                result.append({'file':file,'line':line_index,'input_sha256':hashlib.sha256(line.rstrip('\n').encode()).hexdigest(),'fn':row['fn'],'answer':answer})
    out=root/'n89_expectations.json'
    out.write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
    print(f'{len(result)} independent row expectations -> {out}')

if __name__=='__main__':main()
