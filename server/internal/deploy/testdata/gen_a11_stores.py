"""Generate real Python receipt/preview directories and their public view."""
import json, shutil
from pathlib import Path
from backend.agents.deployment_bundle import build_deployment_bundle
from backend.agents import generated_preview as previews, rule_sync_store as receipts
from backend.common.config_repository import ProfileRepository
from backend.common import config as config_module
root=Path(__file__).parent/'stores'
if root.exists():shutil.rmtree(root)
root.mkdir()
r=ProfileRepository(root);config_module._repository=r;config_module.reset_config_context()
source={'profile_id':'default','system_config':{'server_domain':'https://panel.example'},'rule_library':[], 'unknown_future':{'preserve':['中文😀',1.0]},'agents':[{'id':'must-not-be-saved','token':'synthetic-secret'}]}
agent={'id':'agent-example','profile_id':'default','service_type':'mihomo','name':'Example','host':'192.0.2.10','port':8080,'token':'synthetic-token','enabled':True}
b=build_deployment_bundle(agent,source,'mode: rule\n',custom_files=[{'path':'中文😀.txt','content':'bytes\n'}],deployment_id='python-store')
receipts.capture(r,agent['id'],b,source,'https://panel.example')
main=next(f for f in b.manifest['files'] if f['role']=='config')
agent.update(config_version=main['sha256'],applied_content={'deployment_id':b.manifest['deployment_id'],'profile_id':'default','service_type':'mihomo','config_version':main['sha256']})
pv=previews.save(r,agent,source,1,'https://panel.example',{'bundle':b},members=[agent,dict(agent,id='peer-example')])
folder=root/'.generated-previews'/pv['id'];meta=json.loads((folder/'record.json').read_bytes());meta['expires_at']=4102444800;(folder/'record.json').write_text(json.dumps(meta,ensure_ascii=False));pv['expires_at']=4102444800
(root/'expected.json').write_text(json.dumps({'agent':agent,'preview_id':pv['id'],'public':previews.public(pv),'source':source},ensure_ascii=False))
# Fixtures contain only the private files under test, no unrelated SQLite DB.
for p in root.iterdir():
 if p.name not in ('.generated-previews','.agent-rule-sync','expected.json'):
  shutil.rmtree(p) if p.is_dir() else p.unlink()
print('A11 stores: Python receipt + preview archive/metadata and public files')
