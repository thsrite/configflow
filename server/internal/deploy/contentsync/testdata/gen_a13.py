"""Real Python Checker oracle over deterministic synthetic histories; run via rt."""
import copy, hashlib, json, random
from pathlib import Path
from backend.agents.content_sync import ContentSyncChecker
from backend.test_agent_content_sync import DeferredExecutor
from backend.agents.deployment_bundle import DeploymentBundle, deployment_content_fingerprint, deployment_content_digests
from backend.agents.push_diff import build_push_diff


def bundle(main, rules):
    files = {'config.yaml': (main.encode(), 'config'), 'rules/example.list': (rules.encode(), 'rule')}
    manifest = dict(protocol_version=1, deployment_id='synthetic', agent_id='agent-test', profile_id='default',
                    service_type='mihomo', config_path='config.yaml', files=[dict(path=p, role=r, size=len(b), sha256=hashlib.sha256(b).hexdigest()) for p,(b,r) in files.items()])
    return DeploymentBundle.from_files(manifest, files, '')


def run(actions, limits):
    clock=[100.0]; executor=DeferredExecutor(); calls=[]
    cfg=dict(profile_id='default',test_main='mode: rule\nrules: []\n',test_rule='DOMAIN,example.test\n')
    baseline=bundle(cfg['test_main'],cfg['test_rule'])
    agent=dict(id='agent-test',profile_id='default',service_type='mihomo',config_version='applied-config',applied_content=dict(
        fingerprint=deployment_content_fingerprint(baseline.manifest), profile_id='default',service_type='mihomo',config_version='applied-config',deployment_id='previous',config_path='config.yaml',base_url='https://config.example.test',files=deployment_content_digests(baseline.manifest)))
    checker=ContentSyncChecker(executor=executor,clock=lambda:clock[0],**limits)
    def prepare(a,c,**kw):
        calls.append(1)
        if c.get('fail'):raise ValueError('synthetic private error')
        return bundle(c['test_main'],c['test_rule'])
    def details(a,b,repository):
        lists={}
        result=build_push_diff(a,b,None,receipt=dict(manifest=baseline.manifest,bundle=baseline,comparison_config=None),lists=lists)
        return result,lists
    checker._build_detail=details
    versions={}; observations=[]
    for action in actions:
        op=action['op'];reply=None
        if op=='status':reply=checker.get_content_sync(agent,cfg,'https://config.example.test',prepare)
        elif op=='diff':reply=checker.get_push_diff(agent,cfg,'https://config.example.test',prepare)
        elif op=='sync':reply=checker.compare_now(agent,cfg,'https://config.example.test',prepare)
        elif op=='run':
            if executor.jobs:executor.run()
        elif op=='advance':clock[0]+=action['seconds']
        elif op=='invalidate':checker.invalidate()
        elif op=='config':cfg.update(copy.deepcopy(action['patch']))
        elif op=='agent':agent.update(copy.deepcopy(action['patch']))
        else:raise ValueError(op)
        visible=None
        if reply is not None:
            visible=dict(status=reply['status'],reason=reply.get('reason',''),checked=bool(reply.get('checked_at')),version=0)
            if reply.get('version'):
                versions.setdefault(reply['version'],len(versions)+1);visible['version']=versions[reply['version']]
        observations.append(dict(reply=visible,jobs=len(executor.jobs),calls=len(calls),entries=len(checker._entries),details=len(checker._details)))
    return observations

cases=[]
base=[{'op':'status'},{'op':'status'},{'op':'run'},{'op':'diff'},{'op':'advance','seconds':899},{'op':'status'},{'op':'advance','seconds':1},{'op':'status'},{'op':'run'}]
cases.append(dict(name='production TTLs',limits={},actions=base))
for seed in range(8):
    rng=random.Random(seed);actions=list(base)
    for i in range(240):
        op=rng.choice(['status','status','diff','sync','run','advance','invalidate','config','agent'])
        action=dict(op=op)
        if op=='advance':action['seconds']=rng.choice([0,1,29,30,899,900])
        elif op=='config':
            key=rng.choice(['test_rule','profile_name','created_at','nested','fail'])
            action['patch']={key: (bool(rng.randrange(2)) if key=='fail' else {'created_at':str(i)} if key=='nested' else f'DOMAIN,site-{i%3}.example.test\n' if key=='test_rule' else str(i%3))}
        elif op=='agent':action['patch']=rng.choice([{'id':f'agent-{i%7}'},{'latest_deployment':{'status':'ready'}},{'latest_deployment':{'status':'failed'}},{'config_version':'applied-config'},{'config_version':'other'}])
        actions.append(action)
    cases.append(dict(name=f'history-{seed}',limits=dict(max_entries=5,max_details=2),actions=actions))
for case in cases:case['expected']=run(case['actions'],case['limits'])
path=Path(__file__).with_name('checker.json');path.write_text(json.dumps(cases,separators=(',',':')))
print(f'A13 Python Checker: {len(cases)} histories, {sum(len(c["actions"]) for c in cases)} observations')
