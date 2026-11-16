"""Review C13 reference results from the real Python modules (edge inputs the
porter's corpus misses).

Run on the test server from the repository root:
  PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/substore/testdata/gen_review.py
Writes server/internal/substore/testdata/review_reference.json (synthetic values,
no network: the fetch step, commands and clocks are fakes).
"""
import json
import os
import sys
import tempfile
import threading
import time
from unittest import mock

import requests

from backend.utils import sub_store_client as client
from backend.utils import sub_store_runtime as rt
from backend.utils.subscription_health import parse_userinfo

out = {}


def err(exc):
    return {'error': type(exc).__name__, 'message': str(exc)}


def run(call):
    try:
        value = call()
    except Exception as exc:  # noqa: BLE001
        return err(exc)
    try:
        json.dumps(value)
    except TypeError as exc:
        # json.dumps refuses the value (e.g. a date key): what the cache
        # write would raise later.
        return {'unserializable': str(exc)}
    return {'output': value}


# ---------------------------------------------------------------- parse_userinfo
headers = [
    'upload=1KB; download=2GB; total=3TB', 'total=1.5e3MB', 'expire=0', 'expire=-1', 'expire=1.7e9',
    'total=١٢٣', 'total=1e', 'total=E', 'upload=1e+5', 'total=1e+', 'total=1.', 'total=..1',
    'Upload=1', 'total=NaN', 'total=inf', 'total=1_000', 'total=0x10', 'total=1e309', 'total=' + '9' * 400,
    'total=' + '9' * 300, 'total=\xa05', 'total = 5', 'e=1', '_=1', 'total=5, total=6', 'total=1; name=caf\xe9',
    'expire=1893456000.999', 'upload=1;download=2;total=3;expire=4;extra=5', 'total=+1.5E+2', 'total=1e-5',
    'download=3.99', 'total=007', 'total: 5', 'total=5%', '=5', ' total=5 ', 'totalupload=5', 'upload=1\x00total=2',
    'TOTAL=1E2', 'expire=1e10', 'total=²', 'total​=5', 'x=1 upload =\t2',
]
out['parse_userinfo'] = [{'input': h, **run(lambda h=h: parse_userinfo(h))} for h in headers]

# ---------------------------------------------------------------- _is_yaml_response
yaml_texts = [
    'x' * 191 + 'proxies:', 'x' * 192 + 'proxies:', '　proxies: []', 'x: ' + 'y' * 250 + '\n1: a\nproxies: []\n',
    'x: ' + 'y' * 250 + '\nproxies: &a [*a]\n', '<HTML><head>', '<HTML>\nproxies: []', 'c3M6Ly9hYmNAaDoxIzE=',
    '{"x": "' + 'y' * 250 + '", "proxies": []}', 'x: ' + 'y' * 250 + '\n\tproxies: []', '---\na: 1\n---\nproxies: []',
    '---\nx: ' + 'y' * 250 + '\n---\nproxies: []', 'x: ' + 'y' * 250 + '\nproxies:\n', 'x: ' + 'y' * 250 + '\nProxies: []',
    'x: ' + 'y' * 250 + '\n? [1]\n: a\nproxies: []\n', 'x: ' + 'y' * 250 + '\nproxies: !!set {a, b}\n',
    '  <!doctype html>', '﻿<html>', 'x' * 495 + '<head>', 'x' * 496 + '<head>',
]
out['is_yaml_response'] = [{'input': t, **run(lambda t=t: client._is_yaml_response(t))} for t in yaml_texts]

# ---------------------------------------------------------------- parse_proxies_from_yaml + proxies_to_nodes
docs = [
    'proxies:\n  - 1: a\n    name: A\n', '1: a\nproxies: []\n', 'proxies:\n  - {name: A, true: x, null: y, 1.5: z}\n',
    'proxies:\n  - {name: V, type: vless, reality-opts: {1: a}}\n', 'proxies:\n  - {name: A, ws-opts: {headers: {1: x}}}\n',
    'proxies: {1: a}', 'proxies:\n  - !!binary aGk=\n', 'proxies:\n  - {1: a, "1": b, name: A}\n',
    '{"proxies": [{"name": "A", "type": "ss", "port": 1}]}', 'c3M6Ly9hYmNAaDoxIzE=',
    'base: &b {type: ss, port: 1}\nproxies:\n  - <<: *b\n    name: A\n', 'proxies:\n  - name: A\n    type: VLESS\n',
    'proxies:\n  - {name: A, type: vless, encryption: ""}\n', 'proxies:\n  - {name: A, type: vless, encryption: null}\n',
    'proxies:\n  - {name: A, type: vless, encryption: 0}\n', 'proxies:\n  - {name: A, type: vless, encryption: false}\n',
    'proxies:\n  - {name: A, type: vless, reality-opts: {short-id: x}}\n', 'proxies:\n  - {name: A, type: vless, reality-opts: []}\n',
    'proxies:\n  - {name: 1, type: ss, server: 2, port: "x"}\n', 'proxies:\n  - {name: A, type: null}\n',
    'proxies:\n  - {name: A}\n  - [1]\n', 'proxies: [0, {name: A}]', 'proxies:\n  - {name: A, port: 0x1F, x: 1_000, y: 1e3, z: .inf}\n',
    'proxies:\n  - {name: "中文 🇭🇰", type: Trojan, server: 例子.test, port: 443}\n', 'proxies:\n  - {name: A, 2026-01-01: d}\n',
    '%YAML 1.1\n---\nproxies: []\n', 'proxies:\n- {name: A}\n...\n', 'proxies: [{name: A}]\n---\nproxies: []\n',
    'proxies:\n  - {name: A, type: SS}\n  - {name: A, type: SS}\n', 'proxies:\n  - {name: A, null: ~}\n',
]


def parse_doc(d):
    proxies = client.parse_proxies_from_yaml(d)
    return proxies if isinstance(proxies, list) else {'non_list': proxies}


out['parse_proxies_from_yaml'] = [{'input': d, **run(lambda d=d: parse_doc(d))} for d in docs]
out['proxies_to_nodes'] = [{'input': d, **run(lambda d=d: client.proxies_to_nodes(client.parse_proxies_from_yaml(d)))}
                           for d in docs]

# ---------------------------------------------------------------- response.text charsets
bodies = [
    ('text/plain; charset=gbk; charset=utf-8', 'a: 中'.encode()), ('text/plain;charset="utf-8"', 'a: 中'.encode()),
    ("text/plain; CHARSET='utf-8'", 'a: 中'.encode()), ('TEXT/PLAIN', b'abc'), ('application/json; charset=latin-1', b'caf\xe9'),
    ('text/plain; charset=utf-8-sig', b'\xef\xbb\xbfa: 1'), ('text/plain; foo=bar; charset = utf-8', 'a: 中'.encode()),
    ('text/plain; charset=utf-16', b'\xff\xfea\x00'), ('text/plain; charset=UTF8', 'é'.encode()),
    ('text/plain; charset=latin1; x', b'caf\xe9'), ('text/plain; charset=nonsense', 'é'.encode()),
    ('application/yaml', b'proxies: []'), ('text/yaml', 'é'.encode()), ('text/plain; charset=\xa0utf-8\xa0', 'é'.encode()),
]


def text_of(ctype, body):
    r = requests.models.Response()
    r._content = body
    r.status_code = 200
    r.headers['Content-Type'] = ctype
    r.encoding = requests.utils.get_encoding_from_headers(r.headers)
    return r.text


out['response_text'] = [{'input': [c, list(b)], **run(lambda c=c, b=b: text_of(c, b))} for c, b in bodies]

# ---------------------------------------------------------------- runtime with odd state.json
states = [b'', b'[]', b'[1]', b'{}', b'null', b'0', b'1', b'"x"', b'""', b'true', b'false',
          b'{"state": "running", "last_used": "x"}', b'{"state": "running", "last_used": null}',
          b'{"state": "running", "last_used": 1e400}', b'{"state": "running", "last_used": ' + b'9' * 400 + b'}',
          b'{"state": "running", "last_used": true}', b'{"state": "sleeping", "last_used": "x"}', b'{"state": "running"}',
          b'{"state": "failed", "error": "e", "last_used": 0}', b'NaN', b'{"last_used": NaN, "state": "running"}',
          b'\xff', b'{"state": 5, "last_used": 0}', b'{"state": "running", "last_used": 0, "error": ["x"]}',
          b'{"state": "running", "last_used": 999.5}', b'{"state": "running", "last_used": 700}',
          b'{"state": "updating", "error": "u"}', b'{"state": "starting", "error": 7}', b'{"state": null}',
          b'{"error": "x", "state": "failed"}', b'{"a": 1, "state": "running", "last_used": 0}', b'{"state":"running","last_used":[1]}']


def runtime_case(content, action, running):
    with tempfile.TemporaryDirectory() as tmp:
        commands = []

        def command(action_name):
            commands.append(action_name)
            return 'RUNNING' if action_name == 'status' else ''

        r = rt.SubStoreRuntime(directory=tmp, idle_seconds=300)
        with open(os.path.join(tmp, 'state.json'), 'wb') as f:
            f.write(content)
        with mock.patch.object(r, '_command', command), \
                mock.patch.object(r, '_ready', lambda: running), \
                mock.patch.object(rt, 'get_mode', lambda: 'on_demand'), \
                mock.patch.object(rt.time, 'monotonic', lambda: 1000.0), \
                mock.patch('backend.utils.dependencies.is_builtin_available', lambda: True):
            if action == 'tick':
                result = run(lambda: r.tick())
            elif action == 'status':
                result = run(lambda: r.status(running))
            else:
                def use():
                    with r.use():
                        pass
                result = run(use)
        with open(os.path.join(tmp, 'state.json'), 'rb') as f:
            after = f.read().decode('latin-1')
        return {**result, 'commands': commands, 'after': after}


rows = []
for content in states:
    for action, running in (('tick', False), ('status', False), ('status', True), ('use', True)):
        rows.append({'state': content.decode('latin-1'), 'action': action, 'running': running,
                     **runtime_case(content, action, running)})
out['runtime'] = rows


# ---------------------------------------------------------------- coalescing / cooldown clock edges
class Clock:
    now = 0.0


class Blocked(Exception):
    pass


def http_error(status):
    resp = requests.models.Response()
    resp.status_code = status
    return requests.HTTPError(f'{status} Client Error', response=resp)


def scenario(steps, limit=None):
    """steps: [time, url, refresh, prefer_cached, outcome] with outcome 'ok', 'fail', '403', '429', '500'."""
    client._fetch_results.clear()
    client._fetch_failures.clear()
    log = []
    counter = {'n': 0}
    pending = {}

    def fetch(sub_id, url, target='ClashMeta'):
        counter['n'] += 1
        outcome = pending['outcome']
        log.append(['fetch', url, counter['n']])
        if outcome == 'ok':
            return client.SubscriptionResult(f'r{counter["n"]}', 'direct_yaml', None, fetched_at='t')
        if outcome == 'fail':
            raise client.SubscriptionFetchError(ValueError('s'), ValueError('d'))
        raise client.SubscriptionFetchError(None, http_error(int(outcome)))

    results = []
    with mock.patch.object(client, 'monotonic', lambda: Clock.now), \
            mock.patch.object(client, '_get_base_url', lambda: 'http://127.0.0.1:9'), \
            mock.patch.object(client, '_get_subscription_proxies_yaml', fetch), \
            mock.patch.object(client, 'FETCH_CACHE_LIMIT', limit or client.FETCH_CACHE_LIMIT):
        for at, url, refresh, prefer, outcome in steps:
            Clock.now = at
            pending['outcome'] = outcome
            before = counter['n']
            try:
                value = client.get_subscription_proxies_yaml('s', url, refresh=refresh, prefer_cached=prefer)
                got = value[0]
            except client.SubscriptionFetchError as exc:
                status = getattr(getattr(exc.direct_error, 'response', None), 'status_code', None)
                got = f'error {status}'
            results.append({'fetched': counter['n'] != before, 'got': got,
                            'results': len(client._fetch_results), 'failures': len(client._fetch_failures)})
    return results


U = 'https://sub.test/a'
scenarios = {
    'normal reuse edge': [[0, U, False, False, 'ok'], [299.999, U, False, False, 'ok'], [300, U, False, False, 'ok'],
                          [599.999, U, False, False, 'ok'], [600, U, False, False, 'ok']],
    'refresh edge': [[0, U, False, False, 'ok'], [29.999, U, True, False, 'ok'], [30, U, True, False, 'ok'],
                     [59.5, U, False, False, 'ok'], [60, U, True, False, 'ok']],
    'background edge': [[0, U, False, False, 'ok'], [899.999, U, False, True, 'ok'], [900, U, False, True, 'ok'],
                        [1000, U, True, True, 'ok'], [1029.999, U, True, True, 'ok']],
    'retention cleanup': [[0, U, False, False, 'ok'], [899.999, U, False, True, 'ok'], [900, U + '2', False, False, 'ok'],
                          [900, U, False, True, 'ok']],
    'failure doubling': [[0, U, False, False, 'fail'], [2.999, U, True, False, 'ok'], [3, U, False, False, 'fail'],
                         [8.999, U, False, True, 'ok'], [9, U, False, False, 'fail'], [21, U, False, False, 'fail'],
                         [45, U, False, False, 'fail'], [93, U, False, False, 'fail'], [189, U, False, False, 'fail'],
                         [381, U, False, False, 'fail'], [680.999, U, False, False, 'ok'], [681, U, False, False, 'fail'],
                         [981, U, False, False, 'ok'], [981.5, U, False, False, 'fail']],
    'blocked cooldown': [[0, U, False, False, '403'], [59.999, U, True, False, 'ok'], [60, U, False, False, '429'],
                         [119.999, U, False, False, 'ok'], [120, U, False, False, '500'], [123, U, False, False, '403'],
                         [182.999, U, False, False, 'ok'], [183, U, False, False, 'ok']],
    'success resets failures': [[0, U, False, False, 'fail'], [3, U, False, False, 'fail'], [9, U, False, False, 'ok'],
                                [309, U, False, False, 'fail'], [311.999, U, False, False, 'ok'], [312, U, False, False, 'ok']],
    'failure purge after an hour': [[0, U, False, False, 'fail'], [3, U, False, False, 'fail'], [3603, U, False, False, 'fail'],
                                    [3605.999, U, False, False, 'ok'], [3606, U, False, False, 'ok']],
    'other url independent': [[0, U, False, False, 'fail'], [1, U + '2', False, False, 'ok'], [2, U + '2', True, False, 'ok'],
                              [31, U + '2', True, False, 'ok']],
}
out['coalescing'] = {name: scenario(steps) for name, steps in scenarios.items()}
# Bound: with a limit of 3 the oldest completed entry is evicted for a new owner.
out['coalescing']['bound 3'] = scenario(
    [[0, 'u1', False, False, 'ok'], [1, 'u2', False, False, 'fail'], [2, 'u3', False, False, 'ok'],
     [3, 'u4', False, False, 'ok'], [4, 'u1', False, False, 'ok'], [5, 'u3', False, False, 'ok'],
     [6, 'u2', False, False, 'ok'], [7, 'u4', False, False, 'ok']], limit=3)
out['coalescing']['failure bound 3'] = scenario(
    [[0, 'u1', False, False, 'fail'], [10, 'u2', False, False, 'fail'], [20, 'u3', False, False, 'fail'],
     [30, 'u4', False, False, 'fail'], [40, 'u1', False, False, 'fail'], [50, 'u4', False, False, 'fail']], limit=3)


# ---------------------------------------------------------------- in-flight waiters with mixed flags
def inflight():
    client._fetch_results.clear()
    client._fetch_failures.clear()
    release = threading.Event()
    calls = []

    def fetch(sub_id, url, target='ClashMeta'):
        calls.append(url)
        release.wait(5)
        return client.SubscriptionResult('shared', 'direct_yaml', None, fetched_at='t')

    got = []
    with mock.patch.object(client, '_get_base_url', lambda: 'http://127.0.0.1:9'), \
            mock.patch.object(client, '_get_subscription_proxies_yaml', fetch):
        threads = [threading.Thread(target=lambda r=r, p=p: got.append(
            client.get_subscription_proxies_yaml('s', U, refresh=r, prefer_cached=p)[0]))
            for r, p in ((False, False), (True, False), (False, True), (True, True))]
        threads[0].start()
        time.sleep(0.2)
        for th in threads[1:]:
            th.start()
        time.sleep(0.2)
        release.set()
        for th in threads:
            th.join()
    return {'calls': len(calls), 'got': sorted(got)}


out['inflight'] = inflight()

path = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'review_reference.json')
with open(path, 'w', encoding='utf-8') as f:
    json.dump(out, f, ensure_ascii=False, indent=1)
    f.write('\n')
print({k: len(v) for k, v in out.items()}, file=sys.stderr)
