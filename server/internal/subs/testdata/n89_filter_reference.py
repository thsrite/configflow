"""Real Python filtering on the shared model's consumption view.

Only consumed names/regex fields are projected. The returned items are the
untouched source records selected by the real filter, not copies rewritten by
this projection. No Go output is read; the original corpus stays immutable.
"""
import contextlib
import gzip
import hashlib
import io
import json
import pathlib

with contextlib.redirect_stdout(io.StringIO()):
    from backend.utils import subscription_filter as filters

root = pathlib.Path(__file__).parent
source = (root / "filter.jsonl.gz").read_bytes()
rows = gzip.decompress(source).decode().rstrip("\n").split("\n")


class Number(str):
    pass


def text(value, fallback=""):
    if isinstance(value, (Number, str)):
        return str(value)
    return fallback


cases = {}
for index, line in enumerate(rows):
    row = json.loads(line)
    if row["fn"] != "filter_subscription_items":
        continue
    value = row["in"]
    tokens = json.loads(line, parse_int=Number, parse_float=Number)["in"]
    original = value["items"] or []
    sub = tokens["sub"] if isinstance(tokens["sub"], dict) else {}
    projected_sub = {"name": text(sub.get("name")), "regex_filter": text(sub.get("regex_filter"), None)}
    projected_items = []
    for position, (item, token) in enumerate(zip(original, tokens["items"] or [])):
        if isinstance(item, dict):
            item = dict(item, name=text(token.get("name")), _n89_filter_index=position)
        projected_items.append(item)
    with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
        selected = filters.filter_subscription_items(projected_sub, projected_items)
        compiled = filters.compile_subscription_filter(projected_sub)
    # Without a compiled filter, primitive items are retained in place too.
    if compiled is None:
        expected = original
    else:
        expected = [original[item["_n89_filter_index"]] for item in selected]
    if expected != row["out"]:
        pattern = projected_sub["regex_filter"] or ""
        if "\\N{" in pattern or "$" in pattern and '\\n"' in json.dumps(original):
            continue  # existing, separately documented regex-engine boundaries
        cases[str(index)] = {"input_sha256": hashlib.sha256(line.encode()).hexdigest(), "output": expected}

result = {"source_sha256": hashlib.sha256(source).hexdigest(), "cases": cases}
(root / "n89_filter_reference.json").write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n")
print(f"native filter reference: {len(cases)} exact real-Python projected cases")
