"""Exact D2 layout references derived from unchanged operations and real Python."""
import hashlib
import json
from pathlib import Path
from backend.utils.rule_utils import rule_cache_filename
root=Path(__file__).parent
refs={}
for name,index in [('repo_crosscheck_ops.json',47),('repo_crosscheck_c04_ops.json',189)]:
 data=(root/name).read_bytes()
 op=json.loads(data)[index]
 assert op['op']=='create_rule_library_item'
 rule=op['rule']
 assert rule['id']=='lib3' and rule.get('source_type','url')=='url'
 old='shared/rules/'+rule_cache_filename(rule)
 preimage=json.dumps([rule['id'],rule['url'],rule.get('behavior','domain'),rule.get('format') or ''],ensure_ascii=False,separators=(',',':'))
 native='shared/rules/by-source/'+hashlib.sha256(preimage.encode()).hexdigest()+'.list'
 refs[name]={'source_sha256':hashlib.sha256(data).hexdigest(),'index':index,'old':old,'native':native}
(root/'n89_cache_paths.json').write_text(json.dumps(refs,indent=2)+'\n')
print('REPO D2 REFERENCES: 2 exact source-bound paths')
