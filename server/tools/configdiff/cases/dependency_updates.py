"""Real async update HTTP/TLS/files with a loopback-only process controller."""
import copy
import hashlib
import json
import math
import threading
import time

from compare import Difference, database, differences, files, response_diffs
from dependency_control import managed, events
from dependency_tls import GitHub, release_request
from harness import Pair, Reply, send, json_body
from sources import Source
from cases.config_backup import message
from cases.dependency_modes import STATUS, HEALTH
from cases.dependency_status import DependencyRead
from cases.subscriptions import require

UPDATE = 'POST /api/settings/dependencies/sub-store/update'
RESTART = ['-c', '/etc/supervisor/conf.d/supervisord.conf', 'restart', 'sub-store']


def terminal_diffs(left, right, policy):
    """Compare every terminal status field; validate only the sampled finish time."""
    values = [copy.deepcopy(value) for value in (left, right)]
    stamps = [value['dependencies'][0]['update']['finished_at'] for value in values]
    result = []
    if any(type(value) not in (int, float) or not math.isfinite(value)
           or not policy.start <= value <= policy.end for value in stamps):
        result.append(Difference('dependency.terminal.update.finished_at', 'invalid-finish-time', *stamps))
    for value in values:
        value['dependencies'][0]['update']['finished_at'] = '<validated-finish-time>'
    return result + differences(*values, 'dependency.terminal', policy)


class Update:
    def __init__(self, pair, health, github, *, failure_message=''):
        self.pair, self.health, self.github = pair, health, github
        self.failure_message = failure_message
        self.observed, self.terminal = [None, None], [None, None]
        self.terminal_http = [None, None]

    def before(self, index):
        # Each backend runs its entire worker before the shared fake is reset.
        self.health.body = json.dumps({'data': {'version': '2.40.0'}}).encode()
        self.offsets = len(self.health.requests), len(self.github.requests), len(events(self.pair))
        self.before_db = database(self.pair.dirs[index])
        self.before_files = files(self.pair.dirs[index])

    def after(self, index, reply):
        polls, invalid_progress = 0, []
        terminal = None
        if reply.status == 202:
            deadline = time.monotonic() + 12
            while time.monotonic() < deadline:
                status = send(self.pair.bases[index], 'GET', '/api/settings/dependencies', headers={
                    'Authorization': 'Bearer ' + self.pair.tokens[index]})
                polls += 1
                if status.status != 200:
                    invalid_progress.append({'status': status.status, 'body': status.body.decode(errors='replace')})
                    break
                payload = status.json()
                update = payload['dependencies'][0]['update']
                if update['state'] not in ('running', 'success', 'failed') or update['target_version'] != '2.42.1':
                    invalid_progress.append(update)
                if update['state'] in ('success', 'failed') and payload['dependencies'][0]['lifecycle']['state'] != 'updating':
                    terminal = payload
                    self.terminal_http[index] = Reply(status.status, status.headers, b'{}')
                    break
                time.sleep(0.03)
        rows = self.health.requests[self.offsets[0]:]
        health = [{key: row[key] for key in HEALTH} for row in rows if row['path'] != '/_configdiff/version']
        controls = [{key: row[key] for key in HEALTH} for row in rows if row['path'] == '/_configdiff/version']
        commands = events(self.pair)[self.offsets[2]:]
        self.terminal[index] = terminal
        self.observed[index] = {'database_unchanged': not differences(self.before_db, database(self.pair.dirs[index])),
            'health_protocol_valid': health == [HEALTH] * len(health),
            'worker_health_count': len(health) - polls,
            'controller': controls,
            'commands': [row for row in commands if row != {'args': STATUS}],
            'status_count_valid': sum(row == {'args': STATUS} for row in commands) <= 1,
            'github': self.github.requests[self.offsets[1]:], 'invalid_progress': invalid_progress,
            'terminal_reached': terminal is not None}
        if index == 0:
            expected = {'database_unchanged': True, 'health_protocol_valid': True, 'worker_health_count': 2 if self.failure_message else 3,
                'controller': [] if self.failure_message else [{**HEALTH, 'method': 'POST', 'path': '/_configdiff/version',
                    'authorization': 'Bearer synthetic-controller-capability',
                    'body_hex': json.dumps({'version': '2.42.1'}).encode().hex()}],
                'commands': [] if self.failure_message else [{'args': RESTART}], 'status_count_valid': True,
                'github': release_request() + [{'method': 'CONNECT', 'target': 'downloads.example:443'},
                    {'method': 'GET', 'path': '/sub-store.bundle.js', 'authorization': '', 'body': ''}],
                'invalid_progress': [], 'terminal_reached': True}
            require(not differences(expected, self.observed[0]), 'update uses exactly release/download/restart/readiness protocol: '
                    + repr(self.observed[0]))
            value = terminal['dependencies'][0]
            failed = bool(self.failure_message)
            require(value['update']['state'] == ('failed' if failed else 'success')
                    and value['update']['message'] == (self.failure_message if failed else 'Sub-Store 已更新到 2.42.1')
                    and value['current_version'] == ('2.40.0' if failed else '2.42.1')
                    and value['running'] is True and value['online_updated'] is (not failed)
                    and value['has_update'] is failed and value['lifecycle']['state'] == 'running',
                    'public polling reaches exact terminal result and installed lifecycle')
            expected_files = self.before_files if failed else {**self.before_files,
                'sub-store/runtime/VERSION': {'mode': 0o644, 'content': ['2.42.1']},
                'sub-store/runtime/sub-store.bundle.js': {'mode': 0o644, 'content': self.github.bundle.decode().splitlines()}}
            require(not differences(expected_files, files(self.pair.dirs[0])),
                    'only final VERSION and exact downloaded JavaScript are retained; no temp/backup remains')
        directory = self.pair.dirs[index] / 'sub-store/runtime'
        self.observed[index]['installed_bytes'] = {name: hashlib.sha256((directory / name).read_bytes()).hexdigest()
            for name in ('VERSION', 'sub-store.bundle.js') if (directory / name).is_file()}
        if index == 0:
            require(self.observed[0]['installed_bytes'] == ({} if self.failure_message else {
                'VERSION': hashlib.sha256(b'2.42.1\n').hexdigest(),
                'sub-store.bundle.js': hashlib.sha256(self.github.bundle).hexdigest()}),
                'installed runtime bytes retain exact line endings and final newline')

    def compare(self, policy):
        result = differences(*self.observed, 'dependency.update-protocol', policy)
        if all(value is not None for value in self.terminal):
            result += terminal_diffs(*self.terminal, policy)
        if all(value is not None for value in self.terminal_http):
            result += response_diffs(*self.terminal_http, policy)
        return result


def success(binary, recorder):
    with GitHub() as github, managed(binary, recorder, github=github, allow_restart=True) as (pair, health):
        pair.request('R200-S01', UPDATE, body=json_body({}), expect=202, writes=True, runtime_writes=True,
            check=message('开始更新到 2.42.1'), interaction=Update(pair, health, github),
            variant='trusted-TLS-real-node-check-fake-restart-and-public-poll')


def rejected(binary, recorder):
    for shape, case in [('already-current', 'R200-F09'), ('release-503', 'R200-F10'),
                        ('missing-asset', 'R200-F10'), ('missing-asset-helper', 'H033')]:
        with GitHub() as github, managed(binary, recorder, github=github) as (pair, health):
            if shape == 'already-current':
                github.release['tag_name'] = 'v2.40.0'
            elif shape == 'release-503':
                github.status = 503
            else:
                github.release['assets'] = []
            def check(reply):
                value = reply.json()
                require(value['success'] is False and (value['message'] == '当前已是最新版本 2.40.0'
                    if shape == 'already-current' else value['message'].startswith('检查更新失败：')),
                    'failed preflight returns exact conflict category without starting a worker')
            pair.request(case, UPDATE, body=json_body({}), expect=409, writes=True, check=check,
                interaction=DependencyRead(pair, health, github, True, health_count=1 if shape == 'already-current' else 0),
                variant=shape)
    with GitHub() as github, Source(b'{"data":{"version":"2.40.0"}}') as health:
        health.record_body = True
        with Pair(binary, recorder, options={'environment': {'SUB_STORE_URL': health.url}}) as pair:
            pair.request('R200-F12', UPDATE, body=json_body({}), expect=409, writes=True,
                check=message('当前环境没有内置 Sub-Store，无法在线更新'),
                interaction=DependencyRead(pair, health, github, False, health_count=0),
                variant='external-configured-service-without-builtin-bundle')


GROUPS = {'dependency-update-preflight': rejected, 'dependency-update-success': success}


class Busy(Update):
    def __init__(self, pair, health, github):
        super().__init__(pair, health, github)
        self.entered, self.release = threading.Event(), threading.Event()
        github.before_reply = self.block_download

    def block_download(self, path):
        if path == '/sub-store.bundle.js':
            self.entered.set()
            self.release.wait(10)

    def before(self, index):
        super().before(index)
        self.entered.clear()
        self.release.clear()
        self.first = send(self.pair.bases[index], 'POST', UPDATE.split(' ', 1)[1], json_body({}), {
            'Authorization': 'Bearer ' + self.pair.tokens[index], 'Content-Type': 'application/json'})
        self.blocked = self.first.status == 202 and self.entered.wait(5)
        if index == 0:
            require(self.blocked, 'first update reaches the real TLS bundle gate before conflict request')
        self.held = database(self.pair.dirs[index]), files(self.pair.dirs[index])

    def after(self, index, reply):
        unchanged = not differences(self.held, (database(self.pair.dirs[index]), files(self.pair.dirs[index])))
        self.release.set()
        super().after(index, self.first)
        self.observed[index].update(first_update_blocked=self.blocked, rejected_request_readonly=unchanged)
        if index == 0:
            require(unchanged, 'second request during held download does not change SQL or files')


def conflicts(binary, recorder):
    for case in ('R200-F08', 'R200-B01', 'R199-F12'):
        with GitHub() as github, managed(binary, recorder, github=github, allow_restart=True) as (pair, health):
            observer = Busy(pair, health, github)
            try:
                mode = case == 'R199-F12'
                pair.request(case, 'POST /api/settings/dependencies/sub-store/mode' if mode else UPDATE,
                    body=json_body({'mode': 'on_demand'} if mode else {}), expect=409, writes=True, runtime_writes=True,
                    check=message('Sub-Store 正忙或正在更新，请稍后重试' if mode else '已有更新任务在进行中'),
                    interaction=observer, variant='real-worker-held-in-TLS-download')
            finally:
                observer.release.set()


GROUPS['dependency-update-conflicts'] = conflicts


def invalid_downloads(binary, recorder):
    for case in ('H035', 'H036'):
        with GitHub() as github, managed(binary, recorder, github=github, allow_restart=True) as (pair, health):
            if case == 'H035':
                github.bundle = b'synthetic too small'
                expected = '更新失败：下载的文件过小（19 字节），可能不是有效的 Sub-Store 后端'
            else:
                github.release['assets'][0]['digest'] = 'sha256:' + '0' * 64
                expected = '更新失败：文件校验失败（SHA256 不匹配），请检查 GitHub 代理设置'
            pair.request(case, UPDATE, body=json_body({}), expect=202, writes=True, runtime_writes=True,
                check=message('开始更新到 2.42.1'), interaction=Update(pair, health, github, failure_message=expected),
                variant='real-download-validation-cleans-temp-without-restart')


GROUPS['dependency-update-invalid-downloads'] = invalid_downloads
