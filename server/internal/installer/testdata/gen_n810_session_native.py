"""Bind approved session-ID refusals to frozen real-Python IO records.

No Go process is called. Only the coordinator's exact index/session-ID policy is
applied; complete original results, files and manager effects stay in each row.
"""
import gzip
import hashlib
import json
import re
from pathlib import Path
ROOT = Path(__file__).resolve().parent
SID = re.compile('[0-9a-f]{32}')
def sha(raw): return hashlib.sha256(raw).hexdigest()
def file_bytes(tree,name):
    entry=tree.get(name)
    if not isinstance(entry,list) or entry[0]!='file': return None
    return bytes.fromhex(entry[2])
def corrupt_session(row):
    args=row['args'];tree=row.get('before') or {}
    if row['fn']=='issue':
        if not isinstance(args[0],dict) or not isinstance(args[1],str): return False
        spec=dict(args[0]);spec.update(mode='new',server_url=spec.get('server_url','').rstrip('/'),profile_id=args[1]);spec.pop('action',None)
        if spec.get('install_method')=='compose_compact':
            for key in ('image','ha_host_id_path','create_host_identity'):spec.pop(key,None)
        index=sha(json.dumps(spec,sort_keys=True).encode())+'.index'
        raw=file_bytes(tree,index)
        if raw is None:return False
        try:ident=raw.decode('utf-8')
        except UnicodeDecodeError:return True
        if not SID.fullmatch(ident):return True
    else:
        ident=args[0]
        if not isinstance(ident,str) or not SID.fullmatch(ident):return False
        token=args[1]
        if not isinstance(token,str) or re.fullmatch('[A-Za-z0-9_-]{43}',token) is None:return False
    data=file_bytes(tree,ident+'.json')
    if data is None:return False
    try:
        session=json.loads(data.decode('utf-8'),parse_constant=lambda x:(_ for _ in ()).throw(ValueError(x)))
    except (ValueError,UnicodeDecodeError):return True
    return not isinstance(session,dict) or not isinstance(session.get('id'),str) or SID.fullmatch(session['id']) is None or session['id']!=ident
def native_refusal(row):
    if corrupt_session(row):
        issue=row['fn']=='issue'
        return {'status':409 if issue else 401,'message':'安装记录无法读取，请保留现场并检查服务端数据。' if issue else '安装凭据无效，请重新生成安装命令。'}
    if row['fn']=='issue':
        body=row['args'][0]
        if isinstance(body,dict) and (not isinstance(body.get('agent_name','agent'),str) or not isinstance(body.get('agent_ip'),str) or type(body.get('oxidns_port',8082)) is not int):
            return {'status':500,'message':'安装记录的身份字段无效，请保留现场并重新生成安装命令。'}
    else:
        before=(row.get('manager') or {}).get('before')
        if isinstance(before,list):
            if 'out' in row and any(not isinstance(agent,dict) for agent in before):
                return {'status':500,'message':'model: incompatible JSON type','class':'model-type','registry':True}
            for agent in before:
                if isinstance(agent,dict) and agent.get('docker_ha_installation_id')==row['args'][0]:
                    if any(not isinstance(agent.get(key),str) or not agent[key] for key in ('id','token')):
                        return {'status':409,'message':'本次安装的设备信息已改变，请重新生成命令。','registry':True}
                    break
    return None
rows=[]
for name in ('docker_ha_recorded.jsonl.gz','docker_ha_review.jsonl.gz','docker_ha_compose_review.jsonl.gz'):
    archive=ROOT/name
    for line in gzip.decompress(archive.read_bytes()).splitlines():
        row=json.loads(line)
        if row.get('fn') not in ('issue','enroll') or 'patched' in row:continue
        expected=native_refusal(row)
        if expected is None:continue
        rows.append(dict(archive=name,archive_sha256=sha(archive.read_bytes()),row_sha256=sha(line),original=row,
                         python=row.get('out',row.get('err')),native=expected))
print(json.dumps({'policy':'N8-10 original session ID ownership','rows':rows},ensure_ascii=False,separators=(',',':')))
