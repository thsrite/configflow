"""Complete OxiDNS rankings from stored 15-minute periods.

The fake Agent below behaves as the core does: one request ranks at most the
newest 10,000 records of its window and returns at most `rows` entries.
"""
from collections import Counter
import copy
import random
from pathlib import Path
from types import SimpleNamespace

import pytest
from werkzeug.datastructures import MultiDict

from backend.agents import oxidns_monitor as monitor
from backend.agents import oxidns_ranking_history as history
from backend.test_oxidns_dashboard import Manager, ranking, snapshot

DAY, HOUR, SLOT = history.DAY, history.HOUR, history.SLOT
T0 = 1_793_000_000 - 1_793_000_000 % DAY  # a UTC midnight


class FakeAgent:
    """Answers rankings from a list of (time, client, domain, blocked) records."""

    def __init__(self, records, *, blocked=True, sample_cap=10000):
        self.records = sorted(records)
        self.blocked = blocked
        self.cap = sample_cap
        self.calls = []
        self.fail_at = None

    def window(self, since, until):
        inside = [record for record in self.records if since <= record[0] < until]
        return inside[-self.cap:] if self.cap else inside  # the newest, like the core

    def section(self, records, index, rows):
        counts = Counter(record[index] for record in records)
        top = sorted(counts.items(), key=lambda item: (-item[1], item[0]))[:rows]
        return {'sample_size': len(records), 'at_limit': len(records) == self.cap,
                'rows': [{'key': key, 'count': count, 'share': 0} for key, count in top]}

    def __call__(self, agent, endpoint, params=None, **kwargs):
        assert endpoint == 'rankings' and kwargs.get('min_version') == history.HISTORY_AGENT_VERSION
        since, until = int(params['since_ms']) // 1000, int(params['until_ms']) // 1000
        self.calls.append((since, until))
        if self.fail_at is not None and len(self.calls) >= self.fail_at:
            raise monitor.MonitorError('CORE_UNREACHABLE')
        rows = int(params['rows'])
        sample = self.window(since, until)
        blocked_records = [record for record in self.records if since <= record[0] < until and record[3]][-self.cap:]
        return {'success': True, 'recorder': params['recorder'], 'retention_days': 7,
                'clients': self.section(sample, 1, rows), 'domains': self.section(sample, 2, rows),
                'blocked': ({'available': True, **self.section(blocked_records, 2, rows)} if self.blocked
                            else {'available': False, 'reason': 'unsupported_configuration'})}


def generate(seed, start, days, per_hour, clients=30, domains=3000):
    random.seed(seed)
    names = [f'n{i}.example.test.' for i in range(domains)]
    weights = [1 / (i + 1) ** 1.05 for i in range(domains)]
    hosts = [f'192.0.2.{i + 1}' for i in range(clients)]
    host_weights = [1 / (i + 1) for i in range(clients)]
    total = int(days * 24 * per_hour)
    times = sorted(random.uniform(start, start + days * DAY) for _ in range(total))
    picked = random.choices(names, weights, k=total)
    picked_hosts = random.choices(hosts, host_weights, k=total)
    return [(int(t), host, name, name.startswith('n7')) for t, host, name in zip(times, picked_hosts, picked)]


def exact(records, since, until):
    inside = [record for record in records if since <= record[0] < until]
    top = lambda index, rows: sorted(Counter(record[index] for record in rows).items(), key=lambda item: (-item[1], item[0]))[:10]
    return {'records': len(inside), 'clients': top(1, inside), 'domains': top(2, inside),
            'blocked': top(2, [record for record in inside if record[3]]), 'blocked_records': sum(record[3] for record in inside)}


@pytest.fixture
def store(tmp_path):
    return history.RankingHistory(tmp_path)


def collect_all(store, agent, fake, now, monkeypatch, recorders=('records',)):
    monkeypatch.setattr(monitor, '_request', fake)
    while history.collect_agent(store, agent, list(recorders), now=now, budget=10_000, pause=0):
        pass


AGENT = {'id': 'dns', 'name': 'DNS', 'version': history.HISTORY_AGENT_VERSION}


# --- the requirement: what the panel shows is every record --------------------

@pytest.mark.parametrize('seed', [1, 2, 3])
def test_stored_periods_add_up_to_the_exact_counts_of_every_record(store, monkeypatch, seed):
    """Three days at about 16,500 queries an hour (the production rate): the core
    ranks 10,000 records per request, yet the stored totals equal an exact
    count of every record in each range."""
    records = generate(seed, T0, 3, 16_500 / 8)  # thinned for test speed; per-slot volumes still far below the cap
    fake = FakeAgent(records)
    now = T0 + 3 * DAY + 2 * SLOT
    collect_all(store, AGENT, fake, now, monkeypatch)
    for since in (T0, T0 + DAY, T0 + 2 * DAY + 5 * HOUR):
        until = history.last_complete_slot_end(now)
        got = store.query('dns', 'records', since, until + HOUR)
        want = exact(records, since, until)
        assert got['records'] == want['records'] and got['blocked_records'] == want['blocked_records']
        for kind, index in (('clients', 'clients'), ('domains', 'domains'), ('blocked', 'blocked')):
            assert [(row['key'], row['count']) for row in got['rows'][kind]] == want[index], (since, kind)
    # A single request over the same three days sees the newest 10,000 only.
    assert len(fake.window(T0, until)) == 10_000 < want['records'] * 1


def test_a_slot_over_the_core_cap_is_split_until_it_fits(store, monkeypatch):
    # 25,000 records in one quarter hour: more than one request can rank.
    records = [(T0 + i * SLOT // 25_000, f'192.0.2.{i % 7 + 1}', f'd{i % 40}.example.test.', False) for i in range(25_000)]
    fake = FakeAgent(records)
    monkeypatch.setattr(monitor, '_request', fake)
    assert history.collect_agent(store, AGENT, ['records'], now=T0 + SLOT + 120, budget=1, pause=0) == 1
    got = store.query('dns', 'records', T0, T0 + HOUR)
    assert got['records'] == 25_000 and got['partial_slots'] == 0
    assert [(row['key'], row['count']) for row in got['rows']['domains']] == exact(records, T0, T0 + SLOT)['domains']
    assert len(fake.calls) > 3 and all(until - since >= history.MIN_SLOT for since, until in fake.calls)


def test_a_minute_still_over_the_cap_is_kept_and_marked_partial(store, monkeypatch):
    records = [(T0 + i * 60 // 12_000, '192.0.2.1', 'busy.example.test.', False) for i in range(12_000)]
    monkeypatch.setattr(monitor, '_request', FakeAgent(records))
    history.collect_agent(store, AGENT, ['records'], now=T0 + SLOT + 120, budget=1, pause=0)
    got = store.query('dns', 'records', T0, T0 + HOUR)
    assert got['partial_slots'] == 1 and got['records'] == 10_000


# --- collecting -----------------------------------------------------------------

def test_collection_takes_the_newest_missing_slots_first_and_never_twice(store, monkeypatch):
    fake = FakeAgent(generate(4, T0, 1, 400))
    monkeypatch.setattr(monitor, '_request', fake)
    now = T0 + DAY + 120
    end = history.last_complete_slot_end(now)
    assert history.collect_agent(store, AGENT, ['records'], now=now, budget=3, pause=0, retention_days=1) == 3
    assert [since for since, _ in fake.calls] == [end - SLOT, end - 2 * SLOT, end - 3 * SLOT]
    fake.calls.clear()
    assert history.collect_agent(store, AGENT, ['records'], now=now, budget=2, pause=0, retention_days=1) == 2
    assert [since for since, _ in fake.calls] == [end - 4 * SLOT, end - 5 * SLOT]
    # Everything a day back, and nothing older than the core keeps.
    while history.collect_agent(store, AGENT, ['records'], now=now, budget=50, pause=0, retention_days=1):
        pass
    assert store.query('dns', 'records', 0, end + HOUR)['slots'] == DAY // SLOT
    fake.calls.clear()
    assert history.collect_agent(store, AGENT, ['records'], now=now, budget=50, pause=0, retention_days=1) == 0
    assert fake.calls == []
    # A slot is asked for only once it ended and the core had a minute to write it.
    fake.calls.clear()
    history.collect_agent(store, AGENT, ['records'], now=end + SLOT + history.SETTLE_SECONDS - 1, pause=0, retention_days=1)
    assert fake.calls == []
    history.collect_agent(store, AGENT, ['records'], now=end + SLOT + history.SETTLE_SECONDS, pause=0, retention_days=1)
    assert fake.calls == [(end, end + SLOT)]


def test_a_failing_device_stops_the_round_and_is_asked_again_later(store, monkeypatch):
    fake = FakeAgent(generate(5, T0, 1, 200))
    fake.fail_at = 3
    monkeypatch.setattr(monitor, '_request', fake)
    now = T0 + DAY + 120
    assert history.collect_agent(store, AGENT, ['records'], now=now, budget=10, pause=0, retention_days=1) == 2
    assert len(fake.calls) == 3
    fake.fail_at = None
    assert history.collect_agent(store, AGENT, ['records'], now=now, budget=10, pause=0, retention_days=1) == 10


def test_adding_the_same_slot_twice_counts_it_once(store):
    slot = {'records': 3, 'blocked': 1, 'partial': False,
            'rows': {'clients': {'192.0.2.1': 3}, 'domains': {'a.test.': 2, 'b.test.': 1}, 'blocked': {'b.test.': 1}}}
    assert store.add_slot('dns', 'records', T0, copy.deepcopy(slot)) is True
    assert store.add_slot('dns', 'records', T0, copy.deepcopy(slot)) is False
    store.add_slot('dns', 'records', T0 + SLOT, copy.deepcopy(slot))
    got = store.query('dns', 'records', T0, T0 + HOUR)
    assert got['records'] == 6 and got['slots'] == 2 and got['blocked_records'] == 2
    assert got['rows']['domains'] == [{'key': 'a.test.', 'count': 4}, {'key': 'b.test.', 'count': 2}]
    assert (got['first'], got['last']) == (T0, T0 + 2 * SLOT)


def test_recorders_and_devices_are_kept_apart(store):
    slot = lambda key: {'records': 1, 'blocked': None, 'partial': False, 'rows': {'clients': {'192.0.2.1': 1}, 'domains': {key: 1}}}
    store.add_slot('dns', 'records', T0, slot('a.test.'))
    store.add_slot('dns', 'other', T0, slot('b.test.'))
    store.add_slot('dns2', 'records', T0, slot('c.test.'))
    assert [row['key'] for row in store.query('dns', 'records', T0, T0 + HOUR)['rows']['domains']] == ['a.test.']
    assert [row['key'] for row in store.query('dns', 'other', T0, T0 + HOUR)['rows']['domains']] == ['b.test.']
    assert store.query('dns', 'records', T0, T0 + HOUR)['blocked_slots'] == 0


# --- keeping ----------------------------------------------------------------------

def test_old_hours_become_days_with_the_same_totals_and_expired_days_go(store, monkeypatch):
    records = generate(6, T0, 2, 600)
    collect_all(store, AGENT, FakeAgent(records), T0 + 2 * DAY + 120, monkeypatch)
    before = store.query('dns', 'records', T0, T0 + 2 * DAY)
    # Ten days later the two days are older than the hourly window.
    store.maintain(['dns'], now=T0 + 12 * DAY)
    with store._connect() as connection:
        kinds = connection.execute('SELECT DISTINCT seconds FROM ranking_periods').fetchall()
    assert kinds == [(DAY,)]
    after = store.query('dns', 'records', T0, T0 + 2 * DAY)
    assert {key: after[key] for key in ('records', 'blocked_records', 'slots')} == {key: before[key] for key in ('records', 'blocked_records', 'slots')}
    assert after['rows'] == before['rows']
    # Merging again changes nothing.
    store.maintain(['dns'], now=T0 + 12 * DAY)
    assert store.query('dns', 'records', T0, T0 + 2 * DAY) == after
    # Past the retention period everything of that time is gone.
    store.maintain(['dns'], now=T0 + (history.RETENTION_DAYS + 3) * DAY)
    assert store.query('dns', 'records', 0, T0 + 4 * DAY)['slots'] == 0


def test_a_removed_device_loses_its_history(store):
    store.add_slot('gone', 'records', T0, {'records': 1, 'blocked': None, 'partial': False, 'rows': {'clients': {'192.0.2.1': 1}}})
    store.add_slot('kept', 'records', T0, {'records': 1, 'blocked': None, 'partial': False, 'rows': {'clients': {'192.0.2.1': 1}}})
    store.maintain(['kept'], now=T0 + HOUR)
    assert store.query('gone', 'records', T0, T0 + HOUR)['slots'] == 0
    assert store.query('kept', 'records', T0, T0 + HOUR)['slots'] == 1


# --- the panel ------------------------------------------------------------------------

def panel_env(tmp_path, monkeypatch, records, now):
    manager = Manager(tmp_path)
    manager.agents['one']['version'] = history.HISTORY_AGENT_VERSION
    store = history.get_history(manager.repository.data_dir)
    collect_all(store, manager.agents['one'], FakeAgent(records), now, monkeypatch)
    current = snapshot(now=now * 1000)
    live = []

    def request(agent, endpoint, params=None, **kwargs):
        if endpoint == 'overview':
            return copy.deepcopy(current)
        live.append(params)
        return ranking(now=now * 1000)
    monkeypatch.setattr(monitor, '_request', request)
    return manager, current, live


def test_the_panel_ranks_from_the_stored_periods_for_each_range(tmp_path, monkeypatch):
    records = generate(7, T0, 3, 500)
    now = T0 + 3 * DAY + 120
    manager, current, live = panel_env(tmp_path, monkeypatch, records, now)
    until = history.last_complete_slot_end(now)
    for range_key, since in (('24h', now - DAY), ('7d', now - 7 * DAY)):
        result = monitor.overview(manager, 'one', MultiDict({'range': range_key}))['rankings']
        since -= since % HOUR
        want = exact(records, max(since, T0), until)
        assert result['source'] == 'history' and result['range'] == range_key
        assert result['clients']['sample_size'] == want['records'] and result['clients']['at_limit'] is False
        assert [(row['key'], row['count']) for row in result['domains']['rows']] == want['domains']
        assert [(row['key'], row['count']) for row in result['blocked']['rows']] == want['blocked']
        assert result['until_ms'] == until * 1000 and result['missing_slots'] == 0
    assert live == []  # the stored periods answer; the core is not asked to rank


def test_the_round_range_starts_at_the_statistics_start(tmp_path, monkeypatch):
    records = generate(10, T0, 2, 400)
    manager = Manager(tmp_path)
    manager.agents['one']['version'] = history.HISTORY_AGENT_VERSION
    start = T0 + 5 * HOUR + 20 * 60
    current = snapshot(now=start * 1000)
    monkeypatch.setattr(monitor, '_request', lambda agent, endpoint, params=None, **kwargs:
                        copy.deepcopy(current) if endpoint == 'overview' else ranking(now=start * 1000))
    first = monitor.overview(manager, 'one')
    assert first['statistics']['started_at_ms'] == start * 1000
    now = T0 + 2 * DAY + 120
    store = history.get_history(manager.repository.data_dir)
    collect_all(store, manager.agents['one'], FakeAgent(records), now, monkeypatch)
    current.update(snapshot(now=now * 1000))
    monkeypatch.setattr(monitor, '_request', lambda agent, endpoint, params=None, **kwargs:
                        copy.deepcopy(current) if endpoint == 'overview' else ranking(now=now * 1000))
    result = monitor.overview(manager, 'one')['rankings']
    # Whole hours: the hour the round started in is counted in full.
    since = start - start % HOUR
    want = exact(records, since, history.last_complete_slot_end(now))
    assert result['range'] == 'round' and result['since_ms'] == since * 1000 and result['first_ms'] == since * 1000
    assert result['clients']['sample_size'] == want['records']
    assert [(row['key'], row['count']) for row in result['domains']['rows']] == want['domains']


def test_the_panel_falls_back_to_the_live_sample_until_periods_exist(tmp_path, monkeypatch):
    manager = Manager(tmp_path)
    manager.agents['one']['version'] = history.HISTORY_AGENT_VERSION
    current, _, calls = __import__('backend.test_oxidns_dashboard', fromlist=['transport']).transport(monkeypatch)
    result = monitor.overview(manager, 'one', MultiDict({'range': '7d'}))['rankings']
    assert result.get('source') != 'history' and result['success'] is True and result['history'] == 'collecting'
    assert any(endpoint == 'rankings' for _, endpoint, _ in calls)


def test_an_older_agent_keeps_the_live_sample(tmp_path, monkeypatch):
    records = generate(8, T0, 1, 300)
    now = T0 + DAY + 120
    manager, current, live = panel_env(tmp_path, monkeypatch, records, now)
    manager.agents['one']['version'] = '1.8.54-go'
    result = monitor.overview(manager, 'one')['rankings']
    assert result.get('source') != 'history' and live
    assert result['history'] == 'upgrade_required' and result['history_version'] == history.HISTORY_AGENT_VERSION


def test_the_panel_says_which_slots_are_missing(tmp_path, monkeypatch):
    records = generate(9, T0, 1, 300)
    now = T0 + DAY + 120
    manager, _, _ = panel_env(tmp_path, monkeypatch, records, now)
    store = history.get_history(manager.repository.data_dir)
    with store._connect() as connection:  # the device was away for two hours
        connection.execute('DELETE FROM ranking_slots WHERE start >= ? AND start < ?', (T0 + 5 * HOUR, T0 + 7 * HOUR))
        connection.execute('DELETE FROM ranking_periods WHERE start >= ? AND start < ?', (T0 + 5 * HOUR, T0 + 7 * HOUR))
    result = monitor.overview(manager, 'one', MultiDict({'range': '24h'}))['rankings']
    assert result['missing_slots'] == 8


@pytest.mark.parametrize('value', ['1h', '', 'ALL', '30'])
def test_unknown_ranges_are_refused(tmp_path, monkeypatch, value):
    manager = Manager(tmp_path)
    __import__('backend.test_oxidns_dashboard', fromlist=['transport']).transport(monkeypatch)
    with pytest.raises(monitor.MonitorError) as error:
        monitor.overview(manager, 'one', MultiDict({'range': value}))
    assert error.value.code == 'INVALID_QUERY'


def test_a_collection_round_covers_usable_new_oxidns_devices_only(tmp_path, monkeypatch):
    seen = []
    monkeypatch.setattr(history, '_recorders', lambda agent: ['records'])
    monkeypatch.setattr(history, 'collect_agent', lambda store, agent, recorders, now=None: seen.append(agent['id']))
    monkeypatch.setattr('backend.agents.manager.is_usable', lambda agent: agent.get('enabled') is not False)
    agents = [
        {'id': 'new', 'service_type': 'oxidns', 'version': history.HISTORY_AGENT_VERSION},
        {'id': 'old', 'service_type': 'oxidns', 'version': '1.8.54-go'},
        {'id': 'off', 'service_type': 'oxidns', 'version': '1.8.60-go', 'enabled': False},
        {'id': 'mihomo', 'service_type': 'mihomo', 'version': '1.9.0-go'},
    ]
    manager = SimpleNamespace(repository=SimpleNamespace(data_dir=Path(tmp_path)),
                              get_all_agents=lambda history=True: copy.deepcopy(agents))
    history.collect_round(manager, now=T0)
    assert seen == ['new']


def test_a_round_without_a_finished_period_says_other_ranges_are_complete(tmp_path, monkeypatch):
    """A round that began minutes ago has no period yet; the stored days are still there."""
    records = generate(12, T0, 1, 300)
    now = T0 + DAY + 120
    manager, current, live = panel_env(tmp_path, monkeypatch, records, now)
    later = now + 5 * 60
    current.update(snapshot(now=later * 1000))
    monkeypatch.setattr(monitor, '_request', lambda agent, endpoint, params=None, **kwargs:
                        copy.deepcopy(current) if endpoint == 'overview' else ranking(now=later * 1000))
    monitor.reset_dashboard(manager, 'one', {})  # the round now starts after the newest stored period
    result = monitor.overview(manager, 'one')['rankings']
    assert result.get('source') != 'history' and result['history'] == 'pending_range'
    assert monitor.overview(manager, 'one', MultiDict({'range': '24h'}))['rankings']['source'] == 'history'

