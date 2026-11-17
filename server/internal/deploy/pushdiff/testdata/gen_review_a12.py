"""Independent synthetic A12 review oracle; run through rt only."""
import json
import gzip
from pathlib import Path

from backend.agents import push_diff as p


def nested(value):
    return "{child: " * 80 + value + "}" * 80


rows = []
for name, old, new in [
    ("deep-list", "items: [" + nested("before") + "]", "items: [" + nested("after") + "]"),
    ("numeric-list", "items: [1.00, before]", "items: [1.0, after]"),
    ("numeric-key", "1.00: before", "1.0: after"),
    ("typed-list", "items: [1, before]", "items: [1.0, after]"),
]:
    expected = p.diff_config(old.encode(), new.encode())
    expected.pop("raw", None)
    rows.append(dict(kind="config", name=name, old=old, new=new, expected=expected))
for name, old, new in [
    ("deep-node", "proxies: [{name: alpha, custom: " + nested("before") + "}]",
     "proxies: [{name: alpha, custom: " + nested("after") + "}]"),
    ("numeric-node", "proxies: [{name: alpha, port: 443, ratio: 1.00}]",
     "proxies: [{name: alpha, port: 443, ratio: 1.0}]"),
    ("typed-node", "proxies: [{name: alpha, ratio: 1}]", "proxies: [{name: alpha, ratio: 1.0}]"),
    ("typed-map-key", "proxies: [{name: alpha, custom: {1: value}}]",
     "proxies: [{name: alpha, custom: {'1': value}}]"),
]:
    rows.append(dict(kind="nodes", name=name, old=old, new=new,
                     expected=p._node_change(old.encode(), new.encode(), p._DetailBudget())))
for text in [
    r"https://rules.example.com/?token=synthetic\tail",
    r"https://user:synthetic\tail@rules.example.com/path",
    r"https://rules.example.com/prefix\part?api_key=synthetic-tail",
    "中文 https://user:synthetic@rules.example.com/path?api_key=value",
    "https://rules.example.com/?access_token=synthetic%2Bvalue",
    "https://rules.example.com/abc1234567890123456789012345",
    "user:synthetic@192.0.2.1:1080",
]:
    rows.append(dict(kind="spans", name=text, text=text,
                     expected=[text[a:b] for a, b in p.secret_spans(text)]))

# Every credential key and suffix, including data-key exclusions and continued
# scalar values. Expectations are from Python, not copies of Go's key tables.
for key in sorted(p._SECRET_KEYS) + ["custom" + s for s in p._SECRET_SUFFIXES]:
    for key in [key, key.upper().replace("-", "_")]:
        old = "mode: rule\n"
        new = json.dumps({key: "Bearer\nsynthetic-secret\ncontinued-secret"})
        expected = p.diff_config(old.encode(), new.encode())
        expected.pop("raw", None)
        rows.append(dict(kind="config", name="credential-key-" + key,
                         old=old, new=new, expected=expected))

rules = []
for i in range(713):
    # Interleaved public CJK matches, marked URL credentials and stored tokens.
    rules.append(f"+.香港-{i}.example.com" if i % 3 == 0 else
                 f"https://rules.example.com/?token=private-{i}" if i % 3 == 1 else
                 f"+.stored-synthetic-{i}.example.com")
old, new = b"+.old.example.com\n", ("\n".join(rules) + "\n").encode()
keep = p._Keep()
summary, details = p._rule_change(old, new, p._DetailBudget(), keep)
entries = keep.claim()
pages = []
for change in ["added", "removed"]:
    for query in ["", "香港", "private", "stored-synthetic", "example.com"]:
        for offset in [0, 19, 20, 49, 50, 199, 200, 712, 713]:
            page = entries.page(change, offset, 50, p.search_query(query))
            pages.append(dict(change=change, offset=offset, query=query, expected=page))
rows.append(dict(kind="paging", name="cjk-search-across-pages", old=old.decode(), new=new.decode(),
                 expected=dict(summary=summary, details=details), pages=pages))

for name, before_char, after_char in [("html", "&", "<"), ("cjk", "汉", "语"),
                                      ("js-separators", "\u2028", "\u2029")]:
    old = json.dumps({"proxies": [{"name": f"node-{i}", **{f"f{j:02}": before_char * 240 for j in range(20)}}
                                  for i in range(40)]}, ensure_ascii=True).encode()
    new = json.dumps({"proxies": [{"name": f"node-{i}", **{f"f{j:02}": after_char * 240 for j in range(20)}}
                                  for i in range(40)]}, ensure_ascii=True).encode()
    keep = p._Keep()
    summary, details = p._node_change(old, new, p._DetailBudget(), keep)
    entries = keep.claim()
    page = entries.page("modified", 0, 50)
    rows.append(dict(kind="node-budget", name=name, old=old.decode(), new=new.decode(),
                     expected=dict(shown=len(details["groups"][0]["entries"]),
                                   available=details["groups"][0]["available"],
                                   page_count=len(page["entries"]), next_offset=page["next_offset"])))

path = Path(__file__).with_name("review.json.gz")
with gzip.GzipFile(filename=str(path), mode="wb", mtime=0) as output:
    output.write(json.dumps(rows, ensure_ascii=False, separators=(",", ":")).encode())
print(f"A12 independent Python review: {len(rows)} rows")
