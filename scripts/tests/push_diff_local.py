#!/usr/bin/env python3
"""「待推送的变更」 in a private ConfigFlow on the test server, with no device behind the Agent.

Run on the test server only, from this checkout synced by rt. Touches no PVE guest:

  setup    fresh private data in /tmp/cf-pushdiff-local (refuses to overwrite): a subscription served by a
           local fixture, a manual SS node with a password, a content rule set, ConfigFlow's access token
           (系统设置 → 访问令牌) and one Mihomo Agent record
  serve    the private ConfigFlow on 127.0.0.1:15951 (foreground), plus the subscription fixture and a stand-in
           for the Agent's HTTP API: it answers capabilities and accepts a deployment (succeeded), so the push
           route stores the applied receipt and bundle exactly as for a real device. The record stays online.
  start    serve in the background (its own session, no inherited descriptors such as rt's lock) and wait
  prepare  (server running) push once as the baseline through the API, wait for 已同步, then make the edits of
           push_diff_lab.py change through the same API as the pages, plus a second subscription in the group
           (its pull URL carries the access token)
  stop     stop a background serve;  clean  remove the private data

Then: CF_PUSHDIFF_WORK=/tmp/cf-pushdiff-local CF_PUSHDIFF_AGENT='实验 Mihomo' node scripts/tests/push_diff_browser.mjs changed
"""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import secrets
import shutil
import signal
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
WORK = Path('/tmp/cf-pushdiff-local')
API = 15951
SUB = 15956
AGENT_PORT = 15957
AGENT_ID = 'local-mihomo'
AGENT_NAME = '实验 Mihomo'
AGENT_VERSION = '0a1b2c3d'  # the main file digest a real Agent reports after installing
PASSWORD_BEFORE = 'lab-manual-pass-before-7c1e'
PASSWORD_AFTER = 'lab-manual-pass-after-93ab'
CONTROLLER_SECRET = 'lab-controller-secret-5d20'
RULES_BEFORE = '+.alpha.lab.invalid\n+.beta.lab.invalid\n+.gamma.lab.invalid'
RULES_AFTER = '+.alpha.lab.invalid\n+.gamma.lab.invalid\n+.delta.lab.invalid\n+.epsilon.lab.invalid'
HOSTS_DOMAIN = 'printer.lab.invalid'
CUSTOM = '''mixed-port: 7890
allow-lan: true
mode: rule
log-level: info
external-controller: 127.0.0.1:9090
dns:
  enable: true
  listen: 127.0.0.1:1053
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  nameserver: [198.51.100.53]
  proxy-server-nameserver: [198.51.100.53]
'''
NODES = '''proxies:
  - {name: A 香港, type: socks5, server: edge-a.lab.invalid, port: 1080}
  - {name: B 日本, type: socks5, server: edge-b.lab.invalid, port: 1080}
'''
SUBSCRIPTIONS = {
    'airport-v1': NODES,
    'airport-v2': NODES + '  - {name: C 新增节点, type: socks5, server: edge-c.lab.invalid, port: 1080}\n',
    'backup': 'proxies:\n  - {name: D 备用, type: socks5, server: backup.lab.invalid, port: 1080}\n',
}


def save(name, value):
    path = WORK / name
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, ensure_ascii=False, indent=1))
    path.chmod(0o600)


def credentials():
    return json.loads((WORK / 'credentials.json').read_text())


def environment(auth=True):
    c = credentials()
    env = {'DATA_DIR': str(WORK / 'data'), 'LOG_LEVEL': 'INFO', 'JWT_SECRET_KEY': c['jwt_secret'],
           'AGENT_REGISTRATION_KEY': c['registration_key'], 'SUB_STORE_BUILTIN_DIR': str(WORK / 'no-sub-store'),
           'SUB_STORE_URL': 'http://127.0.0.1:9', 'CONFIGFLOW_BACKEND_HOST': '127.0.0.1',
           'CONFIGFLOW_BACKEND_PORT': str(API)}
    if auth:
        env.update(ADMIN_USERNAME=c['username'], ADMIN_PASSWORD=c['password'])
    return env


def _use_environment(auth=True):
    for key in ('http_proxy', 'https_proxy', 'all_proxy', 'HTTP_PROXY', 'HTTPS_PROXY', 'ALL_PROXY'):
        os.environ.pop(key, None)
    os.environ.update(environment(auth))
    sys.path.insert(0, str(ROOT))
    from backend.common import config as store
    from backend.common.config_repository import ProfileRepository
    store.set_repository(ProfileRepository(WORK / 'data', default_config_factory=store.get_default_config,
                                           initial_config_factory=store.get_default_config))
    return store.get_repository()


def write_subscription(name, version):
    path = WORK / 'sub' / f'{name}.yaml'
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(SUBSCRIPTIONS[version])


def setup():
    if WORK.exists():
        raise SystemExit(f'{WORK} exists; run clean first')
    WORK.mkdir(mode=0o700)
    save('credentials.json', {'username': 'admin', 'password': secrets.token_urlsafe(18),
                              'jwt_secret': secrets.token_hex(32), 'registration_key': secrets.token_urlsafe(24),
                              'config_token': secrets.token_hex(16)})
    write_subscription('airport', 'airport-v1')
    write_subscription('backup', 'backup')
    repo = _use_environment(auth=False)
    sub_url = f'http://127.0.0.1:{SUB}/'
    repo.update_system_transaction(lambda system: system['system_config'].update(
        server_domain=f'http://127.0.0.1:{API}', config_token=credentials()['config_token']))

    def shared(document):
        document['subscriptions'] = [
            {'id': 'lab-airport', 'name': '实验机场', 'url': sub_url + 'airport.yaml', 'enabled': True, 'interval': 86400},
            {'id': 'lab-backup', 'name': '备用机场', 'url': sub_url + 'backup.yaml', 'enabled': True, 'interval': 86400}]
        document['nodes'] = [{'id': 'lab-manual', 'name': '手动 SS', 'type': 'ss', 'server': 'manual.lab.invalid',
                              'port': 8388, 'enabled': True,
                              'params': {'cipher': 'aes-128-gcm', 'password': PASSWORD_BEFORE}}]

    repo.update_shared_transaction(shared)
    rule = {'id': 'lab-direct', 'name': '实验直连', 'source_type': 'content', 'behavior': 'domain', 'format': 'text',
            'content': RULES_BEFORE, 'enabled': True}
    repo.create_rule_library_item(rule, RULES_BEFORE)

    def profile(document):
        document['proxy_groups'] = [{'id': 'lab-group', 'name': '代理', 'type': 'select', 'subscriptions': ['lab-airport'],
                                     'manual_nodes': ['lab-manual'], 'enabled': True}]
        document['rule_configs'] = [
            {'id': 'lab-direct-ref', 'itemType': 'ruleset', 'name': '实验直连', 'library_rule_id': 'lab-direct',
             'policy': 'DIRECT', 'enabled': True, 'behavior': 'domain'},
            {'id': 'lab-match', 'itemType': 'rule', 'rule_type': 'MATCH', 'policy': '代理', 'enabled': True}]
        document.setdefault('mihomo', {}).update(custom_config=CUSTOM, provider_update_interval=3600)

    repo.update_profile_transaction(repo.default_profile_id(), profile)
    from backend.common.agent_manager import get_agent_manager
    now = time.strftime('%Y-%m-%dT%H:%M:%S')
    agent = {'id': AGENT_ID, 'name': AGENT_NAME, 'host': '127.0.0.1', 'port': AGENT_PORT,
             'token': secrets.token_urlsafe(24), 'service_type': 'mihomo', 'profile_id': 'default',
             'deployment_method': 'shell', 'status': 'online', 'version': '1.8.50-go', 'config_version': '0',
             'enabled': True, 'last_heartbeat': now, 'created_at': now}
    get_agent_manager()._update_agents(lambda rows: rows.__setitem__(slice(None), [agent]))
    print(json.dumps({'work': str(WORK), 'agent': AGENT_ID, 'profile': repo.default_profile_id()}, ensure_ascii=False))


class _Files(BaseHTTPRequestHandler):
    def do_GET(self):
        path = WORK / 'sub' / Path(self.path.split('?')[0]).name
        if not path.is_file():
            self.send_error(404)
            return
        body = path.read_bytes()
        self.send_response(200)
        self.send_header('Content-Type', 'text/yaml; charset=utf-8')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass


class _Agent(BaseHTTPRequestHandler):
    """The two answers the push route needs from a Mihomo Agent; nothing is installed anywhere."""

    def _answer(self, status, body):
        data = json.dumps(body).encode()
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        if self.path == '/api/capabilities':
            self._answer(200, {'deployment_protocols': [1], 'service_type': 'mihomo', 'config_path': 'config.yaml'})
        else:
            self._answer(404, {'success': False, 'message': 'not available in the stand-in Agent'})

    def do_POST(self):
        self.rfile.read(int(self.headers.get('Content-Length') or 0))
        if self.path == '/api/deployments':
            self._answer(200, {'success': True, 'status': 'succeeded', 'config_version': AGENT_VERSION})
        else:
            self._answer(404, {'success': False, 'message': 'not available in the stand-in Agent'})

    def log_message(self, *args):
        pass


def serve():
    repo = _use_environment()
    for port, handler in ((SUB, _Files), (AGENT_PORT, _Agent)):
        server = ThreadingHTTPServer(('127.0.0.1', port), handler)
        threading.Thread(target=server.serve_forever, daemon=True).start()
    from datetime import datetime
    from backend.common.agent_manager import get_agent_manager
    manager = get_agent_manager()

    def beat():  # no Agent heartbeats to this private copy; keep the record online
        while True:
            def change(rows):
                for row in rows:
                    if row['id'] == AGENT_ID:
                        row.update(status='online', last_heartbeat=datetime.now().isoformat())
            manager._update_agents(change)
            time.sleep(15)

    threading.Thread(target=beat, daemon=True).start()
    (WORK / 'server.pid').write_text(str(os.getpid()))
    assert repo is not None
    from backend.app import app
    from backend.http_server import run_http_server
    run_http_server(app)


_token = None


def api(method, path, body=None, *, timeout=300):
    global _token
    if _token is None:
        c = credentials()
        request = urllib.request.Request(f'http://127.0.0.1:{API}/api/auth/login', method='POST',
                                         data=json.dumps({'username': c['username'], 'password': c['password']}).encode(),
                                         headers={'Content-Type': 'application/json'})
        _token = json.load(urllib.request.urlopen(request, timeout=30))['token']
    request = urllib.request.Request(f'http://127.0.0.1:{API}/api{path}', method=method,
                                     data=None if body is None else json.dumps(body).encode(),
                                     headers={'Content-Type': 'application/json', 'Authorization': f'Bearer {_token}'})
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            status, data = response.status, json.loads(response.read() or b'null')
    except urllib.error.HTTPError as error:
        status, data = error.code, json.loads(error.read() or b'null')
    assert status in (200, 201, 202), (method, path, status, data)
    return data


def wait_server():
    deadline = time.monotonic() + 90
    while time.monotonic() < deadline:
        try:
            urllib.request.urlopen(f'http://127.0.0.1:{API}/api/auth/status', timeout=2)
            return
        except Exception:
            time.sleep(0.5)
    raise SystemExit('server did not start; see ' + str(WORK / 'server.log'))


def diff_status(expected):
    deadline = time.monotonic() + 180
    while True:
        body = api('GET', f'/agents/{AGENT_ID}/push-diff')
        if body.get('status') != 'checking' or time.monotonic() > deadline:
            break
        time.sleep(1.5)
    assert body.get('status') == expected, body
    return body


def prepare():
    wait_server()
    result = api('POST', f'/agents/{AGENT_ID}/push-config', {'deployment_id': f'baseline-{int(time.time())}',
                                                              'base_url': f'http://127.0.0.1:{API}'})
    assert result.get('success') is not False, result
    pushed = time.monotonic()  # the push downloaded the subscriptions
    diff_status('synced')
    node = next(item for item in api('GET', '/nodes') if item['id'] == 'lab-manual')
    node['params']['password'] = PASSWORD_AFTER
    api('PUT', '/nodes/lab-manual', node)
    rule = next(item for item in api('GET', '/rule-library') if item['id'] == 'lab-direct')
    rule.pop('has_ip_part', None)
    api('PUT', '/rule-library/lab-direct', {**rule, 'content': RULES_AFTER})
    custom = CUSTOM + f'secret: {CONTROLLER_SECRET}\nhosts:\n  {HOSTS_DOMAIN}: 198.51.100.30\n'
    api('POST', '/custom-config/mihomo', {'config': custom, 'provider_update_interval': 3600})
    write_subscription('airport', 'airport-v2')
    # 订阅来源 → 更新 reuses a download of the same URL for 30 seconds.
    time.sleep(max(0.0, 31 - (time.monotonic() - pushed)))
    api('POST', '/subscriptions/lab-airport/fetch', {'preview': True})
    group = next(item for item in api('GET', '/proxy-groups') if item['id'] == 'lab-group')
    api('PUT', '/proxy-groups/lab-group', {**group, 'subscriptions': ['lab-airport', 'lab-backup']})
    api('POST', '/subscriptions/lab-backup/fetch', {'preview': True})
    body = diff_status('changed')
    save('evidence/prepared-push-diff.json', body)
    print(json.dumps({'counts': body.get('counts'), 'files': [(item['path'], item['status']) for item in body['files']],
                      'items': [item['key'] for item in body['config']['items']]}, ensure_ascii=False))


def start():
    stop()
    log = open(WORK / 'server.log', 'ab')
    subprocess.Popen([sys.executable, str(Path(__file__).resolve()), 'serve'], cwd=ROOT, stdout=log,
                     stderr=subprocess.STDOUT, stdin=subprocess.DEVNULL, start_new_session=True, close_fds=True)
    wait_server()
    print(json.dumps({'api': f'http://127.0.0.1:{API}', 'pid': int((WORK / 'server.pid').read_text())}))


def stop():
    path = WORK / 'server.pid'
    if path.exists():
        try:
            os.kill(int(path.read_text()), signal.SIGTERM)
        except (ProcessLookupError, ValueError):
            pass
        path.unlink()


def clean():
    stop()
    shutil.rmtree(WORK, ignore_errors=True)


if __name__ == '__main__':
    import socket
    assert '192.168.0.32' in socket.gethostbyname_ex(socket.gethostname())[2] or os.path.exists('/home/sunbroke/rt'), \
        'Run this on the test server'
    {'setup': setup, 'serve': serve, 'start': start, 'prepare': prepare, 'stop': stop, 'clean': clean}[sys.argv[1]]()
