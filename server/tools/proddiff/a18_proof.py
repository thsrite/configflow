"""Targeted A18 baseline closure against the offline snapshot, egress isolated.

Only HA rules/final-generation and their log consequences run. Values, devices,
responses and logs remain in the private server directory; stdout is summary only.
"""
import contextlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time

sys.path.insert(0, str(Path(__file__).resolve().parent))
import run as pd


def main():
    snapshot, binary = map(Path, sys.argv[1:3])
    pd.assert_no_egress()
    if not os.statvfs(snapshot).f_flag & os.ST_RDONLY:
        raise RuntimeError('snapshot must be read-only')
    before = pd.digest_tree(snapshot)
    private = Path.home() / 'cf-prod-snapshot/runs'
    lock = pd.lock_runs(private)
    processes = []
    try:
        with tempfile.TemporaryDirectory(prefix='run-a18-', dir=private) as temporary, contextlib.ExitStack() as cleanup:
            cleanup.callback(pd.stop_processes, processes)
            root = Path(temporary)
            copy_root = root / 'copies-a18'
            copy_root.mkdir(mode=0o700)
            copies = [copy_root / 'python', copy_root / 'go']
            for data in copies:
                shutil.copytree(snapshot, data)
            pd.migrate_baselines(copies[1], root)
            doc = pd.document(copies[0])
            members = [a for a in doc['system'].get('agents', []) if isinstance(a, dict)
                       and a.get('service_type') == 'oxidns'
                       and isinstance(a.get('oxidns_ha'), dict) and a['oxidns_ha'].get('enabled') is True]
            if not members:
                raise RuntimeError('snapshot has no HA member to prove closure')
            rows = [line.split('\t') for line in (pd.PARITY / 'routes.tsv').read_text().splitlines()
                    if line and not line.startswith('#')]
            rules = pd.load_rules(rows)
            for index, data in enumerate(copies):
                env = dict(os.environ, DATA_DIR=str(data), LOG_FILE=str(data / 'app.log'), LOG_LEVEL='ERROR',
                           ADMIN_USERNAME='proddiff-admin', ADMIN_PASSWORD='proddiff-test-password',
                           JWT_SECRET_KEY='proddiff-test-only-shared-secret', CONFIGFLOW_BACKEND_HOST='127.0.0.1',
                           CONFIGFLOW_BACKEND_PORT=str(15001 + index), CONFIGFLOW_FRONTEND_DIR='',
                           SUB_STORE_BUILTIN_DIR='/nonexistent', PRODDIFF_ROUTES=str(root / 'go-routes.tsv'),
                           PRODDIFF_PY_ROUTES=str(root / 'python-routes.tsv'), PRODDIFF_PY_LOOPS=str(root / 'loops'),
                           PRODDIFF_PY_THREADS=str(root / 'threads'))
                command = [sys.executable, 'server/tools/proddiff/flask_worker.py'] if index == 0 else [str(binary)]
                with (root / ('python.log' if index == 0 else 'go.log')).open('wb') as log:
                    processes.append(subprocess.Popen(command, env=env, stdout=log, stderr=log, start_new_session=True))
            tokens = []
            for index in range(2):
                for _ in range(450):
                    if processes[index].poll() is not None:
                        raise RuntimeError('server startup failed')
                    response = pd.send(15001 + index, 'GET', '/api/auth/status')
                    if response[0] == 200 and (index == 0 or (root / 'go-routes.tsv.ready').exists()):
                        break
                    time.sleep(.2)
                else:
                    raise RuntimeError('server startup timeout')
                login = pd.send(15001 + index, 'POST', '/api/auth/login', body={
                    'username': 'proddiff-admin', 'password': 'proddiff-test-password'})
                if login[0] != 200:
                    raise RuntimeError('login failed')
                tokens.append(json.loads(login[2])['token'])
            selected = []
            for agent in members:
                prefix = '/api/agents/' + pd.urllib.parse.quote(str(agent['id']), safe='')
                selected.append(('GET', '/api/agents/<agent_id>/oxidns/rules/update', prefix + '/oxidns/rules/update', None))
                profile = agent.get('profile_id') or doc['system'].get('default_profile_id') or next(iter(doc['profiles']))
                selected.append(('POST', '/api/agents/<agent_id>/generate-config', prefix + '/generate-config', {
                    'expected_profile_id': profile, 'expected_service_type': 'oxidns'}))
            selected.extend(('GET', '/api/logs/' + leaf, '/api/logs/' + leaf, None) for leaf in ('info', 'tail'))
            found = set()
            negative = False
            for method, route, path, body in selected:
                replies = [pd.scrub(pd.send(15001 + i, method, path, tokens[i], body), copies[i]) for i in range(2)]
                if any(reply[0] == 0 for reply in replies):
                    raise RuntimeError('HTTP transport failed')
                for field, kind in pd.response_diffs(*replies, route, rules):
                    found.add((method + ' ' + route, field, kind))
                if route.endswith('/oxidns/rules/update'):
                    if replies[0][0] != 200 or replies[1][0] != 200:
                        raise RuntimeError('HA rules did not return a successful task')
                    # One negative control proves the exact response comparator observes a changed task.
                    changed = list(replies[1])
                    value = json.loads(changed[2])
                    value['task']['status'] = 'deliberately-wrong'
                    changed[2] = json.dumps(value).encode()
                    negative = negative or bool(pd.response_diffs(replies[0], changed, route, rules))
            deferred = {tuple(line.split('\t')[:3]) for line in (pd.PARITY / 'proddiff-baseline.txt').read_text().splitlines()
                        if '\tdeferred → A18' in line}
            stale = deferred - found
            still_deferred = deferred & found
            new = found - deferred
            if not negative:
                raise RuntimeError('negative control failed')
            print(f'A18 targeted snapshot: cases={len(selected)} new={len(new)} stale={len(stale)} '
                  f'remaining={len(still_deferred)} self-check=PASS', flush=True)
            for row in sorted(found):
                print('DIFF\t' + '\t'.join(row), flush=True)
            # Once the baseline rows are deleted, the same proof has stale=0.
            return int(bool(new or still_deferred))
    finally:
        pd.stop_processes(processes)
        lock.close()
        if pd.digest_tree(snapshot) != before:
            raise RuntimeError('snapshot changed')


if __name__ == '__main__':
    try:
        sys.exit(main())
    except Exception as error:
        print('A18 proof: infrastructure failure (' + type(error).__name__ + ')', flush=True)
        sys.exit(2)
