"""CFG-01 synthetic real-Python network oracle; run only through rt."""
import json,threading
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
from pathlib import Path
from backend.utils.rule_fetch import request_rule

cases=[]
def add(name,start,locations,refused=True):
 cases.append(dict(name=name,start=start,locations=locations,refused=refused))
for name,path in [('dot','/api/../redirect'),('encoded-dot','/api/%2e%2e/redirect'),
 ('encoded-dot-uppercase','/api/%2E%2E/redirect'),('double-encoded-dot','/api/%252e%252e/redirect'),
 ('backslash','/api/..\\redirect'),('encoded-backslash','/api/..%5credirect')]:
 add(name,'{origin}'+path,['{external}/landing'])
add('encoded-api-dot','{origin}/%61pi/../redirect',['{external}/landing'])
add('encoded-api-valid','{origin}/%61pi/start',[],False)
add('cross-origin','{origin}/api/start',['{external}/landing'])
add('outside-api','{origin}/api/start',['/landing'])
add('redirect-dot','{origin}/api/start',['/api/../landing'])
add('redirect-encoded-dot','{origin}/api/start',['/api/%2e%2e/landing'])
add('redirect-double-encoded-dot','{origin}/api/start',['/api/%252e%252e/landing'])
add('redirect-backslash','{origin}/api/start',['/api/..\\landing'])
for hops in (3,5):
 add('escape-hop-'+str(hops),'{origin}/api/start',[f'/api/step{i}' for i in range(1,hops)]+['{external}/landing'])
add('internal-valid','{origin}/api/start',['/api/second'],False)
add('internal-dot-stays-api','{origin}/api/first/../start',['/api/second'],False)
add('internal-encoded-name','{origin}/api/name%20part',[],False)
add('ordinary-external','{external}/source',[],False)

for case in cases:
 state={'source_paths':[],'external_paths':[],'forbidden':0}
 class Handler(BaseHTTPRequestHandler):
  def log_message(self,*args):pass
  def do_GET(self):
   external=self.server is destination
   paths=state['external_paths' if external else 'source_paths']
   index=len(paths);paths.append(self.path)
   if self.path.endswith('/landing'):
    state['forbidden']+=1
    status,location=200,None
   elif not external and index<len(case['locations']):
    status,location=302,case['locations'][index].replace('{origin}',origin).replace('{external}',outside)
   else:status,location=200,None
   self.send_response(status)
   if location:self.send_header('Location',location)
   self.send_header('Content-Length','2');self.end_headers();self.wfile.write(b'ok')
 source=ThreadingHTTPServer(('127.0.0.1',0),Handler)
 destination=ThreadingHTTPServer(('127.0.0.1',0),Handler)
 origin=f'http://127.0.0.1:{source.server_port}'
 outside=f'http://127.0.0.1:{destination.server_port}'
 threads=[threading.Thread(target=s.serve_forever,kwargs={'poll_interval':.01}) for s in (source,destination)]
 for thread in threads:thread.start()
 outcome={'refused':False}
 try:
  response=request_rule(case['start'].replace('{origin}',origin).replace('{external}',outside),
   config_data={'system_config':{'server_domain':origin}},max_redirects=8,timeout=(2,2))
  outcome.update(status=response.status_code,body=response.text)
  response.close()
 except Exception as error:
  outcome.update(refused=True,error_class=type(error).__name__,error=str(error))
 finally:
  for s in (source,destination):s.shutdown();s.server_close()
  for thread in threads:thread.join()
 outcome.update(state)
 case['python']=outcome
 print(case['name'],json.dumps(outcome),flush=True)
Path(__file__).with_name('cfg01.json').write_text(json.dumps(cases,ensure_ascii=False,indent=2)+'\n')
