"""Observe the real 12-second provider response and its background publication."""
import copy
import threading
import time
import yaml

from compare import Difference, differences, files, response_diffs
from harness import DesignError
from providers import comments
from subscriptions import cached


def fallback_timing(seconds):
    return isinstance(seconds, (int, float)) and 11.0 <= seconds <= 13.8


def response_batch_diffs(left, right, policy):
    out = differences(len(left), len(right), 'provider.clients.count', policy)
    for index, (a, b) in enumerate(zip(left, right)):
        for difference in response_diffs(a, b, policy):
            out.append(Difference(f'provider.client[{index}].' + difference.path,
                                  difference.kind, difference.python, difference.go))
    return out


class Fallback:
    def __init__(self, pair, source):
        self.pair, self.source = pair, source
        self.started = [None, None]
        self.observed = [None, None]
        self.release = None
        source.before_reply = self.wait

    def wait(self, path):
        # A bounded fake timeout keeps even broken Go behavior below the first
        # scheduler tick; normal Flask returns the old body at 12 seconds.
        self.release.wait(14)

    def before(self, index):
        self.source.requests.clear()
        self.release = threading.Event()
        self.started[index] = time.monotonic()

    def after(self, index, reply):
        elapsed = time.monotonic() - self.started[index]
        self.release.set()
        path = self.pair.dirs[index] / 'profiles/default/providers/served/subscription-sub-a.yaml'
        deadline = time.monotonic() + 3
        fresh, quiescent = False, False
        previous, unchanged_since = None, None
        while time.monotonic() < deadline:
            try:
                raw = path.read_bytes()
                document = yaml.safe_load(raw)
                names = [item.get('name') for item in document.get('proxies', [])] if isinstance(document, dict) else []
                fresh = '甲🧪' in names and 'last-good-only' not in names
                snapshot = files(self.pair.dirs[index])
                if snapshot == previous:
                    unchanged_since = unchanged_since or time.monotonic()
                else:
                    previous, unchanged_since = snapshot, None
                transient = any('.tmp' in item.name for item in self.pair.dirs[index].rglob('*'))
                quiescent = bool(fresh and cached(self.pair.dirs[index]) and path.with_name(path.name + '.bak').exists()
                                 and unchanged_since and time.monotonic() - unchanged_since >= .15 and not transient)
                if quiescent:
                    break
            except (OSError, ValueError, TypeError, AttributeError, yaml.YAMLError):
                pass
            time.sleep(.02)
        self.pair.policy.end = time.time()
        raw_comments = []
        try:
            raw_comments = comments(path.read_bytes(), self.pair.policy)
        except (OSError, ValueError) as error:
            raw_comments = {'invalid': str(error), 'observed': [line for line in path.read_text(errors='replace').splitlines() if line.startswith('#')] if path.exists() else None}
        self.observed[index] = {'elapsed_valid': fallback_timing(elapsed), 'elapsed': elapsed,
                                'requests': copy.deepcopy(self.source.requests), 'fresh_after_release': fresh,
                                'quiescent_after_release': quiescent,
                                'served_comments': raw_comments}
        if index == 0 and (not fallback_timing(elapsed) or not fresh or not quiescent):
            raise DesignError('Python did not serve 12-second fallback then publish background fresh data: ' + repr(self.observed[0]))

    def compare(self, policy):
        records = copy.deepcopy(self.observed)
        out = []
        for index, record in enumerate(records):
            if not record['elapsed_valid']:
                out.append(Difference('provider.fallback.elapsed', 'not-12-second-response', '11.0–13.8 seconds', record['elapsed']))
            # Validated wall-clock durations may differ; keep the validity bit.
            record.pop('elapsed')
        return out + differences(*records, 'provider.fallback', policy)
