"""Bind native metric arithmetic and strict stored-identity JSON policies.

Run real Python metrics with only the interpreter integer digit cap disabled.
Original archives and the Python business implementation are never rewritten.
"""
from pathlib import Path
import gzip
import hashlib
import json
import sys

from backend.agents import oxidns_metrics
from backend.agents import oxidns_monitor as monitor

root = Path(__file__).resolve().parent
fixture_path = root / "n810_state_native.json"
fixture = json.loads(fixture_path.read_bytes())
fixture["rows"] = [row for row in fixture["rows"] if row["section"] not in ("metrics", "target", "overview_snapshot")]
sys.set_int_max_str_digits(0)
changed = {}


def bind(source, section, row, want):
    raw = json.dumps(row, sort_keys=True, separators=(",", ":"), ensure_ascii=False).replace("\u2028", "\\u2028").replace("\u2029", "\\u2029").encode()
    fixture["rows"].append(dict(source=source, section=section, row=row,
                                sha256=hashlib.sha256(raw).hexdigest(), want=want))


for source, count in (("a06_reference.json.gz", 2), ("a06_review.json.gz", 13)):
    archive = json.loads(gzip.decompress((root / source).read_bytes()))
    fixture["sources"][source] = hashlib.sha256((root / source).read_bytes()).hexdigest()
    changed[source] = 0
    for row in archive["metrics"]:
        try:
            native = dict(parsed=oxidns_metrics.parse_metrics(row["text"]),
                          summary=oxidns_metrics.summarize_metrics(row["text"]))
        except ValueError:
            continue
        if native == json.loads(row["result"]):
            continue
        bind(source, "metrics", row, native)
        changed[source] += 1
    assert changed[source] == count, changed


def reject_constant(value):
    raise ValueError("stored identity must be strict JSON")


source = "a06_review.json.gz"
archive = json.loads(gzip.decompress((root / source).read_bytes()))
changed["target"] = 0
for row in archive["target"]:
    try:
        json.loads(row[0], parse_constant=reject_constant)
    except ValueError:
        bind(source, "target", row, {"refused": True})
        changed["target"] += 1
assert changed["target"] == 17, changed
# The same accepted metric rule propagates through one real overview snapshot.
changed["overview_snapshot"] = 0
for row in archive["overview_snapshot"]:
    seen = []
    original = monitor._request
    def fake(agent, endpoint, params=None, *, min_version=monitor.MIN_AGENT_VERSION):
        seen.append(dict(endpoint=endpoint, params=list((params or {}).items()), min_version=min_version))
        return json.loads(row["data"])
    monitor._request = fake
    try:
        _, core, recorders, observations, labels = monitor._overview_snapshot({}, row["dashboard"])
        summary = oxidns_metrics.summarize_observations(observations)
        summary["upstreams"] = monitor._public_upstreams(summary["upstreams"], labels)
        native = dict(result=dict(core=core, recorders=recorders, summary=summary), seen=seen)
    except Exception:
        continue
    finally:
        monitor._request = original
    if native == json.loads(row["result"]):
        continue
    bind(source, "overview_snapshot", row, native)
    changed["overview_snapshot"] += 1
assert changed["overview_snapshot"] == 1, changed
fixture_path.write_text(json.dumps(fixture, ensure_ascii=False, indent=2) + "\n")
print(f"Bound independent native policies: {changed}; original archives unchanged")
