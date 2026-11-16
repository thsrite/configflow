"""Stateful loopback subscription/rule HTTP source; no external requests."""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import threading


FEED = b'proxies:\n  - {name: synthetic-node, type: socks5, server: 192.0.2.1, port: 1080}\n'


class Source:
    def __init__(self, body=FEED):
        self.requests, self.body = [], body
        self.status = 200
        self.headers = {'Content-Type': 'text/yaml; charset=utf-8'}
        self.responses = {}
        self.before_reply = lambda _path: None
        owner = self
        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_args):
                pass

            def do_GET(self):
                self.respond()

            def do_HEAD(self):
                self.respond(head=True)

            def respond(self, head=False):
                owner.requests.append({'method': self.command, 'path': self.path,
                                       'authorization': self.headers.get('Authorization', ''),
                                       'user_agent': self.headers.get('User-Agent', '')})
                owner.before_reply(self.path)
                status, body, headers = owner.responses.get(self.path, (owner.status, owner.body, owner.headers))
                self.send_response(status)
                for key, value in headers.items():
                    self.send_header(key, value)
                self.send_header('Content-Length', str(len(body)))
                self.end_headers()
                if not head:
                    self.wfile.write(body)
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
