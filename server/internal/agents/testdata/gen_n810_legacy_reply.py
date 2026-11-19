"""Real Python legacy success-HTTP errors; independent exact wire/result rows."""
import hashlib,json,pathlib,tempfile
import requests
from backend.agents.manager import AgentManager
from backend.common import config as config_module
from backend.common.config_repository import ProfileRepository
root=pathlib.Path(__file__).parent
repository=ProfileRepository(tempfile.mkdtemp())
config_module.set_repository(repository)
m=AgentManager(repository)
aid=m.register_agent({'name':'machine','host':'agent.example','port':8080,'service_type':'surge'})['id']
rows=[]
for method in ['push','restart']:
 for body in ['null','[]','"unexpected text"','17','upstream service unavailable\npassword=do-not-display']:
  def reply(*a,**k):
   r=requests.Response();r.status_code=200;r._content=body.encode();r._content_consumed=True
   if body.startswith('upstream'):r.headers['Content-Type']='text/plain'
   return r
  requests.post=reply
  result=m.push_config_to_agent(aid,'[General]') if method=='push' else m.restart_agent_service(aid)
  row={'method':method,'body':body,'status':200,'python_result':result}
  encoded=json.dumps(row,ensure_ascii=False,separators=(',',':')).encode()
  rows.append({'original':row,'sha256':hashlib.sha256(encoded).hexdigest()})
output=json.dumps(rows,ensure_ascii=False,indent=2)+'\n'
(root/'n810_legacy_reply.json').write_text(output)
print(hashlib.sha256(output.encode()).hexdigest())
