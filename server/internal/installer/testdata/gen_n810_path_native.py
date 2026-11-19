"""Bind the approved Linux path-alias overlap refusal to its complete frozen row."""
import gzip,hashlib,json
from pathlib import Path
root=Path(__file__).resolve().parent
archive=root/'docker_ha.jsonl.gz'
rows=[]
for line in gzip.decompress(archive.read_bytes()).splitlines():
 row=json.loads(line)
 if str(row.get('id'))!='1314':continue
 request=row['input']['request']
 assert request['ha_data_dir']=='//a' and request['backup_dir']=='//a/b' and row['ok']
 rows.append(dict(archive=archive.name,archive_sha256=hashlib.sha256(archive.read_bytes()).hexdigest(),row_sha256=hashlib.sha256(line).hexdigest(),original=row,python={k:v for k,v in row.items() if k not in ('id','kind','input')},native={'message':'Agent、OxiDNS、HA 和备份目录须彼此独立，不能相同或互相包含'}))
assert len(rows)==1
print(json.dumps({'rows':rows},ensure_ascii=False,separators=(',',':')))
