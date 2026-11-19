"""Pin explicit native-machine-version expectations to unchanged Python rows.

The coordinator approved ASCII shared-counter components on 2026-11-19.
Expected results below are literal policy decisions, never derived from Go.
"""
import hashlib
import json
from pathlib import Path

base = Path(__file__).parent
original = (base / "a02_reference.json").read_bytes()
rows = json.loads(original)["compare_versions"]
expected = {
    ("１.2", "1.10"): 1,
    ("𝟘", "1"): 1,
    ("𝟙", "1"): 1,
    ("𝟢", "1"): 1,
    ("𝟬", "3"): 1,
    ("٣", "4"): 1,
    ("৯", "10"): 1,
    ("1" * 4301, "2"): 1,
    ("1.٣_٣", "1.33"): 1,
}
result = {"corpus_sha256": hashlib.sha256(original).hexdigest(), "rows": []}
for index, row in enumerate(rows):
    key = (row["a"], row["b"])
    if key not in expected:
        continue
    encoded = json.dumps(row, ensure_ascii=False, separators=(",", ":")).encode()
    assert row["want"] != expected[key], "override must identify a real old/new difference"
    result["rows"].append({"index": index, "sha256": hashlib.sha256(encoded).hexdigest(),
                           "python_want": row["want"], "native_want": expected.pop(key)})
assert not expected, "a complete original row disappeared"
assert len(result["rows"]) == 9
(base / "n810_version_native.json").write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n")
print("bound 9 complete original rows; original archive unchanged")
