# usage: reach2.py target:filler[:excl1,excl2]  -> does filler reach target when filler's listed files are dropped?
import os,re,sys
root='server/internal'  # run from the repository root
files={}
for d,_,fs in os.walk(root):
    pkg=os.path.relpath(d,root)
    for f in fs:
        if f.endswith('.go') and not f.endswith('_test.go'):
            src=open(os.path.join(d,f)).read()
            imps=set()
            for m in re.finditer(r'import\s*\((.*?)\)',src,re.S):
                imps|=set(re.findall(r'"configflow/internal/([^"]+)"',m.group(1)))
            imps|=set(re.findall(r'^import\s+(?:\w+\s+)?"configflow/internal/([^"]+)"',src,re.M))
            files[(pkg,f)]=imps
def deps(excl):
    d={}
    for (p,f),i in files.items():
        if (p,f) in excl: continue
        d.setdefault(p,set()).update(i)
    return d
for arg in sys.argv[1:]:
    parts=arg.split(':'); t,fl=parts[0],parts[1]
    excl=set()
    if len(parts)>2:
        for x in parts[2].split(','): excl.add((fl,x))
    d=deps(excl)
    par={fl:None}; st=[fl]; found=None
    while st:
        x=st.pop()
        if x==t: found=x;break
        for y in d.get(x,()):
            if y not in par: par[y]=x; st.append(y)
    if found:
        p=[];x=found
        while x: p.append(x); x=par[x]
        print(f'{t} <- {fl} (excl {parts[2] if len(parts)>2 else "-"}): CYCLE via', ' -> '.join(reversed(p)))
    else:
        print(f'{t} <- {fl} (excl {parts[2] if len(parts)>2 else "-"}): NO CYCLE, {t} could import {fl}')
