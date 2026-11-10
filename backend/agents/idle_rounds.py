"""Let a periodic background round skip while nothing it depends on changed.

A round that found nothing to do, and that looked only at the stored
configuration, finds the same nothing until the configuration is committed
again. Such a round records the generation it started from; while that
generation is current, the following rounds return without reading the Agents,
whose records carry their whole deployment history.

A round with work in flight, or one that depends on the clock or on a device,
must report that it was not idle, and then every following round runs.
"""
import threading
import time
import weakref

# An idle round is still repeated this often, for anything a loop reads that
# is not in the configuration database.
RECHECK_SECONDS = 600


class IdleRounds:
    def __init__(self, *, clock=time.monotonic):
        self._clock = clock
        self._idle = weakref.WeakKeyDictionary()  # repository -> (generation, when the idle round ran)
        self._lock = threading.Lock()

    def due(self, repository):
        """The current generation when a round has to run, None while the last idle round still holds.

        Taken before the round reads anything, so a commit during the round
        makes the next one run again.
        """
        head = repository.head()
        with self._lock:
            idle = self._idle.get(repository)
        if idle is not None and idle[0] == head and 0 <= self._clock() - idle[1] < RECHECK_SECONDS:
            return None
        return head

    def done(self, repository, head, idle):
        """Record a finished round: idle is whether it found nothing to do or wait for."""
        with self._lock:
            if idle:
                self._idle[repository] = (head, self._clock())
            else:
                self._idle.pop(repository, None)
