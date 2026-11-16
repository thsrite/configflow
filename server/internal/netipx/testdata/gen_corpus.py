"""Reference corpus for internal/netipx from CPython's ipaddress (run on the test server).

    ~/cf-test/.venv/bin/python server/internal/netipx/testdata/gen_corpus.py server/internal/netipx/testdata/corpus.jsonl.gz

Functional parity: accepted/rejected, canonical text, classification and collapse results are
compared; IPv6 scope ids ("%zone") are not chased and are left out of the inputs.

Rows:
  {"k": "addr", "in": text, "out": null | {"s", "v", "int", "priv", "glob", "loop", "mc", "unspec"}}
  {"k": "net", "in": text, "strict": bool, "out": null | {"s", "v", "p", "bcast", "priv", "glob"}}
  {"k": "v4net", "in": text, "out": null | str}   (IPv4Network(text, strict=False))
  {"k": "collapse", "in": [texts], "out": [str] | {"err": str}}
  {"k": "subnet", "a": text, "b": text, "out": bool}
  {"k": "sort", "in": [texts], "out": [str]}
"""
import gzip
import ipaddress
import json
import random
import sys

rng = random.Random(20261116)

PIECES = ['0', '1', '00', '01', '255', '256', '010', '1.2', '', ' ', '１', '٣', '+1', '-1', '0x1', 'a', 'ff', 'FFFF',
          'fffff', '::', ':', '/', '.', '1e1', '\t', '0000', '00000', 'g']


def rand_v4():
    return '.'.join(str(rng.choice([0, 1, 10, 100, 127, 169, 172, 192, 198, 203, 224, 240, 255, rng.randint(0, 255)]))
                    for _ in range(4))


def rand_v6():
    roll = rng.random()
    if roll < 0.2:
        return '::ffff:' + rand_v4()
    groups = [rng.choice(['0', '1', 'fe80', '2001', 'db8', 'ffff', 'fc00', '64', 'ff9b', '2002', '3fff', '100',
                          format(rng.getrandbits(16), 'x')]) for _ in range(8)]
    text = ':'.join(groups)
    if rng.random() < 0.5:
        a = rng.randint(0, 7)
        b = rng.randint(a, 8)
        text = ':'.join(groups[:a]) + '::' + ':'.join(groups[b:])
    return text


def mutate(text):
    roll = rng.random()
    if roll < 0.5:
        return text
    i = rng.randint(0, len(text))
    if roll < 0.8:
        return text[:i] + rng.choice(PIECES) + text[i:]
    j = rng.randint(i, len(text))
    return text[:i] + text[j:]


def rand_net(v6=None):
    v6 = rng.random() < 0.4 if v6 is None else v6
    if v6:
        return rand_v6() + '/' + str(rng.choice([0, 1, 8, 10, 16, 32, 48, 64, 96, 120, 127, 128, 129, rng.randint(0, 128)]))
    roll = rng.random()
    if roll < 0.15:
        mask = rng.choice(['255.255.255.0', '0.0.0.255', '255.0.255.0', '255.255.255.255', '0.0.0.0', '255.255.128.0'])
        return rand_v4() + '/' + mask
    return rand_v4() + '/' + str(rng.choice([0, 1, 4, 8, 10, 12, 15, 16, 24, 31, 32, 33, rng.randint(0, 32)]))


def addr_row(text):
    try:
        a = ipaddress.ip_address(text)
    except ValueError:
        return {'k': 'addr', 'in': text, 'out': None}
    return {'k': 'addr', 'in': text, 'out': {'s': str(a), 'v': a.version, 'int': str(int(a)), 'priv': a.is_private,
                                              'glob': a.is_global, 'loop': a.is_loopback, 'mc': a.is_multicast,
                                              'unspec': a.is_unspecified}}


def net_row(text, strict):
    try:
        n = ipaddress.ip_network(text, strict=strict)
    except ValueError:
        return {'k': 'net', 'in': text, 'strict': strict, 'out': None}
    return {'k': 'net', 'in': text, 'strict': strict, 'out': {'s': str(n), 'v': n.version, 'p': n.prefixlen,
                                                              'bcast': str(n.broadcast_address),
                                                              'priv': n.is_private, 'glob': n.is_global}}


def v4net_row(text):
    try:
        out = str(ipaddress.IPv4Network(text, strict=False))
    except ValueError:
        out = None
    return {'k': 'v4net', 'in': text, 'out': out}


def related_pool(v6, count):
    pool = []
    while len(pool) < count:
        if pool and rng.random() < 0.5:
            base = ipaddress.ip_network(rng.choice(pool), strict=False)
            roll = rng.random()
            if roll < 0.3 and base.prefixlen < base.max_prefixlen:
                sub = list(base.subnets(new_prefix=min(base.max_prefixlen, base.prefixlen + rng.randint(1, 3))))
                pool.append(str(rng.choice(sub)))
            elif roll < 0.5 and base.prefixlen > 0:
                pool.append(str(base.supernet()))
            elif roll < 0.8 and base.prefixlen > 0:
                sibling = int(base.network_address) ^ (1 << (base.max_prefixlen - base.prefixlen))
                pool.append(str(ipaddress.ip_network((sibling, base.prefixlen))))
            else:
                pool.append(str(base))
        else:
            text = rand_net(v6)
            try:
                pool.append(str(ipaddress.ip_network(text, strict=False)))
            except ValueError:
                continue
    return pool


def main(path):
    rows = []
    for _ in range(6000):
        text = mutate(rand_v4() if rng.random() < 0.5 else rand_v6())
        rows.append(addr_row(text))
    for t in ['0.0.0.0', '255.255.255.255', '192.0.0.9', '192.0.0.10', '192.0.0.8', '100.64.0.1', '::', '::1',
              '::ffff:127.0.0.1', '::ffff:100.64.0.1', '2001:1::1', '2001:1::3', '2001:3::1', '2001:4:112::1',
              '2001:20::1', '3fff::1', '64:ff9b:1::1', '64:ff9b::1', '1.2.3.4 ', '１.2.3.4',
              '01.2.3.4', '::1.2.3.4', '1::2::3', ':::', '1:2:3:4:5:6:7:8:9', '1:2:3:4:5:6:7::',
              '::1:2:3:4:5:6:7', 'ff02::1', '224.0.0.1', '127.255.255.255']:
        rows.append(addr_row(t))
    for _ in range(8000):
        text = mutate(rand_net())
        rows.append(net_row(text, rng.random() < 0.3))
        rows.append(v4net_row(text))
    for t in ['198.18.0.1/16', '0.0.0.0/0', '::/0', '100.64.0.0/10', '100.64.0.0/9', '192.0.0.0/24',
              '192.0.0.8/29', '192.0.0.0/29', '2001::/23', '2001::/16', '2001:20::/28', '2001::/32', '1.2.3.4/255.255.255.0', '1.2.3.4/0.0.0.255', '1.2.3.4/255.0.255.0', '1.2.3.4/ 24',
              '1.2.3.4/024', '1.2.3.4/+24', '1.2.3.4/２４', '::/128', '::ffff:0:0/96', '::ffff:1.2.3.0/120']:
        for strict in (False, True):
            rows.append(net_row(t, strict))
        rows.append(v4net_row(t))
    for _ in range(1500):
        v6 = rng.random() < 0.35
        pool = related_pool(v6, rng.randint(1, 40))
        if rng.random() < 0.03:
            pool += related_pool(not v6, 2)
        nets = [ipaddress.ip_network(t) for t in pool]
        try:
            out = [str(n) for n in ipaddress.collapse_addresses(nets)]
        except TypeError as exc:
            out = {'err': str(exc)}
        rows.append({'k': 'collapse', 'in': pool, 'out': out})
        if not isinstance(out, dict):
            same = [n for n in nets if n.version == nets[0].version]
            rows.append({'k': 'sort', 'in': [str(n) for n in same], 'out': [str(n) for n in sorted(same)]})
        for _ in range(3):
            a, b = rng.choice(nets), rng.choice(nets)
            if a.version == b.version:
                rows.append({'k': 'subnet', 'a': str(a), 'b': str(b), 'out': a.subnet_of(b)})
    # Long lists reach timsort's merges and galloping (runs of 32+ items).
    for _ in range(300):
        size = rng.randint(64, 900)
        pool = [t for t in related_pool(True, size) if ':' in t]
        if rng.random() < 0.3:
            pool.sort(key=lambda t: ipaddress.ip_network(t))
            if rng.random() < 0.5:
                pool.reverse()
            for _ in range(rng.randint(0, 20)):
                i, j = rng.randrange(len(pool)), rng.randrange(len(pool))
                pool[i], pool[j] = pool[j], pool[i]
        nets = [ipaddress.ip_network(t) for t in pool]
        rows.append({'k': 'sort', 'in': pool, 'out': [str(n) for n in sorted(nets)]})
    data = ''.join(json.dumps(r, ensure_ascii=False) + '\n' for r in rows)
    with open(path, 'wb') as raw, gzip.GzipFile(fileobj=raw, mode='wb', mtime=0) as fh:
        fh.write(data.encode())
    print(len(rows), 'rows ->', path)


if __name__ == '__main__':
    main(sys.argv[1])
