"""Bind actual Python HTTP rows to the approved native upgrades policy.
Only real Python original rows and the real Python normal update are sources;
no Go response is read. The old 130-request matrix remains unchanged.
"""
import copy,hashlib,json,pathlib
from backend.agents.diagnostics import failure_text
root=pathlib.Path(__file__).parent
archive=root/'n810_upgrades_http_python.jsonl'
originals=[json.loads(line) for line in archive.read_bytes().splitlines()]
by_name={row['input']['name']:row for row in originals}
normal=by_name['update']
old_text=by_name['report']['python_body']['error']
report_input=json.loads(by_name['report']['input']['body'])
new_text=failure_text(json.dumps(report_input['error'],ensure_ascii=False,separators=(',',':')),('tok-op-upgrading',),limit=2048)
def replace_diagnostic(value):
 if isinstance(value,str):return value.replace(old_text,new_text)
 if isinstance(value,list):return [replace_diagnostic(v) for v in value]
 if isinstance(value,dict):return {k:replace_diagnostic(v) for k,v in value.items()}
 return value
def normal_for_noarch(value):
 value=copy.deepcopy(value);value['agent_id']='op-noarch';return value
rows=[]
for row in originals:
 name=row['input']['name']
 if name=='update':continue
 native=dict(status=row['python_status'],type='application/json',body=copy.deepcopy(row['python_body']),calls=copy.deepcopy(row['python_calls']),no_write=False)
 if name in ('report','upgrade poll rolling back'):native['body']=replace_diagnostic(native['body'])
 elif name.startswith('review upgrade '):
  native.update(status=500,body=dict(message='Agent 操作记录损坏（latest_upgrade），请修复存储记录后重试',success=False),no_write=True)
 elif name=='review update info without architecture':
  native.update(status=409,body=dict(message='更新文件未准备就绪，请先更新 ConfigFlow 主服务镜像',success=False),no_write=True)
 elif name=='review update again after stuck reservation':
  native.update(status=normal['python_status'],body=normal_for_noarch(normal['python_body']),calls=[v.replace('tok-op-1','tok-op-noarch') for v in normal['python_calls']])
 else:raise AssertionError(name)
 digest=hashlib.sha256(json.dumps(row,ensure_ascii=False,separators=(',',':')).encode()).hexdigest()
 rows.append(dict(original=row,row_sha256=digest,native=native))
normal_sha=hashlib.sha256(json.dumps(normal,ensure_ascii=False,separators=(',',':')).encode()).hexdigest()
out=dict(archive_sha256=hashlib.sha256(archive.read_bytes()).hexdigest(),normal_control=dict(original=normal,row_sha256=normal_sha),rows=rows,stored=[
 dict(agent='op-upgrading',python=by_name['upgrade poll rolling back']['stored_upgrade'],native=replace_diagnostic(by_name['upgrade poll rolling back']['stored_upgrade'])),
 dict(agent='op-noarch',python=by_name['review update again after stuck reservation']['stored_upgrade'],native=normal_for_noarch(normal['stored_upgrade']))])
(root/'n810_upgrades_http_native.json').write_text(json.dumps(out,ensure_ascii=False,indent=2)+'\n')
print(f'bound {len(rows)} HTTP rows and 2 complete stored subtrees')
