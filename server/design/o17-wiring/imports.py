import os,re,sys,json
root=sys.argv[1]
deps={}
for d,_,fs in os.walk(os.path.join(root,'internal')):
    pkg=os.path.relpath(d,os.path.join(root,'internal'))
    for f in fs:
        if not f.endswith('.go') or f.endswith('_test.go'): continue
        src=open(os.path.join(d,f)).read()
        for m in re.finditer(r'import\s*\((.*?)\)',src,re.S):
            for q in re.findall(r'"configflow/internal/([^"]+)"',m.group(1)):
                deps.setdefault(pkg,set()).add(q)
        for q in re.findall(r'^import\s+(?:\w+\s+)?"configflow/internal/([^"]+)"',src,re.M):
            deps.setdefault(pkg,set()).add(q)
json.dump({k:sorted(v) for k,v in deps.items()},open('design/o17-wiring/imports.json','w'),indent=0)
