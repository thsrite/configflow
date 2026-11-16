"""Matrix IDs and declared Go state; no business source introspection."""
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[3]
MATRIX = ROOT / 'server/parity/difftest-config.md'


def routes():
    return {route: rid for rid, route in re.findall(r'^\| \*\*(R\d{3})\*\* `([^`]+)`', MATRIX.read_text(), re.M)}


def cases():
    out = {}
    for line in MATRIX.read_text().splitlines():
        row = re.match(r'^\| \*\*(R\d{3})\*\* `([^`]+)`', line)
        if row:
            for category, first, last in re.findall(r'\*\*([SFB])(\d+)(?:–(\d+))?(?: \[O6\])?\*\*', line):
                for n in range(int(first), int(last or first) + 1):
                    out[f'{row[1]}-{category}{n:02}'] = row[2]
    return out
