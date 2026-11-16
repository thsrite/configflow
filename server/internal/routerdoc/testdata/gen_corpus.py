"""Reference corpus for internal/routerdoc from the real Python functions.

    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/routerdoc/testdata/gen_corpus.py \
        server/internal/routerdoc/testdata/corpus.jsonl.gz

Rows {"fn", "in", "out" | "err": [class, str]}: ipv4, normalize_mac (every code
point as a part, mutated MACs, non-str values), covers, better, public,
validate_bindings, routers_of / find_router.
"""
import gzip
import json
import random
import sys

from backend.common.client_fixed_ip import validate_bindings
from backend.routers.lan_clients import Reading, Sighting, better, ipv4, normalize_mac
from backend.routers.store import find_router, routers_of
import ipaddress

rng = random.Random(1116)
rows = []


def call(fn, value, f):
    try:
        rows.append({'fn': fn, 'in': value, 'out': f()})
    except Exception as exc:  # noqa: BLE001
        rows.append({'fn': fn, 'in': value, 'err': [type(exc).__name__, str(exc)]})


SPICE = [' ', '\t', '　', '\x85', '+', '-', '_', '0x', '１', '٣', 'ﬀ', 'ß', 'ı', 'Ａ', 'a', 'F', 'g', ':', '.', '', '\n']


def mac():
    return ':'.join(format(rng.getrandbits(8), rng.choice(['02x', '02X', 'x'])) for _ in range(6))


def mutate(text):
    for _ in range(rng.randint(0, 3)):
        i = rng.randint(0, len(text))
        roll = rng.random()
        if roll < 0.6:
            text = text[:i] + rng.choice(SPICE) + text[i:]
        else:
            text = text[:i] + text[i + 1:]
    return text


def main(path):
    for cp in range(0x110000):
        if 0xD800 <= cp <= 0xDFFF:
            continue
        c = chr(cp)
        if c.upper() != c or c.isdigit() or c.isspace() or cp < 0x80:
            call('normalize_mac', c + ':0:0:0:0:0', lambda: normalize_mac(c + ':0:0:0:0:0'))
            call('normalize_mac', '0' + c + ':0:0:0:0:0', lambda: normalize_mac('0' + c + ':0:0:0:0:0'))
    for _ in range(6000):
        text = mutate(mac() if rng.random() < 0.7 else mac().replace(':', '-'))
        call('normalize_mac', text, lambda: normalize_mac(text))
    for value in [None, 0, 1, True, False, [], {}, ['aa'], 12.5, 'AA:BB:CC:DD:EE:FF', ' aa-bb-cc-dd-ee-ff ']:
        call('normalize_mac', value, lambda: normalize_mac(value))
    v4 = lambda: '.'.join(str(rng.choice([0, 1, 10, 192, 168, 255, 256, rng.randint(0, 255)])) for _ in range(4))
    for _ in range(4000):
        text = mutate(v4())
        call('ipv4', text, lambda: ipv4(text))
        call('covers', {'ip': text, 'lan': ['192.168.0.0/16', '10.0.0.0/8'], 'complete': rng.random() < 0.8},
             None)
    for value in [None, 0, 1, True, 3232235777, '::1', '1.2.3.4/32', ' 1.2.3.4 ', '　1.2.3.4', [], {}]:
        call('ipv4', value, lambda: ipv4(value))
    for row in rows:
        if row['fn'] == 'covers':
            arg = row['in']
            reading = Reading(lan={ipaddress.IPv4Network(n) for n in arg['lan']}, complete=arg['complete'])
            row.pop('err', None)
            try:
                row['out'] = reading.covers(arg['ip'])
            except Exception as exc:  # noqa: BLE001
                row.pop('out', None)
                row['err'] = [type(exc).__name__, str(exc)]
    for text in ['', '1.2.3', '1.2.3.4.5', '1.2.3.04', '1.2.3.256', '1.2.3.x', '1.2.3.1234', '1..2.3', '1.2.3.4/8', '１.2.3.4']:
        def covers(text=text):
            return Reading(lan={ipaddress.IPv4Network('0.0.0.0/0')}).covers(text)
        call('covers', {'ip': text, 'lan': ['0.0.0.0/0'], 'complete': True}, covers)
    presences = ['online', 'recent', 'offline', 'unknown', 'bogus']
    for _ in range(400):
        def make():
            return {'ip': '10.0.0.1', 'mac': rng.choice(['', 'AA:BB:CC:DD:EE:FF']), 'hostname': rng.choice(['', 'h', '主机']),
                    'sources': rng.sample(['arp', 'lease', 'online'], rng.randint(0, 3)),
                    'presence': rng.choice(presences), 'basis': rng.choice(['', 'arp']),
                    'last_seen': rng.choice([None, 0, 30]), 'router_id': 'r',
                    'fixed_ip': rng.choice([None, {'ip': '10.0.0.1'}])}
        cur = make() if rng.random() < 0.9 else None
        cand = make()

        def run(cur=cur, cand=cand):
            def build(d):
                s = Sighting(**{**d, 'sources': set(d['sources'])})
                return s
            a = build(cur) if cur is not None else None
            b = build(cand)
            result = better(a, b)
            return {'which': 'current' if result is a else 'candidate', 'public': result.public()}
        call('better', {'current': cur, 'candidate': cand}, run)
    shapes = []
    good = {'router_id': 'r1', 'vendor': 'routeros', 'id': '*1', 'ip': '192.168.0.10', 'mac': 'AA:BB:CC:DD:EE:01',
            'comment': 'cf-client: phone'}
    shapes.append({'192.168.0.10': good})
    for key, values in {'vendor': ['ikuai', 'other', 1], 'mac': ['aa:bb:cc:dd:ee:01', 'AA-BB-CC-DD-EE-01', 'AA:BB:CC:DD:EE:1', ''],
                        'comment': ['cf-client:', 'x', 'cf-client:\n'], 'router_id': ['', None], 'id': [''],
                        'ip': ['192.168.0.11', 1], 'server': ['dhcp1', 5], 'extra': ['x'], 'gateway': ['g' * 513, 'g' * 512]}.items():
        for value in values:
            shapes.append({'192.168.0.10': {**good, key: value}})
    for key in ['router_id', 'mac', 'comment']:
        shapes.append({'192.168.0.10': {k: v for k, v in good.items() if k != key}})
    shapes += [[], 'x', None, {}, {'bad': good}, {'192.168.0.10': 'x'}, {' 192.168.0.10': {**good, 'ip': ' 192.168.0.10'}},
               {str(i): {} for i in range(2049)}]
    for value in shapes:
        def run(value=value):
            validate_bindings(value)
            return None
        call('validate_bindings', value, run)
    configs = [{}, {'routers': []}, {'routers': {'items': {}}}, {'routers': {'items': [1, None, {'id': 'a'}, {'id': 1}, {'id': 'b'}]}},
               {'routers': {}}, {'routers': {'items': [{'id': 'a', 'n': 1}, {'id': 'a', 'n': 2}]}}]
    for config in configs:
        call('routers_of', config, lambda: routers_of(config))
        for rid in ['a', 'b', '1', 'zz']:
            call('find_router', {'config': config, 'id': rid}, lambda: find_router(config, rid))
    data = ''.join(json.dumps(r, ensure_ascii=False) + '\n' for r in rows)
    with open(path, 'wb') as raw, gzip.GzipFile(fileobj=raw, mode='wb', mtime=0) as fh:
        fh.write(data.encode())
    print(len(rows), 'rows ->', path)


if __name__ == '__main__':
    main(sys.argv[1])
