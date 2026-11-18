"""Real Python generation with only the approved Agent version guard injected.

Frozen archives and byte manifests stay unchanged. Both historical outcomes and
new error, partial report and profile state are captured; no Go output is read.
"""
import contextlib,copy,gzip,hashlib,io,json,pathlib,sys
root=pathlib.Path(__file__).parent
sys.path.insert(0,str(root))
with contextlib.redirect_stdout(io.StringIO()):
 import gen_mihomo as gm
 from backend.common import mihomo_easytier as et
original=et.apply_easytier
selection={'mihomo_corpus':(449,1774,1849,2401,2554,2761,3820,4252,4988,5306,6157,7272),'review_mihomo':(471,)}
byte_data=(root/'n88_generation_bytes.json').read_bytes()
byte_cases={entry['row']:entry['sha256'] for entry in json.loads(byte_data)['cases'] if entry['source']=='mihomo_corpus.jsonl.gz'}
sources={};cases={}
def guarded(document,config,*,for_agent=False,core_version=None):
 settings=et.easytier_settings(config)
 if for_agent and core_version and et.applies(settings,for_agent=for_agent):
  assert core_version in ('alpha-abc','unknown')
  raise et.EasyTierSettingsError('Agent 上报的内核版本无法识别：'+core_version+'，请升级或重启 Agent')
 return original(document,config,for_agent=for_agent,core_version=core_version)
def generate(row):
 args=row['args'];work=copy.deepcopy(args['profile']);report={}
 gm.set_caches(row.get('caches',{}),row.get('downloads',{}))
 result={}
 try:result['out']=gm.mihomo.generate_mihomo_config(work,args['base_url'],args['sync_lan_hosts'],preflight_providers=False,region_probe=args['region_probe'],report=report,mihomo_core_version=args['core_version'],for_agent=args['for_agent'])
 except Exception as error:result['err']=[type(error).__name__,str(error)]
 result['report']=gm.conv(report);result['mutated']=gm.conv(work)
 return result
for name,indices in selection.items():
 data=(root/(name+'.jsonl.gz')).read_bytes();sources[name]=hashlib.sha256(data).hexdigest()
 lines=gzip.decompress(data).decode().rstrip('\n').split('\n')
 for index in indices:
  line=lines[index];row=json.loads(line)
  assert row['fn']=='generate' and row['args']['for_agent']is True
  et.apply_easytier=original;before=generate(row)
  for field in ('err','out','mutated','report'):
   if field in row:assert before.get(field)==row[field],(name,index,field)
  et.apply_easytier=guarded;after=generate(row)
  assert after.get('err',[None])[0]=='EasyTierSettingsError',(name,index,after)
  entry={'input_sha256':hashlib.sha256(line.encode()).hexdigest(),'expected':after,'reason':'approved nonempty unknown Agent core version fails before EasyTier graph mutation'}
  if name=='mihomo_corpus' and index in byte_cases:entry['old_byte_sha256']=byte_cases[index]
  cases[f'{name}/{index}']=entry
et.apply_easytier=original
assert len(cases)==13
assert sum('old_byte_sha256'in value for value in cases.values())==9
(root/'n89_agent_generation_reference.json').write_text(json.dumps({'sources':sources,'byte_manifest_sha256':hashlib.sha256(byte_data).hexdigest(),'cases':cases},ensure_ascii=False,indent=2)+'\n')
print('native Agent generation reference: 13 exact Python cases; 9 immutable byte-manifest entries explicitly reject invalid machine versions')
