"""Two public creators pass the initial ID read, but only one may commit."""
import copy
import re
import threading
from concurrent.futures import ThreadPoolExecutor

from compare import database, files, differences, response_diffs
from composition import cache_name
from harness import Pair, json_body, send
from sources import Source
from cases.config_backup import message
from cases.rule_library import DOMAIN
from cases.subscriptions import require


class CreationRace:
    def __init__(self, pair, source):
        self.pair, self.source = pair, source
        self.winner = {**DOMAIN, 'id': 'created-once', 'name': '先提交的创建'}
        self.loser = {**self.winner, 'name': '慢下载创建', 'source_type': 'url', 'url': source.url + '/held-create'}
        self.loser.pop('content')
        self.observed, self.secondary = [None, None], [None, None]

    def before(self, index):
        self.baseline = database(self.pair.dirs[index]), files(self.pair.dirs[index])
        self.source.requests.clear()
        self.entered, self.release = threading.Event(), threading.Event()
        self.timeout = False
        def gate(path):
            self.entered.set()
            if not self.release.wait(15):
                self.timeout = True
        self.source.before_reply = gate
        def create_winner():
            try:
                require(self.entered.wait(10), 'slow creator entered real HTTP after passing the initial ID check')
                return send(self.pair.bases[index], 'POST', '/api/rule-library', json_body(self.winner),
                    {'Authorization': 'Bearer ' + self.pair.tokens[index], 'Content-Type': 'application/json'}, timeout=10)
            finally:
                self.release.set()
        self.pool = ThreadPoolExecutor(max_workers=1)
        self.future = self.pool.submit(create_winner)

    def after(self, index, reply):
        self.release.set()
        try:
            self.secondary[index] = self.future.result(timeout=15)
        finally:
            self.pool.shutdown(wait=True)
            self.source.before_reply = lambda path: None
        after = database(self.pair.dirs[index])
        expected = copy.deepcopy(self.baseline[0])
        expected['fields']['shared//rule_library']['value'].append(self.winner)
        expected['fields']['shared//_revision']['value'] += 1
        expected['meta']['generation'] += 1
        for key in ('shared//rule_library', 'shared//_revision'):
            expected['fields'][key]['revision'] = expected['meta']['generation']
        commit = after['meta']['commit_id']
        fresh_commit = bool(re.fullmatch('[0-9a-f]{32}', commit)) and commit != expected['meta']['commit_id']
        expected['meta']['commit_id'] = commit
        stored = copy.deepcopy(self.baseline[1])
        stored[cache_name(self.winner)] = {'mode': 0o600, 'content': self.winner['content'].encode()}
        self.observed[index] = {'gate_reached': self.entered.is_set(), 'gate_timeout': self.timeout,
            'winner_status': self.secondary[index].status, 'winner_body': self.secondary[index].json(),
            'exact_one_commit': not differences(expected, after), 'fresh_commit': fresh_commit,
            'exact_only_winner_cache': not differences(stored, files(self.pair.dirs[index])),
            'source_requests': copy.deepcopy(self.source.requests)}
        if index == 0:
            proof = self.observed[0]
            require(proof['gate_reached'] and not proof['gate_timeout'] and proof['winner_status'] == 200
                and proof['winner_body'] == {'success': True, 'data': self.winner}
                and proof['exact_one_commit'] and proof['fresh_commit'] and proof['exact_only_winner_cache'],
                'only the real second creator commits SQL and its own cache: ' + repr(proof))
            require(len(proof['source_requests']) == 1 and proof['source_requests'][0]['method'] == 'GET'
                and proof['source_requests'][0]['path'] == '/held-create', 'one held source GET, no extra creator worker')

    def compare(self, policy):
        return differences(*self.observed, 'rule.create-race', policy) + response_diffs(*self.secondary, policy, method='POST')


def conflict(binary, recorder):
    with Source(b'+.remote.example\n') as source, Pair(binary, recorder) as pair:
        observer = CreationRace(pair, source)
        pair.request('H793', 'POST /api/rule-library', body=json_body(observer.loser), expect=409, writes=True,
            interaction=observer, check=message('规则库 ID 已存在，请刷新后重试'),
            variant='ID-created-by-second-public-writer-during-first-download')


GROUPS = {'rule-library-real-concurrent-create-ID-conflict': conflict}
