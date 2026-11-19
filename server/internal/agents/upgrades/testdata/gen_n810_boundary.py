"""Record real Python A15 bugs before applying the approved native policy."""
import copy,datetime,hashlib,json,pathlib,uuid
from backend.agents import upgrades,operation_events
NOW=1800000000.0
class FixedDate(datetime.datetime):
 @classmethod
 def now(cls,tz=None):return cls.fromtimestamp(NOW,tz)
operation_events.datetime=FixedDate
upgrades.time.time=lambda:NOW
upgrades.uuid.uuid4=lambda:uuid.UUID('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')
ARTIFACTS={arch:dict(sha256='a'*64,size=100) for arch in upgrades.ARCHES}
upgrades.artifacts=lambda:copy.deepcopy(ARTIFACTS)
class Manager:
 def __init__(self,agent):self.agent=copy.deepcopy(agent)
 def get_agent_by_id(self,id):return copy.deepcopy(self.agent)
 def _update_agents(self,fn):return fn([self.agent])
class Response:
 def __init__(self,body,status=200):self.body=body;self.status_code=status
 def json(self):return copy.deepcopy(self.body)
 def raise_for_status(self):pass
base=dict(id='a1',name='synthetic',host='192.0.2.1',port=8080,token='synthetic-token',deployment_method='shell',version='1.0')
rows=[]
def run(name,operation,agent,info,worker=None):
 manager=Manager(agent);calls=[]
 def request(a,method,path,**kwargs):
  calls.append(dict(method=method,path=path,json=kwargs.get('json')))
  return Response(info if path.endswith('info') else (worker or {}))
 upgrades._request=request
 original=dict(name=name,operation=operation,agent=copy.deepcopy(agent),info=info,worker=worker,artifacts=ARTIFACTS,now=NOW)
 try:outcome=dict(result=getattr(upgrades,operation)(manager,'a1'))
 except Exception as exc:outcome=dict(error_class=type(exc).__name__,error=str(exc))
 original.update(python=outcome,stored=copy.deepcopy(manager.agent),calls=calls)
 digest=hashlib.sha256(json.dumps(original,ensure_ascii=False,separators=(',',':')).encode()).hexdigest()
 rows.append(dict(original=original,row_sha256=digest,native='pending' if operation=='poll' else 'refuse-before-reserve'))
state=dict(update_id='job',agent_id='a1',status='checking',protocol=2,started_at=NOW,previous_version='1.0',target_version=upgrades.get_latest_version())
run('wrong worker identity with correct executable proof','poll',dict(base,latest_upgrade=state),dict(agent_id='a1',version=upgrades.get_latest_version(),migration_ready=True),dict(update_id='job',agent_id='other',status='succeeded'))
for name,extra in [('missing',{}),('empty',dict(architecture='')),('number',dict(architecture=7)),('array',dict(architecture=[]))]:
 run('architecture '+name,'start',base,dict(agent_id='a1',deployment_method='shell',**extra))
path=pathlib.Path(__file__).with_name('n810_boundary_python.json')
path.write_text(json.dumps(rows,ensure_ascii=False,indent=2)+'\n')
print(f'recorded {len(rows)} real Python rows')
