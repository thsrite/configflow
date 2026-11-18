"""Exact approved native ASCII protocol boundaries; original DNS corpus stays immutable."""
import gzip, hashlib, json
from pathlib import Path
from backend.utils import proxy_server_dns as dns
root=Path(__file__).parent
source=(root/'dns.jsonl.gz').read_bytes()
lines=gzip.decompress(source).decode().rstrip('\n').split('\n')
cases={}
for index,native,reason in [(919,[None,'listen_unparsed'],'ASCII listener port'),(955,None,'ASCII semantic version')]:
    row=json.loads(lines[index])
    actual=dns.listener_address(row['in']) if row['fn']=='listener_address' else dns.parse_core_version(row['in'])
    assert json.loads(json.dumps(actual))==row['out']
    cases[str(index)]={'input_sha256':hashlib.sha256(lines[index].encode()).hexdigest(),'reason':reason,'python':row['out'],'out':native}
(root/'n89_dns_reference.json').write_text(json.dumps({'source_sha256':hashlib.sha256(source).hexdigest(),'cases':cases},ensure_ascii=False,indent=2)+'\n')
print('native DNS reference: 2 exact ASCII protocol cases')
