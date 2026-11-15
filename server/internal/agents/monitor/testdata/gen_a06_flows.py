"""Randomised overview / reset sequences for the A06 review (synthetic values).

Each flow runs overview() and reset_dashboard() steps against one data
directory with a scripted Agent (monitor._request replaced), a scripted
monotonic clock and deterministic generations, and records every answer,
the Agent calls and the exact state file bytes after each step. Writes
a06_flows.json.gz next to this file; a06_review_test.go replays the flows.

    rt -- bash -lc 'cd server/internal/agents/monitor && PYTHONPATH=../../../.. ~/cf-test/.venv/bin/python testdata/gen_a06_flows.py'
"""
import copy
import gzip
import json
import os
import random
import tempfile
import time

from werkzeug.datastructures import MultiDict

from backend.agents import oxidns_monitor as monitor
from backend.agents import oxidns_dashboard_state as storage
from backend.agents import oxidns_publication_stats as publication

random.seed(15112026)


def dump(value):
    return json.dumps(value, sort_keys=True)


def metrics_text(count, failed, cache_lookups, upstream):
    return '\n'.join([
        f'server_request_total{{plugin_tag="dns",protocol="udp"}} {count}',
        f'server_failed_total{{plugin_tag="dns",protocol="udp"}} {failed}',
        'server_inflight{plugin_tag="dns",protocol="udp"} 2',
        f'server_latency_sum_ms{{plugin_tag="dns",protocol="udp"}} {count * 3}.5',
        f'server_latency_count{{plugin_tag="dns",protocol="udp"}} {count}',
        f'cache_lookup_total{{plugin_tag="cache"}} {cache_lookups}',
        f'cache_hit_total{{plugin_tag="cache",kind="fresh"}} {cache_lookups // 2}',
        'cache_hit_total{plugin_tag="cache",kind="stale"} 1',
        'cache_entry_count{plugin_tag="cache"} 30',
        f'forward_upstream_query_total{{plugin_tag="remote",upstream="https://u:p@dns.test/q"}} {upstream}',
        f'forward_upstream_latency_sum_ms{{plugin_tag="remote",upstream="https://u:p@dns.test/q"}} {upstream * 7}',
        f'forward_upstream_latency_count{{plugin_tag="remote",upstream="https://u:p@dns.test/q"}} {upstream}',
    ] + (['forward_upstream_query_total{plugin_tag="local",upstream="udp://192.0.2.1:53"} 3'] if upstream % 2 else []))


def section(rows):
    return {'sample_size': sum(r['count'] for r in rows), 'rows': rows, 'at_limit': False}


def flow():
    steps = []
    count, failed, lookups, upstream = 10, 1, 8, 5
    collected = 1792100000000
    instance, source, runtime = 'core-one', 'a' * 64, 'd' * 64
    recorders = ['r', 'records']
    agent = {'id': 'oxi', 'name': 'DNS', 'host': '127.0.0.1', 'port': 8082, 'profile_id': 'default',
             'service_type': 'oxidns', 'version': '1.8.20-go', 'status': 'online', 'token': 'tok'}
    for _ in range(random.randint(3, 12)):
        r = random.random()
        count += random.choice([0, 1, 5, 5, 20, -3]) if r < 0.9 else -count
        count = max(count, 0)
        failed += random.choice([0, 0, 1, -1])
        failed = max(failed, 0)
        lookups += random.choice([0, 2, 4, -6])
        lookups = max(lookups, 0)
        upstream += random.choice([0, 1, 3])
        collected += random.choice([0, 1000, 10000, 60000, 3600000, -500])
        if random.random() < 0.08:
            instance = random.choice(['core-one', 'core-two'])
        if random.random() < 0.08:
            source = random.choice(['a' * 64, 'b' * 64])
        if random.random() < 0.06:
            runtime = random.choice(['d' * 64, 'e' * 64])
        if random.random() < 0.1:
            recorders = random.choice([['r'], ['r', 'records'], [], ['records']])
        if random.random() < 0.06:
            agent['token'] = random.choice(['tok', 'tok2'])
        if random.random() < 0.06:
            agent['version'] = random.choice(['1.8.20-go', '1.8.60-go', '1.8.14-go'])
        overview = {'success': True, 'health': {'version': '1.6.1', 'status': 'ok', 'uptime_ms': 1234,
                                                'instance_id': instance},
                    'capabilities': {'query_recorders': list(recorders)}, 'collected_at_ms': collected,
                    'source_revision': source, 'runtime_revision': runtime,
                    'upstream_labels': [{'plugin_tag': 'remote', 'upstream': 'https://u:p@dns.test/q',
                                         'address': 'https://u:p@[2001:db8::1]:8443/q'}],
                    'metrics': metrics_text(count, failed, lookups, upstream)}
        mode = random.choice(['ok', 'ok', 'ok', 'ok', 'core_unreachable', 'agent_unreachable', 'invalid', 'changed',
                              'later', 'blocked_off'])
        blocked = {'available': True, 'reason': 'rule_dns', 'matcher_tags': ['m.a'],
                   **section([{'key': 'ads.test', 'count': random.randint(1, 4)}])}
        if mode == 'blocked_off':
            blocked = {'available': False, 'reason': 'too_many_block_rules'}
        rankings = {'success': True, 'recorder': '$param', 'collected_at_ms': collected + (5 if mode == 'later' else 0),
                    'instance_id': instance if mode != 'changed' else 'other', 'source_revision': source,
                    'runtime_revision': runtime, 'retention_days': 7, 'sample_limit': 10000,
                    'clients': section([{'key': '192.0.2.2', 'count': random.randint(1, 3)},
                                        {'key': '2001:db8::2', 'count': 1}]),
                    'domains': section([{'key': 'b.test', 'count': 2}, {'key': 'a.test', 'count': 2}]),
                    'blocked': blocked}
        if mode == 'invalid':
            rankings['sample_limit'] = 5
        if random.random() < 0.75:
            action = {'kind': 'overview', 'args': random.choice([[], [], [('recorder', 'r')], [('recorder', 'records')],
                                                                 [('recorder', 'gone')], [('range', '24h')],
                                                                 [('range', '7d'), ('recorder', 'r')]])}
        else:
            action = {'kind': 'reset', 'body': random.choice([{}, {}, {'recorder': 'r'}, {'recorder': 'records'},
                                                              {'recorder': 'gone'}])}
        steps.append({'action': action, 'agent': copy.deepcopy(agent), 'overview': overview, 'rankings': rankings,
                      'mode': mode, 'dt': random.choice([0, 10, 10, 30, 61, 200])})
    return steps


class Manager:
    def __init__(self, data_dir):
        self.agent = None
        self.repository = type('Repo', (), {'data_dir': data_dir})()

    def get_agent_by_id(self, agent_id, history=True):
        return copy.deepcopy(self.agent) if agent_id == 'oxi' else None


def run_flow(steps):
    monitor._rankings_kept.clear()
    counter = [0]

    def token_hex(n=16):
        counter[0] += 1
        return format(counter[0], '032x')
    publication.secrets.token_hex = token_hex
    clock = [1000.0]
    time.monotonic = lambda: clock[0]
    outputs = []
    with tempfile.TemporaryDirectory() as directory:
        manager = Manager(directory)
        store = storage.DashboardStateStore(directory)
        for step in steps:
            clock[0] += step['dt']
            manager.agent = step['agent']
            seen = []

            def fake(agent, endpoint, params=None, *, min_version=monitor.MIN_AGENT_VERSION):
                seen.append({'endpoint': endpoint, 'params': list((params or {}).items()), 'min_version': min_version})
                if endpoint == 'overview':
                    return copy.deepcopy(step['overview'])
                mode = step['mode']
                if mode in ('core_unreachable', 'agent_unreachable'):
                    raise monitor.MonitorError(mode.upper())
                data = copy.deepcopy(step['rankings'])
                recorders = step['overview']['capabilities']['query_recorders']
                data['recorder'] = (params or {}).get('recorder') or (recorders[0] if recorders else 'r')
                return data
            monitor._request = fake
            try:
                if step['action']['kind'] == 'overview':
                    result = monitor.overview(manager, 'oxi', MultiDict(step['action']['args']))
                else:
                    result = monitor.reset_dashboard(manager, 'oxi', copy.deepcopy(step['action']['body']))
            except monitor.MonitorError as exc:
                result = {'error': exc.code, 'status': exc.status, 'message': exc.message}
            except Exception as exc:  # noqa: BLE001
                result = {'error': type(exc).__name__}
            path = store.path('oxi')
            state = path.read_text('utf-8') if path.exists() else None
            outputs.append({'result': dump(result), 'seen': dump(seen), 'state': state})
    return outputs


original_request, original_monotonic, original_token = monitor._request, time.monotonic, publication.secrets.token_hex
flows = []
try:
    for _ in range(400):
        steps = flow()
        flows.append({'steps': steps, 'outputs': run_flow(steps)})
finally:
    monitor._request, time.monotonic, publication.secrets.token_hex = original_request, original_monotonic, original_token

path = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'a06_flows.json.gz')
with gzip.GzipFile(path, 'wb', mtime=0) as handle:
    handle.write(json.dumps({'flows': flows}, ensure_ascii=True, sort_keys=True).encode())
print(path, len(flows), sum(len(f['steps']) for f in flows))
