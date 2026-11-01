"""Optional real OxiDNS reload acceptance; run only on the test server."""
import hashlib
import json
import os
import subprocess
import time

import pytest
import requests
import yaml

from backend.agents import oxidns_monitor as monitor
from backend.agents import oxidns_publication_stats as publication
from backend.test_oxidns_dashboard import Manager
from backend.test_oxidns_publication_stats import task
from scripts.benchmark_oxidns_domain_routing import DelayedDNS, free_port, query


@pytest.mark.skipif(not os.environ.get('OXIDNS_TEST_BINARY'), reason='Set OXIDNS_TEST_BINARY for real-core acceptance')
def test_real_core_reloads_preserve_dashboard_counters_without_background_sampling(tmp_path, monkeypatch):
    dns_port, api_port = free_port(), free_port()
    upstream = DelayedDNS(True)
    session = requests.Session()
    session.trust_env = False
    base = f'http://127.0.0.1:{api_port}'
    path = tmp_path / 'config.yaml'
    document = {'log': {'level': 'warn'}, 'api': {'http': {'listen': f'127.0.0.1:{api_port}'}}, 'plugins': [
        {'tag': 'cache', 'type': 'cache', 'args': {'size': 128, 'short_circuit': True}},
        {'tag': 'forward', 'type': 'forward', 'args': {'upstreams': [{'addr': f'udp://127.0.0.1:{upstream.server_address[1]}'}]}},
        {'tag': 'main', 'type': 'sequence', 'args': [{'exec': '$cache'}, {'exec': '$forward'}]},
        {'tag': 'dns', 'type': 'udp_server', 'args': {'listen': f'127.0.0.1:{dns_port}', 'entry': 'main'}},
    ]}

    def write_config():
        path.write_text(yaml.safe_dump(document), encoding='utf-8')
        return hashlib.sha256(path.read_bytes()).hexdigest()

    def get(endpoint):
        response = session.get(base + endpoint, timeout=3)
        response.raise_for_status()
        return response

    def snapshot(*args, **kwargs):
        status = get('/api/reload/status').json()
        assert not status['pending'] and not status['in_progress']
        runtime = {key: status.get(key) for key in ('running_version', 'last_started_ms', 'last_completed_ms', 'last_success_ms')}
        return {'success': True, 'health': get('/api/health').json(), 'capabilities': {'query_recorders': []},
                'metrics': get('/api/metrics').text, 'collected_at_ms': time.time_ns() // 1_000_000,
                'source_revision': hashlib.sha256(path.read_bytes()).hexdigest(),
                'runtime_revision': hashlib.sha256(json.dumps(runtime, sort_keys=True).encode()).hexdigest()}

    write_config()
    process = None
    try:
        with (tmp_path / 'core.log').open('w') as log:
            process = subprocess.Popen([os.environ['OXIDNS_TEST_BINARY'], 'start', '-c', str(path), '-d', str(tmp_path)], stdout=log, stderr=log)
            for _ in range(100):
                try:
                    if get('/api/readyz').ok:
                        break
                except requests.RequestException:
                    assert process.poll() is None, (tmp_path / 'core.log').read_text()
                    time.sleep(.05)
            else:
                pytest.fail('Temporary OxiDNS did not become ready')
            monkeypatch.setattr(monitor, '_request', snapshot)
            manager = Manager(tmp_path / 'panel')
            initial = monitor.overview(manager, 'one')
            total = 0
            for index in range(2):
                for _ in range(8):
                    assert query(dns_port, 'publication.direct.test')['rcode'] == 0
                    total += 1
                # Use the candidate's digest without changing the running file yet.
                document['plugins'][0]['args']['size'] += 1
                candidate = yaml.safe_dump(document).encode()
                revision = hashlib.sha256(candidate).hexdigest()
                ident = f'publication-{index}'
                task(manager, ident, revision=revision)
                publication.checkpoint(manager, 'one', ident, revision)
                before = get('/api/reload/status').json().get('last_success_ms')
                path.write_bytes(candidate)
                response = session.post(base + '/api/reload', timeout=3)
                response.raise_for_status()
                for _ in range(100):
                    status = get('/api/reload/status').json()
                    if not status['pending'] and not status['in_progress'] and status.get('last_success_ms') != before:
                        break
                    time.sleep(.05)
                else:
                    pytest.fail('Temporary OxiDNS reload did not finish')
                for _ in range(3):
                    assert query(dns_port, 'publication.direct.test')['rcode'] == 0
                    total += 1
                task(manager, ident, 'succeeded', revision)
                publication.complete(manager, 'one', ident, 'succeeded')
                result = monitor.overview(manager, 'one')
                assert result['metrics']['requests_total'] == total
                assert result['statistics']['publication_count'] == index + 1
                assert result['statistics']['started_at_ms'] == initial['statistics']['started_at_ms']
                assert result['metrics']['cache_hit_rate'] > 0
                assert result['metrics']['inflight'] == 0
    finally:
        session.close()
        upstream.close()
        if process is not None:
            process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)
