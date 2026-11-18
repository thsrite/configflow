"""Synthetic MCP adapter contracts, recorded from real Python tool handlers.
The test double observes only REST calls; HTTP/storage parity uses oracle.py.
"""
import json,sys
from urllib.parse import urlencode,parse_qsl
from pathlib import Path
from backend.common import config
from backend.common.config_repository import ProfileRepository
from backend.mcp_server import tools
r=ProfileRepository(sys.argv[1]);config.set_repository(r)
common={'client_type':'mihomo','content':'proxies: []','name':'Synthetic','profile_id':'default','id':'synthetic','action':'update','data':{'name':'Changed'},'source_profile_id':'synthetic','collection':'nodes','ids':['synthetic'],'target':'mihomo','section':'cache','url':'https://example.test/rules','query':'example.test','rule_type':'DOMAIN','domains':['example.test'],'policy':'DIRECT','method':'docker_compose','regex':'HK','source':'subscription','rule':{'policy':'DIRECT'},'routes':{'synthetic':'default'}}
cases=[]
for definition in tools.list_tools():
 name=definition['name'];args={key:common[key] for key in definition['inputSchema']['required']}
 if name in ('manage_profile','manage_subscription','manage_node','manage_rule_library','manage_proxy_group','manage_rule','manage_aggregation'):args.update(id='synthetic',data={'name':'Changed'})
 if name=='manage_custom_config':args.update(action='get')
 if name=='manage_config_backup':args.update(action='export')
 if name=='run_backup':args.update(action='test_connection')
 if name=='manage_app_logs':args.update(action='info')
 if name=='compose_rule':args.update(source={'kind':'values','text':'example.test'},rule={'policy':'DIRECT'})
 if name=='update_settings':args.update(section='backup')
 if name=='manage_agent':args.update(action='push_config')
 if name=='bind_agent_profile':args.update(profile_id='default')
 cases.append((name,args))
for name in ('manage_subscription','manage_node','manage_rule_library','manage_proxy_group','manage_rule','manage_aggregation','manage_profile'):
 for action in ('create','delete'):
  cases.append((name,dict(id='synthetic',action=action,data={'id':'synthetic','name':'Created'})))
cases.extend([
 ('manage_rule',dict(action='create',position='bottom',data={'id':'synthetic','dns':'default'})),
 ('manage_subscription',dict(action='update',id='synthetic',data={'health_check':{'enable':False}})),
 ('preview_aggregation',dict(id='synthetic',mode='count')),('preview_aggregation',dict(id='synthetic',mode='provider')),
 ('get_agent',dict(id='synthetic',include_status=True)),('get_agent_metrics',dict(id='synthetic',scope='history',hours=3)),
 ('get_agent_logs',dict(id='synthetic',lines=7,log_path='/var/log/synthetic.log')),
 ('get_agent_install_info',dict(method='docker_run',params={'name':'Synthetic','enable_mihomo':True})),
 ('get_settings',dict(section='backup')),('get_mosdns_settings',dict(section='cache')),
 ('update_settings',dict(section='server_domain',data={'server_domain':'https://panel.example.test'})),
 ('update_settings',dict(section='config_token',data={'config_token':'synthetic-token'})),
 ('manage_config_backup',dict(action='import',scope='profile',profile_id='default',data={'name':'Synthetic'})),
 ('manage_config_backup',dict(action='reset')),('manage_config_backup',dict(action='import',data={'name':'Synthetic'})),
 ('update_oxidns_settings',dict(data={'nested':{'child':None}})),
 ('set_rule_dns',dict(routes={'synthetic':'direct'},skip_unsupported=True)),
 ('manage_custom_config',dict(target='surge',action='set',content='')),
])
for action in ('update','delete','restart','uninstall','upgrade','clear_log','validate_log_path','get_logging','set_logging'):
 cases.append(('manage_agent',dict(id='synthetic',action=action,data={'name':'Changed'},log_path='/var/log/synthetic.log',enabled=False)))
collection_paths={'/api/subscriptions','/api/nodes','/api/rules','/api/rule-library','/api/proxy-groups'}
rows=[]
for name,args in cases:
 calls=[]
 def call_api(method,path,body=None,query=None,headers=None,profile_id=None):
  calls.append(dict(method=method,path=path,body=body,query=dict(parse_qsl(urlencode({k:v for k,v in (query or {}).items() if v is not None},doseq=True),keep_blank_values=True))))
  item={'id':'synthetic','name':'Existing','keep':True,'health_check':{'enable':False,'interval':300}}
  if method=='GET':
   if path in collection_paths:return [item]
   if path.endswith('/provider') or path.endswith('/rule-proxy') or path.startswith('/api/generate'):return 'synthetic text'
   return item
  if path.endswith('/reorder'):return {'order':['synthetic']}
  return {'data':body}
 tools.call_api=call_api
 output=tools.call_tool(name,args)
 rows.append(dict(name=name,arguments=args,calls=calls,out=output))
Path('server/internal/mcp/testdata/adapters.json').write_text(json.dumps(rows,ensure_ascii=False,indent=2)+'\n')
print('Python adapter cases:',len(rows),'tools:',len(set(row['name'] for row in rows)))
