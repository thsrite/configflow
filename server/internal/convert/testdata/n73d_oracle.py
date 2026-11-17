"""N7-3d loader-only + approved native writer oracle.

Run on the test server with native reader/writer executable paths. Each source
and its raw Parse graph/native physical values is journaled before injection.
Only yaml.load/safe_load and yaml.dump/safe_dump are replaced. The existing
synthetic corpus environment (gen_mihomo) and business functions are unchanged.
All old answers are reproduced first, with the old golden files left untouched.
"""
import collections
import contextlib
import copy
import gzip
import hashlib
import importlib.util
import inspect
import json
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parent
REPO = ROOT.parents[3]
sys.path.insert(0, str(ROOT))
sys.path.insert(0, str(REPO / "server/internal/yamlx/testdata"))
arguments = sys.argv[:]
sys.argv = [sys.argv[0]]
import gen_mihomo as gm
sys.argv = arguments
import yaml

spec = importlib.util.spec_from_file_location("n74_writer", REPO / "server/internal/installer/testdata/n74_writer_oracle.py")
w = importlib.util.module_from_spec(spec)
spec.loader.exec_module(w)


def packed(value):
    return json.dumps(value, ensure_ascii=True, sort_keys=True, separators=(",", ":"), allow_nan=True).encode()


def digest(value):
    return hashlib.sha256(packed(value)).hexdigest()


def save(name, value):
    with (ROOT / name).open("wb") as target:
        with gzip.GzipFile(filename="", fileobj=target, mode="wb", mtime=0) as stream:
            stream.write(json.dumps(value, ensure_ascii=True, separators=(",", ":")).encode())


class Reader:
    def __init__(self, executable):
        self.process = subprocess.Popen([executable], stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True)
        self.sources = {}
        self.journal = open("/tmp/n73d-reader-journal.jsonl", "w")
        self.trace = []

    def load(self, source, *args, **kwargs):
        mode = "convert-proxy" if any(frame.function == "_parse_structured_proxy_string" for frame in inspect.stack()) else "convert-document"
        identity = hashlib.sha256((mode + "\0" + source).encode()).hexdigest()
        if identity not in self.sources:
            self.process.stdin.write(json.dumps(dict(mode=mode, source=source), ensure_ascii=True) + "\n")
            self.process.stdin.flush()
            record = json.loads(self.process.stdout.readline())
            if record.get("unclassified"):
                raise RuntimeError("unclassified loader: " + record["unclassified"])
            self.sources[identity] = record
            self.journal.write(json.dumps(dict(id=identity, **record), ensure_ascii=True) + "\n")
            self.journal.flush()
        self.trace.append(identity)
        record = self.sources[identity]
        if record.get("error"):
            raise yaml.YAMLError(record["error"])
        nodes = record["values"]
        memo = {}

        def value(identity):
            if identity in memo:
                return memo[identity]
            node = nodes[identity - 1]
            if node["kind"] == "scalar":
                return node.get("value")
            if node["kind"] == "mapping":
                result = memo[identity] = {}
                for pair in node.get("pairs", []):
                    result[pair["key"]] = value(pair["value"])
            else:
                result = memo[identity] = []
                result.extend(value(child) for child in node.get("items", []))
            return result
        return value(1)

    @contextlib.contextmanager
    def hooks(self):
        originals = yaml.load, yaml.safe_load
        yaml.load, yaml.safe_load = self.load, self.load
        try:
            yield
        finally:
            yaml.load, yaml.safe_load = originals

    def close(self):
        self.journal.close()
        self.process.stdin.close()
        assert self.process.wait() == 0


def invoke(row):
    if row["fn"] == "generate":
        args = copy.deepcopy(row["args"])
        profile = args.pop("profile")
        # Reuse the original corpus environment; invoke the real function once
        # and retain the review corpus's partial report even on failure.
        gm.set_caches(row.get("caches", {}), row.get("downloads", {}))
        report, result = {}, {}
        try:
            result["out"] = gm.mihomo.generate_mihomo_config(profile, args["base_url"], args["sync_lan_hosts"],
                preflight_providers=False, region_probe=args["region_probe"], report=report,
                mihomo_core_version=args["core_version"], for_agent=args["for_agent"])
        except Exception as error:
            result["err"] = [type(error).__name__, str(error)]
        if "out" in result or "report" in row:
            result["report"] = gm.conv(report)
        result["mutated"] = gm.conv(profile)
        return result
    args = copy.deepcopy(row["args"])
    gm.set_caches(row.get("caches", {}), row.get("downloads", {}))
    try:
        function = gm.mihomo.get_mihomo_ruleset_downloads if row["fn"] == "ruleset_downloads" else gm.mihomo.get_mihomo_provider_downloads
        return dict(out=gm.conv(function(*args)))
    except Exception as error:
        return dict(err=[type(error).__name__, str(error)])


def main():
    reader, writer = Reader(sys.argv[1]), w.NativeWriter(sys.argv[2])
    counts, checked, classes = collections.Counter(), collections.Counter(), collections.Counter()
    expected = {}
    try:
        for name in ("mihomo_corpus.jsonl.gz", "review_mihomo.jsonl.gz"):
            with gzip.open(ROOT / name, "rt") as stream:
                for index, line in enumerate(stream):
                    row = json.loads(line)
                    if row["fn"] not in ("generate", "ruleset_downloads", "provider_downloads") or row.get("gateway_fixture"):
                        continue
                    # The review's special three-argument download path does not
                    # read YAML; its unchanged original expectation stays strict.
                    if row["fn"] != "generate" and len(row["args"]) != 2:
                        continue
                    reference = {key: row[key] for key in ("out", "err", "report", "mutated") if key in row}
                    before = invoke(row)
                    if packed(before) != packed(reference):
                        # Review reports empty report even on failure.
                        if not (before.get("err") and row.get("report") == {} and packed(dict(before, report={})) == packed(reference)):
                            raise ValueError(f"old golden not reproduced: {name}:{index}/{row['fn']}")
                        before["report"] = {}
                    checked[name] += 1
                    with w.writer_hooks(writer, True, []):
                        writer_only = invoke(row)
                    reader.trace = []
                    with reader.hooks(), w.writer_hooks(writer, True, []):
                        after = invoke(row)
                    if "report" in before and "report" not in after and after.get("err"):
                        after["report"] = {}
                    # Document reads after generation are a real public Python
                    # boundary in review_mihomo, evaluated through the same loader.
                    if "document" in row and "out" in after:
                        after["document"] = gm.conv(reader.load(after["out"]))
                        writer_only["document"] = gm.conv(yaml.safe_load(writer_only["out"]))
                    labels = sorted({reader.sources[key]["class"] for key in reader.trace} - {"equal"})
                    # Layout belongs to N7-4, so compare only the real reader
                    # effects against the writer-only stage. No value normalize.
                    if packed(after) == packed(writer_only):
                        continue
                    if not labels:
                        raise ValueError(f"unclassified expected change: {name}:{index}")
                    identity = name + ":" + str(index)
                    expected[identity] = dict(fn=row["fn"], args=row["args"], old=reference, args_sha256=digest(row["args"]), old_sha256=digest(reference),
                                              expected=after, sources=sorted(set(reader.trace)), classes=labels)
                    counts[name] += 1
                    classes.update(labels)
        save("n73d_reader_values.json.gz", reader.sources)
        save("n73d_writer_graphs.json.gz", writer.graphs)
        save("n73d_expected.json.gz", expected)
        summary = dict(checked=checked, changed=counts, classified=counts, classes=classes, unknown=0,
                       sources=len(reader.sources), modes=collections.Counter(record["mode"] for record in reader.sources.values()))
        (ROOT / "n73d_manifest.json").write_text(json.dumps(summary, ensure_ascii=True, sort_keys=True, indent=2) + "\n")
        print(json.dumps(summary, sort_keys=True))
    finally:
        reader.close()
        writer.close()


if __name__ == "__main__":
    main()
