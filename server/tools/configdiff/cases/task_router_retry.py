"""Router task admission and durable pause after unpublished settings changed."""
import copy
from datetime import datetime, timezone
import hashlib
import json
import time

from compare import Policy, database, differences, timestamp
from harness import Pair, json_body
from sources import Source
from subscriptions import Exchange
from task_lifecycle import Completion
from cases.config_backup import message
from cases.pull_routes import SourceStable
from cases.subscriptions import require

KEY = 'router_rule_sync:default:router-task'
REASON = '来源、规则选择或网关设置尚未发布，请手动预览发布'
CONTENT = '192.0.2.0/24\n'


class RouterCompletion(Completion):
    def __init__(self, pair, fake, receipt, relative):
        super().__init__(pair, KEY, status='failed', error=REASON)
        self.wire = Exchange(pair, fake, expected=0)
        self.original, self.relative = copy.deepcopy(receipt), relative
        self.before_db, self.proofs = [None, None], [None, None]
        pair.policy.times.add('files.' + relative + '.content.checked_at')

    def before(self, index):
        super().before(index)
        self.wire.before(index)
        self.before_db[index] = database(self.pair.dirs[index])

    def after(self, index, reply):
        super().after(index, reply)
        self.wire.after(index, reply)
        try:
            value = json.loads((self.pair.dirs[index] / self.relative).read_text())
        except (OSError, ValueError):
            value = {}
        expected = {**self.original, 'status': 'paused', 'message': REASON, 'checked_at': value.get('checked_at')}
        self.proofs[index] = {'exact_paused_receipt': value == expected,
            'actual_checked_time': timestamp(value.get('checked_at'), Policy(start=self.pair.policy.start, end=time.time())),
            'configuration_unchanged': database(self.pair.dirs[index]) == self.before_db[index]}
        if index == 0:
            require(all(self.proofs[0].values()), 'router task finishes with exact paused receipt, intact historical baseline and no configuration edit')

    def compare(self, policy):
        return super().compare(policy) + self.wire.compare(policy) + differences(*self.proofs, 'task.router.paused-proof', policy)


def queued_then_paused(binary, recorder):
    with Source(b'unpublished local changes must pause before any device access') as fake:
        router = {'id': 'router-task', 'name': '合成同步路由器🧪', 'type': 'routeros', 'endpoint': fake.url,
                  'username': 'synthetic-user', 'password': 'synthetic-password',
                  'auto_rule_sync': True, 'rule_sync_interval': 60, 'ip_rule_sets': ['rs-sync']}
        options = {'shared': {'rule_library': [{'id': 'lib-sync', 'name': '合成同步源', 'source_type': 'content',
                    'content': CONTENT, 'behavior': 'ipcidr', 'format': 'text', 'enabled': True, 'update_interval': 0}]},
                   'profile': {'routers': {'items': [router]}, 'rule_configs': [{'id': 'rs-sync', 'itemType': 'ruleset',
                    'library_rule_id': 'lib-sync', 'policy': 'DIRECT', 'enabled': True}]}}
        with Pair(binary, recorder, options=options) as pair:
            relative = 'router-rule-sync/' + hashlib.sha256(b'["default","router-task"]').hexdigest() + '.json'
            # A historical successful receipt followed by unpublished local
            # edits is valid runtime input. It is not proof of live device
            # ownership; this case intentionally stops before device I/O.
            rules = {'rs-sync': {'name': '合成同步源', 'source_type': 'content', 'ip_count': 1,
                'domain_count': 0, 'ignored_count': 0, 'invalid_count': 0, 'version': hashlib.sha256(CONTENT.encode()).hexdigest()}}
            receipt = {'status': 'synced', 'message': '已建立成功发布的同步基准',
                'checked_at': datetime.fromtimestamp(pair.policy.start, timezone.utc).isoformat(),
                'synced_at': '2026-01-01T00:00:00+00:00', 'updated_at': '2026-01-01T00:00:00+00:00', 'rules': rules,
                'baseline': {'control': hashlib.sha256(b'synthetic-previous-published-controls').hexdigest(), 'rules': rules,
                    'desired': {'fake_ip_range': '198.18.0.0/16', 'proxy_gateway': '192.0.2.10', 'proxy_cidrs': [], 'warnings': []},
                    'static_resources': [], 'policy': None, 'nat_list': None}}
            for directory in pair.dirs:
                path = directory / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(json_body(receipt))
                path.chmod(0o600)
            observer = RouterCompletion(pair, fake, receipt, relative)
            pair.request('R227-S07', 'POST /api/tasks/run', body=json_body({'key': KEY}), expect=202,
                writes=True, runtime_writes=True, interaction=observer,
                variant='real-enqueue-then-durable-pause-for-unpublished-control-change')
            pair.request('R227-F14', 'POST /api/tasks/run', body=json_body({'key': KEY}), expect=409,
                writes=True, interaction=SourceStable(pair, fake), check=message('同步已暂停：' + REASON),
                variant='paused-retry-rejected-after-worker-lock-released')


GROUPS = {'tasks-router-sync-queued-pause-and-retry-guard': queued_then_paused}
