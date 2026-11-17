"""DAV preflight, actual directory failure and local-day retention boundaries."""
from datetime import datetime, timedelta
from zoneinfo import ZoneInfo
import json

from compare import database, differences, files
from fakes import WebDAV
from harness import Pair, json_body
from cases.config_backup import message
from cases.setup import Stable
from cases.subscriptions import require
from cases.webdav import Exchange, diagnostic_policy
from task_lifecycle import receipt_errors


class ConfigurationFailure(Exchange):
    def __init__(self, fake, pair):
        super().__init__(fake, pair, backup=True, expected_status=400)
        self.observed = [None, None]

    def before(self, index):
        super().before(index)
        self.before_db, self.before_files = database(self.pair.dirs[index]), files(self.pair.dirs[index])

    def after(self, index, reply):
        super().after(index, reply)
        directory = self.pair.dirs[index]
        current = files(directory)
        try:
            receipt = json.loads((directory / 'task-status.json').read_text())['webdav_backup']
        except (OSError, ValueError, KeyError, TypeError):
            receipt = {}
        self.observed[index] = {'database_unchanged': database(directory) == self.before_db,
            'only_failed_receipt_file_added': set(current) == set(self.before_files) | {'task-status.json'}
                and {key: value for key, value in current.items() if key != 'task-status.json'} == self.before_files,
            'precise_failed_manual_receipt': not receipt_errors(receipt, count=1, status='failed', error='请填写完整的 WebDAV 配置'),
            'no_network_request': not self.observations[-1]['requests']}
        if index == 0:
            require(all(self.observed[0].values()), 'incomplete DAV configuration records exactly its failed task receipt without changing configuration or sending HTTP: ' + repr(self.observed[0]))

    def compare(self, policy):
        return super().compare(policy) + differences(*self.observed, 'dav.configuration-failure', policy)


def configuration(binary, recorder):
    with WebDAV() as fake:
        settings = {'webdav_url': fake.url, 'webdav_username': fake.username, 'webdav_password': fake.password,
                    'webdav_path': '/backup/', 'keep_latest': 1, 'keep_daily': 0, 'auto_backup': False}
        with Pair(binary, recorder, options={'backup': settings}) as pair:
            class Read(Exchange):
                def __init__(self):
                    super().__init__(fake, pair)
                    self.stable = Stable(pair)
                def before(self, index):
                    super().before(index)
                    self.stable.before(index)
                def after(self, index, reply):
                    super().after(index, reply)
                    self.stable.after(index, reply)
                def compare(self, policy):
                    return super().compare(policy) + self.stable.compare(policy)
            pair.request('R014-B90', 'GET /api/backup/config', expect=200, writes=True, interaction=Read(),
                         check=lambda reply: require(not fake.snapshot()['requests'], 'reading saved settings sends no DAV request at all'),
                         variant='source-correction-settings-read-has-no-WebDAV-interaction')
        for field in ('webdav_url', 'webdav_username', 'webdav_password'):
            with Pair(binary, recorder) as pair:
                fake.reset()
                pair.request('R016-F14', 'POST /api/backup/now', body=json_body({**settings, field: ''}), expect=400,
                             writes=True, interaction=ConfigurationFailure(fake, pair), check=lambda reply: (
                                 message('请填写完整的 WebDAV 配置')(reply),
                                 require(not fake.snapshot()['requests'], 'incomplete credentials fail before network while recording their failed receipt')),
                             variant='missing-required-credential/' + field)
        with Pair(binary, recorder, options={'backup': settings}) as pair:
            diagnostic_policy(pair, 'mkdir', stored=True)
            exchange = Exchange(fake, pair, directories=('/',), failures={'MKCOL': 403}, backup=True, expected_status=400)
            def failed(reply):
                require(reply.json()['success'] is False and reply.json()['message'].startswith('无法创建目录 /one/'),
                        'real rejected MKCOL reaches the directory-creation diagnostic')
            pair.request('R016-F15', 'POST /api/backup/now', body=json_body({**settings, 'webdav_path': '/one/two/'}),
                         expect=400, writes=True, interaction=exchange, check=failed, variant='real-MKCOL-403-before-PUT')


def retention(binary, recorder):
    for tz in ('Asia/Shanghai', 'Etc/GMT-14', 'Etc/GMT+12'):
        with WebDAV() as fake:
            settings = {'webdav_url': fake.url, 'webdav_username': fake.username, 'webdav_password': fake.password,
                        'webdav_path': '/backup/', 'keep_latest': 1, 'keep_daily': 2, 'auto_backup': False}
            with Pair(binary, recorder, tz=tz, options={'backup': settings}) as pair:
                today = datetime.fromtimestamp(pair.policy.start, ZoneInfo(tz)).date()
                previous, expired = today - timedelta(days=1), today - timedelta(days=2)
                def key(day, clock):
                    return '/backup/config_backup_' + day.strftime('%Y%m%d') + '_' + clock + '.json'
                retained = key(previous, '083000')
                deleted = {key(previous, '003000'), key(previous, '073000'), key(expired, '234500')}
                objects = {name: b'{"synthetic-previous":true}' for name in deleted | {retained}}
                objects['/backup/unrelated.json'] = b'{"keep":true}'
                exchange = Exchange(fake, pair, objects=objects, backup=True, put_count=1)
                def check(reply):
                    snapshot = exchange.observations[0]
                    puts = [row for row in snapshot['requests'] if row['method'] == 'PUT']
                    removed = {row['path'] for row in snapshot['requests'] if row['method'] == 'DELETE'}
                    require(removed == deleted and set(snapshot['objects']) == {retained, '/backup/unrelated.json', puts[0]['path']},
                            'daily protection keeps only the newest previous-local-day object, with the new upload and unrelated object intact')
                pair.request('R016-B91', 'POST /api/backup/now', body=json_body(settings), expect=200, writes=True,
                             interaction=exchange, check=check, variant=tz + '/previous-local-day-crosses-UTC-midnight')


GROUPS = {'webdav-named-preflight-and-directory-failure': configuration, 'webdav-local-day-retention-boundary': retention}
