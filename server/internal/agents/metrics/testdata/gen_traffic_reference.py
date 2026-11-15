"""Reference values of backend/agents/traffic_history.py and traffic_store.py
for the Go port (A05). Synthetic values only.

    rt -- bash -lc '~/cf-test/.venv/bin/python -I server/internal/agents/metrics/testdata/gen_traffic_reference.py > server/internal/agents/metrics/testdata/traffic_reference.json'

(run from the worktree root; -I needs the explicit sys.path below.)
"""
import json
import os
import sys
import tempfile
from datetime import datetime, timezone

sys.path.insert(0, os.getcwd())

from backend.agents import traffic_history as th  # noqa: E402
from backend.agents import traffic_store as ts  # noqa: E402

NOW = 1_793_500_000.25
HOUR = th.HOUR
THIS = int(NOW) // HOUR * HOUR

TARGETS = ["", "1.2.3.4", "01.2.3.4", "::FFFF:1.2.3.4", "::ffff:102:304", "fe80::1%eth0", "FE80::1%Eth0", "fe80::1%",
           "2001:DB8:0:0:1:0:0:1", "2001:db8::", "::", "Example.COM.", "a.b.Example.co.uk", "İstanbul.com", "ΣΑΣ.gr",
           "www.例子.中国", "xn--fsqu00a.xn--fiqs8s", "localhost", "com", "a..b.com", "a/b.example.com",
           "http://x.example.org/path", "user@host.example.net", "x.example.com:443", "1.2.3", "256.1.1.1", "[::1]",
           "a.github.io", "s3.amazonaws.com", "foo.blogspot.com", "１２３.com", "a.b.c.d.e.f.g.example.com", "..", ".",
           "a.", "a.b.c.example.co.jp", "x.platform.sh", "EXAMPLE.ORG...", "a。b。example。com", "1.2.3.4.", " 1.2.3.4",
           "::1 ", "x" * 253, "ß.example.de", "café.example.fr"]


def chain(*names):
    return list(names)


def u(**kw):
    item = {'hour': THIS, 'source': '10.0.0.2', 'chain': chain('G', 'N'), 'up': 1, 'down': 2, 'conns': 1}
    item.update(kw)
    return item


def d(**kw):
    item = {'hour': THIS, 'source': '10.0.0.2', 'domain': 'www.example.com', 'chain': chain('G', 'N'), 'up': 1, 'down': 2, 'conns': 1}
    item.update(kw)
    return item


HISTORY_REPORTS = {
    'ok': {'schema': 2, 'usage': [u()], 'domains': [d()]},
    'schema-true': {'schema': True, 'usage': [{'hour': THIS, 'source': 's', 'group': 'G', 'outbound': 'N', 'up': 1, 'down': 1, 'conns': 1}], 'domains': []},
    'schema-1.0': {'schema': 1.0, 'usage': [], 'domains': []},
    'schema-2.0': {'schema': 2.0, 'usage': [u()], 'domains': []},
    'schema-str': {'schema': '2', 'usage': [], 'domains': []},
    'usage-missing': {'schema': 2, 'domains': []},
    'not-dict': [1],
    'hour-float': {'schema': 2, 'usage': [u(hour=float(THIS))], 'domains': []},
    'hour-bool': {'schema': 2, 'usage': [u(hour=True)], 'domains': []},
    'hour-huge': {'schema': 2, 'usage': [u(hour=3600 * 10 ** 30), u()], 'domains': []},
    'hour-negative-stale': {'schema': 2, 'usage': [u(hour=-3600)], 'domains': []},
    'hour-next': {'schema': 2, 'usage': [u(hour=THIS + HOUR)], 'domains': []},
    'hour-after-next': {'schema': 2, 'usage': [u(hour=THIS + 2 * HOUR)], 'domains': []},
    'count-2**62': {'schema': 2, 'usage': [u(up=2 ** 62)], 'domains': []},
    'count-2**62-1': {'schema': 2, 'usage': [u(up=2 ** 62 - 1)], 'domains': []},
    'count-float': {'schema': 2, 'usage': [u(up=1.0)], 'domains': []},
    'count-bool': {'schema': 2, 'usage': [u(conns=False)], 'domains': []},
    'source-64': {'schema': 2, 'usage': [u(source='界' * 64)], 'domains': []},
    'source-65': {'schema': 2, 'usage': [u(source='界' * 65)], 'domains': []},
    'source-none': {'schema': 2, 'usage': [u(source=None)], 'domains': []},
    'unattributed-chain': {'schema': 2, 'usage': [u(source='', chain=['a', 'b'])], 'domains': []},
    'domain-unattributed': {'schema': 2, 'usage': [], 'domains': [d(source='')]},
    'domain-254': {'schema': 2, 'usage': [], 'domains': [d(domain='a' * 254)]},
    'domain-ip6': {'schema': 2, 'usage': [], 'domains': [d(domain='2001:DB8::1')]},
    'chain-empty-list': {'schema': 2, 'usage': [u(chain=[])], 'domains': []},
    'chain-8': {'schema': 2, 'usage': [u(chain=['x'] * 8)], 'domains': []},
    'chain-128': {'schema': 2, 'usage': [u(chain=['界' * 128])], 'domains': []},
    'chain-129': {'schema': 2, 'usage': [u(chain=['界' * 129])], 'domains': []},
    'chain-number': {'schema': 2, 'usage': [u(chain=[1])], 'domains': []},
    'legacy-same': {'schema': 1, 'usage': [{'hour': THIS, 'source': 's', 'group': 'X', 'outbound': 'X', 'up': 1, 'down': 1, 'conns': 1}], 'domains': []},
    'legacy-empty-group': {'schema': 1, 'usage': [{'hour': THIS, 'source': 's', 'group': '', 'outbound': 'X', 'up': 1, 'down': 1, 'conns': 1}], 'domains': []},
    'legacy-empty-outbound': {'schema': 1, 'usage': [{'hour': THIS, 'source': 's', 'group': 'G', 'outbound': '', 'up': 1, 'down': 1, 'conns': 1}], 'domains': []},
    'legacy-no-group': {'schema': 1, 'usage': [{'hour': THIS, 'source': 's', 'outbound': 'X', 'up': 1, 'down': 1, 'conns': 1}], 'domains': []},
    'legacy-domain-group-ignored': {'schema': 1, 'usage': [], 'domains': [{'hour': THIS, 'source': 's', 'domain': 'a.b.example.com', 'group': 5, 'outbound': 'X', 'up': 1, 'down': 1, 'conns': 1}]},
    'too-many': {'schema': 2, 'usage': [u()] * 10001, 'domains': [d()] * 10000},
    'exactly-max': {'schema': 2, 'usage': [u(hour=0)] * 10000, 'domains': [d(hour=0)] * 10000},
    'item-not-dict': {'schema': 2, 'usage': [u(), 5], 'domains': []},
}

STORE_REPORTS = {
    'ok': {'schema': 1, 'status': 'running', 'items': []},
    'schema-true': {'schema': True, 'status': 'running'},
    'schema-1.0': {'schema': 1.0, 'status': 'running'},
    'status-list': {'schema': 1, 'status': ['running']},
    'status-dict': {'schema': 1, 'status': {}},
    'status-number': {'schema': 1, 'status': 1},
    'version-65': {'schema': 1, 'status': 'running', 'mihomo_version': 'v' * 65},
    'version-ctrl': {'schema': 1, 'status': 'running', 'mihomo_version': 'v\x7f'},
    'version-none': {'schema': 1, 'status': 'running', 'mihomo_version': None},
    'dropped-2**53': {'schema': 1, 'status': 'running', 'dropped': 2 ** 53, 'items': [{'bad': 1}]},
    'dropped-2**53+1': {'schema': 1, 'status': 'running', 'dropped': 2 ** 53 + 1},
    'items-none': {'schema': 1, 'status': 'running', 'items': None},
    'outlet-list': {'schema': 1, 'status': 'running', 'items': [{'host': 'a.com', 'outlet': [], 'policy': 'p'}]},
    'outlet-number': {'schema': 1, 'status': 'running', 'items': [{'host': 'a.com', 'outlet': 1, 'policy': 'p'}]},
    'bad-before-unhashable': {'schema': 1, 'status': 'running', 'items': [5, {'host': 'a.com', 'outlet': {}, 'policy': 'p'}]},
    'items-mixed': {'schema': 1, 'status': 'auth_failed', 'items': [
        {'host': 'a.com', 'outlet': 'proxy', 'policy': 'p'},
        {'host': 'a.com\n', 'outlet': 'proxy', 'policy': 'p'},
        {'host': 'xn--fsqu00a.xn--fiqs8s', 'outlet': 'reject', 'policy': 'p', 'network': 'udp', 'port': 65535},
        {'host': 'a.com', 'outlet': 'proxy', 'policy': 'p', 'port': 65536},
        {'host': 'a.com', 'outlet': 'proxy', 'policy': 'p', 'port': True},
        {'host': 'a.com', 'outlet': 'proxy', 'policy': 'p', 'network': 'TCP'},
        {'host': 'a.com', 'outlet': 'proxy', 'policy': ''},
        {'host': 'a.com', 'outlet': 'proxy', 'policy': 'p', 'rule': None, 'rule_payload': None},
        {'host': 'a.com', 'outlet': 'proxy', 'policy': 'p', 'fail_kinds': {'dns': 2 ** 53}},
        {'host': 'a.com', 'outlet': 'proxy', 'policy': 'p', 'fail_kinds': {'dns': 2 ** 53 + 1}},
        {'host': 'a.com', 'outlet': 'proxy', 'policy': 'p', 'fail_kinds': []},
        {'host': '3com.com', 'outlet': 'direct', 'policy': '策略' * 64},
        {'host': '1.2.3', 'outlet': 'direct', 'policy': 'p'},
        {'host': '_srv.a.com', 'outlet': 'direct', 'policy': 'p'},
        {'host': 'a-.com', 'outlet': 'direct', 'policy': 'p'},
        {'host': '::1', 'outlet': 'direct', 'policy': 'p'},
    ]},
}


def call(fn, *args):
    try:
        return fn(*args)
    except Exception as exc:  # noqa: BLE001
        return {'error': f'{type(exc).__name__}: {exc}'}


def tuples(value):
    if value is None:
        return None
    return {key: [list(row) for row in rows] for key, rows in value.items()}


def iso(ts):
    return datetime.fromtimestamp(ts, timezone.utc).isoformat()


# Query scenario: three Agents, reports at several hours, a partial flush,
# then every range and aggregate.
SCENARIO = []
for offset, source, layers, up, down in [
    (0, '10.0.0.2', ['A', 'B', 'C'], 5, 50), (0, '10.0.0.3', ['A', 'C'], 7, 7), (-1, '10.0.0.2', ['D'], 1, 100),
    (-30, '10.0.0.2', ['A', 'B', 'C'], 2, 20), (-200, '10.0.0.4', ['E', 'F'], 3, 30), (-24, '', [], 9, 9),
    (-23, '10.0.0.2', ['A', 'B', 'C'], 1, 1), (-170, '10.0.0.3', ['X'], 4, 4), (-700, '10.0.0.5', ['A'], 6, 60),
]:
    SCENARIO.append(('usage', THIS + offset * HOUR, source, '\x1f'.join(layers), up, down, 1))
for offset, source, target, layers, up, down in [
    (0, '10.0.0.2', 'example.com', ['A', 'B', 'C'], 5, 50), (0, '10.0.0.2', '*', ['A', 'B', 'C'], 1, 1),
    (-1, '10.0.0.3', 'example.org', ['A', 'C'], 7, 7), (-30, '10.0.0.2', '203.0.113.9', ['D'], 1, 100),
    (-50, '10.0.0.2', 'example.com', ['D'], 2, 2), (-100, '10.0.0.4', 'b.example', ['E', 'F'], 3, 30),
]:
    SCENARIO.append(('targets', THIS + offset * HOUR, source, target, '\x1f'.join(layers), up, down, 1))


def run_scenario():
    out = {}
    with tempfile.TemporaryDirectory() as tmp:
        history = th.TrafficHistory(tmp)
        first = {'usage': [row[1:] for row in SCENARIO[:5] if row[0] == 'usage'], 'targets': [row[1:] for row in SCENARIO if row[0] == 'targets'][:3]}
        history.add_report('a1', first)
        history.add_report('a2', {'usage': [SCENARIO[0][1:]], 'targets': []})
        out['flush1'] = history.flush(retention=30, now=NOW + 3 * 86400)
        rest = {'usage': [row[1:] for row in SCENARIO[5:] if row[0] == 'usage'], 'targets': [row[1:] for row in SCENARIO if row[0] == 'targets'][3:]}
        history.add_report('a1', rest)
        for range_key in th.RANGES:
            for by in th.AGGREGATES:
                out[f'{range_key}/{by}'] = history.query('a1', range_key, by, now=NOW + 3 * 86400)
        out['flush2'] = history.flush(retention=30, now=NOW + 3 * 86400)
        out['after/30d/destination'] = history.query('a1', '30d', 'destination', now=NOW + 3 * 86400)
        out['after/7d/source'] = history.query('a1', '7d', 'source', now=NOW + 3 * 86400 + 0.5)
    return out


print(json.dumps({
    'now': NOW,
    'scenario': [list(row) for row in SCENARIO],
    'target_keys': {value: th._target_key(value) for value in TARGETS},
    'history_reports': {name: {'input': report, 'output': tuples(th.clean_report(report, now=NOW))}
                        for name, report in HISTORY_REPORTS.items() if name not in ('too-many', 'exactly-max')},
    'history_sizes': {name: (None if (r := th.clean_report(report, now=NOW)) is None else [len(r['usage']), len(r['targets'])])
                      for name, report in HISTORY_REPORTS.items() if name in ('too-many', 'exactly-max')},
    'store_reports': {name: {'input': report, 'output': call(ts.clean_report, report)} for name, report in STORE_REPORTS.items()},
    'iso': {repr(ts_): iso(ts_) for ts_ in [0, 1.5, 1793500000.0000005, 1793500000.0000015, 1793500000.9999996, 1793500000.123456789,
                                            1793413600.25, -1.25, 1e9 + 0.0000005]},
    'floor_div': {repr(v): [v // 86400, v // 3600] for v in [1793500000.25, 1793491200.0, 0.5, -0.5, 1e15 + 0.3]},
    'scenario_results': run_scenario(),
}, ensure_ascii=False, sort_keys=True))
