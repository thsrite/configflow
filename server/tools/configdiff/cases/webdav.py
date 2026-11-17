"""R014–R017/T15: real clients against a stateful loopback WebDAV server."""
import copy
from datetime import datetime
import json
import re
import time
from urllib.parse import unquote
from zoneinfo import ZoneInfo

from compare import Difference, differences, timestamp, database_diffs, file_diffs
from fakes import WebDAV
from harness import Pair, DesignError, Reply, json_body
from protocol_compare import dav_differences


def diagnostic_policy(pair, kind, *, stored=False):
    """O6 excludes English implementation exception names, never Chinese or codes."""
    prefix, variants = {
        'space': ('备份失败：', ('NotEnoughSpace', 'HTTP 507')),
        'mkdir': ('', ('无法创建目录 /one/：ResponseErrorCode HTTP 403', '无法创建目录 /one/：HTTP 403')),
        'connection': ('连接测试失败：', ('NoConnection', 'network request failed')),
    }[kind]
    pair.policy.internal_error_values['body.message'] = tuple(prefix + value for value in variants)
    if stored:
        for path in ('files.task-status.json.content.webdav_backup.last_error',
                     'files.notifications.json.content.items.task:webdav_backup.message'):
            pair.policy.internal_error_values[path] = variants


def receipt_policy(pair, policy):
    for field in ('last_run_at', 'last_finished_at', 'last_success_at'):
        policy.times.add('files.task-status.json.content.webdav_backup.' + field)
    policy.durations.add('files.task-status.json.content.webdav_backup.last_duration_ms')
    prefix = 'files.notifications.json.content.items.task:webdav_backup.'
    for field in ('checked_at', 'version', 'first_seen_at', 'last_seen_at'):
        policy.times.add(prefix + field)
    values = []
    for directory in pair.dirs:
        path = directory / 'notifications.json'
        item = json.loads(path.read_text()).get('items', {}).get('task:webdav_backup', {}) if path.exists() else {}
        values.append(item.get('evidence'))
    if all(isinstance(v, str) for v in values):
        parts = [v.split(':', 1) for v in values]
        suffix = [p[1].rsplit(':', 1) for p in parts]
        if parts[0][0] == parts[1][0] and suffix[0][1] == suffix[1][1] and all(timestamp(s[0], policy) for s in suffix):
            policy.bind(*values, r'[0-9]+:.+:(success|failed|skipped)', 'task-evidence')


class Exchange:
    def __init__(self, fake, pair, *, directories=('/', '/backup/'), objects=None,
                 failures=None, backup=False, expected_status=200, put_count=0):
        self.fake, self.pair = fake, pair
        self.options = dict(directories=directories, objects=objects, failures=failures)
        self.backup, self.expected_status, self.put_count = backup, expected_status, put_count
        self.observations = []
        self.replies = []

    def before(self, index):
        self.fake.reset(**self.options)

    def after(self, index, reply):
        snapshot = self.fake.snapshot()
        self.observations.append(snapshot)
        self.replies.append(reply)
        if index == 0:
            if any(r['authorization'] != self.fake.basic for r in snapshot['requests']):
                raise DesignError('Python DAV fixture did not use expected Basic authentication')
            puts = [r for r in snapshot['requests'] if r['method'] == 'PUT']
            if len(puts) != self.put_count:
                raise DesignError('Python DAV PUT count: ' + str(len(puts)))
            for put in puts:
                expected = json.loads((self.pair.dirs[0] / 'configdiff-seed.json').read_text())
                if differences(expected, put['body']):
                    raise DesignError('Python DAV did not upload its complete current export')
            for request in snapshot['requests']:
                if request['method'] == 'DELETE' and not re.fullmatch(r'.*/config_backup_[0-9]{8}_[0-9]{6}(?:_[0-9]{6})?\.json', request['path']):
                    raise DesignError('Python DAV tried to delete an unrelated object')
            if self.backup:
                receipt = json.loads((self.pair.dirs[0] / 'task-status.json').read_text())['webdav_backup']
                expected = 'success' if self.expected_status == 200 else 'failed'
                if receipt['last_status'] != expected or receipt['last_trigger'] != 'manual':
                    raise DesignError('Python manual backup receipt does not match outcome')

    def compare(self, policy):
        a, b = self.observations
        diffs = []
        puts = [[r for r in observation['requests'] if r['method'] == 'PUT'] for observation in (a, b)]
        if all(len(p) == 1 for p in puts):
            names = [unquote(p[0]['path']).rsplit('/', 1)[-1] for p in puts]
            for index, name in enumerate(names):
                try:
                    stamp = datetime.strptime(name, 'config_backup_%Y%m%d_%H%M%S_%f.json').replace(tzinfo=ZoneInfo(self.pair.tz)).timestamp()
                    assert policy.start - 2 <= stamp <= policy.end + 2
                except (ValueError, AssertionError):
                    if index == 0:
                        raise DesignError('Python backup filename time contract invalid')
                    diffs.append(Difference('dav.filename', 'invalid-time-or-format', '<dated-backup>', name))
            if not diffs:
                paths = [unquote(p[0]['path']) for p in puts]
                policy.bind(*paths, r'.*/config_backup_[0-9]{8}_[0-9]{6}_[0-9]{6}\.json', 'dav-upload-path')
                policy.bind(*[p[0]['path'] for p in puts], r'.*/config_backup_[0-9]{8}_[0-9]{6}_[0-9]{6}\.json', 'dav-encoded-path')
                messages = [r.json().get('message', '') for r in self.replies]
                if all(r.status == 200 for r in self.replies):
                    tails = [m.removeprefix('备份成功：' + n) for m, n in zip(messages, names)]
                    if tails[0] == tails[1] and all(m.startswith('备份成功：' + n) for m, n in zip(messages, names)):
                        policy.bind(*messages, r'备份成功：config_backup_[0-9]{8}_[0-9]{6}_[0-9]{6}\.json(?:，已清理 [0-9]+ 个旧备份)?', 'dav-success-message')
        receipt_policy(self.pair, policy)
        return diffs + dav_differences(a, b, policy)


def dav_requests(binary, recorder):
    with WebDAV() as fake:
        settings = {'webdav_url': fake.url, 'webdav_username': fake.username, 'webdav_password': fake.password,
                    'webdav_path': '/backup/', 'keep_latest': 1, 'keep_daily': 0, 'auto_backup': False}
        # Every protocol scenario gets a fresh DB/process pair. The same fake URL
        # and initial remote objects are replayed independently for both clients.
        tests = [
            ('R017-S01', {}, {}),
            ('R017-S02', {'directories': ('/',)}, {'webdav_path': '/one/two/'}),
            ('R017-S03', {}, {'webdav_password': '******'}),
            ('R017-B01', {'directories': ('/',)}, {'webdav_path': '/中文🧪/空 格%/'}),
            ('R017-B02', {'directories': ('/',)}, {'webdav_path': 'one/two'}),
            ('R017-B90', {'directories': ('/',)}, {'webdav_path': '/中文🧪/空 格%/'}),
            ('R017-F17', {'failures': {('HEAD', '/'): 404}}, {}),
            ('R017-F18', {'directories': ('/',), 'failures': {'MKCOL': 403}}, {'webdav_path': '/one/two/'}),
            ('R017-F19', {'failures': {'HEAD': 'disconnect'}}, {}),
            ('R016-S01', {}, {}),
            ('R016-S02', {}, {'webdav_password': '******'}),
            ('R016-S03', {'objects': {'/backup/config_backup_20240101_000001.json': b'{"old":1}',
                                      '/backup/unrelated.json': b'{"keep":true}'}}, {}),
            ('R016-F15', {'failures': {'PUT': 507}}, {}),
            ('R016-F16', {'failures': {'PROPFIND': 500}}, {}),
            ('R016-B01', {'objects': {'/backup/config_backup_20240101_000001.json': b'{"old":1}'}}, {'keep_latest': 0}),
            ('R016-B02', {'objects': {'/backup/config_backup_20240101_000001.json': b'{"old":1}',
                                      '/backup/config_backup_20240101_000002.json': b'{"old":2}',
                                      '/backup/config_backup_20240102_000001.json': b'{"old":3}'}}, {'keep_daily': 3650}),
            ('R016-B03', {'directories': ('/',)}, {'webdav_path': '/中文🧪/空 格%/'}),
            ('R016-B90', {'directories': ('/',)}, {'webdav_path': '/中文🧪/空 格%/'})]
        for case_id, options, changed in tests:
            backup = case_id.startswith('R016')
            status = 400 if case_id in ('R017-F17', 'R017-F18') else 500 if case_id in ('R017-F19', 'R016-F15') else 200
            with Pair(binary, recorder, options={'backup': settings}) as pair:
                if case_id in ('R016-F15', 'R017-F18', 'R017-F19'):
                    diagnostic_policy(pair, {'R016-F15': 'space', 'R017-F18': 'mkdir', 'R017-F19': 'connection'}[case_id],
                                      stored=case_id == 'R016-F15')
                exchange = Exchange(fake, pair, **options, backup=backup, expected_status=status, put_count=int(backup))
                pair.request(case_id, 'POST /api/backup/' + ('now' if backup else 'test'),
                             body=json_body({**settings, **changed}), expect=status, writes=True,
                             interaction=exchange)
                observed = exchange.observations[0]
                if case_id == 'R016-S03' and ('/backup/config_backup_20240101_000001.json' in observed['objects']
                                             or '/backup/unrelated.json' not in observed['objects']):
                    raise DesignError('Python DAV retention fixture did not delete only the old backup')
                if case_id == 'R016-B01' and '/backup/config_backup_20240101_000001.json' not in observed['objects']:
                    raise DesignError('Python keep_latest=0 unexpectedly pruned')
                # Pair compares interactions before HTTP so dynamic filename/message
                # correspondence is shared with both HTTP and storage comparators.


def automatic_backup(binary, recorder):
    # Independent loopback endpoints make attribution unambiguous while both
    # production workers are running; their URLs are validated fixture identities.
    with WebDAV() as left, WebDAV() as right:
        with Pair(binary, recorder) as pair:
            policy = pair.policy
            policy.bind(left.url, right.url, r'http://127\.0\.0\.1:[0-9]+', 'dav-endpoint')
            base = {'webdav_username': left.username, 'webdav_password': left.password,
                    'webdav_path': '/backup/', 'auto_backup': True, 'keep_latest': 0, 'keep_daily': 0}
            pair.request('R015-S90', 'POST /api/backup/config', body='{}',
                         bodies=[json_body({**base, 'webdav_url': f.url}) for f in (left, right)],
                         expect=200, writes=True, variant='enable-immediate')
            deadline = time.monotonic() + 6
            while time.monotonic() < deadline:
                if all(any(r['method'] == 'PUT' for r in f.snapshot()['requests']) for f in (left, right)):
                    break
                time.sleep(.05)
            # Wait for final receipt; upload and receipt are different boundaries.
            time.sleep(.2)
            policy.end = time.time()
            observations = [f.snapshot() for f in (left, right)]
            python_puts = [r for r in observations[0]['requests'] if r['method'] == 'PUT']
            if len(python_puts) != 1:
                raise DesignError('auto_backup enable did not debounce to exactly one Python upload')
            receipt = json.loads((pair.dirs[0] / 'task-status.json').read_text())['webdav_backup']
            if receipt['last_trigger'] != 'event' or receipt['last_status'] != 'success':
                raise DesignError('Python automatic backup receipt is not event/success')
            exchange = Exchange(left, pair)
            exchange.observations = observations
            exchange.replies = [Reply(200, {}, b'{}'), Reply(200, {}, b'{}')]
            diffs = exchange.compare(policy) + database_diffs(*pair.dirs, policy) + file_diffs(*pair.dirs, policy)
            recorder.record('T16', 'enable-immediate-final', 'POST /api/backup/config', 'T16',
                            'different' if diffs else 'compared', diffs)


GROUPS = {'settings-webdav': dav_requests, 'settings-auto-backup': automatic_backup}
