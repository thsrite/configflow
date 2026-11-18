"""Exact existing caller cases at the native subscription boundary.

The original archives are immutable. Real Python confirms each historical
outcome; only approved engine-error/type and machine-version policy change.
"""
import contextlib, copy, gzip, hashlib, io, json, pathlib
with contextlib.redirect_stdout(io.StringIO()):
 from backend.converters import surge, loon
 from backend.common import mihomo_easytier as et
root=pathlib.Path(__file__).parent
archives={};cases={}
for name in ('surge_loon_corpus','review_c12','corpus'):
 data=(root/(name+'.jsonl.gz')).read_bytes()
 archives[name]=hashlib.sha256(data).hexdigest()
 for index,line in enumerate(gzip.decompress(data).decode().rstrip('\n').split('\n')):
  row=json.loads(line)
  if name=='surge_loon_corpus':
   if row['fn']not in ('proxies_surge','proxies_loon'):continue
   # Exactly the 22 unclassified archived caller rows; the other typed
   # helper cases retain their existing reviewed index and reference.
   bases=(24090,24446,25056,25466,25544,26190,26370,26742,26978,27314,27942)
   if index not in {value for base in bases for value in (base,base+1)}:continue
   args=row['args'];bad=next((node for node in args[0] if isinstance(node,dict) and type(node.get('type'))is int),None)
   if bad is None:continue
   function=surge.convert_proxies_to_surge_text if row['fn']=='proxies_surge' else loon.convert_proxies_to_loon_text
   call=lambda:function(copy.deepcopy(args[0]))
   error_class='Go:*substore.ProxyShapeError'
   message='节点 type 必须是字符串，当前值：'+json.dumps(bad['type'],ensure_ascii=False,separators=(',',':'))
   reason='native explicit node type validation replaces CPython AttributeError'
  else:
   if index not in ({2121,2125} if name=='review_c12' else {34045,35971}):continue
   assert row['fn']=='apply_easytier' and row['args'][2]is True
   args=row['args'];document=copy.deepcopy(args[0]);call=lambda:et.apply_easytier(document,copy.deepcopy(args[1]),for_agent=args[2],core_version=args[3])
   error_class='EasyTierSettingsError'
   message='Agent 上报的内核版本无法识别：'+args[3]+'，请升级或重启 Agent'
   reason='approved nonempty unrecognized Agent version fails closed'
  try:output=call()
  except Exception as error:
   previous=[type(error).__name__,str(error)]
   assert previous==row['err'],(name,index,previous,row.get('err'))
  else:
   previous=None
   assert output==row['out'],(name,index,output,row.get('out'))
  if name!='surge_loon_corpus':assert document==row['mutated'],(name,index,document,row['mutated'])
  entry={'input_sha256':hashlib.sha256(line.encode()).hexdigest(),'python_error':previous,'native_error':[error_class,message],'reason':reason}
  if name!='surge_loon_corpus':
   entry['native_mutated']=args[0]
   entry['python_mutated']=document
   if previous is None:entry['python_output']=output
  cases[f'{name}/{index}']=entry
assert len(cases)==26,len(cases)
(root/'n89_subs_callers_reference.json').write_text(json.dumps({'sources':archives,'cases':cases},ensure_ascii=False,indent=2)+'\n')
print('native subs caller reference: 26 exact real-Python cases')
