"""Real Python isolation oracle: managed/non-managed file and YAML boundaries."""
import network_guard  # noqa: F401
import base64
import copy
import json
import yaml
from backend.test_rule_sync_projection import fixture, bundle, unpack_document
from backend.converters.oxidns import generate_oxidns_config
from backend.agents.rule_sync_projection import isolate_rule_bundle, read_bundle_files

def view(value):
    return {'manifest': value.manifest, 'files': {path: {'data':base64.b64encode(data).decode(), 'role':role} for path,(data,role) in read_bundle_files(value).items()}}

def make(doc,service,release):
    extras = {'custom.bin': (b'published-private-bytes', 'custom')}
    if service == 'mihomo':
        paths = [p['path'] for p in doc.get('rule-providers',{}).values()]
    else:
        paths = [path for p in doc['plugins'] if p['tag'].startswith(('rules_domain_','rules_ip_')) for path in p.get('args',{}).get('files',[])]
    extras.update({p.removeprefix('./'): (b'example.com\n', 'rule') for p in paths})
    return bundle(doc,service,extras,release)

cases=[]
for service in ('mihomo','oxidns'):
    if service == 'mihomo':
        before={'port':7890,'proxies':[{'name':'kept','type':'direct'}],'rules':['DOMAIN,old.example.com,DIRECT','MATCH,DIRECT'],'rule-providers':{'a':{'type':'http','url':'https://example.com/a','path':'./rules/a.list'}}}
        after=copy.deepcopy(before);after['port']=8890;after['proxies']=[];after['rules'][0]='DOMAIN,new.example.com,DIRECT'
    else:
        source=fixture();before=yaml.safe_load(generate_oxidns_config(source));source['rule_configs'][1]['value']='new.example.com';after=yaml.safe_load(generate_oxidns_config(source))
    for change in ('normal','changed-plugin','changed-order','custom-sequence') if service == 'oxidns' else ('normal','no-providers'):
        proposed=copy.deepcopy(after)
        if change=='changed-plugin':
            next(p for p in proposed['plugins'] if p['tag']=='forward_local')['args']['upstreams']=[{'addr':'udp://192.0.2.99'}]
        elif change=='changed-order':
            proposed['plugins'].reverse()
        elif change=='custom-sequence':
            next(p for p in proposed['plugins'] if p['tag']=='sequence_main')['args'].append({'exec':'custom_exec'})
        elif change=='no-providers':
            proposed.pop('rule-providers')
        old=make(before,service,'old');new=make(proposed,service,'new')
        case={'name':service+'/'+change,'service':service,'base':view(old),'candidate':view(new)}
        try: case['result']=view(isolate_rule_bundle(old,new,service))
        except ValueError as e: case['error']=str(e)
        cases.append(case)
print(json.dumps(cases,ensure_ascii=False))
