"""Synthetic storage faults at the external SQLite boundary, never Go internals."""
from contextlib import contextmanager
import copy
import sqlite3
import re
from datetime import datetime, timezone

from compare import Policy, Difference, database


def exception_policy(pair, prefix=''):
    # Only the explicitly exercised internal exception branch gets this rule.
    policy = copy.deepcopy(pair.policy)
    policy.internal_errors['body.message'] = prefix
    return policy


def profile_policy(pair):
    policy = copy.deepcopy(pair.policy)
    for profile in ('default', 'other'):
        for field in ('created_at', 'updated_at'):
            policy.times.add(f'db.fields.profile/{profile}/{field}.value')
    return policy


def profile_capture(pair, *, generated=False):
    """Bind created IDs and migration filenames, retaining all row/file contents."""
    def capture(replies, policy):
        out = []
        if generated:
            values = [reply.json().get('id', '') for reply in replies]
            if not re.fullmatch(r'profile_[a-f0-9]{12}', values[0]):
                from harness import DesignError
                raise DesignError('Python created profile id format changed')
            if not re.fullmatch(r'profile_[a-f0-9]{12}', values[1]):
                return [Difference('body.id', 'profile-id-format', values[0], values[1])]
            policy.bind(*values, r'profile_[a-f0-9]{12}', 'created-profile')
        for path in ('body', 'body.profile', 'body.from_profile', 'body.to_profile'):
            for name in ('created_at', 'updated_at'):
                policy.times.add(path + '.' + name)
        # The only array here is list_profiles' metadata list, not arbitrary JSON.
        if replies[0].body.startswith(b'['):
            for index in range(len(replies[0].json())):
                for name in ('created_at', 'updated_at'):
                    policy.times.add(f'body[{index}].' + name)
        for key in database(pair.dirs[0])['fields']:
            parts = key.split('/')
            if parts[0] == 'profile':
                mapped_id = policy.identities.get(parts[1], parts[1])
                if mapped_id != parts[1]:
                    policy.bind(key, 'profile/' + mapped_id + '/' + parts[2],
                                r'profile/[A-Za-z0-9_-]{1,64}/[A-Za-z0-9_]+', 'profile-row-key')
                if parts[2] in ('created_at', 'updated_at'):
                    policy.times.add('db.fields.' + key + '.value')
        roots = [directory / 'migrations' for directory in pair.dirs]
        folders = [{p.name for p in root.iterdir() if p.is_dir()} if root.exists() else set() for root in roots]
        unmatched = [folders[0] - folders[1] - policy.identities.keys(),
                     folders[1] - folders[0] - policy.reverse.keys()]
        for a, b in zip(sorted(unmatched[0]), sorted(unmatched[1])):
            pattern = r'(?:deleted-[A-Za-z0-9_-]+-|[0-9]{8}T[0-9]{6}Z-)[a-f0-9]{32}'
            if not re.fullmatch(pattern, a) or not re.fullmatch(pattern, b):
                continue  # Unknown paths remain unnormalized and fail file comparison.
            prefix = a[:-32]
            if a.startswith('deleted-'):
                for old, new in policy.identities.items():
                    if old.startswith('profile_'):
                        prefix = prefix.replace(old, new)
                if prefix != b[:-32]:
                    continue
            if not a.startswith('deleted-'):
                for value in (a, b):
                    stamp = datetime.strptime(value[:16], '%Y%m%dT%H%M%SZ').replace(tzinfo=timezone.utc).timestamp()
                    if not policy.start - 2 <= stamp <= policy.end + 2:
                        out.append(Difference('files.migrations', 'invalid-snapshot-time', a, b))
            policy.bind(a, b, pattern, 'migration-dir')
            for p in (roots[0] / a).rglob('*'):
                if p.is_file():
                    suffix = p.relative_to(roots[0] / a).as_posix()
                    left, right = 'migrations/' + a + '/' + suffix, 'migrations/' + b + '/' + suffix
                    policy.bind(left, right, r'migrations/[^/]+/.+', 'migration-file')
                    if suffix == 'deleted-profile.json':
                        for name in ('created_at', 'updated_at'):
                            policy.times.add('files.' + left + '.content.' + name)
        return out
    return capture


@contextmanager
def deny_writes(pair):
    """SQLite rejects actual field/meta writes; existing read handles still work."""
    names = []
    for directory in pair.dirs:
        with sqlite3.connect(directory / 'configflow.sqlite3') as connection:
            for table in ('fields', 'sections', 'meta'):
                for operation in ('INSERT', 'UPDATE', 'DELETE'):
                    name = 'configdiff_deny_' + table + '_' + operation.lower()
                    connection.execute(f"CREATE TRIGGER {name} BEFORE {operation} ON {table} "
                                       "BEGIN SELECT RAISE(ABORT, 'synthetic-write-denied'); END")
                    if directory == pair.dirs[0]:
                        names.append(name)
    try:
        yield
    finally:
        for directory in pair.dirs:
            with sqlite3.connect(directory / 'configflow.sqlite3') as connection:
                for name in names:
                    connection.execute('DROP TRIGGER ' + name)
