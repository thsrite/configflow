"""Independent reference for typed candidate reads and D2 provider paths.
Original corpus and review archives are never modified. All actual candidate
selection/rendering/lowering is performed by the repository's Python module.
"""
import ast
import copy
import gzip
import hashlib
import json
from pathlib import Path
from types import SimpleNamespace
from backend.utils import group_candidates as gc

root = Path(__file__).parent

def text(value):
    if isinstance(value, str):
        return value
    if isinstance(value, (int, float)) and not isinstance(value, bool):
        return json.dumps(value, ensure_ascii=False, separators=(",", ":"))
    return None

def texts(value):
    if not isinstance(value, list):
        return None
    out = []
    for item in value:
        if item is None:
            out.append("")
        elif text(item) is not None:
            out.append(text(item))
        else:
            return None
    return out

def group(source):
    # Only the declared model.ProxyGroup fields consumed by these readers.
    result = {key: text(source.get(key)) or "" for key in ("id", "name", "type")}
    if isinstance(source.get("enabled"), bool):
        result["enabled"] = source["enabled"]
    for key in ("manual_nodes", "subscriptions", "aggregations", "include_groups", "proxies"):
        value = texts(source.get(key))
        if value is not None:
            result[key] = value
    for key in ("follow_group", "source", "proxy_order", "regex", "exclude_regex", "aggregation_regex", "aggregation_exclude_regex"):
        value = text(source.get(key))
        if value is not None:
            result[key] = value
    value = source.get("proxies_order")
    if isinstance(value, list) and all(isinstance(item, dict) for item in value):
        result["proxies_order"] = [{key: text(item.get(key)) or "" for key in ("type", "id")} for item in value]
    return result

def catalog(source):
    result = {"proxy_groups": [group(item) for item in source.get("proxy_groups", []) if isinstance(item, dict)]}
    for collection in ("nodes", "subscriptions", "subscription_aggregations"):
        result[collection] = []
        for item in source.get(collection, []):
            if not isinstance(item, dict):
                continue
            typed = {key: text(item.get(key)) or "" for key in ("id", "name")}
            if isinstance(item.get("enabled"), bool):
                typed["enabled"] = item["enabled"]
            if collection == "subscription_aggregations":
                typed["subscriptions"] = texts(item.get("subscriptions")) or []
            result[collection].append(typed)
    return result

def canonical(value):
    # encoding/json's standard compact representation, recorded outside Go.
    data = json.dumps(value, ensure_ascii=False, separators=(",", ":"))
    for char, escaped in (("&", "\\u0026"), ("<", "\\u003c"), (">", "\\u003e"), ("\u2028", "\\u2028"), ("\u2029", "\\u2029")):
        data = data.replace(char, escaped)
    return data.encode()

real_sha256 = hashlib.sha256
# Lowering alone owns a repr(tuple) digest. Intercept only that scalar key;
# the Python routine still creates and orders every provider and output field.
def native_key_hash(data):
    key = ast.literal_eval(data.decode())
    assert isinstance(key, tuple) and len(key) == 3 and all(isinstance(item, str) for item in key)
    return real_sha256(canonical(key))

def lower(row, native=True):
    main = copy.deepcopy(row["main"])
    previous = gc.hashlib
    gc.hashlib = SimpleNamespace(sha256=native_key_hash) if native else previous
    try:
        gc.lower_group_provider_filters(catalog(row["config"]), main)
    finally:
        gc.hashlib = previous
    return main

archive = root / "corpus.jsonl.gz"
review_path = root / "review_c09.json.gz"
groups = []
lowered = []
with gzip.open(archive, "rt") as stream:
    for line in stream:
        row = json.loads(line)
        if row["kind"] == "group" and row.get("what") != "merge":
            full = dict(row["shared"], **row["profile"])
            selected = gc.managed_members(group(row["group"]))
            expected = {"members": [list(item) for item in selected["members"]],
                        "selection": {key: selected[key] for key in ("nodes", "subscriptions", "aggregations", "groups", "order")},
                        "render": gc.render_members(group(row["group"]), catalog(full)),
                        "providers": [[item["id"], include, exclude] for item, include, exclude in gc.managed_providers(group(row["group"]), catalog(full))]}
            groups.append({"input": row, "native": expected,
                           "class": "unchanged" if all(expected[key] == row[key] for key in expected) else "R5"})
        if row["kind"] == "lower":
            if str(row["result"]).startswith("__exception__"):
                lowered.append(None)
            else:
                expected = lower(row)
                typed_python = lower(row, native=False)
                kinds = []
                if typed_python != row["result"]:
                    kinds.append("R5")
                if expected != typed_python:
                    kinds.append("D2")
                lowered.append({"input": row, "native": expected, "class": "+".join(kinds) or "unchanged"})
review = json.loads(gzip.decompress(review_path.read_bytes()))
review_lower = []
for row in review["lower"]:
    expected = lower(row)
    typed_python = lower(row, native=False)
    kinds = []
    if typed_python != row["result"]:
        kinds.append("R5")
    if expected != typed_python:
        kinds.append("D2")
    review_lower.append({"input": row, "native": expected, "class": "+".join(kinds) or "unchanged"})
result = {"archive_sha256": real_sha256(archive.read_bytes()).hexdigest(),
          "review_sha256": real_sha256(review_path.read_bytes()).hexdigest(),
          "groups": groups, "lower": lowered, "review_lower": review_lower}
(root / "n89_group_expected.json").write_text(json.dumps(result, ensure_ascii=False, separators=(",", ":")) + "\n")
print("typed group rows:", len(groups), "; lower rows:", len(lowered), "; review lower rows:", len(review_lower))
