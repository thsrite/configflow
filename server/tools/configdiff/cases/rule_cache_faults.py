"""Actual cache permission failures, including Python's post-commit failures."""
import copy
import os
import re

from compare import database, differences, files
from harness import Pair, json_body
from cases.config_backup import message
from cases.helpers import deny_writes, exception_policy
from cases.rule_library import DOMAIN, content_cache
from cases.setup import Stable
from cases.subscriptions import require

UPDATED = '+.changed.example\n'


class CacheFault:
    def __init__(self, pair):
        self.pair = pair
        self.before_db, self.before_files, self.observed = [None, None], [None, None], [None, None]

    def before(self, index):
        self.before_db[index], self.before_files[index] = database(self.pair.dirs[index]), files(self.pair.dirs[index])
        self.path = content_cache(self.pair.dirs[index], DOMAIN['id'])
        self.old_bytes = self.path.read_bytes()
        self.old_modes = self.path.parent.stat().st_mode & 0o777, self.path.stat().st_mode & 0o777
        self.path.chmod(0o444)
        self.path.parent.chmod(0o500)
        self.active = not os.access(self.path, os.W_OK) and not os.access(self.path.parent, os.W_OK)
        if index == 0:
            require(self.active, 'normal user really cannot write existing cache or create/replace files in its parent')

    def after(self, index, reply):
        try:
            try:
                retained = self.path.stat().st_mode & 0o777 == 0o444 and self.path.parent.stat().st_mode & 0o777 == 0o500
                same_bytes = self.path.read_bytes() == self.old_bytes
            except OSError:
                retained = same_bytes = False
            self.observed[index] = {'write_fault_active': self.active, 'fault_modes_retained': retained, 'old_cache_bytes_retained': same_bytes}
            if index == 0:
                require(all(self.observed[0].values()), 'failed cache write/remove retains old bytes and imposed permission fault')
        finally:
            if self.path.parent.exists():
                self.path.parent.chmod(self.old_modes[0])
            if self.path.exists():
                self.path.chmod(self.old_modes[1])

    def compare(self, policy):
        return differences(*self.observed, 'rule.cache-permission', policy)

    def reference(self, operation):
        before, after = self.before_db[0], database(self.pair.dirs[0])
        require(files(self.pair.dirs[0]) == self.before_files[0], 'failure retains all original cache/file bytes, with no temp or backup residue')
        if operation == 'create':
            require(before == after, 'failed atomic cache creation never commits the new shared resource')
            return
        expected = [] if operation == 'delete' else [{**DOMAIN, 'content': UPDATED}]
        require(after['fields']['shared//rule_library']['value'] == expected,
                'Python already committed library deletion/content before subsequent cache failure')
        require(after['meta']['generation'] == before['meta']['generation'] + 1
                and after['fields']['shared//_revision']['value'] == before['fields']['shared//_revision']['value'] + 1
                and after['meta']['commit_id'] != before['meta']['commit_id']
                and re.fullmatch('[0-9a-f]{32}', after['meta']['commit_id']), 'exactly one real shared transaction remains committed after error')
        foreign = lambda value: {key: row for key, row in value['fields'].items() if key not in ('shared//rule_library', 'shared//_revision')}
        require(foreign(before) == foreign(after) and before['sections'] == after['sections'],
                'post-commit failure does not change unrelated profiles/system/shared fields or their row revisions')
        require({key: value for key, value in before['meta'].items() if key not in ('generation', 'commit_id')}
                == {key: value for key, value in after['meta'].items() if key not in ('generation', 'commit_id')},
                'no unrelated repository metadata is changed by cache failure')


def cache_failures(binary, recorder):
    for operation in ('create', 'delete', 'update', 'draft'):
        with Pair(binary, recorder, options={'shared': {'rule_library': [copy.deepcopy(DOMAIN)]}}) as pair:
            for directory in pair.dirs:
                path = content_cache(directory, DOMAIN['id'])
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(DOMAIN['content'].encode())
            bodies = None
            if operation == 'draft':
                route = 'GET /api/rule-library/<rule_id>/draft-content'
                replies = pair.request('R168-S01', route, '/api/rule-library/lib-a/draft-content', expect=200,
                    writes=True, interaction=Stable(pair), variant='cache-write-fault-public-revision-precondition')
                bodies = [json_body({'revision': reply.json()['revision'] if reply.status else '', 'content': UPDATED}) for reply in replies]
                if not pair.route_available(route):
                    pair.unimplemented_mutations.add(route)
            observer = CacheFault(pair)
            if operation == 'create':
                case, route, path = 'R165-F16', 'POST /api/rule-library', '/api/rule-library'
                body = json_body({**DOMAIN, 'id': 'failed-cache-create', 'name': '创建失败🧪'})
            elif operation == 'draft':
                case, route, path = 'R169-F18', 'PUT /api/rule-library/<rule_id>/draft-content', '/api/rule-library/lib-a/draft-content'
                body = '{}'
            else:
                case = 'R166-F10' if operation == 'delete' else 'R167-F14'
                route = ('DELETE' if operation == 'delete' else 'PUT') + ' /api/rule-library/<rule_id>'
                path = '/api/rule-library/lib-a'
                body = None if operation == 'delete' else json_body({**DOMAIN, 'content': UPDATED})
            def check(reply):
                observer.reference(operation)
                if operation == 'create':
                    message('规则保存失败，未添加规则，请稍后重试')(reply)
            pair.request(case, route, path, body=body, bodies=bodies, expect=500, writes=True,
                interaction=observer, check=check, variant='real-readonly-cache-parent-and-file/' + operation)


def proxy_save_failure(binary, recorder):
    old = 'http://old-mirror.example/cache'
    with Pair(binary, recorder, options={'system_config': {'github_proxy_domain': old}}) as pair:
        with deny_writes(pair):
            pair.request('R174-F10', 'POST /api/rule-library/proxy-domains', body=json_body({'proxy_domains': 'http://mirror.example/cache'}),
                expect=500, writes=True, interaction=Stable(pair), policy=exception_policy(pair),
                check=message('exception_type=ConfigStorageError'), variant='SQLite-trigger-rejects-GitHub-prefix-save')
        pair.request('R173-S02', 'GET /api/rule-library/proxy-domains', expect=200, writes=True,
            interaction=Stable(pair), variant='public-read-after-failed-prefix-save',
            check=lambda reply: require(reply.json() == {'proxy_domains': old}, 'failed save never leaks an uncommitted prefix into subsequent public reads'))


GROUPS = {'rule-library-real-cache-permissions': cache_failures, 'rule-library-proxy-save-failure': proxy_save_failure}
