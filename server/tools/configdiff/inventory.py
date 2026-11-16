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


def admin_auth_rows():
    out = []
    for line in MATRIX.read_text().splitlines():
        row = re.match(r'^\| \*\*(R\d{3})\*\* `([^`]+)`', line)
        ids = re.search(r'\*\*F(\d+)–(\d+)\*\* A1–7', line)
        if row and ids:
            if int(ids[2]) - int(ids[1]) != 6:
                raise ValueError('matrix auth range no longer has seven entries')
            out.append((row[1], row[2], int(ids[1])))
    return out


def declared_states():
    return {(method, path): status for line in (ROOT / 'server/parity/routes.tsv').read_text().splitlines()
            if line and not line.startswith('#')
            for method, path, _handler, _package, status in [line.split('\t')]}


def missing_profile_rows():
    out = []
    for line in MATRIX.read_text().splitlines():
        row = re.match(r'^\| \*\*(R\d{3})\*\* `([^`]+)`', line)
        ids = re.search(r'\*\*F(\d+)–(\d+)\*\* P1–2', line)
        if row and ids:
            if int(ids[2]) - int(ids[1]) != 1:
                raise ValueError('profile failure pair must have exactly two IDs')
            out.append((row[1], row[2], int(ids[1])))
    return out
