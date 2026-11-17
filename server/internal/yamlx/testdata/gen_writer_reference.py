"""Record semantic references for the old dump/spec corpus without changing it.

Run on a test server from server/:
  ~/cf-test/.venv/bin/python internal/yamlx/testdata/gen_writer_reference.py

The reference comes only from real Python safe_load of the old Python output.
The old style options become one canonical native writer, so safe_u supplies the
dump value; spec rows retain their own recorded output. No Go code participates.
"""

import gzip
import hashlib
import json
from pathlib import Path

import yaml
from writer_graph import graph

root = Path(__file__).resolve().parent
refs = []
counts = {}
with (root / "corpus.jsonl").open(encoding="utf-8") as stream:
    for row, line in enumerate(stream):
        entry = json.loads(line)
        kind = entry["kind"]
        if kind not in ("dump", "spec"):
            continue
        text = entry["out"]["safe_u"] if kind == "dump" else entry["out"]
        if not isinstance(text, str):
            raise ValueError(f"unexpected Python dump rejection in row {row}")
        refs.append(dict(corpus="corpus.jsonl", row=row,
                         sha256=hashlib.sha256(text.encode()).hexdigest(),
                         nodes=graph(yaml.safe_load(text))))
        counts[kind] = counts.get(kind, 0) + 1

with (root / "writer_reference.json.gz").open("wb") as target:
    with gzip.GzipFile(filename="", fileobj=target, mode="wb", mtime=0) as stream:
        stream.write(json.dumps(refs, ensure_ascii=True, separators=(",", ":")).encode())
print(json.dumps(dict(rows=len(refs), kinds=counts), sort_keys=True))
