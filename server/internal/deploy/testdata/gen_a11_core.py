"""Real Python oracle for persisted deployment identities and archive loading."""
import copy, gzip, hashlib, json
from pathlib import Path
from backend.agents.deployment_bundle import (relative_path, deployment_content_fingerprint,
    deployment_content_digests, _archive_bytes)
root = Path(__file__).parent
files = {'config.yaml': (b'rules: []\n', 'config'), 'rules/中文😀.txt': (b'example.com\n', 'rule'),
         'empty.txt': (b'', 'custom')}
manifest = dict(protocol_version=1, deployment_id='a11-python', agent_id='agent-example',
    profile_id='profile-example', service_type='mihomo', config_path='config.yaml', files=[
    dict(path=p, size=len(v), sha256=hashlib.sha256(v).hexdigest(), role=r) for p,(v,r) in sorted(files.items())])
rows=[]
def record(name, m):
    row={'name':name, 'manifest':m}
    try: row.update(fingerprint=deployment_content_fingerprint(m), digests=deployment_content_digests(m))
    except ValueError as e: row['error']=str(e)
    rows.append(row)
record('base',manifest)
for cp in ('mihomo.yaml','中文😀.yaml'):
    m=copy.deepcopy(manifest);m['config_path']=cp
    next(f for f in m['files'] if f['role']=='config')['path']=cp
    record('retarget-'+cp,m)
for field,values in {'path':['./x','../x','x//y','x/./y','x\x7fy','manifest.json','./config.yaml'],
    'role':['resource','unknown',None,1], 'sha256':['A'*64,'a'*63,'z'*64,123,None],
    'size':[-1,0,64*1024*1024+1,1.0,True,'1',None]}.items():
    for value in values:
        m=copy.deepcopy(manifest);m['files'][0][field]=value;record(f'{field}-{value}',m)
for name, mutate in [('empty',lambda m:m.update(files=[])),('duplicate',lambda m:m['files'].append(m['files'][0])),
    ('4097',lambda m:m.update(files=m['files'][:1]*4097)),
    ('zero-config',lambda m:m['files'][0].update(size=0)),
    ('collision',lambda m:(m.update(config_path='other.yaml'),m['files'][0].update(path='other.yaml'),m['files'].append(dict(path='config.yaml',role='rule',size=0,sha256='0'*64))))]:
    m=copy.deepcopy(manifest);mutate(m);record(name,m)
paths=[]
for p in ('x','./x','././x','中文/😀.txt','../x','x/../y','/x','x//y','x/','x\\y','x\n','x\x7f','manifest.json','.configflow-test','','.','./'):
    row={'value':p}
    try: row['result']=relative_path(p)
    except ValueError as e:row['error']=str(e)
    paths.append(row)
(root/'core.json').write_text(json.dumps(dict(rows=rows,paths=paths),ensure_ascii=False))
(root/'python.tar.gz').write_bytes(_archive_bytes(manifest,files))
(root/'python-manifest.json').write_text(json.dumps(manifest,ensure_ascii=False))
print(f'A11 core: {len(rows)} manifests, {len(paths)} paths, Python PAX archive')
