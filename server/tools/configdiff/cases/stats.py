"""R212 stats history retention, throttle, corrupt files and read/write failure."""
from datetime import datetime, timezone, timedelta
import json

from harness import Pair, DesignError


def sample(at='2026-01-01T00:00:00Z', **changes):
    return {'at': at, 'subscriptions': 0, 'nodes': 0, 'proxyGroups': 0, 'rules': 0, **changes}


def history(binary, recorder):
    for tz in ('UTC', 'Asia/Shanghai'):
        with Pair(binary, recorder, tz=tz) as pair:
            path = 'profiles/default/stats_history.json'
            pair.policy.times.add('files.' + path + '.content.samples[0].at')
            pair.request('R212-S01', 'GET /api/stats/overview', expect=200, writes=True, variant=tz)
            before = [(d / path).read_bytes() for d in pair.dirs]
            pair.request('R212-S02', 'GET /api/stats/overview', expect=200, writes=True, variant=tz)
            for index, d in enumerate(pair.dirs):
                if (d / path).read_bytes() != before[index]:
                    if index == 0:
                        raise DesignError('Python throttle fixture unexpectedly rewrote stats history')
                    recorder.record('R212-S02', tz + '/throttle-side-effect', 'GET /api/stats/overview', 'S', 'different',
                                    [__import__('compare').Difference('stats-history', 'unexpected-rewrite', '<unchanged>', '<rewritten>')])
            pair.request('R212-B02', 'GET /api/stats/overview', expect=200, writes=True, variant=tz)
    scenarios = [('S03', [sample()]), ('B01', [sample('2026-01-01T00:00:00Z') for _ in range(48)]),
                 ('B03', [sample('invalid-time')]), ('F08', [sample(nodes='invalid-count')])]
    for case, samples in scenarios:
        with Pair(binary, recorder) as pair:
            relative = 'profiles/default/stats_history.json'
            for directory in pair.dirs:
                path = directory / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(json.dumps({'samples': samples}))
            index = 47 if case == 'B01' else 1
            pair.policy.times.add(f'files.{relative}.content.samples[{index}].at')
            if case == 'F08':
                pair.policy.internal_errors['body.error'] = ''
            pair.request('R212-' + case, 'GET /api/stats/overview', expect=500 if case == 'F08' else 200,
                         writes=True, check=(lambda r: require(len(r.json()['data']['nodes']['history']) == 48)) if case == 'B01' else None)
    with Pair(binary, recorder) as pair:
        relative = 'profiles/default/stats_history.json'
        for directory in pair.dirs:
            path = directory / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('{invalid')
        pair.policy.times.add('files.' + relative + '.content.samples[0].at')
        pair.request('R212-F09', 'GET /api/stats/overview', expect=200, writes=True)
    with Pair(binary, recorder) as pair:
        # Creating a directory where the file belongs makes reads recover and
        # atomic replacement fail; handler deliberately still returns the counts.
        for directory in pair.dirs:
            (directory / 'profiles/default/stats_history.json').mkdir(parents=True)
        pair.request('R212-F10', 'GET /api/stats/overview', expect=200, writes=True)


def require(value):
    if not value:
        raise DesignError('Python stats history did not retain exactly 48 samples')


GROUPS = {'stats-history': history}
