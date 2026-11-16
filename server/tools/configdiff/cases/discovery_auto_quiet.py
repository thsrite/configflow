"""T21 quiet rounds: ignored/covered/fresh data and not-yet-due rechecks."""
from datetime import datetime, timedelta, timezone
import json

from agent_fake import Agent
from compare import differences, database
from harness import Pair, DesignError, json_body
from task_lifecycle import Completion
from cases.discovery_auto import task_capture
from cases.discovery_reads import heartbeat
from cases.discovery_website import traffic
from cases.domain_discovery import DIRECT, PROXY, options
from cases.helpers import profile_policy
from cases.rule_io import write_cache
from cases.subscriptions import put_file, require

KEY, HOST = 'domain_discovery:default', 'api.example.com'


def probe_record(stamp):
    return {'host': HOST, 'url': 'https://' + HOST + '/', 'checked_at': stamp, 'proxy_path': '代理🧪',
            'direct': {'ok': 0, 'total': 3, 'delays': [], 'errors': {'timeout': 3}},
            'proxy': {'ok': 3, 'total': 3, 'delays': [10, 20, 30], 'errors': {}},
            'verdict': 'needs_proxy', 'target': 'proxy', 'confidence': 99, 'reasons': ['合成历史探测']}


def history(stamp, recheck=None):
    return {'value': 'example.com', 'rule_type': 'DOMAIN-SUFFIX', 'target': 'proxy', 'ruleset_id': PROXY['id'],
            'source': 'auto', 'applied_at': stamp, 'confidence': 99, 'verdict': 'needs_proxy', 'recheck': recheck}


class Quiet(Completion):
    def __init__(self, pair, agents):
        super().__init__(pair, KEY)
        self.agents = agents

    def after(self, index, reply):
        super().after(index, reply)
        if index == 0 and self.agents[0].snapshot():
            raise DesignError('Python quiet automatic round unexpectedly made an Agent RPC')

    def compare(self, policy):
        return super().compare(policy) + differences(self.agents[0].snapshot(), self.agents[1].snapshot(), 'auto.quiet.rpc', policy)


def quiet(binary, recorder):
    for shape in ('ignored', 'already-covered', 'fresh-probe', 'probe-before-3-days', 'history-before-30-days', 'recent-recheck'):
        now = datetime.now(timezone.utc).replace(tzinfo=None)
        stamp = lambda days: (now - timedelta(days=days)).isoformat(timespec='seconds')
        with Agent() as left, Agent() as right:
            left.release.set()
            right.release.set()
            opts = options()
            opts['agent_updates'] = {'domain_discovery_enabled': True}
            opts['agent_port_pairs'] = {'agent-synthetic': [left.port, right.port]}
            opts['profile']['domain_discovery'].update(auto_probe=True, auto_apply=True, auto_apply_min_confidence=95)
            if shape == 'ignored':
                opts['profile']['domain_discovery']['ignored'] = ['example.com']
            if shape in ('already-covered', 'history-before-30-days', 'recent-recheck'):
                target = DIRECT['id'] if shape == 'already-covered' else PROXY['id']
                for library in opts['shared']['rule_library']:
                    if library['id'] == target:
                        library['content'] += ('DOMAIN-SUFFIX,example.com\n' if target == DIRECT['id'] else '+.example.com\n')
            if shape in ('history-before-30-days', 'recent-recheck'):
                before = (now - timedelta(hours=30 * 24 - 1)).isoformat(timespec='seconds')
                recheck = {'at': before, 'verdict': 'both_ok', 'suggest_remove': False, 'reason': '合成原备注'} if shape == 'recent-recheck' else None
                opts['profile']['domain_discovery']['history'] = [history(stamp(31) if recheck else before, recheck)]
            with Pair(binary, recorder, options=opts) as pair:
                pair.policy = profile_policy(pair)
                heartbeat(pair, variant='T21/quiet/' + shape)
                traffic(pair, ['example.com'], variant='T21/quiet/' + shape + '/traffic')
                for library in opts['shared']['rule_library']:
                    if library.get('source_type') == 'content':
                        write_cache(pair, library, library['content'].encode())
                if shape in ('fresh-probe', 'probe-before-3-days'):
                    checked = stamp(0) if shape == 'fresh-probe' else (now - timedelta(hours=3 * 24 - 1)).isoformat(timespec='seconds')
                    put_file(pair, 'probes/agent-synthetic.json', {'hosts': {HOST: probe_record(checked)}})
                before = database(pair.dirs[0])
                probe = pair.dirs[0] / 'probes/agent-synthetic.json'
                original_probe = probe.read_bytes() if probe.exists() else None
                def check(reply):
                    require(database(pair.dirs[0]) == before, 'quiet auto round leaves every DB row, revision and commit unchanged')
                    require((probe.read_bytes() if probe.exists() else None) == original_probe, 'fresh probe is not rerun/replaced and absent probe stays absent')
                    require(not left.snapshot(), 'ignored/covered/fresh/not-due data produces no Agent call')
                pair.request('T21', 'POST /api/tasks/run', body=json_body({'key': KEY}), expect=202, writes=True, runtime_writes=True,
                             interaction=Quiet(pair, [left, right]), check=check, variant='quiet/' + shape)
                pair.request('T21', 'GET /api/tasks', expect=200, writes=True, capture=task_capture(pair), variant='quiet/' + shape + '/task')


GROUPS = {'discovery-automatic-quiet': quiet}
