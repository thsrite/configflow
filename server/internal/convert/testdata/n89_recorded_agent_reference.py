"""One exact recorded call with the already approved invalid Agent version guard.

Verify the original real Python output first, then inject only the approved
version guard. Mutated profile must remain identical to the original record.
No Go output is used and no recorded source data is copied into this fixture.
"""
import contextlib, copy, gzip, hashlib, io, json, pathlib, sys
root=pathlib.Path(__file__).parent
sys.path.insert(0,str(root))
with contextlib.redirect_stdout(io.StringIO()):
 import gen_mihomo as gm
 from backend.common import mihomo_easytier as et
archive=(root/'mihomo_recorded.jsonl.gz').read_bytes()
index=1144
line=gzip.decompress(archive).splitlines()[index]
row=json.loads(line)
assert row['test']=='backend/test_mihomo_easytier.py::test_old_agent_core_is_refused_and_unknown_versions_pass'
assert row['fn']=='generate' and row['args']['for_agent'] is True
assert row['args']['mihomo_core_version']=='alpha-abc' and row['args']['report'] is False
assert row['env']['gateway'][0]['before']==row['env']['gateway'][0]['after']
original=et.apply_easytier
message='Agent 上报的内核版本无法识别：alpha-abc，请升级或重启 Agent'
def guarded(document,config,*,for_agent=False,core_version=None):
 if for_agent and core_version=='alpha-abc' and et.applies(et.easytier_settings(config),for_agent=True):
  raise et.EasyTierSettingsError(message)
 return original(document,config,for_agent=for_agent,core_version=core_version)
def generate():
 args=row['args'];work=copy.deepcopy(args['profile']);result={}
 gm.set_caches(row['env']['caches'],row['env']['downloads'])
 try:
  result['out']=gm.mihomo.generate_mihomo_config(work,args['base_url'],args['sync_lan_hosts'],preflight_providers=args['preflight_providers'],region_probe=args['region_probe'],report=None,mihomo_core_version=args['mihomo_core_version'],for_agent=args['for_agent'])
 except Exception as error:result['err']=[type(error).__name__,str(error)]
 result['mutated']=gm.conv(work)
 return result
before=generate()
assert before['out']==row['out'] and before['mutated']==row['mutated']
et.apply_easytier=guarded
try:after=generate()
finally:et.apply_easytier=original
assert after['err']==['EasyTierSettingsError',message]
assert after['mutated']==row['mutated']
result={'archive_sha256':hashlib.sha256(archive).hexdigest(),'index':index,'row_sha256':hashlib.sha256(line).hexdigest(),'error':after['err'],'mutation':'unchanged from original record','reason':'approved nonempty invalid Agent version is refused before EasyTier graph mutation'}
(root/'n89_recorded_agent_reference.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
print('RECORDED AGENT REFERENCE: 1 exact real Python case; original mutation unchanged')
