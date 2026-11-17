"""Managed on-demand/always mode writes with a non-mutating controller fake."""
from compare import database, differences
from dependency_control import managed, events
from harness import json_body
from cases.config_backup import message
from cases.setup import Stable
from cases.subscriptions import require

STATUS = ['-c', '/etc/supervisor/conf.d/supervisord.conf', 'status', 'sub-store']
HEALTH = {'method': 'GET', 'path': '/api/utils/env', 'authorization': '', 'body_hex': '', 'transfer_encoding': ''}


class ModeProtocol:
    def __init__(self, pair, health, health_count, *, readonly=False, needs_status=False):
        self.pair, self.health, self.health_count = pair, health, health_count
        self.readonly = Stable(pair) if readonly else None
        self.needs_status = needs_status
        self.observed = [None, None]

    def before(self, index):
        if self.readonly:
            self.readonly.before(index)
        self.offsets = len(self.health.requests), len(events(self.pair))

    def after(self, index, reply):
        if self.readonly:
            self.readonly.after(index, reply)
        commands = events(self.pair)[self.offsets[1]:]
        observed_health = [{key: row[key] for key in HEALTH} for row in self.health.requests[self.offsets[0]:]]
        # X08's initial monitor may issue its one read-only status query. It
        # cannot start/stop/restart anything; the fake refuses those actions.
        status_reads = sum(row == {'args': STATUS} for row in commands)
        unexpected = [row for row in commands if row != {'args': STATUS}]
        self.observed[index] = {'health': observed_health, 'unexpected_commands': unexpected,
            'status_count_valid': (1 <= status_reads <= 2) if self.needs_status else status_reads <= 1}
        expected = {'health': [HEALTH] * self.health_count, 'unexpected_commands': [], 'status_count_valid': True}
        if index == 0:
            require(not differences(expected, self.observed[0]), 'mode control uses only declared health reads and read-only supervisor status: '
                    + repr(self.observed[0]))

    def compare(self, policy):
        return (self.readonly.compare(policy) if self.readonly else []) + differences(*self.observed, 'dependency.mode-protocol', policy)


def modes(binary, recorder):
    with managed(binary, recorder) as (pair, health):
        for case, mode in [('R199-S01', 'on_demand'), ('R199-S02', 'always'), ('R199-B01', 'always')]:
            before = database(pair.dirs[0])
            def check(reply):
                after = database(pair.dirs[0])
                require(reply.json() == {'success': True, 'mode': mode}
                        and after['fields']['system//system_config']['value']['sub_store_mode'] == mode,
                        'mode is returned and durably persisted exactly, including repeat selection')
                require(after['meta']['generation'] == before['meta']['generation'] + 1
                        and after['fields']['system//_revision']['value'] == before['fields']['system//_revision']['value'] + 1,
                        'Python records one system transaction even for repeated same mode')
                foreign = lambda db: {key: value for key, value in db['fields'].items()
                                      if key not in ('system//system_config', 'system//_revision')}
                require(foreign(before) == foreign(after), 'mode writes preserve all backup/Agent/shared/profile fields and row revisions')
            pair.request(case, 'POST /api/settings/dependencies/sub-store/mode', body=json_body({'mode': mode}),
                expect=200, writes=True, check=check, interaction=ModeProtocol(pair, health, 1 if mode == 'always' else 0),
                variant='managed-service-already-ready/' + mode)
    with managed(binary, recorder, fail=True) as (pair, health):
        pair.request('R199-F13', 'POST /api/settings/dependencies/sub-store/mode', body=json_body({'mode': 'always'}),
            expect=409, writes=True, check=message('Sub-Store 服务操作失败，请检查容器日志'),
            interaction=ModeProtocol(pair, health, 1, readonly=True, needs_status=True), variant='controller-status-failure')


GROUPS = {'dependency-managed-modes': modes}


def missing_controller(binary, recorder):
    from dependency_tls import GitHub
    with GitHub() as github, managed(binary, recorder, health_status=503, controller_missing=True, github=github) as (pair, health):
        pair.request('H1076', 'POST /api/settings/dependencies/sub-store/mode', body=json_body({'mode': 'always'}),
            expect=409, writes=True, check=message('无法管理内置 Sub-Store，请检查容器服务状态'),
            interaction=ModeProtocol(pair, health, 1, readonly=True), variant='real-exec-failure-with-only-owned-PATH')
        failed_status(pair, health, github, '无法管理内置 Sub-Store，请检查容器服务状态', 'controller-exec-unavailable')


GROUPS['dependency-real-controller-exec-unavailable'] = missing_controller


class NeverReady(ModeProtocol):
    """Observe the unchanged production readiness deadline and complete probes."""
    def __init__(self, pair, health):
        super().__init__(pair, health, 0, readonly=True, needs_status=True)
        self.waits = [None, None]

    def before(self, index):
        import time
        super().before(index)
        self.started = time.monotonic()

    def after(self, index, reply):
        import time
        elapsed = time.monotonic() - self.started
        self.waits[index] = {'production_timeout_observed': 19 <= elapsed <= 65}
        self.health_count = len(self.health.requests[self.offsets[0]:])
        if index == 0:
            require(self.waits[0]['production_timeout_observed'] and self.health_count >= 2,
                'actual readiness loop retries until its original 20-second deadline, without reducing constants')
        super().after(index, reply)

    def compare(self, policy):
        # Complete probe arrays (including count) stay strict. Only elapsed time
        # is tested as the actual production deadline window on each backend.
        return super().compare(policy) + differences(*self.waits, 'dependency.readiness-deadline', policy)


def never_ready(binary, recorder):
    from dependency_tls import GitHub
    with GitHub() as github, managed(binary, recorder, health_status=503, github=github) as (pair, health):
        pair.request('H1078', 'POST /api/settings/dependencies/sub-store/mode', body=json_body({'mode': 'always'}),
            expect=409, writes=True, check=message('Sub-Store 启动后未就绪，请稍后重试或检查容器日志'),
            interaction=NeverReady(pair, health), timeout=70, variant='actual-20-second-readiness-deadline')
        failed_status(pair, health, github, 'Sub-Store 启动后未就绪，请稍后重试或检查容器日志', 'running-never-ready')


GROUPS['dependency-real-running-process-never-ready'] = never_ready


def failed_status(pair, health, github, expected, variant):
    from cases.dependency_status import DependencyRead
    def check(reply):
        value = reply.json()['dependencies'][0]
        require(value['lifecycle'] == {'managed': True, 'mode': 'on_demand', 'state': 'failed',
            'idle_seconds': 300, 'error': expected} and value['running'] is False and value['updatable'] is True,
            'public dependency polling retains the failed lifecycle and exact error without changing saved mode')
    pair.request('R198-S01', 'GET /api/settings/dependencies', expect=200, writes=True, check=check,
        interaction=DependencyRead(pair, health, github, True), variant='after-real-runtime-failure/' + variant)
