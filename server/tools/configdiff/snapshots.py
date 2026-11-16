"""Validate import recovery snapshots against each process's prior SQLite rows."""
from compare import database, differences, Difference
from harness import DesignError
from cases.helpers import profile_capture


def snapshot_errors(directory, before, previous_paths):
    paths = set(directory.glob('migrations/*/import/configflow.sqlite3')) - previous_paths
    if len(paths) != 1:
        return [Difference('snapshot', 'expected-one-new-database', 1, len(paths))]
    return differences(before, database(next(iter(paths)).parent), 'snapshot')


class ImportSnapshot:
    def __init__(self, pair):
        self.pair = pair
        self.before = [database(d) for d in pair.dirs]
        self.paths = [set(d.glob('migrations/*/import/configflow.sqlite3')) for d in pair.dirs]

    def check(self, reply):
        errors = snapshot_errors(self.pair.dirs[0], self.before[0], self.paths[0])
        if errors:
            raise DesignError('Python import snapshot is not the prior database: ' + repr(errors[:3]))

    def capture(self, replies, policy):
        result = snapshot_errors(self.pair.dirs[1], self.before[1], self.paths[1])
        result += profile_capture(self.pair)(replies, policy)
        # Exact known profile timestamp fields within logical SQLite backups;
        # all other snapshot fields, revisions, positions and content stay strict.
        for path in self.pair.dirs[0].glob('migrations/*/import/configflow.sqlite3'):
            relative = path.relative_to(self.pair.dirs[0]).as_posix()
            for key in database(path.parent)['fields']:
                parts = key.split('/')
                if parts[0] == 'profile' and parts[2] in ('created_at', 'updated_at'):
                    policy.times.add('files.' + relative + '.content.fields.' + key + '.value')
        return result
