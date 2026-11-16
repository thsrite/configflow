"""Offline production-snapshot differential. Console and baseline contain schema only."""
import ast
import collections
import concurrent.futures
import copy
import hashlib
import io
import itertools
import json
import os
from pathlib import Path
import re
import shutil
import signal
import socket
import sqlite3
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import zipfile
import yaml

ROOT = Path.cwd()
PARITY = ROOT / 'server/parity'
PARAM = re.compile(r'<(?:[^:>]+:)?([^>]+)>')
BASE_URL = 'http://127.0.0.1:5001'
# A key is public only if it is a literal schema identifier in the reference source.
KEYS = {'<key>', '<file>', 'status', 'Content-Type', 'body', 'content', 'config'}
for source in (ROOT / 'backend').rglob('*.py'):
    try:
        tree = ast.parse(source.read_text())
    except (SyntaxError, UnicodeError):
        continue
    KEYS.update(node.value for node in ast.walk(tree) if isinstance(node, ast.Constant)
                and isinstance(node.value, str) and re.fullmatch(r'[A-Za-z_][A-Za-z_0-9-]*', node.value))


def safe_key(key):
    return key if isinstance(key, str) and key in KEYS else '<key>'


def differences(a, b, path='body'):
    if type(a) is not type(b):
        # JSON numbers use mathematical equality, bool remains a distinct type.
        if type(a) in (int, float) and type(b) in (int, float) and a == b:
            return []
        return [(path, 'type')]
    if isinstance(a, dict):
        out = []
        for key in sorted(a.keys() | b.keys(), key=str):
            child = path + '.' + safe_key(key)
            if key not in a:
                out.append((child, 'extra-in-go'))
            elif key not in b:
                out.append((child, 'missing-in-go'))
            else:
                out.extend(differences(a[key], b[key], child))
        return out
    if isinstance(a, list):
        out = [] if len(a) == len(b) else [(path, 'length')]
        for i, (left, right) in enumerate(zip(a, b)):
            out.extend(differences(left, right, f'{path}[{i}]'))
        return out
    return [] if a == b else [(path, 'value')]


def text(value):
    return value.replace('\r\n', '\n').replace('\r', '\n').rstrip('\n') + '\n'


def parsed(body, content_type, route, member=''):
    if 'json' in content_type:
        return json.loads(body)
    if body.startswith(b'PK\x03\x04'):
        with zipfile.ZipFile(io.BytesIO(body)) as archive:
            return {name: parsed(archive.read(name), '', route, name) for name in archive.namelist() if not name.endswith('/')}
    value = body.decode('utf-8')
    if ('yaml' in content_type or member.endswith(('.yaml', '.yml')) or
            re.search(r'/(mihomo|mosdns|oxidns)(/preview)?$', route)):
        return yaml.safe_load(value)
    return text(value).splitlines()


def response_diffs(left, right, route, rules):
    out = []
    if left[0] != right[0]:
        out.append(('status', 'value'))
    if left[1].lower().replace(' ', '') != right[1].lower().replace(' ', ''):
        out.append(('Content-Type', 'value'))
    if left[0] == 0 or right[0] == 0:
        # Both failures are recorded; a one-sided transport failure cannot pass.
        return out
    try:
        a, b = (parsed(x[2], x[1], route) for x in (left, right))
    except (ValueError, UnicodeError, yaml.YAMLError, zipfile.BadZipFile):
        return out + ([] if left[2] == right[2] else [('body', 'unparseable-value')])
    if isinstance(a, dict) and isinstance(b, dict):
        # Generated YAML embedded in JSON is compared structurally too.
        for key in ('content', 'config'):
            if isinstance(a.get(key), str) and isinstance(b.get(key), str):
                service_yaml = any('/' + service in route for service in ('mihomo', 'mosdns', 'oxidns'))
                try:
                    a[key], b[key] = ((yaml.safe_load(a[key]), yaml.safe_load(b[key])) if service_yaml
                                     else (text(a[key]).splitlines(), text(b[key]).splitlines()))
                except yaml.YAMLError:
                    pass
        for rule in rules:
            if rule['route'] != route:
                continue
            for value in (a, b):
                current = value
                parts = rule['path'].split('.')
                for key in parts[:-1]:
                    current = current.get(key, {}) if isinstance(current, dict) else {}
                if isinstance(current, dict) and parts[-1] in current:
                    current[parts[-1]] = '<runtime>'
    out.extend(differences(a, b))
    return out


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args):
        return None


def send(port, method, path, token='', body=None):
    headers = {'Host': '127.0.0.1:5001', 'X-Forwarded-Host': '127.0.0.1:5001',
               'X-Forwarded-Proto': 'http', 'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    request = urllib.request.Request(f'http://127.0.0.1:{port}' + path,
                                     data=json.dumps(body).encode() if body is not None else None,
                                     headers=headers, method=method)
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    try:
        response = opener.open(request, timeout=20)
    except urllib.error.HTTPError as error:
        response = error
    except (OSError, TimeoutError):
        return 0, '', b''
    with response:
        return response.code, response.headers.get('Content-Type', ''), response.read()


def document(data):
    connection = sqlite3.connect(f'file:{data / "configflow.sqlite3"}?mode=ro', uri=True)
    result = {'system': {}, 'shared': {}, 'profiles': {}}
    with connection:
        for scope, owner, field, value in connection.execute('SELECT scope, owner, field, value FROM fields'):
            section = result['profiles'].setdefault(owner, {}) if scope == 'profile' else result.setdefault(scope, {})
            section[field] = json.loads(value)
    connection.close()
    return result


def ids(rows, field='id'):
    return [str(row[field]) for row in rows if isinstance(row, dict) and row.get(field) is not None]


def cases(rows, doc, data):
    profiles = doc['profiles']
    agents = doc['system'].get('agents', [])
    # Opaque job/artifact IDs are read only from local metadata, never printed.
    stored = collections.defaultdict(list)
    for name in ('.generated-previews', 'deployments', 'setup-sessions', 'gateway-installations', 'probes'):
        folder = data / name
        if folder.exists():
            for file in folder.rglob('*.json'):
                try:
                    value = json.loads(file.read_bytes())
                    if isinstance(value, dict) and value.get('id'):
                        stored[name].append(str(value['id']))
                except (OSError, ValueError):
                    pass
    for method, route, *_ in rows:
        generate = (method == 'POST' and (
            '/generate/' in route or route.endswith('/generate-config') or
            route.endswith(('/preview', '/render', '/template-preview', '/rules/compose'))))
        if method != 'GET' and not generate:
            continue
        for profile_id, profile in profiles.items():
            config = dict(doc['shared'], **profile)
            params = {
                'profile_id': [profile_id], 'agent_id': ids(agents),
                'router_id': ids(config.get('routers', {}).get('items', [])),
                'sub_id': ids(config.get('subscriptions', [])),
                'agg_id': ids(config.get('aggregations', [])),
                'resource_id': ids(config.get('aggregations' if '/aggregations/' in route else 'subscriptions', [])),
                'rule_id': ids(config.get('rule_library', [])),
                'name': [str(x.get('name', x.get('id'))) for x in config.get('rule_library', []) if isinstance(x, dict)],
                'service': ['mihomo', 'surge', 'loon', 'mosdns', 'oxidns'],
                'operation': ['status', 'preview'], 'resource': ['configs', 'proxies', 'rules', 'connections', 'version'],
                'filename': ['loon.lcf'] if '/loon/' in route else ['configflow-agent-linux-amd64'],
                'action': ['file', 'download'], 'preview_id': stored['.generated-previews'],
                'deployment_id': stored['deployments'], 'job_id': stored['probes'],
                'session_id': stored['gateway-installations'] if '/gateway-installations/' in route else stored['setup-sessions'],
                'record_id': [], 'token': [],
            }
            names = PARAM.findall(route)
            no_data = any(not params.get(name) for name in names)
            for values in itertools.product(*(params.get(name) or ['proddiff-missing'] for name in names)):
                mapping = dict(zip(names, values))
                path = PARAM.sub(lambda match: urllib.parse.quote(mapping[match[1]], safe=''), route)
                query = {'profile_id': profile_id}
                agent = next((a for a in agents if str(a.get('id')) == mapping.get('agent_id')), {})
                if route.endswith('/config') and '/agents/' in route:
                    query['token'] = agent.get('token', '')
                body = None
                if method == 'POST':
                    body = {'base_url': BASE_URL}
                    if route.endswith('/generate-config'):
                        body.update(expected_profile_id=agent.get('profile_id') or profile_id,
                                    expected_service_type=agent.get('service_type'))
                    elif route.endswith(('/render', '/template-preview')):
                        body['settings'] = config.get('oxidns', {})
                    elif '/linux-gateway/' in route:
                        body.update(settings=config.get('linux_gateway', {}))
                yield method, route, path + '?' + urllib.parse.urlencode(query), body, no_data


def digest_tree(root):
    digest = hashlib.sha256()
    for file in sorted(root.rglob('*')):
        if file.is_symlink():
            raise RuntimeError('snapshot symlinks are not supported')
        if file.is_file():
            digest.update(str(file.relative_to(root)).encode())
            with file.open('rb') as stream:
                for chunk in iter(lambda: stream.read(1024 * 1024), b''):
                    digest.update(chunk)
    return digest.digest()


def main():
    if [name for _, name in socket.if_nameindex()] != ['lo']:
        raise RuntimeError('network isolation missing')
    snapshot, binary = map(Path, sys.argv[1:3])
    record = '--record-baseline' in sys.argv[3:]
    before = digest_tree(snapshot)
    private = Path.home() / 'cf-prod-snapshot/runs'
    report = Path(tempfile.mkdtemp(prefix='run-', dir=private))
    processes = []
    rows = [line.split('\t') for line in (PARITY / 'routes.tsv').read_text().splitlines() if line and not line.startswith('#')]
    rules = json.loads((PARITY / 'proddiff-normalize.json').read_text())
    try:
        with tempfile.TemporaryDirectory(prefix='copies-', dir=report) as temporary:
            copies = [Path(temporary) / name for name in ('python', 'go')]
            for data in copies:
                shutil.copytree(snapshot, data)
            doc = document(copies[0])
            for index, data in enumerate(copies):
                env = dict(os.environ, DATA_DIR=str(data), LOG_FILE=str(data / 'app.log'), LOG_LEVEL='ERROR',
                           ADMIN_USERNAME='proddiff-admin', ADMIN_PASSWORD='proddiff-test-password',
                           JWT_SECRET_KEY='proddiff-test-only-shared-secret', CONFIGFLOW_BACKEND_HOST='127.0.0.1',
                           CONFIGFLOW_BACKEND_PORT=str(15001 + index), CONFIGFLOW_FRONTEND_DIR='',
                           SUB_STORE_BUILTIN_DIR='/nonexistent', PRODDIFF_ROUTES=str(report / 'go-routes.tsv'),
                           PRODDIFF_PY_LOOPS=str(report / 'python-loops.txt'))
                command = [sys.executable, 'server/tools/proddiff/flask_worker.py'] if index == 0 else [str(binary)]
                with (report / ('python.log' if index == 0 else 'go.log')).open('wb') as log:
                    processes.append(subprocess.Popen(command, env=env, stdout=log, stderr=log, start_new_session=True))
            tokens = []
            for index in range(2):
                for _ in range(450):
                    if processes[index].poll() is not None:
                        raise RuntimeError('server startup failed; inspect private logs')
                    reply = send(15001 + index, 'GET', '/api/auth/status')
                    if reply[0] == 200:
                        break
                    time.sleep(.2)
                else:
                    raise RuntimeError('server startup timeout')
                login = send(15001 + index, 'POST', '/api/auth/login', body={'username': 'proddiff-admin', 'password': 'proddiff-test-password'})
                if login[0] != 200:
                    raise RuntimeError('test login failed')
                tokens.append(json.loads(login[2])['token'])
            implemented = set(tuple(line.split('\t')) for line in (report / 'go-routes.tsv').read_text().splitlines())
            # Mutate an actual Go response and pass it through the real comparator.
            real = send(15002, 'GET', '/api/auth/status', tokens[1])
            broken = (real[0] + 1, real[1], real[2])
            if ('status', 'value') not in response_diffs(real, broken, '/api/auth/status', rules):
                raise RuntimeError('self-check failed')
            print('self-check: PASS (mutated Go status detected)', flush=True)
            found, counts = set(), collections.Counter()
            coverage, failures = {}, collections.Counter()
            all_cases = list(cases(rows, doc, copies[0]))
            def compare(case):
                method, route, path, body, no_data = case
                if (method, route) not in implemented:
                    return case, None, None
                return case, send(15001, method, path, tokens[0], body), send(15002, method, path, tokens[1], body)
            with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
                for case, left, right in pool.map(compare, all_cases):
                    method, route, path, body, no_data = case
                    key = method + ' ' + route
                    status = 'Go 尚未实现' if left is None else 'compared'
                    row = coverage.setdefault(key, collections.Counter())
                    row[status] += 1
                    row['no-snapshot-parameter'] += int(no_data)
                    if left is None:
                        continue
                    counts['compared'] += 1
                    if left[0] == 0 or right[0] == 0:
                        failures['both-transport-failure' if left[0] == right[0] == 0 else 'one-sided-transport-failure'] += 1
                    for field, kind in response_diffs(left, right, route, rules):
                        found.add((key, field, kind))
                        domain = 'agents' if '/agents' in route else 'routers' if '/routers' in route else 'config'
                        counts[domain + ':' + kind] += 1
            baseline_path = PARITY / 'proddiff-baseline.txt'
            baseline = set()
            for line in baseline_path.read_text().splitlines():
                if line and not line.startswith('#'):
                    method_route, field, kind, reason = line.split('\t', 3)
                    if not reason.strip():
                        raise RuntimeError('baseline entry needs reason')
                    baseline.add((method_route, field, kind))
            formatted = '\n'.join('\t'.join(row) + '\topen' for row in sorted(found)) + '\n'
            (report / 'differences.tsv').write_text(formatted)
            if record:
                baseline_path.write_text('# Route template\tkey path\tdifference type\treason\n' + formatted)
                baseline = found
            new, stale = found - baseline, baseline - found
            summary = {'GET_routes_total': sum(row[0] == 'GET' for row in rows),
                       'routes_total': len(coverage), 'routes_compared': sum(row['compared'] > 0 for row in coverage.values()),
                       'Go_not_implemented': sum(row['Go 尚未实现'] > 0 for row in coverage.values()),
                       'cases': len(all_cases), 'counts': dict(counts), 'connection_failures': dict(failures),
                       'unique_differences': len(found), 'new': len(new), 'stale': len(stale), 'self_check': 'PASS'}
            (report / 'summary.json').write_text(json.dumps(summary, ensure_ascii=False, indent=2))
            (report / 'coverage.json').write_text(json.dumps(coverage, ensure_ascii=False, indent=2))
            # A safe report is deliberately stored separately from private server logs.
            print(json.dumps(summary, ensure_ascii=False), flush=True)
            print(f'baseline: new={len(new)} stale={len(stale)}' + ('; update baseline' if stale else ''), flush=True)
            for entry in sorted(new):
                print('NEW\t' + '\t'.join(entry), flush=True)
            return int(bool(new or failures['one-sided-transport-failure']))
    finally:
        for process in processes:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGKILL)
            process.wait()
        if digest_tree(snapshot) != before:
            raise RuntimeError('snapshot changed')


if __name__ == '__main__':
    try:
        sys.exit(main())
    except Exception as error:
        # No traceback/exception values: these can contain snapshot secrets.
        print('proddiff: infrastructure failure (' + type(error).__name__ + '); inspect private run directory', flush=True)
        sys.exit(2)
