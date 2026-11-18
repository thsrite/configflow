"""Real Flask MCP endpoint and existing REST owners; loopback/synthetic data only."""
import json,os,sys
from flask import Flask
from backend.common import config,auth
from backend.common.config_repository import ProfileRepository
from backend.routes import register_blueprints
from backend.mcp_server import mcp_bp
r=ProfileRepository(sys.argv[1]);config.set_repository(r)
r.save_system({'system_config':{'config_token':'synthetic-mcp-admin','rule_proxy_token':'synthetic-internal','retired_rule_proxy_tokens':['synthetic-retired']}})
r.save_shared({'subscriptions':[{'id':'sub-a','name':'A','url':'https://example.test/a','health_check':{'enable':False,'interval':300}},{'id':'sub-b','name':'B','url':'https://example.test/b'}]})
r.save_profile('default',{'proxy_groups':[{'id':'group-a','name':'A','type':'select','manual_nodes':['DIRECT']},{'id':'group-b','name':'B','type':'select','manual_nodes':['DIRECT']}]})
app=Flask(__name__);register_blueprints(app);app.register_blueprint(mcp_bp)
for line in sys.stdin:
 try:
  q=json.loads(line)
  auth.ADMIN_USERNAME='synthetic-admin' if q.get('login') else ''
  auth.ADMIN_PASSWORD='synthetic-password' if q.get('login') else ''
  if 'token' in q:
   r.update_system_transaction(lambda s:s['system_config'].update(config_token=q['token']))
  config.reset_config_context()
  response=app.test_client().open(q['path'],method=q['method'],data=q.get('body',''),headers=q.get('headers',{}))
  raw=response.get_data(as_text=True)
  try:body=json.loads(raw)
  except ValueError:body=raw
  state={'subscriptions':r.get_shared().get('subscriptions'),'groups':r.get_profile('default').get('proxy_groups')}
  print('MCP_RESULT '+json.dumps({'status':response.status_code,'body':body,'state':state},ensure_ascii=False),flush=True)
 except Exception as e:
  print('MCP_RESULT '+json.dumps({'oracle_error':type(e).__name__+': '+str(e)}),flush=True)
