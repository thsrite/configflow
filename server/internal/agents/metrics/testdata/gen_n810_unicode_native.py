"""Independent Unicode 17 expectations from the published UCD, never Go output."""
import hashlib,json,pathlib,sys
root=pathlib.Path(__file__).parent
ucd=pathlib.Path(sys.argv[1])
version='17.0.0'
original_bytes=(root/'a05_reference.json').read_bytes()
original=json.loads(original_bytes)
source_names=['UnicodeData.txt','SpecialCasing.txt','DerivedCoreProperties.txt']
sources={name:{'url':'https://www.unicode.org/Public/'+version+'/ucd/'+name,'sha256':hashlib.sha256((ucd/name).read_bytes()).hexdigest()} for name in source_names}
lower={};records={};all_records={}
for line in (ucd/'UnicodeData.txt').read_text().splitlines():
 parts=line.split(';');cp=int(parts[0],16);all_records[cp]=line
 if parts[13]:lower[cp]=chr(int(parts[13],16));records[cp]=line
for line in (ucd/'SpecialCasing.txt').read_text().splitlines():
 parts=line.split('#',1)[0].strip().split(';')
 if len(parts)>=5 and not parts[4].strip():
  cp=int(parts[0],16);lower[cp]=''.join(chr(int(s,16)) for s in parts[1].split());records[cp]=line
props={'Cased':set(),'Case_Ignorable':set()};evidence={}
for line in (ucd/'DerivedCoreProperties.txt').read_text().splitlines():
 content=line.split('#',1)[0].strip()
 if not content:continue
 parts=content.split(';');name=parts[1].strip()
 if name not in props:continue
 limits=parts[0].strip().split('..');lo=int(limits[0],16);hi=int(limits[-1],16)
 for cp in range(lo,hi+1):props[name].add(cp);evidence[(name,cp)]=line
old_lower={int(cp,16):value for cp,value in original['lower_map'].items()}
def ranges(name):
 out=set()
 for lo,hi in original['sigma_context'][name]:out.update(range(lo,hi+1))
 return out
old_after=ranges('cased_not_ignorable');old_before=ranges('ignorable_or_cased')
def compact(value):return json.dumps(value,ensure_ascii=False,separators=(',',':')).encode()
def bound(old,native,evidence):return {'original':old,'sha256':hashlib.sha256(compact(old)).hexdigest(),'native':native,'evidence':evidence}
rows=[]
for cp,new in sorted(lower.items()):
 old=old_lower.get(cp,chr(cp))
 if old!=new:rows.append(bound({'codepoint':f'{cp:04X}','lower':old},{'lower':new},records[cp]))
sigma=[]
for cp in sorted(props['Cased']|props['Case_Ignorable']|old_after|old_before):
 new_after=cp in props['Cased'] and cp not in props['Case_Ignorable'];new_before=cp in props['Cased'] or cp in props['Case_Ignorable']
 if (cp in old_after,cp in old_before)!=(new_after,new_before):
  lines=[evidence[(name,cp)] for name in props if (name,cp) in evidence]
  if not lines:lines=['UnicodeData: '+all_records[cp]]
  sigma.append(bound({'codepoint':f'{cp:04X}','after':cp in old_after,'before':cp in old_before},{'after':new_after,'before':new_before},lines))
output={'corpus_sha256':hashlib.sha256(original_bytes).hexdigest(),'unicode_version':version,'sources':sources,'lower':rows,'sigma':sigma}
(root/('n810_unicode'+version.split('.')[0]+'_native.json')).write_text(json.dumps(output,ensure_ascii=False,indent=2)+'\n')
print('independent Unicode '+version+' changed rows: lower=%d sigma=%d'%(len(rows),len(sigma)))
