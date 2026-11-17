"""Real Python Build oracle; all remote sources served by one loopback server."""
import base64,copy,hashlib,json,threading,tempfile
from http.server import ThreadingHTTPServer,BaseHTTPRequestHandler
from pathlib import Path
import yaml
from backend.agents.deployment_bundle import build_deployment_bundle
from backend.agents.mihomo_resources import required_geo_resources
from backend.common import config as config_module
from backend.common.config_repository import ProfileRepository
root=Path(__file__).parent
payloads={'/classical':'DOMAIN-SUFFIX,example.com\nIP-CIDR,192.0.2.0/24', '/domain':'+.example.com\nexact.example.com\n','/empty':'', '/geo':'synthetic geo bytes'}
class Handler(BaseHTTPRequestHandler):
 def do_GET(self):
  self.send_response(200 if self.path in payloads else 404);self.end_headers();self.wfile.write(payloads.get(self.path,'').encode())
 def log_message(self,*a):pass
server=ThreadingHTTPServer(('127.0.0.1',0),Handler);threading.Thread(target=server.serve_forever,daemon=True).start();base=f'http://127.0.0.1:{server.server_port}'
source={'profile_id':'alpha','system_config':{'server_domain':'https://panel.example'},'rule_library':[{'id':'shared','name':'Shared','source_type':'content','content':'DOMAIN-SUFFIX,example.com\nIP-CIDR,192.0.2.0/24','behavior':'classical','format':'text'}]}
tmp=tempfile.TemporaryDirectory();config_module._repository=ProfileRepository(tmp.name);config_module.reset_config_context()
rows=[]
def add(name,service,main,*,src=None,providers=[],downloads=[],custom=[],pinned=[],watchdog=False):
 row=dict(name=name,service=service,source=copy.deepcopy(src or source),main=yaml.safe_dump(main,allow_unicode=True,sort_keys=False),providers=providers,downloads=downloads,custom=custom,pinned=pinned,watchdog=watchdog)
 try:
  b=build_deployment_bundle({'id':'agent-example','service_type':service,'profile_id':'alpha'},row['source'],row['main'],provider_downloads=providers,ruleset_downloads=downloads,custom_files=custom,pinned_files=pinned,local_dns_watchdog=watchdog,deployment_id='a11-corpus',base_url='https://panel.example')
  row['manifest']=b.manifest;row['files']={p:base64.b64encode(v.replace(base.encode(),b"@RULE_SERVER@") if p=="config.yaml" else v).decode() for p,v in b.files().items()}
 except ValueError as e:row['error']=str(e)
 rows.append(row)
for service in ['mihomo','mosdns','oxidns']:
 add(service+'-empty',service,{'mode':'rule'} if service=='mihomo' else {'plugins':[]})
 add(service+'-custom',service,{'mode':'rule'} if service=='mihomo' else {'plugins':[]},custom=[{'path':'./files/中文😀.txt','name':'自定义','content':'# bytes\n'}])
 add(service+'-duplicate',service,{'plugins':[]},custom=[{'path':'same','content':'a'},{'path':'same','content':'b'}])
 add(service+'-bad-path',service,{'plugins':[]},custom=[{'path':'../escape','content':'a'}])
 add(service+'-pin',service,{'plugins':[]},pinned=[{'path':'rules/pinned','content':b'original','role':'rule'}])
for fmt in ['text','yaml']:
 src=copy.deepcopy(source);src['rule_library'][0]['format']=fmt
 if fmt=='yaml':src['rule_library'][0]['content']='payload: ["+.example.com", "192.0.2.1"]\n'
 add('mihomo-classical-'+fmt,'mihomo',{'rule-providers':{'Shared':{'type':'http','url':'https://panel.example/api/profiles/alpha/rules/local/Shared?target=mihomo','path':'./rules/shared.list','behavior':'classical','format':fmt}}},src=src)
add('mihomo-inline','mihomo',{'rule-providers':{'Inline':{'type':'inline','payload':['+.example.com'],'path':'bad'}}})
add('mihomo-provider','mihomo',{'proxy-providers':{'Feed':{'type':'http','path':'./providers/feed.yaml'}}},providers=[{'local_path':'./providers/feed.yaml','name':'Feed','content':'proxies: []\n'}])
add('mihomo-missing-provider','mihomo',{'proxy-providers':{'Feed':{'type':'http','path':'./providers/feed.yaml'}}})
add('mihomo-missing-rule-url','mihomo',{'rule-providers':{'Set':{'type':'file','path':'./rules/a'}}})
add('mihomo-remote','mihomo',{'rule-providers':{'Set':{'type':'http','url':base+'/domain','path':'./rules/domain','behavior':'domain','format':'text'}}})
for service in ['mosdns','oxidns']:
 main={'plugins':[{'tag':'domains','type':'domain_set','args':{'files':['./rules/domain']}},{'tag':'ips','type':'ip_set','args':{'files':['./rules/ip']}}]}
 downloads=[{'name':'Domains','url':'https://panel.example/api/profiles/alpha/mosdns/rule-proxy?url='+base+'/classical&part=domain','local_path':'./rules/domain','behavior':'classical'}, {'name':'IPs','url':base+'/classical','local_path':'./rules/ip','behavior':'classical'}]
 add(service+'-split',service,main,downloads=downloads)
 add(service+'-empty-split',service,{'plugins':[{'tag':'ips','type':'ip_set','args':{'files':['./rules/ip']}}]},downloads=[dict(name='Empty',url='https://panel.example/api/profiles/alpha/mosdns/rule-proxy?url='+base+'/empty&part=ip',local_path='./rules/ip')])
 add(service+'-local',service,main,downloads=[dict(name=d['name'],url='https://panel.example/api/profiles/alpha/rules/local/Shared',local_path=d['local_path'],behavior='classical') for d in downloads])
 add(service+'-invalid-rule',service,{'plugins':[{'tag':'bad','type':'domain_set','args':{'files':['./bad']}}]},custom=[{'path':'./bad','content':'# source\n11\n'}])
 add(service+'-missing-file',service,{'plugins':[{'tag':'missing','type':'domain_set','args':{'files':['./absent']}}]})
 add(service+'-wrong-file-list',service,{'plugins':[{'tag':'bad','type':'domain_set','args':{'files':'bad'}}]})
 add(service+'-dual-type',service,{'plugins':[{'tag':k,'type':k,'args':{'files':['./same']}} for k in ['domain_set','ip_set']]})
 add(service+'-watchdog',service,{'plugins':[{'tag':'main','type':'sequence','args':[]}]},watchdog=True)
add('oxidns-native','oxidns',{'plugins':[{'tag':'domains','type':'domain_set','args':{'files':['./rules/a']}}]},custom=[{'path':'./rules/a','content':'example.com','native':True}])
add('oxidns-state','oxidns',{'plugins':[]},custom=[{'path':'state/db','content':'bad'}])
add('oxidns-include','oxidns',{'include':['extra.yaml'],'plugins':[]})
geo=[]
for document in [{'rules':['GEOIP,LAN,DIRECT']},{'rules':['GEOIP,CN,DIRECT']},{'rules':['GEOIP,CN,DIRECT'],'geodata-mode':True},{'rules':['GEOSITE,cn,DIRECT','IP-ASN,123,DIRECT']},{'sub-rules':{'nested':['AND,((GEOSITE,cn),(IP-ASN,123)),DIRECT']}},{'dns':{'fallback':['192.0.2.1']}},{'dns':{'fallback':['192.0.2.1'],'fallback-filter':{'geoip':False,'geosite':['cn']}}},{'dns':{'nameserver-policy':{'geosite:cn':'192.0.2.1'}}},{'dns':{'enhanced-mode':'fake-ip','fake-ip-filter-mode':'rule','fake-ip-filter':['GEOSITE,cn,real-ip']}},{'sniffer':{'enable':False,'skip-domain':['geosite:example'],'skip-src-address':['geoip:lan,cn']}}]:geo.append(dict(document=document,required=required_geo_resources(document,{})))
def default(v):
 if isinstance(v,bytes):return v.decode()
 raise TypeError(type(v))
(root/'build.json').write_text(json.dumps(dict(rows=rows,geo=geo,payloads=payloads),ensure_ascii=False,default=default).replace(base,'@RULE_SERVER@'))
server.shutdown();print(f'A11 Build: {len(rows)} bundles/failures, {len(geo)} Geo selections')
