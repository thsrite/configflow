"""Bounded, recorded Agent HTTP fake; no production Agent code is imported."""
import copy
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import threading

from seed import AGENT

CONTROLS = [{'host': 'www.baidu.com', 'url': 'https://www.baidu.com/'},
            {'host': 'www.gstatic.com', 'url': 'https://www.gstatic.com/generate_204'}]


def probe_calls(hosts, proxy='代理🧪'):
    return [{'method': 'POST', 'path': '/api/domain-probe', 'authorization': 'Bearer ' + AGENT,
             'content_type': 'application/json', 'body': {'targets': CONTROLS + [
                 {'host': host, 'url': 'https://' + host + '/'} for host in hosts[start:start + 18]],
                 'paths': ['DIRECT', proxy], 'attempts': 3, 'timeout_ms': 5000}}
            for start in range(0, len(hosts), 18)]


def probe_result(request, scenario='success'):
    if scenario == '404':
        return 404, {'success': False, 'message': 'synthetic unsupported'}
    if scenario == '500':
        return 500, {'success': False, 'message': '合成Agent失败🧪'}
    if scenario == 'invalid-json':
        return 200, b'<synthetic invalid JSON>'
    if scenario == 'false':
        return 200, {'success': False, 'message': '合成Agent拒绝'}
    rows = []
    for index, target in enumerate(request['body'].get('targets', [])):
        paths = {name: {'ok': 3, 'total': 3, 'delays': [10, 20, 30], 'errors': {}}
                 for name in request['body'].get('paths', [])}
        if index >= 2 and target['host'].startswith('proxy.'):
            paths['DIRECT'] = {'ok': 0, 'total': 3, 'delays': [], 'errors': {'timeout': 3}}
        rows.append({'host': target['host'], 'paths': paths})
    if scenario == 'bad-count':
        rows = rows[:-1]
    return 200, {'success': True, 'results': rows}


class Agent:
    def __init__(self, responder=None, *, gate_index=0):
        self.responder = responder or (lambda request, index: probe_result(request))
        self.lock = threading.Lock()
        self.calls, self.errors = [], []
        self.seen, self.release = threading.Event(), threading.Event()
        owner = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_args):
                pass

            def request(self):
                raw = self.rfile.read(int(self.headers.get('Content-Length', 0)))
                try:
                    body = json.loads(raw) if raw else None
                except ValueError:
                    body = {'invalid_bytes': raw.hex()}
                request = {'method': self.command, 'path': self.path, 'body': body,
                           'authorization': self.headers.get('Authorization', ''),
                           'content_type': self.headers.get('Content-Type', '')}
                with owner.lock:
                    index = len(owner.calls)
                    owner.calls.append(request)
                # Hold the selected outbound call, so running and busy states
                # are observable. Always bounded even when a server is broken.
                if index == gate_index:
                    owner.seen.set()
                    if not owner.release.wait(20):
                        owner.errors.append('fake gate expired before explicit release')
                status, value = owner.responder(request, index)
                content = value if isinstance(value, bytes) else json.dumps(value, ensure_ascii=False).encode()
                self.send_response(status)
                self.send_header('Content-Type', 'application/json')
                self.send_header('Content-Length', str(len(content)))
                self.end_headers()
                try:
                    self.wfile.write(content)
                except (BrokenPipeError, ConnectionResetError):
                    owner.errors.append('panel disconnected before fake reply')

            do_POST = do_GET = request

        self.server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.port = self.server.server_port

    def snapshot(self):
        with self.lock:
            return copy.deepcopy(self.calls)

    def __enter__(self):
        self.thread.start()
        return self

    def __exit__(self, *_args):
        self.release.set()
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()
