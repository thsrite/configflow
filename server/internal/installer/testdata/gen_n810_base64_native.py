"""Bind native RFC 4648 machine grammar to complete frozen base64 rows."""
import base64,gzip,hashlib,json,re
from pathlib import Path
root=Path(__file__).resolve().parent
archive=root/'docker_ha.jsonl.gz'
rows=[]
for line in gzip.decompress(archive.read_bytes()).splitlines():
    row=json.loads(line)
    if row.get('kind')!='b64':continue
    text=row['input']
    valid=re.fullmatch(r'(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?',text) is not None
    result=base64.b64decode(text,validate=True).hex() if valid else ''
    if valid==row['ok'] and (not valid or result==row['result']):continue
    rows.append(dict(archive=archive.name,archive_sha256=hashlib.sha256(archive.read_bytes()).hexdigest(),row_sha256=hashlib.sha256(line).hexdigest(),original=row,python={'ok':row['ok'],'result':row.get('result','')},native={'ok':valid,'result':result}))
print(json.dumps({'rows':rows},ensure_ascii=False,separators=(',',':')))
