"""Independent R5/typed-validation provider expectations. Original archive stays
read-only. Run on .32 with the reference venv and PYTHONPATH=.
Only stored-group read views and explicit malformed-main validation change;
normal business answers come from the real retired Python owner.
"""
import copy, gzip, hashlib, json
from pathlib import Path
from unittest import mock
from backend.utils import provider_delivery as pd
from backend.utils.dialer_references import DialerReferenceError
from backend.agents import deployment_locations

ROOT=Path(__file__).parent
archive=ROOT/'corpus.json.gz'
rows=json.loads(gzip.decompress(archive.read_bytes()))

def outcome(call):
    try: return {'output':call()}
    except Exception as error:
        # Native input/decode errors replace retired internal language exceptions.
        if isinstance(error,NativeInput):return {'error':'*provider.InputError'}
        result={'error':type(error).__name__}
        if isinstance(error,DialerReferenceError):result['message']=str(error)
        return result
class NativeInput(Exception):pass

def shape():raise DialerReferenceError('拨号拓扑字段类型无效')
def native_main(main,chains,validate=False):
    if chains and not validate:return
    if not isinstance(main,dict):raise NativeInput()
    proxies=main.get('proxies',[])
    if validate or not chains:
        if not isinstance(proxies,list):shape()
        if any(not isinstance(proxy,dict) for proxy in proxies):
            if validate:shape()
            raise NativeInput()
    if validate:return
    if chains or any(proxy.get('dialer-proxy') is not None for proxy in proxies):return
    definitions=main.get('proxy-providers',{})
    if not isinstance(definitions,dict):raise NativeInput()
    for definition in definitions.values():
        if not isinstance(definition,dict):raise NativeInput()
        override=definition.get('override',{})
        if not isinstance(override,dict):raise NativeInput()
        target=override.get('dialer-proxy')
        if target is not None and not isinstance(target,str):shape()
        if target:return

def snapshot(row):
    source=copy.deepcopy(row['input']);main=source['main'];profile=source['profile'] or {}
    groups=profile.get('proxy_groups',[])
    if not isinstance(groups,list) or any(not isinstance(group,dict) for group in groups):groups=[]
    for group in groups:
        if isinstance(group.get('name'),(int,float)) and not isinstance(group.get('name'),bool):group['name']=str(group['name'])
        if group.get('type')=='chain' and group.get('enabled',True):
            if not isinstance(group.get('name'),str) or not group['name']:raise NativeInput()
    profile['proxy_groups']=groups
    captured=pd.DeliverySnapshot.capture('default',main,profile=profile)
    out=[{'chain_names':sorted(captured.chain_names)}]
    def has_chains():
        native_main(main,bool(captured.chain_names))
        return captured.has_chains
    out.append(outcome(has_chains))
    for proxies in source['payloads']:
        for force in (False,True):
            for name in (None,'Feed','Other','Nope','Alias'):
                def validate():
                    pd.validate_proxy_shapes(proxies)
                    if set(p['name'] for p in proxies).intersection(captured.chain_names):
                        raise DialerReferenceError('Provider 节点名称与代理链出口冲突')
                    # Native Validate performs the activation check before root COW.
                    native_main(main,bool(captured.chain_names))
                    native_main(main,bool(captured.chain_names),True)
                    try:captured.validate(copy.deepcopy(proxies),force_chains=force,provider_name=name)
                    except (AttributeError,TypeError,KeyError):
                        # Malformed definitions with an active chain are native
                        # convert-topology validation, already approved by 8a.
                        if captured.chain_names:shape()
                        raise NativeInput()
                out.append(outcome(validate))
    return out

references={}
for index,row in enumerate(rows):
    if row['kind']!='snapshot':continue
    result=outcome(lambda:snapshot(row))
    original={key:row[key] for key in ('output','error','message') if key in row}
    if result!=original:
        references[str(index)]={'input':row['input'],'original':original,'expected':result,
          'reason':'R5 stored group read view / native typed malformed-main validation'}
for index,expected_class in {1138:'KeyError',1139:'KeyError',1574:'StopIteration',1580:'ValueError'}.items():
    row=rows[index]
    assert row.get('error')==expected_class and 'output' not in row and 'message' not in row
    references[str(index)]={'input':row['input'],'original':{'error':expected_class},
      'expected':{'error':'*url.Error' if index==1580 else '*provider.InputError'},
      'reason':'native typed input/URL error; rejection and HTTP status unchanged'}
manifest={'archive_sha256':hashlib.sha256(archive.read_bytes()).hexdigest(),
 'recorder_sha256':hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
 'python_owner_sha256':hashlib.sha256(Path('backend/utils/provider_delivery.py').read_bytes()).hexdigest(),
 'rows':references}
(ROOT/'n88d_native_reference.json.gz').write_bytes(gzip.compress((json.dumps(manifest,ensure_ascii=False,indent=2)+'\n').encode(),mtime=0))
print('independent provider snapshot rows:',len(references))
