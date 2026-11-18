"""Bind approved D2 archive cache effects to the original frozen C28 input.

Run with the real backend reference Python from the repository root. No Go
output participates in deriving a path or an expected outcome.
"""
import gzip
import hashlib
import json
from pathlib import Path
from backend.utils.rule_utils import rule_cache_filename

root = Path(__file__).parent
archive = root / "c28_recorded.jsonl.gz"
lines = gzip.decompress(archive.read_bytes()).splitlines()
rows = [json.loads(line) for line in lines]
references = []
for index in (72, 78):
    row = rows[index]
    assert not row.get("err") and row["world"]["files"] == row["after"]
    name = row["args"][2][0]["name"]
    references.append({"index": index, "test": row["test"],
                       "input_sha256": hashlib.sha256(lines[index]).hexdigest(),
                       "kind": "unowned_refusal",
                       "error": f"规则“{name}”下载失败：exception_type=Timeout"})
for index, source_id in ((102, "regex"), (103, "regex"), (113, "regex"),
                         (114, "regex"), (249, "mixed"), (305, "source"), (338, "remote")):
    row = rows[index]
    config = row["args"][0]
    rule = next(item for item in config.get("rule_library", []) + config["rule_configs"]
                if item.get("id") == source_id)
    old = "rules/" + rule_cache_filename(rule)  # actual Python function
    assert old in row["after"]
    identity = rule.get("library_rule_id") or rule.get("id", rule.get("name", ""))
    declaration = [identity, rule["url"], rule.get("behavior", "domain"), rule.get("format") or ""]
    # This is the declared native JSON boundary, independently serialized here.
    encoded = json.dumps(declaration, ensure_ascii=False, separators=(",", ":"))
    encoded = encoded.replace("&", "\\u0026").replace("<", "\\u003c").replace(">", "\\u003e")
    native = "rules/by-source/" + hashlib.sha256(encoded.encode()).hexdigest() + ".list"
    assert native != old
    references.append({"index": index, "test": row["test"],
                       "input_sha256": hashlib.sha256(lines[index]).hexdigest(),
                       "kind": "native_cache", "old": old, "native": native,
                       "retain_old": old in row["world"]["files"]})
output = {"archive_sha256": hashlib.sha256(archive.read_bytes()).hexdigest(), "rows": references}
(root / "n89_archive_cache_expected.json").write_text(json.dumps(output, ensure_ascii=False, indent=2) + "\n")
print("ARCHIVE D2 REFERENCE: 2 exact unowned refusals; 7 exact cache transitions; original archive unchanged")
