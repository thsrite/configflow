"""Independent original inputs/messages of the existing A15 preparation rows."""
import hashlib,json,pathlib
operations=['restart','uninstall','logs','log sources','clear','validate','logging','set logging','status']
rows=[]
def add(name,missing,token,cls,message,payload=''):
 row={'name':name,'missing':missing,'token':token,'payload':payload,'python_class':cls,'python_message':message}
 digest=hashlib.sha256(json.dumps(row,ensure_ascii=False,separators=(',',':')).encode()).hexdigest()
 rows.append({'row':row,'sha256':digest})
for name in operations:add(name+' cjk token',[],'密钥','UnicodeEncodeError',"'latin-1' codec can't encode characters in position 7-8: ordinal not in range(256)")
missing=[]
for key in ['token','port','host']:
 missing=missing+[key]
 for name in operations:add(name+' without '+key,missing,'','KeyError',"'"+key+"'")
add('set logging NaN without host',missing,'','KeyError',"'host'",'non-finite NaN')
root=pathlib.Path(__file__).parent
(root/'n810_call_preparation.json').write_text(json.dumps(rows,ensure_ascii=False,indent=2)+'\n')
