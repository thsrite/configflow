"""Record independent typed-input Python/D2 expectations; archives stay intact.
Run from repository root with the reference Python environment.
"""
import gzip, hashlib, json
from pathlib import Path
from backend.utils.rule_utils import rule_cache_filename
from backend.utils.strategy_references import validate_rule_policies, StrategyReferenceError

root = Path(__file__).parent
archive = root / "corpus.jsonl.gz"

def text(value):
    if isinstance(value, str):
        return value
    if isinstance(value, (int, float)) and not isinstance(value, bool):
        return json.dumps(value, ensure_ascii=False, separators=(",", ":"))
    return None

def record(rule, original):
    # The model.String contract is applied to inputs before calling real Python.
    projected = {}
    for key in ("id", "library_rule_id", "name", "source_type", "url", "behavior", "format"):
        value = text(rule.get(key))
        if value is not None:
            projected[key] = value
    python = rule_cache_filename(projected)
    native = python
    # D2's declared digest representation is a compact native JSON array;
    # this is recorded here, never calculated by the Go implementation under test.
    if projected.get("source_type", "url") == "url" and projected.get("url"):
        identity = projected.get("library_rule_id") or projected.get("id")
        identity = str(identity if identity is not None else projected.get("name", ""))
        payload = [identity, projected["url"], projected.get("behavior", "domain"), projected.get("format") or ""]
        encoded = json.dumps(payload, ensure_ascii=False, separators=(",", ":"))
        native = "by-source/" + hashlib.sha256(encoded.encode()).hexdigest() + ".list"
    kinds = []
    if python != original:
        kinds.append("R5")
    if native != python:
        kinds.append("D2")
    return {"input": rule, "original": original, "python_typed": python, "native": native, "class": "+".join(kinds) or "unchanged"}

rows = []
with gzip.open(archive, "rt") as stream:
    for line in stream:
        row = json.loads(line)
        if row["kind"] == "cache" and row["what"] == "filename" and not str(row["result"]).startswith("__exception__"):
            rows.append(record(row["rule"], row["result"]))
namespace_cases = [
    {"id": "r1", "source_type": "content"},
    {"id": "r1", "library_rule_id": "lib", "source_type": "content"},
    {"name": "Name", "source_type": "content"},
    {"source_type": "content"},
    {"id": "r1", "url": "https://x.test/a.yaml", "behavior": "domain", "format": "yaml"},
    {"id": "r1", "url": "https://x.test/例"},
    {"id": "r1", "source_type": "url"},
]
namespace = [record(rule, rule_cache_filename(rule)) for rule in namespace_cases]
review_path = root / "review_c09.json.gz"
review = json.loads(gzip.decompress(review_path.read_bytes()))
policies = []
for row in review["policies"]:
    rules = []
    for source in row["config"].get("rule_configs", []):
        if not isinstance(source, dict):
            continue
        projected = {}
        for key in ("id", "name", "itemType", "policy"):
            value = text(source.get(key))
            if value is not None and (key != "id" or value != ""):
                projected[key] = value
        for key in ("enabled", "library_enabled"):
            if key in source:
                projected[key] = bool(source[key])
        rules.append(projected)
    try:
        validate_rule_policies({"rule_configs": rules}, row["names"], row["format"])
        result = ""
    except StrategyReferenceError as error:
        result = str(error)
    policies.append({"input": row, "native": result, "class": "unchanged" if result == row["result"] else "R5"})
output = {"archive_sha256": hashlib.sha256(archive.read_bytes()).hexdigest(), "rows": rows, "namespace": namespace,
          "review_sha256": hashlib.sha256(review_path.read_bytes()).hexdigest(), "policies": policies}
(root / "n89_cache_expected.json").write_text(json.dumps(output, ensure_ascii=False, indent=2) + "\n")
print("cache typed reference rows:", len(rows), "; namespace rows:", len(namespace), "; policy rows:", len(policies))
