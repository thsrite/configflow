#!/usr/bin/env python3
"""Python side of TestCrossCheckPython (see crosscheck_test.go).

Runs the real backend.common.config_store against database files shared
with the Go store. CONFIGFLOW_REPO must point at the repository root.

    crosscheck.py create DB OUT    build DB through a sequence of commits; OUT = the steps and a dump
    crosscheck.py dump DB OUT      check() and load() DB; OUT = a dump of every row
    crosscheck.py commit DB OUT    apply one more commit to DB; OUT = a dump
    crosscheck.py errors DIR OUT   provoke failures; OUT = scenarios with Python's exception type and message
"""

import copy
import json
import os
import shutil
import sqlite3
import sys
from pathlib import Path

sys.path.insert(0, os.environ['CONFIGFLOW_REPO'])

from backend.common.config_store import ConfigStore, dumps  # noqa: E402


def sample_document():
    return {
        'schema_version': 5,
        'system': {'system_config': {'server_domain': 'https://例子.test', 'z': 1, 'a': 2},
                   'backup': {}, 'agents': [], '_revision': 3},
        'shared': {'subscriptions': [], 'nodes': [{'id': 'n', 'name': '节点 🚀', 'port': 1.5}],
                   'subscription_aggregations': [], 'rule_library': [], '_revision': 7},
        'profiles': {
            'zeta': {'id': 'zeta', 'name': 'Zeta', '_revision': 2, 'description': '最后创建',
                     'created_at': '2026-01-02T00:00:00Z', 'updated_at': '2026-01-03T00:00:00Z',
                     'extra': {'b': [1, {'c': None}], 'a': True}},
            'default': {'name': 'Default', 'id': 'default', '_revision': 0, 'description': '',
                        'created_at': '2026-01-01T00:00:00Z', 'updated_at': '2026-01-01T00:00:00Z'},
        },
        'future_root_key': {'kept': True},
    }


def store(path):
    return ConfigStore(Path(path), lambda: 2.0)


def dump(path):
    connection = sqlite3.connect(path)
    result = {
        'meta': connection.execute('SELECT key, value FROM meta ORDER BY key').fetchall(),
        'sections': connection.execute('SELECT scope, owner, position FROM sections ORDER BY scope, owner').fetchall(),
        'fields': connection.execute('SELECT scope, owner, field, position, value, revision FROM fields '
                                     'ORDER BY scope, owner, field').fetchall(),
        'schema': connection.execute('SELECT type, name, tbl_name, sql FROM sqlite_master ORDER BY name').fetchall(),
        'pragmas': {name: connection.execute(f'PRAGMA {name}').fetchone()[0]
                    for name in ('application_id', 'journal_mode', 'user_version', 'encoding', 'auto_vacuum')},
    }
    connection.close()
    target = store(path)
    target.check()
    entry = target.load()
    result['document'] = dumps(entry.document)
    result['head'] = list(entry.head)
    result['size'] = entry.size
    result['fingerprint'] = list(target.fingerprint())
    return result


def create(path):
    """Every kind of change a commit can contain, in a fixed order."""
    steps = []
    target = store(path)
    document = sample_document()
    meta = {'migration': {'source': 'defaults', 'at': '2026-01-01T00:00:00Z', 'snapshot': None},
            'legacy_config': {'sha256': None, 'size': None, 'data_source': False, 'note': '迁移'}}
    entry = target.create(document, meta)

    def record(document, meta, committed):
        steps.append({'document': dumps(document), 'meta': None if meta is None else dumps(meta),
                      'committed': committed is not None, 'generation': target.load().head.generation})

    record(document, meta, entry)

    def commit(change, meta=None):
        nonlocal entry
        document = copy.deepcopy(entry.document)
        document = change(document) or document
        committed = target.commit(entry, document, meta=meta)
        record(document, meta, committed)
        entry = committed or entry

    def rename(document):
        document['profiles']['zeta']['name'] = 'Renamed'

    def numbers_and_text(document):
        document['system']['numbers'] = [0, -0.0, 1.0, 1.5, 1e16, 1e-05, 2 ** 64, -(10 ** 30), 3.25, 0.1 + 0.2]
        document['system']['text'] = 'tab\t nl\n quote" back\\ nul\x00 del\x7f sep\u2028 </x> é 例 🚀'
        document['system']['system_config']['z'] = {'nested': [{'k': None}, [], {}]}
        document['shared']['nodes'].append({'id': 'm', 'name': 'second', 'tags': ['a', 'b']})

    def delete_and_add_profiles(document):
        profiles = document['profiles']
        del profiles['zeta']
        document['profiles'] = {'alpha': {'id': 'alpha', 'name': 'Alpha', '_revision': 0}, **profiles}

    def reorder_root(document):
        del document['future_root_key']
        del document['shared']['rule_library']
        reordered = {'new_root': [1, 2, 3], 'shared': document['shared'], 'schema_version': document['schema_version'],
                     'profiles': document['profiles'], 'system': document['system'], 'another': 'value'}
        return reordered

    def reorder_fields(document):
        default = document['profiles']['default']
        document['profiles']['default'] = dict(reversed(list(default.items())))
        document['profiles']['alpha']['description'] = '新增'

    commit(rename)
    commit(numbers_and_text)
    commit(delete_and_add_profiles)
    commit(lambda document: None, meta={'note': {'kept': 1}})  # meta only: the generation stays
    commit(reorder_root)
    commit(lambda document: None)  # nothing to write
    commit(reorder_fields, meta={'note': {'kept': 2}, 'last_legacy_import': {'snapshot': 'migrations/x'}})
    return {'steps': steps, 'dump': dump(path)}


def one_more_commit(path):
    target = store(path)
    target.check()
    entry = target.load()
    document = copy.deepcopy(entry.document)
    document['shared']['python_was_here'] = [1.5, '🚀', {'generation': entry.head.generation}]
    committed = target.commit(entry, document)
    assert committed is not None and committed.head.generation == entry.head.generation + 1
    return dump(path)


SCENARIOS = [
    # name, what to do to a copy of a healthy database, timeout in seconds
    ('healthy', [], 2.0),
    ('garbage', 'garbage', 2.0),
    ('missing', 'missing', 2.0),
    ('locked', 'locked', 0.1),
    ('meta-not-json', ["UPDATE meta SET value = '{oops' WHERE key = 'migration'"], 2.0),
    ('generation-not-json', ["UPDATE meta SET value = '' WHERE key = 'generation'"], 2.0),
    ('generation-float', ["UPDATE meta SET value = '1.0' WHERE key = 'generation'"], 2.0),
    ('generation-bool', ["UPDATE meta SET value = 'true' WHERE key = 'generation'"], 2.0),
    ('store-id-number', ["UPDATE meta SET value = '7' WHERE key = 'store_id'"], 2.0),
    ('commit-id-number', ["UPDATE meta SET value = '7' WHERE key = 'commit_id'"], 2.0),
    ('commit-id-missing', ["DELETE FROM meta WHERE key = 'commit_id'"], 2.0),
    ('root-order-missing', ["DELETE FROM meta WHERE key = 'root_order'"], 2.0),
    ('root-order-invalid', ["UPDATE meta SET value = '[\"system\", 1]' WHERE key = 'root_order'"], 2.0),
    ('root-order-duplicate', ["UPDATE meta SET value = '[\"schema_version\",\"system\",\"shared\",\"profiles\","
                              "\"future_root_key\",\"system\"]' WHERE key = 'root_order'"], 2.0),
    ('root-order-lacks-key', ["UPDATE meta SET value = '[\"system\",\"shared\",\"profiles\"]' WHERE key = 'root_order'"], 2.0),
    ('root-order-lacks-section', ["UPDATE meta SET value = '[\"schema_version\",\"shared\",\"profiles\","
                                  "\"future_root_key\"]' WHERE key = 'root_order'"], 2.0),
    ('root-order-lacks-profiles', ["UPDATE meta SET value = '[\"schema_version\",\"system\",\"shared\","
                                   "\"future_root_key\"]' WHERE key = 'root_order'"], 2.0),
    ('field-not-json', ["UPDATE fields SET value = '[1,' WHERE scope = 'shared' AND field = 'nodes'"], 2.0),
    ('field-without-section', ['PRAGMA foreign_keys=OFF', "DELETE FROM sections WHERE scope = 'profile' AND owner = 'zeta'"], 2.0),
    ('equal-positions', ["UPDATE fields SET position = 0 WHERE scope = 'profile'", "UPDATE sections SET position = 0"], 2.0),
    ('version-string', ["UPDATE meta SET value = '\"x\\u00e9''\"' WHERE key = 'storage_version'"], 2.0),
    ('version-zero', ["UPDATE meta SET value = '0' WHERE key = 'storage_version'"], 2.0),
    ('version-bool', ["UPDATE meta SET value = 'true' WHERE key = 'storage_version'"], 2.0),
    ('version-float', ["UPDATE meta SET value = '1.0' WHERE key = 'storage_version'"], 2.0),
    ('version-null', ["UPDATE meta SET value = 'null' WHERE key = 'storage_version'"], 2.0),
    ('version-list', ["UPDATE meta SET value = '[1, \"a\", null]' WHERE key = 'storage_version'"], 2.0),
    ('version-newer', ["UPDATE meta SET value = '2' WHERE key = 'storage_version'"], 2.0),
    ('version-huge', ["UPDATE meta SET value = '123456789012345678901234567890' WHERE key = 'storage_version'"], 2.0),
    ('version-missing', ["DELETE FROM meta WHERE key IN ('storage_version', 'store_id')"], 2.0),
    ('table-missing', ['DROP TABLE fields'], 2.0),
    ('tables-missing', ['DROP TABLE fields', 'DROP TABLE sections'], 2.0),
    ('wal', ['PRAGMA journal_mode=WAL'], 2.0),
    # Added by the C01 review: row values neither implementation writes.
    ('value-blob', ["UPDATE fields SET value = CAST('[2]' AS BLOB) WHERE scope = 'shared' AND field = 'nodes'"], 2.0),
    ('value-integer', ["UPDATE fields SET value = 5 WHERE scope = 'shared' AND field = 'nodes'"], 2.0),
    ('value-bad-utf8', ["UPDATE fields SET value = CAST(X'5B22FF2041E4B8225D' AS TEXT) "
                        "WHERE scope = 'shared' AND field = 'nodes'"], 2.0),
    ('meta-bad-utf8', ["UPDATE meta SET value = CAST(X'22F09F9822' AS TEXT) WHERE key = 'migration'"], 2.0),
    ('owner-bad-utf8', ['PRAGMA foreign_keys=OFF',
                        "UPDATE fields SET owner = CAST(X'7AED' AS TEXT) WHERE owner = 'zeta'",
                        "UPDATE sections SET owner = CAST(X'7AED' AS TEXT) WHERE owner = 'zeta'"], 2.0),
    ('generation-negative', ["UPDATE meta SET value = '-3' WHERE key = 'generation'"], 2.0),
    ('fields-trigger-fails', ["CREATE TRIGGER no_writes BEFORE INSERT ON fields BEGIN "
                              "SELECT RAISE(ABORT, '只读：拒绝写入'); END"], 2.0),
]


def outcome(call):
    try:
        value = call()
    except Exception as error:  # noqa: BLE001 - the point is to record whatever is raised
        return {'type': type(error).__name__, 'message': str(error)}
    return {'type': None, 'message': None, 'value': value}


def commit_new_root_key(target):
    entry = target.load()
    committed = target.commit(entry, {**entry.document, 'added_by_commit': [1, '例']})
    return None if committed is None else [committed.head.generation, dumps(committed.document), committed.size]


def errors(directory):
    directory = Path(directory)
    base = directory / 'base.sqlite3'
    store(base).create(sample_document(), {'migration': {'source': 'defaults'}})
    results = []
    for name, setup, timeout in SCENARIOS:
        path = directory / f'{name}.sqlite3'
        holder = None
        if setup == 'garbage':
            path.write_bytes(b'this is not a database at all\n' * 200)
        elif setup == 'missing':
            pass
        else:
            shutil.copyfile(base, path)
            if setup == 'locked':
                holder = sqlite3.connect(path, timeout=0, isolation_level=None)
                holder.execute('BEGIN EXCLUSIVE')
            else:
                connection = sqlite3.connect(path, isolation_level=None)
                for statement in setup:
                    connection.execute(statement)
                connection.close()
        target = ConfigStore(path, lambda: timeout)
        results.append({
            'name': name, 'setup': setup, 'timeout': timeout,
            # load before check: check repairs the journal mode
            'load': outcome(lambda: dumps(target.load().document)),
            'head': outcome(lambda: list(target.load().head)),
            'check': outcome(target.check),
            'salvage': outcome(lambda: None if target.salvage_meta() is None else dumps(target.salvage_meta())),
            # last: it may change the file
            'commit': outcome(lambda: commit_new_root_key(target)),
        })
        if holder is not None:
            holder.execute('ROLLBACK')
            holder.close()
    return {'scenarios': {'items': results}}


ODD_SEQUENCES = [
    # documents committed one after another (the first is created); unusual
    # key orders, empty sections and keys, profiles without fields
    [{}, {'profiles': {}}, {'system': {}, 'shared': {}, 'profiles': {'p': {}}}, {}],
    [{'': 1, 'root': {'x': 1}, 'system': {'': '', 'z': 1, 'a': 2},
      'profiles': {'z': {'': None}, 'a': {'b': 1}, '例🚀': {'k': [1.0, -0.0, 1e16, 5e-324, 2 ** 70, 1.25]}}},
     {'system': {'a': 2, 'z': 1, '': ''}, 'profiles': {'a': {'b': 1}, '例🚀': {'k': [1.0]}, 'z': {'': None}},
      'root': {'x': 1}, '': 1},
     {'shared': {'k': 'sep\u2028\u2029 nul\x00'}, 'profiles': {'z': {'': None, 'n': 1}},
      'system': {'z': 1}, 'root': {'x': 1}, '': 1, 'added': True},
     {'profiles': {'z': {'n': 1, '': None}}, 'shared': {'k': 'sep\u2028\u2029 nul\x00'}, 'system': {'z': 1},
      'root': {'x': 2}, 'added': True, '': 1},
     {'schema_version': 5}],
]


def odd(directory):
    results = []
    for index, documents in enumerate(ODD_SEQUENCES):
        path = Path(directory) / f'odd-{index}.sqlite3'
        target = store(path)
        steps = []
        entry = target.create(documents[0], {})
        for number, document in enumerate(documents):
            committed = None
            if number:
                committed = target.commit(entry, copy.deepcopy(document))
                entry = committed or entry
            loaded = target.load()
            steps.append({'document': dumps(document), 'committed': number == 0 or committed is not None,
                          'entry': dumps(entry.document), 'loaded': dumps(loaded.document),
                          'generation': loaded.head.generation, 'size': loaded.size, 'dump': dump(path)})
        results.append(steps)
    return {'sequences': results}


def race(path, mode, timeout):
    """Concurrent commits with a Go writer (TestCrossCheckPythonConcurrentCommits).

    mode 'wait': load, print 'loaded', wait for a line on stdin, then commit.
    mode 'hold': load and commit, printing 'holding' while the write
    transaction is open (all statements done, COMMIT delayed by 0.6 s).
    Prints 'ok <generation>' or '<exception type> <message>' at the end.
    """
    import time
    target = ConfigStore(Path(path), lambda: timeout)
    entry = target.load()
    if mode == 'hold':
        def delayed_commit(connection):
            print('holding', flush=True)
            time.sleep(0.6)
            connection.execute('COMMIT')
        target._commit = delayed_commit
    print('loaded', flush=True)
    if mode == 'wait':
        sys.stdin.readline()
    try:
        committed = target.commit(entry, {**entry.document, 'python_commit': mode})
        print('ok', committed.head.generation, flush=True)
    except Exception as error:  # noqa: BLE001
        print(type(error).__name__, error, flush=True)


def main():
    if sys.argv[1] == 'race':
        race(sys.argv[2], sys.argv[3], float(sys.argv[4]))
        return
    command, target, output = sys.argv[1:4]
    result = {'create': create, 'dump': dump, 'commit': one_more_commit, 'errors': errors, 'odd': odd}[command](target)
    Path(output).write_text(json.dumps(result, ensure_ascii=False), encoding='utf-8')


if __name__ == '__main__':
    main()
