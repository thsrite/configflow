"""Durable dashboard boundaries never clear core DNS records or caches."""
import copy
import json
from pathlib import Path
from types import SimpleNamespace
from concurrent.futures import ThreadPoolExecutor

import pytest
from werkzeug.datastructures import MultiDict

from backend.agents import oxidns_monitor as monitor
from backend.agents import oxidns_dashboard_state as storage
from backend.test_oxidns_monitor import AGENT
from backend.test_oxidns_metrics import server, cache
from backend.test_agent_upgrade_auth import authenticated_app


@pytest.fixture(autouse=True)
def fresh_rankings(monkeypatch):
    """Every poll asks for rankings again, unless a test turns reuse on."""
    monkeypatch.setattr(monitor, 'RANKING_REUSE_SECONDS', 0)
    monkeypatch.setattr(monitor, 'RANKING_BACKOFF_SECONDS', 0)
    monkeypatch.setattr(monitor, '_rankings_kept', {})


class Manager:
    def __init__(self, directory, agent_id='one'):
        self.repository = SimpleNamespace(data_dir=Path(directory))
        self.agents = {agent_id: {**AGENT, 'id': agent_id, 'version': '1.8.14-go'}}

    def get_agent_by_id(self, aid, history=True):
        return copy.deepcopy(self.agents.get(aid))


def upstream(count=10, elapsed=100):
    return '\n'.join([
        f'forward_upstream_query_total{{plugin_tag="remote",upstream="cloud"}} {count}',
        'forward_upstream_error_total{plugin_tag="remote",upstream="cloud"} 0',
        f'forward_upstream_latency_count{{plugin_tag="remote",upstream="cloud"}} {count}',
        f'forward_upstream_latency_sum_ms{{plugin_tag="remote",upstream="cloud"}} {elapsed}',
    ])


def snapshot(now=1000, count=10, elapsed=100):
    return {'success': True, 'health': {'status': 'ok', 'version': '1.6.1',
            'uptime_ms': 500, 'instance_id': 'core-one'},
            'capabilities': {'query_recorders': ['records']},
            'collected_at_ms': now, 'source_revision': 'a' * 64, 'runtime_revision': 'd' * 64,
            'upstream_labels': [{'plugin_tag': 'remote', 'upstream': 'cloud', 'address': 'https://dns.test/dns-query'}],
            'metrics': '\n'.join([server('dns', 'udp', count, 0, elapsed, count, 2),
                                  cache('cache', count, count // 2, 0, entries=30), upstream(count, elapsed)])}


def ranking(now=1000):
    section = {'sample_size': 0, 'rows': [], 'at_limit': False}
    return {'success': True, 'recorder': 'records', 'collected_at_ms': now,
            'source_revision': 'a' * 64, 'runtime_revision': 'd' * 64, 'instance_id': 'core-one', 'retention_days': 7, 'sample_limit': 10000,
            'clients': copy.deepcopy(section), 'domains': copy.deepcopy(section),
            'blocked': {'available': True, 'matcher_tag': 'adguard_match', **copy.deepcopy(section)}}


def transport(monkeypatch, current=None, ranks=None, hook=None):
    current = snapshot() if current is None else current
    ranks = ranking() if ranks is None else ranks
    calls = []

    def request(agent, endpoint, params=None, **kwargs):
        calls.append((agent['id'], endpoint, copy.deepcopy(params)))
        if hook:
            hook(agent, endpoint, params)
        result = current if endpoint == 'overview' else ranks
        if isinstance(result, Exception):
            raise result
        return copy.deepcopy(result)

    monkeypatch.setattr(monitor, '_request', request)
    return current, ranks, calls


def test_initial_boundary_is_durable_does_not_rewrite_on_poll_and_preserves_gauges(tmp_path, monkeypatch):
    manager = Manager(tmp_path)
    current, ranks, calls = transport(monkeypatch)
    initial = monitor.overview(manager, 'one')
    assert initial['statistics']['reason'] == 'initial'
    assert initial['statistics']['started_at_ms'] == 1000
    assert initial['metrics']['requests_total'] == 0
    assert initial['metrics']['avg_latency_ms'] is None
    assert initial['metrics']['inflight'] == 2
    assert initial['caches'][0]['entries'] == 30
    store = storage.DashboardStateStore(tmp_path)
    saved = store.path('one').read_bytes()
    before = store.path('one').stat().st_mtime_ns
    current.update(snapshot(now=2000, count=14, elapsed=180))
    ranks['collected_at_ms'] = 2000
    # A new manager simulates a ConfigFlow restart without keeping a memory cache.
    second = monitor.overview(Manager(tmp_path), 'one')
    assert second['statistics'] == initial['statistics']
    assert second['metrics']['requests_total'] == 4
    assert second['metrics']['avg_latency_ms'] == 20
    assert second['upstreams'][0]['requests'] == 4
    assert second['upstreams'][0]['avg_latency_ms'] == 20
    assert calls[-1][2] == {'since_ms': '1000', 'until_ms': '2000'}
    assert store.path('one').read_bytes() == saved
    assert store.path('one').stat().st_mtime_ns == before
    assert not list((tmp_path / 'dns-monitor').glob('*.bak'))
    assert set(c[1] for c in calls) == {'overview', 'rankings'}


def test_manual_reset_changes_generation_and_only_the_selected_agent(tmp_path, monkeypatch):
    manager = Manager(tmp_path)
    manager.agents['two'] = {**manager.agents['one'], 'id': 'two'}
    current, ranks, _ = transport(monkeypatch)
    first = monitor.overview(manager, 'one')
    other = monitor.overview(manager, 'two')
    store = storage.DashboardStateStore(tmp_path)
    other_saved = store.path('two').read_bytes()
    current.update(snapshot(now=2000, count=20))
    ranks['collected_at_ms'] = 2000
    changed = monitor.reset_dashboard(manager, 'one', {'recorder': 'records'})
    assert changed['statistics']['generation'] != first['statistics']['generation']
    assert changed['statistics']['reason'] == 'manual'
    assert changed['statistics']['started_at_ms'] == 2000
    assert changed['metrics']['requests_total'] == 0
    assert changed['metrics']['inflight'] == 2
    assert changed['caches'][0]['entries'] == 30
    assert store.path('two').read_bytes() == other_saved
    assert monitor.overview(manager, 'two')['statistics'] == other['statistics']


@pytest.mark.parametrize('changed,reason', [('instance_id', 'core_changed'), ('source_revision', 'configuration_changed'), ('host', 'target_changed')])
def test_identity_changes_open_a_new_window(tmp_path, monkeypatch, changed, reason):
    manager = Manager(tmp_path)
    current, ranks, _ = transport(monkeypatch)
    previous = monitor.overview(manager, 'one')
    current.update(snapshot(now=2000, count=40))
    ranks['collected_at_ms'] = 2000
    if changed == 'instance_id':
        current['health'][changed] = ranks[changed] = 'core-two'
    elif changed == 'source_revision':
        current[changed] = ranks[changed] = 'b' * 64
    else:
        manager.agents['one']['host'] = '192.0.2.10'
    result = monitor.overview(manager, 'one')
    assert result['statistics']['reason'] == reason
    assert result['statistics']['generation'] != previous['statistics']['generation']
    assert result['statistics']['started_at_ms'] == 2000
    assert result['metrics']['requests_total'] == 0


@pytest.mark.parametrize('stage', ['overview', 'rankings', 'target', 'mismatch'])
def test_failed_manual_reset_preserves_previous_bytes(tmp_path, monkeypatch, stage):
    manager = Manager(tmp_path)
    current, ranks, _ = transport(monkeypatch)
    monitor.overview(manager, 'one')
    store = storage.DashboardStateStore(tmp_path)
    saved = store.path('one').read_bytes()
    current.update(snapshot(now=2000, count=20))
    ranks['collected_at_ms'] = 2000
    if stage in ('overview', 'rankings'):
        transport(monkeypatch, monitor.MonitorError('AGENT_UNREACHABLE') if stage == 'overview' else current,
                  monitor.MonitorError('AGENT_UNREACHABLE') if stage == 'rankings' else ranks)
    elif stage == 'target':
        def mutate(agent, endpoint, params):
            if endpoint == 'rankings':
                manager.agents['one']['host'] = '192.0.2.50'
        transport(monkeypatch, current, ranks, mutate)
    else:
        ranks['source_revision'] = 'c' * 64
    with pytest.raises(monitor.MonitorError):
        monitor.reset_dashboard(manager, 'one', {})
    assert store.path('one').read_bytes() == saved


def test_old_agents_keep_original_counters_without_state_and_cannot_reset(tmp_path, monkeypatch):
    manager = Manager(tmp_path)
    manager.agents['one']['version'] = '1.8.13-go'
    current, ranks, calls = transport(monkeypatch)
    result = monitor.overview(manager, 'one')
    assert result['metrics']['requests_total'] == 10
    assert result['statistics'] is None
    assert result['rankings']['code'] == 'upgrade_required'
    assert not (tmp_path / 'dns-monitor').exists()
    assert [c[1] for c in calls] == ['overview']
    with pytest.raises(monitor.MonitorError, match='1.8.14-go'):
        monitor.reset_dashboard(manager, 'one', {})
    assert len(calls) == 1


def test_unavailable_rankings_leave_existing_metrics_usable(tmp_path, monkeypatch):
    manager = Manager(tmp_path)
    current, _, _ = transport(monkeypatch)
    previous = monitor.overview(manager, 'one')
    current.update(snapshot(now=2000, count=20))
    transport(monkeypatch, current, monitor.MonitorError('CORE_UNREACHABLE'))
    # Without an earlier answer of this round (a timeout after one keeps showing
    # it: test_after_a_timeout_rankings_rest_and_the_overview_still_answers).
    monitor._rankings_kept.clear()
    result = monitor.overview(manager, 'one')
    assert result['statistics'] == previous['statistics']
    assert result['metrics']['requests_total'] == 10
    assert result['rankings']['available'] is False
    assert result['rankings']['success'] is False


def test_generation_compare_and_set_rejects_stale_snapshot(tmp_path, monkeypatch):
    manager = Manager(tmp_path)
    current, ranks, _ = transport(monkeypatch)
    monitor.overview(manager, 'one')
    store = storage.DashboardStateStore(tmp_path)
    original = store.read('one')
    replacement = {**original, 'generation': 'f' * 32, 'reason': 'manual', 'started_at_ms': 2000}
    current.update(snapshot(now=3000))
    ranks['collected_at_ms'] = 3000
    def concurrent_reset(agent, endpoint, params):
        if endpoint == 'rankings':
            store.compare_and_set('one', original['generation'], replacement)
    transport(monkeypatch, current, ranks, concurrent_reset)
    with pytest.raises(monitor.MonitorError) as error:
        monitor.overview(manager, 'one')
    assert error.value.code == 'DASHBOARD_CHANGED'
    assert store.read('one') == replacement


@pytest.mark.parametrize('bad', [b'not json', b'{}', b'[]', b'{"schema":1}'])
def test_damaged_state_does_not_silently_restore_old_counts(tmp_path, monkeypatch, bad):
    manager = Manager(tmp_path)
    store = storage.DashboardStateStore(tmp_path)
    store.directory.mkdir()
    store.path('one').write_bytes(bad)
    _, _, calls = transport(monkeypatch)
    with pytest.raises(monitor.MonitorError) as error:
        monitor.overview(manager, 'one')
    assert error.value.code == 'DASHBOARD_STATE_ERROR'
    assert store.path('one').read_bytes() == bad
    assert calls == []


def test_failed_atomic_replace_keeps_prior_generation_and_cleans_temp(tmp_path, monkeypatch):
    manager = Manager(tmp_path)
    current, ranks, _ = transport(monkeypatch)
    monitor.overview(manager, 'one')
    store = storage.DashboardStateStore(tmp_path)
    saved = store.path('one').read_bytes()
    current.update(snapshot(now=2000))
    ranks['collected_at_ms'] = 2000
    def fail(*args):
        raise OSError('disk unavailable')
    monkeypatch.setattr(storage.os, 'replace', fail)
    with pytest.raises(monitor.MonitorError) as error:
        monitor.reset_dashboard(manager, 'one', {})
    assert error.value.code == 'DASHBOARD_STATE_ERROR'
    assert store.path('one').read_bytes() == saved
    assert not list(store.directory.glob('*.tmp'))


def test_store_safe_filename_and_parallel_generation_check(tmp_path, monkeypatch):
    manager = Manager(tmp_path)
    transport(monkeypatch)
    monitor.overview(manager, 'one')
    store = storage.DashboardStateStore(tmp_path)
    state = store.read('one')
    assert store.path('../../escape').parent == tmp_path / 'dns-monitor'
    assert store.path('../../escape').name != store.path('..\\..\\escape').name
    def write(index):
        replacement = {**state, 'generation': f'{index:032x}', 'reason': 'manual'}
        try:
            store.compare_and_set('one', state['generation'], replacement)
            return True
        except storage.DashboardStateConflict:
            return False
    with ThreadPoolExecutor(max_workers=4) as pool:
        assert sum(pool.map(write, range(1, 9))) == 1
    assert storage.validate_state(store.read('one'))


def test_domain_rankings_support_multiple_questions_per_record():
    value = {'sample_size': 1, 'rows': [{'key': 'many.test', 'count': 2}], 'at_limit': False}
    assert monitor._ranking_section(value, 'domains')['rows'][0]['share'] == 2
    with pytest.raises(monitor.MonitorError):
        monitor._ranking_section({**value, 'rows': [{'key': '192.0.2.1', 'count': 2}]}, 'clients')


def test_rankings_recompute_share_and_preserve_sampling_limit(tmp_path, monkeypatch):
    manager = Manager(tmp_path)
    current, ranks, _ = transport(monkeypatch)
    ranks['clients'] = {'sample_size': 10000, 'at_limit': True,
                        'rows': [{'key': '192.0.2.2', 'count': 5, 'share': 'bad'}, {'key': '192.0.2.1', 'count': 20}]}
    ranks['blocked'] = {'available': False, 'reason': 'private password and configuration'}
    result = monitor.overview(manager, 'one')['rankings']
    assert result['success'] and result['sample_limit'] == 10000
    assert result['clients']['at_limit'] is True
    assert result['clients']['rows'][0] == {'key': '192.0.2.1', 'count': 20, 'share': 0.002}
    assert result['blocked']['available'] is False
    assert result['blocked']['rows'] == []
    assert 'password' not in json.dumps(result)


def test_upstream_raw_labels_never_leak_url_credentials(tmp_path, monkeypatch):
    manager = Manager(tmp_path)
    current, ranks, _ = transport(monkeypatch)
    raw = 'https://user:password@dns.test/dns-query?key=secret#fragment'
    current['metrics'] = current['metrics'].replace('upstream="cloud"', 'upstream="' + raw + '"')
    current['upstream_labels'][0].update(upstream=raw, address=raw)
    result = monitor.overview(manager, 'one')
    row = result['upstreams'][0]
    assert row['upstream'] == row['address'] == 'https://dns.test/dns-query'
    assert not any(secret in json.dumps(result) for secret in ('password', 'secret', 'fragment'))


@pytest.mark.parametrize('body', [None, [], {'url': 'http://elsewhere'}, {'recorder': '../private'}, {'recorder': 1}])
def test_reset_input_validation_before_network(tmp_path, monkeypatch, body):
    _, _, calls = transport(monkeypatch)
    with pytest.raises(monitor.MonitorError) as error:
        monitor.reset_dashboard(Manager(tmp_path), 'one', body)
    assert error.value.code == 'INVALID_QUERY'
    assert calls == []


def test_reset_route_auth_method_no_store_and_unchanged_config(authenticated_app, monkeypatch):
    client, manager, _, admin, _ = authenticated_app
    registered = manager.register_agent({**{k: v for k, v in AGENT.items() if k not in ('id', 'token')}, 'version': '1.8.14-go'})
    aid = registered['id']
    _, _, calls = transport(monkeypatch)
    url = f'/api/agents/{aid}/oxidns/dashboard/reset'
    assert client.post(url, json={}).status_code == 401
    assert client.post(url, json={}, headers={'Authorization': 'Bearer ' + registered['token']}).status_code == 401
    assert client.get(url, headers=admin).status_code == 405
    assert calls == []
    config_before = manager.repository._store.fingerprint()
    response = client.post(url, json={}, headers=admin)
    assert response.status_code == 200
    assert response.headers['Cache-Control'] == 'no-store'
    assert response.get_json()['statistics']['reason'] == 'manual'
    assert manager.repository._store.fingerprint() == config_before
    assert registered['token'] not in response.get_data(as_text=True)
    assert client.post(url + '?agent_id=elsewhere', json={}, headers=admin).status_code == 400
    assert client.post(url, json={'recorder': 'x' * 3000}, headers=admin).status_code == 400
    assert client.post(url, data='{', content_type='application/json', headers=admin).status_code == 400
    assert client.get(f'/api/agents/{aid}/oxidns/overview?recorder=records', headers=admin).status_code == 200
    assert client.get(f'/api/agents/{aid}/oxidns/overview?unknown=1', headers=admin).status_code == 400


def test_counter_restart_opens_durable_window_and_never_reuses_old_baseline(tmp_path, monkeypatch):
    manager = Manager(tmp_path)
    current, ranks, _ = transport(monkeypatch, snapshot(now=1000, count=100, elapsed=1000))
    previous = monitor.overview(manager, 'one')
    current.update(snapshot(now=2000, count=10, elapsed=100))
    ranks['collected_at_ms'] = 2000
    restarted = monitor.overview(manager, 'one')
    assert restarted['statistics']['reason'] == 'counters_changed'
    assert restarted['statistics']['generation'] != previous['statistics']['generation']
    assert restarted['metrics']['requests_total'] == 0
    current.update(snapshot(now=3000, count=110, elapsed=1100))
    ranks['collected_at_ms'] = 3000
    continued = monitor.overview(Manager(tmp_path), 'one')
    assert continued['statistics'] == restarted['statistics']
    assert continued['metrics']['requests_total'] == 100
    assert continued['metrics']['avg_latency_ms'] == 10


def test_unknown_retention_is_preserved_and_overlong_top_list_is_rejected(tmp_path, monkeypatch):
    manager = Manager(tmp_path)
    current, ranks, _ = transport(monkeypatch)
    ranks['retention_days'] = None
    assert monitor.overview(manager, 'one')['rankings']['retention_days'] is None
    ranks['domains'] = {'sample_size': 20, 'at_limit': False,
                        'rows': [{'key': f'{i}.test', 'count': 1} for i in range(11)]}
    with pytest.raises(monitor.MonitorError) as error:
        monitor.reset_dashboard(manager, 'one', {})
    assert error.value.code == 'INVALID_RESPONSE'


def test_upstream_hash_suffix_keeps_distinct_stable_ids():
    rows = [{'tag': 'forward', 'upstream': 'dns#1'}, {'tag': 'forward', 'upstream': 'dns#2'}]
    result = monitor._public_upstreams(rows, [])
    assert result[0]['upstream'] == 'dns#1'
    assert result[1]['upstream'] == 'dns#2'
    assert result[0]['id'] != result[1]['id']
    assert result == monitor._public_upstreams(rows, [])


def test_missing_recorder_still_allows_reset_of_metrics_without_core_mutation(tmp_path, monkeypatch):
    manager = Manager(tmp_path)
    current, _, calls = transport(monkeypatch)
    current['capabilities']['query_recorders'] = []
    result = monitor.reset_dashboard(manager, 'one', {})
    assert result['metrics']['requests_total'] == 0
    assert result['rankings']['code'] == 'recorder_not_configured'
    assert [item[1] for item in calls] == ['overview']


def test_initial_snapshot_requires_valid_clock_and_keeps_state_absent_on_error(tmp_path, monkeypatch):
    manager = Manager(tmp_path)
    current, _, _ = transport(monkeypatch)
    current['collected_at_ms'] = 2 ** 53 - 1
    with pytest.raises(monitor.MonitorError) as error:
        monitor.overview(manager, 'one')
    assert error.value.code == 'INVALID_RESPONSE'
    assert not storage.DashboardStateStore(tmp_path).path('one').exists()


def test_removed_selected_recorder_does_not_trap_overview_in_errors(tmp_path, monkeypatch):
    manager = Manager(tmp_path)
    current, ranks, calls = transport(monkeypatch)
    monitor.overview(manager, 'one', MultiDict({'recorder': 'records'}))
    current['capabilities']['query_recorders'] = ['replacement']
    current['source_revision'] = ranks['source_revision'] = 'b' * 64
    current['collected_at_ms'] = ranks['collected_at_ms'] = 2000
    ranks['recorder'] = 'replacement'
    result = monitor.overview(manager, 'one', MultiDict({'recorder': 'records'}))
    assert result['recorders'] == ['replacement']
    assert result['rankings']['recorder'] == 'replacement'
    assert 'recorder' not in calls[-1][2]
    assert result['statistics']['reason'] == 'configuration_changed'
    saved = storage.DashboardStateStore(tmp_path).read('one')
    with pytest.raises(monitor.MonitorError) as error:
        monitor.reset_dashboard(manager, 'one', {'recorder': 'records'})
    assert error.value.code == 'INVALID_QUERY'
    assert storage.DashboardStateStore(tmp_path).read('one') == saved


def test_same_file_runtime_reload_starts_new_window_even_when_counters_caught_up(tmp_path, monkeypatch):
    manager = Manager(tmp_path)
    current, ranks, _ = transport(monkeypatch)
    previous = monitor.overview(manager, 'one')
    current.update(snapshot(now=2000, count=100))
    current['runtime_revision'] = ranks['runtime_revision'] = 'e' * 64
    ranks['collected_at_ms'] = 2000
    result = monitor.overview(manager, 'one')
    assert result['statistics']['reason'] == 'core_changed'
    assert result['statistics']['generation'] != previous['statistics']['generation']
    assert result['metrics']['requests_total'] == 0


def test_runtime_generation_change_between_overview_and_rankings_blocks_manual_commit(tmp_path, monkeypatch):
    manager = Manager(tmp_path)
    current, ranks, _ = transport(monkeypatch)
    monitor.overview(manager, 'one')
    store = storage.DashboardStateStore(tmp_path)
    saved = store.path('one').read_bytes()
    ranks['runtime_revision'] = 'e' * 64
    with pytest.raises(monitor.MonitorError) as error:
        monitor.reset_dashboard(manager, 'one', {})
    assert error.value.code == 'SNAPSHOT_CHANGED'
    assert store.path('one').read_bytes() == saved


def _replace_generation_in_process(directory, expected, index, ready, results):
    store = storage.DashboardStateStore(directory)
    replacement = {**expected, 'generation': f'{index:032x}', 'reason': 'manual'}
    ready.wait(10)
    try:
        store.compare_and_set('one', expected['generation'], replacement)
        results.put(True)
    except storage.DashboardStateConflict:
        results.put(False)


def test_file_lock_serializes_resets_across_processes(tmp_path, monkeypatch):
    import multiprocessing
    manager = Manager(tmp_path)
    transport(monkeypatch)
    monitor.overview(manager, 'one')
    store = storage.DashboardStateStore(tmp_path)
    state = store.read('one')
    context = multiprocessing.get_context('spawn')
    ready, results = context.Event(), context.Queue()
    processes = [context.Process(target=_replace_generation_in_process,
                                args=(str(tmp_path), state, index, ready, results))
                 for index in range(1, 5)]
    try:
        for process in processes:
            process.start()
        ready.set()
        assert sum(results.get(timeout=10) for _ in processes) == 1
        for process in processes:
            process.join(timeout=10)
            assert process.exitcode == 0
        assert storage.validate_state(store.read('one'))
    finally:
        for process in processes:
            if process.is_alive():
                process.terminate()
                process.join(timeout=5)
        results.close()


def test_missing_retention_metadata_is_unknown(tmp_path, monkeypatch):
    _, ranks, _ = transport(monkeypatch)
    ranks.pop('retention_days')
    result = monitor.overview(Manager(tmp_path), 'one')
    assert result['rankings']['success'] is True
    assert result['rankings']['retention_days'] is None


@pytest.mark.parametrize('blocked, expected', [
    ({}, ('standard_adguard', '标准 AdGuard 拦截路径')),
    ({'reason': 'standard_adguard'}, ('standard_adguard', '标准 AdGuard 拦截路径')),
    ({'reason': 'rule_dns', 'matcher_tags': ['qs.match.sequence_main.3.0.qname', 'qs.match.sequence_main.4.0.qname']},
     ('rule_dns', '规则拦截（拒绝/黑洞）')),
    ({'reason': 'standard_adguard_rule_dns', 'matcher_tags': ['qs.match.sequence_main.2.0.question']},
     ('standard_adguard_rule_dns', '标准 AdGuard 与规则拦截')),
])
def test_blocked_ranking_reason_follows_agent(tmp_path, monkeypatch, blocked, expected):
    manager = Manager(tmp_path)
    current, ranks, _ = transport(monkeypatch)
    ranks['blocked'].update(blocked)
    result = monitor.overview(manager, 'one')['rankings']['blocked']
    assert result['available'] is True and (result['code'], result['reason']) == expected
    assert result.get('matcher_tags') == blocked.get('matcher_tags')


@pytest.mark.parametrize('blocked', [{'reason': 'Rule DNS'}, {'reason': 'rule_dns', 'matcher_tags': ['../x']},
                                     {'reason': 'rule_dns', 'matcher_tags': []},
                                     {'reason': 'rule_dns', 'matcher_tags': [f'm{i}' for i in range(33)]}])
def test_blocked_ranking_rejects_malformed_reason_or_matchers(tmp_path, monkeypatch, blocked):
    manager = Manager(tmp_path)
    current, ranks, _ = transport(monkeypatch)
    ranks['blocked'].update(blocked)
    assert monitor.overview(manager, 'one')['rankings']['available'] is False


def test_too_many_block_rules_reason_is_actionable(tmp_path, monkeypatch):
    manager = Manager(tmp_path)
    current, ranks, _ = transport(monkeypatch)
    ranks['blocked'] = {'available': False, 'reason': 'too_many_block_rules', 'sample_size': 0, 'rows': [],
                        'at_limit': False}
    result = monitor.overview(manager, 'one')['rankings']['blocked']
    assert result['available'] is False and result['code'] == 'too_many_block_rules'
    assert result['reason'].startswith('拦截规则分组超过 32 个') and '刷新' not in result['reason']
    # The agent's sentence fallback is not a code; the generic text covers rule blocks too.
    ranks['blocked'] = {'available': False, 'reason': '当前配置未包含可确认的标准 AdGuard 拦截路径'}
    result = monitor.overview(manager, 'one')['rankings']['blocked']
    assert result['code'] == 'unsupported_configuration' and '拒绝/黑洞' in result['reason']


# --- Rankings: bounded window, reuse between polls, pause after a timeout ----

@pytest.fixture
def clock(monkeypatch):
    now = [1000.0]
    monkeypatch.setattr(monitor, 'time', SimpleNamespace(monotonic=lambda: now[0]))
    monkeypatch.setattr(monitor, 'RANKING_REUSE_SECONDS', 60)
    monkeypatch.setattr(monitor, 'RANKING_BACKOFF_SECONDS', 180)
    return now


def ranking_calls(calls):
    return [params for _, endpoint, params in calls if endpoint == 'rankings']


def test_rankings_cover_at_most_the_last_day_of_a_long_round(tmp_path, monkeypatch):
    """A statistics round can run for weeks; the core reads the whole window
    to find the newest sample, so the window stops a day back."""
    day = monitor.RANKING_WINDOW_MS
    current, ranks, calls = transport(monkeypatch, snapshot(now=1000), ranking(now=1000))
    manager = Manager(tmp_path)
    monitor.overview(manager, 'one')
    assert ranking_calls(calls)[-1] == {'since_ms': '1000', 'until_ms': '1000'}
    # Hours later the window still starts at the round's start.
    current['collected_at_ms'] = ranks['collected_at_ms'] = 1000 + day // 2
    result = monitor.overview(manager, 'one')
    assert ranking_calls(calls)[-1] == {'since_ms': '1000', 'until_ms': str(1000 + day // 2)}
    assert result['statistics']['started_at_ms'] == 1000
    # Weeks later: the last 24 hours, while the totals keep the round's start.
    later = 1000 + 23 * day
    current['collected_at_ms'] = ranks['collected_at_ms'] = later
    result = monitor.overview(manager, 'one')
    assert ranking_calls(calls)[-1] == {'since_ms': str(later - day), 'until_ms': str(later)}
    assert (result['rankings']['since_ms'], result['rankings']['until_ms']) == (later - day, later)
    assert result['statistics']['started_at_ms'] == 1000


def test_rankings_are_reused_between_polls_for_a_minute(tmp_path, monkeypatch, clock):
    current, ranks, calls = transport(monkeypatch)
    manager = Manager(tmp_path)
    first = monitor.overview(manager, 'one')
    assert len(ranking_calls(calls)) == 1 and first['rankings']['success'] is True
    for step in range(5):  # the panel polls every 10 s
        clock[0] += 10
        current['collected_at_ms'] += 10_000
        polled = monitor.overview(manager, 'one')
        assert polled['rankings'] == first['rankings']
        # The counters are read fresh every time.
        assert polled['collected_at'] != first['collected_at']
    assert len(ranking_calls(calls)) == 1
    clock[0] += 10  # 60 s after the first
    current['collected_at_ms'] += 10_000
    ranks['collected_at_ms'] = current['collected_at_ms']
    monitor.overview(manager, 'one')
    assert len(ranking_calls(calls)) == 2


def test_a_reset_or_another_recorder_asks_again_at_once(tmp_path, monkeypatch, clock):
    current, ranks, calls = transport(monkeypatch)
    manager = Manager(tmp_path)
    monitor.overview(manager, 'one')
    current['capabilities'] = {'query_recorders': ['records', 'other']}
    monitor.overview(manager, 'one')
    assert len(ranking_calls(calls)) == 2  # the recorder list changed
    monitor.overview(manager, 'one')
    assert len(ranking_calls(calls)) == 2
    clock[0] += 1
    monitor.reset_dashboard(manager, 'one', {})
    assert len(ranking_calls(calls)) == 3  # a manual reset never reuses
    monitor.overview(manager, 'one')
    assert len(ranking_calls(calls)) == 3  # and its answer serves the new round
    ranks['recorder'] = 'other'
    monitor.overview(manager, 'one', MultiDict({'recorder': 'other'}))
    assert ranking_calls(calls)[-1]['recorder'] == 'other' and len(ranking_calls(calls)) == 4


@pytest.mark.parametrize('code', ['CORE_UNREACHABLE', 'AGENT_UNREACHABLE'])
def test_after_a_timeout_rankings_rest_and_the_overview_still_answers(tmp_path, monkeypatch, clock, code):
    """A timed-out ranking keeps the core busy; asking again every 10 s kept the
    query records page failing as well."""
    current, ranks, calls = transport(monkeypatch)
    manager = Manager(tmp_path)
    monitor.overview(manager, 'one')
    clock[0] += 61
    good = copy.deepcopy(ranks)
    monkeypatch.setattr(monitor, '_request', _failing_rankings(calls, current, monitor.MonitorError(code)))
    failed = monitor.overview(manager, 'one')
    assert len(ranking_calls(calls)) == 2
    # The last answer of this round stays on screen; the counters keep updating.
    assert failed['rankings']['success'] is True and failed['metrics']['requests_total'] is not None
    for _ in range(17):
        clock[0] += 10
        assert monitor.overview(manager, 'one')['rankings']['success'] is True
    assert len(ranking_calls(calls)) == 2
    clock[0] += 10  # 180 s after the timeout
    monkeypatch.setattr(monitor, '_request', _failing_rankings(calls, current, good))
    monitor.overview(manager, 'one')
    assert len(ranking_calls(calls)) == 3


def test_a_timeout_without_an_earlier_answer_says_why_rankings_wait(tmp_path, monkeypatch, clock):
    current, _, calls = transport(monkeypatch)
    monkeypatch.setattr(monitor, '_request', _failing_rankings(calls, current, monitor.MonitorError('CORE_UNREACHABLE')))
    manager = Manager(tmp_path)
    first = monitor.overview(manager, 'one')
    assert first['rankings']['available'] is False and first['rankings']['code'] == 'core_unreachable'
    clock[0] += 10
    paused = monitor.overview(manager, 'one')
    assert paused['rankings']['code'] == 'rankings_paused' and '查询记录' in paused['rankings']['reason']
    assert len(ranking_calls(calls)) == 1
    # A manual reset is asked for explicitly and does not wait.
    with pytest.raises(monitor.MonitorError):
        monitor.reset_dashboard(manager, 'one', {})
    assert len(ranking_calls(calls)) == 2


def test_other_ranking_errors_do_not_pause(tmp_path, monkeypatch, clock):
    current, _, calls = transport(monkeypatch)
    monkeypatch.setattr(monitor, '_request', _failing_rankings(calls, current, monitor.MonitorError('INVALID_RESPONSE')))
    manager = Manager(tmp_path)
    for _ in range(3):
        assert monitor.overview(manager, 'one')['rankings']['code'] == 'invalid_response'
        clock[0] += 10
    assert len(ranking_calls(calls)) == 3


def test_kept_rankings_belong_to_one_device(tmp_path, monkeypatch, clock):
    current, ranks, calls = transport(monkeypatch)
    manager = Manager(tmp_path)
    manager.agents['two'] = {**manager.agents['one'], 'id': 'two', 'host': '192.0.2.99'}
    monitor.overview(manager, 'one')
    monitor.overview(manager, 'two')
    monitor.overview(manager, 'one')
    assert [agent for agent, endpoint, _ in calls if endpoint == 'rankings'] == ['one', 'two']


def _failing_rankings(calls, current, answer):
    def request(agent, endpoint, params=None, **kwargs):
        calls.append((agent['id'], endpoint, copy.deepcopy(params)))
        if endpoint == 'overview':
            return copy.deepcopy(current)
        if isinstance(answer, Exception):
            raise answer
        return copy.deepcopy(answer)
    return request

