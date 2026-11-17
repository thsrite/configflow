"""Real Python manager oracle; stdin is synthetic Agent/source/scenario, stdout JSON."""
import contextlib,io,json,sys,tempfile
from pathlib import Path
from backend.common.config_repository import ProfileRepository
from backend.common import config as cm
from backend.agents.manager import AgentManager
from backend.agents.deployment_bundle import build_deployment_bundle
from backend.agents import rule_sync_store
# A23 is outside A14; match the specified unregistered SetIPv6Enroll no-op.
# No preparation, publisher, transport, timeout or journal code is replaced.
from backend.agents import ipv6_verification
ipv6_verification.enroll=lambda *args,**kwargs: None
request=json.load(sys.stdin)
with tempfile.TemporaryDirectory() as directory, contextlib.redirect_stdout(io.StringIO()):
 r=ProfileRepository(directory);cm._repository=r;cm.reset_config_context();m=AgentManager(r)
 agent=request['agent'];m._update_agents(lambda agents: agents.append(agent))
 source=request['source'];service=agent['service_type'];main='mode: rule\n' if service=='mihomo' else 'plugins: []\n'
 b=build_deployment_bundle(agent,source,main,deployment_id='oracle',local_dns_watchdog=False)
 answers=[]
 for action in request['actions']:
  if action=='publish':result=m._publish_deployment(agent['id'],b,activate=request['activate'],config_revision=7,source_snapshot=source,base_url='https://panel.example.com')
  elif action=='poll':result=m.get_deployment(agent['id'],'oracle')
  elif action=='activate':result=m.activate_deployment(agent['id'],'oracle')
  elif action=='rollback':result=m.rollback_deployment(agent['id'],'oracle')
  elif action=='recover':result=m.recover_deployment(agent['id'],'oracle')
  else:raise ValueError(action)
  answers.append(dict(result=result,agent=m.get_agent_by_id(agent['id'])))
 files={}
 for path in Path(directory).rglob('*'):
  if path.is_file() and '.agent-rule-sync' in path.parts:
   if path.name=='receipt.json':files[str(path.relative_to(directory))]=json.loads(path.read_bytes())
   elif path.suffix=='.gz':files[str(path.relative_to(directory))]='archive'
 output=dict(answers=answers,files=files)
print(json.dumps(output,ensure_ascii=False))
