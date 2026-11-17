"""N7-3a test oracle. Only yaml.load/safe_load are replaced; business code is real.
collect records actual loader inputs; replay reads the separately recorded native
fixture before calling the same Python entries. No Go business result is an input.
"""
import copy
import datetime
import gzip
import hashlib
import json
import math
import sys
from pathlib import Path
from unittest.mock import patch
import yaml
from backend.utils import subscription_parser as parser
from backend.utils import subscription_hosts as hosts
from backend.utils import subscription_dialers as dialers
from backend.utils import sub_store_client as store
from backend.utils import dialer_references as refs
from backend.converters import mihomo

ROOT = Path(__file__).resolve().parents[4]
FUNCTIONS = {
 'parse_uri_list': parser.parse_uri_list,
 'parse_yaml_object': parser.parse_yaml_object,
 'extract_subscription_hosts': hosts.extract_subscription_hosts,
 'extract_subscription_dialers': dialers.extract_subscription_dialers,
 'parse_structured_proxy_string': mihomo._parse_structured_proxy_string,
 'parse_proxy_string': mihomo._parse_structured_proxy_string,
 'validate_dialers': refs.validate_dialers,
 'is_yaml_response': store._is_yaml_response,
 'parse_proxies_from_yaml': store.parse_proxies_from_yaml,
 'proxies_to_nodes': lambda source: store.proxies_to_nodes(store.parse_proxies_from_yaml(source)),
}

def freeze(value):
 if isinstance(value,float) and not math.isfinite(value): return {"__nonfinite__":repr(value)}
 if isinstance(value, bytes): return {'__bytes__': value.hex()}
 if isinstance(value, (datetime.datetime, datetime.date)): return {'__date__': str(value)}
 if isinstance(value, (list, tuple)): return [freeze(x) for x in value]
 if isinstance(value, set): return {'__set__': sorted([freeze(x) for x in value], key=repr)}
 if isinstance(value, dict): return {json_key(k): freeze(v) for k,v in value.items()}
 return value

def json_key(value):
 if isinstance(value,str): return value
 if value is None: return 'null'
 if isinstance(value,bool): return 'true' if value else 'false'
 if isinstance(value,(int,float)): return str(value)
 raise TypeError('non-JSON key: '+type(value).__name__)

def same(left,right):
 if isinstance(left,bool) or isinstance(right,bool):return type(left) is type(right) and left==right
 if isinstance(left,dict) and isinstance(right,dict):return left.keys()==right.keys() and all(same(left[key],right[key]) for key in left)
 if isinstance(left,list) and isinstance(right,list):return len(left)==len(right) and all(same(a,b) for a,b in zip(left,right))
 return left==right

def call(case):
 try: return {'value':freeze(FUNCTIONS[case['fn']](copy.deepcopy(case['input'])))}
 except Exception as error: return {'error':type(error).__name__, 'message':str(error)}

def rows(path):
 if str(path).endswith('.gz'):
  with gzip.open(path,'rt') as stream: return [json.loads(line) for line in stream]
 return json.loads(path.read_text())

def cases():
 result=[]
 for name in ['parser','hosts','dialers','review_c10']:
  for i,row in enumerate(rows(ROOT/'server/internal/subs/testdata'/f'{name}.jsonl.gz')):
   if row['fn'] in FUNCTIONS: result.append({'id':f'subs/{name}/{i}','fn':row['fn'],'input':row['in']})
 for i,row in enumerate(rows(ROOT/'server/internal/convert/testdata/corpus.jsonl.gz')):
  if row['fn']=='parse_structured_proxy_string': result.append({'id':f'subs/drift/{i}','fn':row['fn'],'input':row['args'][0]})
 for i,row in enumerate(rows(ROOT/'server/internal/repo/testdata/compat_golden.json')['cases']):
  if row['fn'] in ('parse_proxy_string','validate_dialers'):result.append({'id':f'subs/golden/{i}','fn':row['fn'],'input':row['input']})
 for i,row in enumerate(rows(ROOT/'server/internal/substore/testdata/corpus.json')):
  if row['fn'] in ('is_yaml_response','parse_proxies_from_yaml','proxies_to_nodes'):result.append({'id':f'substore/corpus/{i}','fn':row['fn'],'input':row['input']})
 data=rows(ROOT/'server/internal/substore/testdata/review_reference.json')
 for fn in ('is_yaml_response','parse_proxies_from_yaml','proxies_to_nodes'):
  for i,row in enumerate(data[fn]):result.append({'id':f'substore/review/{fn}/{i}','fn':fn,'input':row['input']})
 return result

def caller_mode(case):
 if case['fn']=='extract_subscription_hosts': return 'hosts'
 if case['fn']=='extract_subscription_dialers': return 'dialers'
 if case['fn']=='is_yaml_response': return 'detection'
 if case['fn'] in ('parse_proxies_from_yaml','proxies_to_nodes'): return 'substore'
 return 'parser'

def source_id(text, mode):return hashlib.sha256((mode+'\0'+text).encode()).hexdigest()

def write(path,value):
 payload=json.dumps(value,ensure_ascii=True,separators=(',',':'),sort_keys=True).encode()
 path=Path(path)
 if str(path).endswith('.gz'):
  with gzip.GzipFile(filename=str(path),mode='wb',mtime=0) as stream:stream.write(payload)
 else:path.write_bytes(payload)

if sys.argv[1]=='collect':
 sources={}
 original_load=yaml.load
 original_safe=yaml.safe_load
 current=[]
 def note(text):
  key=source_id(text,mode);sources[key]={"mode":mode,"source":text}
  if key not in current:current.append(key)
 def load(text,*args,**kwargs):note(text);return original_load(text,*args,**kwargs)
 def safe(text,*args,**kwargs):note(text);return original_safe(text,*args,**kwargs)
 output=[]
 with patch.object(yaml,'load',load),patch.object(yaml,'safe_load',safe):
  for case in cases():
   current=[];mode=caller_mode(case);case['old']=call(case);case['old_loads']=current[:];output.append(case)
 core=[]
 for name in ('corpus','review'):
  for i,line in enumerate((ROOT/'server/internal/yamlx/testdata'/f'{name}.jsonl').read_text().splitlines()):
   row=json.loads(line)
   if name=='corpus' and row.get('kind')!='load':continue
   text=row['text'];mode='core-'+name;key=source_id(text,mode)
   sources[key]={'mode':mode,'source':text};core.append({'id':f'core/{name}/{i}','source_id':key})
 write(sys.argv[2],{'cases':output,'sources':sources,'core_rows':core})
 print('recorded',len(output),'business cases and',len(sources),'unique actual loader inputs')
else:
 data=json.loads(Path(sys.argv[2]).read_text());native=json.loads(Path(sys.argv[3]).read_text())
 current=[]
 def value(wire,key=False):
  kind=wire['kind']
  if kind=='scalar':return wire['value']
  if kind=='tagged-key':return ('__yaml_key__',wire['tag'],wire['value'])
  if kind=='sequence':
   result=[value(x) for x in wire['items']]
   return tuple(result) if key else result
  if kind=='mapping':
   result={value(pair[0],True):value(pair[1]) for pair in wire['pairs']}
   return tuple(result.items()) if key else result
  raise ValueError('unknown native fixture kind')
 def load(text,*args,**kwargs):
  key=source_id(text,mode)
  if key not in current:current.append(key)
  if key not in native:raise AssertionError('unrecorded native input '+key)
  record=native[key]
  if record.get('error'):raise yaml.YAMLError(record['error'])
  return value(record['native'])
 output={};stats={}
 with patch.object(yaml,'load',load),patch.object(yaml,'safe_load',load):
  for case in data['cases']:
   current=[];mode=caller_mode(case);expected=call(case)
   classes=sorted({category for key in set(case['old_loads']+current) for category in native[key]['classes'] if category!='equal'})
   changed=not same(case['old'],expected)
   if changed and not classes:raise AssertionError('unclassified expected change '+case['id'])
   output[case['id']]={'expected':expected,'old':case['old'],'changed':changed,'classes':classes,'old_loads':case['old_loads'],'native_loads':current[:]}
   domain=case['id'].split('/')[0];row=stats.setdefault(domain,{'rows':0,'changed':0,'classified':0})
   row['rows']+=1
   if changed:row['changed']+=1;row['classified']+=bool(classes)
 write(sys.argv[4],{'cases':output,'stats':stats,'core_rows':data.get('core_rows',[])})
 print(json.dumps(stats,sort_keys=True))
