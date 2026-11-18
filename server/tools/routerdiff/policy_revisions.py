"""Content-bound deterministic OxiDNS revision tokens for client-device HTTP.

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


def eligible(case, view):
    return (case.route, case.method) in (('R002', 'GET'), ('R003', 'PUT')) and view['status'] == 200


def source(case, view, snapshot):
    if not eligible(case, view):
        return None
    body = view['body']
    profile, token = body['profile_id'], body['oxidns_revision']
    if not isinstance(profile, str) or not isinstance(token, str) or not re.fullmatch('[0-9a-f]{64}', token):
        raise ValueError('invalid client-device profile/revision token type or SHA256 shape')
    profile_fields = fields(snapshot, 'profile', profile)
    if '_revision' not in profile_fields:
        raise ValueError('client-device revision names no stored profile')
    # This is the dependency published by the Python API, not a copy of its hash.
    # It deliberately excludes names/bindings/router/probe/profile revision.
    dependency = profile_fields.get('oxidns') or {}
    return {'profile': profile, 'token': token, 'dependency': dependency}


def projected(view, alias):
    result = copy.deepcopy(view)
    result['body']['oxidns_revision'] = alias
    return result


class PolicyRevisions:
    """One real process pair's observed deterministic settings/token relation."""

    def __init__(self):
        self.records = {'python': [], 'go': []}
        self.aliases = {}

    def _remember(self, side, value):
        for number, saved in enumerate(self.records[side]):
            if saved['profile'] != value['profile']:
                continue
            # Preserve numeric representation distinctions (including signed
            # zero) in own-input provenance. This JSON key is never compared
            # to either implementation's returned hash.
            equal = json.dumps(saved['dependency'], sort_keys=True) == json.dumps(value['dependency'], sort_keys=True)
            if equal and saved['token'] != value['token']:
                raise ValueError('client-device revision changed while its own stored settings stayed equal')
            if not equal and saved['token'] == value['token']:
                raise ValueError('client-device revision reused for different stored settings')
            if equal:
                return number
        self.records[side].append(copy.deepcopy(value))
        return len(self.records[side]) - 1

    def reference(self, case, view, snapshot):
        value = source(case, view, snapshot)
        if value is not None:
            self._remember('python', value)

    def pair(self, case, observations, snapshots, comparisons):
        left = source(case, observations['python'], snapshots['python']['after'])
        if left is None or not eligible(case, observations['go']):
            return comparisons, []
        left_id = self._remember('python', left)
        try:
            right = source(case, observations['go'], snapshots['go']['after'])
            right_id = self._remember('go', right)
        except (KeyError, IndexError, TypeError, ValueError, OverflowError) as error:
            return comparisons, [Difference('/contract/http/body/oxidns_revision',
                'deterministic SHA256 token bound to its own selected profile/settings', str(error), 'revision')]
        if left['profile'] != right['profile'] or differences(left['dependency'], right['dependency']):
            return comparisons, []
        candidate = {side: projected(comparisons[side], '<candidate settings revision>') for side in ('python', 'go')}
        if differences(candidate['python'], candidate['go']):
            return comparisons, []
        key = (left_id, right_id)
        if key not in self.aliases:
            self.aliases[key] = '<validated OxiDNS settings revision:' + str(len(self.aliases) + 1) + '>'
        return {side: projected(comparisons[side], self.aliases[key]) for side in ('python', 'go')}, []
