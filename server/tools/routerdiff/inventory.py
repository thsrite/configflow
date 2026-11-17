"""Read the reviewed routers matrix without claiming executable coverage.

Design entries and executable request variants are separate: an entry is covered
only when a concrete recipe names it and its Python reference prerequisites pass.
"""
from dataclasses import asdict, dataclass
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[3]
MATRIX = ROOT / 'server/parity/difftest-routers.md'


@dataclass(frozen=True)
class Route:
    ident: str
    method: str
    path: str

    @property
    def signature(self):
        return self.method + ' ' + self.path

    @property
    def go_path(self):
        return re.sub(r'<(?:[^:>]+:)?([^>]+)>', r'{\1}', self.path)


@dataclass(frozen=True)
class Entry:
    ident: str
    kind: str
    description: str
    source_line: int
    route: str = ''


def cells(line):
    """Split Markdown tables, retaining literal/escaped pipes inside code."""
    result, buffer = [], []
    quoted = False
    escaped = False
    for char in line.strip():
        if char == '|' and not quoted and not escaped:
            result.append(''.join(buffer).strip())
            buffer = []
        else:
            buffer.append(char)
        if char == '`' and not escaped:
            quoted = not quoted
        if char == '\\' and not escaped:
            escaped = True
        else:
            escaped = False
    result.append(''.join(buffer).strip())
    if result and not result[0]:
        result.pop(0)
    if result and not result[-1]:
        result.pop()
    return result


def load(path=MATRIX):
    routes, entries = {}, {}
    section = ''

    def add(ident, kind, description, number, route=''):
        if ident in entries:
            raise ValueError(f'duplicate design entry {ident} at line {number}')
        entries[ident] = Entry(ident, kind, description.strip(), number, route)

    def scenarios(text, route, number):
        chunks = re.split(r'(?:<br>)?\*\*([SFB][0-9]{2})\*\*\s*', text)
        for index in range(1, len(chunks), 2):
            add(route + '-' + chunks[index], 'route', chunks[index + 1], number, route)

    for number, line in enumerate(Path(path).read_text().splitlines(), 1):
        if line.startswith('## '):
            section = line[3:]
        row = cells(line) if line.startswith('|') else []
        if section == '每条路由覆盖矩阵' and row:
            match = re.fullmatch(r'\*\*(R[0-9]{3})\*\* `([A-Z]+) ([^`]+)`', row[0])
            if match:
                ident, method, url = match.groups()
                if len(row) != 7:
                    raise ValueError(f'{ident}: expected seven matrix columns at line {number}')
                routes[ident] = Route(ident, method, url)
                for value in row[1:4]:
                    scenarios(value, ident, number)
        elif section == '[O6] 逐路由补充用例' and row and re.fullmatch(r'R[0-9]{3}', row[0]):
            scenarios(row[1], row[0], number)
        elif section == '设备协议：请求序列、分批、回读与补偿' and row:
            match = re.match(r'(D[0-9]{2})\b', row[0])
            if match:
                add(match[1], 'device', ' | '.join(row), number)
        elif section == '[O6] 横切补充用例（X01–X14）' and row:
            match = re.match(r'(X[0-9]{2})\b', row[0])
            if match:
                add(match[1], 'crosscutting', ' | '.join(row), number)
        if section == 'Python逐错误、只读与诊断分支 H' and row:
            match = re.match(r'(H[0-9]{4}) / ', row[0])
            if match:
                add(match[1], 'helper', ' | '.join(row), number)
        if section in ('生成计划、脚本与修复命令形态', '长时间运行、轮询、并发与上传下载边界',
                       '[O6] 横切补充用例（X01–X14）'):
            match = re.match(r'- \*\*([GT][0-9]{2})(?:\*\*| )', line)
            if match:
                ident = match[1]
                add(ident, 'generated' if ident.startswith('G') else 'sequence', line[2:], number)
        if section == '共享身份、数据与输入 fixture' and row:
            match = re.match(r'(ADDR|SECRET|RECEIPT)\b', row[0])
            if match:
                add('fixture-' + match[1], 'fixture', ' | '.join(row), number)
        if section == '[O6] 设备协议与fake合约补充':
            match = re.match(r'- \*\*(iKuai fake|RouterOS fake|规则下载fake[^*]*)\*\*', line)
            if match:
                key = {'iKuai fake': 'ikuai', 'RouterOS fake': 'routeros'}.get(match[1], 'download')
                add('fake-' + key, 'fake-contract', line[2:], number)

    # O6 defines this shared recipe on R002–R026 rather than repeating 25 rows.
    for ident in routes:
        if ident != 'R001':
            add(ident + '-F90', 'route', 'X02(a): unauthenticated missing/invalid profile precedes login', 222, ident)
    if len(routes) != 26 or len(entries) != 2301:
        raise ValueError(f'reviewed inventory changed: routes={len(routes)}, entries={len(entries)}')
    signatures = [(route.method, route.path) for route in routes.values()]
    if len(signatures) != len(set(signatures)):
        raise ValueError('duplicate route signature')
    return routes, entries


def declared_route_table(path=None):
    """Return declarations only; todo/done is never an implementation oracle."""
    path = path or ROOT / 'server/parity/routes.tsv'
    selected = {}
    for line in Path(path).read_text().splitlines():
        fields = line.split('\t')
        if len(fields) >= 3 and fields[2].split('.')[1:2] in (['routers'], ['client_devices'], ['upstream_router_pending']):
            selected[(fields[0], fields[1])] = fields[2]
    return selected


def design_json():
    routes, entries = load()
    return {'routes': [asdict(route) for route in routes.values()],
            'entries': [asdict(entry) for entry in entries.values()]}
