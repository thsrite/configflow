"""Record only real Python safe_load values of unchanged Mihomo output goldens.

Run on a test server from server/:
  ~/cf-test/.venv/bin/python internal/convert/testdata/gen_writer_oracle.py > /tmp/n74-writer-graphs.jsonl
  go run ./tools/yamlwriteroracle -in /tmp/n74-writer-graphs.jsonl -out internal/convert/testdata/mihomo_writer_oracle.json.gz

The Go filter selects old-output native interpretations that disagree with these
graphs. Neither generator invokes Go business code or rewrites an old golden.
"""

import base64
import datetime
import gzip
import hashlib
import json
import math
from pathlib import Path

import yaml


def graph(value):
    nodes, memo = [], {}

    def visit(value):
        container = isinstance(value, (dict, list))
        if container and id(value) in memo:
            return memo[id(value)]
        index = len(nodes)
        node = {}
        nodes.append(node)
        if container:
            memo[id(value)] = index
        if value is None:
            tag, text = "null", "null"
        elif isinstance(value, bool):
            tag, text = "bool", str(value).lower()
        elif isinstance(value, int):
            tag, text = "int", str(value)
        elif isinstance(value, float):
            tag = "float"
            text = ".nan" if math.isnan(value) else ("-.inf" if value < 0 else ".inf") if math.isinf(value) else repr(value)
        elif isinstance(value, str):
            tag, text = "str", value
        elif isinstance(value, bytes):
            tag, text = "binary", base64.encodebytes(value).decode("ascii")
        elif isinstance(value, datetime.datetime):
            tag, text = "timestamp", value.isoformat(" ")
        elif isinstance(value, datetime.date):
            tag, text = "timestamp", value.isoformat()
        elif isinstance(value, dict):
            node.update(kind=4, tag="!!map", children=[])
            for key, child in value.items():
                node["children"].extend((visit(key), visit(child)))
            return index
        elif isinstance(value, list):
            node.update(kind=2, tag="!!seq", children=[visit(child) for child in value])
            return index
        else:
            raise TypeError(type(value))
        node.update(kind=8, tag="!!" + tag, value=text)
        return index

    assert visit(value) == 0
    return nodes


root = Path(__file__).resolve().parent
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
