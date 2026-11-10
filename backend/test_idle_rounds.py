"""A background round is skipped only while the generation its last idle round read is current."""
import gc

import pytest

from backend.agents import idle_rounds
from backend.agents.idle_rounds import IdleRounds
from backend.common.config_repository import ProfileRepository


@pytest.fixture
def repository(tmp_path):
    return ProfileRepository(tmp_path)


def commit(repository, value='x'):
    repository.update_profile_fields('default', {'description': value})


def test_round_runs_until_one_reports_idle_and_again_after_every_commit(repository):
    rounds = IdleRounds()
    head = rounds.due(repository)
    assert head == repository.head()
    # Without a report, or after a busy round, every round is due.
    assert rounds.due(repository) == head
    rounds.done(repository, head, False)
    assert rounds.due(repository) == head
    rounds.done(repository, head, True)
    assert rounds.due(repository) is None and rounds.due(repository) is None
    commit(repository)
    after = rounds.due(repository)
    assert after == repository.head() != head
    rounds.done(repository, after, True)
    assert rounds.due(repository) is None
    # A busy round forgets the idle one even at the same generation.
    rounds.done(repository, after, False)
    assert rounds.due(repository) == after


def test_commit_during_the_round_makes_the_next_round_run(repository):
    rounds = IdleRounds()
    head = rounds.due(repository)
    commit(repository)  # another worker saves while this round reads
    rounds.done(repository, head, True)
    assert rounds.due(repository) == repository.head()


def test_write_by_another_process_is_noticed(repository, tmp_path):
    rounds = IdleRounds()
    rounds.done(repository, rounds.due(repository), True)
    assert rounds.due(repository) is None
    commit(ProfileRepository(tmp_path), 'elsewhere')
    assert rounds.due(repository) == repository.head()


def test_idle_round_is_repeated_after_the_recheck_interval_and_after_a_clock_step_back(repository):
    now = [1000.0]
    rounds = IdleRounds(clock=lambda: now[0])
    rounds.done(repository, rounds.due(repository), True)
    now[0] += idle_rounds.RECHECK_SECONDS - 1
    assert rounds.due(repository) is None
    now[0] += 1
    head = rounds.due(repository)
    assert head == repository.head()
    rounds.done(repository, head, True)
    assert rounds.due(repository) is None
    now[0] -= 5000
    assert rounds.due(repository) == head


def test_repositories_and_loops_do_not_share_idle_state(repository, tmp_path_factory):
    first, second = IdleRounds(), IdleRounds()
    other = ProfileRepository(tmp_path_factory.mktemp('other'))
    first.done(repository, first.due(repository), True)
    assert first.due(repository) is None
    assert second.due(repository) is not None and first.due(other) is not None
    # Nothing keeps a discarded repository (and its cached document) alive.
    first.done(other, first.due(other), True)
    assert len(first._idle) == 2
    del other
    gc.collect()
    assert len(first._idle) == 1
