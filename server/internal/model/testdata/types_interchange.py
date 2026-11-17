"""Real Python JSON/SQLite boundary; never print stored values or user map keys."""
import json
import os
import sqlite3
import sys

mode, source, target = sys.argv[1:]
if mode == 'snapshot':
    # The original is opened read-only. Only the private backup is queried, and
    # it is deleted before returning. The Go harness removes its 0700 directory.
    private = os.path.join(os.path.dirname(target), 'private.sqlite3')
    original = sqlite3.connect('file:' + source + '?mode=ro', uri=True)
    copy = sqlite3.connect(private)
    try:
        original.backup(copy)
        rows = list(copy.execute('select field,value from fields order by scope,owner,position'))
    finally:
        copy.close()
        original.close()
        os.remove(private)
    with open(target, 'w') as handle:
        handle.write('[' + ','.join('{"name":' + json.dumps(name) + ',"value":' + value + '}' for name, value in rows) + ']')
elif mode == 'write':
    with open(source) as handle:
        rows = json.load(handle)
    with open(target, 'w') as handle:
        json.dump(rows, handle, ensure_ascii=True)
elif mode == 'audit':
    with open(source) as handle:
        before = json.load(handle)
    with open(target) as handle:
        after = json.load(handle)
    if before != after:
        raise SystemExit('boundary structural mismatch')
else:
    raise SystemExit('unknown boundary mode')
