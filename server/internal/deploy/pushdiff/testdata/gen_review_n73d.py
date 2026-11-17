"""Synthetic A12 lexical rule listing oracle; run only via rt."""
import gzip,json
from pathlib import Path
from backend.agents import push_diff as p
chain='payload:\n  - &l0 [entry]\n'+''.join(f'  - &l{i} [*l{i-1}, *l{i-1}]\n' for i in range(1,41))+'  - *l40\n'
cases={
 'valid-block':"# comment\npayload:\n  - 'DOMAIN,example.com'\n  - IP-CIDR,192.0.2.0/24\n  - \"other.example.com\" # retained comment\n",
 'valid-flow':"payload: ['example.com', 'other.example.com']\n",
 'malformed':"payload: [",
 'indentation':"payload:\n   - example.com\n - other.example.com\n     - 'last.example.com'\n",
 'aliases':"payload:\n  - &first example.com\n  - *first\n  - [example.com, other.example.com]\n",
 'alias-bomb':chain,
 'long-line':"payload:\n  - "+'x'*1048576+".example.com\n",
 'empty':'',
 'text':"# comment\n// comment\n  example.com  \n\nother.example.com\n",
 'empty-item':"payload:\n  -   \n  -\n  -\t\n",
 'unicode-lines':"payload:\r\n  - 中文.example.com\u2028  - other.example.com\u0085  - last.example.com\n",
 'bom':"\ufeffpayload:\n  - example.com\n",
 'after-200':"# comment\n"*200+"payload:\n  - example.com\n",
 'at-200':"# comment\n"*199+"payload:\n  - example.com\n",
}
rows=[]
for name,source in cases.items():
 data=source.encode()
 summary,details=p._rule_change(None,data,p._DetailBudget())
 rows.append(dict(name=name,source=source,entries=p.rule_entries(data),expected=[summary,details]))
path=Path(__file__).with_name('review_n73d.json.gz')
with path.open('wb') as target:
 with gzip.GzipFile(filename='',fileobj=target,mode='wb',mtime=0) as stream:
  stream.write(json.dumps(rows,ensure_ascii=False,separators=(',',':')).encode())
print('A12 lexical oracle:',len(rows),'synthetic cases; summary, complete display and entries')
