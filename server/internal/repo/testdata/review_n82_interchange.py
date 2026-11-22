"""Private-copy audit through the real ConfigStore and ProfileRepository APIs.

No contents or exception diagnostics are emitted; Go discards child output.
"""
import contextlib
import copy
import json
import logging
import os
from pathlib import Path
import sqlite3
import sys

sys.path.insert(0, os.environ['CONFIGFLOW_REPO'])
logging.disable(logging.CRITICAL)
from backend.common.config_store import ConfigStore
from backend.common.config_repository import ProfileRepository


def same(actual, expected):
    assert type(actual) is type(expected)
    if isinstance(actual, dict):
        assert list(actual) == list(expected)
        for key in actual:
            same(actual[key], expected[key])
    elif isinstance(actual, list):
        assert len(actual) == len(expected)
        for left, right in zip(actual, expected):
            same(left, right)
    else:
        assert actual == expected


def rows(path):
    with sqlite3.connect(path) as connection:
        return [connection.execute('SELECT * FROM '+table+' ORDER BY '+order).fetchall()
                for table, order in [('meta', 'key'), ('sections', 'scope,owner'),
                                     ('fields', 'scope,owner,field')]]


def main():
    mode, directory, other = sys.argv[1:]
    path = Path(directory) / 'configflow.sqlite3'
    if mode == 'copy':
        with sqlite3.connect(Path(other).resolve().as_uri()+'?mode=ro', uri=True) as source:
            with sqlite3.connect(path) as destination:
                source.backup(destination)
        os.chmod(path, 0o600)
        return
    repository = ProfileRepository(Path(directory))
    store = ConfigStore(path, lambda: 10)
    if mode == 'dump':
        # Python's view of the database for a value comparison in Go (plain
        # records, O18 T1c): written to the private run directory only.
        target = Path(other)
        target.write_text(json.dumps({'document': store.load().document,
                                      'export': repository.export_all()}, ensure_ascii=False))
        os.chmod(target, 0o600)
        return
    if mode == 'audit':
        expected = json.loads(Path(other).read_text())
        same(store.load().document, expected)
        same(repository.export_all(), expected)
        return
    if mode == 'edit':
        before = store.load()
        profile_id = next(iter(before.document['profiles']))
        original = copy.deepcopy(before.document)
        old_revision = original['profiles'][profile_id]['_revision']
        def update(config):
            config.setdefault("mihomo", {})["n82_review_python"] = {"marker": True, "opaque": {"z": 1.0, "a": None}}
        repository.update_profile_transaction(profile_id, update)
        updated = store.load()
        assert updated.head.generation == before.head.generation+1
        assert updated.document['profiles'][profile_id]['_revision'] == old_revision+1
        expected = copy.deepcopy(original)
        expected["profiles"][profile_id].setdefault("mihomo", {})["n82_review_python"] = {"marker": True, "opaque": {"z": 1.0, "a": None}}
        expected['profiles'][profile_id]['_revision'] += 1
        expected['profiles'][profile_id]['updated_at'] = updated.document['profiles'][profile_id]['updated_at']
        same(updated.document, expected)
        Path(other).write_text(json.dumps(expected, ensure_ascii=False))
        return
    raise ValueError('unknown mode')


if __name__ == '__main__':
    with open(os.devnull, 'w') as sink, contextlib.redirect_stdout(sink), contextlib.redirect_stderr(sink):
        main()
