"""Drop the real HTTP client before work finishes; poll scope concurrently."""
from concurrent.futures import ThreadPoolExecutor
import http.client
import time
from urllib.parse import urlsplit

from agent_fake import Agent, probe_calls
from agent_jobs import Running, job_errors
from compare import response_diffs, database, files, database_diffs, file_diffs
from harness import Pair, DesignError, json_body, send
from cases.discovery_reads import heartbeat
from cases.domain_discovery import options
from cases.helpers import profile_policy
from cases.subscriptions import require


def poll_scopes(pair, observer):
    scopes = ['default', 'other'] * 6
    before = (database(pair.dirs[0]), files(pair.dirs[0]))
    batches = []
    for index, base in enumerate(pair.bases):
        def poll(scope):
            path = '/api/domain-discovery/probe/' + (observer.ids[index] or 'missing') + '?profile=' + scope
            return send(base, 'GET', path, headers={'Authorization': 'Bearer ' + pair.tokens[index],
                                                   'X-ConfigFlow-Profile': 'default'})
        with ThreadPoolExecutor(max_workers=6) as pool:
            batches.append(list(pool.map(poll, scopes)))
    pair.policy.end = time.time()
    for scope, reply in zip(scopes, batches[0]):
        require(reply.status == (200 if scope == 'default' else 404), 'concurrent owner/foreign queries keep their own scope')
        if scope == 'other':
            require(reply.json() == {'success': False, 'message': 'Probe job not found'}, 'foreign poll does not disclose ID, progress or errors')
        else:
            require(reply.json()['job']['id'] == observer.ids[0] and reply.json()['job']['status'] == 'running', 'owner still sees held running job')
    require((database(pair.dirs[0]), files(pair.dirs[0])) == before, 'concurrent poll is read-only for all DB rows and files')
    diffs = []
    for position, (left, right) in enumerate(zip(*batches)):
        for diff in response_diffs(left, right, pair.policy):
            diff.path = f'poll[{position}].' + diff.path
            diffs.append(diff)
    diffs += database_diffs(*pair.dirs, pair.policy) + file_diffs(*pair.dirs, pair.policy)
    pair.recorder.record('R052-B02', '12-concurrent-owner-and-foreign', 'GET /api/domain-discovery/probe/<job_id>',
                         'B', 'different' if diffs else 'compared', diffs, python_status=[r.status for r in batches[0]],
                         go_status=[r.status for r in batches[1]])


def disconnect(binary, recorder):
    with Agent() as left, Agent() as right:
        opts = options()
        opts['agent_updates'] = {'domain_discovery_enabled': True}
        opts['agent_port_pairs'] = {'agent-synthetic': [left.port, right.port]}
        with Pair(binary, recorder, options=opts) as pair:
            pair.policy = profile_policy(pair)
            heartbeat(pair, variant='disconnect-probe')
            pair.policy.times.add('files.probes/agent-synthetic.json.content.hosts.example.com.checked_at')
            pair.policy.times.add('body.probe_job.started_at')
            observer = Running(pair, [left, right], 'probe', 1)
            for index, base in enumerate(pair.bases):
                url = urlsplit(base)
                connection = http.client.HTTPConnection(url.hostname, url.port, timeout=5)
                try:
                    connection.request('POST', '/api/domain-discovery/probe', body=json_body({'domains': ['example.com']}).encode(),
                        headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + pair.tokens[index],
                                 'X-ConfigFlow-Profile': 'default'})
                    started = (left, right)[index].seen.wait(4)
                finally:
                    # No getresponse() or body read: the browser socket is gone
                    # while Agent work is provably still blocked at the gate.
                    connection.close()
                if index == 0 and not started:
                    raise DesignError('Python disconnected start did not contact its Agent')
                if not started:
                    observer.violations[index].append('disconnected start did not contact Agent')
            def capture(replies, policy):
                for index, reply in enumerate(replies):
                    value = observer.decode(reply)
                    job = value.get('probe_job') if isinstance(value, dict) else None
                    errors = job_errors({'success': True, 'job': job}, policy, 'probe', 1)
                    if index == 0 and errors:
                        raise DesignError('Python disconnected running state: ' + repr((errors, value)))
                    observer.violations[index] += errors
                    if not errors:
                        observer.ids[index], observer.previous[index] = job['id'], job
                return observer.capture(replies, policy) + observer.compare(policy)
            pair.request('R047-S03', 'GET /api/domain-discovery/domains', '/api/domain-discovery/domains?view=all',
                         expect=200, writes=True, capture=capture, variant='disconnected-probe-running')
            poll_scopes(pair, observer)
            observer.finish(probe_calls(['example.com']), case='R051-B03', variant='closed-client-before-Agent-release')


GROUPS = {'discovery-client-disconnect': disconnect}
