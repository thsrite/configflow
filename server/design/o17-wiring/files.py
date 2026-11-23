import os,re,sys
root='server/internal'  # run from the repository root
for pair in sys.argv[1:]:
    p,t=pair.split(':')
    d=os.path.join(root,p); hits=[]
    for f in sorted(os.listdir(d)):
        if not f.endswith('.go') or f.endswith('_test.go'): continue
        src=open(os.path.join(d,f)).read()
        if re.search(r'"configflow/internal/%s"'%re.escape(t),src):
            alias=t.split('/')[-1]
            m=re.search(r'(\w+)\s+"configflow/internal/%s"'%re.escape(t),src)
            if m: alias=m.group(1)
            n=len(re.findall(r'\b%s\.'%alias,src))
            hits.append(f'{f}({n})')
    print(f'{p} -> {t}: {" ".join(hits)}')
