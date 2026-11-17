"""Bind a declared fresh probe clock to its exact persisted router and views.

No generic timestamp/property-name mask: only a successful reference R017
response with an explicitly declared probe/at can establish a binding. Raw
observations, DB snapshots and future Ref values are never mutated.
"""
import copy
import re
from urllib.parse import unquote, urlsplit

from cases import at
from compare import Difference, timestamp
from invariants import fields


def eligible(case):
    return case.route == 'R017' and case.method == 'POST' and 'probe/at' in case.times


def router_id(request):
    match = re.fullmatch(r'/api/routers/([^/]+)/probe', urlsplit(request['path']).path)
    if match is None:
        raise ValueError('declared probe clock does not belong to a public router probe path')
    return unquote(match[1])


def stored_clock(snapshot, profile, ident):
    routers = fields(snapshot, 'profile', profile)['routers']['items']
    matches = [row for row in routers if isinstance(row, dict) and row.get('id') == ident]
    if len(matches) != 1:
        raise ValueError('persisted probe clock must name exactly one router in its own profile')
    return at(matches[0], 'last_probe/at')


class ProbeClocks:
    """Own-scope clock bindings for one isolated public-process pair."""

    def __init__(self):
        self.records = {'python': {}, 'go': {}}
        self.sequence = 0

    def _checked(self, case, request, view, snapshot, window):
        ident = router_id(request)
        clock = at(view['body'], 'probe/at')
        timestamp(clock, window[0] - 1, window[1] + 1)
        if stored_clock(snapshot, case.profile, ident) != clock:
            raise ValueError('stored last_probe.at differs from its own actual response clock')
        return ident, clock

    def reference(self, case, request, view, snapshot, window):
        """Keep fixture validation active even while Go's handler is absent."""
        if eligible(case):
            self._checked(case, request, view, snapshot, window)

    def record(self, case, requests, views, snapshots, windows):
        if not eligible(case):
            return []
        left_id, left_clock = self._checked(case, requests['python'], views['python'],
                                            snapshots['python']['after'], windows['python'])
        try:
            right_id, right_clock = self._checked(case, requests['go'], views['go'],
                                                  snapshots['go']['after'], windows['go'])
        except (KeyError, IndexError, TypeError, ValueError, OverflowError) as error:
            return [Difference('/contract/storage/routers/last_probe/at',
                'fresh own response clock stored on exactly its requested profile/router', str(error), 'clock-storage')]
        self.sequence += 1
        alias = '<validated persisted probe clock:' + str(self.sequence) + '>'
        self.records['python'][case.profile, left_id] = (left_clock, alias)
        self.records['go'][case.profile, right_id] = (right_clock, alias)
        return []

    def _router(self, side, profile, row):
        if not isinstance(row, dict) or not isinstance(row.get('id'), str):
            return
        saved = self.records[side].get((profile, row['id']))
        probe = row.get('last_probe')
        if saved is not None and isinstance(probe, dict) and probe.get('at') == saved[0]:
            probe['at'] = saved[1]

    def response(self, side, profile, route, view):
        result = copy.deepcopy(view)
        body = result.get('body')
        if not isinstance(body, dict):
            return result
        if route in ('R014', 'R015'):
            workspace = body.get('workspace')
            rows = [workspace.get('router')] if isinstance(workspace, dict) else []
        else:
            rows = body.get('routers') if route == 'R007' else [body.get('router')] if route in ('R008', 'R010') else []
        if isinstance(rows, list):
            for row in rows:
                self._router(side, profile, row)
        return result

    def snapshot(self, side, snapshot):
        result = copy.deepcopy(snapshot)
        table = result.get('database', {}).get('fields', {})
        columns = table.get('columns', [])
        if not all(name in columns for name in ('scope', 'owner', 'field', 'value')):
            return result
        scope, owner, field, value = (columns.index(name) for name in ('scope', 'owner', 'field', 'value'))
        for row in table.get('rows', []):
            if len(row) != len(columns) or row[scope] != 'profile' or row[field] != 'routers':
                continue
            routers = row[value]
            if isinstance(routers, dict) and isinstance(routers.get('items'), list):
                for router in routers['items']:
                    self._router(side, row[owner], router)
        return result
