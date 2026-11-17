"""Hold real source HTTP while a second public writer changes its declaration."""
import copy
import hashlib
import json
import re
import threading
from concurrent.futures import ThreadPoolExecutor

from compare import database, files, differences, response_diffs
from harness import Pair, json_body, send
from sources import Source
from cases.rule_library import DOMAIN, content_cache
from cases.subscriptions import require

ROUTE = 'PUT /api/rule-library/<rule_id>'
PATH = '/api/rule-library/lib-a'
CONTENT = b'+.remote.example\n'
CONFLICT = {'success': False, 'message': '规则集已在其他页面更新，请重新读取后再编辑。'}


def payload(reply):
    if reply.headers.get('content-type', '').startswith('text/html'):
        return {'html_error_message_present': CONFLICT['message'].encode() in reply.body}
    try:
        return reply.json()
    except (ValueError, UnicodeError):
        return {'invalid_json_bytes': reply.body.hex()}


def remote_cache(rule):
    identity = json.dumps([rule['id'], rule['url'], rule['behavior'], rule['format']], ensure_ascii=False)
    return 'shared/rules/by-source/' + hashlib.sha256(identity.encode()).hexdigest() + '.list'


class Race:
    def __init__(self, pair, source, both_remote):
        self.pair, self.source, self.both_remote = pair, source, both_remote
        self.slow = {**DOMAIN, 'source_type': 'url', 'url': source.url + '/slow'}
        self.slow.pop('content')
        self.winner = {**DOMAIN, 'name': '先提交的编辑🧪'}
        if both_remote:
            self.winner = {**self.slow, 'url': source.url + '/fast', 'name': self.winner['name']}
        self.observed, self.secondary = [None, None], [None, None]

    def request(self, index, body):
        return send(self.pair.bases[index], 'PUT', PATH, json_body(body),
                    {'Content-Type': 'application/json', 'Authorization': 'Bearer ' + self.pair.tokens[index]}, timeout=15)

    def before(self, index):
        self.before_db, self.before_files = database(self.pair.dirs[index]), files(self.pair.dirs[index])
        self.source.requests.clear()
        self.entered, self.release = threading.Event(), threading.Event()
        self.timed_out = False

        def gate(path):
            if path == '/slow':
                self.entered.set()
                if not self.release.wait(10):
                    self.timed_out = True
        self.source.before_reply = gate
        self.pool = ThreadPoolExecutor(max_workers=1)
        if self.both_remote:
            self.future = self.pool.submit(self.request, index, self.slow)
            entered = self.entered.wait(5)
            if index == 0:
                require(entered, 'first real writer is waiting inside remote HTTP before second writer starts')
        else:
            def competing_edit():
                try:
                    if not self.entered.wait(5):
                        self.timed_out = True
                    return self.request(index, self.winner)
                finally:
                    self.release.set()
            self.future = self.pool.submit(competing_edit)

    def after(self, index, reply):
        self.release.set()
        try:
            other = self.future.result(timeout=15)
        finally:
            self.pool.shutdown(wait=True)
            self.source.before_reply = lambda _path: None
        self.secondary[index] = other
        success, rejected = (reply, other) if self.both_remote else (other, reply)
        after, stored = database(self.pair.dirs[index]), files(self.pair.dirs[index])
        expected_db = copy.deepcopy(self.before_db)
        expected_db['fields']['shared//rule_library']['value'] = [self.winner]
        expected_db['fields']['shared//_revision']['value'] += 1
        expected_db['meta']['generation'] += 1
        for key in ('shared//rule_library', 'shared//_revision'):
            expected_db['fields'][key]['revision'] = expected_db['meta']['generation']
        commit = after['meta']['commit_id']
        expected_db['meta']['commit_id'] = commit
        expected_files = copy.deepcopy(self.before_files)
        if not self.both_remote:
            original = content_cache(self.pair.dirs[index], DOMAIN['id']).relative_to(self.pair.dirs[index]).as_posix()
            expected_files[original] = {'mode': 0o600, 'content': DOMAIN['content'].encode()}
            expected_files[original + '.bak'] = copy.deepcopy(expected_files[original])
        for rule in ([self.slow, self.winner] if self.both_remote else [self.slow]):
            expected_files[remote_cache(rule)] = {'mode': 0o600, 'content': CONTENT}
        expected_requests = [('GET', path, '') for path in (('/slow', '/fast') if self.both_remote else ('/slow',))]
        # Preserve the actual prescribed request headers through the normal
        # cross-implementation transcript comparison; source oracle below also
        # proves paths/methods and that no third request escaped the gate.
        transcript = copy.deepcopy(self.source.requests)
        self.observed[index] = {
            'gate_reached': self.entered.is_set(), 'no_gate_timeout': not self.timed_out,
            'success_status': success.status, 'rejected_status': rejected.status,
            'success_body': payload(success), 'rejected_body': payload(rejected),
            'exact_single_commit': not differences(expected_db, after),
            'new_commit_identity': isinstance(commit, str) and bool(re.fullmatch('[0-9a-f]{32}', commit))
                and commit != self.before_db['meta']['commit_id'],
            'exact_files_including_rejected_remote_cache': not differences(expected_files, stored),
            'source_requests': transcript}
        if index == 0:
            expected = {**self.observed[0], 'gate_reached': True, 'no_gate_timeout': True,
                'success_status': 200, 'rejected_status': 409,
                'success_body': {'success': True, 'data': self.winner, 'synced_count': 0, 'warnings': []},
                'rejected_body': {'html_error_message_present': True}, 'exact_single_commit': True, 'new_commit_identity': True,
                'exact_files_including_rejected_remote_cache': True}
            require(not differences(expected, self.observed[0]), 'held remote edit loses to one real committed public writer: '
                + repr(self.observed[0]) + '; db=' + repr(differences(expected_db, after)) + '; files=' + repr(differences(expected_files, stored)))
            require([(r['method'], r['path'], r['authorization']) for r in transcript]
                    == expected_requests,
                    'exact one/two GETs, no credentials or unexpected source calls')

    def compare(self, policy):
        return differences(*self.observed, 'rule.concurrent', policy) + response_diffs(*self.secondary, policy, method='PUT')


def races(binary, recorder):
    for both_remote in (False, True):
        with Source(CONTENT) as source, Pair(binary, recorder,
                options={'shared': {'rule_library': [copy.deepcopy(DOMAIN)]}}) as pair:
            for directory in pair.dirs:
                path = content_cache(directory, DOMAIN['id'])
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(DOMAIN['content'].encode())
            observer = Race(pair, source, both_remote)
            pair.request('R167-B03' if both_remote else 'R167-F12', ROUTE, PATH,
                body=json_body(observer.winner if both_remote else observer.slow),
                expect=200 if both_remote else 409, writes=True, interaction=observer,
                variant='two-remote-writers-one-commit' if both_remote else 'remote-download-held-while-public-metadata-edit-commits')


GROUPS = {'rule-library-concurrent-source-edit': races}
