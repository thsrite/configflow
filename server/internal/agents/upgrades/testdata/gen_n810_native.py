"""Bind unchanged A15 rows to approved native storage/credential policies.
No Go code or output is read. Run on a test server from the repository root.
"""
import hashlib,json,math,pathlib
root=pathlib.Path(__file__).parent
archive=root/'a15_review.json'
source=json.loads(archive.read_bytes())
def compact(value):return json.dumps(value,ensure_ascii=False,separators=(',',':')).encode()
rows=[]
def add(group,key,original,field):
 rows.append(dict(group=group,key=key,original=original,row_sha256=hashlib.sha256(compact(original)).hexdigest(),native_field=field))
for row in source['poll']:
 record=row['record'];field=None
 if not isinstance(record,dict):field='latest_upgrade'
 elif not isinstance(record.get('status'),str):field='latest_upgrade.status'
 elif 'protocol' in record and record['protocol'] is not None and type(record['protocol']) not in (int,float):field='latest_upgrade.protocol'
 elif record.get('status') not in ('succeeded','failed','rolled_back'):
  if not isinstance(record.get('update_id'),str) or not record['update_id']:field='latest_upgrade.update_id'
  elif not all(isinstance(record.get(k),str) for k in ('previous_version','target_version')):field='latest_upgrade.version'
  elif 'protocol' not in record or type(record['protocol']) not in (int,float):field='latest_upgrade.protocol'
  elif record.get('recovery_reported_at') is not None and (type(record['recovery_reported_at']) not in (int,float) or not math.isfinite(float(record['recovery_reported_at']))):field='latest_upgrade.recovery_reported_at'
  elif 'last_heartbeat' in row['agent'] and not isinstance(row['agent']['last_heartbeat'],str):field='last_heartbeat'
  else:
   started=record.get('started_at')
   try:valid=type(started) in (int,float) and math.isfinite(float(started))
   except OverflowError:valid=False
   if not valid:field='latest_upgrade.started_at'
 if field:add('poll',row['name'],row,field)
name='malformed previous'
add('start',name,dict(name=name,python_error=source['starts'][name]),'latest_upgrade')
for name,token in [('none',None),('int',7)]:add('header',name,dict(name=name,token=token,python=source['headers'][name]),'endpoint')
out=dict(archive='a15_review.json',archive_sha256=hashlib.sha256(archive.read_bytes()).hexdigest(),rows=rows)
(root/'n810_native.json').write_text(json.dumps(out,ensure_ascii=False,indent=2)+'\n')
print(f'bound {len(rows)} rows')
