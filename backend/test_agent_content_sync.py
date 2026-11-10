"""Content status is bounded, snapshot-based and never downloads in list requests."""
from concurrent.futures import Future, ThreadPoolExecutor
from copy import deepcopy
import json
import threading

import pytest

from backend.agents.content_sync import ContentSyncChecker
from backend.agents.deployment_bundle import build_deployment_bundle, deployment_content_fingerprint


class DeferredExecutor:
    def __init__(self):
        self.jobs = []

    def submit(self, function, *args):
        future = Future()
        self.jobs.append((future, function, args))
        return future

    def run(self, index=0):
        future, function, args = self.jobs.pop(index)
        future.set_running_or_notify_cancel()
        try:
            result = function(*args)
        except BaseException as error:
            future.set_exception(error)
            raise
        future.set_result(result)


@pytest.fixture
def setup():
    executor = DeferredExecutor()
    now = [100.0]
    checker = ContentSyncChecker(executor=executor, clock=lambda: now[0])
    config = {'profile_id': 'default', 'system_config': {'server_domain': 'https://config.example.test'},
              'test_main': 'mode: rule\nrules: []\n', 'test_rule': 'DOMAIN,example.test\n'}
    agent = {'id': 'agent-test', 'profile_id': 'default', 'service_type': 'mihomo', 'config_version': 'applied-config'}
    calls = []

    def bundle_for(item, snapshot, *, base_url='', deployment_id='previous'):
        return build_deployment_bundle(item, snapshot, snapshot['test_main'],
                                       custom_files=[{'path': 'rules/example.list', 'content': snapshot['test_rule']}],
                                       base_url=base_url, deployment_id=deployment_id)

    previous = bundle_for(agent, config)
    agent['applied_content'] = {
        'fingerprint': deployment_content_fingerprint(previous.manifest), 'profile_id': 'default',
        'service_type': 'mihomo', 'config_version': 'applied-config', 'deployment_id': 'previous',
        'base_url': 'https://config.example.test', 'config_path': 'config.yaml',
    }

    def prepare(item, snapshot, **kwargs):
        calls.append((deepcopy(item), deepcopy(snapshot), kwargs))
        return bundle_for(item, snapshot, **kwargs)

    return checker, executor, now, agent, config, prepare, calls


def query(setup, **kwargs):
    checker, _, _, agent, config, prepare, _ = setup
    return checker.get_content_sync(agent, config, 'https://config.example.test', prepare, **kwargs)


def test_list_only_schedules_and_repeated_polls_share_one_preparation(setup):
    _, executor, _, _, _, _, calls = setup
    for _ in range(20):
        assert query(setup) == {'status': 'checking'}
    assert calls == [] and len(executor.jobs) == 1
    executor.run()
    result = query(setup)
    assert result['status'] == 'synced' and result['checked_at'].endswith('Z')
    for _ in range(20):
        assert query(setup) == result
    assert len(calls) == 1 and executor.jobs == []
    result['status'] = 'changed'
    assert query(setup)['status'] == 'synced'


def test_only_rule_file_change_is_detected_with_unchanged_main_config(setup):
    _, executor, _, _, config, _, _ = setup
    query(setup)
    executor.run()
    config['test_rule'] += 'DOMAIN,other.example.test\n'
    assert query(setup)['status'] == 'checking'
    executor.run()
    assert query(setup)['status'] == 'changed'


def test_runtime_metadata_and_heartbeat_do_not_invalidate_cache(setup):
    _, executor, _, agent, config, _, calls = setup
    query(setup)
    executor.run()
    agent.update(last_heartbeat='later', cpu=90, version='new-agent')
    config.update(_revision=1234, updated_at='later', created_at='different', agents=[agent],
                  name='renamed', description='updated', backup={'last_run': 'later'})
    assert query(setup)['status'] == 'synced'
    assert len(calls) == 1 and executor.jobs == []


def test_nested_config_fields_are_not_silently_removed_from_cache_key(setup):
    _, executor, _, _, config, _, _ = setup
    config['test_plugin'] = {'created_at': 'first'}
    query(setup)
    executor.run()
    config['test_plugin']['created_at'] = 'second'
    assert query(setup)['status'] == 'checking'
    assert len(executor.jobs) == 1


def test_remote_changes_are_rechecked_only_after_success_ttl(setup):
    _, executor, now, _, _, _, calls = setup
    query(setup)
    executor.run()
    now[0] += 899
    assert query(setup)['status'] == 'synced'
    now[0] += 1
    assert query(setup)['status'] == 'checking'
    assert len(calls) == 1 and len(executor.jobs) == 1
    executor.run()
    assert len(calls) == 2


def test_changed_input_never_publishes_old_worker_result(setup):
    _, executor, _, _, config, _, calls = setup
    query(setup)
    config['test_rule'] = 'DOMAIN,new.example.test\n'
    assert query(setup)['status'] == 'checking'
    assert len(executor.jobs) == 1
    executor.run()
    assert calls[0][1]['test_rule'] == 'DOMAIN,example.test\n'
    assert query(setup)['status'] == 'checking'
    assert len(executor.jobs) == 1
    executor.run()
    assert query(setup)['status'] == 'changed'


def test_new_successful_baseline_invalidates_in_flight_comparison(setup):
    _, executor, _, agent, _, _, _ = setup
    query(setup)
    agent['config_version'] = agent['applied_content']['config_version'] = 'new-success'
    agent['applied_content']['deployment_id'] = 'new-success-id'
    query(setup)
    executor.run()
    assert query(setup)['status'] == 'checking'
    executor.run()
    assert query(setup)['status'] == 'synced'


def test_returning_to_original_input_still_discards_obsolete_worker(setup):
    _, executor, _, _, config, _, _ = setup
    original = config['test_rule']
    query(setup)
    config['test_rule'] = 'DOMAIN,temporary.example.test\n'
    query(setup)
    config['test_rule'] = original
    query(setup)
    executor.run()
    assert query(setup)['status'] == 'checking'
    executor.run()
    assert query(setup)['status'] == 'synced'


def test_generation_system_settings_and_base_url_invalidate_cached_result(setup):
    checker, executor, _, agent, config, prepare, _ = setup
    query(setup)
    executor.run()
    config['system_config']['server_domain'] = 'https://new.example.test'
    assert query(setup)['status'] == 'checking'
    executor.run()
    assert query(setup)['status'] == 'synced'
    assert checker.get_content_sync(agent, config, 'https://another.example.test', prepare)['status'] == 'checking'


@pytest.mark.parametrize('status', ['ready', 'preparing', 'uploading', 'checking', 'unknown', 'rollback_failed'])
def test_incomplete_deployment_invalidates_in_flight_success(setup, status):
    _, executor, _, agent, _, _, _ = setup
    query(setup)
    agent['latest_deployment'] = {'status': status}
    assert query(setup) == {'status': 'unknown', 'reason': 'deployment_pending'}
    executor.run()
    assert query(setup)['status'] == 'unknown'
    agent['latest_deployment'] = {'status': 'rolled_back'}
    assert query(setup)['status'] == 'checking'


@pytest.mark.parametrize('status', ['failed', 'rolled_back'])
def test_failed_deployment_can_still_compare_confirmed_prior_baseline(setup, status):
    _, executor, _, agent, _, _, _ = setup
    agent['latest_deployment'] = {'status': status}
    query(setup)
    executor.run()
    assert query(setup)['status'] == 'synced'


@pytest.mark.parametrize('fingerprint', ['', 'a' * 64, 'v2:' + 'a' * 64, 'v1:' + 'a' * 63, 'v1:' + 'A' * 64])
def test_invalid_baseline_fingerprint_never_starts_preparation(setup, fingerprint):
    _, executor, _, agent, _, _, _ = setup
    agent['applied_content']['fingerprint'] = fingerprint
    assert query(setup) == {'status': 'unknown', 'reason': 'baseline_invalid'}
    assert executor.jobs == []


@pytest.mark.parametrize('change, reason', [
    ({'profile_id': 'other'}, 'profile_mismatch'),
    ({'service_type': 'mosdns'}, 'service_mismatch'),
    ({'config_version': 'different'}, 'version_mismatch'),
])
def test_binding_service_or_reported_version_mismatch_is_unknown(setup, change, reason):
    _, executor, _, agent, _, _, _ = setup
    agent.update(change)
    assert query(setup) == {'status': 'unknown', 'reason': reason}
    assert executor.jobs == []


def test_missing_baseline_is_not_backfilled_from_current_config(setup):
    _, executor, _, agent, _, _, _ = setup
    del agent['applied_content']
    assert query(setup) == {'status': 'unknown', 'reason': 'baseline_missing'}
    agent['config_version'] = '0'
    agent['deployments'] = {'previous': {'status': 'succeeded'}}
    assert query(setup)['reason'] == 'baseline_missing'
    agent['deployments'] = {'previous': {'status': 'failed'}}
    assert query(setup) == {'status': 'never-pushed'}
    assert executor.jobs == []


def test_error_is_secret_free_and_uses_short_ttl(setup):
    checker, executor, now, agent, config, _, _ = setup
    secret = 'https://user:password@private.example.test/?token=secret'

    def fail(*_args, **_kwargs):
        raise ValueError(secret)

    def get():
        return checker.get_content_sync(agent, config, '', fail)

    assert get()['status'] == 'checking'
    executor.run()
    result = get()
    assert result == {'status': 'unknown', 'reason': 'check_failed'}
    assert secret not in json.dumps(result)
    now[0] += 29
    assert get() == result and executor.jobs == []
    now[0] += 1
    assert get()['status'] == 'checking' and len(executor.jobs) == 1


def test_workers_are_bounded_without_queued_jobs(setup):
    checker, executor, _, agent, config, prepare, _ = setup
    for index in range(30):
        item = {**agent, 'id': f'agent-{index}'}
        assert checker.get_content_sync(item, config, '', prepare)['status'] == 'checking'
    assert len(executor.jobs) == 2
    executor.run()
    item = {**agent, 'id': 'agent-29'}
    assert checker.get_content_sync(item, config, '', prepare)['status'] == 'checking'
    assert len(executor.jobs) == 2


def test_repository_scope_isolates_identical_agent_ids(setup):
    _, executor, _, _, _, _, _ = setup
    assert query(setup, scope='repository-one')['status'] == 'checking'
    assert query(setup, scope='repository-two')['status'] == 'checking'
    assert len(executor.jobs) == 2
    executor.run()
    assert query(setup, scope='repository-one')['status'] == 'synced'
    assert query(setup, scope='repository-two')['status'] == 'checking'


def test_repository_change_before_worker_starts_skips_preparation(setup, monkeypatch):
    from backend.common import config as config_module
    _, executor, _, _, _, _, calls = setup
    original, replacement = object(), object()
    monkeypatch.setattr(config_module, '_repository', original)
    assert query(setup, repository=original)['status'] == 'checking'
    monkeypatch.setattr(config_module, '_repository', replacement)
    executor.run()
    assert calls == []
    # Restore the request's repository to inspect its rejected worker result.
    monkeypatch.setattr(config_module, '_repository', original)
    assert query(setup, repository=original) == {'status': 'unknown', 'reason': 'check_failed'}


def test_repository_change_during_preparation_never_publishes_success(setup, monkeypatch):
    from backend.common import config as config_module
    checker, executor, _, agent, config, prepare, _ = setup
    original, replacement = object(), object()
    monkeypatch.setattr(config_module, '_repository', original)

    def switching_prepare(*args, **kwargs):
        bundle = prepare(*args, **kwargs)
        monkeypatch.setattr(config_module, '_repository', replacement)
        return bundle

    assert checker.get_content_sync(agent, config, '', switching_prepare, repository=original)['status'] == 'checking'
    executor.run()
    monkeypatch.setattr(config_module, '_repository', original)
    assert checker.get_content_sync(agent, config, '', switching_prepare, repository=original) == {
        'status': 'unknown', 'reason': 'check_failed',
    }


def test_same_scope_does_not_reuse_another_repository_object_result(setup, monkeypatch):
    from backend.common import config as config_module
    _, executor, _, _, _, _, calls = setup
    monkeypatch.setattr(config_module, '_repository', object())
    query(setup, scope='same-directory')
    executor.run()
    assert query(setup, scope='same-directory')['status'] == 'synced'
    monkeypatch.setattr(config_module, '_repository', object())
    assert query(setup, scope='same-directory')['status'] == 'checking'
    assert len(calls) == 1
    executor.run()
    assert query(setup, scope='same-directory')['status'] == 'synced'
    assert len(calls) == 2


def test_uninitialized_repository_is_not_created_for_standalone_checker(setup, monkeypatch):
    from backend.common import config as config_module
    _, executor, _, _, _, _, _ = setup
    monkeypatch.setattr(config_module, '_repository', None)
    monkeypatch.setattr(config_module, 'get_repository',
                        lambda: pytest.fail('content checker must not initialize a repository'))
    assert query(setup)['status'] == 'checking'
    executor.run()
    assert query(setup)['status'] == 'synced'


def test_stale_request_repository_is_rejected_before_scheduling(setup, monkeypatch):
    from backend.common import config as config_module
    _, executor, _, _, _, _, _ = setup
    monkeypatch.setattr(config_module, '_repository', object())
    assert query(setup, repository=object()) == {'status': 'unknown', 'reason': 'check_failed'}
    assert executor.jobs == []


def test_custom_installed_filename_has_same_content_fingerprint(setup):
    _, executor, _, agent, _, _, _ = setup
    agent['applied_content']['config_path'] = 'custom.conf'
    query(setup)
    executor.run()
    assert query(setup)['status'] == 'synced'


def test_worker_resets_reused_config_context_before_and_after(setup, monkeypatch):
    checker, executor, _, agent, config, prepare, _ = setup
    from backend.common import config as config_module
    resets = []
    monkeypatch.setattr(config_module, 'reset_config_context', lambda: resets.append('reset'))

    def checked_prepare(*args, **kwargs):
        assert resets == ['reset']
        return prepare(*args, **kwargs)

    checker.get_content_sync(agent, config, '', checked_prepare)
    assert resets == []
    executor.run()
    assert resets == ['reset', 'reset']


def test_system_exit_is_not_converted_to_unknown_and_releases_slot(setup):
    checker, executor, _, agent, config, prepare, _ = setup

    def stop(*_args, **_kwargs):
        raise SystemExit('stop')

    checker.get_content_sync(agent, config, '', stop)
    with pytest.raises(SystemExit):
        executor.run()
    assert checker.get_content_sync(agent, config, '', prepare)['status'] == 'checking'
    assert len(executor.jobs) == 1


def test_rejected_executor_submission_does_not_leak_error_or_slot(setup):
    _, _, now, agent, config, prepare, _ = setup

    class RejectOnce(DeferredExecutor):
        rejected = False

        def submit(self, *args):
            if not self.rejected:
                self.rejected = True
                raise RuntimeError('private credentials')
            return super().submit(*args)

    executor = RejectOnce()
    checker = ContentSyncChecker(executor=executor, clock=lambda: now[0], max_workers=1)
    assert checker.get_content_sync(agent, config, '', prepare) == {'status': 'unknown', 'reason': 'check_failed'}
    now[0] += 30
    assert checker.get_content_sync(agent, config, '', prepare)['status'] == 'checking'
    executor.run()
    assert checker.get_content_sync(agent, config, '', prepare)['status'] == 'synced'


def test_real_executor_runs_preparation_off_request_thread(setup):
    _, _, _, agent, config, prepare, _ = setup
    started, release = threading.Event(), threading.Event()
    request_thread = threading.get_ident()
    worker_threads = []

    def slow_prepare(*args, **kwargs):
        worker_threads.append(threading.get_ident())
        started.set()
        assert release.wait(5)
        return prepare(*args, **kwargs)

    with ThreadPoolExecutor(max_workers=2) as executor:
        checker = ContentSyncChecker(executor=executor)
        try:
            assert checker.get_content_sync(agent, config, '', slow_prepare)['status'] == 'checking'
            assert started.wait(5)
            for _ in range(20):
                assert checker.get_content_sync(agent, config, '', slow_prepare)['status'] == 'checking'
            assert worker_threads == [worker_threads[0]] and worker_threads[0] != request_thread
        finally:
            release.set()


def test_concurrent_list_requests_schedule_one_job(setup):
    _, executor, _, _, _, _, _ = setup
    with ThreadPoolExecutor(max_workers=8) as callers:
        results = list(callers.map(lambda _: query(setup), range(32)))
    assert results == [{'status': 'checking'}] * 32
    assert len(executor.jobs) == 1
    executor.run()
    assert query(setup)['status'] == 'synced'


def compare(setup, **kwargs):
    checker, _, _, agent, config, prepare, _ = setup
    return checker.compare_now(agent, config, 'https://config.example.test', prepare, **kwargs)


def test_compare_now_answers_in_the_callers_thread_and_shares_the_cache(setup):
    _, executor, _, _, config, _, calls = setup
    result = compare(setup)
    assert result['status'] == 'synced' and len(calls) == 1 and executor.jobs == []
    # The list then reads the same answer without scheduling a worker.
    assert query(setup) == result and executor.jobs == []
    config['test_rule'] += 'DOMAIN,other.example.test\n'
    assert compare(setup)['status'] == 'changed' and len(calls) == 2
    assert query(setup)['status'] == 'changed' and executor.jobs == []


def test_compare_now_reuses_a_fresh_list_result_until_it_expires(setup):
    _, executor, now, _, _, _, calls = setup
    query(setup)
    executor.run()
    assert compare(setup)['status'] == 'synced' and len(calls) == 1
    now[0] += 900
    assert compare(setup)['status'] == 'synced' and len(calls) == 2


def test_compare_now_metadata_alone_is_still_synced(setup):
    _, _, _, _, config, _, calls = setup
    assert compare(setup)['status'] == 'synced'
    # A profile's revision and timestamps are not inputs to the delivered files.
    config.update(_revision=99, updated_at='later')
    assert compare(setup)['status'] == 'synced' and len(calls) == 1


@pytest.mark.parametrize('change, reason', [
    ({'applied_content': None, 'config_version': 'applied-config'}, 'baseline_missing'),
    ({'latest_deployment': {'deployment_id': 'next', 'status': 'uploading'}}, 'deployment_pending'),
])
def test_compare_now_never_claims_a_match_without_a_confirmed_baseline(setup, change, reason):
    _, _, _, agent, _, _, calls = setup
    agent.update(change)
    assert compare(setup) == {'status': 'unknown', 'reason': reason}
    assert calls == []


def test_compare_now_preparation_failure_is_secret_free_unknown(setup):
    checker, _, _, agent, config, _, _ = setup
    def failing(*_args, **_kwargs):
        raise ValueError('token=private-secret https://user:pass@example.test')
    assert checker.compare_now(agent, config, '', failing) == {'status': 'unknown', 'reason': 'check_failed'}


def test_compare_now_rejects_another_repository(setup, monkeypatch):
    from backend.common import config as config_module
    _, _, _, _, _, _, calls = setup
    monkeypatch.setattr(config_module, '_repository', object())
    assert compare(setup, repository=object()) == {'status': 'unknown', 'reason': 'check_failed'}
    assert calls == []


# --- The list reads its inputs and copies them only for a worker ------------

@pytest.fixture
def copies(monkeypatch):
    """Every deepcopy the checker makes, by the copied object's identity."""
    from backend.agents import content_sync
    made = []

    def counting(value):
        made.append(id(value))
        return deepcopy(value)
    monkeypatch.setattr(content_sync, 'deepcopy', counting)
    return made


def test_a_kept_answer_and_a_running_check_copy_nothing(setup, copies):
    """The Agent page polls the list; a poll that finds an answer, or a check
    under way, must not copy the whole profile for each device."""
    _, executor, now, agent, config, _, _ = setup
    assert query(setup) == {'status': 'checking'}
    assert copies == [id(agent), id(config)]  # for the worker, once
    del copies[:]
    for _ in range(5):
        assert query(setup) == {'status': 'checking'}
    executor.run()
    for _ in range(5):
        assert query(setup)['status'] == 'synced'
    agent.update(last_heartbeat='later')
    assert query(setup)['status'] == 'synced'
    assert copies == []
    now[0] += 900
    assert query(setup) == {'status': 'checking'}
    assert copies == [id(agent), id(config)]


@pytest.mark.parametrize('change', [
    {'latest_deployment': {'deployment_id': 'next', 'status': 'uploading'}},
    {'applied_content': None},
    {'config_version': 'another'},
])
def test_an_unavailable_comparison_copies_nothing(setup, copies, change):
    _, executor, _, agent, _, _, _ = setup
    agent.update(change)
    assert query(setup)['status'] == 'unknown'
    assert copies == [] and executor.jobs == []


def test_the_list_never_changes_what_it_was_given(setup):
    _, executor, _, agent, config, _, _ = setup
    agent['deployments'] = {'previous': {'deployment_id': 'previous', 'status': 'succeeded'}}
    before = deepcopy((agent, config))
    query(setup)
    executor.run()
    query(setup)
    assert (agent, config) == before


def test_the_worker_keeps_the_values_of_the_request_that_started_it(setup):
    """A request's values are its own: changing them after the answer, as the
    route does when it finishes the public record, must not reach the worker."""
    _, executor, _, agent, config, _, calls = setup
    expected = deepcopy((agent, config))
    assert query(setup) == {'status': 'checking'}
    (_, _, (_, _, _, worker_agent, worker_config, _, _)), = executor.jobs
    assert worker_agent is not agent and worker_config is not config
    agent['service_type'] = 'mosdns'
    agent['applied_content']['fingerprint'] = 'v1:' + '0' * 64
    config['test_rule'] = 'DOMAIN,changed-after-the-answer.test\n'
    config['system_config']['server_domain'] = 'https://changed.test'
    executor.run()
    assert calls[0][:2] == expected


def test_devices_of_one_profile_get_separate_copies_of_it(setup):
    """The route passes one profile snapshot for all devices bound to it; two
    workers preparing at the same time must not share it."""
    checker, executor, _, agent, config, prepare, _ = setup
    other = {**deepcopy(agent), 'id': 'agent-other'}
    for item in (agent, other):
        assert checker.get_content_sync(item, config, 'https://config.example.test', prepare) == {'status': 'checking'}
    first, second = (job[2] for job in executor.jobs)
    assert first[4] is not second[4] and first[4] is not config and first[4] == second[4] == config
    assert first[3]['id'] == 'agent-test' and second[3]['id'] == 'agent-other'


def test_inputs_that_cannot_be_copied_start_no_worker_and_keep_no_slot(setup, monkeypatch):
    from backend.agents import content_sync
    checker, executor, _, agent, config, prepare, _ = setup

    def failing(value):
        raise TypeError('cannot copy')
    monkeypatch.setattr(content_sync, 'deepcopy', failing)
    for _ in range(4):  # more than the two worker slots
        assert query(setup) == {'status': 'unknown', 'reason': 'invalid_inputs'}
    assert executor.jobs == [] and not checker._running and not checker._entries
    monkeypatch.setattr(content_sync, 'deepcopy', deepcopy)
    assert query(setup) == {'status': 'checking'}
    executor.run()
    assert query(setup)['status'] == 'synced'


def test_requests_arriving_while_the_copies_are_made_start_no_second_worker(setup, monkeypatch):
    """The copies are made outside the lock; the slot is reserved before them."""
    from backend.agents import content_sync
    checker, executor, _, agent, config, prepare, _ = setup
    copying, proceed = threading.Event(), threading.Event()

    def slow(value):
        copying.set()
        assert proceed.wait(10)
        return deepcopy(value)
    monkeypatch.setattr(content_sync, 'deepcopy', slow)
    answers = []
    first = threading.Thread(target=lambda: answers.append(query(setup)))
    first.start()
    assert copying.wait(10)
    monkeypatch.setattr(content_sync, 'deepcopy', deepcopy)
    # Not blocked by the first request, and no second job.
    assert query(setup) == {'status': 'checking'}
    assert executor.jobs == []
    # An invalidation in between only makes the coming worker's answer land nowhere.
    checker.invalidate()
    proceed.set()
    first.join(10)
    assert answers == [{'status': 'checking'}] and len(executor.jobs) == 1
    executor.run()
    assert not checker._running
    assert query(setup) == {'status': 'checking'}  # the invalidated answer was not kept
    executor.run()
    assert query(setup)['status'] == 'synced'


class _CopyFirstChecker(ContentSyncChecker):
    """get_content_sync as it was: both inputs copied on every request."""

    def get_content_sync(self, agent, config, base_url, prepare, *, scope='', repository=None):
        from backend.agents import content_sync
        if not isinstance(agent, dict) or not isinstance(config, dict) or not isinstance(agent.get('id'), str):
            return content_sync._unknown('invalid_inputs')
        identity = (scope, agent['id'])
        repository = content_sync._current_repository()
        try:
            agent_snapshot, config_snapshot = deepcopy(agent), deepcopy(config)
            unavailable = content_sync._precondition(agent_snapshot, config_snapshot)
            key = None if unavailable else content_sync._input_key(agent_snapshot, config_snapshot, base_url)
        except Exception:
            unavailable, key = content_sync._unknown('invalid_inputs'), None
        with self._lock:
            if unavailable:
                self._entries.pop(identity, None)
                return unavailable
            entry = self._entries.get(identity)
            if entry is None or entry.key != key or entry.repository is not repository:
                entry = content_sync._Entry(key, repository)
                self._entries[identity] = entry
            self._entries.move_to_end(identity)
            self._trim()
            if entry.result is not None and self._clock() < entry.expires:
                return dict(entry.result)
            if identity in self._running:
                return {'status': 'checking'}
            if not self._slots.acquire(blocking=False):
                return {'status': 'checking'}
            token = object()
            self._running[identity] = token
            self._executor.submit(self._check, identity, entry, token, agent_snapshot, config_snapshot, base_url, prepare)
            return {'status': 'checking'}


def test_random_histories_answer_as_when_every_request_copied_first():
    """Three devices on one shared profile through the same random histories
    of edits, publications, failures, time and invalidations, once with the
    checker as it is and once copying first: every answer, every scheduled
    worker and everything a worker was given must be the same."""
    import random

    def world(checker_class):
        executor, now, calls, failing = DeferredExecutor(), [100.0], [], [False]
        checker = checker_class(executor=executor, clock=lambda: now[0])
        config = {'profile_id': 'default', 'system_config': {'server_domain': 'https://config.example.test'},
                  'test_main': 'mode: rule\nrules: []\n', 'test_rule': 'DOMAIN,example.test\n'}

        def bundle_for(item, snapshot, *, base_url='', deployment_id='previous'):
            return build_deployment_bundle(item, snapshot, snapshot['test_main'],
                                           custom_files=[{'path': 'rules/example.list', 'content': snapshot['test_rule']}],
                                           base_url=base_url, deployment_id=deployment_id)

        def prepare(item, snapshot, **kwargs):
            calls.append((deepcopy(item), deepcopy(snapshot), kwargs.get('base_url')))
            if failing[0]:
                raise ValueError('preparation failed')
            return bundle_for(item, snapshot, **kwargs)

        agents = []
        for number in range(3):
            agent = {'id': f'agent-{number}', 'profile_id': 'default', 'service_type': 'mihomo',
                     'config_version': 'applied-config'}
            agent['applied_content'] = {
                'fingerprint': deployment_content_fingerprint(bundle_for(agent, config).manifest), 'profile_id': 'default',
                'service_type': 'mihomo', 'config_version': 'applied-config', 'deployment_id': 'previous',
                'base_url': 'https://config.example.test', 'config_path': 'config.yaml'}
            agents.append(agent)
        return checker, executor, now, agents, config, prepare, calls, failing

    def step(state, action, choice, amount, index, counter):
        checker, executor, now, agents, config, prepare, calls, failing = state
        agent, answer = agents[choice], None
        if action == 'poll':
            # One request lists every device against one profile snapshot.
            answer = [checker.get_content_sync(item, config, item['applied_content']['base_url'], prepare) for item in agents]
            for item in answer:
                item.pop('checked_at', None)
        elif action == 'run' and executor.jobs:
            executor.run(index % len(executor.jobs))
        elif action == 'rule':
            config['test_rule'] = ('DOMAIN,example.test\n', 'DOMAIN,other.test\n', 'DOMAIN,third.test\n')[choice]
        elif action == 'main':
            config['test_main'] = f'mode: rule\nrules: []\n# {choice}\n'
        elif action == 'pending':
            agent['latest_deployment'] = {'deployment_id': 'next', 'status': 'uploading'}
        elif action == 'settled':
            agent['latest_deployment'] = {'deployment_id': 'next', 'status': ('succeeded', 'failed', 'rolled_back')[choice]}
        elif action == 'heartbeat':
            agent.update(last_heartbeat=str(counter), cpu=choice)
            config['_revision'] = counter
        elif action == 'time':
            now[0] += amount
        elif action == 'invalidate':
            checker.invalidate()
        elif action in ('fail', 'recover'):
            failing[0] = action == 'fail'
        elif action == 'base_url':
            agent['applied_content']['base_url'] = ('https://config.example.test', 'https://second.test')[choice % 2]
        elif action == 'version':
            agent['config_version'] = ('applied-config', 'reported-another')[choice % 2]
        queued = [(args[0], args[3], args[4], args[5]) for _future, _function, args in executor.jobs]
        return answer, queued, calls, sorted(checker._running), checker._slots._value

    actions = ['poll'] * 8 + ['run'] * 4 + ['rule', 'main', 'pending', 'settled', 'heartbeat', 'time', 'invalidate',
                                            'fail', 'recover', 'base_url', 'version']
    seen = set()
    for seed in range(12):
        ours, reference = world(ContentSyncChecker), world(_CopyFirstChecker)
        random_ = random.Random(seed)
        for counter in range(400):
            move = (random_.choice(actions), random_.randrange(3), random_.choice([1, 20, 31, 400, 901]),
                    random_.randrange(1 << 30), counter)
            result = step(ours, *move)
            assert result == step(reference, *move), (seed, counter, move[0])
            seen.update(json.dumps(item, sort_keys=True) for item in result[0] or ())
    # The histories went through every kind of answer.
    assert seen >= {json.dumps(item, sort_keys=True) for item in (
        {'status': 'checking'}, {'status': 'synced'}, {'status': 'changed'},
        {'status': 'unknown', 'reason': 'check_failed'}, {'status': 'unknown', 'reason': 'deployment_pending'},
        {'status': 'unknown', 'reason': 'version_mismatch'})}
