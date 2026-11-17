"""Record only real Python safe_load values of unchanged Mihomo output goldens.

Run on a test server from server/:
  ~/cf-test/.venv/bin/python internal/convert/testdata/gen_writer_oracle.py > /tmp/n74-writer-graphs.jsonl
  go run ./tools/yamlwriteroracle -in /tmp/n74-writer-graphs.jsonl -out internal/convert/testdata/mihomo_writer_oracle.json.gz

The Go filter selects old-output native interpretations that disagree with these
graphs. Neither generator invokes Go business code or rewrites an old golden.
"""

import gzip
import hashlib
import json
import sys
from pathlib import Path

import yaml




root = Path(__file__).resolve().parent
sys.path.insert(0, str(root.parents[1] / "yamlx" / "testdata"))
from writer_graph import graph  # noqa: E402

for name in ("mihomo_corpus.jsonl.gz", "mihomo_recorded.jsonl.gz", "review_mihomo.jsonl.gz"):
    with gzip.open(root / name, "rt", encoding="utf-8") as stream:
        for row, line in enumerate(stream):
            entry = json.loads(line)
            if entry.get("fn") != "generate" or entry.get("err") or not isinstance(entry.get("out"), str):
                continue
            text = entry["out"]
            reference = dict(corpus=name, row=row, sha256=hashlib.sha256(text.encode()).hexdigest(), text=text,
                             nodes=graph(yaml.safe_load(text)))
            print(json.dumps(reference, ensure_ascii=True, separators=(",", ":")))
