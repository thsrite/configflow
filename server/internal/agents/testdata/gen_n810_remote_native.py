"""Bind changed comparisons to the actual unchanged remote_ref.py oracle."""
import hashlib,json,pathlib
root=pathlib.Path(__file__).parent
raw=(root/'n810_remote_python.json').read_bytes()
original=json.loads(raw)
inputs={
 'status_non_json':('not json','JSONDecodeError'),
 'uninstall_non_json':('oops','JSONDecodeError'),
 'logging_list':('[1]','AttributeError'), 'logging_null':('null','AttributeError'),
 'logging_existing_none':('{"success": true}','TypeError'),
 'logging_existing_list':('{"success": true}','TypeError'),
 'json_list':('[1]','AttributeError'), 'json_null':('null','AttributeError'),
 'ha_list':('[1]',''),
}
rows=[]
for name,(body,cls) in inputs.items():
 row=original[name]
 if 'error' in row:assert row['error'].split(': ',1)[0]==cls
 elif cls:
  try:json.loads(body)
  except Exception as error:assert type(error).__name__==cls
  else:raise AssertionError(name)
 rows.append({'name':name,'body':body,'status':200,'python_class':cls,'sha256':hashlib.sha256(json.dumps(row,ensure_ascii=False,separators=(',',':')).encode()).hexdigest()})
(root/'n810_remote_native.json').write_text(json.dumps({'corpus_sha256':hashlib.sha256(raw).hexdigest(),'rows':rows},indent=2)+'\n')
