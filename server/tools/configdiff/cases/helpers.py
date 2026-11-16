"""Synthetic storage faults at the external SQLite boundary, never Go internals."""
from contextlib import contextmanager
import copy
import sqlite3

from compare import Policy


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
