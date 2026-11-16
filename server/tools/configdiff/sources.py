"""Stateful loopback subscription/rule HTTP source; no external requests."""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import threading


FEED = b'proxies:\n  - {name: synthetic-node, type: socks5, server: 192.0.2.1, port: 1080}\n'


class Source:
    def __init__(self, body=FEED):
        self.requests, self.body = [], body
        self.status = 200
        self.headers = {'Content-Type': 'text/yaml; charset=utf-8'}
        self.before_reply = lambda _path: None
        owner = self
        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_args):
                pass

            def do_GET(self):
                owner.requests.append({'method': self.command, 'path': self.path,
                                       'authorization': self.headers.get('Authorization', ''),
                                       'user_agent': self.headers.get('User-Agent', '')})
                owner.before_reply(self.path)
                self.send_response(owner.status)
                for key, value in owner.headers.items():
                    self.send_header(key, value)
                self.send_header('Content-Length', str(len(owner.body)))
                self.end_headers()
                self.wfile.write(owner.body)
        self.server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.url = 'http://127.0.0.1:' + str(self.server.server_port)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)

    def __enter__(self):
        self.thread.start()
        return self

    def __exit__(self, *_args):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()
