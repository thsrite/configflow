"""Rule/location boundary oracle using real Python functions."""
import base64, itertools, json
from pathlib import Path
from backend.agents.deployment_bundle import convert_mosdns_rules, materialize_rule
from backend.agents.deployment_locations import source_error, source_excerpt, preparation_context, DeploymentPreparationError
root=Path(__file__).parent
rows=[]
texts=['example.com\n+.example.net\n*.example.org', 'DOMAIN-SUFFIX,example.com\nIP-CIDR,192.0.2.0/24\nDOMAIN-REGEX,^a,b$',
 '192.0.2.1\n2001:db8::/32','payload:\n  - DOMAIN,example.com\n  - IP-CIDR,192.0.2.0/24\n',
 'payload:\n  - DOMAIN,example.com\n  - 11\n','payload: wrong\n','# comment\n11\n', 'ip:192.0.2.0/24\nfull:example.com',
 '# comment only','payload: []','payload:\n  - +.example.com\n  - +.example.com\n','domain:example.com\nkeyword:abc\nregexp:^a,b',
 'payload:\n - "DOMAIN-REGEX,^a,b$"', 'payload:\n - DOMAIN,example.com\n - {bad: item}', 'payload:\n - [broken', '\x00binary']
for i,(text,kind,mixed) in enumerate(itertools.product(texts,['domain_set','ip_set'],[False,True])):
 row=dict(text=text,kind=kind,mixed=mixed)
 try:row['result']=base64.b64encode(convert_mosdns_rules(text.encode(),kind,mixed=mixed)).decode()
 except ValueError as e:row['error']=str(e)
 rows.append(row)
locations=[]
for text in ['password: |\n  protected-value\nname: wrong\n','- password: |\n    protected-value\n  other: broken\n','uuid: "first\n  protected-value"\n','{password: "first\n  protected-value" broken}\n','-----BEGIN PRIVATE KEY-----\nprotected-value\n-----END PRIVATE KEY-----\n','payload:\n  - 11\n','name: 中文😀\n']:
 for line in range(1,len(text.splitlines())+2):locations.append(dict(text=text,line=line,result=source_excerpt(text,line)))
contexts=[]
for name,path in [('中文😀','./rules/a.txt'),('name\nx','x\ny'),('','')]:
 for message in ['Invalid rule YAML payload\n位置：第 2 行','Unsupported MosDNS rule plugin type','HTTP 404']:
  contexts.append(dict(name=name,path=path,message=message,result=str(preparation_context(DeploymentPreparationError(message),name=name,path=path))))
config={'profile_id':'alpha','system_config':{'server_domain':'https://panel.example/prefix'},'rule_library':[{'id':'inline','name':'中文😀','source_type':'content','content':'DOMAIN,example.com'}]}
materialized=[]
urls=['/api/profiles/alpha/rules/local/中文😀','https://panel.example/prefix/api/profiles/alpha/rules/local/中文😀',
 '/api/profiles/alpha/rule-library/content/inline','/api/profiles/beta/rules/local/中文😀',
 '/api/profiles/alpha/mosdns/rule-proxy?url=%2Fapi%2Fprofiles%2Falpha%2Frules%2Flocal%2F中文😀',
 '/api/profiles/alpha/mosdns/rule-proxy','/api/profiles/alpha/rules/local/missing']
for raw in urls:
 row={'url':raw}
 try:row['result']=base64.b64encode(materialize_rule(raw,config,'')).decode()
 except ValueError as e:row['error']=str(e)
 materialized.append(row)
(root/'rules.json').write_text(json.dumps(dict(rows=rows,locations=locations,contexts=contexts,source=config,materialized=materialized),ensure_ascii=False))
print(f'A11 rules: {len(rows)} conversions, {len(locations)} excerpts, {len(contexts)} contexts, {len(materialized)} callbacks')
