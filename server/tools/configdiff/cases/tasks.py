"""Task listing and interval/run validation through public HTTP and SQLite."""
import copy
import re

from compare import database, timestamp
from harness import Pair, DesignError, json_body
from cases.config_backup import message
from cases.subscriptions import SUB, put_file, require

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
