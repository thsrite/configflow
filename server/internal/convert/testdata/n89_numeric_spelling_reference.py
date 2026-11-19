"""Exact native YAML numeric spellings in the unchanged byte baseline.

Real Python verifies every original generation. Native callbacks now keep the
input scalar lexeme instead of round-tripping it through JSON; the Go consumer
checks both spellings retain the same float tag and numeric value.
"""
import contextlib,copy,gzip,hashlib,io,json,pathlib,sys
import yaml
root=pathlib.Path(__file__).parent
sys.path.insert(0,str(root))
with contextlib.redirect_stdout(io.StringIO()):import gen_mihomo as gm
archive=(root/'mihomo_corpus.jsonl.gz').read_bytes()
manifest=(root/'n88_generation_bytes.json').read_bytes()
old={r['row']:r['sha256'] for r in json.loads(manifest)['cases'] if r['source']=='mihomo_corpus.jsonl.gz'}
lines=gzip.decompress(archive).splitlines()
refs={}
for index in (380,582,627,634,1959,2084,2095,2617,2739,3040,3181,3792,4411,4457,5113,6339,6456,6465,7241,7416):
 line=lines[index];row=json.loads(line);args=row['args'];assert row['fn']=='generate'
 document=yaml.compose(args['profile']['mihomo']['custom_config'])
 values={k.value:v for k,v in document.value}
 fields=[(key,values[key]) for key in ('number','text') if key in values and values[key].value in ('1.0e+20','1e3')]
 assert len(fields)==1
 field,node=fields[0];original={'1.0e+20':'1e+20','1e3':'1000.0'}[node.value]
 assert float(original)==float(node.value)
 gm.set_caches(row.get('caches',{}),row.get('downloads',{}))
 generated=gm.mihomo.generate_mihomo_config(copy.deepcopy(args['profile']),args['base_url'],args['sync_lan_hosts'],preflight_providers=False,region_probe=args['region_probe'],report={},mihomo_core_version=args['core_version'],for_agent=args['for_agent'])
 assert generated==row['out'],('real Python generation drift',index)
 refs[str(index)]={'row_sha256':hashlib.sha256(line).hexdigest(),'old_byte_sha256':old[index],'field':field,'old':original,'native':node.value,'python_output_sha256':hashlib.sha256(generated.encode()).hexdigest()}
result={'archive_sha256':hashlib.sha256(archive).hexdigest(),'manifest_sha256':hashlib.sha256(manifest).hexdigest(),'rows':refs}
(root/'n89_numeric_spellings.json').write_text(json.dumps(result,indent=2)+'\n')
print('NUMERIC SPELLING REFERENCE: 20 exact real Python cases; immutable original byte hashes')
