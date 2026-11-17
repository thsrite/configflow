"""A19's four approved N7-3d changes: real Python, only YAML loaders replaced."""
import copy
import gzip
import hashlib
import json
from pathlib import Path
import sys
from unittest.mock import patch

import yaml
from backend.common import linux_gateway as reference

ROOT = Path(__file__).resolve().parent
MODE = "gateway-payload"
ROWS = (111, 130, 209, 210)


def identity(text):
    return hashlib.sha256((MODE + "\0" + text).encode()).hexdigest()


def save(path, value):
    data = json.dumps(value, ensure_ascii=True, separators=(",", ":"), sort_keys=True).encode()
    with Path(path).open("wb") as file:
        with gzip.GzipFile(filename="", fileobj=file, mode="wb", mtime=0) as target:
            target.write(data)


def outcome(args):
    try:
        result = reference.extract_gateway_cidrs(args["content"], args["behavior"])
        return {"out": [str(value) for value in result]}
    except Exception as error:
        return {"err": [type(error).__name__, str(error)]}


def unpack(wire):
    if wire["kind"] == "scalar":
        return wire["value"]
    if wire["kind"] == "sequence":
        return [unpack(value) for value in wire["items"]]
    if wire["kind"] == "mapping":
        return {unpack(pair[0]): unpack(pair[1]) for pair in wire["pairs"]}
    raise AssertionError("unexpected native loader shape")


if sys.argv[1] == "collect":
    with gzip.open(ROOT / "a19_corpus.jsonl.gz", "rt") as stream:
        corpus = [json.loads(line) for line in stream]
    sources, cases, current = {}, {}, []
    original = yaml.load
    safe = yaml.safe_load

    def note(text):
        key = identity(text)
        sources[key] = {"mode": MODE, "source": text}
        if key not in current:
            current.append(key)

    def loader(text, *args, **kwargs):
        note(text)
        return original(text, *args, **kwargs)

    def safe_loader(text, *args, **kwargs):
        note(text)
        return safe(text, *args, **kwargs)

    with patch.object(yaml, "load", loader), patch.object(yaml, "safe_load", safe_loader):
        for index in ROWS:
            row = corpus[index]
            assert row["fn"] == "extract"
            current = []
            old = outcome(copy.deepcopy(row["args"]))
            assert old == {key: row[key] for key in ("out", "err") if key in row}, index
            cases[str(index)] = dict(args=row["args"], old=old, old_loads=current[:])
    Path(sys.argv[2]).write_text(json.dumps(dict(sources=sources, cases=cases), ensure_ascii=True))
    print("verified original rows", len(cases), "actual loader sources", len(sources))
else:
    collected = json.loads(Path(sys.argv[2]).read_text())
    native = json.loads(Path(sys.argv[3]).read_text())
    current = []

    def loader(text, *args, **kwargs):
        key = identity(text)
        if key not in current:
            current.append(key)
        record = native[key]
        assert record["source"] == text and record["mode"] == MODE
        if record.get("error"):
            raise yaml.YAMLError(record["error"])
        return unpack(record["native"])

    cases = {}
    with patch.object(yaml, "load", loader), patch.object(yaml, "safe_load", loader):
        for index, row in collected["cases"].items():
            current = []
            expected = outcome(copy.deepcopy(row["args"]))
            classes = sorted({kind for key in set(row["old_loads"] + current) for kind in native[key]["classes"] if kind != "equal"})
            assert expected != row["old"] and classes and "UNCLASSIFIED" not in classes, index
            cases["gateway/a19/" + index] = dict(old=row["old"], expected=expected, changed=True, classes=classes,
                                               args=row["args"], old_loads=row["old_loads"], native_loads=current[:])
    save(sys.argv[4], dict(cases=cases, stats=dict(changed=4, classified=4, unknown=0),
                           corpus_sha256=hashlib.sha256((ROOT / "a19_corpus.jsonl.gz").read_bytes()).hexdigest()))
    save(sys.argv[5], native)
    print("changed=4 classified=4 unknown=0")
