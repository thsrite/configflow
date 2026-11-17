"""O6 pull HEAD parity and GET-only logs with an ordered trailing GET barrier."""
import json
import copy
import re
import time

from compare import differences
from harness import Pair, send
from urllib.parse import quote
from seed import PULL, CURRENT, RETIRED, AGENT
from cases.crosscutting import PULL_ROUTES, pull_path
from cases.setup import Stable
from cases.subscriptions import require

MARKER = '配置被拉取：'
HEADERS = ('content-type', 'content-disposition', 'cache-control', 'etag', 'last-modified',
           'vary', 'x-configflow-profile', 'content-length')
BEFORE, HEAD, AFTER = ('configdiff-before-head', 'configdiff-head', 'configdiff-after-head')


def events(raw):
    result = []
    try:
        lines = raw.decode('utf-8').splitlines()
    except UnicodeDecodeError:
        return [{'level': '<invalid-utf8>', 'message': '<invalid-utf8>', 'raw_hex': raw.hex()}]
    for line in lines:
        text, level = line, ''
        try:
            value = json.loads(line)
            if isinstance(value, dict):
                text = value.get('message', value.get('msg', line))
                level = str(value.get('level', '')).upper()
        except ValueError:
            pass
        if not isinstance(text, str) or MARKER not in text:
            continue
        prefix, message = text.split(MARKER, 1)
        if not level:
            match = re.search(r'\b(INFO|WARNING|ERROR|DEBUG|WARN)\b', prefix)
            level = match[1] if match else '<missing-level>'
        result.append({'level': level, 'message': MARKER + message})
    return result


class HeadPull(Stable):
    def __init__(self, pair, route, path):
        super().__init__(pair)
        self.route, self.path = route, path
        self.observed = [None, None]

    def raw(self, index):
        path = self.pair.dirs[index] / 'app.log'
        return path.read_bytes()[self.offset:] if path.exists() else b''

    def get(self, index, agent):
        return send(self.pair.bases[index], 'GET', self.path, headers={
            'Authorization': 'Bearer ' + self.pair.tokens[index], 'Host': 'panel.example', 'User-Agent': agent})

    def wait(self, index, agent):
        deadline = time.monotonic() + 2
        while time.monotonic() < deadline:
            if any(agent in row['message'] for row in events(self.raw(index))):
                return
            time.sleep(0.02)

    def before(self, index):
        super().before(index)
        path = self.pair.dirs[index] / 'app.log'
        self.offset = path.stat().st_size if path.exists() else 0
        self.first = self.get(index, BEFORE)
        self.wait(index, BEFORE)

    def after(self, index, reply):
        last = self.get(index, AFTER)
        self.wait(index, AFTER)
        super().after(index, reply)
        raw = self.raw(index)
        self.observed[index] = {'get_statuses': [self.first.status, last.status],
            'head_header_violations': [{'field': key, 'GET': self.first.headers.get(key, ''), 'HEAD': reply.headers.get(key, '')}
                for key in HEADERS if self.first.headers.get(key, '') != reply.headers.get(key, '')],
            'pull_events': events(raw), 'capabilities_absent': not any(value.encode() in raw for value in (PULL, CURRENT, RETIRED, AGENT))}
        if index == 0:
            service = next(name for name in ('mihomo', 'surge', 'loon', 'mosdns', 'oxidns') if '/' + name in self.route)
            owner = 'default' if '<profile_id>' in self.route else '默认'
            expected = {'get_statuses': [200, 200], 'head_header_violations': [], 'capabilities_absent': True,
                'pull_events': [{'level': 'INFO', 'message': '配置被拉取：' + service + '（配置空间 ' + owner
                    + '），HTTP 200，来源 127.0.0.1，客户端 ' + agent} for agent in (BEFORE, AFTER)]}
            require(not differences(expected, self.observed[0]), 'HEAD keeps own GET headers and emits no pull log between two real GETs: '
                    + repr(self.observed[0]))

    def compare(self, policy):
        return super().compare(policy) + differences(*self.observed, 'pull.head-and-log', policy)


def heads(binary, recorder):
    with Pair(binary, recorder, options={'config_token': PULL}) as pair:
        for route, rid in PULL_ROUTES:
            path = pull_path(route) + '?token=' + PULL
            pair.request(rid + '-B90', route, path, method='HEAD', expect=200, writes=True,
                headers={'Host': 'panel.example', 'User-Agent': HEAD}, interaction=HeadPull(pair, route, path),
                variant='HEAD-GET-header-parity-and-no-head-pull-log')


GROUPS = {'config-pull-head-and-real-logs': heads}

WARNING_AGENT = 'configdiff-failed-pull-client'


class WarningPull(HeadPull):
    def before(self, index):
        Stable.before(self, index)
        path = self.pair.dirs[index] / 'app.log'
        self.offset = path.stat().st_size if path.exists() else 0

    def after(self, index, reply):
        self.wait(index, WARNING_AGENT)
        Stable.after(self, index, reply)
        raw = self.raw(index)
        self.observed[index] = {'events': events(raw),
            'capabilities_absent': not any(value.encode() in raw for value in (PULL, CURRENT, RETIRED, AGENT))}
        if index == 0:
            expected = {'events': [{'level': 'WARNING', 'message': '配置被拉取：mihomo（配置空间 默认），HTTP '
                + str(reply.status) + '，来源 127.0.0.1，客户端 ' + WARNING_AGENT}], 'capabilities_absent': True}
            require(not differences(expected, self.observed[0]), 'unmatched pull subpath still emits one safe WARNING pull event')


class Tail(Stable):
    def __init__(self, pair):
        super().__init__(pair)
        self.lines = [None, None]

    def before(self, index):
        super().before(index)
        self.lines[index] = (self.pair.dirs[index] / 'app.log').read_text(errors='ignore').splitlines()

    def project(self, value, index):
        if not isinstance(value, dict):
            return value
        result = copy.deepcopy(value)
        rows = result.get('logs')
        if not isinstance(rows, list) or not all(isinstance(row, str) for row in rows):
            return result
        expected = [line for line in self.lines[index][-100:]
                    if MARKER in line and re.search(r'\bWARNING\b', line, re.IGNORECASE)]
        if rows != expected:
            result['logs'] = {'wrong-own-file-filter': rows, 'expected': expected}
        else:
            parsed = [events((row + '\n').encode()) for row in rows]
            result['logs'] = [items[0] if len(items) == 1 else {'unrecognized-row': row} for row, items in zip(rows, parsed)]
        total = result.get('total_lines')
        result['total_lines'] = '<validated-own-line-count>' if type(total) is int and total == len(self.lines[index]) else {
            'invalid-total': total, 'expected': len(self.lines[index])}
        result['capabilities_absent'] = not any(value in '\n'.join(rows) for value in (PULL, CURRENT, RETIRED, AGENT))
        return result

    def reference(self, reply):
        value = self.project(reply.json(), 0)
        require(value['success'] is True and value['filtered_lines'] == 1
                and value['total_lines'] == '<validated-own-line-count>' and value['capabilities_absent'] is True
                and len(value['logs']) == 1 and value['logs'][0]['level'] == 'WARNING'
                and WARNING_AGENT in value['logs'][0]['message'],
                'tail searches real emitted warning and validates counts against actual unmodified file')


def warning_and_tail(binary, recorder):
    with Pair(binary, recorder, options={'config_token': PULL}) as pair:
        path = '/api/config/mihomo/x?token=' + PULL
        pair.request('R030-B93', 'GET /api/config/mihomo', path, expect=404, writes=True, runtime_writes=True,
            headers={'User-Agent': WARNING_AGENT}, interaction=WarningPull(pair, 'GET /api/config/mihomo', path),
            variant='unknown-mihomo-subpath-still-logs-warning')
        for directory in pair.dirs:
            require((directory / 'app.log').is_file(), 'logger owns an actual file, not fixed test lines')
        pair.policy.bind(*[str(directory / 'app.log') for directory in pair.dirs],
                         r'/tmp/configdiff-[^/]+/(python|go)/app[.]log', 'real-tail-file')
        observer = Tail(pair)
        pair.request('R076-B91', 'GET /api/logs/tail', '/api/logs/tail?lines=100&level=warning&search=' + quote(MARKER),
            expect=200, writes=True, interaction=observer, projection=observer.project, check=observer.reference,
            variant='real-emitted-pull-warning-filter-and-capability-redaction')


GROUPS['config-pull-warning-and-real-tail'] = warning_and_tail
