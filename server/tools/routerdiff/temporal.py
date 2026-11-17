"""Pair explicit response clocks after own-window and cache provenance checks.

Raw observations remain the source of future HTTP references and reports. This
never projects persisted timestamps or arbitrary ISO-like text. A cached clock
must equal the exact raw clock from an explicitly declared earlier fresh read,
on the same side, route and profile. No timestamp-shaped field-name masks.
"""
import copy

from cases import at, Ref
from compare import Difference, timestamp


def set_path(document, path, value):
    parts = path.strip('/').split('/')
    if not path.strip('/'):
        raise ValueError('timestamp projection must name an explicit member')
    parent = at(document, '/'.join(parts[:-1]))
    key = parts[-1].replace('~1', '/').replace('~0', '~')
    parent[int(key) if isinstance(parent, list) else key] = value


def fresh_pair(observations, windows, paths):
    if not paths:
        return observations, []
    result = copy.deepcopy(observations)
    found = []
    for path in paths:
        left = at(observations['python']['body'], path)
        timestamp(left, windows['python'][0] - 1, windows['python'][1] + 1)
        try:
            right = at(observations['go']['body'], path)
        except (KeyError, IndexError, TypeError, ValueError):
            right = '<missing>'
        try:
            timestamp(right, windows['go'][0] - 1, windows['go'][1] + 1)
        except (TypeError, ValueError, OverflowError):
            found.append(Difference('/contract/http/body/' + path.strip('/'),
                'ISO timestamp within its own actual HTTP request window', right, 'clock'))
            continue
        alias = '<validated request timestamp:' + path.strip('/') + '>'
        set_path(result['python']['body'], path, alias)
        set_path(result['go']['body'], path, alias)
    return result, found


class ResponseClocks:
    """Keep raw, independently validated response clocks for one isolated pair."""

    def __init__(self):
        self.records = {}

    def _source(self, case, reference):
        if (not isinstance(reference, Ref) or not reference.path.startswith('/body/')
                or reference.prefix or reference.suffix):
            raise ValueError('cached timestamp must reference an exact earlier body member')
        source = self.records.get((reference.case, reference.path[len('/body/'):]))
        if source is None or (source['profile'], source['route']) != (case.profile, case.route):
            raise ValueError('cached timestamp source is not a fresh clock in the same route/profile')
        return source

    def reference(self, case, observation, window):
        """Validate Python cache provenance even while a Go route is absent."""
        if set(case.times) & case.cached_times.keys():
            raise ValueError('one clock cannot be both fresh and cached')
        for path in case.times:
            value = at(observation['body'], path)
            timestamp(value, window[0] - 1, window[1] + 1)
            self.records[case.ident, path] = {'profile': case.profile, 'route': case.route,
                                             'raw': {'python': value, 'go': None}}
        for path, reference in case.cached_times.items():
            if at(observation['body'], path) != self._source(case, reference)['raw']['python']:
                raise ValueError('reference cached timestamp changed since its declared fresh read')

    def pair(self, case, observations, windows):
        self.reference(case, observations['python'], windows.get('python', ()))
        result, found = fresh_pair(observations, windows, case.times)
        # The reference clock must be valid even if native validation failed.
        # Keep that invalid native source unpaired: later equality to the bad
        # raw clock must not rehabilitate it into a valid cached timestamp.
        for path in case.times:
            raw = {'python': at(observations['python']['body'], path)}
            try:
                candidate = at(observations['go']['body'], path)
                timestamp(candidate, windows['go'][0] - 1, windows['go'][1] + 1)
                raw['go'] = candidate
            except (KeyError, IndexError, TypeError, ValueError, OverflowError):
                raw['go'] = None
            self.records[case.ident, path] = {'profile': case.profile, 'route': case.route, 'raw': raw}
        if case.cached_times:
            result = copy.deepcopy(result)
        for path, reference in case.cached_times.items():
            source = self._source(case, reference)
            try:
                right = at(observations['go']['body'], path)
            except (KeyError, IndexError, TypeError, ValueError):
                right = '<missing>'
            if source['raw']['go'] is None or right != source['raw']['go']:
                found.append(Difference('/contract/http/body/' + path.strip('/'),
                    'exact own-side raw timestamp from ' + reference.case + reference.path,
                    right, 'clock-cache'))
                continue
            alias = '<validated cached timestamp:' + reference.case + reference.path + '>'
            set_path(result['python']['body'], path, alias)
            set_path(result['go']['body'], path, alias)
        return result, found
