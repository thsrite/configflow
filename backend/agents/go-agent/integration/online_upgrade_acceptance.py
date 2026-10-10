#!/usr/bin/env python3
"""Real old-to-new systemd Agent update acceptance on an isolated Linux host.

Requires root and refuses to run if a native production Agent or recovery gate
already exists. Only its dedicated core ports, test data, units and container
are used. The legacy executable is compiled unchanged from the old Git revision.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import time
import urllib.error
import urllib.request
import uuid

from acceptance import Target, dns_answer


def run(*args, check=True):
    result = subprocess.run([str(x) for x in args], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=90)
    text = result.stdout.decode(errors='replace')
    if check and result.returncode:
        raise RuntimeError(text[-5000:])
    return text


def request(url, body=None):
    headers = {'Content-Type': 'application/json'}
    if ':23680/' in url:
        headers['Authorization'] = 'Bearer upgrade-acceptance-token'
    req = urllib.request.Request(url, data=json.dumps(body).encode() if body is not None else None,
        headers=headers)
    try:
        response = urllib.request.build_opener(urllib.request.ProxyHandler({})).open(req, timeout=20)
    except urllib.error.HTTPError as exc:
        response = exc
    with response:
        return response.status, json.load(response)


def wait_http(url):
    deadline = time.monotonic() + 35
    while time.monotonic() < deadline:
        try:
            code, value = request(url)
            if code == 200:
                return value
        except Exception:
            pass
        time.sleep(.2)
    raise AssertionError('not ready: ' + url)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--core-root', type=Path, required=True)
    parser.add_argument('--docker', default='/usr/local/bin/docker')
    parser.add_argument('--legacy-binary', default='legacy-agent')
    parser.add_argument('--legacy-version', default='1.1.0-go')
    parser.add_argument('--legacy-revision', default='98e5550')
    args = parser.parse_args()
    root = args.root.resolve()
    binary = Path('/usr/local/bin/configflow-agent')
    identity = Path('/opt/configflow-agent')
    updates = Path('/usr/local/bin/.configflow-updates')
    agent_unit = Path('/run/systemd/system/configflow-agent.service')
    owned = [binary, identity, updates, agent_unit]
    for kind in ('mihomo', 'mosdns'):
        owned += [Path('/etc/systemd/system/configflow-recover-' + kind + '.service'),
                  Path('/etc/systemd/system/configflow-upgrade-test-' + kind + '.service.d'),
                  Path('/run/systemd/system/configflow-upgrade-test-' + kind + '.service')]
    assert 'LoadState=not-found' in run('systemctl', 'show', 'configflow-agent', '-p', 'LoadState')
    assert not [str(path) for path in owned if path.exists()], 'refusing to touch an existing native Agent installation'
    name = 'configflow-online-upgrade-' + uuid.uuid4().hex[:8]
    report = {'success': False, 'cases': [], 'legacy_revision': args.legacy_revision, 'legacy_version': args.legacy_version, 'target_version': '1.4.0-go'}
    report['new_agent_sha256'] = hashlib.sha256((root / 'new-agent').read_bytes()).hexdigest()
    report['legacy_agent_sha256'] = hashlib.sha256((root / args.legacy_binary).read_bytes()).hexdigest()
    jobs, units = [], []
    base = 'http://127.0.0.1:23601'
    try:
        run(args.docker, 'run', '-d', '--name', name, '--network', 'host',
            '-v', str(root / 'src') + ':/src:ro', '-v', str(root) + ':/acceptance',
            '-e', 'PYTHONPATH=/src', '--entrypoint', 'python3', 'thsrite/config-flow:latest',
            '/src/backend/agents/go-agent/integration/upgrade_backend.py', '--root', '/acceptance')
        wait_http(base + '/test/ready')
        for kind in ('mihomo', 'mosdns'):
            target = Target(root, args.docker, 'native', kind, name)
            target.base = 23600
            target.core_port = 23690 if kind == 'mihomo' else 23653
            target.write_initial()
            core_unit = 'configflow-upgrade-test-' + kind + '.service'
            core_binary = args.core_root / 'bin' / kind
            command = str(core_binary) + ' -d ' + str(target.live)
            if kind == 'mosdns':
                command = str(core_binary) + ' start -c ' + str(target.live / 'config.yaml') + ' -d ' + str(target.live)
            Path('/run/systemd/system', core_unit).write_text('[Service]\nType=simple\nExecStart=' + command + '\n')
            units.append(core_unit)
            identity.mkdir(mode=0o700, exist_ok=True)
            config_path = identity / 'config.json'
            original = dict(server_url=base, agent_name='upgrade-test-' + kind, agent_host='127.0.0.1',
                agent_port=23680, service_type=kind, deployment_method='shell', service_name=kind,
                config_path=str(target.live / 'config.yaml'), restart_command='systemctl restart ' + core_unit,
                heartbeat_interval=2, agent_id='upgrade-test-' + kind, token='upgrade-acceptance-token',
                enable_metrics=False, future_extension={'preserve': ['unchanged', 42]})
            agent_unit.write_text('[Unit]\nDescription=Isolated legacy Agent upgrade acceptance\n[Service]\nType=simple\nExecStart=/usr/local/bin/configflow-agent -config /opt/configflow-agent/config.json\nEnvironment=PATH=' + str(args.core_root / 'bin') + ':/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin\nRestart=always\nRestartSec=2\n')
            run('systemctl', 'daemon-reload')
            run('systemctl', 'start', core_unit)

            def reset_old(custom=False):
                run('systemctl', 'stop', 'configflow-agent', check=False)
                value = dict(original)
                if custom:
                    value['restart_command'] = 'sh -c "systemctl restart ' + core_unit + '"'
                config_path.write_text(json.dumps(value))
                os.chmod(config_path, 0o600)
                shutil.copy2(root / args.legacy_binary, binary)
                os.chmod(binary, 0o755)
                code, data = request(base + '/test/reset', {'service': kind})
                assert code == 200, data
                run('systemctl', 'reset-failed', 'configflow-agent', check=False)
                run('systemctl', 'start', 'configflow-agent')
                wait_http('http://127.0.0.1:23680/health')

            def core_snapshot():
                return {str(path.relative_to(target.live)): hashlib.sha256(path.read_bytes()).hexdigest()
                        for path in target.live.rglob('*') if path.is_file() and path.suffix in ('.yaml', '.list', '.txt')
                        and '.configflow-deployments' not in path.parts}

            def update(label, expected, fault='none', interrupt=False):
                before = core_snapshot()
                request(base + '/test/fault', {'fault': fault})
                code, state = request(base + '/api/agents/upgrade-test-' + kind + '/update', {})
                ident = state.get('update_id')
                if ident:
                    jobs.append(ident)
                assert code == 202, (label, code, state)
                stages = []
                killed = False
                health_checks = 0
                for _ in range(450):
                    code, state = request(base + '/api/agents/upgrade-test-' + kind + '/upgrade')
                    assert code == 200, state
                    if not stages or stages[-1] != state['status']:
                        stages.append(state['status'])
                    assert run('systemctl', 'is-active', core_unit).strip() == 'active'
                    health_checks += 1
                    journal_path = updates / 'current.json'
                    journal = json.loads(journal_path.read_text()) if journal_path.exists() else {}
                    if interrupt and not killed and journal.get('update_id') == ident and journal.get('status') == 'checking':
                        run('systemctl', 'kill', '--signal=KILL', 'configflow-upgrade-' + ident[:16])
                        killed = True
                    if state['status'] in ('succeeded', 'failed', 'rolled_back', 'rollback_failed'):
                        break
                    time.sleep(.2)
                assert state['status'] == expected, (label, state, stages)
                assert not interrupt or killed
                if interrupt:
                    assert 'interrupted Agent update' in state.get('error', ''), state
                assert core_snapshot() == before, 'Agent upgrade changed core files'
                current = json.loads(config_path.read_text())
                for key in ('agent_id', 'token', 'server_url', 'agent_port', 'config_path', 'future_extension'):
                    assert current[key] == original[key], ('identity/config changed', key)
                report['cases'].append({'service': kind, 'case': label, 'status': state['status'],
                    'update_id': ident, 'stages': stages, 'core_health_checks': health_checks,
                    'core_files_unchanged': True, 'identity_preserved': True, 'error': state.get('error', '')})
                print('PASS', kind, label, state['status'], flush=True)
                request(base + '/test/fault', {'fault': 'none'})

            reset_old(custom=True)
            update('legacy_migration_failure', 'rolled_back')
            assert hashlib.sha256(binary.read_bytes()).hexdigest() == report['legacy_agent_sha256']
            reset_old()
            update('legacy_one_click_success', 'succeeded')
            info = wait_http('http://127.0.0.1:23680/api/upgrade-info')
            assert info['version'] == '1.4.0-go' and info['migration_ready']
            assert 'configflow-recover-' + kind in run('systemctl', 'show', core_unit, '-p', 'Requires')
            update('new_protocol_success', 'succeeded')
            update('download_http503', 'failed', 'http503')
            update('download_sha256_mismatch', 'failed', 'corrupt')
            update('new_executable_start_failure', 'rolled_back', 'bad_start')
            update('interrupted_update_worker', 'rolled_back', 'bad_start', True)
            deployment_id = uuid.uuid4().hex
            code, result = request(base + '/api/agents/upgrade-test-' + kind + '/push-config', {'deployment_id': deployment_id, 'restart': True})
            assert code == 202, result
            for _ in range(160):
                _, result = request(base + '/api/agents/upgrade-test-' + kind + '/deployments/' + deployment_id)
                if result['status'] == 'succeeded':
                    break
                assert result['status'] not in ('failed', 'rolled_back', 'rollback_failed'), result
                time.sleep(.25)
            assert result['status'] == 'succeeded', result
            if kind == 'mihomo':
                wait_http('http://127.0.0.1:23690/version')
            else:
                assert dns_answer(23653) == '192.0.2.20'
            report['cases'].append({'service': kind, 'case': 'publish_after_upgrade', 'status': 'succeeded'})
            print('PASS', kind, 'publish_after_upgrade', flush=True)
            run('systemctl', 'stop', 'configflow-agent', core_unit)
            # Trigger the actual installed recovery gate before a fresh core start.
            run('systemctl', 'stop', 'configflow-recover-' + kind, check=False)
            run('systemctl', 'start', core_unit)
            assert run('systemctl', 'is-active', 'configflow-recover-' + kind).strip() == 'active'
            report['cases'].append({'service': kind, 'case': 'recovery_gate_before_core', 'status': 'succeeded'})
            run('systemctl', 'stop', core_unit)
            shutil.rmtree(updates, ignore_errors=True)
        report['success'] = True
    except Exception as exc:
        report['error'] = repr(exc)
        raise
    finally:
        (root / 'backend.log').write_text(run(args.docker, 'logs', name, check=False))
        run(args.docker, 'rm', '-f', name, check=False)
        (root / 'agent.log').write_text(run('journalctl', '-u', 'configflow-agent', '--no-pager', check=False))
        run('systemctl', 'stop', 'configflow-agent', check=False)
        for ident in jobs:
            unit = 'configflow-upgrade-' + ident[:16]
            (root / (unit + '.log')).write_text(run('journalctl', '-u', unit, '--no-pager', check=False))
            run('systemctl', 'stop', unit, check=False)
            run('systemctl', 'disable', unit, check=False)
            Path('/etc/systemd/system', unit + '.service').unlink(missing_ok=True)
        for unit in units:
            run('systemctl', 'stop', unit, check=False)
        for kind in ('mihomo', 'mosdns'):
            run('systemctl', 'stop', 'configflow-recover-' + kind, check=False)
        for path in reversed(owned):
            if path.is_dir():
                shutil.rmtree(path)
            else:
                path.unlink(missing_ok=True)
        run('systemctl', 'daemon-reload', check=False)
        report['cleanup_success'] = not any(path.exists() for path in owned)
        (root / 'report.json').write_text(json.dumps(report, indent=2))
        print(json.dumps({'success': report['success'], 'cases': len(report['cases']), 'cleanup': report['cleanup_success']}), flush=True)


if __name__ == '__main__':
    main()
