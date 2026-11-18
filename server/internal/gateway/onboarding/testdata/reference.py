"""Synthetic A21 boundary corpus from the real Python implementation."""
import json
from copy import deepcopy
import yaml
from backend.common.config import get_default_config
from backend.common.gateway_onboarding import (
    prepare_gateway_onboarding, gateway_onboarding_state,
    assert_gateway_onboarding_ready, GatewayOnboardingError,
)
from backend.common.oxidns_editor import prepare_editor
from backend.converters.mihomo import generate_mihomo_config
from backend.converters.oxidns import generate_oxidns_config


def snapshot():
    c = get_default_config()
    c.update(profile_id='home', nodes=[{'id': 'n', 'name': 'Example', 'type': 'ss',
        'server': '192.0.2.2', 'port': 443, 'cipher': 'aes-128-gcm', 'password': 'synthetic-node-secret'}],
        subscriptions=[], subscription_aggregations=[],
        proxy_groups=[{'id': 'g', 'name': 'Proxy', 'type': 'select', 'manual_nodes': ['n']}],
        rule_configs=[{'id': 'ip', 'itemType': 'ruleset', 'library_rule_id': 'lib-ip',
          'name': 'Proxy IP', 'behavior': 'ipcidr', 'policy': 'Proxy', 'source_type': 'content',
          'content': '198.51.100.0/24', 'url': '/api/rules/Proxy%20IP'},
          {'id': 'direct', 'itemType': 'ruleset', 'library_rule_id': 'lib-direct',
           'name': 'Direct Domains', 'behavior': 'domain', 'policy': 'DIRECT', 'url': '/api/rules/Direct%20Domains'},
          {'id': 'proxy', 'itemType': 'ruleset', 'library_rule_id': 'lib-proxy',
           'name': 'Proxy Domains', 'behavior': 'domain', 'policy': 'Proxy', 'url': '/api/rules/Proxy%20Domains'},
          {'id': 'match', 'itemType': 'rule', 'rule_type': 'MATCH', 'policy': 'Proxy'}],
        rule_library=[{'id': 'lib-ip', 'name': 'Proxy IP', 'behavior': 'ipcidr', 'source_type': 'content', 'content': '198.51.100.0/24'},
          {'id': 'lib-direct', 'name': 'Direct Domains', 'behavior': 'domain', 'source_type': 'content', 'content': '+.direct.example'},
          {'id': 'lib-proxy', 'name': 'Proxy Domains', 'behavior': 'domain', 'source_type': 'content', 'content': '+.proxy.example'}])
    c['system_config'].update(server_domain='https://config.example', config_token='synthetic-sub-token', rule_proxy_token='synthetic-rule-token')
    c['mihomo']['custom_config'] = 'mixed-port: 7890\nlog-level: warning\n'
    return c


def form(**kw):
    return dict(rule_set_ids=['ip'], direct_rule_set_ids=['direct'], proxy_rule_set_ids=['proxy'], local_dns='https://192.0.2.53/dns-query', **kw)


def emit(name, c, body=None, takeover='selective'):
    row=dict(name=name, config=c, body=body, takeover=takeover)
    before=deepcopy(c)
    try:
        row['state']=gateway_onboarding_state(c, 7, 'https://panel.example', takeover=takeover)
    except Exception as e:
        row['state_error']=str(e)
    try:
        candidate, report=prepare_gateway_onboarding(c, body, 'https://panel.example', takeover=takeover)
        row.update(candidate=candidate, report=report)
        if report['ready']:
            row['mihomo']=yaml.safe_load(generate_mihomo_config(candidate, 'https://panel.example', preflight_providers=False))
            row['oxidns']=yaml.safe_load(generate_oxidns_config(candidate, 'https://panel.example'))
            try:
                row['readiness']=assert_gateway_onboarding_ready(candidate, 'https://panel.example')
            except Exception as e:
                row['readiness_error']=str(e)
    except Exception as e:
        row['error']=str(e)
    assert c==before
    print(json.dumps(row,ensure_ascii=False))


if __name__=='__main__':
    emit('template',snapshot(),form())
    for take in ('legacy','full_tun'):
        emit(take,snapshot(),form(),take)
    for default in ('forward_local','forward_remote'):
        emit('default-'+default,snapshot(),form(default_forward=default))
    for enabled in (True,False):
        emit('recorder-'+str(enabled),snapshot(),form(query_recorder=enabled))
    for api in ({'username':'leftover'}, {'listen':'0.0.0.0:9199'},
                {'ssl':{'cert':'orphan.pem'}}, {'enabled':True,'listen':'127.0.0.1:9292','username':'user','password':'synthetic-api-secret'}):
        c=snapshot();c['oxidns']['api'].update(api);emit('api-'+str(api),c,form())
    for native in ('normal','disabled','custom','foreign','dynamic','listener','proxy','remote','ttl','parked','nested-dynamic'):
        c=snapshot();c['oxidns']['fallback_dns']='https://192.0.2.54/dns-query';c['oxidns']=prepare_editor(c['oxidns'],c)
        doc=yaml.safe_load(c['oxidns']['native_config'])
        plugins={p['tag']:p for p in doc['plugins']}
        if native in ('disabled','foreign','dynamic','parked'):
            if native=='parked':
                c['oxidns']['_common_disabled']={tag:deepcopy(plugins[tag]) for tag in ('forward_fallback','fallback_dns','fallback')}
            doc['plugins']=[p for p in doc['plugins'] if p['tag'] not in ('forward_fallback','fallback_dns','fallback')]
            plugins['proxy_dns_seq']['args']=[{'exec':'goto remote_dns'}]
        if native in ('foreign','dynamic'):
            doc['plugins'].append(dict(tag='foreign',type='sequence',args=[{'exec':'$fallback' if native=='foreign' else '${BACKUP_EXEC}'}]))
        if native=='custom': doc['plugins']=[dict(tag='mine',type='sequence',args=[{'exec':'reject 0'}])]
        if native=='listener':plugins['server_udp']['args']['entry']='remote_dns'
        if native=='proxy':plugins['proxy_dns_seq']['args']=[{'exec':'reject 0'}]
        if native=='remote':plugins['remote_dns']['args'].append({'exec':'accept'})
        if native=='ttl':plugins['remote_dns']['args'].append({'exec':'ttl 120'})
        if native=='nested-dynamic':doc.setdefault('runtime',{})['future']={'tag':'${DYNAMIC_TAG}'}
        c['oxidns']['native_config']=yaml.safe_dump(doc,allow_unicode=True,sort_keys=False)
        emit('native-'+native,c,form())
        if native in ('custom','foreign','dynamic','listener','proxy','remote'):
            emit('convert-'+native,c,form(confirm_template_conversion=True))
    for change in ({'direct_rule_set_ids':['foreign']},{'direct_rule_set_ids':['ip']},{'direct_rule_set_ids':['direct'],'proxy_rule_set_ids':['direct']},
                   {'rule_set_ids':'ip'},{'rule_set_ids':['ip\nnext']},{'confirm_template_conversion':'true'}, {'unknown':True},
                   {'expected_config_revision':True},{'query_recorder':None},{'expected_config_revision':-1}, {'default_forward':None}):
        b=form();b.update(change);emit('form-'+str(change),snapshot(),b)
    # One representative per parser/policy error, plus both supported formats.
    for dns in ('https://192.0.2.53:00053/dns-query', '- addr: [broken',
                '- wrong: https://192.0.2.53/dns-query',
                'tls://192.0.2.53', '- addr: tls://192.0.2.53\n  idle_timeout: 20'):
        b=form();b['local_dns']=dns;emit('dns-'+dns,snapshot(),b)
    for kind in ('renamed','deleted','disabled','existing-ip','direct-only','no-source','missing','mrs','classical'):
        c=snapshot()
        if kind=='renamed':c['rule_library'][1]['name']='Latest Direct'
        if kind=='deleted':c['rule_library']=c['rule_library'][:1]
        if kind=='disabled':c['rule_configs'][1]['enabled']=False
        if kind=='existing-ip':c['oxidns']['direct_rulesets']=['ip']
        if kind=='direct-only':
            for r in c['rule_configs']:r['policy']='DIRECT'
        if kind=='no-source':c['nodes']=[];c['proxy_groups'][0]['manual_nodes']=[]
        if kind=='missing':c['oxidns']['direct_rulesets']=['gone']
        if kind=='mrs':c['rule_library'][1]['url']='https://rules.example/a.mrs'
        if kind=='classical':c['rule_library'][1]['behavior']='classical'
        emit('choices-'+kind,c,form())

    # Saved full-TUN selections, stale selections and editable API/recorder modes.
    marker=dict(version=1,takeover='full_tun',origin='setup',route_rule_set_ids=['ip','deleted'])
    c=snapshot();c['mihomo'].update(router_split=marker,linux_gateway=dict(enabled=False,rule_set_ids=[]))
    emit('full-tun-saved',c,form(), 'full_tun')
    for key in ('direct','foreign'):
        emit('full-tun-invalid-'+key,c,{**form(),'rule_set_ids':[key]},'full_tun')
    for enabled in (True,False):
        c=snapshot();c['oxidns']=prepare_editor(c['oxidns'],c)
        emit('native-recorder-'+str(enabled),c,form(query_recorder=enabled))
    for custom in ('api-absent','api-existing','runtime-extra','bad-syntax'):
        c=snapshot();c['oxidns']=prepare_editor(c['oxidns'],c)
        doc=yaml.safe_load(c['oxidns']['native_config'])
        if custom=='api-absent':doc.pop('api',None)
        if custom=='api-existing':doc['api']={'http':{'listen':'127.0.0.1:9292','auth':{'type':'basic','username':'operator','password':'synthetic-api-password'}}}
        if custom=='runtime-extra':doc['runtime'].update(worker_threads=9,future={'keep':False})
        c['oxidns']['native_config']='plugins: [' if custom=='bad-syntax' else yaml.safe_dump(doc,allow_unicode=True,sort_keys=False)
        c['oxidns']['additional_files']=[dict(path='notes.txt',content='synthetic notes',encoding='text')]
        emit('native-'+custom,c,form())
        if custom=='bad-syntax':emit('convert-bad-syntax',c,form(confirm_template_conversion=True))
    c=snapshot();c['oxidns']['query_recorder']['enabled']=True
    emit('preserve-recorder',c,form())
    c=snapshot();c['rule_library'].append(dict(id='lib-overlap',name='Direct IP',behavior='ipcidr',source_type='content',content='198.51.100.0/25'))
    c['rule_configs'].insert(-1,dict(id='overlap',name='Direct IP',itemType='ruleset',library_rule_id='lib-overlap',policy='DIRECT'))
    emit('direct-ip-overlap',c,form())
    c=snapshot();c['system_config']['server_domain']='http://127.0.0.1:5001';c['oxidns']['local_dns']='udp://192.0.2.53:53'
    emit('actionable-warnings',c,form())
    c=snapshot();c['mihomo']['custom_config']='dns: [broken]\n'
    emit('dns-not-mapping',c,form())
    c=snapshot();c['mihomo']['custom_config']='dns: [broken\n'
    emit('base-syntax',c,form())
    for switch in (None,False):
        c=snapshot();c['rule_configs'][1]['enabled']=switch;c['nodes'][0]['enabled']=switch
        emit('disabled-switch-'+str(switch),c,form())
    emit('large-integer-revision',snapshot(),form(expected_config_revision=10**40))
