"""Independent exact native hosts block and D2 boundaries on existing inputs.

Normal outputs are never read from Go. Real Python supplies the host business
and old digest, while only the documented native line selection and JSON
identity framing are projected. Frozen corpora are not rewritten.
"""
import contextlib,gzip,hashlib,io,json,pathlib,re
from unittest import mock
with contextlib.redirect_stdout(io.StringIO()):
 from backend.utils import subscription_hosts as hosts
root=pathlib.Path(__file__).parent
cases={};sources={}
old_block=hosts._hosts_block
breaks=re.compile(r'\r\n|[\n\r\x85\u2028\u2029]')
def block(source):
 match=re.search(r"""^(?:hosts|"hosts"|'hosts')[ \t]*:""",source,re.M)
 if not match:return None
 lines=breaks.split(source[match.start():]);end=1
 while end<len(lines) and (lines[end].startswith((' ','\t','#')) or not lines[end].strip()):end+=1
 return '\n'.join(lines[:end])
 return None
for name in ('hosts','review_c10'):
 source=(root/f'{name}.jsonl.gz').read_bytes();sources[name]=hashlib.sha256(source).hexdigest()
 lines=gzip.decompress(source).decode().rstrip('\n').split('\n')
 for index,line in enumerate(lines):
  row=json.loads(line);fn=row['fn'];changed=False
  if fn=='hosts_digest':
   assert hosts.hosts_digest(row['in'])==row['out']
   entries=(row['in'] or {}).get('entries') or {}
   data=json.dumps(entries,sort_keys=True,ensure_ascii=False,separators=(',',':')).replace('\u2028',r'\u2028').replace('\u2029',r'\u2029').encode()
   expected=hashlib.sha256(data).hexdigest();reason='D2 native canonical JSON'
   changed=expected!=row['out']
  elif fn=='extract_subscription_hosts' and old_block(row['in'])!=block(row['in']):
   with mock.patch.object(hosts,'_hosts_block',block):expected=hosts.extract_subscription_hosts(row['in'])
   reason='native YAML line boundaries retain non-YAML control characters for rejection'
   changed=expected!=row['out']
  if changed:cases[f'{name}/{index}']={'input_sha256':hashlib.sha256(line.encode()).hexdigest(),'reason':reason,'output':expected}
(root/'n89_hosts_reference.json').write_text(json.dumps({'sources':sources,'cases':cases},ensure_ascii=False,indent=2)+'\n')
print(f'native hosts reference: {len(cases)} exact existing cases')
