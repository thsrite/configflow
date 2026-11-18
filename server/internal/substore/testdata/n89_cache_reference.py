"""Exact reference for the approved shared model projection; synthetic files only."""
import contextlib
import io
import json
import pathlib
import tempfile
from unittest import mock

with contextlib.redirect_stdout(io.StringIO()):
    from backend.utils import subscription_cache

stored = {"subscriptions": [{"id": 5}]}
projection = {"subscriptions": [{"id": "5"}]}
files = {"5.json": "synthetic-five", "drop.json": "synthetic-drop", "keep.json": "synthetic-keep", "notes.txt": "synthetic-note"}
with tempfile.TemporaryDirectory() as directory:
    root = pathlib.Path(directory)
    for name, content in files.items():
        (root / name).write_text(content, encoding="utf-8")

    class Repository:
        def shared_cache_dir(self):
            return root

        def get_shared(self):
            return projection

    with mock.patch.object(subscription_cache, "get_repository", return_value=Repository()):
        removed = subscription_cache.prune_orphan_caches()
    result = {"removed": sorted(removed), "files": {path.name: path.read_text(encoding="utf-8") for path in sorted(root.iterdir())}}
expected = {"stored": stored, "projection": projection, "files": files, "python": result}
path = pathlib.Path(__file__).with_name("n89_cache_reference.json")
path.write_text(json.dumps(expected, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
print("native cache reference: 1 exact projected Python case")
