"""Task listing and interval/run validation through public HTTP and SQLite."""
import copy
import re
import threading
import time

from compare import database, timestamp
from harness import Pair, DesignError, json_body
from cases.config_backup import message
from cases.subscriptions import SUB, put_file, require
from task_lifecycle import Completion, row
from sources import Source

LIBRARY = [{'id': 'library-url', 'name': 'URL规则🧪', 'source_type': 'url', 'url': 'http://rules.example/feed',
            'behavior': 'domain', 'enabled': True, 'update_interval': 0},
           {'id': 'library-content', 'name': '内容规则', 'source_type': 'content', 'content': '+.example.com',
            'behavior': 'domain', 'enabled': True, 'update_interval': 0}]


def options():
    return {'shared': {'subscriptions': [SUB, {**SUB, 'id': 'no-url', 'name': '旧无地址订阅', 'url': ''}], 'rule_library': LIBRARY}}


def task_times(pair):
    def capture(replies, policy):
        body = replies[0].json()
        rows = body.get('tasks') if isinstance(body, dict) else None
        locations = [(f'body.tasks[{i}]', row) for i, row in enumerate(rows)] if isinstance(rows, list) else [('body.task', body.get('task'))]
        for path, row in locations:
            if not isinstance(row, dict) or row.get('next_run_at') is None:
                continue
            if row.get('key') == 'metrics_history_flush':
                policy.future_times[path + '.next_run_at'] = row['interval_seconds']
            elif not row.get('last_run_at') and row.get('key', '').startswith(('subscription:', 'rule_source:')):
                policy.times.add(path + '.next_run_at')
        return []
    return capture


def deadline_check(pair):
    def check(reply):
        body = reply.json()
        rows = body.get('tasks', [body.get('task')])
        if 'tasks' in body and not any(row.get('key') == 'metrics_history_flush' for row in rows):
            raise DesignError('Python task list omitted its required system task')
        for row in rows:
            if not isinstance(row, dict) or row.get('next_run_at') is None:
                continue
            value = row['next_run_at']
            if row.get('key') == 'metrics_history_flush':
                offset = row['interval_seconds']
            elif row.get('last_run_at'):
                continue  # Fixed seeded receipts/deadlines remain exact.
            else:
                offset = 0
            if not re.fullmatch(r'[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z', value) or not timestamp(value, pair.policy, offset):
                raise DesignError('Python task deadline is not the current baseline plus exact interval')
    return check


def interval(binary, recorder):
    with Pair(binary, recorder, options=options()) as pair:
        capture = task_times(pair)
        pair.request('R225-S02', 'GET /api/tasks', expect=200, writes=True, capture=capture, check=deadline_check(pair))
        for case, key, interval in [('S01', 'subscription:sub-a', 60), ('S02', 'rule_source:library-url', 0),
                                    ('B01', 'subscription:sub-a', 604800), ('B90', 'rule_source:library-content', 60)]:
            def check(reply):
                deadline_check(pair)(reply)
                field = 'subscriptions' if key.startswith('subscription:') else 'rule_library'
                interval_field = 'interval' if field == 'subscriptions' else 'update_interval'
                row = next(item for item in database(pair.dirs[0])['fields']['shared//' + field]['value'] if item['id'] == key.split(':')[1])
                require(row[interval_field] == interval, 'exact task interval stored on selected shared resource')
                if key.endswith('library-content'):
                    require(reply.json().get('task') is None, 'content rule interval is saved despite no runnable task row')
            pair.request('R226-' + case, 'PUT /api/tasks/interval', body=json_body({'key': key, 'interval_seconds': interval}),
                         expect=200, writes=True, capture=capture, check=check)
        for interval in (300, 600, 1800, 3600):
            def check(reply):
                deadline_check(pair)(reply)
                require(database(pair.dirs[0])['fields']['system//system_config']['value']['metrics_history_flush_interval'] == interval,
                        'exact monitoring interval committed in system settings')
            pair.request('R226-S03', 'PUT /api/tasks/interval', body=json_body({'key': 'metrics_history_flush', 'interval_seconds': interval}),
                         expect=200, writes=True, capture=capture, check=check, variant=str(interval))
        for case, data, status, expected, variant in [
            ('F08', {}, 400, '请指定任务和更新间隔', ''),
            ('F09', {'key': 'subscription:sub-a', 'interval_seconds': 60, 'extra': True}, 400, '请指定任务和更新间隔', ''),
            ('F10', {'key': 'subscription:sub-a'}, 400, '请指定任务和更新间隔', ''),
            ('F11', {'key': 'unknown:synthetic', 'interval_seconds': 60}, 400, '这个任务的间隔不能在这里修改', ''),
            ('F12', {'key': 'subscription:sub-a', 'interval_seconds': 59}, 400, '更新间隔必须为 0（关闭自动更新）或 60 ~ 604800 秒的整数', ''),
            ('F13', {'key': 'metrics_history_flush', 'interval_seconds': 60}, 400, '保存间隔只能是 5、10、30 或 60 分钟', ''),
            ('F14', {'key': 'subscription:missing', 'interval_seconds': 60}, 404, '任务对应的来源已不存在，请刷新后重试', 'subscription'),
            ('F14', {'key': 'rule_source:missing', 'interval_seconds': 60}, 404, '任务对应的来源已不存在，请刷新后重试', 'rule-source'),
            ('B01', {'key': 'subscription:sub-a', 'interval_seconds': 2592000}, 400, '更新间隔必须为 0（关闭自动更新）或 60 ~ 604800 秒的整数', 'original-invalid-upper-bound'),
            ('B02', {'key': 'rule_source:library-url', 'interval_seconds': 59}, 400, '更新间隔必须为 0（关闭自动更新）或 60 ~ 604800 秒的整数', '')]:
            pair.request('R226-' + case, 'PUT /api/tasks/interval', body=json_body(data),
                         expect=status, writes=True, check=message(expected), variant=variant)
    with Pair(binary, recorder) as pair:
        pair.request('R225-S01', 'GET /api/tasks', expect=200, writes=True, capture=task_times(pair), check=deadline_check(pair))
        pair.request('R225-B01', 'GET /api/tasks', expect=200, writes=True, capture=task_times(pair), check=deadline_check(pair))
        pair.request('R225-B03', 'GET /api/tasks', expect=200, writes=True, capture=task_times(pair), check=deadline_check(pair))


def run_validation(binary, recorder):
    with Pair(binary, recorder, options=options()) as pair:
        for case, data, status, expected, variant in [
            ('F08', {}, 400, '请指定任务', ''), ('F09', {'key': 1}, 400, '请指定任务', 'integer'),
            ('F09', {'key': None}, 400, '请指定任务', 'null'), ('F09', [], 400, '请指定任务', 'array'),
            ('F10', {'key': 'unknown:synthetic'}, 400, '这个任务不能在这里立即运行', ''),
            ('F11', {'key': 'subscription:missing'}, 404, '任务对应的来源已不存在，请刷新后重试', 'subscription'),
            ('F11', {'key': 'rule_source:missing'}, 404, '任务对应的来源已不存在，请刷新后重试', 'rule-source'),
            ('F13', {'key': 'domain_discovery:default'}, 409, '自动探测未开启', ''),
            ('F90', {'key': 'domain_discovery:missing'}, 404, 'missing', ''),
            ('F91', {'key': 'domain_discovery:bad!id'}, 409, 'Invalid profile id', 'bad-profile'),
            ('F91', {'key': 'subscription:no-url'}, 409, '这个来源没有订阅地址，不需要更新', 'no-url-subscription'),
            ('F91', {'key': 'rule_source:library-content'}, 409, '这个来源没有订阅地址，不需要更新', 'content-rule'),
            ('F91', {'key': 'subscription:'}, 404, '任务对应的来源已不存在，请刷新后重试', 'empty-resource'),
            ('B90', {'key': 'metrics_history_flush:x'}, 400, '这个任务不能在这里立即运行', '')]:
            pair.request('R227-' + case, 'POST /api/tasks/run', body=json_body(data),
                         expect=status, writes=True, check=message(expected), variant=variant)


def backoff_rows(binary, recorder):
    with Pair(binary, recorder, options=options()) as pair:
        pair.request('R226-S01', 'PUT /api/tasks/interval', body=json_body({'key': 'subscription:sub-a', 'interval_seconds': 3600}),
                     expect=200, writes=True, capture=task_times(pair), variant='backoff-enable')
        put_file(pair, 'task-status.json', {'subscription:sub-a': {'last_run_at': '2026-01-01T00:00:00Z',
            'last_status': 'failed', 'run_count': 7, 'failure_count': 3, 'consecutive_failures': 3}})
        # Fixed fixture timestamps and their derived deadlines stay exact.
        def check(reply):
            row = next(row for row in reply.json()['tasks'] if row['key'] == 'subscription:sub-a')
            require(row['next_run_at'] == '2026-01-01T00:20:00Z', 'third failure retries after 1200 seconds, before 3600-second interval')
        pair.request('R225-B02', 'GET /api/tasks', expect=200, writes=True, capture=task_times(pair), check=check)


GROUPS = {'tasks-intervals': interval, 'tasks-run-validation': run_validation, 'tasks-backoff-rows': backoff_rows}


def asynchronous(binary, recorder):
    from cases.webdav import receipt_policy
    for key, case, status, error in [('webdav_backup', 'S90', 'failed', '请填写完整的 WebDAV 配置'),
                                      ('metrics_history_flush', 'S05', 'success', '')]:
        with Pair(binary, recorder, options={'agents': []}) as pair:
            original = database(pair.dirs[0])
            for count in (1, 2):
                def capture(replies, policy):
                    receipt_policy(pair, policy)
                    return []
                def check(reply):
                    require(database(pair.dirs[0]) == original, 'manual background task does not rewrite configuration rows')
                pair.request('R227-' + case, 'POST /api/tasks/run', body=json_body({'key': key}),
                             expect=202, writes=True, interaction=Completion(pair, key, count=count, status=status, error=error),
                             capture=capture, check=check, variant='final-receipt-run-' + str(count))


def source_busy(binary, recorder):
    from subscriptions import configure, capture_ids, capture_notice, cached, validate_ids
    for kind, resource, case, content in [('subscription', 'sub-a', 'S01', None),
                                           ('rule_source', 'library-url', 'S02', b'+.example.com\n')]:
        with Source() as source:
            if content is not None:
                source.body = content
            gate = threading.Event()
            source.before_reply = lambda _path: gate.wait(20)
            opts = options()
            opts['shared']['subscriptions'][0]['url'] = source.url + '/feed'
            opts['shared']['rule_library'][0]['url'] = source.url + '/feed'
            try:
                with Pair(binary, recorder, options=opts) as pair:
                    key = kind + ':' + resource
                    if kind == 'subscription':
                        configure(pair, source.url + '/feed')
                        for name in ('shared/subscription_health.json', 'shared/subscription_health.json.bak'):
                            pair.policy.times.add('files.' + name + '.content.sub-a.history[1].at')
                    before = database(pair.dirs[0])
                    class Started:
                        observations = [None, None]

                        def before(self, index):
                            self.previous = len(source.requests)

                        def after(self, index, reply):
                            deadline = time.monotonic() + 2
                            while len(source.requests) == self.previous and time.monotonic() < deadline:
                                time.sleep(.01)
                            self.observations[index] = {'requests': copy.deepcopy(source.requests[self.previous:]),
                                                        'row': row(pair, index, key) if index == 0 or pair.route_available('GET /api/tasks') else None}
                            if not pair.route_available('GET /api/tasks'):
                                self.observations[index].pop('row')
                            if index == 0:
                                require(len(source.requests) == self.previous + 1, 'accepted task reaches exactly one blocked source request')
                                require(row(pair, index, key).get('running') is True, 'public task row shows blocked worker running')

                        def compare(self, policy):
                            from compare import differences
                            return differences(*self.observations, 'task.start', policy)
                    pair.request('R227-' + case, 'POST /api/tasks/run', body=json_body({'key': key}),
                                 expect=202, interaction=Started(), variant='blocked-source-accept')
                    def check(reply):
                        require(database(pair.dirs[0]) == before, 'scheduled refresh keeps shared configuration unchanged')
                        first = source.requests[0]
                        require(first['method'] == 'GET' and first['path'] == '/feed' and not first['authorization'],
                                'Python source protocol is unauthenticated GET of exact path')
                        if kind == 'subscription':
                            validate_ids(pair.dirs[0])
                            require(cached(pair.dirs[0])['count'] == 1, 'async subscription final raw cache contains downloaded node')
                    def capture(replies, policy):
                        return capture_ids(pair)(replies, policy) + capture_notice(pair)(replies, policy) if kind == 'subscription' else []
                    pair.request('R227-F12', 'POST /api/tasks/run', body=json_body({'key': key}), expect=409,
                                 writes=True, interaction=Completion(pair, key, gate=gate, source=source), capture=capture,
                                 check=check, variant=kind + '-busy-final')
                    pair.request('R227-' + case, 'POST /api/tasks/run', body=json_body({'key': key}), expect=202,
                                 writes=True, interaction=Completion(pair, key, count=2), capture=capture,
                                 variant=kind + '-repeat-after-release')
            finally:
                gate.set()


GROUPS.update({'tasks-asynchronous': asynchronous, 'tasks-source-busy': source_busy})
