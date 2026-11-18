"""Content-bound deterministic policy-editor revisions at public HTTP boundaries.

Go's canonical hash intentionally differs from Python's serialization. Validate
format, own dependency stability and collision freedom across observed settings,
then pair only equal stored settings and equal complete public responses. Never
recompute/emulate either implementation's hash or rewrite raw own-side Refs.
"""
import copy
import json
import re

from compare import Difference, differences
from invariants import fields
from cases import at
from temporal import set_path


def eligible(case, view):
    return (case.route, case.method) in (('R002', 'GET'), ('R003', 'PUT'), ('R014', 'GET'), ('R015', 'PUT')) and view['status'] == 200


def source(case, view, snapshot):
    if not eligible(case, view):
        return []
    body = view['body']
    workspace = case.route in ('R014', 'R015')
    profile = at(body, 'workspace/profile_id' if workspace else 'profile_id')
    if not isinstance(profile, str):
        raise ValueError('invalid policy revision profile type')
    profile_fields = fields(snapshot, 'profile', profile)
    if '_revision' not in profile_fields:
        raise ValueError('policy revision names no stored profile')
    settings = profile_fields.get('oxidns') or {}
    def token(kind, path, dependency):
        value = at(body, path)
        if not isinstance(value, str) or not re.fullmatch('[0-9a-f]{64}', value):
            raise ValueError('invalid policy revision SHA256 type/shape at ' + path)
        return {'profile': profile, 'kind': kind, 'path': path, 'token': value, 'dependency': dependency}
    if not workspace:
        return [token('oxidns', 'oxidns_revision', settings)]
    ident = at(body, 'workspace/router/id')
    rows = profile_fields['routers']['items']
    matches = [row for row in rows if isinstance(row, dict) and row.get('id') == ident]
    if not isinstance(ident, str) or len(matches) != 1:
        raise ValueError('workspace revision must name exactly one own stored router')
    # Scope comes from actual storage/public rows, never a private hash helper.
    router = {key: value for key, value in matches[0].items() if key != 'last_probe'}
    names = []
    policies = at(body, 'workspace/sources/policies')
    if not isinstance(policies, list):
        raise ValueError('workspace names revision needs ordered public policy rows')
    for row in policies:
        if not isinstance(row, dict) or not isinstance(row.get('ip'), str) or not isinstance(row.get('name'), str):
            raise ValueError('workspace names revision needs public string IP/name members')
        names.append([row['ip'], row['name']])
    return [token('oxidns', 'workspace/oxidns_revision', settings),
            token('workspace', 'workspace/revision', {'router': router, 'oxidns': settings}),
            token('names', 'workspace/names_revision', names)]


def projected(view, aliases):
    result = copy.deepcopy(view)
    for path, alias in aliases.items():
        set_path(result['body'], path, alias)
    return result


class PolicyRevisions:
    """One real process pair's observed deterministic settings/token relation."""

    def __init__(self):
        self.records = {'python': [], 'go': []}
        self.aliases = {}

    def _remember(self, side, value):
        for number, saved in enumerate(self.records[side]):
            if (saved['profile'], saved['kind']) != (value['profile'], value['kind']):
                continue
            # Preserve numeric representation distinctions (including signed
            # zero) in own-input provenance. This JSON key is never compared
            # to either implementation's returned hash.
            equal = json.dumps(saved['dependency'], sort_keys=True) == json.dumps(value['dependency'], sort_keys=True)
            if equal and saved['token'] != value['token']:
                raise ValueError(value['kind'] + ' revision changed while its own dependency stayed equal')
            if not equal and saved['token'] == value['token']:
                raise ValueError(value['kind'] + ' revision reused for a different own dependency')
            if equal:
                return number
        self.records[side].append(copy.deepcopy(value))
        return len(self.records[side]) - 1

    def reference(self, case, view, snapshot):
        for value in source(case, view, snapshot):
            self._remember('python', value)

    def pair(self, case, observations, snapshots, comparisons, comparison_states=None):
        left = source(case, observations['python'], snapshots['python']['after'])
        if not left or not eligible(case, observations['go']):
            return comparisons, []
        left_ids = [self._remember('python', value) for value in left]
        try:
            right = source(case, observations['go'], snapshots['go']['after'])
            right_ids = [self._remember('go', value) for value in right]
        except (KeyError, IndexError, TypeError, ValueError, OverflowError) as error:
            return comparisons, [Difference('/contract/http/body/policy-revisions',
                'deterministic SHA256 tokens bound to own profile/storage/public dependencies', str(error), 'revision')]
        # All workspace hashes must be considered together: each otherwise
        # legitimate different native hash must not block pairing its siblings.
        states = comparison_states or {side: value['after'] for side, value in snapshots.items()}
        try:
            scoped = {side: source(case, comparisons[side], states[side]) for side in ('python', 'go')}
        except (KeyError, IndexError, TypeError, ValueError, OverflowError) as error:
            return comparisons, [Difference('/contract/http/body/policy-revisions/scope',
                'independently projected own resource identity and dependency', str(error), 'revision')]
        for a, b in zip(scoped['python'], scoped['go']):
            if ((a['kind'], a['profile'], a['path']) != (b['kind'], b['profile'], b['path'])
                    or differences(a['dependency'], b['dependency'])):
                return comparisons, []
        candidate = {side: projected(comparisons[side], {value['path']: '<candidate ' + value['kind'] + ' revision>'
                                                      for value in scoped[side]}) for side in ('python', 'go')}
        if differences(candidate['python'], candidate['go']):
            return comparisons, []
        aliases = {}
        for value, left_id, right_id in zip(left, left_ids, right_ids):
            key = (left_id, right_id)
            if key not in self.aliases:
                self.aliases[key] = '<validated ' + value['kind'] + ' revision:' + str(len(self.aliases) + 1) + '>'
            aliases[value['path']] = self.aliases[key]
        return {side: projected(comparisons[side], aliases) for side in ('python', 'go')}, []
