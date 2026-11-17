"""N7-4 writer-only oracle; no Python business source or non-writer hook changes.

From repository root on a test server:
  (cd server && go build -o /tmp/n74-yamlwriter ./tools/yamlwriteroracle)
  ~/cf-test/.venv/bin/python server/internal/installer/testdata/n74_writer_oracle.py /tmp/n74-yamlwriter

First reproduce and strictly validate every selected legacy output/hash. Record
real yaml.dump/safe_dump input graphs, then replay with only those endpoints
replaced by native Marshal. Each typed graph is journaled before rendering, and
the rendered bytes are journaled before Python receives them. Unmodified Python
postprocessing produces complete expected answers. Unclassified non-YAML,
semantic or later writer-input changes are fatal; approved Unicode restorations
are recorded explicitly, including derived command fields.
"""

import base64
import collections
import contextlib
import copy
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import zipfile

REPO = Path(__file__).resolve().parents[4]
ROOT = Path(__file__).resolve().parent
sys.path.insert(0, str(REPO))
sys.path.insert(0, str(REPO / "server/internal/yamlx/testdata"))
os.chdir(REPO)

import yaml  # noqa: E402
from writer_graph import graph  # noqa: E402
from backend.agents.docker_ha_compact import generate_compact  # noqa: E402
from backend.agents.docker_ha_compose_direct import generate_direct  # noqa: E402
from backend.agents.docker_ha_compose import generate_bundle  # noqa: E402
from backend.agents.docker_ha_guide import generate_guide  # noqa: E402
from backend.agents.install_script import generate_docker_agent_compose  # noqa: E402


def packed(value):
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode()


def digest(value):
    return hashlib.sha256(packed(value)).hexdigest()


def rows(path):
    stream = gzip.open(path, "rt", encoding="utf-8") if path.suffix == ".gz" else path.open(encoding="utf-8")
    with stream:
        for index, line in enumerate(stream):
            yield index, json.loads(line)


def cases():
    for path in sorted(ROOT.glob("docker_ha*.jsonl.gz")):
        for index, row in rows(path):
            # The original Go replay already excludes these test-monkeypatched
            # internals; reproducing them would require a non-writer patch.
            if isinstance(row.get("patched"), list):
                continue
            if row.get("kind") == "guide" and row.get("ok"):
                request = row["input"]
                yield path.name, index, "generate_guide", [request["request"], request.get("enrollment")], row
            elif row.get("fn") in FUNCTIONS and "out" in row:
                yield path.name, index, row["fn"], row["args"], row
    for name in ("matrix.jsonl", "fuzz.jsonl.gz"):
        for index, row in rows(ROOT / name):
            if row.get("fn") == "compose" and row.get("ok"):
                yield name, index, "compose", row["kwargs"], row


def project_legacy(actual, reference):
    """Validate the two historical ZIP hash encodings, not new baselines."""
    if isinstance(actual, dict) and isinstance(reference, dict):
        actual = dict(actual)
        if "content_sha256" in reference and "content_base64" in actual:
            encoded = actual.pop("content_base64")
            hashes = (hashlib.sha256(base64.b64decode(encoded)).hexdigest(),
                      hashlib.sha256(encoded.encode()).hexdigest())
            if reference["content_sha256"] not in hashes:
                raise ValueError("legacy ZIP digest mismatch")
            actual["content_sha256"] = reference["content_sha256"]
        return {key: project_legacy(value, reference.get(key)) for key, value in actual.items()}
    if isinstance(actual, (list, tuple)) and isinstance(reference, list):
        return [project_legacy(value, reference[i] if i < len(reference) else None) for i, value in enumerate(actual)]
    return actual


def validate_legacy(value, row):
    if row.get("fn") == "compose":
        raw = value.encode()
        if hashlib.sha256(raw).hexdigest() != row["sha256"] or len(raw) != row["length"]:
            raise ValueError("legacy Compose output/hash changed")
    elif "digests" in row:
        if set(value) != set(row["digests"]):
            raise ValueError("legacy guide fields changed")
        for key, old in row["digests"].items():
            if digest(value[key])[:24] != old:
                raise ValueError(f"legacy guide digest changed: {key}")
    else:
        expected = row.get("out", row.get("result"))
        if packed(project_legacy(value, expected)) != packed(expected):
            raise ValueError("legacy full answer changed")


FUNCTIONS = {"generate_guide": generate_guide, "generate_compact": generate_compact,
             "generate_direct": generate_direct, "generate_bundle": generate_bundle,
             "compose": generate_docker_agent_compose}


def invoke(fn, args):
    args = copy.deepcopy(args)
    return FUNCTIONS[fn](**args) if fn == "compose" else FUNCTIONS[fn](*args)


class NativeWriter:
    def __init__(self, executable):
        self.process = subprocess.Popen([executable, "-stream"], stdin=subprocess.PIPE,
                                        stdout=subprocess.PIPE, text=True, encoding="utf-8")
        self.graphs = {}
        self.journal = open("/tmp/n74-writer-source-journal.jsonl", "w", encoding="utf-8")

    def request(self, value):
        self.process.stdin.write(json.dumps(value, ensure_ascii=True, separators=(",", ":")) + "\n")
        self.process.stdin.flush()
        line = self.process.stdout.readline()
        if not line:
            raise RuntimeError("native writer exited before replying")
        answer = json.loads(line)
        if "error" in answer:
            raise RuntimeError(answer["error"])
        return answer

    def record(self, value):
        nodes = graph(value, writer_styles=True)
        identity = digest(nodes)
        if identity not in self.graphs:
            self.graphs[identity] = {"nodes": nodes}
            self.journal.write(json.dumps(dict(id=identity, nodes=nodes), ensure_ascii=True) + "\n")
            self.journal.flush()
        return identity

    def render(self, identity):
        value = self.graphs[identity]
        if "yaml" not in value:
            value["yaml"] = self.request(value)["yaml"]
            self.journal.write(json.dumps(dict(id=identity, yaml=value["yaml"]), ensure_ascii=True) + "\n")
            self.journal.flush()
        return value["yaml"]

    def close(self):
        self.journal.close()
        self.process.stdin.close()
        if self.process.wait(timeout=30):
            raise RuntimeError("native writer failed")


@contextlib.contextmanager
def writer_hooks(native, inject, trace):
    originals = {"dump": yaml.dump, "safe_dump": yaml.safe_dump}
    depth = 0

    def wrap(name):
        def writer(value, *args, **kwargs):
            nonlocal depth
            if depth:
                return originals[name](value, *args, **kwargs)
            depth += 1
            try:
                identity = native.record(value)
                trace.append(dict(method=name, graph=identity))
                if not inject:
                    return originals[name](value, *args, **kwargs)
                output = native.render(identity)
                stream = args[0] if args else kwargs.get("stream")
                if stream is not None:
                    stream.write(output)
                    return None
                return output
            finally:
                depth -= 1
        return writer

    yaml.dump, yaml.safe_dump = wrap("dump"), wrap("safe_dump")
    try:
        yield
    finally:
        yaml.dump, yaml.safe_dump = originals["dump"], originals["safe_dump"]


def classify_yaml(native, old, new, path):
    answer = native.request(dict(op="compare", old=old, new=new))
    changes = answer.get("changes") or []
    for change in changes:
        change["path"] = path + change["path"]
    return changes or [dict(category="layout", path=path)]


def classify_zip(native, old, new, path):
    with zipfile.ZipFile(io.BytesIO(base64.b64decode(old))) as a, zipfile.ZipFile(io.BytesIO(base64.b64decode(new))) as b:
        if a.namelist() != b.namelist():
            raise ValueError(f"ZIP member change at {path}")
        changes = []
        for x, y in zip(a.infolist(), b.infolist()):
            for key in ("external_attr", "compress_type", "create_system", "flag_bits", "date_time", "comment", "extra"):
                if getattr(x, key) != getattr(y, key):
                    raise ValueError(f"ZIP metadata change at {path}/{x.filename}/{key}")
            before, after = a.read(x), b.read(y)
            if before == after:
                continue
            if x.filename != "compose.yaml":
                raise ValueError(f"non-YAML archive bytes changed at {path}/{x.filename}")
            changes.extend(classify_yaml(native, before.decode(), after.decode(), path + "/compose.yaml"))
        return changes


def classify(native, old, new, fn, path=""):
    if packed(old) == packed(new):
        return []
    if isinstance(old, str) and isinstance(new, str):
        if path.endswith(".content_base64"):
            return classify_zip(native, old, new, path)
        if path.endswith(".compose") or path == "[0]" and fn != "compose" or path == "" and fn == "compose":
            return classify_yaml(native, old, new, path)
        restored = {f"U+{ord(ch):04X}": new.count(ch) - old.count(ch)
                    for ch in "\u0085\u2028\u2029" if new.count(ch) > old.count(ch)}
        if restored:
            # A restored bind source can also change the unmodified Python
            # business code's Docker command arguments after its YAML reread.
            return [dict(category="unicode-line-separator-preservation", path=path,
                         restored=restored, derived_command=True)]
        raise ValueError(f"non-YAML string changed at {path}")
    if isinstance(old, dict) and isinstance(new, dict) and set(old) == set(new):
        return [change for key in old for change in classify(native, old[key], new[key], fn, path + "." + key)]
    if isinstance(old, (tuple, list)) and isinstance(new, (tuple, list)) and len(old) == len(new):
        return [change for i, (a, b) in enumerate(zip(old, new)) for change in classify(native, a, b, fn, path + f"[{i}]")]
    raise ValueError(f"non-YAML shape/value changed at {path}")


def write_fixture(name, value):
    with (ROOT / name).open("wb") as target:
        with gzip.GzipFile(filename="", fileobj=target, mode="wb", mtime=0) as stream:
            stream.write(packed(value))


def main():
    native = NativeWriter(sys.argv[1])
    expected, cache = {}, {}
    counts, checked = collections.Counter(), collections.Counter()
    unicode_rows, unicode_fields, unicode_occurrences = collections.Counter(), collections.Counter(), collections.Counter()
    try:
        for filename, index, fn, args, row in cases():
            identity = digest(dict(fn=fn, args=args))
            if identity in cache:
                old, new, before, after, changes, input_changes = cache[identity]
            else:
                before, after = [], []
                with writer_hooks(native, False, before):
                    old = invoke(fn, args)
                try:
                    validate_legacy(old, row)
                except Exception as error:
                    raise RuntimeError(f"{filename}:{index}/{fn}: {error}") from error
                with writer_hooks(native, True, after):
                    new = invoke(fn, args)
                input_changes = []
                if len(before) != len(after):
                    raise ValueError(f"writer call count changed in {filename}:{index}/{fn}")
                for call_index, (a, b) in enumerate(zip(before, after)):
                    if a["method"] != b["method"]:
                        raise ValueError(f"writer method changed in {filename}:{index}/{fn}")
                    if a["graph"] != b["graph"]:
                        delta = classify_yaml(native, native.render(a["graph"]), native.render(b["graph"]), "")
                        input_changes.append(dict(call=call_index, before=a["graph"], after=b["graph"], changes=delta))
                try:
                    changes = classify(native, old, new, fn)
                except Exception as error:
                    raise RuntimeError(f"{filename}:{index}/{fn}: {error}") from error
                cache[identity] = old, new, before, after, changes, input_changes
            try:
                validate_legacy(old, row)
            except Exception as error:
                raise RuntimeError(f"{filename}:{index}/{fn}: {error}") from error
            checked[filename] += 1
            if not changes:
                continue
            categories = sorted({change["category"] for change in changes} - {"layout"}) or ["layout"]
            for category in categories:
                counts[category] += 1
            row_characters = set()
            for change in changes:
                for character, count in change.get("restored", {}).items():
                    row_characters.add(character)
                    unicode_fields[character] += 1
                    unicode_occurrences[character] += count
            unicode_rows.update(row_characters)
            # Layout-only multi-field answers retain their unchanged complete
            # legacy semantic expectation. Avoid duplicating megabytes of the
            # exact same shell scripts and ZIP bodies across thousands of rows.
            # Hash-only plain Compose rows need a stored YAML expectation.
            entry = expected.setdefault(identity, dict(fn=fn, args=args, categories=categories,
                                                       changes=changes, calls=after, input_changes=input_changes,
                                                       legacy_calls=before, sources=[], native_result_sha256=digest(new)))
            if categories != ["layout"] or fn == "compose":
                entry["expected"] = new
            else:
                entry["expected_from_legacy"] = True
            entry["sources"].append(dict(corpus=filename, row=index, old_result_sha256=digest(old)))
        write_fixture("n74_writer_graphs.json.gz", native.graphs)
        write_fixture("n74_writer_expected.json.gz", expected)
        summary = dict(checked=checked, classifications=counts, unique_expected=len(expected),
                       full_expected=sum("expected" in entry for entry in expected.values()),
                       graphs=len(native.graphs), unicode_rows=unicode_rows, unicode_fields=unicode_fields,
                       unicode_occurrences=unicode_occurrences, unknown=0)
        summary["source_sha256"] = {name: hashlib.sha256((ROOT / name).read_bytes()).hexdigest() for name in checked}
        (ROOT / "n74_writer_manifest.json").write_text(json.dumps(summary, ensure_ascii=True, sort_keys=True, indent=2) + "\n")
        print(json.dumps(summary, sort_keys=True))
    finally:
        native.close()


if __name__ == "__main__":
    main()
