"""T21 real manual task triggers, confidence thresholds and synchronous failure."""
from datetime import datetime, timedelta, timezone
from agent_fake import Agent, probe_calls, probe_result
from agent_jobs import Running
from compare import differences, database
from harness import Pair, DesignError, json_body
from seed import AGENT
from task_lifecycle import Completion, configure, row
from cases.discovery_reads import heartbeat, reply_clock
from cases.domain_discovery import DIRECT, PROXY, options, stored
from cases.discovery_writes import history_times
from cases.discovery_website import traffic
from cases.helpers import profile_policy
from cases.rule_io import write_cache
from cases.subscriptions import require, put_file
from cases.tasks import task_times

KEY, HOST = 'domain_discovery:default', 'api.example.com'
FAILURE = '执行失败，请查看服务端日志'


def task_capture(pair):
    def capture(replies, policy):
        task_times(pair)(replies, policy)
        for index, task in enumerate(replies[0].json()['tasks']):
            if task['key'] != KEY:
                continue
            for field in ('last_run_at', 'last_success_at'):
                if task.get(field) is not None:
                    policy.times.add(f'body.tasks[{index}].' + field)
            if task.get('last_duration_ms') is not None:
                policy.durations.add(f'body.tasks[{index}].last_duration_ms')
            if task.get('next_run_at') is not None:
                policy.future_times[f'body.tasks[{index}].next_run_at'] = 600
        return []
    return capture


class Started:
    def __init__(self, pair, agents):
        self.pair, self.agents, self.observations = pair, agents, [None, None]
        self.poll_both = pair.route_available('GET /api/tasks')

    def before(self, index):
        pass

    def after(self, index, reply):
        contacted = self.agents[index].seen.wait(4)
        current = row(self.pair, index, KEY) if index == 0 or self.poll_both else None
        self.observations[index] = {'agent_contacted': contacted}
        if self.poll_both:
            self.observations[index]['task'] = current
        if index == 0 and (reply.status != 202 or not contacted or current.get('running') is not True):
            raise DesignError('Python auto task did not enter real held RPC with public running lock: ' + repr(self.observations[index]))

    def compare(self, policy):
        return differences(*self.observations, 'auto.start', policy)


def automatic(binary, recorder, scenarios=None):
    scenarios = scenarios or [(80, False, False, ''), (90, False, False, ''), (95, True, False, ''), (95, False, True, '')]
    for threshold, passive_failure, malformed, shape in scenarios:
        variant = str(threshold) + ('/passive-failing' if passive_failure else '/normal') + ('/malformed' if malformed else '')
        if shape:
            variant += '/' + shape
        def responder(request, index):
            status, body = probe_result(request)
            if malformed:
                body['results'][0].pop('paths')
            elif shape != 'recheck-after-30-days':
                body['results'][2]['paths']['DIRECT'] = {'ok': 0, 'total': 3, 'delays': [], 'errors': {'timeout': 3}}
            return status, body
        with Agent(responder) as left, Agent(responder) as right:
            # One shared release event lets both real task locks be observed
            # before the busy requests release either worker.
            right.release = left.release
            opts = options()
            opts['agent_updates'] = {'domain_discovery_enabled': True}
            opts['agent_port_pairs'] = {'agent-synthetic': [left.port, right.port]}
            opts['profile']['domain_discovery'].update(auto_probe=True, auto_apply=True, auto_apply_min_confidence=threshold)
            if shape == 'recheck-after-30-days':
                from cases.discovery_auto_quiet import history
                old = (datetime.now(timezone.utc) - timedelta(hours=30 * 24 + 1)).replace(tzinfo=None).isoformat(timespec='seconds')
                opts['profile']['domain_discovery']['history'] = [history(old)]
                for library in opts['shared']['rule_library']:
                    if library['id'] == PROXY['id']:
                        library['content'] += '+.example.com\n'
            if shape == 'manual-apply-undo':
                from cases.discovery_auto_quiet import history
                opts['profile']['domain_discovery']['history'] = [{**history('2026-01-01T00:00:00'), 'value': 'undo.example'}]
                for library in opts['shared']['rule_library']:
                    if library['id'] == PROXY['id']:
                        library['content'] += '+.undo.example\n'
            with Pair(binary, recorder, options=opts) as pair:
                pair.policy = profile_policy(pair)
                other = {key: value for key, value in database(pair.dirs[0])['fields'].items() if key.startswith('profile/other/')}
                heartbeat(pair, variant='T21/' + variant)
                traffic(pair, ['example.com'], variant='T21/' + variant + '/traffic')
                if passive_failure:
                    # A second actual public report adds passive failures; it
                    # is not a fabricated persisted confidence/suggestion.
                    pair.request('T20', 'POST /api/agents/<agent_id>/traffic-report', '/api/agents/agent-synthetic/traffic-report',
                        body=json_body({'schema': 1, 'status': 'running', 'mihomo_version': 'v1.19.33', 'items': [
                            {'host': HOST, 'port': 443, 'network': 'tcp', 'rule': 'Match', 'rule_payload': '', 'policy': 'DIRECT', 'outlet': 'direct',
                             'conns': 0, 'fails': 10, 'fail_kinds': {'timeout': 10}, 'zero_dl': 0, 'up': 0, 'down': 0}]}),
                        auth=AGENT, expect=200, writes=True, variant='T21/' + variant + '/passive-report')
                for library in opts['shared']['rule_library']:
                    if library['id'] in (DIRECT['id'], PROXY['id']):
                        write_cache(pair, library, library['content'].encode())
                if shape == 'reprobe-after-3-days':
                    from cases.discovery_auto_quiet import probe_record
                    old = (datetime.now(timezone.utc) - timedelta(hours=3 * 24 + 1)).replace(tzinfo=None).isoformat(timespec='seconds')
                    put_file(pair, 'probes/agent-synthetic.json', {'hosts': {HOST: probe_record(old)}})
                pair.policy.times.add('files.probes/agent-synthetic.json.content.hosts.' + HOST + '.checked_at')
                configure(pair, KEY)
                observer = Running(pair, [left, right], 'probe', 1)
                primary = threshold == 80 and not shape
                pair.request('R227-S03' if primary else 'T21', 'POST /api/tasks/run', body=json_body({'key': KEY}), expect=202, writes=True, runtime_writes=True,
                             interaction=Started(pair, [left, right]), variant=variant + '/start')
                def current(reply):
                    reply_clock(pair, reply, '')
                    observer.reference_current(reply, 'probe_job')
                pair.request('T21', 'GET /api/domain-discovery/domains', '/api/domain-discovery/domains?view=all', expect=200, writes=True,
                             check=current, capture=observer.current('probe_job'), variant=variant + '/running')
                if shape == 'manual-apply-undo':
                    pair.request('T21', 'POST /api/domain-discovery/apply',
                                 body=json_body({'items': [{'value': 'manual.example.net', 'rule_type': 'DOMAIN', 'target': 'direct'}]}),
                                 expect=200, writes=True, check=lambda r: history_times(pair), variant=variant + '/manual-apply-while-blocked')
                    pair.request('T21', 'POST /api/domain-discovery/undo', body=json_body({'value': 'undo.example', 'target': 'proxy'}),
                                 expect=200, writes=True, check=lambda r: history_times(pair), variant=variant + '/manual-undo-while-blocked')
                status = 'failed' if malformed else 'success'
                completion = Completion(pair, KEY, status=status, error=FAILURE if malformed else '', gate=left.release)
                def after_task(reply):
                    history = stored(pair).get('history', [])
                    adopted = not malformed and (85 if not passive_failure else 99) >= threshold
                    if shape == 'recheck-after-30-days':
                        require(len(history) == 1 and history[0]['applied_at'] == old and history[0]['recheck']['suggest_remove'] is True
                                and history[0]['recheck']['verdict'] == 'both_ok' and history[0]['recheck']['reason'] == '直连已经可以正常访问',
                                '30-day-plus-one-hour proxy history is rechecked, keeps original adoption timestamp, and records removal suggestion without undo')
                        pair.policy.times.add('db.fields.profile/default/domain_discovery.value.history[0].recheck.at')
                    elif shape == 'manual-apply-undo':
                        history_times(pair)
                        require({entry['value']: entry['source'] for entry in history} == {'manual.example.net': 'manual', 'example.com': 'auto'},
                                'manual apply and undo overlapping held auto RPC preserve both intended surviving histories')
                        libs = {lib['id']: lib for lib in database(pair.dirs[0])['fields']['shared//rule_library']['value']}
                        require('DOMAIN,manual.example.net' in libs[DIRECT['id']]['content'] and '+.example.com' in libs[PROXY['id']]['content']
                                and 'undo.example' not in libs[PROXY['id']]['content'], 'auto refresh preserves manual rule and completed undo without restoring stale content')
                    else:
                        history_times(pair)
                        require(len(history) == int(adopted), 'threshold applies only eligible 85/99-confidence suggestion')
                    if adopted and shape != 'recheck-after-30-days':
                        entry = next(item for item in history if item['value'] == 'example.com')
                        require(entry['target'] == 'proxy' and entry['source'] == 'auto' and entry['confidence'] == (99 if passive_failure else 85),
                                'actual probe/passive evidence drives auto-only selected history')
                    require({key: value for key, value in database(pair.dirs[0])['fields'].items() if key.startswith('profile/other/')} == other,
                            'automatic round does not change other profile rows/revisions')
                pair.request('R227-B01' if primary else 'T21', 'POST /api/tasks/run', body=json_body({'key': KEY}), expect=409, writes=True,
                             interaction=completion, check=after_task, variant=variant + '/busy-completion')
                final = observer.finish(probe_calls([HOST]), status='failed' if malformed else 'done', expected_done=0 if malformed else 1,
                                        case='H072' if malformed else 'T21', variant=variant + '/job-final', route='POST /api/tasks/run')
                if malformed:
                    require(final[0]['job']['message'] == FAILURE and not (pair.dirs[0] / 'probes/agent-synthetic.json').exists(),
                            'malformed successful Agent body causes synchronous job failure, exact safe message and no probe output')
                pair.request('T21', 'GET /api/tasks', expect=200, writes=True, capture=task_capture(pair), variant=variant + '/final-task')


GROUPS = {'discovery-automatic-thresholds': automatic}


def aging(binary, recorder):
    automatic(binary, recorder, [(80, False, False, 'reprobe-after-3-days'), (95, False, False, 'recheck-after-30-days'),
                                (80, False, False, 'manual-apply-undo')])


GROUPS['discovery-automatic-aging'] = aging
