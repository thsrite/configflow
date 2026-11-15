"""Review references for A05 (traffic_history.py, traffic_store.py): edge
inputs the porter's corpus (gen_traffic_reference.py) did not cover.
Synthetic values only.

    rt --back server/internal/agents/metrics/testdata/a05_reference.json -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/agents/metrics/testdata/gen_a05_reference.py > server/internal/agents/metrics/testdata/a05_reference.json'
"""
import json
import os
import sqlite3
import sys
import tempfile
from datetime import datetime

sys.path.insert(0, os.getcwd())

from backend.agents import traffic_history as th  # noqa: E402
from backend.agents import traffic_store as ts  # noqa: E402

NOW = 1_793_500_000.25
HOUR, DAY = th.HOUR, th.DAY
THIS = int(NOW) // HOUR * HOUR


def err(exc):
    return {'error': type(exc).__name__, 'message': str(exc)}


# ---------- _target_key ----------

TARGETS = [
    # IPv6 zones: anything but '%' after the first '%', case kept
    'fe80::1%eth 0', 'fe80::1%A B', 'FE80::1%\t', 'fe80::1%é', 'fe80::1%İ', 'fe80::1%1/2', 'fe80::1%a%b',
    'fe80::1% ', '::1%x', '::ffff:1.2.3.4%Zone', '1.2.3.4%eth0', 'fe80::1 %eth0', ' fe80::1%eth0',
    '::ffff:01.2.3.4', '::1.2.3.4', '1::2:3:4:5:6:7', '1:2:3:4:5:6:7::', '::1:2:3:4:5:6:7', '1:2:3:4:5:6:7:8:9',
    '0:0:0:0:0:0:0:0', '00000::', '0001:2::', '1:0:0:2:0:0:0:3', '1:0:0:0:2:0:0:3', '::ffff:0:0', '::ffff:ffff:ffff',
    '64:ff9b::1.2.3.4', '::ffff:1.2.3', '1.2.3.4\n', '1.2.3.04', '0.0.0.0', '255.255.255.255', '1.2.3.٤',
    '１.2.3.4', '1.2.3.4/32', 'FFFF::', 'Fe80::A:b',
    # domains: case, IDN, full-width, CJK, emoji, dots, controls
    'WWW.EXAMPLE.COM', 'www.例子.公司', '例子.中国', 'a.中国', 'xn--55qx5d.cn',
    '\U0001f600.example.com', 'a.\U0001f600', 'İ.com', 'ı.com', 'K.com', 'K.com', 'STRASSE.de',
    'straße.de', 'ＡＢ.com', 'a．b．example．com', 'a｡example｡com', 'a。example。com',
    'a.b.example.com\x00', 'a\tb.example.com', 'a b.example.com', 'example.com:80', 'example.com:abc', '[2001:db8::1]:443',
    'user:pw@a.example.com', 'a.example.com/', 'a.example.com?x=1', 'a.example.com#f', '//a.example.com', 'HTTPS://A.EXAMPLE.COM',
    'mailto:a@b.example.com', 'a.b.c.kawasaki.jp', 'city.kawasaki.jp', 'www.ck', 'a.www.ck', 'a.b.bd', 'b.bd',
    'example.local', 'example.internal', 'example.onion', 'example', 'co.uk', 'a.co.uk', '.co.uk', 'example.co.uk.',
    'a..', '...', '.example.com', 'a.-b.com', 'xn--', 'xn--a.com', 'XN--FIQS8S', 'a.XN--FIQS8S', 'test.المغرب',
    'Σ.gr', 'AΣ.gr', 'AΣB.gr', 'ẞ.de', 'Ω.com', 'Å.no', 'ǅ.com', 'a' * 63 + '.com',
    '1.2.3.4.example.com', '192.168.0.1.nip.io', '10.0.0.1:8080',
]


# ---------- lower ----------

def lower_map():
    out = {}
    for cp in range(0x110000):
        if 0xD800 <= cp <= 0xDFFF:
            continue
        c = chr(cp)
        if c.lower() != c:
            out[format(cp, 'x')] = c.lower()
    return out


def ranges(predicate):
    out, start = [], None
    for cp in range(0x110001):
        ok = cp < 0x110000 and not 0xD800 <= cp <= 0xDFFF and predicate(chr(cp))
        if ok and start is None:
            start = cp
        elif not ok and start is not None:
            out.append([start, cp - 1])
            start = None
    return out


# Final sigma context: ('AΣ' + c).lower() ends in σ when c is cased and not
# case-ignorable; ('A' + c + 'Σ').lower() ends in ς when c is case-ignorable
# or cased.
def sigma_context():
    return {
        'cased_not_ignorable': ranges(lambda c: ('A\u03a3' + c).lower()[1] == '\u03c3'),
        'ignorable_or_cased': ranges(lambda c: ('A' + c + '\u03a3').lower()[-1] == '\u03c2'),
    }


# ---------- int64 overflow in SQLite (REAL counters) ----------

def table(conn, name):
    return [list(row) for row in conn.execute(
        f'SELECT agent_id, bucket, span, source, {"target, " if name == "traffic_targets" else ""}chain, '
        f'up, typeof(up), down, conns FROM {name} ORDER BY 1, 2, 3, 4, 5')]


def call(fn, *args, **kwargs):
    try:
        return fn(*args, **kwargs)
    except Exception as exc:  # noqa: BLE001
        return err(exc)


def overflow_scenario():
    out = {}
    big = 2 ** 62 - 1
    with tempfile.TemporaryDirectory() as tmp:
        h = th.TrafficHistory(tmp)
        conn = lambda: sqlite3.connect(os.path.join(tmp, th.FILENAME))  # noqa: E731
        usage = [(THIS, 's', 'G\x1fN', big, 1, 1), (THIS - HOUR, 's2', 'H', 5, 6, 7)]
        targets = [(THIS, 's', 'example.com', 'G\x1fN', big, 1, 1), (THIS, 's', 'example.org', 'G\x1fN', 3, 3, 1),
                   (THIS, 's2', 'example.com', 'H', 2, 2, 1)]
        h.add_report('a', {'usage': usage, 'targets': targets})
        h.add_report('a', {'usage': usage[:1], 'targets': targets[:1]})
        out['flush1'] = call(h.flush, now=NOW)
        h.add_report('a', {'usage': usage[:1], 'targets': targets[:1]})
        out['flush2'] = call(h.flush, now=NOW)
        out['tables2'] = {name: table(conn(), name) for name in ('traffic_usage', 'traffic_targets')}
        # pending ints on top of stored REAL values
        h.add_report('a', {'usage': [(THIS, 's', 'G\x1fN', 10, 0, 0), (THIS, 's', 'X', big, big, 0)],
                           'targets': [(THIS, 's', 'example.com', 'G\x1fN', 10, 0, 0)]})
        for by in th.AGGREGATES:
            out[f'query/{by}'] = call(h.query, 'a', '24h', by, now=NOW)
        out['flush3'] = call(h.flush, now=NOW + 3 * DAY)
        out['tables3'] = {name: table(conn(), name) for name in ('traffic_usage', 'traffic_targets')}
        out['query/30d/destination'] = call(h.query, 'a', '30d', 'destination', now=NOW + 3 * DAY)
        out['flush4'] = call(h.flush, now=NOW + 9 * DAY)
        out['tables4'] = {name: table(conn(), name) for name in ('traffic_usage', 'traffic_targets')}
        out['query/30d/source'] = call(h.query, 'a', '30d', 'source', now=NOW + 9 * DAY)
    # SUM() integer overflow while compacting; pending big ints beyond int64
    with tempfile.TemporaryDirectory() as tmp:
        h = th.TrafficHistory(tmp)
        rows = [(THIS, 's', 'G', big, 0, 0), (THIS - HOUR, 's', 'G', big, 0, 0)]
        for _ in range(3):
            h.add_report('a', {'usage': rows, 'targets': []})
        out['pending-overflow'] = call(h.flush, now=NOW)
        out['pending-after'] = h.pending_count()
        h._usage.clear()
        for _ in range(2):
            h.add_report('a', {'usage': rows, 'targets': []})
        out['flush-max'] = call(h.flush, now=NOW)
        out['sum-overflow'] = call(h.flush, now=NOW + 9 * DAY)
        out['sum-overflow-tables'] = table(sqlite3.connect(os.path.join(tmp, th.FILENAME)), 'traffic_usage')
    return out


# ---------- compaction / pruning / query boundaries ----------

def boundary_scenario():
    out = {}
    now = 1_793_491_200 + 0.75  # just after a UTC midnight
    day0 = int(now) // DAY * DAY
    with tempfile.TemporaryDirectory() as tmp:
        h = th.TrafficHistory(tmp)
        usage, targets = [], []
        for offset in (-7 * DAY - HOUR, -7 * DAY, -7 * DAY + HOUR, -2 * DAY - HOUR, -2 * DAY, -30 * DAY - HOUR, -30 * DAY,
                       -31 * DAY, -29 * DAY - 23 * HOUR, -HOUR, 0):
            usage.append((day0 + offset, 's', 'G\x1fN', 1, 2, 1))
            targets.append((day0 + offset, 's', 'example.com', 'G\x1fN', 1, 2, 1))
        conn = lambda: sqlite3.connect(os.path.join(tmp, th.FILENAME))  # noqa: E731
        h.add_report('a', {'usage': usage, 'targets': targets})
        out['flush'] = call(h.flush, retention=30, now=now)
        out['tables'] = {name: table(conn(), name) for name in ('traffic_usage', 'traffic_targets')}
        for range_key in th.RANGES:
            for when in (now, day0 + 0.0, day0 - 0.5):
                q = call(h.query, 'a', range_key, 'destination', now=when)
                out[f'query/{range_key}/{when!r}'] = q
        out['flush-later'] = call(h.flush, retention=30, now=now + DAY)
        out['tables-later'] = {name: table(conn(), name) for name in ('traffic_usage', 'traffic_targets')}
    # clean_report hour boundaries with an integral now
    whole = float(THIS)
    report = {'schema': 2, 'usage': [
        {'hour': h_, 'source': 's', 'chain': ['G'], 'up': 1, 'down': 1, 'conns': 1}
        for h_ in (THIS - 2 * DAY, THIS - 2 * DAY - HOUR, THIS + HOUR, THIS + 2 * HOUR)], 'domains': []}
    out['clean-integral-now'] = [list(r) for r in th.clean_report(report, now=whole)['usage']]
    out['clean-integral-now-minus'] = [list(r) for r in th.clean_report(report, now=whole - 1e-6)['usage']]
    return out


# ---------- traffic store: hand-edited files ----------

STORE_REPORT = {'schema': 1, 'status': 'running', 'mihomo_version': '', 'items': [
    {'host': 'a.example.com', 'port': 443, 'outlet': 'proxy', 'policy': 'P', 'rule': 'MATCH', 'conns': 1, 'fails': 1,
     'up': 5, 'down': 6, 'fail_kinds': {'dns': 1}}]}
EMPTY_REPORT = {'schema': 1, 'status': 'unreachable'}
TODAY = '2026-11-15'
STORE_FILES = {
    'hosts-list-no-items': ({'status': None, 'days': {TODAY: {'ip_only_conns': 0, 'dropped': 0, 'hosts': []}}}, EMPTY_REPORT),
    'hosts-str-no-items': ({'status': None, 'days': {TODAY: {'ip_only_conns': 0, 'dropped': 0, 'hosts': 'abc'}}}, EMPTY_REPORT),
    'hosts-int-no-items': ({'status': None, 'days': {TODAY: {'ip_only_conns': 0, 'dropped': 0, 'hosts': 5}}}, EMPTY_REPORT),
    'hosts-none-no-items': ({'status': None, 'days': {TODAY: {'ip_only_conns': 0, 'dropped': 0, 'hosts': None}}}, EMPTY_REPORT),
    'hosts-list-items': ({'status': None, 'days': {TODAY: {'ip_only_conns': 0, 'dropped': 0, 'hosts': []}}}, STORE_REPORT),
    'port-float': ({'status': None, 'days': {TODAY: {'ip_only_conns': 0, 'dropped': 0, 'hosts': {'a.example.com': {
        'first_seen': 'x', 'last_seen': 'x', 'by_route': {}, 'ports': {'443': 1e16}}}}}}, STORE_REPORT),
    'port-bool': ({'status': None, 'days': {TODAY: {'ip_only_conns': 0, 'dropped': 0, 'hosts': {'a.example.com': {
        'first_seen': 'x', 'last_seen': 'x', 'by_route': {}, 'ports': {'443': True}}}}}}, STORE_REPORT),
    'counter-float': ({'status': None, 'days': {TODAY: {'ip_only_conns': 1.5, 'dropped': True, 'hosts': {}}}}, STORE_REPORT),
    'counter-str': ({'status': None, 'days': {TODAY: {'ip_only_conns': 'x', 'dropped': 0, 'hosts': {}}}}, STORE_REPORT),
    'status-str': ({'status': 'old', 'days': {}}, EMPTY_REPORT),
    'status-list-version': ({'status': ['x'], 'days': {}}, STORE_REPORT | {'mihomo_version': 'v1'}),
    'status-version-int': ({'status': {'mihomo_version': 7}, 'days': {}}, EMPTY_REPORT),
    'day-not-dict': ({'status': None, 'days': {TODAY: []}}, EMPTY_REPORT),
    'day-missing-key': ({'status': None, 'days': {TODAY: {'dropped': 0, 'hosts': {}}}}, EMPTY_REPORT),
    'entry-list': ({'status': None, 'days': {TODAY: {'ip_only_conns': 0, 'dropped': 0, 'hosts': {'a.example.com': []}}}}, STORE_REPORT),
    'by-route-missing': ({'status': None, 'days': {TODAY: {'ip_only_conns': 0, 'dropped': 0, 'hosts': {'a.example.com': {
        'first_seen': 'x'}}}}}, STORE_REPORT),
    'fail-kinds-float': ({'status': None, 'days': {TODAY: {'ip_only_conns': 0, 'dropped': 0, 'hosts': {'a.example.com': {
        'first_seen': 'x', 'last_seen': 'x', 'by_route': {'MATCH\t\tproxy\tP': {
            'conns': 1e16, 'fails': 0, 'fail_kinds': {'dns': 0.5}, 'zero_dl': 0, 'up': 0, 'down': 0}}}}}}}, STORE_REPORT),
    'old-days-and-odd-keys': ({'status': None, 'days': {'2026-11-08': {}, '2026-11-09': {}, 'zzz': {}, '': {}, '2026-11-9': {}}},
                              EMPTY_REPORT),
    'not-dict-file': ([1, 2], EMPTY_REPORT),
    'days-not-dict': ({'status': {'value': 'x'}, 'days': []}, EMPTY_REPORT),
}


def store_edits():
    out = {}
    now = datetime(2026, 11, 15, 12, 30, 45, 999999)
    for name, (content, report) in STORE_FILES.items():
        with tempfile.TemporaryDirectory() as tmp:
            store = ts.TrafficStore(tmp)
            text = json.dumps(content)
            with open(os.path.join(tmp, 'a.json'), 'w', encoding='utf-8') as handle:
                handle.write(text)
            cleaned = ts.clean_report(report)
            result = call(store.add_report, 'a', cleaned, now=now)
            with open(os.path.join(tmp, 'a.json'), encoding='utf-8') as handle:
                stored = handle.read()
            out[name] = {'file': text, 'report': report, 'result': result, 'stored': stored}
    return out


# a heavy day: more than MAX_HOSTS_PER_DAY with float / equal weights
def store_cap():
    out = {}
    now = datetime(2026, 11, 15, 8, 0, 0)
    old = ts.MAX_HOSTS_PER_DAY
    ts.MAX_HOSTS_PER_DAY = 3
    try:
        hosts = {}
        for i, weight in enumerate([1, 2.5, 2, 2.5, 0, 3]):
            hosts[f'h{i}.example.com'] = {'first_seen': 'x', 'last_seen': 'x', 'by_route': {
                'r\t\tproxy\tP': {'conns': weight, 'fails': 0, 'fail_kinds': {}, 'zero_dl': 0, 'up': 0, 'down': 0}}}
        content = {'status': None, 'days': {'2026-11-15': {'ip_only_conns': 0, 'dropped': 0, 'hosts': hosts}}}
        with tempfile.TemporaryDirectory() as tmp:
            store = ts.TrafficStore(tmp)
            with open(os.path.join(tmp, 'a.json'), 'w', encoding='utf-8') as handle:
                handle.write(json.dumps(content))
            out['result'] = call(store.add_report, 'a', ts.clean_report(EMPTY_REPORT), now=now)
            with open(os.path.join(tmp, 'a.json'), encoding='utf-8') as handle:
                out['stored'] = handle.read()
        out['file'] = json.dumps(content)
    finally:
        ts.MAX_HOSTS_PER_DAY = old
    return out


HOSTS = ['a.com', 'A.com', 'ä.com', 'xn--4ca.com', 'İ.com', 'K.com', 'straße.de', 'ａ.com', '\U0001f600.com',
         '例子.com', 'a.com.', '.a.com', 'a..com', '-a.com', 'a_b.com', '1.com', '123', '1.2.3.4', '::1', 'a' * 63 + '.com',
         'a' * 64 + '.com', ('a' * 63 + '.') * 3 + 'a' * 61, ('a' * 63 + '.') * 3 + 'a' * 62, 'localhost', 'a.b', 'a.1', 'a.c0m',
         'xn--fiqs8s', 'a.xn--fiqs8s', 'aı.com', 'ı.com', 'k.com']


def store_hosts():
    out = {}
    for host in HOSTS:
        report = {'schema': 1, 'status': 'running', 'items': [{'host': host, 'outlet': 'direct', 'policy': 'p'}]}
        out[host] = len(ts.clean_report(report)['items']) == 1
    return out


def rollback_text():
    conn = sqlite3.connect(':memory:', isolation_level=None)
    try:
        conn.execute('ROLLBACK')
    except sqlite3.Error as exc:
        return err(exc)
    return None


print(json.dumps({
    'now': NOW,
    'target_keys': {value: th._target_key(value) for value in TARGETS},
    'lower_map': lower_map(),
    'sigma_context': sigma_context(),
    'overflow': overflow_scenario(),
    'boundaries': boundary_scenario(),
    'store_edits': store_edits(),
    'store_cap': store_cap(),
    'store_hosts': store_hosts(),
    'rollback_without_transaction': rollback_text(),
}, ensure_ascii=False, sort_keys=True))
