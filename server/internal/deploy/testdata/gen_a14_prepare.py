"""A14 preparation oracle: real public Python generators and preparation, synthetic inputs only."""
import base64, copy, json, tempfile
from pathlib import Path
from backend.common import config as cm
from backend.common.config_repository import ProfileRepository
from backend.routes.agents import _prepare_transactional_content
from backend.agents.config_generator import generate_agent_config
root=Path(__file__).parent
rows=[]
with tempfile.TemporaryDirectory() as directory:
 cm._repository=ProfileRepository(directory);cm.reset_config_context()
 source={'profile_id':'default','system_config':{'server_domain':'https://panel.example.com'},'nodes':[], 'subscriptions':[], 'proxy_groups':[], 'rule_configs':[], 'rule_library':[], 'rulesets':[], 'rules':[], 'mihomo':{'custom_config':'mode: rule\n'},'mosdns':{},'oxidns':{}}
 for service in ('mihomo','mosdns','oxidns','surge','unknown'):
  for variant in ('empty','custom'):
   cfg=copy.deepcopy(source)
   if variant=='custom' and service=='mihomo':cfg['mihomo']['custom_config']='mode: global\nproxies: [{name: Example, type: socks5, server: 192.0.2.1, port: 1080}]\n'
   agent={'id':'prepare-agent','profile_id':'default','service_type':service,'version':'1.8.60-go','deployment_method':'docker'}
   row=dict(name=service+'-'+variant,source=cfg,agent=agent)
   try:row['pull']=generate_agent_config(cfg,agent,'https://panel.example.com')
   except Exception as e:row['pull_error']=str(e)
   try:
    p=_prepare_transactional_content(agent,cfg,base_url='https://panel.example.com',deployment_id='prepare-corpus')
    b=p['bundle'];row['manifest']=b.manifest;row['files']={k:base64.b64encode(v).decode() for k,v in b.files().items()}
   except Exception as e:row['error']=str(e)
   rows.append(row)
(root/'prepare.json').write_text(json.dumps(rows,ensure_ascii=False))
print('A14 prepare:',len(rows),'real Python generator/preparation cases')
