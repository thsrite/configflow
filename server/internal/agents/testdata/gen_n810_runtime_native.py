"""Bind native error comparisons to unchanged rows and real sqlite3 classes."""
import hashlib,json,pathlib,sqlite3,tempfile
root=pathlib.Path(__file__).parent
raw=(root/'a02_reference.json').read_bytes()
reference=json.loads(raw)
rows=[]
for i,row in enumerate(reference['legacy_rows']):
 if not row['logs']:continue
 with tempfile.TemporaryDirectory() as directory:
  path=pathlib.Path(directory)/'runtime.sqlite3'
  if 'raw' in row:path.write_text(row['raw'])
  else:
   con=sqlite3.connect(path)
   for sql in row['sql']:con.execute(sql)
   con.commit();con.close()
  con=sqlite3.connect(path)
  try:
   con.execute('SELECT agent_id, heartbeat_at, last_heartbeat, fields FROM agent_runtime').fetchall()
  except sqlite3.Error as error:
   cls=type(error).__name__
  else:raise AssertionError('original error row no longer rejects')
  finally:con.close()
 rows.append({'index':i,'sha256':hashlib.sha256(json.dumps(row,ensure_ascii=False,separators=(',',':')).encode()).hexdigest(),'python_class':cls})
assert len(rows)==7
(root/'n810_runtime_native.json').write_text(json.dumps({'corpus_sha256':hashlib.sha256(raw).hexdigest(),'rows':rows},indent=2)+'\n')
