"""Real Python storage boundary; no private values or exception text on stdout."""
import copy
import json
import os
from pathlib import Path
import sqlite3
import sys

sys.path.insert(0, os.environ['CONFIGFLOW_REPO'])
from backend.common.config_store import ConfigStore


def document(envelope):
    root = {f['Name']: f['Value'] for f in envelope.get('Root') or []}
    result = {}
    for key in envelope['Order']:
        if key in ('system', 'shared'):
            result[key] = {f['Name']: f['Value'] for f in envelope[key.title()]['Fields']}
        elif key == 'profiles':
            result[key] = {p['ID']: {f['Name']: f['Value'] for f in p['Fields']}
                           for p in envelope['Profiles']}
        else:
            result[key] = root[key]
    return result


def equivalent(actual, expected):
    # bool/int and scalar/container type changes must not hide behind Python ==.
    assert type(actual) is type(expected)
    if isinstance(actual, dict):
        assert list(actual) == list(expected)
        for key in actual:
            equivalent(actual[key], expected[key])
    elif isinstance(actual, list):
        assert len(actual) == len(expected)
        for left, right in zip(actual, expected):
            equivalent(left, right)
    else:
        assert actual == expected


def main():
    mode, path, other = sys.argv[1:]
    if mode == 'copy':
        # Never open the source read-write, including SQLite journal recovery.
        source = sqlite3.connect(Path(path).resolve().as_uri() + '?mode=ro', uri=True)
        target = sqlite3.connect(other)
        try:
            source.backup(target)
        finally:
            target.close()
            source.close()
        os.chmod(other, 0o600)
        return
    expected = document(json.loads(Path(other).read_text()))
    target = ConfigStore(Path(path), lambda: 5.0)
    if mode == 'fixture':
        target.create(expected, {'future_metadata': {'z': 1, 'a': '例'}})
        return
    target.check()
    entry = target.load()
    if mode == 'audit-final':
        expected['n8_2_python_probe'] = {'written': 'Python', 'number': 1.0}
    equivalent(entry.document, expected)
    # Check object/section/profile order as well as JSON values.
    assert list(entry.document) == list(expected)
    assert list(entry.document.get('profiles', {})) == list(expected.get('profiles', {}))
    if mode == 'audit-edit':
        updated = copy.deepcopy(entry.document)
        updated['n8_2_python_probe'] = {'written': 'Python', 'number': 1.0}
        committed = target.commit(entry, updated)
        assert committed.head.generation == entry.head.generation + 1
        equivalent(target.load().document, updated)


if __name__ == '__main__':
    main()
