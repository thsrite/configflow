"""Private, egress-isolated A13 causality test using real Python preparation.

No production values or prepared bytes leave the temporary server directory.
The normal production differential and its comparison rules are unchanged.
"""
import hashlib
from concurrent.futures import ThreadPoolExecutor
import json
import os
import re
from pathlib import Path
import runpy
import shutil
import subprocess
import sys
import tempfile
import traceback

sys.path.insert(0, str(Path(__file__).resolve().parent))
import run as pd
from content_policy import HEADER, checking


def main():
    snapshot, binary = map(Path, sys.argv[1:3])
    pd.assert_no_egress()
    before = pd.digest_tree(snapshot)
    private = Path.home() / 'cf-prod-snapshot/runs'
    with tempfile.TemporaryDirectory(prefix='a13-proof-', dir=private) as temporary:
        root = Path(temporary)
        py_data, go_data = root / 'python', root / 'go'
        for data in (py_data, go_data):
            shutil.copytree(snapshot, data)
        prepared = root / 'prepared'
        prepared.mkdir(mode=0o700)
        os.environ.update(DATA_DIR=str(py_data), LOG_FILE=str(root / 'python.log'), LOG_LEVEL='CRITICAL',
                          ADMIN_USERNAME='proddiff-admin', ADMIN_PASSWORD='proddiff-test-password',
                          JWT_SECRET_KEY='proddiff-test-only-shared-secret', SUB_STORE_BUILTIN_DIR='/nonexistent',
                          CONFIGFLOW_BACKEND_PORT='15001',
                          PRODDIFF_PY_LOOPS=str(root / 'loops'), PRODDIFF_PY_ROUTES=str(root / 'routes'),
                          PRODDIFF_PY_THREADS=str(root / 'threads'))
        # Reuse the standard launcher's disabled-loop list and app setup; only
        # serving is suppressed because this oracle uses Flask's real test client.
        import waitress
        waitress.serve = lambda *args, **kwargs: None
        with (root / 'oracle.log').open('w') as log:
            original_stdout, original_stderr = sys.stdout, sys.stderr
            executor = None
            try:
                sys.stdout = sys.stderr = log
                app = runpy.run_path('server/tools/proddiff/flask_worker.py')['app']
                from backend.routes import agents
                from backend.agents import content_sync
                real_prepare = agents._prepare_content_bundle

                def capture(agent, config, **kwargs):
                    bundle = real_prepare(agent, config, **kwargs)
                    name = hashlib.sha256(agent['id'].encode()).hexdigest()
                    # Export exactly the bundle returned by the real A14 Python
                    # entry; Go Open verifies its archive and manifest digests.
                    (prepared / (name + '.tar.gz')).write_bytes(bundle.archive)
                    (prepared / (name + '.json')).write_text(json.dumps(dict(
                        manifest=bundle.manifest, sha256=bundle.sha256,
                        config_version=bundle.config_version, comparison_config=bundle.comparison_config)))
                    return bundle

                agents._prepare_content_bundle = capture
                executor = ThreadPoolExecutor(max_workers=2, thread_name_prefix='a13-proof-content')
                content_sync._checker = content_sync.ContentSyncChecker(executor=executor)
                client = app.test_client()
                login = client.post('/api/auth/login', json=dict(username='proddiff-admin', password='proddiff-test-password'))
                headers = {'Authorization': 'Bearer ' + login.get_json()['token'], 'Host': '127.0.0.1:5001'}
                rows = [line.split('\t') for line in (pd.PARITY / 'routes.tsv').read_text().splitlines()
                        if line and not line.startswith('#')]
                selected = {'/api/agents', '/api/agents/<agent_id>', '/api/agents/<agent_id>/push-diff'}
                cases = [case for case in pd.cases(rows, pd.document(py_data), py_data)
                         if case[0] == 'GET' and case[1] in selected]
                fixtures = []
                for _, route, path, _, _ in cases:
                    reply = client.get(path, headers=headers)
                    if reply.headers.get(HEADER):
                        raise RuntimeError('reference content observer timed out')
                    value = reply.get_json()
                    if checking(value):
                        raise RuntimeError('reference observer returned a transient comparison')
                    fixtures.append(dict(route=route, path=path, status=reply.status_code,
                                         headers={k: reply.headers.get(k, '') for k in
                                                  ('Content-Type', 'Cache-Control', 'X-ConfigFlow-Profile')}, body=value))
                (root / 'fixtures.json').write_text(json.dumps(fixtures))
            finally:
                if executor is not None:
                    executor.shutdown(wait=True, cancel_futures=True)
                sys.stdout, sys.stderr = original_stdout, original_stderr
        env = dict(os.environ, CONFIGFLOW_A13_PROOF=str(root), DATA_DIR=str(go_data), LOG_FILE=str(root / 'go.log'))
        with (root / 'go-proof.log').open('w') as log:
            result = subprocess.run([str(binary), '-test.run=^TestRealPreparationProof$', '-test.v'],
                                    env=env, stdout=log, stderr=subprocess.STDOUT)
        if result.returncode:
            # Surface only our fixed diagnostic sentences, never framework
            # errors or logs that could contain private response values.
            for line in (root / 'go-proof.log').read_text().splitlines():
                if re.search(r'content_proof_test.go:\d+: (case \d+:|unexpected difference|an authorized|invalid comparison|cached comparison|distinct comparison)', line):
                    print(line)
            raise RuntimeError('A13 real-preparation proof failed')
        if before != pd.digest_tree(snapshot):
            raise RuntimeError('source snapshot changed')
        print(f'A13 real preparation: {len(fixtures)} HTTP cases, {len(list(prepared.glob("*.json")))} actual bundles; all 24 deferred fields reproduced without preparation and resolved with it; PASS')


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        frames = ', '.join(f'{Path(frame.filename).name}:{frame.lineno}' for frame in traceback.extract_tb(error.__traceback__))
        print(f'A13 preparation proof failed ({type(error).__name__}; {frames}); no response data is printed', file=sys.stderr)
        sys.exit(1)
