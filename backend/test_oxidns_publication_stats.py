"""Publication checkpoints preserve counters without a background collector."""
from copy import deepcopy

import pytest

from backend.agents import oxidns_publication_stats as publication
from backend.agents import oxidns_metrics as metrics
from backend.agents import oxidns_monitor as monitor
from backend.agents.oxidns_dashboard_state import DashboardStateStore, validate_state
from backend.test_oxidns_dashboard import Manager, snapshot, ranking, transport


def task(manager, ident='publish-one', status='uploading', revision='b' * 64):
    agent = manager.agents['one']
    agent.setdefault('deployments', {})[ident] = {
        'deployment_id': ident, 'status': status, 'service_type': 'oxidns',
        'profile_id': agent.get('profile_id') or 'default', 'config_version': revision,
    }
    agent['latest_deployment'] = deepcopy(agent['deployments'][ident])


def change(current, ranks, now, count, elapsed, revision='b' * 64, runtime='e' * 64):
    current.update(snapshot(now=now, count=count, elapsed=elapsed))
    current.update(source_revision=revision, runtime_revision=runtime)
    ranks.update(ranking(now=now), source_revision=revision, runtime_revision=runtime)


@pytest.fixture
def env(tmp_path, monkeypatch):
    manager = Manager(tmp_path)
    current, ranks, calls = transport(monkeypatch)
    initial = monitor.overview(manager, 'one')
    current.update(snapshot(now=2000, count=40, elapsed=900))
    ranks['collected_at_ms'] = 2000
    task(manager)
    return manager, current, ranks, calls, initial, DashboardStateStore(tmp_path)


def test_two_publications_accumulate_weighted_counters_keep_rankings_and_live_gauges(env):
    manager, current, ranks, calls, initial, store = env
    publication.checkpoint(manager, 'one', 'publish-one', 'b' * 64)
    assert store.read('one')['schema'] == 2
    assert calls[-1][1] == 'overview'  # no history scan at publication time
    change(current, ranks, 3000, 6, 60)
    task(manager, status='succeeded')
    publication.complete(manager, 'one', 'publish-one', 'succeeded')
    result = monitor.overview(manager, 'one')
    assert result['metrics']['requests_total'] == 36  # (40 - 10) + 6
    assert result['metrics']['avg_latency_ms'] == pytest.approx(860 / 36)
    assert result['metrics']['cache_hit_rate'] == 50
    assert result['metrics']['inflight'] == 2
    assert result['caches'][0]['entries'] == 30
    assert result['upstreams'][0]['requests'] == 36
    assert result['statistics']['started_at_ms'] == initial['statistics']['started_at_ms']
    assert result['statistics']['publication_count'] == 1
    assert result['statistics']['reason'] == 'publication'
    assert calls[-1][2]['since_ms'] == '1000'
    saved = store.path('one').read_bytes()
    publication.complete(manager, 'one', 'publish-one', 'succeeded')
    monitor.overview(manager, 'one')
    assert store.path('one').read_bytes() == saved

    # Simulate a ConfigFlow process restart between publications.
    restarted = Manager(manager.repository.data_dir)
    restarted.agents = deepcopy(manager.agents)
    change(current, ranks, 4000, 16, 160)
    task(restarted, 'publish-two', revision='c' * 64)
    publication.checkpoint(restarted, 'one', 'publish-two', 'c' * 64)
    change(current, ranks, 5000, 5, 100, revision='c' * 64, runtime='f' * 64)
    task(restarted, 'publish-two', 'succeeded', 'c' * 64)
    # Overview also reconciles a terminal result if the server died before complete().
    result = monitor.overview(restarted, 'one')
    assert result['metrics']['requests_total'] == 51
    assert result['metrics']['avg_latency_ms'] == pytest.approx(1060 / 51)
    assert result['metrics']['cache_hit_rate'] == pytest.approx(25 / 51 * 100)
    assert result['statistics']['publication_count'] == 2
    assert result['statistics']['started_at_ms'] == 1000


@pytest.mark.parametrize('status', ['uploading', 'reloading', 'unknown'])
def test_overview_does_not_discard_checkpoint_before_transaction_confirmation(env, status):
    manager, current, ranks, _, _, store = env
    publication.checkpoint(manager, 'one', 'publish-one', 'b' * 64)
    saved = store.path('one').read_bytes()
    change(current, ranks, 3000, 6, 60)
    task(manager, status=status)
    with pytest.raises(monitor.MonitorError) as error:
        monitor.overview(manager, 'one')
    assert error.value.code == 'SNAPSHOT_CHANGED'
    assert store.path('one').read_bytes() == saved


@pytest.mark.parametrize('status', ['failed', 'succeeded'])
def test_no_runtime_change_never_adds_checkpoint_twice(env, status):
    manager, current, ranks, _, _, store = env
    publication.checkpoint(manager, 'one', 'publish-one', 'b' * 64)
    current.update(snapshot(now=3000, count=46, elapsed=960))
    ranks['collected_at_ms'] = 3000
    task(manager, status=status)
    publication.complete(manager, 'one', 'publish-one', status)
    result = monitor.overview(manager, 'one')
    assert result['metrics']['requests_total'] == 36
    assert result['statistics']['reason'] == 'initial'
    assert store.read('one')['pending'] is None


def test_automatic_rollback_preserves_prepublication_counts(env):
    manager, current, ranks, _, _, _ = env
    publication.checkpoint(manager, 'one', 'publish-one', 'b' * 64)
    change(current, ranks, 3000, 4, 20, revision='a' * 64)
    task(manager, status='rolled_back')
    publication.complete(manager, 'one', 'publish-one', 'rolled_back')
    assert monitor.overview(manager, 'one')['metrics']['requests_total'] == 34


@pytest.mark.parametrize('cause', ['wrong_revision', 'wrong_task', 'wrong_target', 'rollback_failed', 'manual_reset'])
def test_unrelated_changes_never_inherit_the_publication_checkpoint(env, cause):
    manager, current, ranks, _, _, store = env
    publication.checkpoint(manager, 'one', 'publish-one', 'b' * 64)
    if cause == 'manual_reset':
        monitor.reset_dashboard(manager, 'one', {})
    change(current, ranks, 3000, 6, 60)
    task(manager, status='rollback_failed' if cause == 'rollback_failed' else 'succeeded')
    if cause == 'wrong_revision':
        current['source_revision'] = ranks['source_revision'] = 'c' * 64
    elif cause == 'wrong_task':
        task(manager, 'another', 'succeeded')
    elif cause == 'wrong_target':
        manager.agents['one']['host'] = '192.0.2.99'
    result = monitor.overview(manager, 'one')
    assert result['metrics']['requests_total'] == 0
    assert result['statistics']['started_at_ms'] == 3000
    assert 'pending' not in store.read('one')


def test_external_reload_after_completed_publication_is_not_counted_again(env):
    manager, current, ranks, _, _, _ = env
    publication.checkpoint(manager, 'one', 'publish-one', 'b' * 64)
    change(current, ranks, 3000, 6, 60)
    task(manager, status='succeeded')
    publication.complete(manager, 'one', 'publish-one', 'succeeded')
    change(current, ranks, 4000, 2, 20, runtime='f' * 64)
    result = monitor.overview(manager, 'one')
    assert result['statistics']['reason'] == 'core_changed'
    assert result['metrics']['requests_total'] == 0


def test_next_publication_recovers_prior_pending_success_before_checkpoint(env):
    manager, current, ranks, _, _, _ = env
    publication.checkpoint(manager, 'one', 'publish-one', 'b' * 64)
    change(current, ranks, 3000, 6, 60)
    task(manager, status='succeeded')
    # Complete() could not reach the core; no overview was opened in between.
    task(manager, 'publish-two', revision='c' * 64)
    publication.checkpoint(manager, 'one', 'publish-two', 'c' * 64)
    change(current, ranks, 4000, 4, 20, revision='c' * 64, runtime='f' * 64)
    task(manager, 'publish-two', 'succeeded', 'c' * 64)
    result = monitor.overview(manager, 'one')
    assert result['metrics']['requests_total'] == 40
    assert result['statistics']['publication_count'] == 2


def test_refresh_ready_candidate_checkpoint_after_rejected_activation(env):
    manager, current, ranks, _, _, store = env
    task(manager, status='ready')
    publication.checkpoint(manager, 'one', 'publish-one', 'b' * 64)
    current.update(snapshot(now=3000, count=60, elapsed=1000))
    publication.checkpoint(manager, 'one', 'publish-one', 'b' * 64)
    assert store.read('one')['pending']['totals']['servers'][0]['requests'] == 50
    change(current, ranks, 4000, 2, 20)
    task(manager, status='succeeded')
    assert monitor.overview(manager, 'one')['metrics']['requests_total'] == 52


def test_checkpoint_failure_does_not_modify_previous_state(env, monkeypatch):
    manager, _, _, _, _, store = env
    saved = store.path('one').read_bytes()
    def fail(*args, **kwargs):
        raise monitor.MonitorError('CORE_UNREACHABLE')
    monkeypatch.setattr(monitor, '_overview_snapshot', fail)
    publication.checkpoint(manager, 'one', 'publish-one', 'b' * 64)
    assert store.path('one').read_bytes() == saved


def test_never_opened_dashboard_does_not_start_monitoring_on_publication(tmp_path, monkeypatch):
    manager = Manager(tmp_path)
    _, _, calls = transport(monkeypatch)
    task(manager)
    publication.checkpoint(manager, 'one', 'publish-one', 'b' * 64)
    task(manager, status='succeeded')
    publication.complete(manager, 'one', 'publish-one', 'succeeded')
    assert calls == []
    assert not DashboardStateStore(tmp_path).path('one').exists()


def test_concurrent_reset_wins_over_prepublication_network_snapshot(env, monkeypatch):
    manager, _, _, _, _, store = env
    saved = store.read('one')
    replacement = {**saved, 'generation': 'f' * 32, 'reason': 'manual'}
    original = monitor._overview_snapshot
    def concurrent(*args):
        result = original(*args)
        store.compare_and_set('one', saved['generation'], replacement)
        return result
    monkeypatch.setattr(monitor, '_overview_snapshot', concurrent)
    publication.checkpoint(manager, 'one', 'publish-one', 'b' * 64)
    assert store.read('one') == replacement


def test_retired_series_keep_counters_and_added_series_start_at_zero():
    old = metrics.parse_metrics(snapshot(count=40)['metrics'])
    new = metrics.parse_metrics(snapshot(count=6)['metrics'].replace('dns"', 'new-dns"'))
    combined = metrics.add_observations(new, old)
    result = metrics.summarize_observations(combined)
    assert result['metrics']['requests_total'] == 46
    assert result['metrics']['inflight'] == 2
    assert result['caches'][0]['entries'] == 30
    assert metrics.apply_metrics_baseline(new, metrics.zero_baseline(new))[1] is False


def test_missing_prepublication_counter_remains_unknown(env):
    manager, current, ranks, _, _, _ = env
    current['metrics'] = current['metrics'].replace('cache_hit_total', 'unavailable_hit_total')
    publication.checkpoint(manager, 'one', 'publish-one', 'b' * 64)
    change(current, ranks, 3000, 6, 60)
    task(manager, status='succeeded')
    result = monitor.overview(manager, 'one')
    assert result['metrics']['cache_hit_rate'] is None
    assert result['statistics']['partial'] is True
    assert result['metrics']['requests_total'] == 36


@pytest.mark.parametrize('field,value', [('carried', {}), ('carried_partial', 1),
                                      ('publication_count', -1), ('pending', {'deployment_id': 'x'})])
def test_invalid_extended_state_is_rejected(env, field, value):
    manager, _, _, _, _, store = env
    publication.checkpoint(manager, 'one', 'publish-one', 'b' * 64)
    state = store.read('one')
    assert validate_state(state)
    state[field] = value
    assert not validate_state(state)
