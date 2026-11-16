"""Validate discovery-created UUID identities and their derived cache paths."""
import re

from compare import Difference, database
from composition import cache_name
from harness import DesignError


def allocation_ids(libraries, rules):
    result = []
    for kind, rows in (('lib', libraries), ('ruleset', rules)):
        for row in rows:
            ident = row.get('id') if isinstance(row, dict) else None
            pattern = r'lib_[a-f0-9]{32}' if kind == 'lib' else r'ruleset_[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}'
            if not isinstance(ident, str) or not re.fullmatch(pattern, ident):
                raise ValueError('discovery allocation needs its resource prefix and lowercase hex/UUID shape')
            result.append((kind, ident))
    if len({ident for _, ident in result}) != len(result):
        raise ValueError('duplicate discovery allocation')
    return result


def rows(directory):
    fields = database(directory)['fields']
    return [fields['shared//rule_library']['value'], fields['profile/default/rule_configs']['value']]


class Allocations:
    def __init__(self, pair, count):
        self.pair, self.count = pair, count
        self.before = [[{row['id'] for row in collection} for collection in rows(directory)] for directory in pair.dirs]

    def added(self, index):
        return [[row for row in collection if row.get('id') not in known]
                for collection, known in zip(rows(self.pair.dirs[index]), self.before[index])]

    def reference(self, reply):
        new = self.added(0)
        if [len(part) for part in new] != [self.count, self.count]:
            raise DesignError('Python init did not allocate one library and reference per requested target')
        allocation_ids(*new)
        selected = {value.get('id') for key in ('direct', 'proxy') if isinstance(value := reply.json().get(key), dict)}
        for library in new[0]:
            path = self.pair.dirs[0] / cache_name(library)
            if library['id'] in selected:
                if not path.is_file():
                    raise DesignError('Python init failed to materialize the independently derived selected source cache')
            elif path.exists():
                raise DesignError('Python duplicate-target orphan unexpectedly received a cache')

    def capture(self, replies, policy):
        left = self.added(0)
        try:
            right = self.added(1)
            a, b = allocation_ids(*left), allocation_ids(*right)
            if [len(part) for part in left] != [len(part) for part in right]:
                return [Difference('discovery.allocations', 'wrong-allocation-count', [len(p) for p in left], [len(p) for p in right])]
            for (kind, ident), (other, opposite) in zip(a, b):
                if kind != other:
                    raise ValueError('allocation kind differs')
                pattern = r'lib_[a-f0-9]{32}' if kind == 'lib' else r'ruleset_[a-f0-9-]{36}'
                policy.bind(ident, opposite, pattern, 'discovery-' + kind)
            for library, opposite in zip(left[0], right[0]):
                for suffix in ('', '.bak'):
                    paths = [cache_name(value) + suffix for value in (library, opposite)]
                    if all((directory / path).is_file() for directory, path in zip(self.pair.dirs, paths)):
                        policy.bind(*paths, r'shared/rules/by-id/[a-f0-9]{64}\.list(?:\.bak)?', 'discovery-cache')
            return []
        except (AssertionError, ValueError, KeyError, TypeError) as error:
            return [Difference('discovery.allocations', 'invalid-Go-allocation', '<resource-prefixed hex/UUID IDs and derived paths>', str(error))]
