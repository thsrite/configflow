import re,collections,sys
edges=collections.defaultdict(dict);files=[]
for line in open('server/design/o17-wiring/routers-filedeps.txt'):
    if line.startswith('FILE '): files.append(line.split()[1])
    elif line.startswith('EDGE'):
        m=re.match(r'EDGE (\S+) -> (\S+) : (.*)',line); edges[m[1]][m[2]]=m[3].split()
sizes={l.split()[1]:int(l.split()[0]) for l in open('server/design/o17-wiring/routers_files.txt')}
exec(open(sys.argv[1]).read())  # defines PKG (name->(layer,[files])), ALLOWED (name->set of names it may import)
where={}
for p,(lay,fs) in PKG.items():
    for f in fs:
        assert f not in where, f; where[f]=p
missing=[f for f in files if f not in where]
print("UNASSIGNED",missing)
for p,(lay,fs) in sorted(PKG.items(),key=lambda x:x[1][0]):
    print(f"PKG {p} L{lay} files={len(fs)} lines={sum(sizes.get(f,0) for f in fs)}")
bad=collections.defaultdict(lambda: collections.defaultdict(set))
pkgdeps=collections.defaultdict(set)
for a in files:
    for b,syms in edges[a].items():
        pa=where.get(a)
        for s in syms:
            pb=MOVE.get(s, where.get(b))
            if b=='messages.go' and s not in MOVE: pb=MSGPKG.get(s, pb)
            if pa==pb or pa is None or pb is None: continue
            pkgdeps[pa].add(pb)
            if pb not in ALLOWED.get(pa,set()):
                bad[(pa,pb)][s].add(a)
for (pa,pb),syms in sorted(bad.items()):
    print(f"\nVIOLATION {pa} -> {pb}  ({len(syms)} symbols)")
    for s,fr in sorted(syms.items()):
        print(f"   {s:45s} used in {' '.join(sorted(fr))}")
print()
for p in sorted(pkgdeps): print("DEPS",p,"->",sorted(pkgdeps[p]))
