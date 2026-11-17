"""Record actual Flask handlers and strict JSON error controls for N8-6.

Only the documented bad-request migration gets a different Go expectation;
normal success/credential/field-type cases compare the live servers directly.
"""
import json
import os
from pathlib import Path
import tempfile

with tempfile.TemporaryDirectory(prefix='n8-6-bind-') as directory:
 os.environ.update(DATA_DIR=directory,ADMIN_USERNAME='diff-admin',ADMIN_PASSWORD='diff-password',JWT_SECRET_KEY='diff-shared-jwt-secret')
 from flask import Flask,request,jsonify
 from werkzeug.exceptions import HTTPException
 from backend.routes import auth_bp
 from backend.routes import auth
 from backend.routes.tasks import tasks_bp
 from backend.common.auth import generate_token
 app=Flask(__name__)
 app.register_blueprint(auth_bp)
 app.register_blueprint(tasks_bp)
 @app.post('/control/strict')
 def strict():
  return jsonify(request.get_json())
 @app.errorhandler(HTTPException)
 def error(exc):
  return jsonify(success=False,message=exc.description),exc.code
 client=app.test_client()
 headers={'Authorization':'Bearer '+generate_token('diff-admin')}
 J='application/json'
 vectors=[
  ('login bad json','/api/auth/login','{"username":',J,400),
  ('login text/plain','/api/auth/login','{}','text/plain',415),
  ('login list','/api/auth/login','[1]',J,400),
  ('login absent content-type','/api/auth/login','{}','',415),
  ('login empty JSON','/api/auth/login','',J,400),
  ('login whitespace JSON','/api/auth/login','  ',J,400),
  ('login trailing JSON','/api/auth/login','{} {}',J,400),
  ('login null','/api/auth/login','null',J,400),
  ('login scalar bool','/api/auth/login','true',J,400),
  ('login scalar number','/api/auth/login','1',J,400),
  ('login scalar string','/api/auth/login','"text"',J,400),
 ]
 for literal in ('NaN','Infinity','-Infinity'):
  vectors.append(('login nonfinite '+literal,'/api/auth/login','{"username":'+literal+',"password":"diff-password"}',J,400))
  vectors.append(('task nonfinite '+literal,'/api/tasks/interval','{"key":"subscription:missing","interval_seconds":'+literal+'}',J,400))
 output=[]
 for name,path,body,content_type,status in vectors:
  kwargs={'data':body,'headers':headers}
  if content_type:kwargs['content_type']=content_type
  old=client.open(path,method='PUT' if path.startswith('/api/tasks') else 'POST',**kwargs)
  if path.startswith('/api/tasks'):
   control=client.put(path,data='{',content_type=J,headers=headers)
  else:
   control=client.post('/control/strict',data='{' if status==400 else '{}',content_type=J if status==400 else content_type or None)
  assert control.status_code==status,(name,control.status_code)
  output.append(dict(name=name,path=path,body=body,content_type=content_type,python_status=old.status_code,
   python_body=json.dumps(old.get_json(),ensure_ascii=True,separators=(',',':')),
   go_status=status,go_body=json.dumps(control.get_json(),ensure_ascii=True,separators=(',',':'))))
 path=Path(__file__).with_name('n8_6_bind.json')
 path.write_text(json.dumps(output,ensure_ascii=True,indent=2)+'\n')
 print(f'{len(output)} actual-handler/control HTTP vectors recorded')
