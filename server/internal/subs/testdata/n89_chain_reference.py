"""Real Python chain/dialer business after the approved shared R5 read view.

No Go output is read. Frozen input matrices stay unchanged. Numeric string
fields, whole-catalog R5 fallback and fail-closed managed-reference helpers are
classified explicitly; existing ordinary business errors keep exact comparisons.
"""
import contextlib,copy,gzip,hashlib,io,json,pathlib,math
from unittest import mock
with contextlib.redirect_stdout(io.StringIO()):
 from backend.utils import dialer_references as dr
 from backend.utils import proxy_chains as chains
root=pathlib.Path(__file__).parent
class NativeReferenceError(Exception):pass
classes=set()
original_target=dr.dialer_target
original_raw=dr.raw_dialer
n7_bytes=(root/'n7_reader_expected.json.gz').read_bytes()
n7=json.loads(gzip.decompress(n7_bytes))['cases']
class ModelNode(dict):pass
def number(value):return type(value) in (int,float)
def text(value):return str(value) if number(value) else value
def ref_view(value):
 if isinstance(value,dict):
  value=copy.deepcopy(value)
  for field in ('type','id'):
   if number(value.get(field)):value[field]=text(value[field]);classes.add('ResourceRef/'+field+'/numeric-text')
 return value
def group_view(value,helper=False):
 if not isinstance(value,dict):return {}
 out=copy.deepcopy(value)
 for flag in ('include-all','include_all'):
  if flag in out and type(out[flag])is not bool:out.pop(flag);classes.add('group/'+flag+'/strict-bool')
 for field in ('id','name'):
  if number(out.get(field)):out[field]=text(out[field]);classes.add('group/'+field+'/numeric-text')
 for field in ('manual_nodes','include_groups','proxies','subscriptions','aggregations','use'):
  if isinstance(out.get(field),list):
   if helper and any(item is not None and not isinstance(item,str) and not number(item) for item in out[field]):out[field]=[];classes.add('group/'+field+'/R5-array')
   else:out[field]=[text(item) for item in out[field]]
   if out[field]!=value.get(field):classes.add('group/'+field+'/shared-string-items')
  elif helper and field in out:out[field]=[];classes.add('group/'+field+'/R5-array')
 if isinstance(out.get('proxies_order'),list):
  if helper and any(not isinstance(item,dict) for item in out['proxies_order']):out['proxies_order']=[];classes.add('group/proxies_order/R5-array')
  else:out['proxies_order']=[ref_view(item) for item in out['proxies_order']]
 elif helper and 'proxies_order' in out:out['proxies_order']=[];classes.add('group/proxies_order/R5-array')
 if isinstance(out.get('chain'),dict):
  for field in ('entry','exit'):
   if field in out['chain']:out['chain'][field]=ref_view(out['chain'][field])
 if 'follow_group' in out and out['follow_group'] is not None and not isinstance(out['follow_group'],str) and not number(out['follow_group']):out.pop('follow_group');classes.add('group/follow_group/R5-text')
 return out
def node_view(value,helper=False):
 if not isinstance(value,dict):return {}
 out=ModelNode(copy.deepcopy(value))
 out.original_ref=copy.deepcopy(value.get('dialer_ref'))
 for field in ('id','name','proxy_string'):
  if number(out.get(field)):out[field]=text(out[field]);classes.add('node/'+field+'/numeric-text')
 if helper:
  if out.get('proxy_string') is not None and not isinstance(out['proxy_string'],str):out.pop('proxy_string');classes.add('node/proxy_string/R5-text')
  if out.get('params',{}) is None:out['params']={};classes.add('node/params/null-absent')
 if 'dialer_ref' in out:
  out['dialer_ref']=ref_view(out['dialer_ref'])
  if out['dialer_ref'] is not None and not isinstance(out['dialer_ref'],dict):out['dialer_ref']=None;classes.add('dialer_ref/R5-record')
 return out
def profile_view(value):
 out=copy.deepcopy(value) if isinstance(value,dict) else {}
 for field,project in (('nodes',node_view),('proxy_groups',group_view)):
  if not isinstance(out.get(field,[]),list) or any(not isinstance(item,dict) for item in out.get(field,[])):
   out[field]=[];classes.add(field+'/R5-catalog')
  elif field in out:out[field]=[project(item) for item in out[field]]
 for field in ('subscriptions','subscription_aggregations'):
  if not isinstance(out.get(field,[]),list) or any(not isinstance(item,dict) for item in out.get(field,[])):out[field]=[];classes.add(field+'/R5-catalog')
  for record in out.get(field,[]):
   if 'id' not in record:record['id']='';classes.add(field+'/missing-id-text')
 return out
def invalid_ref_type(value):
 if value is None:return False
 if not isinstance(value,dict):return True
 return any(field in value and value[field] is not None and not isinstance(value[field],str) and not number(value[field]) for field in ('type','id'))
def stored_ref_guard(node):
 if not isinstance(node,dict):return
 value=getattr(node,'original_ref',node.get('dialer_ref'))
 if invalid_ref_type(value):
  classes.add('dialer_ref/fail-closed-type')
  raw=json.dumps(value,ensure_ascii=False,separators=(',',':'))
  raise NativeReferenceError('dialer_ref 必须是 {type: node|group, id: 非空字符串}，当前值：'+raw)
def managed_ref_guard(node):
 stored_ref_guard(node)
 value=node.get('dialer_ref')
 if value is not None and (not isinstance(value,dict) or set(value)!={'type','id'} or value.get('type') not in ('node','group') or not isinstance(value.get('id'),str) or not value['id'].strip()):
  classes.add('dialer_ref/fail-closed-shape')
  raise NativeReferenceError('dialer_ref 必须为 {type: node|group, id: 非空字符串}')
def checked_target(profile,node):managed_ref_guard(node);return original_target(profile,node)
def checked_raw(node):stored_ref_guard(node);return original_raw(node)
def call(fn,*args,**kwargs):
 try:return {'value':fn(*args,**kwargs)}
 except Exception as error:return {'error':str(error),'class':'DialerReferenceError' if isinstance(error,NativeReferenceError) else type(error).__name__}
def validate(profile,original,require_ids=True,shape_only=False):
 dr.validate_shapes(profile,require_ids=require_ids)
 if shape_only:return None
 old=original.get('nodes',[]) if isinstance(original,dict) else []
 if isinstance(old,list) and all(isinstance(item,dict) for item in old):
  for node in old:stored_ref_guard(node)
 return dr.validate_dialers(profile)
def lower(profile,original):
 validate(profile,original)
 value=chains.lower_proxy_chains(copy.deepcopy(profile))
 active=any(group.get('type')=='chain' for group in profile.get('proxy_groups',[])) or any(node.get('dialer_ref') is not None for node in profile.get('nodes',[]))
 if not active:return copy.deepcopy(original) # R2: unchanged runtime retains source bytes/values.
 return value
dr.dialer_target=checked_target
dr.raw_dialer=checked_raw
sources={};cases={}
for archive,filename in [('chains',root/'chains.jsonl.gz'),('drift',root.parent.parent/'convert/testdata/corpus.jsonl.gz')]:
 source=filename.read_bytes();sources[archive]=hashlib.sha256(source).hexdigest()
 for index,line in enumerate(gzip.decompress(source).decode().rstrip('\n').split('\n')):
  row=json.loads(line);classes.clear()
  if archive=='chains':
   profile=profile_view(row['in']);expected=copy.deepcopy(row['out'])
   for field,fn in [('validate',validate),('lower',lower)]:
    result=call(fn,profile,row['in'])
    expected[field]=result['value'] if 'value'in result else {'error':result['error'],'type':result['class']}
   if classes and expected!=row['out']:cases[f'{archive}/{index}']={'input_sha256':hashlib.sha256(line.encode()).hexdigest(),'classes':sorted(classes),'expected':expected}
   continue
  fn=row['fn'];args=row['args']
  if fn not in ('validate_dialers','validate_shapes','dependency_graph','group_members','dialer_target','resolve_dialer','raw_dialer'):continue
  if fn=='group_members':
   group=group_view(args[0],True);result=call(lambda:[list(ref) for ref in dr.group_members(group)])
  elif fn=='raw_dialer':
   node=node_view(args[0],True)
   def raw():stored_ref_guard(args[0]);return dr.raw_dialer(node)
   result=call(raw)
  else:
   profile=profile_view(args[0])
   if fn=='validate_dialers':result=call(validate,profile,args[0])
   elif fn=='validate_shapes':result=call(validate,profile,args[0],args[1],True)
   elif fn=='dependency_graph':
    def graph():
     # The shared graph builder validates shapes and chain structure; the
     # source Python builder performs those same checks itself.
     return [[list(key),[list(child) for child in children]] for key,children in dr.dependency_graph(profile).items()]
    result=call(graph)
   else:
    node=node_view(args[1],True)
    def target():managed_ref_guard(node);return dr.dialer_target(profile,node)
    def resolve():managed_ref_guard(node);return dr.resolve_dialer(profile,node)
    result=call(target if fn=='dialer_target' else resolve)
    if isinstance(result.get('value'),tuple):result['value']=list(result['value'])
  original={'value':row.get('out')} if 'err' not in row else {'error':row['err'][1],'class':row['err'][0]}
  # Existing generic engine failures remain covered by their original any-error
  # contract; all success/business-error changes get complete exact outcomes.
  both_generic='error'in original and original.get('class')!='DialerReferenceError' and 'error'in result
  if classes and result!=original and not both_generic:cases[f'{archive}/{index}']={'input_sha256':hashlib.sha256(line.encode()).hexdigest(),'classes':sorted(classes),'expected':result}

# The old private parser used a reject-keys reader and a non-finite JSON
# fallback. The canonical owner uses native scalar-key normalization and
# validates the actual type discriminator. Reuse the independently audited N7
# values; reparse only newly-YAML invalid-JSON input with real PyYAML.
def structured_expected(source,reference):
 expected=copy.deepcopy(reference.get('expected',{}).get('value'))
 if 'native-json-key-policy'in reference.get('classes',[]) and isinstance(reference.get('old',{}).get('value'),dict):expected=copy.deepcopy(reference['old']['value']);classes.add('structured/shared-scalar-key-policy')
 if isinstance(source,str) and source.lstrip().startswith('{'):
  try:json.loads(source,parse_constant=lambda value:(_ for _ in ()).throw(ValueError(value)))
  except ValueError:
   try:
    candidate=dr.yaml.safe_load(source) if hasattr(dr,'yaml') else __import__('yaml').safe_load(source)
    if isinstance(candidate,dict):expected=candidate;classes.add('structured/native-finite-JSON')
   except Exception:pass
 if isinstance(expected,dict) and not isinstance(expected.get('type'),str):expected=None;classes.add('structured/type-discriminator')
 return expected
# Structured cases in the convert corpus.
source=(root.parent.parent/'convert/testdata/corpus.jsonl.gz').read_bytes()
for index,line in enumerate(gzip.decompress(source).decode().rstrip('\n').split('\n')):
 row=json.loads(line)
 if row['fn']!='parse_structured_proxy_string':continue
 classes.clear();reference=n7['subs/drift/'+str(index)];expected=structured_expected(row['args'][0],reference)
 if classes and expected!=reference.get('expected',{}).get('value'):cases[f'drift/{index}']={'input_sha256':hashlib.sha256(line.encode()).hexdigest(),'classes':sorted(classes),'expected':{'value':expected}}
# The repository's original independent Python golden remains immutable.
source=(root.parent.parent/'repo/testdata/compat_golden.json').read_bytes();sources['golden']=hashlib.sha256(source).hexdigest()
def canonical(value):return json.dumps(value,sort_keys=True,ensure_ascii=False,separators=(',',':')).replace('\u2028',r'\u2028').replace('\u2029',r'\u2029').encode()
for index,row in enumerate(json.loads(source)['cases']):
 classes.clear();fn=row['fn']
 if fn=='validate_dialers':
  profile=profile_view(row['input']);result=call(validate,profile,row['input'])
  original={'error':row['error'],'class':row['error_class']} if row.get('raised') else {'value':row.get('ok')}
 elif fn=='parse_proxy_string':
  reference=n7['subs/golden/'+str(index)];result={'value':structured_expected(row['input'],reference)};original=reference['expected']
 else:continue
 both_generic='error'in original and original.get('class')!='DialerReferenceError' and 'error'in result
 if classes and result!=original and not both_generic:cases[f'golden/{index}']={'input_sha256':hashlib.sha256(canonical(row)).hexdigest(),'classes':sorted(classes),'expected':result}
(root/'n89_chain_reference.json').write_text(json.dumps({'sources':sources,'n7_reference_sha256':hashlib.sha256(n7_bytes).hexdigest(),'cases':cases},ensure_ascii=False,indent=2,allow_nan=False)+'\n')
print(f'native chain reference: {len(cases)} exact existing cases')
