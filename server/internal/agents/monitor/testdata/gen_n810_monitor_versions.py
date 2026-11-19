"""Bind native ASCII/count decisions to complete, immutable Python rows.

Run on the test server. No Go implementation or output is read. The old archives
remain unchanged; the independent policy uses ASCII release syntax and unlimited
integer components. Public machine versions also retain the existing 64-byte
version.txt alphabet/budget, rather than accepting interpreter error triggers.
"""
from pathlib import Path
import gzip
import hashlib
import json
import re
import sys

root = Path(__file__).resolve().parent
sources = {}
rows = []
release = re.compile(r"v?([0-9]+)\.([0-9]+)\.([0-9]+)\Z", re.ASCII)
version = re.compile(r"[A-Za-z0-9][A-Za-z0-9._+-]{0,63}\Z", re.ASCII)
sys.set_int_max_str_digits(0)

def newer(a, b):
    left, right = release.fullmatch(a), release.fullmatch(b)
    return bool(left and right and tuple(map(int, left.groups())) > tuple(map(int, right.groups())))

def add(source, section, row, want):
    sources[source] = hashlib.sha256((root / source).read_bytes()).hexdigest()
    raw = json.dumps(row, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()
    rows.append(dict(source=source, section=section, row=row, sha256=hashlib.sha256(raw).hexdigest(), want=want))

source = "a07_reference.json.gz"
archive = json.loads(gzip.decompress((root / source).read_bytes()))
for row in archive["newer_release"]:
    want = newer(row[0], row[1])
    if row[2] != want:
        add(source, "newer_release", row, dict(result=want))
source = "a07_review.json"
archive = json.loads((root / source).read_bytes())
for row in archive["newer_release"]:
    want = dict(result=newer(row[0], row[1]))
    if row[2] != want:
        add(source, "newer_release", row, want)
for row in archive["core_update"]:
    if not version.fullmatch(row[0]):
        add(source, "core_update", row, dict(error="INVALID_RESPONSE", status=502, message="Mihomo 返回的数据格式异常。"))
(root / "n810_versions_native.json").write_text(json.dumps(dict(sources=sources, rows=rows), ensure_ascii=False, indent=2) + "\n")
from collections import Counter
print(Counter((row["source"], row["section"]) for row in rows))
