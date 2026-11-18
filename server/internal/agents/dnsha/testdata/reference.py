"""Real Python boundary oracle. Inputs and reports are synthetic only."""
import copy
import json
from backend.agents import oxidns_ha as ha
from backend.agents import oxidns_ha_publish as pub

A = dict(id='primary', name='DNS primary', host='192.0.2.20', port=8082,
         service_type='oxidns', profile_id='default', enabled=True, deployment_method='shell')
B = dict(A, id='secondary', name='DNS secondary', host='192.0.2.21')
BODY = dict(peer_id='secondary', vip='192.0.2.53')
rows = []
def add(name, kind, **values):
    row = dict(name=name, kind=kind, **copy.deepcopy(values))
    try:
        if kind == 'normalize':
            row['want'] = ha.normalize(values['body'], values['a'], values['b'], values['agents'])
        elif kind == 'environment':
            ha.validate_pair_environments(values['members'], values['caps'])
            row['want'] = True
        elif kind == 'health':
            row['want'] = ha._health(values['members'], values['state'])
        elif kind == 'listener':
            from backend.agents.deployment_bundle import build_deployment_bundle
            bundle = build_deployment_bundle(A, {'profile_id':'default'}, values['config'], deployment_id='candidate', local_dns_watchdog=False)
            pub._require_listener(bundle)
            row['want'] = True
    except Exception as error:
        row['error'] = str(error)
    rows.append(row)
def norm(name, body=BODY, a=A, b=B, agents=None):
    add(name, 'normalize', body=body, a=a, b=b, agents=agents or [a,b])
norm('normal')
norm('default-vrid-zero', dict(BODY, vip='192.0.2.0'))
norm('default-vrid-max', dict(BODY, vip='192.0.2.255'))
norm('explicit-vrid', dict(BODY, vrid=200))
for name,body in [('null',None),('non-object',[]),('unknown-field',dict(BODY,extra=1)),
                  ('peer-type',dict(BODY,peer_id=3)),('missing-peer',{'vip':'192.0.2.53'}),
                  ('missing-vip',{'peer_id':'secondary'}),('vip-loopback',dict(BODY,vip='127.0.0.1')),
                  ('vip-same-host',dict(BODY,vip=A['host'])),('vrid-bool',dict(BODY,vrid=True)),
                  ('vrid-float',dict(BODY,vrid=2.0)),('vrid-range',dict(BODY,vrid=256))]:
    norm(name,body)
norm('wrong-service',a=dict(A,service_type='mihomo'))
norm('peer-gone',b=None)
norm('same-agent',dict(BODY,peer_id='primary'),b=A)
norm('disabled',a=dict(A,enabled=False))
norm('profile',b=dict(B,profile_id='another'))
norm('host-loopback',b=dict(B,host='127.0.0.1'))
norm('same-host',b=dict(B,host=A['host']))
norm('vip-used',agents=[A,B,dict(B,id='third',host=BODY['vip'])])
hostid = ['a'*64,'b'*64]
bootid = ['c'*64,'d'*64]
base = [dict(oxidns_ha_protocols=[1],oxidns_ha_container_protocols=[1],
    oxidns_ha_environment=dict(kind='container',ready=True,network='host',host_id=hostid[i],
      host_id_source='docker-volume',host_boot_id=bootid[i])) for i in range(2)]
for name,edit in [('native',None),('docker',lambda caps:None),
                 ('not-ready',lambda caps:caps[0]['oxidns_ha_environment'].update(ready=False,blockers=['needs mount'])),
                 ('network',lambda caps:caps[0]['oxidns_ha_environment'].update(network='bridge')),
                 ('protocol',lambda caps:caps[0].update(oxidns_ha_container_protocols=[])),
                 ('source',lambda caps:caps[0]['oxidns_ha_environment'].update(host_id_source='invalid')),
                 ('boot-missing',lambda caps:caps[0]['oxidns_ha_environment'].pop('host_boot_id')),
                 ('same-boot',lambda caps:caps[1]['oxidns_ha_environment'].update(host_boot_id=bootid[0])),
                 ('host-missing',lambda caps:caps[0]['oxidns_ha_environment'].pop('host_id')),
                 ('same-host-id',lambda caps:caps[1]['oxidns_ha_environment'].update(host_id=hostid[0]))]:
    caps = copy.deepcopy(base if edit else [{},{}])
    if edit: edit(caps)
    add(name,'environment',members=[A,B],caps=caps)
for holds,running,healthy,state in [(1,2,True,'succeeded'),(2,2,True,'succeeded'),(0,2,True,'succeeded'),
                                  (0,0,False,'succeeded'),(1,2,False,'succeeded'),(1,2,True,'failed')]:
    members=[dict(runtime=dict(vip_present=i<holds,keepalived_running=i<running,healthy=healthy),error='') for i in range(2)]
    add(f'health-{holds}-{running}-{healthy}-{state}','health',members=members,state={'status':state})
for listen in ['0.0.0.0:53','[::]:53',':53','*:53','\u00a00.0.0.0:53\u3000','192.0.2.20:53','0.0.0.0:5353']:
    config = 'plugins:\n  - type: udp_server\n    args:\n      listen: '+json.dumps(listen)+'\n'
    add('listener-'+listen,'listener',config=config)
print(json.dumps(rows,ensure_ascii=False))
