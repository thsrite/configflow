"""Narrow generated router-ID bindings, established by actual storage effects.

Only router definition ID slots are projected. Messages, references inside device
scripts, hashes, receipt filenames and arbitrary fields are never rewritten.
"""
import copy
import re

from compare import differences
from invariants import fields


class RouterIDs:
    def __init__(self):
        self.records = []

    def created(self, profile, observations, snapshots):
        identities = {}
        for side in ('python', 'go'):
            try:
                view = observations[side]
                ident = view['body']['router']['id']
                original = fields(snapshots[side]['before'], 'profile', profile)['routers']['items']
                current = fields(snapshots[side]['after'], 'profile', profile)['routers']['items']
                if (view['status'] != 200 or not isinstance(ident, str)
                        or not re.fullmatch(r'[A-Za-z0-9._~-]+', ident) or ident in ('.', '..')):
                    raise ValueError
                if (not isinstance(original, list) or not isinstance(current, list)
                        or len(current) != len(original) + 1 or differences(current[:-1], original)
                        or current[-1]['id'] != ident
                        or sum(isinstance(row, dict) and row.get('id') == ident for row in current) != 1):
                    raise ValueError
            except (KeyError, IndexError, TypeError, ValueError):
                raise ValueError(side + ' create ID is not a fresh URL-safe identity persisted exactly once') from None
            identities[side] = ident
        # Keep historical identities after deletion: recycling a previous alias
        # cannot silently identify a different object as the original one.
        if any(row['profile'] == profile and any(row[side] == identities[side] for side in identities)
               for row in self.records):
            raise ValueError('generated router identity mapping is not injective')
        record = {'profile': profile, **identities, 'alias': '<generated-router:' + str(len(self.records) + 1) + '>'}
        self.records.append(record)
        return copy.deepcopy(record)

    def _mapped(self, side, profile, ident):
        return next((row['alias'] for row in self.records
                     if row['profile'] == profile and row[side] == ident), ident)

    def response(self, side, profile, route, view):
        result = copy.deepcopy(view)
        body = result.get('body')
        if not isinstance(body, dict):
            return result
        rows = body.get('routers') if route == 'R007' else [body.get('router')] if route in ('R008', 'R010') else []
        if isinstance(rows, list):
            for row in rows:
                if isinstance(row, dict) and isinstance(row.get('id'), str):
                    row['id'] = self._mapped(side, profile, row['id'])
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
            if not isinstance(routers, dict) or not isinstance(routers.get('items'), list):
                continue
            for router in routers['items']:
                if isinstance(router, dict) and isinstance(router.get('id'), str):
                    router['id'] = self._mapped(side, row[owner], router['id'])
        return result
