"""D2 private provider paths from the real Python generator. Original corpus is
read-only. Retain only the exact path substitutions; every other generated field
must be unchanged, checked here before writing the small independent manifest.
"""
import ast
import copy
import gzip
import hashlib
import json
from pathlib import Path
from types import SimpleNamespace
import yaml
from backend.utils import group_candidates as gc

root=Path(__file__).parent
archive=root/'mihomo_corpus.jsonl.gz'
# Reuse the original independent recorder's fake subscription/device owners,
# without executing corpus generation or writing the original archive.
source=(root/'gen_mihomo.py').read_text().split('# ------------------------------------------------------------------ values')[0]
env={}
exec(compile(source,'gen_mihomo.py (owner fixtures)','exec'),env)
real_sha256=hashlib.sha256

def native_key(data):
    key=ast.literal_eval(data.decode())
    assert isinstance(key,tuple) and len(key)==3 and all(isinstance(v,str) for v in key)
    payload=json.dumps(key,ensure_ascii=False,separators=(',',':'))
    for char,escaped in [('&','\\u0026'),('<','\\u003c'),('>','\\u003e'),('\u2028','\\u2028'),('\u2029','\\u2029')]:
        payload=payload.replace(char,escaped)
    return real_sha256(payload.encode())

references={}
with gzip.open(archive,'rt') as handle:
 for index,line in enumerate(handle):
    row=json.loads(line)
    if row['fn'] not in ('generate','provider_downloads') or 'group_filter_' not in str(row.get('out','')):
        continue
    env['set_caches'](row.get('caches',{}),row.get('downloads',{}))
    previous=gc.hashlib
    gc.hashlib=SimpleNamespace(sha256=native_key)
    try:
        if row['fn']=='generate':
            args=row['args']
            native=env['mihomo'].generate_mihomo_config(copy.deepcopy(args['profile']),args['base_url'],args['sync_lan_hosts'],
                preflight_providers=False,region_probe=args['region_probe'],report={},mihomo_core_version=args['core_version'],for_agent=args['for_agent'])
            original=yaml.safe_load(row['out']); changed=yaml.safe_load(native)
            originals=original.get('proxy-providers',{});changes=changed.get('proxy-providers',{})
            pairs=[(provider,changes[name]) for name,provider in originals.items()]
        else:
            native=env['mihomo'].get_mihomo_provider_downloads(copy.deepcopy(row['args'][0]),row['args'][1])
            original=copy.deepcopy(row['out']);changed=native
            assert len(original)==len(changed)
            pairs=list(zip(original,changed))
    finally:
        gc.hashlib=previous
    paths={}
    field='path' if row['fn']=='generate' else 'local_path'
    for before,after in pairs:
        if before.get(field)!=after.get(field):
            old,new=before[field],after[field]
            assert old.startswith('./providers/group_filter_') and new.startswith('./providers/group_filter_')
            assert old not in paths or paths[old]==new
            paths[old]=new
            before[field]=new
    assert paths and original==changed, ('non-D2 generator difference',index)
    references[str(index)]={'fn':row['fn'],'paths':paths}
(root/'n89_provider_paths.json').write_text(json.dumps({'archive_sha256':real_sha256(archive.read_bytes()).hexdigest(),'rows':references},ensure_ascii=False,indent=2)+'\n')
print('independent D2 provider rows:',len(references))

# The reused owner fixtures create only synthetic temporary cache markers.
import shutil
shutil.rmtree(env["TMP"])
