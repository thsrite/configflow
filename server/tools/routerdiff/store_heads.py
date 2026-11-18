"""Bind opaque store commit IDs only to proven equal stored content.

No field-name/UUID replacement: only meta.commit_id is projected, scoped by its
actual store identity, generation and own complete config-table content. All
other SQL rows, metadata, files and messages remain byte/value comparisons.
"""
import copy
import hashlib
import json

from compare import Difference, differences


def head(snapshot):
    table = snapshot['database']['meta']
    if table['columns'] != ['key', 'value'] or any(len(row) != 2 for row in table['rows']):
        raise ValueError('invalid store metadata schema')
    meta = dict(table['rows'])
    if len(meta) != len(table['rows']):
        raise ValueError('duplicate store metadata key')
    store, generation = (meta[key] for key in ('store_id', 'generation'))
    commit = meta.get('commit_id')  # The public Python head accepts legacy absent/null IDs.
    if (not isinstance(store, str) or not store or type(generation) is not int or generation < 0
            or commit is not None and not isinstance(commit, str)):
        raise ValueError('invalid store identity/generation/commit token')
    # commit_id identifies config changes, not metadata-only transactions. The
    # actual Python store allows metadata-only commits without a new head.
    # Those meta rows remain strict in the final comparison; never mask them.
    content = {'fields': snapshot['database']['fields'], 'sections': snapshot['database']['sections'],
               'root_order': meta.get('root_order')}
    digest = hashlib.sha256(json.dumps(content, sort_keys=True, ensure_ascii=False,
                                       separators=(',', ':'), allow_nan=False).encode()).hexdigest()
    return store, generation, commit, digest


def project(snapshot, alias):
    result = copy.deepcopy(snapshot)
    for row in result['database']['meta']['rows']:
        if row[0] == 'commit_id':
            row[1] = alias
    return result


class StoreHeads:
    """One disposable process pair's content-bound commit-token history."""

    def __init__(self):
        self.records = {'python': {}, 'go': {}}
        self.aliases = {}
        self.generations = {'python': {}, 'go': {}}

    def _remember(self, side, value):
        store, generation, commit, digest = value
        previous = self.records[side].get((store, commit))
        if previous is not None and previous != (generation, digest):
            raise ValueError('store commit token was reused for a different generation/content')
        prior_generation = self.generations[side].get((store, generation))
        if prior_generation is not None and prior_generation != (commit, digest):
            raise ValueError('store generation changed its bound commit token/content')
        self.records[side][store, commit] = (generation, digest)
        self.generations[side][store, generation] = (commit, digest)

    def validate(self, side, before, after):
        original, current = head(before), head(after)
        if current[0] != original[0] or current[1] < original[1]:
            raise ValueError('router operation changed store identity or rewound generation')
        if current[1] == original[1] and (current[2] != original[2] or current[3] != original[3]):
            raise ValueError('configuration/head changed without a new generation')
        if current[1] > original[1] and (not current[2] or current[2] == original[2]):
            raise ValueError('configuration commit did not create a fresh nonempty commit token')
        self._remember(side, original)
        self._remember(side, current)
        return current

    def reference(self, before, after):
        self.validate('python', before, after)

    def pair(self, snapshots, comparisons):
        values = {'python': self.validate('python', snapshots['python']['before'], snapshots['python']['after'])}
        try:
            values['go'] = self.validate('go', snapshots['go']['before'], snapshots['go']['after'])
        except (KeyError, IndexError, TypeError, ValueError, OverflowError) as error:
            return comparisons, [Difference('/contract/storage/meta/commit_id',
                'opaque token bound to its own stable store, generation and complete configuration', str(error), 'store-head')]
        left, right = values['python'], values['go']
        if not left[2] or not right[2]:
            return comparisons, []  # Legacy null/empty/missing is literal data, never an opaque alias.
        # A mismatching content/head never earns an alias. The full unprojected
        # differences remain visible; equal UUID-looking text is no evidence.
        candidate = {side: project(comparisons[side], '<candidate store commit>') for side in ('python', 'go')}
        if differences(candidate['python']['database'], candidate['go']['database']):
            return comparisons, []
        key = (left, right)
        if key not in self.aliases:
            self.aliases[key] = '<validated stored commit:' + str(len(self.aliases) + 1) + '>'
        return {side: project(comparisons[side], self.aliases[key]) for side in ('python', 'go')}, []
