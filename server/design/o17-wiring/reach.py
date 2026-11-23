import json,sys
d=json.load(open('design/o17-wiring/imports.json'))
def reach(a,b):
    seen=set();st=[a];par={a:None}
    while st:
        x=st.pop()
        if x==b:
            p=[];
            while x: p.append(x);x=par[x]
            return ' -> '.join(reversed(p))
        for y in d.get(x,[]):
            if y not in par: par[y]=x;st.append(y)
    return None
for pair in sys.argv[1:]:
    a,b=pair.split(':')
    print(f'{a} reaches {b}:', reach(a,b))
