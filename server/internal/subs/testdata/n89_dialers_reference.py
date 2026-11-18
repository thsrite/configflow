"""Real source-dialer business on the approved native record consumption view.

The independently audited N7 YAML value archive supplies source decoding. Only
Node.Name numeric text, list/reference decoding and native array errors differ;
airport group type whitelist/default-select compatibility stays unchanged.
"""
import contextlib,copy,gzip,hashlib,io,json,pathlib
from unittest import mock
with contextlib.redirect_stdout(io.StringIO()):
 from backend.utils import subscription_dialers as dialers
root=pathlib.Path(__file__).parent
native_bytes=(root/'n7_native_values.json.gz').read_bytes()
native=json.loads(gzip.decompress(native_bytes))
previous=json.loads(gzip.decompress((root/'n7_reader_expected.json.gz').read_bytes()))['cases']
class NativeArrayError(Exception):pass
class RequiredArray:
 def __init__(self,field):self.field=field
 def __iter__(self):raise NativeArrayError('订阅 '+self.field+' 必须是列表')
def unpack(wire,key=False):
 kind=wire['kind']
 if kind=='scalar':return wire['value']
 if kind=='tagged-key':return ('__yaml_key__',wire['tag'],wire['value'])
 if kind=='sequence':
  values=[unpack(x) for x in wire['items']]
  return tuple(values) if key else values
 pairs={unpack(k,True):unpack(v) for k,v in wire['pairs']}
 return tuple(pairs.items()) if key else pairs
def numeric_text(value):
 if isinstance(value,(int,float)) and not isinstance(value,bool):return str(value)
 return value
unregistered=set()
def load(source):
 key=hashlib.sha256(('dialers\0'+source).encode()).hexdigest()
 if key not in native:
  unregistered.add(key);raise AssertionError('unregistered native YAML '+key)
 record=native[key]
 if record.get('error'):raise dialers.yaml.YAMLError(record['error'])
 document=copy.deepcopy(unpack(record['native']))
 if not isinstance(document,dict):return document
 for field in ('proxies','proxy-groups'):
  items=document.get(field)
  if items is None:items=[]
  if not isinstance(items,list):raise NativeArrayError('订阅 '+field+' 必须是列表')
  document[field]=items
 for proxy in document['proxies']:
  if isinstance(proxy,dict) and proxy.get('name') is not None:proxy['name']=numeric_text(proxy['name'])
 for group in document['proxy-groups']:
  if not isinstance(group,dict):continue
  if group.get('proxies') is None:group['proxies']=[]
  elif not isinstance(group['proxies'],list):
   name=group.get('name','')
   group['proxies']=RequiredArray('proxy-groups['+json.dumps(name,ensure_ascii=False,separators=(',',':'))+'].proxies')
  else:group['proxies']=[numeric_text(value) for value in group['proxies']]
 return document
cases={};sources={}
for name in ('dialers','review_c10'):
 source=(root/f'{name}.jsonl.gz').read_bytes();sources[name]=hashlib.sha256(source).hexdigest()
 for index,line in enumerate(gzip.decompress(source).decode().rstrip('\n').split('\n')):
  row=json.loads(line)
  if row['fn']!='extract_subscription_dialers':continue
  try:
   with mock.patch.object(dialers,'_load',load):output=dialers.extract_subscription_dialers(row['in'])
   result={'value':output}
  except NativeArrayError as error:result={'error':str(error),'class':'SubscriptionShapeError'}
  prior=previous[f'subs/{name}/{index}']['expected']
  if result!=prior:
   # Error classes from the original Python internals are not product contracts.
   if 'value' in result and 'value' in prior and result['value']==prior['value']:continue
   cases[f'{name}/{index}']={'input_sha256':hashlib.sha256(line.encode()).hexdigest(),'reason':'native typed array/reference consumption','expected':result}
assert not unregistered,unregistered
payload={'sources':sources,'native_loader_sha256':hashlib.sha256(native_bytes).hexdigest(),'cases':cases}
(root/'n89_dialers_reference.json').write_text(json.dumps(payload,ensure_ascii=False,indent=2,allow_nan=False)+'\n')
print(f'native dialers reference: {len(cases)} exact existing cases')
