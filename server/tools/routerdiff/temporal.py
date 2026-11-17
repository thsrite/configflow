"""Pair only explicitly declared fresh response clocks after own-window checks.

Raw observations remain the source of future HTTP references and reports. This
does not project cached clocks, persisted timestamps or arbitrary ISO-like text.
"""
import copy

from cases import at
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
