"""End-to-end rejection of one deliberately corrupted real Go HTTP response."""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import threading

from harness import Pair, Recorder, DesignError, send


def verify_harness(binary):
    recorder = Recorder()
    with Pair(binary, recorder) as pair:
        target = pair.bases[1]
        baseline = [send(base, 'GET', '/api/auth/status').json() for base in pair.bases]
        if baseline != [{'authEnabled': True}, {'authEnabled': True}]:
            raise DesignError('mutation self-check baseline auth/status is not enabled on both actual servers')
        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_args):
                pass

            def do_GET(self):
                response = send(target, 'GET', self.path, headers=dict(self.headers))
                body = response.json()
                body['authEnabled'] = False
                raw = json.dumps(body).encode()
                self.send_response(response.status)
                for key, value in response.headers.items():
                    if key not in ('content-length', 'transfer-encoding', 'connection'):
                        self.send_header(key, value)
                self.send_header('Content-Length', str(len(raw)))
                self.end_headers()
                self.wfile.write(raw)
        server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            pair.bases[1] = 'http://127.0.0.1:' + str(server.server_port)
            pair.request('MUTATION', 'GET /api/auth/status', auth='', expect=200)
            result = recorder.results[-1]
            if result['state'] != 'different' or not any(d['path'] == 'body.authEnabled' for d in result['differences']):
                raise DesignError('harness accepted a deliberately wrong real Go HTTP authEnabled response')
        finally:
            pair.bases[1] = target
            server.shutdown()
            server.server_close()
            thread.join()
    print('MUTATION-CHECK detected deliberately corrupted Go body.authEnabled', flush=True)
    return True
